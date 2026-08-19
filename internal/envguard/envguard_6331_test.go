package envguard

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// envOf builds an Env from a map.
func envOf(m map[string]string) Env {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func homeKey() string {
	if runtime.GOOS == "windows" {
		return "USERPROFILE"
	}
	return "HOME"
}

// TestEnvRootLiteralMatchesDaemon pins the duplicated literal against the
// daemon package's own constant, so a rename there cannot silently disarm this
// guard.
func TestEnvRootLiteralMatchesDaemon(t *testing.T) {
	if EnvDaemonRoot != daemon.EnvRoot {
		t.Fatalf("EnvDaemonRoot = %q, daemon.EnvRoot = %q — the guard would read the wrong variable", EnvDaemonRoot, daemon.EnvRoot)
	}
}

// TestCheckMatrix is the decision table from the package doc, executed.
func TestCheckMatrix(t *testing.T) {
	const realHome = "/Users/real"
	sandbox := "/tmp/sandbox"

	cases := []struct {
		name string
		env  map[string]string
		want Verdict
	}{
		{
			// Normal production: nothing redirected. This is the control that
			// stops the guard becoming a blanket refusal.
			name: "neither set",
			env:  map[string]string{homeKey(): realHome},
			want: VerdictOK,
		},
		{
			// The #6331 incident shape: private store, LIVE daemon socket.
			name: "GRAFEL_HOME only, real HOME",
			env:  map[string]string{homeKey(): realHome, EnvGrafelHome: sandbox},
			want: VerdictRefuse,
		},
		{
			// Still partial even with HOME redirected: the socket is the real
			// one either way, because DefaultLayout reads neither HOME nor
			// GRAFEL_HOME — only GRAFEL_DAEMON_ROOT.
			name: "GRAFEL_HOME only, redirected HOME",
			env:  map[string]string{homeKey(): sandbox, EnvGrafelHome: sandbox},
			want: VerdictRefuse,
		},
		{
			// The #6134 shape: private socket, LIVE store (which the daemon's
			// startup tail relocates and prunes).
			name: "GRAFEL_DAEMON_ROOT only, real HOME",
			env:  map[string]string{homeKey(): realHome, EnvDaemonRoot: sandbox},
			want: VerdictRefuse,
		},
		{
			name: "GRAFEL_DAEMON_ROOT only, redirected HOME",
			env:  map[string]string{homeKey(): sandbox, EnvDaemonRoot: sandbox},
			want: VerdictRefuse,
		},
		{
			// scripts/phase-b-bench.sh's shape. Store + daemon plane isolated,
			// HOME-derived config paths not. WARN, not refuse — see the package
			// doc for why this leg is the one that does not fail.
			name: "both set, real HOME",
			env:  map[string]string{homeKey(): realHome, EnvGrafelHome: sandbox, EnvDaemonRoot: sandbox},
			want: VerdictWarn,
		},
		{
			// Full isolation, i.e. what testsupport.IsolateHome produces.
			name: "both set, redirected HOME",
			env:  map[string]string{homeKey(): sandbox, EnvGrafelHome: sandbox, EnvDaemonRoot: sandbox},
			want: VerdictOK,
		},
		{
			// Whitespace-only IS "set", because that is what every consumer
			// thinks. registry.HomeDir, paths_unix.go and paths_windows.go all
			// test `os.Getenv(...) != ""` with no trimming, so GRAFEL_HOME="   "
			// resolves the store into a literal three-space directory while
			// daemon.DefaultLayout still points at the live socket — the
			// #6331 incident shape exactly. A guard that trims here is
			// checking a different environment than the program runs in.
			name: "GRAFEL_HOME whitespace only",
			env:  map[string]string{homeKey(): realHome, EnvGrafelHome: "   "},
			want: VerdictRefuse,
		},
		{
			name: "GRAFEL_DAEMON_ROOT whitespace only",
			env:  map[string]string{homeKey(): realHome, EnvDaemonRoot: " "},
			want: VerdictRefuse,
		},
		{
			// Escape hatch downgrades refuse -> warn, never to silence.
			name: "partial with escape hatch",
			env: map[string]string{
				homeKey(): realHome, EnvGrafelHome: sandbox, EnvAllowPartial: "1",
			},
			want: VerdictWarn,
		},
		{
			name: "escape hatch does not accept arbitrary truthy values",
			env: map[string]string{
				homeKey(): realHome, EnvGrafelHome: sandbox, EnvAllowPartial: "true",
			},
			want: VerdictRefuse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Check(envOf(tc.env), realHome)
			if got.Verdict != tc.want {
				t.Fatalf("Check = %v (msg %q), want %v", got.Verdict, got.Message, tc.want)
			}
			if got.Verdict != VerdictOK && strings.TrimSpace(got.Message) == "" {
				t.Fatalf("verdict %v carries an empty message", got.Verdict)
			}
			if got.Verdict == VerdictOK && got.Message != "" {
				t.Fatalf("VerdictOK carries a message: %q", got.Message)
			}
		})
	}
}

