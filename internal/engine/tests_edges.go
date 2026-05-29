// TESTS edge multi-hop propagation via HTTP router registration — #2549.
//
// # Problem
//
// Django REST Framework tests call the API through the Django test client:
//
//	self.client.post('/api/v1/schedule/import', data, format='json')
//
// The existing testmap extractor (internal/extractors/cross/testmap) detects
// the test function and emits a TESTS edge, but only to the HTTP-client method
// call object itself ("self.client.post").  The production ViewSet method
// (e.g. ScheduleViewset.import_csv) never receives a TESTS edge — it is
// invisible to coverage queries.
//
// The root cause is that the extractor works file-by-file and has no access to
// the routing graph that links `/api/v1/schedule/import` → the ViewSet. Only
// 14 of 8,564 production entities on upvate had ANY incoming TESTS edge (0.16%
// coverage) as a result.
//
// # Fix
//
// ApplyTestsMultiHopViaHTTP is a repo-wide, append-only pass that runs AFTER
// the per-file extractor passes have completed.  It needs two inputs:
//
//  1. The full set of classified file paths + content, so it can scan test
//     files for HTTP client call patterns.
//  2. The ROUTES_TO relationships already collected from the DRF router /
//     URL-conf passes, so it can follow endpoint → ViewSet without re-parsing
//     urlconfs.
//
// For each HTTP client call found in a test function body the pass:
//
//  1. Extracts the URL path literal from the call.
//  2. Normalises the path (strips trailing slash, lowercases) and looks it up
//     in the ROUTES_TO index (keyed by normalised path suffix).
//  3. When a matching ROUTES_TO edge is found, reads the ViewSet ToID from
//     that edge as the production target.
//  4. Synthesises a TESTS relationship from the enclosing test function → the
//     ViewSet entity, tagged with Properties["via"]="http_router" for
//     traceability.
//
// The pass is append-only: it never modifies or removes any entity or
// relationship. It returns only the newly synthesised TESTS edges; the caller
// appends them to the pass-3 relationship set.
//
// Refs #2549.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Regex patterns
// ---------------------------------------------------------------------------

// testClientHTTPCallRe matches HTTP test-client calls inside test function
// bodies across the supported Python web frameworks.  Patterns covered:
//
//	Django:    self.client.post('/path', ...) / client.get('/path')
//	FastAPI:   client.get('/path')            # fastapi.testclient.TestClient
//	Starlette: client.get('/path')            # starlette.testclient.TestClient
//	Flask:     client.get('/path')            # app.test_client()
//	Quart:     await client.get('/path')      # async test client
//	Sanic:     app.test_client.get('/path')   # app.test_client.<verb>
//	aiohttp:   await session.get('/path') / async with session.get('/path')
//	httpx:     await ac.get('/path')          # AsyncClient aliased `ac`
//	requests:  requests.get('/path', ...)
//
// The optional `self.` / `app.` / `cls.` prefix is consumed so that both bare
// (`client.get`) and attribute (`self.client.get`, `app.test_client.get`)
// receivers match.  An optional `await` is tolerated by the leading-boundary
// match.  The receiver token must be one of the recognised test-client names
// (client, test_client, session, ac, async_client) or an HTTP library module
// (requests, httpx) — this keeps unrelated `.get(...)` calls such as
// `cache.get('key')` or `logger.get('config')` from producing phantom edges.
//
// Group 1 = HTTP verb (get|post|put|patch|delete|head|options).
// Group 2 = URL literal (single or double quoted).  Absolute URLs
// (http://host/path) are normalised to their path by normaliseHTTPPath.
var testClientHTTPCallRe = regexp.MustCompile(
	`(?:\b(?:self|app|cls)\s*\.\s*)?\b(?:client|test_client|session|ac|async_client|requests|httpx)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["']`,
)

// pyTestFuncRe re-matches test function headers to locate the enclosing test
// function of a call site. Using a lightweight version here rather than
// importing the testmap internal type.
var pyTestFuncForEdgeRe = regexp.MustCompile(
	`(?m)^(?:[ \t]*)(?:async\s+)?def\s+(test_\w+)\s*\(`,
)

// isTestFilePath returns true when the path looks like a Python test file by
// naming convention. Mirrors the pytest frameworkEntry filename hints.
func isTestFilePath(p string) bool {
	// Path-segment check: /tests/ directory (idiomatic Django/Python layout).
	// Prefix with "/" so leading segments like "tests/foo.py" also match "/tests/".
	slashed := "/" + filepath.ToSlash(strings.ToLower(p))
	if strings.Contains(slashed, "/tests/") {
		return true
	}

	base := p
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		base = p[idx+1:]
	}
	lower := strings.ToLower(base)
	return strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py") ||
		strings.HasSuffix(lower, "_test.py")
}

// ---------------------------------------------------------------------------
// Path normalisation
// ---------------------------------------------------------------------------

