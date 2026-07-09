package extract

import (
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// TestConcurrencyEnvOverride verifies the #3648 emergency throttle:
// GRAFEL_EXTRACT_CONCURRENCY overrides the auto-tuned subprocess fan-out,
// while an explicit CoordinatorConfig.Concurrency still wins over the env var.
func TestConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("GRAFEL_EXTRACT_CONCURRENCY", "1")
	if got := (CoordinatorConfig{}).concurrency(); got != 1 {
		t.Fatalf("env override: concurrency() = %d, want 1", got)
	}

	// Explicit config field takes precedence over the env var.
	if got := (CoordinatorConfig{Concurrency: 3}).concurrency(); got != 3 {
		t.Fatalf("explicit config: concurrency() = %d, want 3", got)
	}

	// Garbage / non-positive values are ignored → fall back to auto-tune.
	t.Setenv("GRAFEL_EXTRACT_CONCURRENCY", "not-a-number")
	auto := (CoordinatorConfig{}).concurrency()
	want := backgroundConcurrency(runtime.NumCPU(), process.TotalMemoryMB())
	if auto != want {
		t.Fatalf("invalid env ignored: concurrency() = %d, want auto %d", auto, want)
	}
}

// TestBackgroundConcurrencyFormula pins the #5692 background-fan-out formula:
// scale with NumCPU (ceiling backgroundConcurrencyCeiling) but never exceed the
// per-child RSS budget (totalMem / perSubprocessRSSBudgetMB). It is IDENTICAL to
// the historical min(NumCPU/2, 4) for <=9-core hosts with ample RAM, only lifts
// above 9 cores, and only tightens when memory is scarce.
func TestBackgroundConcurrencyFormula(t *testing.T) {
	const ampleMem = 65536 // 64 GB — memory clamp never binds
	cases := []struct {
		name   string
		numCPU int
		memMB  int64
		want   int
	}{
		// Low-core hosts: unchanged from the historical formula.
		{"1-core", 1, ampleMem, 1},
		{"2-core", 2, ampleMem, 1},
		{"4-core", 4, ampleMem, 2},
		{"8-core", 8, ampleMem, 4}, // pre-#5692: min(4,4)=4 — unchanged
		{"9-core", 9, ampleMem, 4}, // 9/2=4 — unchanged
		// High-core hosts: the raised ceiling lets fan-out grow past the old 4.
		{"10-core", 10, ampleMem, 5}, // was 4, now 5
		{"16-core", 16, ampleMem, 8},
		{"24-core-hits-ceiling", 24, ampleMem, backgroundConcurrencyCeiling},
		{"64-core-ceiling", 64, ampleMem, backgroundConcurrencyCeiling},
		// Memory clamp: high cores but scarce RAM tightens below the CPU target.
		{"16-core-2GB", 16, 2048, 4},  // 2048/512=4 < cpuTarget 8
		{"16-core-1GB", 16, 1024, 2},  // 1024/512=2
		{"16-core-300MB", 16, 300, 1}, // floors at 1, never 0
		// Unknown memory (<=0): skip the clamp, use the CPU target.
		{"16-core-mem-unknown", 16, 0, 8},
		{"16-core-mem-negative", 16, -1, 8},
		// Degenerate core count clamps to 1.
		{"zero-core", 0, ampleMem, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := backgroundConcurrency(tc.numCPU, tc.memMB); got != tc.want {
				t.Fatalf("backgroundConcurrency(%d, %d) = %d, want %d",
					tc.numCPU, tc.memMB, got, tc.want)
			}
		})
	}
}

// TestExtractGOMAXPROCS verifies the per-subprocess GOMAXPROCS cap and its
// override. Each extract subprocess inherits this value so concurrent children
// cannot collectively saturate the host (#3648 runaway).
func TestExtractGOMAXPROCS(t *testing.T) {
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("default extractGOMAXPROCS() = %d, want 2", got)
	}

	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "1")
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("override extractGOMAXPROCS() = %d, want 1", got)
	}

	// Non-positive / garbage → default.
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "0")
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("zero override ignored: extractGOMAXPROCS() = %d, want 2", got)
	}
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "-4")
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("negative override ignored: extractGOMAXPROCS() = %d, want 2", got)
	}
}

