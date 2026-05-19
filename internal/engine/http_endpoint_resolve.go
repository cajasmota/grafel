// Phase-2 post-pass for the synthetic http_endpoint entities emitted by
// http_endpoint_synthesis.go.
//
// Phase 1 emits one synthetic http_endpoint per route with a
// `source_handler` property of the form "<HandlerKind>:<HandlerName>"
// but deliberately does NOT emit edges to the handler. Emitting unresolved
// edges in Phase 1 inflated bug-rate because every dangling target counts
// as a resolver failure.
//
// Phase 2 (this file) runs AFTER the merged entity table is assembled but
// BEFORE EntityIDs are stamped. It:
//
//  1. Builds a (kind, name, sourceFile) → record-pointer index over the
//     merged set.
//  2. For each synthetic http_endpoint with a source_handler property:
//     a. Parses the property into (handlerKind, handlerName).
//     b. Resolves to a real entity in the same SourceFile (handlers and
//        their owning routes always live in the same file by construction
//        of Phase 1).
//     c. If resolved: appends an IMPLEMENTS edge (handler → synthetic)
//        to the handler's embedded Relationships, then clears the
//        source_handler property (its job is done).
//     d. If NOT resolved: marks the synthetic for removal so it never
//        reaches the resolver as an orphan.
//
// Returning a NEW slice of EntityRecords (with unresolved synthetics
// dropped) keeps the data flow obvious and avoids in-place slice
// shuffling at the call site.
//
// Refs #534 Phase 2.
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ResolveHTTPEndpointStats reports counters for a single resolve pass.
// Exposed so cmd/archigraph can log a stats line analogous to the
// import-aware resolver line.
type ResolveHTTPEndpointStats struct {
	Synthetics       int // total http_endpoint records seen
	HandlerResolved  int // source_handler resolved → IMPLEMENTS edge emitted
	HandlerDropped   int // synthetics dropped because source_handler unresolved
	NoHandlerProp    int // synthetics with no source_handler property (kept as-is)
}

// ResolveHTTPEndpointHandlers runs the Phase-2 post-pass over `merged`.
// Returns a (possibly shorter) slice with unresolved synthetics removed,
// plus stats for verbose logging.
//
// `merged` MUST already be sorted in canonical order (entity-id
// disambiguation depends on first-writer-wins). The slice may be
// returned as-is if no synthetics were dropped.
func ResolveHTTPEndpointHandlers(merged []types.EntityRecord) ([]types.EntityRecord, ResolveHTTPEndpointStats) {
	var stats ResolveHTTPEndpointStats

	// (kind, name, sourceFile) → index into `merged`.
	type key struct{ kind, name, sourceFile string }
	idx := make(map[key]int, len(merged))
	for i := range merged {
		r := &merged[i]
		if r.Kind == httpEndpointKind {
			continue // never resolve a synthetic against another synthetic
		}
		k := key{r.Kind, r.Name, r.SourceFile}
		if _, ok := idx[k]; !ok {
			idx[k] = i
		}
	}

	// Collect indices of synthetics to drop (unresolved handlers).
	drop := map[int]bool{}

	for i := range merged {
		r := &merged[i]
		if r.Kind != httpEndpointKind {
			continue
		}
		stats.Synthetics++

		handlerRef := ""
		if r.Properties != nil {
			handlerRef = r.Properties["source_handler"]
		}
		if handlerRef == "" {
			stats.NoHandlerProp++
			continue
		}

		// source_handler is "<HandlerKind>:<HandlerName>" — split on the
		// FIRST colon only because Spring-style names can themselves
		// contain a colon-less path identifier but kinds never do.
		hk, hn, ok := splitHandlerRef(handlerRef)
		if !ok {
			// Malformed — drop the synthetic to avoid leaking the bad
			// reference into the graph.
			drop[i] = true
			stats.HandlerDropped++
			continue
		}

		// Prefer same-file match (handlers and route synthetics are
		// always emitted from the same file by Phase 1 construction).
		handlerIdx, found := idx[key{hk, hn, r.SourceFile}]
		if !found {
			// Spring's composed Route handler reference uses
			// Kind="Route" + Name=<path>, which collides with the
			// synthetic itself — the synthetic IS the canonicalised path.
			// In that case Phase 1 only had the path to refer to, not the
			// underlying controller method, so we treat it as a true
			// unresolved (no real handler entity exists with that kind+name
			// distinct from the synthetic).
			drop[i] = true
			stats.HandlerDropped++
			continue
		}

		// Resolved. Append an embedded IMPLEMENTS edge on the handler.
		// Use placeholder ID stubs (Kind:Name) for the endpoints; the
		// resolver in buildDocument rewrites these against the stamped
		// entity index after we return.
		handler := &merged[handlerIdx]
		fromStub := handler.Kind + ":" + handler.Name
		toStub := r.Kind + ":" + r.Name
		handler.Relationships = append(handler.Relationships, types.RelationshipRecord{
			FromID: fromStub,
			ToID:   toStub,
			Kind:   implementsEdgeKind,
			Properties: map[string]string{
				"pattern_type": "http_endpoint_synthesis_resolved",
				"framework":    propOr(r, "framework", ""),
			},
		})
		// Clear the now-redundant property.
		delete(r.Properties, "source_handler")
		stats.HandlerResolved++
	}

	if len(drop) == 0 {
		return merged, stats
	}
	out := make([]types.EntityRecord, 0, len(merged)-len(drop))
	for i := range merged {
		if drop[i] {
			continue
		}
		out = append(out, merged[i])
	}
	return out, stats
}

// splitHandlerRef parses "<Kind>:<Name>" into its parts. Returns ok=false
// when the input lacks a colon or has an empty kind/name.
func splitHandlerRef(ref string) (kind, name string, ok bool) {
	i := strings.Index(ref, ":")
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// propOr returns r.Properties[k] or fallback if missing/nil.
func propOr(r *types.EntityRecord, k, fallback string) string {
	if r.Properties == nil {
		return fallback
	}
	if v, ok := r.Properties[k]; ok && v != "" {
		return v
	}
	return fallback
}
