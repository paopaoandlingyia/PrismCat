package storage

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// RequestLog 请求日志记录
type RequestLog struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`

	// 上游信息
	Upstream       string `json:"upstream"`                  // 上游名称 (openai, gemini 等)
	UpstreamTarget string `json:"upstream_target,omitempty"` // 请求开始时选中的目标预设
	TargetURL      string `json:"target_url"`                // 实际请求的上游 URL

	// 请求信息
	Method              string              `json:"method"`
	Path                string              `json:"path"`
	Query               string              `json:"query,omitempty"`
	RequestHeaders      map[string][]string `json:"request_headers,omitempty"`
	RequestBody         string              `json:"request_body,omitempty"`
	RequestBodyOriginal string              `json:"request_body_original,omitempty"`
	RequestBodyFinal    string              `json:"request_body_final,omitempty"`
	RequestBodyRef      string              `json:"request_body_ref,omitempty"`
	RequestBodySize     int64               `json:"request_body_size"`

	// 响应信息
	StatusCode       int                 `json:"status_code"`
	ResponseHeaders  map[string][]string `json:"response_headers,omitempty"`
	ResponseBody     string              `json:"response_body,omitempty"`
	ResponseBodyRef  string              `json:"response_body_ref,omitempty"`
	ResponseBodySize int64               `json:"response_body_size"`
	Bodies           []LogBody           `json:"bodies,omitempty"`

	// 元数据
	Streaming                    bool                `json:"streaming"`       // 是否为流式响应
	Latency                      int64               `json:"latency_ms"`      // 响应延迟(毫秒)
	Error                        string              `json:"error,omitempty"` // 错误信息
	Truncated                    bool                `json:"truncated"`       // 请求或响应内容是否无法完整恢复
	Tag                          string              `json:"tag,omitempty"`   // 来自 X-PrismCat-Tag 请求头
	TraceID                      string              `json:"trace_id,omitempty"`
	ParentLogID                  string              `json:"parent_log_id,omitempty"`
	TraceSeq                     int                 `json:"trace_seq,omitempty"`
	RequestOverrideApplied       bool                `json:"request_override_applied,omitempty"`
	RequestOverrideRules         []string            `json:"request_override_rules,omitempty"`
	RequestOverrideError         string              `json:"request_override_error,omitempty"`
	RequestHeaderOverrideApplied bool                `json:"request_header_override_applied,omitempty"`
	RequestHeaderOverrideChanges json.RawMessage     `json:"request_header_override_changes,omitempty"`
	RequestHeadersOriginal       map[string][]string `json:"request_headers_original,omitempty"`
	UsageInputTokens             *int64              `json:"usage_input_tokens,omitempty"`
	UsageOutputTokens            *int64              `json:"usage_output_tokens,omitempty"`
	UsageTotalTokens             *int64              `json:"usage_total_tokens,omitempty"`
	UsageRaw                     string              `json:"usage_raw,omitempty"`
	UsageSource                  string              `json:"usage_source,omitempty"`
	BodyStorageError             string              `json:"body_storage_error,omitempty"`
	Origin                       string              `json:"origin,omitempty"`
	BackupVerifiedAt             *time.Time          `json:"backup_verified_at,omitempty"`
	BackupBatchID                string              `json:"backup_batch_id,omitempty"`
	DeleteGraceStartedAt         *time.Time          `json:"delete_grace_started_at,omitempty"`
	DeleteEligibleAt             *time.Time          `json:"delete_eligible_at,omitempty"`
	ImportBatchID                string              `json:"import_batch_id,omitempty"`
	Annotation                   LogAnnotation       `json:"annotation"`

	// Transient capture state used only before async persistence.
	RequestBodyRaw               []byte `json:"-"`
	RequestBodyOriginalRaw       []byte `json:"-"`
	RequestBodyFinalRaw          []byte `json:"-"`
	RequestBodyCaptureTruncated  bool   `json:"-"`
	ResponseBodyRaw              []byte `json:"-"`
	ResponseBodyCaptureTruncated bool   `json:"-"`
}

const (
	BodyPartRequest         = "request"
	BodyPartRequestOriginal = "request_original"
	BodyPartRequestFinal    = "request_final"
	BodyPartResponse        = "response"
)

type LogBody struct {
	LogID           string `json:"-"`
	Part            string `json:"part"`
	BlobRef         string `json:"blob_ref,omitempty"`
	CapturedBytes   int64  `json:"captured_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Truncated       bool   `json:"truncated"`
	ContentType     string `json:"content_type,omitempty"`
	ContentEncoding string `json:"content_encoding,omitempty"`
	Representation  string `json:"representation"`
	Recoverable     bool   `json:"recoverable"`
}

