package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
