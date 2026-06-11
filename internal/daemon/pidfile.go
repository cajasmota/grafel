package daemon

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/cajasmota/archigraph/internal/process"
)

// ErrAlreadyRunning is returned by AcquirePIDFile when another daemon
// process holds the pid file. The pid of the live owner is included in
// the wrapped error message so callers can surface it directly.
var ErrAlreadyRunning = errors.New("daemon already running")

// AcquirePIDFile writes the current pid to path, returning a release
// closure. If path already contains a pid for a running process, the
// call returns ErrAlreadyRunning. Stale pid files (the named process is
// gone) are overwritten silently — a crash should never wedge startup.
//
// We deliberately do NOT use flock here: the goal is to detect another
// daemon, and pid+syscall.Kill(pid,0) is portable across darwin/linux
// without a new dependency.
func AcquirePIDFile(path string) (release func(), err error) {
	if existing, ok := readPID(path); ok && pidIsLiveDaemon(existing) {
		return nil, fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, existing)
	}
	pid := os.Getpid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write pid file %s: %w", path, err)
	}
	return func() {
		// Best-effort cleanup. Errors here are not actionable — if we
		// can't remove our own pid file, the next startup will see a
		// stale entry and overwrite it.
		_ = os.Remove(path)
	}, nil
}

// ReadPIDFile returns the pid recorded in path, or 0 if the file is
// missing/empty/unreadable. Clients use this for `archigraph status`.
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
//  2. The live pid is actually an archigraph process (name match), which
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
	isArchigraph, err := process.PidIsArchigraph(pid)
	if err != nil {
		// Cannot determine the process name (unsupported platform or a
		// transient enumeration failure). The pid is alive, so honor it as
		// the owner — the same conservative behavior as before this fix.
		return true
	}
	return isArchigraph
}

// pidAlive returns true when a process with the given pid exists and
// the current user can signal it. signal 0 is the POSIX existence
// probe; on darwin and linux it does not deliver a signal but does
// validate the pid.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}
