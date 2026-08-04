package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Guard-level tests for the #6090 prior-resolution replay.
//
// The end-to-end gates in cmd/grafel pin the HAPPY PATH: a resolved edge out of
// a changed file survives repeated edits, and a deleted call still disappears.
// They do not pin the mechanisms that keep the replay from becoming the #6085
// failure mode (retaining edges that no longer reflect source) — every one of
// those guards could be deleted with the behavioural suite still green, because
// the fixtures never reach them.
//
// These tests reach them directly. Each one is a mutation gate: deleting the
// single guard it names makes exactly this test fail.
//
// The subject is a pure function over slices, so the cases are exact — no
// extraction, no resolver, no corpus.

const (
	prCaller = "aaaaaaaaaaaaaaa1" // 16 hex — a resolved entity id
	prAlpha  = "bbbbbbbbbbbbbbb2"
	prBeta   = "ccccccccccccccc3"
	prDead   = "ddddddddddddddd4"
	prDead2  = "eeeeeeeeeeeeeee5"
)

// prRel builds a relationship, optionally with properties.
func prRel(from, to, kind string, props map[string]string) graph.Relationship {
	r := graph.Relationship{ID: graph.RelationshipID(from, to, kind), FromID: from, ToID: to, Kind: kind}
	if len(props) > 0 {
		r = r.WithProperties(props)
	}
	return r
}

func prEnt(id, name, file string) graph.Entity {
	return graph.Entity{ID: id, Name: name, Kind: "SCOPE.Operation", SourceFile: file}
}

// ── the baseline: the replay fires at all ────────────────────────────────────

// TestReplayPriorResolution_BindsUnresolvedStubFromPriorBinding is the control.
// Without it a test that asserts "nothing was bound" could pass on a function
// that never binds anything.
func TestReplayPriorResolution_BindsUnresolvedStubFromPriorBinding(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Do11", "CALLS", nil)}
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", nil)}
	survivors := []graph.Entity{prEnt(prAlpha, "T11.Do11", "r11.go")}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 1 {
		t.Fatalf("bound %d endpoint(s), want 1", n)
	}
	if fresh[0].ToID != prAlpha {
		t.Errorf("ToID = %q, want the prior binding %q", fresh[0].ToID, prAlpha)
	}
	if want := graph.RelationshipID(prCaller, prAlpha, "CALLS"); fresh[0].ID != want {
		t.Errorf("ID = %q, want it recomputed to %q after the rebind", fresh[0].ID, want)
	}
}

// TestReplayPriorResolution_NeverAppendsRows pins the structural property the
// whole safety argument rests on: the replay only ever mutates the ToID of an
// edge the fresh extraction emitted. It cannot resurrect a prior edge, so a call
// deleted from source has nothing to bind.
func TestReplayPriorResolution_NeverAppendsRows(t *testing.T) {
	// The fresh pass emitted NOTHING for the prior edge's name — the call was
	// deleted from source. The prior binding must not come back in any form.
	fresh := []graph.Relationship{prRel(prCaller, "Unrelated", "CALLS", nil)}
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", nil)}
	survivors := []graph.Entity{prEnt(prAlpha, "T11.Do11", "r11.go")}

	before := len(fresh)
	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 0 {
		t.Errorf("bound %d endpoint(s) for a name the fresh pass never emitted, want 0", n)
	}
	if len(fresh) != before {
		t.Fatalf("the replay changed the row count %d → %d — it must never append", before, len(fresh))
	}
	if fresh[0].ToID != "Unrelated" {
		t.Errorf("ToID = %q, want the unresolved stub left verbatim", fresh[0].ToID)
	}
}

// ── guard 1: liveness ────────────────────────────────────────────────────────

