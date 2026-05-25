package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var Version = "1.8.0"

const DefaultUpstreamTimeoutSeconds = 120

// Config 应用配置
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Upstreams map[string]UpstreamConfig `yaml:"upstreams"`
	Logging   LoggingConfig             `yaml:"logging"`
	Storage   StorageConfig             `yaml:"storage"`
	Overrides RequestOverridesConfig    `yaml:"request_overrides"`
	Usage     UsageExtractionConfig     `yaml:"usage_extraction"`

	configPath     string // 配置文件路径
	fileUIPassword string
	envUIPassword  bool
	mu             sync.RWMutex
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Addr           string   `yaml:"addr"`
	Port           int      `yaml:"port"`
	UIHosts        []string `yaml:"ui_hosts"`
	UIPassword     string   `yaml:"ui_password"`
	UIPasswordHash string   `yaml:"ui_password_hash"`

	// ProxyDomains defines the base domains used for host-based upstream routing.
	// For example, if ProxyDomains contains "localhost", then requests to
	// "openai.localhost" will be routed to upstream "openai".
	//
	// Cloud deployments typically set this to something like "prismcat.example.com"
	// so that "openai.prismcat.example.com" routes to upstream "openai".
	ProxyDomains []string `yaml:"proxy_domains"`

	// EnablePathRouting allows routing requests through a reserved path prefix
	// on the UI host, e.g. /_proxy/openai/v1/chat/completions.
	EnablePathRouting bool `yaml:"enable_path_routing"`
	// PathRoutingPrefix controls the reserved path prefix used when
	// EnablePathRouting is on.
	PathRoutingPrefix string `yaml:"path_routing_prefix"`

	// ShutdownTimeoutSeconds controls graceful shutdown time budget.
	ShutdownTimeoutSeconds int `yaml:"shutdown_timeout_seconds"`

	// CORS settings (primarily for local/dev UI usage).
	// Use cors_allow_origins: ["*"] to keep current permissive behaviour.
	CORSAllowOrigins []string `yaml:"cors_allow_origins"`
	CORSAllowMethods []string `yaml:"cors_allow_methods"`
	CORSAllowHeaders []string `yaml:"cors_allow_headers"`
}

// UpstreamConfig 上游配置
type UpstreamConfig struct {
	Target        string `yaml:"target"`
	Timeout       int    `yaml:"timeout"` // 秒
	Order         int    `yaml:"order,omitempty"`
	OutboundProxy string `yaml:"outbound_proxy,omitempty"`
}

type RequestOverridesConfig struct {
	Enabled      bool                                      `yaml:"enabled" json:"enabled"`
	MaxBodyBytes int64                                     `yaml:"max_body_bytes" json:"max_body_bytes"`
	Upstreams    map[string]RequestOverrideUpstreamBinding `yaml:"upstreams" json:"upstreams"`
	Rules        []RequestOverrideRule                     `yaml:"rules" json:"rules"`
}

type RequestOverrideUpstreamBinding struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	RuleNames []string `yaml:"rule_names" json:"rule_names"`
}

type RequestOverrideRule struct {
	Name    string                  `yaml:"name" json:"name"`
	Enabled bool                    `yaml:"enabled" json:"enabled"`
	Match   RequestOverrideMatch    `yaml:"match" json:"match"`
	Patch   []RequestOverridePatch  `yaml:"patch" json:"patch"`
	Headers []RequestOverrideHeader `yaml:"headers,omitempty" json:"headers,omitempty"`
}

