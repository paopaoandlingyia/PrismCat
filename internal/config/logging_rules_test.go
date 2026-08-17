package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemModelPathTemplates(t *testing.T) {
	templates, err := SystemModelPathTemplates()
	if err != nil {
		t.Fatalf("SystemModelPathTemplates() error = %v", err)
	}
	wantInitialized := map[string]bool{
		"openai-core": true,
		"claude-core": true,
		"gemini-core": true,
	}
	if len(templates) != 18 {
		t.Fatalf("template count = %d, want 18", len(templates))
	}
	var openAIFull *SystemModelPathTemplate
	for _, template := range templates {
		if template.DisplayName == "" || template.Description == "" || template.Provider == "" || template.Category == "" {
			t.Fatalf("template %q has incomplete metadata: %#v", template.Tag, template)
		}
		if template.Initialize != wantInitialized[template.Tag] {
			t.Fatalf("template %q initialize = %t, want %t", template.Tag, template.Initialize, wantInitialized[template.Tag])
		}
		for _, rule := range template.Rules {
			if rule.Matcher != PathMatcherAnt || !strings.HasPrefix(rule.Pattern, "/") {
				t.Fatalf("template %q contains invalid system rule %#v", template.Tag, rule)
			}
		}
		if template.Tag == "openai-full" {
			copy := template
			openAIFull = &copy
		}
	}
	if openAIFull == nil || len(openAIFull.Includes) != 7 || len(openAIFull.Rules) != 16 {
		t.Fatalf("openai-full was not expanded correctly: %#v", openAIFull)
	}
	patterns := make(map[string]struct{}, len(openAIFull.Rules))
	for _, rule := range openAIFull.Rules {
		patterns[rule.Pattern] = struct{}{}
	}
	for _, required := range []string{"/v1/responses/**", "/v1/images/**", "/v1/audio/**", "/v1/videos/**", "/v1/vector_stores/**"} {
		if _, ok := patterns[required]; !ok {
			t.Errorf("openai-full is missing %q", required)
		}
	}
}

func TestBuildSystemModelPathTemplatesRejectsInvalidMetadataAndIncludes(t *testing.T) {
	tests := []string{
		`{"schema_version":2,"templates":[{"tag":"a"}]}`,
		`{"schema_version":1,"templates":[{"tag":"a","display_name":"A","description":"A","provider":"p","category":"core","includes":["missing"]}]}`,
		`{"schema_version":1,"templates":[{"tag":"a","display_name":"A","description":"A","provider":"p","category":"core","includes":["b"]},{"tag":"b","display_name":"B","description":"B","provider":"p","category":"core","includes":["a"]}]}`,
	}
	for _, input := range tests {
		var catalog systemAIModelCatalog
		if err := json.Unmarshal([]byte(input), &catalog); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if _, err := buildSystemModelPathTemplates(catalog); err == nil {
			t.Fatalf("buildSystemModelPathTemplates(%s) succeeded, want error", input)
		}
	}
}

