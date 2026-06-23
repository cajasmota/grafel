//go:build windows

package sched

import "os/exec"

// On Windows there is no setpriority(2)/nice. The CPU cap (GOMAXPROCS) is the
// portable control; OS-priority demotion is a no-op here. (A BELOW_NORMAL
// priority class could be set via CREATE_* flags, but the cap already bounds
// the draw and we avoid platform-specific spawn surgery.)

const groupAlgoNice = 0

// applyGroupAlgoNice is a no-op on Windows.
func applyGroupAlgoNice(cmd *exec.Cmd) {}

// NiceSelf is a no-op on Windows.
func NiceSelf() {}
