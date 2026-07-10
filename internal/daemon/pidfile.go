package daemon

import (
	"errors"
	"fmt"
	"net/rpc/jsonrpc"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/process"
)

// ErrAlreadyRunning is returned by AcquirePIDFile when another daemon
// process holds the pid file. The pid of the live owner is included in
// the wrapped error message so callers can surface it directly.
var ErrAlreadyRunning = errors.New("daemon already running")

// socketHealthProbeTimeout bounds a single dial+Ping attempt when deciding
// whether a live, correctly-named pidfile owner is actually serving (#5710).
// Kept short — this runs synchronously on the `grafel start` path — but long
// enough that a momentarily-busy daemon (e.g. mid-GC) is not misjudged.
const socketHealthProbeTimeout = 300 * time.Millisecond

// socketHealthProbeRetries is the number of retries (beyond the first
// attempt) before a live grafel pid whose socket won't answer Ping is
// treated as reclaimable. Guards against a false-positive reclaim of a
// healthy daemon that is merely slow to respond to one probe.
const socketHealthProbeRetries = 2

// socketHealthProbe reports whether the daemon behind socketPath answers a
// Ping RPC. It is a package variable (rather than a hardcoded call) so tests
// can substitute a fake without standing up a real daemon + listener for
// every pidfile-reclaim case; production code uses dialAndPing.
var socketHealthProbe = dialAndPing

// dialAndPing is the real health probe: dial the daemon's IPC transport and
// issue a single Ping RPC, both bounded by timeout. transport/proto are leaf
// packages that do not import package daemon (nor does this function import
// internal/daemon/client, which itself imports package daemon — importing
// client here would create an import cycle; see #5710).
func dialAndPing(socketPath string, timeout time.Duration) bool {
	conn, err := transport.DialTimeout(socketPath, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	rpcClient := jsonrpc.NewClient(conn)
	defer rpcClient.Close()
	var reply proto.PingReply
	err = rpcClient.Call(proto.ServiceName+".Ping", proto.PingArgs{}, &reply)
	return err == nil
}

// socketIsHealthy retries socketHealthProbe up to socketHealthProbeRetries
// times (with a short pause between attempts) before declaring the socket
// unhealthy. A single failed probe is not enough to condemn a live daemon —
// only sustained unreachability across every attempt does.
func socketIsHealthy(socketPath string) bool {
	for attempt := 0; attempt <= socketHealthProbeRetries; attempt++ {
		if socketHealthProbe(socketPath, socketHealthProbeTimeout) {
			return true
		}
		if attempt < socketHealthProbeRetries {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return false
}

// AcquirePIDFile writes the current pid to pidPath, returning a release
// closure. If pidPath already contains a pid for a running grafel process,
// AcquirePIDFile probes that daemon's socketPath with a Ping RPC:
//
//   - If the daemon answers, it is genuinely alive and serving — the call
//     returns ErrAlreadyRunning, as before.
//   - If the daemon does NOT answer within socketHealthProbeRetries+1
//     attempts, it is treated as wedged (#5710 — e.g. blocked forever in
//     graceful shutdown behind a stalled RPC) rather than "running": the old
//     process is force-killed and the pidfile is reclaimed so `grafel start`
//     is not permanently refused by a daemon that can never again serve a
//     request.
//
// Stale pid files (the named process is gone entirely) are overwritten
// silently, as before — a crash should never wedge startup.
//
// We deliberately do NOT use flock here: the goal is to detect another
// daemon, and pid+syscall.Kill(pid,0) is portable across darwin/linux
// without a new dependency.
func AcquirePIDFile(pidPath, socketPath string) (release func(), err error) {
	if existing, ok := readPID(pidPath); ok && pidIsLiveDaemonFunc(existing) {
		if socketIsHealthy(socketPath) {
			return nil, fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, existing)
		}
		// The pid is alive and is a grafel process, but its socket will not
		// answer a Ping within the bounded retry window — the daemon is wedged
		// (e.g. stuck in graceful shutdown behind a stalled Rebuild RPC, #5710)
		// and can never again serve a request. Reclaim rather than refuse.
		_ = forceKillFunc(existing)
	}
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write pid file %s: %w", pidPath, err)
	}
	return func() {
		// Best-effort cleanup. Errors here are not actionable — if we
		// can't remove our own pid file, the next startup will see a
		// stale entry and overwrite it.
		_ = os.Remove(pidPath)
	}, nil
}

// ReadPIDFile returns the pid recorded in path, or 0 if the file is
// missing/empty/unreadable. Clients use this for `grafel status`.
func ReadPIDFile(path string) int {
	pid, _ := readPID(path)
	return pid
}

func readPID(path string) (int, bool) {
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

// pidIsLiveDaemon reports whether the pid recorded in a pidfile names a
// process we should honor as "the daemon is already running".
//
// This is the issue #4549 fix for the stale-pidfile false-positive: after a
// daemon dies (SIGKILL, crash, or a defer that never ran), its pidfile still
// names the dead pid. A bare kill(pid,0) liveness probe then returns true the
// moment that pid is RECYCLED by any unrelated process, so `start`/`restart`
// wrongly reports "daemon already running (pid X)" and bails, leaving NO
// daemon. We therefore require two conditions:
//
//  1. The pid is alive (kill(pid,0) succeeds), AND
//  2. The live pid is actually an grafel process (name match), which
//     defeats pid reuse.
//
// On platforms where process enumeration is unavailable (process.ErrUnsupported,
// currently Windows), we cannot verify the name, so we fall back to the bare
// liveness probe to preserve prior behavior rather than wrongly declaring a
// live owner stale. A transient scan error is treated the same way (fail
// safe toward "honor the live pid").
func pidIsLiveDaemon(pid int) bool {
	if !pidAlive(pid) {
		return false
	}
	isGrafel, err := process.PidIsGrafel(pid)
	if err != nil {
		// Cannot determine the process name (unsupported platform or a
		// transient enumeration failure). The pid is alive, so honor it as
		// the owner — the same conservative behavior as before this fix.
		return true
	}
	return isGrafel
}

// pidIsLiveDaemonFunc indirects pidIsLiveDaemon so tests can simulate "the
// pidfile names a live grafel process" without spawning a real grafel
// binary (process.PidIsGrafel matches on executable name, which a unit test
// binary can never satisfy). Production code always uses pidIsLiveDaemon.
var pidIsLiveDaemonFunc = pidIsLiveDaemon

// forceKillFunc indirects process.ForceKill so the #5710 pidfile-reclaim
// tests can observe/stub the kill without actually depending on
// process.ForceKill's platform-specific signal delivery semantics.
var forceKillFunc = process.ForceKill

// pidAlive returns true when a process with the given pid exists. The
// platform-specific liveness probe lives in internal/process: signal 0
// on unix, OpenProcess + GetExitCodeProcess on windows (where the unix
// probe always reports the wrong answer).
func pidAlive(pid int) bool {
	return process.IsAlive(pid)
}
