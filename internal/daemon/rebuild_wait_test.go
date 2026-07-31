package daemon

// rebuild_wait_test.go — serve-side completion-wait for a split-mode Rebuild
// (#5790, advances epic #5729). These assert the WaitForCompletion contract:
// in split mode Service.Rebuild(WaitForCompletion=true) blocks until the engine
// drains+acks OUR enqueued KindRebuild request, and returns an error on
// ack-timeout / engine-death — while WaitForCompletion=false keeps today's
// fire-and-forget fast path. Monolith mode ignores the flag (already
// synchronous) so the CLI gets identical "err==nil means done" in both modes.

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// waitForEnqueuedRebuild blocks until exactly one KindRebuild request is pending
// under group's requests dir and returns it (the fake engine's cue to drain it).
func waitForEnqueuedRebuild(t *testing.T, group string) requests.Record {
	t.Helper()
	dir := requestsDirForGroup(group)
	deadline := time.Now().Add(2 * time.Second)
	for {
		recs, err := requests.ListPending(dir)
		if err == nil && len(recs) == 1 {
			return recs[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("rebuild request for %q was never enqueued", group)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// withSplitWaitTest enables split mode under a temp root and shrinks the wait
// knobs so the completion loop resolves in milliseconds, restoring them after.
func withSplitWaitTest(t *testing.T) {
	t.Helper()
	t.Setenv(SplitModeEnvVar, "1")
	t.Setenv(EnvRoot, t.TempDir())

	t.Cleanup(setRebuildWaitIntervalForTest(3 * time.Millisecond))
	t.Cleanup(setRebuildWaitStartupWindowForTest(50 * time.Millisecond))
	t.Cleanup(setRebuildWaitTimeoutForTest(3 * time.Second))
}

func newWaitService(t *testing.T) *Service {
	t.Helper()
	return newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		t.Errorf("split mode must NOT call s.rebuild in-serve, but it did for %q", args.Group)
		return nil, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)
}

// TestRebuild_SplitMode_WaitForCompletion_BlocksUntilAck: the RPC must NOT
// return while our request is still queued, and must return (nil) once the
// engine drains+acks it — exercising the REAL requests ack machinery.
func TestRebuild_SplitMode_WaitForCompletion_BlocksUntilAck(t *testing.T) {
	withSplitWaitTest(t)
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return true }))
	const group = "g"
	svc := newWaitService(t)

	returned := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		returned <- svc.Rebuild(&proto.RebuildArgs{Group: group, WaitForCompletion: true}, &reply)
	}()

	// While the request is pending (engine hasn't acked), Rebuild must block.
	time.Sleep(40 * time.Millisecond)
	select {
	case err := <-returned:
		t.Fatalf("Rebuild returned before the engine acked (fire-and-forget not gated): err=%v", err)
	default:
	}

	// Fake engine: drain the pending request via the REAL bounded consumer with
	// keepAck=true (as the drain does for a WaitForCompletion rebuild), so the
	// StatusOK terminal ack is left on disk for the waiter to read.
	dir := requestsDirForGroup(group)
	rec := waitForEnqueuedRebuild(t, group)
	if _, err := requests.ApplyAndAckBounded(dir, rec, maxRebuildAttempts, true, func(requests.Record) error { return nil }); err != nil {
		t.Fatalf("ApplyAndAckBounded: %v", err)
	}

	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Rebuild after OK ack: want nil, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Rebuild did not return after the engine acked")
	}
	if pending, _ := RebuildRequestPending(group, ""); pending {
		t.Fatal("request dir should be drained after ack")
	}
}

// TestRebuild_SplitMode_WaitForCompletion_FailedRebuildReturnsError is the
// honesty regression guard (#5790): a rebuild whose apply ERRORS acks with
// StatusError — Rebuild(WaitForCompletion) must return that error, never nil, so
// `indexed:true` is impossible off a failed rebuild.
func TestRebuild_SplitMode_WaitForCompletion_FailedRebuildReturnsError(t *testing.T) {
	withSplitWaitTest(t)
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return true }))
	const group = "g"
	svc := newWaitService(t)

	returned := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		returned <- svc.Rebuild(&proto.RebuildArgs{Group: group, WaitForCompletion: true}, &reply)
	}()

	dir := requestsDirForGroup(group)
	rec := waitForEnqueuedRebuild(t, group)
	if _, err := requests.ApplyAndAckBounded(dir, rec, maxRebuildAttempts, true, func(requests.Record) error {
		return fmt.Errorf("boom: OOM-reaped mid-rebuild")
	}); err != nil {
		t.Fatalf("ApplyAndAckBounded: %v", err)
	}

	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("a FAILED rebuild must NOT be reported as success (#5790 honesty gap)")
		}
		if !strings.Contains(err.Error(), "group rebuild failed") {
			t.Fatalf("err = %q, want a 'group rebuild failed' message", err.Error())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Rebuild did not return after the engine acked an error")
	}
}

