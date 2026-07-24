package extract

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// hazardCores models the OBSERVED hazard this issue is about, independently of
// whatever constants the implementation happens to use: a background extraction
// round runs `concurrency` extract subprocesses in parallel, and each of those
// children can itself occupy up to its own GOMAXPROCS worth of runnable threads
// (its Go runtime) — plus, because tree-sitter parsing is cgo and a goroutine
// in a cgo call parks in _Gsyscall while the runtime hands its P away, the
// child's parses are NOT bounded by GOMAXPROCS at all. So `concurrency × gmp`
// is the FLOOR of the real thread draw, never an over-estimate.
//
// Deliberately derived from the process model, not from the implementation
// constants: if backgroundConcurrency or extractGOMAXPROCS is retuned, this
// still measures the thing that hurts the user's machine.
func hazardCores(concurrency, gmp int) int { return concurrency * gmp }

// TestBackgroundFanoutRespectsCoreBudget is the #5960 red→green test. Against
// the pre-fix code on a 12-core host this fails: backgroundConcurrency(12) = 4
// and extractGOMAXPROCS() = 2, so the background draw is 4×2 = 8 threads while
// the project's rule allows max(1, 12/4) = 3.
func TestBackgroundFanoutRespectsCoreBudget(t *testing.T) {
	for _, numCPU := range []int{1, 2, 4, 8, 12, 16, 32, 64} {
		budget := process.IndexCoreBudgetFor(numCPU)
		conc := backgroundConcurrency(numCPU)
		gmp := extractGOMAXPROCS()

		if conc < 1 {
			t.Errorf("numCPU=%d: backgroundConcurrency = %d, must be >= 1 "+
				"(0 subprocesses = indexing never progresses)", numCPU, conc)
		}
		if got := hazardCores(conc, gmp); got > budget {
			t.Errorf("numCPU=%d: background draw = concurrency(%d) × GOMAXPROCS(%d) = %d "+
				"threads, over the %d-core budget (25%% of %d cores)",
				numCPU, conc, gmp, got, budget, numCPU)
		}
		// The budget is a target, not just a ceiling: leaving cores unused makes
		// background indexing needlessly slow. The fanout must consume the
		// largest whole multiple of gmp that still fits inside the budget.
		if got, floor := hazardCores(conc, gmp), budget-(gmp-1); got < floor {
			t.Errorf("numCPU=%d: background draw = %d threads, under-uses the %d-core "+
				"budget (should be >= %d)", numCPU, got, budget, floor)
		}
	}
}

// TestBackgroundFanoutOnThisHost proves the invariant on the real host the
// tests are running on, so a machine-specific regression (e.g. a hard-coded
// constant that only fits a 12-core box) is caught in CI on any runner.
func TestBackgroundFanoutOnThisHost(t *testing.T) {
	budget := process.IndexCoreBudget()
	conc := (CoordinatorConfig{Interactive: false}).concurrency()
	gmp := (CoordinatorConfig{Interactive: false}).childGOMAXPROCS()
	if got := hazardCores(conc, gmp); got > budget {
		t.Fatalf("host (%d cores): background draw = %d×%d = %d threads, over budget %d",
			runtime.NumCPU(), conc, gmp, got, budget)
	}
}

// TestInteractivePathIsUncapped asserts requirement 3: the budget applies ONLY
// to background work. A user-initiated rebuild is awaited by a human and must
// keep running at host speed.
func TestInteractivePathIsUncapped(t *testing.T) {
	wantCPU := runtime.NumCPU()
	if wantCPU < 1 {
		wantCPU = 1
	}
	if got := (CoordinatorConfig{Interactive: true}).concurrency(); got != wantCPU {
		t.Fatalf("interactive concurrency() = %d, want host cores %d (must stay uncapped)", got, wantCPU)
	}
	if got := (CoordinatorConfig{Interactive: true}).childGOMAXPROCS(); got != wantCPU {
		t.Fatalf("interactive childGOMAXPROCS() = %d, want host cores %d (must stay uncapped)", got, wantCPU)
	}
	// And it must be allowed to exceed the background budget on a real host.
	if runtime.NumCPU() >= 8 {
		if got := (CoordinatorConfig{Interactive: true}).concurrency(); got <= process.IndexCoreBudget() {
			t.Fatalf("interactive fanout %d should exceed the background budget %d on a %d-core host",
				got, process.IndexCoreBudget(), runtime.NumCPU())
		}
	}
}

// TestOperatorOverridesBeatTheBudget asserts requirement 4: every existing
// escape hatch still wins over the adaptive default and is NOT clamped to the
// budget, even when it asks for more than 25% of the box.
func TestOperatorOverridesBeatTheBudget(t *testing.T) {
	budget := process.IndexCoreBudget()
	over := budget*4 + 1

	t.Run("explicit-config-field", func(t *testing.T) {
		if got := (CoordinatorConfig{Concurrency: over}).concurrency(); got != over {
			t.Fatalf("explicit Concurrency=%d clamped to %d", over, got)
		}
	})

	t.Run("env-concurrency", func(t *testing.T) {
		t.Setenv("GRAFEL_EXTRACT_CONCURRENCY", strconv.Itoa(over))
		if got := (CoordinatorConfig{}).concurrency(); got != over {
			t.Fatalf("GRAFEL_EXTRACT_CONCURRENCY=%d clamped to %d", over, got)
		}
	})

	t.Run("env-gomaxprocs", func(t *testing.T) {
		t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", strconv.Itoa(over))
		if got := extractGOMAXPROCS(); got != over {
			t.Fatalf("GRAFEL_EXTRACT_GOMAXPROCS=%d clamped to %d", over, got)
		}
		// The derived default fanout still degrades gracefully: it never
		// returns 0 subprocesses even when one child alone eats the budget.
		if got := backgroundConcurrency(runtime.NumCPU()); got < 1 {
			t.Fatalf("backgroundConcurrency = %d with a huge GOMAXPROCS override, want >= 1", got)
		}
	})
}
