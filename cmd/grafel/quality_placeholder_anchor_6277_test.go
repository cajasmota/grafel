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
// java-spring-mini and elixir-phoenix-mini were FULLY FIXED by #6275: the
// must-have now binds to a real, content-ful entity (not the sentinel), and
// entity_found/relationship_found are back at their historic highs.
//
// python-django-mini's OWN "SCOPE.Component User" must-have was NOT fixed by
// #6275 — it was a separate defect (#6276): the base class node folded into
// Pass 2.5's bare "Model"-kind record (see internal/engine/classfold.go's
// FrameworkClassKindPriority, which ranks bare "Model" as a legitimate
// framework-typed survivor, not a #6104 twin), so no non-sentinel record ever
// collided with the ormlink sentinel for (SCOPE.Component, User,
// users/models.py) and there was nothing for #6275's dedup fix to promote.
// #6276 fixed that at the FOLD, not at the dedup: a record named as some other
// record's #6104 twin anchor is no longer an eligible fold SOURCE, so the base
// class node survives and it is that node — not the sentinel — that the dedup
// then keeps. All three cases now assert the same shape.
//
// WHY ABSOLUTE NUMBERS AND NAMED ENTITIES, and not "the ratchet is green":
// scripts/quality/ratchet.py grades against
// internal/quality/golden/baseline.json and its own `update` subcommand
// rewrites that baseline from the current run, so a test asserting only that
// `ratchet check` exits 0 survives a revert of isPlaceholderAnchor — the floor
// would be re-recorded one higher and the gate would still pass. The same
// reasoning is written out on TestJobFixturesAbsoluteRecall_6260, which was
// added after that failure mode was demonstrated on #6260.
//
// WHY THE *EXPECTED* DENOMINATORS ARE PINNED TOO, not just the found numerators:
// a must-have added to or removed from an expected.json changes what the whole
// suite grades, and every ratio-shaped gate in the repo moves with it silently.
// wantEntityExpected / wantRelExpected exist so a fixture cannot GROW (or
// shrink) without one named place going red. When it does, that is the tripwire
// working: read the expected.json diff, satisfy yourself the new must-have is
// one the pipeline should land, and only then bump the constant — never bump it
// to make the build green.
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
		// #6471 moved entity 25/28 -> 26/29 and relationship 7/12 -> 8/13.
		// Deliberate expected.json edit, not drift: the FBV health_check
		// IMPLEMENTS row named to_kind "http_endpoint", the LEGACY kind
		// http_endpoint_resolve.go rewrites in place BEFORE the resolve pass,
		// so diff.go's exact `kind\x00name` key could never match and the row
		// sat green only because it was nice_to_have. It is now must_exist
		// against http_endpoint_definition, and the endpoint it targets was
		// declared as a must-have entity (+1 to both entity figures).
		//
		// #6276 FIXED, by the mirror of #6275's fold rule: a record another
		// record names as its #6104 merge-facet ANCHOR (grafel.twin_of) is no
		// longer an eligible fold SOURCE either. The base SCOPE.Component User
		// used to fold into Pass 2.5's bare "Model" record even though the
		// Django custom pass's SCOPE.Schema/model facet was anchored on it,
		// which left ormlink's sentinel as the only record wearing
		// (SCOPE.Component, User, users/models.py) — the state the false
		// below used to record. It now survives, so this row asserts the same
		// thing the other two do: the must-have binds to a real, content-ful
		// entity with a real span, never the sentinel. Both figures move,
		// entity 26 -> 29 (100% must-have entity recall) and relationship
		// 8 -> 12; the single remaining relationship miss is the
		// `main --CALLS--> execute_from_command_line` bare-name FIXTURE ROW
		// diff.go's #6476 diagnostic already explains.
		{"python-django-mini", "SCOPE.Component", "User", true, 29, 29, 12, 13},
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
