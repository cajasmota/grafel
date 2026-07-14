package sched

import (
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestForegroundReindexGOMAXPROCS verifies the foreground cap resolver honours
// GRAFEL_REBUILD_GOMAXPROCS and otherwise defaults to the host core count (the
// human is waiting, so the first index runs at host speed).
func TestForegroundReindexGOMAXPROCS(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "7")
		if got := ForegroundReindexGOMAXPROCS(); got != 7 {
			t.Errorf("ForegroundReindexGOMAXPROCS() = %d, want 7", got)
		}
	})
	t.Run("default host cores", func(t *testing.T) {
		t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "")
		want := runtime.NumCPU()
		if want < 1 {
			want = 1
		}
		if got := ForegroundReindexGOMAXPROCS(); got != want {
			t.Errorf("ForegroundReindexGOMAXPROCS() = %d, want %d (host cores)", got, want)
		}
	})
	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "0")
		want := runtime.NumCPU()
		if want < 1 {
			want = 1
		}
		if got := ForegroundReindexGOMAXPROCS(); got != want {
			t.Errorf("ForegroundReindexGOMAXPROCS() = %d, want %d", got, want)
		}
	})
}

// TestResolveChildGOMAXPROCS_Interactive verifies a human-awaited rebuild runs
// its child at the FOREGROUND cap, not the throttled background reindex budget —
// the fix so a first-index through the subprocess path is not slower than the
// old in-process rebuild.
func TestResolveChildGOMAXPROCS_Interactive(t *testing.T) {
	// Explicit cap wins verbatim.
	if n, _ := resolveChildGOMAXPROCS(true, 5); n != 5 {
		t.Errorf("interactive explicit cap: got %d, want 5", n)
	}
	// Unset explicit cap → resolve GRAFEL_REBUILD_GOMAXPROCS.
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "9")
	if n, _ := resolveChildGOMAXPROCS(true, 0); n != 9 {
		t.Errorf("interactive default cap: got %d, want 9 (foreground cap)", n)
	}
}

// TestResolveChildGOMAXPROCS_BackgroundUnchanged verifies the scheduler
// (non-interactive) path is byte-identical to before: with no foreground index
// active it uses the daemon-wide reindex budget, and while a foreground index is
// active it yields.
func TestResolveChildGOMAXPROCS_BackgroundUnchanged(t *testing.T) {
	t.Run("no foreground active → reindex budget", func(t *testing.T) {
		indexstate.SetIndexConcurrency(0, 0, 2, 0) // foregroundActive=0
		want := ReindexGraphPhaseGOMAXPROCS()
		if n, _ := resolveChildGOMAXPROCS(false, 0); n != want {
			t.Errorf("background (no fg): got %d, want %d (daemon-wide reindex budget)", n, want)
		}
	})
	t.Run("foreground active → yield", func(t *testing.T) {
		indexstate.SetIndexConcurrency(1, 0, 2, 1) // foregroundActive=1
		t.Cleanup(func() { indexstate.SetIndexConcurrency(0, 0, 2, 0) })
		want := BackgroundYieldGOMAXPROCS()
		if n, _ := resolveChildGOMAXPROCS(false, 0); n != want {
			t.Errorf("background (fg active): got %d, want %d (yield)", n, want)
		}
	})
}
