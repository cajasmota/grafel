package walk

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestWalkRepo_NestedIgnoreDoesNotLeakToSiblingFiles_6931 is the regression for
// the over-exclusion the file branch made live.
//
// WalkRepo popped nested ignore files ONLY inside the d.IsDir() branch, so a
// nested .gitignore stayed on the stack while filepath.WalkDir visited every
// FILE that sorts after that subdirectory. While the stack only decided
// directories the staleness was invisible — a directory entry pops first — and
// the moment files consult it, a rule two levels down starts deleting files it
// has no authority over.
//
// The ordering IS the bug, so every row here is chosen for its position in
// lexical DFS order, not for its name:
//
//	pkg/sub/.gitignore   ignores "victim.go"
//	pkg/sub/inside.go    inside the subtree      -> walked (rule does not match)
//	pkg/victim.go        sibling of sub/, sorts AFTER "sub"   -> MUST be walked
//	victim.go            repo root, sorts AFTER "pkg"         -> MUST be walked
//	pkg/sub/victim.go    genuinely governed                    -> must be SKIPPED
//
// VARIED: the depth at which the file is visited relative to the ignore file's
// own directory (inside it, a sibling one level up, the repo root two levels
// up). HELD CONSTANT: the basename `victim.go` for all four rows, so the only
// thing separating a correct skip from an over-exclusion is position — a row
// that passed by name would prove nothing.
func TestWalkRepo_NestedIgnoreDoesNotLeakToSiblingFiles_6931(t *testing.T) {
	root := t.TempDir()
	writeIgnoreFixtureFile(t, root, "pkg/sub/.gitignore", "victim.go\n")
	for _, p := range []string{
		"pkg/sub/inside.go",
		"pkg/sub/victim.go",
		"pkg/victim.go",
		"victim.go",
	} {
		writeIgnoreFixtureFile(t, root, p, "package p\n")
	}

	got, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	sort.Strings(got)
	want := []string{"pkg/sub/.gitignore", "pkg/sub/inside.go", "pkg/victim.go", "victim.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		for _, s := range skipped {
			rel, _ := filepath.Rel(root, s.AbsPath)
			t.Logf("skipped %s rule=%s", filepath.ToSlash(rel), s.Rule)
		}
		t.Errorf("walked = %v, want %v", got, want)
	}
}
