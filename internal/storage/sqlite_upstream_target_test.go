package storage

import (
	"testing"
	"time"
)

func TestSQLiteRepositoryPersistsUpstreamTarget(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()

	entry := newTestLog("target-log", time.Now())
	entry.UpstreamTarget = "backup"
	entry.TraceID = "target-trace"
	if err := repo.SaveLog(entry); err != nil {
		t.Fatalf("SaveLog() error = %v", err)
	}

	got, err := repo.GetLog(entry.ID)
	if err != nil {
		t.Fatalf("GetLog() error = %v", err)
	}
	if got.UpstreamTarget != "backup" {
		t.Fatalf("GetLog().UpstreamTarget = %q, want backup", got.UpstreamTarget)
	}

	logs, _, err := repo.ListLogs(LogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListLogs() error = %v", err)
	}
	if len(logs) != 1 || logs[0].UpstreamTarget != "backup" {
		t.Fatalf("ListLogs() target = %#v, want backup", logs)
	}

	traceLogs, err := repo.GetTraceRequests(entry.TraceID)
	if err != nil {
		t.Fatalf("GetTraceRequests() error = %v", err)
	}
	if len(traceLogs) != 1 || traceLogs[0].UpstreamTarget != "backup" {
		t.Fatalf("GetTraceRequests() target = %#v, want backup", traceLogs)
	}
}
