package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeManager is an in-memory ServiceManager used to unit-test the
// platform-agnostic orchestration (ensureLoaded / restart / teardown /
// waitReady) WITHOUT touching real launchctl / systemctl / schtasks.
//
// It records the call order so tests can assert invariants like
// "Unload happens before Load" (the bootout-before-bootstrap fix for #4458).
type fakeManager struct {
	mu sync.Mutex

	loaded       bool
	calls        []string
	loadErr      error
	unloadErr    error
	writeUnitErr error

	// probeReadyAfter: Probe returns false until this many probe calls have
	// happened, then true. Simulates a slow cold start (>5 s) that the old
	// cliff false-failed.
	probeReadyAfter int
	probeCalls      int

	// neverReady forces Probe to always return false (budget-exhaustion case).
	neverReady bool
}

func (f *fakeManager) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func (f *fakeManager) WriteUnit() error {
	f.record("WriteUnit")
	return f.writeUnitErr
}

func (f *fakeManager) IsLoaded() (bool, error) {
	f.record("IsLoaded")
	return f.loaded, nil
}

func (f *fakeManager) Unload() error {
	f.record("Unload")
	if f.unloadErr != nil {
		return f.unloadErr
	}
	f.loaded = false
	return nil
}

func (f *fakeManager) Load() error {
	f.record("Load")
	if f.loadErr != nil {
		return f.loadErr
	}
	f.loaded = true
	return nil
}

func (f *fakeManager) RemoveArtifacts() error {
	f.record("RemoveArtifacts")
	return nil
}

func (f *fakeManager) Probe() bool {
	f.mu.Lock()
	f.probeCalls++
	n := f.probeCalls
	f.mu.Unlock()
	if f.neverReady {
		return false
	}
	return n > f.probeReadyAfter
}

func (f *fakeManager) Status() (StatusInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return StatusInfo{Installed: true, Running: f.loaded}, nil
}

func (f *fakeManager) callOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// fastReadiness is a tight poll config so tests don't take real seconds.
var fastReadiness = readinessConfig{budget: 2 * time.Second, interval: 5 * time.Millisecond}

// --- ordering / idempotency -------------------------------------------------

// TestEnsureLoaded_BootoutBeforeBootstrap asserts the core #4458 invariant:
// Unload (bootout) ALWAYS runs before Load (bootstrap), even when the service
// is already loaded. This is what prevents launchctl err 5.
func TestEnsureLoaded_BootoutBeforeBootstrap(t *testing.T) {
	f := &fakeManager{loaded: true} // already loaded
	if _, err := ensureLoaded(context.Background(), f, fastReadiness, nil); err != nil {
		t.Fatalf("ensureLoaded: %v", err)
	}
	order := f.callOrder()
	unloadIdx, loadIdx := indexOf(order, "Unload"), indexOf(order, "Load")
	if unloadIdx < 0 || loadIdx < 0 {
		t.Fatalf("expected both Unload and Load to be called, got %v", order)
	}
	if unloadIdx > loadIdx {
		t.Errorf("Unload must precede Load; got order %v", order)
	}
	writeIdx := indexOf(order, "WriteUnit")
	if writeIdx > unloadIdx {
		t.Errorf("WriteUnit must precede Unload; got order %v", order)
	}
}

// TestEnsureLoaded_NotLoaded converges even when the service starts absent —
// Unload (treated as success on not-loaded) then Load.
func TestEnsureLoaded_NotLoaded(t *testing.T) {
	f := &fakeManager{loaded: false}
	st, err := ensureLoaded(context.Background(), f, fastReadiness, nil)
	if err != nil {
		t.Fatalf("ensureLoaded: %v", err)
	}
	if !st.Running {
		t.Errorf("expected Running after load, got %+v", st)
	}
	if indexOf(f.callOrder(), "Load") < 0 {
		t.Errorf("expected Load to be called, got %v", f.callOrder())
	}
}

// TestEnsureLoaded_UnloadNotLoadedTreatedAsSuccess: an Unload that reports
// success on a not-loaded service must not abort the install.
func TestEnsureLoaded_UnloadNotLoadedTreatedAsSuccess(t *testing.T) {
	f := &fakeManager{loaded: false, unloadErr: nil}
	if _, err := ensureLoaded(context.Background(), f, fastReadiness, nil); err != nil {
		t.Fatalf("ensureLoaded should tolerate not-loaded Unload: %v", err)
	}
}

// --- readiness polling (the 5 s cliff fix) ---------------------------------

// TestWaitReady_SucceedsPastFiveSeconds: the daemon comes up only after a
// delay that would have tripped the old 5 s cliff; the poll loop must keep
// waiting and ultimately succeed.
func TestWaitReady_SucceedsPastFiveSeconds(t *testing.T) {
	// With a 5 ms interval, probeReadyAfter=20 => ready at ~100ms of polling,
	// but the assertion below proves the loop tolerates an arbitrary number of
	// not-ready probes rather than a single check. Use a larger value with a
	// short interval so wall time stays small while exceeding the 1-probe cliff.
	f := &fakeManager{probeReadyAfter: 30}
	cfg := readinessConfig{budget: 2 * time.Second, interval: 2 * time.Millisecond}
	start := time.Now()
	if err := waitReady(context.Background(), f.Probe, cfg, nil); err != nil {
		t.Fatalf("waitReady should succeed after slow start: %v", err)
	}
	if f.probeCalls < 30 {
		t.Errorf("expected the loop to poll past the cliff (>30 probes), got %d", f.probeCalls)
	}
	if time.Since(start) > cfg.budget {
		t.Errorf("waitReady took longer than budget")
	}
}

