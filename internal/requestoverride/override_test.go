package requestoverride

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestApplyPatchSupportsCoreOps(t *testing.T) {
	var doc interface{}
	if err := json.Unmarshal([]byte(`{"model":"claude","system":["base"],"metadata":{"a":1}}`), &doc); err != nil {
		t.Fatal(err)
	}

	got, err := ApplyPatch(doc, []config.RequestOverridePatch{
		{Op: "add", Path: "/system/0", Value: "billing"},
		{Op: "replace", Path: "/metadata/a", Value: 2},
		{Op: "copy", From: "/model", Path: "/metadata/model"},
		{Op: "test", Path: "/metadata/model", Value: "claude"},
		{Op: "move", From: "/metadata/model", Path: "/metadata/copied_model"},
		{Op: "remove", Path: "/metadata/a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"metadata":{"copied_model":"claude"},"model":"claude","system":["billing","base"]}`
	if string(out) != want {
		t.Fatalf("patched JSON = %s, want %s", out, want)
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
						{Path: "/model", StartsWith: "claude"},
					},
				},
				Patch: []config.RequestOverridePatch{
					{Op: "add", Path: "/metadata/user_id", Value: "u1"},
				},
			},
		},
	}

	result, err := Apply(cfg, RequestInfo{
		Upstream:    "anthropic",
		Method:      "POST",
		Path:        "/v1/messages",
		ContentType: "application/json",
	}, []byte(`{"model":"claude-3","metadata":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedRuleNames) != 1 || result.AppliedRuleNames[0] != "claude metadata" {
		t.Fatalf("applied rules = %#v", result.AppliedRuleNames)
	}
	if string(result.Body) != `{"metadata":{"user_id":"u1"},"model":"claude-3"}` {
		t.Fatalf("body = %s", result.Body)
	}
}

func TestApplySkipsWhenJSONConditionDoesNotMatch(t *testing.T) {
	cfg := config.RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]config.RequestOverrideUpstreamBinding{
			"anthropic": {
				Enabled:   true,
				RuleNames: []string{"only claude"},
			},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "only claude",
				Enabled: true,
				Match: config.RequestOverrideMatch{
					JSON: []config.RequestOverrideJSONCondition{{Path: "/model", StartsWith: "claude"}},
				},
				Patch: []config.RequestOverridePatch{{Op: "add", Path: "/metadata/user_id", Value: "u1"}},
			},
		},
	}

	body := []byte(`{"model":"gpt-4","metadata":{}}`)
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
			"anthropic": {
				Enabled:   true,
				RuleNames: []string{"add metadata"},
			},
		},
		Rules: []config.RequestOverrideRule{
			{
				Name:    "add metadata",
				Enabled: true,
				Patch:   []config.RequestOverridePatch{{Op: "add", Path: "/metadata/user_id", Value: "u1"}},
			},
		},
	}

	body := []byte(`{"metadata":{}}`)
	result, err := Apply(cfg, RequestInfo{Upstream: "openai", ContentType: "application/json"}, body)
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

func TestApplyPatchRejectsInvalidJSONPointer(t *testing.T) {
	doc := map[string]interface{}{}
	if _, err := ApplyPatch(doc, []config.RequestOverridePatch{
		{Op: "add", Path: "no-leading-slash", Value: "x"},
	}); err == nil {
		t.Fatal("expected error for pointer missing leading slash")
	}
}

func TestApplyPatchRejectsUnsupportedOp(t *testing.T) {
	doc := map[string]interface{}{}
	if _, err := ApplyPatch(doc, []config.RequestOverridePatch{
		{Op: "frob", Path: "/x", Value: 1},
	}); err == nil {
		t.Fatal("expected error for unsupported op")
	}
}

func TestApplyPatchRejectsArrayIndexOutOfRange(t *testing.T) {
	doc := map[string]interface{}{
		"arr": []interface{}{"only"},
	}
	if _, err := ApplyPatch(doc, []config.RequestOverridePatch{
		{Op: "replace", Path: "/arr/5", Value: "x"},
	}); err == nil {
		t.Fatal("expected error for out-of-range array index")
	}
}

func TestApplyPatchHandlesPointerEscapes(t *testing.T) {
	doc := map[string]interface{}{}
	got, err := ApplyPatch(doc, []config.RequestOverridePatch{
		{Op: "add", Path: "/a~1b", Value: "slash"},
		{Op: "add", Path: "/c~0d", Value: "tilde"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("result type = %T, want map", got)
	}
	if m["a/b"] != "slash" {
		t.Fatalf("a/b = %v, want \"slash\"", m["a/b"])
	}
	if m["c~d"] != "tilde" {
		t.Fatalf("c~d = %v, want \"tilde\"", m["c~d"])
	}
}
