package fbwriter_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// bigDoc builds a graph.Document with n entities (unique, sortable ids) and a
// relationship chain, each entity padded with a property so per-entity
// serialized size is large enough that a small threshold forces many segments.
func bigDoc(repo string, n int) *graph.Document {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Repo:        repo,
	}
	doc.Entities = make([]graph.Entity, 0, n)
	for i := 0; i < n; i++ {
		e := graph.Entity{
			// zero-padded id so lexicographic (byte-wise) order == numeric order.
			ID:            fmt.Sprintf("ent-%08d", i),
			Name:          fmt.Sprintf("sym_%08d", i),
			QualifiedName: fmt.Sprintf("pkg/mod.sym_%08d", i),
			Kind:          "function",
			SourceFile:    fmt.Sprintf("pkg/file_%04d.go", i%1000),
			StartLine:     i,
			Language:      "go",
		}
		e.PropSet("visibility", "public")
		e.PropSet("padding", fmt.Sprintf("payload-%08d-xxxxxxxxxxxxxxxxxxxx", i))
		doc.Entities = append(doc.Entities, e)
	}
	// A relationship chain ent[i] -> ent[i+1], deliberately inserted in REVERSE
	// order so the writer's own sort is exercised.
	for i := n - 1; i >= 1; i-- {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     fmt.Sprintf("rel-%08d", i),
			FromID: fmt.Sprintf("ent-%08d", i-1),
			ToID:   fmt.Sprintf("ent-%08d", i),
			Kind:   "calls",
		})
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return doc
}

// openSegmentSet resolves dir as a segment-set and opens a routed MultiReader
// over it (key ranges attached from the manifest, exactly as a real reader
// would). The caller must Close the returned reader before the TempDir is
// torn down (Windows-safe).
func openSegmentSet(t *testing.T, dir string) (*fbreader.MultiReader, graph.GraphDescriptor) {
	t.Helper()
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("descriptor kind = %v, want GraphSegmentSet", desc.Kind)
	}
	ranges := make([]fbreader.KeyRange, 0, len(desc.Manifest.Segments))
	for _, s := range desc.Manifest.Segments {
		ranges = append(ranges, fbreader.KeyRange{
			HasEntities: s.EntityCount > 0,
			Min:         s.MinKey,
			Max:         s.MaxKey,
		})
	}
	mr, err := fbreader.OpenSegmentsWithRanges(desc.Segments, ranges)
	if err != nil {
		t.Fatalf("OpenSegmentsWithRanges: %v", err)
	}
	return mr, desc
}

