package fbwriter_test

// Tests for the CreateSharedString interning optimization (grafel memory
// optimization: dedupe repeated relationship endpoints / entity metadata
// strings). See internal/graph/fbwriter/writer.go buildEntity/buildRelationship.
//
// Background: on the real corpus (427,261 entities / 1,852,582 relationships)
// from_id/to_id endpoint strings are repeated ~8.7x on average (avg node
// degree), and kind/language/source_file/module/property-key strings repeat
// heavily across entities. CreateSharedString dedupes identical string
// content to a single stored copy + reused 4-byte offsets, which is a
// byte-valid FlatBuffer under the unchanged schema (offset sharing is
// transparent to any spec-compliant reader).

import (
	"os"
	"path/filepath"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// buildDupRelGraph builds a document with two entities and n relationships
// that ALL reference the same two entity IDs with the same Kind — the
// maximally-duplicated shape that CreateSharedString should collapse.
func buildDupRelGraph(n int) *graph.Document {
	doc := &graph.Document{
		Repo: "dedup-fixture",
		Entities: []graph.Entity{
			graph.Entity{ID: "ent00000000000a01", Name: "A", Kind: "function", SourceFile: "shared/file.go", Language: "go"}.WithProperties(map[string]string{"module": "pkg/shared"}),
			graph.Entity{ID: "ent00000000000b02", Name: "B", Kind: "function", SourceFile: "shared/file.go", Language: "go"}.WithProperties(map[string]string{"module": "pkg/shared"}),
		},
	}
	for i := 0; i < n; i++ {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			FromID: "ent00000000000a01", ToID: "ent00000000000b02", Kind: "calls",
		})
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return doc
}

// TestSharedStringDedup_ReducesSize is the dedup-proof test. It computes an
// analytical lower bound on the total from_id + to_id + kind STRING CONTENT
// bytes that would be required if every relationship stored its own full
// copy of these three strings with no framing overhead whatsoever (i.e. the
// bare minimum any "no sharing" implementation could possibly achieve — the
// real "no sharing" cost is always higher once length prefixes, null
// terminators, padding, and per-record table/vtable overhead are counted).
// If CreateSharedString is wired up, the actual serialized graph.fb — which
// DOES include all of that framing — must still come in UNDER this bare
// content-only bound, because the repeated strings are stored exactly once
// rather than n times. That is only possible through string interning.
func TestSharedStringDedup_ReducesSize(t *testing.T) {
	const n = 5000
	dup := buildDupRelGraph(n)

	bufDup, err := fbwriter.Marshal(dup)
	if err != nil {
		t.Fatalf("marshal dup: %v", err)
	}

	fromID, toID, kind := "ent00000000000a01", "ent00000000000b02", "calls"
	perRelStringContentBytes := len(fromID) + len(toID) + len(kind)
	naiveContentOnlyLowerBound := n * perRelStringContentBytes

	t.Logf("dup graph (%d rels, 2 entities): %d bytes (%.2f bytes/rel); naive per-rel-copy string-content-only lower bound: %d bytes (%.2f bytes/rel)",
		n, len(bufDup), float64(len(bufDup))/float64(n), naiveContentOnlyLowerBound, float64(naiveContentOnlyLowerBound)/float64(n))

	if len(bufDup) >= naiveContentOnlyLowerBound {
		t.Errorf("expected the fully-framed graph.fb (%d bytes) to be SMALLER than the bare string-content-only naive lower bound (%d bytes) — this is only possible if from_id/to_id/kind are being interned rather than copied per relationship",
			len(bufDup), naiveContentOnlyLowerBound)
	}

	// Correctness: read back and verify every relationship still resolves to
	// the correct (shared) entity IDs and kind — dedup must be transparent.
	dupPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := os.WriteFile(dupPath, bufDup, 0o644); err != nil {
		t.Fatalf("write dup buf: %v", err)
	}
	r, err := fbreader.Open(dupPath)
	if err != nil {
		t.Fatalf("open dup buf: %v", err)
	}
	defer r.Close()

	if got := r.RelationshipCount(); got != n {
		t.Fatalf("relationship count: got %d want %d", got, n)
	}
	for i := 0; i < n; i++ {
		rel := r.RelationshipAt(i)
		if rel == nil {
			t.Fatalf("relationship %d: nil", i)
		}
		if got := string(rel.FromId()); got != "ent00000000000a01" {
			t.Errorf("relationship %d FromId: got %q want %q", i, got, "ent00000000000a01")
		}
		if got := string(rel.ToId()); got != "ent00000000000b02" {
			t.Errorf("relationship %d ToId: got %q want %q", i, got, "ent00000000000b02")
		}
		if got := string(rel.Kind()); got != "calls" {
			t.Errorf("relationship %d Kind: got %q want %q", i, got, "calls")
		}
	}
}

