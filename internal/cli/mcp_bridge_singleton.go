package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/process"
)

// mcp_bridge_singleton.go — per-SESSION bridge ownership record.
//
// History. #5633 asked for "exactly one bridge per daemon socket" and this file
// implemented it by reaping (SIGTERM) whatever pid a per-SOCKET pidfile named.
// The socket path (internal/daemon.DefaultLayout) has no cwd, repo, worktree or
// session component: it is per-user, i.e. one pidfile for the whole machine. So
// every newly started bridge SIGTERMed the incumbent bridge of every OTHER live
// session — #6999, observed as a tool-agnostic `Transport closed`.
//
// #6999: the reap is GONE. This file no longer signals any process, ever.
//
// Why deleting it is safe rather than a regression of #5633:
//
//   - A bridge that is IDLE when its client goes collects itself. bridge.run
//     reads stdin and returns on io.EOF; when the client's end of the pipe is
//     closed the bridge falls out of its loop and nothing external is needed to
//     collect it. Verified with real processes on three shapes: client SIGKILL,
//     client SIGTERM, and a live client that closes the bridge's stdin.
//     TestBridge_ExitsWhenStdinCloses pins the EOF-while-idle case in process.
//
//     It is NOT true of every client-less bridge, and the comment must not say
//     so. A bridge BLOCKED in a daemon call is not in its read loop and never
//     observes the EOF: callDaemon → net/rpc Call has no deadline, so against a
//     daemon that accepts and never replies (wedged or swapping — on record for
//     this project) the bridge stays alive indefinitely after its client dies.
//     That residual is #7003. It is not an argument for the reap: the old reap
//     only collected such a bridge when a NEW bridge started, which is the
//     #6999 event itself, and it collected healthy concurrent bridges with it.
//   - The orphan #5633 saw therefore had a LIVE client (a daemon restart does
//     not touch stdin). Such a process is indistinguishable, from the outside,
//     from a second concurrent session's perfectly healthy bridge — there is no
//     key that unifies "an orphan and its replacement" while separating "two
//     concurrent sessions", because they look identical. Choosing to kill in
//     that ambiguity is exactly what produced #6999.
//   - The remedy for a bridge that has lost the DAEMON (the real #5633 case) is
//     owned by that bridge itself: reconnect (#6722) or exit — not a signal from
//     a stranger.
//
// What remains is an ownership RECORD: a per-session pidfile naming the bridge
// currently serving this socket for this session, plus a log line when a prior
// record for the same session is still live. It is diagnostic only. Its key
// includes a session identity so that concurrent sessions cannot overwrite each
// other's record; a session identity that collides (two bridges whose stdin is
// /dev/null, say) costs nothing beyond an overwritten record, because no signal
// is ever derived from it.

// EnvBridgeSession lets an MCP client name the session explicitly. When unset,
// bridgeSessionID falls back to the identity of the client's stdin pipe, then
// to this process's own pid (which is unique by construction).
const EnvBridgeSession = "GRAFEL_MCP_SESSION"

// bridgeSessionID returns an identifier for the client session this bridge
// serves. It is used only to scope the ownership record's filename; it is never
// a licence to signal another process.
func bridgeSessionID() string {
	if v := strings.TrimSpace(os.Getenv(EnvBridgeSession)); v != "" {
		return "env:" + v
	}
	if id, ok := stdinIdentity(); ok {
		return "stdin:" + id
	}
	return "pid:" + strconv.Itoa(os.Getpid())
}

