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

// TestBackgroundConcurrencyCap pins the conservative background-fan-out default:
// min(NumCPU/2, 4). This cap is INTENTIONALLY low — the daemon reindexes in the
// background on the developer's own box while they work, and each child also
// runs GOMAXPROCS=2, so the effective draw is ~concurrency × 2 threads. A low
// cap leaves CPU headroom so background indexing never hangs the machine
// (#3648). Index speed is not the priority; box responsiveness is. Power users
// who want more cores opt in via GRAFEL_EXTRACT_CONCURRENCY (tested separately).
//
// #5692 briefly lifted this to a NumCPU/2 formula with a ceiling of 12 (only
// memory-clamped); that saturated high-core dev boxes and was reverted. The cap
// stays flat at 4 by default regardless of host core count.
func TestBackgroundConcurrencyCap(t *testing.T) {
	cases := []struct {
		name   string
		numCPU int
		want   int
	}{
		{"1-core", 1, 1},
		{"2-core", 2, 1},
		{"4-core", 4, 2},
		{"8-core", 8, 4},   // min(4, 4) = 4
		{"9-core", 9, 4},   // 9/2 = 4
		{"10-core", 10, 4}, // restored default: capped at 4, NOT 5 (#5692 lift reverted)
		{"16-core", 16, 4}, // high-core host still capped at 4 by default
		{"64-core", 64, 4}, // no ceiling-12 lift; stays conservative
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
