package extract

import (
	"runtime"
	"testing"
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
	want := backgroundConcurrency(runtime.NumCPU())
	if auto != want {
		t.Fatalf("invalid env ignored: concurrency() = %d, want auto %d", auto, want)
	}
}

// TestBackgroundConcurrencyCap pins the background-fan-out default under the
// #5960 core-budget policy: the fanout is the 25%-of-machine budget
// (max(1, NumCPU/4)) divided by the per-child GOMAXPROCS (default 1 since
// #5960), so concurrency × GOMAXPROCS never exceeds the budget.
//
// The daemon reindexes in the background on the developer's own box while they
// work, so background indexing gets a quarter of the machine and no more. Unlike
// the previous flat "min(NumCPU/2, 4)" cap (which drew 4 × GOMAXPROCS 2 = ~8
// threads on a 12-core host — the #5960 bug), the budget now scales with the
// host: a 32-core workstation indexes faster than a 12-core laptop instead of
// being pinned to the same flat 4.
//
// History: #5692 briefly lifted this to NumCPU/2 with a ceiling of 12; that
// saturated high-core dev boxes and was reverted. Power users who want more
// cores still opt in via GRAFEL_EXTRACT_CONCURRENCY (tested separately).
func TestBackgroundConcurrencyCap(t *testing.T) {
	cases := []struct {
		name   string
		numCPU int
		want   int
	}{
		{"1-core", 1, 1}, // floor: never 0 subprocesses
		{"2-core", 2, 1},
		{"4-core", 4, 1},   // 4/4 = 1
		{"8-core", 8, 2},   // 8/4 = 2
		{"12-core", 12, 3}, // matches the old static <=3-core rule
		{"16-core", 16, 4},
		{"32-core", 32, 8},  // bigger host → proportionally faster indexing
		{"64-core", 64, 16}, // still only 25% of the box
		{"zero-core", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := backgroundConcurrency(tc.numCPU); got != tc.want {
				t.Fatalf("backgroundConcurrency(%d) = %d, want %d",
					tc.numCPU, got, tc.want)
			}
		})
	}
}

// TestBackgroundConcurrencyShrinksUnderGOMAXPROCSOverride proves the budget
// invariant holds when an operator raises the per-child GOMAXPROCS: the derived
// default fanout divides down so the product stays inside the budget, and it
// never reaches 0 (which would stall indexing entirely).
func TestBackgroundConcurrencyShrinksUnderGOMAXPROCSOverride(t *testing.T) {
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "2")
	// 32-core host: budget 8, GOMAXPROCS 2 → 4 children × 2 = 8 threads.
	if got := backgroundConcurrency(32); got != 4 {
		t.Fatalf("backgroundConcurrency(32) with GOMAXPROCS=2 = %d, want 4", got)
	}
	// 4-core host: budget 1, GOMAXPROCS 2 → floored at 1 child, not 0.
	if got := backgroundConcurrency(4); got != 1 {
		t.Fatalf("backgroundConcurrency(4) with GOMAXPROCS=2 = %d, want 1 (floor)", got)
	}
}

// TestExtractGOMAXPROCS verifies the per-subprocess GOMAXPROCS cap and its
// override. Each extract subprocess inherits this value so concurrent children
// cannot collectively saturate the host (#3648 runaway).
func TestExtractGOMAXPROCS(t *testing.T) {
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("default extractGOMAXPROCS() = %d, want 1 (#5960)", got)
	}

	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "3")
	if got := extractGOMAXPROCS(); got != 3 {
		t.Fatalf("override extractGOMAXPROCS() = %d, want 3", got)
	}

	// Non-positive / garbage → default.
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "0")
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("zero override ignored: extractGOMAXPROCS() = %d, want 1", got)
	}
	t.Setenv("GRAFEL_EXTRACT_GOMAXPROCS", "-4")
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("negative override ignored: extractGOMAXPROCS() = %d, want 1", got)
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
// rebuild fans out to NumCPU subprocesses while a background reindex stays
// inside the 25%-of-machine core budget (#5960) — unless an explicit
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
