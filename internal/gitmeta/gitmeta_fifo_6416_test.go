//go:build unix

package gitmeta

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gitmetaFIFODeadline bounds each subtest. A correctly-gated read returns in
// microseconds — safeio's stat gate never opens a FIFO — so anything near this
// value is the hang, not a slow machine. It is well under the package test
// timeout so a regression FAILS with attribution rather than wedging the suite
// until the watchdog kills the binary with no clue which test parked.
const gitmetaFIFODeadline = 10 * time.Second

// mkfifoInGitTemp creates a named pipe at dir/rel, where dir MUST be a
// t.TempDir(). A FIFO outside a temp dir outlives the test and hangs any other
// process that later walks over it, so the root temp DIRECTORY is taken
// separately from the relative path and this helper cannot be pointed
// elsewhere by accident.
func mkfifoInGitTemp(t *testing.T, dir string, rel ...string) string {
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

// mustReturnGit runs fn and fails if it has not returned within
// gitmetaFIFODeadline. Under the pre-fix code a bare call parks in open(2)
// forever, which wedges the whole test binary AND leaves t.TempDir's cleanup
// unrun — leaking a FIFO onto a shared machine. Running fn on its own
// goroutine means the deadline fires, cleanup runs, and the failure names the
// call that hung.
func mustReturnGit(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(gitmetaFIFODeadline):
		t.Fatalf("HANG: %s did not return within %s", what, gitmetaFIFODeadline)
	}
}

// writeGitFile writes a regular file under dir, creating parents.
func writeGitFile(t *testing.T, dir, content string, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{dir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReadGitdirFileFIFODoesNotHang covers the ".git" read (cache.go).
//
// This is the entry point of the whole package: headPointerKey and
// resolveGitDirs both os.Stat "<repo>/.git", find it is not a directory, and
// hand it to readGitdirFile. A FIFO passes the !IsDir() test, so the read
// parked in open(2) with nothing bounding the wait.
func TestReadGitdirFileFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	mkfifoInGitTemp(t, dir, ".git")

	var got string
	mustReturnGit(t, "readGitdirFile with a FIFO .git", func() {
		got = readGitdirFile(filepath.Join(dir, ".git"))
	})
	if got != "" {
		t.Errorf("readGitdirFile = %q, want \"\" for a refused .git", got)
	}
}

// TestResolveGitDirsCommondirFIFODoesNotHang covers the "commondir" read
// (cache.go). It is reached in the linked-worktree layout: .git is a
// gitdir-file naming the private dir, whose commondir file names the shared
// one.
func TestResolveGitDirsCommondirFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "private-git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitFile(t, dir, "gitdir: "+gitDir+"\n", ".git")
	mkfifoInGitTemp(t, dir, "private-git", "commondir")

	var common string
	var ok bool
	mustReturnGit(t, "resolveGitDirs with a FIFO commondir", func() {
		_, common, ok = resolveGitDirs(dir)
	})
	// A refused commondir must degrade to the same "no separate common dir"
	// behaviour an absent one already produces, not to a failure.
	if !ok || common != gitDir {
		t.Errorf("resolveGitDirs = (%q, %v), want (%q, true) for a refused commondir", common, ok, gitDir)
	}
}

// TestCommitTokenHEADFIFODoesNotHang covers the "HEAD" read (cache.go).
// commitToken is on CaptureCachedFresh's path, which ResolveCWD and
// grafel_whoami call on every request.
func TestCommitTokenHEADFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "private-git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitFile(t, dir, "gitdir: "+gitDir+"\n", ".git")
	mkfifoInGitTemp(t, dir, "private-git", "HEAD")

	var ok bool
	mustReturnGit(t, "commitToken with a FIFO HEAD", func() {
		_, ok = commitToken(dir)
	})
	if ok {
		t.Errorf("commitToken ok = true for a refused HEAD; want false so the caller runs uncached")
	}
}

// TestContentTokenLooseRefFIFODoesNotHang covers the "refs/…" read (cache.go).
// The ref path is built from HEAD's own bytes, so a hostile repo picks the
// leaf name; the read is name-chosen either way.
func TestContentTokenLooseRefFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "private-git")
	writeGitFile(t, dir, "gitdir: "+gitDir+"\n", ".git")
	writeGitFile(t, gitDir, "ref: refs/heads/main\n", "HEAD")
	mkfifoInGitTemp(t, gitDir, "refs", "heads", "main")

	var tok string
	var ok bool
	mustReturnGit(t, "commitToken with a FIFO loose ref", func() {
		tok, ok = commitToken(dir)
	})
	// A refused ref is indistinguishable from an absent one, which is a
	// meaningful state (the ref is packed), so the token still resolves.
	if !ok {
		t.Errorf("commitToken ok = false for a refused loose ref; want true with the absent-ref sentinel")
	}
	if strings.Contains(tok, "\x00\x00") == false && !strings.Contains(tok, "-") {
		t.Errorf("commitToken = %q, want it to carry the absent-ref sentinel", tok)
	}
}

