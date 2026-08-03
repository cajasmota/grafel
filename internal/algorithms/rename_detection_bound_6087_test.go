package algorithms_test

import (
	"fmt"
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

// noisePair builds `n` deleted and `n` added entities of kind `kind` whose
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

// TestDetectRenames_PairBudgetBounded pins the ceiling: with an explicit budget
// far below the full pair count, the pass must stop early and must SAY that it
// stopped early. Removing the bound makes PairsExamined blow past the budget
// and Truncated go false — this test fails either way.
func TestDetectRenames_PairBudgetBounded(t *testing.T) {
	t.Parallel()

	const n = 400 // 160_000 pairs
	prev, next := noiseDocs(n, "Function")
	prevDoc := makeDoc(prev, nil)
	newDoc := makeDoc(next, nil)

	const budget = 1000
	stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, budget)

	if !stats.Truncated {
		t.Errorf("expected Truncated=true with budget=%d over %d pairs, got false", budget, n*n)
	}
	if stats.PairsExamined > budget {
		t.Errorf("pair budget not enforced: examined=%d budget=%d", stats.PairsExamined, budget)
	}
	if stats.AddedSkipped <= 0 {
		t.Errorf("expected some added entities to be reported as skipped, got %d", stats.AddedSkipped)
	}
	if stats.PairsSkipped <= 0 {
		t.Errorf("expected the dropped pair count to be reported, got %d", stats.PairsSkipped)
	}
	if stats.PairBudget != budget {
		t.Errorf("PairBudget = %d, want %d", stats.PairBudget, budget)
	}
}

// TestDetectRenames_DefaultBudgetBounded pins that the DEFAULT entry point —
// the one cmd/grafel actually calls — is bounded too. A fix that only bounds
// the explicit-budget variant leaves the shipped path quadratic.
func TestDetectRenames_DefaultBudgetBounded(t *testing.T) {
	t.Parallel()

	if algorithms.DefaultRenamePairBudget <= 0 {
		t.Fatalf("DefaultRenamePairBudget must be positive, got %d", algorithms.DefaultRenamePairBudget)
	}

	// Enough entities that the full pair set exceeds the default budget.
	n := 1
	for n*n <= algorithms.DefaultRenamePairBudget {
		n *= 2
	}
	prev, next := noiseDocs(n, "Function")
	prevDoc := makeDoc(prev, nil)
	newDoc := makeDoc(next, nil)

	stats := algorithms.DetectRenames(prevDoc, newDoc)

	if !stats.Truncated {
		t.Errorf("default path not bounded: %d pairs, budget %d, Truncated=false",
			n*n, algorithms.DefaultRenamePairBudget)
	}
	if stats.PairsExamined > algorithms.DefaultRenamePairBudget {
		t.Errorf("default budget not enforced: examined=%d budget=%d",
			stats.PairsExamined, algorithms.DefaultRenamePairBudget)
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

	stats := algorithms.DetectRenamesBounded(prevDoc, newDoc, 1000)
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
	s1 := algorithms.DetectRenamesBounded(p1, n1, 900)
	p2, n2 := build()
	s2 := algorithms.DetectRenamesBounded(p2, n2, 900)

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
	if s1.PairsExamined != s2.PairsExamined || s1.Truncated != s2.Truncated || s1.AddedSkipped != s2.AddedSkipped {
		t.Errorf("non-deterministic stats: %+v vs %+v", s1, s2)
	}
}

// TestDetectRenames_UntruncatedRunsReportNoTruncation guards the other failure
// mode: an ordinary small delta must NOT be reported as truncated, or the
// signal is worthless.
func TestDetectRenames_UntruncatedRunsReportNoTruncation(t *testing.T) {
	t.Parallel()

	const file = "pkg/service.go"
	oldName, newName := "getUserByID", "getUserByName"
	oldID := entityID("Function", oldName, file)
	newID := entityID("Function", newName, file)

	prevDoc := makeDoc([]graph.Entity{{ID: oldID, Name: oldName, Kind: "Function", SourceFile: file}}, nil)
	newDoc := makeDoc([]graph.Entity{{ID: newID, Name: newName, Kind: "Function", SourceFile: file}}, nil)

	stats := algorithms.DetectRenames(prevDoc, newDoc)
	if stats.Truncated {
		t.Errorf("small delta wrongly reported as truncated: %+v", stats)
	}
	if stats.AddedSkipped != 0 || stats.PairsSkipped != 0 {
		t.Errorf("small delta reported dropped work: %+v", stats)
	}
	if findRenamedFrom(newDoc, newID) == nil {
		t.Errorf("ordinary rename not detected")
	}
}
