package cli

import (
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

// TestAttachOutstanding_NilClient_ReturnsEmpty: attachOutstanding must never
// panic or block on a nil client (e.g. --no-index / daemon-down paths that
// never dial) — it degrades to "no caveat", same as an absent RSS reading.
func TestAttachOutstanding_NilClient_ReturnsEmpty(t *testing.T) {
	if got := attachOutstanding(nil, "mygroup"); got != "" {
		t.Errorf("attachOutstanding(nil, ...) = %q, want empty", got)
	}
}