type RequestOverrideHeader struct {
	Op    string `yaml:"op" json:"op"`
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

type RequestOverrideMatch struct {
	Methods      []string                       `yaml:"methods" json:"methods"`
	PathPrefixes []string                       `yaml:"path_prefixes" json:"path_prefixes"`
	Paths        []string                       `yaml:"paths" json:"paths"`
	JSON         []RequestOverrideJSONCondition `yaml:"json" json:"json"`
}

type RequestOverrideJSONCondition struct {
	Path       string        `yaml:"path" json:"path"`
	Exists     *bool         `yaml:"exists,omitempty" json:"exists,omitempty"`
	Equals     interface{}   `yaml:"equals,omitempty" json:"equals,omitempty"`
	StartsWith string        `yaml:"starts_with,omitempty" json:"starts_with,omitempty"`
	In         []interface{} `yaml:"in,omitempty" json:"in,omitempty"`
}

type RequestOverridePatch struct {
	Op    string      `yaml:"op" json:"op"`
	Path  string      `yaml:"path" json:"path"`
	Value interface{} `yaml:"value,omitempty" json:"value,omitempty"`
}

type UsageExtractionConfig struct {
	Enabled   bool                                      `yaml:"enabled" json:"enabled"`
	Upstreams map[string]UsageExtractionUpstreamBinding `yaml:"upstreams" json:"upstreams"`
	Rules     []UsageExtractionRule                     `yaml:"rules" json:"rules"`
}

type UsageExtractionUpstreamBinding struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	RuleNames []string `yaml:"rule_names" json:"rule_names"`
}

type UsageExtractionRule struct {
	Name    string               `yaml:"name" json:"name"`
	Enabled bool                 `yaml:"enabled" json:"enabled"`
	Match   UsageExtractionMatch `yaml:"match" json:"match"`
	Paths   UsageExtractionPaths `yaml:"paths" json:"paths"`
}

type UsageExtractionMatch struct {
	ContentTypes []string `yaml:"content_types" json:"content_types"`
}

