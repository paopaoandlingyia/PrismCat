package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestIsUIHostIncludesProxyDomainBase(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			UIHosts:      []string{"localhost"},
			ProxyDomains: []string{"prismcat.example.com"},
		},
	}

	if !cfg.IsUIHost("prismcat.example.com") {
		t.Fatal("proxy domain base should be treated as UI host")
	}
	if !cfg.IsUIHost("prismcat.example.com:8080") {
		t.Fatal("proxy domain base with port should be treated as UI host")
	}
	if cfg.IsUIHost("openai.prismcat.example.com") {
		t.Fatal("upstream subdomain should not be treated as UI host")
	}
}

func TestSaveDoesNotPersistEnvUIPassword(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "data", "prismcat.db")
	blobDir := filepath.Join(dir, "data", "blobs")
	content := "server:\n  ui_password: file-secret\nstorage:\n  database: " + strconvQuote(dbPath) + "\n  blob_dir: " + strconvQuote(blobDir) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("PRISMCAT_UI_PASSWORD", "env-secret")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := cfg.AuthSnapshot().UIPassword; got != "env-secret" {
		t.Fatalf("runtime password = %q, want env-secret", got)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(saved), "env-secret") {
		t.Fatalf("saved config leaked env password:\n%s", saved)
	}
	if !strings.Contains(string(saved), "file-secret") {
		t.Fatalf("saved config did not preserve file password:\n%s", saved)
	}
}

func TestFileUIPasswordIsRuntimePasswordWhenNoEnvOverride(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "data", "prismcat.db")
	blobDir := filepath.Join(dir, "data", "blobs")
	content := "server:\n  ui_password: file-secret\n  ui_password_hash: generated-hash\nstorage:\n  database: " + strconvQuote(dbPath) + "\n  blob_dir: " + strconvQuote(blobDir) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := cfg.AuthSnapshot().UIPassword; got != "file-secret" {
		t.Fatalf("runtime password = %q, want file-secret", got)
	}
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
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

func TestNormalizeOutboundProxy(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty_defaults_to_env", in: "", want: "env"},
		{name: "env_case_insensitive", in: " ENV ", want: "env"},
		{name: "direct_case_insensitive", in: " Direct ", want: "direct"},
		{name: "http_url", in: "http://127.0.0.1:7890", want: "http://127.0.0.1:7890"},
		{name: "socks5_url", in: "socks5://127.0.0.1:7891", want: "socks5://127.0.0.1:7891"},
		{name: "missing_scheme", in: "127.0.0.1:7890", wantErr: true},
		{name: "unsupported_scheme", in: "ftp://127.0.0.1:7890", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeOutboundProxy(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeOutboundProxy(%q) succeeded, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeOutboundProxy(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeOutboundProxy(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeUpstreamsDefaultsTimeout(t *testing.T) {
	got, err := normalizeUpstreams(map[string]UpstreamConfig{
		"openai": {Target: "https://api.openai.com"},
	})
	if err != nil {
		t.Fatalf("normalizeUpstreams returned error: %v", err)
	}
	if got["openai"].Timeout != DefaultUpstreamTimeoutSeconds {
		t.Fatalf("Timeout = %d, want %d", got["openai"].Timeout, DefaultUpstreamTimeoutSeconds)
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

func TestRequestOverridesSnapshotDeepCopiesNestedValues(t *testing.T) {
	exists := true
	cfg := &Config{
		Overrides: RequestOverridesConfig{
			MaxBodyBytes: 1024,
			Upstreams: map[string]RequestOverrideUpstreamBinding{
				"openai": {
					Enabled:   true,
					RuleNames: []string{"rule-a"},
				},
			},
			Rules: []RequestOverrideRule{
				{
					Name:    "rule-a",
					Enabled: true,
					Match: RequestOverrideMatch{
						Methods: []string{"POST"},
						JSON: []RequestOverrideJSONCondition{
							{
								Path:   "/metadata",
								Exists: &exists,
								Equals: map[string]interface{}{
									"nested": []interface{}{"original"},
								},
								In: []interface{}{
									map[string]interface{}{"candidate": "original"},
								},
							},
						},
					},
					Patch: []RequestOverridePatch{
						{
							Op:   "add",
							Path: "/metadata",
							Value: map[string]interface{}{
								"nested": []interface{}{"original"},
							},
						},
					},
				},
			},
		},
	}

	snapshot := cfg.RequestOverridesSnapshot()
	binding := snapshot.Upstreams["openai"]
	binding.RuleNames[0] = "changed"
	snapshot.Upstreams["openai"] = binding
	*snapshot.Rules[0].Match.JSON[0].Exists = false
	snapshot.Rules[0].Match.JSON[0].Equals.(map[string]interface{})["nested"].([]interface{})[0] = "changed"
	snapshot.Rules[0].Match.JSON[0].In[0].(map[string]interface{})["candidate"] = "changed"
	snapshot.Rules[0].Patch[0].Value.(map[string]interface{})["nested"].([]interface{})[0] = "changed"

	if got := cfg.Overrides.Upstreams["openai"].RuleNames[0]; got != "rule-a" {
		t.Fatalf("original rule name = %q, want rule-a", got)
	}
	if got := *cfg.Overrides.Rules[0].Match.JSON[0].Exists; !got {
		t.Fatal("original exists pointer was mutated")
	}
	if got := cfg.Overrides.Rules[0].Match.JSON[0].Equals.(map[string]interface{})["nested"].([]interface{})[0]; got != "original" {
		t.Fatalf("original equals nested value = %v, want original", got)
	}
	if got := cfg.Overrides.Rules[0].Match.JSON[0].In[0].(map[string]interface{})["candidate"]; got != "original" {
		t.Fatalf("original in nested value = %v, want original", got)
	}
	if got := cfg.Overrides.Rules[0].Patch[0].Value.(map[string]interface{})["nested"].([]interface{})[0]; got != "original" {
		t.Fatalf("original patch value = %v, want original", got)
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
