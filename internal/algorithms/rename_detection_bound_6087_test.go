package algorithms_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/graph"
)

// ─── #6087 — the rename-detection pass must be bounded ───────────────────────
//
// DetectRenames ran an unbounded pairwise comparison over (deleted × added)
// entities. With a largely dissimilar prior graph over ~119k entities that is
// ~1.4e10 pairs at ~0.9 µs/pair — hours of pinned CPU on a 49-second index.
//
// The guards below are behavioural: they assert on the work actually performed
// and on the edges actually emitted, not on the source text.

// noiseDocs builds `n` deleted and `n` added entities of kind `kind` whose
// names are the same length but share no useful structure, so nothing matches
// and every pair survives every cheap prefilter.
func noiseDocs(n int, kind string) (prev, next []graph.Entity) {
	for i := 0; i < n; i++ {
		on := fmt.Sprintf("alphaOld%05d", i)
		nn := fmt.Sprintf("betaNew__%04d", i)
		of := fmt.Sprintf("old/p%03d.go", i%64)
		nf := fmt.Sprintf("new/p%03d.go", i%64)
		prev = append(prev, graph.Entity{ID: entityID(kind, on, of), Name: on, Kind: kind, SourceFile: of})
		next = append(next, graph.Entity{ID: entityID(kind, nn, nf), Name: nn, Kind: kind, SourceFile: nf})
	}
	return prev, next
}

// renameDocs builds `n` genuine same-file, same-kind renames: name N -> name
// N+"s", which is a high-similarity in-place rename of exactly the shape a
// refactor produces. Returns the prev/new entity slices and the new→old ID map
// the caller uses to assert recall by CONTENT.
func renameDocs(n int, kind string) (prev, next []graph.Entity, wantEdge map[string]string) {
	wantEdge = make(map[string]string, n)
	for i := 0; i < n; i++ {
		f := fmt.Sprintf("pkg/svc%04d.go", i)
		on := fmt.Sprintf("handleOrder%04d", i)
		nn := fmt.Sprintf("handleOrders%04d", i)
		oid := entityID(kind, on, f)
		nid := entityID(kind, nn, f)
		prev = append(prev, graph.Entity{ID: oid, Name: on, Kind: kind, SourceFile: f})
		next = append(next, graph.Entity{ID: nid, Name: nn, Kind: kind, SourceFile: f})
		wantEdge[nid] = oid
	}
	return prev, next, wantEdge
}

// recall counts how many of the wanted new→old RENAMED_FROM edges are present
// AND point at the right old entity. Content, not counts: an implementation
// that emits the right number of edges pointing at the wrong entities scores 0.
func recall(doc *graph.Document, wantEdge map[string]string) int {
	got := 0
	for newID, oldID := range wantEdge {
		rel := findRenamedFrom(doc, newID)
		if rel != nil && rel.ToID == oldID {
			got++
		}
	}
	return got
}

// TestDetectRenames_WorkBudgetBounded pins the ceiling: with an explicit budget
// far below the full cost, the pass must stop early and must SAY that it
// stopped early. Removing the bound makes WorkUsed blow past the budget and
// Truncated go false — this test fails either way.
func TestDetectRenames_WorkBudgetBounded(t *testing.T) {
	t.Parallel()

	const n = 400 // 160_000 pairs, ~170 work units each
	prev, next := noiseDocs(n, "Function")
	prevDoc := makeDoc(prev, nil)
	newDoc := makeDoc(next, nil)

	const budget = 10_000
	stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, budget)

	if !stats.Truncated {
		t.Errorf("expected Truncated=true with budget=%d over %d pairs, got false", budget, n*n)
	}
	if stats.WorkUsed > budget {
		t.Errorf("work budget not enforced: used=%d budget=%d", stats.WorkUsed, budget)
	}
	if stats.AddedSkipped <= 0 {
		t.Errorf("expected some added entities to be reported as skipped, got %d", stats.AddedSkipped)
	}
	if stats.PairsSkipped <= 0 {
		t.Errorf("expected the dropped pair count to be reported, got %d", stats.PairsSkipped)
	}
	if stats.WorkBudget != budget {
		t.Errorf("WorkBudget = %d, want %d", stats.WorkBudget, budget)
	}
	if stats.PairsExamined != stats.PairsPrefiltered+stats.Comparisons {
		t.Errorf("accounting inconsistent: examined=%d prefiltered=%d comparisons=%d",
			stats.PairsExamined, stats.PairsPrefiltered, stats.Comparisons)
	}
}

