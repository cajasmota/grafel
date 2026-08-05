package sresolver_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/graph"
)

// Issue #6141 in the SCOPED (incremental) resolver.
//
// memberIndexes.leafByFile / leafByPkg are the scoped path's ports of
// internal/resolve/refs.go's lookupMemberByLeafName /
// lookupPackageMemberByLeafName, and they inherited the same defect: the
// index is built over every dotted-name entity regardless of Kind, so a
// bare CALLS stub binds to a FIELD, and a legitimate operation binding is
// LOST when an unrelated same-leaf-named field trips the ambiguity guard.
//
// Only the RECALL half is fixed, matching internal/resolve: a hard kind
// filter was implemented, then measured to destroy real Ruby and JS/TS
// bindings and reduced to an operation PREFERENCE. The precision half is
// pinned as a known gap below rather than left untested.
//
// These are separate tests rather than a note on the refs.go fix because
// the two resolvers are separate code with separate indexes: fixing only
// refs.go would make an incremental build disagree with a full rebuild on
// exactly these shapes, which is the divergence class pkgmember.go's own
// header warns about.

const (
	lfCaller = "1111111111111111" // svc/a.go — the calling method
	lfOp     = "2222222222222222" // svc/b.go — Owned.owner, an OPERATION
	lfField  = "3333333333333333" // svc/c.go — Other.owner, a FIELD
)

// lfEnt builds an entity with an explicit Kind (mtEnt hardcodes
// SCOPE.Operation, which is exactly the distinction under test here).
func lfEnt(id, name, sourceFile, kind string) graph.Entity {
	return graph.Entity{
		ID: id, Name: name, Kind: kind,
		SourceFile: sourceFile, Language: "go",
	}
}

// TestResolveScoped_LeafTier_PrecisionGap_StillBindsToField_6141 is the
// scoped mirror of internal/resolve's
// TestLeafNameTier_PrecisionGap_CallStillBindsToField_6141 — a
// CHARACTERISATION test on behaviour that is still wrong for
// Solidity/Java/Go and deliberately left that way.
//
// The only `owner` in the caller's package is a field, so the
// operation-preferring pass finds nothing and the kind-blind pass binds the
// call to it. Refusing instead was measured to destroy real Ruby
// attr_accessor and JS function-field bindings; see the note on
// memberIndexes.leafByFileOp.
//
// The two resolvers MUST agree here. If you change one, change the other,
// or an incremental build and a full rebuild will disagree on the same
// source — the divergence class this file's header warns about.
func TestResolveScoped_LeafTier_PrecisionGap_StillBindsToField_6141(t *testing.T) {
	existing := []graph.Entity{lfEnt(lfField, "Other.owner", "svc/c.go", "SCOPE.Schema")}
	fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "owner", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != lfField {
		t.Errorf("known precision gap changed shape: bare CALLS `owner` gave ToID=%q, want the "+
			"field %s. If a refusal was intended, make internal/resolve refuse too and check the "+
			"Ruby attr_accessor end-to-end test", got.ToID, lfField)
	}
}

// TestResolveScoped_LeafTier_FieldMustNotShadowOperation_6141 — recall. The
// package holds one genuine operation `Owned.owner` and one unrelated field
// `Other.owner`. Today the field enters the candidate set, two distinct
// scopes claim the leaf name, and the CORRECT edge is dropped.
func TestResolveScoped_LeafTier_FieldMustNotShadowOperation_6141(t *testing.T) {
	existing := []graph.Entity{
		lfEnt(lfOp, "Owned.owner", "svc/b.go", "SCOPE.Operation"),
		lfEnt(lfField, "Other.owner", "svc/c.go", "SCOPE.Schema"),
	}
	fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "owner", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != lfOp {
		t.Errorf("bare CALLS `owner` should bind to the unique OPERATION Owned.owner (%s) — "+
			"the sibling FIELD Other.owner must not enter the candidate set and trip the "+
			"ambiguity guard; got ToID=%q", lfOp, got.ToID)
	}
}