// bridgeRecordDir returns the directory the ownership records live in:
// <Layout.Root>/sockets, on every platform.
//
// It is derived from the LAYOUT ROOT, not from filepath.Dir(SocketPath), and
// that is the whole point. SocketPath is not a filesystem path everywhere: on
// Windows it is a named pipe (`\\.\pipe\grafel-<hash>`, see
// internal/daemon/paths_windows.go, where SocketDir is deliberately ""), so
// filepath.Dir of it is `\\.\pipe` — not a directory anything can write into.
// The pre-#6999 code keyed off filepath.Dir(SocketPath) too, so on Windows the
// pidfile write ALWAYS failed and the record never existed at all: readBridgePID
// therefore never returned a prior pid and the reap never fired there. Windows
// users were never hit by #6999, and they also never got the diagnostic record
// this file exists to leave behind. Layout.Root is a real directory on both
// platforms, so deriving from it fixes the diagnostic everywhere.
//
// On unix this keeps the record in the same <root>/sockets it already used
// under GRAFEL_DAEMON_ROOT; it also pins it there when the socket itself lives
// in XDG_RUNTIME_DIR, which is a per-boot tmpfs rather than grafel's own state.
// sockets/ is already one of grafel's scratch dirs (internal/install:
// grafelScratchDirs), so uninstall --purge still cleans these up.
func bridgeRecordDir() (string, error) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return "", fmt.Errorf("resolve grafel layout for bridge record: %w", err)
	}
	if strings.TrimSpace(layout.Root) == "" {
		return "", fmt.Errorf("grafel layout has no root; cannot place bridge record")
	}
	return filepath.Join(layout.Root, "sockets"), nil
}

// bridgeSingletonPath derives the per-session bridge pidfile path inside dir.
// Both the socket path AND the session id are hashed into the name: keying on
// the socket alone is #6999, because one file then names the single bridge of
// an entire machine. The socket stays in the hash so two daemons (two roots,
// two sockets) never share a record even if a session id repeats.
func bridgeSingletonPath(dir, socketPath, sessionID string) string {
	sum := sha256.Sum256([]byte(socketPath + "\x00" + sessionID))
	name := "mcp-bridge-" + hex.EncodeToString(sum[:6]) + ".pid"
	return filepath.Join(dir, name)
}

// acquireBridgeSingleton records this process as the bridge serving socketPath
// for this session and returns the pidfile path plus a release closure that
// removes it on clean shutdown.
//
// It sends no signal to any process. If a prior record for THIS session still
// names a live grafel process, that is logged and left alone: it is either an
// orphan that will exit when its stdin closes, or a bridge that is still
// serving somebody.
//
// Errors are non-fatal by design: a bridge that cannot write its record should
// still serve, so callers log and continue rather than aborting the session.
func acquireBridgeSingleton(recordDir, socketPath string, logf func(string, ...any)) (release func(), pidfile string, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	path := bridgeSingletonPath(recordDir, socketPath, bridgeSessionID())
	self := os.Getpid()

	// The record dir is grafel's own scratch dir, and nothing else guarantees
	// it exists: on Windows no socket is ever bound inside it.
	if merr := os.MkdirAll(recordDir, 0o700); merr != nil {
		return func() {}, path, fmt.Errorf("create bridge record dir %s: %w", recordDir, merr)
	}

	if prior, ok := readBridgePID(path); ok && prior != self && isLiveBridge(prior) {
		// #6999: log, never signal. A prior bridge is not ours to terminate.
		logf("mcp-bridge: prior bridge (pid %d) for this session is still live; "+
			"leaving it alone — grafel never signals another bridge (#6999). pidfile %s",
			prior, path)
	}

	if werr := os.WriteFile(path, []byte(strconv.Itoa(self)+"\n"), 0o600); werr != nil {
		return func() {}, path, fmt.Errorf("write bridge pidfile %s: %w", path, werr)
	}
	return func() {
		// Only remove the record if it still names us — a newer bridge for the
		// same session may already own it.
		if cur, ok := readBridgePID(path); ok && cur == self {
			_ = os.Remove(path)
		}
	}, path, nil
}

// isLiveBridge reports whether pid names a live grafel process. It gates a log
// line only.
//
// When the executable behind pid cannot be verified we answer FALSE. The
// pre-#6999 code answered true ("reaping a live grafel pid is the safe failure
// mode"), which was the opposite of safe: PidIsGrafel matches ANY grafel
// process by basename, so a recycled pid belonging to the daemon or the engine
// satisfied it, and the answer fed a SIGTERM. Nothing signals on this answer
// any more, and it still must not assert what it cannot verify.
func isLiveBridge(pid int) bool {
	if !process.IsAlive(pid) {
		return false
	}
	isGrafel, err := pidIsGrafel(pid)
	if err != nil {
		return false
	}
	return isGrafel
}

// pidIsGrafel is a seam over process.PidIsGrafel so the unverifiable-executable
// branch of isLiveBridge is reachable from a test.
var pidIsGrafel = process.PidIsGrafel

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
