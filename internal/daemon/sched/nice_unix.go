//go:build !windows

package sched

import (
	"os"
	"os/exec"
	"strconv"
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
//
// It also wires cmd.Cancel so cancellation reaches that whole process group —
// see applyProcessGroupCancel. The two belong together: the moment a child gets
// its own group, the default single-pid kill stops covering everything the
// child spawned, so no caller of this hook can opt into one without the other.
func applyGroupAlgoNice(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	applyProcessGroupCancel(cmd)
}

// applyProcessGroupCancel makes ctx cancellation SIGKILL the child's entire
// process GROUP instead of its pid alone (#5999).
//
// exec.CommandContext's default Cancel is cmd.Process.Kill(): one pid, one
// SIGKILL. Because Setpgid above already gave the child its own group (pgid ==
// the child's pid), every process the child forks lands in that same group, and
// the default kill leaves all of them running. Two consequences, both real:
// the index child fans out `grafel extract` subprocesses, so a cancelled pass
// leaked live grandchildren; and those grandchildren inherit the child's
// stdout/stderr pipes, so the runner's drain-to-EOF loop never finishes and the
// supervising call never returns — with the daemon's exclusive heavy-stage
// token still held.
//
// Must be called only on a cmd that carries Setpgid. kill(-pid) addresses the
// group whose PGID equals that pid — NOT the caller's own group — so without
// Setpgid the child leads no group, the kill returns ESRCH, and the fallback
// below quietly papers over it: the group kill would never actually bind. (The
// residual hazard is narrower but real: after pid reuse, that pid could have
// become the leader of an unrelated group, and the signal would land there.)
// That is why the only caller is applyGroupAlgoNice, which sets Setpgid on the
// same cmd — the pairing must not be split.
//
// Setting Cancel on a cmd built without a context is harmless: os/exec only
// consults it when a context is present.
func applyProcessGroupCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Negative pid == "the process group led by that pid".
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			// The group may already be gone, or Setpgid may not have taken;
			// fall back to the single-pid kill we are replacing.
			return cmd.Process.Kill()
		}
		return nil
	}
}

// NiceSelf lowers the CURRENT process's OS scheduling priority by
// groupAlgoNice. Best-effort: a failure (e.g. lacking permission to renice) is
// silently ignored — the cap is the primary control.
//
// Prefer NiceSelfUnlessForeground at a child's startup: an unconditional
// NiceSelf is what put a user-blocking rebuild's group-algo pass below the
// user's editor.
func NiceSelf() {
	// PRIO_PROCESS=0, who=0 → the calling process. A positive value lowers
	// priority. Errors are ignored: renicing DOWN never requires privilege, but
	// guard anyway.
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, groupAlgoNice)
}

// niceIsSupported reports whether this platform demotes priority at all.
func niceIsSupported() bool { return true }

// itoaPriority renders the calling process's current nice value, for tests that
// need the OS's answer rather than a function's return value.
func itoaPriority() string {
	n, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		return "err"
	}
	// Darwin/Linux return the raw nice value from Getpriority via the syscall
	// package (no PZERO bias), so this is directly comparable to groupAlgoNice.
	return strconv.Itoa(n)
}
