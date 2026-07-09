package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

// TestCheckRepoGitDetection covers the four .git-resolution cases for checkRepo
// (issue #5675 item 4): a normal single repo with its own .git must pass OK; a
// repo with no .git anywhere up the tree must warn "missing .git"; a module
// subdir of a single-.git monorepo must NOT warn (the .git lives at the repo
// root); and each repo of a related-repo group (each its own .git) must pass OK.
func TestCheckRepoGitDetection(t *testing.T) {
	base := t.TempDir()

	// Case: normal single repo — .git at the repo path itself.
	singleRepo := filepath.Join(base, "single")
	mustMkdir(t, filepath.Join(singleRepo, ".git"))

	// Case: no .git anywhere up the tree.
	noGit := filepath.Join(base, "no-git-tree", "plain")
	mustMkdir(t, noGit)

	// Case: monorepo module — single .git at the monorepo root, module is a
	// subdir with no .git of its own.
	monoRoot := filepath.Join(base, "mono")
	mustMkdir(t, filepath.Join(monoRoot, ".git"))
	monoModule := filepath.Join(monoRoot, "services", "api")
	mustMkdir(t, monoModule)

	// Case: related repos — two independent repos, each its own .git.
	relA := filepath.Join(base, "related", "svc-a")
	relB := filepath.Join(base, "related", "svc-b")
	mustMkdir(t, filepath.Join(relA, ".git"))
	mustMkdir(t, filepath.Join(relB, ".git"))

	tests := []struct {
		name     string
		path     string
		wantWarn bool
	}{
		{"single-repo-ok", singleRepo, false},
		{"no-git-warns", noGit, true},
		{"monorepo-module-no-warn", monoModule, false},
		{"related-repo-a-ok", relA, false},
		{"related-repo-b-ok", relB, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			checkRepo(&buf, registry.Repo{
				Slug:  tt.name,
				Path:  tt.path,
				Stack: registry.StackList{"go"},
			})
			out := buf.String()
			gotWarn := strings.Contains(out, "missing .git")
			if gotWarn != tt.wantWarn {
				t.Errorf("checkRepo(%s): missing .git warning = %v, want %v\noutput:\n%s",
					tt.path, gotWarn, tt.wantWarn, out)
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}