// TestSegmentedRoundTrip — the make-or-break correctness test. A graph large
// enough (with a tiny threshold) to force MULTIPLE segments is written via the
// SegmentedWriter, then read back through the segment-aware, key-routed reader.
// EVERY entity must be findable by id and EVERY relationship present — proving
// the writer's per-segment MinKey/MaxKey bounds never cause the reader to
// false-skip a present entity.
func TestSegmentedRoundTrip(t *testing.T) {
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", "65536") // 64 KB — forces many segments
	dir := t.TempDir()
	const n = 3000

	doc := bigDoc("seg-rt", n)
	genPath, err := fbwriter.WriteGraphGen(dir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	// A segment set is a gen DIR, not a .fb file.
	if filepath.Ext(genPath) == ".fb" {
		t.Fatalf("expected a gen dir, got file %q", genPath)
	}

	mr, desc := openSegmentSet(t, dir)
	defer mr.Close()

	if mr.SegmentCount() < 2 {
		t.Fatalf("SegmentCount = %d, want >= 2 (threshold should have split)", mr.SegmentCount())
	}
	if mr.EntityCount() != n {
		t.Fatalf("EntityCount = %d, want %d", mr.EntityCount(), n)
	}
	if got := mr.RelationshipCount(); got != n-1 {
		t.Fatalf("RelationshipCount = %d, want %d", got, n-1)
	}

	// Every entity is findable by id through the ROUTED reader (routing that
	// false-skipped a segment would return nil here).
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("ent-%08d", i)
		e := mr.LookupEntityByID(id)
		if e == nil {
			t.Fatalf("LookupEntityByID(%s) = nil — routing false-skipped a present entity", id)
		}
		if string(e.Id()) != id {
			t.Fatalf("LookupEntityByID(%s) returned id %q", id, e.Id())
		}
	}
	// A negative lookup: an id outside every segment's range is not found and
	// does not spuriously resolve.
	if e := mr.LookupEntityByID("ent-99999999"); e != nil {
		t.Fatalf("lookup of absent id resolved to %q", e.Id())
	}

	// Every relationship is present.
	seen := map[string]bool{}
	mr.IterateRelationships(func(r *fb.Relationship) bool {
		seen[string(r.FromId())+"->"+string(r.ToId())+":"+string(r.Kind())] = true
		return true
	})
	for i := 1; i < n; i++ {
		key := fmt.Sprintf("ent-%08d->ent-%08d:calls", i-1, i)
		if !seen[key] {
			t.Fatalf("relationship %q missing after round-trip", key)
		}
	}

	// Cross-check: the writer's recorded [MinKey,MaxKey] windows must TILE the
	// whole id space with no gap and no overlap — the property that guarantees
	// the reader never false-skips. Entity segments are contiguous and ordered.
	var prevMax string
	for _, s := range desc.Manifest.EntitySegments() {
		if s.MinKey == "" || s.MaxKey == "" {
			t.Fatalf("entity segment %s missing key bounds", s.File)
		}
		if s.MinKey > s.MaxKey {
			t.Fatalf("segment %s has MinKey %q > MaxKey %q", s.File, s.MinKey, s.MaxKey)
		}
		if prevMax != "" && !(prevMax < s.MinKey) {
			t.Fatalf("segment %s MinKey %q overlaps previous MaxKey %q", s.File, s.MinKey, prevMax)
		}
		prevMax = s.MaxKey
	}
}

// TestSegmentedBoundedBuilder — with a small threshold, N entities produce
// multiple segments each ~<= threshold, and no single segment file holds the
// whole graph. This is the bounded-memory proxy: peak builder ~ one segment.
func TestSegmentedBoundedBuilder(t *testing.T) {
	const threshold = 64 * 1024
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", fmt.Sprintf("%d", threshold))
	dir := t.TempDir()
	const n = 4000

	if _, err := fbwriter.WriteGraphGen(dir, bigDoc("seg-bound", n)); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("kind = %v, want segment set", desc.Kind)
	}
	if len(desc.Segments) < 3 {
		t.Fatalf("segment count = %d, want several (>=3)", len(desc.Segments))
	}
	// Every segment file is bounded near the threshold. Each segment finalizes
	// only AFTER a record crosses the threshold, so allow the header/vector
	// finalize overhead plus one oversized record of slack.
	const slack = 64 * 1024
	for _, p := range desc.Segments {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.Size() > threshold+slack {
			t.Fatalf("segment %s = %d bytes, exceeds threshold %d + slack %d — builder not bounded",
				filepath.Base(p), fi.Size(), threshold, slack)
		}
	}
}