func TestSystemModelPathTemplatesCoverDocumentedSub2APIRoutes(t *testing.T) {
	templates, err := SystemModelPathTemplates()
	if err != nil {
		t.Fatalf("SystemModelPathTemplates() error = %v", err)
	}
	documentedPaths := []string{
		"/v1/messages",
		"/v1/messages/count_tokens",
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/alpha/search",
		"/backend-api/codex/responses",
		"/backend-api/codex/alpha/search",
		"/v1/images/generations",
		"/v1/images/edits/async",
		"/v1/images/tasks/task-123",
		"/v1/images/batches/models",
		"/v1/images/batches/batch-123/items/item-123/content",
		"/v1/images/batches/batch-123/download",
		"/v1/videos/generations",
		"/v1/videos/request-123/content",
		"/v1/tts",
		"/v1/stt",
		"/v1/realtime",
		"/v1/custom-voices/voice-123/audio",
		"/v1/web_search",
		"/v1/x_search",
		"/v1/models",
		"/v1beta/models",
		"/v1beta/models/gemini-2.5-pro:generateContent",
		"/v1/usage",
		"/v1/sub2api/billing",
		"/v1/live",
		"/v1/live/call-123",
		"/backend-api/codex/realtime/calls",
		"/backend-api/codex/call-123",
		"/messages",
		"/responses",
		"/chat/completions",
	}
	for _, requestPath := range documentedPaths {
		matched := false
		for _, template := range templates {
			for _, rule := range template.Rules {
				if rule.matches(requestPath) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			t.Errorf("documented Sub2API path %q is not covered by any system template", requestPath)
		}
	}
}

func TestNormalizeLoggingPathFilterAndMatch(t *testing.T) {
	filter, err := NormalizeLoggingPathFilter(&LoggingPathFilterConfig{
		Mode: " ALLOWLIST ",
		Rules: []LoggingPathRule{
			{Matcher: "ant", Pattern: "v1/**"},
			{Matcher: "ant", Pattern: "/v1/**"},
			{Matcher: "regex", Pattern: `/custom/[0-9]+`},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeLoggingPathFilter() error = %v", err)
	}
	if len(filter.Rules) != 2 {
		t.Fatalf("deduplicated rules = %d, want 2", len(filter.Rules))
	}
	for _, requestPath := range []string{"/v1", "/v1/responses", "/v1/a/b", "/custom/42"} {
		if !filter.Allows(requestPath) {
			t.Errorf("Allows(%q) = false, want true", requestPath)
		}
	}
	for _, requestPath := range []string{"/v10/responses", "/custom/42/extra", "/assets/app.js"} {
		if filter.Allows(requestPath) {
			t.Errorf("Allows(%q) = true, want false", requestPath)
		}
	}
}

func TestDenylistInvertsRuleMatch(t *testing.T) {
	filter, err := NormalizeLoggingPathFilter(&LoggingPathFilterConfig{
		Mode:  LoggingModeDenylist,
		Rules: []LoggingPathRule{{Matcher: PathMatcherAnt, Pattern: "/assets/**"}},
	})
	if err != nil {
		t.Fatalf("NormalizeLoggingPathFilter() error = %v", err)
	}
	if filter.Allows("/assets/app.js") {
		t.Fatal("denylist allowed a matching path")
	}
	if !filter.Allows("/v1/responses") {
		t.Fatal("denylist rejected a non-matching path")
	}
}

func TestNormalizeLoggingPathFilterRejectsInvalidRules(t *testing.T) {
	tests := []LoggingPathFilterConfig{
		{Mode: LoggingModeAllowlist},
		{Mode: "unknown", Rules: []LoggingPathRule{{Pattern: "/v1"}}},
		{Mode: LoggingModeAllowlist, Rules: []LoggingPathRule{{Matcher: PathMatcherRegex, Pattern: "("}}},
		{Mode: LoggingModeAllowlist, Rules: []LoggingPathRule{{Matcher: PathMatcherAnt, Pattern: "/v1/**suffix"}}},
	}
	for _, test := range tests {
		if _, err := NormalizeLoggingPathFilter(&test); err == nil {
			t.Fatalf("NormalizeLoggingPathFilter(%#v) succeeded, want error", test)
		}
	}
}

func TestEnsureModelPathTemplatesInitializedOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &Config{configPath: path, Upstreams: map[string]UpstreamConfig{}}
	if err := cfg.EnsureModelPathTemplatesInitialized(); err != nil {
		t.Fatalf("EnsureModelPathTemplatesInitialized() error = %v", err)
	}
	if !cfg.LogRules.ModelPathTemplatesInitialized || len(cfg.LogRules.ModelPathTemplates) == 0 {
		t.Fatalf("templates were not initialized: %#v", cfg.LogRules)
	}
	initializedTags := make([]string, 0, len(cfg.LogRules.ModelPathTemplates))
	for _, template := range cfg.LogRules.ModelPathTemplates {
		initializedTags = append(initializedTags, template.Tag)
	}
	if strings.Join(initializedTags, ",") != "openai-core,claude-core,gemini-core" {
		t.Fatalf("initialized templates = %v", initializedTags)
	}

	cfg.LogRules.ModelPathTemplates = nil
	if err := cfg.EnsureModelPathTemplatesInitialized(); err != nil {
		t.Fatalf("second EnsureModelPathTemplatesInitialized() error = %v", err)
	}
	if cfg.LogRules.ModelPathTemplates != nil {
		t.Fatal("initialized empty templates were regenerated")
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(saved), "model_path_templates_initialized: true") {
		t.Fatalf("initialization marker was not persisted:\n%s", saved)
	}
}

func TestEnsureModelPathTemplatesInitializedPreservesExistingTemplates(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		configPath: filepath.Join(dir, "config.yaml"),
		Upstreams:  map[string]UpstreamConfig{},
		LogRules: LoggingRulesConfig{ModelPathTemplates: []ModelPathTemplate{{
			Tag:   "openai2",
			Rules: []LoggingPathRule{{Matcher: PathMatcherAnt, Pattern: "/custom"}},
		}}},
	}
	if err := cfg.EnsureModelPathTemplatesInitialized(); err != nil {
		t.Fatalf("EnsureModelPathTemplatesInitialized() error = %v", err)
	}
	if len(cfg.LogRules.ModelPathTemplates) != 1 || cfg.LogRules.ModelPathTemplates[0].Tag != "openai2" {
		t.Fatalf("existing templates were replaced: %#v", cfg.LogRules.ModelPathTemplates)
	}
}

func TestEnsureModelPathTemplatesInitializedRollsBackWhenSaveFails(t *testing.T) {
	cfg := &Config{
		configPath: filepath.Join(t.TempDir(), "missing", "config.yaml"),
		Upstreams:  map[string]UpstreamConfig{},
	}
	if err := cfg.EnsureModelPathTemplatesInitialized(); err == nil {
		t.Fatal("EnsureModelPathTemplatesInitialized() succeeded, want save error")
	}
	if cfg.LogRules.ModelPathTemplatesInitialized || len(cfg.LogRules.ModelPathTemplates) != 0 {
		t.Fatalf("failed initialization was not rolled back: %#v", cfg.LogRules)
	}
}
