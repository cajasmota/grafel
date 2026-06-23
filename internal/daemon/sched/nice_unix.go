//go:build !windows

package sched

import (
	"os/exec"
	"syscall"
)

// groupAlgoNice is the positive nice increment applied to the group-algo
// subprocess so it runs at a lower OS scheduling priority than foreground work.
// +10 is a substantial-but-not-extreme demotion: the background analytics pass
// yields the CPU to a consumer's CI / dev harness instead of starving it (the
// v0.1.3 regression starved a user's Django test harness).
const groupAlgoNice = 10

// applyGroupAlgoNice arranges for the spawned child to start at a lower OS
// priority. On Unix we put the child in its own process group and the runtime
// applies the nice increment relative to the parent's priority via
// SysProcAttr. The actual setpriority(2) call is issued by the child itself at
// startup (see niceSelf), because Go's exec package does not expose a portable
// "spawn at nice N" knob; here we only ensure the child is independently
// schedulable. Kept as a hook so the spawn site stays platform-agnostic.
func applyGroupAlgoNice(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// NiceSelf lowers the CURRENT process's OS scheduling priority by
// groupAlgoNice. Called by the group-algo child at startup so it runs niced
// regardless of how it was spawned. Best-effort: a failure (e.g. lacking
// permission to renice) is silently ignored — the cap is the primary control.
func NiceSelf() {
	// PRIO_PROCESS=0, who=0 → the calling process. A positive value lowers
	// priority. Errors are ignored: renicing DOWN never requires privilege, but
	// guard anyway.
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, groupAlgoNice)
}
