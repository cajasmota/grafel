package extract

import (
	"runtime"
	"testing"
)

// TestConcurrencyEnvOverride verifies the #3648 emergency throttle:
// ARCHIGRAPH_EXTRACT_CONCURRENCY overrides the auto-tuned subprocess fan-out,
// while an explicit CoordinatorConfig.Concurrency still wins over the env var.
func TestConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EXTRACT_CONCURRENCY", "1")
	if got := (CoordinatorConfig{}).concurrency(); got != 1 {
		t.Fatalf("env override: concurrency() = %d, want 1", got)
	}

	// Explicit config field takes precedence over the env var.
	if got := (CoordinatorConfig{Concurrency: 3}).concurrency(); got != 3 {
		t.Fatalf("explicit config: concurrency() = %d, want 3", got)
	}

	// Garbage / non-positive values are ignored → fall back to auto-tune.
	t.Setenv("ARCHIGRAPH_EXTRACT_CONCURRENCY", "not-a-number")
	auto := (CoordinatorConfig{}).concurrency()
	want := runtime.NumCPU() / 2
	if want < 1 {
		want = 1
	}
	if want > 4 {
		want = 4
	}
	if auto != want {
		t.Fatalf("invalid env ignored: concurrency() = %d, want auto %d", auto, want)
	}
}

// TestExtractGOMAXPROCS verifies the per-subprocess GOMAXPROCS cap and its
// override. Each extract subprocess inherits this value so concurrent children
// cannot collectively saturate the host (#3648 runaway).
func TestExtractGOMAXPROCS(t *testing.T) {
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("default extractGOMAXPROCS() = %d, want 2", got)
	}

	t.Setenv("ARCHIGRAPH_EXTRACT_GOMAXPROCS", "1")
	if got := extractGOMAXPROCS(); got != 1 {
		t.Fatalf("override extractGOMAXPROCS() = %d, want 1", got)
	}

	// Non-positive / garbage → default.
	t.Setenv("ARCHIGRAPH_EXTRACT_GOMAXPROCS", "0")
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("zero override ignored: extractGOMAXPROCS() = %d, want 2", got)
	}
	t.Setenv("ARCHIGRAPH_EXTRACT_GOMAXPROCS", "-4")
	if got := extractGOMAXPROCS(); got != 2 {
		t.Fatalf("negative override ignored: extractGOMAXPROCS() = %d, want 2", got)
	}
}

func TestEnvPositiveInt(t *testing.T) {
	cases := map[string]int{
		"":          0,
		"   ":       0,
		"5":         5,
		" 7 ":       7,
		"0":         0,
		"-3":        0,
		"abc":       0,
		"3.5":       0,
		"1000000":   1000000,
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
