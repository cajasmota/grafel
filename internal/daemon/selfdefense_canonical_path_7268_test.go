package daemon

// selfdefense_canonical_path_7268_test.go — alphabet of IsCanonicalBinaryPath,
// the identity gate the two kill-stale call sites now share (#7268).
//
// The gate answers "is this exec path our binary", and a false positive there
// means SIGTERM to a stranger's process, so the accept side is enumerated
// tightly and the reject side broadly.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

func TestIsCanonicalBinaryPath_7268(t *testing.T) {
	accept := []string{
		"/usr/local/bin/grafel",
		"/tmp/agent-worktree/grafel",
		"/opt/grafel/daemon/bin/grafel",
		"/Users/jane smith/Library/grafel", // spaces are ordinary path characters
		"/opt/GRAFEL",                      // the basename match is case-insensitive
		"/opt/Grafel/Daemon/bin/GRAFEL",
	}
	for _, p := range accept {
		if !IsCanonicalBinaryPath(p) {
			t.Errorf("IsCanonicalBinaryPath(%q) = false, want true", p)
		}
	}

	reject := []string{
		// not our binary — the basename is what decides
		"/Users/jane smith/Library/grafel-daemon-helper/bin/helper",
		"/Users/jane/src/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild",
		"/usr/local/bin/grafel-daemon-old",
		"/opt/grafel/daemon/bin/daemon",
		"/opt/grafel/bin/grafeld",
		"/opt/grafel/bin/mygrafel",
		"/opt/grafel/bin/grafel-mcp",
		"/opt/grafel/bin/grafel.exe", // no extension stripping: see doc
		// not an absolute path — identity is not established
		"grafel",
		"./grafel",
		"../bin/grafel",
		"bin/grafel",
		"",
		// a directory named grafel is not a grafel binary
		"/opt/grafel/",
	}
	for _, p := range reject {
		if IsCanonicalBinaryPath(p) {
			t.Errorf("IsCanonicalBinaryPath(%q) = true, want false", p)
		}
	}
}

// TestIsCanonicalBinaryPath_AgreesWithFindCanonicalDaemon_7268 checks the gate
// against the set it was lifted from, so the two cannot drift: every name in
// canonicalBasenames must be accepted when given as an absolute path.
func TestIsCanonicalBinaryPath_AgreesWithCanonicalBasenames_7268(t *testing.T) {
	if len(canonicalBasenames) == 0 {
		t.Fatal("canonicalBasenames is empty — this test would be vacuous")
	}
	for name := range canonicalBasenames {
		if !IsCanonicalBinaryPath("/usr/local/bin/" + name) {
			t.Errorf("canonicalBasenames contains %q but IsCanonicalBinaryPath rejects it", name)
		}
		if IsCanonicalBinaryPath(name) {
			t.Errorf("IsCanonicalBinaryPath(%q) accepted a bare basename", name)
		}
	}
}

// TestFindCanonicalDaemon_AgreesWithIsCanonicalBinaryPath_TrailingSep_7268
// pins the ONE divergence that made IsCanonicalBinaryPath's doc comment false.
//
// The comment says the gate is "the identity test findCanonicalDaemon has
// applied since #1719, lifted out". It was not lifted: findCanonicalDaemon
// kept its own filepath.IsAbs + canonicalBasenames pair and had no
// trailing-separator guard, so "/opt/grafel/" — a DIRECTORY — was rejected by
// IsCanonicalBinaryPath and ACCEPTED by findCanonicalDaemon as the user's
// canonical daemon. Both halves are asserted below, on the same string, so a
// future edit that re-spells the rule in either place and drops the guard
// fails here rather than re-opening the disagreement silently.
//
// The positive control is not decoration: without it a findCanonicalDaemon
// stubbed to return (0, "") — or a synthetic table the seam never reached —
// would satisfy the "rejects the directory" half for the wrong reason.
func TestFindCanonicalDaemon_AgreesWithIsCanonicalBinaryPath_TrailingSep_7268(t *testing.T) {
	// Built with the platform separator so the fixture IS a trailing-separator
	// path on windows too, rather than passing there for an unrelated reason.
	dir := absFixture("/opt/grafel") + string(filepath.Separator)
	realBin := absFixture("/opt/grafel/bin/grafel")

	if IsCanonicalBinaryPath(dir) {
		t.Errorf("IsCanonicalBinaryPath(%q) = true, want false (it is a directory)", dir)
	}

	withProcs(t, []process.Info{
		{PID: os.Getpid() + 1001, PPID: 1, Name: "grafel", Exe: dir},
	}, nil)
	if pid, exe := findCanonicalDaemon(); pid != 0 || exe != "" {
		t.Errorf("findCanonicalDaemon() = (%d, %q) for directory path %q; want (0, \"\") — "+
			"it must agree with IsCanonicalBinaryPath, which rejects it", pid, exe, dir)
	}

	// Positive control: the same table shape with a real binary path IS found,
	// so the rejection above is the guard talking and not an unreached loop.
	wantPID := os.Getpid() + 1002
	withProcs(t, []process.Info{
		{PID: wantPID, PPID: 1, Name: "grafel", Exe: realBin},
	}, nil)
	if pid, exe := findCanonicalDaemon(); pid != wantPID || exe != realBin {
		t.Fatalf("findCanonicalDaemon() = (%d, %q), want (%d, %q) — the synthetic "+
			"table never reached the classification, so the rejection above graded nothing",
			pid, exe, wantPID, realBin)
	}
}

// TestCanonicalBasenames_IsJustGrafel_7268 is the guard the owning package was
// missing.
//
// canonicalBasenames now decides a KILL: IsCanonicalBinaryPath reads it, and
// both `grafel doctor --kill-stale` (internal/cli) and
// POST /api/diagnostics/kill-stale (internal/dashboard) make it a precondition
// for sending SIGTERM. Adding a name here makes every process with that
// basename signal-eligible from two other packages. Those packages have their
// own #7268 tables that would catch it — but an author widening this map is
// editing THIS file, for the canonical-daemon question, and the alarm has to
// ring where the edit happens. Without this, adding `"node": true` left
// ./internal/daemon fully green.
func TestCanonicalBasenames_IsJustGrafel_7268(t *testing.T) {
	if len(canonicalBasenames) != 1 || !canonicalBasenames["grafel"] {
		t.Fatalf("canonicalBasenames = %v; want exactly {\"grafel\": true}. This set "+
			"gates SIGTERM on two kill-stale paths (internal/cli, internal/dashboard) "+
			"via IsCanonicalBinaryPath — widening it makes every process with the added "+
			"basename kill-eligible. If the widening is intended, update this test "+
			"deliberately and re-score the kill-path tables in both consumers.", canonicalBasenames)
	}
}