// TestWaitReady_FastPath: already-connectable returns immediately.
func TestWaitReady_FastPath(t *testing.T) {
	f := &fakeManager{probeReadyAfter: 0} // first probe is true
	if err := waitReady(context.Background(), f.Probe, fastReadiness, nil); err != nil {
		t.Fatalf("waitReady fast path: %v", err)
	}
	if f.probeCalls != 1 {
		t.Errorf("expected exactly 1 probe on fast path, got %d", f.probeCalls)
	}
}

// TestWaitReady_FailsOnlyAfterBudget: never-ready endpoint fails, and only
// after the full budget has elapsed (not at an early cliff).
func TestWaitReady_FailsOnlyAfterBudget(t *testing.T) {
	f := &fakeManager{neverReady: true}
	cfg := readinessConfig{budget: 200 * time.Millisecond, interval: 10 * time.Millisecond}
	start := time.Now()
	err := waitReady(context.Background(), f.Probe, cfg, nil)
	elapsed := time.Since(start)
	if !errors.Is(err, errNotReady) {
		t.Fatalf("expected errNotReady, got %v", err)
	}
	if elapsed < cfg.budget {
		t.Errorf("waitReady gave up early after %s (budget %s) — that is the cliff bug", elapsed, cfg.budget)
	}
}

// TestWaitReady_ContextCancel: cancelling the context aborts the poll promptly.
func TestWaitReady_ContextCancel(t *testing.T) {
	f := &fakeManager{neverReady: true}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	cfg := readinessConfig{budget: 10 * time.Second, interval: 5 * time.Millisecond}
	start := time.Now()
	err := waitReady(ctx, f.Probe, cfg, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("context cancel did not abort promptly")
	}
}

// TestEnsureLoaded_SlowSocketStillSucceeds: end-to-end through ensureLoaded
// with a slow socket — proves install no longer false-fails the cold start.
func TestEnsureLoaded_SlowSocketStillSucceeds(t *testing.T) {
	f := &fakeManager{probeReadyAfter: 25}
	cfg := readinessConfig{budget: 2 * time.Second, interval: 2 * time.Millisecond}
	st, err := ensureLoaded(context.Background(), f, cfg, nil)
	if err != nil {
		t.Fatalf("ensureLoaded with slow socket: %v", err)
	}
	if !st.Running {
		t.Errorf("expected Running, got %+v", st)
	}
}

// TestEnsureLoaded_NeverReadyReportsInstalled: when the socket never comes up,
// ensureLoaded returns an error BUT reports Installed=true so the caller knows
// the service was loaded (avoids a misleading "not installed" rollback).
func TestEnsureLoaded_NeverReadyReportsInstalled(t *testing.T) {
	f := &fakeManager{neverReady: true}
	cfg := readinessConfig{budget: 100 * time.Millisecond, interval: 10 * time.Millisecond}
	st, err := ensureLoaded(context.Background(), f, cfg, nil)
	if err == nil {
		t.Fatal("expected error when socket never ready")
	}
	if !st.Installed {
		t.Errorf("expected Installed=true on socket-not-ready, got %+v", st)
	}
}

// TestEnsureLoaded_LoadErrorPropagates: a genuine Load failure aborts.
func TestEnsureLoaded_LoadErrorPropagates(t *testing.T) {
	f := &fakeManager{loadErr: errors.New("bootstrap blew up")}
	if _, err := ensureLoaded(context.Background(), f, fastReadiness, nil); err == nil {
		t.Fatal("expected Load error to propagate")
	}
}

// --- restart ---------------------------------------------------------------

// TestRestart_IsUnloadThenLoad: restart of an already-loaded service performs
// the same clear-then-load convergence.
func TestRestart_IsUnloadThenLoad(t *testing.T) {
	f := &fakeManager{loaded: true}
	if _, err := restart(context.Background(), f, fastReadiness, nil); err != nil {
		t.Fatalf("restart: %v", err)
	}
	order := f.callOrder()
	if indexOf(order, "Unload") > indexOf(order, "Load") {
		t.Errorf("restart must Unload before Load; got %v", order)
	}
}

// --- teardown (uninstall) --------------------------------------------------

// TestTeardown_LoadedService: unload then remove artifacts, in that order.
func TestTeardown_LoadedService(t *testing.T) {
	f := &fakeManager{loaded: true}
	if err := teardown(f); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	order := f.callOrder()
	ui, ri := indexOf(order, "Unload"), indexOf(order, "RemoveArtifacts")
	if ui < 0 || ri < 0 || ui > ri {
		t.Errorf("teardown must Unload before RemoveArtifacts; got %v", order)
	}
	if f.loaded {
		t.Errorf("service should be unloaded after teardown")
	}
}

// TestTeardown_NotLoadedIsIdempotent: tearing down an absent service succeeds
// (the non-interactive uninstall must never fail because nothing is installed).
func TestTeardown_NotLoadedIsIdempotent(t *testing.T) {
	f := &fakeManager{loaded: false}
	if err := teardown(f); err != nil {
		t.Fatalf("teardown of absent service should succeed: %v", err)
	}
	// Second teardown is still a no-op success.
	if err := teardown(f); err != nil {
		t.Fatalf("repeated teardown should be idempotent: %v", err)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
