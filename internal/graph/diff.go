// Package graph — diff.go computes structural diffs between two graph Documents.
//
// DiffDocs is the entry-point: given two *Documents (graphA and graphB), it
// returns a DiffResult containing which entities were added/removed/modified
// and which relationships were added/removed. The algorithm uses canonical
// entity IDs as the primary key — the same entity appearing in both graphs
// (same ID) is considered "same entity". Key-field changes (name, source_file
// hash, start_line, end_line) trigger a "modified" classification.
//
// This is a pure-Go, zero-dependency function. It never mutates the inputs.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ---------------------------------------------------------------------------
// Wire types (used by both the diff engine and the HTTP handler)
// ---------------------------------------------------------------------------

// DiffEntityEntry is a slim record describing an entity in the diff output.
type DiffEntityEntry struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	SourceFile     string   `json:"source_file"`
	ModifiedFields []string `json:"modified_fields,omitempty"`
}

// DiffRelEntry is a slim record describing a relationship in the diff output.
type DiffRelEntry struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Kind   string `json:"kind"`
}

// DiffSummary aggregates the change counts.
type DiffSummary struct {
	EntitiesAdded        int `json:"entities_added"`
	EntitiesRemoved      int `json:"entities_removed"`
	EntitiesModified     int `json:"entities_modified"`
	RelationshipsAdded   int `json:"relationships_added"`
	RelationshipsRemoved int `json:"relationships_removed"`
	FilesChanged         int `json:"files_changed"`
}

// DiffResult is the complete output of DiffDocs.
type DiffResult struct {
	Group string `json:"group,omitempty"`
	Repo  string `json:"repo,omitempty"`
	RefA  string `json:"ref_a,omitempty"`
	RefB  string `json:"ref_b,omitempty"`

	Summary DiffSummary `json:"summary"`

	Entities struct {
		Added    []DiffEntityEntry `json:"added"`
		Removed  []DiffEntityEntry `json:"removed"`
		Modified []DiffEntityEntry `json:"modified"`
	} `json:"entities"`

	Relationships struct {
		Added   []DiffRelEntry `json:"added"`
		Removed []DiffRelEntry `json:"removed"`
	} `json:"relationships"`
}

// ---------------------------------------------------------------------------
// DiffDocs — core algorithm
// ---------------------------------------------------------------------------

