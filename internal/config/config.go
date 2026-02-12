package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

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
	Port    int      `yaml:"port"`
	UIHosts []string `yaml:"ui_hosts"`
	UIPassword string    `yaml:"ui_password"`

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

	// DetachBodyOverBytes detaches large captured bodies into the blob store.
	// The log table keeps only a short preview + a content-addressed reference.
	//
	// 0: use default (256KB). <0: disable detaching.
	DetachBodyOverBytes int64 `yaml:"detach_body_over_bytes"`
	// BodyPreviewBytes controls how many bytes of a detached body are kept inline
	// in request_logs.request_body/response_body for quick viewing.
	// 0: disable preview (store empty preview).
	BodyPreviewBytes int64 `yaml:"body_preview_bytes"`

	detachBodyOverBytesSet bool `yaml:"-"`
	bodyPreviewBytesSet    bool `yaml:"-"`
}

func (c *LoggingConfig) UnmarshalYAML(value *yaml.Node) error {
	// Avoid recursion by decoding into a separate type.
	var raw struct {
		MaxRequestBody   int64    `yaml:"max_request_body"`
		MaxResponseBody  int64    `yaml:"max_response_body"`
		SensitiveHeaders []string `yaml:"sensitive_headers"`
		DetachBodyOver   int64    `yaml:"detach_body_over_bytes"`
		BodyPreviewBytes int64    `yaml:"body_preview_bytes"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}

	c.MaxRequestBody = raw.MaxRequestBody
	c.MaxResponseBody = raw.MaxResponseBody
	c.SensitiveHeaders = raw.SensitiveHeaders
	c.DetachBodyOverBytes = raw.DetachBodyOver
	c.BodyPreviewBytes = raw.BodyPreviewBytes

	c.detachBodyOverBytesSet = false
	c.bodyPreviewBytesSet = false
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			key := value.Content[i]
			if key == nil {
				continue
			}
			switch key.Value {
			case "detach_body_over_bytes":
				c.detachBodyOverBytesSet = true
			case "body_preview_bytes":
				c.bodyPreviewBytesSet = true
			}
		}
	}
	return nil
}

// StorageConfig 存储配置
type StorageConfig struct {
	Database      string `yaml:"database"`
	RetentionDays int    `yaml:"retention_days"`

	// BlobStore defines where detached bodies are stored.
	// Supported values: "fs" (filesystem). (Others can be added later, e.g. "sqlite", "s3".)
	BlobStore string `yaml:"blob_store"`
	// BlobDir is used when BlobStore == "fs".
	BlobDir string `yaml:"blob_dir"`
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

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	c.configPath = path
	if c.Upstreams == nil {
		c.Upstreams = make(map[string]UpstreamConfig)
	}

	// 设置默认值
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if len(c.Server.UIHosts) == 0 {
		c.Server.UIHosts = []string{"localhost", "127.0.0.1"}
	}
	if len(c.Server.ProxyDomains) == 0 {
		c.Server.ProxyDomains = []string{"localhost"}
	}
	if c.Server.ShutdownTimeoutSeconds <= 0 {
		c.Server.ShutdownTimeoutSeconds = 10
	}
	if len(c.Server.CORSAllowOrigins) == 0 {
		c.Server.CORSAllowOrigins = []string{"*"}
	}
	if len(c.Server.CORSAllowMethods) == 0 {
		c.Server.CORSAllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if len(c.Server.CORSAllowHeaders) == 0 {
		c.Server.CORSAllowHeaders = []string{"Content-Type", "Authorization"}
	}
	if c.Logging.MaxRequestBody == 0 {
		c.Logging.MaxRequestBody = 1 << 20 // 1MB
	}
	if c.Logging.MaxResponseBody == 0 {
		c.Logging.MaxResponseBody = 10 << 20 // 10MB
	}
	if len(c.Logging.SensitiveHeaders) == 0 {
		c.Logging.SensitiveHeaders = []string{"Authorization", "x-api-key", "api-key"}
	}
	// Default: detach large bodies to blob storage to keep the log table lightweight.
	// If explicitly configured <= 0, detaching is disabled.
	if !c.Logging.detachBodyOverBytesSet {
		c.Logging.DetachBodyOverBytes = 256 * 1024 // 256KB
	} else if c.Logging.DetachBodyOverBytes <= 0 {
		c.Logging.DetachBodyOverBytes = 0
	}
	// Default: keep a small preview inline. 0 disables preview.
	if !c.Logging.bodyPreviewBytesSet {
		c.Logging.BodyPreviewBytes = 4 * 1024 // 4KB
	} else if c.Logging.BodyPreviewBytes < 0 {
		c.Logging.BodyPreviewBytes = 0
	}
	if c.Storage.Database == "" {
		c.Storage.Database = "./data/prismcat.db"
	}
	if c.Storage.BlobStore == "" {
		c.Storage.BlobStore = "fs"
	}
	if c.Storage.BlobDir == "" {
		c.Storage.BlobDir = "./data/blobs"
	}

	// 覆盖环境变量 (云端/容器化部署优先)
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
	if envPassword := os.Getenv("PRISMCAT_UI_PASSWORD"); envPassword != "" {
		c.Server.UIPassword = envPassword
	}

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

	c.Upstreams[name] = config
	return nil // 实际上应该由调用者决定是否立即 Save
}

// RemoveUpstream 删除上游配置
func (c *Config) RemoveUpstream(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	for _, h := range c.Server.UIHosts {
		if h == host {
			return true
		}
	}
	return false
}

// GetUpstream 根据子域名获取上游配置
func (c *Config) GetUpstream(subdomain string) (*UpstreamConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
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
