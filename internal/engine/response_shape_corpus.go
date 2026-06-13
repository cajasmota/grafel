// Corpus-wide response-shape extraction post-pass (#753).
//
// The per-file extractor in applyHTTPEndpointSynthesis only works when the
// handler lives in the same file as the route registration (Flask
// @app.route over def, JAX-RS @GET over method, FastAPI @app.get over
// def, inline Express closures). Real production codebases dispatch URLs
// from a central place: Django's urls.py points at view classes in
// views.py, DRF routers reference ViewSet classes in another module,
// Express apps import controllers, Spring composes RequestMapping
// prefixes across files.
//
// PR #744 (response shape extraction) shipped the per-file path and
// produced response_keys = 0 on every real corpus fixture as a result
// (#753). This pass closes that gap by running AFTER the producer-side
// passes have populated their handler references (`source_handler`,
// `drf_view_method`, or a ROUTES_TO edge) and resolving them across the
// full classified-file set rather than only same-file.
//
// Resolution priority for finding the handler entity, per http_endpoint:
//  1. `source_handler` property "<Kind>:<Name>" — set by every
//     framework-specific synthesizer (#534, #722).
//  2. `drf_view_method` property "<ViewSet>.<method>" — set by the DRF
//     action expander (#705). Falls back to the View entity for the
//     ViewSet class and looks up the method name inside.
//  3. ROUTES_TO relationship from a composed Route to a View entity
//     (Django composed routes set this in django_routes.go but do NOT
//     propagate the view name into `source_handler` because the
//     Phase-2 resolver historically required same-file resolution and
//     would drop the synthetic). We follow the edge here instead.
//
// Once we have a handler entity, we read its SourceFile content (still
// live on the `classified` slice at this point of the pipeline), and
// dispatch to the existing per-language shape extractor with that
// content as `src` and the handler name as `handler`. The extractors
// have always been written against the handler's source file — they
// just need to be given the right file.
//
// The pass is additive: it only adds Properties to http_endpoint
// entities, never removes them, never drops entities. It is a no-op for
// http_endpoints whose `response_keys` is already populated by the
// same-file pass.
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// handlerKey is the (Kind, Name) tuple used to index handler entities
// for cross-file resolution in the corpus-wide response-shape pass.
type handlerKey struct{ kind, name string }

// isHTTPEndpointKind reports whether a kind is one of the three http
// endpoint synthetic kinds. Post-#1217 the synthesizer splits endpoints
// into http_endpoint_definition / http_endpoint_call alongside the legacy
// http_endpoint kind; the corpus-wide response-shape pass must consider all
// three (it previously only matched the legacy kind, leaving cross-file
// definitions — e.g. Go gin routes whose handlers live in a sibling file —
// with empty response shapes).
func isHTTPEndpointKind(kind string) bool {
	return kind == httpEndpointKind ||
		kind == httpEndpointDefinitionKind ||
		kind == httpEndpointCallKind
}

// CorpusFileReader returns the source bytes for a repo-relative file
// path, or nil if not available. The corpus-wide post-pass uses this
// to read handler source from a different file than the route
// registration.
type CorpusFileReader func(relPath string) []byte

// ResponseShapeCorpusStats reports counters from the post-pass.
type ResponseShapeCorpusStats struct {
	// Endpoints is the total number of http_endpoint entities the pass
	// considered (after filtering out ones already populated by the
	// same-file pass).
	Endpoints int
	// HandlerResolved is the number of endpoints for which the pass
	// successfully located a handler entity to scan.
	HandlerResolved int
	// ShapeExtracted is the number of endpoints that had at least one
	// response/request key populated by the pass.
	ShapeExtracted int
	// NoHandlerFound counts endpoints where neither source_handler nor
	// drf_view_method nor a ROUTES_TO edge produced a resolvable handler.
	NoHandlerFound int
}

