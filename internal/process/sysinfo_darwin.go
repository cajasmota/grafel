//go:build darwin

package process

import (
	"os/exec"
	"strconv"
	"strings"
)

// TotalMemoryMB returns the total physical memory of the host in megabytes.
// On macOS it reads hw.memsize via sysctl. Returns 0 on failure.
func TotalMemoryMB() int64 {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || bytes <= 0 {
		return 0
	}
	return bytes / (1024 * 1024)
}

// AvailableMemoryMB returns the memory available for a new allocation without
// paging, in megabytes. macOS exposes no single MemAvailable counter, so we sum
// the page classes the kernel can hand out immediately — free, inactive,
// speculative and purgeable — from vm_stat, mirroring what Activity Monitor
// treats as reclaimable. Returns 0 when it cannot be determined; callers must
// treat 0 as "unknown" and fall back rather than guessing (#5954).
func AvailableMemoryMB() int64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	pageSize := int64(4096)
	var pages int64
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics:") {
			// "... (page size of 16384 bytes)"
			if i := strings.Index(line, "page size of "); i >= 0 {
				f := strings.Fields(line[i+len("page size of "):])
				if len(f) > 0 {
					if n, err := strconv.ParseInt(f[0], 10, 64); err == nil && n > 0 {
						pageSize = n
					}
				}
			}
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pages free", "Pages inactive", "Pages speculative", "Pages purgeable":
		default:
			continue
		}
		val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "."))
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil || n < 0 {
			continue
		}
		pages += n
	}
	if pages <= 0 {
		return 0
	}
	return pages * pageSize / (1024 * 1024)
}