func TestEnvPositiveInt(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"   ":     0,
		"5":       5,
		" 7 ":     7,
		"0":       0,
		"-3":      0,
		"abc":     0,
		"3.5":     0,
		"1000000": 1000000,
	}
	for raw, want := range cases {
		t.Setenv("AG_TEST_ENV_POSINT", raw)
		if got := envPositiveInt("AG_TEST_ENV_POSINT"); got != want {
			t.Errorf("envPositiveInt(%q) = %d, want %d", raw, got, want)
		}
	}
	// Unset var → 0.
	if got := envPositiveInt("AG_TEST_DEFINITELY_UNSET_VAR_3648"); got != 0 {
		t.Errorf("unset var: envPositiveInt() = %d, want 0", got)
	}
}

// TestRebuildGOMAXPROCS verifies the #5135 explicit-rebuild cap and its
// override. Foreground rebuilds run at host speed by default (= NumCPU), and
// GRAFEL_REBUILD_GOMAXPROCS overrides the per-child value.
func TestRebuildGOMAXPROCS(t *testing.T) {
	wantDefault := runtime.NumCPU()
	if wantDefault < 1 {
		wantDefault = 1
	}
	if got := rebuildGOMAXPROCS(); got != wantDefault {
		t.Fatalf("default rebuildGOMAXPROCS() = %d, want host cores %d", got, wantDefault)
	}

	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "6")
	if got := rebuildGOMAXPROCS(); got != 6 {
		t.Fatalf("override rebuildGOMAXPROCS() = %d, want 6", got)
	}

	// Non-positive / garbage → host-cores default.
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "0")
	if got := rebuildGOMAXPROCS(); got != wantDefault {
		t.Fatalf("zero override ignored: rebuildGOMAXPROCS() = %d, want %d", got, wantDefault)
	}
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "garbage")
	if got := rebuildGOMAXPROCS(); got != wantDefault {
		t.Fatalf("garbage override ignored: rebuildGOMAXPROCS() = %d, want %d", got, wantDefault)
	}
}

// TestChildGOMAXPROCSSplit is the core #5135 proof: the SAME env settings yield
// the LOW background cap for a watch/churn reindex and the HIGH rebuild cap for
// an explicit foreground rebuild, dispatched purely on CoordinatorConfig.Interactive.
func TestChildGOMAXPROCSSplit(t *testing.T) {
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "2")
	t.Setenv("GRAFEL_REBUILD_GOMAXPROCS", "9")

	bg := CoordinatorConfig{Interactive: false}.childGOMAXPROCS()
	if bg != 2 {
		t.Fatalf("background childGOMAXPROCS() = %d, want 2 (extract cap)", bg)
	}

	fg := CoordinatorConfig{Interactive: true}.childGOMAXPROCS()
	if fg != 9 {
		t.Fatalf("interactive childGOMAXPROCS() = %d, want 9 (rebuild cap)", fg)
	}

	if bg >= fg {
		t.Fatalf("expected background cap (%d) < interactive cap (%d)", bg, fg)
	}
}

// TestInteractiveConcurrency verifies the #5135 fan-out split: an explicit
// rebuild fans out to NumCPU subprocesses while a background reindex stays at
// the conservative NumCPU/2-capped-at-4 default — unless an explicit
// CoordinatorConfig.Concurrency or GRAFEL_EXTRACT_CONCURRENCY ceiling is set.
func TestInteractiveConcurrency(t *testing.T) {
	// No env override: interactive fans wider than background.
	bg := CoordinatorConfig{Interactive: false}.concurrency()
	fg := CoordinatorConfig{Interactive: true}.concurrency()

	wantFG := runtime.NumCPU()
	if wantFG < 1 {
		wantFG = 1
	}
	if fg != wantFG {
		t.Fatalf("interactive concurrency() = %d, want host cores %d", fg, wantFG)
	}
	if runtime.NumCPU() > 8 && !(fg > bg) {
		t.Fatalf("expected interactive concurrency (%d) > background (%d) on a >8-core host", fg, bg)
	}

	// An operator-set ceiling (GRAFEL_EXTRACT_CONCURRENCY) is honored on
	// BOTH paths — even interactive rebuilds respect a contended-host cap.
	t.Setenv("GRAFEL_EXTRACT_CONCURRENCY", "1")
	if got := (CoordinatorConfig{Interactive: true}).concurrency(); got != 1 {
		t.Fatalf("interactive should honor GRAFEL_EXTRACT_CONCURRENCY ceiling: got %d, want 1", got)
	}

	// An explicit config field still wins over everything.
	if got := (CoordinatorConfig{Interactive: true, Concurrency: 3}).concurrency(); got != 3 {
		t.Fatalf("explicit Concurrency should win: got %d, want 3", got)
	}
}