// ApplyResponseShapesCorpus runs the corpus-wide response-shape post-pass
// over the merged record set. It mutates `entities` in place by adding
// shape properties to http_endpoint entities whose handler can be
// located via cross-file resolution. Returns counters for verbose logging.
//
// `relationships` is the post-pass-2.5 standalone relationship slice
// (used to follow Route -> View ROUTES_TO edges).
//
// `fileReader` returns file content by repo-relative path. When the
// reader is nil or no content is available for a handler's file, the
// endpoint is skipped (handler not resolvable).
func ApplyResponseShapesCorpus(
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
	fileReader CorpusFileReader,
) ResponseShapeCorpusStats {
	var stats ResponseShapeCorpusStats
	if fileReader == nil || len(entities) == 0 {
		return stats
	}

	// Build a (Kind, Name) -> *EntityRecord index across the full merged
	// set so a handler reference from one file can resolve to an entity
	// in another file. We pick the first record for each (kind, name)
	// pair, matching the first-writer-wins disambiguation in BuildIndex.
	idx := make(map[handlerKey]*types.EntityRecord, len(entities))
	// Also index by Name only so we can fall back when the kind in
	// source_handler doesn't exactly match what the extractor emitted.
	// Example: Django composed-route synthesizer records `View:<Class>`
	// but the YAML extractor may have emitted that class as `Controller`
	// instead depending on the regex that fired first.
	byName := make(map[string][]*types.EntityRecord, len(entities))
	for i := range entities {
		e := &entities[i]
		if isHTTPEndpointKind(e.Kind) {
			continue
		}
		k := handlerKey{e.Kind, e.Name}
		if _, ok := idx[k]; !ok {
			idx[k] = e
		}
		byName[e.Name] = append(byName[e.Name], e)
	}

	// Build Route -> (handlerKind, handlerName) map from ROUTES_TO
	// edges so we can resolve a composed Route synthetic's handler even
	// when source_handler is empty or a placeholder. The edges are
	// emitted as Kind:Name stubs at this stage (FromID=Route:<path>,
	// ToID=<Kind>:<Name>) — django_routes.go produces View:<ViewSet>
	// targets while spring_routes.go produces Controller:<methodName>.
	routesToHandler := make(map[string]resolveHandlerRef)
	consider := func(rel types.RelationshipRecord) {
		if rel.Kind != "ROUTES_TO" {
			return
		}
		fromName := stubName(rel.FromID, "Route:")
		if fromName == "" {
			return
		}
		hk, hn, ok := splitHandlerRef(rel.ToID)
		if !ok {
			return
		}
		if _, exists := routesToHandler[fromName]; !exists {
			routesToHandler[fromName] = resolveHandlerRef{hk, hn}
		}
	}
	for _, r := range relationships {
		consider(r)
	}
	for i := range entities {
		for _, r := range entities[i].Relationships {
			consider(r)
		}
	}

	// srcByPath caches the string form of each handler file's content so
	// many endpoints sharing one large views.py reuse the same string
	// identity (and therefore the same memoized pySourceIndex) instead of
	// allocating + re-hashing a fresh string per endpoint (#5143).
	srcByPath := make(map[string]string)
	// pyIdxByPath caches the resolved per-source def/class index by handler
	// file path. This is the key O(n²)→O(n) fix: the file string is hashed
	// (to build the index) at most ONCE per file, then every endpoint on that
	// file reuses the prebuilt index via a cheap short-path-key map lookup
	// instead of re-hashing the multi-KB source on every call.
	pyIdxByPath := make(map[string]*pySourceIndex)

	for i := range entities {
		e := &entities[i]
		if !isHTTPEndpointKind(e.Kind) {
			continue
		}
		if e.Properties == nil {
			continue
		}
		// Skip endpoints whose shape was already populated by the
		// same-file pass. response_keys + response_schema + request_keys
		// are the three "successful extraction" indicators.
		if e.Properties["response_keys"] != "" ||
			e.Properties["response_schema"] != "" ||
			e.Properties["request_keys"] != "" {
			continue
		}
		stats.Endpoints++

		handlerEnt, methodName := resolveHandlerForEndpoint(e, idx, byName, routesToHandler)
		if handlerEnt == nil {
			stats.NoHandlerFound++
			continue
		}
		stats.HandlerResolved++

		// Read the handler's source file and dispatch to the
		// language-appropriate extractor with the resolved method name.
		// Reuse a previously-read string for the same path so the per-source
		// index memo (#5143) hits on string identity rather than re-hashing
		// an equal-but-distinct string for every endpoint in the file.
		src, ok := srcByPath[handlerEnt.SourceFile]
		if !ok {
			content := fileReader(handlerEnt.SourceFile)
			src = string(content)
			srcByPath[handlerEnt.SourceFile] = src
		}
		if len(src) == 0 {
			continue
		}
		framework := e.Properties["framework"]

		var sh shape
		switch handlerEnt.Language {
		case "python":
			// Many Django views are class-based — the response method
			// lives on the class (def get/post/list/create/...). When
			// we don't have an explicit method name (Django composed
			// routes give us only the View class), try the standard
			// DRF/Django entry methods in order of specificity.
			pyIdx, ok := pyIdxByPath[handlerEnt.SourceFile]
			if !ok {
				pyIdx = getPySourceIndex(src)
				pyIdxByPath[handlerEnt.SourceFile] = pyIdx
			}
			sh = extractPythonShapeWithFallbacksIdx(pyIdx, src, methodName, framework, e)
		case "javascript", "typescript":
			sh = extractJSShape(src, methodName, framework)
		case "java":
			sh = extractJavaShape(src, methodName, framework)
		case "go":
			sh = extractGoShape(src, methodName, framework)
		default:
			continue
		}
		if !sh.knownResponse && !sh.dynamicResponse {
			continue
		}
		writeShapeProps(e.Properties, sh)
		if e.Properties["response_keys"] != "" ||
			e.Properties["response_schema"] != "" ||
			e.Properties["request_keys"] != "" {
			stats.ShapeExtracted++
		}
	}
	return stats
}

