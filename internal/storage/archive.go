package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *SQLiteRepository) RecoverInterruptedArchiveWork(now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ms := now.UTC().UnixMilli()
	if _, err := tx.Exec(`UPDATE archive_jobs SET status='failed',
		error='interrupted before completion; retry is safe', updated_at_unix_ms=?, completed_at_unix_ms=?
		WHERE status IN ('queued', 'running')`, ms, ms); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE archive_batches SET status='failed',
		error='interrupted before verification; retry is safe', updated_at_unix_ms=?
		WHERE status IN ('building', 'uploading')`, ms); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM archive_batch_items"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) OldestUnarchivedLogTime(before time.Time) (*time.Time, error) {
	var ms sql.NullInt64
	err := r.db.QueryRow(`
		SELECT MIN(created_at_unix_ms) FROM request_logs
		WHERE created_at_unix_ms < ?
		  AND origin != 'archive_import'
		  AND backup_verified_at_unix_ms IS NULL
	`, before.UTC().UnixMilli()).Scan(&ms)
	if err != nil || !ms.Valid {
		return nil, err
	}
	v := time.UnixMilli(ms.Int64).UTC()
	return &v, nil
}

func (r *SQLiteRepository) CreateArchiveJob(job ArchiveJob) error {
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	_, err := r.db.Exec(`INSERT INTO archive_jobs (
		id, trigger, cutoff_unix_ms, status, package_count, log_count, error,
		created_at_unix_ms, updated_at_unix_ms, completed_at_unix_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Trigger, job.Cutoff.UTC().UnixMilli(), job.Status, job.PackageCount,
		job.LogCount, job.Error, job.CreatedAt.UnixMilli(), job.UpdatedAt.UnixMilli(), nullableTimeMS(job.CompletedAt))
	return err
}

func (r *SQLiteRepository) UpdateArchiveJob(job ArchiveJob) error {
	job.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`UPDATE archive_jobs SET status=?, package_count=?, log_count=?, error=?,
		updated_at_unix_ms=?, completed_at_unix_ms=? WHERE id=?`, job.Status, job.PackageCount,
		job.LogCount, job.Error, job.UpdatedAt.UnixMilli(), nullableTimeMS(job.CompletedAt), job.ID)
	return err
}

func (r *SQLiteRepository) ListArchiveJobs(limit int) ([]ArchiveJob, error) {
	items, _, err := r.ListArchiveJobsPage(0, limit)
	return items, err
}

