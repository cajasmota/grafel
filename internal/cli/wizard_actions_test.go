package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/detect"
)

func mkGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGroupCandidates_Container is the ivivo case: classifying the container
// folder yields its child git repos as the group candidates — NOT the parent's
// siblings.
func TestGroupCandidates_Container(t *testing.T) {
	root := t.TempDir()
	ivivo := filepath.Join(root, "ivivo")
	mkGitRepo(t, filepath.Join(ivivo, "backend"))
	mkGitRepo(t, filepath.Join(ivivo, "frontend"))
	mkGitRepo(t, filepath.Join(root, "unrelated"))

	class, err := detect.ClassifyPath(ivivo)
	if err != nil {
		t.Fatal(err)
	}
	got := groupCandidates(class)
	if len(got) != 2 {
		t.Fatalf("groupCandidates = %v, want 2 (backend, frontend)", got)
	}
	if filepath.Base(got[0]) != "backend" || filepath.Base(got[1]) != "frontend" {
		t.Errorf("got %v, want [backend frontend]", got)
	}
	for _, c := range got {
		if filepath.Base(c) == "unrelated" {
			t.Errorf("unrelated sibling leaked into candidates: %v", got)
		}
	}
	if defaultAction(class) != actionGroup {
		t.Errorf("defaultAction = %q, want group", defaultAction(class))
	}
}

// TestGroupCandidates_RepoWithSiblings: classifying a git repo that has siblings
// yields the repo itself plus its siblings.
func TestGroupCandidates_RepoWithSiblings(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "service-a")
	b := filepath.Join(root, "service-b")
	mkGitRepo(t, a)
	mkGitRepo(t, b)

	class, err := detect.ClassifyPath(a)
	if err != nil {
		t.Fatal(err)
	}
	got := groupCandidates(class)
	if len(got) != 2 {
		t.Fatalf("groupCandidates = %v, want 2 (self + sibling)", got)
	}
	if defaultAction(class) != actionGroup {
		t.Errorf("defaultAction = %q, want group", defaultAction(class))
	}
}

func TestDefaultAction(t *testing.T) {
	cases := []struct {
		sug  detect.SuggestedAction
		want wizardAction
	}{
		{detect.ActionSingle, actionSingle},
		{detect.ActionGroup, actionGroup},
		{detect.ActionMonorepo, actionMonorepo},
		{detect.ActionNone, actionSingle}, // falls back to single
	}
	for _, c := range cases {
		got := defaultAction(detect.Classification{Suggested: c.sug})
		if got != c.want {
			t.Errorf("defaultAction(%q) = %q, want %q", c.sug, got, c.want)
		}
	}
}
