package storage

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRequestLog_Clone(t *testing.T) {
	inputTokens := int64(10)
	outputTokens := int64(20)
	totalTokens := int64(30)

	log := &RequestLog{
		ID:        "log-1",
		Upstream:  "upstream-1",
		Method:    "POST",
		Path:      "/v1/completions",
		Query:     "foo=bar",
		Tag:       "tag-1",
		TraceID:   "trace-1",
		StatusCode: 200,
		Error:     "err",
		Latency:   100,
		Streaming: true,
		RequestBody: "req-body",
		ResponseBody: "resp-body",
		RequestHeaders: map[string][]string{
			"Content-Type": {"application/json"},
		},
		ResponseHeaders: map[string][]string{
			"Server": {"Nginx"},
		},
		RequestHeadersOriginal: map[string][]string{
			"Content-Type": {"application/json"},
		},
		RequestBodyRaw:               []byte("raw-req"),
		RequestBodyOriginalRaw:       []byte("raw-orig-req"),
		RequestBodyFinalRaw:          []byte("raw-final-req"),
		ResponseBodyRaw:              []byte("raw-resp"),
		RequestOverrideRules:         []string{"rule-1", "rule-2"},
		RequestHeaderOverrideChanges: json.RawMessage(`{"set":{"x-foo":"bar"}}`),
		UsageInputTokens:             &inputTokens,
		UsageOutputTokens:            &outputTokens,
		UsageTotalTokens:             &totalTokens,
		Annotation: LogAnnotation{
			Saved:  true,
			Status: "completed",
			Note:   "some note",
			Labels: []string{"label-1", "label-2"},
		},
	}

	cloned := log.Clone()

	// Verify deep equality of values
	if cloned.ID != log.ID || cloned.Upstream != log.Upstream || cloned.Method != log.Method {
		t.Errorf("Basic fields not matched")
	}

	// Verify slices and maps are distinct copies
	if &cloned.RequestHeaders == &log.RequestHeaders {
		t.Errorf("RequestHeaders map pointer should be different")
	}
	if reflect.ValueOf(cloned.RequestHeaders).Pointer() == reflect.ValueOf(log.RequestHeaders).Pointer() {
		t.Errorf("RequestHeaders underlying map should be different")
	}
	if &cloned.RequestBodyRaw[0] == &log.RequestBodyRaw[0] {
		t.Errorf("RequestBodyRaw slice backing array should be different")
	}
	if &cloned.RequestOverrideRules[0] == &log.RequestOverrideRules[0] {
		t.Errorf("RequestOverrideRules slice backing array should be different")
	}
	if &cloned.Annotation.Labels[0] == &log.Annotation.Labels[0] {
		t.Errorf("Annotation Labels slice backing array should be different")
	}

	// Verify pointer fields are distinct copies
	if cloned.UsageInputTokens == log.UsageInputTokens {
		t.Errorf("UsageInputTokens pointer should be different")
	}
	if *cloned.UsageInputTokens != *log.UsageInputTokens {
		t.Errorf("UsageInputTokens value should match")
	}
}

func TestFirstHeaderValue(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"application/json"},
		"x-api-key":    {"secret-key"},
	}

	if val := FirstHeaderValue(headers, "Content-Type"); val != "application/json" {
		t.Errorf("Expected application/json, got %q", val)
	}
	if val := FirstHeaderValue(headers, "content-type"); val != "application/json" {
		t.Errorf("Expected application/json (case-insensitive), got %q", val)
	}
	if val := FirstHeaderValue(headers, "X-API-KEY"); val != "secret-key" {
		t.Errorf("Expected secret-key, got %q", val)
	}
	if val := FirstHeaderValue(headers, "Non-Existent"); val != "" {
		t.Errorf("Expected empty string for non-existent header, got %q", val)
	}
}
