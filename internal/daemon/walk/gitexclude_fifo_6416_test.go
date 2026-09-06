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
)

// TestIgnoredFIFOIsReportedAsAHazardNotAsIgnored_6931 grades the ORDER of the
// new file-branch ignore check against the entry-type gate.
//
// The walker's comment argues the check "runs AFTER the entry-type gate so a
// FIFO is still reported as the hazard it is rather than disappearing under an
// ignore rule". Nothing observed that: both orderings `return nil`, so the walked
// SET is identical and only the REPORTED REASON differs. Swapping the two blocks
// left the whole package green — prose asserting what no test observes.
//
// A FIFO planted in a source tree is exactly the thing someone has to explain,
// and `*.pb.go` is exactly the pattern a repo ignores, so the two overlap in
// practice. Under the swapped order the operator is told the file was ignored by
// their own .gitignore and never learns a named pipe is sitting in their tree.
//
// VARIED: the entry type only. HELD CONSTANT: the name `ignored.pb.go` matches
// the ignore rule in BOTH rows, so the control cannot pass by name.
func TestIgnoredFIFOIsReportedAsAHazardNotAsIgnored_6931(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.pb.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Row 1: a FIFO whose name matches the ignore rule.
	mkfifoInWalkTemp(t, root, "ignored.pb.go")
	// Row 2: a REGULAR file whose name matches the same rule.
	if err := os.WriteFile(filepath.Join(root, "regular.pb.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var files []string
	var skipped []SkipEntry
	mustReturnWalk(t, "WalkRepo over an ignored FIFO", func() {
		files, skipped, _ = WalkRepo(root, nil)
	})

	rules := map[string]string{}
	for _, s := range skipped {
		rel, _ := filepath.Rel(root, s.AbsPath)
		rules[filepath.ToSlash(rel)] = s.Rule
	}
	if got := rules["ignored.pb.go"]; !strings.HasPrefix(got, "irregular:") {
		t.Errorf("FIFO ignored.pb.go reported with rule %q, want an irregular: hazard — "+
			"the ignore layer must not swallow the entry-type gate's reason", got)
	}
	if got := rules["regular.pb.go"]; !strings.Contains(got, ".gitignore") {
		t.Errorf("regular.pb.go reported with rule %q, want the .gitignore rule", got)
	}
	// Both are still omitted from the walked set — only the reason differs.
	for _, f := range files {
		if f == "ignored.pb.go" || f == "regular.pb.go" {
			t.Errorf("%s should not be walked", f)
		}
	}
}

// TestInfoExcludeFIFODoesNotHang_6416 grades the reason parseExcludeFile reads
// through readIgnoreFile rather than os.ReadFile.
//
// .git/info/exclude is a NAME-CHOSEN read that runs BEFORE the walk, so the
// entry-type gate #6468 added has nothing to say about it — the #6416 hazard
// verbatim. os.ReadFile on a FIFO does not fail, it parks in open(2) waiting
// for a writer: no timeout, no error, no log line, and `grafel index` never
// returns. Both the PR body and the doc comment named that hazard and no test
// observed it, so the mutant survived.
//
// It is a hang, so the pin is a deadline plus the always-on report — the
// mutant produces no value to assert on.
func TestInfoExcludeFIFODoesNotHang_6416(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	hermeticGitEnv(t)
	root := t.TempDir()
	mustGit(t, root, "init", "-q")

	// git init writes a regular .git/info/exclude; replace it with a FIFO.
	excl := filepath.Join(root, ".git", "info", "exclude")
	if err := os.Remove(excl); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove %s: %v", excl, err)
	}
	if err := os.MkdirAll(filepath.Dir(excl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(excl, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", excl, err)
	}
	t.Cleanup(func() { _ = os.Remove(excl) })

	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restore := setIgnoreSkipOutput(&buf)
	defer restore()

	var files []string
	mustReturnWalk(t, "WalkRepo with a FIFO .git/info/exclude", func() {
		files, _, _ = WalkRepo(root, nil)
	})

	if len(files) == 0 {
		t.Fatal("walk produced nothing; the refused exclude file must not stop the walk")
	}
	if !strings.Contains(buf.String(), excl) {
		t.Errorf("refused .git/info/exclude was not reported; got %q", buf.String())
	}
}
