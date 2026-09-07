// Package resolve — #6976: a record minted from a MENTION of a name evicted
// the sole real DECLARATION of that name from the repository-wide byName
// index, and took every bare-name edge to it down with it.
//
// THE SHAPE, from `aspnetcore-mvc` with the custom-extractor gate ON. The
// .NET DI extractor reads a constructor parameter `ActionContext context` in
// HomeController.cs and mints a SCOPE.Class named `ActionContext` in THAT
// file. The class `ActionContext` is declared in ActionContext.cs and
// extracted there. Two entities, one name, different files — so indexByName
// took its default arm: `delete(byName, "ActionContext")` + ambiguous, for the
// whole repository. Every bare-name TESTS edge naming `ActionContext` then
// dangled, in files that had never heard of HomeController.
//
// WHY THE ASSERTIONS ARE ON EDGES AND NOT ON COUNTS. This defect survived a
// measurement that reported explicit TESTS at +8 net while 7,221 edges were
// SUBSTITUTED (#6973): a cardinality assertion cannot see a substitution. So
// every case below names the id the edge must land on.
package resolve_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	declFile6976 = "src/Mvc/ActionContext.cs"
	ctrlFile6976 = "src/Mvc/Controllers/HomeController.cs"
	apiFile6976  = "src/Mvc/Api/ApiController.cs"
	testFile6976 = "test/Mvc.Test/ActionContextTest.cs"

	// Stand-ins for the production sha256 ids — distinct by construction so
	// no case can pass by ID collision.
	declID6976     = "d000000000000001" // the real `class ActionContext`
	ctrlRefID6976  = "d000000000000002" // DI provider mention in HomeController.cs
	apiRefID6976   = "d000000000000003" // return-type mention in ApiController.cs
	otherDecl6976  = "d000000000000004" // a SECOND real declaration of the name
	placeholder976 = "d000000000000005" // a per-import placeholder for the name
	localBind6976  = "d000000000000006" // a function-body local named the same
	testerID6976   = "d000000000000009" // the test entity carrying the edge

	sharedName6976 = "ActionContext"
)

// declRecord6976 is the real `public class ActionContext` — unmarked, the
// thing every consumer of the name wants.
func declRecord6976(id, file string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "SCOPE.Class", Name: sharedName6976,
		SourceFile: file, Language: "csharp", StartLine: 12, EndLine: 40,
	}
}

// refRecord6976 is a reference-shaped record: the entity a custom extractor
// mints when it reads a MENTION of the name. `reference_shaped` is stamped at
// the emit site (internal/custom/csharp) — see resolve.ReferenceShapedProp.
func refRecord6976(id, file, provenance string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "SCOPE.Class", Name: sharedName6976,
		SourceFile: file, Language: "csharp", StartLine: 7, EndLine: 7,
		Properties: map[string]string{
			"framework":                  "dotnet_di",
			"provenance":                 provenance,
			resolve.ReferenceShapedProp:  "true",
			"grafel_fixture_marker_only": "6976",
		},
	}
}

// testerRecord6976 carries the bare-name TESTS edge the mechanism destroyed.
func testerRecord6976() types.EntityRecord {
	return types.EntityRecord{
		ID: testerID6976, Kind: "SCOPE.Pattern", Name: "ActionContextTest",
		Subtype: "unit", SourceFile: testFile6976, Language: "csharp",
		Relationships: []types.RelationshipRecord{
			{FromID: testerID6976, ToID: sharedName6976, Kind: "TESTS"},
		},
	}
}

// resolveTESTS6976 runs the production index + rewrite over recs and returns
// the ToID the single TESTS edge ended up on. It fails the test if the fixture
// stops carrying exactly one such edge, so no case can pass vacuously.
func resolveTESTS6976(t *testing.T, recs []types.EntityRecord) string {
	t.Helper()
	cp := append([]types.EntityRecord(nil), recs...)
	for i := range cp {
		cp[i].Relationships = append([]types.RelationshipRecord(nil), cp[i].Relationships...)
	}
	idx := resolve.BuildIndex(cp)
	resolve.ReferencesEmbedded(cp, idx)

	got, seen := "", 0
	for i := range cp {
		for _, r := range cp[i].Relationships {
			if r.Kind == "TESTS" {
				seen++
				got = r.ToID
			}
		}
	}
	if seen != 1 {
		t.Fatalf("fixture is vacuous: found %d TESTS edge(s), want exactly 1", seen)
	}
	return got
}

