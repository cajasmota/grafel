// Package extractors_test — #6148.
//
// A class that arrives in a CHANGED file must end up carrying the same KIND the
// full rebuild gives it.
//
// The full path types class symbols in Pass 2.5 (engine.Detector over the YAML
// rule sets) and then folds the per-language AST's generic SCOPE.Component node
// into that framework-typed record (#1613). The incremental path re-extracts a
// changed file with the per-language extractor ONLY, so before this change the
// class kept the generic kind and a full rebuild of the same tree disagreed.
//
// Classes in UNCHANGED files never showed it: those entities are carried
// forward from the previous full rebuild's graph with the typed kind already on
// them. Only a class the incremental path actually re-extracts diverges — which
// is why the #6129 parity gate, whose fixture had never put a class in its
// delta file, stayed green throughout.
package extractors_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// ckProbeSource is the minimum case from #6148: a class with no decorator, no
// base class, no route annotation and no naming convention — nothing that
// should attract any particular classification on either path. What it gets is
// beside the point; the two paths must agree on it.
const ckProbeSource = `def ck_handle(x):
    return x + 1


class CkPlainProbe:
    def ck_probe_noop(self):
        return 1
`

// ckExpectedClassKind asks the SAME oracle the full rebuild's Pass 2.5 asks —
// engine.Detector over the embedded YAML rules — what kind this class symbol
// carries. Hard-coding the answer would pin today's rule set rather than the
// invariant under test (the two paths agree), and would go stale the moment a
// rule changes.
func ckExpectedClassKind(t *testing.T, path, src string) string {
	t.Helper()
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("load engine rules: %v", err)
	}
	res, err := engine.New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil || res == nil {
		t.Fatalf("detect: %v", err)
	}
	var best string
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.Name != "CkPlainProbe" {
			continue
		}
		if _, eligible := engine.FrameworkClassKindPriority[e.Kind]; !eligible {
			continue
		}
		if best == "" ||
			engine.FrameworkClassKindPriority[e.Kind] > engine.FrameworkClassKindPriority[best] {
			best = e.Kind
		}
	}
	if best == "" {
		t.Fatalf("fixture no longer reaches a framework-typed class kind — the rule set that " +
			"typed a bare Python class changed, so this test can no longer distinguish the two paths")
	}
	return best
}

// ckClassRows returns every entity row for the probe class as a content tuple
// (kind|name|source_file), sorted. Content, not ids: the id is a hash OF the
// kind, so an id-keyed assertion would report a re-kinded entity as "a
// different entity" without ever naming the kind that changed.
func ckClassRows(t *testing.T, stateDir string) []string {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	var out []string
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Name != "CkPlainProbe" {
			continue
		}
		out = append(out, e.Kind+"|"+e.Name+"|"+e.SourceFile)
	}
	sort.Strings(out)
	return out
}

// TestIncremental_ClassInChangedFile_CarriesFrameworkKind_6148 is the gate.
//
// The assertion is BIDIRECTIONAL over the content rows for the class symbol:
// the expected row must be present AND nothing else may be. A one-directional
// "the typed row exists" check would pass while the generic row also survived —
// two nodes for one class, the #1613 invariant broken — and a one-directional
// "the generic row is gone" check would pass on an empty graph.
func TestIncremental_ClassInChangedFile_CarriesFrameworkKind_6148(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	// Baseline: the same file WITHOUT the class.
	writeFile(t, repo, "ckhandler.py", "def ck_handle(x):\n    return x\n")
	buildMinimalGraph(t, stateDir, []graph.Entity{
		{
			ID:         graph.EntityID("test-repo", "SCOPE.Operation", "ck_handle", "ckhandler.py"),
			Name:       "ck_handle",
			Kind:       "SCOPE.Operation",
			SourceFile: "ckhandler.py",
			Language:   "python",
		},
	}, nil)
	seedManifest(t, repo, stateDir)

	// The delta: the class appears.
	writeFile(t, repo, "ckhandler.py", ckProbeSource)

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back to a full reindex (%s) — the run under test never "+
			"happened, and a full-reindex fallback would trivially satisfy this assertion",
			res.FallbackReason)
	}

	want := []string{ckExpectedClassKind(t, "ckhandler.py", ckProbeSource) + "|CkPlainProbe|ckhandler.py"}
	got := ckClassRows(t, stateDir)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("class rows after incremental re-extraction:\n  want %v\n  got  %v\n"+
			"The full rebuild types this class via Pass 2.5 (engine.Detector over the YAML "+
			"rules) and folds the generic AST node into it; the incremental path must reach "+
			"the same kind for the class it re-extracted.", want, got)
	}
}
