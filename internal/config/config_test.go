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
