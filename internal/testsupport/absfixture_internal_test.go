package testsupport

// absfixture_internal_test.go — the windows-branch decision, graded on every
// platform through the injected predicate.
//
// THE DOUBLE IS A MODEL, NOT A PROOF. winIsAbs below models windows' rule
// (a "C:" volume followed by a separator) purely so the COMPOSITION in
// absFixtureFor can be exercised off windows. It deliberately does not attempt
// to reproduce volumeNameLen — UNC paths, device paths, drive-relative forms —
// because a double that claimed to be the real predicate would be the
// "test re-implements the rule it grades" defect in a new costume. Agreement
// between this model and real filepath.IsAbs is established by the windows CI
// leg and by nothing here.

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

// TestFixtureVolume_IsNonEmpty pins the other half: a volume of "" would make
// AbsFixture a silent no-op on windows, and every table would go back to
// grading nothing there while staying green everywhere.
func TestFixtureVolume_IsNonEmpty(t *testing.T) {
	if v := fixtureVolume(); v == "" {
		t.Fatal("fixtureVolume() is empty — AbsFixture would be a no-op on windows")
	}
}