// TestSingleFileFastPath — a small graph under the threshold takes the flat
// path even with the flag ON: a plain graph.<gen>.fb, no dir, no manifest, and
// byte-identical to the flag-OFF write of the same doc.
func TestSingleFileFastPath(t *testing.T) {
	// Flag-OFF reference write.
	offDir := t.TempDir()
	offPath, err := fbwriter.WriteGraphGen(offDir, smallDoc("fast"))
	if err != nil {
		t.Fatalf("flag-off WriteGraphGen: %v", err)
	}
	offBytes, err := os.ReadFile(offPath)
	if err != nil {
		t.Fatalf("read off bytes: %v", err)
	}

	// Flag-ON write of an identical doc (default large threshold).
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	onDir := t.TempDir()
	onPath, err := fbwriter.WriteGraphGen(onDir, smallDoc("fast"))
	if err != nil {
		t.Fatalf("flag-on WriteGraphGen: %v", err)
	}
	if filepath.Base(onPath) != "graph.1.fb" {
		t.Fatalf("flag-on small graph wrote %q, want graph.1.fb (single-file fast path)", filepath.Base(onPath))
	}
	// No gen dir, no manifest.
	if _, err := os.Stat(filepath.Join(onDir, "graph.1")); err == nil {
		t.Fatalf("single-file fast path wrote a gen dir graph.1/")
	}
	desc, err := graph.CurrentGraphDescriptor(onDir)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	if desc.Kind != graph.GraphSingleFile {
		t.Fatalf("kind = %v, want GraphSingleFile", desc.Kind)
	}
	onBytes, err := os.ReadFile(onPath)
	if err != nil {
		t.Fatalf("read on bytes: %v", err)
	}
	if string(offBytes) != string(onBytes) {
		t.Fatalf("single-file fast path bytes differ from flag-off write (%d vs %d bytes)",
			len(offBytes), len(onBytes))
	}
}

// TestFlagOffParity — with GRAFEL_STREAM_SEGMENTS unset, even a graph that
// WOULD split under a tiny threshold is written as a single flat file (the
// flag gates the whole segmented path; threshold is irrelevant when OFF).
func TestFlagOffParity(t *testing.T) {
	// Threshold set tiny, but the stream flag is UNSET — segmented path must
	// not engage.
	t.Setenv("GRAFEL_SEGMENT_BYTES", "1024")
	os.Unsetenv("GRAFEL_STREAM_SEGMENTS")
	dir := t.TempDir()
	genPath, err := fbwriter.WriteGraphGen(dir, bigDoc("parity", 2000))
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	if filepath.Base(genPath) != "graph.1.fb" {
		t.Fatalf("flag-off wrote %q, want single flat graph.1.fb", filepath.Base(genPath))
	}
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	if desc.Kind != graph.GraphSingleFile {
		t.Fatalf("kind = %v, want GraphSingleFile (flag OFF)", desc.Kind)
	}
}

// TestSegmentedIndependentStreams — entity segments and relationship segments
// are SEPARATE files with the correct Kind and counts, and the per-stream
// totals recorded in the manifest sum to the full graph.
func TestSegmentedIndependentStreams(t *testing.T) {
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", "65536")
	dir := t.TempDir()
	const n = 3000

	if _, err := fbwriter.WriteGraphGen(dir, bigDoc("seg-indep", n)); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	m := desc.Manifest
	entSegs := m.EntitySegments()
	relSegs := m.RelationshipSegments()
	if len(entSegs) == 0 || len(relSegs) == 0 {
		t.Fatalf("want both entity (%d) and relationship (%d) segments", len(entSegs), len(relSegs))
	}
	// Streams are disjoint files: no segment carries both entities and rels.
	for _, s := range m.Segments {
		if s.EntityCount > 0 && s.RelCount > 0 {
			t.Fatalf("segment %s mixes streams (ent=%d rel=%d)", s.File, s.EntityCount, s.RelCount)
		}
	}
	// Kind matches the populated count.
	for _, s := range entSegs {
		if s.Kind != graph.SegmentEntities {
			t.Fatalf("entity segment %s has kind %q", s.File, s.Kind)
		}
	}
	for _, s := range relSegs {
		if s.Kind != graph.SegmentRelationships {
			t.Fatalf("relationship segment %s has kind %q", s.File, s.Kind)
		}
	}
	if m.TotalEntityCount() != n {
		t.Fatalf("manifest total entities = %d, want %d", m.TotalEntityCount(), n)
	}
	if m.TotalRelationshipCount() != n-1 {
		t.Fatalf("manifest total rels = %d, want %d", m.TotalRelationshipCount(), n-1)
	}
}