// TestDetectRenames_BandRejectionsAreCheap pins the accounting fix: a pair
// rejected by the length band is two integer compares, and must not consume a
// full comparison's worth of budget. Under a pair-denominated budget a band
// rejection cost exactly as much as a Levenshtein, so a delta full of
// length-mismatched names burned the ceiling doing nothing.
//
// Both fixtures have the same pair count. The band-rejecting one must cost
// dramatically less work.
func TestDetectRenames_BandRejectionsAreCheap(t *testing.T) {
	t.Parallel()

	const n = 120
	const kind = "Function"

	buildPrev := func() *graph.Document {
		var prev []graph.Entity
		for i := 0; i < n; i++ {
			on := fmt.Sprintf("alphaHandler%04d", i) // 16 chars
			of := fmt.Sprintf("old/p%03d.go", i)
			prev = append(prev, graph.Entity{ID: entityID(kind, on, of), Name: on, Kind: kind, SourceFile: of})
		}
		return makeDoc(prev, nil)
	}
	buildNext := func(newName func(i int) string) *graph.Document {
		var next []graph.Entity
		for i := 0; i < n; i++ {
			nn := newName(i)
			nf := fmt.Sprintf("new/p%03d.go", i)
			next = append(next, graph.Entity{ID: entityID(kind, nn, nf), Name: nn, Kind: kind, SourceFile: nf})
		}
		return makeDoc(next, nil)
	}

	const hugeBudget = int64(1) << 40
	// Same length as the deleted names → in band → full comparisons.
	sameLen := algorithms.DetectRenamesBounded(buildPrev(),
		buildNext(func(i int) string { return fmt.Sprintf("betaProcess_%04d", i) }), hugeBudget)
	// Wildly different length → out of band → visit only.
	outOfBand := algorithms.DetectRenamesBounded(buildPrev(),
		buildNext(func(i int) string { return fmt.Sprintf("b%04d", i) }), hugeBudget)

	if sameLen.PairsExamined != outOfBand.PairsExamined {
		t.Fatalf("fixtures must examine the same pair count: %d vs %d",
			sameLen.PairsExamined, outOfBand.PairsExamined)
	}
	if outOfBand.Comparisons != 0 {
		t.Errorf("out-of-band fixture ran %d comparisons, want 0", outOfBand.Comparisons)
	}
	if sameLen.Comparisons == 0 {
		t.Fatalf("in-band fixture ran no comparisons; guard would be vacuous")
	}
	// The in-band fixture must cost at least an order of magnitude more work
	// for the identical number of pairs.
	if sameLen.WorkUsed < 10*outOfBand.WorkUsed {
		t.Errorf("band rejections are not being charged less: in-band work=%d out-of-band work=%d",
			sameLen.WorkUsed, outOfBand.WorkUsed)
	}
}

// TestDetectRenames_DefaultBudgetIsFiniteAndEnforced pins that the DEFAULT
// entry point — the one cmd/grafel actually calls — is bounded too. A fix that
// only bounds the explicit-budget variant leaves the shipped path quadratic.
//
// The fixture is deliberately cheap to build and cheap to reject: one added
// entity against a bucket whose worst-case cost exceeds the default budget, so
// the admission test fires immediately and no work is done.
func TestDetectRenames_DefaultBudgetIsFiniteAndEnforced(t *testing.T) {
	t.Parallel()

	if algorithms.DefaultRenameWorkBudget <= 0 {
		t.Fatalf("DefaultRenameWorkBudget must be positive, got %d", algorithms.DefaultRenameWorkBudget)
	}

	const nameLen = 1000
	// The worst-case cost of the single added entity's bucket is
	// len(bucket) + nameLen*sum(nameLen), i.e. ~bucket*nameLen^2. Size the
	// bucket so that comfortably exceeds the default budget — the admission
	// test must then reject it without doing any work at all.
	bucket := int(algorithms.DefaultRenameWorkBudget/int64(nameLen*nameLen)) + 256
	long := strings.Repeat("a", nameLen-8)

	prev := make([]graph.Entity, 0, bucket)
	for i := 0; i < bucket; i++ {
		on := fmt.Sprintf("%s%06d", long, i)
		f := fmt.Sprintf("old/p%05d.go", i)
		prev = append(prev, graph.Entity{ID: entityID("Function", on, f), Name: on, Kind: "Function", SourceFile: f})
	}
	nn := fmt.Sprintf("%s%06d", strings.Repeat("b", nameLen-8), 0)
	next := []graph.Entity{{ID: entityID("Function", nn, "new/p.go"), Name: nn, Kind: "Function", SourceFile: "new/p.go"}}

	stats := algorithms.DetectRenames(makeDoc(prev, nil), makeDoc(next, nil))

	if !stats.Truncated {
		t.Errorf("default path not bounded: worst-case cost ~%d, budget %d, Truncated=false",
			int64(bucket)*int64(1+nameLen*nameLen), algorithms.DefaultRenameWorkBudget)
	}
	if stats.WorkUsed > algorithms.DefaultRenameWorkBudget {
		t.Errorf("default budget not enforced: used=%d budget=%d",
			stats.WorkUsed, algorithms.DefaultRenameWorkBudget)
	}
	if stats.AddedSkipped != 1 {
		t.Errorf("AddedSkipped = %d, want 1", stats.AddedSkipped)
	}
	// Admission rejected the bucket up front, so nothing was scanned.
	if stats.WorkUsed != 0 || stats.PairsExamined != 0 {
		t.Errorf("over-budget bucket was partially scanned: work=%d pairs=%d",
			stats.WorkUsed, stats.PairsExamined)
	}
}