// normaliseHTTPPath strips a trailing slash, collapses multiple slashes, and
// lower-cases the path so that '/api/v1/Foo/' and '/api/v1/foo' match the same
// ROUTES_TO entry.
func normaliseHTTPPath(raw string) string {
	p := strings.ToLower(raw)
	// aiohttp / httpx tests often issue absolute URLs against a test server
	// (e.g. session.get('http://localhost/api/x') or client.get(f'{base}/api/x')
	// rendered as 'http://testserver/api/x'). Strip the scheme + authority so
	// the remaining path lines up with the ROUTES_TO index, which is keyed by
	// path only.
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			p = rest[slash:]
		} else {
			p = "/"
		}
	}
	// Drop a query string / fragment — routes are matched on path alone.
	if q := strings.IndexAny(p, "?#"); q >= 0 {
		p = p[:q]
	}
	// Collapse repeated slashes.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		p = "/"
	}
	return p
}

// ---------------------------------------------------------------------------
// ROUTES_TO index
// ---------------------------------------------------------------------------

// buildRoutesToIndex indexes the ROUTES_TO relationships by the normalised
// path suffix that is embedded in the FromID.
//
// FromID convention used by the DRF router + URL-conf passes:
//
//	"Route:/api/v1/users"    (from ApplyDjangoNestedURLConf / ApplyDjangoDRFRoutes)
//	"http:POST:/api/v1/foo"  (from http_endpoint_synthesis)
//
// For each, we record normalisedPath → ToID so the caller can do a
// O(1) lookup from a test URL path to the downstream ViewSet/handler entity.
//
// A single path may be served by multiple ViewSets (e.g. list vs detail) but
// in practice the ROUTES_TO index maps one URL prefix per ViewSet class; we
// store all matching ToIDs and emit one TESTS edge per.
func buildRoutesToIndex(rels []types.RelationshipRecord) map[string][]string {
	idx := make(map[string][]string)
	for _, r := range rels {
		if r.Kind != string(types.RelationshipKindRoutesTo) {
			continue
		}
		// Extract path from FromID.
		var rawPath string
		switch {
		case strings.HasPrefix(r.FromID, "Route:"):
			rawPath = r.FromID[len("Route:"):]
		case strings.HasPrefix(r.FromID, "http:"):
			// "http:POST:/api/v1/foo" → extract path after the second colon.
			parts := strings.SplitN(r.FromID, ":", 3)
			if len(parts) == 3 {
				rawPath = parts[2]
			}
		}
		if rawPath == "" {
			continue
		}
		norm := normaliseHTTPPath(rawPath)
		idx[norm] = append(idx[norm], r.ToID)
	}
	return idx
}

// ---------------------------------------------------------------------------
// Main pass
// ---------------------------------------------------------------------------

// ApplyTestsMultiHopViaHTTP synthesises TESTS edges from test functions to
// ViewSet / handler entities by following HTTP client call sites through the
// ROUTES_TO graph index.
//
// Parameters:
//
//	paths      — repo-relative paths of every file in the index.
//	fileReader — returns the raw source bytes for a repo-relative path.
//	routesToRels — the full set of ROUTES_TO relationships already collected
//	               by the URL-conf / DRF passes.  May include other edge kinds;
//	               the function filters by Kind=="ROUTES_TO".
//
// Returns only the newly synthesised TESTS RelationshipRecords. The caller is
// responsible for appending these to its accumulated relationship slice.
func ApplyTestsMultiHopViaHTTP(
	paths []string,
	fileReader NestedURLConfFileReader,
	routesToRels []types.RelationshipRecord,
) []types.RelationshipRecord {
	if fileReader == nil || len(routesToRels) == 0 {
		return nil
	}

	// Build the path → ViewSet(s) lookup from ROUTES_TO edges.
	routeIdx := buildRoutesToIndex(routesToRels)
	if len(routeIdx) == 0 {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{} // deduplicate (testFunc, toID) pairs

	for _, p := range paths {
		if !isTestFilePath(p) {
			continue
		}
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Quick bail-out: file must contain a recognised HTTP test-client
		// receiver token followed by a dotted call.  Covers Django/FastAPI/
		// Flask/Starlette (.client.), Quart, Sanic (.test_client.), aiohttp
		// (session.), httpx AsyncClient (ac. / async_client.), and the
		// requests/httpx libraries.  Cheap substring gate before the regex.
		if !strings.Contains(src, "client.") && !strings.Contains(src, "session.") &&
			!strings.Contains(src, "requests.") && !strings.Contains(src, "httpx.") &&
			!strings.Contains(src, "ac.") {
			continue
		}

		// Find every HTTP client call site in this test file.
		for _, callIdx := range testClientHTTPCallRe.FindAllStringSubmatchIndex(src, -1) {
			if len(callIdx) < 6 {
				continue
			}
			// callIdx[4]:callIdx[5] = URL path literal (group 2).
			rawPath := src[callIdx[4]:callIdx[5]]
			if rawPath == "" {
				continue
			}

			norm := normaliseHTTPPath(rawPath)

			// Try to find a matching ROUTES_TO target via prefix matching.
			// The test URL may include query params or be a detail URL
			// (/api/v1/users/42) while the route is registered for the
			// collection (/api/v1/users).  We attempt exact match first, then
			// walk up the path hierarchy stripping the last segment.
			var viewSetIDs []string
			candidate := norm
			for candidate != "" && candidate != "/" {
				if ids, ok := routeIdx[candidate]; ok {
					viewSetIDs = ids
					break
				}
				// Strip the last path segment.
				idx := strings.LastIndexByte(candidate, '/')
				if idx <= 0 {
					break
				}
				candidate = candidate[:idx]
			}
			if len(viewSetIDs) == 0 {
				continue
			}

			// Determine enclosing test function at call site position.
			callPos := callIdx[0]
			testFunc := enclosingPyTestFunc(src, callPos)
			if testFunc == "" {
				continue
			}

			// Emit one TESTS edge per matching ViewSet.
			for _, toID := range viewSetIDs {
				key := p + "|" + testFunc + "|" + toID
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, types.RelationshipRecord{
					FromID: "SCOPE.Operation:" + testFunc,
					ToID:   toID,
					Kind:   "TESTS",
					Properties: map[string]string{
						"via":           "http_router",
						"http_path":     rawPath,
						"test_file":     p,
						"test_function": testFunc,
						"pattern_type":  "tests_multi_hop_http_router",
						"confidence":    "high",
					},
				})
			}
		}
	}
	return out
}

