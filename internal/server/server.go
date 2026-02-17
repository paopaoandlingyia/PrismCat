package server

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prismcat/prismcat/internal/api"
	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/proxy"
	"github.com/prismcat/prismcat/internal/storage"
)

//go:embed all:ui
var uiFS embed.FS

func init() {
	// 显式注册 MIME 类型，防止在 Windows 等环境中因注册表缺失导致静态资源识别错误（如 SVG logo 无法显示）
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".js", "text/javascript")
	_ = mime.AddExtensionType(".css", "text/css")
}

// spaHandler 处理本地文件系统的 SPA 路由
type spaHandler struct {
	staticPath string
	indexFile  string
}

func hasPathExt(urlPath string) bool {
	i := strings.LastIndex(urlPath, "/")
	base := urlPath
	if i >= 0 {
		base = urlPath[i+1:]
	}
	dot := strings.LastIndexByte(base, '.')
	return dot > 0 && dot < len(base)-1
}

func applyCORS(w http.ResponseWriter, r *http.Request, cfg config.ServerConfig) {
	if len(cfg.CORSAllowOrigins) == 0 {
		return
	}

	allowOrigin := ""
	if len(cfg.CORSAllowOrigins) == 1 && cfg.CORSAllowOrigins[0] == "*" {
		allowOrigin = "*"
	} else {
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, o := range cfg.CORSAllowOrigins {
				if o == origin {
					allowOrigin = origin
					break
				}
			}
		}
	}

	if allowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		if allowOrigin != "*" {
			// Origin-specific CORS should vary to avoid cache poisoning.
			w.Header().Add("Vary", "Origin")
		}
	}
	if len(cfg.CORSAllowMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.CORSAllowMethods, ", "))
	}
	if len(cfg.CORSAllowHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.CORSAllowHeaders, ", "))
	}
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Use http.Dir so URL paths are safely resolved relative to staticPath.
	fsys := http.Dir(h.staticPath)

	f, err := fsys.Open(r.URL.Path)
	if err != nil {
		// 如果是 API 请求或静态资源请求（有扩展名），返回 404
		if strings.HasPrefix(r.URL.Path, "/api/") || hasPathExt(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		// 对于 SPA 路由，返回 index.html
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexFile))
		return
	}
	defer f.Close()

	stat, statErr := f.Stat()
	if statErr != nil || stat.IsDir() {
		if strings.HasPrefix(r.URL.Path, "/api/") || hasPathExt(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexFile))
		return
	}

	http.FileServer(fsys).ServeHTTP(w, r)
}

// spaFSHandler 处理嵌入文件系统的 SPA 路由
type spaFSHandler struct {
	fs        http.FileSystem
	indexFile string
}

// serveIndex 直接从嵌入文件系统读取 index.html 并写入响应。
// 不经过 http.FileServer，避免其对 /index.html 的自动 301 重定向。
func (h spaFSHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := h.fs.Open("/" + h.indexFile)
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, h.indexFile, stat.ModTime(), f.(io.ReadSeeker))
}

func (h spaFSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Root always serves the SPA entry.
	if r.URL.Path == "/" {
		h.serveIndex(w, r)
		return
	}

	// Check if the requested path exists and is a file; otherwise fall back to index.html.
	f, err := h.fs.Open(r.URL.Path)
	if err == nil {
		stat, statErr := f.Stat()
		_ = f.Close()
		if statErr == nil && !stat.IsDir() {
			http.FileServer(h.fs).ServeHTTP(w, r)
			return
		}
	}

	// 如果是 API 请求或静态资源请求（有扩展名），返回 404
	if strings.HasPrefix(r.URL.Path, "/api/") || hasPathExt(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	h.serveIndex(w, r)
}

// Server HTTP 服务器
type Server struct {
	cfg    *config.Config
	repo   storage.Repository
	blobs  storage.BlobStore
	proxy  *proxy.Proxy
	api    *api.Handler
	server *http.Server
}

// New 创建服务器实例
func New(cfg *config.Config, repo storage.Repository, blobs storage.BlobStore) *Server {
	return &Server{
		cfg:   cfg,
		repo:  repo,
		blobs: blobs,
		proxy: proxy.New(cfg, repo),
		api:   api.New(cfg, repo, blobs),
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()
	serverCfg := s.cfg.ServerSnapshot()

	// 注册 API 路由
	s.api.RegisterRoutes(mux)

	// 静态文件服务（UI）- 支持 SPA 路由
	var uiHandler http.Handler
	if uiContent, err := fs.Sub(uiFS, "ui"); err == nil {
		// `go:embed` requires the directory to exist at compile time; we keep a
		// tracked placeholder file so builds work even when UI isn't built.
		// If index.html isn't embedded, fall back to local dist or placeholder.
		if f, err := uiContent.Open("index.html"); err == nil {
			_ = f.Close()
			uiHandler = spaFSHandler{fs: http.FS(uiContent), indexFile: "index.html"}
		}
	}
	if uiHandler == nil {
		log.Println("未找到可用的嵌入 UI，尝试从本地目录加载...")
		if _, err := os.Stat("./web/dist/index.html"); err == nil {
			uiHandler = spaHandler{staticPath: "./web/dist", indexFile: "index.html"}
		} else {
			uiHandler = http.HandlerFunc(s.placeholderUI)
		}
	}
	mux.Handle("/", uiHandler)

	var activeRequests atomic.Int64

	// authMiddleware handles password protection for UI and API
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if serverCfg.UIPassword != "" {
				_, pass, ok := r.BasicAuth()
				if !ok || pass != serverCfg.UIPassword {
					w.Header().Set("WWW-Authenticate", `Basic realm="PrismCat Control Panel"`)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}

	// Create main handler with routing and auth
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeRequests.Add(1)
		defer activeRequests.Add(-1)

		applyCORS(w, r, serverCfg)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Routing: UI Host (Control Panel + API) vs Proxy Host
		if s.cfg.IsUIHost(r.Host) {
			authMiddleware(mux).ServeHTTP(w, r)
		} else {
			s.proxy.ServeHTTP(w, r)
		}
	})

	addr := fmt.Sprintf("%s:%d", serverCfg.Addr, serverCfg.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mainHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 流式响应需要禁用写超时
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("🐱 PrismCat 启动成功！")
	log.Printf("📊 控制台: http://localhost:%d", serverCfg.Port)
	proxyDomain := "localhost"
	if len(serverCfg.ProxyDomains) > 0 {
		proxyDomain = serverCfg.ProxyDomains[0]
	}
	log.Printf("🔀 代理示例: http://openai.%s:%d", proxyDomain, serverCfg.Port)
	log.Println("按 Ctrl+C 停止服务")

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.ListenAndServe()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("服务器启动失败: %w", err)
		}
		return nil
	case <-sigChan:
	}

	log.Println("正在关闭服务器...")
	shutdownTimeout := 10 * time.Second
	if serverCfg.ShutdownTimeoutSeconds > 0 {
		shutdownTimeout = time.Duration(serverCfg.ShutdownTimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		log.Printf("服务器关闭错误: %v", err)
		// Force close active connections if graceful shutdown times out.
		_ = s.server.Close()
	}

	// Ensure handlers finish before returning (prevents closing repositories too early).
	deadline := time.Now().Add(shutdownTimeout)
	for activeRequests.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := activeRequests.Load(); n > 0 {
		log.Printf("shutdown: %d request(s) still active after timeout", n)
	}

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("服务器启动失败: %w", err)
	}
	return nil
}

