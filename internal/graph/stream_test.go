package graph_test

// Tests for the streaming prior-graph reader (#5954 item 16).
//
// The contract these lock down is PARITY: a streamed read must yield exactly
// the record SET that LoadGraphFromDir materialises — no record lost, no record
// invented — because the incremental merge switches from the materialised
// Document to the stream and any divergence is silent graph corruption.
//
// Sets are compared BOTH WAYS on purpose. A one-way check ("everything the
// stream produced is in the document") passes trivially when the stream skips
// records, which is exactly the mutation this file has to catch.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// streamFixtureDoc builds a Document that exercises every shape the loader
// treats specially: source-bearing entities, a source-less ext:* synthetic,
// entity properties, relationships with a derivable ID, relationships with a
// NON-derivable (salted) ID that must round-trip through the reserved
// "\x00id" slot, relationship properties, and two edges sharing one
// (from, to, kind) triple.
func streamFixtureDoc() *graph.Document {
	ents := []graph.Entity{
		graph.Entity{
			ID: "e0000000000001", Name: "Alpha", QualifiedName: "pkg.Alpha",
			Kind: "FUNCTION", SourceFile: "a.go", StartLine: 3, Language: "go",
			Signature: "func Alpha()",
		}.WithProperties(map[string]string{"module": "m1", "visibility": "public"}),
		graph.Entity{
			ID: "e0000000000002", Name: "Beta", QualifiedName: "pkg.Beta",
			Kind: "FUNCTION", SourceFile: "b.go", StartLine: 9, Language: "go",
		}.WithProperties(map[string]string{"module": "m1"}),
		{
			ID: "ext:github.com/x/y", Name: "y", Kind: "EXTERNAL_PACKAGE",
			SourceFile: "",
		},
	}
	for i := 0; i < 40; i++ {
		ents = append(ents, graph.Entity{
			ID: fmt.Sprintf("e00000000001%02d", i), Name: fmt.Sprintf("F%d", i),
			Kind: "FUNCTION", SourceFile: fmt.Sprintf("f%02d.go", i), StartLine: i,
		}.WithProperties(map[string]string{"module": "m2"}))
	}

	rels := []graph.Relationship{
		// Derivable ID — the writer omits the reserved slot for this one.
		{
			ID:     graph.RelationshipID("e0000000000001", "e0000000000002", "CALLS"),
			FromID: "e0000000000001", ToID: "e0000000000002", Kind: "CALLS",
		},
		// Salted siblings sharing one triple: identity is NOT derivable and
		// must survive the round-trip or the two collapse into one.
		{
			ID:     "salted-op-1",
			FromID: "e0000000000001", ToID: "e0000000000002", Kind: "MIGRATES",
		},
		{
			ID:     "salted-op-2",
			FromID: "e0000000000001", ToID: "e0000000000002", Kind: "MIGRATES",
		},
		// Edge into a source-less synthetic.
		{
			ID:     graph.RelationshipID("e0000000000002", "ext:github.com/x/y", "IMPORTS"),
			FromID: "e0000000000002", ToID: "ext:github.com/x/y", Kind: "IMPORTS",
		},
		// Edge into an unresolved bare name (not an entity row at all).
		{
			ID:     graph.RelationshipID("e0000000000002", "SomeBareName", "CALLS"),
			FromID: "e0000000000002", ToID: "SomeBareName", Kind: "CALLS",
		},
	}
	rels[1].PropSet("op", "add_column")
	rels[2].PropSet("op", "drop_column")
	for i := 0; i < 60; i++ {
		r := graph.Relationship{
			FromID: fmt.Sprintf("e00000000001%02d", i%40),
			ToID:   fmt.Sprintf("e00000000001%02d", (i+7)%40),
			Kind:   "REFERENCES",
		}
		r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
		r.PropSet("idx", fmt.Sprintf("%d", i))
		rels = append(rels, r)
	}

	return &graph.Document{
		Version:       graph.SchemaVersion,
		GeneratedAt:   time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		Repo:          "stream-fixture",
		Entities:      ents,
		Relationships: rels,
	}
}

func writeStreamFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), streamFixtureDoc()); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	return dir
}

// entityKey / relKeyForTest serialise a record's FULL observable content, so
// a difference in any field shows up as a set difference rather than being
// silently tolerated.
func entityKeyForTest(e graph.Entity) string {
	props := e.PropsSnapshot()
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%s|%s",
		e.ID, e.Name, e.QualifiedName, e.Kind, e.Subtype, e.SourceFile,
		e.StartLine, e.Language, e.Signature)
	for _, k := range keys {
		s += "|" + k + "=" + props[k]
	}
	return s
}

func relKeyForTest(r graph.Relationship) string {
	props := r.PropsSnapshot()
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("%s|%s|%s|%s", r.ID, r.FromID, r.ToID, r.Kind)
	for _, k := range keys {
		s += "|" + k + "=" + props[k]
	}
	return s
}

// diffMultisets reports what is in a but not b, and in b but not a, honouring
// multiplicity (a dropped duplicate row is a real loss).
func diffMultisets(a, b []string) (onlyA, onlyB []string) {
	ca := map[string]int{}
	for _, s := range a {
		ca[s]++
	}
	for _, s := range b {
		ca[s]--
	}
	for s, n := range ca {
		for i := 0; i < n; i++ {
			onlyA = append(onlyA, s)
		}
		for i := 0; i < -n; i++ {
			onlyB = append(onlyB, s)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func TestGraphStream_RecordParityWithLoadGraphFromDir(t *testing.T) {
	t.Parallel()
	dir := writeStreamFixture(t)

	want, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	var gotEnts, gotRels []string
	s.EachEntity(func(e graph.Entity) bool { gotEnts = append(gotEnts, entityKeyForTest(e)); return true })
	s.EachRelationship(func(r graph.Relationship) bool { gotRels = append(gotRels, relKeyForTest(r)); return true })

	var wantEnts, wantRels []string
	for _, e := range want.Entities {
		wantEnts = append(wantEnts, entityKeyForTest(e))
	}
	for _, r := range want.Relationships {
		wantRels = append(wantRels, relKeyForTest(r))
	}

	if lost, invented := diffMultisets(wantEnts, gotEnts); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("entity parity broken: stream LOST %d, INVENTED %d\n lost=%v\n invented=%v",
			len(lost), len(invented), first(lost, 5), first(invented, 5))
	}
	if lost, invented := diffMultisets(wantRels, gotRels); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("relationship parity broken: stream LOST %d, INVENTED %d\n lost=%v\n invented=%v",
			len(lost), len(invented), first(lost, 5), first(invented, 5))
	}

	if s.EntityCount() != len(want.Entities) {
		t.Errorf("EntityCount()=%d want %d", s.EntityCount(), len(want.Entities))
	}
	if s.RelationshipCount() != len(want.Relationships) {
		t.Errorf("RelationshipCount()=%d want %d", s.RelationshipCount(), len(want.Relationships))
	}
}

func first(xs []string, n int) []string {
	if len(xs) < n {
		return xs
	}
	return xs[:n]
}

// A stream that visits fewer records than it claims is the exact silent-skip
// mutation this suite exists to catch, so assert the visit COUNT against the
// header count independently of content parity.
func TestGraphStream_VisitsEveryRecord(t *testing.T) {
	t.Parallel()
	dir := writeStreamFixture(t)
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	nE, nR := 0, 0
	s.EachEntity(func(graph.Entity) bool { nE++; return true })
	s.EachRelationship(func(graph.Relationship) bool { nR++; return true })

	fixture := streamFixtureDoc()
	if nE != len(fixture.Entities) {
		t.Errorf("EachEntity visited %d, fixture has %d", nE, len(fixture.Entities))
	}
	if nR != len(fixture.Relationships) {
		t.Errorf("EachRelationship visited %d, fixture has %d", nR, len(fixture.Relationships))
	}
}

// Re-iterating must be stable: the merge streams entities early (to seed the
// resolver) and again later (to merge), so a one-shot iterator would silently
// merge nothing on the second pass.
func TestGraphStream_IsReIterable(t *testing.T) {
	t.Parallel()
	dir := writeStreamFixture(t)
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	collect := func() ([]string, []string) {
		var e, r []string
		s.EachEntity(func(x graph.Entity) bool { e = append(e, entityKeyForTest(x)); return true })
		s.EachRelationship(func(x graph.Relationship) bool { r = append(r, relKeyForTest(x)); return true })
		return e, r
	}
	e1, r1 := collect()
	e2, r2 := collect()
	if lost, invented := diffMultisets(e1, e2); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("entity pass 2 diverged: lost=%d invented=%d", len(lost), len(invented))
	}
	if lost, invented := diffMultisets(r1, r2); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("relationship pass 2 diverged: lost=%d invented=%d", len(lost), len(invented))
	}
	if len(e1) == 0 || len(r1) == 0 {
		t.Fatal("fixture produced no records — the re-iteration check is vacuous")
	}
}