type UsageExtractionPaths struct {
	InputTokens  []string `yaml:"input_tokens" json:"input_tokens"`
	OutputTokens []string `yaml:"output_tokens" json:"output_tokens"`
	TotalTokens  []string `yaml:"total_tokens" json:"total_tokens"`
	RawUsage     []string `yaml:"raw_usage,omitempty" json:"raw_usage,omitempty"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	MaxRequestBody   int64    `yaml:"max_request_body"`
	MaxResponseBody  int64    `yaml:"max_response_body"`
	SensitiveHeaders []string `yaml:"sensitive_headers"`
	StoreBase64      bool     `yaml:"store_base64"`

	// EarlyRequestBodySnapshot controls whether PrismCat persists an additional
	// log snapshot right after the request body has been fully sent to the
	// upstream (i.e. before the upstream responds).
	//
	// Live log detail updates do not depend on this setting; this is only useful
	// when you want the in-flight request body to survive process restarts or UI
	// reloads before the final response is saved. It adds an extra DB write per
	// request.
	EarlyRequestBodySnapshot bool `yaml:"early_request_body_snapshot"`

	// DetachBodyOverBytes detaches large captured bodies into the blob store.
	// Log details keep only an inline preview + a content-addressed reference.
	//
	// 0: disable detaching. Omit the field to use the default.
	DetachBodyOverBytes int64 `yaml:"detach_body_over_bytes"`
	// BodyPreviewBytes controls how many bytes of readable body preview are kept
	// inline in request_logs.request_body/response_body while the full body is
	// loaded on demand from the blob store when detached.
	// 0: disable preview (store empty preview).
	BodyPreviewBytes int64 `yaml:"body_preview_bytes"`
}

// StorageConfig 存储配置
type StorageConfig struct {
	Database      string `yaml:"database"`
	RetentionDays int    `yaml:"retention_days"`

	// BlobStore defines where detached bodies are stored.
	// Supported values: "fs" (filesystem). (Others can be added later, e.g. "sqlite", "s3".)
	BlobStore string `yaml:"blob_store"`
	// BlobDir is used when BlobStore == "fs".
	// BlobDir is used when BlobStore == "fs".
	BlobDir string `yaml:"blob_dir"`
	// AsyncBuffer controls the capacity of the async log queue.
	AsyncBuffer int `yaml:"async_buffer"`
}

var (
	cfg  *Config
	once sync.Once
)

// Load 加载配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	c := Config{
		Server: ServerConfig{
			Port:                   8080,
			UIHosts:                []string{"localhost", "127.0.0.1"},
			ProxyDomains:           []string{"localhost"},
			PathRoutingPrefix:      "/_proxy",
			ShutdownTimeoutSeconds: 10,
			CORSAllowOrigins:       []string{"*"},
			CORSAllowMethods:       []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			CORSAllowHeaders:       []string{"Content-Type", "Authorization"},
		},
		Logging: LoggingConfig{
			MaxRequestBody:           5 << 20,  // 5MB
			MaxResponseBody:          32 << 20, // 32MB
			SensitiveHeaders:         []string{"Authorization", "x-api-key", "api-key"},
			StoreBase64:              true,
			EarlyRequestBodySnapshot: false,
			DetachBodyOverBytes:      2 << 20,    // 2MB
			BodyPreviewBytes:         512 * 1024, // 512KB
		},
		Storage: StorageConfig{
			Database:    "./data/prismcat.db",
			BlobStore:   "fs",
			BlobDir:     "./data/blobs",
			AsyncBuffer: 4096,
		},
		Overrides: RequestOverridesConfig{
			MaxBodyBytes: 1 << 20,
		},
		Usage:     defaultUsageExtractionConfig(),
		Upstreams: make(map[string]UpstreamConfig),
	}

	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	c.configPath = path
	c.fileUIPassword = c.Server.UIPassword

	// 覆盖环境变量 (云端/容器化部署优先)
	if envAddr := os.Getenv("PRISMCAT_ADDR"); envAddr != "" {
		c.Server.Addr = envAddr
	}
	if envPort := os.Getenv("PRISMCAT_PORT"); envPort != "" {
		if p, err := parsePort(envPort); err == nil {
			c.Server.Port = p
		}
	}
	if envUIHosts := os.Getenv("PRISMCAT_UI_HOSTS"); envUIHosts != "" {
		c.Server.UIHosts = splitCSV(envUIHosts)
	}
	if envProxyDomains := os.Getenv("PRISMCAT_PROXY_DOMAINS"); envProxyDomains != "" {
		c.Server.ProxyDomains = splitCSV(envProxyDomains)
	}
	if envEnablePathRouting := os.Getenv("PRISMCAT_ENABLE_PATH_ROUTING"); envEnablePathRouting != "" {
		if enabled, err := strconv.ParseBool(envEnablePathRouting); err == nil {
			c.Server.EnablePathRouting = enabled
		}
	}
	if envPathRoutingPrefix := os.Getenv("PRISMCAT_PATH_ROUTING_PREFIX"); envPathRoutingPrefix != "" {
		c.Server.PathRoutingPrefix = envPathRoutingPrefix
	}
	if envDB := os.Getenv("PRISMCAT_DB_PATH"); envDB != "" {
		c.Storage.Database = envDB
	}
	if envBlobDir := os.Getenv("PRISMCAT_BLOB_DIR"); envBlobDir != "" {
		c.Storage.BlobDir = envBlobDir
	}
	if envRetention := os.Getenv("PRISMCAT_RETENTION_DAYS"); envRetention != "" {
		if d, err := parsePort(envRetention); err == nil { // reuse parsePort for int
			c.Storage.RetentionDays = d
		}
	}
	if envAsyncBuffer := os.Getenv("PRISMCAT_ASYNC_BUFFER"); envAsyncBuffer != "" {
		if b, err := parsePort(envAsyncBuffer); err == nil {
			c.Storage.AsyncBuffer = b
		}
	}
	if envPassword := os.Getenv("PRISMCAT_UI_PASSWORD"); envPassword != "" {
		c.envUIPassword = true
		c.Server.UIPassword = envPassword
	}
	if envOverridesEnabled := os.Getenv("PRISMCAT_REQUEST_OVERRIDES_ENABLED"); envOverridesEnabled != "" {
		if enabled, err := strconv.ParseBool(envOverridesEnabled); err == nil {
			c.Overrides.Enabled = enabled
		}
	}

	c.Server = normalizeServerConfig(c.Server)

	normalizedUpstreams, err := normalizeUpstreams(c.Upstreams)
	if err != nil {
		return nil, err
	}
	c.Upstreams = normalizedUpstreams
	c.Overrides = NormalizeRequestOverrides(c.Overrides)
	c.Usage = NormalizeUsageExtraction(c.Usage)

	// 确保目录存在
	dbDir := filepath.Dir(c.Storage.Database)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}
	if c.Storage.BlobStore == "fs" {
		if err := os.MkdirAll(c.Storage.BlobDir, 0755); err != nil {
			return nil, fmt.Errorf("创建 blob 目录失败: %w", err)
		}
	}

	cfg = &c
	return &c, nil
}

func parsePort(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}

func normalizeLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeLowerList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := normalizeLower(v)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizeServerConfig(in ServerConfig) ServerConfig {
	in.UIHosts = normalizeLowerList(in.UIHosts)
	in.ProxyDomains = normalizeLowerList(in.ProxyDomains)
	in.PathRoutingPrefix = NormalizePathRoutingPrefix(in.PathRoutingPrefix)
	return in
}

func NormalizePathRoutingPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/_proxy"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" || prefix == "/" {
		return "/_proxy"
	}
	return prefix
}

func normalizeUpstreams(in map[string]UpstreamConfig) (map[string]UpstreamConfig, error) {
	if in == nil {
		return make(map[string]UpstreamConfig), nil
	}
	out := make(map[string]UpstreamConfig, len(in))
	for k, v := range in {
		n := normalizeLower(k)
		if n == "" {
			continue
		}
		if _, exists := out[n]; exists {
			return nil, fmt.Errorf("重复的 upstream 名称（大小写不敏感）: %q", n)
		}
		if strings.TrimSpace(v.OutboundProxy) != "" {
			outboundProxy, err := NormalizeOutboundProxy(v.OutboundProxy)
			if err != nil {
				return nil, fmt.Errorf("upstream %q: %w", n, err)
			}
			v.OutboundProxy = outboundProxy
		}
		if v.Timeout <= 0 {
			v.Timeout = DefaultUpstreamTimeoutSeconds
		}
		out[n] = v
	}
	return out, nil
}

func NormalizeOutboundProxy(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "env") {
		return "env", nil
	}
	if strings.EqualFold(value, "direct") {
		return "direct", nil
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid outbound_proxy %q: use env, direct, or a proxy URL", value)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return "", fmt.Errorf("unsupported outbound_proxy scheme %q", u.Scheme)
	}
	return u.String(), nil
}

func NormalizeRequestOverrides(in RequestOverridesConfig) RequestOverridesConfig {
	if in.MaxBodyBytes <= 0 {
		in.MaxBodyBytes = 1 << 20
	}
	if in.Upstreams == nil {
		in.Upstreams = make(map[string]RequestOverrideUpstreamBinding)
	} else {
		normalized := make(map[string]RequestOverrideUpstreamBinding, len(in.Upstreams))
		for name, binding := range in.Upstreams {
			n := normalizeLower(name)
			if n == "" {
				continue
			}
			binding.RuleNames = normalizeNameList(binding.RuleNames)
			normalized[n] = binding
		}
		in.Upstreams = normalized
	}
	for i := range in.Rules {
		in.Rules[i].Name = strings.TrimSpace(in.Rules[i].Name)
		in.Rules[i].Match.Methods = normalizeUpperList(in.Rules[i].Match.Methods)
		in.Rules[i].Match.PathPrefixes = normalizePathList(in.Rules[i].Match.PathPrefixes)
		in.Rules[i].Match.Paths = normalizePathList(in.Rules[i].Match.Paths)
		for j := range in.Rules[i].Patch {
			in.Rules[i].Patch[j].Op = strings.ToLower(strings.TrimSpace(in.Rules[i].Patch[j].Op))
			in.Rules[i].Patch[j].Path = strings.TrimSpace(in.Rules[i].Patch[j].Path)
		}
		for j := range in.Rules[i].Headers {
			in.Rules[i].Headers[j].Op = strings.ToLower(strings.TrimSpace(in.Rules[i].Headers[j].Op))
			in.Rules[i].Headers[j].Name = strings.TrimSpace(in.Rules[i].Headers[j].Name)
		}
	}
	return in
}

func defaultUsageExtractionConfig() UsageExtractionConfig {
	return UsageExtractionConfig{
		Enabled:   false,
		Upstreams: map[string]UsageExtractionUpstreamBinding{},
		Rules: []UsageExtractionRule{
			{
				Name:    "OpenAI compatible",
				Enabled: true,
				Match: UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: UsageExtractionPaths{
					InputTokens:  []string{"/usage/prompt_tokens", "/usage/input_tokens"},
					OutputTokens: []string{"/usage/completion_tokens", "/usage/output_tokens"},
					TotalTokens:  []string{"/usage/total_tokens"},
					RawUsage:     []string{"/usage"},
				},
			},
			{
				Name:    "OpenAI Responses",
				Enabled: true,
				Match: UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: UsageExtractionPaths{
					InputTokens:  []string{"/usage/input_tokens", "/response/usage/input_tokens"},
					OutputTokens: []string{"/usage/output_tokens", "/response/usage/output_tokens"},
					TotalTokens:  []string{"/usage/total_tokens", "/response/usage/total_tokens"},
					RawUsage:     []string{"/usage", "/response/usage"},
				},
			},
			{
				Name:    "Anthropic",
				Enabled: true,
				Match: UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: UsageExtractionPaths{
					InputTokens:  []string{"/usage/input_tokens", "/message/usage/input_tokens"},
					OutputTokens: []string{"/usage/output_tokens", "/message/usage/output_tokens"},
					RawUsage:     []string{"/usage", "/message/usage"},
				},
			},
			{
				Name:    "Gemini",
				Enabled: true,
				Match: UsageExtractionMatch{
					ContentTypes: []string{"application/json", "text/event-stream"},
				},
				Paths: UsageExtractionPaths{
					InputTokens:  []string{"/usageMetadata/promptTokenCount"},
					OutputTokens: []string{"/usageMetadata/candidatesTokenCount"},
					TotalTokens:  []string{"/usageMetadata/totalTokenCount"},
					RawUsage:     []string{"/usageMetadata"},
				},
			},
		},
	}
}

func NormalizeUsageExtraction(in UsageExtractionConfig) UsageExtractionConfig {
	if in.Upstreams == nil {
		in.Upstreams = make(map[string]UsageExtractionUpstreamBinding)
	} else {
		normalized := make(map[string]UsageExtractionUpstreamBinding, len(in.Upstreams))
		for name, binding := range in.Upstreams {
			n := normalizeLower(name)
			if n == "" {
				continue
			}
			binding.RuleNames = normalizeNameList(binding.RuleNames)
			normalized[n] = binding
		}
		in.Upstreams = normalized
	}
	for i := range in.Rules {
		in.Rules[i].Name = strings.TrimSpace(in.Rules[i].Name)
		in.Rules[i].Match.ContentTypes = normalizeContentTypeList(in.Rules[i].Match.ContentTypes)
		in.Rules[i].Paths.InputTokens = normalizeNameList(in.Rules[i].Paths.InputTokens)
		in.Rules[i].Paths.OutputTokens = normalizeNameList(in.Rules[i].Paths.OutputTokens)
		in.Rules[i].Paths.TotalTokens = normalizeNameList(in.Rules[i].Paths.TotalTokens)
		in.Rules[i].Paths.RawUsage = normalizeNameList(in.Rules[i].Paths.RawUsage)
	}
	return in
}

func normalizeNameList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := strings.TrimSpace(v)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizeContentTypeList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := strings.ToLower(strings.TrimSpace(strings.Split(v, ";")[0]))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizeUpperList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := strings.ToUpper(strings.TrimSpace(v))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizePathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := strings.TrimSpace(v)
		if n == "" {
			continue
		}
		if !strings.HasPrefix(n, "/") {
			n = "/" + n
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// Update applies an in-memory update under an exclusive lock.
// Callers should call Save separately if persistence is required.
func (c *Config) Update(fn func(*Config)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

// LoggingSnapshot returns a copy of the current logging config safe for use
// without holding locks.
func (c *Config) LoggingSnapshot() LoggingConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := c.Logging
	if len(out.SensitiveHeaders) > 0 {
		out.SensitiveHeaders = append([]string(nil), c.Logging.SensitiveHeaders...)
	}
	return out
}

// StorageSnapshot returns a copy of the current storage config.
func (c *Config) StorageSnapshot() StorageConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Storage
}

func (c *Config) RequestOverridesSnapshot() RequestOverridesConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return NormalizeRequestOverrides(cloneRequestOverridesConfig(c.Overrides))
}

func (c *Config) UsageExtractionSnapshot() UsageExtractionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return NormalizeUsageExtraction(cloneUsageExtractionConfig(c.Usage))
}

func cloneRequestOverridesConfig(in RequestOverridesConfig) RequestOverridesConfig {
	out := in
	if len(in.Upstreams) > 0 {
		out.Upstreams = make(map[string]RequestOverrideUpstreamBinding, len(in.Upstreams))
		for name, binding := range in.Upstreams {
			next := binding
			if len(binding.RuleNames) > 0 {
				next.RuleNames = append([]string(nil), binding.RuleNames...)
			}
			out.Upstreams[name] = next
		}
	}
	if len(in.Rules) > 0 {
		out.Rules = make([]RequestOverrideRule, len(in.Rules))
		for i, rule := range in.Rules {
			out.Rules[i] = cloneRequestOverrideRule(rule)
		}
	}
	return out
}

func cloneRequestOverrideRule(in RequestOverrideRule) RequestOverrideRule {
	out := in
	if len(in.Match.Methods) > 0 {
		out.Match.Methods = append([]string(nil), in.Match.Methods...)
	}
	if len(in.Match.PathPrefixes) > 0 {
		out.Match.PathPrefixes = append([]string(nil), in.Match.PathPrefixes...)
	}
	if len(in.Match.Paths) > 0 {
		out.Match.Paths = append([]string(nil), in.Match.Paths...)
	}
	if len(in.Match.JSON) > 0 {
		out.Match.JSON = make([]RequestOverrideJSONCondition, len(in.Match.JSON))
		for i, condition := range in.Match.JSON {
			next := condition
			if condition.Exists != nil {
				exists := *condition.Exists
				next.Exists = &exists
			}
			next.Equals = cloneRequestOverrideValue(condition.Equals)
			if len(condition.In) > 0 {
				next.In = make([]interface{}, len(condition.In))
				for j, value := range condition.In {
					next.In[j] = cloneRequestOverrideValue(value)
				}
			}
			out.Match.JSON[i] = next
		}
	}
	if len(in.Patch) > 0 {
		out.Patch = make([]RequestOverridePatch, len(in.Patch))
		for i, patch := range in.Patch {
			next := patch
			next.Value = cloneRequestOverrideValue(patch.Value)
			out.Patch[i] = next
		}
	}
	if len(in.Headers) > 0 {
		out.Headers = append([]RequestOverrideHeader(nil), in.Headers...)
	}
	return out
}

func cloneRequestOverrideValue(in interface{}) interface{} {
	switch value := in.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(value))
		for k, v := range value {
			out[k] = cloneRequestOverrideValue(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, v := range value {
			out[i] = cloneRequestOverrideValue(v)
		}
		return out
	default:
		return in
	}
}

func cloneUsageExtractionConfig(in UsageExtractionConfig) UsageExtractionConfig {
	out := in
	if len(in.Upstreams) > 0 {
		out.Upstreams = make(map[string]UsageExtractionUpstreamBinding, len(in.Upstreams))
		for name, binding := range in.Upstreams {
			next := binding
			if len(binding.RuleNames) > 0 {
				next.RuleNames = append([]string(nil), binding.RuleNames...)
			}
			out.Upstreams[name] = next
		}
	}
	if len(in.Rules) > 0 {
		out.Rules = make([]UsageExtractionRule, len(in.Rules))
		for i, rule := range in.Rules {
			out.Rules[i] = cloneUsageExtractionRule(rule)
		}
	}
	return out
}

func cloneUsageExtractionRule(in UsageExtractionRule) UsageExtractionRule {
	out := in
	if len(in.Match.ContentTypes) > 0 {
		out.Match.ContentTypes = append([]string(nil), in.Match.ContentTypes...)
	}
	if len(in.Paths.InputTokens) > 0 {
		out.Paths.InputTokens = append([]string(nil), in.Paths.InputTokens...)
	}
	if len(in.Paths.OutputTokens) > 0 {
		out.Paths.OutputTokens = append([]string(nil), in.Paths.OutputTokens...)
	}
	if len(in.Paths.TotalTokens) > 0 {
		out.Paths.TotalTokens = append([]string(nil), in.Paths.TotalTokens...)
	}
	if len(in.Paths.RawUsage) > 0 {
		out.Paths.RawUsage = append([]string(nil), in.Paths.RawUsage...)
	}
	return out
}

// ServerSnapshot returns a copy of the current server config safe for use
// without holding locks.
func (c *Config) ServerSnapshot() ServerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := c.Server
	if len(out.UIHosts) > 0 {
		out.UIHosts = append([]string(nil), c.Server.UIHosts...)
	}
	if len(out.ProxyDomains) > 0 {
		out.ProxyDomains = append([]string(nil), c.Server.ProxyDomains...)
	}
	if len(out.CORSAllowOrigins) > 0 {
		out.CORSAllowOrigins = append([]string(nil), c.Server.CORSAllowOrigins...)
	}
	if len(out.CORSAllowMethods) > 0 {
		out.CORSAllowMethods = append([]string(nil), c.Server.CORSAllowMethods...)
	}
	if len(out.CORSAllowHeaders) > 0 {
		out.CORSAllowHeaders = append([]string(nil), c.Server.CORSAllowHeaders...)
	}
	return out
}

type AuthSnapshot struct {
	UIPassword     string
	UIPasswordHash string
}

func (c *Config) AuthSnapshot() AuthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return AuthSnapshot{
		UIPassword:     c.Server.UIPassword,
		UIPasswordHash: c.Server.UIPasswordHash,
	}
}

// Get 获取当前配置（需要先调用 Load）
func Get() *Config {
	return cfg
}

// Save 保存配置文件
func (c *Config) Save() error {
	// Save writes the config file; it must be exclusive to avoid concurrent writes.
	c.mu.Lock()
	defer c.mu.Unlock()

	saved := *c
	if c.envUIPassword {
		saved.Server.UIPassword = c.fileUIPassword
	}

	data, err := yaml.Marshal(saved)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(c.configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

func (c *Config) SetUIPasswordHash(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Server.UIPassword = ""
	c.Server.UIPasswordHash = strings.TrimSpace(hash)
	c.fileUIPassword = ""
}

// AddUpstream 添加或更新上游配置
func (c *Config) AddUpstream(name string, config UpstreamConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = normalizeLower(name)
	if name == "" {
		return fmt.Errorf("upstream name is empty")
	}
	if strings.TrimSpace(config.OutboundProxy) != "" {
		outboundProxy, err := NormalizeOutboundProxy(config.OutboundProxy)
		if err != nil {
			return err
		}
		config.OutboundProxy = outboundProxy
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultUpstreamTimeoutSeconds
	}
	c.Upstreams[name] = config
	return nil // 实际上应该由调用者决定是否立即 Save
}

// RemoveUpstream 删除上游配置
func (c *Config) RemoveUpstream(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = normalizeLower(name)
	if name == "" {
		return fmt.Errorf("upstream name is empty")
	}
	delete(c.Upstreams, name)
	if c.Overrides.Upstreams != nil {
		delete(c.Overrides.Upstreams, name)
	}
	if c.Usage.Upstreams != nil {
		delete(c.Usage.Upstreams, name)
	}
	return nil
}

// IsUIHost 判断是否为 UI 请求的 Host
func (c *Config) IsUIHost(host string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 移除端口号
	if idx := len(host) - 1; idx > 0 {
		for i := idx; i >= 0; i-- {
			if host[i] == ':' {
				host = host[:i]
				break
			}
			if host[i] == ']' { // IPv6
				break
			}
		}
	}

	host = normalizeLower(host)
	for _, h := range c.Server.UIHosts {
		if normalizeLower(h) == host {
			return true
		}
	}
	for _, d := range c.Server.ProxyDomains {
		d = normalizeLower(strings.TrimPrefix(d, "."))
		if d != "" && d == host {
			return true
		}
	}
	return false
}

// GetUpstream 根据子域名获取上游配置
func (c *Config) GetUpstream(subdomain string) (*UpstreamConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	subdomain = normalizeLower(subdomain)
	if subdomain == "" {
		return nil, false
	}
	up, ok := c.Upstreams[subdomain]
	if !ok {
		return nil, false
	}
	return &up, true
}

// ListUpstreams returns a copy of upstream configs for safe iteration.
func (c *Config) ListUpstreams() map[string]UpstreamConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[string]UpstreamConfig, len(c.Upstreams))
	for k, v := range c.Upstreams {
		out[k] = v
	}
	return out
}

// ExtractSubdomain 从 Host 中提取子域名
// 例如: openai.localhost:8080 -> openai
func ExtractSubdomain(host string, proxyDomains []string) string {
	// 移除端口号
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			host = host[:i]
			break
		}
		if host[i] == ']' { // IPv6
			break
		}
	}

	host = strings.ToLower(host)
	if len(proxyDomains) == 0 {
		proxyDomains = []string{"localhost"}
	}

	for _, d := range proxyDomains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d == "" {
			continue
		}
		d = strings.TrimPrefix(d, ".") // tolerate ".localhost"

		suffix := "." + d
		if len(host) <= len(suffix) || !strings.HasSuffix(host, suffix) {
			continue
		}
		sub := strings.TrimSuffix(host, suffix)
		// Require single-label subdomain to avoid ambiguity (a.b.example.com).
		if sub == "" || strings.Contains(sub, ".") {
			continue
		}
		return sub
	}

	return ""
}

func ExtractPathUpstream(path, prefix string) (string, string, bool) {
	prefix = NormalizePathRoutingPrefix(prefix)
	if path == "" {
		path = "/"
	}
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return "", "", false
	}

	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "", "", false
	}

	upstream := rest
	forwardPath := "/"
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		upstream = rest[:idx]
		forwardPath = rest[idx:]
	}

	upstream = normalizeLower(upstream)
	if upstream == "" || strings.Contains(upstream, ".") {
		return "", "", false
	}
	if forwardPath == "" {
		forwardPath = "/"
	}
	return upstream, forwardPath, true
}

func IsPathRoutingRequest(path, prefix string) bool {
	_, _, ok := ExtractPathUpstream(path, prefix)
	return ok
}
