package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestOutstandingCaveat_NothingOutstanding_ReturnsEmpty pins direction 2 for
// #6047: a status snapshot where nothing names our group (an already-warm
// overlay, or a small group that finished before the wizard even asked) must
// produce NO caveat. This must be checked against a StatusReply that DOES
// name a DIFFERENT group in every field, proving the emptiness is really
// group-scoped filtering and not just "always returns ”".
func TestOutstandingCaveat_NothingOutstanding_ReturnsEmpty(t *testing.T) {
	st := proto.StatusReply{
		GroupAlgoRunning:  []string{"other-group"},
		PendingAlgo:       []string{"other-group"},
		PendingLinks:      []string{"other-group"},
		StageGateHolder:   "group-algo:other-group",
		StageGateDeferred: []string{"links:other-group"},
		StageGateBarging:  []string{"analytics:other-group"},
	}
	got := outstandingCaveat(st, "mygroup")
	if got != "" {
		t.Errorf("outstandingCaveat = %q, want empty (nothing names mygroup)", got)
	}
}

// TestOutstandingCaveat_TrulyIdle_ReturnsEmpty pins the plain idle case: an
// entirely empty StatusReply.
func TestOutstandingCaveat_TrulyIdle_ReturnsEmpty(t *testing.T) {
	got := outstandingCaveat(proto.StatusReply{}, "mygroup")
	if got != "" {
		t.Errorf("outstandingCaveat = %q, want empty on a fully idle status", got)
	}
}

// TestOutstandingCaveat_GroupAlgoRunning_ReturnsCaveat pins direction 1 via
// the coarse GroupAlgoRunning list (split-mode's status snapshot source —
// see internal/daemon/service.go's snap.GroupAlgoRunning branch).
func TestOutstandingCaveat_GroupAlgoRunning_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{GroupAlgoRunning: []string{"mygroup"}}
	got := outstandingCaveat(st, "mygroup")
	if got == "" {
		t.Fatal("outstandingCaveat = \"\", want a non-empty caveat")
	}
	if !strings.Contains(got, "group-algo") {
		t.Errorf("caveat missing stage name %q: %q", "group-algo", got)
	}
	if !strings.Contains(got, outstandingOverlayCaveat) {
		t.Errorf("caveat must reuse `grafel status`'s exact overlay wording:\ngot:  %q\nwant substring: %q", got, outstandingOverlayCaveat)
	}
}

// TestOutstandingCaveat_PendingAlgo_ReturnsCaveat pins the PENDING (not yet
// running, debounce-armed) group-algo case, phrased distinctly from RUNNING.
func TestOutstandingCaveat_PendingAlgo_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{PendingAlgo: []string{"mygroup"}}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "group-algo (pending)") {
		t.Errorf("caveat = %q, want it to name group-algo as pending", got)
	}
}

// TestOutstandingCaveat_StageGateHolder_GroupAlgo_ReturnsCaveat pins the
// fine-grained stage-gate holder naming scheme ("group-algo:<group>", the
// exact string internal/daemon/sched/scheduler.go's group-algo pass acquires
// — see stageAcquire's "group-algo:" + group) — the source monolith-mode's
// status snapshot uses (the `g.Holder` branch in service.go).
func TestOutstandingCaveat_StageGateHolder_GroupAlgo_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{StageGateHolder: "group-algo:mygroup"}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "group-algo") {
		t.Errorf("caveat = %q, want it to name group-algo", got)
	}
}

// TestOutstandingCaveat_StageGateHolder_Links_ReturnsCaveat pins the "links"
// holder case — the cross-repo link pass that produces cross-repo edges/flows
// (the second stage #6047 names, distinct from group-algo).
func TestOutstandingCaveat_StageGateHolder_Links_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{StageGateHolder: "links:mygroup"}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "links") {
		t.Errorf("caveat = %q, want it to name links", got)
	}
}

// TestOutstandingCaveat_StageGateDeferred_Links_ReturnsCaveat pins the
// deferred (turned-away, waiting its turn behind the gate) links case.
func TestOutstandingCaveat_StageGateDeferred_Links_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{StageGateDeferred: []string{"links:mygroup"}}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "links (pending)") {
		t.Errorf("caveat = %q, want it to name links as pending", got)
	}
}

