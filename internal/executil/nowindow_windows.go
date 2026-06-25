//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// NoWindow sets CREATE_NO_WINDOW on cmd so that child processes spawned by the
// daemon never flash a visible console window. This is required on Windows
// because exec.Command inherits the parent's console by default — when the
// daemon runs as a Task Scheduler task (no console) Windows allocates a new
// console window for each child, producing the "flashing terminal" effect.
func NoWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
