// Tests for the FB-first graph loader introduced by ADR-0016 flip-day (#808).
package graph_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// makeTestDoc creates a small Document for use in loader tests.
func makeTestDoc() *graph.Document {
	return &graph.Document{
		Version:     graph.SchemaVersion,
		GeneratedAt: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
		Repo:        "test-repo",
		Entities: []graph.Entity{
			graph.Entity{
				ID:            "aabbccdd00000001",
				Name:          "MyHandler",
				QualifiedName: "pkg.MyHandler",
				Kind:          "FUNCTION",
				SourceFile:    "handler.go",
				StartLine:     10,
			}.WithProperties(map[string]string{"language": "go"}),
			graph.Entity{
				ID:            "aabbccdd00000002",
				Name:          "OtherFunc",
				QualifiedName: "pkg.OtherFunc",
				Kind:          "FUNCTION",
				SourceFile:    "other.go",
				StartLine:     5,
			}.WithProperties(map[string]string{"language": "go"}),
		},
		Relationships: []graph.Relationship{
			{
				ID:     "rel-001",
				FromID: "aabbccdd00000001",
				ToID:   "aabbccdd00000002",
				Kind:   "CALLS",
			},
		},
	}
}

// TestLoadGraphFromDir_FBOnly verifies that LoadGraphFromDir loads from
// graph.fb when only the binary file is present.
func TestLoadGraphFromDir_FBOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if got.Repo != doc.Repo {
		t.Errorf("repo: got %q want %q", got.Repo, doc.Repo)
	}
	if len(got.Entities) != len(doc.Entities) {
		t.Errorf("entities: got %d want %d", len(got.Entities), len(doc.Entities))
	}
	if len(got.Relationships) != len(doc.Relationships) {
		t.Errorf("relationships: got %d want %d", len(got.Relationships), len(doc.Relationships))
	}
}

// TestLoadGraphFromDir_JSONOnly verifies the JSON fallback path.
func TestLoadGraphFromDir_JSONOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if got.Repo != doc.Repo {
		t.Errorf("repo: got %q want %q", got.Repo, doc.Repo)
	}
	if len(got.Entities) != len(doc.Entities) {
		t.Errorf("entities: got %d want %d", len(got.Entities), len(doc.Entities))
	}
}

// TestLoadGraphFromDir_BothPresent verifies that graph.fb is preferred when
// both files exist.
func TestLoadGraphFromDir_BothPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	// Write graph.fb.
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	// Write a graph.json with a different Repo tag so we can tell which
	// file LoadGraphFromDir actually read.
	docJSON := makeTestDoc()
	docJSON.Repo = "json-repo"
	b, err := json.Marshal(docJSON)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	// Should have read from graph.fb (Repo = "test-repo"), NOT graph.json.
	if got.Repo != doc.Repo {
		t.Errorf("expected fb-sourced repo %q, got %q — LoadGraphFromDir did not prefer graph.fb",
			doc.Repo, got.Repo)
	}
}

// TestLoadGraphFromDir_NeitherPresent verifies that an error is returned
// when the directory is empty.
func TestLoadGraphFromDir_NeitherPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := graph.LoadGraphFromDir(dir)
	if err == nil {
		t.Fatal("expected error when neither graph.fb nor graph.json exists")
	}
}

// TestLoadGraphFromDir_EntityProperties verifies that Properties on
// entities are preserved through the FB round-trip.
func TestLoadGraphFromDir_EntityProperties(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	doc.Entities[0].PropSet("framework", "gin")

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var handlerEnt *graph.Entity
	for i := range got.Entities {
		if got.Entities[i].Name == "MyHandler" {
			handlerEnt = &got.Entities[i]
			break
		}
	}
	if handlerEnt == nil {
		t.Fatal("MyHandler entity not found after FB round-trip")
	}
	if handlerEnt.PropGet("framework") != "gin" {
		t.Errorf("Properties[framework]: got %q want %q",
			handlerEnt.PropGet("framework"), "gin")
	}
}