// TestResolveScoped_LeafTier_ReceiverTierStillSeesFields_6141 is the scope
// guard: only the LEAF tiers are kind-filtered. Tier 1 (receiver-stamped
// byPackageMember) names its scope explicitly, so it is precise by
// construction and is deliberately left kind-blind — filtering it would be
// a separate, unassessed change. This test fails if the filter is applied
// to the receiver tier's index by mistake.
func TestResolveScoped_LeafTier_ReceiverTierStillSeesFields_6141(t *testing.T) {
	existing := []graph.Entity{lfEnt(lfField, "Other.owner", "svc/c.go", "SCOPE.Schema")}
	fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "owner", "Other")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != lfField {
		t.Errorf("the receiver-stamped tier names its scope (receiver_type=Other) and must stay "+
			"kind-blind; want ToID=%s, got %q", lfField, got.ToID)
	}
}

// TestResolveScoped_LeafTier_OperationKindsStillBind_6141 pins the whole
// operation kind family through the scoped leaf tiers, so the filter cannot
// silently drop a language whose callables carry a different operation kind.
func TestResolveScoped_LeafTier_OperationKindsStillBind_6141(t *testing.T) {
	for _, kind := range []string{"Operation", "Function", "Method", "SCOPE.Operation"} {
		t.Run(kind, func(t *testing.T) {
			existing := []graph.Entity{lfEnt(lfOp, "T11.Do11", "svc/b.go", kind)}
			fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
			newRels := []graph.Relationship{mtCallRel(lfCaller, "Do11", "")}

			res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
			got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
			if got.ToID != lfOp {
				t.Errorf("callable of kind %q no longer binds through the scoped leaf tier; "+
					"want ToID=%s, got %q", kind, lfOp, got.ToID)
			}
		})
	}
}

// TestResolveScoped_LeafTier_TwoOperationsStillAmbiguous_6141 is the
// false-negative control on the guard: the kind filter must NARROW the
// candidate set, never DISABLE the ambiguity refusal.
func TestResolveScoped_LeafTier_TwoOperationsStillAmbiguous_6141(t *testing.T) {
	existing := []graph.Entity{
		lfEnt(lfOp, "T11.Do11", "svc/b.go", "SCOPE.Operation"),
		lfEnt(lfField, "T23.Do11", "svc/c.go", "SCOPE.Operation"),
	}
	fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != "Do11" {
		t.Errorf("two same-named OPERATIONS in package svc must stay unresolved; "+
			"got a confident binding ToID=%q", got.ToID)
	}
}

// TestResolveScoped_LeafTier_PreferenceIsWithinTierNotAcrossTiers_6141 is the
// tier-ORDER guard, and it is the shape every other fixture in this file
// misses: they all put the competing member in a DIFFERENT file from the
// caller, so file-scope and package-scope never actually contend.
//
//	caller  Local07      svc/a.go
//	field   Other.owner  svc/a.go   SCOPE.Schema  <- caller's OWN file
//	op      Owned.owner  svc/b.go   SCOPE.Operation
//
// internal/resolve applies the operation preference INSIDE each tier —
// (fileOp, fileAny) then (pkgOp, pkgAny) — so the caller's own file wins and
// it binds the FIELD. An implementation that applies the preference ACROSS
// tiers — fileOp, pkgOp, fileAny, pkgAny — reaches into the sibling file and
// binds the OPERATION instead.
//
// Both are defensible in isolation; what is not defensible is the two
// resolvers choosing differently, because then a full rebuild and an
// incremental build disagree on the same source. That is reachable in
// ordinary code: a Ruby `attr_accessor :owner` beside a sibling class's
// `def owner`. Locality wins, matching refs.go.
func TestResolveScoped_LeafTier_PreferenceIsWithinTierNotAcrossTiers_6141(t *testing.T) {
	existing := []graph.Entity{lfEnt(lfOp, "Owned.owner", "svc/b.go", "SCOPE.Operation")}
	fresh := []graph.Entity{
		lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation"),
		lfEnt(lfField, "Other.owner", "svc/a.go", "SCOPE.Schema"),
	}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "owner", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != lfField {
		t.Errorf("the operation preference must apply WITHIN a tier, not ACROSS tiers: the "+
			"caller's own file declares `owner` (%s) and must win over the sibling-file "+
			"operation (%s); got ToID=%q. internal/resolve binds the caller-file member here, "+
			"so this ordering is what keeps a full rebuild and an incremental build agreeing",
			lfField, lfOp, got.ToID)
	}
}