func (l *RequestLog) Body(part string) (LogBody, bool) {
	if l == nil {
		return LogBody{}, false
	}
	for _, body := range l.Bodies {
		if body.Part == part {
			return body, true
		}
	}
	return LogBody{}, false
}

type LogAnnotation struct {
	Saved     bool      `json:"saved"`
	Status    string    `json:"status"`
	Note      string    `json:"note,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// LogFilter 日志查询过滤器
type LogFilter struct {
	Upstream     string     // 按上游名称过滤
	Method       string     // 按请求方法过滤
	StatusCode   int        // 按状态码过滤
	Path         string     // 按路径模糊搜索
	Tag          string     // 按标签过滤
	TraceID      string     // 按 trace ID 过滤
	Saved        *bool      // 是否保存
	Status       string     // 人工处理状态：none/todo/done
	Label        string     // 按人工标签过滤
	StartTime    *time.Time // 开始时间
	EndTime      *time.Time // 结束时间
	HasError     *bool      // 是否有错误
	Streaming    *bool      // 是否为流式
	BackupStatus string     // 备份状态：pending/verified/restored

	// 分页
	Offset int
	Limit  int
}

const (
	BackupStatusPending  = "pending"
	BackupStatusVerified = "verified"
	BackupStatusRestored = "restored"
)

// LogStats 日志统计
type LogStats struct {
	TotalRequests  int64            `json:"total_requests"`
	SuccessCount   int64            `json:"success_count"`
	ErrorCount     int64            `json:"error_count"`
	StreamingCount int64            `json:"streaming_count"`
	AvgLatency     float64          `json:"avg_latency_ms"`
	ByUpstream     map[string]int64 `json:"by_upstream"`
	ByStatusCode   map[int]int64    `json:"by_status_code"`
}

// TraceSummary trace 列表的聚合摘要
type TraceSummary struct {
	TraceID           string   `json:"trace_id"`
	RequestCount      int      `json:"request_count"`
	FirstTime         int64    `json:"first_time"`
	LastTime          int64    `json:"last_time"`
	TotalLatency      int64    `json:"total_latency_ms"`
	ErrorCount        int      `json:"error_count"`
	UsageInputTokens  *int64   `json:"usage_input_tokens,omitempty"`
	UsageOutputTokens *int64   `json:"usage_output_tokens,omitempty"`
	UsageTotalTokens  *int64   `json:"usage_total_tokens,omitempty"`
	Upstreams         []string `json:"upstreams"`
	Tags              []string `json:"tags"`
}

// TraceFilter trace 查询过滤器
type TraceFilter struct {
	TraceID   string     // 模糊搜索 trace ID
	Upstream  string     // 按上游名称过滤
	Tag       string     // 按标签过滤
	HasError  *bool      // 是否有错误
	StartTime *time.Time // 开始时间
	EndTime   *time.Time // 结束时间

	Offset int
	Limit  int
}

// Repository 存储接口
type Repository interface {
	// 日志操作
	SaveLog(log *RequestLog) error
	GetLog(id string) (*RequestLog, error)
	ListLogs(filter LogFilter) ([]*RequestLog, int64, error) // 返回日志列表和总数
	ExportLogs(ctx context.Context, filter LogFilter, each func(*RequestLog) error) error
	DeleteLogsBefore(before time.Time) (int64, error) // 返回删除数量
	DeleteOldestLogs(count int) (int64, error)        // 按时间正序删除最老的 count 条
	CountDeletableLogs() (int64, error)               // 统计可删除的日志数量
	WALCheckpoint() error                             // 截断 WAL 回收磁盘空间
	Vacuum() error                                    // 重建数据库文件回收空间
	GetLogAnnotation(logID string) (LogAnnotation, error)
	SaveLogAnnotation(logID string, annotation LogAnnotation) (LogAnnotation, error)

	// Trace 操作
	ListTraces(filter TraceFilter) ([]TraceSummary, int64, error)
	GetTraceRequests(traceID string) ([]*RequestLog, error)

	// 统计
	GetStats(since *time.Time) (*LogStats, error)

	// 生命周期
	Close() error
}

type BodyRepository interface {
	GetLogBody(logID, part string) (LogBody, error)
	GetLogBodies(logID string) ([]LogBody, error)
}

type BlobRefRepository interface {
	ListBlobRefs() ([]string, error)
}

type ArchiveBatch struct {
	ID              string     `json:"id"`
	JobID           string     `json:"job_id,omitempty"`
	Trigger         string     `json:"trigger,omitempty"`
	ArchiveDate     string     `json:"archive_date"`
	ObjectKey       string     `json:"object_key,omitempty"`
	ManifestKey     string     `json:"manifest_key,omitempty"`
	RangeStart      time.Time  `json:"range_start"`
	RangeEnd        time.Time  `json:"range_end"`
	Status          string     `json:"status"`
	LogCount        int64      `json:"log_count"`
	BodyCount       int64      `json:"body_count"`
	LogicalBytes    int64      `json:"logical_bytes"`
	CompressedBytes int64      `json:"compressed_bytes"`
	SHA256          string     `json:"sha256,omitempty"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
}

