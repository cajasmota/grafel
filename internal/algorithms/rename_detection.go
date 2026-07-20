// Package algorithms provides graph-level analysis passes that operate on a
// fully-built graph.Document rather than on raw entity/relationship records.
//
// rename_detection.go — post-rebuild entity rename detection (#1344).
//
// After each index pass a new graph.Document is ready but the old one still
// lives on disk. This pass compares the two snapshots:
//
//   - Entities present in OLD but absent in NEW are treated as candidates for
//     deletion (they may have been renamed).
//   - Entities present in NEW but absent in OLD are treated as candidates for
//     addition (they may be the result of a rename).
//
// For every "added" entity we search the "deleted" set for a match using
// three independent signals:
//
//  1. Same kind (function / method / class / …).
//  2. Similar name: Levenshtein distance < 30 % of max(len(old), len(new)).
//  3. Preserved neighborhood: at least one caller or callee is shared between
//     old and new, OR the files are identical (intra-file rename).
//
// When all three signals agree the new entity receives a RENAMED_FROM edge
// pointing at the old entity's ID.  The edge carries a "confidence" property
// (0.0–1.0) that reflects how strongly the signals agree, a "old_name"
// property with the previous name, and a "method" property describing which
// heuristics fired.
//
// Move detection (file changed, signature identical) is handled as a
// special-case short-circuit before the fuzzy matching: if kind+name match
// exactly but the source file changed, a RENAMED_FROM edge is emitted with
// method="moved" and confidence=1.0.
//
// Split detection (one old entity → two new entities sharing callers) is
// attempted when a deleted entity has more than one new-candidate hit.  If
// two new entities each satisfy the rename heuristic against the same old
// entity, both receive RENAMED_FROM edges with method="split".
//
// The pass is append-only: it never removes or modifies existing entities or
// edges.  It is safe to skip (--skip-pass=rename-detect) without affecting any
// other pass.
package algorithms

