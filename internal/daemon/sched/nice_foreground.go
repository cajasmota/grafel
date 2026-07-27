package sched

import (
	"os"
	"strings"
)

// nice_foreground.go — the OS-PRIORITY half of "interactive remains uncapped"
// (#5954).
//
// THE GAP THIS CLOSES. GOMAXPROCS was not the only thing capping a user-awaited
// rebuild. Both heavy batch children renice THEMSELVES to +10 at startup, with
// no notion of what triggered the work, so a `grafel reset` the user is
// blocking on ran its group-algo pass below the user's editor, below their
// browser, and below grafel's own un-niced index children.
//
// WHAT THIS IS WORTH IS NOT ESTABLISHED — do not cite it as the wall-time fix.
// nice weighting binds under oversubscription; a single-threaded pass on a box
// with idle cores gets a core on demand at any nice level, and a process that
// is blocked, sleeping, or waiting on a debounce is blocked at every nice level
// too. The case for the change is that it removes a real, provable demotion of
// work a human is waiting on, and costs nothing when the machine is idle. Where
// the elapsed time actually goes is a separate question and needs measuring,
// not patching.
//
// WHY AN ENV VAR AND NOT A FLAG. The renice is issued BY THE CHILD (see
// cmd/grafel/group_algo.go and cmd/grafel/links_internal.go), deliberately, so
// it holds however the child was spawned. A parent-side-only fix therefore
// would not bind at all — the recurring failure mode of this epic: a guard that
// exists, looks correct, and does not bind the path that runs. The signal has
// to cross the fork, and these children already take their whole configuration
// (GOMAXPROCS, GODEBUG, state dirs) through the environment.
//
// BACKGROUND KEEPS NICE+10. That demotion is the standing policy and the reason
// a background pass does not make the machine unusable — it is what stopped the
// v0.1.3 regression from starving a consumer's test harness. It lifts only for
// work the user explicitly asked for and is blocking on.

// childForegroundEnv tells a spawned batch child that the work it is about to
// do is user-awaited. Set by the spawn helpers from the per-group foreground
// registry (foreground.go); read by the child at startup.
const childForegroundEnv = "GRAFEL_CHILD_FOREGROUND"

// childForegroundEnvEntry renders the env entry for a child spawn.
//
// It is always emitted, including as an explicit "=0" for background children,
// because the child inherits the daemon's whole environment: if the daemon
// itself was started with GRAFEL_CHILD_FOREGROUND=1 in its environment (an
// operator export, a stale launchd plist), an omit-when-false scheme would
// silently un-nice every background pass on the machine. Appended last, so it
// wins over any inherited value.
func childForegroundEnvEntry(foreground bool) string {
	if foreground {
		return childForegroundEnv + "=1"
	}
	return childForegroundEnv + "=0"
}

// ChildIsForeground reports whether THIS process was spawned as part of
// user-awaited work.
func ChildIsForeground() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(childForegroundEnv))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// NiceSelfUnlessForeground applies the background OS-priority demotion to this
// process unless it was spawned for user-awaited work. Returns whether the
// demotion was applied.
//
// Call this at the top of a batch child instead of NiceSelf.
func NiceSelfUnlessForeground() bool {
	if ChildIsForeground() {
		return false
	}
	NiceSelf()
	return true
}
