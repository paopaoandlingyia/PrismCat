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
	Annotation                   LogAnnotation       `json:"annotation"`

	// Transient capture state used only before async persistence.
	RequestBodyRaw               []byte `json:"-"`
	RequestBodyOriginalRaw       []byte `json:"-"`
	RequestBodyFinalRaw          []byte `json:"-"`
	RequestBodyCaptureTruncated  bool   `json:"-"`
	ResponseBodyRaw              []byte `json:"-"`
	ResponseBodyCaptureTruncated bool   `json:"-"`
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
	Upstream   string     // 按上游名称过滤
	Method     string     // 按请求方法过滤
	StatusCode int        // 按状态码过滤
	Path       string     // 按路径模糊搜索
	Tag        string     // 按标签过滤
	TraceID    string     // 按 trace ID 过滤
	Saved      *bool      // 是否保存
	Status     string     // 人工处理状态：none/todo/done
	Label      string     // 按人工标签过滤
	StartTime  *time.Time // 开始时间
	EndTime    *time.Time // 结束时间
	HasError   *bool      // 是否有错误
	Streaming  *bool      // 是否为流式

	// 分页
	Offset int
	Limit  int
}

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
