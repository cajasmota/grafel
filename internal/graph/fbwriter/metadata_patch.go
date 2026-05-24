// metadata_patch.go provides PatchMetadata, an in-place metadata update
// for graph.fb files.
//
// # Design rationale
//
// FlatBuffers is a non-mutating serialisation format: string offsets are
// absolute byte positions into the buffer, so in-place modification of a
// string field (e.g. indexed_ref) would require the new value to be
// identical in byte length. Rather than implement fragile length-matching
// logic, we use a streaming read-modify-write approach:
//
//  1. Decode the existing graph.fb into a *graph.Document via
//     graph.LoadGraphFromDir (O(N) decode, single mmap bulk read).
//  2. Overwrite the three metadata fields on the decoded struct.
//  3. Re-encode to a fresh FlatBuffers buffer and write atomically to the
//     same path via WriteAtomic.
//
// The read-modify-write is O(N) in entity count, but on a 6k-entity repo
// the full round-trip costs < 20 ms — negligible relative to the 5 s
// full-reindex it replaces. No in-place byte patching is performed.
//
// This helper is exported so clone.TryClone and any future callers that
// need to update only the metadata fields (e.g. an explicit `archigraph
// retag` command) can do so without reimplementing the decode/re-encode
// cycle.
package fbwriter

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// MetadataPatch carries the fields to overwrite. Zero-value fields are
// left unchanged (the original value from the graph.fb is preserved).
type MetadataPatch struct {
	// IndexedRef is the new git ref name (branch/tag). Empty = no change.
	IndexedRef string
	// IndexedSHA is the new abbreviated commit hash. Empty = no change.
	IndexedSHA string
	// IndexedAt, if non-zero, overwrites the GeneratedAt timestamp.
	IndexedAt time.Time
}

// PatchMetadata loads the graph.fb in stateDir, applies p, and writes
// the updated document back to stateDir/graph.fb atomically.
//
// The read-modify-write is O(N) in entity count. All other fields
// (entities, relationships, communities, algorithm stats, repo tag) are
// preserved verbatim.
//
// Returns an error if:
//   - the graph.fb cannot be decoded (corrupt file),
//   - the re-encode or atomic write fails.
func PatchMetadata(stateDir string, p MetadataPatch) error {
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return fmt.Errorf("fbwriter.PatchMetadata: load %s: %w", stateDir, err)
	}

	if p.IndexedRef != "" {
		doc.IndexedRef = p.IndexedRef
	}
	if p.IndexedSHA != "" {
		doc.IndexedSHA = p.IndexedSHA
	}
	if !p.IndexedAt.IsZero() {
		doc.GeneratedAt = p.IndexedAt.UTC()
	}

	outPath := filepath.Join(stateDir, "graph.fb")
	if err := WriteAtomic(outPath, doc); err != nil {
		return fmt.Errorf("fbwriter.PatchMetadata: write %s: %w", outPath, err)
	}
	return nil
}
