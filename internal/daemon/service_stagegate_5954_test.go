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
		HeartbeatAt:       time.Now().UTC(),
		StageGateHolder:   "group-algo:acme",
		StageGateDeferred: []string{"links:acme"},
		StageGateBarging:  []string{"rebuild:acme"},
	})

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if reply.StageGateHolder != "group-algo:acme" {
		t.Errorf("StageGateHolder = %q, want %q", reply.StageGateHolder, "group-algo:acme")
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
		Holder:   "group-algo:acme",
		Deferred: []string{"links:acme"},
		Barging:  []string{"rebuild:acme"},
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
}
