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
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	Query           string              `json:"query,omitempty"`
	RequestHeaders  map[string][]string `json:"request_headers,omitempty"`
	RequestBody     string              `json:"request_body,omitempty"`
	RequestBodyRef  string              `json:"request_body_ref,omitempty"`
	RequestBodySize int64               `json:"request_body_size"`

	// 响应信息
	StatusCode       int                 `json:"status_code"`
	ResponseHeaders  map[string][]string `json:"response_headers,omitempty"`
	ResponseBody     string              `json:"response_body,omitempty"`
	ResponseBodyRef  string              `json:"response_body_ref,omitempty"`
	ResponseBodySize int64               `json:"response_body_size"`

	// 元数据
	Streaming bool   `json:"streaming"`       // 是否为流式响应
	Latency   int64  `json:"latency_ms"`      // 响应延迟(毫秒)
	Error     string `json:"error,omitempty"` // 错误信息
	Truncated bool   `json:"truncated"`       // 响应体是否被截断
	Tag       string `json:"tag,omitempty"`   // 来自 X-PrismCat-Tag 请求头
}

// LogFilter 日志查询过滤器
type LogFilter struct {
	Upstream   string     // 按上游名称过滤
	Method     string     // 按请求方法过滤
	StatusCode int        // 按状态码过滤
	Path       string     // 按路径模糊搜索
	Tag        string     // 按标签过滤
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

// Repository 存储接口
type Repository interface {
	// 日志操作
	SaveLog(log *RequestLog) error
	GetLog(id string) (*RequestLog, error)
	ListLogs(filter LogFilter) ([]*RequestLog, int64, error) // 返回日志列表和总数
	DeleteLogsBefore(before time.Time) (int64, error)        // 返回删除数量

	// 统计
	GetStats(since *time.Time) (*LogStats, error)

	// 生命周期
	Close() error
}
