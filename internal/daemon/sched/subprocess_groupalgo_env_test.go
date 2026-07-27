package sched

import (
	"strings"
	"testing"
)

// The group-algo child is the second background batch process the daemon
// fork-execs, and until #5954's follow-up it was built with a plain
// `append(os.Environ(), "GOMAXPROCS=...")` — no GODEBUG merge, unlike the
// index child. That left it returning freed pages with MADV_FREE, so any RSS
// it gave back stayed invisible to the OS (and to whole-machine measurement)
// until the kernel came under pressure.
//
// GODEBUG is read once at process start, so — unlike GOGC, which the child
// sets on itself — madvdontneed can ONLY be delivered through the child's env.
// That is why this is asserted on the constructed env rather than on runtime
// state.

// TestGroupAlgoChildEnvSetsMadvDontNeed asserts the constructed child env
// carries the reclaim setting, and still carries the CPU bound.
func TestGroupAlgoChildEnvSetsMadvDontNeed(t *testing.T) {
	env := groupAlgoChildEnv([]string{"PATH=/usr/bin", "HOME=/home/x"}, 2, false)

	godebug, ok := lookupEnv(env, "GODEBUG")
	if !ok {
		t.Fatalf("group-algo child env has no GODEBUG entry: %v", env)
	}
	if !strings.Contains(godebug, madvDontNeedSetting) {
		t.Errorf("GODEBUG = %q, want it to carry %q", godebug, madvDontNeedSetting)
	}
	if got, ok := lookupEnv(env, "GOMAXPROCS"); !ok || got != "2" {
		t.Errorf("GOMAXPROCS = %q (present=%v), want \"2\" — the CPU bound must survive the GODEBUG merge", got, ok)
	}
	if got, ok := lookupEnv(env, "PATH"); !ok || got != "/usr/bin" {
		t.Errorf("PATH = %q (present=%v), want the inherited value preserved", got, ok)
	}
}

// TestGroupAlgoChildEnvMergesInheritedGODEBUG asserts an operator's other
// GODEBUG settings are merged rather than clobbered, matching withMadvDontNeed's
// contract, and that an explicit madvdontneed=0 is left alone.
func TestGroupAlgoChildEnvMergesInheritedGODEBUG(t *testing.T) {
	env := groupAlgoChildEnv([]string{"GODEBUG=http2debug=1"}, 3, false)
	godebug, _ := lookupEnv(env, "GODEBUG")
	if !strings.Contains(godebug, "http2debug=1") || !strings.Contains(godebug, madvDontNeedSetting) {
		t.Errorf("GODEBUG = %q, want both the inherited setting and %q", godebug, madvDontNeedSetting)
	}

	env = groupAlgoChildEnv([]string{"GODEBUG=madvdontneed=0"}, 1, false)
	godebug, _ = lookupEnv(env, "GODEBUG")
	if godebug != "madvdontneed=0" {
		t.Errorf("GODEBUG = %q, want an explicit operator madvdontneed=0 left alone", godebug)
	}
}

// lookupEnv returns the LAST value for key, mirroring os/exec's dedupEnv
// ("preserve the last occurrence of each key") — i.e. what the child sees.
func lookupEnv(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			val, found = v, true
		}
	}
	return val, found
}
