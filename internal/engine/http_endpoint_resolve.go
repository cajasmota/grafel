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
//  2. For each synthetic http_endpoint with a source_handler property
//     (producer side):
//     a. Parses the property into (handlerKind, handlerName).
//     b. Resolves to a real entity in the same SourceFile (handlers and
//     their owning routes always live in the same file by construction
//     of Phase 1).
//     c. If resolved: appends an IMPLEMENTS edge (handler → synthetic)
//     to the handler's embedded Relationships, then clears the
//     source_handler property (its job is done).
//     d. If NOT resolved: marks the synthetic for removal so it never
//     reaches the resolver as an orphan.
//  3. For each synthetic http_endpoint with a source_caller property
//     (consumer side, #754):
//     a. Parses the property into (callerKind, callerName).
//     b. Resolves to a real entity in the same SourceFile (the JS/TS
//     and Python consumer extractors stamp the enclosing function's
//     NAME on each emitted endpoint, and that function lives in the
//     same file by construction).
//     c. If resolved: appends a FETCHES edge (caller → synthetic) to
//     the caller's embedded Relationships, then clears the
//     source_caller property. This wires the consumer-side
//     http_endpoint into the per-repo graph so the process-flow BFS
//     (#724) can traverse from the caller into the bridge node, and
//     the cross-stack detector (#754) can fire correctly when the
//     chain crosses a repo boundary. Without this edge, the 41
//     consumer endpoints on fixture-e remained structural orphans
//     and no fixture-e Process was ever marked cross_stack=true.
//     d. If NOT resolved: the synthetic is kept (consumer endpoints
//     are valuable cross-repo bridges regardless of caller resolution)
//     but no FETCHES edge is emitted.
//
// Returning a NEW slice of EntityRecords (with unresolved producer
// synthetics dropped) keeps the data flow obvious and avoids in-place
// slice shuffling at the call site.
//
// Refs #534 Phase 2, #754.
package engine

