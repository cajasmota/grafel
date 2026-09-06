package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/walk"
)

// TestWalkerAndEventBoundaryAgreeOnIgnoredFiles pins the property #6931's fix
// establishes: for a FILE matched by a root .gitignore pattern, the indexer's
// walker and the watcher's event boundary reach the SAME verdict.
//
// Before the fix they disagreed. ShouldSkipPathForRepo already consulted the
// root .gitignore for the full relative path (skip.go walks every prefix,
// including the last), so the watcher dropped the event — while walk.WalkRepo
// consulted the ignore stack only inside its d.IsDir() branch and indexed the
// file anyway. The watcher was the stricter of the two, which is why the
// divergence never showed up as reindex churn.
//
// VARIED: pattern form (bare name, glob) and path depth (root, nested).
// HELD CONSTANT: extension (.go / .json are both indexable), one repo root,
// no nested .gitignore — the watcher only reads the ROOT one, so a nested rule
// would test a divergence this test does not claim to close.
func TestWalkerAndEventBoundaryAgreeOnIgnoredFiles_6931(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "graph.json\n*.gen.go\n")
	cases := map[string]bool{ // rel path → want skipped
		"graph.json":        true,
		"pkg/graph.json":    true,
		"a.gen.go":          true,
		"pkg/b.gen.go":      true,
		"keep.go":           false,
		"pkg/keep.go":       false,
		"pkg/graph.json.go": false, // control: the pattern is not a prefix rule
	}
	for rel := range cases {
		write(rel, "x\n")
	}
	// The cache is process-wide and keyed by repo path; a fresh TempDir is a
	// fresh key, but evict anyway so a re-run inside one process is honest.
	evictRepoIgnoreState(root)

	walked, _, err := walk.WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	inWalk := map[string]bool{}
	for _, f := range walked {
		inWalk[f] = true
	}

	for rel, wantSkip := range cases {
		walkerSkipped := !inWalk[rel]
		watcherSkipped := ShouldSkipPathForRepo(root, filepath.Join(root, filepath.FromSlash(rel)))
		if walkerSkipped != wantSkip {
			t.Errorf("%s: walker skipped=%v, want %v", rel, walkerSkipped, wantSkip)
		}
		if watcherSkipped != wantSkip {
			t.Errorf("%s: watcher skipped=%v, want %v", rel, watcherSkipped, wantSkip)
		}
		if walkerSkipped != watcherSkipped {
			t.Errorf("%s: walker and watcher DISAGREE (walker=%v watcher=%v)", rel, walkerSkipped, watcherSkipped)
		}
	}
}
