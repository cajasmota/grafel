// Package resolve — bazel_overlay.go
//
// BazelOverlay is the resolver pass that cross-references BAZEL_DEPENDS_ON
// edges (declared build-level deps) against CALLS edges (inferred runtime
// deps) and annotates both sets with reconciliation status.
//
// Issue #2183 — Monorepo M6: Bazel BUILD-graph fusion.
//
// # Three reconciliation categories
//
//   - "declared+used"   — a BAZEL_DEPENDS_ON edge exists AND at least one
//     CALLS / IMPORTS edge crosses the same two Bazel targets.
//     Build dep is confirmed by runtime usage.
//
//   - "declared_unused" — a BAZEL_DEPENDS_ON edge exists but NO call-graph
//     edge crosses those two targets. Candidate for dep-pruning.
//
//   - "undeclared_used" — a CALLS / IMPORTS edge crosses two Bazel targets
//     but NO corresponding BAZEL_DEPENDS_ON edge exists. Missing dep warning:
//     the build may succeed by transitive luck but is fragile.
//
// # How target identity is mapped
//
// Each Bazel target entity carries Properties["label"] = "//pkg:name".
// CALLS edges use grafel entity IDs (16-char hex). The overlay builds
// a bipartite index:
//
//	entityID → bazel label  (from target entities)
//	bazel label → entityID  (reverse)
//
// then for every CALLS / IMPORTS edge it checks whether both endpoints live
// in known Bazel targets and looks up the BAZEL_DEPENDS_ON edge.
//
// The overlay is additive: it appends new annotated edges and never modifies
// or deletes existing ones.
package resolve

import (
	"github.com/cajasmota/grafel/internal/extractors/bazel"
	"github.com/cajasmota/grafel/internal/types"
)

// BazelOverlayResult holds the output of the overlay pass.
type BazelOverlayResult struct {
	// AnnotatedRels are new relationship records produced by the overlay.
	// They supplement (do not replace) the original edges.
	AnnotatedRels []types.RelationshipRecord

	// Stats summarises the reconciliation.
	Stats BazelOverlayStats
}

// BazelOverlayStats holds counters for the three reconciliation categories.
type BazelOverlayStats struct {
	DeclaredUsed   int // BAZEL_DEPENDS_ON confirmed by CALLS/IMPORTS
	DeclaredUnused int // BAZEL_DEPENDS_ON with no matching call edge
	UndeclaredUsed int // CALLS/IMPORTS crossing Bazel targets without BAZEL_DEPENDS_ON
}

// callEdgeKinds is the set of relationship kinds that count as "used" for the
// purposes of the overlay. We include IMPORTS because import edges are the
// primary signal in many language extractors (Go, Python, Java, etc.) even
// before CALLS edges are resolved.
var callEdgeKinds = map[string]bool{
	string(types.RelationshipKindCalls):   true,
	string(types.RelationshipKindImports): true,
}

