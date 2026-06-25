package mcp

// quarantine_recover_5618_test.go — Q3 query-side hook (#5618).
//
// Verifies that the MCP server's noteEntityAccess feeds the query/reference
// signal to a wired QuarantineRecoverer with a correctly-resolved absolute path,
// and is a safe no-op when no recoverer is wired or inputs are empty. The
// tracker's own Recover semantics (pin-respect, re-quarantine, persistence) are
// covered in internal/daemon/watch/quarantine_recover_test.go.

import (
	"os"
	"path/filepath"
	"testing"
)

// absRepoRoot returns an OS-absolute repo root used by these tests. A literal
// "/proj/repo" is absolute on unix but NOT on Windows (filepath.IsAbs needs a
// volume like C:\ or a UNC path), so we prefix the current working directory's
// volume name. This keeps the path absolute on every platform without touching
// the filesystem.
func absRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// On unix VolumeName is "" so this is just "/proj/repo"; on Windows it is
	// e.g. "C:" giving "C:\proj\repo" — absolute in both cases.
	return filepath.Join(filepath.VolumeName(wd)+string(filepath.Separator), "proj", "repo")
}

type fakeRecoverer struct {
	calls []struct{ repo, path string }
	usage []struct{ repo, path string }
	ret   bool
}

func (f *fakeRecoverer) Recover(repo, path string) (string, bool) {
	f.calls = append(f.calls, struct{ repo, path string }{repo, path})
	return "", f.ret
}

func (f *fakeRecoverer) NoteUsage(repo, path string) {
	f.usage = append(f.usage, struct{ repo, path string }{repo, path})
}

func TestNoteEntityAccess_RelativeSourceResolvedToAbs(t *testing.T) {
	fr := &fakeRecoverer{}
	s := &Server{}
	s.SetQuarantineRecoverer(fr)

	root := absRepoRoot(t)
	lr := &LoadedRepo{Repo: "r", Path: root}
	s.noteEntityAccess(lr, filepath.Join("app", "build", "x.go"))

	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 Recover call, got %d", len(fr.calls))
	}
	wantPath := filepath.Join(root, "app", "build", "x.go")
	if fr.calls[0].repo != root || fr.calls[0].path != wantPath {
		t.Fatalf("Recover called with (%q,%q), want (%q,%q)",
			fr.calls[0].repo, fr.calls[0].path, root, wantPath)
	}
}

func TestNoteEntityAccess_AbsoluteSourcePassedThrough(t *testing.T) {
	fr := &fakeRecoverer{}
	s := &Server{}
	s.SetQuarantineRecoverer(fr)

	root := absRepoRoot(t)
	abs := filepath.Join(root, "gen", "y.ts")
	s.noteEntityAccess(&LoadedRepo{Repo: "r", Path: root}, abs)

	if len(fr.calls) != 1 || fr.calls[0].path != abs {
		t.Fatalf("absolute source should pass through unchanged, got %+v", fr.calls)
	}
}

func TestNoteEntityAccess_NoRecovererIsNoOp(t *testing.T) {
	s := &Server{} // no recoverer wired
	// Must not panic and must do nothing.
	s.noteEntityAccess(&LoadedRepo{Repo: "r", Path: absRepoRoot(t)}, "a/b.go")
}

func TestNoteEntityAccess_EmptyInputsAreNoOp(t *testing.T) {
	fr := &fakeRecoverer{}
	s := &Server{}
	s.SetQuarantineRecoverer(fr)

	s.noteEntityAccess(nil, "a/b.go")                         // nil repo
	s.noteEntityAccess(&LoadedRepo{Path: ""}, "a/b.go")       // no repo root
	s.noteEntityAccess(&LoadedRepo{Path: absRepoRoot(t)}, "") // no source file

	if len(fr.calls) != 0 {
		t.Fatalf("empty inputs must not call Recover, got %+v", fr.calls)
	}
}
