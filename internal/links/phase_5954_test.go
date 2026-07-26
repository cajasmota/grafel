package links

// phase_5954_test.go — the cross-repo link pass's phase stamps (#5954).
//
// The engine process was the last unprofiled memory plane in the epic: it
// calls memtrace.Start with a nil phaseFn, so every sample it emits is tagged
// "" and the only usable artifact is heap-peak.pprof. That is enough to know
// the engine is big and not enough to know WHICH stage is big — and the two
// candidate stages (the multi-pass window over the whole group union, versus
// the phantom-edge reload of the same union) call for completely different
// fixes.
//
// This test observes a REAL RunAllPasses run rather than asserting the
// constants against literals. An earlier test in this epic did the latter and
// three of four stamps turned out to be unprotected: round-tripping a constant
// through the holder proves the holder works, not that the pass stamps. The
// assertion here is derived from res.Results — the pass's own report of which
// passes it ran, in the order it ran them — so a new pass added without a
// stamp, or a stamp that drifts from the pass it labels, fails.

import (
	"path/filepath"
	"testing"
)

func TestRunAllPasses_StampsOnePhasePerPassInOrder(t *testing.T) {
	ResetPhaseHistory()
	t.Cleanup(ResetPhaseHistory)

	root := fixtureRoot(t)
	twoRepoGraphs(t, root)

	res, err := RunAllPasses("g1", root, filepath.Join(root, "ag-home"))
	if err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}
	if len(res.Results) < 10 {
		t.Fatalf("fixture ran only %d passes — too few for this contract to mean anything", len(res.Results))
	}

	// The expected history is derived from the run itself: the load stage,
	// then one phase per pass, labelled with the pass's own name.
	want := make([]string, 0, len(res.Results)+1)
	want = append(want, PhaseLoad)
	for _, r := range res.Results {
		want = append(want, PhaseForPass(r.Pass))
	}

	got := PhaseHistory()
	if len(got) != len(want) {
		t.Fatalf("phase history has %d entries, want %d\n got: %v\nwant: %v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phase history[%d] = %q, want %q\n got: %v\nwant: %v",
				i, got[i], want[i], got, want)
		}
	}

	if cur := CurrentPhase(); cur != want[len(want)-1] {
		t.Errorf("CurrentPhase() = %q after the run, want the last stamped phase %q", cur, want[len(want)-1])
	}
}

// TestPhaseLoadIsStampedBeforeTheGraphsAreRead pins the ordering property the
// measurement actually depends on: loadAllGraphs — which materialises every
// repo's Document and holds them for the whole pass — is attributed to
// link_pass_load and not to whichever pass happens to run first. A stamp placed
// after the load would silently bill the union's peak to the import pass.
func TestPhaseLoadIsStampedBeforeTheGraphsAreRead(t *testing.T) {
	ResetPhaseHistory()
	t.Cleanup(ResetPhaseHistory)

	root := fixtureRoot(t)
	twoRepoGraphs(t, root)
	if _, err := RunAllPasses("g1", root, filepath.Join(root, "ag-home")); err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}
	hist := PhaseHistory()
	if len(hist) == 0 || hist[0] != PhaseLoad {
		t.Fatalf("first stamped phase = %v, want %q first", hist, PhaseLoad)
	}
}

// TestPhaseHistoryIsResettable keeps ResetPhaseHistory honest: a second run in
// the same process must not inherit the first run's transitions, or a hand-run
// diagnostic reading PhaseHistory would report a concatenation of two passes.
func TestPhaseHistoryIsResettable(t *testing.T) {
	ResetPhaseHistory()
	t.Cleanup(ResetPhaseHistory)

	SetPhase(PhaseLoad)
	if len(PhaseHistory()) != 1 {
		t.Fatalf("history = %v, want one entry", PhaseHistory())
	}
	ResetPhaseHistory()
	if h := PhaseHistory(); len(h) != 0 {
		t.Fatalf("history after reset = %v, want empty", h)
	}
	if c := CurrentPhase(); c != "" {
		t.Fatalf("CurrentPhase after reset = %q, want empty", c)
	}
}
