package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/live"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

// Handler API 处理器
type Handler struct {
	cfg    *config.Config
	repo   storage.Repository
	blobs  storage.BlobStore
	live   *live.Registry
	client *http.Client
}

// New 创建 API 处理器
func New(cfg *config.Config, repo storage.Repository, blobs storage.BlobStore, liveRegistry *live.Registry) *Handler {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Handler{
		cfg:   cfg,
		repo:  repo,
		blobs: blobs,
		live:  liveRegistry,
		client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: transport,
		},
	}
}

// RegisterRoutes 注册 API 路由
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", h.handleLogs)
	mux.HandleFunc("/api/logs/", h.handleLogDetail)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/upstreams", h.handleUpstreams)
	mux.HandleFunc("/api/config", h.handleConfig)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/blobs/", h.handleBlob)
	mux.HandleFunc("/api/replay", h.handleReplay)
}

// handleLogs 获取日志列表
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	filter := storage.LogFilter{
		Upstream: query.Get("upstream"),
		Method:   query.Get("method"),
		Path:     query.Get("path"),
		Tag:      query.Get("tag"),
	}

	if statusCode := query.Get("status_code"); statusCode != "" {
		if code, err := strconv.Atoi(statusCode); err == nil {
			filter.StatusCode = code
		}
	}

	if offset := query.Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}

	if limit := query.Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}

	if startTime := query.Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = &t
		}
	}

	if endTime := query.Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = &t
		}
	}

	logs, total, err := h.repo.ListLogs(filter)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponse(w, map[string]interface{}{
		"logs":   logs,
		"total":  total,
		"offset": filter.Offset,
		"limit":  filter.Limit,
	})
}

// handleLogDetail 获取日志详情
func (h *Handler) handleLogDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 从路径中提取 ID: /api/logs/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	if path == "" {
		h.jsonError(w, "缺少日志 ID", http.StatusBadRequest)
		return
	}

	if id, ok := strings.CutSuffix(path, "/live"); ok {
		if id == "" {
			h.jsonError(w, "缺少日志 ID", http.StatusBadRequest)
			return
		}
		h.handleLogLive(w, r, id)
		return
	}

	id := path

	log, err := h.repo.GetLog(id)
	if err != nil {
		h.jsonError(w, "日志不存在", http.StatusNotFound)
		return
	}

	h.jsonResponse(w, log)
}

func (h *Handler) handleLogLive(w http.ResponseWriter, r *http.Request, id string) {
	if h.live == nil {
		http.Error(w, "live stream unavailable", http.StatusNotImplemented)
		return
	}

	events, unsubscribe, ok := h.live.Subscribe(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer unsubscribe()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}

			if err := h.writeLiveEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) writeLiveEvent(w io.Writer, event live.Event) error {
	payload := map[string]interface{}{
		"type": event.Type,
	}
	if event.Log != nil {
		payload["log"] = event.Log
	}
	if event.Chunk != "" {
		payload["chunk"] = event.Chunk
	}
	if event.SizeDelta > 0 {
		payload["size_delta"] = event.SizeDelta
	}

	return writeSSEEvent(w, string(event.Type), payload)
}

func writeSSEEvent(w io.Writer, event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

// handleStats 获取统计信息
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var since *time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = &t
		}
	}

	stats, err := h.repo.GetStats(since)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponse(w, stats)
}

