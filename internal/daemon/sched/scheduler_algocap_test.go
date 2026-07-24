package sched

import "testing"

// TestResolveAlgoCap_ExplicitOverride asserts that an operator-supplied
// AlgoCap > 0 is honored verbatim, with no clamping applied.
func TestResolveAlgoCap_ExplicitOverride(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{1, 1},
		{4, 4},
		{6, 6},
	}
	for _, c := range cases {
		if got := resolveAlgoCap(c.in); got != c.want {
			t.Errorf("resolveAlgoCap(%d) = %d, want %d (explicit override must pass through unclamped)", c.in, got, c.want)
		}
	}
}

// TestResolveAlgoCap_AutoClampedTo3Cores asserts the project's hard
// constraint: auto-tuned algo-pass concurrency must never exceed 3 cores,
// regardless of host size, so indexing never saturates the user's machine.
// It must also never fall below 2, to preserve minimal usable parallelism.
//
// This is intentionally an invariant test (not an exact-value test tied to
// a specific host core count) per the task guidance: injecting NumCPU would
// require restructuring resolveAlgoCap's signature, which is out of scope.
// On this repo's 12-core dev/CI host, this test is RED against the
// pre-fix implementation (returns 6, which is > 3) and GREEN after the fix.
func TestResolveAlgoCap_AutoClampedTo3Cores(t *testing.T) {
	got := resolveAlgoCap(0)
	if got < 2 || got > 3 {
		t.Fatalf("resolveAlgoCap(0) = %d, want value in [2,3] (auto-tuned cap must be clamped to the project's <=3-core ceiling)", got)
	}
}
