// http_endpoint_e2e_testmap.go — link e2e HTTP route tests (supertest /
// MockMvc-style "call a route by string") to the http_endpoint_definition they
// exercise, emitting a TESTS edge from the test_suite to the endpoint
// (issue #4351).
//
// Background
// ----------
// NestJS / Express e2e specs drive the app through HTTP:
//
//	request(app.getHttpServer()).post('/inspections/123/items').send(dto).expect(201)
//
// The Jest extractor (internal/custom/javascript/jest.go) collapses such a spec
// into ONE test_suite entity (#4343) and stamps the verb+route pairs it issues
// onto an `e2e_route_calls` property (one "VERB route" per line). After #4343
// the suite links (at best) to a CLASS subject — but the route-string →
// http_endpoint_definition linkage was missing, so e2e suites never connected
// to the endpoints they cover and those endpoints looked untested.
//
// This pass closes that gap. It runs INSIDE ResolveHTTPEndpointHandlers, after
// the http_endpoint_definition entities have been migrated and indexed
// (definitionByPath), so it reuses the exact same path-normalization the
// call→definition resolver uses and is merge-stable: it operates over the fully
// merged entity table, the same place call/definition linkage resolves, rather
// than a fragile post-hoc name match in an extractor that cannot see the
// controller in another file.
//
// Matching
// --------
// The route in the test (`/inspections/123/items`) carries CONCRETE path
// params; the endpoint definition carries a TEMPLATE
// (`/inspections/:id/items` → synthesized as `/inspections/{id}/items`,
// possibly under a `/api/v1` mount prefix). We match in two tiers:
//
//  1. Structural key match via endpointMatchKeys — handles the common case
//     where the test route is ALREADY in template/wildcard shape (a
//     `${ROUTE}/${id}` that folded to `/api/v1/x/{id}`-ish) and the API-prefix
//     stripping aligns the two sides. Verb must be compatible (verbsMatchCompat).
//
//  2. Segment matcher (matchConcreteRouteToDefinition) — handles the
//     concrete-vs-template case: the test path `/inspections/123/items` is
//     compared SEGMENT-BY-SEGMENT against every same-verb definition template,
//     treating a definition `{param}` / `:param` / `<int:id>` segment as a
//     wildcard that any single concrete test segment satisfies, and requiring
//     literal segments to be equal (case-insensitively). The API/version mount
//     prefix on either side is tolerated.
//
// Conservatism (no fabricated edges)
// ----------------------------------
// An edge is emitted ONLY when verb+route uniquely matches EXACTLY ONE
// definition. If zero or more-than-one definitions match, the route is skipped
// (no TESTS edge, no `untested_route` fabrication). This mirrors the
// no-guessing posture of the #4319 co-location fallback.
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// testSuiteKind is the entity Kind the Jest extractor stamps on the one
// suite-per-spec node (see internal/custom/javascript/jest.go).
const testSuiteKind = "test_suite"

// linkE2ERouteTestsToEndpoints walks every test_suite carrying an
// `e2e_route_calls` property and emits a TESTS edge to each
// http_endpoint_definition uniquely matched by (verb, normalized-route).
// Returns the number of TESTS edges emitted. definitionByPath/defByIndex are
// the indices already built by the caller over the merged set.
func linkE2ERouteTestsToEndpoints(
	merged []types.EntityRecord,
	definitionByPath map[string][]int,
	defIndices []int,
) int {
	emitted := 0
	for i := range merged {
		s := &merged[i]
		if s.Kind != testSuiteKind || s.Properties == nil {
			continue
		}
		raw := s.Properties["e2e_route_calls"]
		if raw == "" {
			continue
		}
		// De-dup target definitions per suite so a suite hitting the same
		// endpoint under several verbs/fixtures yields ONE TESTS edge.
		linked := map[int]bool{}
		for _, line := range strings.Split(raw, "\n") {
			verb, route, ok := splitVerbRoute(line)
			if !ok {
				continue
			}
			defIdx, matched := resolveRouteTestToDefinition(verb, route, merged, definitionByPath, defIndices)
			if !matched || linked[defIdx] {
				continue
			}
			linked[defIdx] = true
			def := &merged[defIdx]
			s.Relationships = append(s.Relationships, types.RelationshipRecord{
				FromID: s.Kind + ":" + s.Name,
				ToID:   def.Kind + ":" + def.Name,
				Kind:   string(types.RelationshipKindTests),
				Properties: map[string]string{
					"framework":    propOr(s, "framework", "jest"),
					"match_source": "e2e_supertest_route",
					"verb":         verb,
					"route":        route,
				},
				Confidence: 0.9,
			})
			emitted++
		}
	}
	return emitted
}

// splitVerbRoute parses a "VERB route" line into its parts. Returns ok=false
// when the line is empty/malformed or the route is not path-shaped.
func splitVerbRoute(line string) (verb, route string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	sp := strings.IndexByte(line, ' ')
	if sp <= 0 {
		return "", "", false
	}
	verb = strings.ToUpper(strings.TrimSpace(line[:sp]))
	route = strings.TrimSpace(line[sp+1:])
	if verb == "" || !strings.HasPrefix(route, "/") {
		return "", "", false
	}
	return verb, route, true
}