// TestOutstandingCaveat_PendingLinks_ReturnsCaveat pins the coarse
// PendingLinks list.
func TestOutstandingCaveat_PendingLinks_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{PendingLinks: []string{"mygroup"}}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "links (pending)") {
		t.Errorf("caveat = %q, want it to name links as pending", got)
	}
}

// TestOutstandingCaveat_BothStagesOutstanding_NamesBoth pins a real-world
// snapshot shaped like the issue's own reproduction: links holding the gate
// while group-algo is deferred behind it — both must be named.
func TestOutstandingCaveat_BothStagesOutstanding_NamesBoth(t *testing.T) {
	st := proto.StatusReply{
		StageGateHolder:   "links:mygroup",
		StageGateDeferred: []string{"group-algo:mygroup"},
	}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "group-algo (pending)") || !strings.Contains(got, "links") {
		t.Errorf("caveat = %q, want both group-algo (pending) and links named", got)
	}
}

// TestOutstandingCaveat_GroupAlgoInFlight_ReturnsCaveat pins the SPLIT-MODE
// signal (#6047 review round 2): GroupAlgoInFlight is the ONLY group-algo
// signal ever populated in split mode (the default) — GroupAlgoRunning /
// PendingAlgo / PendingLinks are structurally always empty there (see
// wizard_outstanding.go's top doc). Without consuming this field the wizard
// would silently report nothing outstanding in the exact mode/scenario #6047
// was measured in, whenever the stage-gate holder/deferred fields also
// happen to be empty (e.g. the debounce-armed window right after the ack).
func TestOutstandingCaveat_GroupAlgoInFlight_ReturnsCaveat(t *testing.T) {
	st := proto.StatusReply{GroupAlgoInFlight: 1}
	got := outstandingCaveat(st, "mygroup")
	if got == "" {
		t.Fatal("outstandingCaveat = \"\", want a non-empty caveat")
	}
	if !strings.Contains(got, "group-algo") {
		t.Errorf("caveat missing stage name %q: %q", "group-algo", got)
	}
	if !strings.Contains(got, outstandingOverlayCaveat) {
		t.Errorf("caveat must reuse `grafel status`'s exact overlay wording:\ngot:  %q\nwant substring: %q", got, outstandingOverlayCaveat)
	}
}

// TestOutstandingCaveat_GroupAlgoInFlightZero_NoCaveat pins the other
// direction for the same field: GroupAlgoInFlight==0 (the JSON zero value,
// indistinguishable from "field never populated") must NOT, by itself,
// produce a caveat.
func TestOutstandingCaveat_GroupAlgoInFlightZero_NoCaveat(t *testing.T) {
	got := outstandingCaveat(proto.StatusReply{GroupAlgoInFlight: 0}, "mygroup")
	if got != "" {
		t.Errorf("outstandingCaveat = %q, want empty when GroupAlgoInFlight==0", got)
	}
}

// TestOutstandingCaveat_AnalyticsBarging_ReturnsCaveat_NoOverlayClause pins
// the analytics stage (#6047 review round 2 — the issue's own reproduction
// showed `barging=analytics:<group>` and it was never checked). analytics is
// the post-rebuild quality-metrics history scan (cmd/grafel/rebuild_history.go),
// NOT part of the community/pagerank/centrality overlay — so when it is the
// ONLY outstanding stage, the caveat must name it WITHOUT claiming the
// overlay is not current (that claim would itself be dishonest).
func TestOutstandingCaveat_AnalyticsBarging_ReturnsCaveat_NoOverlayClause(t *testing.T) {
	st := proto.StatusReply{StageGateBarging: []string{"analytics:mygroup"}}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "analytics") {
		t.Errorf("caveat = %q, want it to name analytics", got)
	}
	if strings.Contains(got, "communities/pagerank/centrality") {
		t.Errorf("caveat = %q, must NOT claim the overlay is stale when analytics is the only outstanding stage", got)
	}
	if !strings.Contains(got, graphQueryableCaveat) {
		t.Errorf("caveat = %q, want it to still reuse the graph-queryable clause", got)
	}
}

