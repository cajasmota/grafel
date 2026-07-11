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
func signalTerminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
