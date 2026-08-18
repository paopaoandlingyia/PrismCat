package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/archive"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/live"
	"github.com/paopaoandlingyia/PrismCat/internal/outbound"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
	"github.com/paopaoandlingyia/PrismCat/internal/storageusage"
	"github.com/paopaoandlingyia/PrismCat/internal/systemmetrics"
	"github.com/paopaoandlingyia/PrismCat/internal/updatecheck"
)

// Handler API 处理器
type Handler struct {
	cfg      *config.Config
	repo     storage.Repository
	blobs    storage.BlobStore
	live     *live.Registry
	clients  *outbound.ClientCache
	metrics  *systemmetrics.Collector
	updates  *updatecheck.Checker
	archives *archive.Manager
}

// New 创建 API 处理器
func New(cfg *config.Config, repo storage.Repository, blobs storage.BlobStore, liveRegistry *live.Registry, archiveManagers ...*archive.Manager) *Handler {
	h := &Handler{
		cfg:     cfg,
		repo:    repo,
		blobs:   blobs,
		live:    liveRegistry,
		clients: outbound.NewClientCache(50, 10),
		metrics: systemmetrics.NewCollector(),
		updates: updatecheck.NewChecker(),
	}
	if len(archiveManagers) > 0 {
		h.archives = archiveManagers[0]
	} else {
		h.archives, _ = archive.NewManager(cfg, repo, blobs)
	}
	return h
}

// RegisterRoutes 注册 API 路由
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", h.handleLogs)
	mux.HandleFunc("/api/logs/export", h.handleLogsExport)
	mux.HandleFunc("/api/logs/", h.handleLogDetail)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/upstreams", h.handleUpstreams)
	mux.HandleFunc("/api/upstreams/active-target", h.handleUpstreamActiveTarget)
	mux.HandleFunc("/api/logging-rules/model-path-templates", h.handleModelPathTemplates)
	mux.HandleFunc("/api/logging-rules/ignored-paths", h.handleIgnoredPaths)
	mux.HandleFunc("/api/config", h.handleConfig)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/system/metrics", h.handleSystemMetrics)
	mux.HandleFunc("/api/system/storage", h.handleSystemStorage)
	mux.HandleFunc("/api/system/update", h.handleSystemUpdate)
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/api/blobs/", h.handleBlob)
	mux.HandleFunc("/api/replay", h.handleReplay)
	mux.HandleFunc("/api/traces", h.handleTraces)
	mux.HandleFunc("/api/traces/", h.handleTraceDetail)
	mux.HandleFunc("/api/archives", h.handleArchives)
	mux.HandleFunc("/api/archives/", h.handleArchiveAction)
	mux.HandleFunc("/api/archive-imports/", h.handleArchiveImportDelete)
}

// handleLogs 获取日志列表
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	filter, err := parseLogFilter(query, true)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	logs, total, err := h.repo.ListLogs(filter)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, logEntry := range logs {
		h.enrichLogArchiveState(logEntry)
	}

	h.jsonResponse(w, map[string]interface{}{
		"logs":   logs,
		"total":  total,
		"offset": filter.Offset,
		"limit":  filter.Limit,
	})
}

func parseLogFilter(query url.Values, includePagination bool) (storage.LogFilter, error) {
	filter := storage.LogFilter{
		Upstream:     query.Get("upstream"),
		Method:       query.Get("method"),
		Path:         query.Get("path"),
		Tag:          query.Get("tag"),
		TraceID:      query.Get("trace_id"),
		Status:       query.Get("annotation_status"),
		Label:        query.Get("annotation_label"),
		BackupStatus: strings.TrimSpace(query.Get("backup_status")),
	}
	switch filter.BackupStatus {
	case "", storage.BackupStatusPending, storage.BackupStatusVerified, storage.BackupStatusRestored:
	default:
		return storage.LogFilter{}, fmt.Errorf("backup_status 参数无效")
	}
	if saved := query.Get("saved"); saved != "" {
		if v, err := strconv.ParseBool(saved); err == nil {
			filter.Saved = &v
		}
	}

	if statusCode := query.Get("status_code"); statusCode != "" {
		if code, err := strconv.Atoi(statusCode); err == nil {
			filter.StatusCode = code
		}
	}

	if includePagination {
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

	return filter, nil
}

func (h *Handler) handleLogsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	format := strings.ToLower(strings.TrimSpace(query.Get("format")))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" {
		h.jsonError(w, "不支持的导出格式", http.StatusBadRequest)
		return
	}

	includeBody := true
	if raw := query.Get("include_body"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			h.jsonError(w, "include_body 参数无效", http.StatusBadRequest)
			return
		}
		includeBody = v
	}

	filter, err := parseLogFilter(query, false)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+logsExportFilename(filter)+"\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	err = h.repo.ExportLogs(r.Context(), filter, func(logEntry *storage.RequestLog) error {
		h.enrichLogArchiveState(logEntry)
		if includeBody {
			h.fillExportBodies(r.Context(), logEntry)
		} else {
			clearExportBodies(logEntry)
		}
		if err := encoder.Encode(logEntry); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil && r.Context().Err() == nil {
		// Headers may already be sent; in that case the partial JSONL file is still
		// useful up to the last complete line.
		return
	}
}

