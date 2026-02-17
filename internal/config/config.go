package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const Version = "1.0.0"

// Config 应用配置
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Upstreams map[string]UpstreamConfig `yaml:"upstreams"`
	Logging   LoggingConfig             `yaml:"logging"`
	Storage   StorageConfig             `yaml:"storage"`

	configPath string // 配置文件路径
	mu         sync.RWMutex
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Addr       string   `yaml:"addr"`
	Port       int      `yaml:"port"`
	UIHosts    []string `yaml:"ui_hosts"`
	UIPassword string   `yaml:"ui_password"`

	// ProxyDomains defines the base domains used for host-based upstream routing.
	// For example, if ProxyDomains contains "localhost", then requests to
	// "openai.localhost" will be routed to upstream "openai".
	//
	// Cloud deployments typically set this to something like "prismcat.example.com"
	// so that "openai.prismcat.example.com" routes to upstream "openai".
	ProxyDomains []string `yaml:"proxy_domains"`

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
	Target  string `yaml:"target"`
	Timeout int    `yaml:"timeout"` // 秒
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	MaxRequestBody   int64    `yaml:"max_request_body"`
	MaxResponseBody  int64    `yaml:"max_response_body"`
	SensitiveHeaders []string `yaml:"sensitive_headers"`
	StoreBase64      bool     `yaml:"store_base64"`

	// DetachBodyOverBytes detaches large captured bodies into the blob store.
	// The log table keeps only a short preview + a content-addressed reference.
	//
	// 0: use default (256KB). <0: disable detaching.
	DetachBodyOverBytes int64 `yaml:"detach_body_over_bytes"`
	// BodyPreviewBytes controls how many bytes of a detached body are kept inline
	// in request_logs.request_body/response_body for quick viewing.
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
			ShutdownTimeoutSeconds: 10,
			CORSAllowOrigins:       []string{"*"},
			CORSAllowMethods:       []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			CORSAllowHeaders:       []string{"Content-Type", "Authorization"},
		},
		Logging: LoggingConfig{
			MaxRequestBody:      1 << 20, // 1MB
			MaxResponseBody:     10 << 20, // 10MB
			SensitiveHeaders:    []string{"Authorization", "x-api-key", "api-key"},
			StoreBase64:         true,
			DetachBodyOverBytes: 256 * 1024,
			BodyPreviewBytes:    4 * 1024,
		},
		Storage: StorageConfig{
			Database:    "./data/prismcat.db",
			BlobStore:   "fs",
			BlobDir:     "./data/blobs",
			AsyncBuffer: 4096,
		},
		Upstreams: make(map[string]UpstreamConfig),
	}

	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	c.configPath = path

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
		c.Server.UIPassword = envPassword
	}

	// Normalize case/spacing for host-based matching.
	c.Server.UIHosts = normalizeLowerList(c.Server.UIHosts)
	c.Server.ProxyDomains = normalizeLowerList(c.Server.ProxyDomains)

	normalizedUpstreams, err := normalizeUpstreams(c.Upstreams)
	if err != nil {
		return nil, err
	}
	c.Upstreams = normalizedUpstreams

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
		out[n] = v
	}
	return out, nil
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

// Get 获取当前配置（需要先调用 Load）
func Get() *Config {
	return cfg
}

// Save 保存配置文件
func (c *Config) Save() error {
	// Save writes the config file; it must be exclusive to avoid concurrent writes.
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(c.configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// AddUpstream 添加或更新上游配置
func (c *Config) AddUpstream(name string, config UpstreamConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = normalizeLower(name)
	if name == "" {
		return fmt.Errorf("upstream name is empty")
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
