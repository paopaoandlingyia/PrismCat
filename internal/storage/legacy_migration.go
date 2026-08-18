package storage

import (
	"context"
	"fmt"
)

// MigrateLegacyBodies externalizes body columns from pre-log_bodies databases.
// Columns are dropped only after every inline body has been stored, making a
// failed migration retryable without losing the legacy source data.
func (r *SQLiteRepository) MigrateLegacyBodies(blobs BlobStore) error {
	legacyColumns := []string{"request_body", "request_body_original", "request_body_final", "request_body_ref", "response_body", "response_body_ref"}
	present := make(map[string]bool, len(legacyColumns))
	legacy := false
	for _, column := range legacyColumns {
		has, err := r.hasColumn("request_logs", column)
		if err != nil {
			return err
		}
		present[column] = has
		legacy = legacy || has
	}
	if !legacy {
		return nil
	}
	if blobs == nil {
		return fmt.Errorf("legacy body migration requires a blob store")
	}
	expr := func(column string) string {
		if present[column] {
			return "COALESCE(" + column + ", '')"
		}
		return "''"
	}
	query := fmt.Sprintf(`
		SELECT id, COALESCE(request_headers, ''), COALESCE(response_headers, ''),
			COALESCE(request_body_size, 0), COALESCE(response_body_size, 0), COALESCE(truncated, 0),
			%s, %s, %s, %s, %s, %s
		FROM request_logs
	`, expr("request_body"), expr("request_body_original"), expr("request_body_final"),
		expr("request_body_ref"), expr("response_body"), expr("response_body_ref"))
	rows, err := r.db.Query(query)
	if err != nil {
		return fmt.Errorf("query legacy bodies: %w", err)
	}
	type legacyRow struct {
		id, reqHeaders, respHeaders                        string
		reqSize, respSize                                  int64
		truncated                                          int
		request, requestOriginal, requestFinal, requestRef string
		response, responseRef                              string
	}
	var legacyRows []legacyRow
	for rows.Next() {
		var row legacyRow
		if err := rows.Scan(&row.id, &row.reqHeaders, &row.respHeaders, &row.reqSize, &row.respSize,
			&row.truncated, &row.request, &row.requestOriginal, &row.requestFinal, &row.requestRef,
			&row.response, &row.responseRef); err != nil {
			_ = rows.Close()
			return err
		}
		legacyRows = append(legacyRows, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	ctx := context.Background()
	for _, row := range legacyRows {
		reqHeaders := unmarshalHeaders(row.reqHeaders)
		respHeaders := unmarshalHeaders(row.respHeaders)
		requestType := FirstHeaderValue(reqHeaders, "Content-Type")
		requestEncoding := FirstHeaderValue(reqHeaders, "Content-Encoding")
		responseType := FirstHeaderValue(respHeaders, "Content-Type")
		responseEncoding := FirstHeaderValue(respHeaders, "Content-Encoding")
		var bodies []LogBody
		add := func(part, inline, ref string, total int64, contentType, contentEncoding string) error {
			if ref == "" && inline == "" {
				return nil
			}
			body := LogBody{LogID: row.id, Part: part, BlobRef: ref, ContentType: contentType,
				ContentEncoding: contentEncoding, TotalBytes: total, Representation: "wire"}
			if ref != "" {
				data, err := blobs.Get(ctx, ref)
				if err != nil {
					return fmt.Errorf("legacy log %s body %s ref %s: %w", row.id, part, ref, err)
				}
				body.CapturedBytes = int64(len(data))
				if body.TotalBytes <= 0 {
					body.TotalBytes = body.CapturedBytes
				}
			} else {
				data := []byte(inline)
				storedRef, err := blobs.Put(ctx, data)
				if err != nil {
					return fmt.Errorf("legacy log %s body %s: %w", row.id, part, err)
				}
				body.BlobRef = storedRef
				body.CapturedBytes = int64(len(data))
				if body.TotalBytes <= 0 {
					body.TotalBytes = body.CapturedBytes
				}
				// Inline legacy values were display-formatted and may already have
				// decoded Content-Encoding, so they must not be decoded again.
				body.Representation = "display"
			}
			body.Truncated = row.truncated == 1 && body.CapturedBytes < body.TotalBytes
			body.Recoverable = !body.Truncated
			bodies = append(bodies, body)
			return nil
		}
		if err := add(BodyPartRequest, row.request, row.requestRef, row.reqSize, requestType, requestEncoding); err != nil {
			return err
		}
		if err := add(BodyPartRequestOriginal, row.requestOriginal, "", int64(len(row.requestOriginal)), requestType, requestEncoding); err != nil {
			return err
		}
		if err := add(BodyPartRequestFinal, row.requestFinal, "", int64(len(row.requestFinal)), requestType, requestEncoding); err != nil {
			return err
		}
		if err := add(BodyPartResponse, row.response, row.responseRef, row.respSize, responseType, responseEncoding); err != nil {
			return err
		}
		for _, body := range bodies {
			if _, err := r.db.Exec(`
				INSERT INTO log_bodies (log_id, part, blob_ref, captured_bytes, total_bytes, truncated, content_type, content_encoding, representation)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(log_id, part) DO UPDATE SET blob_ref=excluded.blob_ref,
					captured_bytes=excluded.captured_bytes, total_bytes=excluded.total_bytes,
					truncated=excluded.truncated, content_type=excluded.content_type,
					content_encoding=excluded.content_encoding, representation=excluded.representation
			`, body.LogID, body.Part, body.BlobRef, body.CapturedBytes, body.TotalBytes,
				boolInt(body.Truncated), body.ContentType, body.ContentEncoding, body.Representation); err != nil {
				return err
			}
		}
	}

	for _, column := range legacyColumns {
		if !present[column] {
			continue
		}
		if _, err := r.db.Exec("ALTER TABLE request_logs DROP COLUMN " + column); err != nil {
			return fmt.Errorf("drop legacy body column %s: %w", column, err)
		}
	}
	return nil
}
