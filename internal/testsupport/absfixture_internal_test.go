package testsupport

// absfixture_internal_test.go — the windows-branch decision, graded on every
// platform through the injected predicate.
//
// THE DOUBLE IS A MODEL, NOT A PROOF. winIsAbs below models windows' rule
// (a "C:" volume followed by a separator) purely so the COMPOSITION in
// absFixtureFor can be exercised off windows. It deliberately does not attempt
// to reproduce volumeNameLen — UNC and device roots — because a double that
// claimed to be the real predicate would be the "test re-implements the rule it
// grades" defect in a new costume. Agreement between this model and real
// filepath.IsAbs is established by the windows CI leg and by nothing here.
//
// THE DIRECTION OF THE DIVERGENCE IS THE REASSURING PART, and "does not
// reproduce volumeNameLen" does not say which way it runs. The model is
// STRICTER than the real predicate: it rejects exactly one class the real one
// accepts — UNC and device roots (`\\server\share\x`, `\\?\C:\x`) — and
// accepts nothing the real one rejects. A stricter model can only leave a
// correct case UNGRADED; it cannot make a passing assertion here into a red
// windows leg. (There is no divergence on non-letter drive letters: Go
// deliberately does not enforce A-Z.)

import (
	"strings"
	"testing"
)

func winIsAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

func TestAbsFixtureFor_WindowsBranch(t *testing.T) {
	const vol = "C:"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative unix-rooted fixture gains exactly one volume", "/opt/grafel/bin/grafel", "C:/opt/grafel/bin/grafel"},
		{"already absolute is returned untouched", "C:/opt/grafel/bin/grafel", "C:/opt/grafel/bin/grafel"},
		{"already absolute with a backslash is untouched", `C:\opt\grafel\grafel`, `C:\opt\grafel\grafel`},
		// SEPARATES THE TWO HALVES OF THE GUARD. Every other absolute fixture
		// here carries the same volume the helper would graft on, so isAbs and
		// the volume-prefix test both fire and each masks the other — deleting
		// isAbs left this table green (measured). A path that is absolute on a
		// DIFFERENT volume is accepted by isAbs alone, so it is the only row
		// that grades that half.
		{"absolute on another volume is untouched", "D:/opt/grafel/bin/grafel", "D:/opt/grafel/bin/grafel"},
		{"a bare relative name still gets the volume", "grafel", "C:grafel"},
		{"empty", "", "C:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := absFixtureFor(winIsAbs, vol, tc.in); got != tc.want {
				t.Errorf("absFixtureFor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestAbsFixtureFor_IsIdempotent is the property BLOCKING 1 was about, now
// observable off windows: a second application must change nothing, so a shared
// fixture variable routed twice does not become "C:C:/usr/local/bin/grafel".
func TestAbsFixtureFor_IsIdempotent(t *testing.T) {
	const vol = "C:"
	for _, p := range []string{
		"/usr/local/bin/grafel",
		"/opt/grafel/daemon/bin/grafel",
		"/tmp/agent-worktree-1/grafel",
		"grafel",
		"",
		"D:/opt/grafel/bin/grafel", // absolute on a volume the helper would not graft
	} {
		once := absFixtureFor(winIsAbs, vol, p)
		twice := absFixtureFor(winIsAbs, vol, once)
		if once != twice {
			t.Errorf("not idempotent for %q: once=%q twice=%q", p, once, twice)
		}
		// The failure this pins is specifically a doubled volume.
		if strings.HasPrefix(twice, vol+vol) {
			t.Errorf("double application produced a doubled volume: %q", twice)
		}
	}
}

// TestFixtureVolume_IsUsable pins the other half.
//
// Emptiness was already pinned: a volume of "" makes AbsFixture a silent no-op
// on windows and every table goes back to grading nothing there while staying
// green everywhere. WELL-FORMEDNESS was not, and nothing on any leg graded it:
// mutating the fallback from "C:" to "C" was ALIVE, because on unix the
// fallback is the only branch ever taken and a malformed-but-non-empty value
// satisfies a non-empty check, while on a normal windows runner the fallback is
// not reached at all.
//
// THE FALLBACK IS REACHABLE ON WINDOWS, so this is not an equivalent mutant.
// os.tempDir() (os/file_windows.go) DISCARDS getTempPath's error — `n, _ =
// getTempPath(...)` — and returns "" when n == 0, and filepath.VolumeName("")
// is "". GetTempPathW itself returns TMP, then TEMP, then USERPROFILE without
// validating any of them, so a relative TMP yields a relative path whose
// volumeNameLen is 0. Either route lands on the fallback.
//
// Asserted through the same model the rest of this file uses, which is round
// 6's own thesis applied to the sibling function: absFixtureFor got its
// platform-dependent input injected so the decision is gradable off windows;
// fixtureVolume did not.
func TestFixtureVolume_IsUsable(t *testing.T) {
	v := fixtureVolume()
	if v == "" {
		t.Fatal("fixtureVolume() is empty — AbsFixture would be a no-op on windows")
	}
	// A usable volume is one that yields an ABSOLUTE path when a rooted
	// remainder is appended to it. "C" satisfies non-emptiness and fails this.
	if !winIsAbs(v + `\x`) {
		t.Fatalf("fixtureVolume() = %q is not a usable volume: %q is not absolute under the "+
			"windows rule, so AbsFixture would produce fixtures the identity gate rejects",
			v, v+`\x`)
	}
}
