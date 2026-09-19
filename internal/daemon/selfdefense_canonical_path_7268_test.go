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
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

func TestIsCanonicalBinaryPath_7268(t *testing.T) {
	// PLATFORM. The gate opens with filepath.IsAbs, whose answer is
	// GOOS-dependent, so every ACCEPT row must be made absolute for the running
	// platform or the whole accept side inverts on windows — while the reject
	// side would pass there for the unrelated reason that nothing looked
	// absolute. See absFixture / testsupport.AbsFixture.
	accept := []string{
		absFixture("/usr/local/bin/grafel"),
		absFixture("/opt/grafel/daemon/bin/grafel"),
		absFixture("/Users/jane smith/Library/grafel"), // spaces are ordinary path characters
		absFixture("/opt/GRAFEL"),                      // the basename match is case-insensitive
		absFixture("/opt/Grafel/Daemon/bin/GRAFEL"),
	}

	// UNIX-ONLY ACCEPT ROWS. A /tmp-prefix fixture cannot be absolutised — a
	// volume prefix moves it off the boundary the sibling guards test on — so
	// it is NOT absolute on windows and IsCanonicalBinaryPath correctly rejects
	// it there. Asserting acceptance anyway is how round 4 left the windows leg
	// red: the file printed a diagnostic saying the row grades nothing here and
	// then called t.Errorf regardless. A DIAGNOSTIC IS NOT A SKIP.
	//
	// The row is still worth having where it can run: the gate must accept a
	// /tmp-installed grafel, because IsCanonicalBinaryPath says nothing about
	// WHERE a binary lives.
	const unixOnlyAccept = 1
	acceptUnixOnly := []string{
		"/tmp/agent-worktree/grafel",
	}
	if len(acceptUnixOnly) != unixOnlyAccept {
		t.Fatalf("%d unix-only accept rows, want %d — change the constant deliberately",
			len(acceptUnixOnly), unixOnlyAccept)
	}
	if runtime.GOOS != "windows" {
		accept = append(accept, acceptUnixOnly...)
	}
	// A count floor, because a skipped row reports SUCCESS: assert how many
	// rows this platform actually checks, so a widened skip is loud.
	wantAccept := 5 + unixOnlyAccept
	if runtime.GOOS == "windows" {
		wantAccept = 5
	}
	if len(accept) != wantAccept {
		t.Fatalf("%d accept rows on %s, want %d", len(accept), runtime.GOOS, wantAccept)
	}
	for _, p := range accept {
		if !IsCanonicalBinaryPath(p) {
			// Diagnostic for the windows leg: the overwhelmingly likely cause
			// of an accept row failing there is that it is not absolute, which
			// means the row grades nothing rather than that the gate is wrong.
			extra := ""
			if !filepath.IsAbs(p) {
				extra = " — and it is NOT absolute on " + runtime.GOOS +
					", so this row grades nothing here; route it through absFixture"
			}
			t.Errorf("IsCanonicalBinaryPath(%q) = false, want true%s", p, extra)
		}
	}

	reject := []string{
		// not our binary — the basename is what decides. Absolutised so it is
		// the BASENAME that rejects each of these on every platform; left bare,
		// a windows run rejects them all at filepath.IsAbs and this block
		// grades nothing there.
		absFixture("/Users/jane smith/Library/grafel-daemon-helper/bin/helper"),
		absFixture("/Users/jane/src/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild"),
		absFixture("/usr/local/bin/grafel-daemon-old"),
		absFixture("/opt/grafel/daemon/bin/daemon"),
		absFixture("/opt/grafel/bin/grafeld"),
		absFixture("/opt/grafel/bin/mygrafel"),
		absFixture("/opt/grafel/bin/grafel-mcp"),
		absFixture("/opt/grafel/bin/grafel.exe"), // no extension stripping: see doc
		// not an absolute path — identity is not established. Deliberately NOT
		// absolutised: being relative is the property under test.
		"grafel",
		"./grafel",
		"../bin/grafel",
		"bin/grafel",
		"",
		// a directory named grafel is not a grafel binary. Built with the
		// platform separator so it is a trailing-separator path on windows too.
		absFixture("/opt/grafel") + string(filepath.Separator),
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
		if !IsCanonicalBinaryPath(absFixture("/usr/local/bin/" + name)) {
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
