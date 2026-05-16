//go:build !linux && !windows

package systemmetrics

func processRSSBytes() *uint64 {
	return nil
}

func systemMemory() MemoryMetrics {
	return MemoryMetrics{Source: "unavailable"}
}
