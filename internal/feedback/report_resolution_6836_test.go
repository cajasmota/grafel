package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// resolutionSection returns ONLY the text of "## 3. Resolution Disposition",
// up to the next "## " heading. Every assertion in this file is scoped through
// it: a whole-output strings.Contains survives deleting the very section it
// claims to check, and several of the strings below (the word "dynamic", the
// word "external") legitimately appear elsewhere in the rendered report.
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

// renderResolution generates a report over one document and returns just its
// resolution section.
func renderResolution(t *testing.T, rels []graph.Relationship) string {
	t.Helper()
	entities := repeat(makeEntity("e1", "X", "SCOPE.Function", "go", "x.go", 1), 50)
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

// TestResolution_TableOmitsRowsTheShapeClassifierCannotProduce (#6836) pins
// that the resolution table renders ONLY the three dispositions a ToID-shape
// classifier can actually produce.
//
// The report reads a PERSISTED graph: for each edge it has the final ToID and
// nothing else. That shape supports exactly three outcomes — 16-hex (bound),
// "ext:"-prefixed (external), any other non-empty stub (unresolved). The finer
// six-way taxonomy in internal/resolve (external-known vs external-unknown;
// bug-extractor vs bug-resolver vs dynamic) needs inputs this report never
// receives: the ExternalAllowlist, the resolver's name Index, and the
// PRE-resolution stub. So external-unknown / bug-resolver / dynamic rows can
// never be non-zero here, and a permanent "0.00%" is a false measurement
// rather than a missing one.
func TestResolution_TableOmitsRowsTheShapeClassifierCannotProduce(t *testing.T) {
	sec := renderResolution(t, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"},
		{ID: "r2", FromID: "e1c", ToID: "ext:react", Kind: "IMPORTS"},
		{ID: "r3", FromID: "e1e", ToID: "SomeBareStub", Kind: "CALLS"},
	})

	// Row forms, not bare words: the caveat prose below the table names
	// "dynamic dispatch" and "unknown externals" on purpose, and a bare-word
	// negative would fail on the explanation instead of on a row.
	for _, dead := range []string{"| external-unknown |", "| bug-resolver |", "| dynamic |"} {
		if strings.Contains(sec, dead) {
			t.Errorf("resolution table still renders %s — that counter can never be non-zero:\n%s", dead, sec)
		}
	}
	for _, live := range []string{"| resolved |", "| external |", "| unresolved |"} {
		if !strings.Contains(sec, live) {
			t.Errorf("resolution table is missing %s:\n%s", live, sec)
		}
	}
}

// TestResolution_ExtPrefixedEdgesAreExternalNotUnresolved (#6836) pins the
// external bucket in BOTH directions: an "ext:"-prefixed ToID must be counted
// external, and must NOT leak into resolved or unresolved. Asserting only that
// external is non-zero would survive a classifier that counts every edge in
// every bucket, so all three percentages are pinned exactly.
func TestResolution_ExtPrefixedEdgesAreExternalNotUnresolved(t *testing.T) {
	sec := renderResolution(t, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "ext:react", Kind: "IMPORTS"},
		{ID: "r2", FromID: "e1c", ToID: "ext:django.db", Kind: "IMPORTS"},
		{ID: "r3", FromID: "e1e", ToID: "aabb112233445566", Kind: "CALLS"},
		{ID: "r4", FromID: "e1g", ToID: "SomeBareStub", Kind: "CALLS"},
	})
	for _, want := range []string{
		"| resolved | 25.00% |",
		"| external | 50.00% |",
		"| unresolved | 25.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q in resolution section:\n%s", want, sec)
		}
	}
}

// TestResolution_DynamicAndResolverMissStubsCountAsUnresolved (#6836) uses
// stubs that internal/resolve's full classifier WOULD split three ways —
// a Python reflection builtin (dynamic), a bare name that exists in the graph
// (bug-resolver), and a "Kind:Name" stub that does not (bug-extractor). The
// persisted ToID makes them indistinguishable, so all three must land in the
// single honest "unresolved" bucket at 100%, with the other two buckets empty.
func TestResolution_DynamicAndResolverMissStubsCountAsUnresolved(t *testing.T) {
	sec := renderResolution(t, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "getattr", Kind: "CALLS"},
		{ID: "r2", FromID: "e1c", ToID: "X", Kind: "CALLS"},
		{ID: "r3", FromID: "e1e", ToID: "SCOPE.Function:Nowhere", Kind: "CALLS"},
	})
	for _, want := range []string{
		"| resolved | 0.00% |",
		"| external | 0.00% |",
		"| unresolved | 100.00% |",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q in resolution section:\n%s", want, sec)
		}
	}
}

// TestResolution_FooterDoesNotClaimEveryEdgeWasExamined (#6836) pins the
// denominator label. The total counts edges with a NON-EMPTY ToID only: an
// empty-ToID edge is iterated and then skipped ("nothing to resolve"), so
// calling the total "edges examined" overstates it. Two hex edges plus two
// empty-ToID edges must bucket as 1-5, not as the 6-20 that four "examined"
// edges would suggest, and the footer must say what it excluded.
func TestResolution_FooterDoesNotClaimEveryEdgeWasExamined(t *testing.T) {
	sec := renderResolution(t, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"},
		{ID: "r2", FromID: "e1c", ToID: "aabb112233445577", Kind: "CALLS"},
		{ID: "r3", FromID: "e1e", ToID: "", Kind: "CALLS"},
		{ID: "r4", FromID: "e1g", ToID: "", Kind: "CALLS"},
	})
	// If an empty-ToID edge were dispositioned instead of skipped it would
	// land in some bucket and move these two numbers; pinning both directions
	// catches that without relying on the coarse count bucket below.
	for _, want := range []string{"| resolved | 100.00% |", "| unresolved | 0.00% |"} {
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
	if !strings.Contains(sec, "1-5") {
		t.Errorf("expected the 2 non-empty-ToID edges to bucket as 1-5:\n%s", sec)
	}
}

// TestResolution_SectionStatesWhatTheShapeClassifierCannotSplit (#6836).
// Deleting the three rows removes a false zero; without a caveat it would also
// silently remove the reader's only signal that "external" mixes allowlisted
// and unknown packages, and that "unresolved" mixes extractor bugs, resolver
// misses and dynamic dispatch. The section must say so.
func TestResolution_SectionStatesWhatTheShapeClassifierCannotSplit(t *testing.T) {
	sec := renderResolution(t, []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"},
	})
	for _, want := range []string{"ToID shape", "unknown", "dynamic"} {
		if !strings.Contains(sec, want) {
			t.Errorf("resolution section does not mention %q:\n%s", want, sec)
		}
	}
}
