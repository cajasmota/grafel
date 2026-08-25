// Package extractors — #6461 (MOUNT direction) unit coverage for the
// cross-file Pass-2.5 late bind.
//
// IN-PACKAGE because `bindDeferredPass25Stubs` is unexported. The end-to-end
// proof is cmd/grafel's `TestMountParity_6461_MountEdit_PathA`, which is now
// UNGATED; what is asserted here is the part that gate cannot separate — the
// two REFUSALS, which are the whole reason this is a bind rather than a guess:
//
//   - the TARGET must be globally unique by `Kind:Name` over the emitted
//     entity set;
//   - the SOURCE must resolve inside the CHANGED file that emitted the stub,
//     never corpus-wide, so the edge is owned by a re-extracted entity and the
//     FromID-keyed stale-edge eviction (#6094) can reclaim it.
//
// A permissive change — dropping either refusal, or binding sources from
// survivors — must fail here. The parity gate alone would not see it: its
// fixture has no ambiguous key and no unchanged-file source.
package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func lbEnt(kind, name, file string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID("test-repo", kind, name, file),
		Kind:       kind,
		Name:       name,
		SourceFile: file,
	}
}

func lbStub(file, from, to, kind string) deferredStubRel {
	return deferredStubRel{
		file: file,
		rel:  types.RelationshipRecord{FromID: from, ToID: to, Kind: kind},
	}
}

// TestBindDeferredPass25Stubs_BindsCrossFileTarget is the #6461 MOUNT-direction
// headline, in miniature: the mount file was edited, so `Service:mp_app` is a
// FRESH entity, while `Route:mp_router` lives on the unchanged route file and
// is only a SURVIVOR. The file-scoped binder refused it; this must bind it.
func TestBindDeferredPass25Stubs_BindsCrossFileTarget(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	survivors := []graph.Entity{lbEnt("Route", "mp_router", "route.py")}

	got := bindDeferredPass25Stubs(
		[]deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")},
		survivors, fresh, nil, nil, nil)

	if len(got) != 1 {
		t.Fatalf("late-bound %d relationship(s); want 1: %+v", len(got), got)
	}
	if got[0].FromID != fresh[0].ID {
		t.Errorf("FromID = %q, want the FRESH source entity id %q", got[0].FromID, fresh[0].ID)
	}
	if got[0].ToID != survivors[0].ID {
		t.Errorf("ToID = %q, want the SURVIVING target entity id %q", got[0].ToID, survivors[0].ID)
	}
	if got[0].Kind != "ROUTES_TO" {
		t.Errorf("Kind = %q, want ROUTES_TO", got[0].Kind)
	}
}

// TestBindDeferredPass25Stubs_RefusesAmbiguousTarget — two entities share
// `Route:mp_router` across two files. The corpus resolver has an ambiguity
// sentinel; this has a coin flip, so it must refuse. Ambiguity must also be
// STICKY: it is asserted with the unique-looking entity FIRST so a
// last-writer-wins map cannot pass.
func TestBindDeferredPass25Stubs_RefusesAmbiguousTarget(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	survivors := []graph.Entity{
		lbEnt("Route", "mp_router", "route_a.py"),
		lbEnt("Route", "mp_router", "route_b.py"),
	}

	got := bindDeferredPass25Stubs(
		[]deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")},
		survivors, fresh, nil, nil, nil)

	if len(got) != 0 {
		t.Fatalf("bound an AMBIGUOUS target: %+v", got)
	}
}

// TestBindDeferredPass25Stubs_RefusesSourceOutsideEmittingFile — the source
// `Kind:Name` exists, and is even freshly extracted, but on a DIFFERENT changed
// file than the one whose Detect run emitted the stub. Binding it would hang
// the edge off an entity the rule has no evidence about.
func TestBindDeferredPass25Stubs_RefusesSourceOutsideEmittingFile(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "other_changed.py")}
	survivors := []graph.Entity{lbEnt("Route", "mp_router", "route.py")}

	got := bindDeferredPass25Stubs(
		[]deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")},
		survivors, fresh, nil, nil, nil)

	if len(got) != 0 {
		t.Fatalf("bound a source outside the emitting file: %+v", got)
	}
}

