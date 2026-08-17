package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type flakyIgnoredPathStore struct {
	Repository
	failures int
	saved    []IgnoredPathObservation
}

func (s *flakyIgnoredPathStore) UpsertIgnoredPaths(entries []IgnoredPathObservation, _ int) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("temporary write failure")
	}
	s.saved = append(s.saved, entries...)
	return nil
}

func (s *flakyIgnoredPathStore) ListIgnoredPaths(IgnoredPathFilter) (IgnoredPathListResult, error) {
	return IgnoredPathListResult{}, nil
}

func (s *flakyIgnoredPathStore) DeleteIgnoredPaths(string, string) (int64, error)  { return 0, nil }
func (s *flakyIgnoredPathStore) DeleteIgnoredPathsBefore(time.Time) (int64, error) { return 0, nil }
func (s *flakyIgnoredPathStore) Close() error                                      { return nil }

func TestIgnoredPathStatsAggregateListAndDelete(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "ignored.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository() error = %v", err)
	}
	defer repo.Close()

	base := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertIgnoredPaths([]IgnoredPathObservation{
		{Upstream: "openai", Path: "/assets/app.js", Count: 2, LastSeen: base},
		{Upstream: "openai", Path: "/assets/app.js", Count: 3, LastSeen: base.Add(time.Minute)},
		{Upstream: "openai", Path: "/favicon.ico", Count: 1, LastSeen: base.Add(2 * time.Minute)},
		{Upstream: "claude", Path: "/", Count: 4, LastSeen: base.Add(3 * time.Minute)},
	}, IgnoredPathMaxEntriesPerUpstream); err != nil {
		t.Fatalf("UpsertIgnoredPaths() error = %v", err)
	}

	result, err := repo.ListIgnoredPaths(IgnoredPathFilter{Upstream: "openai", Limit: 20})
	if err != nil {
		t.Fatalf("ListIgnoredPaths() error = %v", err)
	}
	if result.Total != 2 || result.TotalRequests != 6 {
		t.Fatalf("summary = total %d requests %d, want 2/6", result.Total, result.TotalRequests)
	}
	if len(result.Paths) != 2 || result.Paths[1].RequestCount != 5 || !result.Paths[1].LastSeen.Equal(base.Add(time.Minute)) {
		t.Fatalf("aggregated paths = %#v", result.Paths)
	}

	deleted, err := repo.DeleteIgnoredPaths("openai", "/assets/app.js")
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteIgnoredPaths() = %d, %v; want 1, nil", deleted, err)
	}
	deleted, err = repo.DeleteIgnoredPathsBefore(base.Add(3 * time.Minute))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteIgnoredPathsBefore() = %d, %v; want 1, nil", deleted, err)
	}
}

func TestIgnoredPathStatsEnforcePerUpstreamLimit(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "ignored-limit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository() error = %v", err)
	}
	defer repo.Close()

	base := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	entries := []IgnoredPathObservation{
		{Upstream: "openai", Path: "/old", Count: 1, LastSeen: base},
		{Upstream: "openai", Path: "/middle", Count: 1, LastSeen: base.Add(time.Minute)},
		{Upstream: "openai", Path: "/new", Count: 1, LastSeen: base.Add(2 * time.Minute)},
	}
	if err := repo.UpsertIgnoredPaths(entries, 2); err != nil {
		t.Fatalf("UpsertIgnoredPaths() error = %v", err)
	}
	result, err := repo.ListIgnoredPaths(IgnoredPathFilter{Upstream: "openai", Limit: 20})
	if err != nil {
		t.Fatalf("ListIgnoredPaths() error = %v", err)
	}
	if result.Total != 2 || result.Paths[0].Path != "/new" || result.Paths[1].Path != "/middle" {
		t.Fatalf("limited paths = %#v", result.Paths)
	}
}

func TestAsyncRepositoryFlushesIgnoredPaths(t *testing.T) {
	inner, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "ignored-async.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository() error = %v", err)
	}
	async := NewAsyncRepository(inner, nil, 16)
	seenAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := async.RecordIgnoredPath("openai", "/assets/app.js", seenAt); err != nil {
			t.Fatalf("RecordIgnoredPath() error = %v", err)
		}
	}
	async.flushIgnoredPaths()
	result, err := async.ListIgnoredPaths(IgnoredPathFilter{Limit: 20})
	if err != nil {
		t.Fatalf("ListIgnoredPaths() error = %v", err)
	}
	if result.Total != 1 || result.TotalRequests != 3 {
		t.Fatalf("async result = %#v", result)
	}
	if err := async.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAsyncRepositoryRetriesIgnoredPathsAfterFlushFailure(t *testing.T) {
	inner := &flakyIgnoredPathStore{failures: 1}
	async := NewAsyncRepository(inner, nil, 16)
	seenAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := async.RecordIgnoredPath("openai", "/assets/app.js", seenAt); err != nil {
			t.Fatalf("RecordIgnoredPath() error = %v", err)
		}
	}
	if err := async.flushIgnoredPaths(); err == nil {
		t.Fatal("first flush succeeded, want temporary error")
	}
	if err := async.flushIgnoredPaths(); err != nil {
		t.Fatalf("second flush error = %v", err)
	}
	if len(inner.saved) != 1 || inner.saved[0].Count != 3 {
		t.Fatalf("saved observations = %#v", inner.saved)
	}
	if err := async.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
