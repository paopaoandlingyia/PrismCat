package main

import (
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func TestScheduledArchiveHandledFindsEarlierJobAfterManualRun(t *testing.T) {
	cutoff := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	jobs := []storage.ArchiveJob{
		{ID: "manual-latest", Trigger: "manual", Cutoff: cutoff.Add(time.Hour), Status: "complete"},
		{ID: "scheduled", Trigger: "scheduled", Cutoff: cutoff, Status: "complete"},
	}
	if !scheduledArchiveHandled(jobs, cutoff) {
		t.Fatal("completed scheduled job was ignored after a newer manual job")
	}
	jobs[1].Status = "failed"
	if scheduledArchiveHandled(jobs, cutoff) {
		t.Fatal("failed scheduled job should remain retryable")
	}
}
