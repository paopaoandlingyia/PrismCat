//go:build windows

package systemmetrics

import "unsafe"

var globalMemoryStatusEx = kernel32Metrics.NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func systemMemory() MemoryMetrics {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return MemoryMetrics{Source: "unavailable"}
	}

	used := status.TotalPhys - status.AvailPhys
	return MemoryMetrics{
		TotalBytes:     &status.TotalPhys,
		UsedBytes:      &used,
		AvailableBytes: &status.AvailPhys,
		Source:         "host",
	}
}