// enclosingPyTestFunc returns the name of the innermost test_ function whose
// header appears before pos in src.  Returns "" when no enclosing test
// function can be identified.
func enclosingPyTestFunc(src string, pos int) string {
	sub := src[:pos]
	matches := pyTestFuncForEdgeRe.FindAllStringSubmatch(sub, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// ---------------------------------------------------------------------------
// Synthetic ROUTES_TO from http_endpoint entities (#2570)
// ---------------------------------------------------------------------------

// SynthesiseRoutesToFromEndpoints builds synthetic ROUTES_TO RelationshipRecords
// from http_endpoint entity records emitted by ApplyDjangoDRFRoutes and
// ApplyDjangoNestedURLConf.  These entity records carry routing metadata in
// their Properties map but do NOT produce standalone RelationshipRecord entries
// — they live in pass3Records, not pass2Rels.
//
// Pass 2.8 (ApplyTestsMultiHopViaHTTP) needs ROUTES_TO in pass2Rels to build
// its route index.  When the application separates router.register() calls and
// include(router.urls) into different files (upvate pattern: routers.py +
// urls.py), applyDjangoRouteComposition never fires in same-file mode and the
// composed ROUTES_TO edges are never added to pass2Rels — leaving the route
// index empty and producing zero TESTS edges.
//
// This function reconstructs synthetic ROUTES_TO records from two entity
// property conventions:
//
//  1. drf_router_expanded entities: Kind==http_endpoint_synthesis,
//     Properties["path"] = "/api/v1/schedule",
//     Properties["source_handler"] = "SCOPE.Operation:ScheduleViewset.create"
//     → emits http:<VERB>:<path> -ROUTES_TO-> SCOPE.Operation:<ViewSet.method>
//
//  2. urlconf_nested_include entities: Kind==http_endpoint_synthesis,
//     Properties["path"] = "/api/v1/schedule",
//     Properties["source_handler"] = "Controller:schedule_view"  (FBV)
//     → emits http:ANY:<path> -ROUTES_TO-> Controller:<handler>
//
// Only entities where both "path" and "source_handler" are non-empty are
// processed; catch-all (ANY-verb, no single handler) entities are skipped.
//
// The returned records are append-only and intended to be merged into
// pass2Rels before calling ApplyTestsMultiHopViaHTTP.
func SynthesiseRoutesToFromEndpoints(entityRecords []types.EntityRecord) []types.RelationshipRecord {
	const httpEndpointKind = "http_endpoint_synthesis"
	var out []types.RelationshipRecord
	seen := map[string]bool{}
	for _, e := range entityRecords {
		if e.Kind != httpEndpointKind {
			continue
		}
		path := e.Properties["path"]
		handler := e.Properties["source_handler"]
		if path == "" || handler == "" {
			continue
		}
		verb := e.Properties["verb"]
		if verb == "" {
			verb = "ANY"
		}
		fromID := "http:" + strings.ToUpper(verb) + ":" + path
		key := fromID + "|" + handler
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, types.RelationshipRecord{
			FromID: fromID,
			ToID:   handler,
			Kind:   string(types.RelationshipKindRoutesTo),
			Properties: map[string]string{
				"pattern_type": "synthesised_from_endpoint",
				"framework":    "django",
			},
		})
	}
	return out
}
