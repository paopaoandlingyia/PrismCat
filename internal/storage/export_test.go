package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRepositoryExportLogsUsesFiltersAndFullRows(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository returned error: %v", err)
	}
	defer repo.Close()

	base := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	if err := repo.SaveLog(&RequestLog{
		ID:           "before",
		CreatedAt:    base.Add(-time.Hour),
		Upstream:     "openai",
		TargetURL:    "https://example.test",
		Method:       "POST",
		Path:         "/v1/responses",
		RequestBody:  `{"before":true}`,
		ResponseBody: `{"ok":false}`,
	}); err != nil {
		t.Fatalf("SaveLog(before) returned error: %v", err)
	}
	if err := repo.SaveLog(&RequestLog{
		ID:           "matched",
		CreatedAt:    base,
		Upstream:     "openai",
		TargetURL:    "https://example.test",
		Method:       "POST",
		Path:         "/v1/responses",
		RequestBody:  `{"prompt":"hello"}`,
		ResponseBody: `{"answer":"world"}`,
	}); err != nil {
		t.Fatalf("SaveLog(matched) returned error: %v", err)
	}
	if _, err := repo.SaveLogAnnotation("matched", LogAnnotation{
		Saved:  true,
		Status: "todo",
		Labels: []string{"export"},
	}); err != nil {
		t.Fatalf("SaveLogAnnotation returned error: %v", err)
	}

	saved := true
	var exported []*RequestLog
	if err := repo.ExportLogs(context.Background(), LogFilter{
		StartTime: &base,
		Saved:     &saved,
		Label:     "export",
	}, func(log *RequestLog) error {
		exported = append(exported, log)
		return nil
	}); err != nil {
		t.Fatalf("ExportLogs returned error: %v", err)
	}

	if len(exported) != 1 {
		t.Fatalf("exported logs = %d, want 1", len(exported))
	}
	got := exported[0]
	if got.ID != "matched" || got.RequestBody == "" || got.ResponseBody == "" {
		t.Fatalf("exported log missing full fields: %#v", got)
	}
	if !got.Annotation.Saved || got.Annotation.Status != "todo" || len(got.Annotation.Labels) != 1 {
		t.Fatalf("exported annotation = %#v, want saved todo export", got.Annotation)
	}
}