// TestBindDeferredPass25Stubs_RefusesSurvivorSource — the source resolves only
// among SURVIVORS (an unchanged file). Such an edge would never be reclaimed by
// the FromID-keyed stale-edge eviction, so it must not be created here even
// though the file matches.
func TestBindDeferredPass25Stubs_RefusesSurvivorSource(t *testing.T) {
	survivors := []graph.Entity{
		lbEnt("Service", "mp_app", "mount.py"),
		lbEnt("Route", "mp_router", "route.py"),
	}

	got := bindDeferredPass25Stubs(
		[]deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")},
		survivors, nil, nil, nil, nil)

	if len(got) != 0 {
		t.Fatalf("bound a SURVIVOR source: %+v", got)
	}
}

// TestBindDeferredPass25Stubs_SkipsAlreadyPresentEdge — a stub that was ALSO
// bound in-file (or whose edge survived the prune) must not land a second copy.
// #6033 is what duplicate edge accumulation on this path looks like.
func TestBindDeferredPass25Stubs_SkipsAlreadyPresentEdge(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	survivors := []graph.Entity{lbEnt("Route", "mp_router", "route.py")}
	dup := graph.Relationship{
		ID:     graph.RelationshipID(fresh[0].ID, survivors[0].ID, "ROUTES_TO"),
		FromID: fresh[0].ID,
		ToID:   survivors[0].ID,
		Kind:   "ROUTES_TO",
	}

	stub := []deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")}

	if got := bindDeferredPass25Stubs(stub, survivors, fresh, []graph.Relationship{dup}, nil, nil); len(got) != 0 {
		t.Fatalf("re-emitted an edge already among the SURVIVING relationships: %+v", got)
	}
	if got := bindDeferredPass25Stubs(stub, survivors, fresh, nil, []graph.Relationship{dup}, nil); len(got) != 0 {
		t.Fatalf("re-emitted an edge already among the FRESH relationships: %+v", got)
	}

	// Positive control: without the duplicate it DOES bind, so the two
	// assertions above are observing the dedup and not a broken premise.
	if got := bindDeferredPass25Stubs(stub, survivors, fresh, nil, nil, nil); len(got) != 1 {
		t.Fatalf("positive control: want 1 bound, got %d", len(got))
	}
}

// TestBindDeferredPass25Stubs_LeavesNonStructuralRefsAlone — a bare name, a
// dotted import and an already-stamped hex id carry no `Kind:Name` key, so
// there is nothing here to bind and the scoped resolver keeps them.
func TestBindDeferredPass25Stubs_LeavesNonStructuralRefsAlone(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	survivors := []graph.Entity{lbEnt("Route", "mp_router", "route.py")}

	for _, to := range []string{"mp_router", "cfg.mp_router", "a1b2c3d4e5f60718"} {
		got := bindDeferredPass25Stubs(
			[]deferredStubRel{lbStub("mount.py", "Service:mp_app", to, "ROUTES_TO")},
			survivors, fresh, nil, nil, nil)
		if len(got) != 0 {
			t.Errorf("to=%q: bound a non-structural ref: %+v", to, got)
		}
	}
}

// TestBindDeferredPass25Stubs_RefusesComposeRemovedTarget pins the 7b→7c
// ORDERING, which is load-bearing and was otherwise documented nowhere.
//
// A permissive mutant that HOISTS Step 7c above Step 7b survives every other
// assertion in this file and both full packages: `go vet` 0,
// `./internal/extractors/` 0, `./cmd/grafel/` 0. It is a real defect because
// Step 7b deletes entities via `compose.removedIDs` and their edges via
// `compose.prunesRel`, and that edge filter walks `doc.Relationships` ONLY,
// never `newRels`. A late bind made against the PRE-prune entity set therefore
// lands in `newRels`, is appended at Step 8 after the relationship prune has
// already run, and reaches graph.fb as a dangling row no full rebuild holds.
//
// Passing the deletion set in and refusing it makes the hoist produce NOTHING
// rather than a dangling edge, and makes the constraint observable here.
func TestBindDeferredPass25Stubs_RefusesComposeRemovedTarget(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	survivors := []graph.Entity{lbEnt("Route", "mp_router", "route.py")}
	stub := []deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")}

	// Positive control FIRST: with an empty deletion set this binds, so the
	// refusal below is observing composeRemoved and not a broken premise.
	if got := bindDeferredPass25Stubs(stub, survivors, fresh, nil, nil, map[string]bool{}); len(got) != 1 {
		t.Fatalf("positive control: want 1 bound, got %d", len(got))
	}

	removed := map[string]bool{survivors[0].ID: true}
	if got := bindDeferredPass25Stubs(stub, survivors, fresh, nil, nil, removed); len(got) != 0 {
		t.Fatalf("bound a target Step 7b is deleting — the edge would land in "+
			"newRels and survive the relationship prune: %+v", got)
	}
}

