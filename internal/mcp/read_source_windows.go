//go:build windows

package mcp

// read_source_windows.go — plain os.Open path for readSourceWindow on Windows.
//
// Windows does not have fsevents / kqueue kernel-stall behaviour, so the
// non-blocking syscall.Open dance (darwin || linux) is not needed. Plain
// os.Open is sufficient; the 5s context deadline in handleGetNodeSource
// remains the only safety net, which is fine on Windows.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// readSourceWindow opens path, scans lines [start,end] (1-indexed inclusive),
// and returns the formatted text.
func readSourceWindow(path string, start, end int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	var b strings.Builder
	line := 0
	for scanner.Scan() {
		line++
		if line < start {
			continue
		}
		if line > end {
			break
		}
		b.WriteString(fmt.Sprintf("%5d  %s\n", line, scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
