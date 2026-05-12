package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLogAnnotationRoundTripAndFilters(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository returned error: %v", err)
	}
	defer repo.Close()

	log := &RequestLog{
		ID:        "annotated",
		CreatedAt: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
		Upstream:  "openai",
		TargetURL: "https://api.openai.com/v1/responses",
		Method:    "POST",
		Path:      "/v1/responses",
	}
	if err := repo.SaveLog(log); err != nil {
		t.Fatalf("SaveLog returned error: %v", err)
	}

	annotation, err := repo.SaveLogAnnotation(log.ID, LogAnnotation{
		Saved:  true,
		Status: "todo",
		Note:   "Worth investigating",
		Labels: []string{"bug", "agent"},
	})
	if err != nil {
		t.Fatalf("SaveLogAnnotation returned error: %v", err)
	}
	if !annotation.Saved || annotation.Status != "todo" || len(annotation.Labels) != 2 {
		t.Fatalf("annotation = %#v, want saved todo with labels", annotation)
	}

	got, err := repo.GetLog(log.ID)
	if err != nil {
		t.Fatalf("GetLog returned error: %v", err)
	}
	if !got.Annotation.Saved || got.Annotation.Status != "todo" || got.Annotation.Note != "Worth investigating" {
		t.Fatalf("GetLog annotation = %#v", got.Annotation)
	}

	saved := true
	logs, total, err := repo.ListLogs(LogFilter{Saved: &saved, Status: "todo", Label: "bug"})
	if err != nil {
		t.Fatalf("ListLogs returned error: %v", err)
	}
	if total != 1 || len(logs) != 1 || logs[0].ID != log.ID || !logs[0].Annotation.Saved {
		t.Fatalf("filtered logs total=%d logs=%#v", total, logs)
	}
}

func TestDeleteLogsBeforeKeepsSavedLogs(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository returned error: %v", err)
	}
	defer repo.Close()

	oldTime := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	for _, log := range []*RequestLog{
		{ID: "old-saved", CreatedAt: oldTime, Upstream: "openai", TargetURL: "https://example.test", Method: "POST", Path: "/v1"},
		{ID: "old-unsaved", CreatedAt: oldTime, Upstream: "openai", TargetURL: "https://example.test", Method: "POST", Path: "/v1"},
		{ID: "new-unsaved", CreatedAt: newTime, Upstream: "openai", TargetURL: "https://example.test", Method: "POST", Path: "/v1"},
	} {
		if err := repo.SaveLog(log); err != nil {
			t.Fatalf("SaveLog(%s) returned error: %v", log.ID, err)
		}
	}
	if _, err := repo.SaveLogAnnotation("old-saved", LogAnnotation{Saved: true}); err != nil {
		t.Fatalf("SaveLogAnnotation returned error: %v", err)
	}

	deleted, err := repo.DeleteLogsBefore(time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DeleteLogsBefore returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := repo.GetLog("old-saved"); err != nil {
		t.Fatalf("old saved log should remain: %v", err)
	}
	if _, err := repo.GetLog("old-unsaved"); err == nil {
		t.Fatal("old unsaved log should have been deleted")
	}
	if _, err := repo.GetLog("new-unsaved"); err != nil {
		t.Fatalf("new unsaved log should remain: %v", err)
	}
}
