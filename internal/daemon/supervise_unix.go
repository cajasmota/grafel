//go:build darwin || linux

package daemon

import (
	"os"
	"syscall"
)

// engineChildSysProcAttr puts the engine child in its OWN process group
// (Setpgid) so a terminal signal to serve's foreground group does not race
// serve's own graceful drain of the child, and so the supervisor is the single
// authority over the child's lifecycle (mirrors the detach pattern in
// internal/cli/detach_unix.go). It remains a child process, so cmd.Wait reaps
// it normally.
func engineChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// signalTerminate asks the engine child to shut down gracefully (SIGTERM). Its
// RunEngine signal handler catches this and unwinds the scheduler/watcher.
//
// Group-directed (issue #6044, following the #5999 precedent in
// internal/daemon/sched/nice_unix.go applyProcessGroupCancel): the engine
// child owns its own process group (Setpgid, engineChildSysProcAttr above).
// A single-pid signal — the trap exec.CommandContext's default Cancel falls
// into — only reaches the child itself, not any grandchildren it forks (its
// own subprocess fanout), leaving them running as orphans once the child
// exits. See groupOrSingleSignal for the pid-reuse guard this needs that the
// #5999 precedent did not (different call shape — see its doc comment).
func signalTerminate(p *os.Process) error {
	return groupOrSingleSignal(p, syscall.SIGTERM)
}

// signalKill is the SIGKILL escalation used when the child does not exit
// within the drain window. Group-directed for the same reason as
// signalTerminate: a lone SIGKILL to the child pid can leave grandchildren
// alive. This is the branch where the pid-reuse race in groupOrSingleSignal
// actually matters in production — see its doc comment.
func signalKill(p *os.Process) error {
	return groupOrSingleSignal(p, syscall.SIGKILL)
}

// groupOrSingleSignal delivers sig to the process group p leads (kill(-pid)),
// but only after confirming p.Pid is STILL that group's leader (Getpgid(pid)
// == pid) at the moment of signalling. If it is not — or the check itself
// fails (ESRCH: the pid is already gone) — it falls back to a plain
// single-pid signal instead of attempting the group kill at all.
//
// Why the check matters (issue #6044 review item 5): terminateChild's SIGKILL
// escalation (supervise.go) races cmd.Wait() — a separate supervisor-owned
// goroutine reaps the child concurrently with the drain timer, and `select`
// picks whichever case is ready with no ordering guarantee between them. If
// Wait wins that race, the child's pid is freed back to the OS *before* this
// signal fires; on a busy system pids recycle within milliseconds. A blind
// kill(-pid) at that point would signal the process group led by whatever
// unrelated process the OS handed that pid to next — a BIGGER blast radius
// than the single-pid kill it replaces, not a smaller one.
//
// This is precisely the caveat that is present, but only implicit, in the
// #5999 precedent this follows (internal/daemon/sched/nice_unix.go
// applyProcessGroupCancel — its own doc comment names the pid-reuse hazard
// explicitly). That call site is safe from reuse ONLY because os/exec's
// Cancel hook runs on the watchdog goroutine strictly BEFORE Wait returns and
// frees the pid — an ordering guarantee this supervisor's separate-goroutine
// drain race does not have. Getpgid closes that gap: it fails once the pid is
// freed, and even in the freed-then-reused window, a stale pid coincidentally
// being reported as its own group's leader is a materially narrower hazard
// than signalling an arbitrary recycled pid outright.
func groupOrSingleSignal(p *os.Process, sig syscall.Signal) error {
	if pgid, err := syscall.Getpgid(p.Pid); err == nil && pgid == p.Pid {
		if err := syscall.Kill(-p.Pid, sig); err == nil {
			return nil
		}
	}
	return p.Signal(sig)
}
