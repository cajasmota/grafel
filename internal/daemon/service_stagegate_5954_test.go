package daemon

// service_stagegate_5954_test.go — the heavy write-stage gate must be
// OBSERVABLE FROM OUTSIDE THE PROCESS, in both deployment modes.
//
// The gate's decisions previously existed only as entries in the scheduler's
// in-memory RecentLog ring. In SPLIT MODE — the default — the scheduler lives
// in the engine process and `grafel status` is answered by SERVE, whose
// s.scheduler is nil: the entire scheduler block of the reply, RecentLog
// included, was simply absent. So there was no way at all to confirm from
// outside that the gate had fired, and a measurement run was read as "the gate
// is dead" when it had merely never been reported.
//
// These tests pin BOTH routes to the same three fields: the in-process
// scheduler (monolith) and the engine-liveness status sidecar (split mode).

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// newStageGateTestService builds a bare Service with no scheduler attached.
func newStageGateTestService(t *testing.T) *Service {
	t.Helper()
	return newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(proto.RebuildArgs) ([]string, string, error) { return []string{}, "", nil },
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test-stagegate.sock",
		make(chan struct{}),
		nil, // logger
		2,   // maxConcurrentGroups
	)
}

// writeStageGateLivenessFixture stamps the engine-global liveness sidecar with
// gate state, via the SAME (DefaultLayout + EngineLivenessStatusKey) derivation
// the production writer and reader use.
func writeStageGateLivenessFixture(t *testing.T, f *statusfile.File) {
	t.Helper()
	layout, err := DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout: %v", err)
	}
	if err := statusfile.Write(EngineLivenessStatusKey(layout.Root), f); err != nil {
		t.Fatalf("write engine liveness fixture: %v", err)
	}
}

// TestStatus_StageGateFieldsFromInProcessScheduler: with a scheduler attached
// (monolith mode), a live foreground barge must appear in the RPC reply.
func TestStatus_StageGateFieldsFromInProcessScheduler(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)
	s := sched.New(sched.Config{Workers: 1, MemReleaseDisabled: true})
	s.Start()
	defer s.Stop()
	svc.scheduler = s

	release := sched.BargeForeground("rebuild:acme")
	defer release()

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if len(reply.StageGateBarging) != 1 || reply.StageGateBarging[0] != "rebuild:acme" {
		t.Fatalf("StageGateBarging = %v, want [rebuild:acme]: the gate's state must reach the status RPC, "+
			"not just the in-memory RecentLog ring", reply.StageGateBarging)
	}
}

// TestStatus_ForfeitedHolderFromInProcessScheduler pins the MONOLITH half of
// the live forfeit signal, driving a real forfeit through a real scheduler
// rather than asserting on a hand-built snapshot.
//
// The split-mode test below covers the sidecar route, but monolith mode reads a
// different line in Service.Status (snap.StageForfeitedHolder, not
// g.ForfeitedHolder). Deleting that line left the whole daemon suite green until
// this test existed — the two branches have to be pinned separately or one of
// them silently reports nothing.
func TestStatus_ForfeitedHolderFromInProcessScheduler(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	s := sched.New(sched.Config{
		Workers: 1,
		// An index chains to the LINK pass, and group-algo is chained off the
		// link pass's success path (#5349 A3) — so links must be allowed to run
		// or the group-algo pass this test needs is never scheduled at all.
		LinkDebounce:      time.Millisecond,
		GroupAlgoDebounce: time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    5 * time.Millisecond,
		StageGateMaxDefer: time.Hour,
		// Forfeit almost immediately, but never reach the grace: this test is
		// about the FORFEITED-AND-STILL-HELD state, which is the whole point of
		// a live signal the sticky counter cannot express.
		StageGateHoldMax:      10 * time.Millisecond,
		StageGateForfeitGrace: time.Hour,
		Index:                 func(context.Context, string, string) error { return nil },
		Links:                 func(context.Context, string) error { return nil },
		GroupAlgo: func(context.Context, string) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release // wedged past hold-max
			return nil
		},
		GroupsForRepo:      func(string) []string { return []string{"acme"} },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer func() { close(release); s.Stop() }()
	svc.scheduler = s

	// An index job schedules the group-algo pass, which takes the token and wedges.
	s.Enqueue("/repo-a")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("group-algo never started; cannot exercise the forfeit path")
	}

	// The forfeit is declared by the next gate DECISION, not by a timer, so poke
	// the admit loop into making one.
	deadline := time.Now().Add(10 * time.Second)
	var reply proto.StatusReply
	for {
		s.Enqueue("/repo-b")
		reply = proto.StatusReply{}
		if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
			t.Fatalf("Status RPC: %v", err)
		}
		// Require the WEDGED holder specifically. The instantaneous link pass
		// shares this gate and could in principle be the one caught by a reap,
		// which would satisfy a bare ForfeitedHolder check without exercising
		// what this test is about.
		if reply.StageGateForfeitedHolder && reply.StageGateHolder == "group-algo:acme" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("StageGateForfeitedHolder never became true for the wedged group-algo in monolith mode "+
				"(holder=%q forfeited=%v forfeits=%d): the live forfeit-grace signal does not reach the status RPC",
				reply.StageGateHolder, reply.StageGateForfeitedHolder, reply.StageGateForfeits)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reply.StageGateForfeits < 1 {
		t.Errorf("StageGateForfeits = %d, want >= 1 alongside the live signal", reply.StageGateForfeits)
	}
}