// TestSharedStringDedup_RoundtripCorrectness verifies that entities and
// relationships sharing common Kind/Language/SourceFile/Module/property-key
// strings each still read back with their OWN correct field values — i.e.
// dedup only shares identical byte content, it never conflates distinct
// entities.
func TestSharedStringDedup_RoundtripCorrectness(t *testing.T) {
	doc := &graph.Document{
		Repo: "dedup-correctness",
		Entities: []graph.Entity{
			graph.Entity{ID: "ent0000000000000a", Name: "Alpha", Kind: "function", SourceFile: "pkg/shared/file.go", Language: "go"}.WithProperties(map[string]string{"module": "pkg/shared", "visibility": "public"}),
			graph.Entity{ID: "ent0000000000000b", Name: "Beta", Kind: "function", SourceFile: "pkg/shared/file.go", Language: "go"}.WithProperties(map[string]string{"module": "pkg/shared", "visibility": "private"}),
			graph.Entity{ID: "ent0000000000000c", Name: "Gamma", Kind: "type", SourceFile: "pkg/other/file2.go", Language: "python"}.WithProperties(map[string]string{"module": "pkg/other", "visibility": "public"}),
		},
		Relationships: []graph.Relationship{
			{FromID: "ent0000000000000a", ToID: "ent0000000000000b", Kind: "calls"},
			{FromID: "ent0000000000000a", ToID: "ent0000000000000c", Kind: "calls"},
			{FromID: "ent0000000000000b", ToID: "ent0000000000000c", Kind: "references"},
		},
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	out := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(out, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byID := make(map[string]*graph.Entity, len(got.Entities))
	for i := range got.Entities {
		byID[got.Entities[i].ID] = &got.Entities[i]
	}

	a := byID["ent0000000000000a"]
	b := byID["ent0000000000000b"]
	c := byID["ent0000000000000c"]
	if a == nil || b == nil || c == nil {
		t.Fatalf("missing entities: a=%v b=%v c=%v", a, b, c)
	}
	if a.Kind != "function" || b.Kind != "function" || c.Kind != "type" {
		t.Errorf("kind mismatch: a=%q b=%q c=%q", a.Kind, b.Kind, c.Kind)
	}
	if a.SourceFile != "pkg/shared/file.go" || b.SourceFile != "pkg/shared/file.go" || c.SourceFile != "pkg/other/file2.go" {
		t.Errorf("source_file mismatch: a=%q b=%q c=%q", a.SourceFile, b.SourceFile, c.SourceFile)
	}
	if a.Language != "go" || b.Language != "go" || c.Language != "python" {
		t.Errorf("language mismatch: a=%q b=%q c=%q", a.Language, b.Language, c.Language)
	}
	if a.PropGet("module") != "pkg/shared" || c.PropGet("module") != "pkg/other" {
		t.Errorf("module mismatch: a=%q c=%q", a.PropGet("module"), c.PropGet("module"))
	}
	if a.PropGet("visibility") != "public" || b.PropGet("visibility") != "private" {
		t.Errorf("visibility mismatch: a=%q b=%q", a.PropGet("visibility"), b.PropGet("visibility"))
	}

	if len(got.Relationships) != 3 {
		t.Fatalf("relationship count: got %d want 3", len(got.Relationships))
	}
	byPair := map[[2]string]string{}
	for _, rel := range got.Relationships {
		byPair[[2]string{rel.FromID, rel.ToID}] = rel.Kind
	}
	if byPair[[2]string{"ent0000000000000a", "ent0000000000000b"}] != "calls" {
		t.Errorf("a->b kind mismatch")
	}
	if byPair[[2]string{"ent0000000000000a", "ent0000000000000c"}] != "calls" {
		t.Errorf("a->c kind mismatch")
	}
	if byPair[[2]string{"ent0000000000000b", "ent0000000000000c"}] != "references" {
		t.Errorf("b->c kind mismatch")
	}
}

// TestSharedStringDedup_RawSpecCompliance verifies that a graph.fb produced
// with string interning is decoded correctly when read directly through the
// low-level generated fb.GetRootAsGraph accessors (bypassing fbreader/graph
// entirely) — i.e. offset sharing is invisible to ANY spec-compliant
// FlatBuffers reader, old or new, because CreateSharedString only reuses
// already-written, byte-identical string vectors; it does not change the
// schema or vtable layout.
func TestSharedStringDedup_RawSpecCompliance(t *testing.T) {
	doc := buildDupRelGraph(50)
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	root := fb.GetRootAsGraph(buf, 0)
	if got := root.RelationshipsLength(); got != 50 {
		t.Fatalf("relationships length: got %d want 50", got)
	}
	var rel fb.Relationship
	for i := 0; i < 50; i++ {
		if !root.Relationships(&rel, i) {
			t.Fatalf("relationship %d: accessor returned false", i)
		}
		if got := string(rel.FromId()); got != "ent00000000000a01" {
			t.Errorf("relationship %d raw FromId: got %q", i, got)
		}
		if got := string(rel.ToId()); got != "ent00000000000b02" {
			t.Errorf("relationship %d raw ToId: got %q", i, got)
		}
	}
	if got := root.EntitiesLength(); got != 2 {
		t.Fatalf("entities length: got %d want 2", got)
	}
	var ent fb.Entity
	if !root.Entities(&ent, 0) {
		t.Fatalf("entity 0: accessor returned false")
	}
	if got := string(ent.Kind()); got != "function" {
		t.Errorf("entity 0 raw Kind: got %q", got)
	}
	if got := string(ent.SourceFile()); got != "shared/file.go" {
		t.Errorf("entity 0 raw SourceFile: got %q", got)
	}
}

// TestSharedStringDedup_OldWriterFormatStillReadable is the explicit
// backward-compat check: it hand-builds a graph.fb using ONLY plain
// CreateString (i.e. byte-for-byte what the pre-interning writer produced —
// no shared-offset reuse at all) and verifies the CURRENT loader
// (graph.LoadGraphFromDir, which goes through fbreader) still reads it
// correctly. CreateSharedString only changes how the WRITER assigns offsets;
// it does not touch the schema, vtable layout, or field encoding, so a file
// written entirely with CreateString must remain fully readable by the new
// code with no FormatVersion bump required.
func TestSharedStringDedup_OldWriterFormatStillReadable(t *testing.T) {
	b := flatbuffers.NewBuilder(1024)

	// entity 0 ("a")
	idA := b.CreateString("ent00000000000a01")
	nameA := b.CreateString("A")
	kindA := b.CreateString("function")
	srcA := b.CreateString("shared/file.go")
	langA := b.CreateString("go")
	fb.EntityStartPropertiesVector(b, 0)
	propsA := b.EndVector(0)
	fb.EntityStart(b)
	fb.EntityAddId(b, idA)
	fb.EntityAddName(b, nameA)
	fb.EntityAddKind(b, kindA)
	fb.EntityAddSourceFile(b, srcA)
	fb.EntityAddProperties(b, propsA)
	fb.EntityAddLanguage(b, langA)
	entA := fb.EntityEnd(b)

	// entity 1 ("b") — deliberately re-creates the SAME string content via
	// independent CreateString calls, exactly mimicking the pre-interning
	// writer where identical content was copied per-record rather than
	// shared.
	idB := b.CreateString("ent00000000000b02")
	nameB := b.CreateString("B")
	kindB := b.CreateString("function")
	srcB := b.CreateString("shared/file.go")
	langB := b.CreateString("go")
	fb.EntityStartPropertiesVector(b, 0)
	propsB := b.EndVector(0)
	fb.EntityStart(b)
	fb.EntityAddId(b, idB)
	fb.EntityAddName(b, nameB)
	fb.EntityAddKind(b, kindB)
	fb.EntityAddSourceFile(b, srcB)
	fb.EntityAddProperties(b, propsB)
	fb.EntityAddLanguage(b, langB)
	entB := fb.EntityEnd(b)

	fb.GraphStartEntitiesVector(b, 2)
	b.PrependUOffsetT(entB)
	b.PrependUOffsetT(entA)
	entitiesVec := b.EndVector(2)

	// One relationship a->b, kind "calls" — again via independent
	// CreateString calls (old-writer shape).
	fromID := b.CreateString("ent00000000000a01")
	toID := b.CreateString("ent00000000000b02")
	relKind := b.CreateString("calls")
	fb.EntityStartPropertiesVector(b, 0)
	relProps := b.EndVector(0)
	fb.RelationshipStart(b)
	fb.RelationshipAddFromId(b, fromID)
	fb.RelationshipAddToId(b, toID)
	fb.RelationshipAddKind(b, relKind)
	fb.RelationshipAddProperties(b, relProps)
	rel := fb.RelationshipEnd(b)

	fb.GraphStartRelationshipsVector(b, 1)
	b.PrependUOffsetT(rel)
	relsVec := b.EndVector(1)

	fb.GraphStartCommunitiesVector(b, 0)
	commsVec := b.EndVector(0)

	repoTag := b.CreateString("old-writer-fixture")
	computedAt := b.CreateString("2026-01-01T00:00:00Z")

	fb.GraphStart(b)
	fb.GraphAddVersion(b, int32(fbwriter.FormatVersion))
	fb.GraphAddComputedAt(b, computedAt)
	fb.GraphAddRepoTag(b, repoTag)
	fb.GraphAddEntities(b, entitiesVec)
	fb.GraphAddRelationships(b, relsVec)
	fb.GraphAddCommunities(b, commsVec)
	root := fb.GraphEnd(b)
	fb.FinishGraphBuffer(b, root)

	dir := t.TempDir()
	path := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(path, b.FinishedBytes(), 0o644); err != nil {
		t.Fatalf("write old-format fixture: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("current loader failed to read old-writer-format graph.fb: %v", err)
	}
	if len(got.Entities) != 2 || len(got.Relationships) != 1 {
		t.Fatalf("counts: entities=%d relationships=%d", len(got.Entities), len(got.Relationships))
	}
	byID := map[string]*graph.Entity{}
	for i := range got.Entities {
		byID[got.Entities[i].ID] = &got.Entities[i]
	}
	if byID["ent00000000000a01"] == nil || byID["ent00000000000b02"] == nil {
		t.Fatalf("entities not found: %+v", byID)
	}
	if byID["ent00000000000a01"].Kind != "function" || byID["ent00000000000a01"].Language != "go" {
		t.Errorf("entity a mismatch: %+v", byID["ent00000000000a01"])
	}
	r := got.Relationships[0]
	if r.FromID != "ent00000000000a01" || r.ToID != "ent00000000000b02" || r.Kind != "calls" {
		t.Errorf("relationship mismatch: %+v", r)
	}
}
