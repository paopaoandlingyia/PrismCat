package storage

import (
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
		request_override_error TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_logs_created_at ON request_logs(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_logs_upstream ON request_logs(upstream);
	CREATE INDEX IF NOT EXISTS idx_logs_status_code ON request_logs(status_code);
	CREATE INDEX IF NOT EXISTS idx_logs_method ON request_logs(method);
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

	query := `
	INSERT INTO request_logs (
		id, created_at, created_at_unix_ms, upstream, target_url, method, path, query,
		request_headers, request_body, request_body_original, request_body_final, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated, tag,
		request_override_applied, request_override_rules, request_override_error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		created_at = excluded.created_at,
		created_at_unix_ms = excluded.created_at_unix_ms,
		upstream = excluded.upstream,
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
		request_override_error = excluded.request_override_error
	`

	_, err := r.db.Exec(query,
		log.ID, log.CreatedAt, log.CreatedAt.UnixMilli(), log.Upstream, log.TargetURL, log.Method, log.Path, log.Query,
		string(reqHeaders), log.RequestBody, log.RequestBodyOriginal, log.RequestBodyFinal, log.RequestBodyRef, log.RequestBodySize,
		log.StatusCode, string(respHeaders), log.ResponseBody, log.ResponseBodyRef, log.ResponseBodySize,
		log.Streaming, log.Latency, log.Error, log.Truncated, log.Tag,
		log.RequestOverrideApplied, string(overrideRules), log.RequestOverrideError,
	)
	return err
}

func (r *SQLiteRepository) GetLog(id string) (*RequestLog, error) {
	query := `
	SELECT id, created_at, upstream, target_url, method, path, query,
		request_headers, request_body, request_body_original, request_body_final, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated, tag,
		request_override_applied, request_override_rules, request_override_error
	FROM request_logs WHERE id = ?
	`
	row := r.db.QueryRow(query, id)
	return r.scanLog(row)
}

func (r *SQLiteRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	var conditions []string
	var args []interface{}

	if filter.Upstream != "" {
		conditions = append(conditions, "upstream = ?")
		args = append(args, filter.Upstream)
	}
	if filter.Method != "" {
		conditions = append(conditions, "method = ?")
		args = append(args, filter.Method)
	}
	if filter.StatusCode > 0 {
		conditions = append(conditions, "status_code = ?")
		args = append(args, filter.StatusCode)
	}
	if filter.Path != "" {
		conditions = append(conditions, "path LIKE ?")
		args = append(args, "%"+filter.Path+"%")
	}
	if filter.StartTime != nil {
		conditions = append(conditions, "created_at_unix_ms >= ?")
		args = append(args, filter.StartTime.UTC().UnixMilli())
	}
	if filter.EndTime != nil {
		conditions = append(conditions, "created_at_unix_ms <= ?")
		args = append(args, filter.EndTime.UTC().UnixMilli())
	}
	if filter.HasError != nil {
		if *filter.HasError {
			conditions = append(conditions, "(error IS NOT NULL AND error != '')")
		} else {
			conditions = append(conditions, "(error IS NULL OR error = '')")
		}
	}
	if filter.Streaming != nil {
		conditions = append(conditions, "streaming = ?")
		args = append(args, *filter.Streaming)
	}
	if filter.Tag != "" {
		conditions = append(conditions, "tag = ?")
		args = append(args, filter.Tag)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Total count (for pagination).
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs %s", where)
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
	SELECT id, created_at, upstream, target_url, method, path, query,
		request_body_size, status_code, response_body_size,
		streaming, latency_ms, error, truncated, tag, request_override_applied
	FROM request_logs %s
	ORDER BY created_at_unix_ms DESC, created_at DESC
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

func (r *SQLiteRepository) DeleteLogsBefore(before time.Time) (int64, error) {
	result, err := r.db.Exec("DELETE FROM request_logs WHERE created_at_unix_ms < ?", before.UTC().UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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

	query := fmt.Sprintf(`
	SELECT 
		COUNT(*) as total,
		SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END) as success,
		SUM(CASE WHEN (error IS NOT NULL AND error != '') OR status_code >= 400 THEN 1 ELSE 0 END) as errors,
		SUM(CASE WHEN streaming = 1 THEN 1 ELSE 0 END) as streaming,
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

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&log.RequestBodySize, &log.StatusCode, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated, &log.Tag, &overrideApplied,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1
	log.RequestOverrideApplied = overrideApplied == 1
	log.CreatedAt = log.CreatedAt.UTC()

	return &log, nil
}

func (r *SQLiteRepository) scanLog(scanner interface{ Scan(...interface{}) error }) (*RequestLog, error) {
	var log RequestLog
	var reqHeaders, respHeaders, overrideRules string
	var streaming, truncated, overrideApplied int

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&reqHeaders, &log.RequestBody, &log.RequestBodyOriginal, &log.RequestBodyFinal, &log.RequestBodyRef, &log.RequestBodySize,
		&log.StatusCode, &respHeaders, &log.ResponseBody, &log.ResponseBodyRef, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated, &log.Tag,
		&overrideApplied, &overrideRules, &log.RequestOverrideError,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1
	log.RequestOverrideApplied = overrideApplied == 1
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