// TestReplayPriorResolution_RefusesDeadPriorTarget is the mutation gate for the
// `live` filter in replayPriorResolution.
//
// The prior graph bound "Do11" to an entity this run's re-extraction destroyed
// (the method was deleted, or moved to another file — entity IDs are
// deterministic over kind/name/source_file, so a move re-keys it). Replaying
// that binding would point a live edge at an id no entity carries: a dangling
// endpoint a full rebuild never produces. Without the filter this test binds.
func TestReplayPriorResolution_RefusesDeadPriorTarget(t *testing.T) {
	fresh := []graph.Relationship{
		prRel(prCaller, "Do11", "CALLS", nil),  // prior target destroyed
		prRel(prCaller, "Other", "CALLS", nil), // prior target still live
	}
	prior := []graph.Relationship{
		prRel(prCaller, prDead, "CALLS", nil),
		prRel(prCaller, prAlpha, "CALLS", nil),
	}
	// prDead is in NEITHER the survivors nor the freshly extracted entities.
	// The live sibling is deliberately present so the case does not pass merely
	// via the "no live prior targets at all" early exit.
	survivors := []graph.Entity{prEnt(prAlpha, "T99.Other", "r99.go")}

	n := replayPriorResolution(fresh, prior, survivors, nil)
	if fresh[0].ToID != "Do11" {
		t.Errorf("ToID = %q, want the stub left verbatim rather than bound to the dead id %q",
			fresh[0].ToID, prDead)
	}
	if n != 1 || fresh[1].ToID != prAlpha {
		t.Errorf("bound n=%d, fresh[1].ToID=%q; want n=1 with only the LIVE prior target bound",
			n, fresh[1].ToID)
	}
}

// TestReplayPriorResolution_AcceptsTargetReEmittedByThisRun is the other half of
// the liveness rule: a prior target INSIDE the changed file was removed from the
// survivor set and comes back only via re-extraction, under the same id. It is
// live, and refusing it would re-open the #6090 loss for intra-file edges.
func TestReplayPriorResolution_AcceptsTargetReEmittedByThisRun(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "N", "CALLS", nil)}
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", nil)}
	reEmitted := []graph.Entity{prEnt(prAlpha, "T07.N", "r07.go")}

	if n := replayPriorResolution(fresh, prior, nil, reEmitted); n != 1 {
		t.Fatalf("bound %d endpoint(s) for a target re-emitted by this run, want 1", n)
	}
	if fresh[0].ToID != prAlpha {
		t.Errorf("ToID = %q, want %q", fresh[0].ToID, prAlpha)
	}
}

// ── guard 2: ambiguity ───────────────────────────────────────────────────────

// TestReplayPriorResolution_RefusesAmbiguousPriorBinding is the mutation gate
// for the "two distinct targets → give up" branch in replayLookup.
//
// The prior graph bound the same bare name, from the same source under the same
// kind, to two different live entities ("A30.Same" and "B31.Same" both spell the
// member name "Same"). That is exactly the case the corpus-wide resolver itself
// calls ambiguous and leaves unresolved, so the replay must leave the stub
// verbatim too. Without the guard it binds to whichever prior row is visited
// first — a coin flip persisted into the graph.
func TestReplayPriorResolution_RefusesAmbiguousPriorBinding(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Same", "CALLS", nil)}
	prior := []graph.Relationship{
		prRel(prCaller, prAlpha, "CALLS", nil),
		prRel(prCaller, prBeta, "CALLS", nil),
	}
	survivors := []graph.Entity{
		prEnt(prAlpha, "A30.Same", "r30.go"),
		prEnt(prBeta, "B31.Same", "r31.go"),
	}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 0 {
		t.Errorf("bound %d endpoint(s) for a name the prior graph bound to TWO live targets, want 0", n)
	}
	if fresh[0].ToID != "Same" {
		t.Errorf("ToID = %q, want the ambiguous stub left verbatim", fresh[0].ToID)
	}
}

// TestReplayPriorResolution_AmbiguityIsPerNameKeyNotPerTarget guards the
// converse: two prior rows that agree on the target are not ambiguous (the same
// call appearing twice in a body, or the same edge reached through both the Name
// and the tail key), and must still bind.
func TestReplayPriorResolution_AmbiguityIsPerNameKeyNotPerTarget(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Do11", "CALLS", nil)}
	prior := []graph.Relationship{
		prRel(prCaller, prAlpha, "CALLS", nil),
		prRel(prCaller, prAlpha, "CALLS", nil),
	}
	survivors := []graph.Entity{prEnt(prAlpha, "T11.Do11", "r11.go")}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 1 {
		t.Errorf("bound %d endpoint(s) for two prior rows agreeing on one target, want 1", n)
	}
}

