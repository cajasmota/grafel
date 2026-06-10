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
	// #4351 — TESTS edges emitted from an e2e test_suite to the
	// http_endpoint_definition it exercises via a supertest route-by-string call.
	E2ERouteTestEdges int
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

// httpResolveNameKey is the (kind, name) index key used by the global
// bare-name fallback and the multi-match candidate list. Package-level so
// helpers such as firstAppCandidate (#3426) can take it by value.
type httpResolveNameKey struct{ kind, name string }

// ResolveHTTPEndpointHandlers is the repoTag-less entry point retained for the
// existing call sites and the engine test corpus. It delegates with an empty
// repoTag, in which case the e2e route-test pass emits its TESTS edges as
// `Kind:Name` stubs (resolved later by resolve.References). Prefer
// ResolveHTTPEndpointHandlersWithRepo from the production pipeline so the e2e
// pass can mint each endpoint's unique entity ID directly and resolve
// same-Name endpoint collisions (#4651).
func ResolveHTTPEndpointHandlers(merged []types.EntityRecord) ([]types.EntityRecord, ResolveHTTPEndpointStats) {
	return ResolveHTTPEndpointHandlersWithRepo(merged, "")
}

// ResolveHTTPEndpointHandlersWithRepo is ResolveHTTPEndpointHandlers with the
// caller's repoTag threaded through to the e2e route-test linker (#4651). When
// repoTag is non-empty the linker emits each TESTS edge with the matched
// http_endpoint_definition's deterministic entity ID (graph.EntityID over the
// def's Kind/Name/SourceFile) instead of an ambiguous `Kind:Name` stub — so a
// route whose synthesized endpoint Name collides across modules (upvate-v3 has
// many same-named handlers/routes, e.g. `getCounts`) still credits the correct,
// per-file endpoint as covered rather than dangling on an ambiguous name.
func ResolveHTTPEndpointHandlersWithRepo(merged []types.EntityRecord, repoTag string) ([]types.EntityRecord, ResolveHTTPEndpointStats) {
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
	type knKey = httpResolveNameKey
	globalIdx := make(map[knKey]int, len(merged))
	// #4319 — same-file bare↔qualified bridge index. The per-language
	// synthesizers were written against an extractor convention that records
	// handlers by BARE method name (NestJS / Express / Axum / Rocket / JAX-RS
	// all stamp `source_handler=Controller:<method>`). Some extractor
	// configurations instead land a controller method as a QUALIFIED entity
	// (`<Class>.<method>` — the same shape Django/Spring/ASP.NET handlers use),
	// so the resolver's exact-Name lookup misses and the synthetic is DROPPED,
	// leaving the http_endpoint_definition a graph island (its handler's
	// VALIDATES / CALLS / RETURNS edges are unreachable from the endpoint node).
	// This index maps (sourceFile, bareName) → candidate indices of same-file
	// handler entities whose Name is exactly `bareName` OR ends in
	// `.<bareName>` (qualified `Class.method`). It is consulted ONLY as a
	// same-file fallback, so it can never mis-bridge an endpoint to a method in
	// a different controller, and it keys on the precise method name so a
	// multi-method controller maps each endpoint to ITS OWN handler.
	type fileBareKey struct{ sourceFile, bare string }
	sameFileBareIdx := make(map[fileBareKey][]int, len(merged))
	// #4319 re-fix — file:line co-location index. The bare↔qualified NAME
	// reconciliation above only fires when the synthetic carries a bindable
	// `source_handler` whose bare name matches a same-file handler. But the LIVE
	// NestJS failure is different: entity-merge can keep, for a given (verb,path),
	// a synthetic that carries NO usable `source_handler` (e.g. a same-path
	// definition emitted by another pass wins attribution), so every name-based
	// path misses and the endpoint is left a graph island even though its handler
	// Operation sits at the EXACT same file:line (the decorated controller
	// method). This index maps (sourceFile, startLine) → candidate indices of
	// handler-kind Operation entities at that position. It is consulted ONLY as
	// the final fallback, AFTER all name-based resolution fails, and ONLY binds
	// when EXACTLY ONE handler-kind Operation sits at that file:line — so it can
	// never guess between a DTO/Schema (different line) or two co-located methods.
	type fileLineKey struct {
		sourceFile string
		line       int
	}
	sameFileLineIdx := make(map[fileLineKey][]int, len(merged))
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
		// #4319 — register this handler under its bare method name within its
		// source file. For a qualified Name (`Class.method`) the bare key is
		// `method`; for an already-bare Name the bare key equals the Name (so
		// the inverse mismatch — synthesizer stamps qualified, handler is bare —
		// is also covered).
		bare := r.Name
		if dot := strings.LastIndexByte(bare, '.'); dot >= 0 && dot < len(bare)-1 {
			bare = bare[dot+1:]
		}
		if bare != "" {
			fk := fileBareKey{r.SourceFile, bare}
			sameFileBareIdx[fk] = append(sameFileBareIdx[fk], i)
		}
		// #4319 re-fix — register handler-kind Operations under their file:line so
		// the co-location fallback can find the controller method an endpoint sits
		// on. Restricted to handler-shaped kinds (a controller/route method) so a
		// DTO Component / Schema / Class that happens to share the line is never a
		// co-location candidate. A non-positive line carries no co-location signal
		// (synthetics and many declarations land at 0), so skip it.
		if r.StartLine > 0 && isCoLocationHandlerKind(r.Kind) {
			flk := fileLineKey{r.SourceFile, r.StartLine}
			sameFileLineIdx[flk] = append(sameFileLineIdx[flk], i)
		}
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
	// #4351 — flat list of definition indices for the e2e route-test matcher,
	// which scans every definition with the concrete-vs-template segment matcher.
	var defIndices []int
	for i := range merged {
		r := &merged[i]
		if r.Kind == httpEndpointDefinitionKind {
			if _, ok := definitionByName[r.Name]; !ok {
				definitionByName[r.Name] = i
			}
			for _, k := range endpointMatchKeys(propOr(r, "path", "")) {
				definitionByPath[k] = append(definitionByPath[k], i)
			}
			defIndices = append(defIndices, i)
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
			// #4319 re-fix — no bindable source_handler (the live NestJS island:
			// the surviving merged synthetic for this (verb,path) carries no
			// handler ref). Before giving up, try file:line co-location: bind to
			// the lone handler-kind Operation sitting at the endpoint's own
			// file:line (the decorated controller method). Guarded to fire only on
			// an EXACTLY-ONE match so it never guesses.
			if hi, ok := resolveCoLocatedHandler(r, sameFileLineIdx[fileLineKey{r.SourceFile, r.StartLine}], merged); ok {
				bridgeEndpointToHandler(&merged[hi], r)
				stats.HandlerResolved++
				continue
			}
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
		// #3426 — same-file CROSS-KIND lookup BEFORE the global bare-name
		// fallback. The exact-kind same-file lookup (above) misses for
		// annotation-based frameworks (NestJS @Controller, Spring, JAX-RS)
		// because the synthesizer emits the handler ref as `Controller:check`
		// while the real handler method `check()` is indexed as a
		// SCOPE.Operation (or another method kind) in the SAME file. Without
		// this step the resolver falls to the GLOBAL bare-name index, where a
		// common method name like `check` collides repo-wide (e.g. a
		// `function check(...)` in scripts/docs-check.mjs) and mis-sources the
		// route to an unrelated tooling file. Same-file resolution is the
		// correct default for annotation frameworks: the handler is emitted in
		// the same file as the route synthetic by construction, so a same-file
		// hit is strictly more precise than any global match. Only attempt this
		// when no handler_file hint redirected lookupFile elsewhere; with a hint
		// the global hint-substring branch above already covered cross-kind.
		if !found && handlerFileHint == "" {
			for _, altKind := range resolverKindEquivalents[hk] {
				if hi, ok := idx[key{altKind, hn, lookupFile}]; ok {
					handlerIdx = hi
					found = true
					break
				}
			}
		}
		// #4319 — same-file bare↔qualified bridge. The exact-Name lookups above
		// match only when the synthesizer's bare `source_handler` name and the
		// handler entity's Name agree character-for-character. For the
		// bare-name-emitting frameworks (NestJS / Express / Axum / Rocket /
		// JAX-RS) the handler method is sometimes indexed QUALIFIED
		// (`Controller.method`) — the same shape Django/Spring/ASP.NET handlers
		// carry — so the route synthetic was DROPPED and the
		// http_endpoint_definition left a graph island. Here, before falling
		// back to the (risky) cross-file global lookup, we try the same-file
		// bare-name index: a handler in the SAME file whose Name is exactly `hn`
		// or ends in `.hn`. Same-file scoping makes this unambiguous — a
		// multi-method controller still maps each endpoint to its OWN handler
		// because the bare method name is the key. When more than one candidate
		// shares the bare name in one file (rare: an overload-like collision) we
		// do NOT guess; we leave it for the existing fallbacks so we never
		// mis-bridge. Skipped when a handler_file hint redirected lookupFile.
		if !found && handlerFileHint == "" {
			if cands := sameFileBareIdx[fileBareKey{r.SourceFile, hn}]; len(cands) == 1 {
				handlerIdx = cands[0]
				found = true
			}
		}
		if !found {
			// #4319 re-fix — file:line co-location fallback. All same-file
			// name-based resolution (exact-kind, cross-kind, bare↔qualified) has
			// missed: the synthesizer's `source_handler` name does not match any
			// same-file handler entity (the live NestJS case — the bare ref points
			// at a method the indexer landed under a different identity, or the
			// surviving merged synthetic's ref is stale). Before resorting to the
			// risky cross-file GLOBAL bare-name guess, bind to the lone handler-kind
			// Operation at the endpoint's OWN file:line — the decorated controller
			// method. Same-file + same-line + exactly-one makes this unambiguous and
			// strictly safer than a repo-wide name guess; it never fires when 0 or
			// >1 handlers share the line (no guessing) and never binds a DTO/Schema
			// (those sit at a different line and are not handler-kind).
			if hi, ok := resolveCoLocatedHandler(r, sameFileLineIdx[fileLineKey{r.SourceFile, r.StartLine}], merged); ok {
				bridgeEndpointToHandler(&merged[hi], r)
				stats.HandlerResolved++
				continue
			}
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
			// #3426 — GLOBAL bare-name fallback, with a non-app/tooling
			// exclusion. A route handler must NEVER resolve to a build/CLI
			// script: a common method name like `check` collides with a
			// `function check(...)` in scripts/docs-check.mjs and the old
			// globalIdx (first-writer-wins) could bind the route to it,
			// mis-sourcing the endpoint to a tooling file. We iterate the
			// globalMulti candidate LIST (across the declared kind + its
			// equivalence classes) and pick the FIRST candidate whose
			// SourceFile is NOT isNonAppSourceFile. If every candidate is a
			// non-app file we leave `found` false so the synthetic keeps its
			// own (correct) source instead of being rebound to a script.
			// Try the declared kind first, then the cross-kind equivalents
			// (Flask/FastAPI/Express function handlers land as SCOPE.Operation
			// while the synthesizer emits Controller:<name> — #753).
			handlerIdx, found = firstAppCandidate(merged, globalMulti, knKey{hk, hn})
			if !found {
				for _, altKind := range resolverKindEquivalents[hk] {
					if hi, ok := firstAppCandidate(merged, globalMulti, knKey{altKind, hn}); ok {
						handlerIdx = hi
						found = true
						break
					}
				}
			}
			if !found {
				// #3426 — distinguish "no candidate at all" (genuine orphan →
				// drop) from "the only global candidate(s) live in non-app /
				// tooling files" (e.g. the bare name `check` exists ONLY in
				// scripts/docs-check.mjs). In the latter case we must NOT drop
				// a real route just because its handler couldn't be cross-
				// linked to an app entity — and we must NOT rebind it to the
				// build script. Keep the synthetic with its own (controller)
				// source and source_handler intact, mirroring the
				// handler_file-hint miss path (#2851).
				if hasGlobalCandidate(globalMulti, knKey{hk, hn}, resolverKindEquivalents[hk]) {
					stats.NoHandlerProp++
					continue
				}
				stats.HandlerDropped++
				drop[i] = true
				continue
			}
		}

		// Resolved via a name-based path. Emit the IMPLEMENTS bridge and rebind
		// the synthetic onto the handler body.
		bridgeEndpointToHandler(&merged[handlerIdx], r)
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

	// #4351 — link e2e HTTP route tests (supertest route-by-string) to the
	// http_endpoint_definition they exercise. The Jest extractor stamps an
	// `e2e_route_calls` property on the one-per-spec test_suite; here we resolve
	// each (verb, route) against the definition index built above and emit a
	// TESTS edge from the suite to each uniquely-matched endpoint. Runs at
	// resolve-time (merge-stable, cross-file index available) and is
	// conservative — only unique verb+route matches produce an edge.
	stats.E2ERouteTestEdges = linkE2ERouteTestsToEndpoints(merged, definitionByPath, defIndices, repoTag)

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

// firstAppCandidate looks up the globalMulti candidate list for `gk`
// (kind, name) and returns the index of the FIRST candidate whose
// SourceFile is NOT a non-app/tooling file (per isNonAppSourceFile in
// http_endpoint_synthesis.go). #3426: the global bare-name fallback must
// never bind a route handler to a build/CLI script — a common method name
// like `check` collides with `function check(...)` in
// scripts/docs-check.mjs and would mis-source the endpoint to tooling. When
// every candidate is a non-app file (or none exist) it returns ok=false so
// the caller skips the rebind and the synthetic keeps its own source.
func firstAppCandidate(merged []types.EntityRecord, globalMulti map[httpResolveNameKey][]int, gk httpResolveNameKey) (int, bool) {
	for _, ci := range globalMulti[gk] {
		if !isNonAppSourceFile(merged[ci].SourceFile) {
			return ci, true
		}
	}
	return 0, false
}

// hasGlobalCandidate reports whether ANY global (kind, name) candidate
// exists for the base kind or one of its equivalence kinds, regardless of
// whether that candidate is an app or non-app file. #3426: used to tell a
// genuine unresolved orphan (no candidate anywhere → drop) apart from a
// tooling-only collision (a candidate exists but only in a build/CLI
// script → keep the synthetic, don't rebind, don't drop).
func hasGlobalCandidate(globalMulti map[httpResolveNameKey][]int, base httpResolveNameKey, altKinds []string) bool {
	if len(globalMulti[base]) > 0 {
		return true
	}
	for _, ak := range altKinds {
		if len(globalMulti[httpResolveNameKey{ak, base.name}]) > 0 {
			return true
		}
	}
	return false
}

// isCoLocationHandlerKind reports whether a kind is a plausible HTTP-route
// HANDLER (a controller/route method or function) for the #4319 file:line
// co-location fallback. It deliberately EXCLUDES container/data kinds — a
// SCOPE.Class controller, a SCOPE.Component / Schema / Model DTO — so a DTO or
// the controller class that shares a line with the method is never a
// co-location candidate. Mirrors the handler kinds the synthesizers and the
// cross-kind equivalence table already treat as methods.
func isCoLocationHandlerKind(kind string) bool {
	switch kind {
	case "SCOPE.Operation", "SCOPE.Function", "Operation", "Function", "Method":
		return true
	}
	return false
}

// resolveCoLocatedHandler returns the index of the unique handler-kind
// Operation co-located at the endpoint's file:line, or ok=false when the
// binding would be a guess. `cands` is the pre-filtered candidate list from the
// file:line index (already restricted to handler-kind Operations at that
// position). The no-guess guard: bind ONLY when exactly one candidate exists,
// and never when the endpoint carries no positive line (a synthetic anchored at
// line 0 has no co-location signal). The candidate is re-validated as a handler
// kind defensively even though the index is pre-filtered, so a future index
// change can't silently widen the binding.
func resolveCoLocatedHandler(endpoint *types.EntityRecord, cands []int, merged []types.EntityRecord) (int, bool) {
	if endpoint == nil || endpoint.StartLine <= 0 {
		return 0, false
	}
	if len(cands) != 1 {
		return 0, false
	}
	hi := cands[0]
	if hi < 0 || hi >= len(merged) {
		return 0, false
	}
	if !isCoLocationHandlerKind(merged[hi].Kind) {
		return 0, false
	}
	return hi, true
}

// bridgeEndpointToHandler emits the IMPLEMENTS bridge edge from a resolved
// handler to its http_endpoint synthetic and rebinds the synthetic's
// source_file/line onto the handler body. Shared by every resolution path
// (name-based and #4319 file:line co-location) so the bridge shape, the
// deploy-9 auth-permission propagation, and the #2678 attribution rebind stay
// identical regardless of HOW the handler was found.
func bridgeEndpointToHandler(handler, r *types.EntityRecord) {
	// Append an embedded IMPLEMENTS edge on the handler. Use placeholder ID
	// stubs (Kind:Name) for the endpoint; the resolver in buildDocument rewrites
	// these against the stamped entity index after we return.
	handler.Relationships = append(handler.Relationships, types.RelationshipRecord{
		FromID: handler.Kind + ":" + handler.Name,
		ToID:   r.Kind + ":" + r.Name,
		Kind:   implementsEdgeKind,
		Properties: map[string]string{
			"pattern_type": "http_endpoint_synthesis_resolved",
			"framework":    propOr(r, "framework", ""),
		},
	})
	// deploy-9 item-3 — propagate the endpoint's resolved fine-grained
	// authorisation identity onto the handler Operation (see
	// propagateHandlerAuthPermissions). Honest-partial + first-write-wins.
	propagateHandlerAuthPermissions(handler, r)
	// #2678 — rebind the synthetic's source_file / start_line / end_line to the
	// resolved handler so the endpoint points at the method body, not the
	// routing/registration site. Record the previous values for auditability.
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
		r.Properties["attribution"] = "handler_resolved"
	}
	// Clear the now-redundant properties.
	delete(r.Properties, "source_handler")
	delete(r.Properties, "handler_file")
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

// propagateHandlerAuthPermissions copies the resolved fine-grained
// authorisation identity from a synthesized http_endpoint onto its handler
// entity (deploy-9 item-3). The DRF get_permissions per-action page-key pass
// (#3978) stamps `auth_permissions` (e.g. the PERMISSION_PAGES["JURISDICTIONS"]
// constant key) on the endpoint, but symbol-keyed consumers — archigraph_inspect
// / get_source on the @action method, archigraph_auth_coverage starting from the
// handler — read the property off the handler Operation, which never carried it.
// We copy it (plus `auth_required`, which a page-key guard always implies) at the
// resolution site that already pairs an endpoint with its handler.
//
// First-write-wins: an existing value on the handler is preserved (an explicit
// handler annotation, or the first of several verb-routes that share one handler
// — the email PATCH/PUT pair). Honest-partial: an endpoint with no resolved
// `auth_permissions` (dynamic / unresolvable page key) propagates nothing, so the
// handler is not falsely marked as carrying a fine-grained grant.
func propagateHandlerAuthPermissions(handler, endpoint *types.EntityRecord) {
	if handler == nil || endpoint == nil {
		return
	}
	perms := propOr(endpoint, "auth_permissions", "")
	if perms == "" {
		return
	}
	if handler.Properties == nil {
		handler.Properties = map[string]string{}
	}
	if _, exists := handler.Properties["auth_permissions"]; !exists {
		handler.Properties["auth_permissions"] = perms
	}
	// A fine-grained page-key guard always requires authentication; mirror the
	// endpoint's auth_required so handler-keyed posture queries agree with the
	// endpoint. Only set when absent (do not downgrade an existing value).
	if _, exists := handler.Properties["auth_required"]; !exists {
		if ar := propOr(endpoint, "auth_required", ""); ar != "" {
			handler.Properties["auth_required"] = ar
		}
	}
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