func (h *Handler) fillExportBodies(ctx context.Context, logEntry *storage.RequestLog) {
	if h.blobs == nil || logEntry == nil {
		return
	}

	logging := h.cfg.LoggingSnapshot()
	for _, metadata := range logEntry.Bodies {
		if metadata.BlobRef == "" {
			continue
		}
		body, err := h.blobs.Get(ctx, metadata.BlobRef)
		if err != nil {
			logEntry.Truncated = true
			continue
		}
		max := logging.MaxRequestBody
		if metadata.Part == storage.BodyPartResponse {
			max = logging.MaxResponseBody
		}
		formatOptions := httpbody.FormatOptions{
			MaxOutputBytes:               max,
			TrimLargeBase64:              !logging.StoreBase64,
			RequireContentEncodingDecode: true,
		}
		var formatted httpbody.FormatResult
		if metadata.Representation == "display" {
			formatOptions.RequireContentEncodingDecode = false
			formatted = httpbody.FormatPreviewForDisplay(metadata.ContentType, "", body, formatOptions)
		} else {
			formatted = httpbody.FormatForDisplay(metadata.ContentType, metadata.ContentEncoding, body, formatOptions)
		}
		switch metadata.Part {
		case storage.BodyPartRequest:
			logEntry.RequestBody = formatted.Text
		case storage.BodyPartRequestOriginal:
			logEntry.RequestBodyOriginal = formatted.Text
		case storage.BodyPartRequestFinal:
			logEntry.RequestBodyFinal = formatted.Text
		case storage.BodyPartResponse:
			logEntry.ResponseBody = formatted.Text
		}
		logEntry.Truncated = logEntry.Truncated || metadata.Truncated || formatted.Truncated
	}
}

func clearExportBodies(logEntry *storage.RequestLog) {
	if logEntry == nil {
		return
	}
	logEntry.RequestBody = ""
	logEntry.RequestBodyOriginal = ""
	logEntry.RequestBodyFinal = ""
	logEntry.ResponseBody = ""
}

func logsExportFilename(filter storage.LogFilter) string {
	const layout = "20060102T150405Z"
	start := "all"
	end := time.Now().UTC().Format(layout)
	if filter.StartTime != nil {
		start = filter.StartTime.UTC().Format(layout)
	}
	if filter.EndTime != nil {
		end = filter.EndTime.UTC().Format(layout)
	}
	return "prismcat-logs-" + start + "-" + end + ".jsonl"
}

// handleLogDetail 获取日志详情
func (h *Handler) handleLogDetail(w http.ResponseWriter, r *http.Request) {
	// 从路径中提取 ID: /api/logs/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	if path == "" {
		h.jsonError(w, "缺少日志 ID", http.StatusBadRequest)
		return
	}

	if logID, ok := strings.CutSuffix(path, "/annotation"); ok {
		if logID == "" {
			h.jsonError(w, "缺少日志 ID", http.StatusBadRequest)
			return
		}
		h.handleLogAnnotation(w, r, logID)
		return
	}

	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
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

	if id, ok := strings.CutSuffix(path, "/body"); ok {
		if id == "" {
			h.jsonError(w, "缺少日志 ID", http.StatusBadRequest)
			return
		}
		h.handleLogBody(w, r, id)
		return
	}

	id := path
	log, err := h.repo.GetLog(id)
	if err != nil {
		h.jsonError(w, "日志不存在", http.StatusNotFound)
		return
	}
	h.enrichLogArchiveState(log)

	h.jsonResponse(w, log)
}

