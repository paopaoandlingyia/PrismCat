package usage

import (
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestExtractOpenAIJSONUsage(t *testing.T) {
	cfg := testConfig()
	body := []byte(`{"id":"x","usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}}`)

	got := Extract(cfg, "openai", "application/json", "", body)

	if got.InputTokens == nil || *got.InputTokens != 12 {
		t.Fatalf("InputTokens = %v, want 12", got.InputTokens)
	}
	if got.OutputTokens == nil || *got.OutputTokens != 34 {
		t.Fatalf("OutputTokens = %v, want 34", got.OutputTokens)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 46 {
		t.Fatalf("TotalTokens = %v, want 46", got.TotalTokens)
	}
	if got.Raw == "" || got.Source != "OpenAI compatible:/usage" {
		t.Fatalf("Raw = %q Source = %q, want usage raw/source", got.Raw, got.Source)
	}
}

func TestExtractSSEUsesLastUsage(t *testing.T) {
	cfg := testConfig()
	body := []byte("data: {\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"total_tokens\":30}}\n\n" +
		"data: [DONE]\n\n")

	got := Extract(cfg, "openai", "text/event-stream", "", body)

	if got.TotalTokens == nil || *got.TotalTokens != 30 {
		t.Fatalf("TotalTokens = %v, want 30", got.TotalTokens)
	}
}

func TestExtractRequiresUpstreamBinding(t *testing.T) {
	cfg := testConfig()
	got := Extract(cfg, "gemini", "application/json", "", []byte(`{"usage":{"total_tokens":1}}`))
	if got.Source != "" {
		t.Fatalf("Source = %q, want empty without binding", got.Source)
	}
}

func testConfig() config.UsageExtractionConfig {
	return config.UsageExtractionConfig{
		Enabled: true,
		Upstreams: map[string]config.UsageExtractionUpstreamBinding{
			"openai": {
				Enabled:   true,
				RuleNames: []string{"OpenAI compatible"},
			},
		},
		Rules: []config.UsageExtractionRule{
			{
				Name:    "OpenAI compatible",
				Enabled: true,
				Match: config.UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: config.UsageExtractionPaths{
					InputTokens:  []string{"/usage/prompt_tokens"},
					OutputTokens: []string{"/usage/completion_tokens"},
					TotalTokens:  []string{"/usage/total_tokens"},
					RawUsage:     []string{"/usage"},
				},
			},
		},
	}
}
