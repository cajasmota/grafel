package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6104 — merge FACETS in the symbol index.
//
// The #6104 merge keeps both nodes when a custom/framework extractor models a
// source construct the base path already emitted under a different Kind (e.g.
// SCOPE.Schema|Order alongside the base SCOPE.Component|Order). That puts a
// SECOND node under an existing name. Without special handling the symbol
// index reads that as a name collision and flips the name to ambiguous, which
// un-resolves every edge that names it — the #4366 `Class:Order` and #4379
// dotted-middleware-path failures.
//
// The rule: a facet is an ALIAS of its anchor, not a competing definition.
//
//	F1. A facet never makes its anchor unresolvable.
//	F2. A facet never displaces its anchor in a symbol table.
//	F3. Order-independence: a real entity arriving AFTER a facet still takes
//	    the entry.
//	F4. NON-FACETS ARE UNTOUCHED — two genuinely different definitions sharing
//	    a name are still ambiguous. This is the regression risk: the alias rule
//	    must not become a general "first writer wins" that silently resolves
//	    real ambiguity.

func anchorEnt(kind, name, file, qname string) types.EntityRecord {
	e := types.EntityRecord{Kind: kind, Name: name, SourceFile: file, QualifiedName: qname}
	e.ID = e.ComputeID()
	return e
}

func facetEnt(kind, name, file, qname, anchorID string) types.EntityRecord {
	e := types.EntityRecord{
		Kind: kind, Name: name, SourceFile: file, QualifiedName: qname,
		Properties: map[string]string{types.EntityTwinOfProperty: anchorID},
	}
	e.ID = e.ComputeID()
	return e
}