// TestLoadGraphFromDir_EmbeddingRefRoundTrip verifies that an entity's
// EmbeddingRef (PH8 / #2100) is preserved through the FB round-trip.
//
// Regression test: fbEntityToGraphEntity (the shared FB->Document entity
// conversion used by both the single-file and segment-set load paths) never
// copied e.EmbeddingRef() into the resulting graph.Entity, even though
// fbwriter correctly persists it. Every FB-backed load (single-file or
// segment-set) silently came back with EmbeddingRef == "" regardless of what
// was written, which defeated internal/cli/cleanup.go's
// collectActiveEmbeddingHashes: an entity's embedding could never be
// reported "active", so the embedding-cache TTL sweep in `grafel cleanup`
// determined unreferenced-ness by age alone. Discovered while building the
// #5915 J2 slice-3 segment-set fixture for that walk. Purely additive fix:
// EmbeddingRef was always the zero value before, so this can only ever
// populate a previously-empty field.
func TestLoadGraphFromDir_EmbeddingRefRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := makeTestDoc()

	doc.Entities[0].EmbeddingRef = "sha256:embedding-round-trip-hash"

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	var handlerEnt *graph.Entity
	for i := range got.Entities {
		if got.Entities[i].Name == "MyHandler" {
			handlerEnt = &got.Entities[i]
			break
		}
	}
	if handlerEnt == nil {
		t.Fatal("MyHandler entity not found after FB round-trip")
	}
	if handlerEnt.EmbeddingRef != "sha256:embedding-round-trip-hash" {
		t.Errorf("EmbeddingRef: got %q want %q",
			handlerEnt.EmbeddingRef, "sha256:embedding-round-trip-hash")
	}
}

// TestLoadGraphFromDir_RelationshipIdentitySurvivesFBRoundTrip pins #6085:
// graph.fb must preserve Relationship.ID for edges whose ID is NOT derivable
// from (from, to, kind).
//
// The IDs below are not invented for the test — they are built with the exact
// salting scheme of real producers, which is the contract that matters:
//
//	internal/engine/migration_schema_ops.go:134  RelationshipID(f,t,kind+"\x00"+op)
//	internal/links/phantom_edges.go:149          RelationshipID(f,t,"CALLS:phantom:"+method)
//	internal/engine/process_flow.go:475          RelationshipID(f,t,kind+":"+stepIndex)
//	internal/graph/tests_walkup.go:161           domain-prefixed 4-tuple
//	internal/daemon/mcp/handlers.go:177          residual edge id
//
// All of them store a PLAIN Kind, so all of them collide under
// RelationshipID(from, to, kind). Asserting only that the loader reproduces
// graph.RelationshipID would pin the loader against itself and pass while
// three distinct edges collapse onto one identity. The assertions here are on
// the ORIGINAL IDs the producer minted.
func TestLoadGraphFromDir_RelationshipIdentitySurvivesFBRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const from, to = "aabbccdd00000001", "aabbccdd00000002"
	doc := makeTestDoc()
	// One ordinary edge (ID derivable from the triple) …
	plain := graph.Relationship{
		ID: graph.RelationshipID(from, to, "CALLS"), FromID: from, ToID: to, Kind: "CALLS",
	}
	// … and three salted edges that share a single (from, to, kind) triple.
	salted := func(op string) graph.Relationship {
		return graph.Relationship{
			ID:     graph.RelationshipID(from, to, "MODIFIES\x00"+op),
			FromID: from, ToID: to, Kind: "MODIFIES",
		}.WithProperties(map[string]string{"op": op})
	}
	ops := []string{"create_table", "add_column", "drop_column"}
	doc.Relationships = []graph.Relationship{plain, salted(ops[0]), salted(ops[1]), salted(ops[2])}

	wantIDs := map[string]bool{}
	for _, r := range doc.Relationships {
		wantIDs[r.ID] = true
	}
	if len(wantIDs) != 4 {
		t.Fatalf("fixture is wrong: %d distinct IDs, want 4", len(wantIDs))
	}

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(got.Relationships) != 4 {
		t.Fatalf("relationships after round-trip: got %d want 4", len(got.Relationships))
	}
	gotIDs := map[string]bool{}
	for _, r := range got.Relationships {
		if r.ID == "" {
			t.Errorf("edge %s→%s (%s) lost its ID in the FB round-trip", r.FromID, r.ToID, r.Kind)
		}
		gotIDs[r.ID] = true
	}
	if len(gotIDs) != 4 {
		t.Errorf("round-trip collapsed 4 distinct edge identities onto %d (#6085): %v", len(gotIDs), gotIDs)
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("producer-minted ID %q did not survive the FB round-trip", id)
		}
	}

	// The reserved id slot is identity, not a property: a round-tripped edge
	// must carry exactly the properties it was written with.
	for _, r := range got.Relationships {
		if _, leaked := r.PropLookup(graph.RelationshipIDProperty); leaked {
			t.Errorf("edge %s leaked the reserved %q key into Properties",
				r.ID, graph.RelationshipIDProperty)
		}
		if r.Kind == "MODIFIES" {
			if op, ok := r.PropLookup("op"); !ok || !slices.Contains(ops, op) {
				t.Errorf("edge %s lost its op property (got %q, ok=%v)", r.ID, op, ok)
			}
		}
	}
}