// DiffDocs computes the diff between docA (the "before" ref) and docB (the
// "after" ref). It returns an annotated DiffResult ready for JSON encoding.
//
// Algorithm:
//  1. Build an ID-keyed map for entities in each document.
//  2. Walk docA.Entities — anything not in docB is "removed"; anything in
//     docB with changed key fields is "modified".
//  3. Walk docB.Entities — anything not in docA is "added".
//  4. Build (fromID, toID, kind) sets for relationships; set-diff gives
//     added and removed relationships.
//  5. Derive files_changed from the union of changed source files.
func DiffDocs(docA, docB *Document) DiffResult {
	var r DiffResult

	// ── Entity diff ──────────────────────────────────────────────────────────
	mapA := make(map[string]Entity, len(docA.Entities))
	for _, e := range docA.Entities {
		mapA[e.ID] = e
	}
	mapB := make(map[string]Entity, len(docB.Entities))
	for _, e := range docB.Entities {
		mapB[e.ID] = e
	}

	changedFiles := map[string]struct{}{}

	// Pass 1: removed + modified.
	for id, ea := range mapA {
		eb, inB := mapB[id]
		if !inB {
			r.Entities.Removed = append(r.Entities.Removed, DiffEntityEntry{
				ID:         id,
				Kind:       ea.Kind,
				Name:       ea.Name,
				SourceFile: ea.SourceFile,
			})
			if ea.SourceFile != "" {
				changedFiles[ea.SourceFile] = struct{}{}
			}
			continue
		}
		// Both refs have this entity — check for modifications.
		modified, fields := entityModified(ea, eb)
		if modified {
			r.Entities.Modified = append(r.Entities.Modified, DiffEntityEntry{
				ID:             id,
				Kind:           eb.Kind,
				Name:           eb.Name,
				SourceFile:     eb.SourceFile,
				ModifiedFields: fields,
			})
			if ea.SourceFile != "" {
				changedFiles[ea.SourceFile] = struct{}{}
			}
			if eb.SourceFile != "" {
				changedFiles[eb.SourceFile] = struct{}{}
			}
		}
	}

	// Pass 2: added.
	for id, eb := range mapB {
		if _, inA := mapA[id]; !inA {
			r.Entities.Added = append(r.Entities.Added, DiffEntityEntry{
				ID:         id,
				Kind:       eb.Kind,
				Name:       eb.Name,
				SourceFile: eb.SourceFile,
			})
			if eb.SourceFile != "" {
				changedFiles[eb.SourceFile] = struct{}{}
			}
		}
	}

	// Ensure non-nil slices for clean JSON output.
	if r.Entities.Added == nil {
		r.Entities.Added = []DiffEntityEntry{}
	}
	if r.Entities.Removed == nil {
		r.Entities.Removed = []DiffEntityEntry{}
	}
	if r.Entities.Modified == nil {
		r.Entities.Modified = []DiffEntityEntry{}
	}

	// ── Relationship diff ────────────────────────────────────────────────────

	relKeyA := make(map[string]DiffRelEntry, len(docA.Relationships))
	for _, rel := range docA.Relationships {
		k := relKey(rel.FromID, rel.ToID, rel.Kind)
		relKeyA[k] = DiffRelEntry{FromID: rel.FromID, ToID: rel.ToID, Kind: rel.Kind}
	}
	relKeyB := make(map[string]DiffRelEntry, len(docB.Relationships))
	for _, rel := range docB.Relationships {
		k := relKey(rel.FromID, rel.ToID, rel.Kind)
		relKeyB[k] = DiffRelEntry{FromID: rel.FromID, ToID: rel.ToID, Kind: rel.Kind}
	}

	for k, rel := range relKeyA {
		if _, ok := relKeyB[k]; !ok {
			r.Relationships.Removed = append(r.Relationships.Removed, rel)
		}
	}
	for k, rel := range relKeyB {
		if _, ok := relKeyA[k]; !ok {
			r.Relationships.Added = append(r.Relationships.Added, rel)
		}
	}

	if r.Relationships.Added == nil {
		r.Relationships.Added = []DiffRelEntry{}
	}
	if r.Relationships.Removed == nil {
		r.Relationships.Removed = []DiffRelEntry{}
	}

	// ── Summary ──────────────────────────────────────────────────────────────
	r.Summary = DiffSummary{
		EntitiesAdded:        len(r.Entities.Added),
		EntitiesRemoved:      len(r.Entities.Removed),
		EntitiesModified:     len(r.Entities.Modified),
		RelationshipsAdded:   len(r.Relationships.Added),
		RelationshipsRemoved: len(r.Relationships.Removed),
		FilesChanged:         len(changedFiles),
	}

	return r
}

// entityModified returns true when key fields differ between ea and eb,
// along with the list of changed field names.
//
// Key fields compared:
//   - name (human-readable identifier)
//   - source_file (location changed)
//   - source_window_hash: SHA-256 of the source window (start/end lines + sig)
//   - kind (promotion/demotion)
func entityModified(ea, eb Entity) (bool, []string) {
	var changed []string

	if ea.Name != eb.Name {
		changed = append(changed, "name")
	}
	if ea.SourceFile != eb.SourceFile {
		changed = append(changed, "source_file")
	}
	if ea.Kind != eb.Kind {
		changed = append(changed, "kind")
	}
	// source_window_hash: compare a fingerprint of the source window.
	hashA := sourceWindowHash(ea)
	hashB := sourceWindowHash(eb)
	if hashA != hashB {
		changed = append(changed, "source_window")
	}

	return len(changed) > 0, changed
}

// sourceWindowHash returns a short hex hash of the fields that define the
// entity's source location and signature. Changing any of these triggers a
// "modified" classification even when the entity ID remains the same.
func sourceWindowHash(e Entity) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s", e.SourceFile, e.StartLine, e.EndLine, e.Signature)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// relKey builds a collision-resistant string key for a (fromID, toID, kind)
// triple. Used as a map key for the set-diff.
func relKey(fromID, toID, kind string) string {
	return fromID + "\x00" + toID + "\x00" + kind
}
