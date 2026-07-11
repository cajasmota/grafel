//go:build windows

package daemon

import (
	"os"
	"syscall"
)

// engineChildSysProcAttr requests a new process group on Windows so the child
// is not sent console CTRL_C_EVENT alongside serve; the supervisor drives its
// lifecycle explicitly.
func engineChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// signalTerminate on Windows cannot deliver a POSIX SIGTERM to another process;
// os.Process.Signal only supports Kill/Interrupt and Interrupt is unsupported
// for non-console children. Kill is the reliable terminate here — the engine's
// defers do not run, but the supervised graph.fb writes are atomic (temp+rename)
// so a killed engine never leaves a torn graph.
func signalTerminate(p *os.Process) error {
	return p.Kill()
}
