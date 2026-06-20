package main

import "testing"

// TestResolveDaemonGOMAXPROCS verifies the #5135 native in-process daemon CPU
// knob: GRAFEL_DAEMON_GOMAXPROCS caps the daemon's own Go-runtime parallelism.
//
// Resource-safe default (v0.1.1): when the env var is unset/blank/invalid the
// resolver now falls back to ~half the host cores (6 on a 12-core host) rather
// than returning 0 ("no cap"). An explicit value below the host wins; a value
// at/above the host count returns 0 because the Go default is already correct.
// envPositiveInt2 treats an empty or whitespace-only value as unset, so the
// empty-string cases exercise the default branch deterministically.
func TestResolveDaemonGOMAXPROCS(t *testing.T) {
	const host = 12
	const halfDefault = host / 2 // resource-safe default when nothing pinned

	cases := []struct {
		name string
		env  string
		want int
	}{
		{"empty-defaults-half", "", halfDefault},
		{"blank-defaults-half", "   ", halfDefault},
		{"valid-below-host", "3", 3},
		{"one", "1", 1},
		{"equal-host-noop", "12", 0},
		{"above-host-noop", "20", 0},
		{"zero-defaults-half", "0", halfDefault},
		{"negative-defaults-half", "-4", halfDefault},
		{"garbage-defaults-half", "abc", halfDefault},
		{"float-defaults-half", "2.5", halfDefault},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", tc.env)
			if got := resolveDaemonGOMAXPROCS(host); got != tc.want {
				t.Fatalf("resolveDaemonGOMAXPROCS(env=%q, host=%d) = %d, want %d", tc.env, host, got, tc.want)
			}
		})
	}

	// host=0 (unknown core count) must not panic. An explicit cap is honored
	// (no host ceiling to compare against); with the env unset the half-cores
	// default can't be computed so the resolver leaves the Go default (0).
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "4")
	if got := resolveDaemonGOMAXPROCS(0); got != 4 {
		t.Fatalf("resolveDaemonGOMAXPROCS(host=0) = %d, want 4", got)
	}
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	if got := resolveDaemonGOMAXPROCS(0); got != 0 {
		t.Fatalf("resolveDaemonGOMAXPROCS(host=0, env unset) = %d, want 0 (unknown cores → no cap)", got)
	}
}

// TestDefaultDaemonGOMAXPROCS covers the half-cores default helper directly.
func TestDefaultDaemonGOMAXPROCS(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 2: 1, 3: 1, 4: 2, 12: 6, 11: 5}
	for host, want := range cases {
		if got := defaultDaemonGOMAXPROCS(host); got != want {
			t.Errorf("defaultDaemonGOMAXPROCS(%d) = %d, want %d", host, got, want)
		}
	}
}

// TestEnvPositiveInt2 covers the local env helper used by the daemon CPU knob.
func TestEnvPositiveInt2(t *testing.T) {
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
		t.Setenv("AG_TEST_ENV_POSINT2", raw)
		if got := envPositiveInt2("AG_TEST_ENV_POSINT2"); got != want {
			t.Errorf("envPositiveInt2(%q) = %d, want %d", raw, got, want)
		}
	}
}