// TestParseSparsePatternFileFIFODoesNotHang covers the
// "info/sparse-checkout" read (sparse.go). This one used os.Open + a scanner
// rather than os.ReadFile, which blocks identically: it is open(2) that waits,
// not the read.
func TestParseSparsePatternFileFIFODoesNotHang(t *testing.T) {
	dir := t.TempDir()
	p := mkfifoInGitTemp(t, dir, "info", "sparse-checkout")

	var got []string
	mustReturnGit(t, "parseSparsePatternFile with a FIFO sparse-checkout", func() {
		got = parseSparsePatternFile(p)
	})
	if len(got) != 0 {
		t.Errorf("parseSparsePatternFile = %v, want none for a refused pattern file", got)
	}
}

// TestGitMetaReadersStillReadRegularFiles is the liveness half. A gate that
// refuses everything also never hangs, so the deadline tests above are only
// meaningful next to this one — and sparse patterns in particular are easy to
// break silently, since dropping them indexes a sparse repo as if it were
// full.
func TestGitMetaReadersStillReadRegularFiles(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "private-git")
	writeGitFile(t, dir, "gitdir: "+gitDir+"\n", ".git")
	writeGitFile(t, gitDir, "ref: refs/heads/main\n", "HEAD")
	writeGitFile(t, gitDir, "../..\n", "commondir")
	writeGitFile(t, gitDir, "deadbeef\n", "refs", "heads", "main")

	if got := readGitdirFile(filepath.Join(dir, ".git")); got != gitDir {
		t.Errorf("readGitdirFile = %q, want %q", got, gitDir)
	}
	if _, _, ok := resolveGitDirs(dir); !ok {
		t.Error("resolveGitDirs ok = false for a well-formed worktree layout")
	}
	tok, ok := commitToken(dir)
	if !ok || !strings.Contains(tok, "ref: refs/heads/main") {
		t.Errorf("commitToken = (%q, %v), want the HEAD bytes to appear", tok, ok)
	}

	sp := writeGitFile(t, dir, "# comment\nsrc/\n\napps/web/\n", "info", "sparse-checkout")
	pats := parseSparsePatternFile(sp)
	if len(pats) != 2 || pats[0] != "src/" || pats[1] != "apps/web/" {
		t.Errorf("parseSparsePatternFile = %v, want [src/ apps/web/]", pats)
	}
}

// TestGitMetaSkipIsReported pins the report, not just the absence of the hang.
// A silent skip here changes indexing behaviour — a dropped sparse pattern set
// indexes a sparse repo as a full one — with nothing on stderr saying why.
func TestGitMetaSkipIsReported(t *testing.T) {
	var buf bytes.Buffer
	restore := setGitMetaSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	p := mkfifoInGitTemp(t, dir, "info", "sparse-checkout")
	mustReturnGit(t, "parseSparsePatternFile with a FIFO", func() { _ = parseSparsePatternFile(p) })

	out := buf.String()
	if !strings.Contains(out, p) {
		t.Errorf("skip report = %q, want it to name %q", out, p)
	}
	if !strings.Contains(out, "6416") {
		t.Errorf("skip report = %q, want it to cite #6416", out)
	}
}

// TestGitMetaSkipReportIsDedupedAndCapped keeps the always-on report from
// becoming the noise it exists to cut through. Dedup matters more here than
// anywhere else in this class: the daemon re-enters CaptureCached on every
// request, so an undeduped line would repeat for the life of the process.
func TestGitMetaSkipReportIsDedupedAndCapped(t *testing.T) {
	var buf bytes.Buffer
	restore := setGitMetaSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	p := mkfifoInGitTemp(t, dir, ".git")
	for i := 0; i < 3; i++ {
		mustReturnGit(t, "repeat read", func() { _ = readGitdirFile(p) })
	}
	if n := strings.Count(buf.String(), "grafel: skipped"); n != 1 {
		t.Errorf("same path read 3 times produced %d skip lines, want 1", n)
	}

	for i := 0; i < maxGitMetaSkipReports+5; i++ {
		d := filepath.Join(dir, "d", string(rune('a'+i%26)), string(rune('a'+i/26)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		q := filepath.Join(d, "HEAD")
		if err := syscall.Mkfifo(q, 0o600); err != nil {
			t.Fatal(err)
		}
		mustReturnGit(t, "capped read", func() { _ = readGitdirFile(q) })
	}
	if n := strings.Count(buf.String(), "grafel: skipped"); n > maxGitMetaSkipReports {
		t.Errorf("emitted %d skip lines, want at most %d", n, maxGitMetaSkipReports)
	}
	if !strings.Contains(buf.String(), "suppressed") {
		t.Errorf("report never announced suppression despite exceeding the cap: %q", buf.String())
	}
}

// TestGitMetaSkipNotReportedForAbsentFile is the noise guard. Absence is the
// ORDINARY case for every file this package reads — commondir exists only in a
// linked worktree, a loose ref only while unpacked, sparse-checkout only in a
// sparse clone — so reporting ENOENT would emit lines for every healthy repo
// and bury the FIFO signal.
func TestGitMetaSkipNotReportedForAbsentFile(t *testing.T) {
	var buf bytes.Buffer
	restore := setGitMetaSkipOutput(&buf)
	defer restore()

	dir := t.TempDir()
	_ = readGitdirFile(filepath.Join(dir, ".git"))
	_ = parseSparsePatternFile(filepath.Join(dir, "info", "sparse-checkout"))
	_ = contentToken(filepath.Join(dir, "refs", "heads", "main"))
	if buf.Len() != 0 {
		t.Errorf("absent files reported %q, want silence", buf.String())
	}
}
