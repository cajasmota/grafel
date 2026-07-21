package graph

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCurrentGraphPath_FlatFallback: a dir with ONLY the legacy flat graph.fb
// and no `current` pointer must resolve to the flat path (lazy-migration
// compat: existing repos keep working untouched).
func TestCurrentGraphPath_FlatFallback(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(flat, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CurrentGraphPath(dir); got != flat {
		t.Fatalf("CurrentGraphPath = %q, want flat %q", got, flat)
	}
}

// TestCurrentGraphPath_NeverIndexed: a brand-new empty dir resolves to the
// (non-existent) flat path — preserving the pre-gen "graph absent" semantics.
func TestCurrentGraphPath_NeverIndexed(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "graph.fb")
	if got := CurrentGraphPath(dir); got != want {
		t.Fatalf("CurrentGraphPath = %q, want %q", got, want)
	}
}

// TestCurrentGraphPath_ResolvesPointer: after a gen write the resolver returns
// the gen file, NOT the flat path (even if a stale flat file also exists).
func TestCurrentGraphPath_ResolvesPointer(t *testing.T) {
	dir := t.TempDir()
	// A stale legacy flat file that must be IGNORED once a pointer exists.
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	genPath, err := WriteGenGraph(dir, []byte("gen1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := CurrentGraphPath(dir); got != genPath {
		t.Fatalf("CurrentGraphPath = %q, want gen %q", got, genPath)
	}
	if filepath.Base(genPath) != "graph.1.fb" {
		t.Fatalf("first gen file = %q, want graph.1.fb", filepath.Base(genPath))
	}
}

// TestCurrentGraphPath_MissingGenFallsBack: a pointer naming a gen file that no
// longer exists on disk must fall back to the flat path, never return ENOENT.
func TestCurrentGraphPath_MissingGenFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), []byte("flat"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCurrentPointer(dir, "graph.7.fb"); err != nil {
		t.Fatal(err)
	}
	// graph.7.fb does not exist.
	want := filepath.Join(dir, "graph.fb")
	if got := CurrentGraphPath(dir); got != want {
		t.Fatalf("CurrentGraphPath = %q, want flat fallback %q", got, want)
	}
}

// TestCurrentGraphPath_RejectsHostilePointer: a pointer whose content is not a
// valid graph.<gen>.fb name (path traversal / junk) is ignored.
func TestCurrentGraphPath_RejectsHostilePointer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, currentPointerName), []byte("../../etc/passwd"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "graph.fb")
	if got := CurrentGraphPath(dir); got != want {
		t.Fatalf("hostile pointer not rejected: got %q, want %q", got, want)
	}
}

// TestNextGen_Monotonic: generations increase by one per write, and a manually
// deleted pointer (with gen files remaining) still advances monotonically.
func TestNextGen_Monotonic(t *testing.T) {
	dir := t.TempDir()
	if g := NextGen(dir); g != 1 {
		t.Fatalf("first NextGen = %d, want 1", g)
	}
	for i := uint64(1); i <= 3; i++ {
		p, err := WriteGenGraph(dir, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(p) != GenFileName(i) {
			t.Fatalf("write %d produced %q, want %q", i, filepath.Base(p), GenFileName(i))
		}
	}
	// Delete the pointer; NextGen must still exceed the max on-disk gen (3).
	if err := os.Remove(filepath.Join(dir, currentPointerName)); err != nil {
		t.Fatal(err)
	}
	if g := NextGen(dir); g != 4 {
		t.Fatalf("NextGen after pointer deletion = %d, want 4 (scan gen files)", g)
	}
}

// TestGCStaleGens_KeepsCurrentAndPrevious: after N writes only the current and
// immediately-previous gen files remain; older ones are unlinked.
func TestGCStaleGens_KeepsCurrentAndPrevious(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if _, err := WriteGenGraph(dir, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	// gens 1..5 written; GC after the 5th keeps 5 and 4.
	for gen := uint64(1); gen <= 5; gen++ {
		p := filepath.Join(dir, GenFileName(gen))
		_, err := os.Stat(p)
		exists := err == nil
		wantExists := gen >= 4
		if exists != wantExists {
			t.Fatalf("gen %d exists=%v, want %v", gen, exists, wantExists)
		}
	}
}

// TestGCStaleGens_FailSoft: a gen older than previous that CANNOT be removed
// (simulated by making the dir un-writable is platform-fragile; instead we
// assert GCStaleGens never panics/propagates and that keeping the previous gen
// means an in-use previous gen is never targeted). Here we assert the
// keep-window: GCStaleGens(dir, currentGen) must not remove currentGen or
// currentGen-1 even when told to sweep.
func TestGCStaleGens_KeepWindow(t *testing.T) {
	dir := t.TempDir()
	for gen := uint64(1); gen <= 3; gen++ {
		if err := os.WriteFile(filepath.Join(dir, GenFileName(gen)), []byte{byte(gen)}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removed := GCStaleGens(dir, 3)
	if len(removed) != 1 || removed[0] != "graph.1.fb" {
		t.Fatalf("GCStaleGens removed %v, want [graph.1.fb]", removed)
	}
	// current (3) and previous (2) must survive.
	for _, keep := range []uint64{2, 3} {
		if _, err := os.Stat(filepath.Join(dir, GenFileName(keep))); err != nil {
			t.Fatalf("gen %d was removed but should be kept", keep)
		}
	}
}

// TestWriteGenGraph_RoundTrip: the bytes written are readable back at the
// resolved path.
func TestWriteGenGraph_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []byte("hello-gen")
	genPath, err := WriteGenGraph(dir, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(CurrentGraphPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("round-trip: got %q, want %q", got, want)
	}
	// No stray .tmp left behind.
	if _, err := os.Stat(genPath + ".tmp"); err == nil {
		t.Fatalf("leftover tmp file at %s", genPath+".tmp")
	}
}

// TestWriteCurrentPointer_Atomic: the pointer content is exactly the gen name
// (trimmed) and never a torn/partial value.
func TestWriteCurrentPointer_Atomic(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCurrentPointer(dir, "graph.42.fb"); err != nil {
		t.Fatal(err)
	}
	name, ok := readPointer(dir)
	if !ok || name != "graph.42.fb" {
		t.Fatalf("readPointer = %q ok=%v, want graph.42.fb", name, ok)
	}
	if err := WriteCurrentPointer(dir, "not-a-gen"); err == nil {
		t.Fatal("WriteCurrentPointer accepted an invalid gen name")
	}
}
