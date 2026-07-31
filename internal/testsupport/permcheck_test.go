package testsupport

// permcheck_test.go — both platform branches of the perm expectation, driven
// from any host.
//
// AssertPerm exists because Windows cannot represent Unix permission bits, and
// the temptation there is to reach for t.Skip. A skip is invisible: a helper
// that stopped applying perm entirely would keep the Windows job green
// forever. So the Windows branch still asserts writability, and these cases
// pin that it can actually FAIL — which is the only thing that distinguishes
// it from the skip it replaced.

import (
	"os"
	"strings"
	"testing"
)

func TestPermMismatch(t *testing.T) {
	cases := []struct {
		name    string
		got     os.FileMode
		want    os.FileMode
		windows bool
		fail    bool
	}{
		// Unix: exact bits, nothing else.
		{"unix exact match", 0o644, 0o644, false, false},
		{"unix wider than wanted", 0o666, 0o644, false, true},
		{"unix narrower than wanted", 0o600, 0o644, false, true},
		{"unix read-only match", 0o444, 0o444, false, false},

		// Windows: only the read-only attribute is representable, and
		// os.Stat there only ever reports 0666 or 0444.
		{"windows 0666 satisfies 0644", 0o666, 0o644, true, false},
		{"windows 0666 satisfies 0600", 0o666, 0o600, true, false},
		{"windows 0666 satisfies 0755", 0o666, 0o755, true, false},
		// The rows that keep the Windows branch honest: a requested read-only
		// mode that did not take, and a writable mode that came out read-only.
		{"windows 0666 does NOT satisfy 0444", 0o666, 0o444, true, true},
		{"windows 0444 does NOT satisfy 0644", 0o444, 0o644, true, true},
		{"windows 0444 satisfies 0444", 0o444, 0o444, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := permMismatch(c.got, c.want, c.windows)
			if c.fail && msg == "" {
				t.Fatalf("permMismatch(%04o, %04o, windows=%v) = \"\", want a failure message",
					c.got, c.want, c.windows)
			}
			if !c.fail && msg != "" {
				t.Fatalf("permMismatch(%04o, %04o, windows=%v) = %q, want no failure",
					c.got, c.want, c.windows, msg)
			}
			if c.fail && !strings.Contains(msg, "want") {
				t.Fatalf("failure message %q does not say what was wanted", msg)
			}
		})
	}
}

// TestPermMismatch_WindowsIsNotAlwaysPassing is the anti-skip guard, stated as
// its own assertion rather than left implicit in the table above: if this
// helper ever degenerates into "return \"\"" on Windows, this fails.
func TestPermMismatch_WindowsIsNotAlwaysPassing(t *testing.T) {
	if permMismatch(0o666, 0o444, true) == "" {
		t.Fatal("the windows branch accepts everything — it has become a silent skip")
	}
}
