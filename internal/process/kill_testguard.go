package process

// kill_testguard.go — the fail-closed guard that stops a TEST BINARY from
// sending a real SIGTERM (#7268).
//
// # Why this is not just a convention
//
// `grafel doctor --kill-stale` and POST /api/diagnostics/kill-stale both
// enumerate the host process table and feed what they select to Kill. Their
// tests drive them with SYNTHETIC process tables full of invented PIDs — 31001,
// 41001 — which on a real host belong to somebody else. For a long time the only
// thing standing between those PIDs and SIGTERM was an argument (kill=false)
// and a query-string parse, both of them single tokens, both ungraded.
//
// #7268 round 4 added a killProc seam at each call site so tests could point the
// kill at a recorder, and asserted in a comment that "every test in this file
// installs it". That claim was FALSE AT PACKAGE SCOPE the moment it was written:
// internal/cli/doctor_staleness_test.go drove the same function with neither
// seam installed, and a reviewer proved reachability by swapping the default for
// a panic — it fired, naming the user's live launchd daemon (PPID=1) as the
// process the loop had reached. The predicate happened to select nothing on that
// host, but this repo's own agents manufacture orphaned /tmp/<worktree>/grafel
// processes with PPID=1, and those satisfy criterion 1 with NO predicate
// regression at all.
//
// A comment cannot enforce that invariant and a reviewer cannot re-derive it for
// every future test. This does: under `go test`, reaching the real kill is a
// panic naming the PID, not a signal.
//
// # What it does NOT cover
//
// testing.Testing() is false in a binary produced by `go build` that a harness
// then execs — a real shipped grafel, which SHOULD be able to kill. That is the
// intended production path, not a gap.

import (
	"fmt"
	"testing"
)

// killGuardUnderGoTest is testing.Testing(), captured once. It is a package var
// rather than a direct call so killGuardedFor's two branches are both gradable:
// the interesting one — a process where testing.Testing() is FALSE — is a state
// no test in this package can ever be in. The same seam shape as
// internal/install/watchers' guardUnderGoTest, for the same reason.
var killGuardUnderGoTest = testing.Testing()

// KillGuarded is Kill, refusing to signal anything from inside a test binary.
//
// Production kill paths should use THIS rather than Kill directly, so that the
// protection is a property of the call site and not of each test's discipline.
func KillGuarded(pid int) error {
	return killGuardedFor(killGuardUnderGoTest, Kill, pid)
}

// killGuardedFor is the decision, with both of its inputs passed explicitly.
//
// kill is a parameter rather than a direct call to Kill for a reason that is the
// whole point of this file: a test that exercised the pass-through branch by
// calling the real Kill would have to name a PID, and there is no PID that is
// safe to signal — 0 and negatives address process GROUPS. Injecting the
// function is the only way to grade "it forwards" without signalling anything.
func killGuardedFor(underGoTest bool, kill func(int) error, pid int) error {
	if underGoTest {
		panic(fmt.Sprintf("process.KillGuarded(%d) reached inside a test binary: a test drove a "+
			"kill path without installing its killProc seam. Install the package's recorder "+
			"(withNoKills) in that test. This panic is the guard working — no signal was sent.", pid))
	}
	return kill(pid)
}
