package requestoverride

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func jsonEq(t *testing.T, got, want string) {
	t.Helper()
	var a, b interface{}
	if err := json.Unmarshal([]byte(got), &a); err != nil {
		t.Fatalf("got is not valid JSON: %s (%v)", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("want is not valid JSON: %s (%v)", want, err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestApplyPatchSet(t *testing.T) {
	got, err := ApplyPatch(`{"model":"claude"}`, []config.RequestOverridePatch{
		{Op: "set", Path: "metadata.user_id", Value: "u1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"model":"claude","metadata":{"user_id":"u1"}}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchSetAutoCreatesNestedPath(t *testing.T) {
	got, err := ApplyPatch(`{}`, []config.RequestOverridePatch{
		{Op: "set", Path: "metadata.session.id", Value: "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"metadata":{"session":{"id":"abc"}}}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchSetOverwritesExisting(t *testing.T) {
	got, err := ApplyPatch(`{"model":"gpt-4"}`, []config.RequestOverridePatch{
		{Op: "set", Path: "model", Value: "gpt-4o-mini"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"model":"gpt-4o-mini"}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchRemove(t *testing.T) {
	got, err := ApplyPatch(`{"model":"gpt-4","user":"x"}`, []config.RequestOverridePatch{
		{Op: "remove", Path: "user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"model":"gpt-4"}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchDefaultSkipsIfPresent(t *testing.T) {
	got, err := ApplyPatch(`{"max_tokens":1000}`, []config.RequestOverridePatch{
		{Op: "default", Path: "max_tokens", Value: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"max_tokens":1000}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchDefaultSetsIfMissing(t *testing.T) {
	got, err := ApplyPatch(`{"model":"x"}`, []config.RequestOverridePatch{
		{Op: "default", Path: "max_tokens", Value: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"model":"x","max_tokens":4096}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchAppendToExistingArray(t *testing.T) {
	got, err := ApplyPatch(`{"system":[{"type":"text","text":"a"}]}`, []config.RequestOverridePatch{
		{Op: "append", Path: "system", Value: map[string]interface{}{"type": "text", "text": "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonEq(t, got, `{"system":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`)
}

func TestApplyPatchAppendCreatesArrayIfMissing(t *testing.T) {
	got, err := ApplyPatch(`{}`, []config.RequestOverridePatch{
		{Op: "append", Path: "tags", Value: "foo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"tags":["foo"]}` {
		t.Fatalf("got %s", got)
	}
}

func TestApplyPatchPrependToArray(t *testing.T) {
	got, err := ApplyPatch(`{"system":[{"type":"text","text":"existing"}]}`, []config.RequestOverridePatch{
		{Op: "prepend", Path: "system", Value: map[string]interface{}{"type": "text", "text": "injected"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonEq(t, got, `{"system":[{"type":"text","text":"injected"},{"type":"text","text":"existing"}]}`)
}

func TestApplyPatchAppendFailsOnNonArrayPath(t *testing.T) {
	_, err := ApplyPatch(`{"system":"a string"}`, []config.RequestOverridePatch{
		{Op: "append", Path: "system", Value: "x"},
	})
	if err == nil {
		t.Fatal("expected error appending to non-array")
	}
}

func TestApplyPatchRejectsUnsupportedOp(t *testing.T) {
	_, err := ApplyPatch(`{}`, []config.RequestOverridePatch{
		{Op: "frob", Path: "x", Value: 1},
	})
	if err == nil {
		t.Fatal("expected error for unsupported op")
	}
}

func TestApplyMatchesRuleAndJSONCondition(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled:      true,
		MaxBodyBytes: 1024,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"anthropic": {
				Enabled:   true,
				RuleNames: []string{"claude metadata"},
			},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "claude metadata",
				Enabled: true,
				Match: config.RequestOverrideMatch{
					Methods:      []string{"POST"},
					PathPrefixes: []string{"/v1/messages"},
					JSON: []config.RequestOverrideJSONCondition{
						{Path: "model", StartsWith: "claude"},
					},
				},
				Patch: []config.RequestOverridePatch{
					{Op: "set", Path: "metadata.user_id", Value: "u1"},
				},
			},
		},
	}

	result, err := Apply(cfg, RequestInfo{
		Upstream:    "anthropic",
		Method:      "POST",
		Path:        "/v1/messages",
		ContentType: "application/json",
	}, []byte(`{"model":"claude-3"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedRuleNames) != 1 || result.AppliedRuleNames[0] != "claude metadata" {
		t.Fatalf("applied rules = %#v", result.AppliedRuleNames)
	}
	jsonEq(t, string(result.Body), `{"model":"claude-3","metadata":{"user_id":"u1"}}`)
}

func TestApplySkipsWhenJSONConditionDoesNotMatch(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"anthropic": {Enabled: true, RuleNames: []string{"only claude"}},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "only claude",
				Enabled: true,
				Match: config.RequestOverrideMatch{
					JSON: []config.RequestOverrideJSONCondition{{Path: "model", StartsWith: "claude"}},
				},
				Patch: []config.RequestOverridePatch{{Op: "set", Path: "metadata.user_id", Value: "u1"}},
			},
		},
	}

	body := []byte(`{"model":"gpt-4"}`)
	result, err := Apply(cfg, RequestInfo{Upstream: "anthropic", ContentType: "application/json"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedRuleNames) != 0 {
		t.Fatalf("applied rules = %#v", result.AppliedRuleNames)
	}
	if string(result.Body) != string(body) {
		t.Fatalf("body = %s", result.Body)
	}
}

func TestApplySkipsRulesNotBoundToUpstream(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"anthropic": {Enabled: true, RuleNames: []string{"add metadata"}},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "add metadata",
				Enabled: true,
				Patch:   []config.RequestOverridePatch{{Op: "set", Path: "metadata.user_id", Value: "u1"}},
			},
		},
	}

	body := []byte(`{}`)
	result, err := Apply(cfg, RequestInfo{Upstream: "openai", ContentType: "application/json"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedRuleNames) != 0 {
		t.Fatalf("applied rules = %#v", result.AppliedRuleNames)
	}
}

func TestApplyReturnsErrUnsupportedEncoding(t *testing.T) {
	cfg := config.RequestOverridesConfig{Enabled: true}
	_, err := Apply(cfg, RequestInfo{
		Upstream:        "anthropic",
		ContentType:     "application/json",
		ContentEncoding: "gzip",
	}, []byte(`{}`))
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("err = %v, want ErrUnsupportedEncoding", err)
	}
}

func TestApplyReturnsErrUnsupportedContent(t *testing.T) {
	cfg := config.RequestOverridesConfig{Enabled: true}
	_, err := Apply(cfg, RequestInfo{
		Upstream:    "anthropic",
		ContentType: "text/plain",
	}, []byte(`hello`))
	if !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("err = %v, want ErrUnsupportedContent", err)
	}
}

func TestApplyHeadersSetAndRemove(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"openai": {Enabled: true, RuleNames: []string{"header rule"}},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "header rule",
				Enabled: true,
				Match:   config.RequestOverrideMatch{Methods: []string{"POST"}},
				Headers: []config.RequestOverrideHeader{
					{Op: "set", Name: "Authorization", Value: "Bearer new-key"},
					{Op: "set", Name: "X-Custom", Value: "hello"},
					{Op: "remove", Name: "X-Unwanted"},
				},
			},
		},
	}

	header := http.Header{
		"Authorization": []string{"Bearer old-key"},
		"X-Unwanted":    []string{"remove-me"},
		"Content-Type":  []string{"application/json"},
	}

	changes, ruleNames := ApplyHeaders(cfg, RequestInfo{
		Upstream: "openai",
		Method:   "POST",
		Path:     "/v1/chat/completions",
	}, header)

	if len(ruleNames) != 1 || ruleNames[0] != "header rule" {
		t.Fatalf("rule names = %v", ruleNames)
	}
	if len(changes) != 3 {
		t.Fatalf("changes count = %d, want 3", len(changes))
	}
	if header.Get("Authorization") != "Bearer new-key" {
		t.Fatalf("Authorization = %q", header.Get("Authorization"))
	}
	if header.Get("X-Unwanted") != "" {
		t.Fatalf("X-Unwanted should be removed, got %q", header.Get("X-Unwanted"))
	}
	if changes[0].OldValue != "Bearer old-key" {
		t.Fatalf("changes[0].OldValue = %q", changes[0].OldValue)
	}
}

func TestApplyHeadersOnlyRuleDoesNotTriggerBodyRead(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"openai": {Enabled: true, RuleNames: []string{"headers only"}},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "headers only",
				Enabled: true,
				Headers: []config.RequestOverrideHeader{{Op: "set", Name: "X-Injected", Value: "yes"}},
			},
		},
	}

	if HasCandidate(cfg, RequestInfo{Upstream: "openai", Method: "POST", ContentType: "text/plain"}) {
		t.Fatal("HasCandidate should return false for headers-only rule")
	}

	header := http.Header{}
	changes, ruleNames := ApplyHeaders(cfg, RequestInfo{Upstream: "openai", Method: "POST"}, header)
	if len(changes) != 1 || header.Get("X-Injected") != "yes" {
		t.Fatalf("expected header to be set, got changes=%v header=%v", changes, header)
	}
	if len(ruleNames) != 1 || ruleNames[0] != "headers only" {
		t.Fatalf("rule names = %v", ruleNames)
	}
}

func TestGjsonValueEquals(t *testing.T) {
	cases := []struct {
		jsonStr string
		path    string
		want    interface{}
		equals  bool
	}{
		// Strings
		{`{"name":"claude"}`, "name", "claude", true},
		{`{"name":"claude"}`, "name", "gpt", false},
		// Numbers
		{`{"count":10}`, "count", float64(10), true},
		{`{"count":10}`, "count", 10, true},
		{`{"count":10}`, "count", int64(10), true},
		{`{"count":10.5}`, "count", float64(10.5), true},
		{`{"count":10.5}`, "count", 10, false},
		{`{"count":10}`, "count", 5, false},
		// Booleans
		{`{"ok":true}`, "ok", true, true},
		{`{"ok":true}`, "ok", false, false},
		{`{"ok":false}`, "ok", false, true},
		// Nulls
		{`{"none":null}`, "none", nil, true},
		// Slices (Slow path)
		{`{"arr":[1,2,3]}`, "arr", []interface{}{1.0, 2.0, 3.0}, true},
		{`{"arr":[1,2,3]}`, "arr", []interface{}{1, 2, 3}, true},
		{`{"arr":[1,2,3]}`, "arr", []interface{}{1, 2}, false},
		// Maps (Slow path)
		{`{"obj":{"a":1,"b":true}}`, "obj", map[string]interface{}{"a": 1, "b": true}, true},
		{`{"obj":{"a":1,"b":true}}`, "obj", map[string]interface{}{"a": 2, "b": true}, false},
	}

	for i, tc := range cases {
		importResult := gjson.Result{}
		if tc.path != "" {
			importResult = gjson.Get(tc.jsonStr, tc.path)
		} else {
			importResult = gjson.Parse(tc.jsonStr)
		}
		got := gjsonValueEquals(importResult, tc.want)
		if got != tc.equals {
			t.Errorf("case %d: gjsonValueEquals(%s, %#v) = %t; want %t", i, tc.jsonStr, tc.want, got, tc.equals)
		}
	}
}
