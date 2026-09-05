package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
)

// resolutionSection returns ONLY the text of "## 3. Resolution Disposition",
// up to the next "## " heading. Every assertion in this file is scoped through
// it: a whole-output strings.Contains survives deleting the very section it
// claims to check, and several of the strings below ("dynamic", "external")
// legitimately appear elsewhere in the rendered report.
func resolutionSection(t *testing.T, out string) string {
	t.Helper()
	const head = "## 3. Resolution Disposition"
	i := strings.Index(out, head)
	if i < 0 {
		t.Fatalf("rendered report has no %q section:\n%s", head, out)
	}
	rest := out[i+len(head):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// resolutionFixtureEntities returns the 50 entities every fixture below needs
// to clear minEntitiesForReport, plus any extra the caller supplies. The extras
// exist so a stub can be made to NAME something in the graph (bug-resolver) or
// name nothing (bug-extractor) — that distinction is the resolver name index's,
// and it is the whole reason those two are separate dispositions.
func resolutionFixtureEntities(extra ...graph.Entity) []graph.Entity {
	base := repeat(makeEntity("e1", "X", "SCOPE.Function", "go", "x.go", 1), 50)
	return append(base, extra...)
}

// renderResolution generates a report over one document and returns just its
// resolution section.
func renderResolution(t *testing.T, entities []graph.Entity, rels []graph.Relationship) string {
	t.Helper()
	r, err := Generate(context.Background(), []*graph.Document{makeDoc(entities, rels)}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return resolutionSection(t, sb.String())
}

// TestResolution_EveryDispositionInTheTaxonomyHasARow (#6836) pins that the
// table is driven by resolve.AllDispositions rather than a hand-written list.
//
// The defect this replaces was three rows wired to counters nothing
// incremented, rendering a permanent 0.00% under a footer claiming all six
// dispositions had been evaluated. The structural fix is that rows and counts
// come from the SAME source — the classifier's own taxonomy — so a disposition
// can no longer exist in resolve and be silently absent from, or unfed by, the
// table. This test fails if either side is hand-maintained again.
func TestResolution_EveryDispositionInTheTaxonomyHasARow(t *testing.T) {
	sec := renderResolution(t, resolutionFixtureEntities(), []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"},
	})
	if len(resolve.AllDispositions) < 6 {
		t.Fatalf("taxonomy unexpectedly small (%d) — fixture assumption broken", len(resolve.AllDispositions))
	}
	for _, d := range resolve.AllDispositions {
		row := "| " + d.String() + " |"
		if !strings.Contains(sec, row) {
			t.Errorf("resolution table has no row for disposition %q:\n%s", d.String(), sec)
		}
	}
}

// TestResolution_ExternalKnownAndUnknownAreSeparated (#6836) is the first of
// the three rows that were structurally dead before this change.
//
// "ext:react" is on the compiled-in allowlist; "ext:not_a_real_pkg_xyzzy" is
// not. Both persist in the graph with the SAME "ext:" prefix, so a ToID-shape
// classifier cannot tell them apart and reported both as external-known; only
// the allowlist predicate can, and the report now consults it. Both
// percentages are pinned so a classifier that collapsed the two back into one
// bucket fails, in either direction.
func TestResolution_ExternalKnownAndUnknownAreSeparated(t *testing.T) {
	sec := renderResolution(t, resolutionFixtureEntities(), []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "ext:react", Kind: "IMPORTS"},
		{ID: "r2", FromID: "e1c", ToID: "ext:not_a_real_pkg_xyzzy", Kind: "IMPORTS"},
	})
	for _, want := range []string{
		"| external-known | 50.00% |",
		"| external-unknown | 50.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q — known and unknown externals must not share a bucket:\n%s", want, sec)
		}
	}
}

// TestResolution_BugResolverIsSeparatedFromBugExtractor (#6836) is the second
// dead row.
//
// Both stubs below are raw, unresolved and identical in SHAPE. The only thing
// that separates them is whether the name exists in the graph: "Widget" does
// (an entity is added for it), "NoSuchNameAnywhere" does not. That lookup is
// the resolver name index's, which is why the report now builds one. Bug-
// extractor at 100% — the pre-change behaviour — must fail here.
func TestResolution_BugResolverIsSeparatedFromBugExtractor(t *testing.T) {
	entities := resolutionFixtureEntities(
		makeEntity("w1", "Widget", "SCOPE.Class", "go", "w.go", 1),
	)
	sec := renderResolution(t, entities, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "Widget", Kind: "CALLS"},
		{ID: "r2", FromID: "e1c", ToID: "NoSuchNameAnywhere", Kind: "CALLS"},
	})
	for _, want := range []string{
		"| bug-resolver | 50.00% |",
		"| bug-extractor | 50.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q — a stub naming a real entity is a resolver miss, not an extractor bug:\n%s", want, sec)
		}
	}
}

