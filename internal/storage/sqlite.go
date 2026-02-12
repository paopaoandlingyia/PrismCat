package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
		upstream TEXT NOT NULL,
		target_url TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		query TEXT,
		request_headers TEXT,
		request_body TEXT,
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
		truncated INTEGER DEFAULT 0
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

// SaveLog inserts or updates a log entry (upsert by id).
func (r *SQLiteRepository) SaveLog(log *RequestLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}

	reqHeaders, _ := json.Marshal(log.RequestHeaders)
	respHeaders, _ := json.Marshal(log.ResponseHeaders)

	query := `
	INSERT INTO request_logs (
		id, created_at, upstream, target_url, method, path, query,
		request_headers, request_body, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		created_at = excluded.created_at,
		upstream = excluded.upstream,
		target_url = excluded.target_url,
		method = excluded.method,
		path = excluded.path,
		query = excluded.query,
		request_headers = excluded.request_headers,
		request_body = excluded.request_body,
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
		truncated = excluded.truncated
	`

	_, err := r.db.Exec(query,
		log.ID, log.CreatedAt, log.Upstream, log.TargetURL, log.Method, log.Path, log.Query,
		string(reqHeaders), log.RequestBody, log.RequestBodyRef, log.RequestBodySize,
		log.StatusCode, string(respHeaders), log.ResponseBody, log.ResponseBodyRef, log.ResponseBodySize,
		log.Streaming, log.Latency, log.Error, log.Truncated,
	)
	return err
}

func (r *SQLiteRepository) GetLog(id string) (*RequestLog, error) {
	query := `
	SELECT id, created_at, upstream, target_url, method, path, query,
		request_headers, request_body, request_body_ref, request_body_size,
		status_code, response_headers, response_body, response_body_ref, response_body_size,
		streaming, latency_ms, error, truncated
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
		conditions = append(conditions, "created_at >= ?")
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, *filter.EndTime)
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
		streaming, latency_ms, error, truncated
	FROM request_logs %s
	ORDER BY created_at DESC
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
	result, err := r.db.Exec("DELETE FROM request_logs WHERE created_at < ?", before)
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
		where = "WHERE created_at >= ?"
		args = append(args, *since)
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
	var streaming, truncated int

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&log.RequestBodySize, &log.StatusCode, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1

	return &log, nil
}

func (r *SQLiteRepository) scanLog(scanner interface{ Scan(...interface{}) error }) (*RequestLog, error) {
	var log RequestLog
	var reqHeaders, respHeaders string
	var streaming, truncated int

	err := scanner.Scan(
		&log.ID, &log.CreatedAt, &log.Upstream, &log.TargetURL, &log.Method, &log.Path, &log.Query,
		&reqHeaders, &log.RequestBody, &log.RequestBodyRef, &log.RequestBodySize,
		&log.StatusCode, &respHeaders, &log.ResponseBody, &log.ResponseBodyRef, &log.ResponseBodySize,
		&streaming, &log.Latency, &log.Error, &truncated,
	)
	if err != nil {
		return nil, err
	}

	log.Streaming = streaming == 1
	log.Truncated = truncated == 1

	if reqHeaders != "" && reqHeaders != "null" {
		_ = json.Unmarshal([]byte(reqHeaders), &log.RequestHeaders)
	}
	if respHeaders != "" && respHeaders != "null" {
		_ = json.Unmarshal([]byte(respHeaders), &log.ResponseHeaders)
	}

	return &log, nil
}
