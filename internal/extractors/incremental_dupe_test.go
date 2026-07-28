// Package extractors_test — incremental_dupe_test.go
//
// Regression gate for issue #6033: every incremental pass duplicated the
// entire surviving relationship set (edge multiplicity doubled per pass:
// 2, 4, 8, 16 copies of the same edge).
//
// The bug survived from #2167 to #5231-default-on because EVERY assertion in
// incremental_test.go is existence-only (`if r.ToID == x { found = true }`).
// An existence assertion cannot see duplication. The tests below assert on
// relationship MULTISET CARDINALITY across repeated passes — that is the
// property that actually binds this class of defect.
//
// SCOPE WARNING — DO NOT GENERALISE THE "x1" ASSERTIONS.
// Several tests here assert that EVERY relationship has multiplicity exactly 1.
// That is a property of THESE FIXTURES, not a grafel invariant. RelationshipID
// is hash(from, to, kind), and extractors deliberately emit multiple distinct
// edges sharing one ID — e.g. the Go extractor's multi-value call handling
// (internal/extractors/golang/extractor.go, `mvKey := mvTarget + "?mv"`) emits
// two CALLS with the same ID and different properties. A full rebuild keeps
// both. The fixtures below are chosen to contain no such construct, which is
// what makes the blanket "x1" check safe HERE.
//
// This is also why the #6033 fix does not dedupe: deduping by RelationshipID
// would collapse legitimately-distinct edges and diverge from a full rebuild.
// If you add a fixture with multi-value calls, overloads, or repeated calls at
// different lines, assert on the SEEDED edge's count rather than on all edges.
package extractors_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// relMultiset loads graph.fb from stateDir and returns a map of
// "fromID→toID:kind" → count, plus the total relationship count.
func relMultiset(t *testing.T, stateDir string) (map[string]int, int) {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	counts := make(map[string]int, len(doc.Relationships))
	for _, r := range doc.Relationships {
		counts[r.FromID+"→"+r.ToID+":"+r.Kind]++
	}
	return counts, len(doc.Relationships)
}

// runIncremental runs the incremental pass and fails the test if it
// falls back to a full reindex (a fallback would silently void every assertion
// in this file, since the fallback path never merges anything).
func runIncremental(t *testing.T, repo, stateDir string) {
	t.Helper()
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back (%s) — the merge path under test never ran", res.FallbackReason)
	}
}

// formatMultiset renders a multiset deterministically for failure messages.
func formatMultiset(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += fmt.Sprintf("\n  %s x%d", k, m[k])
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// #6033 — relationship multiplicity must be stable across repeated passes
// ─────────────────────────────────────────────────────────────────────────────

// TestIncremental_RepeatedPasses_RelationshipMultiplicityStable is the primary
// regression gate for #6033.
//
// Setup: a repo with a STABLE file (never edited, carrying a seeded CALLS edge
// between two of its entities) and a CHURN file that is edited on every pass.
// The seeded CALLS edge lives entirely in the surviving portion of the graph,
// so it is never re-extracted — it can only ever change multiplicity if the
// merge step duplicates the surviving set.
//
// Assertion: after each of 4 consecutive incremental passes, EVERY relationship
// appears exactly once, and the total relationship count does not grow.
//
// With the #6033 bug live this fails on pass 1 already (copies=2) and the
// counts double every pass thereafter.
func TestIncremental_RepeatedPasses_RelationshipMultiplicityStable(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "stable.go", "package p\n\nfunc StableOne() {}\n\nfunc StableTwo() {}\n")
	writeFile(t, repo, "churn.go", "package p\n\nfunc Churn0() {}\n")

	stableOne := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "StableOne", "stable.go"),
		Name: "StableOne", Kind: "SCOPE.Operation", SourceFile: "stable.go", Language: "go",
	}
	stableTwo := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "StableTwo", "stable.go"),
		Name: "StableTwo", Kind: "SCOPE.Operation", SourceFile: "stable.go", Language: "go",
	}
	churn := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "Churn0", "churn.go"),
		Name: "Churn0", Kind: "SCOPE.Operation", SourceFile: "churn.go", Language: "go",
	}

	// The seeded edge: entirely inside the unchanged file, both endpoints
	// already hex — the scoped resolver has no work to do on it, so its
	// multiplicity is a pure measurement of the merge step.
	seeded := graph.Relationship{
		ID:     graph.RelationshipID(stableOne.ID, stableTwo.ID, "CALLS"),
		FromID: stableOne.ID,
		ToID:   stableTwo.ID,
		Kind:   "CALLS",
	}
	seededKey := seeded.FromID + "→" + seeded.ToID + ":" + seeded.Kind

	buildMinimalGraph(t, stateDir,
		[]graph.Entity{stableOne, stableTwo, churn},
		[]graph.Relationship{seeded})
	seedManifest(t, repo, stateDir)

	prevTotal := -1
	for pass := 1; pass <= 4; pass++ {
		// Edit only churn.go — stable.go and its seeded edge never change.
		writeFile(t, repo, "churn.go",
			fmt.Sprintf("package p\n\nfunc Churn%d() {}\n", pass))

		runIncremental(t, repo, stateDir)

		counts, total := relMultiset(t, stateDir)

		if got := counts[seededKey]; got != 1 {
			t.Fatalf("pass %d: seeded CALLS edge in the UNCHANGED file appears %d times, want exactly 1 (#6033 duplication)%s",
				pass, got, formatMultiset(counts))
		}
		for key, n := range counts {
			if n != 1 {
				t.Fatalf("pass %d: relationship %q appears %d times, want exactly 1 (#6033 duplication)%s",
					pass, key, n, formatMultiset(counts))
			}
		}
		if prevTotal >= 0 && total > prevTotal {
			t.Fatalf("pass %d: total relationship count grew %d → %d across passes (#6033 duplication)%s",
				pass, prevTotal, total, formatMultiset(counts))
		}
		t.Logf("pass %d: totalRels=%d distinct=%d", pass, total, len(counts))
		prevTotal = total
	}
}