// TestBindDeferredPass25Stubs_AmbiguityIsSticky pins that the sentinel is
// STICKY, not merely present — a THREE-way collision on one `Kind:Name` must
// stay refused, in either arrival order.
//
// This is strictly subtler than "the sentinel exists". A mutant that keeps the
// first poisoning but lets a LATER distinct entity un-poison the key —
//
//	if prev != e.ID {
//	    if prev == ambiguous { target[k] = e.ID } else { target[k] = ambiguous }
//	}
//
// — refuses every 2-WAY collision and therefore SURVIVED both packages
// (`go vet` 0, ./internal/extractors/ 0, ./cmd/grafel/ 0) before this test
// existed, including the AmbiguousRouter parity fixture, because every
// collision in the suite is 2-way. On a 3-way collision it binds where real
// code refuses, inventing an edge no full rebuild holds.
//
// Three files each doing `mp_router = APIRouter()` is an ordinary FastAPI
// layout, so this is a reachable input, not a theoretical one.
//
// BOTH orders are asserted because they exercise different branches of
// addTarget: unique→dup→third walks first-write, then poison, then the
// post-poison branch; dup-first→third reaches the post-poison branch with the
// key already sentinel-valued from the very first pair.
func TestBindDeferredPass25Stubs_AmbiguityIsSticky(t *testing.T) {
	fresh := []graph.Entity{lbEnt("Service", "mp_app", "mount.py")}
	stub := []deferredStubRel{lbStub("mount.py", "Service:mp_app", "Route:mp_router", "ROUTES_TO")}

	a := lbEnt("Route", "mp_router", "route_a.py")
	b := lbEnt("Route", "mp_router", "route_b.py")
	c := lbEnt("Route", "mp_router", "route_c.py")

	// Positive control FIRST: ONE target binds, so a refusal below is the
	// stickiness and not an unrelated failure to bind at all.
	if got := bindDeferredPass25Stubs(stub, []graph.Entity{a}, fresh, nil, nil, nil); len(got) != 1 {
		t.Fatalf("positive control: one unique target should bind, got %d", len(got))
	}

	for _, tc := range []struct {
		name      string
		survivors []graph.Entity
	}{
		{"two-way", []graph.Entity{a, b}},
		{"three-way", []graph.Entity{a, b, c}},
		{"four-way", []graph.Entity{a, b, c, lbEnt("Route", "mp_router", "route_d.py")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bindDeferredPass25Stubs(stub, tc.survivors, fresh, nil, nil, nil); len(got) != 0 {
				t.Fatalf("bound a %s ambiguous target — the sentinel was un-poisoned "+
					"by a later entity: %+v", tc.name, got)
			}
		})
	}

	// Same 3-way collision, but split across the SURVIVOR and FRESH slices so
	// the poisoning happens in the survivors loop and the third arrival lands in
	// the fresh loop. addTarget is called from both; a mutant that only gets the
	// stickiness right within one loop must not pass.
	t.Run("three-way-split-across-survivors-and-fresh", func(t *testing.T) {
		got := bindDeferredPass25Stubs(stub,
			[]graph.Entity{a, b},
			append([]graph.Entity{}, fresh[0], c),
			nil, nil, nil)
		if len(got) != 0 {
			t.Fatalf("bound a 3-way ambiguous target split across survivors/fresh: %+v", got)
		}
	})

	// And the third arrival re-asserting an ID ALREADY seen must not un-poison
	// either: `prev != e.ID` is false for a repeat, so the key must simply stay
	// sentinel-valued rather than falling through to a write.
	t.Run("three-way-with-repeat-of-first", func(t *testing.T) {
		if got := bindDeferredPass25Stubs(stub, []graph.Entity{a, b, a}, fresh, nil, nil, nil); len(got) != 0 {
			t.Fatalf("bound after a repeat of an already-seen id un-poisoned the key: %+v", got)
		}
	})
}