import (
	"fmt"
	"math"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// RelKindRenamedFrom is the edge kind emitted by the rename-detection pass.
// The edge runs from the NEW entity (the post-rename entity) → the OLD entity
// ID.  Consumers that care about history can follow the edge backwards to
// recover old enrichment, findings, and metrics.
const RelKindRenamedFrom = "RENAMED_FROM"

// RenameStats summarises the rename-detection pass output.
type RenameStats struct {
	// Candidates is the number of (deleted, added) entity pairs examined.
	Candidates int
	// Renames is the number of RENAMED_FROM edges emitted (rename + move).
	Renames int
	// Moves is the subset of Renames where only the source file changed.
	Moves int
	// Splits is the number of split events (one old entity → two+ new ones).
	Splits int
}

// DetectRenames compares prevDoc (the last persisted graph) and newDoc (the
// freshly-built graph) and appends RENAMED_FROM edges to newDoc.Relationships.
// prevDoc may be nil (first-ever index, or the caller chose to skip loading
// it); in that case the function is a no-op.
//
// The function is idempotent: running it twice on the same pair produces the
// same set of edges (because both docs are immutable from the caller's
// perspective; only newDoc.Relationships grows).
func DetectRenames(prevDoc, newDoc *graph.Document) RenameStats {
	if prevDoc == nil || newDoc == nil {
		return RenameStats{}
	}

	// Build ID sets for fast membership tests.
	prevIDs := make(map[string]struct{}, len(prevDoc.Entities))
	for _, e := range prevDoc.Entities {
		prevIDs[e.ID] = struct{}{}
	}
	newIDs := make(map[string]struct{}, len(newDoc.Entities))
	for _, e := range newDoc.Entities {
		newIDs[e.ID] = struct{}{}
	}

	// Collect the two candidate sets.
	var deleted []graph.Entity // in prev, absent from new
	var added []graph.Entity   // in new, absent from prev

	for _, e := range prevDoc.Entities {
		if _, ok := newIDs[e.ID]; !ok {
			deleted = append(deleted, e)
		}
	}
	for _, e := range newDoc.Entities {
		if _, ok := prevIDs[e.ID]; !ok {
			added = append(added, e)
		}
	}

	if len(deleted) == 0 || len(added) == 0 {
		return RenameStats{}
	}

	// Build neighborhood indices (callers + callees) keyed by entity ID.
	prevNeighbors := buildNeighborIndex(prevDoc.Relationships)
	newNeighbors := buildNeighborIndex(newDoc.Relationships)

	// Existing RENAMED_FROM edges in newDoc — guard against duplicates.
	existingRenames := make(map[string]struct{})
	for _, r := range newDoc.Relationships {
		if r.Kind == RelKindRenamedFrom {
			existingRenames[r.FromID+"\x00"+r.ToID] = struct{}{}
		}
	}

	// For each deleted entity, track which added entities matched it so we
	// can detect splits (one old → multiple new).
	type renameEdge struct {
		fromID     string // new entity
		toID       string // old entity ID
		oldName    string
		confidence float64
		method     string
	}

	// deletedMatchCounts[deletedID] = []renameEdge — accumulate all matches
	// for a single deleted entity so splits can be flagged.
	matchesByDeleted := make(map[string][]renameEdge, len(deleted))

	var stats RenameStats
	stats.Candidates = len(deleted) * len(added)

	// Phase 1 — exact name+kind, file changed (move detection).
	// Build lookup: (kind, name) → deleted entity for O(1) move probe.
	deletedByKindName := make(map[string]graph.Entity, len(deleted))
	for _, d := range deleted {
		key := d.Kind + "\x00" + d.Name
		deletedByKindName[key] = d
	}

	remainingAdded := make([]graph.Entity, 0, len(added))
	for _, a := range added {
		key := a.Kind + "\x00" + a.Name
		d, ok := deletedByKindName[key]
		if !ok {
			remainingAdded = append(remainingAdded, a)
			continue
		}
		if d.SourceFile == a.SourceFile {
			// Same name, same file, same kind → IDs should be identical (would
			// have matched above). Skip; this is a graph schema change, not a
			// rename.
			remainingAdded = append(remainingAdded, a)
			continue
		}
		// File changed but kind+name identical → MOVE.
		dedupKey := a.ID + "\x00" + d.ID
		if _, dup := existingRenames[dedupKey]; dup {
			continue
		}
		edge := renameEdge{
			fromID:     a.ID,
			toID:       d.ID,
			oldName:    d.Name,
			confidence: 1.0,
			method:     "moved",
		}
		matchesByDeleted[d.ID] = append(matchesByDeleted[d.ID], edge)
	}

	// Phase 2 — fuzzy rename matching for remaining added entities.
	for _, a := range remainingAdded {
		bestEdge := renameEdge{}
		bestScore := -1.0

		for _, d := range deleted {
			if d.Kind != a.Kind {
				continue
			}
			// Signal 1: name similarity.
			// Threshold: reject pairs where more than 35 % of the longer name
			// needs to change (sim < 0.65). This accepts common rename patterns
			// like getUserByID→getUserByName (sim≈0.69) while rejecting
			// completely unrelated names like foo→bar (sim=0.0).
			nameSim := nameSimilarity(d.Name, a.Name)
			if nameSim < 0.65 {
				continue
			}

			// Signal 2: neighborhood preservation.
			nbSim := neighborhoodSimilarity(d.ID, a.ID, prevNeighbors, newNeighbors)

			// Signal 3: same file (intra-file rename).
			sameFile := 0.0
			if d.SourceFile == a.SourceFile {
				sameFile = 1.0
			}

			// Require at least one of (neighborhood OR same file) to match, in
			// addition to name similarity.  Without this guard, two completely
			// unrelated functions that happen to have similar names (e.g. init /
			// init2) in different files and different callers would be linked.
			if nbSim < 0.1 && sameFile == 0.0 {
				continue
			}

			// Composite confidence: weighted average of the three signals.
			// Weights: name=0.5, neighborhood=0.35, file=0.15.
			confidence := nameSim*0.50 + nbSim*0.35 + sameFile*0.15

			if confidence > bestScore {
				method := buildMethodTag(nameSim, nbSim, sameFile > 0)
				bestScore = confidence
				bestEdge = renameEdge{
					fromID:     a.ID,
					toID:       d.ID,
					oldName:    d.Name,
					confidence: math.Round(confidence*100) / 100,
					method:     method,
				}
			}
		}

		if bestScore < 0 {
			continue
		}
		dedupKey := bestEdge.fromID + "\x00" + bestEdge.toID
		if _, dup := existingRenames[dedupKey]; dup {
			continue
		}
		matchesByDeleted[bestEdge.toID] = append(matchesByDeleted[bestEdge.toID], bestEdge)
	}

	// Phase 3 — emit edges. Tag splits where one deleted entity maps to 2+
	// new ones.
	for deletedID, edges := range matchesByDeleted {
		isSplit := len(edges) > 1
		if isSplit {
			stats.Splits++
		}
		for _, edge := range edges {
			method := edge.method
			if isSplit && method != "moved" {
				method = "split"
			}

			props := map[string]string{
				"confidence": fmt.Sprintf("%.2f", edge.confidence),
				"old_name":   edge.oldName,
				"method":     method,
				"old_id":     deletedID,
			}
			rel := graph.Relationship{
				ID:     graph.RelationshipID(edge.fromID, edge.toID, RelKindRenamedFrom),
				FromID: edge.fromID,
				ToID:   edge.toID,
				Kind:   RelKindRenamedFrom,
			}.WithProperties(props)
			newDoc.Relationships = append(newDoc.Relationships, rel)
			existingRenames[edge.fromID+"\x00"+edge.toID] = struct{}{}

			stats.Renames++
			if method == "moved" {
				stats.Moves++
			}
		}
	}

	return stats
}

// ─── helpers ────────────────────────────────────────────────────────────────

// neighborIndex maps an entity ID to the set of IDs it is connected to (both
// callers and callees across all edge kinds).
type neighborIndex map[string]map[string]struct{}

// buildNeighborIndex scans rels and builds a bidirectional adjacency set so
// neighborhoodSimilarity can find shared neighbors in O(1) per pair.
func buildNeighborIndex(rels []graph.Relationship) neighborIndex {
	idx := make(neighborIndex, len(rels)/2+1)
	for _, r := range rels {
		if idx[r.FromID] == nil {
			idx[r.FromID] = make(map[string]struct{})
		}
		if idx[r.ToID] == nil {
			idx[r.ToID] = make(map[string]struct{})
		}
		idx[r.FromID][r.ToID] = struct{}{}
		idx[r.ToID][r.FromID] = struct{}{}
	}
	return idx
}

// neighborhoodSimilarity returns the Jaccard similarity between the neighbor
// sets of oldID (in prevNeighbors) and newID (in newNeighbors).
// Returns 0 when both sets are empty (no structural signal).
func neighborhoodSimilarity(oldID, newID string, prev, next neighborIndex) float64 {
	oldNb := prev[oldID]
	newNb := next[newID]

	if len(oldNb) == 0 && len(newNb) == 0 {
		return 0
	}

	// Count intersection (ignoring the specific IDs which differ after rename —
	// we match by name within the neighbor sets instead of by ID).
	// Since IDs are stable-hash based on (repo, kind, name, file), the callers'
	// IDs will be the same across both snapshots as long as THEY were not renamed.
	intersection := 0
	for id := range oldNb {
		if _, ok := newNb[id]; ok {
			intersection++
		}
	}

	union := len(oldNb) + len(newNb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// nameSimilarity returns a 0–1 score: 1 = identical, 0 = completely different.
// Based on normalised Levenshtein: 1 - dist/max(len(a),len(b)).
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	// Case-insensitive comparison — rename may change casing.
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	if al == bl {
		return 0.97 // slight penalty for casing change
	}
	maxLen := len(al)
	if len(bl) > maxLen {
		maxLen = len(bl)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(al, bl)
	sim := 1.0 - float64(dist)/float64(maxLen)
	return sim
}

// levenshtein computes the edit distance between two lowercase strings.
// Uses the standard two-row DP; O(mn) time, O(min(m,n)) space.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// Ensure a is the shorter string to minimise allocations.
	if len(a) > len(b) {
		a, b = b, a
	}
	prev := make([]int, len(a)+1)
	curr := make([]int, len(a)+1)
	for i := range prev {
		prev[i] = i
	}
	for j := 1; j <= len(b); j++ {
		curr[0] = j
		for i := 1; i <= len(a); i++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[i] + 1
			ins := curr[i-1] + 1
			sub := prev[i-1] + cost
			if del < ins {
				curr[i] = del
			} else {
				curr[i] = ins
			}
			if sub < curr[i] {
				curr[i] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(a)]
}

// buildMethodTag returns a human-readable description of which signals fired.
func buildMethodTag(nameSim, nbSim float64, sameFile bool) string {
	var parts []string
	if nameSim >= 0.97 {
		parts = append(parts, "name_exact")
	} else {
		parts = append(parts, "name_fuzzy")
	}
	if nbSim >= 0.1 {
		parts = append(parts, "neighborhood")
	}
	if sameFile {
		parts = append(parts, "same_file")
	}
	return strings.Join(parts, "+")
}