// TestRebuild_SplitMode_WaitForCompletion_DeadLetterReturnsError: a rebuild
// dead-lettered after exhausting the attempt budget acks with StatusError — the
// waiter must surface it as an error, not a false success.
func TestRebuild_SplitMode_WaitForCompletion_DeadLetterReturnsError(t *testing.T) {
	withSplitWaitTest(t)
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return true }))
	const group = "g"
	svc := newWaitService(t)

	returned := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		returned <- svc.Rebuild(&proto.RebuildArgs{Group: group, WaitForCompletion: true}, &reply)
	}()

	dir := requestsDirForGroup(group)
	rec := waitForEnqueuedRebuild(t, group)
	// Attempt budget already exhausted → ApplyAndAckBounded takes the dead-letter
	// branch: StatusError ack, kept (keepAck=true).
	rec.Attempts = maxRebuildAttempts
	if _, err := requests.ApplyAndAckBounded(dir, rec, maxRebuildAttempts, true, func(requests.Record) error { return nil }); err != nil {
		t.Fatalf("ApplyAndAckBounded (dead-letter): %v", err)
	}

	select {
	case err := <-returned:
		if err == nil || !strings.Contains(err.Error(), "group rebuild failed") {
			t.Fatalf("dead-lettered rebuild must be surfaced as an error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Rebuild did not return after the dead-letter ack")
	}
}

// TestRebuild_SplitMode_WaitForCompletion_AckTimeout: engine is alive but never
// acks — Rebuild must return a clear timeout error, not hang forever.
func TestRebuild_SplitMode_WaitForCompletion_AckTimeout(t *testing.T) {
	withSplitWaitTest(t)
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return true }))
	t.Cleanup(setRebuildWaitTimeoutForTest(80 * time.Millisecond))
	svc := newWaitService(t)

	var reply proto.RebuildReply
	err := svc.Rebuild(&proto.RebuildArgs{Group: "g", WaitForCompletion: true}, &reply)
	if err == nil {
		t.Fatal("want a timeout error when the engine never acks, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q, want a timeout message", err.Error())
	}
}

// TestRebuild_SplitMode_WaitForCompletion_EngineDeath: the engine was live then
// stopped responding before acking — Rebuild must fail fast with a clear error.
func TestRebuild_SplitMode_WaitForCompletion_EngineDeath(t *testing.T) {
	withSplitWaitTest(t)
	var polls int32
	// alive once, then dead
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return atomic.AddInt32(&polls, 1) <= 1 }))
	// high, so death (not timeout) is what fires
	t.Cleanup(setRebuildWaitTimeoutForTest(3 * time.Second))
	svc := newWaitService(t)

	var reply proto.RebuildReply
	err := svc.Rebuild(&proto.RebuildArgs{Group: "g", WaitForCompletion: true}, &reply)
	if err == nil {
		t.Fatal("want an engine-death error, got nil")
	}
	if !strings.Contains(err.Error(), "stopped responding") {
		t.Fatalf("error = %q, want an engine-death message", err.Error())
	}
}

// TestRebuild_SplitMode_FireAndForget_ReturnsImmediately: WaitForCompletion=false
// preserves today's behavior — enqueue and return at once, never blocking on the
// engine (proved by a dead engine that a wait would have errored on).
func TestRebuild_SplitMode_FireAndForget_ReturnsImmediately(t *testing.T) {
	withSplitWaitTest(t)
	t.Cleanup(setRebuildEngineAliveForTest(func() bool { return false }))
	svc := newWaitService(t)

	done := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		done <- svc.Rebuild(&proto.RebuildArgs{Group: "g"}, &reply)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fire-and-forget Rebuild: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("fire-and-forget Rebuild did not return immediately")
	}
	recs, _ := requests.ListPending(requestsDirForGroup("g"))
	if len(recs) != 1 {
		t.Fatalf("fire-and-forget must still enqueue exactly 1 request, got %d", len(recs))
	}
}

// TestRebuild_MonolithMode_WaitForCompletion_Synchronous: parity — in monolith
// mode WaitForCompletion is a no-op; Rebuild runs the rebuild in-process and
// returns the real reply with err==nil, writing no request file.
func TestRebuild_MonolithMode_WaitForCompletion_Synchronous(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "0")
	t.Setenv(EnvRoot, t.TempDir())

	ran := make(chan struct{}, 1)
	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		ran <- struct{}{}
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "g", WaitForCompletion: true}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("monolith mode must run the rebuild synchronously in-process")
	}
	if len(reply.Repos) != 1 || reply.Repos[0] != "/some/repo" {
		t.Fatalf("want synchronous reply carrying the rebuilt repos, got %+v", reply)
	}
	recs, _ := requests.ListPending(requestsDirForGroup("g"))
	if len(recs) != 0 {
		t.Fatalf("monolith mode must not write a request file, got %d", len(recs))
	}
}