// resolveHandlerForEndpoint locates the handler entity (the function /
// class whose source we should scan) for one http_endpoint. Returns
// the entity record and the method name to scan for (which may be
// different from the entity's Name when the endpoint references a
// method on a class — e.g. DRF ViewSet.create).
// resolveHandlerRef is a (kind, name) pair recording the handler entity
// pointed at by a ROUTES_TO edge from a composed Route.
type resolveHandlerRef struct{ kind, name string }

func resolveHandlerForEndpoint(
	e *types.EntityRecord,
	idx map[handlerKey]*types.EntityRecord,
	byName map[string][]*types.EntityRecord,
	routesToHandler map[string]resolveHandlerRef,
) (*types.EntityRecord, string) {
	props := e.Properties

	// 1) source_handler "<Kind>:<Name>" — primary path for every framework.
	if ref := props["source_handler"]; ref != "" {
		hk, hn, ok := splitHandlerRef(ref)
		if ok {
			// Spring's synthesizer emits source_handler="Route:<path>"
			// because at synthesis time it doesn't have the controller
			// method name — that lives in the spring_routes.go ROUTES_TO
			// edge from Route:<path> to Controller:<methodName>.
			// Follow the edge here.
			if hk == "Route" {
				if href, ok := routesToHandler[hn]; ok {
					if ent := lookupHandler(href.kind, href.name, idx, byName); ent != nil {
						return ent, methodNameFromRef(href.name)
					}
				}
			} else if ent := lookupHandler(hk, hn, idx, byName); ent != nil {
				return ent, methodNameFromRef(hn)
			}
		}
	}

	// 2) drf_view_method "ViewSet.method" — DRF action expander.
	if ref := props["drf_view_method"]; ref != "" {
		viewClass, method := ref, ""
		if dot := strings.LastIndex(ref, "."); dot > 0 {
			viewClass = ref[:dot]
			method = ref[dot+1:]
		}
		if ent := lookupHandler("View", viewClass, idx, byName); ent != nil {
			if method == "" {
				method = methodNameFromRef(viewClass)
			}
			return ent, method
		}
	}

	// 3) ROUTES_TO edge — Django composed routes only carry the View
	// name as a separate edge. The endpoint's Name is the synthetic
	// `http:VERB:/canonical/path` and the Route entity that the YAML +
	// AST passes emit shares the same canonical path (via the `path`
	// property on the synthetic).
	if path := props["path"]; path != "" {
		// The Route entity that owns the ROUTES_TO edge has Name equal
		// to the composed path (without the verb prefix). Try both with
		// and without a leading slash because django_routes.go composes
		// without one (`api/users`).
		candidates := []string{
			path,
			strings.TrimPrefix(path, "/"),
		}
		// Also try the URL fragment without the leading verb. Spring's
		// composed Route name is the path itself (e.g. "/owners/{id}");
		// Django composed routes don't carry a leading slash.
		for _, c := range candidates {
			if href, ok := routesToHandler[c]; ok {
				if ent := lookupHandler(href.kind, href.name, idx, byName); ent != nil {
					return ent, methodNameFromRef(href.name)
				}
			}
		}
	}

	return nil, ""
}

