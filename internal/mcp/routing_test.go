package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

var osMkdirAll = os.MkdirAll

// TestResolveGroup_CWDFromRegistry verifies that when cwd is inside a
// registered repo path, the group is inferred without an explicit group=
// argument and without a .archigraph/group.json marker (#1650).
func TestResolveGroup_CWDFromRegistry(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "upvate_core")
	if err := mkdirp(repoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"upvate": {
				Repos: map[string]RegistryRepo{
					"upvate-core": {Path: repoPath},
				},
			},
			"polyglot-platform": {
				Repos: map[string]RegistryRepo{
					"other": {Path: filepath.Join(tmp, "other-root")},
				},
			},
		},
	}
	st := NewState(reg)

	// Bare cwd inside the registered repo path → resolve "upvate" via registry.
	subdir := filepath.Join(repoPath, "src", "views")
	if err := mkdirp(subdir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	g, src, err := resolveGroup(st, "", subdir)
	if err != nil {
		t.Fatalf("resolveGroup: %v", err)
	}
	if g != "upvate" {
		t.Errorf("group: want upvate, got %s", g)
	}
	if src != "cwd_registry" {
		t.Errorf("source: want cwd_registry, got %s", src)
	}

	// Outside any registered repo → ambiguous error (two groups registered).
	_, _, err = resolveGroup(st, "", filepath.Join(tmp, "nowhere"))
	if err == nil {
		t.Errorf("expected ambiguous-group error for cwd outside any repo")
	}
}

func mkdirp(p string) error {
	return osMkdirAll(p, 0o755)
}
