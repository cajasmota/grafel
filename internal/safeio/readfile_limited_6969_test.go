// ReadFileLimited's truncation signal (#6969).
//
// ReadFile caps a read and returns the prefix with a NIL ERROR. For a caller
// reading a config that costs a field; for a caller reading a LIST that
// decides what gets indexed, a prefix is a different answer, not a smaller
// one. The signal exists so that second kind of caller can tell.
//
// The boundary is the whole of it. Detecting truncation as
// len(b) == maxBytes is wrong for a file whose size is EXACTLY the cap, so
// these cases sit at cap-1, cap and cap+1 rather than at "small" and "huge".
package safeio_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/safeio"
)

func rlWrite(t *testing.T, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, bytes.Repeat([]byte("x"), n), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReadFileLimited_AtTheBoundary walks the three sizes around the cap. The
// case table is written out as literals rather than derived from the cap, so a
// change to how the boundary is computed cannot move the expectations with it.
func TestReadFileLimited_AtTheBoundary(t *testing.T) {
	const cap0 = 64

	cases := []struct {
		name          string
		size          int
		wantLen       int
		wantTruncated bool
	}{
		{"one byte under the cap", 63, 63, false},
		{"exactly the cap is COMPLETE", 64, 64, false},
		{"one byte over the cap", 65, 64, true},
		{"far over the cap", 4096, 64, true},
		{"empty", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := rlWrite(t, tc.size)
			b, truncated, err := safeio.ReadFileLimited(p, safeio.FollowSymlinks, cap0)
			if err != nil {
				t.Fatal(err)
			}
			if truncated != tc.wantTruncated {
				t.Fatalf("truncated = %v for a %d-byte file under a %d cap, want %v", truncated, tc.size, cap0, tc.wantTruncated)
			}
			if len(b) != tc.wantLen {
				t.Fatalf("len = %d, want %d (the extra probe byte must never reach the caller)", len(b), tc.wantLen)
			}
			if want := bytes.Repeat([]byte("x"), tc.wantLen); !bytes.Equal(b, want) {
				t.Fatalf("content mismatch: got %d bytes", len(b))
			}
		})
	}
}

// TestReadFileLimited_UnlimitedNeverReportsTruncation — maxBytes <= 0 is the
// "no cap" spelling, and a read with no cap has no cap to hit. The negative
// case is included because a caller passing a negative by accident must not
// get a silently empty read.
func TestReadFileLimited_UnlimitedNeverReportsTruncation(t *testing.T) {
	p := rlWrite(t, 5000)
	for _, maxBytes := range []int64{0, -1} {
		b, truncated, err := safeio.ReadFileLimited(p, safeio.FollowSymlinks, maxBytes)
		if err != nil {
			t.Fatal(err)
		}
		if truncated {
			t.Fatalf("maxBytes=%d reported truncation", maxBytes)
		}
		if len(b) != 5000 {
			t.Fatalf("maxBytes=%d read %d bytes, want the whole 5000-byte file", maxBytes, len(b))
		}
	}
}

// TestReadFileLimited_MaxInt64DoesNotOverflow — the implementation reads
// maxBytes+1 to observe the extra byte. At math.MaxInt64 that addition wraps
// negative, and io.LimitReader with a negative n reads ZERO bytes, so an
// unguarded +1 turns the widest possible cap into an empty read: the exact
// silent under-read this function exists to prevent.
func TestReadFileLimited_MaxInt64DoesNotOverflow(t *testing.T) {
	p := rlWrite(t, 5000)
	b, truncated, err := safeio.ReadFileLimited(p, safeio.FollowSymlinks, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("a 5000-byte file reported truncated under a MaxInt64 cap")
	}
	if len(b) != 5000 {
		t.Fatalf("read %d bytes under a MaxInt64 cap, want 5000 — the +1 overflowed", len(b))
	}
}

// TestReadFileLimited_ErrorNeverClaimsTruncation — a read that failed has no
// claim to make about how much of the file it saw, and a caller that treats
// truncated as authoritative must not be handed a guess.
func TestReadFileLimited_ErrorNeverClaimsTruncation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent")
	b, truncated, err := safeio.ReadFileLimited(p, safeio.FollowSymlinks, 8)
	if err == nil {
		t.Fatal("reading an absent file succeeded")
	}
	if truncated {
		t.Fatal("a failed read reported truncated=true")
	}
	if b != nil {
		t.Fatalf("a failed read returned %d bytes", len(b))
	}
}

// TestReadFile_StillTruncatesSilently pins the deliberate NON-change. Every
// other caller in the tree keeps ReadFile, and ReadFile's contract — a prefix
// with a nil error — is what those callers were written against. If this ever
// starts erroring, the change reached callers it was not supposed to reach.
func TestReadFile_StillTruncatesSilently(t *testing.T) {
	p := rlWrite(t, 100)
	b, err := safeio.ReadFile(p, safeio.FollowSymlinks, 64)
	if err != nil {
		t.Fatalf("ReadFile now errors on a truncated read: %v — that is a behaviour change for every caller", err)
	}
	if len(b) != 64 {
		t.Fatalf("ReadFile returned %d bytes under a 64 cap, want 64", len(b))
	}
}
