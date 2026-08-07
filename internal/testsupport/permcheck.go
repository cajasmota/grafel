package testsupport

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// AssertPerm asserts path's permission bits, adapted to what the running
// platform is capable of representing.
//
// # Why this is not just `fi.Mode().Perm() != want`
//
// Go's Windows os.Stat does not report Unix permission bits, because NTFS does
// not have them: it reports 0666 for an ordinary file and 0444 for one
// carrying FILE_ATTRIBUTE_READONLY, and nothing else. A `want 0644` assertion
// therefore cannot pass on Windows no matter what the code under test does —
// it is testing the platform, not the product (#6053).
//
// So on Windows this asserts the one thing that IS representable and IS
// enforced: writability. want with an owner-write bit must produce a writable
// file; want without one (0444) must produce a read-only file. That is a
// genuinely weaker assertion than on unix and it is deliberately still an
// assertion — the alternative, t.Skip, would let a helper that stopped
// applying perm entirely go unnoticed on Windows.
//
// # Is anything security-sensitive weakened by this?
//
// No — but the reason is narrower than it was, and the original wording ("no
// credential, token or key is written through this path anywhere in the tree")
// stopped being true in #6240. internal/install/mcpreg now asserts perms on
// ~/.claude.json, which DOES hold an auth token, and creates such files 0600.
//
// What is unweakened is the thing that matters: the runtime perm argument is
// identical on every platform; only the TEST's expectation is adapted. On
// Windows those files are protected by the ACL on the user profile directory
// they live in rather than by mode bits, which is the platform's normal
// mechanism and is not something a chmod could improve — so there is no mode
// this helper could assert there that would protect a token better.
//
// The practical consequence for a caller: on Windows this assertion cannot
// distinguish 0600 from 0644, so a test that only ever seeds writable modes has
// no signal there at all. Include a 0444 row — the one mode Windows can
// represent — whenever the property under test is "the mode was applied".
func AssertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if msg := permMismatch(fi.Mode().Perm(), want, runtime.GOOS == "windows"); msg != "" {
		t.Fatalf("%s: %s", path, msg)
	}
}

// permMismatch returns "" when got satisfies want on the given platform, and
// the failure message otherwise.
//
// The decision is split out of AssertPerm so that BOTH branches are testable
// from any host. The Windows branch is the one that cannot be exercised by
// running the assertion off Windows, and a helper that quietly stopped
// asserting anything there is exactly the failure this change must not
// introduce — see permcheck_test.go.
func permMismatch(got, want os.FileMode, windows bool) string {
	if windows {
		wantWritable := want&0o200 != 0
		gotWritable := got&0o200 != 0
		if wantWritable != gotWritable {
			return fmt.Sprintf("writable = %v (mode %04o), want writable = %v (requested mode %04o); "+
				"Windows represents only the read-only attribute",
				gotWritable, got, wantWritable, want)
		}
		return ""
	}
	if got != want {
		return fmt.Sprintf("mode = %04o, want %04o", got, want)
	}
	return ""
}
