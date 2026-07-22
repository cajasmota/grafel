package descriptions_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSingleFileGen writes graph.1.fb + points current at it, returning stateDir.
func writeSingleFileGen(t *testing.T, dir string) {
	t.Helper()
	doc := &graph.Document{Repo: "r", Entities: []graph.Entity{
		{ID: "e1", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "e2", QualifiedName: "p.B", Kind: "struct", Name: "B"},
	}}
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
}

// writeSegmentSet hand-builds a multi-segment generation on disk (mirrors the
// graph package's writeSegmentSet test helper).
func writeSegmentSet(t *testing.T, dir string, gen uint64) {
	t.Helper()
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(dir, genDirName)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docs := []*graph.Document{
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "p.B", Kind: "struct", Name: "B"},
		}},
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "m", QualifiedName: "p.M", Kind: "function", Name: "M"},
			{ID: "n", QualifiedName: "p.N", Kind: "struct", Name: "N"},
		}},
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		seg := graph.SegmentMeta{
			File:        name,
			Kind:        graph.SegmentEntities,
			EntityCount: len(doc.Entities),
		}
		ids := make([]string, 0, len(doc.Entities))
		for _, e := range doc.Entities {
			ids = append(ids, e.ID)
		}
		sort.Strings(ids)
		seg.MinKey, seg.MaxKey = ids[0], ids[len(ids)-1]
		m.Segments = append(m.Segments, seg)
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(dir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
}

// TestUpsertRead_SingleFileRoundtrip: an upserted description reads back on a
// single-file graph.
func TestUpsertRead_SingleFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)

	if err := descriptions.Upsert(dir, "e1", "the first entity"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	sc, ok := descriptions.Read(dir)
	if !ok {
		t.Fatal("Read returned not-ok for a fresh sidecar")
	}
	if got := sc.Results["e1"]; got != "the first entity" {
		t.Errorf("Results[e1] = %q, want %q", got, "the first entity")
	}
	if sc.SourceKey == "" {
		t.Error("SourceKey should be non-empty for a single-file graph")
	}
}

// TestUpsert_MultipleEntities: read-modify-write preserves prior entries.
func TestUpsert_MultipleEntities(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	if err := descriptions.Upsert(dir, "e1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := descriptions.Upsert(dir, "e2", "second"); err != nil {
		t.Fatal(err)
	}
	sc, ok := descriptions.Read(dir)
	if !ok {
		t.Fatal("Read not-ok")
	}
	if sc.Results["e1"] != "first" || sc.Results["e2"] != "second" {
		t.Errorf("expected both entries, got %+v", sc.Results)
	}
}

// TestUpsert_Overwrite: upserting the same id overwrites its description.
func TestUpsert_Overwrite(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	_ = descriptions.Upsert(dir, "e1", "old")
	_ = descriptions.Upsert(dir, "e1", "new")
	sc, ok := descriptions.Read(dir)
	if !ok || sc.Results["e1"] != "new" {
		t.Errorf("want new, got %+v (ok=%v)", sc, ok)
	}
}

// TestUpsertRead_SegmentSetRoundtrip: the sidecar works over a segment-set and
// its source key is segment-derived (never a collapsed single-file path).
func TestUpsertRead_SegmentSetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 3)

	if err := descriptions.Upsert(dir, "a", "entity a in seg 0"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	sc, ok := descriptions.Read(dir)
	if !ok {
		t.Fatal("Read not-ok for segment-set sidecar")
	}
	if sc.Results["a"] != "entity a in seg 0" {
		t.Errorf("Results[a] = %q", sc.Results["a"])
	}
	if len(sc.SourceKey) < 4 || sc.SourceKey[:4] != "seg:" {
		t.Errorf("segment-set SourceKey = %q, want seg: prefix", sc.SourceKey)
	}
}

// TestRead_Absent: no sidecar → (nil,false), never a crash.
func TestRead_Absent(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	if sc, ok := descriptions.Read(dir); ok || sc != nil {
		t.Errorf("absent sidecar: want (nil,false), got (%v,%v)", sc, ok)
	}
}

// TestRead_Corrupt: garbage bytes → (nil,false), never a crash.
func TestRead_Corrupt(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	if err := os.WriteFile(descriptions.Path(dir), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sc, ok := descriptions.Read(dir); ok || sc != nil {
		t.Errorf("corrupt sidecar: want (nil,false), got (%v,%v)", sc, ok)
	}
}

// TestRead_StaleAfterReindex: a new graph generation (reindex) invalidates the
// sidecar by source-key mismatch → (nil,false).
func TestRead_StaleAfterReindex(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	if err := descriptions.Upsert(dir, "e1", "desc"); err != nil {
		t.Fatal(err)
	}
	if _, ok := descriptions.Read(dir); !ok {
		t.Fatal("precondition: fresh sidecar must read ok")
	}
	// Simulate a reindex: write a new generation + flip current.
	doc := &graph.Document{Repo: "r", Entities: []graph.Entity{{ID: "e1", QualifiedName: "p.A", Kind: "function", Name: "A"}}}
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(2)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}
	if sc, ok := descriptions.Read(dir); ok {
		t.Errorf("stale sidecar after reindex must be ignored, got ok with %+v", sc)
	}
}

// TestUpsert_DiscardsStale: after a reindex, an upsert starts fresh (drops the
// stale entries written for the previous generation).
func TestUpsert_DiscardsStale(t *testing.T) {
	dir := t.TempDir()
	writeSingleFileGen(t, dir)
	_ = descriptions.Upsert(dir, "e1", "old-gen desc")
	// Reindex.
	doc := &graph.Document{Repo: "r", Entities: []graph.Entity{{ID: "e2", QualifiedName: "p.B", Kind: "struct", Name: "B"}}}
	_ = fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(2)), doc)
	_ = graph.WriteCurrentPointer(dir, graph.GenFileName(2))
	// Upsert a new-gen entity.
	if err := descriptions.Upsert(dir, "e2", "new-gen desc"); err != nil {
		t.Fatal(err)
	}
	sc, ok := descriptions.Read(dir)
	if !ok {
		t.Fatal("Read not-ok after fresh upsert")
	}
	if _, present := sc.Results["e1"]; present {
		t.Errorf("stale e1 should have been discarded, got %+v", sc.Results)
	}
	if sc.Results["e2"] != "new-gen desc" {
		t.Errorf("Results[e2] = %q", sc.Results["e2"])
	}
}

// TestCurrentSourceKey_JSONOnlyFallback: a repo with only graph.json (no
// graph.fb / gen) derives a key from graph.json's mtime.
func TestCurrentSourceKey_JSONOnlyFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), []byte(`{"entities":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	key := descriptions.CurrentSourceKey(dir)
	if len(key) < 5 || key[:5] != "json:" {
		t.Errorf("JSON-only key = %q, want json: prefix", key)
	}
}

// TestWriteTo_Atomic: the written file is valid JSON with the expected shape.
func TestWriteTo_Atomic(t *testing.T) {
	dir := t.TempDir()
	sc := &descriptions.Sidecar{Version: 1, SourceKey: "k", Results: map[string]string{"x": "y"}}
	if err := descriptions.WriteTo(descriptions.Path(dir), sc); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(descriptions.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	var got descriptions.Sidecar
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if got.Results["x"] != "y" {
		t.Errorf("roundtrip Results = %+v", got.Results)
	}
}
