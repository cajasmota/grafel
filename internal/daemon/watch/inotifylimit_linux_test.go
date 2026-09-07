package watch

// inotifylimit_linux_test.go — the Linux half of the probe, GRADED rather than
// asserted.
//
// Everything else in this package's #6932 tests runs on every platform and
// therefore grades arithmetic. The kernel read itself — the sysctl that makes
// the whole comparison mean anything — only exists here, and its two failure
// branches (unreadable, unparseable) cannot be produced on demand by a host
// with a working /proc. Redirecting the path is the only way to reach them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real read, against the real kernel. This is the end-to-end leg: an
// actual max_user_watches, an actual demand, an actual comparison.
func TestReadInotifyLimit_ReadsTheRealSysctl(t *testing.T) {
	n, src, err := readInotifyLimit()
	if err != nil {
		t.Skipf("no readable %s on this host: %v", src, err)
	}
	if src != inotifyMaxUserWatchesPath {
		t.Errorf("LimitSource = %q, want %q", src, inotifyMaxUserWatchesPath)
	}
	if n <= 0 {
		t.Fatalf("max_user_watches read as %d — a non-positive ceiling would disable every "+
			"comparison the probe makes", n)
	}

	b := MeasuredInotifyBudget(1, 4)
	if !b.LimitKnown() || b.Limit != n {
		t.Fatalf("report Limit = %d (known=%v), kernel says %d", b.Limit, b.LimitKnown(), n)
	}
	if b.WouldExceed() {
		t.Errorf("4 watch descriptors reported as exceeding a ceiling of %d", n)
	}
	if !strings.Contains(b.Summary(), "fits") {
		t.Errorf("summary of a trivially-fitting demand: %q", b.Summary())
	}
}

// An unparseable sysctl must produce "unknown ceiling, and here is why", never
// a fabricated 0 that reads as "no headroom".
func TestReadInotifyLimit_UnparseableValueIsReportedNotInvented(t *testing.T) {
	f := filepath.Join(t.TempDir(), "max_user_watches")
	if err := os.WriteFile(f, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := inotifyMaxUserWatchesPath
	inotifyMaxUserWatchesPath = f
	defer func() { inotifyMaxUserWatchesPath = orig }()

	os.Unsetenv(inotifyLimitEnv)
	b := MeasuredInotifyBudget(1, 4)
	if b.LimitKnown() {
		t.Fatalf("an unparseable sysctl produced a usable ceiling of %d", b.Limit)
	}
	if b.LimitErr == "" {
		t.Error("no reason recorded for the missing ceiling")
	}
	if b.WouldExceed() {
		t.Error("WouldExceed() true with no readable ceiling")
	}
	if !strings.Contains(b.Summary(), "host limit unknown") {
		t.Errorf("summary hides that the ceiling could not be read: %q", b.Summary())
	}
	if !notesMention(b.Notes, "could not read") {
		t.Errorf("notes do not explain the failed read: %v", b.Notes)
	}
}

// An absent sysctl is the container/exotic-kernel case: still Linux, still a
// pool, but no number. Same contract as above.
func TestReadInotifyLimit_MissingSysctlIsReportedNotInvented(t *testing.T) {
	orig := inotifyMaxUserWatchesPath
	inotifyMaxUserWatchesPath = filepath.Join(t.TempDir(), "definitely-absent")
	defer func() { inotifyMaxUserWatchesPath = orig }()

	os.Unsetenv(inotifyLimitEnv)
	b := MeasuredInotifyBudget(1, 4)
	if !b.PoolApplies {
		t.Error("an unreadable sysctl was taken as proof that Linux has no inotify pool")
	}
	if b.LimitKnown() || b.LimitErr == "" {
		t.Fatalf("Limit=%d known=%v err=%q, want unknown with a reason", b.Limit, b.LimitKnown(), b.LimitErr)
	}
}