// TestOutstandingCaveat_AnalyticsPlusGroupAlgo_NamesBothWithOverlayClause
// pins the combined case: when an overlay-affecting stage IS also
// outstanding alongside analytics, the overlay clause belongs in the caveat
// and analytics is named alongside it.
func TestOutstandingCaveat_AnalyticsPlusGroupAlgo_NamesBothWithOverlayClause(t *testing.T) {
	st := proto.StatusReply{
		StageGateBarging: []string{"analytics:mygroup"},
		GroupAlgoRunning: []string{"mygroup"},
	}
	got := outstandingCaveat(st, "mygroup")
	if !strings.Contains(got, "analytics") || !strings.Contains(got, "group-algo") {
		t.Errorf("caveat = %q, want both analytics and group-algo named", got)
	}
	if !strings.Contains(got, outstandingOverlayCaveat) {
		t.Errorf("caveat = %q, want the overlay clause present (group-algo is outstanding)", got)
	}
}

// TestAttachOutstanding_NilClient_ReturnsEmpty: attachOutstanding must never
// panic or block on a nil client (e.g. --no-index / daemon-down paths that
// never dial) — it degrades to "no caveat", same as an absent RSS reading.
func TestAttachOutstanding_NilClient_ReturnsEmpty(t *testing.T) {
	if got := attachOutstanding(nil, "mygroup"); got != "" {
		t.Errorf("attachOutstanding(nil, ...) = %q, want empty", got)
	}
}

// TestAttachOutstandingWith_QueriesAndAppliesCaveat is the pinning test for
// the attachOutstanding→outstandingCaveat BRIDGE (#6047 review round 2
// finding 3): a mutation that replaces attachOutstanding's body with "call
// the fetcher, discard the result, return ”" left the entire ./internal/cli
// suite green, because nothing exercised the bridge — only the nil-client
// short-circuit and the pure outstandingCaveat function were pinned
// separately. This drives attachOutstandingWith with a fake fetcher standing
// in for c.Status, proving the fetched StatusReply is actually threaded
// through to the returned caveat.
func TestAttachOutstandingWith_QueriesAndAppliesCaveat(t *testing.T) {
	fetch := func() (proto.StatusReply, error) {
		return proto.StatusReply{GroupAlgoRunning: []string{"mygroup"}}, nil
	}
	got := attachOutstandingWith(fetch, "mygroup")
	if got == "" {
		t.Fatal("attachOutstandingWith = \"\", want the fetched status's caveat")
	}
	if !strings.Contains(got, "group-algo") {
		t.Errorf("attachOutstandingWith = %q, want it to name group-algo", got)
	}
}

// TestAttachOutstandingWith_NothingOutstanding_ReturnsEmpty pins the other
// direction of the bridge: a fetched status with nothing outstanding must
// still flow through to an empty caveat (not a stale/fabricated one).
func TestAttachOutstandingWith_NothingOutstanding_ReturnsEmpty(t *testing.T) {
	fetch := func() (proto.StatusReply, error) { return proto.StatusReply{}, nil }
	if got := attachOutstandingWith(fetch, "mygroup"); got != "" {
		t.Errorf("attachOutstandingWith = %q, want empty", got)
	}
}

// TestAttachOutstandingWith_FetchError_ReturnsEmpty: an RPC error from the
// fetcher must degrade to "no caveat", never panic or propagate.
func TestAttachOutstandingWith_FetchError_ReturnsEmpty(t *testing.T) {
	fetch := func() (proto.StatusReply, error) { return proto.StatusReply{}, errors.New("dial: connection refused") }
	if got := attachOutstandingWith(fetch, "mygroup"); got != "" {
		t.Errorf("attachOutstandingWith = %q, want empty on fetch error", got)
	}
}

// TestAttachOutstandingWith_NilFetcher_ReturnsEmpty: a nil fetcher (mirrors
// attachOutstanding's nil-client short-circuit) must never panic.
func TestAttachOutstandingWith_NilFetcher_ReturnsEmpty(t *testing.T) {
	if got := attachOutstandingWith(nil, "mygroup"); got != "" {
		t.Errorf("attachOutstandingWith(nil, ...) = %q, want empty", got)
	}
}