// bothOrders6976 runs one case in both extraction orders. Extraction order is
// not stable across runs, so a tier that only works in one direction is a bug
// that reports itself as a flake.
func bothOrders6976(t *testing.T, a, b types.EntityRecord, check func(t *testing.T, got string)) {
	t.Helper()
	tester := testerRecord6976()
	for _, o := range []struct {
		tag  string
		recs []types.EntityRecord
	}{
		{"a-then-b", []types.EntityRecord{a, b, tester}},
		{"b-then-a", []types.EntityRecord{b, a, tester}},
	} {
		t.Run(o.tag, func(t *testing.T) {
			check(t, resolveTESTS6976(t, o.recs))
		})
	}
}

// TestDeclarationOutranksReference_6976 is the reported defect. The bare-name
// TESTS edge must land on the DECLARATION's id, in either extraction order.
func TestDeclarationOutranksReference_6976(t *testing.T) {
	bothOrders6976(t,
		declRecord6976(declID6976, declFile6976),
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		func(t *testing.T, got string) {
			if got != declID6976 {
				t.Errorf("TESTS -> %q, want the declaration %q (%s): a record minted from a "+
					"constructor-parameter MENTION of %q evicted the sole declaration of that "+
					"name from the repository-wide index, leaving the edge unresolvable (#6976)",
					got, declID6976, declFile6976, sharedName6976)
			}
		})
}

// TestTwoDeclarationsStillCollide_6976 is the OTHER direction, and it is the
// one that matters more: the tier must not start resolving names that are
// GENUINELY ambiguous between two real declarations. A confidently-wrong bind
// reads as valid and is worse than a dangling reference (#6369).
func TestTwoDeclarationsStillCollide_6976(t *testing.T) {
	bothOrders6976(t,
		declRecord6976(declID6976, declFile6976),
		declRecord6976(otherDecl6976, apiFile6976),
		func(t *testing.T, got string) {
			if got != sharedName6976 {
				t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: two real "+
					"declarations of one name are genuinely ambiguous, and the #6976 tier "+
					"must not invent a winner for them (#6369 wrong-node hazard)",
					got, sharedName6976)
			}
		})
}

// TestTwoReferencesStillCollide_6976 — two mentions and no declaration is not
// a case where anything real is lost, and it must behave exactly as it did
// before this tier: ambiguous, so lookupBareWithLocality and then
// external.Synthesize still get their turn. Ambiguity is load-bearing (#6427),
// and this tier does not trade it away.
func TestTwoReferencesStillCollide_6976(t *testing.T) {
	bothOrders6976(t,
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		refRecord6976(apiRefID6976, apiFile6976, "INFERRED_FROM_ASPNET_RETURN_TYPE"),
		func(t *testing.T, got string) {
			if got != sharedName6976 {
				t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: with no "+
					"declaration of the name in the repository, two references must still "+
					"collide into ambiguity rather than have one of them win arbitrarily",
					got, sharedName6976)
			}
		})
}

// TestDeclarationReclaimsFromReferenceOnlyAmbiguity_6976 — once two references
// have raised ambiguity, a declaration arriving LATER must still take the
// slot. Without nameAmbigRef the name would stay dead for the rest of the
// index, and which arm you land in would depend on file-walk order.
func TestDeclarationReclaimsFromReferenceOnlyAmbiguity_6976(t *testing.T) {
	recs := []types.EntityRecord{
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		refRecord6976(apiRefID6976, apiFile6976, "INFERRED_FROM_ASPNET_RETURN_TYPE"),
		declRecord6976(declID6976, declFile6976),
		testerRecord6976(),
	}
	if got := resolveTESTS6976(t, recs); got != declID6976 {
		t.Errorf("TESTS -> %q, want the declaration %q: a declaration extracted AFTER two "+
			"references must still reclaim the name — otherwise the tier's outcome depends "+
			"on file-walk order (#6369's nameAmbigImport, same reason)", got, declID6976)
	}
}

