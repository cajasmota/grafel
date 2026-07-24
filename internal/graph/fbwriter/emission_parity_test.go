package fbwriter

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// parityDoc returns a document deliberately NOT in canonical emission order:
// entities are ID-descending. Before #5974 the segmented writer re-sorted and
// the flat writer did not, so the two producers emitted different bytes for
// the same input — and the flat file's entity vector violated the
// sorted-by-key invariant EntitiesByKey binary-searches on.
func parityDoc(n int) *graph.Document {
	// A fixed GeneratedAt is required for a byte-wise comparison: a zero
	// timestamp makes the writer stamp time.Now(), which differs between the
	// two writes whenever they straddle a second boundary.
	doc := &graph.Document{
		Repo:        "parity",
		GeneratedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := n - 1; i >= 0; i-- {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         fmt.Sprintf("ent-%03d", i),
			Name:       fmt.Sprintf("Sym%03d", i),
			Kind:       "function",
			SourceFile: fmt.Sprintf("src/f%03d.go", i),
		})
	}
	for i := 0; i+1 < n; i++ {
		from := fmt.Sprintf("ent-%03d", i+1)
		to := fmt.Sprintf("ent-%03d", i)
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(from, to, "CALLS"),
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		})
	}
	return doc
}

// TestFlatAndSegmentedWritersAgree asserts the flat gen writer and the
// segmented writer's single-file fast path emit byte-identical output for the
// same (unsorted) document (#5974).
func TestFlatAndSegmentedWritersAgree(t *testing.T) {
	flatPath, err := WriteGraphGen(t.TempDir(), parityDoc(48))
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	segPath, err := WriteGraphGenSegmented(t.TempDir(), parityDoc(48))
	if err != nil {
		t.Fatalf("WriteGraphGenSegmented: %v", err)
	}
	flatBytes, err := os.ReadFile(flatPath)
	if err != nil {
		t.Fatal(err)
	}
	segBytes, err := os.ReadFile(segPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(flatBytes, segBytes) {
		t.Fatalf("flat and segmented writers disagree: %d vs %d bytes", len(flatBytes), len(segBytes))
	}
}

// TestWriteGraphGenCanonicalisesEntityOrder asserts the flat producer leaves
// the document in the ID order the reader's binary search requires, even when
// the caller hands it an unsorted slice.
func TestWriteGraphGenCanonicalisesEntityOrder(t *testing.T) {
	doc := parityDoc(48)
	if _, err := WriteGraphGen(t.TempDir(), doc); err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	for i := 1; i < len(doc.Entities); i++ {
		if doc.Entities[i-1].ID > doc.Entities[i].ID {
			t.Fatalf("entities not ID-sorted at %d: %q then %q",
				i, doc.Entities[i-1].ID, doc.Entities[i].ID)
		}
	}
}
