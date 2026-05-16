package systemmetrics

import (
	"os"
	"runtime"
	"sync"
	"time"
)

type Collector struct {
	startedAt time.Time
	mu        sync.Mutex
	lastCPU   *cpuSample
}

type cpuSample struct {
	at      time.Time
	seconds float64
}

func NewCollector() *Collector {
	return &Collector{startedAt: time.Now()}
}

func (c *Collector) Snapshot() Metrics {
	now := time.Now()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	cpuSeconds := processCPUSeconds()
	cpuPercent := c.cpuPercent(now, cpuSeconds)
	rssBytes := processRSSBytes()

	return Metrics{
		Timestamp: now,
		Platform:  runtime.GOOS,
		Runtime: RuntimeMetrics{
			GoVersion:     runtime.Version(),
			NumCPU:        runtime.NumCPU(),
			Goroutines:    runtime.NumGoroutine(),
			UptimeSeconds: now.Sub(c.startedAt).Seconds(),
		},
		Process: ProcessMetrics{
			PID:            os.Getpid(),
			RSSBytes:       rssBytes,
			HeapAllocBytes: mem.HeapAlloc,
			HeapSysBytes:   mem.HeapSys,
			CPUSeconds:     cpuSeconds,
			CPUPercent:     cpuPercent,
		},
		Memory: systemMemory(),
	}
}

func (c *Collector) cpuPercent(now time.Time, seconds *float64) *float64 {
	if seconds == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	current := cpuSample{at: now, seconds: *seconds}
	if c.lastCPU == nil {
		c.lastCPU = &current
		return nil
	}

	elapsed := now.Sub(c.lastCPU.at).Seconds()
	used := current.seconds - c.lastCPU.seconds
	c.lastCPU = &current
	if elapsed <= 0 || used < 0 {
		return nil
	}

	percent := used / elapsed * 100
	if percent < 0.01 {
		percent = 0
	}
	return &percent
}
