package sresolver_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Receiver-type / package-member tier (#6098, #6090 residual)
//
// The scoped resolver's ladder used to stop at whole-string name /
// qualified-name plus the Format A (file, tail) tiers. The corpus-wide
// resolver has one more: when a CALLS edge carries
// Properties["receiver_type"], it probes a PACKAGE-scoped member index —
// byPackageMember[pkgDirOf(callerFile)][receiverType][member] — so a bare
// stub `Do11` out of svc/a.go binds to the entity `T11.Do11` declared in the
// SIBLING file svc/b.go (internal/resolve/refs.go:5684, refs #148/#364).
//
// byLocation cannot do this: it is keyed on the exact source file, so a
// same-package/different-file member is invisible to it, and the bare name
// `Do11` is not a key in nameToID (the entity is named `T11.Do11`).
//
// These are unit tests on ResolveScoped's observable output — no source
// scanning.
// ─────────────────────────────────────────────────────────────────────────────

const (
	mtCaller = "1111111111111111" // svc/a.go — the calling method
	mtDoB    = "2222222222222222" // svc/b.go — T11.Do11
	mtDoC    = "3333333333333333" // svc/c.go — a second T11.Do11 (ambiguity)
	mtDoOth  = "4444444444444444" // other/b.go — T11.Do11 in a DIFFERENT package
)

func mtEnt(id, name, sourceFile string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "SCOPE.Operation",
		SourceFile: sourceFile,
		Language:   "go",
	}
}

// mtCallRel builds a CALLS edge with a bare-name stub ToID, optionally
// stamped with a receiver_type property (empty string = unstamped).
func mtCallRel(fromID, stub, recvType string) graph.Relationship {
	r := graph.Relationship{
		ID:     graph.RelationshipID(fromID, stub, "CALLS"),
		FromID: fromID,
		ToID:   stub,
		Kind:   "CALLS",
	}
	if recvType != "" {
		r = r.WithProperties(map[string]string{"receiver_type": recvType})
	}
	return r
}

// mtOnlyCall returns the single CALLS edge out of fromID in rels.
func mtOnlyCall(t *testing.T, rels []graph.Relationship, fromID string) graph.Relationship {
	t.Helper()
	var hits []graph.Relationship
	for _, r := range rels {
		if r.Kind == "CALLS" && r.FromID == fromID {
			hits = append(hits, r)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 CALLS edge from %s, got %d: %+v", fromID, len(hits), hits)
	}
	return hits[0]
}

// TestResolveScoped_ReceiverTypeTier_SamePackageSiblingFile is the core
// #6090-residual case: a call the edit ITSELF introduces, so there is no
// prior binding for replayPriorResolution to replay. Only a resolver tier can
// bind it.
func TestResolveScoped_ReceiverTypeTier_SamePackageSiblingFile(t *testing.T) {
	// Sibling file in the same package, present in the SURVIVING set.
	existing := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
	// The caller is freshly extracted out of the edited file.
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	if res.FallbackRequired {
		t.Fatalf("unexpected fallback: %s", res.UnresolvedTarget)
	}
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != mtDoB {
		t.Errorf("bare stub %q with receiver_type=T11 from svc/a.go should bind to T11.Do11 in the "+
			"sibling file svc/b.go (%s); got ToID=%q (#6098 / #6090 residual)", "Do11", mtDoB, got.ToID)
	}
	if got.ID != graph.RelationshipID(got.FromID, got.ToID, got.Kind) {
		t.Errorf("rebound edge did not have its RelationshipID recomputed: ID=%q", got.ID)
	}
}

// TestResolveScoped_ReceiverTypeTier_QualifiedStampStripped covers the #364
// follow-up: the extractor may stamp a package-qualified receiver
// (`chi.Mux`), while entities are emitted under the bare receiver name.
func TestResolveScoped_ReceiverTypeTier_QualifiedStampStripped(t *testing.T) {
	existing := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "pkg.T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != mtDoB {
		t.Errorf("package-qualified receiver stamp %q should be retried bare (#364); got ToID=%q",
			"pkg.T11", got.ToID)
	}
}

