package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func (h *Handler) handleModelPathTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		systemDefaults, err := config.SystemModelPathTemplates()
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		snapshot := h.cfg.LoggingRulesSnapshot()
		h.jsonResponse(w, map[string]interface{}{
			"templates":       snapshot.ModelPathTemplates,
			"system_defaults": systemDefaults,
		})
	case http.MethodPut:
		var req struct {
			Templates []config.ModelPathTemplate `json:"templates"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			h.jsonError(w, "无效的请求体: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.cfg.ReplaceModelPathTemplates(req.Templates); err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.jsonResponse(w, map[string]string{"status": "ok"})
	default:
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleIgnoredPaths(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repo.(storage.IgnoredPathRepository)
	if !ok {
		h.jsonError(w, "被忽略路径存储不可用", http.StatusNotImplemented)
		return
	}

	switch r.Method {
	case http.MethodGet:
		query := r.URL.Query()
		filter := storage.IgnoredPathFilter{
			Upstream: query.Get("upstream"),
			Path:     query.Get("path"),
			Sort:     query.Get("sort"),
			Order:    query.Get("order"),
			Offset:   parseNonNegativeInt(query.Get("offset"), 0),
			Limit:    parseNonNegativeInt(query.Get("limit"), 50),
		}
		result, err := repo.ListIgnoredPaths(filter)
		if err != nil {
			h.jsonError(w, "读取被忽略路径失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, result)
	case http.MethodDelete:
		query := r.URL.Query()
		upstream := strings.TrimSpace(query.Get("upstream"))
		requestPath := strings.TrimSpace(query.Get("path"))
		if requestPath != "" && upstream == "" {
			h.jsonError(w, "删除单条路径时必须指定上游", http.StatusBadRequest)
			return
		}
		deleted, err := repo.DeleteIgnoredPaths(upstream, requestPath)
		if err != nil {
			h.jsonError(w, "删除被忽略路径失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, map[string]interface{}{"deleted": deleted})
	default:
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

func parseNonNegativeInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
