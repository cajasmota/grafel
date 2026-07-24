//go:build linux

package process

import (
	"os"
	"strconv"
	"strings"
)

// TotalMemoryMB returns the total physical memory of the host in megabytes.
// On Linux it reads MemTotal from /proc/meminfo. Returns 0 on failure.
func TotalMemoryMB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		// Format: "MemTotal:       16384000 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			break
		}
		return kb / 1024
	}
	return 0
}

// AvailableMemoryMB returns the memory available for a new allocation without
// swapping, in megabytes. On Linux it reads MemAvailable from /proc/meminfo
// (the kernel's own estimate, which already accounts for reclaimable page
// cache). Returns 0 when it cannot be determined; callers must treat 0 as
// "unknown" and fall back rather than guessing (#5954).
func AvailableMemoryMB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		// Format: "MemAvailable:   8123456 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			break
		}
		return kb / 1024
	}
	return 0
}
