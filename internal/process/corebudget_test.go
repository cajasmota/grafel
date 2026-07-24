package process

import (
	"runtime"
	"testing"
)

// TestIndexCoreBudgetFor pins the 25%-of-machine policy (#5960) across host
// sizes. The property under test is "background indexing never gets more than
// a quarter of the box, and never gets zero cores" — a budget of 0 would stall
// indexing entirely, and anything above NumCPU/4 re-opens the over-subscription
// hazard this budget exists to close.
func TestIndexCoreBudgetFor(t *testing.T) {
	cases := []struct {
		numCPU int
		want   int
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 1},
		{8, 2},
		{12, 3}, // matches the previous static <=3-core rule on a 12-core host
		{16, 4},
		{32, 8},
		{64, 16},
	}
	for _, tc := range cases {
		if got := IndexCoreBudgetFor(tc.numCPU); got != tc.want {
			t.Errorf("IndexCoreBudgetFor(%d) = %d, want %d", tc.numCPU, got, tc.want)
		}
	}
}

// TestIndexCoreBudgetNeverZero guards the floor for degenerate core counts
// (containers and some platforms report 0 or garbage).
func TestIndexCoreBudgetNeverZero(t *testing.T) {
	for _, n := range []int{-8, -1, 0, 1, 2, 3} {
		if got := IndexCoreBudgetFor(n); got < 1 {
			t.Errorf("IndexCoreBudgetFor(%d) = %d, want >= 1", n, got)
		}
	}
}

// TestIndexCoreBudgetUsesHostCores verifies the no-arg form is the pure form
// applied to the real host core count.
func TestIndexCoreBudgetUsesHostCores(t *testing.T) {
	if got, want := IndexCoreBudget(), IndexCoreBudgetFor(runtime.NumCPU()); got != want {
		t.Fatalf("IndexCoreBudget() = %d, want IndexCoreBudgetFor(NumCPU=%d) = %d",
			got, runtime.NumCPU(), want)
	}
	if IndexCoreBudget() > runtime.NumCPU() {
		t.Fatalf("budget %d exceeds host cores %d", IndexCoreBudget(), runtime.NumCPU())
	}
}