// TestReplayPriorResolution_AmbiguityIsScopedToSourceAndKind pins that the index
// key is the full (FromID, Kind, name) triple: the same member name bound from a
// DIFFERENT caller, or under a different edge kind, is not a competing candidate.
func TestReplayPriorResolution_AmbiguityIsScopedToSourceAndKind(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Same", "CALLS", nil)}
	prior := []graph.Relationship{
		prRel(prCaller, prAlpha, "CALLS", nil),
		prRel("aaaaaaaaaaaaaaa9", prBeta, "CALLS", nil), // other caller
		prRel(prCaller, prBeta, "REFERENCES", nil),      // other kind
	}
	survivors := []graph.Entity{
		prEnt(prAlpha, "A30.Same", "r30.go"),
		prEnt(prBeta, "B31.Same", "r31.go"),
	}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 1 {
		t.Fatalf("bound %d endpoint(s), want 1 — only the same (source, kind) is a candidate", n)
	}
	if fresh[0].ToID != prAlpha {
		t.Errorf("ToID = %q, want %q", fresh[0].ToID, prAlpha)
	}
}

// ── guard 3: the receiver_type veto ──────────────────────────────────────────

// TestReplayPriorResolution_ReceiverTypeVetoBlocksRetarget is the mutation gate
// for the receiver_type comparison in replayLookup.
//
// The call site moved from `a.Same(x)` (receiver A30) to `b.Same(x)` (receiver
// B07) in the same edit. The stub is "Same" either way, so without the veto the
// prior binding to A30.Same is replayed onto a call that now targets a different
// type — a wrong edge, persisted, where today there would merely be an
// unresolved stub.
func TestReplayPriorResolution_ReceiverTypeVetoBlocksRetarget(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Same", "CALLS", map[string]string{"receiver_type": "B07"})}
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", map[string]string{"receiver_type": "A30"})}
	survivors := []graph.Entity{prEnt(prAlpha, "A30.Same", "r30.go")}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 0 {
		t.Errorf("bound %d endpoint(s) across a changed receiver_type, want 0", n)
	}
	if fresh[0].ToID != "Same" {
		t.Errorf("ToID = %q, want the stub left verbatim", fresh[0].ToID)
	}
}

// TestReplayPriorResolution_ReceiverTypeVetoDisambiguates pins the veto's second
// role: when the prior graph bound one member name to two live targets, matching
// receivers pick the right one instead of giving up.
func TestReplayPriorResolution_ReceiverTypeVetoDisambiguates(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Same", "CALLS", map[string]string{"receiver_type": "B31"})}
	prior := []graph.Relationship{
		prRel(prCaller, prAlpha, "CALLS", map[string]string{"receiver_type": "A30"}),
		prRel(prCaller, prBeta, "CALLS", map[string]string{"receiver_type": "B31"}),
	}
	survivors := []graph.Entity{
		prEnt(prAlpha, "A30.Same", "r30.go"),
		prEnt(prBeta, "B31.Same", "r31.go"),
	}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 1 {
		t.Fatalf("bound %d endpoint(s), want 1", n)
	}
	if fresh[0].ToID != prBeta {
		t.Errorf("ToID = %q, want the receiver-matched target %q", fresh[0].ToID, prBeta)
	}
}

// TestReplayPriorResolution_ReceiverTypeVetoIsOffWhenUnstamped documents the
// veto's known limit rather than pretending it is universal: the property is
// stamped by the Go extractor only, and even there it is dropped when the
// extractor is unsure of the receiver. When either side lacks it the veto cannot
// fire, and the binding rests on the ambiguity guard alone — which is why that
// guard, not this one, is the load-bearing one.
func TestReplayPriorResolution_ReceiverTypeVetoIsOffWhenUnstamped(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Same", "CALLS", nil)} // no receiver_type
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", map[string]string{"receiver_type": "A30"})}
	survivors := []graph.Entity{prEnt(prAlpha, "A30.Same", "r30.go")}

	if n := replayPriorResolution(fresh, prior, survivors, nil); n != 1 {
		t.Errorf("bound %d endpoint(s), want 1 — an unstamped fresh edge is not vetoed", n)
	}
}

// ── guard 4: the fresh resolution always wins ────────────────────────────────

