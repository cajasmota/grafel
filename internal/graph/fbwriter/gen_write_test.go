package fbwriter_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// smallDoc builds a minimal, valid graph.Document for the gen-layout tests.
func smallDoc(repo string) *graph.Document {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Repo:        repo,
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go", StartLine: 10},
			{ID: "ent0000000000000b", Name: "bar", Kind: "function", SourceFile: "b.go", StartLine: 20},
		},
		Relationships: []graph.Relationship{
			{ID: "rel000000000000aa", FromID: "ent0000000000000a", ToID: "ent0000000000000b", Kind: "calls"},
		},
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return doc
}

// TestWriteGraphGen_EmitsGenAndPointer: the writer emits graph.<gen>.fb + a
// `current` pointer, the central resolver returns the gen file, and the shared
// loader loads it back with the expected entity/relationship counts.
func TestWriteGraphGen_EmitsGenAndPointer(t *testing.T) {
	dir := t.TempDir()
	genPath, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini"))
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	if filepath.Base(genPath) != "graph.1.fb" {
		t.Fatalf("first gen file = %q, want graph.1.fb", filepath.Base(genPath))
	}
	// A `current` pointer must exist.
	if _, err := os.Stat(filepath.Join(dir, "current")); err != nil {
		t.Fatalf("current pointer missing: %v", err)
	}
	// The central resolver returns the gen file.
	if got := graph.CurrentGraphPath(dir); got != genPath {
		t.Fatalf("CurrentGraphPath = %q, want %q", got, genPath)
	}
	// The shared loader loads it.
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if loaded.Stats.Entities != 2 || loaded.Stats.Relationships != 1 {
		t.Fatalf("loaded stats = %d ents / %d rels, want 2/1",
			loaded.Stats.Entities, loaded.Stats.Relationships)
	}
}

// TestGen_NoFlatGraphFile: WriteGraphGen must NOT create a legacy flat
// graph.fb — the whole point is to stop overwriting a fixed (possibly mapped)
// file.
func TestGen_NoFlatGraphFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini")); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "graph.fb")); err == nil {
		t.Fatal("a flat graph.fb was written — gen layout must never create it")
	}
}

// TestCompat_LegacyFlatGraphNoPointer: a dir with ONLY the legacy flat graph.fb
// and NO pointer must keep working untouched — the resolver returns the flat
// path, the loader loads it, ReindexRequiredReason is false, and
// PersistedStatsFromDir reports ok==true. This is the lazy-migration contract:
// NO reindex is triggered for an existing flat-file repo.
func TestCompat_LegacyFlatGraphNoPointer(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(flat, smallDoc("legacy-mini")); err != nil {
		t.Fatalf("WriteAtomic flat: %v", err)
	}
	// No pointer exists.
	if _, err := os.Stat(filepath.Join(dir, "current")); err == nil {
		t.Fatal("unexpected pointer for a legacy flat-file repo")
	}
	if got := graph.CurrentGraphPath(dir); got != flat {
		t.Fatalf("CurrentGraphPath = %q, want flat %q", got, flat)
	}
	if _, err := graph.LoadGraphFromDir(dir); err != nil {
		t.Fatalf("LoadGraphFromDir(flat): %v", err)
	}
	if required, reason := graph.ReindexRequiredReason(dir); required {
		t.Fatalf("ReindexRequiredReason=true for a current-version flat graph: %s", reason)
	}
	if _, ok := graph.PersistedStatsFromDir(dir); !ok {
		t.Fatal("PersistedStatsFromDir ok=false for a legacy flat graph — would falsely look never-indexed")
	}
}

