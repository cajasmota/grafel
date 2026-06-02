// Package engine — dependency-boundary annotation pass (#3638, epic #3625).
//
// This pass promotes the previously-orphaned lib_boundary enricher
// (internal/enrichers/lib_boundary_enricher.go — a Go port of the Python
// lib_boundary_enricher.py that was imported by zero production code) into a
// live, registered project-scope pass. It annotates every dependency /import
// DEPENDS_ON edge with a boundary signal:
//
//   - boundary=first_party  — the target is internal to this org/module: a
//     local/relative import, or a code-to-code dependency between two entities
//     that both live in this repo (neither end is an external manifest dep).
//   - boundary=third_party  — the target is an external library: a manifest
//     dependency (DEPENDS_ON kind=external_dependency) or a non-local import
//     resolved to an external package.
//
// WHY. The first_party / third_party split is a rewrite-scope signal: code
// inside a rewrite boundary is first_party (you own it, you can change it),
// whereas third_party edges cross an org boundary into a vendored library (you
// can swap it but not rewrite it). Persisting it onto the graph lets
// find/neighbors/agents reason about rewrite scope without re-deriving it.
//
// REUSE, DO NOT RE-PARSE. The classification is read entirely off properties the
// extractors already attached to each edge:
//
//   - cross/manifest emits DEPENDS_ON{kind=external_dependency} for declared
//     manifest deps                                            → third_party.
//   - cross/imports emits DEPENDS_ON{kind=import, is_local, external_dependency}
//     where is_local distinguishes a relative/internal import (first_party)
//     from a resolved external package (third_party).
//   - language extractors emit DEPENDS_ON code-to-code edges (struct field,
//     receiver, …). When NEITHER endpoint is an external_dependency entity the
//     dependency is internal → first_party.
//
// No source is re-read and no manifest is re-parsed; this pass runs after the
// document is assembled and mutates existing edges (and external_dependency
// entities, for parity) in place — no new entities, no double emit.
//
// HONEST-PARTIAL. When an edge's origin is genuinely ambiguous (a DEPENDS_ON
// whose kind/locality cannot be determined and whose endpoints are not both
// resolvable in this document), the pass leaves it UNANNOTATED rather than
// guessing. The boundary property is therefore present only where the signal is
// real; its absence reads as "unknown", not "first_party".
package engine

import "github.com/cajasmota/archigraph/internal/graph"

// boundaryProp is the edge/entity property key carrying the dependency-boundary
// classification. Kept in one place so queries and tests agree on the name.
const boundaryProp = "boundary"

// Boundary classification values.
const (
	boundaryFirstParty = "first_party"
	boundaryThirdParty = "third_party"
)

// LibBoundaryStats summarises one ApplyLibBoundary run.
type LibBoundaryStats struct {
	// EdgesConsidered is the number of DEPENDS_ON edges inspected.
	EdgesConsidered int
	// FirstParty is the count of edges annotated boundary=first_party.
	FirstParty int
	// ThirdParty is the count of edges annotated boundary=third_party.
	ThirdParty int
	// Ambiguous is the count of DEPENDS_ON edges left unannotated because their
	// origin could not be determined (honest-partial — see package doc).
	Ambiguous int
	// EntitiesAnnotated is the number of external_dependency entities stamped
	// boundary=third_party for parity with their edges.
	EntitiesAnnotated int
}

// ApplyLibBoundary classifies every DEPENDS_ON edge in doc as first_party or
// third_party from the locality/kind properties the extractors already attached,
// writing boundary onto the edges (and onto external_dependency entities) in
// place. It returns stats describing the run. Deterministic and idempotent: a
// second call recomputes identical values. Safe to call on a nil/empty doc.
func ApplyLibBoundary(doc *graph.Document) LibBoundaryStats {
	stats := LibBoundaryStats{}
	if doc == nil {
		return stats
	}

	// Index entity IDs that are external_dependency carriers so a code-to-code
	// DEPENDS_ON edge whose target is one of them can be classed third_party,
	// and so we know which endpoints are "owned" (present, non-external) when
	// resolving the internal-vs-unknown case.
	externalDepIDs := make(map[string]bool)
	ownedIDs := make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		ownedIDs[e.ID] = true
		if e.Kind == "SCOPE.Component" && e.Subtype == "external_dependency" {
			externalDepIDs[e.ID] = true
		}
	}

	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "DEPENDS_ON" {
			continue
		}
		stats.EdgesConsidered++

		boundary := classifyBoundary(r, externalDepIDs, ownedIDs)
		switch boundary {
		case boundaryFirstParty:
			if r.Properties == nil {
				r.Properties = make(map[string]string)
			}
			r.Properties[boundaryProp] = boundaryFirstParty
			stats.FirstParty++
		case boundaryThirdParty:
			if r.Properties == nil {
				r.Properties = make(map[string]string)
			}
			r.Properties[boundaryProp] = boundaryThirdParty
			stats.ThirdParty++
		default:
			// Ambiguous — leave unannotated (honest-partial).
			stats.Ambiguous++
		}
	}

	// Parity: stamp the external_dependency entities themselves third_party so
	// entity-oriented queries surface the boundary without an edge lookup. By
	// definition a manifest external dependency is third_party.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "external_dependency" {
			continue
		}
		if e.Properties == nil {
			e.Properties = make(map[string]string)
		}
		e.Properties[boundaryProp] = boundaryThirdParty
		stats.EntitiesAnnotated++
	}

	return stats
}

// classifyBoundary returns boundaryFirstParty, boundaryThirdParty, or "" (the
// ambiguous sentinel) for a single DEPENDS_ON edge, reading only properties the
// extractors already attached plus the resolved-endpoint sets.
func classifyBoundary(r *graph.Relationship, externalDepIDs, ownedIDs map[string]bool) string {
	props := r.Properties

	// 1. Manifest dependency edge — DEPENDS_ON{kind=external_dependency}. A
	//    declared third-party library by construction.
	if props != nil && props["kind"] == "external_dependency" {
		return boundaryThirdParty
	}
	// A code-to-code edge whose target is a manifest external_dependency entity
	// is likewise crossing into a third-party library.
	if externalDepIDs[r.ToID] {
		return boundaryThirdParty
	}

	// 2. Import edge — DEPENDS_ON{kind=import}. The cross/imports extractor
	//    already decided locality; reuse it verbatim.
	if props != nil && props["kind"] == "import" {
		// is_local=true → relative/internal import → first_party.
		if props["is_local"] == "true" {
			return boundaryFirstParty
		}
		// external_dependency mirrors !is_local on the import edge's carrier.
		if v, ok := props["external_dependency"]; ok {
			if v == "false" {
				return boundaryFirstParty
			}
			return boundaryThirdParty
		}
		if props["is_local"] == "false" {
			return boundaryThirdParty
		}
		// kind=import but no locality signal at all — ambiguous.
		return ""
	}

	// 3. Code-to-code DEPENDS_ON (struct field, receiver, injected bean, …).
	//    When BOTH endpoints resolve to entities owned by this document — and
	//    neither is an external dependency (handled above) — the dependency is
	//    internal to the repo → first_party.
	if ownedIDs[r.FromID] && ownedIDs[r.ToID] {
		return boundaryFirstParty
	}

	// 4. Otherwise the origin is ambiguous (unresolved target, no kind/locality
	//    signal). Honest-partial: do not guess.
	return ""
}
