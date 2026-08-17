package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteRepository implements Repository using SQLite.
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates a new SQLite repository.
func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Pragmas for better concurrency and write performance on local usage.
	// WAL helps UI reads stay responsive while logs are being written.
	if err := applySQLitePragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Connection pool: allow concurrent reads; SQLite still serializes writes.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	repo := &SQLiteRepository{db: db}
	if err := repo.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return repo, nil
}

func applySQLitePragmas(db *sql.DB) error {
	// Use Query so PRAGMA statements that return rows are handled consistently.
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, stmt := range pragmas {
		rows, err := db.Query(stmt)
		if err != nil {
			return fmt.Errorf("apply sqlite pragma failed (%s): %w", stmt, err)
		}
		_ = rows.Close()
	}
	return nil
}

func (r *SQLiteRepository) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS request_logs (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL,
		created_at_unix_ms INTEGER NOT NULL,
		upstream TEXT NOT NULL,
		upstream_target TEXT DEFAULT '',
		target_url TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		query TEXT,
		request_headers TEXT,
		request_body TEXT,
		request_body_original TEXT,
		request_body_final TEXT,
		request_body_ref TEXT,
		request_body_size INTEGER DEFAULT 0,
		status_code INTEGER DEFAULT 0,
		response_headers TEXT,
		response_body TEXT,
		response_body_ref TEXT,
		response_body_size INTEGER DEFAULT 0,
		streaming INTEGER DEFAULT 0,
		latency_ms INTEGER DEFAULT 0,
		error TEXT,
		truncated INTEGER DEFAULT 0,
		request_override_applied INTEGER DEFAULT 0,
		request_override_rules TEXT DEFAULT '[]',
		request_override_error TEXT,
		request_header_override_applied INTEGER DEFAULT 0,
		request_header_override_changes TEXT DEFAULT '',
		request_headers_original TEXT DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_logs_created_at ON request_logs(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_logs_upstream ON request_logs(upstream);
	CREATE INDEX IF NOT EXISTS idx_logs_status_code ON request_logs(status_code);
	CREATE INDEX IF NOT EXISTS idx_logs_method ON request_logs(method);

	CREATE TABLE IF NOT EXISTS log_annotations (
		log_id TEXT PRIMARY KEY,
		saved INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'none',
		note TEXT NOT NULL DEFAULT '',
		labels TEXT NOT NULL DEFAULT '[]',
		created_at_unix_ms INTEGER NOT NULL,
		updated_at_unix_ms INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_log_annotations_saved ON log_annotations(saved);
	CREATE INDEX IF NOT EXISTS idx_log_annotations_status ON log_annotations(status);

	CREATE TABLE IF NOT EXISTS ignored_path_stats (
		upstream TEXT NOT NULL,
		path TEXT NOT NULL,
		request_count INTEGER NOT NULL DEFAULT 0,
		last_seen_unix_ms INTEGER NOT NULL,
		PRIMARY KEY (upstream, path)
	);

	CREATE INDEX IF NOT EXISTS idx_ignored_path_stats_last_seen
		ON ignored_path_stats(last_seen_unix_ms DESC);
	CREATE INDEX IF NOT EXISTS idx_ignored_path_stats_upstream_last_seen
		ON ignored_path_stats(upstream, last_seen_unix_ms DESC);
	`
	if _, err := r.db.Exec(schema); err != nil {
		return fmt.Errorf("database migrate failed: %w", err)
	}

	// Backward-compatible migration for existing DBs.
	if err := r.ensureLogColumn("request_body_ref", "request_body_ref TEXT"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("response_body_ref", "response_body_ref TEXT"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("created_at_unix_ms", "created_at_unix_ms INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("upstream_target", "upstream_target TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("tag", "tag TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_body_original", "request_body_original TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_body_final", "request_body_final TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_override_applied", "request_override_applied INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_override_rules", "request_override_rules TEXT DEFAULT '[]'"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_override_error", "request_override_error TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_header_override_applied", "request_header_override_applied INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_header_override_changes", "request_header_override_changes TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("request_headers_original", "request_headers_original TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("trace_id", "trace_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("parent_log_id", "parent_log_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("trace_seq", "trace_seq INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("usage_input_tokens", "usage_input_tokens INTEGER"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("usage_output_tokens", "usage_output_tokens INTEGER"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("usage_total_tokens", "usage_total_tokens INTEGER"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("usage_raw", "usage_raw TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := r.ensureLogColumn("usage_source", "usage_source TEXT DEFAULT ''"); err != nil {
		return err
	}
	// Index for unix timestamp based sort/filter.
	if _, err := r.db.Exec("CREATE INDEX IF NOT EXISTS idx_logs_created_at_unix_ms ON request_logs(created_at_unix_ms DESC)"); err != nil {
		return fmt.Errorf("create created_at_unix_ms index: %w", err)
	}
	if err := r.backfillCreatedAtUnixMS(); err != nil {
		return err
	}
	// Index for tag filtering.
	if _, err := r.db.Exec("CREATE INDEX IF NOT EXISTS idx_logs_tag ON request_logs(tag)"); err != nil {
		return fmt.Errorf("create tag index: %w", err)
	}
	// Indexes for trace filtering and ordering.
	if _, err := r.db.Exec("CREATE INDEX IF NOT EXISTS idx_logs_trace_id ON request_logs(trace_id)"); err != nil {
		return fmt.Errorf("create trace_id index: %w", err)
	}
	if _, err := r.db.Exec("CREATE INDEX IF NOT EXISTS idx_logs_trace_id_seq ON request_logs(trace_id, trace_seq)"); err != nil {
		return fmt.Errorf("create trace_id_seq index: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ensureLogColumn(colName, colDef string) error {
	has, err := r.hasColumn("request_logs", colName)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	if _, err := r.db.Exec(fmt.Sprintf("ALTER TABLE request_logs ADD COLUMN %s", colDef)); err != nil {
		return fmt.Errorf("add column %s failed: %w", colName, err)
	}
	return nil
}

func (r *SQLiteRepository) hasColumn(table, colName string) (bool, error) {
	rows, err := r.db.Query(fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == colName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (r *SQLiteRepository) backfillCreatedAtUnixMS() error {
	rows, err := r.db.Query("SELECT id, created_at FROM request_logs WHERE created_at_unix_ms IS NULL OR created_at_unix_ms <= 0")
	if err != nil {
		return fmt.Errorf("query missing created_at_unix_ms rows: %w", err)
	}
	defer rows.Close()

	type updateRow struct {
		id     string
		unixMS int64
	}
	updates := make([]updateRow, 0)
	for rows.Next() {
		var id string
		var createdAtRaw any
		if err := rows.Scan(&id, &createdAtRaw); err != nil {
			return fmt.Errorf("scan row for created_at_unix_ms backfill: %w", err)
		}

		createdAt, err := parseDBTime(createdAtRaw)
		if err != nil {
			return fmt.Errorf("parse created_at for row %s: %w", id, err)
		}
		updates = append(updates, updateRow{
			id:     id,
			unixMS: createdAt.UTC().UnixMilli(),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows for created_at_unix_ms backfill: %w", err)
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin created_at_unix_ms backfill tx: %w", err)
	}

	stmt, err := tx.Prepare("UPDATE request_logs SET created_at_unix_ms = ? WHERE id = ?")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare created_at_unix_ms backfill statement: %w", err)
	}

	for _, row := range updates {
		if _, err := stmt.Exec(row.unixMS, row.id); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("update created_at_unix_ms for row %s: %w", row.id, err)
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("close created_at_unix_ms backfill statement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit created_at_unix_ms backfill tx: %w", err)
	}
	return nil
}

func parseDBTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		return parseTimeString(t)
	case []byte:
		return parseTimeString(string(t))
	case int64:
		return time.UnixMilli(t), nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return time.Time{}, fmt.Errorf("invalid float timestamp: %v", t)
		}
		return time.UnixMilli(int64(t)), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", v)
	}
}

func parseTimeString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	zonedLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
	}
	for _, layout := range zonedLayouts {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm, nil
		}
	}

	// Legacy rows without explicit timezone are interpreted in local timezone,
	// matching historical behavior of writing time.Now() on the host.
	localLayouts := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range localLayouts {
		if tm, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return tm, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time string format: %q", s)
}

// SaveLog inserts or updates a log entry (upsert by id).
func (r *SQLiteRepository) SaveLog(log *RequestLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	log.CreatedAt = log.CreatedAt.UTC()

	reqHeaders, _ := json.Marshal(log.RequestHeaders)
	respHeaders, _ := json.Marshal(log.ResponseHeaders)
	overrideRules, _ := json.Marshal(log.RequestOverrideRules)
	reqHeadersOriginal := ""
	if len(log.RequestHeadersOriginal) > 0 {
		b, _ := json.Marshal(log.RequestHeadersOriginal)
		reqHeadersOriginal = string(b)
	}

	query := `
	INSERT INTO request_logs (
		id, created_at, created_at_unix_ms, upstream, upstream_target, target_url, method, path, query,
		request_headers, request_body, request_body_original, request_body_final, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated, tag,
		request_override_applied, request_override_rules, request_override_error,
		request_header_override_applied, request_header_override_changes, request_headers_original,
		trace_id, parent_log_id, trace_seq,
		usage_input_tokens, usage_output_tokens, usage_total_tokens, usage_raw, usage_source
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		created_at = excluded.created_at,
		created_at_unix_ms = excluded.created_at_unix_ms,
		upstream = excluded.upstream,
		upstream_target = excluded.upstream_target,
		target_url = excluded.target_url,
		method = excluded.method,
		path = excluded.path,
		query = excluded.query,
		request_headers = excluded.request_headers,
		request_body = excluded.request_body,
		request_body_original = excluded.request_body_original,
		request_body_final = excluded.request_body_final,
		request_body_ref = excluded.request_body_ref,
		request_body_size = excluded.request_body_size,
		status_code = excluded.status_code,
		response_headers = excluded.response_headers,
		response_body = excluded.response_body,
		response_body_ref = excluded.response_body_ref,
		response_body_size = excluded.response_body_size,
		streaming = excluded.streaming,
		latency_ms = excluded.latency_ms,
		error = excluded.error,
		truncated = excluded.truncated,
		tag = excluded.tag,
		request_override_applied = excluded.request_override_applied,
		request_override_rules = excluded.request_override_rules,
		request_override_error = excluded.request_override_error,
		request_header_override_applied = excluded.request_header_override_applied,
		request_header_override_changes = excluded.request_header_override_changes,
		request_headers_original = excluded.request_headers_original,
		trace_id = excluded.trace_id,
		parent_log_id = excluded.parent_log_id,
		trace_seq = excluded.trace_seq,
		usage_input_tokens = excluded.usage_input_tokens,
		usage_output_tokens = excluded.usage_output_tokens,
		usage_total_tokens = excluded.usage_total_tokens,
		usage_raw = excluded.usage_raw,
		usage_source = excluded.usage_source
	`

	_, err := r.db.Exec(query,
		log.ID, log.CreatedAt, log.CreatedAt.UnixMilli(), log.Upstream, log.UpstreamTarget, log.TargetURL, log.Method, log.Path, log.Query,
		string(reqHeaders), log.RequestBody, log.RequestBodyOriginal, log.RequestBodyFinal, log.RequestBodyRef, log.RequestBodySize,
		log.StatusCode, string(respHeaders), log.ResponseBody, log.ResponseBodyRef, log.ResponseBodySize,
		log.Streaming, log.Latency, log.Error, log.Truncated, log.Tag,
		log.RequestOverrideApplied, string(overrideRules), log.RequestOverrideError,
		log.RequestHeaderOverrideApplied, string(log.RequestHeaderOverrideChanges), reqHeadersOriginal,
		log.TraceID, log.ParentLogID, log.TraceSeq,
		log.UsageInputTokens, log.UsageOutputTokens, log.UsageTotalTokens, log.UsageRaw, log.UsageSource,
	)
	return err
}

func (r *SQLiteRepository) GetLog(id string) (*RequestLog, error) {
	query := `
	SELECT id, created_at, upstream, upstream_target, target_url, method, path, query,
		request_headers, request_body, request_body_original, request_body_final, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated, tag,
		request_override_applied, request_override_rules, request_override_error,
		request_header_override_applied, request_header_override_changes, request_headers_original,
		trace_id, parent_log_id, trace_seq,
		usage_input_tokens, usage_output_tokens, usage_total_tokens, usage_raw, usage_source
	FROM request_logs WHERE id = ?
	`
	row := r.db.QueryRow(query, id)
	log, err := r.scanLog(row)
	if err != nil {
		return nil, err
	}
	if annotation, err := r.GetLogAnnotation(id); err == nil {
		log.Annotation = annotation
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return log, nil
}

func (r *SQLiteRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	where, args := buildLogWhereClause(filter)

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs l LEFT JOIN log_annotations a ON a.log_id = l.id %s", where)
	var total int64
	if err := r.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Pagination.
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	query := fmt.Sprintf(`
	SELECT l.id, l.created_at, l.upstream, l.upstream_target, l.target_url, l.method, l.path, l.query,
		l.request_body_size, l.status_code, l.response_body_size,
		l.streaming, l.latency_ms, l.error, l.truncated, l.tag, l.request_override_applied,
		COALESCE(a.saved, 0), COALESCE(a.status, 'none'), COALESCE(a.note, ''), COALESCE(a.labels, '[]'),
		COALESCE(a.created_at_unix_ms, 0), COALESCE(a.updated_at_unix_ms, 0),
		l.trace_id, l.parent_log_id, l.trace_seq,
		l.usage_input_tokens, l.usage_output_tokens, l.usage_total_tokens
	FROM request_logs l
	LEFT JOIN log_annotations a ON a.log_id = l.id
	%s
	ORDER BY l.created_at_unix_ms DESC, l.created_at DESC
	LIMIT ? OFFSET ?
	`, where)

	args = append(args, filter.Limit, filter.Offset)
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []*RequestLog
	for rows.Next() {
		log, err := r.scanLogSummary(rows)
		if err != nil {
			return nil, 0, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

func buildLogWhereClause(filter LogFilter) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if filter.Upstream != "" {
		conditions = append(conditions, "l.upstream = ?")
		args = append(args, filter.Upstream)
	}
	if filter.Method != "" {
		conditions = append(conditions, "l.method = ?")
		args = append(args, filter.Method)
	}
	if filter.StatusCode > 0 {
		conditions = append(conditions, "l.status_code = ?")
		args = append(args, filter.StatusCode)
	}
	if filter.Path != "" {
		conditions = append(conditions, "l.path LIKE ?")
		args = append(args, "%"+filter.Path+"%")
	}
	if filter.StartTime != nil {
		conditions = append(conditions, "l.created_at_unix_ms >= ?")
		args = append(args, filter.StartTime.UTC().UnixMilli())
	}
	if filter.EndTime != nil {
		conditions = append(conditions, "l.created_at_unix_ms <= ?")
		args = append(args, filter.EndTime.UTC().UnixMilli())
	}
	if filter.HasError != nil {
		if *filter.HasError {
			conditions = append(conditions, "(l.error IS NOT NULL AND l.error != '')")
		} else {
			conditions = append(conditions, "(l.error IS NULL OR l.error = '')")
		}
	}
	if filter.Streaming != nil {
		conditions = append(conditions, "l.streaming = ?")
		args = append(args, *filter.Streaming)
	}
	if filter.Tag != "" {
		conditions = append(conditions, "l.tag = ?")
		args = append(args, filter.Tag)
	}
	if filter.TraceID != "" {
		conditions = append(conditions, "l.trace_id = ?")
		args = append(args, filter.TraceID)
	}
	if filter.Saved != nil {
		if *filter.Saved {
			conditions = append(conditions, "COALESCE(a.saved, 0) = 1")
		} else {
			conditions = append(conditions, "COALESCE(a.saved, 0) = 0")
		}
	}
	if filter.Status != "" {
		conditions = append(conditions, "COALESCE(a.status, 'none') = ?")
		args = append(args, normalizeAnnotationStatus(filter.Status))
	}
	if filter.Label != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(COALESCE(a.labels, '[]')) WHERE value = ?)")
		args = append(args, filter.Label)
	}

	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func (r *SQLiteRepository) ExportLogs(ctx context.Context, filter LogFilter, each func(*RequestLog) error) error {
	if each == nil {
		return nil
	}

	where, args := buildLogWhereClause(filter)
	query := fmt.Sprintf(`
	SELECT l.id, l.created_at, l.upstream, l.upstream_target, l.target_url, l.method, l.path, l.query,
		l.request_headers, l.request_body, l.request_body_original, l.request_body_final, l.request_body_ref, l.request_body_size,
		l.status_code, l.response_headers, l.response_body, l.response_body_ref, l.response_body_size,
		l.streaming, l.latency_ms, l.error, l.truncated, l.tag,
		l.request_override_applied, l.request_override_rules, l.request_override_error,
		l.request_header_override_applied, l.request_header_override_changes, l.request_headers_original,
		l.trace_id, l.parent_log_id, l.trace_seq,
		l.usage_input_tokens, l.usage_output_tokens, l.usage_total_tokens, l.usage_raw, l.usage_source,
		COALESCE(a.saved, 0), COALESCE(a.status, 'none'), COALESCE(a.note, ''), COALESCE(a.labels, '[]'),
		COALESCE(a.created_at_unix_ms, 0), COALESCE(a.updated_at_unix_ms, 0)
	FROM request_logs l
	LEFT JOIN log_annotations a ON a.log_id = l.id
	%s
	ORDER BY l.created_at_unix_ms ASC, l.created_at ASC
	`, where)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		log, err := r.scanLogWithAnnotation(rows)
		if err != nil {
			return err
		}
		if err := each(log); err != nil {
			return err
		}
	}
	return rows.Err()
}

const deleteBatchSize = 2000

func (r *SQLiteRepository) DeleteLogsBefore(before time.Time) (int64, error) {
	cutoffMS := before.UTC().UnixMilli()
	var totalDeleted int64
	for {
		tx, err := r.db.Begin()
		if err != nil {
			return totalDeleted, err
		}

		result, err := tx.Exec(`
			DELETE FROM request_logs
			WHERE id IN (
				SELECT id FROM request_logs
				WHERE created_at_unix_ms < ?
				  AND id NOT IN (SELECT log_id FROM log_annotations WHERE saved = 1)
				LIMIT ?
			)
		`, cutoffMS, deleteBatchSize)
		if err != nil {
			_ = tx.Rollback()
			return totalDeleted, err
		}
		if _, err := tx.Exec(`
			DELETE FROM log_annotations
			WHERE log_id NOT IN (SELECT id FROM request_logs)
		`); err != nil {
			_ = tx.Rollback()
			return totalDeleted, err
		}
		if err := tx.Commit(); err != nil {
			return totalDeleted, err
		}
		n, _ := result.RowsAffected()
		totalDeleted += n
		if n < deleteBatchSize {
			break
		}
	}
	return totalDeleted, nil
}

// DeleteOldestLogs removes the oldest unsaved logs, up to count records.
func (r *SQLiteRepository) DeleteOldestLogs(count int) (int64, error) {
	if count <= 0 {
		return 0, nil
	}
	var totalDeleted int64
	for count > 0 {
		batch := count
		if batch > deleteBatchSize {
			batch = deleteBatchSize
		}
		tx, err := r.db.Begin()
		if err != nil {
			return totalDeleted, err
		}
		result, err := tx.Exec(`
			DELETE FROM request_logs
			WHERE id IN (
				SELECT id FROM request_logs
				WHERE id NOT IN (SELECT log_id FROM log_annotations WHERE saved = 1)
				ORDER BY created_at_unix_ms ASC
				LIMIT ?
			)
		`, batch)
		if err != nil {
			_ = tx.Rollback()
			return totalDeleted, err
		}
		if _, err := tx.Exec(`
			DELETE FROM log_annotations
			WHERE log_id NOT IN (SELECT id FROM request_logs)
		`); err != nil {
			_ = tx.Rollback()
			return totalDeleted, err
		}
		if err := tx.Commit(); err != nil {
			return totalDeleted, err
		}
		n, _ := result.RowsAffected()
		totalDeleted += n
		count -= batch
		if n < int64(batch) {
			break
		}
	}
	return totalDeleted, nil
}

// WALCheckpoint truncates the WAL file to reclaim disk space.
func (r *SQLiteRepository) WALCheckpoint() error {
	_, err := r.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// CountDeletableLogs returns the number of logs not marked as saved.
func (r *SQLiteRepository) CountDeletableLogs() (int64, error) {
	var count int64
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs
		WHERE id NOT IN (SELECT log_id FROM log_annotations WHERE saved = 1)
	`).Scan(&count)
	return count, err
}

// Vacuum rebuilds the database file to reclaim unused pages.
func (r *SQLiteRepository) Vacuum() error {
	_, err := r.db.Exec("VACUUM")
	return err
}

func (r *SQLiteRepository) GetLogAnnotation(logID string) (LogAnnotation, error) {
	var annotation LogAnnotation
	var labelsJSON string
	var saved int
	var createdMS, updatedMS int64
	err := r.db.QueryRow(`
		SELECT saved, status, note, labels, created_at_unix_ms, updated_at_unix_ms
		FROM log_annotations
		WHERE log_id = ?
	`, logID).Scan(&saved, &annotation.Status, &annotation.Note, &labelsJSON, &createdMS, &updatedMS)
	if err != nil {
		return LogAnnotation{}, err
	}
	annotation.Saved = saved == 1
	annotation.Status = normalizeAnnotationStatus(annotation.Status)
	_ = json.Unmarshal([]byte(labelsJSON), &annotation.Labels)
	if createdMS > 0 {
		annotation.CreatedAt = time.UnixMilli(createdMS).UTC()
	}
	if updatedMS > 0 {
		annotation.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	}
	return annotation, nil
}

func (r *SQLiteRepository) SaveLogAnnotation(logID string, annotation LogAnnotation) (LogAnnotation, error) {
	logID = strings.TrimSpace(logID)
	if logID == "" {
		return LogAnnotation{}, fmt.Errorf("log id is empty")
	}
	annotation.Status = normalizeAnnotationStatus(annotation.Status)
	annotation.Note = strings.TrimSpace(annotation.Note)
	annotation.Labels = normalizeLabels(annotation.Labels)
	if annotation.Status != "none" {
		annotation.Saved = true
	}

	if !annotation.Saved && annotation.Status == "none" && annotation.Note == "" && len(annotation.Labels) == 0 {
		if _, err := r.db.Exec("DELETE FROM log_annotations WHERE log_id = ?", logID); err != nil {
			return LogAnnotation{}, err
		}
		return LogAnnotation{Status: "none"}, nil
	}

	now := time.Now().UTC().UnixMilli()
	labelsJSON, _ := json.Marshal(annotation.Labels)
	_, err := r.db.Exec(`
		INSERT INTO log_annotations (
			log_id, saved, status, note, labels, created_at_unix_ms, updated_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(log_id) DO UPDATE SET
			saved = excluded.saved,
			status = excluded.status,
			note = excluded.note,
			labels = excluded.labels,
			updated_at_unix_ms = excluded.updated_at_unix_ms
	`, logID, boolInt(annotation.Saved), annotation.Status, annotation.Note, string(labelsJSON), now, now)
	if err != nil {
		return LogAnnotation{}, err
	}
	return r.GetLogAnnotation(logID)
}

func (r *SQLiteRepository) GetStats(since *time.Time) (*LogStats, error) {
	stats := &LogStats{
		ByUpstream:   make(map[string]int64),
		ByStatusCode: make(map[int]int64),
	}

	where := ""
	var args []interface{}
	if since != nil {
		where = "WHERE created_at_unix_ms >= ?"
		args = append(args, since.UTC().UnixMilli())
	}

	// SUM 在零行时返回 NULL,直接扫进 int64 会报错 —— 全新安装、还没有任何日志时
	// 走的正是这条路径,所以每个聚合都要 COALESCE
	query := fmt.Sprintf(`
	SELECT
		COUNT(*) as total,
		COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0) as success,
		COALESCE(SUM(CASE WHEN (error IS NOT NULL AND error != '') OR status_code >= 400 THEN 1 ELSE 0 END), 0) as errors,
		COALESCE(SUM(CASE WHEN streaming = 1 THEN 1 ELSE 0 END), 0) as streaming,
		COALESCE(AVG(latency_ms), 0) as avg_latency
	FROM request_logs %s
	`, where)

	if err := r.db.QueryRow(query, args...).Scan(
		&stats.TotalRequests,
		&stats.SuccessCount,
		&stats.ErrorCount,
		&stats.StreamingCount,
		&stats.AvgLatency,
	); err != nil {
		return nil, err
	}

	upstreamQuery := fmt.Sprintf("SELECT upstream, COUNT(*) FROM request_logs %s GROUP BY upstream", where)
	rows, err := r.db.Query(upstreamQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var upstream string
		var count int64
		if err := rows.Scan(&upstream, &count); err != nil {
			return nil, err
		}
		stats.ByUpstream[upstream] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	statusQuery := fmt.Sprintf("SELECT status_code, COUNT(*) FROM request_logs %s GROUP BY status_code", where)
	rows2, err := r.db.Query(statusQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var code int
		var count int64
		if err := rows2.Scan(&code, &count); err != nil {
			return nil, err
		}
		stats.ByStatusCode[code] = count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *SQLiteRepository) ListTraces(filter TraceFilter) ([]TraceSummary, int64, error) {
	var havingConds []string
	var havingArgs []interface{}
	var whereConds []string
	var whereArgs []interface{}

	whereConds = append(whereConds, "trace_id != ''")

	if filter.TraceID != "" {
		whereConds = append(whereConds, "trace_id LIKE ?")
		whereArgs = append(whereArgs, "%"+filter.TraceID+"%")
	}
	if filter.Upstream != "" {
		whereConds = append(whereConds, "upstream = ?")
		whereArgs = append(whereArgs, filter.Upstream)
	}
	if filter.Tag != "" {
		whereConds = append(whereConds, "tag = ?")
		whereArgs = append(whereArgs, filter.Tag)
	}
	if filter.StartTime != nil {
		whereConds = append(whereConds, "created_at_unix_ms >= ?")
		whereArgs = append(whereArgs, filter.StartTime.UTC().UnixMilli())
	}
	if filter.EndTime != nil {
		whereConds = append(whereConds, "created_at_unix_ms <= ?")
		whereArgs = append(whereArgs, filter.EndTime.UTC().UnixMilli())
	}

	if filter.HasError != nil {
		if *filter.HasError {
			havingConds = append(havingConds, "error_count > 0")
		} else {
			havingConds = append(havingConds, "error_count = 0")
		}
	}

	where := "WHERE " + strings.Join(whereConds, " AND ")
	having := ""
	if len(havingConds) > 0 {
		having = "HAVING " + strings.Join(havingConds, " AND ")
	}

	allArgs := append([]interface{}{}, whereArgs...)
	allArgs = append(allArgs, havingArgs...)

	var countQuery string
	var countArgs []interface{}
	if having == "" {
		countQuery = fmt.Sprintf("SELECT COUNT(DISTINCT trace_id) FROM request_logs %s", where)
		countArgs = whereArgs
	} else {
		countQuery = fmt.Sprintf(`
			SELECT COUNT(*) FROM (
				SELECT trace_id FROM request_logs %s GROUP BY trace_id %s
			)
		`, where, having)
		countArgs = allArgs
	}
	var total int64
	if err := r.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	query := fmt.Sprintf(`
		SELECT trace_id,
			COUNT(*) as request_count,
			MIN(created_at_unix_ms) as first_time,
			MAX(created_at_unix_ms) as last_time,
			SUM(latency_ms) as total_latency,
			SUM(CASE WHEN (error IS NOT NULL AND error != '') OR status_code >= 400 THEN 1 ELSE 0 END) as error_count,
			SUM(usage_input_tokens) as usage_input_tokens,
			SUM(usage_output_tokens) as usage_output_tokens,
			SUM(usage_total_tokens) as usage_total_tokens,
			GROUP_CONCAT(DISTINCT upstream) as upstreams,
			GROUP_CONCAT(DISTINCT CASE WHEN tag != '' THEN tag END) as tags
		FROM request_logs
		%s
		GROUP BY trace_id
		%s
		ORDER BY MAX(created_at_unix_ms) DESC
		LIMIT ? OFFSET ?
	`, where, having)

	pagArgs := append(allArgs, filter.Limit, filter.Offset)
	rows, err := r.db.Query(query, pagArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var traces []TraceSummary
	for rows.Next() {
		var t TraceSummary
		var upstreamsCSV, tagsCSV sql.NullString
		var usageInput, usageOutput, usageTotal sql.NullInt64
		if err := rows.Scan(&t.TraceID, &t.RequestCount, &t.FirstTime, &t.LastTime,
			&t.TotalLatency, &t.ErrorCount, &usageInput, &usageOutput, &usageTotal, &upstreamsCSV, &tagsCSV); err != nil {
			return nil, 0, err
		}
		t.UsageInputTokens = nullIntPtr(usageInput)
		t.UsageOutputTokens = nullIntPtr(usageOutput)
		t.UsageTotalTokens = nullIntPtr(usageTotal)
		if upstreamsCSV.Valid && upstreamsCSV.String != "" {
			t.Upstreams = strings.Split(upstreamsCSV.String, ",")
		}
		if tagsCSV.Valid && tagsCSV.String != "" {
			t.Tags = strings.Split(tagsCSV.String, ",")
		}
		traces = append(traces, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return traces, total, nil
}

func (r *SQLiteRepository) GetTraceRequests(traceID string) ([]*RequestLog, error) {
	query := `
	SELECT id, created_at, upstream, upstream_target, target_url, method, path, query,
		request_headers, request_body, request_body_original, request_body_final, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated, tag,
		request_override_applied, request_override_rules, request_override_error,
		request_header_override_applied, request_header_override_changes, request_headers_original,
		trace_id, parent_log_id, trace_seq,
		usage_input_tokens, usage_output_tokens, usage_total_tokens, usage_raw, usage_source
	FROM request_logs
	WHERE trace_id = ?
	ORDER BY trace_seq, created_at_unix_ms
	`
	rows, err := r.db.Query(query, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*RequestLog
	for rows.Next() {
		log, err := r.scanLog(rows)
		if err != nil {
			return nil, err
		}
		if annotation, aErr := r.GetLogAnnotation(log.ID); aErr == nil {
			log.Annotation = annotation
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

// ListBlobRefs returns all distinct blob refs currently referenced by logs.
func (r *SQLiteRepository) ListBlobRefs() ([]string, error) {
	query := `
	SELECT request_body_ref AS ref
	FROM request_logs
	WHERE request_body_ref IS NOT NULL AND request_body_ref != ''
	UNION
	SELECT response_body_ref AS ref
	FROM request_logs
	WHERE response_body_ref IS NOT NULL AND response_body_ref != ''
	`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

func (r *SQLiteRepository) scanLogSummary(scanner interface{ Scan(...interface{}) error }) (*RequestLog, error) {
	var log RequestLog
	var streaming, truncated, overrideApplied int
	var annotationSaved int
	var annotationLabels string
	var annotationCreatedMS, annotationUpdatedMS int64
	var usageInput, usageOutput, usageTotal sql.NullInt64

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.UpstreamTarget, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&log.RequestBodySize, &log.StatusCode, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated, &log.Tag, &overrideApplied,
		&annotationSaved, &log.Annotation.Status, &log.Annotation.Note, &annotationLabels,
		&annotationCreatedMS, &annotationUpdatedMS,
		&log.TraceID, &log.ParentLogID, &log.TraceSeq,
		&usageInput, &usageOutput, &usageTotal,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1
	log.RequestOverrideApplied = overrideApplied == 1
	log.UsageInputTokens = nullIntPtr(usageInput)
	log.UsageOutputTokens = nullIntPtr(usageOutput)
	log.UsageTotalTokens = nullIntPtr(usageTotal)
	log.CreatedAt = log.CreatedAt.UTC()
	log.Annotation.Saved = annotationSaved == 1
	log.Annotation.Status = normalizeAnnotationStatus(log.Annotation.Status)
	_ = json.Unmarshal([]byte(annotationLabels), &log.Annotation.Labels)
	if annotationCreatedMS > 0 {
		log.Annotation.CreatedAt = time.UnixMilli(annotationCreatedMS).UTC()
	}
	if annotationUpdatedMS > 0 {
		log.Annotation.UpdatedAt = time.UnixMilli(annotationUpdatedMS).UTC()
	}

	return &log, nil
}

func (r *SQLiteRepository) scanLog(scanner interface{ Scan(...interface{}) error }) (*RequestLog, error) {
	var log RequestLog
	var reqHeaders, respHeaders, overrideRules string
	var streaming, truncated, overrideApplied int
	var headerOverrideApplied int
	var headerOverrideChanges, reqHeadersOriginal string
	var usageInput, usageOutput, usageTotal sql.NullInt64
	var usageRaw, usageSource sql.NullString

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.UpstreamTarget, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&reqHeaders, &log.RequestBody, &log.RequestBodyOriginal, &log.RequestBodyFinal, &log.RequestBodyRef, &log.RequestBodySize,
		&log.StatusCode, &respHeaders, &log.ResponseBody, &log.ResponseBodyRef, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated, &log.Tag,
		&overrideApplied, &overrideRules, &log.RequestOverrideError,
		&headerOverrideApplied, &headerOverrideChanges, &reqHeadersOriginal,
		&log.TraceID, &log.ParentLogID, &log.TraceSeq,
		&usageInput, &usageOutput, &usageTotal, &usageRaw, &usageSource,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1
	log.RequestOverrideApplied = overrideApplied == 1
	log.RequestHeaderOverrideApplied = headerOverrideApplied == 1
	log.UsageInputTokens = nullIntPtr(usageInput)
	log.UsageOutputTokens = nullIntPtr(usageOutput)
	log.UsageTotalTokens = nullIntPtr(usageTotal)
	if usageRaw.Valid {
		log.UsageRaw = usageRaw.String
	}
	if usageSource.Valid {
		log.UsageSource = usageSource.String
	}
	log.CreatedAt = log.CreatedAt.UTC()

	if reqHeaders != "" && reqHeaders != "null" {
		log.RequestHeaders = unmarshalHeaders(reqHeaders)
	}
	if respHeaders != "" && respHeaders != "null" {
		log.ResponseHeaders = unmarshalHeaders(respHeaders)
	}
	if overrideRules != "" && overrideRules != "null" {
		_ = json.Unmarshal([]byte(overrideRules), &log.RequestOverrideRules)
	}
	if headerOverrideChanges != "" && headerOverrideChanges != "null" {
		log.RequestHeaderOverrideChanges = json.RawMessage(headerOverrideChanges)
	}
	if reqHeadersOriginal != "" && reqHeadersOriginal != "null" {
		log.RequestHeadersOriginal = unmarshalHeaders(reqHeadersOriginal)
	}

	return &log, nil
}

func (r *SQLiteRepository) scanLogWithAnnotation(scanner interface{ Scan(...interface{}) error }) (*RequestLog, error) {
	var log RequestLog
	var reqHeaders, respHeaders, overrideRules string
	var streaming, truncated, overrideApplied int
	var headerOverrideApplied int
	var headerOverrideChanges, reqHeadersOriginal string
	var usageInput, usageOutput, usageTotal sql.NullInt64
	var usageRaw, usageSource sql.NullString
	var annotationSaved int
	var annotationLabels string
	var annotationCreatedMS, annotationUpdatedMS int64

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.UpstreamTarget, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&reqHeaders, &log.RequestBody, &log.RequestBodyOriginal, &log.RequestBodyFinal, &log.RequestBodyRef, &log.RequestBodySize,
		&log.StatusCode, &respHeaders, &log.ResponseBody, &log.ResponseBodyRef, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated, &log.Tag,
		&overrideApplied, &overrideRules, &log.RequestOverrideError,
		&headerOverrideApplied, &headerOverrideChanges, &reqHeadersOriginal,
		&log.TraceID, &log.ParentLogID, &log.TraceSeq,
		&usageInput, &usageOutput, &usageTotal, &usageRaw, &usageSource,
		&annotationSaved, &log.Annotation.Status, &log.Annotation.Note, &annotationLabels,
		&annotationCreatedMS, &annotationUpdatedMS,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1
	log.RequestOverrideApplied = overrideApplied == 1
	log.RequestHeaderOverrideApplied = headerOverrideApplied == 1
	log.UsageInputTokens = nullIntPtr(usageInput)
	log.UsageOutputTokens = nullIntPtr(usageOutput)
	log.UsageTotalTokens = nullIntPtr(usageTotal)
	if usageRaw.Valid {
		log.UsageRaw = usageRaw.String
	}
	if usageSource.Valid {
		log.UsageSource = usageSource.String
	}
	log.CreatedAt = log.CreatedAt.UTC()

	if reqHeaders != "" && reqHeaders != "null" {
		log.RequestHeaders = unmarshalHeaders(reqHeaders)
	}
	if respHeaders != "" && respHeaders != "null" {
		log.ResponseHeaders = unmarshalHeaders(respHeaders)
	}
	if overrideRules != "" && overrideRules != "null" {
		_ = json.Unmarshal([]byte(overrideRules), &log.RequestOverrideRules)
	}
	if headerOverrideChanges != "" && headerOverrideChanges != "null" {
		log.RequestHeaderOverrideChanges = json.RawMessage(headerOverrideChanges)
	}
	if reqHeadersOriginal != "" && reqHeadersOriginal != "null" {
		log.RequestHeadersOriginal = unmarshalHeaders(reqHeadersOriginal)
	}

	log.Annotation.Saved = annotationSaved == 1
	log.Annotation.Status = normalizeAnnotationStatus(log.Annotation.Status)
	_ = json.Unmarshal([]byte(annotationLabels), &log.Annotation.Labels)
	if annotationCreatedMS > 0 {
		log.Annotation.CreatedAt = time.UnixMilli(annotationCreatedMS).UTC()
	}
	if annotationUpdatedMS > 0 {
		log.Annotation.UpdatedAt = time.UnixMilli(annotationUpdatedMS).UTC()
	}

	return &log, nil
}

func unmarshalHeaders(data string) map[string][]string {
	// First try unmarshaling as map[string][]string (new format)
	var multi map[string][]string
	if err := json.Unmarshal([]byte(data), &multi); err == nil {
		return multi
	}

	// Fallback to map[string]string (old format)
	var single map[string]string
	if err := json.Unmarshal([]byte(data), &single); err == nil {
		res := make(map[string][]string)
		for k, v := range single {
			res[k] = []string{v}
		}
		return res
	}

	return nil
}

func nullIntPtr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func normalizeAnnotationStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "todo":
		return "todo"
	case "done":
		return "done"
	default:
		return "none"
	}
}

func normalizeLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