// Early return must stop the walk (used to bail out of a scan) without
// corrupting a later full pass.
func TestGraphStream_EarlyReturnStopsWalk(t *testing.T) {
	t.Parallel()
	dir := writeStreamFixture(t)
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	n := 0
	s.EachEntity(func(graph.Entity) bool { n++; return n < 3 })
	if n != 3 {
		t.Errorf("early return visited %d entities, want 3", n)
	}
	full := 0
	s.EachEntity(func(graph.Entity) bool { full++; return true })
	if full != s.EntityCount() {
		t.Errorf("after early return, full pass visited %d, want %d", full, s.EntityCount())
	}
}

// The JSON-only fallback has no mmap to stream from; it must still present the
// same iteration contract rather than silently yielding nothing.
func TestGraphStream_JSONFallbackStillStreams(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := streamFixtureDoc()
	if err := writeJSONGraphForTest(t, filepath.Join(dir, "graph.json"), doc); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream (json): %v", err)
	}
	defer s.Close()
	n := 0
	s.EachEntity(func(graph.Entity) bool { n++; return true })
	if n != len(doc.Entities) {
		t.Errorf("json fallback visited %d entities, want %d", n, len(doc.Entities))
	}
	m := 0
	s.EachRelationship(func(graph.Relationship) bool { m++; return true })
	if m != len(doc.Relationships) {
		t.Errorf("json fallback visited %d relationships, want %d", m, len(doc.Relationships))
	}
}

func writeJSONGraphForTest(t *testing.T, path string, doc *graph.Document) error {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// An incompatible graph.fb must be REFUSED at open, exactly as
// LoadGraphFromDir refuses it. Without this gate the stream would hand the
// incremental merge rows decoded against the wrong schema, which is worse than
// falling back to a full reindex.
func TestGraphStream_OldFormatVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeOldFormatGraphFB(t, dir, 2)

	s, err := graph.OpenGraphStream(dir)
	if err == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("OpenGraphStream accepted a below-minimum format version; the incremental path would decode garbage instead of falling back")
	}
	var fvErr *graph.FormatVersionError
	if !errors.As(err, &fvErr) {
		t.Fatalf("expected a *graph.FormatVersionError, got %v", err)
	}
	if fvErr.Found != 2 {
		t.Errorf("FormatVersionError.Found = %d, want 2", fvErr.Found)
	}
}

func TestGraphStream_MissingGraphIsAnError(t *testing.T) {
	t.Parallel()
	if s, err := graph.OpenGraphStream(t.TempDir()); err == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("OpenGraphStream on an empty dir returned nil error; the incremental path relies on this to fall back to a full reindex")
	}
}
