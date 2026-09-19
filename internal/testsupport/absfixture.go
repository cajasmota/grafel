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
)

// AbsFixture returns path unchanged on unix, and prefixes a volume on windows
// so filepath.IsAbs(AbsFixture(p)) is true on every platform for a p that is
// absolute on unix.
//
// It is for test fixtures only. Pass it a unix-style rooted path such as
// "/opt/grafel/bin/grafel"; a relative path is returned with a volume glued on
// and stays relative on unix, which is not a meaningful fixture either way.
func AbsFixture(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	vol := filepath.VolumeName(os.TempDir())
	if vol == "" {
		vol = "C:"
	}
	return vol + path
}
