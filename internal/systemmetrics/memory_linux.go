//go:build linux

package systemmetrics

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processRSSBytes() *uint64 {
	values, err := readProcStatusValues("/proc/self/status", "VmRSS")
	if err != nil {
		return nil
	}
	return values["VmRSS"]
}

func systemMemory() MemoryMetrics {
	host := hostMemory()
	cgroup := cgroupMemory()
	if cgroup.TotalBytes != nil && cgroup.UsedBytes != nil {
		return cgroup
	}
	return host
}

func hostMemory() MemoryMetrics {
	values, err := readMeminfo("/proc/meminfo")
	if err != nil {
		return MemoryMetrics{Source: "unavailable"}
	}

	total := values["MemTotal"]
	available := values["MemAvailable"]
	var used *uint64
	if total != nil && available != nil && *total >= *available {
		value := *total - *available
		used = &value
	}

	return MemoryMetrics{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		Source:         "host",
	}
}

func cgroupMemory() MemoryMetrics {
	current := readFirstUint(cgroupFileCandidates("memory.current", "memory.usage_in_bytes")...)
	limit := readFirstUint(cgroupFileCandidates("memory.max", "memory.limit_in_bytes")...)
	if current == nil || limit == nil || *limit == 0 || *limit > 1<<60 {
		return MemoryMetrics{Source: "unavailable"}
	}

	var available *uint64
	if *limit >= *current {
		value := *limit - *current
		available = &value
	}
	return MemoryMetrics{
		TotalBytes:     limit,
		UsedBytes:      current,
		AvailableBytes: available,
		Source:         "cgroup",
	}
}

func cgroupFileCandidates(v2Name string, v1Name string) []string {
	v2Path, v1MemoryPath := readSelfCgroupPaths()
	paths := make([]string, 0, 4)
	if v2Path != "" {
		paths = append(paths, filepath.Join("/sys/fs/cgroup", cleanCgroupPath(v2Path), v2Name))
	}
	if v1MemoryPath != "" {
		paths = append(paths, filepath.Join("/sys/fs/cgroup/memory", cleanCgroupPath(v1MemoryPath), v1Name))
	}
	paths = append(paths,
		filepath.Join("/sys/fs/cgroup", v2Name),
		filepath.Join("/sys/fs/cgroup/memory", v1Name),
	)
	return paths
}

func readSelfCgroupPaths() (v2Path string, v1MemoryPath string) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			v2Path = parts[2]
			continue
		}
		for _, controller := range strings.Split(parts[1], ",") {
			if controller == "memory" {
				v1MemoryPath = parts[2]
				break
			}
		}
	}
	return v2Path, v1MemoryPath
}

func cleanCgroupPath(path string) string {
	return strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
}

func readProcStatusValues(path string, keys ...string) (map[string]*uint64, error) {
	want := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		want[key] = struct{}{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*uint64, len(keys))
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, ok := want[name]; !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		value := kb * 1024
		result[name] = &value
	}
	return result, nil
}

func readMeminfo(path string) (map[string]*uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := map[string]*uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		value := kb * 1024
		result[name] = &value
	}
	return result, nil
}

func readFirstUint(paths ...string) *uint64 {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" || text == "max" {
			continue
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			continue
		}
		return &value
	}
	return nil
}
