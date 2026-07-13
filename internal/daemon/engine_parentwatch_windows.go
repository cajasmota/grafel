//go:build windows

package daemon

import "os"

// parentWatchGetppid returns the parent-pid observer the engine's
// parent-death watchdog polls on Windows.
//
// IMPORTANT Windows caveat: unlike Unix, Windows has no orphan-reparent-to-
// init semantics — when a parent process dies, a still-running child's
// recorded parent pid is NOT reassigned to a live adopting process. Win32
// process-creation APIs stamp the parent pid at CreateProcess time and never
// update it; os.Getppid() on Windows will therefore typically keep returning
// the ORIGINAL (now-dead, and possibly already recycled to an unrelated
// process) parent pid even after that parent is gone. This means the
// getppid-divergence signal this watchdog relies on rarely — if ever — fires
// on Windows.
//
// This is a documented, INTENTIONAL best-effort for v0.1.8 (ADR-0024, epic
// #5729): we still wire the same portable watchdog rather than special-
// casing Windows out entirely (it is harmless and free — one Getppid() call
// per tick — and self-corrects if a future Go runtime or platform change
// makes Getppid observe reparenting). The SECONDARY layer — the
// serve-startup reap in supervise.go (reapStaleEngine) — is what actually
// guards Windows: it detects and SIGTERMs (TerminateProcess) any lingering
// engine.pid-recorded process before serve spawns a new engine child, which
// bounds orphan accumulation to "at most one extra engine between serve
// restarts" even though the parent-death watchdog itself is not reliable
// here.
func parentWatchGetppid() func() int {
	return os.Getppid
}