// TestFBWriter_ReservedIDSlotOnlyForNonDerivableIDs guards the write predicate
// itself (#6085). fbwriter persists the reserved identity slot ONLY when the ID
// cannot be recomputed from (from, to, kind); an unsalted edge must cost zero
// extra bytes on disk and be reconstructed by the loader instead.
//
// Without this, "always persist the ID" is an unguarded claim: it round-trips
// identically, so every behavioural test still passes while the graph pays ~16
// bytes plus a property entry on every edge in the corpus. The assertion is on
// the RAW property-vector length, which is the only place the difference shows.
func TestFBWriter_ReservedIDSlotOnlyForNonDerivableIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const from, to = "aabbccdd00000001", "aabbccdd00000002"
	doc := makeTestDoc()
	doc.Relationships = []graph.Relationship{
		// Derivable ID: the slot must NOT be written — 1 property on disk.
		graph.Relationship{
			ID: graph.RelationshipID(from, to, "CALLS"), FromID: from, ToID: to, Kind: "CALLS",
		}.WithProperties(map[string]string{"line": "12"}),
		// Salted ID: the slot IS written — 2 properties on disk.
		graph.Relationship{
			ID: graph.RelationshipID(from, to, "MODIFIES\x00add_column"), FromID: from, ToID: to, Kind: "MODIFIES",
		}.WithProperties(map[string]string{"line": "12"}),
	}

	path := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	r, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	defer r.Close()

	if got := r.RelationshipCount(); got != 2 {
		t.Fatalf("relationship count on disk = %d, want 2", got)
	}
	wantRaw := []int{1, 2} // derivable → no slot; salted → slot present
	for i, want := range wantRaw {
		if got := r.RelationshipAt(i).PropertiesLength(); got != want {
			t.Errorf("rel[%d] on-disk property count = %d, want %d — the write predicate changed",
				i, got, want)
		}
	}

	// And the heap view of both is identical: one property, right ID.
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	for i, rel := range loaded.Relationships {
		if rel.PropLen() != 1 {
			t.Errorf("rel[%d] loaded PropLen=%d want 1 (reserved slot must not surface)", i, rel.PropLen())
		}
		if rel.ID != doc.Relationships[i].ID {
			t.Errorf("rel[%d] ID=%q want %q", i, rel.ID, doc.Relationships[i].ID)
		}
	}
}

// TestFBWriter_ProducerSuppliedIDPropertyIsNotIdentity pins the other half of
// the reserved-key choice (#6085): a relationship property literally called
// "id" is an ORDINARY property. It must round-trip as one, and it must not be
// mistaken for identity in either direction — neither swallowed into
// Relationship.ID and deleted from the payload, nor overwritten by the writer.
// Such relationships exist in tree (internal/mcp/mmapview_test.go:70), which is
// why RelationshipIDProperty carries a NUL that no producer can emit.
func TestFBWriter_ProducerSuppliedIDPropertyIsNotIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const from, to = "aabbccdd00000001", "aabbccdd00000002"
	saltedID := graph.RelationshipID(from, to, "MODIFIES\x00add_column")
	doc := makeTestDoc()
	doc.Relationships = []graph.Relationship{
		graph.Relationship{ID: saltedID, FromID: from, ToID: to, Kind: "MODIFIES"}.
			WithProperties(map[string]string{"id": "edge-0001", "line": "12"}),
	}

	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("relationships: got %d want 1", len(got.Relationships))
	}
	rel := got.Relationships[0]
	if rel.ID != saltedID {
		t.Errorf("ID=%q want %q — a producer's \"id\" property must not become identity", rel.ID, saltedID)
	}
	if v, ok := rel.PropLookup("id"); !ok || v != "edge-0001" {
		t.Errorf(`PropLookup("id") = (%q,%v), want ("edge-0001",true) — the producer's property was swallowed`, v, ok)
	}
	if rel.PropLen() != 2 {
		t.Errorf("PropLen=%d want 2 (id, line)", rel.PropLen())
	}
}
