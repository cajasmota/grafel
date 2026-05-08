package enrichment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

func mkDoc(es ...graph.Entity) *graph.Document {
	return &graph.Document{
		Version:       graph.SchemaVersion,
		Repo:          "testrepo",
		Entities:      es,
		Relationships: nil,
	}
}

// Test 1: an entity with no description triggers exactly one
// describe_entity candidate.
func TestEmitFor_DescribeEntity_NoDescription(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "e1", Name: "AuthService", Kind: "class"})
	cands := CollectCandidates(doc, []CandidateEmitter{describeEntityEmitter{}}, nil)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].Kind != KindDescribeEntity {
		t.Fatalf("kind = %q, want %q", cands[0].Kind, KindDescribeEntity)
	}
	if cands[0].SubjectID != "e1" {
		t.Fatalf("subject_id = %q, want e1", cands[0].SubjectID)
	}
	// Already-described entity → no candidate.
	doc2 := mkDoc(graph.Entity{
		ID: "e2", Name: "X", Kind: "class",
		Properties: map[string]string{"description": "already set"},
	})
	if got := CollectCandidates(doc2, []CandidateEmitter{describeEntityEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("expected 0 candidates for described entity, got %d", len(got))
	}
}

// Test 2: a god node triggers a describe_role candidate.
func TestEmitFor_DescribeRole_GodNode(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "g1", Name: "Coordinator", Kind: "class", IsGodNode: true})
	cands := CollectCandidates(doc, []CandidateEmitter{describeRoleEmitter{}}, nil)
	if len(cands) != 1 {
		t.Fatalf("expected 1 describe_role candidate, got %d", len(cands))
	}
	if cands[0].Kind != KindDescribeRole {
		t.Fatalf("kind = %q, want %q", cands[0].Kind, KindDescribeRole)
	}

	// Articulation-point also qualifies.
	doc2 := mkDoc(graph.Entity{ID: "a1", Name: "Bridge", Kind: "class", IsArticulationPt: true})
	if got := CollectCandidates(doc2, []CandidateEmitter{describeRoleEmitter{}}, nil); len(got) != 1 {
		t.Fatalf("articulation point: expected 1 candidate, got %d", len(got))
	}

	// A vanilla entity should NOT trigger describe_role.
	doc3 := mkDoc(graph.Entity{ID: "v1", Name: "Plain", Kind: "function"})
	if got := CollectCandidates(doc3, []CandidateEmitter{describeRoleEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("vanilla entity: expected 0, got %d", len(got))
	}
}

// Test 3: emitting twice produces identical IDs (idempotence).
func TestEmit_Idempotent(t *testing.T) {
	doc := mkDoc(
		graph.Entity{ID: "e1", Name: "A", Kind: "class"},
		graph.Entity{ID: "e2", Name: "B", Kind: "class", IsGodNode: true},
	)
	first := CollectCandidates(doc, DefaultEmitters(), nil)
	second := CollectCandidates(doc, DefaultEmitters(), nil)
	if len(first) != len(second) {
		t.Fatalf("len mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("idempotence violated at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
		if first[i].Kind != second[i].Kind || first[i].SubjectID != second[i].SubjectID {
			t.Fatalf("(kind, subject) mismatch at %d", i)
		}
	}
}

// Test 4: rejected (subject_id, kind) pairs are not re-emitted.
func TestEmit_SkipsRejected(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed rejections file.
	rej := []Rejection{{
		ID:        candidateID("e1", KindDescribeEntity),
		SubjectID: "e1",
		Kind:      KindDescribeEntity,
		Reason:    "irrelevant",
	}}
	data, _ := json.MarshalIndent(rej, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "enrichment-rejections.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	doc := mkDoc(
		graph.Entity{ID: "e1", Name: "Rejected", Kind: "class"},
		graph.Entity{ID: "e2", Name: "Allowed", Kind: "class"},
	)
	cands := CollectCandidatesSkippingRejected(doc, []CandidateEmitter{describeEntityEmitter{}}, dir)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate after rejection filter, got %d", len(cands))
	}
	if cands[0].SubjectID != "e2" {
		t.Fatalf("expected e2 to survive, got %q", cands[0].SubjectID)
	}
}

