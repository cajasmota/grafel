//go:build unix

package walk

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// walkFIFODeadline bounds each subtest below. A correctly-gated read returns in
// microseconds — safeio's stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine. It is well under the package test
// timeout so a regression FAILS with attribution rather than wedging the suite
// until the watchdog kills the binary with no clue which test parked.
const walkFIFODeadline = 10 * time.Second

// mkfifoInWalkTemp creates a named pipe at dir/rel, where dir MUST be a
// t.TempDir(). A FIFO outside a temp dir outlives the test and hangs any other
// process that later walks over it, so the root temp DIRECTORY is taken
// separately from the relative path and this helper cannot be pointed
// elsewhere by accident.
func mkfifoInWalkTemp(t *testing.T, dir string, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{dir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", p, err)
	}
	// t.TempDir's RemoveAll unlinks a FIFO without opening it, so cleanup is
	// already correct; this is belt-and-braces against a future refactor that
	// stops using t.TempDir.
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// mustReturnWalk runs fn and fails if it has not returned within
// walkFIFODeadline. Under the pre-fix code a bare call parks in open(2)
// forever, which wedges the whole test binary AND leaves t.TempDir's cleanup
// unrun — leaking a FIFO onto a shared machine. Running fn on its own
// goroutine means the deadline fires, cleanup runs, and the failure names the
// call that hung.
func mustReturnWalk(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(walkFIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, walkFIFODeadline)
	}
}

// TestParseInheritedGrafelIgnoreFIFODoesNotHang is the #6416 regression for the
// read this PR's own package still did with os.ReadFile.
//
// The entry-type gate #6468 added protects entries the WALKER produced. This
// read is name-chosen — ".grafelignore" joined onto a parent directory — and
// runs BEFORE the walk starts, so no gate sits in front of it. `mkfifo
// .grafelignore` in any ancestor directory of an indexed subdirectory parked
// `grafel index` in open(2) permanently: no timeout, no error, no log line.
//
// It is a hang, so the honest pin is a deadline: the pre-fix code never
// produced a value to assert on.
func TestParseInheritedGrafelIgnoreFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	mkfifoInWalkTemp(t, dir, ".grafelignore")

	var got *IgnoreFile
	mustReturnWalk(t, "parseInheritedGrafelIgnore with a FIFO .grafelignore", func() {
		got = parseInheritedGrafelIgnore(dir, "sub")
	})
	// A refused file must degrade to the same "no patterns inherited"
	// behaviour an absent one already produces.
	if got != nil {
		t.Errorf("parseInheritedGrafelIgnore = %+v, want nil for a refused .grafelignore", got)
	}
}

// TestParseInheritedGrafelIgnoreStillReadsRegularFiles is the liveness half.
// A gate that refuses everything also never hangs, so the deadline test above
// is only meaningful next to this one.
func TestParseInheritedGrafelIgnoreStillReadsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".grafelignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := parseInheritedGrafelIgnore(dir, "sub")
	if got == nil || len(got.patterns) == 0 {
		t.Fatalf("parseInheritedGrafelIgnore = %+v, want the parsed patterns of a regular file", got)
	}
}

// TestInheritedGrafelIgnoresFIFODoesNotHang is the same hang reached through
// the real entry point rather than the helper, in the layout a user actually
// hits: a git repo whose ROOT carries the hostile .grafelignore, with the
// indexed path a subdirectory below it. WalkRepo calls inheritedGrafelIgnores
// on every run, so this is `grafel index` itself and not only a helper.
func TestInheritedGrafelIgnoresFIFODoesNotHang(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available; inheritedGrafelIgnores needs a real checkout to resolve TopLevel")
	}
	cmd := exec.Command("git", "init", "-q", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init failed (%v): %s", err, out)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfifoInWalkTemp(t, root, ".grafelignore")

	var got []*IgnoreFile
	mustReturnWalk(t, "inheritedGrafelIgnores over a repo root with a FIFO .grafelignore", func() {
		got = inheritedGrafelIgnores(sub)
	})
	if len(got) != 0 {
		t.Errorf("inheritedGrafelIgnores = %d ignore file(s), want 0 for a refused .grafelignore", len(got))
	}
}

// TestIgnoreSkipIsReported pins the report, not just the absence of the hang.
// A silent skip is #6338's shape: the user sees a .grafelignore in their tree
// whose rules stopped applying, with nothing on stderr saying why. The line
// must name the path, or it tells a reader nothing they can act on.
func TestIgnoreSkipIsReported(t *testing.T) {
	var buf bytes.Buffer
	restore := setIgnoreSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	path := mkfifoInWalkTemp(t, dir, ".grafelignore")

	mustReturnWalk(t, "parseInheritedGrafelIgnore with a FIFO .grafelignore", func() {
		_ = parseInheritedGrafelIgnore(dir, "sub")
	})

	out := buf.String()
	if !strings.Contains(out, path) {
		t.Errorf("skip report = %q, want it to name %q", out, path)
	}
	if !strings.Contains(out, "6416") {
		t.Errorf("skip report = %q, want it to cite #6416", out)
	}
}

// TestIgnoreSkipReportIsDedupedAndCapped keeps the always-on report from
// becoming the noise it exists to cut through: one line per distinct path, and
// no more than maxIgnoreSkipReports of them however many hostile ancestors a
// deep tree has.
func TestIgnoreSkipReportIsDedupedAndCapped(t *testing.T) {
	var buf bytes.Buffer
	restore := setIgnoreSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	// Same path twice: the second must not print.
	mkfifoInWalkTemp(t, dir, ".grafelignore")
	for i := 0; i < 2; i++ {
		mustReturnWalk(t, "repeat read", func() { _ = parseInheritedGrafelIgnore(dir, "sub") })
	}
	if n := strings.Count(buf.String(), "skipped"); n != 1 {
		t.Errorf("same path read twice produced %d skip lines, want 1", n)
	}

	for i := 0; i < maxIgnoreSkipReports+5; i++ {
		d := filepath.Join(dir, "d", string(rune('a'+i%26)), string(rune('a'+i/26)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(filepath.Join(d, ".grafelignore"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustReturnWalk(t, "capped read", func() { _ = parseInheritedGrafelIgnore(d, "sub") })
	}
	if n := strings.Count(buf.String(), "grafel: skipped"); n > maxIgnoreSkipReports {
		t.Errorf("emitted %d skip lines, want at most %d", n, maxIgnoreSkipReports)
	}
	if !strings.Contains(buf.String(), "suppressed") {
		t.Errorf("report never announced suppression despite exceeding the cap: %q", buf.String())
	}
}

// TestIgnoreSkipNotReportedForAbsentFile is the noise guard. A missing
// .grafelignore is the ORDINARY case — every ancestor directory of every
// indexed subdirectory is probed for one — so reporting ENOENT would emit
// several lines per healthy repo and bury the FIFO signal the report exists
// to carry.
func TestIgnoreSkipNotReportedForAbsentFile(t *testing.T) {
	var buf bytes.Buffer
	restore := setIgnoreSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	if got := parseInheritedGrafelIgnore(dir, "sub"); got != nil {
		t.Fatalf("parseInheritedGrafelIgnore = %+v, want nil for an absent file", got)
	}
	if buf.Len() != 0 {
		t.Errorf("absent .grafelignore reported %q, want silence", buf.String())
	}
}