// TestResolveScoped_ReceiverTypeTier_AmbiguousStaysUnresolved is the
// SOUNDNESS guard. Two entities named T11.Do11 live in the same package
// directory (different files). The corpus-wide resolver calls this ambiguous
// and leaves the stub verbatim; the scoped resolver must do the same rather
// than silently picking one.
//
// Deleting the blank-sentinel ambiguity guard makes this test bind to
// whichever candidate was indexed last — a confident WRONG edge, which is
// strictly worse than the unresolved stub it replaces.
func TestResolveScoped_ReceiverTypeTier_AmbiguousStaysUnresolved(t *testing.T) {
	existing := []graph.Entity{
		mtEnt(mtDoB, "T11.Do11", "svc/b.go"),
		mtEnt(mtDoC, "T11.Do11", "svc/c.go"),
	}
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != "Do11" {
		t.Errorf("two candidates for (svc, T11, Do11) must leave the stub UNRESOLVED, matching the "+
			"corpus-wide resolver; got a confident binding ToID=%q (candidates were %s and %s)",
			got.ToID, mtDoB, mtDoC)
	}
}

// TestResolveScoped_ReceiverTypeTier_AmbiguityDoesNotFallThrough is the
// SECOND half of the soundness guard, and the one the previous test cannot
// see. Returning false on ambiguity and *continuing down the ladder* is
// observationally identical to refusing, UNLESS a later tier can bind the
// name — and the very next tier is the last-writer-wins whole-string tier,
// which has no ambiguity sentinel at all.
//
// So: (svc, T11, Do11) is ambiguous AND a foreign-package free function is
// literally named `Do11`. Falling through converts a correctly-ambiguous stub
// into a confident cross-package edge. That mutation survives the previous
// test and is killed only by this one.
//
// THIS ASSERTS A DELIBERATE ASYMMETRY WITH refs.go, NOT PARITY WITH IT.
// On exactly this shape the corpus-wide resolver BINDS the edge: refs.go:5697
// `break`s with resolved=false on ambiguity and control reaches
// rewriteOneWithCaller's global name index, despite an inline comment there
// claiming it "preserves the stub". So a full rebuild resolves this and the
// scoped path leaves a stub — a divergence in the #6090 loss direction, which
// will become a live failure if a fixture ever reaches this shape.
//
// We keep the refusal anyway, because refs.go's fall-through is safe only for
// refs.go: its global tier carries an ambiguity sentinel and the scoped
// ladder's nameToID does not, so falling through here binds arbitrarily
// rather than bind-or-refuse. Closing the divergence means fixing the refs.go
// side; that is filed separately. Do not "restore parity" by deleting this.
func TestResolveScoped_ReceiverTypeTier_AmbiguityDoesNotFallThrough(t *testing.T) {
	existing := []graph.Entity{
		mtEnt(mtDoB, "T11.Do11", "svc/b.go"),
		mtEnt(mtDoC, "T11.Do11", "svc/c.go"),
		mtEnt(mtDoOth, "Do11", "other/free.go"), // whole-string tier WOULD hit this
	}
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID == mtDoOth {
		t.Errorf("ambiguity fell through to the unsound whole-string tier and bound the "+
			"foreign-package %q; ambiguity must terminate the ToID ladder", mtDoOth)
	}
	if got.ToID != "Do11" {
		t.Errorf("ambiguous (svc, T11, Do11) must stay unresolved; got ToID=%q", got.ToID)
	}
}

