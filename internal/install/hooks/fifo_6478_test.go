//go:build unix

package hooks

// fifo_6478_test.go — deadline pin for the three reads in this package
// (docs/blocking-open-audit.md, "rules/agent files" family).
//
// Two shapes, both name-chosen:
//
//   - hooksDir reads a `.git` FILE to follow a worktree's `gitdir:` pointer.
//     It was guarded by os.Stat + !IsDir(), which is the TOCTOU window #6478
//     names verbatim rather than a gate: stat says "not a directory" and a FIFO
//     satisfies that.
//   - Install / Uninstall read each name in HookNames out of the hooks
//     directory. Those names carry no extension, so the #6478 AST sweep cannot
//     see them by construction — this test is the only thing that pins them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestHooksDirFIFOGitFileDoesNotHang(t *testing.T) {
	repo := t.TempDir()
	testsupport.MkfifoInTemp(t, repo, ".git")

	var err error
	testsupport.MustReturn(t, "hooksDir with a FIFO named .git", func() {
		_, err = hooksDir(repo)
	})
	if err == nil {
		t.Fatal("hooksDir returned a nil error for a FIFO .git; a refused pointer file must be " +
			"reported, not treated as an ordinary resolution failure with no cause")
	}
}

func TestUninstallFIFOHookDoesNotHang(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Every hook name gets its own tree: Uninstall returns on the first error,
	// so a single tree would only ever exercise HookNames[0].
	for _, name := range HookNames {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			testsupport.MkfifoInTemp(t, repo, ".git", "hooks", name)
			testsupport.MustReturn(t, "Uninstall with a FIFO named "+name, func() {
				_ = Uninstall(repo)
			})
		})
	}
}

// TestHooksDirStillFollowsARegularGitFile is the positive control for the
// hooksDir half: a reader that refused every .git file would pass the FIFO test
// and break every git worktree.
func TestHooksDirStillFollowsARegularGitFile(t *testing.T) {
	repo := t.TempDir()
	real := filepath.Join(repo, "realgit")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: "+real+"\n"), 0o600); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	got, err := hooksDir(repo)
	if err != nil {
		t.Fatalf("hooksDir on a regular .git file: %v", err)
	}
	if want := filepath.Join(real, "hooks"); !strings.EqualFold(got, want) {
		t.Fatalf("hooksDir = %q, want %q", got, want)
	}
}