// TestLoneReferenceStillOccupiesTheSlot_6976 — withholding references from
// byName entirely is NOT what this tier does, and the difference is
// observable. A mention whose declaration is not in the indexed tree (a
// framework type, a partial checkout) is the only claimant on its name, and
// the edges that name it must still bind to it. Same clause #6369 keeps for a
// lone import placeholder.
func TestLoneReferenceStillOccupiesTheSlot_6976(t *testing.T) {
	recs := []types.EntityRecord{
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		testerRecord6976(),
	}
	if got := resolveTESTS6976(t, recs); got != ctrlRefID6976 {
		t.Errorf("TESTS -> %q, want the lone reference %q: the tier is a PRECEDENCE between "+
			"competing records, not a blanket skip — an unclaimed name may still be occupied "+
			"by a reference", got, ctrlRefID6976)
	}
}

// TestReferenceDoesNotOutrankImportPlaceholder_6976 and its local-binding twin
// pin the two pairings #6369 and #6467 already decided, in the direction this
// tier could have broken them. A placeholder and a mention are both
// non-declarations: neither may hand the other the repository-wide slot, so
// the pairing must fall through to ambiguity and let the external-library
// binder run.
func TestReferenceDoesNotOutrankImportPlaceholder_6976(t *testing.T) {
	placeholder := types.EntityRecord{
		ID: placeholder976, Kind: "SCOPE.Component", Name: sharedName6976,
		Subtype: "import", SourceFile: apiFile6976, Language: "csharp",
		Properties: map[string]string{"provenance": "INFERRED_FROM_IMPORT_STATEMENT"},
	}
	bothOrders6976(t,
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		placeholder,
		func(t *testing.T, got string) {
			if got != sharedName6976 {
				t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: an import "+
					"placeholder and a mention are both non-declarations, so neither may "+
					"win the repository-wide slot from the other (#6369 + #6976)",
					got, sharedName6976)
			}
		})
}

func TestReferenceDoesNotOutrankLocalBinding_6976(t *testing.T) {
	local := types.EntityRecord{
		ID: localBind6976, Kind: "SCOPE.Component", Name: sharedName6976,
		Subtype: "const", SourceFile: apiFile6976, Language: "csharp",
		Properties: map[string]string{"local_scope": "true"},
	}
	bothOrders6976(t,
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		local,
		func(t *testing.T, got string) {
			if got != sharedName6976 {
				t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: a function-body "+
					"local and a mention are both non-declarations of the repository-wide "+
					"name (#6467 + #6976)", got, sharedName6976)
			}
		})
}

// TestUnmarkedRecordIsUnchanged_6976 pins the SCOPE of the marker. This tier
// is opt-in per producer: an entity that carries no `reference_shaped` stamp
// competes exactly as it did before #6976 — which for two unmarked records of
// one name means ambiguity. It is the control for every case above: if the
// tier had been keyed on something derivable from the record (span, provenance
// presence, confidence — all of which were measured and rejected), this case
// would have changed too.
func TestUnmarkedRecordIsUnchanged_6976(t *testing.T) {
	unmarked := refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER")
	delete(unmarked.Properties, resolve.ReferenceShapedProp)
	bothOrders6976(t,
		declRecord6976(declID6976, declFile6976),
		unmarked,
		func(t *testing.T, got string) {
			if got != sharedName6976 {
				t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: without the "+
					"producer-side stamp the record must behave exactly as it did before "+
					"#6976 — the tier reads the marker and nothing else", got, sharedName6976)
			}
		})
}

// TestReferenceShapedPropSpelling_6976 pins the wire spelling. The producers
// in internal/custom/csharp stamp this key as a literal string (the same
// arrangement `local_scope` uses between the JS extractor and this package),
// and a graph written by an older indexer carries whatever was current then.
// Renaming the key silently un-marks every producer, which this package cannot
// detect on its own — the corpus would just quietly regress.
func TestReferenceShapedPropSpelling_6976(t *testing.T) {
	if resolve.ReferenceShapedProp != "reference_shaped" {
		t.Fatalf("ReferenceShapedProp = %q, want %q: the six producers in "+
			"internal/custom/csharp stamp the literal string, so changing this constant "+
			"un-marks all of them", resolve.ReferenceShapedProp, "reference_shaped")
	}
}

