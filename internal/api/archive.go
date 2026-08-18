package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	archivepkg "github.com/paopaoandlingyia/PrismCat/internal/archive"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type archiveS3Update struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	ClearSecret     bool   `json:"clear_secret_access_key"`
	ForcePathStyle  bool   `json:"force_path_style"`
}

type archiveConfigUpdate struct {
	Enabled              bool            `json:"enabled"`
	S3                   archiveS3Update `json:"s3"`
	KeyPrefix            string          `json:"key_prefix"`
	ScheduleTime         string          `json:"schedule_time"`
	Timezone             string          `json:"timezone"`
	ZstdLevel            int             `json:"zstd_level"`
	LocalRetentionHours  int             `json:"local_retention_hours"`
	ImportRetentionHours int             `json:"import_retention_hours"`
}

func mergeArchiveConfig(base config.ArchiveConfig, update archiveConfigUpdate) config.ArchiveConfig {
	next := config.ArchiveConfig{
		Enabled: update.Enabled, KeyPrefix: update.KeyPrefix, ScheduleTime: update.ScheduleTime,
		Timezone: update.Timezone, ZstdLevel: update.ZstdLevel,
		LocalRetentionHours: update.LocalRetentionHours, ImportRetentionHours: update.ImportRetentionHours,
		S3: config.ArchiveS3Config{
			Endpoint: update.S3.Endpoint, Region: update.S3.Region, Bucket: update.S3.Bucket,
			AccessKeyID: update.S3.AccessKeyID, ForcePathStyle: update.S3.ForcePathStyle,
			SecretAccessKey: base.S3.SecretAccessKey,
		},
	}
	if update.S3.ClearSecret {
		next.S3.SecretAccessKey = ""
	} else if strings.TrimSpace(update.S3.SecretAccessKey) != "" {
		next.S3.SecretAccessKey = update.S3.SecretAccessKey
	}
	return next
}

func publicArchiveConfig(cfg config.ArchiveConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled": cfg.Enabled, "key_prefix": cfg.KeyPrefix, "schedule_time": cfg.ScheduleTime,
		"timezone": cfg.Timezone, "zstd_level": cfg.ZstdLevel,
		"local_retention_hours": cfg.LocalRetentionHours, "import_retention_hours": cfg.ImportRetentionHours,
		"s3": map[string]interface{}{
			"endpoint": cfg.S3.Endpoint, "region": cfg.S3.Region, "bucket": cfg.S3.Bucket,
			"access_key_id": cfg.S3.AccessKeyID, "force_path_style": cfg.S3.ForcePathStyle,
			"secret_configured": cfg.S3.SecretAccessKey != "",
		},
	}
}

