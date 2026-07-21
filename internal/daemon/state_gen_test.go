package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestFindGraphFileInDir_ResolvesGenAndAdvances is the PROGRESS linchpin
// (#5891): because writers stop bumping a fixed graph.fb, findGraphFileInDir —
// the source of statuswriter's GraphFBMtime — MUST stat the resolved gen file
// so a completed rebuild's mtime keeps advancing. If it froze, the status-plane
// completion classifier (GraphFBMtime >= fgStart) would misclassify a finished
// rebuild as "produced no graph" (the wizard false-Failed regression).
func TestFindGraphFileInDir_ResolvesGenAndAdvances(t *testing.T) {
	dir := t.TempDir()

	gen1, err := graph.WriteGenGraph(dir, []byte("gen-one-bytes"))
	if err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	// Pin gen1's mtime to a known-old value so the advance is unambiguous even
	// on coarse-granularity filesystems.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(gen1, old, old); err != nil {
		t.Fatalf("chtimes gen1: %v", err)
	}

	p1, m1 := findGraphFileInDir(dir)
	if p1 != gen1 {
		t.Fatalf("findGraphFileInDir p1 = %q, want gen1 %q", p1, gen1)
	}
	if m1 != old.UnixNano() {
		t.Fatalf("findGraphFileInDir m1 = %d, want gen1 mtime %d", m1, old.UnixNano())
	}

	gen2, err := graph.WriteGenGraph(dir, []byte("gen-two-bytes-longer"))
	if err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	p2, m2 := findGraphFileInDir(dir)
	if p2 != gen2 {
		t.Fatalf("findGraphFileInDir p2 = %q, want gen2 %q", p2, gen2)
	}
	if m2 < m1 {
		t.Fatalf("GraphFBMtime went backwards: m2=%d < m1=%d", m2, m1)
	}
	if m2 == old.UnixNano() {
		t.Fatal("GraphFBMtime froze at the previous generation's mtime — wizard would false-Fail")
	}
}

// TestFindGraphFileInDir_FlatFallback: a legacy flat graph.fb with no pointer
// still resolves (compat), so a never-migrated repo keeps reporting its mtime.
func TestFindGraphFileInDir_FlatFallback(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(flat, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, m := findGraphFileInDir(dir)
	if p != flat {
		t.Fatalf("findGraphFileInDir = %q, want flat %q", p, flat)
	}
	if m == 0 {
		t.Fatal("flat graph.fb mtime resolved to 0")
	}
}
