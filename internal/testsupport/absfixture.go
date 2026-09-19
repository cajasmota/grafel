package testsupport

// absfixture.go — the one copy of "make a unix-style fixture path absolute for
// the platform the test is running on".
//
// # Why this exists as shared code
//
// Any guard built on filepath.IsAbs — daemon.IsCanonicalBinaryPath is the one
// that motivated this, and it gates SIGTERM on two kill-stale paths (#7268) —
// answers differently on windows, where a path needs a VOLUME to be absolute.
// "/usr/local/bin/grafel" is absolute on unix and NOT absolute on windows, so a
// table of unix literals does not grade the guard on windows: every row passes
// there for the unrelated reason that every path looked relative. A table of
// FORBIDDEN rows keeps passing while grading nothing, and a table with required
// rows inverts and goes red.
//
// internal/daemon learned this on #7211 and grew a private absFixture; #7268
// then grew two more private copies, in internal/cli and internal/dashboard,
// before anyone noticed the first one. Three copies of a platform helper is how
// the drift starts, and this package's own fifo_unix.go says why that is worth
// a shared home: "Fourteen copies of the same helper is how the ORIGINAL defect
// happened."
//
// # Why filepath.Join is not the answer
//
// filepath.Join("/", "usr", "local") yields `\usr\local` on windows. That still
// has no volume and is still not absolute, so it fixes nothing while looking
// like it did. A volume is required, and one is taken from os.TempDir(), which
// is absolute on every platform.
//
// # What it deliberately does NOT do
//
// It does not make a /tmp-PREFIX fixture portable, and must not be used for
// one. A guard that asks strings.HasPrefix(exe, "/tmp/") is a byte comparison
// against a literal; prefixing a volume moves the fixture off the boundary
// entirely, turning a boundary row into an unrelated row that still passes. A
// /tmp-prefix row is therefore ungradable on windows and the honest thing is to
// say so at the row, not to absolutise it and claim coverage. See the /tmp*
// rows in internal/cli's and internal/dashboard's kill-stale tables.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AbsFixture returns path unchanged on unix, and prefixes a volume on windows
// so filepath.IsAbs(AbsFixture(p)) is true on every platform for a p that is
// absolute on unix.
//
// It is for test fixtures only. Pass it a unix-style rooted path such as
// "/opt/grafel/bin/grafel"; a relative path is returned with a volume glued on
// and stays relative on unix, which is not a meaningful fixture either way.
//
// # It is IDEMPOTENT, and that is a requirement rather than a nicety
//
// A fixture reaches this function twice as soon as one variable is shared
// between a table row and the value the row is compared against — which is
// exactly the shape both identity tables have:
//
//	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")   // once
//	{name: "self", exe: selfExe, ...}                            // routed again
//
// Without the guard below the second application yields "C:C:/usr/local/bin/
// grafel", which filepath.IsAbs REJECTS on windows: volumeNameLen is 2, and
// path[2:] then starts with 'C' rather than a separator. The row would fail on
// the windows leg — as a confusing "not absolute" fatal, on a path that visibly
// starts with a volume.
//
// Guarding here retires the whole class. The alternative considered and
// rejected was flagging the two rows that happen to double-route today: that
// fixes the instances and leaves the class open for the next shared fixture
// variable, and this branch has already been bitten twice by pinning one axis
// and leaving its neighbour free.
func AbsFixture(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	return absFixtureFor(filepath.IsAbs, fixtureVolume(), path)
}

// absFixtureFor is the windows-branch DECISION, with both of its
// platform-dependent inputs passed explicitly.
//
// It is split out for the reason internal/install/watchers splits guardActiveFor
// from testing.Testing(), and internal/process splits killGuardedFor from Kill:
// the interesting state is one no test on this machine can be in. Every
// assertion about AbsFixture is tautological on unix — the function returns its
// input unchanged there — so before this split the idempotence guard was
// UNGRADED ANYWHERE except the windows CI leg (measured: deleting it left every
// package green on darwin).
//
// WHAT THIS DOES AND DOES NOT ESTABLISH. Injecting isAbs makes the COMPOSITION
// gradable off windows: that an already-absolute path is returned untouched and
// a relative one gets exactly one volume. It does NOT establish that real
// windows filepath.IsAbs agrees with any test double, and a test double is not
// permitted to stand in for that — re-deriving volumeNameLen here would be the
// very "test re-implements the rule it grades" defect this branch has already
// been caught by. The predicate itself is graded only by the windows leg.
func absFixtureFor(isAbs func(string) bool, vol, path string) string {
	// IDEMPOTENCE, in both of its cases.
	//
	// isAbs covers the one that matters in practice: a fixture that has already
	// been absolutised — the shared `selfExe` both identity tables compute once
	// and then route again — must not collect a second volume and become
	// "C:C:/usr/local/bin/grafel", which windows' filepath.IsAbs REJECTS.
	//
	// The volume-prefix test covers the rest. isAbs alone is NOT enough: a
	// RELATIVE fixture stays relative after the volume is grafted on
	// ("grafel" -> "C:grafel"), so isAbs is still false and a second
	// application produced "C:C:grafel". Nothing routes a relative row today —
	// the tables flag them and keep them byte-for-byte — but "nothing does it
	// today" is what left the absolute case open until a reviewer found it, and
	// the point of fixing this here rather than at the two call sites was to
	// retire the CLASS. Half a guard would have retired half of it.
	if isAbs(path) || strings.HasPrefix(path, vol) {
		return path
	}
	return vol + path
}

// fixtureVolume returns the volume to graft onto a unix-style fixture path.
func fixtureVolume() string {
	vol := filepath.VolumeName(os.TempDir())
	if vol == "" {
		vol = "C:"
	}
	return vol
}
