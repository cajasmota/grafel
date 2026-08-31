//go:build unix

package safeio

// report_6478_test.go — the reporting half of #6478.
//
// docs/blocking-open-audit.md is explicit that routing alone is not the fix:
// rounds 2 and 3 of #6416 routed two packages and then mapped ErrNotRegular to
// a bare `return nil`, so `mkfifo creds.go` produced a secrets scan that
// answered "clean" without ever reading creds.go. The hang was closed and the
// silence was kept. So these assertions are about the STDERR LINE, not about a
// counter the package keeps about itself: the artefact is what a user sees.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// captureStderr swaps os.Stderr for a pipe. fmt.Fprintf reads the variable at
// call time, so this reaches the reporter without it needing a seam.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func fifo(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestReadFileReportedNamesTheRefusedPath(t *testing.T) {
	resetSkipReportsForTest()
	dir := t.TempDir()
	p := fifo(t, dir, "CLAUDE.md")

	var err error
	out := captureStderr(t, func() {
		_, err = ReadFileReported(p, FollowSymlinks, MaxConfigFileBytes)
	})
	if err == nil {
		t.Fatal("ReadFileReported returned a nil error for a FIFO")
	}
	if !strings.Contains(out, p) {
		t.Fatalf("stderr did not name the refused path.\ngot: %q\nwant a line containing %q\n\n"+
			"A warning that names no file is not a safety net — the user cannot tell WHICH file "+
			"vanished from a message that does not say.", out, p)
	}
	if !strings.Contains(out, "named-pipe") {
		t.Fatalf("stderr did not say WHY the file was refused.\ngot: %q\nwant the entry kind "+
			"(\"named-pipe\") — that is the whole difference between a diagnosis and a shrug.", out)
	}
}

func TestReportSkipIsSilentForENOENT(t *testing.T) {
	resetSkipReportsForTest()
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")

	out := captureStderr(t, func() {
		_, _ = ReadFileReported(missing, FollowSymlinks, MaxConfigFileBytes)
	})
	if out != "" {
		t.Fatalf("ReadFileReported reported an ordinary missing file: %q.\n\n"+
			"Nearly every #6478 site probes for a file it expects to be absent (.gitignore, "+
			"CLAUDE.md, .grafel/group.json). Announcing ENOENT would emit several lines per "+
			"healthy repo and train everyone to ignore the channel.", out)
	}
}

func TestReportSkipDeduplicatesByPath(t *testing.T) {
	resetSkipReportsForTest()
	dir := t.TempDir()
	p := fifo(t, dir, "AGENTS.md")

	out := captureStderr(t, func() {
		for i := 0; i < 5; i++ {
			_, _ = ReadFileReported(p, FollowSymlinks, MaxConfigFileBytes)
		}
	})
	// Counted by LINE, not by substring: the path appears twice in one line —
	// once as the reporter's own prefix and once inside ErrNotRegular's own
	// message — and a substring count would make this assertion say something
	// other than what its name claims.
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(l, p) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("path reported on %d lines over 5 reads, want exactly 1:\n%s", n, out)
	}
}

func TestReportSkipCapsAndSaysSo(t *testing.T) {
	resetSkipReportsForTest()
	dir := t.TempDir()

	out := captureStderr(t, func() {
		for i := 0; i < maxSkipReports+3; i++ {
			p := fifo(t, dir, "f"+string(rune('a'+i))+".json")
			_, _ = ReadFileReported(p, FollowSymlinks, MaxConfigFileBytes)
		}
	})
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "grafel: skipped ") {
			lines++
		}
	}
	if lines != maxSkipReports {
		t.Fatalf("emitted %d skip lines, want exactly %d — an uncapped report turns a hostile tree "+
			"into the denial-of-service it is warning about", lines, maxSkipReports)
	}
	if !strings.Contains(out, "suppressed") {
		t.Fatalf("the cap fired but said nothing.\ngot: %q\n\nSilent truncation is the same defect "+
			"one layer up: the reader believes they saw everything.", out)
	}
}

// TestOpenReportedReportsToo pins the *os.File form, which three #6478 sites
// use (internal/dashboard, internal/engine, internal/coverage). It is a
// separate function from ReadFileReported and would otherwise be reported by
// nothing.
func TestOpenReportedReportsToo(t *testing.T) {
	resetSkipReportsForTest()
	dir := t.TempDir()
	p := fifo(t, dir, "application.properties")

	var f *os.File
	out := captureStderr(t, func() {
		f, _ = OpenReported(p, FollowSymlinks)
	})
	if f != nil {
		t.Fatal("OpenReported returned a non-nil *os.File for a FIFO")
	}
	if !strings.Contains(out, p) {
		t.Fatalf("OpenReported did not report the refusal: %q", out)
	}
}