// TestReferenceDoesNotReclaimPlaceholderOnlyAmbiguity_6976 grades the
// ambigName early-return arm, which the pairing tests above never reach. Two
// placeholders raise placeholder-only ambiguity; #6369 lets a real declaration
// arriving later reclaim the name. A mention is not a real declaration, so it
// must NOT — otherwise the very case #6427 kept ambiguous for the
// external-library binder gets a confident wrong bind instead.
func TestReferenceDoesNotReclaimPlaceholderOnlyAmbiguity_6976(t *testing.T) {
	ph := func(id, file string) types.EntityRecord {
		return types.EntityRecord{
			ID: id, Kind: "SCOPE.Component", Name: sharedName6976,
			Subtype: "import", SourceFile: file, Language: "csharp",
			Properties: map[string]string{"provenance": "INFERRED_FROM_IMPORT_STATEMENT"},
		}
	}
	recs := []types.EntityRecord{
		ph(placeholder976, apiFile6976),
		ph("d00000000000000a", ctrlFile6976),
		refRecord6976(ctrlRefID6976, declFile6976, "INFERRED_FROM_ASPNET_RETURN_TYPE"),
		testerRecord6976(),
	}
	if got := resolveTESTS6976(t, recs); got != sharedName6976 {
		t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: placeholder-only "+
			"ambiguity yields to a DECLARATION arriving later (#6369), and a mention is "+
			"not one — reclaiming here would hand the name to a record that declares "+
			"nothing", got, sharedName6976)
	}
}

// TestSameIDReindexDoesNotReRaiseReferenceStatus_6976 grades the AND-ing in
// the insert tail. EntityID is sha256(repo, kind, name, sourceFile) and hashes
// neither Subtype nor Properties, so a declaration and a mention of the same
// name in the SAME file share one id by construction and arrive as re-indexes
// of one entity. If a trailing marked record could flip the flag back on, the
// flag would describe the last record that mentioned the name rather than the
// entity sitting in byName — and the next file's real declaration would take
// the "declaration reclaims from a reference" arm instead of colliding, so a
// genuinely ambiguous name would silently resolve to whichever file was walked
// last. That is #6369's follow-up bug, one tier over.
func TestSameIDReindexDoesNotReRaiseReferenceStatus_6976(t *testing.T) {
	sameID := "d00000000000000b"
	decl := declRecord6976(sameID, declFile6976)
	mention := refRecord6976(sameID, declFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER")
	competitor := declRecord6976(otherDecl6976, apiFile6976)

	recs := []types.EntityRecord{decl, mention, competitor, testerRecord6976()}
	if got := resolveTESTS6976(t, recs); got != sharedName6976 {
		t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: %s carries BOTH a "+
			"declaration record and a mention record on one id, so the id is a "+
			"declaration; the second real declaration in %s makes the name genuinely "+
			"ambiguous and must not be resolved to either",
			got, sharedName6976, declFile6976, apiFile6976)
	}
}

// TestSameFilePairingIsUnchanged_6976 scopes the tier to CROSS-FILE pairings,
// which is the only shape the defect has: the reference is minted in the file
// that MENTIONS the name and the declaration lives in its own file. #6976's
// own body says why the same-file case is different — "#6104's facet rule
// rescues same-name pairs that are two views of one thing in one file. It does
// not apply here because the claimants live in different files, which is
// precisely the case that goes to the destructive default arm."
//
// THIS IS NOT A HYPOTHETICAL. The golden fixture csharp-aspnet-core-mini has
// `IUserService` with NO declaration anywhere in the tree and two claimants in
// ONE file, Controllers/UsersController.cs: the .NET DI pass's SCOPE.Class
// (reference-shaped) and the YAML rule-pack's `Dependency` (unmarked, and also
// a mention — internal/engine/detector.go is not one of the six producers and
// is deliberately out of scope). Before this scoping the pair stopped
// colliding and the unmarked record took the slot outright, which moved
// `UserService -IMPLEMENTS-> IUserService` off the SCOPE.Class node and
// regressed the fixture's must-have relationship_found 13 -> 12. Ambiguity was
// carrying that edge to a better answer through the kind-hint path, which is
// exactly why this tier narrows WHICH records compete rather than softening
// the default arm.
func TestSameFilePairingIsUnchanged_6976(t *testing.T) {
	// Both claimants in ONE file, as the fixture has them. Distinct kinds, so
	// distinct ids — a real collision, not a re-index of one entity.
	marked := refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER")
	unmarked := types.EntityRecord{
		ID: apiRefID6976, Kind: "Dependency", Name: sharedName6976,
		SourceFile: ctrlFile6976, Language: "csharp",
		Properties: map[string]string{"framework": "csharp", "pattern_type": "yaml_driven"},
	}
	bothOrders6976(t, marked, unmarked, func(t *testing.T, got string) {
		if got != sharedName6976 {
			t.Errorf("TESTS -> %q, want the edge LEFT UNRESOLVED as %q: two claimants in one "+
				"file are the #6104 shape, not the #6976 one, and must keep colliding — "+
				"resolving them hands the slot to whichever record happens to be unmarked "+
				"(golden fixture csharp-aspnet-core-mini, must-have IMPLEMENTS edge)",
				got, sharedName6976)
		}
	})
}

