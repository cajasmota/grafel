// Package mcp — read_source_export.go
//
// Exports readSourceWindow (defined in the build-tag-split files
// read_source_unix.go / read_source_other.go) so that sibling packages
// such as internal/docgen can reuse the cross-platform implementation
// without duplicating the fsevents-defense logic.
//
// The exported name is ReadSourceWindow; the behaviour is identical to the
// unexported readSourceWindow used by handleGetNodeSource.
package mcp

// ReadSourceWindow opens path and returns a formatted excerpt of lines
// [start, end] (1-indexed, inclusive). It delegates to the platform-split
// readSourceWindow implementation which applies the macOS fsevents O_NONBLOCK
// defense on darwin/linux and plain os.Open on Windows.
//
// On error (file not found, fsevents stall, permission denied) the error is
// returned to the caller; the caller is responsible for deciding whether a
// missing source window is fatal or a non-fatal warning.

import (
	"bufio"
	"strings"
)
func ReadSourceWindow(path string, start, end int) (string, error) {
	return readSourceWindow(path, start, end)
}

// readRawSourceWindow returns lines [start,end] (1-indexed, inclusive) of path
// WITHOUT the line-number prefix that readSourceWindow prepends. Used by the
// #4423 branches facet, whose analyzers regex over verbatim source — the
// "%5d  " prefix would corrupt indentation-sensitive Python parsing. Reuses the
// shared cross-platform openSourceFile (and thus the macOS fsevents defense).
func readRawSourceWindow(path string, start, end int) (string, error) {
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
		b.WriteString(scanner.Text())
		b.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