// TestDetectRenames_DefaultBudgetCoversARealisticRefactor is the guard a
// too-small budget fails. It is the direct regression test for the first
// version of this fix, whose 2e6-PAIR budget returned 666 of 3000 renames on
// exactly this shape — trading a hang for silent incompleteness, the worse bug.
//
// 3000 same-kind renames-in-place is a large but entirely ordinary refactor
// (a package-wide method rename). It must be detected in FULL, with no
// truncation, under the shipped default.
func TestDetectRenames_DefaultBudgetCoversARealisticRefactor(t *testing.T) {
	t.Parallel()

	const n = 3000
	prev, next, wantEdge := renameDocs(n, "Method")
	newDoc := makeDoc(next, nil)

	stats := algorithms.DetectRenames(makeDoc(prev, nil), newDoc)

	if stats.Truncated {
		t.Errorf("a %d-entity same-kind refactor was truncated under the default budget "+
			"(used=%d budget=%d, %d added entities dropped) — the budget is too small",
			n, stats.WorkUsed, stats.WorkBudget, stats.AddedSkipped)
	}
	if got := recall(newDoc, wantEdge); got != n {
		t.Errorf("recall %d/%d renames under the default budget", got, n)
	}
}

// TestDetectRenames_TruncationKeepsCheapCandidates asserts detection quality
// survives the cap: a genuine rename whose candidate set is tiny must still be
// found even when a huge same-kind noise set exhausts the budget. Asserted on
// edge CONTENT (which old entity, under which name), so a pass that emits the
// right number of edges pointing at the wrong entities still fails.
func TestDetectRenames_TruncationKeepsCheapCandidates(t *testing.T) {
	t.Parallel()

	const n = 400
	prev, next := noiseDocs(n, "Function")

	// A genuine rename in a kind of its own, so its candidate bucket is tiny.
	const file = "pkg/repo.go"
	oldName, newName := "loadUserRecord", "loadUserRecords"
	oldID := entityID("Method", oldName, file)
	newID := entityID("Method", newName, file)
	prev = append(prev, graph.Entity{ID: oldID, Name: oldName, Kind: "Method", SourceFile: file})
	next = append(next, graph.Entity{ID: newID, Name: newName, Kind: "Method", SourceFile: file})

	prevDoc := makeDoc(prev, nil)
	newDoc := makeDoc(next, nil)

	stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, 10_000)
	if !stats.Truncated {
		t.Fatalf("fixture did not truncate; the guard would be vacuous")
	}

	rel := findRenamedFrom(newDoc, newID)
	if rel == nil {
		t.Fatalf("genuine rename %s -> %s lost under truncation", oldName, newName)
	}
	if rel.ToID != oldID {
		t.Errorf("RENAMED_FROM points at %q, want %q", rel.ToID, oldID)
	}
	if got := rel.PropGet("old_name"); got != oldName {
		t.Errorf("old_name = %q, want %q", got, oldName)
	}
}