// F1 + F2: the anchor stays resolvable by bare name and by QualifiedName, and
// the ID returned is the ANCHOR's, not the facet's.
func TestFacetDoesNotMakeAnchorUnresolvable(t *testing.T) {
	anchor := anchorEnt("SCOPE.Component", "Order", "app/models/orders.py", "app.models.orders.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "app/models/orders.py", "app.models.orders.Order", anchor.ID)

	idx := BuildIndex([]types.EntityRecord{anchor, facet})

	id, ok := idx.Lookup("Order")
	if !ok {
		t.Fatalf("bare-name lookup of the anchor failed once a facet was added (the #4366 shape)")
	}
	if id != anchor.ID {
		t.Errorf("bare name resolved to the facet %q, want the anchor %q", id, anchor.ID)
	}

	// Class:Order — kind "Class" is not a SCOPE.Component alias, so this falls
	// through to the kind-agnostic path. This is the exact stub #4366 asserts.
	if id, ok := idx.Lookup("Class:Order"); !ok || id != anchor.ID {
		t.Errorf("Class:Order did not resolve to the anchor (id=%q ok=%v)", id, ok)
	}

	// QualifiedName — the facet inherits its anchor's, which must not blank the
	// entry (the #4379 dotted-middleware-path shape).
	if id, ok := idx.Lookup("app.models.orders.Order"); !ok || id != anchor.ID {
		t.Errorf("QualifiedName lookup did not resolve to the anchor (id=%q ok=%v)", id, ok)
	}
}

// F3: order-independence. The facet is indexed FIRST here; the anchor arriving
// afterwards must still take the entry rather than colliding with it.
func TestFacetDoesNotDisplaceAnchorWhenIndexedFirst(t *testing.T) {
	anchor := anchorEnt("SCOPE.Component", "Order", "app/models/orders.py", "app.models.orders.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "app/models/orders.py", "app.models.orders.Order", anchor.ID)

	idx := BuildIndex([]types.EntityRecord{facet, anchor}) // facet first

	if id, ok := idx.Lookup("Order"); !ok || id != anchor.ID {
		t.Errorf("anchor did not take the byName entry from the facet incumbent (id=%q ok=%v)", id, ok)
	}
	if id, ok := idx.Lookup("app.models.orders.Order"); !ok || id != anchor.ID {
		t.Errorf("anchor did not take the byQualifiedName entry from the facet incumbent (id=%q ok=%v)", id, ok)
	}
}

// The facet is still addressable under its OWN Kind — keeping both nodes must
// not make one of them unreachable.
func TestFacetRemainsAddressableByItsOwnKind(t *testing.T) {
	anchor := anchorEnt("SCOPE.Component", "Order", "app/models/orders.py", "app.models.orders.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "app/models/orders.py", "app.models.orders.Order", anchor.ID)

	idx := BuildIndex([]types.EntityRecord{anchor, facet})

	if id, ok := idx.Lookup("SCOPE.Schema:Order"); !ok || id != facet.ID {
		t.Errorf("facet not addressable by its own kind (id=%q ok=%v want %q)", id, ok, facet.ID)
	}
	if id, ok := idx.Lookup("SCOPE.Component:Order"); !ok || id != anchor.ID {
		t.Errorf("anchor not addressable by its own kind (id=%q ok=%v want %q)", id, ok, anchor.ID)
	}
}

// F4 — THE REGRESSION GUARD. Two genuinely different definitions sharing a
// name are still ambiguous. The alias rule keys strictly on
// EntityTwinOfProperty; it must not leak into ordinary entities.
func TestNonFacetNameCollisionIsStillAmbiguous(t *testing.T) {
	a := anchorEnt("SCOPE.Component", "Order", "app/models/orders.py", "app.models.orders.Order")
	b := anchorEnt("SCOPE.Component", "Order", "billing/models.py", "billing.models.Order")

	idx := BuildIndex([]types.EntityRecord{a, b})

	if id, ok := idx.Lookup("Order"); ok {
		t.Errorf("two real definitions named Order must stay AMBIGUOUS, resolved to %q", id)
	}
	// Same-kind collision must still be ambiguous within the kind too.
	if id, ok := idx.Lookup("SCOPE.Component:Order"); ok {
		t.Errorf("same-kind collision between two real definitions must stay ambiguous, got %q", id)
	}
	// Distinct QualifiedNames are unaffected either way.
	if id, ok := idx.Lookup("billing.models.Order"); !ok || id != b.ID {
		t.Errorf("distinct QualifiedName broke (id=%q ok=%v)", id, ok)
	}
}

// A duplicate QualifiedName between two NON-facets must still blank the entry.
func TestNonFacetQualifiedNameCollisionIsStillBlanked(t *testing.T) {
	a := anchorEnt("SCOPE.Component", "Order", "a.py", "shared.Order")
	b := anchorEnt("SCOPE.Operation", "handle", "b.py", "shared.Order")

	idx := BuildIndex([]types.EntityRecord{a, b})
	if id, ok := idx.Lookup("shared.Order"); ok {
		t.Errorf("a duplicate QualifiedName between two real entities must stay ambiguous, got %q", id)
	}
}

// A self-referential or empty twin marker is NOT a facet — the property must
// name a DIFFERENT entity for the alias rule to apply, so a stray or corrupted
// value cannot be used to suppress a real ambiguity.
func TestSelfReferentialTwinMarkerIsNotAFacet(t *testing.T) {
	a := anchorEnt("SCOPE.Component", "Order", "a.py", "a.Order")
	b := anchorEnt("SCOPE.Component", "Order", "b.py", "b.Order")
	b.Properties = map[string]string{types.EntityTwinOfProperty: b.ID} // points at itself

	if b.IsMergeTwinAlias() {
		t.Fatalf("an entity naming ITSELF as its twin must not count as a facet")
	}
	idx := BuildIndex([]types.EntityRecord{a, b})
	if id, ok := idx.Lookup("Order"); ok {
		t.Errorf("self-referential marker suppressed a real ambiguity, resolved to %q", id)
	}
}

// The module-index path (GRAFEL_RESOLVE_MODULE_INDEX=1, #4901) must agree with
// flat BuildIndex on all of the above — a divergence between the two builders
// is exactly the #5206 class of bug.
func TestFacetAliasRuleMatchesOnTheModuleIndexPath(t *testing.T) {
	anchor := anchorEnt("SCOPE.Component", "Order", "app/models/orders.py", "app.models.orders.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "app/models/orders.py", "app.models.orders.Order", anchor.ID)
	other := anchorEnt("SCOPE.Component", "Order", "billing/models.py", "billing.models.Order")

	for _, tc := range []struct {
		name     string
		ents     []types.EntityRecord
		stub     string
		wantID   string
		wantOK   bool
		wantWhat string
	}{
		{"facet keeps anchor resolvable", []types.EntityRecord{anchor, facet}, "Order", anchor.ID, true, "anchor"},
		{"facet first", []types.EntityRecord{facet, anchor}, "Order", anchor.ID, true, "anchor"},
		{"facet keeps qname resolvable", []types.EntityRecord{anchor, facet}, "app.models.orders.Order", anchor.ID, true, "anchor"},
		{"real collision stays ambiguous", []types.EntityRecord{anchor, other}, "Order", "", false, "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flat := BuildIndex(tc.ents)
			gotFlatID, gotFlatOK := flat.Lookup(tc.stub)
			if gotFlatOK != tc.wantOK || (tc.wantOK && gotFlatID != tc.wantID) {
				t.Errorf("flat BuildIndex: got (%q,%v) want %s", gotFlatID, gotFlatOK, tc.wantWhat)
			}

			mod := BuildIndexFromModulesOrdered(tc.ents, ModuleKeyByPkgDir)
			gotModID, gotModOK := mod.Lookup(tc.stub)
			if gotModOK != gotFlatOK || gotModID != gotFlatID {
				t.Errorf("module index DIVERGES from flat: module=(%q,%v) flat=(%q,%v)",
					gotModID, gotModOK, gotFlatID, gotFlatOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ORPHANED FACETS (#6104 review finding M2)
// ---------------------------------------------------------------------------
//
// The alias rule is scoped to the facet/ANCHOR PAIR. An earlier revision
// short-circuited on "is a facet" alone, which excluded facets from ambiguity
// detection GLOBALLY. That is reachable in production: deduplicateEntities
// runs AFTER the merge and can remove the anchor, orphaning the facet. With
// the global short-circuit, an orphaned facet standing next to a genuinely
// unrelated same-named definition let that unrelated definition SILENTLY own
// the bare name — trading an honestly-unresolved edge for a possibly-wrong
// one, deterministically, in both orderings.
//
// These pin the pair-scoping in BOTH index builders.

// orphanedFacetEnts builds the ORD-6 shape: a facet whose anchor is ABSENT
// from the batch, plus an unrelated genuine definition of the same name in a
// different file.
func orphanedFacetEnts() (facet, unrelated types.EntityRecord) {
	missingAnchor := anchorEnt("SCOPE.Component", "Order", "a/m.py", "a.m.Order")
	facet = facetEnt("SCOPE.Schema", "Order", "a/m.py", "a.m.Order", missingAnchor.ID)
	unrelated = anchorEnt("SCOPE.Component", "Order", "totally/other.py", "totally.other.Order")
	return facet, unrelated
}

func TestOrphanedFacetDoesNotHandTheNameToAnUnrelatedDefinition(t *testing.T) {
	facet, unrelated := orphanedFacetEnts()

	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"facet first", []types.EntityRecord{facet, unrelated}},
		{"unrelated first", []types.EntityRecord{unrelated, facet}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := BuildIndex(tc.ents)
			if id, ok := idx.Lookup("Order"); ok {
				var who string
				switch id {
				case unrelated.ID:
					who = "the UNRELATED definition in totally/other.py"
				case facet.ID:
					who = "the orphaned facet"
				default:
					who = "an unexpected entity"
				}
				t.Errorf("an orphaned facet must not suppress a real ambiguity: "+
					"bare name resolved to %s (%q); it must stay ambiguous", who, id)
			}
		})
	}
}

// Same shape on the module-index path (#4901), which must not diverge (#5206).
func TestOrphanedFacetOnTheModuleIndexPath(t *testing.T) {
	facet, unrelated := orphanedFacetEnts()

	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"facet first", []types.EntityRecord{facet, unrelated}},
		{"unrelated first", []types.EntityRecord{unrelated, facet}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flat := BuildIndex(tc.ents)
			mod := BuildIndexFromModulesOrdered(tc.ents, ModuleKeyByPkgDir)

			flatID, flatOK := flat.Lookup("Order")
			modID, modOK := mod.Lookup("Order")
			if modOK != flatOK || modID != flatID {
				t.Errorf("module index DIVERGES from flat on the orphaned-facet shape: "+
					"module=(%q,%v) flat=(%q,%v)", modID, modOK, flatID, flatOK)
			}
			if modOK {
				t.Errorf("module index: orphaned facet suppressed a real ambiguity, resolved to %q", modID)
			}
		})
	}
}

// A facet must not suppress ambiguity against a THIRD entity even when its own
// anchor IS present — the rule protects the pair, not the name.
func TestFacetDoesNotSuppressAmbiguityAgainstAThirdDefinition(t *testing.T) {
	anchor := anchorEnt("SCOPE.Component", "Order", "a/m.py", "a.m.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "a/m.py", "a.m.Order", anchor.ID)
	third := anchorEnt("SCOPE.Component", "Order", "totally/other.py", "totally.other.Order")

	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"anchor facet third", []types.EntityRecord{anchor, facet, third}},
		{"third facet anchor", []types.EntityRecord{third, facet, anchor}},
		{"facet third anchor", []types.EntityRecord{facet, third, anchor}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if id, ok := BuildIndex(tc.ents).Lookup("Order"); ok {
				t.Errorf("a third real definition must still make the name ambiguous, got %q", id)
			}
		})
	}
}

// The QualifiedName tier needs the same pair-scoping: an orphaned facet must
// not let an unrelated entity own a QualifiedName it shares.
func TestOrphanedFacetDoesNotHandOverAQualifiedName(t *testing.T) {
	missingAnchor := anchorEnt("SCOPE.Component", "Order", "a/m.py", "shared.Order")
	facet := facetEnt("SCOPE.Schema", "Order", "a/m.py", "shared.Order", missingAnchor.ID)
	unrelated := anchorEnt("SCOPE.Operation", "handle", "totally/other.py", "shared.Order")

	for _, tc := range []struct {
		name string
		ents []types.EntityRecord
	}{
		{"facet first", []types.EntityRecord{facet, unrelated}},
		{"unrelated first", []types.EntityRecord{unrelated, facet}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if id, ok := BuildIndex(tc.ents).Lookup("shared.Order"); ok {
				t.Errorf("orphaned facet let %q own a contested QualifiedName", id)
			}
		})
	}
}
