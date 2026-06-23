package sched

import (
	"runtime"
	"testing"
	"time"
)

// TestGroupAlgoGOMAXPROCSDefault asserts the background group-algo subprocess
// CPU cap defaults to 2 cores ("the less the better"), independent of host
// core count — the fix for the v0.1.3 CPU regression where the pass ran at the
// host's full GOMAXPROCS.
func TestGroupAlgoGOMAXPROCSDefault(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_CPU", "")
	if got := GroupAlgoGOMAXPROCS(); got != 2 {
		t.Fatalf("default GroupAlgoGOMAXPROCS = %d, want 2", got)
	}
}

// TestGroupAlgoGOMAXPROCSEnvOverride asserts GRAFEL_GROUP_ALGO_CPU overrides the
// default, including the user's "throttle to a single core" case (=1).
func TestGroupAlgoGOMAXPROCSEnvOverride(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"1", 1}, // the user's explicit "one core" knob
		{"3", 3},
		{"  4  ", 4}, // whitespace-tolerant
		{"0", 2},     // non-positive → default
		{"-1", 2},    // non-positive → default
		{"abc", 2},   // non-numeric → default
		{"", 2},      // unset → default
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("GRAFEL_GROUP_ALGO_CPU", c.env)
			if got := GroupAlgoGOMAXPROCS(); got != c.want {
				t.Fatalf("GroupAlgoGOMAXPROCS(%q) = %d, want %d", c.env, got, c.want)
			}
		})
	}
}

// TestGroupAlgoDebounceDefault asserts the debounce default is 180s (3 min),
// raised from 30s so a burst of commits coalesces into ONE group-algo pass.
func TestGroupAlgoDebounceDefault(t *testing.T) {
	t.Setenv("GRAFEL_GROUP_ALGO_DEBOUNCE", "")
	if got := groupAlgoDebounceFromEnv(); got != 180*time.Second {
		t.Fatalf("default group-algo debounce = %s, want 180s", got)
	}
	if groupAlgoDebounceDefault != 180*time.Second {
		t.Fatalf("groupAlgoDebounceDefault = %s, want 180s", groupAlgoDebounceDefault)
	}
}

// TestGroupAlgoDebounceEnvOverride asserts GRAFEL_GROUP_ALGO_DEBOUNCE still
// overrides the (now-longer) default, and that bad values fall back.
func TestGroupAlgoDebounceEnvOverride(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"45s", 45 * time.Second},
		{"5m", 5 * time.Minute},
		{"0s", 180 * time.Second},      // non-positive → default
		{"garbage", 180 * time.Second}, // unparseable → default
		{"", 180 * time.Second},        // unset → default
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("GRAFEL_GROUP_ALGO_DEBOUNCE", c.env)
			if got := groupAlgoDebounceFromEnv(); got != c.want {
				t.Fatalf("debounce(%q) = %s, want %s", c.env, got, c.want)
			}
		})
	}
}

// TestGroupAlgoNiceValue asserts the OS-priority demotion is a positive nice on
// Unix and guarded off (0) on Windows. NiceSelf itself must never panic.
func TestGroupAlgoNiceValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		if groupAlgoNice != 0 {
			t.Fatalf("groupAlgoNice on windows = %d, want 0 (guarded off)", groupAlgoNice)
		}
	} else {
		if groupAlgoNice <= 0 {
			t.Fatalf("groupAlgoNice on %s = %d, want positive demotion", runtime.GOOS, groupAlgoNice)
		}
	}
	// Best-effort, must not panic regardless of platform/permission.
	NiceSelf()
}