func (r *SQLiteRepository) ListArchiveJobsPage(offset, limit int) ([]ArchiveJob, int64, error) {
	offset, limit = archivePageBounds(offset, limit)
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM archive_jobs").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(`SELECT id, trigger, cutoff_unix_ms, status, package_count,
		log_count, error, created_at_unix_ms, updated_at_unix_ms, completed_at_unix_ms
		FROM archive_jobs ORDER BY created_at_unix_ms DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ArchiveJob
	for rows.Next() {
		var j ArchiveJob
		var cutoffMS, createdMS, updatedMS int64
		var completedMS sql.NullInt64
		if err := rows.Scan(&j.ID, &j.Trigger, &cutoffMS, &j.Status, &j.PackageCount,
			&j.LogCount, &j.Error, &createdMS, &updatedMS, &completedMS); err != nil {
			return nil, 0, err
		}
		j.Cutoff = time.UnixMilli(cutoffMS).UTC()
		j.CreatedAt = time.UnixMilli(createdMS).UTC()
		j.UpdatedAt = time.UnixMilli(updatedMS).UTC()
		if completedMS.Valid {
			v := time.UnixMilli(completedMS.Int64).UTC()
			j.CompletedAt = &v
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

func (r *SQLiteRepository) CreateArchiveBatch(batch ArchiveBatch) error {
	now := time.Now().UTC()
	if batch.CreatedAt.IsZero() {
		batch.CreatedAt = now
	}
	if batch.UpdatedAt.IsZero() {
		batch.UpdatedAt = now
	}
	_, err := r.db.Exec(`INSERT INTO archive_batches (
		id, job_id, archive_date, object_key, manifest_key, range_start_unix_ms, range_end_unix_ms,
		status, log_count, body_count, logical_bytes, compressed_bytes, sha256, error,
		created_at_unix_ms, updated_at_unix_ms, verified_at_unix_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		batch.ID, batch.JobID, batch.ArchiveDate, batch.ObjectKey, batch.ManifestKey,
		batch.RangeStart.UTC().UnixMilli(), batch.RangeEnd.UTC().UnixMilli(), batch.Status,
		batch.LogCount, batch.BodyCount, batch.LogicalBytes, batch.CompressedBytes, batch.SHA256,
		batch.Error, batch.CreatedAt.UnixMilli(), batch.UpdatedAt.UnixMilli(), nullableTimeMS(batch.VerifiedAt))
	return err
}

func (r *SQLiteRepository) UpdateArchiveBatch(batch ArchiveBatch) error {
	batch.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`UPDATE archive_batches SET job_id=?, object_key=?, manifest_key=?,
		range_start_unix_ms=?, range_end_unix_ms=?, status=?, log_count=?, body_count=?,
		logical_bytes=?, compressed_bytes=?, sha256=?, error=?, updated_at_unix_ms=?,
		verified_at_unix_ms=? WHERE id=?`, batch.JobID, batch.ObjectKey, batch.ManifestKey,
		batch.RangeStart.UTC().UnixMilli(), batch.RangeEnd.UTC().UnixMilli(), batch.Status,
		batch.LogCount, batch.BodyCount, batch.LogicalBytes, batch.CompressedBytes, batch.SHA256,
		batch.Error, batch.UpdatedAt.UnixMilli(), nullableTimeMS(batch.VerifiedAt), batch.ID)
	return err
}

func (r *SQLiteRepository) ListArchiveBatches(limit int) ([]ArchiveBatch, error) {
	items, _, err := r.ListArchiveBatchesPage(ArchiveBatchFilter{Limit: limit})
	return items, err
}

func (r *SQLiteRepository) ListArchiveBatchesPage(filter ArchiveBatchFilter) ([]ArchiveBatch, int64, error) {
	filter.Offset, filter.Limit = archivePageBounds(filter.Offset, filter.Limit)
	where := make([]string, 0, 3)
	args := make([]any, 0, 5)
	if filter.DateType == ArchiveDateTypeArchiveDate && strings.TrimSpace(filter.Date) != "" {
		where = append(where, "b.archive_date=?")
		args = append(args, strings.TrimSpace(filter.Date))
	}
	if filter.DateType == ArchiveDateTypeCompletedAt && filter.CompletedFrom != nil && filter.CompletedTo != nil {
		where = append(where, "COALESCE(b.verified_at_unix_ms, b.updated_at_unix_ms)>=? AND COALESCE(b.verified_at_unix_ms, b.updated_at_unix_ms)<?")
		args = append(args, filter.CompletedFrom.UTC().UnixMilli(), filter.CompletedTo.UTC().UnixMilli())
	}
	if strings.TrimSpace(filter.JobID) != "" {
		where = append(where, "b.job_id=?")
		args = append(args, strings.TrimSpace(filter.JobID))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM archive_batches b"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any(nil), args...), filter.Limit, filter.Offset)
	rows, err := r.db.Query(`SELECT b.id, b.job_id, COALESCE(j.trigger, ''), b.archive_date, b.object_key, b.manifest_key,
		b.range_start_unix_ms, b.range_end_unix_ms, b.status, b.log_count, b.body_count,
		b.logical_bytes, b.compressed_bytes, b.sha256, b.error, b.created_at_unix_ms,
		b.updated_at_unix_ms, b.verified_at_unix_ms
		FROM archive_batches b LEFT JOIN archive_jobs j ON j.id=b.job_id`+whereSQL+`
		ORDER BY COALESCE(b.verified_at_unix_ms, b.updated_at_unix_ms, b.created_at_unix_ms) DESC, b.id DESC
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ArchiveBatch
	for rows.Next() {
		var b ArchiveBatch
		var startMS, endMS, createdMS, updatedMS int64
		var verifiedMS sql.NullInt64
		if err := rows.Scan(&b.ID, &b.JobID, &b.Trigger, &b.ArchiveDate, &b.ObjectKey, &b.ManifestKey,
			&startMS, &endMS, &b.Status, &b.LogCount, &b.BodyCount, &b.LogicalBytes,
			&b.CompressedBytes, &b.SHA256, &b.Error, &createdMS, &updatedMS, &verifiedMS); err != nil {
			return nil, 0, err
		}
		b.RangeStart = time.UnixMilli(startMS).UTC()
		b.RangeEnd = time.UnixMilli(endMS).UTC()
		b.CreatedAt = time.UnixMilli(createdMS).UTC()
		b.UpdatedAt = time.UnixMilli(updatedMS).UTC()
		if verifiedMS.Valid {
			v := time.UnixMilli(verifiedMS.Int64).UTC()
			b.VerifiedAt = &v
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func (r *SQLiteRepository) ReserveArchiveBatchLogs(batchID string, start, end time.Time) (int64, error) {
	result, err := r.db.Exec(`INSERT OR IGNORE INTO archive_batch_items (batch_id, log_id)
		SELECT ?, id FROM request_logs
		WHERE created_at_unix_ms>=? AND created_at_unix_ms<?
		  AND origin!='archive_import' AND backup_verified_at_unix_ms IS NULL`,
		batchID, start.UTC().UnixMilli(), end.UTC().UnixMilli())
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

func (r *SQLiteRepository) ReleaseArchiveBatchLogs(batchID string) error {
	_, err := r.db.Exec("DELETE FROM archive_batch_items WHERE batch_id=?", batchID)
	return err
}

func (r *SQLiteRepository) ExportArchiveBatch(ctx context.Context, batchID string, each func(*RequestLog) error) error {
	rows, err := r.db.QueryContext(ctx, `SELECT i.log_id FROM archive_batch_items i
		JOIN request_logs l ON l.id=i.log_id WHERE i.batch_id=?
		ORDER BY l.created_at_unix_ms, i.log_id`, batchID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := r.GetLog(id)
		if err != nil {
			return err
		}
		if err := each(entry); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) MarkArchiveBatchVerified(batchID string, verifiedAt time.Time) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	ms := verifiedAt.UTC().UnixMilli()
	result, err := tx.Exec(`UPDATE request_logs SET backup_verified_at_unix_ms=?,
		backup_batch_id=?, delete_grace_started_at_unix_ms=?
		WHERE backup_verified_at_unix_ms IS NULL
		  AND id IN (SELECT log_id FROM archive_batch_items WHERE batch_id=?)`, ms, batchID, ms, batchID)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE archive_batches SET status='verified', error='',
		verified_at_unix_ms=?, updated_at_unix_ms=? WHERE id=?`, ms, ms, batchID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM archive_batch_items WHERE batch_id=?", batchID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

func (r *SQLiteRepository) DeleteEligibleBackedLogs(cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 2000
	}
	result, err := r.db.Exec(`DELETE FROM request_logs WHERE id IN (
		SELECT l.id FROM request_logs l LEFT JOIN log_annotations a ON a.log_id=l.id
		WHERE l.origin='live' AND l.backup_verified_at_unix_ms IS NOT NULL
		  AND l.delete_grace_started_at_unix_ms<=? AND COALESCE(a.saved, 0)=0
		ORDER BY l.delete_grace_started_at_unix_ms LIMIT ?
	)`, cutoff.UTC().UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

func (r *SQLiteRepository) CountEligibleBackedLogs(cutoff time.Time) (int64, error) {
	var n int64
	err := r.db.QueryRow(`SELECT COUNT(*) FROM request_logs l
		LEFT JOIN log_annotations a ON a.log_id=l.id
		WHERE l.origin='live' AND l.backup_verified_at_unix_ms IS NOT NULL
		  AND l.delete_grace_started_at_unix_ms<=? AND COALESCE(a.saved, 0)=0`, cutoff.UTC().UnixMilli()).Scan(&n)
	return n, err
}

func (r *SQLiteRepository) PendingBackedLogCleanup() (int64, *time.Time, error) {
	var count int64
	var earliest sql.NullInt64
	err := r.db.QueryRow(`SELECT COUNT(*), MIN(l.delete_grace_started_at_unix_ms) FROM request_logs l
		LEFT JOIN log_annotations a ON a.log_id=l.id
		WHERE l.origin='live' AND l.backup_verified_at_unix_ms IS NOT NULL
		  AND l.delete_grace_started_at_unix_ms IS NOT NULL AND COALESCE(a.saved, 0)=0`).Scan(&count, &earliest)
	if err != nil || !earliest.Valid {
		return count, nil, err
	}
	value := time.UnixMilli(earliest.Int64).UTC()
	return count, &value, nil
}

func (r *SQLiteRepository) LogExists(id string) (bool, error) {
	var exists int
	err := r.db.QueryRow("SELECT EXISTS(SELECT 1 FROM request_logs WHERE id=?)", id).Scan(&exists)
	return exists == 1, err
}

func (r *SQLiteRepository) SaveImportedLog(log *RequestLog) error {
	if log == nil {
		return nil
	}
	log.Origin = "archive_import"
	log.BackupVerifiedAt = nil
	log.BackupBatchID = ""
	log.DeleteGraceStartedAt = nil
	return r.SaveLog(log)
}

func (r *SQLiteRepository) CreateArchiveImport(batch ArchiveImport) error {
	if batch.CreatedAt.IsZero() {
		batch.CreatedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO archive_imports
		(id, source_key, status, log_count, error, created_at_unix_ms, expires_at_unix_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, batch.ID, batch.SourceKey, batch.Status, batch.LogCount,
		batch.Error, batch.CreatedAt.UnixMilli(), nullableTimeMS(batch.ExpiresAt))
	return err
}

func (r *SQLiteRepository) UpdateArchiveImport(batch ArchiveImport) error {
	_, err := r.db.Exec(`UPDATE archive_imports SET source_key=?, status=?, log_count=?, error=?,
		expires_at_unix_ms=? WHERE id=?`, batch.SourceKey, batch.Status, batch.LogCount,
		batch.Error, nullableTimeMS(batch.ExpiresAt), batch.ID)
	return err
}

func (r *SQLiteRepository) ListArchiveImports() ([]ArchiveImport, error) {
	items, _, err := r.ListArchiveImportsPage(0, 1000)
	return items, err
}

func (r *SQLiteRepository) ListArchiveImportsPage(offset, limit int) ([]ArchiveImport, int64, error) {
	offset, limit = archivePageBounds(offset, limit)
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM archive_imports").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(`SELECT id, source_key, status, log_count, error,
		created_at_unix_ms, expires_at_unix_ms FROM archive_imports
		ORDER BY created_at_unix_ms DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ArchiveImport
	for rows.Next() {
		var b ArchiveImport
		var createdMS int64
		var expiresMS sql.NullInt64
		if err := rows.Scan(&b.ID, &b.SourceKey, &b.Status, &b.LogCount, &b.Error, &createdMS, &expiresMS); err != nil {
			return nil, 0, err
		}
		b.CreatedAt = time.UnixMilli(createdMS).UTC()
		if expiresMS.Valid {
			v := time.UnixMilli(expiresMS.Int64).UTC()
			b.ExpiresAt = &v
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func archivePageBounds(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return offset, limit
}

func (r *SQLiteRepository) DeleteArchiveImport(batchID string) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec("DELETE FROM request_logs WHERE origin='archive_import' AND import_batch_id=?", batchID)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM archive_imports WHERE id=?", batchID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM archive_staged_blob_refs WHERE batch_id=?", batchID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

func (r *SQLiteRepository) DeleteExpiredArchiveImports(now time.Time) (int64, error) {
	rows, err := r.db.Query(`SELECT id FROM archive_imports
		WHERE expires_at_unix_ms IS NOT NULL AND expires_at_unix_ms<=?`, now.UTC().UnixMilli())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	var total int64
	for _, id := range ids {
		n, err := r.DeleteArchiveImport(id)
		if err != nil {
			return total, fmt.Errorf("delete import %s: %w", id, err)
		}
		total += n
	}
	return total, nil
}

func (r *SQLiteRepository) StageArchiveBlobRef(batchID, ref string) error {
	_, err := r.db.Exec(`INSERT OR IGNORE INTO archive_staged_blob_refs (batch_id, blob_ref) VALUES (?, ?)`, batchID, ref)
	return err
}

func (r *SQLiteRepository) ClearArchiveBlobRefs(batchID string) error {
	_, err := r.db.Exec("DELETE FROM archive_staged_blob_refs WHERE batch_id=?", batchID)
	return err
}

func nullableTimeMS(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().UnixMilli()
}
