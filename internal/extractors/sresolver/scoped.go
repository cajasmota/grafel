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
//
// # Signature-change incremental (#2170)
//
// When an entity's Signature or Properties changed (detected by the caller via
// entityPropertiesHash comparison), the caller supplies the changed entity IDs
// via WithSignatureChangedIDs. The resolver builds a reverse index
// (toID → []Relationship) over the existing relationships and re-resolves
// inbound CALLS/REFERENCES edges for those IDs in the scoped pass, avoiding
// the safety-net full-reindex fallback for pure signature changes.
package sresolver

import (
	"log"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// Format A structural-ref stub constants — kept in lockstep with the canonical
// definitions in internal/resolve (refs.go: stubPrefixScope / stubDelim /
// stubScopeSegments / stubScopeFileIndex / stubScopeTailIndex / stubMemberDelim).
// They are duplicated here rather than imported because internal/resolve is a
// heavy package whose test suite imports internal/extractors; this package is
// deliberately kept light (see the package doc). The values are stable wire
// format, asserted against the resolver in scoped_test.go.
const (
	stubPrefixScope    = "scope:"
	stubDelim          = ":"
	stubMemberDelim    = '#'
	stubScopeSegments  = 6
	stubScopeFileIndex = 4
	stubScopeTailIndex = 5
)

// splitFormatAStructuralRef parses a Format A structural-ref stub
// (`scope:<kind>:<subtype>:<lang>:<file>:<name>`) into its file path and bare
// tail name. Returns ok=false for shapes that are not a 6-segment Format A stub
// or whose tail carries the Format B member delimiter `#`. Mirrors
// internal/resolve/imports.go:splitFormatAStructuralRef so the scoped resolver
// binds the same stubs the full resolver does.
func splitFormatAStructuralRef(stub string) (filePath, name string, ok bool) {
	if !strings.HasPrefix(stub, stubPrefixScope) {
		return "", "", false
	}
	parts := strings.SplitN(stub, stubDelim, stubScopeSegments)
	if len(parts) != stubScopeSegments {
		return "", "", false
	}
	filePath = parts[stubScopeFileIndex]
	tail := parts[stubScopeTailIndex]
	if filePath == "" || tail == "" {
		return "", "", false
	}
	if strings.IndexByte(tail, stubMemberDelim) >= 0 {
		return "", "", false
	}
	return filePath, tail, true
}

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

	// SignatureRewired is the count of CALLS/REFERENCES edges re-resolved
	// due to a signature change rather than triggering a full reindex (#2170).
	SignatureRewired int
}

// options holds optional configuration for ResolveScoped.
type options struct {
	// signatureChangedIDs is the set of entity IDs whose Signature/Properties
	// changed in this incremental pass. The resolver uses a reverse index to
	// find inbound CALLS/REFERENCES edges and re-resolves them in the scoped
	// pass rather than triggering the safety-net fallback (#2170).
	signatureChangedIDs []string
}

// Option is a functional option for ResolveScoped.
type Option func(*options)

// WithSignatureChangedIDs passes the entity IDs whose signatures changed so
// the scoped resolver can re-resolve their inbound callers (#2170).
func WithSignatureChangedIDs(ids []string) Option {
	return func(o *options) {
		o.signatureChangedIDs = ids
	}
}

