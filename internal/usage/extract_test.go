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

func TestExtractClaudeSSEMergesSplitUsage(t *testing.T) {
	cfg := claudeTestConfig()
	body := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":15}}\n\n")

	got := Extract(cfg, "anthropic", "text/event-stream", "", body)

	if got.InputTokens == nil || *got.InputTokens != 25 {
		t.Fatalf("InputTokens = %v, want 25", got.InputTokens)
	}
	if got.OutputTokens == nil || *got.OutputTokens != 15 {
		t.Fatalf("OutputTokens = %v, want 15", got.OutputTokens)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 40 {
		t.Fatalf("TotalTokens = %v, want 40", got.TotalTokens)
	}
}

func TestExtractOpenAIResponsesJSONUsage(t *testing.T) {
	cfg := responsesTestConfig()
	body := []byte(`{"id":"resp_123","usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}}`)

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
	if got.Raw == "" || got.Source != "OpenAI Responses:/usage" {
		t.Fatalf("Raw = %q Source = %q, want responses usage raw/source", got.Raw, got.Source)
	}
}

func TestExtractOpenAIResponsesSSECompletedUsage(t *testing.T) {
	cfg := responsesTestConfig()
	body := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"usage\":{\"input_tokens\":12,\"output_tokens\":34,\"total_tokens\":46}}}\n\n")

	got := Extract(cfg, "openai", "text/event-stream", "", body)

	if got.InputTokens == nil || *got.InputTokens != 12 {
		t.Fatalf("InputTokens = %v, want 12", got.InputTokens)
	}
	if got.OutputTokens == nil || *got.OutputTokens != 34 {
		t.Fatalf("OutputTokens = %v, want 34", got.OutputTokens)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 46 {
		t.Fatalf("TotalTokens = %v, want 46", got.TotalTokens)
	}
	if got.Raw == "" || got.Source != "OpenAI Responses:/response/usage" {
		t.Fatalf("Raw = %q Source = %q, want nested responses usage raw/source", got.Raw, got.Source)
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

func responsesTestConfig() config.UsageExtractionConfig {
	return config.UsageExtractionConfig{
		Enabled: true,
		Upstreams: map[string]config.UsageExtractionUpstreamBinding{
			"openai": {
				Enabled:   true,
				RuleNames: []string{"OpenAI Responses"},
			},
		},
		Rules: []config.UsageExtractionRule{
			{
				Name:    "OpenAI Responses",
				Enabled: true,
				Match: config.UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: config.UsageExtractionPaths{
					InputTokens:  []string{"/usage/input_tokens", "/response/usage/input_tokens"},
					OutputTokens: []string{"/usage/output_tokens", "/response/usage/output_tokens"},
					TotalTokens:  []string{"/usage/total_tokens", "/response/usage/total_tokens"},
					RawUsage:     []string{"/usage", "/response/usage"},
				},
			},
		},
	}
}

func claudeTestConfig() config.UsageExtractionConfig {
	return config.UsageExtractionConfig{
		Enabled: true,
		Upstreams: map[string]config.UsageExtractionUpstreamBinding{
			"anthropic": {
				Enabled:   true,
				RuleNames: []string{"Anthropic"},
			},
		},
		Rules: []config.UsageExtractionRule{
			{
				Name:    "Anthropic",
				Enabled: true,
				Match: config.UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: config.UsageExtractionPaths{
					InputTokens:  []string{"/usage/input_tokens", "/message/usage/input_tokens"},
					OutputTokens: []string{"/usage/output_tokens", "/message/usage/output_tokens"},
					RawUsage:     []string{"/usage", "/message/usage"},
				},
			},
		},
	}
}
