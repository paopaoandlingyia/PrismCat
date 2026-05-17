package storage

import "time"

// RequestLog 请求日志记录
type RequestLog struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`

	// 上游信息
	Upstream  string `json:"upstream"`   // 上游名称 (openai, gemini 等)
	TargetURL string `json:"target_url"` // 实际请求的上游 URL

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
	Streaming              bool          `json:"streaming"`       // 是否为流式响应
	Latency                int64         `json:"latency_ms"`      // 响应延迟(毫秒)
	Error                  string        `json:"error,omitempty"` // 错误信息
	Truncated              bool          `json:"truncated"`       // 请求或响应内容是否无法完整恢复
	Tag                    string        `json:"tag,omitempty"`   // 来自 X-PrismCat-Tag 请求头
	TraceID                string        `json:"trace_id,omitempty"`
	ParentLogID            string        `json:"parent_log_id,omitempty"`
	TraceSeq               int           `json:"trace_seq,omitempty"`
	RequestOverrideApplied bool          `json:"request_override_applied,omitempty"`
	RequestOverrideRules   []string      `json:"request_override_rules,omitempty"`
	RequestOverrideError   string        `json:"request_override_error,omitempty"`
	UsageInputTokens       *int64        `json:"usage_input_tokens,omitempty"`
	UsageOutputTokens      *int64        `json:"usage_output_tokens,omitempty"`
	UsageTotalTokens       *int64        `json:"usage_total_tokens,omitempty"`
	UsageRaw               string        `json:"usage_raw,omitempty"`
	UsageSource            string        `json:"usage_source,omitempty"`
	Annotation             LogAnnotation `json:"annotation"`

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
	DeleteLogsBefore(before time.Time) (int64, error)        // 返回删除数量
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