// resolveRouteTestToDefinition resolves a (verb, route) test call to a UNIQUE
// http_endpoint_definition index. Tier 1 reuses the structural key index
// (endpointMatchKeys) used by the call→definition resolver. Tier 2 runs the
// concrete-vs-template segment matcher across every definition. An edge is
// returned only on a UNIQUE same-verb match in whichever tier first yields one;
// ambiguous or empty results return matched=false.
func resolveRouteTestToDefinition(
	verb, route string,
	merged []types.EntityRecord,
	definitionByPath map[string][]int,
	defIndices []int,
) (int, bool) {
	// Tier 1 — structural key match (handles already-templated test routes and
	// API-prefix alignment). Collect UNIQUE same-verb candidates.
	tier1 := map[int]bool{}
	for _, key := range endpointMatchKeys(route) {
		for _, idx := range definitionByPath[key] {
			if verbsMatchCompat(verb, propOr(&merged[idx], "verb", "")) {
				tier1[idx] = true
			}
		}
	}
	if len(tier1) == 1 {
		for idx := range tier1 {
			return idx, true
		}
	}
	if len(tier1) > 1 {
		// Ambiguous on the structural key — do not guess.
		return 0, false
	}

	// Tier 2 — concrete-vs-template segment matcher over all definitions.
	tier2 := -1
	count := 0
	for _, idx := range defIndices {
		def := &merged[idx]
		if !verbsMatchCompat(verb, propOr(def, "verb", "")) {
			continue
		}
		if matchConcreteRouteToDefinition(route, propOr(def, "path", "")) {
			tier2 = idx
			count++
			if count > 1 {
				return 0, false // ambiguous — no guessing
			}
		}
	}
	if count == 1 {
		return tier2, true
	}
	return 0, false
}

// matchConcreteRouteToDefinition reports whether a CONCRETE test route (e.g.
// `/api/v1/inspections/123/items`) matches a definition TEMPLATE (e.g.
// `/inspections/{id}/items` or `/inspections/:id/items`). A definition
// param-segment ({x} / :x / <int:x>) is a wildcard satisfied by any single
// concrete test segment; literal segments must match case-insensitively. The
// API/version mount prefix (`/api`, `/api/vN`, `/vN`) is tolerated on EITHER
// side so a prefixed backend route and an unprefixed test route still align.
func matchConcreteRouteToDefinition(testRoute, defPath string) bool {
	if defPath == "" {
		return false
	}
	testSegs := routeSegments(testRoute)
	defSegs := routeSegments(defPath)

	// Try aligning with the API/version prefix present on neither, one, or both
	// sides: strip from each independently and compare every combination. This
	// is cheap (at most 4 comparisons) and conservative — only the well-known
	// prefixes are stripped (stripRouteAPIPrefix).
	testVariants := [][]string{testSegs}
	if stripped, ok := stripRouteAPIPrefix(testSegs); ok {
		testVariants = append(testVariants, stripped)
	}
	defVariants := [][]string{defSegs}
	if stripped, ok := stripRouteAPIPrefix(defSegs); ok {
		defVariants = append(defVariants, stripped)
	}
	for _, ts := range testVariants {
		for _, ds := range defVariants {
			if segmentsMatch(ts, ds) {
				return true
			}
		}
	}
	return false
}

// segmentsMatch compares concrete test segments against definition template
// segments: same length, definition param-segments wildcard a concrete
// segment, literal segments compared case-insensitively.
func segmentsMatch(testSegs, defSegs []string) bool {
	if len(testSegs) != len(defSegs) {
		return false
	}
	for i := range defSegs {
		if isRouteParamSegment(defSegs[i]) {
			continue // wildcard — any single concrete segment satisfies it
		}
		// A test segment that is ITSELF an unresolved template (`${expr}`) can
		// satisfy a literal only if it actually equals it; otherwise treat the
		// definition literal as required. Concrete test param values are common
		// (e.g. "123"), so a literal-vs-literal compare is the norm here.
		if !strings.EqualFold(testSegs[i], defSegs[i]) {
			return false
		}
	}
	return true
}

// routeSegments splits a route path into non-empty segments, dropping any query
// string and trailing slash. `/api/v1/x/{id}` → ["api","v1","x","{id}"].
func routeSegments(p string) []string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isRouteParamSegment reports whether a definition path segment is a
// path-parameter placeholder in any notation the synthesizers emit:
// `{id}` (OpenAPI/Nest/Express curly), `:id` (Express/Rails colon),
// `<int:id>` (Flask/Django angle), or a folded-but-unresolved `${expr}`.
func isRouteParamSegment(seg string) bool {
	if seg == "" {
		return false
	}
	if strings.HasPrefix(seg, "${") && strings.HasSuffix(seg, "}") {
		return true
	}
	if seg[0] == ':' {
		return true
	}
	if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
		return true
	}
	if strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">") {
		return true
	}
	return false
}

// stripRouteAPIPrefix removes a leading `api`, `api/vN`, or `vN` segment group
// from a segment slice. Returns (stripped, true) when a prefix was present.
func stripRouteAPIPrefix(segs []string) ([]string, bool) {
	if len(segs) == 0 {
		return segs, false
	}
	first := strings.ToLower(segs[0])
	if first == "api" {
		if len(segs) >= 2 && isVersionSegment(segs[1]) {
			return segs[2:], true
		}
		return segs[1:], true
	}
	if isVersionSegment(segs[0]) {
		return segs[1:], true
	}
	return segs, false
}

// isVersionSegment reports whether a segment is a `vN` API-version marker.
func isVersionSegment(seg string) bool {
	seg = strings.ToLower(seg)
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for i := 1; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}
