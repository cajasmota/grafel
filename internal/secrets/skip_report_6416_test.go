package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// The #6416 re-review's last open finding: internal/secrets closed the hang but
// kept the silence. scanFile's safeio.Open error was mapped to a bare
// `return nil // skip unreadable files silently` in ScanPath, so a FIFO named
// creds.go produced a "clean" scan with no record anywhere — the #6338 shape
// this PR argues against, on the one path reachable from the daemon's MCP tool
// and an HTTP dashboard handler.
//
// PINNED HERE: the gate itself (both reportable errors announced, ordinary
// errors silent), and that the line names the file in BOTH error shapes.
// PINNED BY TestScanPathReportsFifoSkip below: that a real refused file reaches
// the reporter at all — the wiring, end to end through ScanPath.

// TestSecretSkipReportsBothReportableErrors kills the mutant that drops either
// arm of reportSecretScanSkip's gate.
//
// The ErrWouldBlock arm needs its own case because it is not reachable from
// `go test` on a healthy filesystem: nonBlockingOpen passes O_NONBLOCK, so the
// one hanging object a test can create (syscall.Mkfifo) opens IMMEDIATELY and
// is refused by fstat as ErrNotRegular instead. Its genuine producers are
// semaphore exhaustion and hung network/FUSE mounts. Dropping that arm
// therefore costs nothing an end-to-end test would notice, which is exactly why
// it survived a mutant in all three earlier reporters and has to be pinned
// directly.
//
// The "bare" case is the shape openWithDeadline actually returns: ErrWouldBlock
// carries NO path, unlike ErrNotRegular. A warning that names no file is not a
// safety net, so withPath's decoration is asserted here rather than assumed.
func TestSecretSkipReportsBothReportableErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.go")

	for name, err := range map[string]error{
		"not-regular":        fmt.Errorf("safeio: %s: %w (fifo)", path, safeio.ErrNotRegular),
		"would-block-bare":   safeio.ErrWouldBlock, // exactly what openWithDeadline returns
		"would-block-wrappd": fmt.Errorf("open %s: %w", path, safeio.ErrWouldBlock),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setSecretSkipOutput(&buf)
			defer restore()

			reportSecretScanSkip(path, err)

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

// TestSecretSkipIgnoresOrdinaryErrors pins the other side of the gate. ENOENT
// is the ORDINARY case on this path — ScanPath walks a live tree, and files are
// deleted between readdir and open all the time — so announcing it would turn
// the report into noise people learn to ignore. Permission errors are the same:
// a mode-000 file is a user's own choice, not a hang.
func TestSecretSkipIgnoresOrdinaryErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.go")

	for name, err := range map[string]error{
		"not-exist":  fmt.Errorf("open %s: %w", path, os.ErrNotExist),
		"permission": &os.PathError{Op: "open", Path: path, Err: os.ErrPermission},
		"unrelated":  errors.New("some other failure"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			restore := setSecretSkipOutput(&buf)
			defer restore()

			reportSecretScanSkip(path, err)

			if got := buf.String(); got != "" {
				t.Errorf("ordinary error %v was announced as a skip: %q", err, got)
			}
		})
	}
}

// TestSecretSkipIsDedupedAndCapped pins the two properties that keep an
// always-on warning readable: one line per path however often it is refused,
// and a hard ceiling so a tree full of device nodes cannot bury the scan
// output. The cap matters more here than at the name-chosen read sites — those
// see a handful of paths per repo, this one walks a whole tree.
func TestSecretSkipIsDedupedAndCapped(t *testing.T) {
	dir := t.TempDir()
	notRegular := func(p string) error {
		return fmt.Errorf("safeio: %s: %w (fifo)", p, safeio.ErrNotRegular)
	}

	var buf strings.Builder
	restore := setSecretSkipOutput(&buf)
	defer restore()

	same := filepath.Join(dir, "creds.go")
	for i := 0; i < 5; i++ {
		reportSecretScanSkip(same, notRegular(same))
	}
	if n := strings.Count(buf.String(), same); n != 1 {
		t.Errorf("the same path was reported %d times; want 1 (deduped)", n)
	}

	for i := 0; i < maxSecretSkipReports+10; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		reportSecretScanSkip(p, notRegular(p))
	}
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(l, "skipped secret scan of") {
			lines++
		}
	}
	if lines > maxSecretSkipReports {
		t.Errorf("emitted %d skip lines; cap is %d", lines, maxSecretSkipReports)
	}
	if !strings.Contains(buf.String(), "suppressed after") {
		t.Errorf("cap was reached but never announced; got %q", buf.String())
	}
}