// lookupHandler resolves a (kind, name) reference to an entity. It
// prefers an exact kind match but falls back to any same-named entity
// when no exact kind hit exists — the Python YAML rules sometimes emit
// the same class as `View` or `Controller` depending on which regex
// fires first.
func lookupHandler(kind, name string, idx map[handlerKey]*types.EntityRecord, byName map[string][]*types.EntityRecord) *types.EntityRecord {
	if ent, ok := idx[handlerKey{kind, name}]; ok {
		return ent
	}
	// Cross-kind fallback for known equivalence classes.
	equiv := map[string][]string{
		"View":       {"Controller", "SCOPE.Class", "SCOPE.Function", "SCOPE.Operation"},
		"Controller": {"View", "SCOPE.Class", "SCOPE.Function", "SCOPE.Operation"},
		"Operation":  {"SCOPE.Operation", "Controller", "View"},
		"Route":      {"http_endpoint"},
	}
	for _, alt := range equiv[kind] {
		if ent, ok := idx[handlerKey{alt, name}]; ok {
			return ent
		}
	}
	// Last-ditch by-name fallback. Pick the first entity that looks like
	// a code unit (skip http_endpoint synthetics — we never scan those).
	for _, ent := range byName[name] {
		if ent.Kind == httpEndpointKind {
			continue
		}
		return ent
	}
	return nil
}

// methodNameFromRef strips a class-prefix from a dotted reference. The
// extractor's `findHandlerBody` looks for `def <name>(` so when our ref
// is `UserViewSet.create` we want to pass `create`. When the ref is a
// bare class like `UserViewSet`, return it unchanged — the Python
// fallback walker will try the standard ViewSet method names.
func methodNameFromRef(ref string) string {
	if dot := strings.LastIndex(ref, "."); dot > 0 {
		return ref[dot+1:]
	}
	return ref
}

// stubName extracts the Name portion from a "Kind:Name" stub when the
// stub has the expected prefix. Returns "" otherwise.
func stubName(stub, prefix string) string {
	if !strings.HasPrefix(stub, prefix) {
		return ""
	}
	return strings.TrimPrefix(stub, prefix)
}

