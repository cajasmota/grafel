package progress_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/progress"
)

// TestTracker_SubPhase_StampsWithoutPublishing is the zero-risk gate for the
// #5954 resolving_refs instrumentation.
//
// The whole point of SubPhase is that it splits the memtrace phase= tag
// WITHOUT touching the event stream: if it published, every consumer of the
// progress feed (SSE, the CLI renderer, the wizard fold table) would suddenly
// see a dozen phase labels it has no mapping for. So the test asserts both
// halves — CurrentPhase moves, the collector stays empty — and that a
// following PhaseStart resets the stamp cleanly.
func TestTracker_SubPhase_StampsWithoutPublishing(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")

	trk.PhaseStart(progress.PhaseResolveRefs, 10, 0)
	if len(col.Events) != 1 {
		t.Fatalf("PhaseStart should publish exactly 1 event, got %d", len(col.Events))
	}

	for _, label := range []string{
		progress.SubPhasePass25FrameworkRules,
		progress.SubPhasePass3CrossLang,
		progress.SubPhasePass27ResponseShapes,
	} {
		trk.SubPhase(label)
		if got := trk.CurrentPhase(); got != label {
			t.Errorf("CurrentPhase() after SubPhase(%q) = %q", label, got)
		}
		if len(col.Events) != 1 {
			t.Fatalf("SubPhase(%q) published an event — it must not; events=%d",
				label, len(col.Events))
		}
	}

	// An empty label is a no-op, not a phase reset.
	trk.SubPhase("")
	if got := trk.CurrentPhase(); got != progress.SubPhasePass27ResponseShapes {
		t.Errorf("SubPhase(\"\") changed the phase to %q", got)
	}

	// A malformed (parentless) label is dropped, not stamped.
	trk.SubPhase("pass99_no_parent")
	if got := trk.CurrentPhase(); got != progress.SubPhasePass27ResponseShapes {
		t.Errorf("SubPhase on a parentless label changed the phase to %q", got)
	}

	// The next real phase boundary takes over, sub-phase stamp and all.
	trk.PhaseStart(progress.PhaseMaterialize, 10, 5)
	if got := trk.CurrentPhase(); got != progress.PhaseMaterialize {
		t.Errorf("CurrentPhase() after PhaseStart(materializing) = %q", got)
	}
	if len(col.Events) != 2 {
		t.Fatalf("expected exactly 2 published events (the two PhaseStarts), got %d", len(col.Events))
	}
}

// TestSubPhaseLabels_ParentPrefixContract pins the naming contract the memtrace
// analysis relies on: every sub-phase label is "<parent>.<pass>", so grouping
// samples on the segment before the first "." reproduces the coarse phase
// breakdown exactly as it read before the split.
func TestSubPhaseLabels_ParentPrefixContract(t *testing.T) {
	labels := []string{
		progress.SubPhasePass25FrameworkRules,
		progress.SubPhasePass3CrossLang,
		progress.SubPhasePass35ConfigDiscovery,
		progress.SubPhasePass36Bazel,
		progress.SubPhasePass37Mage,
		progress.SubPhasePass38Task,
		progress.SubPhasePass26DjangoRoutes,
		progress.SubPhasePass26DjangoEdges,
		progress.SubPhasePass26JavaRoutes,
		progress.SubPhasePass27ResponseShapes,
		progress.SubPhasePass28TestsMultiHop,
		progress.SubPhaseReleaseASTs,
	}
	seen := make(map[string]bool, len(labels))
	for _, l := range labels {
		parent, pass, ok := strings.Cut(l, ".")
		if !ok {
			t.Errorf("label %q has no \".\" separator", l)
			continue
		}
		if parent != progress.PhaseResolveRefs {
			t.Errorf("label %q: parent = %q, want %q", l, parent, progress.PhaseResolveRefs)
		}
		if pass == "" || strings.Contains(pass, ".") {
			t.Errorf("label %q: pass segment %q must be non-empty and dot-free", l, pass)
		}
		if seen[l] {
			t.Errorf("duplicate sub-phase label %q", l)
		}
		seen[l] = true
	}
}

// TestTracker_SubPhase_DroppedWhenParentPhaseInactive pins the enforcement of
// the "<parent>.<pass>" contract.
//
// This is not hypothetical: cmd/grafel/index.go stamps the pass-3.5-and-later
// labels from code that runs in BOTH the in-process and the
// GRAFEL_SUBPROC_EXTRACT branch, and only the former enters resolving_refs. If
// SubPhase stamped unconditionally, a subproc-extract run would tag samples
// "resolving_refs.pass35_config_discovery" while extracting_ast was the active
// phase — breaking the prefix contract the memtrace analysis groups on. The
// stamp must be dropped instead, leaving the coarse phase in place.
func TestTracker_SubPhase_DroppedWhenParentPhaseInactive(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")

	// The subproc-extract shape: extracting_ast is active, and the shared
	// pass-3.5+ code path stamps a resolving_refs.* label.
	trk.PhaseStart(progress.PhaseExtractAST, 0, 0)
	for _, label := range []string{
		progress.SubPhasePass35ConfigDiscovery,
		progress.SubPhasePass36Bazel,
		progress.SubPhasePass28TestsMultiHop,
	} {
		trk.SubPhase(label)
		if got := trk.CurrentPhase(); got != progress.PhaseExtractAST {
			t.Fatalf("SubPhase(%q) under active phase %q stamped %q — cross-phase "+
				"stamps must be dropped, not recorded",
				label, progress.PhaseExtractAST, got)
		}
	}

	// Same labels, correct parent active: now they stamp.
	trk.PhaseStart(progress.PhaseResolveRefs, 0, 0)
	trk.SubPhase(progress.SubPhasePass35ConfigDiscovery)
	if got := trk.CurrentPhase(); got != progress.SubPhasePass35ConfigDiscovery {
		t.Fatalf("SubPhase under the matching parent = %q, want %q",
			got, progress.SubPhasePass35ConfigDiscovery)
	}

	// Before any phase has started there is no parent, so nothing stamps.
	fresh := progress.NewTracker(&progress.SliceCollector{}, "g", "r")
	fresh.SubPhase(progress.SubPhasePass25FrameworkRules)
	if got := fresh.CurrentPhase(); got != "" {
		t.Errorf("SubPhase before any PhaseStart = %q, want empty", got)
	}
}

// TestTracker_SubPhase_RaceWithCurrentPhase mirrors the real wiring: the
// indexer goroutine stamps sub-phases while the memtrace sampler goroutine
// polls CurrentPhase. Run under -race this pins the atomic.Value contract that
// makes the instrumentation safe to add anywhere inside a phase.
func TestTracker_SubPhase_RaceWithCurrentPhase(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")
	trk.PhaseStart(progress.PhaseResolveRefs, 0, 0)

	labels := []string{
		progress.SubPhasePass25FrameworkRules,
		progress.SubPhasePass3CrossLang,
		progress.SubPhasePass28TestsMultiHop,
		progress.SubPhaseReleaseASTs,
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // sampler
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = trk.CurrentPhase()
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		trk.SubPhase(labels[i%len(labels)])
	}
	close(done)
	wg.Wait()
}
