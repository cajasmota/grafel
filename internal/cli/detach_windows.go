//go:build windows

package cli

import "syscall"

// detachSysProcAttr returns a SysProcAttr that detaches the daemon on
// Windows so it survives the launching shell/console.
//
// The `grafel start` command (and any direct daemon spawn) must produce a
// process that is NOT tied to the launching console window — otherwise closing
// that terminal takes the daemon (and the dashboard it serves) down with it.
// We therefore:
//
//   - DETACHED_PROCESS: the child does not inherit the parent's console; it has
//     no controlling console, so a closed terminal cannot signal it.
//   - CREATE_NEW_PROCESS_GROUP: the child is not in the launcher's process
//     group, so a Ctrl-C / Ctrl-Break to the launcher is not propagated.
//   - CREATE_NO_WINDOW: do not allocate a new console window for the daemon
//     (it is a background service, not an interactive program).
//   - HideWindow: belt-and-suspenders — if a window would otherwise appear,
//     start it hidden (SW_HIDE).
func detachSysProcAttr() *syscall.SysProcAttr {
	const (
		detachedProcess       = 0x00000008
		createNewProcessGroup = 0x00000200
		createNoWindow        = 0x08000000
	)
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup | createNoWindow,
	}
}