// TestIncremental_UnchangedRepo_RelationshipsIdempotent verifies the degenerate
// case: running the incremental pass repeatedly with NO file change at all must
// leave the relationship multiset bit-for-bit identical. Under #6033 even the
// no-op path duplicated whatever survived.
func TestIncremental_UnchangedRepo_RelationshipsIdempotent(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "a.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")

	entA := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "A", "a.go"),
		Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go",
	}
	entB := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "B", "a.go"),
		Name: "B", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go",
	}
	rel := graph.Relationship{
		ID:     graph.RelationshipID(entA.ID, entB.ID, "CALLS"),
		FromID: entA.ID, ToID: entB.ID, Kind: "CALLS",
	}
	buildMinimalGraph(t, stateDir, []graph.Entity{entA, entB}, []graph.Relationship{rel})
	seedManifest(t, repo, stateDir)

	var first map[string]int
	for pass := 1; pass <= 3; pass++ {
		runIncremental(t, repo, stateDir)
		counts, total := relMultiset(t, stateDir)
		for key, n := range counts {
			if n != 1 {
				t.Fatalf("pass %d (no-op): relationship %q appears %d times, want 1%s",
					pass, key, n, formatMultiset(counts))
			}
		}
		if first == nil {
			first = counts
		} else if len(counts) != len(first) {
			t.Fatalf("pass %d (no-op): distinct relationship set changed %d → %d%s",
				pass, len(first), len(counts), formatMultiset(counts))
		}
		t.Logf("pass %d (no-op): totalRels=%d", pass, total)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #6033 — the "new edges are actually merged" direction
// ─────────────────────────────────────────────────────────────────────────────

// TestIncremental_NewEdgeFromChangedFileAppendedExactlyOnce guards the OTHER
// direction of the #6033 fix. Splitting ScopedResult into two slices means the
// caller could silently drop the new-edge slice and every existence-only test
// in this package would still pass (verified by mutation: setting
// `newRels = nil` after the scoped resolver left the whole
// internal/extractors suite green before this test existed).
//
// A freshly-introduced call edge from a CHANGED file must land in the graph,
// exactly once.
func TestIncremental_NewEdgeFromChangedFileAppendedExactlyOnce(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "helper.go", "package p\n\nfunc Helper() {}\n")
	writeFile(t, repo, "caller.go", "package p\n\nfunc Caller() {}\n")

	helper := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "Helper", "helper.go"),
		Name: "Helper", Kind: "SCOPE.Operation", SourceFile: "helper.go", Language: "go",
	}
	caller := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "Caller", "caller.go"),
		Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "caller.go", Language: "go",
	}
	buildMinimalGraph(t, stateDir, []graph.Entity{helper, caller}, nil)
	seedManifest(t, repo, stateDir)

	before, _ := relMultiset(t, stateDir)
	if len(before) != 0 {
		t.Fatalf("baseline graph should hold no relationships, got%s", formatMultiset(before))
	}

	// Introduce a call: Caller now calls Helper. This edge exists ONLY in the
	// freshly-extracted (new) relationship slice.
	writeFile(t, repo, "caller.go", "package p\n\nfunc Caller() { Helper() }\n")

	runIncremental(t, repo, stateDir)

	counts, _ := relMultiset(t, stateDir)

	// Find any edge originating at Caller that is not a module-layer edge.
	newEdges := 0
	for key, n := range counts {
		if len(key) > len(caller.ID) && key[:len(caller.ID)] == caller.ID {
			newEdges += n
			if n != 1 {
				t.Errorf("new outbound edge %q appears %d times, want 1 (#6033)%s", key, n, formatMultiset(counts))
			}
		}
	}
	if newEdges == 0 {
		t.Errorf("no outbound edge from the CHANGED file's Caller entity reached the graph — the freshly-extracted relationship slice was dropped%s",
			formatMultiset(counts))
	}
	for key, n := range counts {
		if n != 1 {
			t.Errorf("relationship %q appears %d times, want 1 (#6033)%s", key, n, formatMultiset(counts))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #6033 — the inbound-fix path the fix must NOT break
// ─────────────────────────────────────────────────────────────────────────────

// TestIncremental_InboundStubRewiredExactlyOnce is the counter-guard: the naive
// "just drop updatedExistingRels" fix would trade the duplication bug for a
// silent edge-corruption bug. ResolveScoped performs real mutation on the
// SURVIVING edge set — it binds inbound stub ToIDs to the (possibly re-keyed)
// entity ID of a re-extracted entity.
//
// Setup: caller.go (unchanged) holds an edge whose ToID is a bare-name STUB
// pointing at an entity in target.go (edited, hence re-extracted). After the
// pass the edge's ToID must be the hex entity ID — and there must be exactly
// one copy of it.
func TestIncremental_InboundStubRewiredExactlyOnce(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "caller.go", "package p\n\nfunc Caller() {}\n")
	writeFile(t, repo, "target.go", "package p\n\nfunc Target() {}\n")

	caller := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "Caller", "caller.go"),
		Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "caller.go", Language: "go",
	}
	target := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "Target", "target.go"),
		Name: "Target", Kind: "SCOPE.Operation", SourceFile: "target.go", Language: "go",
	}

	// Inbound edge from the SURVIVING file with an UNRESOLVED (stub) ToID.
	// Only ResolveScoped's existing-edge walk can bind this.
	stubEdge := graph.Relationship{
		ID:     graph.RelationshipID(caller.ID, "Target", "CALLS"),
		FromID: caller.ID,
		ToID:   "Target", // stub — not a hex ID
		Kind:   "CALLS",
	}

	buildMinimalGraph(t, stateDir,
		[]graph.Entity{caller, target},
		[]graph.Relationship{stubEdge})
	seedManifest(t, repo, stateDir)

	// Edit target.go so Target is re-extracted (same name → same ID, but the
	// scoped resolver still has to bind the stub).
	writeFile(t, repo, "target.go", "package p\n\nfunc Target() { _ = 1 }\n")

	runIncremental(t, repo, stateDir)

	counts, _ := relMultiset(t, stateDir)

	boundKey := caller.ID + "→" + target.ID + ":CALLS"
	stubKey := caller.ID + "→Target:CALLS"

	if n := counts[stubKey]; n != 0 {
		t.Errorf("inbound stub ToID=%q was NOT rewired to the entity ID (found %d unbound copies) — ResolveScoped's inbound-fix was lost%s",
			"Target", n, formatMultiset(counts))
	}
	if n := counts[boundKey]; n != 1 {
		t.Errorf("inbound CALLS edge should be bound to Target's entity ID exactly once, got %d%s",
			n, formatMultiset(counts))
	}
	for key, n := range counts {
		if n != 1 {
			t.Errorf("relationship %q appears %d times, want 1 (#6033)%s", key, n, formatMultiset(counts))
		}
	}
}
