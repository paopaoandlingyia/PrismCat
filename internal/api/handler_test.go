package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func TestConfigGETDoesNotExposeArchiveSecret(t *testing.T) {
	h := &Handler{cfg: &config.Config{Archive: config.ArchiveConfig{
		S3:        config.ArchiveS3Config{AccessKeyID: "visible-access-key", SecretAccessKey: "never-return-this-secret"},
		KeyPrefix: "backups/prismcat", ScheduleTime: "02:00", Timezone: "Asia/Shanghai",
		ZstdLevel: 10, LocalRetentionHours: 24, ImportRetentionHours: 24,
	}}}
	recorder := httptest.NewRecorder()
	h.handleConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "never-return-this-secret") || strings.Contains(recorder.Body.String(), "secret_access_key") {
		t.Fatalf("config response exposed secret field: %s", recorder.Body.String())
	}
	var body struct {
		Archive struct {
			S3 struct {
				SecretConfigured bool `json:"secret_configured"`
			} `json:"s3"`
		} `json:"archive"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Archive.S3.SecretConfigured {
		t.Fatal("secret_configured = false, want true")
	}
}

func TestConfigPUTRejectsInvalidArchiveWithoutPartialUpdate(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{MaxRequestBody: 1234},
		Archive: config.ArchiveConfig{
			S3:        config.ArchiveS3Config{Region: "test", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret"},
			KeyPrefix: "backups/prismcat", ScheduleTime: "02:00", Timezone: "Asia/Shanghai",
			ZstdLevel: 10, LocalRetentionHours: 24, ImportRetentionHours: 24,
		},
	}
	h := &Handler{cfg: cfg}
	payload := []byte(`{
		"logging":{"max_request_body":9999},
		"archive":{"enabled":true,"s3":{"region":"test","bucket":"bucket","access_key_id":"key"},"key_prefix":"backups/${unknown}","schedule_time":"02:00","timezone":"Asia/Shanghai","zstd_level":10,"local_retention_hours":24,"import_retention_hours":24}
	}`)
	recorder := httptest.NewRecorder()
	h.handleConfig(recorder, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(payload)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := cfg.LoggingSnapshot().MaxRequestBody; got != 1234 {
		t.Fatalf("logging was partially updated: max_request_body=%d", got)
	}
	if got := cfg.ArchiveSnapshot().KeyPrefix; got != "backups/prismcat" {
		t.Fatalf("archive was partially updated: key_prefix=%q", got)
	}
}

func TestResolveReplayTargetFromUpstream(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"openai": {
					Target:        "https://api.openai.com/base",
					Timeout:       42,
					OutboundProxy: "direct",
				},
			},
		},
	}

	fullURL, host, outboundProxy, timeout, err := h.resolveReplayTarget("openai", "", "/v1/chat/completions?debug=1")
	if err != nil {
		t.Fatalf("resolveReplayTarget returned error: %v", err)
	}
	if fullURL != "https://api.openai.com/base/v1/chat/completions?debug=1" {
		t.Fatalf("fullURL = %q", fullURL)
	}
	if host != "api.openai.com" {
		t.Fatalf("host = %q", host)
	}
	if outboundProxy != "direct" {
		t.Fatalf("outboundProxy = %q", outboundProxy)
	}
	if timeout != 42 {
		t.Fatalf("timeout = %d", timeout)
	}
}

func TestResolveReplayTargetFromCustomURL(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}

	fullURL, host, outboundProxy, timeout, err := h.resolveReplayTarget("", "https://example.com/v1/messages?q=1", "")
	if err != nil {
		t.Fatalf("resolveReplayTarget returned error: %v", err)
	}
	if fullURL != "https://example.com/v1/messages?q=1" {
		t.Fatalf("fullURL = %q", fullURL)
	}
	if host != "example.com" {
		t.Fatalf("host = %q", host)
	}
	if outboundProxy != "env" {
		t.Fatalf("outboundProxy = %q", outboundProxy)
	}
	if timeout != 120 {
		t.Fatalf("timeout = %d", timeout)
	}
}

func TestResolveReplayTargetRejectsUnsupportedCustomURL(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}

	_, _, _, _, err := h.resolveReplayTarget("", "ftp://example.com/file", "")
	if err == nil {
		t.Fatal("resolveReplayTarget succeeded, want error")
	}
	if !strings.Contains(err.Error(), "http 或 https") {
		t.Fatalf("error = %q", err)
	}
}

func TestParseLogFilterValidatesBackupStatus(t *testing.T) {
	filter, err := parseLogFilter(url.Values{"backup_status": {storage.BackupStatusVerified}}, true)
	if err != nil || filter.BackupStatus != storage.BackupStatusVerified {
		t.Fatalf("valid backup filter = %#v, %v", filter, err)
	}
	if _, err := parseLogFilter(url.Values{"backup_status": {"uploading"}}, true); err == nil {
		t.Fatal("invalid backup_status was accepted")
	}
}

func TestEnrichLogArchiveStateDerivesDeleteEligibility(t *testing.T) {
	graceStarted := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	verifiedAt := graceStarted.Add(-time.Hour)
	h := &Handler{cfg: &config.Config{Archive: config.ArchiveConfig{LocalRetentionHours: 6}}}
	logEntry := &storage.RequestLog{
		Origin: "live", BackupVerifiedAt: &verifiedAt, DeleteGraceStartedAt: &graceStarted,
		Annotation: storage.LogAnnotation{Saved: false},
	}
	h.enrichLogArchiveState(logEntry)
	if logEntry.DeleteEligibleAt == nil || !logEntry.DeleteEligibleAt.Equal(graceStarted.Add(6*time.Hour)) {
		t.Fatalf("delete_eligible_at = %v", logEntry.DeleteEligibleAt)
	}

	logEntry.Annotation.Saved = true
	h.enrichLogArchiveState(logEntry)
	if logEntry.DeleteEligibleAt != nil {
		t.Fatalf("saved log has delete_eligible_at = %v", logEntry.DeleteEligibleAt)
	}

	logEntry.Annotation.Saved = false
	h.cfg.Archive.LocalRetentionHours = 2
	h.enrichLogArchiveState(logEntry)
	if logEntry.DeleteEligibleAt == nil || !logEntry.DeleteEligibleAt.Equal(graceStarted.Add(2*time.Hour)) {
		t.Fatalf("updated retention delete_eligible_at = %v", logEntry.DeleteEligibleAt)
	}
}

func TestArchiveHistoryQueryValidation(t *testing.T) {
	h := &Handler{cfg: &config.Config{Archive: config.ArchiveConfig{Timezone: "Asia/Shanghai"}}}
	for _, test := range []struct {
		name string
		url  string
	}{
		{"date type", "/api/archives/packages?date_type=created_at"},
		{"date", "/api/archives/packages?date_type=completed_at&date=2026-02-30"},
		{"offset", "/api/archives/packages?offset=-1"},
		{"limit", "/api/archives/packages?limit=201"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.handleArchivePackages(recorder, httptest.NewRequest(http.MethodGet, test.url, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/archives/jobs?offset=20&limit=50", nil)
	offset, limit, err := parseArchivePagination(request)
	if err != nil || offset != 20 || limit != 50 {
		t.Fatalf("pagination = %d, %d, %v", offset, limit, err)
	}
}