func (h *Handler) handleArchives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	if h.archives == nil {
		h.jsonError(w, "归档服务不可用", http.StatusNotImplemented)
		return
	}
	includeS3 := r.URL.Query().Get("include_s3") != "false"
	status, err := h.archives.Status(r.Context(), includeS3, strings.TrimSpace(r.URL.Query().Get("date")))
	if err != nil {
		h.jsonError(w, "读取归档状态失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, status)
}

func (h *Handler) handleArchiveAction(w http.ResponseWriter, r *http.Request) {
	if h.archives == nil {
		h.jsonError(w, "归档服务不可用", http.StatusNotImplemented)
		return
	}
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/archives/"), "/")
	switch action {
	case "packages":
		h.handleArchivePackages(w, r)
	case "jobs":
		h.handleArchiveJobs(w, r)
	case "imports":
		h.handleArchiveImports(w, r)
	case "test":
		if r.Method != http.MethodPost {
			h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		candidate := h.cfg.ArchiveSnapshot()
		var req archiveConfigUpdate
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err == nil {
			candidate = mergeArchiveConfig(candidate, req)
		} else if !errors.Is(err, io.EOF) {
			h.jsonError(w, "无效的 S3 配置", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := h.archives.TestConnection(ctx, candidate); err != nil {
			h.jsonError(w, "S3 连接测试失败: "+err.Error(), http.StatusBadGateway)
			return
		}
		h.jsonResponse(w, map[string]string{"status": "ok"})
	case "run":
		if r.Method != http.MethodPost {
			h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		job, err := h.archives.StartManual()
		if errors.Is(err, archivepkg.ErrArchiveBusy) {
			h.jsonError(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			h.jsonError(w, "归档失败: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		h.jsonResponse(w, map[string]interface{}{"status": "accepted", "job": job})
	case "deletion-preview":
		if r.Method != http.MethodGet {
			h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		hours, err := time.ParseDuration(strings.TrimSpace(r.URL.Query().Get("hours")) + "h")
		if err != nil || hours < time.Hour {
			h.jsonError(w, "hours 必须至少为 1", http.StatusBadRequest)
			return
		}
		count, err := h.archives.DeletionPreview(int(hours / time.Hour))
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.jsonResponse(w, map[string]interface{}{"count": count})
	case "import":
		if r.Method != http.MethodPost {
			h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Key string `json:"key"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Key) == "" {
			h.jsonError(w, "key 必填", http.StatusBadRequest)
			return
		}
		batch, err := h.archives.ImportFromS3(r.Context(), strings.TrimSpace(req.Key))
		if err != nil {
			h.jsonError(w, "导入失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		h.jsonResponse(w, batch)
	case "import-date":
		if r.Method != http.MethodPost {
			h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Date string `json:"date"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.Date) == "" {
			h.jsonError(w, "date 必填", http.StatusBadRequest)
			return
		}
		batches, err := h.archives.ImportDate(r.Context(), req.Date)
		if err != nil {
			h.jsonError(w, "按日期恢复失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		h.jsonResponse(w, map[string]interface{}{"imports": batches})
	case "import-upload":
		h.handleArchiveUpload(w, r)
	default:
		h.jsonError(w, "归档操作不存在", http.StatusNotFound)
	}
}

func (h *Handler) handleArchivePackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	offset, limit, err := parseArchivePagination(r)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	dateType := strings.TrimSpace(r.URL.Query().Get("date_type"))
	if dateType == "" {
		dateType = storage.ArchiveDateTypeCompletedAt
	}
	if dateType != storage.ArchiveDateTypeCompletedAt && dateType != storage.ArchiveDateTypeArchiveDate {
		h.jsonError(w, "date_type 必须是 completed_at 或 archive_date", http.StatusBadRequest)
		return
	}
	filter := storage.ArchiveBatchFilter{
		DateType: dateType, JobID: strings.TrimSpace(r.URL.Query().Get("job_id")), Offset: offset, Limit: limit,
	}
	if date := strings.TrimSpace(r.URL.Query().Get("date")); date != "" {
		loc, loadErr := time.LoadLocation(h.cfg.ArchiveSnapshot().Timezone)
		if loadErr != nil {
			h.jsonError(w, "归档时区无效", http.StatusInternalServerError)
			return
		}
		day, parseErr := time.ParseInLocation("2006-01-02", date, loc)
		if parseErr != nil || day.Format("2006-01-02") != date {
			h.jsonError(w, "date 必须使用 YYYY-MM-DD 格式", http.StatusBadRequest)
			return
		}
		filter.Date = date
		if dateType == storage.ArchiveDateTypeCompletedAt {
			end := day.AddDate(0, 0, 1)
			filter.CompletedFrom = &day
			filter.CompletedTo = &end
		}
	}
	items, total, err := h.archives.ListBatches(filter)
	if err != nil {
		h.jsonError(w, "读取备份包历史失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *Handler) handleArchiveJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	offset, limit, err := parseArchivePagination(r)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	items, total, err := h.archives.ListJobs(offset, limit)
	if err != nil {
		h.jsonError(w, "读取备份任务历史失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *Handler) handleArchiveImports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	offset, limit, err := parseArchivePagination(r)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	items, total, err := h.archives.ListImports(offset, limit)
	if err != nil {
		h.jsonError(w, "读取导入历史失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func parseArchivePagination(r *http.Request) (int, int, error) {
	offset, limit := 0, 50
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return 0, 0, errors.New("offset 必须是非负整数")
		}
		offset = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return 0, 0, errors.New("limit 必须是 1 到 200")
		}
		limit = value
	}
	return offset, limit, nil
}

func (h *Handler) handleArchiveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.jsonError(w, "读取上传文件失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.jsonError(w, "缺少 file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".tar.zst") {
		h.jsonError(w, "只支持 .tar.zst", http.StatusBadRequest)
		return
	}
	tempDir, err := os.MkdirTemp("", "prismcat-upload-*")
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, "upload.tar.zst")
	out, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		if copyErr == nil {
			copyErr = closeErr
		}
		h.jsonError(w, "保存上传文件失败: "+copyErr.Error(), http.StatusInternalServerError)
		return
	}
	batch, err := h.archives.ImportFile(r.Context(), tempPath, "upload:"+filepath.Base(header.Filename))
	if err != nil {
		h.jsonError(w, "导入失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	h.jsonResponse(w, batch)
}

func (h *Handler) handleArchiveImportDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	if h.archives == nil {
		h.jsonError(w, "归档服务不可用", http.StatusNotImplemented)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/archive-imports/"), "/")
	if id == "" {
		h.jsonError(w, "缺少导入批次 ID", http.StatusBadRequest)
		return
	}
	deleted, err := h.archives.DeleteImport(id)
	if err != nil {
		h.jsonError(w, "删除导入批次失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, map[string]interface{}{"status": "ok", "deleted_logs": deleted})
}