// TestNoReindexLoop_GenOnlyDir: a dir holding ONLY a gen file (no flat
// graph.fb) must resolve through the pointer so PersistedStatsFromDir reports
// ok==true AND LoadGraphFromDir succeeds. If either resolved to the absent flat
// path, the incremental poller's absent-graph guard (incremental.go:331) and
// base-load (incremental.go:383) would force a full reindex on EVERY no-op
// poll — an infinite reindex loop. This asserts the guards are pointer-aware.
func TestNoReindexLoop_GenOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini")); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	// Precondition: no flat graph.fb.
	if _, err := os.Stat(filepath.Join(dir, "graph.fb")); err == nil {
		t.Fatal("precondition failed: flat graph.fb exists")
	}
	// incremental.go:331 path — absent-graph guard reads this.
	if _, ok := graph.PersistedStatsFromDir(dir); !ok {
		t.Fatal("PersistedStatsFromDir ok=false on a gen-only dir → would force full reindex every poll")
	}
	// incremental.go:383 path — incremental base load reads this.
	if _, err := graph.LoadGraphFromDir(dir); err != nil {
		t.Fatalf("LoadGraphFromDir on a gen-only dir: %v → would force full reindex", err)
	}
	// And the version gate must NOT demand a reindex.
	if required, reason := graph.ReindexRequiredReason(dir); required {
		t.Fatalf("ReindexRequiredReason=true on a fresh gen graph: %s", reason)
	}
}

// TestNoFbversionBump: the gen layout must NOT bump the on-disk FB format
// version — bumping it would force-reindex every existing repo. Assert the
// constant is still 4 and that a freshly-written gen file's header is v4.
func TestNoFbversionBump(t *testing.T) {
	if fbversion.Version != 4 {
		t.Fatalf("fbversion.Version = %d, want 4 (a bump force-reindexes the whole corpus)", fbversion.Version)
	}
	dir := t.TempDir()
	genPath, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini"))
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	r, err := fbreader.Open(genPath)
	if err != nil {
		t.Fatalf("open gen file: %v", err)
	}
	defer r.Close()
	if v := r.Version(); v != 4 {
		t.Fatalf("gen file header version = %d, want 4", v)
	}
}

// TestGen_PreviousGenSurvivesOpenHandleAcrossWrite: holding an open mmap reader
// on the previous generation across a new write must NOT break the write, and
// the previous gen must survive (the GC keep-window retains current+previous so
// serve can still map the previous during a reload overlap). This is the Unix
// analogue of the Windows fail-soft: the previous gen is never a GC target, so
// an in-use previous gen is never at risk.
func TestGen_PreviousGenSurvivesOpenHandleAcrossWrite(t *testing.T) {
	dir := t.TempDir()
	prevPath, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini")) // gen 1
	if err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	// Hold gen 1 open (simulating serve still mapping the previous gen).
	held, err := fbreader.Open(prevPath)
	if err != nil {
		t.Fatalf("open prev gen: %v", err)
	}
	defer held.Close()

	// Write gen 2 while gen 1 is held open — must succeed.
	if _, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini")); err != nil {
		t.Fatalf("write gen2 while prev held open: %v", err)
	}
	// gen 1 (previous) must survive the GC keep-window and stay readable.
	if held.EntityCount() != 2 {
		t.Fatalf("held prev-gen reader broke: EntityCount=%d, want 2", held.EntityCount())
	}
	if _, err := os.Stat(prevPath); err != nil {
		t.Fatalf("previous gen was GC'd while still current-1: %v", err)
	}
}

// TestGen_OldGensCollected: after several writes only the current + previous
// gen files remain on disk (older ones are best-effort unlinked).
func TestGen_OldGensCollected(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		if _, err := fbwriter.WriteGraphGen(dir, smallDoc("gen-mini")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// gens 1..4 written → keep 4 and 3, drop 1 and 2.
	for gen := uint64(1); gen <= 4; gen++ {
		p := filepath.Join(dir, graph.GenFileName(gen))
		_, err := os.Stat(p)
		wantExists := gen >= 3
		if (err == nil) != wantExists {
			t.Fatalf("gen %d exists=%v, want %v", gen, err == nil, wantExists)
		}
	}
}
