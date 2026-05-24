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
