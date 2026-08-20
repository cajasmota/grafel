package main

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/quality"
)

// End-to-end proof, over the real indexer rather than a hand-built document,
// that #6275's dedup-by-ID fix (cmd/grafel/index.go's "an ormlink sentinel
// must never win identity fields over a real collision partner") restores
// the must-have entity these three fixtures previously scored as satisfied
// only by ormlink's thin MAPS_TO anchor (Refs #6277, #6275).
//
// java-spring-mini and elixir-phoenix-mini are FULLY FIXED by #6275: the
// must-have now binds to a real, content-ful entity (not the sentinel), and
// entity_found/relationship_found are back at their historic highs.
// python-django-mini's OWN "SCOPE.Component User" must-have is NOT fixed by
// #6275 — it is a separate defect (#6276): the Django custom pass folds the
// base class node into a bare "Model"-kind record (see
// internal/engine/classfold.go's FrameworkClassKindPriority, which ranks
// bare "Model" as a legitimate framework-typed survivor, not a #6104 twin),
// so no non-sentinel record ever collides with the ormlink sentinel for
// (SCOPE.Component, User, users/models.py) and there is nothing for #6275's
// dedup fix to promote. python-django-mini's entity_found/relationship_found
// DO move here (+1/+2) — an unrelated, legitimate side effect of #6275's
// twin_of anchor-id fix (cmd/grafel/index.go's stampEntityIDs) repairing a
// DIFFERENT symbol's resolution in the same fixture — verified by the
// unchanged Found=false/MatchedID="" assertion on the User case below.
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
		// anchoredKind/anchoredName is the must-have ormlink's anchor used to
		// mask before #6275; wantFound/wantRealMatch report the POST-#6275 state.
		anchoredKind, anchoredName string
		// wantFound is whether the must-have is now satisfied by a real
		// (non-placeholder) entity. false only for python-django-mini, whose
		// own kind-mismatch defect (#6276) is untouched by this fix.
		wantFound          bool
		wantEntityFound    int
		wantEntityExpected int
		wantRelFound       int
		wantRelExpected    int
	}{
		// #6275 FIXED: the real class node no longer folds into its own
		// #6104 Hibernate twin (SCOPE.Schema facet) and no longer loses the
		// #4406 dedup-by-ID identity race to ormlink's sentinel, so the
		// must-have binds to the real, content-ful entity. Both figures are
		// back at their historic highs (25/25, 21/21).
		//
		// #6429 moved all four figures 25/25 + 21/21 -> 29/29 + 27/27. This is
		// a deliberate expected.json edit, not drift: Spring's route->handler
		// hop dangled (the AST pass discarded the handler method name and the
		// endpoint synthesizer stamped source_handler at the route's own
		// path), and expected.json asserted that PLACEHOLDER as correct with
		// must_exist:true (#6441). The two `to_bare_name: "Controller:<method>"`
		// ROUTES_TO rows were amended to the resolved
		// to_name+to_kind+to_file form, the two previously-ungraded /users
		// routes were added, and Spring's share of #6374 — 4
		// http_endpoint_definition entities + 4 IMPLEMENTS edges — was graded
		// for the first time. The must-have this test actually guards
		// (SCOPE.Component User) is untouched by #6429: it still binds to the
		// real class record, never the ormlink sentinel.
		{"java-spring-mini", "SCOPE.Component", "User", true, 29, 29, 27, 27},
		// #6275 FIXED via the SAME dedup-by-ID mechanism as java-spring-mini
		// — no #6104 twin/facet is involved here (Ecto's SCOPE.Schema "users"
		// table record has a DIFFERENT Name than "Demo.Schemas.User", so it
		// never becomes a fold candidate for the module node); it is purely
		// the base AST module node losing identity to the ormlink sentinel on
		// first-writer-wins, same as java-spring-mini's base class node did.
		{"elixir-phoenix-mini", "SCOPE.Component", "Demo.Schemas.User", true, 22, 22, 9, 10},
		// #6276, NOT #6275 — untouched. No non-sentinel record ever collides
		// with ormlink's sentinel for SCOPE.Component/User/users/models.py
		// (the base class folds into bare "Model" instead; see comment
		// above), so nothing here for #6275's fix to promote. The +1/+2
		// entity/relationship bump is an unrelated side effect of the
		// twin_of anchor-id fix elsewhere in this fixture.
		{"python-django-mini", "SCOPE.Component", "User", false, 25, 28, 7, 12},
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

			// entityByID for the wantFound branch below — verifies the match
			// is a REAL entity (not merely that isPlaceholderAnchor got
			// bypassed some other way).
			entityByID := make(map[string]graph.Entity, len(doc.Entities))
			for _, e := range doc.Entities {
				entityByID[e.ID] = e
			}

			// The named expectation, not just a count.
			var seen bool
			for _, r := range rep.EntityResults {
				if r.Expected.Kind != tc.anchoredKind || r.Expected.Name != tc.anchoredName {
					continue
				}
				seen = true
				if r.Found != tc.wantFound {
					t.Errorf("must-have %s %s Found = %v, want %v (MatchedID=%q)",
						r.Expected.Kind, r.Expected.Name, r.Found, tc.wantFound, r.MatchedID)
				}
				if !tc.wantFound {
					// #6275 did not reach this fixture's case (python-django-mini,
					// #6276): only ormlink's placeholder anchor carries this
					// (Kind, Name), and isPlaceholderAnchor correctly rejects it.
					if r.MatchedID != "" {
						t.Errorf("must-have %s %s bound MatchedID=%q, want empty",
							r.Expected.Kind, r.Expected.Name, r.MatchedID)
					}
					continue
				}
				// #6275 FIXED: the match must be a real entity, never the
				// content-free ormlink sentinel it used to fall back to.
				if r.MatchedID == "" {
					t.Fatalf("must-have %s %s Found=true but MatchedID is empty", r.Expected.Kind, r.Expected.Name)
				}
				matched, ok := entityByID[r.MatchedID]
				if !ok {
					t.Fatalf("must-have %s %s MatchedID=%q not present in doc.Entities", r.Expected.Kind, r.Expected.Name, r.MatchedID)
				}
				if matched.Subtype == ormlink.SubtypeSentinel {
					t.Errorf("must-have %s %s matched ormlink's placeholder sentinel (id=%q) — #6275 should have promoted the real class record's identity over it",
						r.Expected.Kind, r.Expected.Name, r.MatchedID)
				}
				if matched.StartLine == 0 {
					t.Errorf("must-have %s %s matched an entity with StartLine 0 (id=%q) — want the real class's span",
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
				t.Errorf("relationship_found = %d, want %d", rep.RelFound, tc.wantRelFound)
			}
		})
	}
}
