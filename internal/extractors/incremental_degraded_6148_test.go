// Package extractors — #6148 degraded-path coverage.
//
// This file is IN-PACKAGE (the rest of the incremental tests are
// `extractors_test`) for one reason: `frameworkDetector` is unexported, and the
// behaviour under test is what happens when it returns nil — the embedded YAML
// rule sets failed to load.
//
// Nothing else asserts this. The #6129 parity gate notices the rules being
// wired away (that is the whole point of it), but "the rules are GONE" and "the
// rules are ignored" are different failures: the first must degrade to the
// pre-#6148 graph shape, quietly and without panicking, and the second must
// fail the gate. Only the second was covered.
package extractors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// dgWrite writes one file under repo, creating parents.
func dgWrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// dgSeed writes a one-entity baseline graph plus a manifest matching the repo's
// current contents, which is what makes the next TryIncremental a real
// incremental run rather than a fallback.
func dgSeed(t *testing.T, repo, stateDir string, ents []graph.Entity) {
	t.Helper()
	doc := &graph.Document{
		Version: graph.SchemaVersion, GeneratedAt: time.Now().UTC(), Repo: "test-repo",
		Entities: ents, Stats: graph.Stats{Entities: len(ents)},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	var paths []string
	_ = filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, paths, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
}

// TestIncremental_FrameworkRulesUnavailable_DegradesNotPanics_6148 pins the
// contract of a nil detector: the run still completes, the file's entities are
// still re-extracted, and the class simply keeps its generic kind. The rules are
// an enrichment input, not a precondition for re-extraction — losing them must
// not cost the caller its graph.
func TestIncremental_FrameworkRulesUnavailable_DegradesNotPanics_6148(t *testing.T) {
	prev := frameworkDetector
	frameworkDetector = func() *engine.Detector { return nil }
	t.Cleanup(func() { frameworkDetector = prev })

	repo := t.TempDir()
	stateDir := t.TempDir()
	dgWrite(t, repo, "dghandler.py", "def dg_handle(x):\n    return x\n")
	dgSeed(t, repo, stateDir, []graph.Entity{{
		ID:         graph.EntityID("test-repo", "SCOPE.Operation", "dg_handle", "dghandler.py"),
		Name:       "dg_handle",
		Kind:       "SCOPE.Operation",
		SourceFile: "dghandler.py",
		Language:   "python",
	}})
	dgWrite(t, repo, "dghandler.py", "def dg_handle(x):\n    return x\n\n\nclass DgProbe:\n    def m(self):\n        return 1\n")

	res := TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("a nil detector must not cost the run: fell back with %q", res.FallbackReason)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	var kinds []string
	for i := range doc.Entities {
		if doc.Entities[i].Name == "DgProbe" {
			kinds = append(kinds, doc.Entities[i].Kind)
		}
	}
	// Exactly one node for the class, carrying the pre-#6148 generic kind. Not
	// zero (the re-extraction must still have happened) and not two.
	if len(kinds) != 1 || kinds[0] != "SCOPE.Component" {
		t.Errorf("degraded run: want exactly [SCOPE.Component] for the class, got %v", kinds)
	}
}
