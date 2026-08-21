//go:build unix

// Tests for #6507: ScanPath's max_size gate read d.Info(), which is an lstat.
// For a symlink that reports the LINK's own size — a few dozen bytes — while
// scanFile opens with safeio.FollowSymlinks and reads the TARGET in full. So a
// symlink to an oversized file passed a cap it should have failed.
//
// Build tag: symlink creation needs Developer Mode or elevation on
// windows-latest, the same reason the FIFO fixtures carry one. The plain
// oversized-file and overlong-line gates are covered without a tag in
// scan_skips_6483_test.go, so Windows CI still exercises the size gate itself.
package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// awsKeyLine is a realistic-looking AWS key so the scan has something to find:
// a test that only counts skips cannot tell "the cap was applied" from "the
// file was never interesting".
const awsKeyLine = "var awsKey = \"AKIAIOSFODNN7REAL000\"\n"

// writeOversizedTarget writes a file that is BOTH over a small caller cap and
// carries a line over bufio.MaxScanTokenSize. Both properties are needed: the
// oversized-ness is what the cap must catch, and the overlong line is what the
// buggy code reported INSTEAD — SkipLineTooLong, a limit no caller can raise.
func writeOversizedTarget(t *testing.T, path string) {
	t.Helper()
	body := "package p\n" + awsKeyLine +
		"var blob = \"" + strings.Repeat("x", 70000) + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
}

// TestScanPathSizeGateStatsSymlinkTarget is the #6507 regression.
//
// The assertion is on the REASON, not merely on the presence of a skip: the
// buggy code did produce a skip. It produced the WRONG one — line_too_long,
// which tells a caller that nothing can be done, for a file whose own max_size
// would have excluded it and where raising the cap is exactly the remedy.
func TestScanPathSizeGateStatsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	// The target lives OUTSIDE root, or the walk would reach it directly and
	// the assertion could pass on the direct entry rather than the link.
	outside := t.TempDir()
	target := filepath.Join(outside, "big.go")
	writeOversizedTarget(t, target)

	link := filepath.Join(root, "link.go")
	symlinkOrSkip(t, target, link)

	res, err := ScanPath(root, 1024)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}

	if len(res.Findings) != 0 {
		t.Errorf("the caller capped the scan at 1024 bytes and the link's "+
			"target is ~70 KB, yet it was opened and read: got %d findings, "+
			"want 0: %+v", len(res.Findings), res.Findings)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("want exactly 1 skip, got %d: %+v", len(res.Skipped), res.Skipped)
	}
	s := res.Skipped[0]
	if s.Reason != SkipTooLarge {
		t.Errorf("reason = %q, want %q: the file exceeds the cap the CALLER "+
			"chose, so raising max_size is the remedy; %q claims a limit no "+
			"caller can raise", s.Reason, SkipTooLarge, s.Reason)
	}
	// Pinned as a literal too: the reason strings travel to the MCP tool and
	// the dashboard verbatim in JSON, so they are a client-facing contract and
	// an assertion written only against the constant moves with a rename.
	if s.Reason != "too_large" {
		t.Errorf("wire reason = %q, want %q", s.Reason, "too_large")
	}
	if s.Rel != "link.go" || s.Path != link {
		t.Errorf("skip names %q/%q, want %q/%q", s.Path, s.Rel, link, "link.go")
	}
}

// TestScanPathStillScansSymlinkToInBudgetTarget is the permissive-mutant
// guard. Statting the target must make the cap MEAN something, not make every
// symlink disappear: the scanner follows symlinks deliberately
// (safeio.FollowSymlinks), and monorepo/vendor link farms are the ordinary
// case. A mutant that skips symlinks outright, or that reports too_large
// whenever the stat succeeds, dies here rather than in the test above.
func TestScanPathStillScansSymlinkToInBudgetTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "small.go")
	if err := os.WriteFile(target, []byte("package p\n"+awsKeyLine), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link.go")
	symlinkOrSkip(t, target, link)

	res, err := ScanPath(root, 1024)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("a symlink to a ~40-byte target under a 1024-byte cap must "+
			"still be scanned: got %d findings, want 1: %+v",
			len(res.Findings), res.Findings)
	}
	if res.Findings[0].File != "link.go" {
		t.Errorf("finding names %q, want %q", res.Findings[0].File, "link.go")
	}
	if len(res.Skipped) != 0 {
		t.Errorf("an in-budget symlink must produce no skip, got %+v", res.Skipped)
	}
}

// TestScanPathSymlinkTooLargeIsTheCapNotThePolicy proves the too_large in
// TestScanPathSizeGateStatsSymlinkTarget comes from the CAP and not from a
// blanket symlink policy: the identical fixture under the default 512 KB cap
// must be opened, must keep the finding from before the overlong line, and
// must report line_too_long — the reason that IS correct once the file is
// inside the caller's budget.
//
// Together the two tests pin the precedence #6504 established for the direct
// path (size gate first, scanner limit second) onto the symlink path as well.
func TestScanPathSymlinkTooLargeIsTheCapNotThePolicy(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "big.go")
	writeOversizedTarget(t, target)

	link := filepath.Join(root, "link.go")
	symlinkOrSkip(t, target, link)

	res, err := ScanPath(root, 0) // default 512 KB: the ~70 KB target is in budget
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("in-budget symlink target: got %d findings, want 1: %+v",
			len(res.Findings), res.Findings)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("want exactly 1 skip, got %d: %+v", len(res.Skipped), res.Skipped)
	}
	if got := res.Skipped[0].Reason; got != SkipLineTooLong {
		t.Errorf("reason = %q, want %q: inside the caller's cap, the overlong "+
			"line is the real and unraisable limit", got, SkipLineTooLong)
	}
}

// TestScanPathDanglingSymlinkStaysSilent covers the arm the target stat adds.
// A broken link is an ordinary condition on a live tree — the same class as the
// ENOENT the walk already swallows — and must not manufacture a skip entry. A
// mutant that reports too_large (or any skip) when the target stat fails dies
// here.
func TestScanPathDanglingSymlinkStaysSilent(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link.go")
	symlinkOrSkip(t, filepath.Join(t.TempDir(), "does-not-exist.go"), link)

	res, err := ScanPath(root, 1024)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Errorf("got %d findings, want 0: %+v", len(res.Findings), res.Findings)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("a dangling symlink must stay silent, got %+v", res.Skipped)
	}
}
