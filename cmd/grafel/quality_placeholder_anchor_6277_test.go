package main

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/quality"
)

// End-to-end proof that the golden benchmark now SEES the ORM-anchor
// substitution, over the real indexer rather than a hand-built document
// (Refs #6277).
//
// The hermetic half lives in internal/quality/placeholder_anchor_6277_test.go
// and pins the matcher. This half pins the consequence: three fixtures score
// one must-have lower because the entity that was satisfying it is ormlink's
// anchor, not the declaration the fixture names.
//
// WHY ABSOLUTE NUMBERS AND NAMED ENTITIES, and not "the ratchet is green":
// scripts/quality/ratchet.py grades against
// internal/quality/golden/baseline.json and its own `update` subcommand
// rewrites that baseline from the current run, so a test asserting only that
// `ratchet check` exits 0 survives a revert of isPlaceholderAnchor — the floor
// would be re-recorded one higher and the gate would still pass. The same
// reasoning is written out on TestJobFixturesAbsoluteRecall_6260, which was
// added after that failure mode was demonstrated on #6260.
func TestPlaceholderAnchorIsNotCountedAsRecall_6277(t *testing.T) {
	// Pinned OFF for the same reason as TestJobFixturesAbsoluteRecall_6260:
	// classifyAndReadWithProgress ORs this variable with the per-Index option,
	// so with it set in the ambient environment the numbers below no longer
	// depend on qualityIndexOptions passing WithCustomExtractors(true), and
	// python-django-mini's figure in particular comes from that pass.
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")

	cases := []struct {
		fixture string
		// anchoredKind/anchoredName is the must-have that ormlink's anchor was
		// satisfying before this change.
		anchoredKind, anchoredName string
		wantEntityFound            int
		wantEntityExpected         int
		// Relationship recall must NOT move: the placeholder check is scoped to
		// entity expectations, and edge endpoints still resolve to anchors.
		wantRelFound    int
		wantRelExpected int
	}{
		// The anchor here carries StartLine 0 and collides with the real class
		// on Kind+Name+SourceFile.
		{"java-spring-mini", "SCOPE.Component", "User", 24, 25, 15, 21},
		// The anchor here carries StartLine 1 / EndLine 24 — a source span. It
		// is caught by the producer's subtype tag and would be missed by any
		// "reject entities with no span" rule.
		{"elixir-phoenix-mini", "SCOPE.Component", "Demo.Schemas.User", 21, 22, 9, 10},
		// Already below its historic high for an unrelated reason (#6276,
		// ActiveUserManager); this drops it one further.
		{"python-django-mini", "SCOPE.Component", "User", 24, 28, 5, 12},
	}

	goldenDir, err := filepath.Abs(filepath.Join("..", "..", "internal", "quality", "golden"))
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			fixtureDir := filepath.Join(goldenDir, tc.fixture)
			fix, err := quality.LoadFixture(fixtureDir)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			graphPath := filepath.Join(t.TempDir(), "graph.json")
			// Same entry point and option list as runQuality.
			if err := Index(quality.SourceDir(fixtureDir), graphPath, fix.Name,
				nil /*skip*/, false /*pretty*/, false, /*jsonStats*/
				qualityIndexOptions()...); err != nil {
				t.Fatalf("index fixture: %v", err)
			}
			doc, err := loadDocument(graphPath)
			if err != nil {
				t.Fatalf("load graph: %v", err)
			}
			rep := quality.Evaluate(fix, doc)

			// The named expectation, not just a count. Reverting
			// isPlaceholderAnchor flips exactly this to Found=true.
			var seen bool
			for _, r := range rep.EntityResults {
				if r.Expected.Kind != tc.anchoredKind || r.Expected.Name != tc.anchoredName {
					continue
				}
				seen = true
				if r.Found {
					t.Errorf("must-have %s %s is reported FOUND, bound to %q — the only "+
						"entity in this fixture's graph carrying that kind and name is "+
						"ormlink's MAPS_TO anchor, which holds none of the model's members",
						r.Expected.Kind, r.Expected.Name, r.MatchedID)
				}
				if r.MatchedID != "" {
					t.Errorf("must-have %s %s bound MatchedID=%q, want empty",
						r.Expected.Kind, r.Expected.Name, r.MatchedID)
				}
			}
			if !seen {
				t.Fatalf("fixture no longer declares a must-have %s %s — expected.json was "+
					"edited; update this test deliberately", tc.anchoredKind, tc.anchoredName)
			}

			if rep.EntityExpected != tc.wantEntityExpected {
				t.Fatalf("entity_expected = %d, want %d (expected.json changed; update this test deliberately)",
					rep.EntityExpected, tc.wantEntityExpected)
			}
			if rep.EntityFound != tc.wantEntityFound {
				t.Errorf("entity_found = %d, want %d", rep.EntityFound, tc.wantEntityFound)
			}
			if rep.RelExpected != tc.wantRelExpected {
				t.Fatalf("relationship_expected = %d, want %d", rep.RelExpected, tc.wantRelExpected)
			}
			if rep.RelFound != tc.wantRelFound {
				t.Errorf("relationship_found = %d, want %d — this figure must not move; "+
					"the placeholder check is scoped to entity expectations",
					rep.RelFound, tc.wantRelFound)
			}
		})
	}
}