// placeholderUI 占位 UI（在没有前端构建时使用）
func (s *Server) placeholderUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `<!DOCTYPE html>
<html lang="zh">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>PrismCat</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            color: #fff;
        }
        .container {
            text-align: center;
            padding: 2rem;
        }
        .logo {
            margin-bottom: 2rem;
            display: flex;
            justify-content: center;
        }
        .logo svg {
            width: 80px;
            height: 80px;
        }
        h1 {
            font-size: 2.5rem;
            margin-bottom: 0.5rem;
            background: linear-gradient(90deg, #3B82F6, #8B5CF6);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .subtitle {
            color: #8b8b9a;
            margin-bottom: 2rem;
        }
        .status {
            background: rgba(255,255,255,0.05);
            border: 1px solid rgba(255,255,255,0.1);
            border-radius: 12px;
            padding: 1.5rem;
            margin-bottom: 2rem;
            width: 100%;
            max-width: 400px;
            margin-left: auto;
            margin-right: auto;
        }
        .status-item {
            display: flex;
            justify-content: space-between;
            padding: 0.5rem 0;
            border-bottom: 1px solid rgba(255,255,255,0.05);
        }
        .status-item:last-child { border: none; }
        .badge {
            background: rgba(74, 222, 128, 0.2);
            color: #4ade80;
            border: 1px solid rgba(74, 222, 128, 0.3);
            padding: 0.25rem 0.75rem;
            border-radius: 999px;
            font-size: 0.875rem;
            font-weight: 500;
        }
        .info {
            font-size: 0.875rem;
            color: #8b8b9a;
            max-width: 500px;
            line-height: 1.6;
            margin-left: auto;
            margin-right: auto;
        }
        .info code {
            background: rgba(255,255,255,0.1);
            padding: 0.125rem 0.5rem;
            border-radius: 4px;
            font-family: 'Fira Code', monospace;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo">
            <svg viewBox="14 14 72 78" fill="none" xmlns="http://www.w3.org/2000/svg">
                <defs>
                    <linearGradient id="prismGradientPlace" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#3B82F6" />
                        <stop offset="100%" stop-color="#8B5CF6" />
                    </linearGradient>
                </defs>
                <path d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z" fill="url(#prismGradientPlace)" fill-opacity="0.2" />
                <path d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z" fill="url(#prismGradientPlace)" fill-opacity="0.1" stroke="url(#prismGradientPlace)" stroke-width="3" stroke-linejoin="round" />
                <path d="M50 32 V88 M18 40 L50 60 L82 40" stroke="#FFFFFF" stroke-width="2" stroke-opacity="0.4" stroke-linecap="round" stroke-linejoin="round" />
                <circle cx="50" cy="60" r="1.5" fill="white" fill-opacity="0.8" />
            </svg>
        </div>
        <h1>PrismCat</h1>
        <p class="subtitle">LLM API 透传代理 & 日志记录</p>
        
        <div class="status">
            <div class="status-item">
                <span>服务状态</span>
                <span class="badge">运行中</span>
            </div>
            <div class="status-item">
                <span>API 端点</span>
                <span><code>/api/logs</code></span>
            </div>
            <div class="status-item">
                <span>健康检查</span>
                <span><code>/api/health</code></span>
            </div>
        </div>
        
        <p class="info">
            前端 UI 尚未同步或构建。请运行 <code>快速编译并运行.bat</code> 尝试重新构建并同步。
        </p>
    </div>
</body>
</html>`
	w.Write([]byte(html))
}
