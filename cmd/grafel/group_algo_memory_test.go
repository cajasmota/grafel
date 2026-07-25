package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The GC-pacing cap was originally scoped to the `index-internal` child alone
// (#5954). Whole-machine measurement then showed the peak instant has moved
// entirely POST-index: the index child is at 0MB while `group-algo` is one of
// the two largest processes on the box. group-algo is the second background,
// nobody-is-watching batch child in the product, so the same standing trade —
// spend GC CPU where no human is waiting, to buy RSS — applies verbatim.
//
// These tests are written against the HAZARD (an uncapped background child /
// a cap leaking onto an interactive command), not against the implementation's
// constants.

// TestBackgroundGCPercentCommandsCoverBothChildren asserts the gate is a SET
// covering every background child the daemon fork-execs, and that it does not
// admit anything else.
func TestBackgroundGCPercentCommandsCoverBothChildren(t *testing.T) {
	background := []string{"index-internal", "group-algo"}
	for _, cmd := range background {
		if !isBackgroundGCPercentCommand(cmd) {
			t.Errorf("isBackgroundGCPercentCommand(%q) = false, want true — it is a background batch child and must be capped", cmd)
		}
	}
	// Everything a human can be waiting on must stay out of the set. These are
	// the real argv[1] values on the interactive/foreground paths.
	interactive := []string{"", "index", "rebuild", "daemon", "serve", "engine", "mcp", "doctor", "selftest", "group-algorithm", "group"}
	for _, cmd := range interactive {
		if isBackgroundGCPercentCommand(cmd) {
			t.Errorf("isBackgroundGCPercentCommand(%q) = true, want false — interactive/foreground work is never capped", cmd)
		}
	}
}

// TestGroupAlgoGCPercentDecision pins the policy for the group-algo child. It
// is the same precedence ladder the index child already has; the point of the
// test is that group-algo actually rides it.
func TestGroupAlgoGCPercentDecision(t *testing.T) {
	const ga = "group-algo"
	cases := []struct {
		name        string
		command     string
		rawEnv      string
		rawGOGC     string
		wantPercent int
		wantSource  string
	}{
		{"group-algo child, no env -> policy default", ga, "", "", indexGCPercentDefault, "policy"},
		{"operator override wins", ga, "35", "", 35, "GRAFEL_INDEX_GOGC"},
		{"operator override wins over an explicit GOGC", ga, "35", "200", 35, "GRAFEL_INDEX_GOGC"},
		{"operator override can disable the cap", ga, "off", "", gcPercentUnset, "GRAFEL_INDEX_GOGC"},
		{"explicit GOGC is respected", ga, "", "200", gcPercentUnset, "GOGC"},
		{"malformed override falls back to policy", ga, "banana", "", indexGCPercentDefault, "policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercent, gotSource := indexGCPercentDecision(tc.command, tc.rawEnv, tc.rawGOGC)
			if gotPercent != tc.wantPercent {
				t.Errorf("indexGCPercentDecision(%q, %q, %q) percent = %d, want %d",
					tc.command, tc.rawEnv, tc.rawGOGC, gotPercent, tc.wantPercent)
			}
			if !strings.Contains(gotSource, tc.wantSource) {
				t.Errorf("indexGCPercentDecision(%q, %q, %q) source = %q, want it to mention %q",
					tc.command, tc.rawEnv, tc.rawGOGC, gotSource, tc.wantSource)
			}
		})
	}
}

// TestApplyGroupAlgoGCPercentReachesTheRuntime asserts the decision actually
// lands in the Go runtime for a group-algo invocation, not merely that a pure
// function returned it. debug.SetGCPercent returns the PREVIOUS value, so one
// call both reads the setting and restores the one we want.
func TestApplyGroupAlgoGCPercentReachesTheRuntime(t *testing.T) {
	orig := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(orig) })

	t.Run("group-algo child is capped at the policy value", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent("group-algo", "", "")
		if got := debug.SetGCPercent(100); got != indexGCPercentDefault {
			t.Fatalf("GOGC in force after applyIndexGCPercent(group-algo) = %d, want %d", got, indexGCPercentDefault)
		}
	})

	t.Run("operator override is what actually reaches the runtime", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent("group-algo", "35", "")
		if got := debug.SetGCPercent(100); got != 35 {
			t.Fatalf("GOGC in force after applyIndexGCPercent(group-algo) = %d, want 35", got)
		}
	})

	// The standing product rule, restated where it is easiest to break: adding
	// a second command to the gate must not turn the gate into a pass-through.
	// Not even an explicit GRAFEL_INDEX_GOGC may create a cap on a command a
	// human is waiting on. Break the gate (e.g. `return true` from
	// isBackgroundGCPercentCommand) and this subtest fails.
	t.Run("interactive commands stay uncapped even with the override set", func(t *testing.T) {
		for _, cmd := range []string{"index", "rebuild", "daemon", ""} {
			debug.SetGCPercent(100)
			applyIndexGCPercent(cmd, "35", "")
			if got := debug.SetGCPercent(100); got != 100 {
				t.Fatalf("applyIndexGCPercent(%q, \"35\", \"\") changed GOGC to %d, want it left at 100", cmd, got)
			}
		}
	})
}
