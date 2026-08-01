package mcp

import (
	"testing"
	"time"
)

// setFixedNowForTest pins serverNow() to a single instant for the duration of
// t, restoring the real clock on cleanup. Every serverNow() call then observes
// exactly the same time, which is what turns a byte-for-byte comparison of two
// timestamp-bearing payloads into a real assertion rather than a race with the
// wall clock (#6073).
func setFixedNowForTest(t testing.TB, at time.Time) {
	t.Helper()
	setNowOverride(func() time.Time { return at })
	t.Cleanup(func() { setNowOverride(nil) })
}

// TestServerNowDefaultsToRealClock guards the production path: with no override
// installed, serverNow must track the wall clock and report UTC.
func TestServerNowDefaultsToRealClock(t *testing.T) {
	before := time.Now().UTC()
	got := serverNow()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("serverNow()=%v outside [%v,%v] with no override installed", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Fatalf("serverNow() location = %v, want UTC", got.Location())
	}
}

// TestSetFixedNowForTestPinsAndRestores checks the seam both installs and, via
// t.Cleanup, restores — a leaked override would silently freeze the clock for
// every subsequent test in the package.
func TestSetFixedNowForTestPinsAndRestores(t *testing.T) {
	pinned := time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)
	t.Run("pinned", func(t *testing.T) {
		setFixedNowForTest(t, pinned)
		for i := 0; i < 3; i++ {
			if got := serverNow(); !got.Equal(pinned) {
				t.Fatalf("serverNow() = %v, want pinned %v", got, pinned)
			}
		}
	})
	if got := serverNow(); got.Equal(pinned) {
		t.Fatalf("clock override leaked past the subtest: serverNow() = %v", got)
	}
}