// drfViewSetEntryMethods is the standard DRF / class-based-view list of
// method names that may serve a single endpoint. When the
// http_endpoint references a View class (rather than a specific
// method), we walk this list in order and merge keys from every method
// that exists in the class body. This means a class-based view that
// implements `def get` and `def post` contributes the union of their
// response keys — which is the right answer for a single canonical
// URL that dispatches by HTTP verb.
//
// Order matters only for early-exit on first-hit (we don't early-exit;
// we union). The list covers Django generic CBVs, DRF ViewSets, and
// the legacy `dispatch` entry point.
var drfViewSetEntryMethods = []string{
	"list", "create", "retrieve", "update", "partial_update", "destroy",
	"get", "post", "put", "patch", "delete", "head", "options",
}

// extractPythonShapeWithFallbacks runs the Python extractor against a
// concrete method name when one is known, then — if no shape was
// produced and the method name happens to be a View class identifier
// (CapitalCase ending in View / ViewSet) — walks the standard
// CBV/ViewSet entry methods and unions the results. This makes
// class-based views productive without requiring the synthesizer to
// pre-resolve a verb-to-method mapping.
func extractPythonShapeWithFallbacks(src, name, framework string, e *types.EntityRecord) shape {
	return extractPythonShapeWithFallbacksIdx(getPySourceIndex(src), src, name, framework, e)
}

// extractPythonShapeWithFallbacksIdx is extractPythonShapeWithFallbacks with
// the per-source def/class index resolved once by the caller. The corpus pass
// caches that index by handler-file path so the file string is hashed at most
// once per file, not once per endpoint (#5143).
func extractPythonShapeWithFallbacksIdx(idx *pySourceIndex, src, name, framework string, e *types.EntityRecord) shape {
	if name == "" {
		return shape{}
	}
	// If the synthesizer already gave us a concrete method name, trust it.
	if !looksLikeClassName(name) {
		return extractPythonShapeIdx(idx, src, name, framework)
	}
	// Class name — union across standard entry methods. We don't merge
	// extractor outputs across calls (the shape struct has no merge
	// helper), so we collect from each method and combine.
	//
	// The index (resolved above) is shared across all method extractions
	// for this file.
	var combined shape
	for _, method := range drfViewSetEntryMethods {
		if _, ok := idx.funcBodies[method]; !ok {
			// Method not defined anywhere in this file — skip without paying
			// for a full extractPythonShape scan.
			continue
		}
		sh := extractPythonShapeIdx(idx, src, method, framework)
		if sh.knownResponse || sh.dynamicResponse {
			mergeShape(&combined, sh)
		}
	}
	return combined
}

// looksLikeClassName returns true when `s` looks like a Python class
// identifier (starts with uppercase, contains no `.`, no spaces). Used
// to decide whether to walk the standard ViewSet method list.
func looksLikeClassName(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, ". ") {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// mergeShape folds src's keys/status/etc. into dst. The dst pointer
// must be non-nil. Keys are union'd; status codes are union'd.
func mergeShape(dst *shape, src shape) {
	if src.knownResponse {
		dst.knownResponse = true
	}
	if src.dynamicResponse {
		dst.dynamicResponse = true
	}
	dst.responseKeys = append(dst.responseKeys, src.responseKeys...)
	dst.errorKeys = append(dst.errorKeys, src.errorKeys...)
	dst.statusCodes = append(dst.statusCodes, src.statusCodes...)
	dst.requestKeys = append(dst.requestKeys, src.requestKeys...)
	if dst.responseSchema == nil && len(src.responseSchema) > 0 {
		dst.responseSchema = make(map[string]string, len(src.responseSchema))
	}
	for k, v := range src.responseSchema {
		if _, ok := dst.responseSchema[k]; !ok {
			dst.responseSchema[k] = v
		}
	}
	if dst.requestSchema == nil && len(src.requestSchema) > 0 {
		dst.requestSchema = make(map[string]string, len(src.requestSchema))
	}
	for k, v := range src.requestSchema {
		if _, ok := dst.requestSchema[k]; !ok {
			dst.requestSchema[k] = v
		}
	}
}