// TestStatus_StageGateFieldsFromEngineLivenessSidecarInSplitMode is the split-
// mode half, and the one that actually matters: serve has NO scheduler, so the
// only route is the engine's liveness sidecar. Without the fallback this reply
// is silently empty in the DEFAULT deployment mode.
func TestStatus_StageGateFieldsFromEngineLivenessSidecarInSplitMode(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)
	if svc.scheduler != nil {
		t.Fatal("precondition: this test models a serve process with no scheduler")
	}

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:              time.Now().UTC(),
		StageGateHolder:          "group-algo:acme",
		StageGateDeferred:        []string{"links:acme"},
		StageGateBarging:         []string{"rebuild:acme"},
		StageGateForfeits:        2,
		StageGateForfeitedHolder: true,
	})

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if reply.StageGateHolder != "group-algo:acme" {
		t.Errorf("StageGateHolder = %q, want %q", reply.StageGateHolder, "group-algo:acme")
	}
	if reply.StageGateForfeits != 2 {
		t.Errorf("StageGateForfeits = %d, want 2", reply.StageGateForfeits)
	}
	// The LIVE forfeit signal, which is the one an operator looking at a stalled
	// daemon actually needs: FORFEITS=n is sticky for the life of the daemon and
	// cannot answer "is a forfeit grace running right now". It crosses the
	// engine→serve boundary by exactly the same route as the rest, and in split
	// mode — the default — that route is the ONLY one.
	if !reply.StageGateForfeitedHolder {
		t.Errorf("StageGateForfeitedHolder = false, want true: the live 'inside a forfeit grace' signal " +
			"must survive the sidecar round trip, or it is invisible in the default deployment mode")
	}
	if len(reply.StageGateDeferred) != 1 || reply.StageGateDeferred[0] != "links:acme" {
		t.Errorf("StageGateDeferred = %v, want [links:acme]", reply.StageGateDeferred)
	}
	if len(reply.StageGateBarging) != 1 || reply.StageGateBarging[0] != "rebuild:acme" {
		t.Errorf("StageGateBarging = %v, want [rebuild:acme]: with no in-process scheduler this is the ONLY "+
			"route by which gate state reaches an operator, and split mode is the default", reply.StageGateBarging)
	}
}

// TestStatus_StageGateFieldsOmittedWhenSidecarStale: a stale heartbeat means
// the engine is down, starting, or wedged. Reporting its last-known holder as
// LIVE would be worse than reporting nothing — this surface exists precisely to
// adjudicate "is a heavy stage running right now", so a phantom holder would
// mislead exactly the reader it was built for.
func TestStatus_StageGateFieldsOmittedWhenSidecarStale(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:      time.Now().UTC().Add(-24 * time.Hour),
		StageGateHolder:  "group-algo:acme",
		StageGateBarging: []string{"rebuild:acme"},
	})

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if reply.StageGateHolder != "" || len(reply.StageGateBarging) != 0 {
		t.Fatalf("stale sidecar reported as live gate state: holder=%q barging=%v; want empty (unknown)",
			reply.StageGateHolder, reply.StageGateBarging)
	}
}

// TestEngineLivenessHeartbeat_PublishesStageGateState pins the WRITE half: the
// engine's heartbeat must actually stamp gate state onto the sidecar. Without
// it the reader above has nothing to read and the split-mode path is dead.
func TestEngineLivenessHeartbeat_PublishesStageGateState(t *testing.T) {
	root := t.TempDir()
	gate := sched.StageGateState{
		Holder:          "group-algo:acme",
		Deferred:        []string{"links:acme"},
		Barging:         []string{"rebuild:acme"},
		Forfeits:        2,
		ForfeitedHolder: true,
	}
	stop := startEngineLivenessHeartbeat(root, 0, nil, func() sched.StageGateState { return gate }, nil)
	stop() // writeOnce has already run synchronously before the ticker loop

	f, err := statusfile.Read(EngineLivenessStatusKey(root))
	if err != nil {
		t.Fatalf("read engine liveness sidecar: %v", err)
	}
	if f.StageGateHolder != gate.Holder {
		t.Errorf("StageGateHolder = %q, want %q", f.StageGateHolder, gate.Holder)
	}
	if len(f.StageGateDeferred) != 1 || f.StageGateDeferred[0] != "links:acme" {
		t.Errorf("StageGateDeferred = %v, want [links:acme]", f.StageGateDeferred)
	}
	if len(f.StageGateBarging) != 1 || f.StageGateBarging[0] != "rebuild:acme" {
		t.Errorf("StageGateBarging = %v, want [rebuild:acme]", f.StageGateBarging)
	}
	if f.StageGateForfeits != 2 {
		t.Errorf("StageGateForfeits = %d, want 2", f.StageGateForfeits)
	}
	// The heartbeat is the ONLY writer of the split-mode route, so a field the
	// reader handles but the writer never stamps is dead in the default
	// deployment mode while looking wired end to end.
	if !f.StageGateForfeitedHolder {
		t.Errorf("StageGateForfeitedHolder = false, want true: the heartbeat must publish the live " +
			"forfeit-grace signal, not only the sticky counter")
	}
}
