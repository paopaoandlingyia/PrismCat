package config

import "testing"

func TestExtractSubdomain(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		proxyDomains []string
		want         string
	}{
		{
			name:         "localhost_with_port",
			host:         "openai.localhost:8080",
			proxyDomains: []string{"localhost"},
			want:         "openai",
		},
		{
			name:         "case_insensitive",
			host:         "OpenAI.LocalHost",
			proxyDomains: []string{"LOCALHOST"},
			want:         "openai",
		},
		{
			name:         "custom_domain",
			host:         "gemini.prismcat.example.com",
			proxyDomains: []string{"prismcat.example.com"},
			want:         "gemini",
		},
		{
			name:         "multi_label_rejected",
			host:         "a.b.example.com",
			proxyDomains: []string{"example.com"},
			want:         "",
		},
		{
			name:         "no_subdomain",
			host:         "example.com",
			proxyDomains: []string{"example.com"},
			want:         "",
		},
		{
			name:         "nil_domains_default_localhost",
			host:         "openai.localhost",
			proxyDomains: nil,
			want:         "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractSubdomain(tt.host, tt.proxyDomains); got != tt.want {
				t.Fatalf("ExtractSubdomain(%q, %v) = %q, want %q", tt.host, tt.proxyDomains, got, tt.want)
			}
		})
	}
}

func TestNormalizePathRoutingPrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "/_proxy"},
		{in: "/_proxy", want: "/_proxy"},
		{in: "_proxy", want: "/_proxy"},
		{in: "/proxy/", want: "/proxy"},
		{in: "  /proxy/v2/  ", want: "/proxy/v2"},
		{in: "/", want: "/_proxy"},
	}

	for _, tt := range tests {
		if got := NormalizePathRoutingPrefix(tt.in); got != tt.want {
			t.Fatalf("NormalizePathRoutingPrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeRequestOverridesNormalizesBindingsAndRules(t *testing.T) {
	cfg := NormalizeRequestOverrides(RequestOverridesConfig{
		Enabled: true,
		Upstreams: map[string]RequestOverrideUpstreamBinding{
			" Anthropic ": {
				Enabled:   true,
				RuleNames: []string{" add metadata ", "add metadata", ""},
			},
		},
		Rules: []RequestOverrideRule{
			{
				Name: " add metadata ",
				Match: RequestOverrideMatch{
					Methods:      []string{"post"},
					PathPrefixes: []string{"v1/messages"},
				},
			},
		},
	})

	if cfg.MaxBodyBytes != 1<<20 {
		t.Fatalf("MaxBodyBytes = %d, want default", cfg.MaxBodyBytes)
	}
	binding := cfg.Upstreams["anthropic"]
	if !binding.Enabled {
		t.Fatal("anthropic binding is not enabled")
	}
	if len(binding.RuleNames) != 1 || binding.RuleNames[0] != "add metadata" {
		t.Fatalf("rule names = %#v", binding.RuleNames)
	}
	if len(cfg.Rules[0].Match.Methods) != 1 || cfg.Rules[0].Match.Methods[0] != "POST" {
		t.Fatalf("methods = %#v", cfg.Rules[0].Match.Methods)
	}
	if len(cfg.Rules[0].Match.PathPrefixes) != 1 || cfg.Rules[0].Match.PathPrefixes[0] != "/v1/messages" {
		t.Fatalf("path prefixes = %#v", cfg.Rules[0].Match.PathPrefixes)
	}
	if cfg.Rules[0].Name != "add metadata" {
		t.Fatalf("rule name = %q", cfg.Rules[0].Name)
	}
}

func TestExtractPathUpstream(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		wantName string
		wantPath string
		wantOK   bool
	}{
		{
			name:     "default_prefix_with_rest_path",
			path:     "/_proxy/openai/v1/chat/completions",
			prefix:   "/_proxy",
			wantName: "openai",
			wantPath: "/v1/chat/completions",
			wantOK:   true,
		},
		{
			name:     "default_prefix_root_forward",
			path:     "/_proxy/openai",
			prefix:   "/_proxy",
			wantName: "openai",
			wantPath: "/",
			wantOK:   true,
		},
		{
			name:     "custom_prefix_without_leading_slash",
			path:     "/proxy/Claude/v1/messages",
			prefix:   "proxy",
			wantName: "claude",
			wantPath: "/v1/messages",
			wantOK:   true,
		},
		{
			name:   "prefix_boundary_required",
			path:   "/_proxyx/openai/v1",
			prefix: "/_proxy",
			wantOK: false,
		},
		{
			name:   "missing_upstream",
			path:   "/_proxy/",
			prefix: "/_proxy",
			wantOK: false,
		},
		{
			name:   "multi_label_upstream_rejected",
			path:   "/_proxy/a.b/v1",
			prefix: "/_proxy",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotPath, gotOK := ExtractPathUpstream(tt.path, tt.prefix)
			if gotName != tt.wantName || gotPath != tt.wantPath || gotOK != tt.wantOK {
				t.Fatalf("ExtractPathUpstream(%q, %q) = (%q, %q, %v), want (%q, %q, %v)", tt.path, tt.prefix, gotName, gotPath, gotOK, tt.wantName, tt.wantPath, tt.wantOK)
			}
		})
	}
}
