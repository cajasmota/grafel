package errors

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI colour escapes — only emitted when the writer is a TTY.
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31;1m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiBold   = "\033[1m"
)

// isTTY returns true when w is os.Stderr or os.Stdout attached to a terminal.
// We check the concrete type rather than calling Fd() on an arbitrary writer so
// tests that pass a bytes.Buffer get plain-text output without escape codes.
func isTTY(w io.Writer) bool {
	if w == os.Stdout || w == os.Stderr {
		fi, err := os.Stdout.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// Format writes a human-readable, optionally coloured rendering of ie to w.
//
// Example output (plain):
//
//	error IDX-002  file is 42 MiB
//	       --> /repo/vendor/huge_generated.js
//
//	hint:
//	  This file exceeds the 10 MiB single-file limit. Add it to
//	  .archigraphignore to skip it, or split the file.
//
//	see: https://archigraph.dev/docs/errors/IDX-002
func Format(w io.Writer, ie *IndexerError) {
	if ie == nil {
		return
	}
	color := isTTY(w)

	bold := func(s string) string {
		if color {
			return ansiBold + s + ansiReset
		}
		return s
	}
	red := func(s string) string {
		if color {
			return ansiRed + s + ansiReset
		}
		return s
	}
	yellow := func(s string) string {
		if color {
			return ansiYellow + s + ansiReset
		}
		return s
	}
	cyan := func(s string) string {
		if color {
			return ansiCyan + s + ansiReset
		}
		return s
	}

	// Header line
	fmt.Fprintf(w, "%s %s  %s\n", red("error"), bold(string(ie.Code)), ie.Message)

	// File location
	if ie.FilePath != "" {
		if ie.Line > 0 {
			fmt.Fprintf(w, "       %s %s:%d\n", yellow("-->"), ie.FilePath, ie.Line)
		} else {
			fmt.Fprintf(w, "       %s %s\n", yellow("-->"), ie.FilePath)
		}
	}

	// Hint
	if ie.Hint != "" {
		fmt.Fprintf(w, "\n%s\n", cyan("hint:"))
		for _, line := range wrapText(ie.Hint, 72) {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	// Docs
	if ie.DocsURL != "" {
		fmt.Fprintf(w, "\n%s %s\n", cyan("see:"), ie.DocsURL)
	}

	fmt.Fprintln(w)
}

// FormatToStderr is a convenience wrapper that writes to os.Stderr.
func FormatToStderr(ie *IndexerError) {
	Format(os.Stderr, ie)
}

// FormatError formats any error: if it is (or wraps) an *IndexerError it uses
// the rich format; otherwise it falls back to a plain error line.
func FormatError(w io.Writer, err error) {
	if err == nil {
		return
	}
	var ie *IndexerError
	if As(err, &ie) {
		Format(w, ie)
		return
	}
	fmt.Fprintf(w, "error: %s\n\n", err.Error())
}

// wrapText breaks s into lines of at most width bytes at word boundaries.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > width {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return lines
}