// handleUpstreams 获取或管理上游配置
func (h *Handler) handleUpstreams(w http.ResponseWriter, r *http.Request) {
	// GET: 获取列表
	if r.Method == http.MethodGet {
		upstreams := make([]map[string]interface{}, 0)
		// Snapshot upstreams for safe iteration.
		for name, upCfg := range h.cfg.ListUpstreams() {
			upstreams = append(upstreams, map[string]interface{}{
				"name":    name,
				"target":  upCfg.Target,
				"timeout": upCfg.Timeout,
				"order":   upCfg.Order,
			})
		}
		sort.Slice(upstreams, func(i, j int) bool {
			leftOrder, _ := upstreams[i]["order"].(int)
			rightOrder, _ := upstreams[j]["order"].(int)
			if leftOrder != rightOrder {
				return leftOrder < rightOrder
			}
			return fmt.Sprint(upstreams[i]["name"]) < fmt.Sprint(upstreams[j]["name"])
		})
		h.jsonResponse(w, upstreams)
		return
	}

	// POST: 添加/更新
	if r.Method == http.MethodPost {
		var req struct {
			Name    string `json:"name"`
			Target  string `json:"target"`
			Timeout int    `json:"timeout"`
			Order   int    `json:"order"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.jsonError(w, "无效的请求体", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Target == "" {
			h.jsonError(w, "名称和目标必填", http.StatusBadRequest)
			return
		}

		err := h.cfg.AddUpstream(req.Name, config.UpstreamConfig{
			Target:  req.Target,
			Timeout: req.Timeout,
			Order:   req.Order,
		})
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := h.cfg.Save(); err != nil {
			h.jsonError(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, map[string]string{"status": "ok"})
		return
	}

	// DELETE: 删除
	if r.Method == http.MethodDelete {
		name := r.URL.Query().Get("name")
		if name == "" {
			h.jsonError(w, "名称必填", http.StatusBadRequest)
			return
		}
		if err := h.cfg.RemoveUpstream(name); err != nil {
			h.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := h.cfg.Save(); err != nil {
			h.jsonError(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, map[string]string{"status": "ok"})
		return
	}

	h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
}

// handleHealth 健康检查
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, map[string]string{
		"status":  "ok",
		"version": config.Version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

// handleConfig 获取或更新配置
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	// GET: 获取配置
	if r.Method == http.MethodGet {
		logging := h.cfg.LoggingSnapshot()
		storageCfg := h.cfg.StorageSnapshot()
		serverCfg := h.cfg.ServerSnapshot()
		overrides := h.cfg.RequestOverridesSnapshot()
		h.jsonResponse(w, map[string]interface{}{
			"version": config.Version,
			"server": map[string]interface{}{
				"proxy_domains":       serverCfg.ProxyDomains,
				"enable_path_routing": serverCfg.EnablePathRouting,
				"path_routing_prefix": serverCfg.PathRoutingPrefix,
			},
			"logging": map[string]interface{}{
				"max_request_body":            logging.MaxRequestBody,
				"max_response_body":           logging.MaxResponseBody,
				"sensitive_headers":           logging.SensitiveHeaders,
				"early_request_body_snapshot": logging.EarlyRequestBodySnapshot,
				"detach_body_over_bytes":      logging.DetachBodyOverBytes,
				"body_preview_bytes":          logging.BodyPreviewBytes,
				"store_base64":                logging.StoreBase64,
			},
			"storage": map[string]interface{}{
				"database":       storageCfg.Database,
				"retention_days": storageCfg.RetentionDays,
				"blob_store":     storageCfg.BlobStore,
				"blob_dir":       storageCfg.BlobDir,
			},
			"request_overrides": overrides,
		})
		return
	}

	// PUT: 更新配置
	if r.Method == http.MethodPut {
		var req struct {
			Server *struct {
				EnablePathRouting *bool   `json:"enable_path_routing"`
				PathRoutingPrefix *string `json:"path_routing_prefix"`
			} `json:"server"`
			Logging *struct {
				MaxRequestBody   *int64    `json:"max_request_body"`
				MaxResponseBody  *int64    `json:"max_response_body"`
				SensitiveHeaders *[]string `json:"sensitive_headers"`
				EarlyReqSnapshot *bool     `json:"early_request_body_snapshot"`
				DetachBodyOver   *int64    `json:"detach_body_over_bytes"`
				BodyPreviewBytes *int64    `json:"body_preview_bytes"`
				StoreBase64      *bool     `json:"store_base64"`
			} `json:"logging"`
			Storage *struct {
				RetentionDays *int `json:"retention_days"`
			} `json:"storage"`
			RequestOverrides *config.RequestOverridesConfig `json:"request_overrides"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.jsonError(w, "无效的请求体", http.StatusBadRequest)
			return
		}

		// 更新日志配置
		h.cfg.Update(func(c *config.Config) {
			if req.Server != nil {
				if req.Server.EnablePathRouting != nil {
					c.Server.EnablePathRouting = *req.Server.EnablePathRouting
				}
				if req.Server.PathRoutingPrefix != nil {
					c.Server.PathRoutingPrefix = config.NormalizePathRoutingPrefix(*req.Server.PathRoutingPrefix)
				}
			}

			if req.Logging != nil {
				if req.Logging.MaxRequestBody != nil {
					c.Logging.MaxRequestBody = *req.Logging.MaxRequestBody
				}
				if req.Logging.MaxResponseBody != nil {
					c.Logging.MaxResponseBody = *req.Logging.MaxResponseBody
				}
				if req.Logging.SensitiveHeaders != nil {
					c.Logging.SensitiveHeaders = *req.Logging.SensitiveHeaders
				}
				if req.Logging.EarlyReqSnapshot != nil {
					c.Logging.EarlyRequestBodySnapshot = *req.Logging.EarlyReqSnapshot
				}
				if req.Logging.DetachBodyOver != nil {
					c.Logging.DetachBodyOverBytes = *req.Logging.DetachBodyOver
				}
				if req.Logging.BodyPreviewBytes != nil {
					c.Logging.BodyPreviewBytes = *req.Logging.BodyPreviewBytes
				}
				if req.Logging.StoreBase64 != nil {
					c.Logging.StoreBase64 = *req.Logging.StoreBase64
				}
			}

			if req.Storage != nil {
				if req.Storage.RetentionDays != nil {
					c.Storage.RetentionDays = *req.Storage.RetentionDays
				}
			}

			if req.RequestOverrides != nil {
				c.Overrides = config.NormalizeRequestOverrides(*req.RequestOverrides)
			}
		})

		// 保存配置
		if err := h.cfg.Save(); err != nil {
			h.jsonError(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, map[string]string{"status": "ok"})
		return
	}

	h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
}

func (h *Handler) handleBlob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	if h.blobs == nil {
		h.jsonError(w, "blob 存储未启用", http.StatusNotImplemented)
		return
	}

	ref := strings.TrimPrefix(r.URL.Path, "/api/blobs/")
	if ref == "" {
		h.jsonError(w, "缺少 blob ref", http.StatusBadRequest)
		return
	}
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}

	data, err := h.blobs.Get(r.Context(), ref)
	if err != nil {
		if err == storage.ErrBlobNotFound {
			http.NotFound(w, r)
			return
		}
		h.jsonError(w, "读取 blob 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+blobFilename(ref, data)+"\"")
	_, _ = w.Write(data)
}