// TestCrossFileTierStillFiresWhenSameFileIsScopedOut_6976 is the control for
// the test above: the narrowing must not cost the fix. Same two records, the
// reference moved to its own file — the reported shape — and the declaration
// must win.
func TestCrossFileTierStillFiresWhenSameFileIsScopedOut_6976(t *testing.T) {
	bothOrders6976(t,
		declRecord6976(declID6976, declFile6976),
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		func(t *testing.T, got string) {
			if got != declID6976 {
				t.Errorf("TESTS -> %q, want the declaration %q: scoping the tier to "+
					"cross-file pairings must not disarm it for the shape it exists to fix",
					got, declID6976)
			}
		})
}

// TestHolderFileFollowsTheSlot_6976 grades the bookkeeping the cross-file test
// rests on. nameHolderFile must describe the entity CURRENTLY in byName, not
// the last record that touched the name. When a declaration reclaims the slot
// from a cross-file reference, the recorded file has to move to the
// declaration's file — otherwise a SECOND reference arriving from the FIRST
// reference's file reads as same-file, the tier steps aside, and the pair
// falls to the default arm that destroys the declaration all over again.
//
// Three records, in the order the walk produces them: a reference in
// HomeController.cs, the declaration in ActionContext.cs, then a second
// reference (different kind, so a different id) back in HomeController.cs.
func TestHolderFileFollowsTheSlot_6976(t *testing.T) {
	secondRef := refRecord6976("d00000000000000c", ctrlFile6976, "INFERRED_FROM_ASPNET_RETURN_TYPE")
	secondRef.Kind = "SCOPE.Schema" // distinct kind => distinct id => a real second claimant

	recs := []types.EntityRecord{
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		declRecord6976(declID6976, declFile6976),
		secondRef,
		testerRecord6976(),
	}
	if got := resolveTESTS6976(t, recs); got != declID6976 {
		t.Errorf("TESTS -> %q, want the declaration %q: after the declaration reclaimed the "+
			"slot, the recorded holder file must be %s. Leaving it at %s makes the second "+
			"reference look same-file, the tier stands down, and the declaration is evicted "+
			"again — the defect, reintroduced two records later",
			got, declID6976, declFile6976, ctrlFile6976)
	}
}

// TestHolderFileFollowsTheSlotAfterPlaceholderDisplacement_6976 is the same
// bookkeeping claim on the OTHER arm that hands the slot over: #6369's "a real
// declaration displaces the placeholder that happened to be extracted first".
// The recorded file has to move there too, for the same reason.
func TestHolderFileFollowsTheSlotAfterPlaceholderDisplacement_6976(t *testing.T) {
	placeholder := types.EntityRecord{
		ID: placeholder976, Kind: "SCOPE.Component", Name: sharedName6976,
		Subtype: "import", SourceFile: ctrlFile6976, Language: "csharp",
		Properties: map[string]string{"provenance": "INFERRED_FROM_IMPORT_STATEMENT"},
	}
	recs := []types.EntityRecord{
		placeholder,
		declRecord6976(declID6976, declFile6976),
		refRecord6976(ctrlRefID6976, ctrlFile6976, "INFERRED_FROM_DOTNET_DI_PROVIDER"),
		testerRecord6976(),
	}
	if got := resolveTESTS6976(t, recs); got != declID6976 {
		t.Errorf("TESTS -> %q, want the declaration %q: the declaration displaced a "+
			"placeholder from %s, so the holder file must become %s. Left at %s, the "+
			"reference that follows reads as same-file, the tier stands down, and the "+
			"declaration is evicted", got, declID6976, ctrlFile6976, declFile6976, ctrlFile6976)
	}
}
