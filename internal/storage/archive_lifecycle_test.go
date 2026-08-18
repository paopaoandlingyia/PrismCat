package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupVerificationAndDelayedCleanup(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	day := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"ordinary", "saved"} {
		if err := repo.SaveLog(&RequestLog{ID: id, CreatedAt: day.Add(time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.SaveLogAnnotation("saved", LogAnnotation{Saved: true}); err != nil {
		t.Fatal(err)
	}
	batch := ArchiveBatch{ID: "batch", ArchiveDate: "2026-08-16", RangeStart: day, RangeEnd: day.AddDate(0, 0, 1), Status: "building"}
	if err := repo.CreateArchiveBatch(batch); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReserveArchiveBatchLogs(batch.ID, batch.RangeStart, batch.RangeEnd); err != nil || n != 2 {
		t.Fatalf("reserve = %d, %v", n, err)
	}
	verifiedAt := time.Now().Add(-2 * time.Hour).UTC()
	if n, err := repo.MarkArchiveBatchVerified(batch.ID, verifiedAt); err != nil || n != 2 {
		t.Fatalf("mark = %d, %v", n, err)
	}
	for _, id := range []string{"ordinary", "saved"} {
		entry, err := repo.GetLog(id)
		if err != nil || entry.BackupVerifiedAt == nil {
			t.Fatalf("%s backup state = %#v, %v", id, entry, err)
		}
	}
	pending, earliest, err := repo.PendingBackedLogCleanup()
	if err != nil || pending != 1 || earliest == nil || earliest.UnixMilli() != verifiedAt.UnixMilli() {
		t.Fatalf("pending cleanup = %d, %v, %v", pending, earliest, err)
	}
	if n, err := repo.DeleteEligibleBackedLogs(time.Now().Add(-time.Hour), 100); err != nil || n != 1 {
		t.Fatalf("cleanup = %d, %v", n, err)
	}
	if _, err := repo.GetLog("ordinary"); err == nil {
		t.Fatal("ordinary backed-up log was not deleted")
	}
	if _, err := repo.GetLog("saved"); err != nil {
		t.Fatalf("saved log was deleted: %v", err)
	}

	if _, err := repo.SaveLogAnnotation("saved", LogAnnotation{Status: "none"}); err != nil {
		t.Fatal(err)
	}
	entry, err := repo.GetLog("saved")
	if err != nil || entry.DeleteGraceStartedAt == nil || entry.DeleteGraceStartedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("unsave did not reset grace: %#v, %v", entry, err)
	}
	if n, err := repo.DeleteEligibleBackedLogs(time.Now().Add(-time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("fresh grace cleanup = %d, %v", n, err)
	}
}

func TestArchiveBatchUsesExactReservedIDs(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	day := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	first := &RequestLog{ID: "reserved", CreatedAt: day.Add(time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}
	if err := repo.SaveLog(first); err != nil {
		t.Fatal(err)
	}
	batch := ArchiveBatch{ID: "exact-batch", ArchiveDate: "2026-08-16", RangeStart: day, RangeEnd: day.AddDate(0, 0, 1), Status: "building"}
	if err := repo.CreateArchiveBatch(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReserveArchiveBatchLogs(batch.ID, batch.RangeStart, batch.RangeEnd); err != nil {
		t.Fatal(err)
	}
	late := &RequestLog{ID: "late", CreatedAt: day.Add(2 * time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}
	if err := repo.SaveLog(late); err != nil {
		t.Fatal(err)
	}
	var exported []string
	if err := repo.ExportArchiveBatch(context.Background(), batch.ID, func(entry *RequestLog) error { exported = append(exported, entry.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 1 || exported[0] != "reserved" {
		t.Fatalf("exported IDs = %v", exported)
	}
	if _, err := repo.MarkArchiveBatchVerified(batch.ID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteEligibleBackedLogs(time.Now().Add(-time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	entry, err := repo.GetLog("late")
	if err != nil || entry.BackupVerifiedAt != nil {
		t.Fatalf("late log was touched by prior batch: %#v, %v", entry, err)
	}
}

func TestRecoverInterruptedArchiveWorkReleasesReservedLogs(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	day := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	if err := repo.SaveLog(&RequestLog{ID: "reserved", CreatedAt: day.Add(time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	job := ArchiveJob{ID: "interrupted-job", Trigger: "scheduled", Cutoff: day.AddDate(0, 0, 1), Status: "running"}
	if err := repo.CreateArchiveJob(job); err != nil {
		t.Fatal(err)
	}
	batch := ArchiveBatch{ID: "interrupted-batch", JobID: job.ID, ArchiveDate: "2026-08-16", RangeStart: day, RangeEnd: day.AddDate(0, 0, 1), Status: "uploading"}
	if err := repo.CreateArchiveBatch(batch); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReserveArchiveBatchLogs(batch.ID, batch.RangeStart, batch.RangeEnd); err != nil || n != 1 {
		t.Fatalf("reserve = %d, %v", n, err)
	}
	if err := repo.RecoverInterruptedArchiveWork(time.Now()); err != nil {
		t.Fatal(err)
	}
	jobs, err := repo.ListArchiveJobs(1)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "failed" {
		t.Fatalf("recovered jobs = %#v, %v", jobs, err)
	}
	retry := ArchiveBatch{ID: "retry-batch", JobID: "retry-job", ArchiveDate: "2026-08-16", RangeStart: day, RangeEnd: day.AddDate(0, 0, 1), Status: "building"}
	if err := repo.CreateArchiveBatch(retry); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReserveArchiveBatchLogs(retry.ID, retry.RangeStart, retry.RangeEnd); err != nil || n != 1 {
		t.Fatalf("retry reserve = %d, %v", n, err)
	}
}

func TestListAndExportLogsFilterBackupStatus(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	day := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	verified := &RequestLog{ID: "verified", CreatedAt: day.Add(time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/verified"}
	if err := repo.SaveLog(verified); err != nil {
		t.Fatal(err)
	}
	batch := ArchiveBatch{ID: "filter-batch", ArchiveDate: "2026-08-16", RangeStart: day, RangeEnd: day.Add(90 * time.Minute), Status: "building"}
	if err := repo.CreateArchiveBatch(batch); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReserveArchiveBatchLogs(batch.ID, batch.RangeStart, batch.RangeEnd); err != nil || n != 1 {
		t.Fatalf("reserve = %d, %v", n, err)
	}
	if n, err := repo.MarkArchiveBatchVerified(batch.ID, day.Add(3*time.Hour)); err != nil || n != 1 {
		t.Fatalf("mark verified = %d, %v", n, err)
	}
	if err := repo.SaveLog(&RequestLog{ID: "pending", CreatedAt: day.Add(2 * time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/pending"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveImportedLog(&RequestLog{ID: "restored", CreatedAt: day.Add(3 * time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/restored"}); err != nil {
		t.Fatal(err)
	}

	for status, wantID := range map[string]string{
		BackupStatusPending: "pending", BackupStatusVerified: "verified", BackupStatusRestored: "restored",
	} {
		logs, total, err := repo.ListLogs(LogFilter{BackupStatus: status, Limit: 10})
		if err != nil || total != 1 || len(logs) != 1 || logs[0].ID != wantID {
			t.Fatalf("ListLogs(%q) = %#v, total=%d, err=%v; want %q", status, logs, total, err, wantID)
		}
		var exported []string
		if err := repo.ExportLogs(context.Background(), LogFilter{BackupStatus: status}, func(log *RequestLog) error {
			exported = append(exported, log.ID)
			return nil
		}); err != nil {
			t.Fatalf("ExportLogs(%q): %v", status, err)
		}
		if len(exported) != 1 || exported[0] != wantID {
			t.Fatalf("ExportLogs(%q) IDs = %v; want [%s]", status, exported, wantID)
		}
	}
}

func TestArchiveHistoryPaginationAndDateFilters(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	loc := time.FixedZone("CST", 8*60*60)
	created := time.Date(2026, 8, 18, 1, 0, 0, 0, loc)
	for index, job := range []ArchiveJob{
		{ID: "manual-job", Trigger: "manual", Cutoff: created, Status: "complete", CreatedAt: created.UTC()},
		{ID: "scheduled-job", Trigger: "scheduled", Cutoff: created, Status: "complete", CreatedAt: created.Add(time.Minute).UTC()},
	} {
		completed := created.Add(time.Duration(index+1) * time.Hour).UTC()
		job.CompletedAt = &completed
		if err := repo.CreateArchiveJob(job); err != nil {
			t.Fatal(err)
		}
	}

	verifiedOne := time.Date(2026, 8, 18, 1, 10, 0, 0, loc).UTC()
	verifiedTwo := time.Date(2026, 8, 18, 3, 10, 0, 0, loc).UTC()
	verifiedThree := time.Date(2026, 8, 19, 1, 10, 0, 0, loc).UTC()
	for _, batch := range []ArchiveBatch{
		{ID: "one", JobID: "manual-job", ArchiveDate: "2026-08-17", Status: "verified", RangeStart: created, RangeEnd: created.Add(time.Hour), CreatedAt: created.UTC(), UpdatedAt: verifiedOne, VerifiedAt: &verifiedOne},
		{ID: "two", JobID: "scheduled-job", ArchiveDate: "2026-08-18", Status: "verified", RangeStart: created, RangeEnd: created.Add(time.Hour), CreatedAt: created.UTC(), UpdatedAt: verifiedTwo, VerifiedAt: &verifiedTwo},
		{ID: "three", JobID: "manual-job", ArchiveDate: "2026-08-17", Status: "verified", RangeStart: created, RangeEnd: created.Add(time.Hour), CreatedAt: created.UTC(), UpdatedAt: verifiedThree, VerifiedAt: &verifiedThree},
	} {
		if err := repo.CreateArchiveBatch(batch); err != nil {
			t.Fatal(err)
		}
	}

	page, total, err := repo.ListArchiveBatchesPage(ArchiveBatchFilter{Offset: 0, Limit: 1})
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != "three" || page[0].Trigger != "manual" {
		t.Fatalf("first package page = %#v, total=%d, err=%v", page, total, err)
	}
	from := time.Date(2026, 8, 18, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	page, total, err = repo.ListArchiveBatchesPage(ArchiveBatchFilter{
		DateType: ArchiveDateTypeCompletedAt, CompletedFrom: &from, CompletedTo: &to, Limit: 10,
	})
	if err != nil || total != 2 || len(page) != 2 || page[0].ID != "two" || page[1].ID != "one" {
		t.Fatalf("completion-date packages = %#v, total=%d, err=%v", page, total, err)
	}
	page, total, err = repo.ListArchiveBatchesPage(ArchiveBatchFilter{
		DateType: ArchiveDateTypeArchiveDate, Date: "2026-08-17", JobID: "manual-job", Limit: 10,
	})
	if err != nil || total != 2 || len(page) != 2 || page[0].ID != "three" || page[1].ID != "one" {
		t.Fatalf("archive-date packages = %#v, total=%d, err=%v", page, total, err)
	}

	jobs, total, err := repo.ListArchiveJobsPage(0, 1)
	if err != nil || total != 2 || len(jobs) != 1 || jobs[0].ID != "scheduled-job" {
		t.Fatalf("jobs page = %#v, total=%d, err=%v", jobs, total, err)
	}
	for index := 0; index < 2; index++ {
		if err := repo.CreateArchiveImport(ArchiveImport{ID: fmt.Sprintf("import-%d", index), Status: "complete", CreatedAt: created.Add(time.Duration(index) * time.Minute).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	imports, total, err := repo.ListArchiveImportsPage(1, 1)
	if err != nil || total != 2 || len(imports) != 1 || imports[0].ID != "import-0" {
		t.Fatalf("imports page = %#v, total=%d, err=%v", imports, total, err)
	}
}
