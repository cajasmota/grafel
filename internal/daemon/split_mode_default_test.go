package daemon_test

// split_mode_default_test.go — ADR-0024 PR6 (epic #5729): pins the flipped
// default for daemon.SplitModeEnabled(). Split mode is now ON BY DEFAULT;
// GRAFEL_SPLIT_MODE=0 (or "false"/"off"/"no", case-insensitive, trimmed) is
// the only escape hatch back to monolith. Any other value — including unset,
// empty, or an unrecognized string — is treated as split-mode ON.
//
// This package's TestMain (daemon_test.go) pins GRAFEL_SPLIT_MODE=0 as the
// process-level default so the broad daemon test suite keeps running
// monolith without every test needing to opt out individually. Each case
// below sets the env var explicitly via t.Setenv, so the TestMain default is
// irrelevant here — this test exercises SplitModeEnabled()'s parsing logic
// directly, not the suite-wide test default.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

func TestSplitModeEnabled_DefaultOnEscapeHatchOff(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"unset/empty defaults ON", "", true},
		{"whitespace-only defaults ON", "   ", true},
		{"explicit 1 is ON", "1", true},
		{"explicit true is ON", "true", true},
		{"explicit TRUE (case-insensitive) is ON", "TRUE", true},
		{"unrecognized value defaults ON", "bogus", true},
		{"explicit 0 is the escape hatch OFF", "0", false},
		{"explicit false is the escape hatch OFF", "false", false},
		{"explicit FALSE (case-insensitive) is OFF", "FALSE", false},
		{"explicit off is the escape hatch OFF", "off", false},
		{"explicit no is the escape hatch OFF", "no", false},
		{"explicit OFF with surrounding whitespace is OFF", "  off  ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(daemon.SplitModeEnvVar, tc.env)
			if got := daemon.SplitModeEnabled(); got != tc.want {
				t.Errorf("SplitModeEnabled() with %s=%q = %v, want %v", daemon.SplitModeEnvVar, tc.env, got, tc.want)
			}
		})
	}
}