// Test 5: WriteCandidates and ReadResolutions / ApplyResolutions roundtrip.
func TestWriteCandidates_AndApplyResolutions(t *testing.T) {
	dir := t.TempDir()
	doc := mkDoc(graph.Entity{ID: "e1", Name: "A", Kind: "class"})
	cands := CollectCandidates(doc, DefaultEmitters(), nil)
	if err := WriteCandidates(dir, cands); err != nil {
		t.Fatalf("WriteCandidates: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "enrichment-candidates.json")); err != nil {
		t.Fatalf("candidates file not written: %v", err)
	}

	// Pre-seed a resolution and confirm ApplyResolutions writes it.
	resolutions := []Resolution{{
		ID:         "ec:any",
		SubjectID:  "e1",
		Kind:       "description",
		Value:      "An auth service.",
		Confidence: 0.9,
	}}
	doc2 := mkDoc(graph.Entity{ID: "e1", Name: "A", Kind: "class"})
	if got := ApplyResolutions(doc2, resolutions); got != 1 {
		t.Fatalf("ApplyResolutions = %d, want 1", got)
	}
	if doc2.Entities[0].Properties["description"] != "An auth service." {
		t.Fatalf("description not applied: %v", doc2.Entities[0].Properties)
	}
}

// Test 6 (issue #53): two consecutive WriteCandidates runs over the same
// input produce byte-identical output, even though emitters stamp
// DiscoveredAt with the wall clock between runs. This is the byte-stable
// idempotence guarantee the README and Pass 6 docstring promise.
func TestWriteCandidates_ByteIdenticalAcrossRuns(t *testing.T) {
	// Drive nowRFC3339 from a counter so each emit-pass produces a
	// different "current" timestamp. If WriteCandidates didn't preserve
	// the prior discovered_at, the second run's bytes would differ.
	origNow := nowRFC3339
	t.Cleanup(func() { nowRFC3339 = origNow })
	var ticks int
	nowRFC3339 = func() string {
		ticks++
		return time.Date(2026, 5, 9, 0, 0, ticks, 0, time.UTC).Format(time.RFC3339)
	}

	dir := t.TempDir()
	mkInput := func() *graph.Document {
		return mkDoc(
			graph.Entity{ID: "e1", Name: "AuthService", Kind: "class"},
			graph.Entity{ID: "g1", Name: "Coordinator", Kind: "class", IsGodNode: true},
			graph.Entity{ID: "a1", Name: "Bridge", Kind: "class", IsArticulationPt: true},
		)
	}

	// Run 1.
	first := CollectCandidates(mkInput(), DefaultEmitters(), nil)
	if err := WriteCandidates(dir, first); err != nil {
		t.Fatalf("first WriteCandidates: %v", err)
	}
	bytes1, err := os.ReadFile(filepath.Join(dir, "enrichment-candidates.json"))
	if err != nil {
		t.Fatalf("read after first run: %v", err)
	}

	// Run 2 — fresh emit pass, fresh "now" timestamps.
	second := CollectCandidates(mkInput(), DefaultEmitters(), nil)
	if err := WriteCandidates(dir, second); err != nil {
		t.Fatalf("second WriteCandidates: %v", err)
	}
	bytes2, err := os.ReadFile(filepath.Join(dir, "enrichment-candidates.json"))
	if err != nil {
		t.Fatalf("read after second run: %v", err)
	}

	if !bytes.Equal(bytes1, bytes2) {
		t.Fatalf("byte-stability violated:\n--- run 1 ---\n%s\n--- run 2 ---\n%s",
			string(bytes1), string(bytes2))
	}
}
