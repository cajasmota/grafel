package detect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The #6416 re-review's last open finding for this package: all three workspace
// parsers closed the hang but kept the silence. Each mapped both
// safeio.ErrNotRegular and safeio.ErrWouldBlock to a bare `return nil`, so
// `mkfifo package.json` turned a monorepo into a single-package repo with no
// record anywhere — the #6338 shape this PR invokes as its own rationale.
//
// PINNED HERE: the gate (both reportable errors announced, ordinary errors
// silent) and path attribution in both error shapes. PINNED BY
// TestDetectMonorepoReportsFifoManifest: that a real refused manifest reaches
// the reporter through all three parsers.

// TestManifestSkipReportsBothReportableErrors kills the mutant that drops
// either arm of reportManifestSkip's gate.
//
// The ErrWouldBlock arm needs a direct-call case because it is unreachable from
// `go test` on a healthy filesystem — nonBlockingOpen passes O_NONBLOCK, so a
// syscall.Mkfifo target opens immediately and is refused by fstat as
// ErrNotRegular; the real producers are semaphore exhaustion and hung
// network/FUSE mounts. That is precisely why this arm survived a mutant in all
// three earlier reporters.
//
// The "bare" case is the shape openWithDeadline actually returns: ErrWouldBlock
// carries NO path, unlike ErrNotRegular, so withPath's decoration is asserted
// rather than assumed. A warning that names no file is not a safety net.
func TestManifestSkipReportsBothReportableErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.json")

	for name, err := range map[string]error{
		"not-regular":         fmt.Errorf("safeio: %s: %w (fifo)", path, safeio.ErrNotRegular),
		"would-block-bare":    safeio.ErrWouldBlock, // exactly what openWithDeadline returns
		"would-block-wrapped": fmt.Errorf("read %s: %w", path, safeio.ErrWouldBlock),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setManifestSkipOutput(&buf)
			defer restore()

			reportManifestSkip(path, err)

			got := buf.String()
			if got == "" {
				t.Fatalf("skip of %s (%v) was reported NOWHERE", path, err)
			}
			if !strings.Contains(got, path) {
				t.Errorf("skip report does not name the path %q; got %q", path, got)
			}
			if !strings.Contains(got, "#6416") {
				t.Errorf("skip report does not cite the issue; got %q", got)
			}
		})
	}
}

// TestManifestSkipIgnoresOrdinaryErrors pins the other side of the gate. ENOENT
// is the overwhelmingly common case here — most repos have no lerna.json or
// pnpm-workspace.yaml, and plenty have no package.json at all — so announcing
// it would print a warning on nearly every index and train users to ignore the
// one line that matters.
func TestManifestSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.json")

	for name, err := range map[string]error{
		"not-exist":  fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{Op: "open", Path: path, Err: os.ErrPermission},
		"unrelated":  errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setManifestSkipOutput(&buf)
			defer restore()

			reportManifestSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}

// TestManifestSkipIsDedupedAndCapped pins the two properties that keep an
// always-on warning readable: one line per path, and a ceiling.
func TestManifestSkipIsDedupedAndCapped(t *testing.T) {
	dir := t.TempDir()
	notRegular := func(p string) error {
		return fmt.Errorf("safeio: %s: %w (fifo)", p, safeio.ErrNotRegular)
	}

	var buf strings.Builder
	restore := setManifestSkipOutput(&buf)
	defer restore()

	same := filepath.Join(dir, "package.json")
	for i := 0; i < 5; i++ {
		reportManifestSkip(same, notRegular(same))
	}
	if n := strings.Count(buf.String(), same); n != 1 {
		t.Errorf("the same path was reported %d times; want 1 (deduped)", n)
	}

	for i := 0; i < maxManifestSkipReports+10; i++ {
		p := filepath.Join(dir, fmt.Sprintf("r%d", i), "package.json")
		reportManifestSkip(p, notRegular(p))
	}
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(l, "skipped workspace manifest") {
			lines++
		}
	}
	if lines > maxManifestSkipReports {
		t.Errorf("emitted %d skip lines; cap is %d", lines, maxManifestSkipReports)
	}
	if !strings.Contains(buf.String(), "suppressed after") {
		t.Errorf("cap was reached but never announced; got %q", buf.String())
	}
}
