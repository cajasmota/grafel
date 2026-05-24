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
