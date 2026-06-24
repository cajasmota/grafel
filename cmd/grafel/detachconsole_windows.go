//go:build windows

package main

import "syscall"

// detachConsole drops the daemon's association with any console window it
// inherited from the launching process.
//
// Why this matters: on Windows, a console application that is started as a child
// of an interactive shell (e.g. `grafel install` opening a second terminal, or a
// Task Scheduler action running with an InteractiveToken) shares that console.
// When the user closes the terminal window, Windows delivers CTRL_CLOSE_EVENT to
// every process attached to the console and then terminates them — taking the
// daemon (and the dashboard it serves) down with the window.
//
// See #5594. FreeConsole detaches the calling process from its console. After this call the
// daemon has no controlling console, so closing the launching terminal can no
// longer signal or kill it. The daemon logs to its log file (not the console),
// so losing the console has no functional downside in service mode.
//
// Errors are intentionally ignored: if the process has no console to begin with
// (the desired end state — e.g. spawned with DETACHED_PROCESS/CREATE_NO_WINDOW),
// FreeConsole returns an error that is simply a no-op for our purposes.
func detachConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	freeConsole := kernel32.NewProc("FreeConsole")
	_, _, _ = freeConsole.Call()
}
