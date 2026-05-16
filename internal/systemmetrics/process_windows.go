//go:build windows

package systemmetrics

import (
	"syscall"
	"unsafe"
)

var (
	kernel32Metrics = syscall.NewLazyDLL("kernel32.dll")
	psapiMetrics    = syscall.NewLazyDLL("psapi.dll")

	getCurrentProcess    = kernel32Metrics.NewProc("GetCurrentProcess")
	getProcessTimes      = kernel32Metrics.NewProc("GetProcessTimes")
	getProcessMemoryInfo = psapiMetrics.NewProc("GetProcessMemoryInfo")
)

type filetime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

func processCPUSeconds() *float64 {
	handle, _, _ := getCurrentProcess.Call()
	var createTime, exitTime, kernelTime, userTime filetime
	ret, _, _ := getProcessTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&createTime)),
		uintptr(unsafe.Pointer(&exitTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return nil
	}

	kernelTicks := uint64(kernelTime.HighDateTime)<<32 | uint64(kernelTime.LowDateTime)
	userTicks := uint64(userTime.HighDateTime)<<32 | uint64(userTime.LowDateTime)
	seconds := float64(kernelTicks+userTicks) / 10_000_000
	return &seconds
}

func processRSSBytes() *uint64 {
	handle, _, _ := getCurrentProcess.Call()
	counters := processMemoryCountersEx{}
	counters.CB = uint32(unsafe.Sizeof(counters))
	ret, _, _ := getProcessMemoryInfo.Call(
		handle,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if ret == 0 {
		return nil
	}
	value := uint64(counters.WorkingSetSize)
	return &value
}
