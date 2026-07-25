package groupalgo

import (
	"sync"
	"testing"
)

// The group-algo child had ZERO memory instrumentation while being one of the
// two largest processes on the machine at the whole-machine peak (#5954). The
// memtrace sampler tags every sample with a phase read from a caller-supplied
// phaseFn; this package owns that phase state so the four stages of the pass
// are distinguishable in the NDJSON and in the per-phase heap profiles.
//
// Without a phase label the trace is one flat curve and the same misreading
// that cost this epic a day — attributing heap_inuse to the wrong stage — is
// exactly as available as before.

// TestPhaseOrder asserts the labels a full group-algo pass stamps, in the
// order the pass stamps them. The labels are the operator-facing contract with
// the measurement harness, so they are spelled out here rather than derived
// from the constants.
func TestPhaseOrder(t *testing.T) {
	restorePhase(t)

	want := []string{"assembling", "hashing", "running_algorithms", "writing_overlay"}
	got := []string{PhaseAssembling, PhaseHashing, PhaseRunningAlgorithms, PhaseWritingOverlay}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("phase %d = %q, want %q", i, got[i], want[i])
		}
	}

	// The holder must report whatever the pass last stamped, in sequence.
	for _, p := range want {
		SetPhase(p)
		if CurrentPhase() != p {
			t.Fatalf("after SetPhase(%q), CurrentPhase() = %q", p, CurrentPhase())
		}
	}
}

// TestCurrentPhaseBeforeAnyStamp asserts the zero value is a safe empty
// string, not a panic. memtrace polls phaseFn on a ticker from its own
// goroutine and may well poll before the pass has stamped anything.
func TestCurrentPhaseBeforeAnyStamp(t *testing.T) {
	restorePhase(t)
	currentPhase = phaseHolder{}
	if got := CurrentPhase(); got != "" {
		t.Fatalf("CurrentPhase() before any stamp = %q, want \"\"", got)
	}
}

// TestPhaseHolderIsRaceFree exercises the real access pattern: the pass
// goroutine stamps while the memtrace sampler goroutine polls. Run under -race
// this fails outright on an unsynchronised holder.
func TestPhaseHolderIsRaceFree(t *testing.T) {
	restorePhase(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
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
	for i := 0; i < 1000; i++ {
		SetPhase(PhaseRunningAlgorithms)
		SetPhase(PhaseHashing)
	}
	close(stop)
	wg.Wait()
}

// TestAssemblingIsStampedByTheEntrypoint asserts the FIRST stamp is real —
// that the phase is set by the pass itself and not only by the tests. A group
// that does not resolve fails inside assembly, which is precisely when the
// phase must already read "assembling".
func TestAssemblingIsStampedByTheEntrypoint(t *testing.T) {
	restorePhase(t)
	currentPhase = phaseHolder{}

	if _, err := RunGroupAlgorithms("grafel-no-such-group-5954"); err == nil {
		t.Skip("unexpected: a bogus group resolved; cannot exercise the assembly failure path")
	}
	if got := CurrentPhase(); got != PhaseAssembling {
		t.Fatalf("CurrentPhase() after a failed assembly = %q, want %q — the entrypoint does not stamp the phase",
			got, PhaseAssembling)
	}
}

// restorePhase makes the process-wide holder test-local.
func restorePhase(t *testing.T) {
	t.Helper()
	prev := CurrentPhase()
	t.Cleanup(func() { SetPhase(prev) })
}
