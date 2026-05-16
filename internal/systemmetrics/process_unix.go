//go:build !windows

package systemmetrics

import "syscall"

func processCPUSeconds() *float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return nil
	}
	seconds := float64(usage.Utime.Sec) +
		float64(usage.Utime.Usec)/1_000_000 +
		float64(usage.Stime.Sec) +
		float64(usage.Stime.Usec)/1_000_000
	return &seconds
}
