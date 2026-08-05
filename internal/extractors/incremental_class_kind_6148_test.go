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

// ckProbeSource carries both halves of what Pass 2.5 produces for one file:
//
//   - CkPlainProbe — a class with no decorator and no base class. What kind it
//     gets is beside the point; the two paths must agree on it (#6148).
//   - the `on_get` responder and `ck_app = falcon.App()` — framework records
//     that pair with NO extractor record, so nothing folds them and the class
//     fold alone would still leave them dropped (#6150).
const ckProbeSource = `def ck_handle(x):
    return x + 1


class CkPlainProbe:
    def on_get(self, req, resp):
        return 1


ck_app = falcon.App()
`

// ckDetect is the ORACLE: the same engine.Detector over the same embedded YAML
// rules that the full rebuild's Pass 2.5 runs. Every expectation below is
// derived from it rather than hard-coded, so this file pins the invariant (the
// two paths agree) instead of pinning today's rule set — which would go stale
// the moment a rule changes, in a test that would then be asserting the wrong
// thing while still passing.
func ckDetect(t *testing.T, path, src string) *engine.DetectResult {
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
	return res
}

// ckExpectedClassKind asks the oracle what kind this class symbol carries.
func ckExpectedClassKind(t *testing.T, path, src string) string {
	t.Helper()
	res := ckDetect(t, path, src)
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

	// #6150 — the other half of Pass 2.5's output. `ck_app = falcon.App()` and
	// the `on_get` responder produce framework records that pair with NO
	// extractor record at all, so the class fold alone would leave them dropped:
	// a full rebuild carries them and this path did not. Asserted as a SET, both
	// directions, against the same oracle rather than a hard-coded kind list.
	wantFW := ckFrameworkOnlyRows(t, "ckhandler.py", ckProbeSource)
	if len(wantFW) == 0 {
		t.Fatal("fixture no longer reaches any framework-only record — the rules that matched " +
			"the app object / responder changed, and this assertion has gone vacuous")
	}
	gotFW := ckRowsFor(t, stateDir, "ckhandler.py", wantFW)
	if strings.Join(gotFW, ",") != strings.Join(wantFW, ",") {
		t.Errorf("framework-only rows after incremental re-extraction:\n  want %v\n  got  %v\n"+
			"Pass 2.5 emits these on a full rebuild; the incremental path must emit them too.",
			wantFW, gotFW)
	}
}

// ckFrameworkOnlyRows asks the oracle which Detect records for this file pair
// with no extractor record — the ones whose only source is Pass 2.5.
func ckFrameworkOnlyRows(t *testing.T, path, src string) []string {
	t.Helper()
	res := ckDetect(t, path, src)
	var out []string
	for i := range res.Entities {
		e := &res.Entities[i]
		// The class itself is covered by the fold assertion above; here we want
		// only the records that have no counterpart in the extractor's output.
		if e.Name == "CkPlainProbe" {
			continue
		}
		out = append(out, e.Kind+"|"+e.Name+"|"+path)
	}
	sort.Strings(out)
	return out
}

// ckRowsFor returns the graph's rows for path whose KIND is one the oracle
// emitted — not merely the rows the oracle predicted. Keying on the kind
// vocabulary rather than on the wanted rows themselves is what makes the
// comparison bidirectional: a row INVENTED under one of those kinds shows up as
// an extra element, where a containment check would miss it. It is bounded, and
// deliberately so: an invention under an unrelated kind is the #6129 parity
// gate's job, not this test's.
func ckRowsFor(t *testing.T, stateDir, path string, oracle []string) []string {
	t.Helper()
	kinds := make(map[string]bool, len(oracle))
	for _, row := range oracle {
		kinds[strings.SplitN(row, "|", 2)[0]] = true
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	var out []string
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.SourceFile != path || !kinds[e.Kind] || e.Name == "CkPlainProbe" {
			continue
		}
		out = append(out, e.Kind+"|"+e.Name+"|"+e.SourceFile)
	}
	sort.Strings(out)
	return out
}