// RunBazelOverlay performs the reconciliation pass over the merged entity +
// relationship set produced by the indexer.
//
// entities:  the full merged entity slice (all languages + config + bazel).
// rels:      the full merged relationship slice (all passes).
//
// Returns a BazelOverlayResult with new annotated relationship records and
// reconciliation stats. The caller is responsible for appending
// result.AnnotatedRels to the relationship set before graph serialisation.
//
// The function never mutates the input slices.
func RunBazelOverlay(entities []types.EntityRecord, rels []types.RelationshipRecord) BazelOverlayResult {
	// Build index: entity ID → Bazel label (only for bazel_target entities).
	idToLabel := map[string]string{}
	labelToID := map[string]string{}
	for i := range entities {
		e := &entities[i]
		if e.Subtype != "bazel_target" {
			continue
		}
		lbl := e.Properties["label"]
		if lbl == "" {
			continue
		}
		idToLabel[e.ID] = lbl
		labelToID[lbl] = e.ID
	}

	if len(idToLabel) == 0 {
		// No Bazel targets in graph — nothing to do.
		return BazelOverlayResult{}
	}

	// Build index of declared BAZEL_DEPENDS_ON edges.
	// Key: "fromLabel\x00toLabel"
	type edgePair struct{ from, to string }
	declaredPairs := map[edgePair]string{} // pair → dep_label property

	for i := range rels {
		r := &rels[i]
		if r.Kind != bazel.RelationshipKindBazelDependsOn {
			continue
		}
		fromLabel, ok1 := idToLabel[r.FromID]
		toLabel, ok2 := idToLabel[r.ToID]
		if !ok1 || !ok2 {
			continue
		}
		declaredPairs[edgePair{fromLabel, toLabel}] = r.Properties.Get("dep_label")
	}

	// Build index of call/import edges that cross Bazel target boundaries.
	// Key: edgePair of *labels* (not IDs) for the two Bazel targets.
	usedPairs := map[edgePair]bool{}

	// We also need a "which Bazel target does this entity belong to?" mapping.
	// Heuristic: an entity's SourceFile maps to the Bazel package that owns
	// the BUILD file in that directory (or nearest ancestor). We build this
	// from the target entities' Package property.
	//
	// For simplicity in v1 we map entity ID → label only when the entity IS
	// a bazel_target. For language entities (functions, classes, etc.) we
	// look up the Bazel target that covers their source file.
	//
	// Build: sourceDir → label for packages we've seen.
	dirToLabel := map[string]string{}
	for i := range entities {
		e := &entities[i]
		if e.Subtype != "bazel_target" {
			continue
		}
		pkg := e.Properties["bazel_package"]
		lbl := e.Properties["label"]
		if pkg != "" && lbl != "" {
			dirToLabel[pkg] = lbl
		}
	}

	// ownerLabel returns the Bazel target label that "owns" an entity,
	// by walking the entity's source-file directory up to the nearest
	// Bazel package boundary.
	entityToLabel := make(map[string]string, len(entities))
	for i := range entities {
		e := &entities[i]
		if l, ok := idToLabel[e.ID]; ok {
			// Already a bazel_target entity.
			entityToLabel[e.ID] = l
			continue
		}
		if e.SourceFile == "" {
			continue
		}
		lbl := findOwnerLabel(e.SourceFile, dirToLabel)
		if lbl != "" {
			entityToLabel[e.ID] = lbl
		}
	}

	for i := range rels {
		r := &rels[i]
		if !callEdgeKinds[r.Kind] {
			continue
		}
		fromLabel, ok1 := entityToLabel[r.FromID]
		toLabel, ok2 := entityToLabel[r.ToID]
		if !ok1 || !ok2 || fromLabel == toLabel {
			// Same package or unknown — skip intra-package edges.
			continue
		}
		usedPairs[edgePair{fromLabel, toLabel}] = true
	}

	// Reconcile.
	var annotated []types.RelationshipRecord
	var stats BazelOverlayStats

	// Declared edges: check if they are also used.
	for pair, depLabel := range declaredPairs {
		fromID := labelToID[pair.from]
		toID := labelToID[pair.to]
		used := usedPairs[pair]

		status := "declared_unused"
		if used {
			status = "declared+used"
			stats.DeclaredUsed++
		} else {
			stats.DeclaredUnused++
		}

		annotated = append(annotated, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   "BAZEL_DEP_STATUS",
			Properties: types.Props{
				{K: "dep_label", V: depLabel},
				{K: "from", V: pair.from},
				{K: "status", V: status},
				{K: "to", V: pair.to},
			},
		})
	}

	// Used edges: check if they are also declared.
	for pair := range usedPairs {
		if _, declared := declaredPairs[pair]; declared {
			// Already handled above.
			continue
		}
		fromID := labelToID[pair.from]
		toID := labelToID[pair.to]
		if fromID == "" || toID == "" {
			continue
		}

		stats.UndeclaredUsed++
		annotated = append(annotated, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   "BAZEL_DEP_STATUS",
			Properties: types.Props{
				{K: "from", V: pair.from},
				{K: "status", V: "undeclared_used"},
				{K: "to", V: pair.to},
			},
		})
	}

	return BazelOverlayResult{
		AnnotatedRels: annotated,
		Stats:         stats,
	}
}

// findOwnerLabel walks sourceFile's directory path up until it finds a
// matching Bazel package in dirToLabel. Returns "" if none found.
// Example: "services/auth/handler.go" matches "services/auth" if that
// is a known Bazel package.
func findOwnerLabel(sourceFile string, dirToLabel map[string]string) string {
	// Normalise to forward slashes.
	sf := toSlash(sourceFile)
	// Walk from the file's directory toward the root.
	dir := slashDir(sf)
	for {
		if lbl, ok := dirToLabel[dir]; ok {
			return lbl
		}
		parent := slashDir(dir)
		if parent == dir || dir == "" || dir == "." {
			break
		}
		dir = parent
	}
	// Check root package.
	if lbl, ok := dirToLabel[""]; ok {
		return lbl
	}
	return ""
}

// toSlash replaces backslashes with forward slashes (Windows paths).
func toSlash(p string) string {
	result := make([]byte, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' {
			result[i] = '/'
		} else {
			result[i] = p[i]
		}
	}
	return string(result)
}

// slashDir returns the parent directory component of a slash-separated path.
func slashDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}
