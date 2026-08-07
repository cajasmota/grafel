package coverage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/types"
)

// TestIssue6236SpanAttributionSurvivesFBRoundTrip is the consumer-layer
// regression test for #6236: the reported damage was not "a field reads back
// as 0", it was that coverage attribution silently changed branch. The Entity
// table had source_line and no end line, so every entity loaded from graph.fb
// arrived with EndLine 0; isFileScope treats EndLine <= 0 as "no usable span",
// so a span-bearing function could only ever be handed the whole-file roll-up.
// A group indexed from graph.json and the same group indexed from graph.fb
// reported different coverage for the same function.
//
// The entities here go through the real fbwriter and the real loader, and the
// records handed to Attribute come from documentRecords — the same projection
// the live indexer pass (enrich.go) uses — so nothing in the chain can
// re-supply an EndLine the binary format dropped.
func TestIssue6236SpanAttributionSurvivesFBRoundTrip(t *testing.T) {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC),
		Repo:        "fixture-6236",
		Entities: []graph.Entity{
			{ID: "ent-deposit", Name: "Vault.deposit", Kind: string(types.EntityKindFunction), Subtype: "function", SourceFile: "src/vault.ts", Language: "typescript", StartLine: 10, EndLine: 14},
			// Control: a span-less function must still land on the whole-file
			// roll-up, so the fix cannot pass by dropping the branch.
			{ID: "ent-legacy", Name: "Vault.legacy", Kind: string(types.EntityKindFunction), Subtype: "function", SourceFile: "src/vault.ts", Language: "typescript", StartLine: 2},
			// Control: a module entity is file-scope by kind, span or not.
			{ID: "ent-module", Name: "vault", Kind: string(types.EntityKindModule), SourceFile: "src/vault.ts", Language: "typescript"},
		},
	}
	doc.Stats.Entities = len(doc.Entities)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Lines 1-5 are instrumented and never executed; lines 10-14 are
	// instrumented and always executed. The whole-file roll-up is 5/10 and the
	// 10-14 span is 5/5, so the two branches of Attribute produce numbers that
	// cannot be mistaken for one another.
	rep := &Report{
		Source: SourceLCOV,
		Files: []FileCoverage{{
			Path:         "src/vault.ts",
			LineHits:     map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0, 10: 3, 11: 3, 12: 3, 13: 3, 14: 3},
			CoveredLines: 5,
			TotalLines:   10,
		}},
	}

	attrs := Attribute(documentRecords(loaded), rep, "")
	got := make(map[string]Attribution, len(attrs))
	for _, a := range attrs {
		got[a.EntityID] = a
	}

	for _, tc := range []struct {
		id            string
		wantPct       float64
		wantCovered   int
		wantTotal     int
		wantScopeDesc string
	}{
		{"ent-deposit", 100.0, 5, 5, "its own 10-14 span"},
		{"ent-legacy", 50.0, 5, 10, "the whole-file roll-up"},
		{"ent-module", 50.0, 5, 10, "the whole-file roll-up"},
	} {
		a, ok := got[tc.id]
		if !ok {
			t.Errorf("entity %s: no attribution emitted", tc.id)
			continue
		}
		if a.CoveragePct != tc.wantPct || a.CoveredLines != tc.wantCovered || a.TotalLines != tc.wantTotal {
			t.Errorf("entity %s: got %.1f%% %d/%d, want %.1f%% %d/%d from %s",
				tc.id, a.CoveragePct, a.CoveredLines, a.TotalLines,
				tc.wantPct, tc.wantCovered, tc.wantTotal, tc.wantScopeDesc)
		}
	}
}
