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

// TestResolveScoped_LeafTier_MustNotBindCallToField_6141 — precision. The
// only `owner` in the caller's package is a state variable / struct field.
// A bare call must stay unresolved rather than bind cross-scope to data.
func TestResolveScoped_LeafTier_MustNotBindCallToField_6141(t *testing.T) {
	existing := []graph.Entity{lfEnt(lfField, "Other.owner", "svc/c.go", "SCOPE.Schema")}
	fresh := []graph.Entity{lfEnt(lfCaller, "Local07", "svc/a.go", "SCOPE.Operation")}
	newRels := []graph.Relationship{mtCallRel(lfCaller, "owner", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, lfCaller)
	if got.ToID != "owner" {
		t.Errorf("bare CALLS `owner` bound to the FIELD Other.owner [SCOPE.Schema] @ svc/c.go "+
			"(ToID=%q); a call target must be an operation, so the stub must stay verbatim", got.ToID)
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