// TestDetectRenames_BoundedIsDeterministic pins that the cap falls in the same
// place on every run: same edges, same targets, same stats. A map-iteration
// dependent cap would produce a different surviving edge set across runs.
func TestDetectRenames_BoundedIsDeterministic(t *testing.T) {
	t.Parallel()

	const n = 200
	build := func() (*graph.Document, *graph.Document) {
		prev, next := noiseDocs(n, "Function")
		// Sprinkle in real renames so there is content to compare.
		for i := 0; i < 8; i++ {
			f := fmt.Sprintf("pkg/svc%02d.go", i)
			on := fmt.Sprintf("handleOrder%02d", i)
			nn := fmt.Sprintf("handleOrders%02d", i)
			prev = append(prev, graph.Entity{ID: entityID("Method", on, f), Name: on, Kind: "Method", SourceFile: f})
			next = append(next, graph.Entity{ID: entityID("Method", nn, f), Name: nn, Kind: "Method", SourceFile: f})
		}
		return makeDoc(prev, nil), makeDoc(next, nil)
	}

	fingerprint := func(doc *graph.Document) []string {
		var out []string
		for _, r := range doc.Relationships {
			if r.Kind == algorithms.RelKindRenamedFrom {
				out = append(out, r.FromID+"->"+r.ToID+"|"+r.PropGet("method")+"|"+r.PropGet("old_name")+"|"+r.PropGet("confidence"))
			}
		}
		return out
	}

	p1, n1 := build()
	s1 := algorithms.DetectRenamesBounded(p1, n1, 60_000)
	p2, n2 := build()
	s2 := algorithms.DetectRenamesBounded(p2, n2, 60_000)

	// The determinism being pinned is specifically determinism UNDER
	// truncation; without this assertion the fixture could drift into fitting
	// the budget and the guard would silently stop testing anything.
	if !s1.Truncated {
		t.Fatalf("fixture no longer truncates; the determinism guard would be vacuous")
	}

	f1, f2 := fingerprint(n1), fingerprint(n2)
	if len(f1) == 0 {
		t.Fatalf("fixture produced no rename edges; guard would be vacuous")
	}
	if len(f1) != len(f2) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(f1), len(f2))
	}
	for i := range f1 {
		if f1[i] != f2[i] {
			t.Errorf("edge %d differs across runs:\n  %s\n  %s", i, f1[i], f2[i])
		}
	}
	if s1.WorkUsed != s2.WorkUsed || s1.Truncated != s2.Truncated || s1.AddedSkipped != s2.AddedSkipped {
		t.Errorf("non-deterministic stats: %+v vs %+v", s1, s2)
	}
}

// TestDetectRenames_FittingDeltaReportsNoTruncation guards the other failure
// mode: a delta that fits must NOT be reported as truncated, or the signal is
// worthless. Uses a 1000×1000 delta — large enough that a budget
// implementation which truncates on any sizeable input fails here, unlike a
// 1×1 delta which almost anything passes.
func TestDetectRenames_FittingDeltaReportsNoTruncation(t *testing.T) {
	t.Parallel()

	const n = 1000
	prev, next, wantEdge := renameDocs(n, "Function")
	newDoc := makeDoc(next, nil)

	stats := algorithms.DetectRenamesBounded(makeDoc(prev, nil), newDoc, algorithms.DefaultRenameWorkBudget)

	if stats.Truncated {
		t.Errorf("fitting %dx%d delta wrongly reported as truncated: %+v", n, n, stats)
	}
	if stats.AddedSkipped != 0 || stats.PairsSkipped != 0 {
		t.Errorf("fitting delta reported dropped work: added=%d pairs=%d",
			stats.AddedSkipped, stats.PairsSkipped)
	}
	if got := recall(newDoc, wantEdge); got != n {
		t.Errorf("recall %d/%d on a fitting delta", got, n)
	}
}

// TestDetectRenames_BandBoundaryIsInclusive pins the exact off-by-one the
// losslessness argument depends on.
//
// lengthBandOK admits a pair when 1 - diff/maxLen >= 0.65. At maxLen=20,
// diff=7 that expression is 0.65000000000000002 — accepted by the band AND by
// the inner-loop similarity floor, because they are the same float
// computation. Tightening the band to a strict `>` silently drops this pair,
// and every other pair on the boundary, losing real renames. That mutation
// survives every other test in this package.
func TestDetectRenames_BandBoundaryIsInclusive(t *testing.T) {
	t.Parallel()

	const file = "pkg/handlers.go"
	// 20 chars -> 13 chars by deleting a 7-char suffix: distance exactly 7,
	// similarity exactly 1 - 7/20 = 0.65, the inclusive boundary.
	oldName := "handleUserRequest123"
	newName := "handleUserReq"
	if len(oldName) != 20 || len(newName) != 13 {
		t.Fatalf("fixture drift: len(old)=%d len(new)=%d, want 20 and 13", len(oldName), len(newName))
	}

	oldID := entityID("Function", oldName, file)
	newID := entityID("Function", newName, file)
	prevDoc := makeDoc([]graph.Entity{{ID: oldID, Name: oldName, Kind: "Function", SourceFile: file}}, nil)
	newDoc := makeDoc([]graph.Entity{{ID: newID, Name: newName, Kind: "Function", SourceFile: file}}, nil)

	stats := algorithms.DetectRenames(prevDoc, newDoc)

	if stats.Comparisons != 1 {
		t.Fatalf("boundary pair was rejected by the length band: comparisons=%d prefiltered=%d "+
			"(the band must admit sim == 0.65 exactly, or real renames are lost)",
			stats.Comparisons, stats.PairsPrefiltered)
	}
	rel := findRenamedFrom(newDoc, newID)
	if rel == nil {
		t.Fatalf("boundary rename %s -> %s not detected", oldName, newName)
	}
	if rel.ToID != oldID {
		t.Errorf("RENAMED_FROM points at %q, want %q", rel.ToID, oldID)
	}
}
