package daemon

// status_rebuild_inflight_6014_test.go — #6014.
//
// `grafel status` reported rebuild_in_flight / in_flight as a STRUCTURAL ZERO
// in split mode (the default since ADR-0024). Two independent defects produced
// it, and both are pinned here:
//
//  1. THE WORK MOVED PROCESSES. Service.Rebuild's rebuildInFlight /
//     groupsActiveCount increments sit BELOW the `if SplitModeEnabled()` early
//     return, on the monolith path. In split mode the rebuild runs in the
//     ENGINE process, so serve has no local counter that could ever be
//     non-zero. The only honest fix is cross-process: the engine publishes its
//     live rebuild count onto the liveness sidecar it already heartbeats, and
//     serve reads it — exactly the route #6002 established for
//     GroupAlgoInFlight and #5954 for the stage gate. Serve must NOT invent a
//     number of its own; a wrong non-zero is no better than a wrong zero.
//
//     Getting the number into the RPC reply is necessary but NOT sufficient:
//     `grafel status` gated its `rebuild:` line on RSSBudgetMB, which only an
//     in-process scheduler populates, so in split mode the line never printed
//     regardless. That gate is pinned separately, in internal/cli's
//     status_daemon_detail_6014_test.go — nothing in THIS file would notice it
//     regressing.
//
//  2. THE RPC ITSELF WAS UNCOUNTED. Service.Rebuild's `s.inFlight` increment
//     also sat below the split-mode branch, so a WaitForCompletion rebuild —
//     which BLOCKS the handler for the whole rebuild — was invisible in
//     StatusReply.InFlight, the "in_flight=" field `grafel status` prints.
//     That one is purely serve-local and needs no sidecar: the RPC is in
//     flight in serve, in serve's own memory, for as long as it blocks.

import (
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestStatus_RebuildInFlightFromEngineSidecarInSplitMode is the READ half: a
// serve process with no scheduler must surface the engine's live rebuild count.
// Without it, "is a rebuild running right now?" is answered `0` in the default
// deployment mode no matter what the engine is doing.
func TestStatus_RebuildInFlightFromEngineSidecarInSplitMode(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)
	if svc.scheduler != nil {
		t.Fatal("precondition: this test models a serve process with no scheduler")
	}

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:           time.Now().UTC(),
		EngineRebuildInFlight: 2,
	})

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if reply.RebuildInFlight != 2 {
		t.Errorf("RebuildInFlight = %d, want 2: in split mode the engine's liveness sidecar is the ONLY "+
			"route by which a running rebuild can reach `grafel status`", reply.RebuildInFlight)
	}
	if reply.RebuildGroupsActive != 2 {
		t.Errorf("RebuildGroupsActive = %d, want 2: the engine's guard is per-group, so its in-flight "+
			"count IS the number of groups actively rebuilding", reply.RebuildGroupsActive)
	}
}

// TestStatus_RebuildInFlightOmittedWhenSidecarStale: a stale heartbeat means the
// engine is down, starting, or wedged. Reporting its last-known rebuild count as
// LIVE would be a fabricated non-zero — the precise failure mode #6014 warns
// against, and worse than reporting nothing.
func TestStatus_RebuildInFlightOmittedWhenSidecarStale(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	svc := newStageGateTestService(t)

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:           time.Now().UTC().Add(-24 * time.Hour),
		EngineRebuildInFlight: 3,
	})

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	if reply.RebuildInFlight != 0 || reply.RebuildGroupsActive != 0 {
		t.Fatalf("stale sidecar reported as a live rebuild: in_flight=%d groups=%d; want 0 (unknown)",
			reply.RebuildInFlight, reply.RebuildGroupsActive)
	}
}

// TestRebuildInFlightFromStatusFile_FreshVsStale binds the READER'S OWN
// freshness guard directly.
//
// The Status-RPC test above cannot do it: the split-mode branch it exercises is
// itself entered via `StageGateFromStatusFile() ok`, which already encodes the
// same freshness rule — so deleting RebuildInFlightFromStatusFile's guard leaves
// that test green while the function is, on its own terms, wrong. Any other
// caller (and there will be one) would then read a dead engine's last-known
// count as live. Pinning the accessor directly is what makes the guard real
// rather than decorative.
func TestRebuildInFlightFromStatusFile_FreshVsStale(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:           time.Now().UTC(),
		EngineRebuildInFlight: 4,
	})
	if got := RebuildInFlightFromStatusFile(); got != 4 {
		t.Errorf("fresh sidecar: RebuildInFlightFromStatusFile = %d, want 4", got)
	}

	writeStageGateLivenessFixture(t, &statusfile.File{
		HeartbeatAt:           time.Now().UTC().Add(-24 * time.Hour),
		EngineRebuildInFlight: 4,
	})
	if got := RebuildInFlightFromStatusFile(); got != 0 {
		t.Errorf("stale sidecar: RebuildInFlightFromStatusFile = %d, want 0 — a dead engine's last-known "+
			"count reported as live is a fabricated non-zero, no better than the wrong zero it replaces", got)
	}
}

