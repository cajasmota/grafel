package daemon

import (
	"context"
	"log/slog"
	"time"
)

// defaultParentWatchInterval is the poll interval the engine's parent-death
// watchdog uses in production. A short interval keeps the "orphan window"
// (the time between the parent dying and the engine self-terminating) small
// without meaningfully taxing the engine — this is a single getppid() call
// per tick, effectively free.
const defaultParentWatchInterval = time.Second

// startParentDeathWatchdog is the PRIMARY layer of the orphan-engine
// hardening (ADR-0024, epic #5729): it detects when the engine's original
// serve parent has died UNCLEANLY (SIGKILL / crash / OOM / `launchctl
// kickstart -k`) — the case where serve's own graceful drain never had a
// chance to SIGTERM the engine child — and self-terminates the engine so it
// never lingers as a reparented orphan writing the store alongside a freshly
// spawned replacement engine.
//
// Design: it polls getppid() (the caller-supplied observer, so RunEngine can
// pass a real os.Getppid-backed function and tests can inject a fake one)
// every interval and compares the result against originalParent — the pid
// recorded ONCE at engine startup via os.Getppid(). The FIRST poll that
// returns a value different from originalParent means this process has been
// reparented: on Unix/macOS an orphaned child is adopted by init (ppid
// becomes 1), so any divergence from the recorded original is a reliable,
// portable signal that the original parent is gone — deliberately compared
// against the ORIGINAL parent pid rather than hardcoded to ==1, since the
// adopting pid can vary by platform/container (e.g. a pid-namespaced init
// that isn't 1).
//
// On divergence, onParentDeath is invoked exactly once (typically the
// engine's own context-cancel func, triggering the SAME graceful shutdown
// path a SIGTERM would: the deferred engine-plane shutdown unwinds the
// scheduler/watcher and removes engine.pid) and the watchdog goroutine
// exits.
//
// The watchdog also exits cleanly — WITHOUT ever calling onParentDeath — when
// ctx is cancelled (normal engine shutdown: ctx.Done() or a real SIGTERM).
// The returned done channel closes when the goroutine has exited, so callers
// (and tests) can assert there is no goroutine leak.
func startParentDeathWatchdog(ctx context.Context, originalParent int, getppid func() int, interval time.Duration, onParentDeath func(), logger *slog.Logger) (done <-chan struct{}) {
	doneCh := make(chan struct{})
	if interval <= 0 {
		interval = defaultParentWatchInterval
	}
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if cur := getppid(); cur != originalParent {
					if logger != nil {
						logger.Warn("engine: reparented — original parent is gone, self-terminating to avoid an orphaned engine",
							"original_parent", originalParent, "current_parent", cur)
					}
					onParentDeath()
					return
				}
			}
		}
	}()
	return doneCh
}