func (h *Handler) enrichLogArchiveState(logEntry *storage.RequestLog) {
	if logEntry == nil {
		return
	}
	logEntry.DeleteEligibleAt = nil
	if logEntry.Origin == "archive_import" || logEntry.BackupVerifiedAt == nil ||
		logEntry.DeleteGraceStartedAt == nil || logEntry.Annotation.Saved {
		return
	}
	eligibleAt := logEntry.DeleteGraceStartedAt.Add(
		time.Duration(h.cfg.ArchiveSnapshot().LocalRetentionHours) * time.Hour,
	)
	logEntry.DeleteEligibleAt = &eligibleAt
}

func (h *Handler) handleLogBody(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	if _, err := h.repo.GetLog(id); err != nil {
		h.jsonError(w, "日志不存在", http.StatusNotFound)
		return
	}

	part := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("part")))
	if part == "" {
		part = "response"
	}

	var maxOutputBytes int64

	logging := h.cfg.LoggingSnapshot()
	switch part {
	case storage.BodyPartRequest, storage.BodyPartRequestOriginal, storage.BodyPartRequestFinal:
		maxOutputBytes = logging.MaxRequestBody
	case storage.BodyPartResponse:
		maxOutputBytes = logging.MaxResponseBody
	default:
		h.jsonError(w, "不支持的 body part", http.StatusBadRequest)
		return
	}

	bodyRepo, ok := h.repo.(storage.BodyRepository)
	if !ok {
		h.jsonError(w, "正文元数据存储不可用", http.StatusNotImplemented)
		return
	}
	metadata, err := bodyRepo.GetLogBody(id, part)
	if err == sql.ErrNoRows {
		h.jsonResponse(w, map[string]interface{}{
			"body":        "",
			"part":        part,
			"recoverable": false,
		})
		return
	}
	if err != nil {
		h.jsonError(w, "读取正文元数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if metadata.BlobRef == "" || h.blobs == nil {
		h.jsonResponse(w, map[string]interface{}{
			"body":        "",
			"metadata":    metadata,
			"truncated":   true,
			"recoverable": false,
		})
		return
	}

	data, err := h.blobs.Get(r.Context(), metadata.BlobRef)
	if err != nil {
		h.jsonError(w, "读取 Blob 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	formatOptions := httpbody.FormatOptions{
		MaxOutputBytes:               maxOutputBytes,
		TrimLargeBase64:              !logging.StoreBase64,
		RequireContentEncodingDecode: true,
	}
	var formatted httpbody.FormatResult
	if metadata.Representation == "display" {
		formatOptions.RequireContentEncodingDecode = false
		formatted = httpbody.FormatPreviewForDisplay(metadata.ContentType, "", data, formatOptions)
	} else {
		formatted = httpbody.FormatForDisplay(metadata.ContentType, metadata.ContentEncoding, data, formatOptions)
	}
	h.jsonResponse(w, map[string]interface{}{
		"body":              formatted.Text,
		"metadata":          metadata,
		"truncated":         metadata.Truncated || formatted.Truncated,
		"recoverable":       metadata.Recoverable,
		"body_decoded":      formatted.Decoded,
		"body_decoded_from": formatted.DecodedFrom,
		"decode_failed":     formatted.DecodeFailed,
	})
}

func (h *Handler) handleLogAnnotation(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	if _, err := h.repo.GetLog(id); err != nil {
		h.jsonError(w, "日志不存在", http.StatusNotFound)
		return
	}

	current, err := h.repo.GetLogAnnotation(id)
	if err != nil && err != sql.ErrNoRows {
		h.jsonError(w, "读取日志标记失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if current.Status == "" {
		current.Status = "none"
	}

	var req struct {
		Saved  *bool     `json:"saved"`
		Status *string   `json:"status"`
		Note   *string   `json:"note"`
		Labels *[]string `json:"labels"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if req.Saved != nil {
		current.Saved = *req.Saved
	}
	if req.Status != nil {
		current.Status = *req.Status
	}
	if req.Note != nil {
		current.Note = *req.Note
	}
	if req.Labels != nil {
		current.Labels = *req.Labels
	}

	annotation, err := h.repo.SaveLogAnnotation(id, current)
	if err != nil {
		h.jsonError(w, "保存日志标记失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, annotation)
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

func (h *Handler) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	filter := storage.TraceFilter{
		TraceID:  query.Get("trace_id"),
		Upstream: query.Get("upstream"),
		Tag:      query.Get("tag"),
	}

	if hasError := query.Get("has_error"); hasError != "" {
		if v, err := strconv.ParseBool(hasError); err == nil {
			filter.HasError = &v
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

	traces, total, err := h.repo.ListTraces(filter)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponse(w, map[string]interface{}{
		"traces": traces,
		"total":  total,
		"offset": filter.Offset,
		"limit":  filter.Limit,
	})
}

func (h *Handler) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	traceID := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	if traceID == "" {
		h.jsonError(w, "缺少 trace ID", http.StatusBadRequest)
		return
	}

	requests, err := h.repo.GetTraceRequests(traceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(requests) == 0 {
		h.jsonError(w, "trace 不存在", http.StatusNotFound)
		return
	}

	var totalLatency int64
	var errorCount int
	var firstTime, lastTime int64
	var usageInput, usageOutput, usageTotal int64
	var hasUsageInput, hasUsageOutput, hasUsageTotal bool
	upstreamSet := make(map[string]struct{})
	for i, req := range requests {
		totalLatency += req.Latency
		if req.UsageInputTokens != nil {
			usageInput += *req.UsageInputTokens
			hasUsageInput = true
		}
		if req.UsageOutputTokens != nil {
			usageOutput += *req.UsageOutputTokens
			hasUsageOutput = true
		}
		if req.UsageTotalTokens != nil {
			usageTotal += *req.UsageTotalTokens
			hasUsageTotal = true
		}
		if req.Error != "" || req.StatusCode >= 400 {
			errorCount++
		}
		ms := req.CreatedAt.UnixMilli()
		if i == 0 || ms < firstTime {
			firstTime = ms
		}
		if i == 0 || ms > lastTime {
			lastTime = ms
		}
		upstreamSet[req.Upstream] = struct{}{}
	}
	upstreams := make([]string, 0, len(upstreamSet))
	for u := range upstreamSet {
		upstreams = append(upstreams, u)
	}

	summary := map[string]interface{}{
		"request_count":    len(requests),
		"total_latency_ms": totalLatency,
		"error_count":      errorCount,
		"first_time":       firstTime,
		"last_time":        lastTime,
		"upstreams":        upstreams,
	}
	if hasUsageInput {
		summary["usage_input_tokens"] = usageInput
	}
	if hasUsageOutput {
		summary["usage_output_tokens"] = usageOutput
	}
	if hasUsageTotal {
		summary["usage_total_tokens"] = usageTotal
	}

	h.jsonResponse(w, map[string]interface{}{
		"trace_id": traceID,
		"requests": requests,
		"summary":  summary,
	})
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
			resolved, _, ok := h.cfg.ResolveUpstreamSnapshot(name)
			if !ok {
				continue
			}
			upstreams = append(upstreams, map[string]interface{}{
				"name":                             name,
				"target":                           resolved.Target,
				"timeout":                          resolved.Timeout,
				"response_header_timeout":          resolved.ResponseHeaderTimeout,
				"response_body_first_byte_timeout": resolved.ResponseBodyFirstByteTimeout,
				"response_body_idle_timeout":       resolved.ResponseBodyIdleTimeout,
				"order":                            upCfg.Order,
				"outbound_proxy":                   resolved.OutboundProxy,
				"logging_enabled":                  !upCfg.LoggingDisabled,
				"logging_path_filter":              upCfg.LoggingPathFilter,
				"active_target":                    upCfg.ActiveTarget,
				"targets":                          upCfg.Targets,
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
			Name                         string                                 `json:"name"`
			Target                       string                                 `json:"target"`
			Timeout                      int                                    `json:"timeout"`
			ResponseHeaderTimeout        int                                    `json:"response_header_timeout"`
			ResponseBodyFirstByteTimeout int                                    `json:"response_body_first_byte_timeout"`
			ResponseBodyIdleTimeout      int                                    `json:"response_body_idle_timeout"`
			Order                        int                                    `json:"order"`
			OutboundProxy                string                                 `json:"outbound_proxy"`
			LoggingEnabled               *bool                                  `json:"logging_enabled"`
			LoggingPathFilter            *config.LoggingPathFilterConfig        `json:"logging_path_filter"`
			ActiveTarget                 string                                 `json:"active_target"`
			Targets                      map[string]config.UpstreamTargetConfig `json:"targets"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.jsonError(w, "无效的请求体", http.StatusBadRequest)
			return
		}
		if req.Name == "" || (req.Target == "" && len(req.Targets) == 0) {
			h.jsonError(w, "名称和目标必填", http.StatusBadRequest)
			return
		}

		loggingDisabled := false
		var loggingPathFilter *config.LoggingPathFilterConfig
		if current, ok := h.cfg.GetUpstream(req.Name); ok {
			loggingDisabled = current.LoggingDisabled
			loggingPathFilter = current.LoggingPathFilter
		}
		if req.LoggingEnabled != nil {
			loggingDisabled = !*req.LoggingEnabled
		}
		if req.LoggingPathFilter != nil {
			loggingPathFilter = req.LoggingPathFilter
		}

		err := h.cfg.AddUpstream(req.Name, config.UpstreamConfig{
			Target:                       req.Target,
			Timeout:                      req.Timeout,
			ResponseHeaderTimeout:        req.ResponseHeaderTimeout,
			ResponseBodyFirstByteTimeout: req.ResponseBodyFirstByteTimeout,
			ResponseBodyIdleTimeout:      req.ResponseBodyIdleTimeout,
			Order:                        req.Order,
			OutboundProxy:                req.OutboundProxy,
			LoggingDisabled:              loggingDisabled,
			LoggingPathFilter:            loggingPathFilter,
			ActiveTarget:                 req.ActiveTarget,
			Targets:                      req.Targets,
		})
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
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

func (h *Handler) handleUpstreamActiveTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Upstream string `json:"upstream"`
		Target   string `json:"target"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "无效的请求体", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Upstream) == "" || strings.TrimSpace(req.Target) == "" {
		h.jsonError(w, "上游和目标必填", http.StatusBadRequest)
		return
	}
	if err := h.cfg.ActivateUpstreamTarget(req.Upstream, req.Target); err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.jsonResponse(w, map[string]string{"status": "ok", "active_target": strings.ToLower(strings.TrimSpace(req.Target))})
}

// handleHealth 健康检查
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, map[string]string{
		"status":  "ok",
		"version": config.Version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (h *Handler) handleSystemMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	h.jsonResponse(w, h.metrics.Snapshot())
}

func (h *Handler) handleSystemStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	usage, err := storageusage.Calculate(h.cfg.StorageSnapshot())
	if err != nil {
		h.jsonError(w, "计算存储占用失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, usage)
}

func (h *Handler) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	info, err := h.updates.Check(r.Context(), config.Version)
	if err != nil {
		h.jsonError(w, "检查更新失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	h.jsonResponse(w, info)
}

// handleConfig 获取或更新配置
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	// GET: 获取配置
	if r.Method == http.MethodGet {
		logging := h.cfg.LoggingSnapshot()
		storageCfg := h.cfg.StorageSnapshot()
		archiveCfg := h.cfg.ArchiveSnapshot()
		serverCfg := h.cfg.ServerSnapshot()
		overrides := h.cfg.RequestOverridesSnapshot()
		usageExtraction := h.cfg.UsageExtractionSnapshot()
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
				"store_base64":                logging.StoreBase64,
			},
			"storage": map[string]interface{}{
				"database":          storageCfg.Database,
				"retention_days":    storageCfg.RetentionDays,
				"max_storage_bytes": storageCfg.MaxStorageBytes,
				"blob_store":        storageCfg.BlobStore,
				"blob_dir":          storageCfg.BlobDir,
				"body_compression":  storageCfg.BodyCompression,
			},
			"archive":           publicArchiveConfig(archiveCfg),
			"request_overrides": overrides,
			"usage_extraction":  usageExtraction,
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
				StoreBase64      *bool     `json:"store_base64"`
			} `json:"logging"`
			Storage *struct {
				RetentionDays   *int                          `json:"retention_days"`
				MaxStorageBytes *int64                        `json:"max_storage_bytes"`
				BodyCompression *config.BodyCompressionConfig `json:"body_compression"`
			} `json:"storage"`
			Archive          *archiveConfigUpdate           `json:"archive"`
			RequestOverrides *config.RequestOverridesConfig `json:"request_overrides"`
			UsageExtraction  *config.UsageExtractionConfig  `json:"usage_extraction"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.jsonError(w, "无效的请求体", http.StatusBadRequest)
			return
		}

		var archiveCandidate *config.ArchiveConfig
		if req.Archive != nil {
			candidate := mergeArchiveConfig(h.cfg.ArchiveSnapshot(), *req.Archive)
			if err := config.ValidateArchiveConfig(candidate); err != nil {
				h.jsonError(w, "无效的归档配置: "+err.Error(), http.StatusBadRequest)
				return
			}
			candidate = config.NormalizeArchiveConfig(candidate)
			archiveCandidate = &candidate
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
				if req.Logging.StoreBase64 != nil {
					c.Logging.StoreBase64 = *req.Logging.StoreBase64
				}
			}

			if req.Storage != nil {
				if req.Storage.RetentionDays != nil {
					c.Storage.RetentionDays = *req.Storage.RetentionDays
				}
				if req.Storage.MaxStorageBytes != nil {
					c.Storage.MaxStorageBytes = *req.Storage.MaxStorageBytes
				}
				if req.Storage.BodyCompression != nil {
					c.Storage.BodyCompression = *req.Storage.BodyCompression
				}
				c.Storage = config.NormalizeStorageConfig(c.Storage)
			}
			if archiveCandidate != nil {
				c.Archive = *archiveCandidate
			}

			if req.RequestOverrides != nil {
				c.Overrides = config.NormalizeRequestOverrides(*req.RequestOverrides)
			}
			if req.UsageExtraction != nil {
				c.Usage = config.NormalizeUsageExtraction(*req.UsageExtraction)
			}
		})
		if fsStore, ok := h.blobs.(*storage.FileBlobStore); ok {
			storageCfg := h.cfg.StorageSnapshot()
			fsStore.SetCompression(storageCfg.BodyCompression.Algorithm, storageCfg.BodyCompression.Level)
		}

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
		Upstream  string            `json:"upstream"`
		TargetURL string            `json:"target_url"`
		Method    string            `json:"method"`
		Path      string            `json:"path"`
		Headers   map[string]string `json:"headers"`
		Body      string            `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20) // 100MB
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if req.Method == "" {
		h.jsonError(w, "method 必填", http.StatusBadRequest)
		return
	}
	if req.Upstream == "" && req.TargetURL == "" {
		h.jsonError(w, "upstream 或 target_url 必填", http.StatusBadRequest)
		return
	}

	fullURL, host, outboundProxy, timeout, err := h.resolveReplayTarget(req.Upstream, req.TargetURL, req.Path)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	client, err := h.clients.Client(outboundProxy)
	if err != nil {
		h.jsonError(w, "出站代理配置无效: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if timeout <= 0 {
		timeout = config.DefaultUpstreamTimeoutSeconds
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
	upstreamReq.Host = host

	resp, err := client.Do(upstreamReq)
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

func (h *Handler) resolveReplayTarget(upstreamName string, target string, path string) (string, string, string, int, error) {
	if target != "" {
		u, err := url.Parse(target)
		if err != nil {
			return "", "", "", 0, fmt.Errorf("target_url 无效: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", "", "", 0, fmt.Errorf("target_url 只支持 http 或 https")
		}
		if u.Host == "" {
			return "", "", "", 0, fmt.Errorf("target_url 缺少 host")
		}
		return u.String(), u.Host, "env", 120, nil
	}

	upstream, ok := h.cfg.GetUpstream(upstreamName)
	if !ok {
		return "", "", "", 0, fmt.Errorf("未知的 upstream: %s", upstreamName)
	}
	targetURL, err := url.Parse(upstream.Target)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("上游配置无效")
	}

	fullURL := strings.TrimRight(targetURL.String(), "/")
	if path != "" {
		if !strings.HasPrefix(path, "/") {
			fullURL += "/"
		}
		fullURL += path
	}

	return fullURL, targetURL.Host, upstream.OutboundProxy, upstream.Timeout, nil
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