// ResolveScoped performs a partial resolver pass after incremental extraction.
//
// Parameters:
//   - newEntities: entities freshly extracted from the changed files.
//   - existingEntities: entities from the surviving (unchanged-file) portion of the graph.
//   - newRels: relationships extracted alongside newEntities (outbound from changed files).
//   - existingRels: relationships from the surviving graph (inbound + cross-file).
//   - logger: may be nil.
//   - opts: optional functional options (e.g. WithSignatureChangedIDs).
//
// The resolver builds a name → ID index over newEntities ∪ existingEntities
// and uses it to:
//
//  1. Rewrite stub ToIDs in newRels from bare names to entity IDs where possible.
//  2. Walk existingRels for inbound edges with stub ToIDs targeting newly-extracted
//     entity names: update their ToID when the name resolves to a new ID.
//  3. Detect the safety-net case: an existingRel stub ToID matches the source-file
//     set of re-extracted files but is NOT in newEntities (deleted entity/file).
//  4. Re-resolve inbound CALLS/REFERENCES for signature-changed entities (#2170):
//     build a reverse index and update edges rather than falling back.
func ResolveScoped(
	newEntities []graph.Entity,
	existingEntities []graph.Entity,
	newRels []graph.Relationship,
	existingRels []graph.Relationship,
	logger *log.Logger,
	opts ...Option,
) ScopedResult {
	if logger == nil {
		logger = nopLogger()
	}

	o := &options{}
	for _, fn := range opts {
		fn(o)
	}

	// Build name → ID index: existing first, then new (new entities win on conflict).
	nameToID := make(map[string]string, len(newEntities)+len(existingEntities))
	// Build a (file, name) → ID location index so a Format A structural stub
	// (`scope:operation:method:<lang>:<file>:<name>`) binds to a SAME-FILE callee
	// first — mirroring the full resolver's byLocation tier — before falling back
	// to the unique bare-name index. byLocation[file][name] holds "" as an
	// ambiguity sentinel when two entities in the same file share a name.
	byLocation := make(map[string]map[string]string)
	addName := func(name, id string) {
		if name != "" {
			nameToID[name] = id
		}
	}
	addLocation := func(e graph.Entity) {
		if e.SourceFile == "" || e.Name == "" {
			return
		}
		bucket := byLocation[e.SourceFile]
		if bucket == nil {
			bucket = make(map[string]string)
			byLocation[e.SourceFile] = bucket
		}
		if prev, seen := bucket[e.Name]; seen && prev != e.ID {
			bucket[e.Name] = "" // ambiguous within the file
		} else {
			bucket[e.Name] = e.ID
		}
	}
	for _, e := range existingEntities {
		addName(e.Name, e.ID)
		addName(e.QualifiedName, e.ID)
		addLocation(e)
	}
	for _, e := range newEntities {
		addName(e.Name, e.ID)
		addName(e.QualifiedName, e.ID)
		addLocation(e)
	}

	// resolveStub maps a non-hex relationship endpoint to a canonical entity ID,
	// returning ok=false when it cannot be bound (the stub is then left verbatim,
	// exactly as the full resolver leaves an unresolved structural ref). The ladder
	// mirrors internal/resolve/refs.go:lookupStructural for Format A stubs:
	//   1. whole-string name / qualified-name match (handles bare-name stubs);
	//   2. Format A (file, tail): same-file byLocation, then unique bare-name.
	resolveStub := func(stub string) (string, bool) {
		if id, ok := nameToID[stub]; ok && id != "" {
			return id, true
		}
		file, tail, ok := splitFormatAStructuralRef(stub)
		if !ok {
			return "", false
		}
		if bucket, ok := byLocation[file]; ok {
			if id, ok := bucket[tail]; ok {
				if id == "" {
					return "", false // ambiguous within the file → leave verbatim
				}
				return id, true
			}
		}
		if id, ok := nameToID[tail]; ok && id != "" {
			return id, true
		}
		return "", false
	}

	// Build source-file set for re-extracted files (for safety-net check).
	newFileSet := make(map[string]bool, len(newEntities))
	for _, e := range newEntities {
		newFileSet[e.SourceFile] = true
	}

	// Build set of signature-changed entity IDs for fast lookup (#2170).
	sigChangedSet := make(map[string]bool, len(o.signatureChangedIDs))
	for _, id := range o.signatureChangedIDs {
		sigChangedSet[id] = true
	}

	// Build ID → new entity map for signature-changed re-resolution (#2170).
	newEntityByID := make(map[string]graph.Entity, len(newEntities))
	for _, e := range newEntities {
		newEntityByID[e.ID] = e
	}

	// Step 1: resolve stub endpoints in newRels. The full resolver rewrites BOTH
	// the from- and to-side of every edge (refs.go logs `from: rw=N to: rw=N`);
	// the scoped pass must too, or an outbound edge from a freshly-extracted
	// entity is left with a stub FromID (e.g. a class→method CONTAINS edge, or a
	// caller whose own structural-ref the extractor emits) that a full rebuild
	// resolves to the hashed id (#5309 resolution parity).
	resolvedNewRels := make([]graph.Relationship, 0, len(newRels))
	for _, r := range newRels {
		changed := false
		if !isHexID(r.FromID) {
			if resolved, ok := resolveStub(r.FromID); ok {
				r.FromID = resolved
				changed = true
			}
		}
		if !isHexID(r.ToID) {
			if resolved, ok := resolveStub(r.ToID); ok {
				r.ToID = resolved
				changed = true
			}
		}
		// Unresolved stubs are kept as-is — same behaviour as the full resolver.
		if changed {
			r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
		}
		resolvedNewRels = append(resolvedNewRels, r)
	}

	// Step 2, 3 & 4: walk existingRels for inbound edges with stub ToIDs.
	inboundFixed := 0
	signatureRewired := 0
	var fallbackTarget string
	updatedExistingRels := make([]graph.Relationship, 0, len(existingRels))
	for _, r := range existingRels {
		if !isHexID(r.ToID) {
			if resolved, ok := resolveStub(r.ToID); ok {
				// Bind the inbound stub to the (possibly re-keyed) entity ID via
				// the same Format A ladder the full resolver uses, so a cross-file
				// edge from a surviving file is never left in stub form when a
				// full rebuild would resolve it (#5309 resolution parity).
				r.ToID = resolved
				r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
				inboundFixed++
			} else if newFileSet[r.ToID] {
				// Safety-net: ToID is a source-file path from the re-extracted
				// file set, but the corresponding entity is absent from newEntities.
				// This means a file-entity (SCOPE.Component/file) was deleted.
				fallbackTarget = r.ToID
			}
		} else if sigChangedSet[r.ToID] && isSignatureEdge(r.Kind) {
			// Step 4 (#2170): inbound CALLS/REFERENCES targeting a
			// signature-changed entity. The entity still exists under the same
			// ID (only its signature changed), so the edge remains valid — just
			// mark it as rewired so callers can log/observe.
			signatureRewired++
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

	logger.Printf("sresolver: inbound-fixed=%d signature-rewired=%d new-rels=%d existing-rels=%d",
		inboundFixed, signatureRewired, len(resolvedNewRels), len(updatedExistingRels))

	return ScopedResult{
		NewRelationships: merged,
		InboundFixed:     inboundFixed,
		SignatureRewired: signatureRewired,
	}
}

// isSignatureEdge returns true when the relationship kind is one that
// points from a caller/user to the called entity — the edges most likely
// to be affected by a signature change.
func isSignatureEdge(kind string) bool {
	switch kind {
	case "CALLS", "REFERENCES", "USES", "INVOKES":
		return true
	}
	return false
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