// TestReplayPriorResolution_LeavesResolvedEdgesAlone is the mutation gate for
// the `if replayIsHexID(r.ToID) { continue }` skip.
//
// The skip is a real contract — the fresh pass's own resolution is authoritative
// and must never be overwritten by a stale one — even though the index cannot
// reach a hex endpoint in practice (its keys are entity names). Pinning it here
// means a future change that starts indexing ids, or an entity whose Name is
// 16 hex characters, is caught rather than silently retargeting live edges.
func TestReplayPriorResolution_LeavesResolvedEdgesAlone(t *testing.T) {
	fresh := []graph.Relationship{
		// The edge under test: already resolved by the fresh pass.
		prRel(prCaller, prBeta, "CALLS", nil),
		// A second, genuinely unresolved edge. Without it the function's
		// "nothing to do" pre-scan would return before reaching the per-edge
		// skip, and this case would pass on a tree with the skip deleted.
		prRel(prCaller, "Do11", "CALLS", nil),
	}
	prior := []graph.Relationship{
		prRel(prCaller, prAlpha, "CALLS", nil),
		prRel(prCaller, prDead2, "CALLS", nil),
	}
	// The first prior target is NAMED with the fresh edge's resolved ToID, so
	// an index keyed on names reaches it the moment the hex skip is removed.
	survivors := []graph.Entity{
		prEnt(prAlpha, prBeta, "r30.go"),
		prEnt(prBeta, "T23.Do23", "r23.go"),
		prEnt(prDead2, "T11.Do11", "r11.go"),
	}

	n := replayPriorResolution(fresh, prior, survivors, nil)
	if fresh[0].ToID != prBeta {
		t.Errorf("ToID = %q, want the fresh resolution %q preserved — an ALREADY-RESOLVED "+
			"endpoint must never be overwritten by a prior binding", fresh[0].ToID, prBeta)
	}
	// The unresolved sibling is expected to bind; only the resolved one is off limits.
	if n != 1 || fresh[1].ToID != prDead2 {
		t.Errorf("bound n=%d, fresh[1].ToID=%q; want n=1 with the UNRESOLVED sibling bound to %q",
			n, fresh[1].ToID, prDead2)
	}
}

// ── name-key shapes ──────────────────────────────────────────────────────────

// TestReplayPriorResolution_NameKeyShapes pins which spellings of a stub reach a
// prior binding. The member-tail form is the whole point of the fix (entity
// "T11.Do11" vs call-site stub "Do11"); the Format A structural ref is the other
// shape the extractors emit.
func TestReplayPriorResolution_NameKeyShapes(t *testing.T) {
	cases := []struct {
		name     string
		stub     string
		priorEnt graph.Entity
		wantBind bool
	}{
		{"bare member tail", "Do11", prEnt(prAlpha, "T11.Do11", "r11.go"), true},
		{"whole name", "T11.Do11", prEnt(prAlpha, "T11.Do11", "r11.go"), true},
		{"format A ref", "scope:operation:method:go:r11.go:Do11", prEnt(prAlpha, "T11.Do11", "r11.go"), true},
		{"qualified name", "svc.T11.Do11", graph.Entity{
			ID: prAlpha, Name: "T11.Do11", QualifiedName: "svc.T11.Do11", SourceFile: "r11.go",
		}, true},
		{"unrelated name", "Nope", prEnt(prAlpha, "T11.Do11", "r11.go"), false},
		// A stub that shares only a PREFIX must not bind — suffix matching is
		// deliberate, prefix matching would be a fabrication.
		{"prefix only", "T11", prEnt(prAlpha, "T11.Do11", "r11.go"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fresh := []graph.Relationship{prRel(prCaller, tc.stub, "CALLS", nil)}
			prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", nil)}
			n := replayPriorResolution(fresh, prior, []graph.Entity{tc.priorEnt}, nil)
			if got := n == 1; got != tc.wantBind {
				t.Errorf("bound=%v (n=%d), want bound=%v", got, n, tc.wantBind)
			}
		})
	}
}

// TestReplayPriorResolution_NoPriorOutboundIsANoOp pins the cheap exits: a run
// with nothing pruned, or nothing unresolved, must not touch the fresh edges.
func TestReplayPriorResolution_NoPriorOutboundIsANoOp(t *testing.T) {
	fresh := []graph.Relationship{prRel(prCaller, "Do11", "CALLS", nil)}
	if n := replayPriorResolution(fresh, nil, []graph.Entity{prEnt(prAlpha, "T11.Do11", "r11.go")}, nil); n != 0 {
		t.Errorf("bound %d with no prior outbound edges, want 0", n)
	}
	resolved := []graph.Relationship{prRel(prCaller, prBeta, "CALLS", nil)}
	prior := []graph.Relationship{prRel(prCaller, prAlpha, "CALLS", nil)}
	if n := replayPriorResolution(resolved, prior, []graph.Entity{prEnt(prAlpha, "T11.Do11", "r11.go")}, nil); n != 0 {
		t.Errorf("bound %d with no unresolved endpoints, want 0", n)
	}
}