func blobFilename(ref string, data []byte) string {
	suffix := "bin"
	switch http.DetectContentType(data) {
	case "image/png":
		suffix = "png"
	case "image/jpeg":
		suffix = "jpg"
	case "image/gif":
		suffix = "gif"
	case "image/webp":
		suffix = "webp"
	case "application/pdf":
		suffix = "pdf"
	}
	name := strings.TrimPrefix(ref, "sha256:")
	if len(name) > 12 {
		name = name[:12]
	}
	if name == "" {
		name = "blob"
	}
	return name + "." + suffix
}

// handleReplay sends a request to the configured upstream and returns the response.
func (h *Handler) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Upstream string            `json:"upstream"`
		Method   string            `json:"method"`
		Path     string            `json:"path"`
		Headers  map[string]string `json:"headers"`
		Body     string            `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20) // 100MB
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if req.Upstream == "" || req.Method == "" {
		h.jsonError(w, "upstream 和 method 必填", http.StatusBadRequest)
		return
	}

	upstream, ok := h.cfg.GetUpstream(req.Upstream)
	if !ok {
		h.jsonError(w, "未知的 upstream: "+req.Upstream, http.StatusBadRequest)
		return
	}

	targetURL, err := url.Parse(upstream.Target)
	if err != nil {
		h.jsonError(w, "上游配置无效", http.StatusInternalServerError)
		return
	}

	// Build full URL: upstream target + request path.
	fullURL := strings.TrimRight(targetURL.String(), "/")
	if req.Path != "" {
		if !strings.HasPrefix(req.Path, "/") {
			fullURL += "/"
		}
		fullURL += req.Path
	}

	timeout := upstream.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, fullURL, body)
	if err != nil {
		h.jsonError(w, "创建请求失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set headers.
	for k, v := range req.Headers {
		upstreamReq.Header.Set(k, v)
	}
	upstreamReq.Host = targetURL.Host

	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		h.jsonError(w, "上游请求失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body (limit to 10MB to avoid memory issues).
	const maxRespBody = 10 * 1024 * 1024
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody+1))
	truncated := false
	if int64(len(respBody)) > maxRespBody {
		respBody = respBody[:maxRespBody]
		truncated = true
	}

	respHeaders := make(map[string][]string)
	for k, vv := range resp.Header {
		if len(vv) > 0 {
			respHeaders[k] = vv
		}
	}

	formattedBody := httpbody.FormatForDisplay(
		resp.Header.Get("Content-Type"),
		resp.Header.Get("Content-Encoding"),
		respBody,
		httpbody.FormatOptions{MaxOutputBytes: maxRespBody},
	)
	bodyDecoded := formattedBody.Decoded || resp.Uncompressed
	bodyDecodedFrom := formattedBody.DecodedFrom
	if bodyDecodedFrom == "" && resp.Uncompressed {
		bodyDecodedFrom = "gzip"
	}

	h.jsonResponse(w, map[string]interface{}{
		"status_code":       resp.StatusCode,
		"headers":           respHeaders,
		"body":              formattedBody.Text,
		"truncated":         truncated || formattedBody.Truncated,
		"body_decoded":      bodyDecoded,
		"body_decoded_from": bodyDecodedFrom,
	})
}

// jsonResponse 发送 JSON 响应
func (h *Handler) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// jsonError 发送错误响应
func (h *Handler) jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
