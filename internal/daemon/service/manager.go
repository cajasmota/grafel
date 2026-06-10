package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ServiceManager is the platform-agnostic seam over an OS service backend
// (launchd on macOS, systemd --user on Linux, Task Scheduler on Windows).
//
// The orchestration logic in this file (EnsureLoaded / Restart / Teardown /
// WaitReady) is written ONLY against this interface so it can be unit-tested
// with a fake backend, without invoking real launchctl / systemctl / schtasks.
// Each platform supplies a concrete implementation via newServiceManager.
//
// Implementations MUST be idempotent-by-construction:
//   - IsLoaded never errors just because the service is absent; it reports false.
//   - Unload treats a not-loaded service as success (no error).
//   - Load treats an already-loaded service by converging (callers Unload first).
//   - Probe reports whether the daemon endpoint (unix socket / named pipe / TCP)
//     is currently connectable.
type ServiceManager interface {
	// WriteUnit renders and writes the plist / unit / task-XML to disk and
	// ensures supporting directories (log dir, agent dir) exist. It does not
	// load the service. Idempotent: overwrites any existing file.
	WriteUnit() error

	// IsLoaded reports whether the OS service manager currently has the
	// service registered/loaded. It must NOT error for the not-loaded case —
	// it returns (false, nil). A non-nil error means the query itself failed.
	IsLoaded() (bool, error)

	// Unload removes the service from the OS service manager (launchctl
	// bootout / systemctl stop+disable / schtasks /end). It MUST treat a
	// not-loaded service as success (return nil) so it is safe to call
	// unconditionally before Load.
	Unload() error

	// Load registers + starts the service (launchctl bootstrap / systemctl
	// enable --now / schtasks /create+/run). Callers guarantee the service is
	// not currently loaded (EnsureLoaded calls Unload first), so Load need not
	// itself handle the already-loaded case.
	Load() error

	// RemoveArtifacts deletes the on-disk unit/plist/task-XML file(s). Missing
	// files are not an error. Used by Teardown after Unload.
	RemoveArtifacts() error

	// Probe reports whether the daemon endpoint is connectable right now.
	// Used by WaitReady's poll loop. It must be cheap and non-blocking beyond
	// a short dial timeout.
	Probe() bool

	// Status returns the current StatusInfo (installed / running / pid).
	Status() (StatusInfo, error)
}

// readinessConfig controls the socket-readiness poll loop. These replace the
// former hard 5 s cliff that false-failed cold starts on large stores
// (issue #4458): the daemon legitimately takes >5 s to open its socket.
type readinessConfig struct {
	// budget is the total time WaitReady waits for the endpoint to become
	// connectable before giving up.
	budget time.Duration
	// interval is the gap between connection probes.
	interval time.Duration
}

// defaultReadiness is the production poll configuration. 60 s budget covers a
// cold start on a large store; 250 ms interval keeps the loop responsive while
// not busy-spinning.
var defaultReadiness = readinessConfig{
	budget:   60 * time.Second,
	interval: 250 * time.Millisecond,
}

// errNotReady is returned by waitReady when the budget is exhausted without the
// endpoint becoming connectable.
var errNotReady = errors.New("daemon endpoint did not become ready within budget")

// progressFn receives human-readable progress lines during a poll. nil is a
// no-op. Kept tiny so the orchestration stays testable.
type progressFn func(string)

func (p progressFn) emit(format string, args ...any) {
	if p != nil {
		p(fmt.Sprintf(format, args...))
	}
}

// waitReady polls probe until it returns true or the budget is exhausted.
// It is context-cancellable and platform-agnostic: probe abstracts the unix
// socket / named-pipe / TCP dial. This is the long-term fix for the 5 s
// readiness cliff (#4458) — failure is only reported after the FULL budget.
func waitReady(ctx context.Context, probe func() bool, cfg readinessConfig, onProgress progressFn) error {
	if cfg.budget <= 0 {
		cfg.budget = defaultReadiness.budget
	}
	if cfg.interval <= 0 {
		cfg.interval = defaultReadiness.interval
	}

	// Fast path: already connectable.
	if probe() {
		return nil
	}

	deadline := time.Now().Add(cfg.budget)
	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	lastProgress := time.Now()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for daemon endpoint: %w", ctx.Err())
		case <-ticker.C:
			if probe() {
				return nil
			}
			if now := time.Now(); now.After(deadline) {
				return fmt.Errorf("%w (%s)", errNotReady, cfg.budget)
			} else if now.Sub(lastProgress) >= 5*time.Second {
				remaining := time.Until(deadline).Round(time.Second)
				onProgress.emit("  waiting for daemon socket… (%s remaining)", remaining)
				lastProgress = now
			}
		}
	}
}

// ensureLoaded converges the OS service to the loaded+ready state in an
// idempotent, race-free way (issue #4458):
//
//  1. WriteUnit (write/overwrite the plist/unit/task file).
//  2. Unconditionally Unload first — this clears an already-loaded service so
//     the subsequent Load cannot fail with launchctl err 5 ("Bootstrap
//     failed: 5: Input/output error"). Unload treats not-loaded as success,
//     so this is safe whether or not the service was previously loaded. This
//     deterministic bootout-before-bootstrap ordering replaces the previous
//     racy "IsLoaded?-then-maybe-bootout" detection that produced both err 5
//     (loaded but bootstrap-ed again) and "Boot-out failed: 3: No such
//     process" (bootout-ed when not loaded).
//  3. Load (bootstrap / enable --now / create+run).
//  4. WaitReady — poll the endpoint up to the full budget before declaring
//     failure, instead of a 5 s cliff.
//
// It never fails because the service already exists or doesn't exist; it
// converges to the desired state.
func ensureLoaded(ctx context.Context, sm ServiceManager, cfg readinessConfig, onProgress progressFn) (StatusInfo, error) {
	if err := sm.WriteUnit(); err != nil {
		return StatusInfo{}, fmt.Errorf("write service unit: %w", err)
	}

	// Deterministic clear-then-load. Unload is idempotent (not-loaded == ok).
	if err := sm.Unload(); err != nil {
		return StatusInfo{}, fmt.Errorf("clear existing service: %w", err)
	}

	if err := sm.Load(); err != nil {
		return StatusInfo{}, fmt.Errorf("load service: %w", err)
	}

	if err := waitReady(ctx, sm.Probe, cfg, onProgress); err != nil {
		st, _ := sm.Status()
		st.Installed = true
		return st, fmt.Errorf("service loaded but socket not ready: %w", err)
	}

	return sm.Status()
}

// restart converges an already-installed service back to a healthy loaded
// state. It is identical to ensureLoaded — unload-then-load is exactly a
// restart — and is provided as a named entry point for callers (update /
// reinstall) that semantically mean "restart".
func restart(ctx context.Context, sm ServiceManager, cfg readinessConfig, onProgress progressFn) (StatusInfo, error) {
	return ensureLoaded(ctx, sm, cfg, onProgress)
}

// teardown idempotently removes the service: Unload (treating not-loaded as
// success) then RemoveArtifacts (treating missing files as success). It never
// fails because the service is already gone — the desired post-state is
// "absent", and an already-absent service satisfies it. This backs a
// non-interactive, scriptable uninstall (issue #4462).
func teardown(sm ServiceManager) error {
	if err := sm.Unload(); err != nil {
		return fmt.Errorf("unload service: %w", err)
	}
	if err := sm.RemoveArtifacts(); err != nil {
		return fmt.Errorf("remove service artifacts: %w", err)
	}
	return nil
}