import (
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// formatLine renders a 1-based line number for property serialisation.
func formatLine(line int) string { return strconv.Itoa(line) }

// resolverKindEquivalents maps a synthesizer-emitted handler Kind to
// the list of fallback Kinds the resolver should try when the exact
// match misses. The synthesizers were written against an older
// extractor convention (Controller / View) but the per-language
// extractors land function-shaped handlers as SCOPE.Operation and
// class-shaped handlers as SCOPE.Class. Without this fallback the
// resolver drops every Flask / FastAPI / Express endpoint whose
// handler is a plain function. #753.
var resolverKindEquivalents = map[string][]string{
	"Controller":      {"SCOPE.Operation", "SCOPE.Function", "View"},
	"View":            {"SCOPE.Operation", "SCOPE.Class", "Controller"},
	"SCOPE.Operation": {"Controller", "View"},
	"SCOPE.Class":     {"View", "Controller"},
	"SCOPE.Function":  {"SCOPE.Operation", "Controller"},
}

// ResolveHTTPEndpointStats reports counters for a single resolve pass.
// Exposed so cmd/archigraph can log a stats line analogous to the
// import-aware resolver line.
type ResolveHTTPEndpointStats struct {
	Synthetics       int // total http_endpoint* records seen
	HandlerResolved  int // source_handler resolved → IMPLEMENTS edge emitted
	HandlerDropped   int // synthetics dropped because source_handler unresolved
	NoHandlerProp    int // synthetics with no source_handler property (kept as-is)
	CallerResolved   int // #754: source_caller resolved → FETCHES edge emitted
	CallerUnresolved int // #754: source_caller present but not found in-file
	// #1217 migration counters.
	DefinitionsMigrated int // entities that were http_endpoint (legacy) → definition
	CallsMigrated       int // entities that were http_endpoint (legacy) → call
	// #1217 cross-link counters.
	CallsLinked     int // call → definition FETCHES edges emitted
	CallsUnresolved int // call entities with no matching definition (UNRESOLVED_FETCH)
	// #1615 — caller→call-synthetic FETCHES edges retargeted at the resolved
	// definition so the call-site is no longer an orphan.
	CallerEdgesRetargeted int
	// #1999 — DTO → handler REFERENCES edges emitted from request_body_type /
	// response_body_type properties on http_endpoint_definition entities. The
	// handler → DTO direction is implicit via the property; this counts the
	// new explicit DTO → handler direction that makes the DTO node
	// self-documenting ("which handlers consume this DTO?").
	DTOHandlerEdgesEmitted int
	// #1999 — request_body_type / response_body_type values that did not
	// resolve to a known SCOPE.Component / Model in the merged set. Counted
	// separately from emitted so an external DTO (third-party SDK) doesn't
	// look like a bug in the new pass.
	DTOHandlerEdgesUnresolved int
}

// ResolveHTTPEndpointHandlers runs the Phase-2 post-pass over `merged`.
// Returns a (possibly shorter) slice with unresolved synthetics removed,
// plus stats for verbose logging.
//
// `merged` MUST already be sorted in canonical order (entity-id
// disambiguation depends on first-writer-wins). The slice may be
// returned as-is if no synthetics were dropped.
// httpResolveKey is the (kind, name, sourceFile) index key used by the
// resolver to look up entities by their stable in-file identity.
type httpResolveKey struct{ kind, name, sourceFile string }

func ResolveHTTPEndpointHandlers(merged []types.EntityRecord) ([]types.EntityRecord, ResolveHTTPEndpointStats) {
	var stats ResolveHTTPEndpointStats

	// (kind, name, sourceFile) → index into `merged`.
	type key = httpResolveKey
	idx := make(map[key]int, len(merged))
	// (kind, name) → first index — used as cross-file fallback for
	// handlers declared in a different module than the route synthetic
	// (Django composed routes, Express imported controllers, etc.).
	// See #753: the original same-file-only resolver dropped every
	// Django-composed and imported-controller endpoint because the
	// view/controller body lives in a different file than the URL
	// dispatcher. Falling back to a global (kind, name) match keeps
	// those endpoints alive so the corpus-wide response-shape pass
	// can locate and scan the actual handler body.
	type knKey struct{ kind, name string }
	globalIdx := make(map[knKey]int, len(merged))
	// #2692 — multi-match index for the Phoenix-style `name@file_hint`
	// resolution path. Routes-file frameworks (Phoenix router) declare
	// handlers by short action name (`index`, `show`) that collides
	// repo-wide. The synthesizer encodes the controller-module basename
	// as a hint suffix; the resolver uses this list to pick the candidate
	// whose source_file matches.
	globalMulti := make(map[knKey][]int, len(merged))
	// #1217: migrate legacy http_endpoint entities to the new split kinds
	// based on their pattern_type property. Graphs indexed before this
	// release may still carry the old kind string; we rewrite it in-place
	// so the rest of the resolve pass works uniformly with the new kinds.
	for i := range merged {
		r := &merged[i]
		if r.Kind != httpEndpointKind {
			continue
		}
		if r.Properties != nil && r.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
			r.Kind = httpEndpointCallKind
			stats.CallsMigrated++
		} else {
			r.Kind = httpEndpointDefinitionKind
			stats.DefinitionsMigrated++
		}
	}

	for i := range merged {
		r := &merged[i]
		// Exclude all three http endpoint kinds from the handler index to
		// avoid synthetics resolving against each other.
		if r.Kind == httpEndpointDefinitionKind || r.Kind == httpEndpointCallKind || r.Kind == httpEndpointKind {
			continue
		}
		k := key{r.Kind, r.Name, r.SourceFile}
		if _, ok := idx[k]; !ok {
			idx[k] = i
		}
		gk := knKey{r.Kind, r.Name}
		if _, ok := globalIdx[gk]; !ok {
			globalIdx[gk] = i
		}
		globalMulti[gk] = append(globalMulti[gk], i)
	}

	// Build an index of definitions by canonical Name (= synthetic ID)
	// so that http_endpoint_call entities can be linked to their matching
	// http_endpoint_definition via a FETCHES edge (#1217).
	// Key: synthetic ID (e.g. "http:GET:/api/users") → merged index.
	definitionByName := make(map[string]int, len(merged))
	// #1615 — structural fallback index: normalized-path-key → []merged index.
	// When the exact-Name match misses (because the call and the definition
	// differ on API prefix / param-token name / trailing slash / case) we look
	// the call's normalized path keys up here and accept any definition with a
	// compatible verb. Conservative: only well-known API/version prefixes are
	// stripped and ANY-verb is the only cross-verb match (see
	// http_endpoint_match.go).
	definitionByPath := make(map[string][]int, len(merged))
	for i := range merged {
		r := &merged[i]
		if r.Kind == httpEndpointDefinitionKind {
			if _, ok := definitionByName[r.Name]; !ok {
				definitionByName[r.Name] = i
			}
			for _, k := range endpointMatchKeys(propOr(r, "path", "")) {
				definitionByPath[k] = append(definitionByPath[k], i)
			}
		}
	}

	// Collect indices of synthetics to drop (unresolved handlers).
	drop := map[int]bool{}

	// #1615 — records, for every call synthetic that resolves to a definition,
	// the rewrite from its FETCHES stub (http_endpoint_call:<name>) to the
	// definition stub (http_endpoint_definition:<name>). After the main loop we
	// sweep every caller→call-synthetic FETCHES edge and retarget it at the
	// definition so the call-site is no longer counted as an orphan. Without
	// this retarget, the structural call→definition match links the synthetic
	// pair but the inbound caller edge still points at the (non-definition)
	// call synthetic and keeps inflating the orphan-call metric.
	callStubToDefStub := map[string]string{}

	for i := range merged {
		r := &merged[i]
		if r.Kind != httpEndpointDefinitionKind && r.Kind != httpEndpointCallKind {
			continue
		}
		stats.Synthetics++

		// #754 — consumer-side: resolve source_caller into a FETCHES edge.
		// We run this BEFORE the producer-side source_handler branch so a
		// synthetic that somehow carries both is handled correctly (in
		// practice they're mutually exclusive: makeEmit in
		// http_endpoint_synthesis.go uses a single refPropKey per side).
		if r.Properties != nil {
			if callerRef := r.Properties["source_caller"]; callerRef != "" {
				if resolveCallerToFetchesEdge(callerRef, r, merged, idx) {
					stats.CallerResolved++
				} else {
					stats.CallerUnresolved++
				}
			}
		}

		// #1217 — for http_endpoint_call entities, attempt to link to the
		// matching http_endpoint_definition by canonical Name (= synthetic ID).
		// The synthetic ID is identical on both sides (http:<VERB>:<path>),
		// so a simple name lookup is sufficient. Emit FETCHES on success;
		// emit UNRESOLVED_FETCH when no definition is found in the merged set.
		if r.Kind == httpEndpointCallKind {
			defIdx, found := definitionByName[r.Name]
			var prefixNorm string
			if !found {
				// #1615 — structural fallback: match by normalized path shape
				// + compatible verb when the exact Name missed.
				// #2547 — resolveCallByPath now also tries prefix-injection for
				// frontend calls that omit the backend's API mount prefix.
				defIdx, prefixNorm, found = resolveCallByPath(r, merged, definitionByPath)
			}
			if found {
				def := &merged[defIdx]
				callStub := r.Kind + ":" + r.Name
				defStub := def.Kind + ":" + def.Name
				edgeProps := map[string]string{
					"pattern_type": "http_endpoint_split_resolved",
					"resolved":     "true",
				}
				// #2547 — stamp prefix_normalized when tier-2 prefix-injection
				// matched so the match strategy is traceable in the graph.
				if prefixNorm != "" {
					edgeProps["prefix_normalized"] = prefixNorm
				}
				r.Relationships = append(r.Relationships, types.RelationshipRecord{
					FromID:     callStub,
					ToID:       defStub,
					Kind:       fetchesEdgeKind,
					Properties: edgeProps,
				})
				// #1615 — remember the stub rewrite so the post-loop sweep can
				// retarget inbound caller→call-synthetic FETCHES edges at the
				// definition.
				callStubToDefStub[callStub] = defStub
				stats.CallsLinked++
			} else {
				// No matching definition found — emit UNRESOLVED_FETCH so the
				// orphan is first-class in the graph topology (deprecates the
				// post-hoc orphan_caller detection from #1099).
				r.Relationships = append(r.Relationships, types.RelationshipRecord{
					FromID: r.Kind + ":" + r.Name,
					ToID:   r.Name, // canonical ID stub — no definition entity exists
					Kind:   string(types.RelationshipKindUnresolvedFetch),
					Properties: map[string]string{
						"pattern_type": "http_endpoint_split_unresolved",
						"path":         propOr(r, "path", ""),
						"verb":         propOr(r, "verb", ""),
					},
				})
				stats.CallsUnresolved++
			}
			// http_endpoint_call has no source_handler — skip the handler
			// resolution branch below.
			stats.NoHandlerProp++
			continue
		}

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

		// #2691 — Rails-style cross-file hint: when the synthesizer
		// supplied a `handler_file` property (derived from
		// 'users#index' → app/controllers/users_controller.rb plus
		// any enclosing namespace stack), prefer that file as the
		// same-file lookup target. This disambiguates method names
		// shared across many controllers (every Rails app has dozens
		// of `index` / `show` / `create` methods) BEFORE the global
		// (kind, name) fallback picks the first arbitrary match.
		lookupFile := r.SourceFile
		handlerFileHint := ""
		if r.Properties != nil {
			if hf := r.Properties["handler_file"]; hf != "" {
				lookupFile = hf
				handlerFileHint = hf
			}
		}
		// Prefer same-file match (handlers and route synthetics are
		// often emitted from the same file by Phase 1 construction).
		handlerIdx, found := idx[key{hk, hn, lookupFile}]
		// #2692 — Phoenix-style cross-file hint: the synthesizer cannot
		// know the full controller file path because Phoenix repos
		// arrange them differently (lib/myapp_web/controllers/, web/,
		// etc.). When the exact-file lookup misses and a `handler_file`
		// hint is set, use it as a SUBSTRING match against every
		// candidate entity with the right (kind, name).
		if !found && handlerFileHint != "" {
			candidateKinds := append([]string{hk}, resolverKindEquivalents[hk]...)
			for _, ck := range candidateKinds {
				for _, ci := range globalMulti[knKey{ck, hn}] {
					if strings.Contains(merged[ci].SourceFile, handlerFileHint) {
						handlerIdx = ci
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		if !found {
			// Cross-file fallback (#753). Django composed routes record
			// a `View:<ViewSet>` handler reference whose entity lives in
			// views.py while the synthetic lives in urls.py. Express
			// imported controllers have the same shape — handler in
			// controllers/users.js, route registration in routes.js.
			// Try the global (kind, name) index before giving up.
			//
			// Skip the cross-file fallback when the reference is
			// Kind="Route" + Name=<path> — that's Spring's
			// "synthesizer didn't have the method name" placeholder
			// and would always collide with the synthetic itself.
			if hk == "Route" {
				stats.HandlerDropped++
				drop[i] = true
				continue
			}
			// #2851 — when a `handler_file` hint was supplied but no entity
			// in that file matched (e.g. a declarative Sails / AdonisJS-
			// resource route whose controller class is out of the indexed
			// scope, or whose object-literal action methods the extractor
			// did not surface as symbols), do NOT fall through to the
			// unscoped global (kind, name) index. The hint asserts a
			// SPECIFIC file; resolving the bare method name globally would
			// misattribute the endpoint to an unrelated same-named method in
			// a different module (every controller has a `create` / `show`).
			// Keep the synthetic with its source_handler property intact —
			// the handler IS attributed (as a property), just not cross-
			// linked to an extracted entity. This preserves the route in the
			// graph instead of dropping a genuine endpoint.
			if handlerFileHint != "" {
				stats.NoHandlerProp++
				continue
			}
			handlerIdx, found = globalIdx[knKey{hk, hn}]
			if !found {
				// Cross-kind fallback. Synthesizers historically emit
				// `Controller:<name>` but the Python YAML rules + the
				// generic SCOPE extractor produce `SCOPE.Operation`
				// for function-shaped handlers (Flask def, FastAPI
				// def, Express function expressions). Likewise the
				// Java AST pass emits `SCOPE.Operation:Class.method`
				// while older synthesizers still emit `Controller:method`.
				// Try the known equivalence classes before dropping —
				// without this fallback every Flask synthetic with a
				// Controller-shaped ref gets dropped because the
				// matching entity has kind SCOPE.Operation. #753.
				for _, altKind := range resolverKindEquivalents[hk] {
					if hi, ok := globalIdx[knKey{altKind, hn}]; ok {
						handlerIdx = hi
						found = true
						break
					}
				}
			}
			if !found {
				stats.HandlerDropped++
				drop[i] = true
				continue
			}
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
		// #2678 — rebind the synthetic's source_file / start_line / end_line
		// to the resolved handler so the endpoint points at the method body,
		// not the routing/registration site. Mirrors the DRF fix (#2677): for
		// route-table frameworks (Laravel, Rust Axum, future Phoenix/ASP.NET)
		// the synthetic was anchored to the routes file by construction. For
		// annotation-based frameworks (Spring, JAX-RS) the file was already
		// correct but the line was unset; this rebind populates it from the
		// handler entity. Record the previous values as properties so the bug
		// is auditable and the resolver's strategy is traceable in the graph.
		if handler.SourceFile != "" {
			if r.Properties == nil {
				r.Properties = map[string]string{}
			}
			if r.SourceFile != "" && r.SourceFile != handler.SourceFile {
				r.Properties["registration_source_file"] = r.SourceFile
			}
			if r.StartLine > 0 && r.StartLine != handler.StartLine {
				r.Properties["registration_start_line"] = itoaSmall(r.StartLine)
			}
			r.SourceFile = handler.SourceFile
			r.StartLine = handler.StartLine
			r.EndLine = handler.EndLine
			if r.Language == "" {
				r.Language = handler.Language
			}
			// Mark that the handler resolution and re-attribution passed
			r.Properties["attribution"] = "handler_resolved"
		}
		// Clear the now-redundant properties.
		delete(r.Properties, "source_handler")
		delete(r.Properties, "handler_file")
		stats.HandlerResolved++
	}

	// #1615 — retarget caller→call-synthetic FETCHES edges at the resolved
	// definition. The caller edges were emitted by resolveCallerToFetchesEdge
	// with ToID = "http_endpoint_call:<name>"; once that call synthetic has been
	// matched (exact or structural) to a definition we point the caller straight
	// at the definition so the call-site stops counting as an orphan. Only
	// http_endpoint_client_synthesis_resolved edges are touched; all other
	// FETCHES edges (including the call→def edges we just emitted) are left
	// intact. No-op when callStubToDefStub is empty.
	if len(callStubToDefStub) > 0 {
		for i := range merged {
			rels := merged[i].Relationships
			for j := range rels {
				rel := &rels[j]
				if rel.Kind != fetchesEdgeKind {
					continue
				}
				if rel.Properties["pattern_type"] != "http_endpoint_client_synthesis_resolved" {
					continue
				}
				if defStub, ok := callStubToDefStub[rel.ToID]; ok {
					rel.ToID = defStub
					if rel.Properties == nil {
						rel.Properties = map[string]string{}
					}
					rel.Properties["retargeted"] = "http_endpoint_call_to_definition"
					stats.CallerEdgesRetargeted++
				}
			}
		}
	}

	// Issue #1999 — DTO ↔ Handler bidirectional REFERENCES edges.
	//
	// http_endpoint_definition entities carry `request_body_type` and/or
	// `response_body_type` properties (post-#1909 for Java JAX-RS / Spring
	// controllers). Those properties already encode the handler → DTO
	// direction implicitly. But the inverse — "which handlers consume this
	// DTO?" — is not directly walkable from the DTO node. Without it, the
	// dashboard flows section has to text-scan source_window content of
	// every handler to reconstruct the linkage.
	//
	// This pass walks every http_endpoint_definition with a non-empty
	// {request,response}_body_type, resolves the DTO simple name to a
	// candidate Schema / Component / Model in the merged set, and appends a
	// REFERENCES edge FROM the DTO TO the handler (the inverse direction).
	// The edge is annotated with reference_kind = "request_body" or
	// "response_body" so downstream consumers can filter the surfacing.
	//
	// Resolution heuristic: first lookup by (kind, name) using globalIdx for
	// the same handler kind family; failing that, scan globalIdx for any
	// entity whose Name == typeName and whose Kind looks DTO-ish
	// (SCOPE.Component, SCOPE.Schema, Model, Schema, DTO, Component). We
	// pick the first match; ambiguity is rare for DTOs (project-scoped
	// names), and a wrong match still produces useful navigation.
	//
	// Edges live on the DTO entity's embedded Relationships so the entity
	// is self-documenting — the edge travels with the DTO record through
	// the merge + resolver pipeline.
	dtoKinds := map[string]bool{
		"SCOPE.Component": true,
		"SCOPE.Schema":    true,
		"SCOPE.Class":     true,
		"Model":           true,
		"Schema":          true,
		"DTO":             true,
		"Component":       true,
	}
	resolveDTO := func(typeName string) int {
		if typeName == "" {
			return -1
		}
		for kind := range dtoKinds {
			if idx, ok := globalIdx[knKey{kind, typeName}]; ok {
				return idx
			}
		}
		return -1
	}
	for i := range merged {
		r := &merged[i]
		if r.Kind != httpEndpointDefinitionKind {
			continue
		}
		if r.Properties == nil {
			continue
		}
		handlerID := r.ID
		if handlerID == "" {
			// Endpoint synthesis hasn't stamped IDs yet — fall back to the
			// synthetic ID encoded in Name (e.g. "http:POST:/orders"). The
			// resolver downstream re-stamps these consistently.
			handlerID = httpEndpointDefinitionKind + ":" + r.Name
		}
		for _, propKey := range []string{"request_body_type", "response_body_type"} {
			typeName := r.Properties[propKey]
			if typeName == "" {
				continue
			}
			dtoIdx := resolveDTO(typeName)
			if dtoIdx < 0 {
				stats.DTOHandlerEdgesUnresolved++
				continue
			}
			refKind := "request_body"
			if propKey == "response_body_type" {
				refKind = "response_body"
			}
			merged[dtoIdx].Relationships = append(merged[dtoIdx].Relationships,
				types.RelationshipRecord{
					FromID: merged[dtoIdx].ID,
					ToID:   handlerID,
					Kind:   string(types.RelationshipKindReferences),
					Properties: map[string]string{
						"reference_kind": refKind,
						"pattern_type":   "dto_handler_bidirectional",
					},
				})
			stats.DTOHandlerEdgesEmitted++
		}
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

// resolveCallerToFetchesEdge attempts to resolve a consumer synthetic's
// `source_caller` property into a real caller entity in the same file
// and, on success, appends a FETCHES edge from caller → synthetic and
// clears the property. Returns true iff an edge was emitted.
//
// The `key` type and `idx` map are passed by the caller (they're
// computed once per resolve pass). We do a primary lookup on the
// declared (kind, name, file) and fall back to a small allow-list of
// equivalent caller kinds — the consumer extractors stamp
// `source_caller="Function:<name>"` regardless of whether the real
// merged record's kind is "Function", "SCOPE.Operation", "Method", or
// a framework-specific kind like "TypeScriptFunction".
func resolveCallerToFetchesEdge(
	callerRef string,
	syn *types.EntityRecord,
	merged []types.EntityRecord,
	idx map[httpResolveKey]int,
) bool {
	ck, cn, ok := splitHandlerRef(callerRef)
	if !ok {
		return false
	}
	emit := func(callerIdx int) {
		caller := &merged[callerIdx]
		fromStub := caller.Kind + ":" + caller.Name
		toStub := syn.Kind + ":" + syn.Name
		caller.Relationships = append(caller.Relationships, types.RelationshipRecord{
			FromID: fromStub,
			ToID:   toStub,
			Kind:   string(types.RelationshipKindFetches),
			Properties: map[string]string{
				"pattern_type": "http_endpoint_client_synthesis_resolved",
				"framework":    propOr(syn, "framework", ""),
			},
		})
		delete(syn.Properties, "source_caller")
	}
	if callerIdx, found := idx[httpResolveKey{ck, cn, syn.SourceFile}]; found {
		emit(callerIdx)
		return true
	}
	for _, altKind := range callerKindAliases(ck) {
		if callerIdx, found := idx[httpResolveKey{altKind, cn, syn.SourceFile}]; found {
			emit(callerIdx)
			return true
		}
	}
	// Final fallback: the consumer extractors stamp the enclosing
	// function's NAME, but real-world JS/TS class-field arrow methods
	// (e.g. `byId = (id) => $http.get(...)` on fixture-e) are not
	// surfaced as discrete function entities by the per-language
	// extractor — only the enclosing class or the file-level component
	// is. To keep the consumer http_endpoint reachable in the graph
	// (so the process-flow BFS can land on it and the cross-stack
	// detector can fire), wire FETCHES edges from EVERY plausible
	// same-file container (class, module, file-component, exported
	// service singleton) to the synthetic. The cross-repo HTTP linker
	// is unaffected — it pairs synthetics by Name only. Emitting
	// multiple FETCHES edges is logically over-coarse but structurally
	// correct: whichever container the BFS actually reaches via CALLS
	// resolution becomes a viable entry into the bridge.
	emitted := false
	for i := range merged {
		c := &merged[i]
		if c.SourceFile != syn.SourceFile {
			continue
		}
		if !isFallbackCallerCandidate(c) {
			continue
		}
		// Skip any http endpoint synthetic (all three kind variants).
		if c.Kind == httpEndpointKind || c.Kind == httpEndpointDefinitionKind || c.Kind == httpEndpointCallKind {
			continue
		}
		emit(i)
		emitted = true
	}
	return emitted
}

// isFallbackCallerCandidate reports whether an entity is a plausible
// source for a FETCHES edge when the precise per-method caller can't be
// resolved. We accept the file-level container kinds (SCOPE.Component
// where Name=path, SCOPE.Module, SCOPE.File) AND any class-shaped
// entity (SCOPE.Component / SCOPE.Class / SCOPE.Operation) in the same
// file. This produces a small fan-out (typically 1-3 edges per
// synthetic) and keeps the consumer endpoint reachable regardless of
// which container the resolver/BFS happens to traverse to.
func isFallbackCallerCandidate(r *types.EntityRecord) bool {
	switch r.Kind {
	case "SCOPE.Component", "SCOPE.Module", "SCOPE.File",
		"SCOPE.Class", "SCOPE.Operation", "SCOPE.Function",
		"Function", "Method":
		return true
	}
	return false
}

// callerKindAliases returns the set of entity kinds that the consumer
// extractors might use for a caller named in `source_caller`. The JS/TS
// and Python extractors stamp `Function:<name>` but the actual merged
// record may be a SCOPE.Operation, a Method, or a language-specific
// function kind depending on which extractor produced it. Probing this
// list lets us resolve callers without forcing the extractors to know
// the downstream kind name in advance.
func callerKindAliases(declared string) []string {
	switch declared {
	case "Function":
		return []string{
			"SCOPE.Operation",
			"Method",
			"TypeScriptFunction",
			"JavaScriptFunction",
			"PythonFunction",
		}
	case "Method":
		return []string{"Function", "SCOPE.Operation"}
	case "SCOPE.Operation":
		return []string{"Function", "Method"}
	}
	return nil
}

// itoaSmall converts a non-negative int to its decimal string. Used by the
// #2678 rebind to stash registration_start_line as a property without
// pulling in strconv.
func itoaSmall(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for n > 0 {
		p--
		b[p] = byte('0' + n%10)
		n /= 10
	}
	return string(b[p:])
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