// TestResolution_DynamicIsSeparatedFromBugExtractor (#6836) is the third dead
// row. A Python reflection builtin is intrinsically runtime-dispatched, not an
// extractor defect, and the pre-change ToID-shape classifier counted it as
// bug-extractor — inflating the headline bug bucket with edges nobody can fix.
//
// The edge carries Properties["language"], which is what gates the per-language
// dynamic catalog; the non-dynamic stub alongside it holds bug-extractor
// non-zero so this cannot pass by classifying everything dynamic.
func TestResolution_DynamicIsSeparatedFromBugExtractor(t *testing.T) {
	dyn := graph.Relationship{ID: "r1", FromID: "e1a", ToID: "getattr", Kind: "CALLS"}
	dyn = dyn.WithProperties(map[string]string{"language": "python"})
	sec := renderResolution(t, resolutionFixtureEntities(), []graph.Relationship{
		dyn,
		{ID: "r2", FromID: "e1c", ToID: "NoSuchNameAnywhere", Kind: "CALLS"},
	})
	for _, want := range []string{
		"| dynamic | 50.00% |",
		"| bug-extractor | 50.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q — reflection dispatch is not an extractor bug:\n%s", want, sec)
		}
	}
}

// TestResolution_ExtPrefixedDynamicBuiltinIsDynamicNotExternal (#6836, #95)
// pins the ToOriginal reconstruction, which is the one place this report has
// to rebuild an input the persisted graph does not store.
//
// The external synthesiser stamps reflection builtins with an "ext:" prefix
// because they sit in the stdlib stop-list, and classifyDispositionLang runs
// its dynamic check on the ORIGINAL stub BEFORE the ext: check specifically so
// those land in `dynamic`. The dynamic catalog matches "getattr" and NOT
// "ext:getattr", so feeding it the raw ToID silently defeats #95 — measured on
// testdata/golden, that moves 6 of 796 edges from `dynamic` into
// `external-unknown`. Trimming the prefix recovers the stub exactly.
//
// external-unknown is pinned at 0 as well as dynamic at 100: asserting only
// that dynamic is non-zero would survive a classifier that double-counted the
// edge into both buckets.
func TestResolution_ExtPrefixedDynamicBuiltinIsDynamicNotExternal(t *testing.T) {
	rel := graph.Relationship{ID: "r1", FromID: "e1a", ToID: "ext:getattr", Kind: "CALLS"}
	rel = rel.WithProperties(map[string]string{"language": "python"})
	sec := renderResolution(t, resolutionFixtureEntities(), []graph.Relationship{rel})
	for _, want := range []string{
		"| dynamic | 100.00% |",
		"| external-unknown | 0.00% |",
		"| external-known | 0.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q — an ext:-stamped reflection builtin is dynamic dispatch (#95):\n%s", want, sec)
		}
	}
}

// TestResolution_FooterDoesNotClaimEveryEdgeWasExamined (#6836) pins the
// denominator label. The total counts edges with a NON-EMPTY ToID only: an
// empty-ToID edge has nothing to resolve, so it is skipped, and calling the
// total "edges examined" overstates it.
//
// Both surviving percentages are pinned: if an empty-target edge were given a
// disposition it would land in some bucket and move them. A count-bucket
// assertion would not catch that — countRangeLabel returns "1-5" for anything
// up to 5, so 2 and 4 are indistinguishable through it.
func TestResolution_FooterDoesNotClaimEveryEdgeWasExamined(t *testing.T) {
	sec := renderResolution(t, resolutionFixtureEntities(), []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"},
		{ID: "r2", FromID: "e1c", ToID: "aabb112233445577", Kind: "CALLS"},
		{ID: "r3", FromID: "e1e", ToID: "", Kind: "CALLS"},
		{ID: "r4", FromID: "e1g", ToID: "", Kind: "CALLS"},
	})
	for _, want := range []string{"| resolved | 100.00% |", "| bug-extractor | 0.00% |"} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q — empty-ToID edges must carry no disposition:\n%s", want, sec)
		}
	}
	if strings.Contains(sec, "edges examined") {
		t.Errorf("footer still claims %q — the total excludes empty-ToID edges:\n%s", "edges examined", sec)
	}
	if !strings.Contains(sec, "empty") {
		t.Errorf("footer does not say that empty-target edges are excluded:\n%s", sec)
	}
}
