// Package sresolver implements the partial (scoped) resolver pass for the S3
// incremental file-level reindex (issue #2153 of epic #2149).
//
// This package is intentionally kept separate from internal/resolve to avoid
// an import cycle: internal/resolve's integration tests import internal/extractors,
// and internal/extractors/incremental.go imports this package.
//
// # Purpose
//
// After the per-file extraction step removes stale entities and adds freshly
// extracted ones, some relationships in the surviving graph may point TO
// entities that were just renamed, removed, or re-keyed. The scoped resolver
// re-examines:
//
//  1. Outbound relationships FROM newly extracted entities (already in newRels).
//  2. Inbound relationships in the EXISTING graph that point TO any of the
//     newly extracted entities' names — these need their ToID updated if the
//     entity's stable ID changed.
//
// If any inbound relationship points to a name that is from a re-extracted file
// but is NO LONGER present in newEntities (deleted entity), we set
// FallbackRequired = true so the caller falls back to a full reindex.
package sresolver

import (
	"log"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ScopedResult is the output of ResolveScoped.
type ScopedResult struct {
	// NewRelationships is the merged + re-resolved relationship slice to use
	// in the patched graph. Only valid when FallbackRequired is false.
	NewRelationships []graph.Relationship

	// FallbackRequired is true when the scoped resolver found a relationship
	// whose target cannot be resolved. The caller must fall back to full reindex.
	FallbackRequired bool

	// UnresolvedTarget is the first unresolved target name, for logging.
	UnresolvedTarget string

	// InboundFixed is the count of inbound relationships whose ToID was
	// updated to reflect the new entity ID.
	InboundFixed int
}

// ResolveScoped performs a partial resolver pass after incremental extraction.
//
// Parameters:
//   - newEntities: entities freshly extracted from the changed files.
//   - existingEntities: entities from the surviving (unchanged-file) portion of the graph.
//   - newRels: relationships extracted alongside newEntities (outbound from changed files).
//   - existingRels: relationships from the surviving graph (inbound + cross-file).
//   - logger: may be nil.
//
// The resolver builds a name → ID index over newEntities ∪ existingEntities
// and uses it to:
//
//  1. Rewrite stub ToIDs in newRels from bare names to entity IDs where possible.
//  2. Walk existingRels for inbound edges with stub ToIDs targeting newly-extracted
//     entity names: update their ToID when the name resolves to a new ID.
//  3. Detect the safety-net case: an existingRel stub ToID matches the source-file
//     set of re-extracted files but is NOT in newEntities (deleted entity/file).
func ResolveScoped(
	newEntities []graph.Entity,
	existingEntities []graph.Entity,
	newRels []graph.Relationship,
	existingRels []graph.Relationship,
	logger *log.Logger,
) ScopedResult {
	if logger == nil {
		logger = nopLogger()
	}

	// Build name → ID index: existing first, then new (new entities win on conflict).
	nameToID := make(map[string]string, len(newEntities)+len(existingEntities))
	for _, e := range existingEntities {
		if e.Name != "" {
			nameToID[e.Name] = e.ID
		}
		if e.QualifiedName != "" {
			nameToID[e.QualifiedName] = e.ID
		}
	}
	for _, e := range newEntities {
		if e.Name != "" {
			nameToID[e.Name] = e.ID
		}
		if e.QualifiedName != "" {
			nameToID[e.QualifiedName] = e.ID
		}
	}

	// Build source-file set for re-extracted files (for safety-net check).
	newFileSet := make(map[string]bool, len(newEntities))
	for _, e := range newEntities {
		newFileSet[e.SourceFile] = true
	}

	// Build name set for newly extracted entities (for inbound-fixup).
	newEntityByName := make(map[string]graph.Entity, len(newEntities))
	for _, e := range newEntities {
		if e.Name != "" {
			newEntityByName[e.Name] = e
		}
		if e.QualifiedName != "" {
			newEntityByName[e.QualifiedName] = e
		}
	}

	// Step 1: resolve stub ToIDs in newRels.
	resolvedNewRels := make([]graph.Relationship, 0, len(newRels))
	for _, r := range newRels {
		if !isHexID(r.ToID) {
			if resolved, ok := nameToID[r.ToID]; ok {
				r.ToID = resolved
				r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
			}
			// Unresolved stubs are kept as-is — same behaviour as the full resolver.
		}
		resolvedNewRels = append(resolvedNewRels, r)
	}

	// Step 2 & 3: walk existingRels for inbound edges with stub ToIDs.
	inboundFixed := 0
	var fallbackTarget string
	updatedExistingRels := make([]graph.Relationship, 0, len(existingRels))
	for _, r := range existingRels {
		if !isHexID(r.ToID) {
			if newE, ok := newEntityByName[r.ToID]; ok {
				// Update to the new entity's ID.
				r.ToID = newE.ID
				r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
				inboundFixed++
			} else if newFileSet[r.ToID] {
				// Safety-net: ToID is a source-file path from the re-extracted
				// file set, but the corresponding entity is absent from newEntities.
				// This means a file-entity (SCOPE.Component/file) was deleted.
				fallbackTarget = r.ToID
			}
		}
		updatedExistingRels = append(updatedExistingRels, r)
	}

	if fallbackTarget != "" {
		logger.Printf("sresolver: unresolved inbound rel target=%q → fallback to full reindex", fallbackTarget)
		return ScopedResult{
			FallbackRequired: true,
			UnresolvedTarget: fallbackTarget,
		}
	}

	merged := make([]graph.Relationship, 0, len(resolvedNewRels)+len(updatedExistingRels))
	merged = append(merged, updatedExistingRels...)
	merged = append(merged, resolvedNewRels...)

	logger.Printf("sresolver: inbound-fixed=%d new-rels=%d existing-rels=%d",
		inboundFixed, len(resolvedNewRels), len(updatedExistingRels))

	return ScopedResult{
		NewRelationships: merged,
		InboundFixed:     inboundFixed,
	}
}

// isHexID returns true when s looks like a 16-character lowercase hex string
// (the format produced by graph.EntityID and graph.RelationshipID).
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// nopLogger returns a logger that discards all output.
func nopLogger() *log.Logger {
	return log.New(nopWriter{}, "", 0)
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
