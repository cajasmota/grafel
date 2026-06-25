package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

// mcp_bridge_singleton.go — single-bridge guarantee (#5633).
//
// Claude Code spawns one `grafel mcp-bridge` per session. A daemon restart (or
// a crashed/abandoned session) can leave a PRIOR bridge orphaned — reparented
// to init and still attached to the daemon socket — so two bridges race for the
// same daemon connection. This is the "two mcp-bridge processes at once"
// symptom in #5633.
//
// We guarantee exactly one bridge per daemon socket with a per-socket pidfile,
// mirroring the daemon's own AcquirePIDFile reap approach (internal/daemon/
// pidfile.go) and the watcher-reaping precedent (#5632 / internal/process):
//
//   - On startup the bridge reads the per-socket pidfile. If it names a LIVE
//     grafel process that is not us, that is an orphaned prior bridge for this
//     socket — we reap it (SIGTERM via process.Kill) and wait briefly for it to
//     exit before claiming ownership.
//   - We then write our own pid into the pidfile and return a release closure
//     that removes it on clean shutdown.
//
// A stale pidfile (dead pid, or a recycled pid that is not a grafel process) is
// overwritten silently — a crashed bridge must never wedge the next session.

// bridgeReapGrace bounds how long we wait for a reaped prior bridge to exit
// after SIGTERM before claiming the pidfile anyway. Kept short: the prior
// bridge is a thin stdio proxy with nothing to flush.
var bridgeReapGrace = 500 * time.Millisecond

// bridgeSingletonPath derives the per-socket bridge pidfile path. It lives
// beside the daemon socket so it shares the socket's per-user directory and
// lifecycle. The socket basename is hashed into the name so distinct sockets
// (e.g. a test override) never collide, without embedding a long path.
func bridgeSingletonPath(socketPath string) string {
	dir := filepath.Dir(socketPath)
	sum := sha256.Sum256([]byte(socketPath))
	name := "mcp-bridge-" + hex.EncodeToString(sum[:6]) + ".pid"
	return filepath.Join(dir, name)
}

// acquireBridgeSingleton ensures this is the only live bridge for socketPath.
// It reaps an orphaned prior bridge (if any) and records our pid. The returned
// release closure removes the pidfile and must be called on shutdown. Errors
// are non-fatal by design: a bridge that cannot write its pidfile should still
// serve (degrading to the pre-#5633 best-effort behavior), so callers log and
// continue rather than aborting the session.
func acquireBridgeSingleton(socketPath string, logf func(string, ...any)) (release func(), err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	path := bridgeSingletonPath(socketPath)
	self := os.Getpid()

	if prior, ok := readBridgePID(path); ok && prior != self {
		if isLiveBridge(prior) {
			logf("reaping orphaned prior mcp-bridge (pid %d) for socket %s", prior, socketPath)
			reapPriorBridge(prior)
		}
	}

	if werr := os.WriteFile(path, []byte(strconv.Itoa(self)+"\n"), 0o600); werr != nil {
		return func() {}, fmt.Errorf("write bridge pidfile %s: %w", path, werr)
	}
	return func() {
		// Only remove the pidfile if it still names us — a newer bridge that
		// reaped us may already own it.
		if cur, ok := readBridgePID(path); ok && cur == self {
			_ = os.Remove(path)
		}
	}, nil
}

// reapPriorBridge sends SIGTERM to a confirmed orphaned prior bridge and waits
// (bounded) for it to exit. Best-effort: a signal/exit failure is non-fatal —
// the new bridge claims the pidfile regardless so the session proceeds.
func reapPriorBridge(pid int) {
	_ = process.Kill(pid)
	_ = waitForExit(pid, bridgeReapGrace)
}

// isLiveBridge reports whether pid names a live grafel process (a prior
// bridge). PidIsGrafel defeats pid reuse: after a bridge dies its pid can be
// recycled by an unrelated program, and we must not SIGTERM that. On platforms
// where process enumeration is unavailable we fall back to a bare liveness
// probe, matching daemon.pidIsLiveDaemon's conservative behavior.
func isLiveBridge(pid int) bool {
	if !process.IsAlive(pid) {
		return false
	}
	isGrafel, err := process.PidIsGrafel(pid)
	if err != nil {
		// Cannot verify the name (unsupported platform / transient scan
		// failure). The pid is alive; honor it as a bridge to reap rather than
		// leave a possible duplicate. Reaping a live grafel pid is the safe
		// failure mode here.
		return true
	}
	return isGrafel
}

func readBridgePID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