const (
	ArchiveDateTypeCompletedAt = "completed_at"
	ArchiveDateTypeArchiveDate = "archive_date"
)

type ArchiveBatchFilter struct {
	DateType      string
	Date          string
	CompletedFrom *time.Time
	CompletedTo   *time.Time
	JobID         string
	Offset        int
	Limit         int
}

type ArchiveJob struct {
	ID           string     `json:"id"`
	Trigger      string     `json:"trigger"`
	Cutoff       time.Time  `json:"cutoff"`
	Status       string     `json:"status"`
	PackageCount int64      `json:"package_count"`
	LogCount     int64      `json:"log_count"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type ArchiveImport struct {
	ID        string     `json:"id"`
	SourceKey string     `json:"source_key,omitempty"`
	Status    string     `json:"status"`
	LogCount  int64      `json:"log_count"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ArchiveRepository interface {
	RecoverInterruptedArchiveWork(now time.Time) error
	OldestUnarchivedLogTime(before time.Time) (*time.Time, error)
	CreateArchiveJob(job ArchiveJob) error
	UpdateArchiveJob(job ArchiveJob) error
	ListArchiveJobs(limit int) ([]ArchiveJob, error)
	ListArchiveJobsPage(offset, limit int) ([]ArchiveJob, int64, error)
	CreateArchiveBatch(batch ArchiveBatch) error
	UpdateArchiveBatch(batch ArchiveBatch) error
	ListArchiveBatches(limit int) ([]ArchiveBatch, error)
	ListArchiveBatchesPage(filter ArchiveBatchFilter) ([]ArchiveBatch, int64, error)
	ReserveArchiveBatchLogs(batchID string, start, end time.Time) (int64, error)
	ReleaseArchiveBatchLogs(batchID string) error
	ExportArchiveBatch(ctx context.Context, batchID string, each func(*RequestLog) error) error
	MarkArchiveBatchVerified(batchID string, verifiedAt time.Time) (int64, error)
	DeleteEligibleBackedLogs(cutoff time.Time, limit int) (int64, error)
	CountEligibleBackedLogs(cutoff time.Time) (int64, error)
	PendingBackedLogCleanup() (int64, *time.Time, error)
	LogExists(id string) (bool, error)
	SaveImportedLog(log *RequestLog) error
	CreateArchiveImport(batch ArchiveImport) error
	UpdateArchiveImport(batch ArchiveImport) error
	ListArchiveImports() ([]ArchiveImport, error)
	ListArchiveImportsPage(offset, limit int) ([]ArchiveImport, int64, error)
	DeleteArchiveImport(batchID string) (int64, error)
	DeleteExpiredArchiveImports(now time.Time) (int64, error)
	StageArchiveBlobRef(batchID, ref string) error
	ClearArchiveBlobRefs(batchID string) error
}

// Clone returns a deep copy of the RequestLog.
func (l *RequestLog) Clone() *RequestLog {
	if l == nil {
		return nil
	}
	out := *l
	out.RequestHeaders = CloneHeaders(l.RequestHeaders)
	out.ResponseHeaders = CloneHeaders(l.ResponseHeaders)
	out.RequestHeadersOriginal = CloneHeaders(l.RequestHeadersOriginal)
	out.RequestBodyRaw = cloneBytes(l.RequestBodyRaw)
	out.RequestBodyOriginalRaw = cloneBytes(l.RequestBodyOriginalRaw)
	out.RequestBodyFinalRaw = cloneBytes(l.RequestBodyFinalRaw)
	out.ResponseBodyRaw = cloneBytes(l.ResponseBodyRaw)
	if len(l.Bodies) > 0 {
		out.Bodies = append([]LogBody(nil), l.Bodies...)
	}
	if l.BackupVerifiedAt != nil {
		v := *l.BackupVerifiedAt
		out.BackupVerifiedAt = &v
	}
	if l.DeleteGraceStartedAt != nil {
		v := *l.DeleteGraceStartedAt
		out.DeleteGraceStartedAt = &v
	}
	if l.DeleteEligibleAt != nil {
		v := *l.DeleteEligibleAt
		out.DeleteEligibleAt = &v
	}
	if len(l.RequestOverrideRules) > 0 {
		out.RequestOverrideRules = append([]string(nil), l.RequestOverrideRules...)
	}
	if len(l.RequestHeaderOverrideChanges) > 0 {
		out.RequestHeaderOverrideChanges = append(json.RawMessage(nil), l.RequestHeaderOverrideChanges...)
	}
	out.UsageInputTokens = cloneInt64Ptr(l.UsageInputTokens)
	out.UsageOutputTokens = cloneInt64Ptr(l.UsageOutputTokens)
	out.UsageTotalTokens = cloneInt64Ptr(l.UsageTotalTokens)

	if len(l.Annotation.Labels) > 0 {
		out.Annotation.Labels = append([]string(nil), l.Annotation.Labels...)
	}
	return &out
}

// CloneHeaders deep copies headers map.
func CloneHeaders(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, vv := range in {
		if vv == nil {
			out[k] = nil
			continue
		}
		newVv := make([]string, len(vv))
		copy(newVv, vv)
		out[k] = newVv
	}
	return out
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// FirstHeaderValue returns the first value of a header key in a case-insensitive manner.
func FirstHeaderValue(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	// Direct lookup (usually matches canonical format)
	if vv, ok := headers[key]; ok && len(vv) > 0 {
		return vv[0]
	}
	// Case-insensitive fallback
	for k, vv := range headers {
		if strings.EqualFold(k, key) && len(vv) > 0 {
			return vv[0]
		}
	}
	return ""
}
