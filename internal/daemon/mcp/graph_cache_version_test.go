package mcp

// graph_cache_version_test.go — #5907 FIX 1: the zero-copy MCP graph cache must
// NEVER serve (or crash on) an on-disk graph.fb whose format is older than /
// incompatible with this binary. These are the RED→GREEN tests for the
// version gate + panic-recover added to openReader/openAndGate: an old-format
// file yields a typed *graph.FormatVersionError (no panic, not cached), and a
// genuinely-corrupt buffer degrades to an error instead of taking down the
// daemon.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeStaleFormatGraph builds a valid graph.fb via fbwriter.Marshal and then
// patches its on-disk Graph.version scalar DOWN to oldVersion — the same
// technique used by internal/graph/fbwriter's TestLoaderRejectsOldFormatVersion
// and internal/graph's reindex_required_test.go — so a test can fabricate an
// "old on-disk format vs new binary" file without an actual old binary or the
// (not-yet-shipped) segmented format.
func writeStaleFormatGraph(t *testing.T, path string, oldVersion int) {
	t.Helper()
	doc := &graph.Document{
		Repo:     "stale-fixture",
		Entities: []graph.Entity{{ID: "s::a", QualifiedName: "pkg.A", Kind: "function", Name: "A"}},
	}
	doc.Stats.Entities = 1
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root := fb.GetRootAsGraph(buf, 0)
	if !root.MutateVersion(int32(oldVersion)) {
		t.Fatalf("MutateVersion(%d) returned false — slot missing?", oldVersion)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestCache_StaleFormat_TypedError_NotCached is the core FIX-1 assertion: a
// Get against an old-format graph.fb returns a *graph.FormatVersionError (via
// errors.As, not a string match), never panics, and never leaves a resident
// handle in the cache (a stale-format reader must never be served).
func TestCache_StaleFormat_TypedError_NotCached(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stale.fb")
	writeStaleFormatGraph(t, p, fbversion.Version-1)

	c := NewCache(4)
	defer c.Close()

	r, release, err := c.Get(p) // must NOT panic
	if release != nil {
		release()
	}
	if r != nil {
		t.Fatalf("expected nil reader for a stale-format graph.fb, got %v", r)
	}
	if err == nil {
		t.Fatal("expected an error for a stale-format graph.fb, got nil")
	}
	var fvErr *graph.FormatVersionError
	if !errors.As(err, &fvErr) {
		t.Fatalf("expected errors.As to find a *graph.FormatVersionError, got %v", err)
	}
	if fvErr.Found != fbversion.Version-1 {
		t.Errorf("FormatVersionError.Found = %d, want %d", fvErr.Found, fbversion.Version-1)
	}
	if fvErr.Required != fbversion.Version {
		t.Errorf("FormatVersionError.Required = %d, want %d", fvErr.Required, fbversion.Version)
	}
	// The stale reader must NOT be cached (never served on a subsequent Get).
	if c.Len() != 0 {
		t.Fatalf("stale-format reader must not be cached; cache Len = %d", c.Len())
	}
	if s := c.Stats(); s.OpenErrors < 1 {
		t.Errorf("expected an open error to be counted, stats = %+v", s)
	}
}

// TestQueryService_StaleFormat_DegradesGracefully proves the typed error
// propagates through a real handler as an error return — NOT a panic / crash.
func TestQueryService_StaleFormat_DegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stale.fb")
	writeStaleFormatGraph(t, p, fbversion.Version-1)

	c := NewCache(4)
	defer c.Close()
	q := NewQueryService(c)

	view, err := q.ReadEntity(p, "s::a") // must not panic
	if view != nil {
		t.Fatalf("expected nil entity view for stale-format graph, got %+v", view)
	}
	var fvErr *graph.FormatVersionError
	if !errors.As(err, &fvErr) {
		t.Fatalf("expected a *graph.FormatVersionError from the handler, got %v", err)
	}
}

// TestCache_CorruptBuffer_NoCrash covers the second crash vector: a genuinely
// corrupt / incompatible buffer (long enough to pass fbreader's length guard
// but with a garbage vtable) must NOT re-panic out of the cache. The recover
// in openAndGate converts it into an error and leaves the cache empty.
func TestCache_CorruptBuffer_NoCrash(t *testing.T) {
	dir := t.TempDir()

	// Two flavours of corruption:
	//  1. Too short for a flatbuffer (< 8 bytes) — fbreader.Open rejects directly.
	//  2. Long enough to pass the length guard but structurally garbage — the
	//     flatbuffer parse/scalar-read re-panics inside fbreader; openAndGate
	//     must recover it into an error.
	cases := map[string][]byte{
		"tooShort": {0x01, 0x02, 0x03},
		"garbageVTable": func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = 0xFF
			}
			return b
		}(),
	}

	c := NewCache(4)
	defer c.Close()

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name+".fb")
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			r, release, err := c.Get(p) // must NOT panic/crash the process
			if release != nil {
				release()
			}
			if r != nil {
				t.Fatalf("expected nil reader for a corrupt buffer, got %v", r)
			}
			if err == nil {
				t.Fatal("expected an error for a corrupt buffer, got nil")
			}
		})
	}
	if c.Len() != 0 {
		t.Fatalf("corrupt buffers must not be cached; cache Len = %d", c.Len())
	}
}

// TestCache_CurrentFormat_Unaffected is the parity guard: a normal,
// current-version graph.fb loads exactly as before the gate was added.
func TestCache_CurrentFormat_Unaffected(t *testing.T) {
	dir := t.TempDir()
	p := writeFixtureGraph(t, dir, "current", time.Time{})

	c := NewCache(4)
	defer c.Close()

	r, release, err := c.Get(p)
	if err != nil {
		t.Fatalf("current-format Get: %v", err)
	}
	defer release()
	if r == nil || r.EntityCount() != 2 {
		t.Fatalf("expected a usable reader with 2 entities, got %v", r)
	}
	if c.Len() != 1 {
		t.Fatalf("current-format reader should be cached; Len = %d", c.Len())
	}
}
