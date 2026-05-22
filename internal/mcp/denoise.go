// denoise.go — MCP serving-layer result de-noising and re-ranking (#1614).
//
// The graph carries many synthetic / structural entities that are useful for
// traversal but pure noise in a ranked find/search result: file-and-module
// CONTAINER components, inferred class-hierarchy shadows, raw Pattern nodes, and
// Process nodes for array built-ins. Worse, because these often share the BM25
// score of a real match (label substring), they frequently rank ABOVE the real
// lined entity the agent actually wants.
//
// This file classifies entities into noise buckets and provides a stable
// re-rank comparator so that real, lined, qualified entities sort first. It is
// purely a serving-layer concern — no extraction state is touched (other grinds
// own internal/extractors and internal/engine).
package mcp

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// noiseKind enumerates the de-noise buckets. noiseNone means the entity is a
// real, surfacable result.
type noiseKind int

const (
	noiseNone noiseKind = iota
	// noiseContainer: a file/module CONTAINER Component — its label is the
	// source-file path and it has no body (start_line==0). Useful for
	// traversal, never a useful ranked hit.
	noiseContainer
	// noiseShadow: an inferred class-hierarchy / implicit-method shadow. Either
	// carries provenance==INFERRED_FROM_CLASS_HIERARCHY, or has an empty
	// qualified_name AND start_line==0 (e.g. drf_viewset_implicit_method
	// LoginViewSet.list / .retrieve / .update — bodies that don't exist in
	// source).
	noiseShadow
	// noisePattern: a raw structural Pattern node (SCOPE.Pattern), e.g.
	// error_handling:try_catch:N. Not the agent-learned AgentPattern kind.
	noisePattern
	// noiseProcess: a Process node for an array/string built-in call, e.g.
	// "Login → map", "Foo → trim". Identified by the proc: ID prefix and/or
	// the "X → builtin" label shape.
	noiseProcess
)

// arrayBuiltins is the set of array/string built-in method names whose Process
// nodes (e.g. "Login → map") are pure noise in a ranked result.
var arrayBuiltins = map[string]bool{
	"map": true, "filter": true, "reduce": true, "forEach": true,
	"some": true, "every": true, "find": true, "findIndex": true,
	"includes": true, "indexOf": true, "join": true, "split": true,
	"trim": true, "slice": true, "splice": true, "concat": true,
	"push": true, "pop": true, "shift": true, "unshift": true,
	"sort": true, "reverse": true, "flat": true, "flatMap": true,
	"keys": true, "values": true, "entries": true, "toLowerCase": true,
	"toUpperCase": true, "replace": true, "padStart": true, "padEnd": true,
}

// classifyNoise returns the noise bucket for an entity. noiseNone means the
// entity is a real, surfacable result.
func classifyNoise(e *graph.Entity) noiseKind {
	if e == nil {
		return noiseNone
	}
	bareKind := strings.ToLower(stripScopePrefix(e.Kind))

	// Process nodes for built-ins: the local ID carries a "proc:" segment and
	// the label has the "X → builtin" shape.
	if bareKind == "process" || strings.Contains(e.ID, "proc:") {
		if _, builtin := splitProcessBuiltin(e.Name); builtin {
			return noiseProcess
		}
	}

	// Raw structural Pattern nodes (NOT the agent-learned AgentPattern kind,
	// which has no SCOPE. prefix and is surfaced deliberately).
	if e.Kind == string(types.EntityKindPattern) {
		return noisePattern
	}

	// File/module container Component: label == source-file path, no body.
	if bareKind == "component" && e.StartLine == 0 {
		if e.Properties["subtype"] == "file" || e.Properties["subtype"] == "module" {
			return noiseContainer
		}
		// Fallback: label literally equals the source file path.
		if e.SourceFile != "" && e.Name == e.SourceFile {
			return noiseContainer
		}
	}

	// Inferred class-hierarchy / implicit-method shadows.
	if prov := e.Properties["provenance"]; prov == "INFERRED_FROM_CLASS_HIERARCHY" {
		return noiseShadow
	}
	if e.StartLine == 0 && e.QualifiedName == "" {
		// Bodiless, unqualified entity. Endpoint kinds legitimately have
		// start_line==0 (they are route declarations, not source bodies) — keep
		// those. Same for external/datastore/queue/event kinds that model
		// non-source resources.
		if !isStructuralLineless(bareKind) {
			return noiseShadow
		}
	}

	return noiseNone
}

// isStructuralLineless reports whether a bare (scope-stripped, lowercased) kind
// is one that legitimately has start_line==0 and is NOT a shadow — i.e. it
// models a route/resource rather than a source body.
func isStructuralLineless(bareKind string) bool {
	switch bareKind {
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call",
		"endpoint", "route", "externalapi", "datastore", "dataaccess",
		"queue", "event", "infraresource", "messagetopic", "service",
		"externalpackage", "external", "config":
		return true
	}
	return false
}

// splitProcessBuiltin parses a Process label of the form "Caller → builtin" and
// reports whether the right-hand side is a known array/string built-in.
func splitProcessBuiltin(label string) (string, bool) {
	// Labels use a unicode right-arrow; tolerate "->" as well.
	for _, sep := range []string{" → ", "→", " -> ", "->"} {
		if i := strings.Index(label, sep); i >= 0 {
			rhs := strings.TrimSpace(label[i+len(sep):])
			return rhs, arrayBuiltins[rhs]
		}
	}
	return "", false
}

// isNoise reports whether the entity is in any noise bucket.
func isNoise(e *graph.Entity) bool { return classifyNoise(e) != noiseNone }

// rankTier returns a coarse ranking tier for an entity; LOWER is better. Real
// lined+qualified entities are tier 0, real lined entities tier 1, structural
// lineless (endpoints/resources) tier 2, and every noise bucket tier 3+. The
// caller combines tier with BM25 score so that within a tier the BM25 order is
// preserved, but a real entity always outranks a shadow/container/pattern.
func rankTier(e *graph.Entity) int {
	switch classifyNoise(e) {
	case noiseContainer:
		return 5
	case noiseShadow:
		return 4
	case noisePattern:
		return 6
	case noiseProcess:
		return 6
	}
	// Real entity. Lined entities (whether or not they carry a qualified_name)
	// share the top tier so that BM25 relevance — not the mere presence of a
	// qualified_name — orders them. Lineless-but-legitimate entities (routes /
	// resources, e.g. endpoint definitions) sit just below.
	if e.StartLine > 0 {
		return 0
	}
	return 1 // lineless but legitimate (endpoint/resource)
}
