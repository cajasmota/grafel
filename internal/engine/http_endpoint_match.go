// http_endpoint_match.go — structural path normalization for matching
// http_endpoint_call synthetics to http_endpoint_definition synthetics
// (issue #1615).
//
// Background
// ----------
// The Phase-2 resolve pass (http_endpoint_resolve.go) links a call synthetic to
// a definition synthetic by an EXACT match on the canonical synthetic Name
// (`http:<VERB>:<path>`). On real polyglot codebases that exact match is far too
// strict: a frontend/mobile client calls `/inspections/{id}/create-deficiencies/`
// while the backend mounts the route at `/api/v1/inspections/{pk}/create-deficiencies`.
// The two Names differ on three independent axes —
//
//	(a) the configurable API mount prefix (`/api`, `/api/vN`, `/vN`),
//	(b) the path-parameter token name (`{id}` vs `{pk}` vs `{slug}` …),
//	(c) the trailing slash + letter case,
//
// — so the exact match misses and the call-site is counted as an orphan even
// though it clearly targets the backend route. On the upvate corpus this
// accounted for the bulk of intra-repo HTTP orphans (143 of 144 unresolved
// call synthetics in the backend repo become resolvable once these three axes
// are normalized away).
//
// Strategy
// --------
// normalizeEndpointPath collapses (b) and (c): every path-parameter placeholder
// — `{pk}`, `{id}`, `{userId}`, `:id`, `<int:id>`, … — becomes the uniform
// token `{*}`, the result is lower-cased and the trailing slash is stripped.
// stripEndpointAPIPrefix collapses (a): a leading `/api`, `/api/vN` or bare
// `/vN` segment is removed so a prefixed producer and an unprefixed consumer
// bucket together.
//
// This is intentionally CONSERVATIVE to avoid false matches:
//   - Only the well-known `api` / `vN` first segments are stripped — never an
//     arbitrary first segment, which would collapse genuinely-distinct routes.
//   - The verb must still be compatible (verbsMatchCompat): `ANY` matches any
//     specific verb (Django ViewSets emit ANY), but two DIFFERENT specific
//     verbs never match (DELETE/{*} must not resolve to PATCH/{*}).
//   - The exact-Name match in the resolve pass always runs FIRST; this
//     normalized match is a fallback used only when the exact match misses.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// endpointPathParamRe matches a path-parameter placeholder in any of the three
// notations the various extractors emit:
//   - {pk}, {id}, {userId}            (OpenAPI / Django / Express curly form)
//   - :id, :pk, :userId              (Express / Rails colon form)
//   - <int:id>, <slug>, <uuid:pk>    (Django / Flask angle form)
//
// Each is replaced with the canonical token {*} so matching is on path
// STRUCTURE, not parameter name. Mirrors internal/links/http_pass.go.
var endpointPathParamRe = regexp.MustCompile(`\{[^}]+\}|:[a-zA-Z][a-zA-Z0-9_]*|<[^>]+>`)

// endpointAPIPrefixRe matches a leading API/version mount prefix. Only the
// FIRST segment group is matched and only the well-known `api` / `vN` forms —
// an arbitrary first segment is never stripped. Go's RE2 has no lookahead so
// group 1 captures the boundary `/` (kept out of the strip) or end-of-string.
// Mirrors internal/links/http_pass.go.
var endpointAPIPrefixRe = regexp.MustCompile(`^/(?:api(?:/v\d+)?|v\d+)(/|$)`)

// normalizeEndpointPath canonicalizes every path-parameter placeholder to the
// uniform token {*}, lower-cases the result and strips a trailing slash so that
// route shapes from different extractors compare without caring about parameter
// name, case or trailing-slash convention.
//
//	/users/{pk}                    → /users/{*}
//	/Users/{userId}/               → /users/{*}
//	/users/<int:id>                → /users/{*}
//	/users/{userId}/posts/{postId} → /users/{*}/posts/{*}
func normalizeEndpointPath(path string) string {
	path = endpointPathParamRe.ReplaceAllString(path, "{*}")
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// stripEndpointAPIPrefix removes a leading `/api`, `/api/vN`, or `/vN` segment
// from an already-normalized path. Returns ("", false) when no such prefix is
// present. The returned path always keeps a leading slash.
func stripEndpointAPIPrefix(normalizedPath string) (string, bool) {
	m := endpointAPIPrefixRe.FindStringSubmatchIndex(normalizedPath)
	if m == nil {
		return "", false
	}
	stripped := normalizedPath[m[2]:]
	if stripped == "" {
		stripped = "/"
	}
	return stripped, stripped != normalizedPath
}

// endpointMatchKeys returns the set of normalized keys under which a synthetic
// with the given canonical path should be registered / probed: the normalized
// path, plus its API/version-prefix-stripped form when one is present. Keys are
// deduplicated.
func endpointMatchKeys(path string) []string {
	if path == "" {
		return nil
	}
	nk := normalizeEndpointPath(path)
	keys := []string{nk}
	if stripped, ok := stripEndpointAPIPrefix(nk); ok && stripped != nk {
		keys = append(keys, stripped)
	}
	return keys
}

// resolveCallByPath is the structural fallback used by the Phase-2 resolve pass
// when the exact-Name match for an http_endpoint_call synthetic misses. It
// looks the call's normalized path keys up in definitionByPath and returns the
// first definition (in merged order, for determinism) whose verb is compatible.
//
// Determinism: definitionByPath slices are appended in `merged` iteration
// order, which is canonical by contract of ResolveHTTPEndpointHandlers, so the
// first compatible candidate is stable across runs.
//
// Returns (index, true) on a match, (0, false) otherwise.
func resolveCallByPath(call *types.EntityRecord, merged []types.EntityRecord, definitionByPath map[string][]int) (int, bool) {
	callVerb := propOr(call, "verb", "")
	for _, key := range endpointMatchKeys(propOr(call, "path", "")) {
		for _, idx := range definitionByPath[key] {
			if verbsMatchCompat(callVerb, propOr(&merged[idx], "verb", "")) {
				return idx, true
			}
		}
	}
	return 0, false
}

// verbsMatchCompat reports whether a call verb and a definition verb may refer
// to the same logical endpoint. ANY (case-insensitive) on either side matches
// everything — Django ViewSets wire HTTP methods at the View level and emit
// ANY-verb route synthetics. An empty verb is treated as ANY. Two different
// specific verbs never match.
func verbsMatchCompat(callVerb, defVerb string) bool {
	c := strings.ToUpper(strings.TrimSpace(callVerb))
	d := strings.ToUpper(strings.TrimSpace(defVerb))
	if c == "" || c == "ANY" || d == "" || d == "ANY" {
		return true
	}
	return c == d
}
