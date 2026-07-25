package groupalgo

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// The group-algo child had ZERO memory instrumentation while being one of the
// two largest processes on the machine at the whole-machine peak (#5954). The
// memtrace sampler tags every sample with a phase read from a caller-supplied
// phaseFn; this package owns that phase state so the four stages of the pass
// are distinguishable in the NDJSON and in the per-phase heap profiles.
//
// These tests must OBSERVE THE PASS. An earlier version of TestPhaseOrder
// compared the four constants to four string literals and round-tripped them
// through the holder — a tautology: every stamp except `assembling` could be
// deleted from the production code with the whole package still green. The
// labels are only worth anything if the pass actually stamps them, in order.

// TestPhaseOrder runs a real RunGroupAlgorithms and asserts the sequence of
// phases the pass stamped. Deleting any single SetPhase call in
// RunGroupAlgorithms fails this test.
//
// An empty (registered but never-indexed) group is deliberate: assembly,
// hashing and the algorithm pass all still run — graph.RunAlgorithms guards
// len==0 — so all three stamps are exercised with no graph.fb fixture, and the
// test stays fast enough to leave the ordering the only thing under assertion.
func TestPhaseOrder(t *testing.T) {
	restorePhase(t)
	registerEmptyGroup(t, "phase-order")
	ResetPhaseHistory()

	if _, err := RunGroupAlgorithms("phase-order"); err != nil {
		t.Fatalf("RunGroupAlgorithms: %v", err)
	}

	// Spelled out rather than derived from the constants: these strings are the
	// operator-facing contract — they appear verbatim in the memtrace NDJSON
	// `phase` field and in the heap-profile filenames — so a rename must break
	// a test rather than silently re-key every trace the harness has collected.
	want := []string{"assembling", "hashing", "running_algorithms"}
	if got := PhaseHistory(); !slices.Equal(got, want) {
		t.Fatalf("phases stamped by RunGroupAlgorithms = %v, want %v", got, want)
	}
}

// TestPhaseOrderIncremental asserts the incremental entrypoint — the one the
// daemon's --write child actually calls — stamps the SAME order. The two
// entrypoints agreeing is the property that lets the phase doc state one order
// instead of two.
func TestPhaseOrderIncremental(t *testing.T) {
	restorePhase(t)
	registerEmptyGroup(t, "phase-order-inc")
	ResetPhaseHistory()

	if _, err := RunGroupAlgorithmsIncremental("phase-order-inc"); err != nil {
		t.Fatalf("RunGroupAlgorithmsIncremental: %v", err)
	}

	want := []string{"assembling", "hashing", "running_algorithms"}
	if got := PhaseHistory(); !slices.Equal(got, want) {
		t.Fatalf("phases stamped by RunGroupAlgorithmsIncremental = %v, want %v", got, want)
	}
}

// TestPhaseConstantsMatchTheContract pins the label spellings themselves. The
// order tests above compare against literals, so this is the one place a
// constant rename is reported as a rename rather than as a mystery sequence
// mismatch.
func TestPhaseConstantsMatchTheContract(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{PhaseAssembling, "assembling"},
		{PhaseHashing, "hashing"},
		{PhaseRunningAlgorithms, "running_algorithms"},
		{PhaseWritingOverlay, "writing_overlay"},
	} {
		if tc.got != tc.want {
			t.Errorf("phase constant = %q, want %q", tc.got, tc.want)
		}
	}
}

// TestCurrentPhaseBeforeAnyStamp asserts the zero value is a safe empty
// string, not a panic. memtrace polls phaseFn on a ticker from its own
// goroutine and may well poll before the pass has stamped anything.
func TestCurrentPhaseBeforeAnyStamp(t *testing.T) {
	restorePhase(t)
	ResetPhaseHistory()
	if got := CurrentPhase(); got != "" {
		t.Fatalf("CurrentPhase() before any stamp = %q, want \"\"", got)
	}
	if got := PhaseHistory(); len(got) != 0 {
		t.Fatalf("PhaseHistory() before any stamp = %v, want empty", got)
	}
}

// TestPhaseHistoryRecordsTransitionsNotCalls asserts a re-stamp of the same
// phase does not grow the log. The log is read as a transition sequence, and a
// stamp inside a loop must not turn it into a call counter.
func TestPhaseHistoryRecordsTransitionsNotCalls(t *testing.T) {
	restorePhase(t)
	ResetPhaseHistory()

	SetPhase(PhaseAssembling)
	SetPhase(PhaseAssembling)
	SetPhase(PhaseHashing)
	SetPhase(PhaseAssembling)

	want := []string{"assembling", "hashing", "assembling"}
	if got := PhaseHistory(); !slices.Equal(got, want) {
		t.Fatalf("PhaseHistory() = %v, want %v", got, want)
	}
}

// TestPhaseHolderIsRaceFree exercises the real access pattern: the pass
// goroutine stamps while the memtrace sampler goroutine polls. Run under -race
// this fails outright on an unsynchronised holder.
func TestPhaseHolderIsRaceFree(t *testing.T) {
	restorePhase(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = CurrentPhase()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = PhaseHistory()
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		SetPhase(PhaseRunningAlgorithms)
		SetPhase(PhaseHashing)
	}
	close(stop)
	wg.Wait()
}

// TestAssemblingIsStampedOnTheFailurePath asserts the first stamp lands before
// assembly can fail — the phase must already read "assembling" when a group
// that does not resolve blows up inside it, or a trace of a failed run is
// unattributable.
func TestAssemblingIsStampedOnTheFailurePath(t *testing.T) {
	restorePhase(t)
	ResetPhaseHistory()

	if _, err := RunGroupAlgorithms("grafel-no-such-group-5954"); err == nil {
		t.Skip("unexpected: a bogus group resolved; cannot exercise the assembly failure path")
	}
	if got := CurrentPhase(); got != PhaseAssembling {
		t.Fatalf("CurrentPhase() after a failed assembly = %q, want %q", got, PhaseAssembling)
	}
}

// registerEmptyGroup registers a group whose single repo was never indexed (no
// graph.fb), so assembly yields an empty union without needing a fixture.
func registerEmptyGroup(t *testing.T, name string) {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	cfgPath, err := registry.ConfigPathFor(name)
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{
		Name:  name,
		Repos: []registry.Repo{{Slug: "ghost", Path: filepath.Join(root, "ghost")}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup(name, cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}
}

// restorePhase makes the process-wide holder test-local.
func restorePhase(t *testing.T) {
	t.Helper()
	prevPhase, prevHistory := CurrentPhase(), PhaseHistory()
	t.Cleanup(func() {
		ResetPhaseHistory()
		for _, p := range prevHistory {
			SetPhase(p)
		}
		if prevPhase != "" {
			SetPhase(prevPhase)
		}
	})
}
