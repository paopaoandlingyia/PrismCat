package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type loggingRulesAPIRepo struct {
	storage.Repository
	filter  storage.IgnoredPathFilter
	deleted struct {
		upstream string
		path     string
	}
}

func (r *loggingRulesAPIRepo) RecordIgnoredPath(string, string, time.Time) error { return nil }

func (r *loggingRulesAPIRepo) ListIgnoredPaths(filter storage.IgnoredPathFilter) (storage.IgnoredPathListResult, error) {
	r.filter = filter
	return storage.IgnoredPathListResult{
		Paths: []storage.IgnoredPathRecord{{
			Upstream:     "openai",
			Path:         "/assets/app.js",
			RequestCount: 3,
			LastSeen:     time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
		}},
		Total:         1,
		TotalRequests: 3,
	}, nil
}

func (r *loggingRulesAPIRepo) DeleteIgnoredPaths(upstream, requestPath string) (int64, error) {
	r.deleted.upstream = upstream
	r.deleted.path = requestPath
	return 1, nil
}

func (r *loggingRulesAPIRepo) DeleteIgnoredPathsBefore(time.Time) (int64, error) { return 0, nil }

func TestHandleModelPathTemplatesReturnsUserAndSystemTemplates(t *testing.T) {
	h := &Handler{cfg: &config.Config{LogRules: config.LoggingRulesConfig{
		ModelPathTemplatesInitialized: true,
		ModelPathTemplates: []config.ModelPathTemplate{{
			Tag:   "openai2",
			Rules: []config.LoggingPathRule{{Matcher: config.PathMatcherAnt, Pattern: "/custom"}},
		}},
	}}}
	req := httptest.NewRequest(http.MethodGet, "/api/logging-rules/model-path-templates", nil)
	recorder := httptest.NewRecorder()

	h.handleModelPathTemplates(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Templates      []config.ModelPathTemplate       `json:"templates"`
		SystemDefaults []config.SystemModelPathTemplate `json:"system_defaults"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Templates) != 1 || response.Templates[0].Tag != "openai2" {
		t.Fatalf("user templates = %#v", response.Templates)
	}
	if len(response.SystemDefaults) < 3 || response.SystemDefaults[0].Provider == "" || response.SystemDefaults[0].Category == "" {
		t.Fatalf("system defaults = %#v", response.SystemDefaults)
	}
}

func TestHandleModelPathTemplatesRejectsDuplicateTags(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}
	body := `{"templates":[{"tag":"OpenAI","rules":[]},{"tag":"openai","rules":[]}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/logging-rules/model-path-templates", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	h.handleModelPathTemplates(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleIgnoredPathsSupportsFiltersAndDelete(t *testing.T) {
	repo := &loggingRulesAPIRepo{}
	h := &Handler{cfg: &config.Config{}, repo: repo}
	req := httptest.NewRequest(http.MethodGet, "/api/logging-rules/ignored-paths?upstream=openai&path=%2Fassets&sort=count&order=asc&offset=10&limit=25", nil)
	recorder := httptest.NewRecorder()

	h.handleIgnoredPaths(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if repo.filter.Upstream != "openai" || repo.filter.Path != "/assets" || repo.filter.Sort != "count" || repo.filter.Order != "asc" || repo.filter.Offset != 10 || repo.filter.Limit != 25 {
		t.Fatalf("filter = %#v", repo.filter)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/logging-rules/ignored-paths?upstream=openai&path=%2Fassets%2Fapp.js", nil)
	recorder = httptest.NewRecorder()
	h.handleIgnoredPaths(recorder, req)
	if recorder.Code != http.StatusOK || repo.deleted.upstream != "openai" || repo.deleted.path != "/assets/app.js" {
		t.Fatalf("DELETE status = %d, deleted = %#v, body = %s", recorder.Code, repo.deleted, recorder.Body.String())
	}
}

func TestHandleUpstreamsRejectsInvalidLoggingPathFilter(t *testing.T) {
	h := &Handler{cfg: &config.Config{Upstreams: map[string]config.UpstreamConfig{}}}
	body := `{"name":"openai","target":"https://api.openai.com","logging_path_filter":{"mode":"allowlist","rules":[]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/upstreams", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	h.handleUpstreams(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