// TestResolveScoped_ReceiverTypeTier_DoesNotCrossPackages guards the other
// direction: the tier is package-SCOPED. A same-named member in a different
// directory must not be bound, or the scoped path would invent cross-package
// edges the full rebuild does not have.
func TestResolveScoped_ReceiverTypeTier_DoesNotCrossPackages(t *testing.T) {
	existing := []graph.Entity{mtEnt(mtDoOth, "T11.Do11", "other/b.go")}
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != "Do11" {
		t.Errorf("T11.Do11 in package %q must not bind a caller in package %q; got ToID=%q",
			"other", "svc", got.ToID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Leaf-name tiers (#778) — the ones that bind REAL Go source
//
// The receiver-stamped tier above is the precise one, but it only fires when
// the callee is invoked on the enclosing method's OWN receiver. The
// #6090-residual shape is `a.Do11(x)` on a local variable inside a free
// function, which carries no stamp. The corpus-wide resolver binds that with
// lookupMemberByLeafName / lookupPackageMemberByLeafName
// (internal/resolve/refs.go:2963 and :2969, probed from
// rewriteOneWithCaller): a bare CALLS name matching exactly ONE member across
// the caller's file, then across the caller's package.
//
// Grounding note recorded because it contradicts #6098's own write-up: that
// issue attributes the gap to the receiver-type tier alone. Measured
// end-to-end, the tier that binds `a.Do11(x)` is the #778 leaf tier. Porting
// only the receiver tier leaves the residual open.
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveScoped_LeafTier_SamePackageUnstamped is the #6090-residual shape.
func TestResolveScoped_LeafTier_SamePackageUnstamped(t *testing.T) {
	existing := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
	fresh := []graph.Entity{mtEnt(mtCaller, "Local07", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != mtDoB {
		t.Errorf("unstamped bare CALLS %q should bind to the unique same-package member T11.Do11 "+
			"(%s) via the #778 leaf tier; got ToID=%q", "Do11", mtDoB, got.ToID)
	}
}

// TestResolveScoped_LeafTier_AmbiguousStaysUnresolved is the leaf tier's own
// soundness guard. Note this is a DIFFERENT ambiguity from the receiver
// tier's: there the scope is pinned and the collision is inside it; here two
// distinct scopes each declare a member by that name and there is no type
// information to choose between them.
func TestResolveScoped_LeafTier_AmbiguousStaysUnresolved(t *testing.T) {
	existing := []graph.Entity{
		mtEnt(mtDoB, "T11.Do11", "svc/b.go"),
		mtEnt(mtDoC, "T23.Do11", "svc/c.go"), // different scope, same member
	}
	fresh := []graph.Entity{mtEnt(mtCaller, "Local07", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != "Do11" {
		t.Errorf("two scopes in package svc declare member Do11 — the stub must stay unresolved; "+
			"got a confident binding ToID=%q", got.ToID)
	}
}

// TestResolveScoped_LeafTier_IsCallsOnly: the leaf tiers key on a bare member
// name with NO scope information, so they are gated to CALLS, matching
// refs.go's `case "CALLS":`. A DEPENDS_ON naming a type must not catch a
// same-named method.
//
// The stamped sub-case is the one that actually exercises lookupLeaf's inner
// CALLS gate. Without it the gate is DEAD CODE for testing purposes:
// relWantsMemberTier already rejects an unstamped non-CALLS edge, so deleting
// the inner gate leaves the unstamped case passing. The stamp gets the edge
// past relWantsMemberTier, and a receiver type with no matching scope gets it
// past tier 1, so the inner gate is the only thing standing between a
// DEPENDS_ON and a method binding.
func TestResolveScoped_LeafTier_IsCallsOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recvType string
	}{
		// Rejected early, by relWantsMemberTier.
		{"unstamped", ""},
		// Reaches lookupLeaf and is stopped only by its inner CALLS gate. The
		// receiver type names no scope in the package, so tier 1 misses.
		{"stamped_with_unknown_receiver", "Tzz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
			fresh := []graph.Entity{mtEnt(mtCaller, "Local07", "svc/a.go")}
			dep := graph.Relationship{
				ID:     graph.RelationshipID(mtCaller, "Do11", "DEPENDS_ON"),
				FromID: mtCaller,
				ToID:   "Do11",
				Kind:   "DEPENDS_ON",
			}
			if tc.recvType != "" {
				dep = dep.WithProperties(map[string]string{"receiver_type": tc.recvType})
			}

			res := sresolver.ResolveScoped(fresh, existing, []graph.Relationship{dep}, nil, nil)
			if len(res.ResolvedNewRelationships) != 1 {
				t.Fatalf("expected 1 rel, got %d", len(res.ResolvedNewRelationships))
			}
			if got := res.ResolvedNewRelationships[0]; got.ToID != "Do11" {
				t.Errorf("the leaf tier is CALLS-only; a DEPENDS_ON must not bind to the method "+
					"T11.Do11 (%s); got ToID=%q", mtDoB, got.ToID)
			}
		})
	}
}

// TestResolveScoped_LeafTier_FollowsWholeStringTier pins the OTHER half of the
// ordering. Unlike the receiver tier, the leaf tiers are FALLBACKS inside
// rewriteOneWithCaller — they run only after the global name index misses. So
// an entity literally NAMED `Do11` must win over a leaf match on `T11.Do11`.
//
// Together with TestResolveScoped_ReceiverTypeTier_PrecedesWholeStringTier
// this brackets the whole-string tier from both sides; reversing either
// direction changes which entity a real call binds to.
func TestResolveScoped_LeafTier_FollowsWholeStringTier(t *testing.T) {
	const exactName = "5555555555555555"
	existing := []graph.Entity{
		mtEnt(mtDoB, "T11.Do11", "svc/b.go"),
		mtEnt(exactName, "Do11", "svc/free.go"), // exact-name free function
	}
	fresh := []graph.Entity{mtEnt(mtCaller, "Local07", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != exactName {
		t.Errorf("an entity literally named %q must win over the #778 leaf tier's member match; "+
			"got ToID=%q (want %s)", "Do11", got.ToID, exactName)
	}
}

// TestResolveScoped_LeafTier_SameFileBeatsSamePackage pins tier 2 ahead of
// tier 3 (refs.go probes lookupMemberByLeafName before
// lookupPackageMemberByLeafName). Two scopes declare `Do11`, one of them in
// the caller's own file — which makes the package-wide view ambiguous and the
// same-file view decisive.
func TestResolveScoped_LeafTier_SameFileBeatsSamePackage(t *testing.T) {
	existing := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
	fresh := []graph.Entity{
		mtEnt(mtCaller, "Local07", "svc/a.go"),
		mtEnt(mtDoC, "T23.Do11", "svc/a.go"), // same file as the caller
	}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != mtDoC {
		t.Errorf("the same-file member T23.Do11 (%s) must win over the same-package T11.Do11 (%s); "+
			"got ToID=%q", mtDoC, mtDoB, got.ToID)
	}
}

// TestResolveScoped_LeafTier_DoesNotCrossPackages: the leaf tier is
// package-scoped, and a same-named member in another directory must not be
// bound.
func TestResolveScoped_LeafTier_DoesNotCrossPackages(t *testing.T) {
	existing := []graph.Entity{mtEnt(mtDoOth, "T11.Do11", "other/b.go")}
	fresh := []graph.Entity{mtEnt(mtCaller, "Local07", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID != "Do11" {
		t.Errorf("T11.Do11 in package %q must not bind a caller in package %q; got ToID=%q",
			"other", "svc", got.ToID)
	}
}

// TestResolveScoped_ReceiverTypeTier_AppliesToSurvivingInboundEdges: the
// corpus-wide resolver applies this tier to EVERY edge it walks, not only to
// the ones out of changed files. A surviving edge left in stub form by an
// earlier build must be bindable too, or the incremental graph keeps a stub
// the full rebuild resolves.
func TestResolveScoped_ReceiverTypeTier_AppliesToSurvivingInboundEdges(t *testing.T) {
	// The caller survives; the callee is what was just re-extracted.
	existing := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	fresh := []graph.Entity{mtEnt(mtDoB, "T11.Do11", "svc/b.go")}
	existingRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, nil, existingRels, nil)
	if res.FallbackRequired {
		t.Fatalf("unexpected fallback: %s", res.UnresolvedTarget)
	}
	got := mtOnlyCall(t, res.UpdatedExistingRelationships, mtCaller)
	if got.ToID != mtDoB {
		t.Errorf("surviving stub edge with receiver_type=T11 should bind to the re-extracted "+
			"T11.Do11 (%s); got ToID=%q", mtDoB, got.ToID)
	}
	if res.InboundFixed != 1 {
		t.Errorf("expected InboundFixed=1, got %d", res.InboundFixed)
	}
}

// TestResolveScoped_ReceiverTypeTier_PrecedesWholeStringTier pins the tier
// ORDER. The scoped ladder's whole-string tier is last-writer-wins with no
// ambiguity sentinel, so it is unsound where the corpus-wide resolver is not.
// The receiver tier must be probed FIRST, exactly as
// internal/resolve/refs.go does ("probe the package-scoped member index FIRST
// so a bare-name target like `handle` binds to the local `<pkg>/Mux.handle`
// rather than colliding with same-named methods elsewhere").
//
// Here a FOREIGN-package entity is literally NAMED `Do11`, so nameToID["Do11"]
// hits. If the whole-string tier ran first the edge would bind cross-package;
// with the receiver tier first it binds to the local T11.Do11.
func TestResolveScoped_ReceiverTypeTier_PrecedesWholeStringTier(t *testing.T) {
	existing := []graph.Entity{
		mtEnt(mtDoB, "T11.Do11", "svc/b.go"),
		mtEnt(mtDoOth, "Do11", "other/free.go"), // free function, foreign package
	}
	fresh := []graph.Entity{mtEnt(mtCaller, "T11.Use", "svc/a.go")}
	newRels := []graph.Relationship{mtCallRel(mtCaller, "Do11", "T11")}

	res := sresolver.ResolveScoped(fresh, existing, newRels, nil, nil)
	got := mtOnlyCall(t, res.ResolvedNewRelationships, mtCaller)
	if got.ToID == mtDoOth {
		t.Errorf("receiver tier must precede the whole-string tier: bound to the foreign-package "+
			"free function %q instead of the same-package T11.Do11", mtDoOth)
	}
	if got.ToID != mtDoB {
		t.Errorf("expected ToID=%s (svc/b.go T11.Do11), got %q", mtDoB, got.ToID)
	}
}