// TestRefusalMessageNamesTheMissingVariable is the operator-facing contract:
// the incident happened because nothing told the operator WHICH variable was
// missing. A refusal that does not name it repeats the bug.
func TestRefusalMessageNamesTheMissingVariable(t *testing.T) {
	const realHome = "/Users/real"

	res := Check(envOf(map[string]string{homeKey(): realHome, EnvGrafelHome: "/tmp/s"}), realHome)
	if res.Verdict != VerdictRefuse {
		t.Fatalf("verdict = %v, want refuse", res.Verdict)
	}
	for _, want := range []string{EnvDaemonRoot, EnvGrafelHome, EnvAllowPartial, "HOME="} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("refusal message does not mention %q:\n%s", want, res.Message)
		}
	}

	res = Check(envOf(map[string]string{homeKey(): realHome, EnvDaemonRoot: "/tmp/s"}), realHome)
	if res.Verdict != VerdictRefuse {
		t.Fatalf("verdict = %v, want refuse", res.Verdict)
	}
	if !strings.Contains(res.Message, EnvGrafelHome) {
		t.Errorf("refusal message does not name %s:\n%s", EnvGrafelHome, res.Message)
	}
}

// TestUnknownRealHomeSkipsOnlyTheHomeLeg: when the OS-level home cannot be
// resolved, the partial-isolation leg must still fire (it does not depend on
// HOME at all) while the both-set leg falls open rather than guessing.
func TestUnknownRealHomeSkipsOnlyTheHomeLeg(t *testing.T) {
	partial := envOf(map[string]string{homeKey(): "/Users/real", EnvGrafelHome: "/tmp/s"})
	if got := Check(partial, "").Verdict; got != VerdictRefuse {
		t.Fatalf("partial isolation with unknown real home = %v, want refuse", got)
	}
	both := envOf(map[string]string{homeKey(): "/Users/real", EnvGrafelHome: "/tmp/s", EnvDaemonRoot: "/tmp/s"})
	if got := Check(both, "").Verdict; got != VerdictOK {
		t.Fatalf("both-set with unknown real home = %v, want OK", got)
	}
}

// TestRealUserHomeIgnoresHOME is the property RealUserHome exists for: under a
// redirected HOME it must still report the genuine home, or the both-set leg
// answers "HOME is fine" every single time.
func TestRealUserHomeIgnoresHOME(t *testing.T) {
	before := RealUserHome()
	if before == "" {
		t.Skip("user.Current() unavailable on this runner")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	got := RealUserHome()
	if got != before {
		t.Fatalf("RealUserHome() moved with $HOME: %q -> %q", before, got)
	}
	if sameDir(got, filepath.Clean(tmp)) {
		t.Fatalf("RealUserHome() returned the redirected home %q", tmp)
	}
}

// TestIsolateHomeProducesVerdictOK proves the guard does not fire against the
// project's own sanctioned isolation helper. If this ever refuses, every
// isolated test in the tree is one wiring change away from being unrunnable.
func TestIsolateHomeProducesVerdictOK(t *testing.T) {
	real := RealUserHome()
	if real == "" {
		t.Skip("user.Current() unavailable on this runner")
	}
	testsupport.IsolateHome(t)
	if got := Check(lookupEnvFunc, real); got.Verdict != VerdictOK {
		t.Fatalf("testsupport.IsolateHome yields %v (%s), want VerdictOK", got.Verdict, got.Message)
	}
}

// TestSetnessMatchesConsumers pins the guard's notion of "set" to the
// consumers' notion of "set".
//
// The consumers are all bare `!= ""` checks with no trimming:
//
//	internal/registry/registry.go   if override := os.Getenv("GRAFEL_HOME"); override != ""
//	internal/daemon/paths_unix.go   if root := os.Getenv(EnvRoot); root != ""
//	internal/daemon/paths_windows.go  same
//
// If the guard normalises a value the consumers do not, the two disagree about
// whether the environment is redirected, and the guard waves through exactly
// the state it exists to refuse.
func TestSetnessMatchesConsumers(t *testing.T) {
	const realHome = "/Users/real"
	values := []string{"", " ", "   ", "\t", "\n", "/tmp/sandbox", " /tmp/sandbox "}

	for _, v := range values {
		v := v
		t.Run("GRAFEL_HOME="+strconv.Quote(v), func(t *testing.T) {
			consumerSaysSet := v != "" // verbatim consumer predicate
			got := Check(envOf(map[string]string{homeKey(): realHome, EnvGrafelHome: v}), realHome)
			guardSaysSet := got.Verdict == VerdictRefuse
			if guardSaysSet != consumerSaysSet {
				t.Fatalf("GRAFEL_HOME=%q: consumers treat it as set=%v, guard treats it as set=%v (verdict %v)",
					v, consumerSaysSet, guardSaysSet, got.Verdict)
			}
		})
	}
}
