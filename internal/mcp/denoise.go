// denoise.go — MCP serving-layer result de-noising and re-ranking (#1614).
//
// The graph carries many synthetic / structural entities that are useful for
// traversal but pure noise in a ranked find/search result: file-and-module
// CONTAINER components, inferred class-hierarchy shadows, raw SCOPE.Pattern nodes
// (e.g. error_handling:try_catch:N), Schema field members, and Process nodes for
// array built-ins. Worse, because these often share the BM25 score of a real
// match (label substring), they frequently rank ABOVE the real lined entity the
// agent actually wants.
//
// This file classifies entities into noise buckets and provides a stable
// re-rank comparator so that real, lined, qualified entities sort first. It is
// purely a serving-layer concern — no extraction state is touched (other grinds
// own internal/extractors and internal/engine).
//
// Noise tiers (ascending = worse rank):
//
//	noiseNone (0)       — real entity; ranks by BM25 and start_line presence
//	noiseShadow (4)     — inferred class-hierarchy / implicit-method shadow
//	noiseContainer (5)  — file/module CONTAINER Component
//	noiseProcess (6)    — array/string built-in Process node
//	noiseSchemaField (7) — SCOPE.Schema subtype=field member (#1715)
//	noisePattern (8)    — SCOPE.Pattern structural node (#1733)
//	noiseLocalScope (9) — non-addressable function-body local binding (#1748)
package mcp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
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
	// noiseSchemaField: a SCOPE.Schema entity with Subtype="field" — a
	// single field/attribute belonging to a serializer or schema class (e.g.
	// DeficiencyCreateSerializer.amount). These are member entities of their
	// parent class and clutter default results when ~25 fields accompany every
	// serializer. Suppressed by default (#1712); the parent class surfaces
	// normally. Reachable via include_noise:true.
	noiseSchemaField
	// noiseLocalScope: a non-addressable local binding emitted inside a
	// function/method body (#1748). Examples: `const { counts } = someData`
	// or `const [a, b] = arr` inside a React component. These are kept in
	// the graph so the resolver can bind REFERENCES/CALLS edges, but they
	// are never independently inspectable via grafel_inspect (the name
	// is not addressable as "Component.counts") so surfacing them in
	// grafel_find wastes tokens and violates "everything you see is
	// queryable". Identified by Properties["local_scope"]=="true".
	noiseLocalScope
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
	// Match both the canonical "SCOPE.Pattern" kind and any bare "pattern"
	// variant that extractors may emit without the scope prefix.
	if e.Kind == string(types.EntityKindPattern) || bareKind == "pattern" {
		return noisePattern
	}

	// File/module container Component: label == source-file path, no body.
	//
	// #2015: also check the top-level Subtype field. The extractor stamps the
	// subtype into BOTH the EntityRecord.Subtype field and Properties["subtype"]
	// (extractor.FileEntity), but some load / conversion paths only repopulate
	// one of them. Checking both makes the classification robust regardless of
	// which side carried the value through to the loaded graph.Entity. We also
	// relax the StartLine==0 gate: the python extractor's #1964 finalize sweep
	// stamps StartLine=1 onto previously-zero entities — including file
	// containers — so a real file Component can arrive here with StartLine>0
	// yet still be the synthetic file node whose only role is anchoring
	// file-level IMPORTS/REFERENCES edges.
	subtype := e.Subtype
	if subtype == "" {
		subtype = e.PropGet("subtype")
	}
	if bareKind == "component" && (subtype == "file" || subtype == "module") {
		return noiseContainer
	}
	if bareKind == "component" && e.StartLine == 0 {
		if e.PropGet("subtype") == "file" || e.PropGet("subtype") == "module" {
			return noiseContainer
		}
		// Fallback: label literally equals the source file path.
		if e.SourceFile != "" && e.Name == e.SourceFile {
			return noiseContainer
		}
	}

	// Inferred class-hierarchy / implicit-method shadows.
	if prov := e.PropGet("provenance"); prov == "INFERRED_FROM_CLASS_HIERARCHY" {
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

	// Schema field members (#1712): a SCOPE.Schema entity whose Subtype is
	// "field" is a single attribute of a parent serializer/schema class
	// (e.g. DeficiencyCreateSerializer.amount). ~25 of these accompany every
	// serializer class and pollute default ranked results. The parent class
	// entity (same Kind, no "field" Subtype) surfaces normally.
	if bareKind == "schema" && e.Subtype == "field" {
		return noiseSchemaField
	}

	// Non-addressable function-body locals (#1748): emitted at extraction
	// time for resolver use but not independently inspectable. The extractor
	// stamps Properties["local_scope"]="true" on these entities.
	if e.PropGet("local_scope") == "true" {
		return noiseLocalScope
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
//
// Tier map (ascending = worse rank):
//
//	0 — real lined entity (start_line > 0)
//	1 — lineless but legitimate (endpoint/resource)
//	4 — noiseShadow (inferred class-hierarchy / implicit-method shadow)
//	5 — noiseContainer (file/module CONTAINER Component)
//	6 — noiseProcess (array/string built-in Process node)
//	7 — noiseSchemaField (SCOPE.Schema subtype=field member, #1712)
//	8 — noisePattern (SCOPE.Pattern structural node, #1733)
func rankTier(e *graph.Entity) int {
	switch classifyNoise(e) {
	case noiseContainer:
		return 5
	case noiseShadow:
		return 4
	case noiseProcess:
		return 6
	case noiseSchemaField:
		return 7
	case noisePattern:
		// SCOPE.Pattern nodes (e.g. error_handling:try_catch:N) rank below all
		// other noise tiers — they are structural enrichment signals, never
		// direct answers to a user search query (#1733).
		return 8
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