// TestEngineLivenessHeartbeat_PublishesRebuildInFlight is the WRITE half. A
// field the reader handles but the heartbeat never stamps is dead in the default
// deployment mode while looking fully wired.
func TestEngineLivenessHeartbeat_PublishesRebuildInFlight(t *testing.T) {
	root := t.TempDir()

	engineRebuildInFlight.Add(2)
	defer engineRebuildInFlight.Add(-2)

	stop := startEngineLivenessHeartbeat(root, 0, nil, nil, nil)
	stop() // writeOnce runs synchronously before the ticker loop

	f, err := statusfile.Read(EngineLivenessStatusKey(root))
	if err != nil {
		t.Fatalf("read engine liveness sidecar: %v", err)
	}
	if f.EngineRebuildInFlight != 2 {
		t.Errorf("EngineRebuildInFlight = %d, want 2: the heartbeat is the ONLY writer of the split-mode route",
			f.EngineRebuildInFlight)
	}
}

// TestRebuildWorker_TracksInFlightCount pins the SOURCE of the published number
// to real work: the counter must rise while rebuildFn is genuinely executing and
// return to zero once it returns. This is what makes the reported value derived
// rather than plausible.
func TestRebuildWorker_TracksInFlightCount(t *testing.T) {
	if got := EngineRebuildInFlightCount(); got != 0 {
		t.Fatalf("precondition: EngineRebuildInFlightCount = %d, want 0", got)
	}
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	w := newRebuildWorker(func(proto.RebuildArgs) ([]string, string, error) {
		once.Do(func() { close(entered) })
		<-release
		return nil, "", nil
	}, nil)

	dir := requestsDirForGroup("acme")
	payload := []byte(`{"group":"acme"}`)
	if _, err := requests.Write(dir, requests.Record{Kind: requests.KindRebuild, Payload: payload}); err != nil {
		t.Fatalf("queue rebuild request: %v", err)
	}

	w.submit(root, dir, "acme")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("rebuildFn never ran")
	}
	if got := EngineRebuildInFlightCount(); got != 1 {
		t.Errorf("EngineRebuildInFlightCount = %d while a rebuild is genuinely executing, want 1: "+
			"the published number must be derived from real work, not fabricated", got)
	}
	close(release)
	w.waitIdle()
	if got := EngineRebuildInFlightCount(); got != 0 {
		t.Errorf("EngineRebuildInFlightCount = %d after the rebuild returned, want 0 (leaked counter)", got)
	}
}

// TestStatus_InFlightCountsBlockingSplitModeRebuildRPC: the WaitForCompletion
// rebuild blocks the serve handler for the entire rebuild, yet
// StatusReply.InFlight — the `in_flight=` field `grafel status` prints —
// reported 0 for the whole time, because the increment sat below the split-mode
// branch. This is serve-local truth about a serve-local RPC; no sidecar needed.
func TestStatus_InFlightCountsBlockingSplitModeRebuildRPC(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	// Keep the completion wait honest but fast: the engine is "alive" and we
	// poll frequently, so the handler blocks until we write the terminal ack.
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return true }))
	t.Cleanup(setRebuildWaitIntervalForTest(time.Millisecond))

	svc := newService(nil, func(proto.RebuildArgs) ([]string, string, error) {
		t.Error("split mode must never call s.rebuild in serve")
		return nil, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)

	done := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		done <- svc.Rebuild(&proto.RebuildArgs{Group: "acme", WaitForCompletion: true}, &reply)
	}()

	dir := requestsDirForGroup("acme")
	// Wait for the handler to be genuinely blocked in the completion wait: the
	// request file is on disk and no ack has been written.
	var id string
	deadline := time.Now().Add(10 * time.Second)
	for {
		recs, err := requests.ListPending(dir)
		if err == nil && len(recs) == 1 {
			id = recs[0].ID
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rebuild request never landed on disk (err=%v)", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	var reply proto.StatusReply
	if err := svc.Status(&proto.StatusArgs{}, &reply); err != nil {
		t.Fatalf("Status RPC: %v", err)
	}
	inFlight := reply.InFlight

	// Release the blocked handler with a terminal OK ack.
	if err := requests.WriteAck(dir, id, requests.Ack{Status: requests.StatusOK}); err != nil {
		t.Fatalf("write terminal ack: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Rebuild: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Rebuild never returned after the terminal ack")
	}

	if inFlight < 1 {
		t.Errorf("StatusReply.InFlight = %d while a WaitForCompletion rebuild RPC was BLOCKED in this "+
			"process, want >= 1: `in_flight=` is serve's own RPC count and the RPC was demonstrably in "+
			"flight", inFlight)
	}
}
