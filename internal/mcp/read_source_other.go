//go:build !darwin && !linux

package mcp

// read_source_other.go — plain os.Open implementation of openSourceFile and
// readSourceWindow for platforms without the macOS fsevents kernel-stall
// concern (windows, the BSDs, and anything future).
//
// Off-Darwin there is no fsevents stall, so the non-blocking syscall.Open +
// semaphore defense in read_source_unix.go is unnecessary. Plain os.Open is
// sufficient; the 5s context deadline in handleGetNodeSource remains the only
// safety net, which is fine here. The function signatures match the unix
// variants exactly so callers (handleGetNodeSource, readSourceLines) are
// platform-agnostic.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// openSourceFile opens path for reading. It performs the same Layer-1
// regular-file check as the unix variant, then a plain os.Open — no
// non-blocking open or semaphore is needed off-Darwin. The caller must Close
// the returned *os.File.
func openSourceFile(path string) (*os.File, error) {
	// Layer 1 parity: reject non-existent or non-regular paths up front so a
	// directory / device path fails the same way it does on unix.
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ENOTSUP}
	}
	return os.Open(path)
}

// readSourceWindow opens path, scans lines [start,end] (1-indexed inclusive),
// and returns the formatted text.
func readSourceWindow(path string, start, end int) (string, error) {
	f, err := openSourceFile(path)
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
