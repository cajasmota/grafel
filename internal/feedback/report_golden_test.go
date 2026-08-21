package feedback

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
)

// TestGenerate_GoldenFB is the regression guard for issue #5683 (D1/D2/D3). It
// runs feedback.Generate over a REAL graph.fb-loaded document — not a hand-built
// in-memory fixture — and asserts that the three collector metrics that read
// fields which do not survive the FB round-trip now report real values.
//
// The fixture (testdata/golden/graph.fb) is a genuine graph.fb: it was produced
// by round-tripping an indexed multi-language golden graph through the real
// fbwriter, so it speaks the production dialect the unit fixtures used to lie
// about — canonical SCOPE.* / bare kinds, no Properties["resolution"] tag, and
// ToID-shaped resolution.
//
// Every entity in it also carries EndLine == 0. That was once a property of the
// FB path itself: the Entity table had no end-line slot, so the value could not
// survive. #6236 added the slot, so it is no longer true of graph.fb in general
// — it is now just the shape of THIS fixture, which was captured before the
// slot existed. That shape is what keeps D1 honest, since the StartLine anchor
// is the only thing left to count, so the sanity check below still pins it.
//
// Against the pre-#5683 collector this test FAILS on all three axes:
//   - D1: source-window completeness == 0.0% (EndLine > StartLine never holds).
//   - D2: field-extraction ClassTotal == 0 ("No class or model entities found").
//   - D3: ResolutionTotal == 0 ("no resolution property found on edges").
func TestGenerate_GoldenFB(t *testing.T) {
	doc, err := graph.LoadGraphFromDir("testdata/golden")
	if err != nil {
		t.Fatalf("LoadGraphFromDir(testdata/golden): %v", err)
	}
	if len(doc.Entities) == 0 || len(doc.Relationships) == 0 {
		t.Fatalf("golden graph.fb empty: %d entities, %d rels", len(doc.Entities), len(doc.Relationships))
	}

	// Sanity: confirm the fixture is still span-less — no entity carries an
	// EndLine, and the class/model kinds are canonical (not lowercase). This is
	// what makes the assertions below meaningful regressions.
	for i := range doc.Entities {
		if doc.Entities[i].EndLine != 0 {
			t.Fatalf("fixture is no longer span-less: entity %s has EndLine=%d (expected 0)",
				doc.Entities[i].ID, doc.Entities[i].EndLine)
		}
	}

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "golden"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.IsSuppressed() {
		t.Fatalf("golden report unexpectedly suppressed (entities=%d)", r.TotalEntities)
	}

	// --- D1: source-window completeness must be > 0 -------------------------
	if r.SourceWindow.TotalWithWindow == 0 || r.SourceWindow.PctComplete <= 0 {
		t.Errorf("D1: source-window completeness is zero (with-window=%d, pct=%.1f%%); "+
			"StartLine anchor should be counted", r.SourceWindow.TotalWithWindow, r.SourceWindow.PctComplete)
	}
	// Cross-check against a direct StartLine count.
	wantWindow := 0
	for i := range doc.Entities {
		if doc.Entities[i].StartLine > 0 {
			wantWindow++
		}
	}
	if r.SourceWindow.TotalWithWindow != wantWindow {
		t.Errorf("D1: with-window=%d, want %d (entities with StartLine>0)",
			r.SourceWindow.TotalWithWindow, wantWindow)
	}

	// --- D2: field-extraction must find the class/model entities ------------
	if r.FieldExtractionRate.ClassTotal == 0 {
		t.Errorf("D2: field-extraction found no class/model entities in a graph with Model + SCOPE.Schema kinds")
	}
	// Field LEAVES (Subtype == "field") are themselves class/model/schema-tail
	// kinds (SCOPE.Schema) but are not class/model CONTAINERS — they must be
	// excluded from the class-like count (Fix 1), or every field doubles as a
	// "class" and the zero-fields rate is guaranteed to read 100%. The same
	// holds for the other nonClassSubtypes (enum/const/file — #6536).
	wantClassLike := 0
	for i := range doc.Entities {
		if isClassLikeKind(doc.Entities[i].Kind) && !nonClassSubtypes[doc.Entities[i].Subtype] {
			wantClassLike++
		}
	}
	if r.FieldExtractionRate.ClassTotal != wantClassLike {
		t.Errorf("D2: ClassTotal=%d, want %d (class/model/schema-like kinds, excluding non-container subtypes)",
			r.FieldExtractionRate.ClassTotal, wantClassLike)
	}

	// --- D3: resolution disposition must be non-empty and match ToID shape --
	if r.ResolutionTotal == 0 {
		t.Errorf("D3: ResolutionTotal is zero; disposition must be derived from ToID shape")
	}
	if r.Resolution.BugExtractorPct == 0 && r.Resolution.ResolvedPct == 0 && r.Resolution.ExternalKnownPct == 0 {
		t.Errorf("D3: resolution vector is all-zero; expected a resolved/external/bug split")
	}
	// The resolved fraction (hex + ext) must equal the ToID-derived import
	// fidelity computed the SAME way grafel_stats/orient does, over the same
	// edge universe the collector examined (every non-empty ToID).
	var total, resolved int
	for i := range doc.Relationships {
		toID := doc.Relationships[i].ToID
		if toID == "" {
			continue
		}
		total++
		if resolve.IsResolvedToID(toID) {
			resolved++
		}
	}
	if total == 0 {
		t.Fatal("D3: no non-empty ToID edges in fixture")
	}
	if r.ResolutionTotal != total {
		t.Errorf("D3: ResolutionTotal=%d, want %d (non-empty-ToID edges)", r.ResolutionTotal, total)
	}
	wantResolvedPct := 100.0 * float64(resolved) / float64(total)
	gotResolvedPct := r.Resolution.ResolvedPct + r.Resolution.ExternalKnownPct
	if diff := gotResolvedPct - wantResolvedPct; diff > 0.01 || diff < -0.01 {
		t.Errorf("D3: resolved+external = %.3f%%, want %.3f%% (ToID-derived fidelity)",
			gotResolvedPct, wantResolvedPct)
	}

	t.Logf("golden FB: entities=%d rels=%d | D1 window=%.1f%% (%d) | D2 classLike=%d | D3 total=%d resolved+ext=%.2f%% bug=%.2f%%",
		len(doc.Entities), len(doc.Relationships),
		r.SourceWindow.PctComplete, r.SourceWindow.TotalWithWindow,
		r.FieldExtractionRate.ClassTotal,
		r.ResolutionTotal, gotResolvedPct, r.Resolution.BugExtractorPct)
}
