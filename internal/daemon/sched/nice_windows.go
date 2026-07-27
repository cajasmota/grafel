//go:build windows

package sched

import "os/exec"

// On Windows there is no setpriority(2)/nice. The CPU cap (GOMAXPROCS) is the
// portable control; OS-priority demotion is a no-op here. (A BELOW_NORMAL
// priority class could be set via CREATE_* flags, but the cap already bounds
// the draw and we avoid platform-specific spawn surgery.)

const groupAlgoNice = 0

// applyGroupAlgoNice is a no-op on Windows.
//
// It is also where the #5999 process-group kill lives on Unix. Windows has no
// setpgid and no signals, so there is nothing equivalent to wire here: cmd.Cancel
// is left at the os/exec default (cmd.Process.Kill(), which terminates the child
// alone). Killing a whole tree on Windows needs a Job object around the spawn,
// which this hook deliberately does not attempt — so on Windows a cancelled
// child's grandchildren still outlive it.
func applyGroupAlgoNice(cmd *exec.Cmd) {}

// NiceSelf is a no-op on Windows.
func NiceSelf() {}

// niceIsSupported reports that this platform does not demote priority.
func niceIsSupported() bool { return false }

// itoaPriority has no meaningful answer on Windows.
func itoaPriority() string { return "0" }
