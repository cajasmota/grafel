//go:build unix

package install

// gitignore_fifo_6478_test.go — deadline pin for the two .gitignore reads
// (docs/blocking-open-audit.md, "git/ignore" family).
//
// EnsureGitignore runs during `grafel install` and checkGitignore during
// `grafel doctor`, both against the .gitignore of whatever repo the user is
// standing in. #6478's grounding comment lists both as observable without
// source: "mkfifo-ing any of .gitignore, CLAUDE.md, AGENTS.md, .git/hooks/*,
// .grafel/group.json or .grafel/fitness.yaml inside a repo hangs the reading
// command indefinitely … Not behind a flag."

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestEnsureGitignoreFIFODoesNotHang(t *testing.T) {
	repo := t.TempDir()
	testsupport.MkfifoInTemp(t, repo, ".gitignore")

	var err error
	testsupport.MustReturn(t, "EnsureGitignore with a FIFO named .gitignore", func() {
		_, err = EnsureGitignore(repo)
	})
	// EnsureGitignore already surfaced non-ENOENT read errors; a FIFO must
	// travel that path rather than being mistaken for an absent file and
	// OVERWRITTEN, which is what a bare `if err != nil { existing = nil }`
	// would have done.
	if err == nil {
		t.Fatal("EnsureGitignore returned a nil error for a FIFO .gitignore")
	}
}

// TestEnsureGitignoreStillWritesARegularFile is the positive control.
func TestEnsureGitignoreStillWritesARegularFile(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := EnsureGitignore(repo)
	if err != nil {
		t.Fatalf("EnsureGitignore on a regular file: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(b), "node_modules/") {
		t.Fatalf("EnsureGitignore dropped existing content: %q", string(b))
	}
	if !strings.Contains(string(b), grafelGitignoreEntry) {
		t.Fatalf("EnsureGitignore did not append %q; the guard is refusing regular files: %q",
			grafelGitignoreEntry, string(b))
	}
}
