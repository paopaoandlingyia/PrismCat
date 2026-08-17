package storage

import (
	"fmt"
	"strings"
	"time"
)

const IgnoredPathMaxEntriesPerUpstream = 10_000

type IgnoredPathObservation struct {
	Upstream string
	Path     string
	Count    int64
	LastSeen time.Time
}

type IgnoredPathRecord struct {
	Upstream     string    `json:"upstream"`
	Path         string    `json:"path"`
	RequestCount int64     `json:"request_count"`
	LastSeen     time.Time `json:"last_seen"`
}

type IgnoredPathFilter struct {
	Upstream string
	Path     string
	Sort     string
	Order    string
	Offset   int
	Limit    int
}

type IgnoredPathListResult struct {
	Paths         []IgnoredPathRecord `json:"paths"`
	Total         int64               `json:"total"`
	TotalRequests int64               `json:"total_requests"`
}

type IgnoredPathRepository interface {
	RecordIgnoredPath(upstream, requestPath string, seenAt time.Time) error
	ListIgnoredPaths(filter IgnoredPathFilter) (IgnoredPathListResult, error)
	DeleteIgnoredPaths(upstream, requestPath string) (int64, error)
	DeleteIgnoredPathsBefore(before time.Time) (int64, error)
}

type ignoredPathPersistence interface {
	UpsertIgnoredPaths(entries []IgnoredPathObservation, maxPerUpstream int) error
	ListIgnoredPaths(filter IgnoredPathFilter) (IgnoredPathListResult, error)
	DeleteIgnoredPaths(upstream, requestPath string) (int64, error)
	DeleteIgnoredPathsBefore(before time.Time) (int64, error)
}

func (r *SQLiteRepository) UpsertIgnoredPaths(entries []IgnoredPathObservation, maxPerUpstream int) error {
	if len(entries) == 0 {
		return nil
	}
	if maxPerUpstream <= 0 {
		maxPerUpstream = IgnoredPathMaxEntriesPerUpstream
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin ignored path upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO ignored_path_stats (upstream, path, request_count, last_seen_unix_ms)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(upstream, path) DO UPDATE SET
			request_count = ignored_path_stats.request_count + excluded.request_count,
			last_seen_unix_ms = MAX(ignored_path_stats.last_seen_unix_ms, excluded.last_seen_unix_ms)`)
	if err != nil {
		return fmt.Errorf("prepare ignored path upsert: %w", err)
	}

	affected := make(map[string]struct{})
	for _, entry := range entries {
		entry.Upstream = strings.TrimSpace(entry.Upstream)
		entry.Path = strings.TrimSpace(entry.Path)
		if entry.Upstream == "" || entry.Path == "" || entry.Count <= 0 {
			continue
		}
		if entry.LastSeen.IsZero() {
			entry.LastSeen = time.Now()
		}
		if _, err := stmt.Exec(entry.Upstream, entry.Path, entry.Count, entry.LastSeen.UTC().UnixMilli()); err != nil {
			_ = stmt.Close()
			return fmt.Errorf("upsert ignored path: %w", err)
		}
		affected[entry.Upstream] = struct{}{}
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("close ignored path upsert: %w", err)
	}

	for upstream := range affected {
		if _, err := tx.Exec(`
			DELETE FROM ignored_path_stats
			WHERE rowid IN (
				SELECT rowid FROM ignored_path_stats
				WHERE upstream = ?
				ORDER BY last_seen_unix_ms DESC, path ASC
				LIMIT -1 OFFSET ?
			)`, upstream, maxPerUpstream); err != nil {
			return fmt.Errorf("enforce ignored path limit for %s: %w", upstream, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ignored path upsert: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ListIgnoredPaths(filter IgnoredPathFilter) (IgnoredPathListResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	where := []string{"1=1"}
	args := make([]interface{}, 0, 4)
	if upstream := strings.TrimSpace(filter.Upstream); upstream != "" {
		where = append(where, "upstream = ?")
		args = append(args, upstream)
	}
	if requestPath := strings.TrimSpace(filter.Path); requestPath != "" {
		where = append(where, "path LIKE ?")
		args = append(args, "%"+requestPath+"%")
	}
	whereSQL := strings.Join(where, " AND ")

	var result IgnoredPathListResult
	if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(request_count), 0) FROM ignored_path_stats WHERE `+whereSQL, args...).Scan(&result.Total, &result.TotalRequests); err != nil {
		return IgnoredPathListResult{}, fmt.Errorf("count ignored paths: %w", err)
	}

	sortColumn := "last_seen_unix_ms"
	if strings.EqualFold(filter.Sort, "count") {
		sortColumn = "request_count"
	}
	order := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		order = "ASC"
	}
	queryArgs := append(append([]interface{}{}, args...), filter.Limit, filter.Offset)
	rows, err := r.db.Query(`
		SELECT upstream, path, request_count, last_seen_unix_ms
		FROM ignored_path_stats
		WHERE `+whereSQL+`
		ORDER BY `+sortColumn+` `+order+`, upstream ASC, path ASC
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return IgnoredPathListResult{}, fmt.Errorf("list ignored paths: %w", err)
	}
	defer rows.Close()

	result.Paths = make([]IgnoredPathRecord, 0)
	for rows.Next() {
		var record IgnoredPathRecord
		var lastSeenMS int64
		if err := rows.Scan(&record.Upstream, &record.Path, &record.RequestCount, &lastSeenMS); err != nil {
			return IgnoredPathListResult{}, fmt.Errorf("scan ignored path: %w", err)
		}
		record.LastSeen = time.UnixMilli(lastSeenMS).UTC()
		result.Paths = append(result.Paths, record)
	}
	if err := rows.Err(); err != nil {
		return IgnoredPathListResult{}, fmt.Errorf("iterate ignored paths: %w", err)
	}
	return result, nil
}

func (r *SQLiteRepository) DeleteIgnoredPaths(upstream, requestPath string) (int64, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0, 2)
	if upstream = strings.TrimSpace(upstream); upstream != "" {
		where = append(where, "upstream = ?")
		args = append(args, upstream)
	}
	if requestPath = strings.TrimSpace(requestPath); requestPath != "" {
		if upstream == "" {
			return 0, fmt.Errorf("upstream is required when deleting one ignored path")
		}
		where = append(where, "path = ?")
		args = append(args, requestPath)
	}
	res, err := r.db.Exec("DELETE FROM ignored_path_stats WHERE "+strings.Join(where, " AND "), args...)
	if err != nil {
		return 0, fmt.Errorf("delete ignored paths: %w", err)
	}
	return res.RowsAffected()
}

func (r *SQLiteRepository) DeleteIgnoredPathsBefore(before time.Time) (int64, error) {
	res, err := r.db.Exec("DELETE FROM ignored_path_stats WHERE last_seen_unix_ms < ?", before.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete old ignored paths: %w", err)
	}
	return res.RowsAffected()
}
