package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

// TestScanPathReturnsTooLargeSkip covers the second silent skip found while
// grounding #6483, and it is the portable one — no FIFO, so Windows CI
// exercises the same plumbing the unix test does.
//
// This gate is the more dangerous of the two in practice: the dashboard
// exposes max_size as a QUERY PARAMETER (GET /api/quality/secrets/{group}),
// so a caller can pass max_size=1024 and receive an unqualified "clean" for a
// repo in which nearly every file was skipped unread. Unlike the non-regular
// skip, this one was reported nowhere at all — not even on stderr.
func TestScanPathReturnsTooLargeSkip(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big.go")
	body := "package p\n" + strings.Repeat("// filler\n", 128) // > 1 KB
	if err := os.WriteFile(big, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ScanPath(root, 10) // 10-byte cap: every file is over it
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(res.Findings))
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("expected exactly 1 skip for a tree where the only file is over "+
			"the size cap, got %d: %+v", len(res.Skipped), res.Skipped)
	}
	s := res.Skipped[0]
	if s.Reason != SkipTooLarge {
		t.Errorf("reason = %q, want %q", s.Reason, SkipTooLarge)
	}
	if s.Path != big {
		t.Errorf("path = %q, want %q", s.Path, big)
	}
	if s.Rel != "big.go" {
		t.Errorf("rel = %q, want %q", s.Rel, "big.go")
	}
}

// TestScanPathTooLargeGateStaysExclusive is the permissiveness mutant for the
// size gate: flipping `>` to `>=`, or reporting the skip before the size
// comparison, makes every in-budget file land in the skip list. A file at
// exactly the cap is scanned, so it must not be reported.
func TestScanPathTooLargeGateStaysExclusive(t *testing.T) {
	root := t.TempDir()
	body := []byte("package p\n") // 10 bytes
	if err := os.WriteFile(filepath.Join(root, "exact.go"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ScanPath(root, int64(len(body)))
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("a file exactly at the size cap was reported as skipped: %+v", res.Skipped)
	}
}

// TestScanPathReportsOverlongLineAsSkip pins the third silent-drop path found
// while reviewing #6483, and it is the most reachable of the three.
//
// scanFile returns (findings, scanner.Err()). bufio.Scanner fails a line
// longer than bufio.MaxScanTokenSize (64 KB) with bufio.ErrTooLong — well
// under the 512 KB default file cap — and the walk's error branch used to
// drop `ff` WHOLESALE while classifyScanSkip returned ok=false. A real key on
// line 1 of a file that also contains one minified line was therefore
// reported as findings=0, skipped=[]: exactly the "clean, but never read"
// answer #6483 exists to make impossible.
//
// It is not a corner case. skipFile denies no .json, .lock, .map or minified
// .js — the files that actually carry >64 KB lines are precisely the ones
// that reach scanFile.
//
// Two assertions, because two separate decisions are under test:
//   - the overlong line is REPORTED (SkipLineTooLong), and
//   - the findings scanFile collected BEFORE the overlong line survive.
func TestScanPathReportsOverlongLineAsSkip(t *testing.T) {
	root := t.TempDir()
	body := "package p\n\nvar awsKey = \"AKIAIOSFODNN7REAL000\"\n\nvar blob = \"" +
		strings.Repeat("x", 70000) + "\"\n" // one line over bufio.MaxScanTokenSize
	target := filepath.Join(root, "creds.go")
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ScanPath(root, 0) // default 512 KB cap: the FILE is in budget
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}

	if len(res.Findings) != 1 {
		t.Errorf("the AWS key on line 3 was dropped because a LATER line was "+
			"too long for bufio.Scanner: got %d findings, want 1: %+v",
			len(res.Findings), res.Findings)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("expected exactly 1 skip for a file whose tail was never "+
			"scanned, got %d: %+v", len(res.Skipped), res.Skipped)
	}
	s := res.Skipped[0]
	if s.Reason != SkipLineTooLong {
		t.Errorf("reason = %q, want %q", s.Reason, SkipLineTooLong)
	}
	if s.Rel != "creds.go" || s.Path != target {
		t.Errorf("skip names %q/%q, want %q/%q", s.Path, s.Rel, target, "creds.go")
	}
}

// TestClassifyScanSkipReportsWouldBlock covers the SkipWouldBlock arm, which
// no end-to-end test reaches: safeio.ErrWouldBlock comes from
// openWithDeadline's semaphore timeout and its 5s open deadline, neither of
// which a test can provoke deterministically.
//
// It is exercised here at unit level rather than left to a comment, because
// the arm carries a real decision the issue explicitly flagged: ErrWouldBlock
// is returned BARE by safeio, with no path inside it, so Path/Rel must come
// from the walk's own context. A mutant that scrapes the error text — or that
// leaves Path empty — dies here.
func TestClassifyScanSkipReportsWouldBlock(t *testing.T) {
	sk, ok := classifyScanSkip("/abs/repo/creds.go", "creds.go", 0o644,
		fmt.Errorf("open: %w", safeio.ErrWouldBlock))
	if !ok {
		t.Fatalf("ErrWouldBlock was not classified as a skip; the caller is told the file was read")
	}
	if sk.Reason != SkipWouldBlock {
		t.Errorf("reason = %q, want %q", sk.Reason, SkipWouldBlock)
	}
	if sk.Path != "/abs/repo/creds.go" || sk.Rel != "creds.go" {
		t.Errorf("path/rel = %q/%q; ErrWouldBlock carries no path of its own, so "+
			"both must be filled from the walk", sk.Path, sk.Rel)
	}
	if sk.Kind != "" {
		t.Errorf("kind = %q, want empty: Kind names an entry type, and a blocked "+
			"open never learned one", sk.Kind)
	}
}
