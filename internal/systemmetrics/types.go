package systemmetrics

import "time"

type Metrics struct {
	Timestamp time.Time      `json:"timestamp"`
	Platform  string         `json:"platform"`
	Runtime   RuntimeMetrics `json:"runtime"`
	Process   ProcessMetrics `json:"process"`
	Memory    MemoryMetrics  `json:"memory"`
}

type RuntimeMetrics struct {
	GoVersion     string  `json:"go_version"`
	NumCPU        int     `json:"num_cpu"`
	Goroutines    int     `json:"goroutines"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

type ProcessMetrics struct {
	PID            int      `json:"pid"`
	RSSBytes       *uint64  `json:"rss_bytes,omitempty"`
	HeapAllocBytes uint64   `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64   `json:"heap_sys_bytes"`
	CPUSeconds     *float64 `json:"cpu_seconds,omitempty"`
	CPUPercent     *float64 `json:"cpu_percent,omitempty"`
}

type MemoryMetrics struct {
	TotalBytes     *uint64 `json:"total_bytes,omitempty"`
	UsedBytes      *uint64 `json:"used_bytes,omitempty"`
	AvailableBytes *uint64 `json:"available_bytes,omitempty"`
	Source         string  `json:"source"`
}
