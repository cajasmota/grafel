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
// exits. Negative pid addresses the whole group the child leads. Fall back
// to the single-pid signal if the group kill fails (group already gone, or
// Setpgid didn't take for some reason) rather than silently doing nothing.
func signalTerminate(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err != nil {
		return p.Signal(syscall.SIGTERM)
	}
	return nil
}

// signalKill is the SIGKILL escalation used when the child does not exit
// within the drain window. Group-directed for the same reason as
// signalTerminate: a lone SIGKILL to the child pid can leave grandchildren
// alive.
func signalKill(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}
