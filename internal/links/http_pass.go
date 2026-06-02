package links

// http_pass.go implements the cross-repo HTTP route ↔ fetch matcher.
//
// Foundation:
//   - #534 Phase 1 emits a synthetic `http_endpoint` entity on the
//     producer side (backend that SERVES the route).
//   - #534 Phase 2 attaches an IMPLEMENTS edge from the handler to its
//     synthetic endpoint.
//   - #533 Phase 1 emits a synthetic `http_endpoint` on the consumer
//     side (frontend / RN / Node client that CALLS the route) and
//     records the call-site as a `source_caller` property on the
//     synthetic.
//
// This pass closes the loop. For every synthetic-entity Name shared by
// ≥ 2 repos in the group, we look for at least one producer-side and
// one consumer-side appearance and emit a cross-repo CALLS link from
// the consumer's caller → the producer's handler. The link carries
//
//	relation   = "calls"
//	method     = "http"
//	channel    = "http"
//	identifier = "http:<VERB>:<canonical-path>"
//
// so dashboards and graph queries can filter for typed-HTTP edges
// independently of the structural import-pass output.
//
// Producer / consumer disambiguation
// ----------------------------------
// Each synthetic carries a `pattern_type` property set during emission
// in #533 / #534:
//
//	"http_endpoint_synthesis"         → producer side
//	"http_endpoint_client_synthesis"  → consumer side
//
// When the property is missing or ambiguous, we fall back to the
// attached edges:
//
//	IMPLEMENTS edge from handler → endpoint  → producer
//	CALLS edge from caller → endpoint        → consumer
//
// When neither signal is present we treat the synthetic as
// producer-side by default — this matches the framework prior (most
// http_endpoint synthetics in practice come from the producer-side
// scan).
//
// Verb wildcarding
// ----------------
// Django emits route synthetics with `verb=ANY` because its URL
// configuration wires HTTP methods at the View / ViewSet level rather
// than the URL level. An `ANY`-verb endpoint MUST be matched against
// any specific verb (`GET`, `POST`, …) from the opposite side; we
// canonicalise pairs by collapsing `ANY` to the partner's verb when
// emitting the identifier.
//
// Idempotency
// -----------
// The pass is method-segregated (MethodHTTP). Re-running it replaces
// every entry whose method is "http" while leaving links from
// import_pass, label_pass, and string_pass intact.

import (
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// pathParamRe matches any path-parameter placeholder regardless of origin:
//   - {pk}, {id}, {param}, {userId}, {branchId}, etc.  (curly-brace style)
//   - :id, :pk, :userId, etc.                           (Express/Rails colon style)
//   - <int:id>, <slug>, <uuid:pk>, etc.                 (Django/Flask angle style)
//
// All of these are replaced with the canonical token {*} for byPath index
// lookup only — the original canonicalPath is preserved on the hit object.
//
// Producer synthetics are normally canonicalised to {name} form before they
// reach this pass, but consumer-side synthetics (and a few producer paths that
// skip canonicalisation) can still arrive with raw `:id` / `<int:id>` shapes;
// matching all three styles here makes the byPath index resilient regardless
// of which synthesizer emitted the hit.
var pathParamRe = regexp.MustCompile(`\{[^}]+\}|:[a-zA-Z][a-zA-Z0-9_]*|<[^>]+>`)

// apiPrefixRe matches a leading API/version prefix on a path so the byPath
// index can register a prefix-stripped alias. This complements the
// url_prefix-driven strip (#819): many producers (urlconf_nested_include,
// hand-written routers) and consumers carry an `/api`, `/api/v1`, or bare
// `/v2` prefix WITHOUT a populated url_prefix property, so a property-free
// generic strip is needed to bucket `/api/v1/inspections/{*}` together with a
// consumer's `/inspections/{*}`.
//
// Only the FIRST segment group is stripped and only the well-known `api` /
// version forms — we never strip an arbitrary first segment, which would
// collapse genuinely-distinct routes. Go's RE2 has no lookahead, so the
// pattern requires either a following `/` (kept out of the match via the
// trailing optional segment) or end-of-string; stripAPIPrefix anchors on the
// match end and re-adds the leading slash.
var apiPrefixRe = regexp.MustCompile(`^/(?:api(?:/v\d+)?|v\d+)(/|$)`)

// normalizePathForIndex canonicalizes all path-parameter placeholders to
// the uniform token {*}, lower-cases the result, and strips a trailing slash
// so that route shapes from different extractors can be compared without
// caring about parameter names, case, or trailing-slash convention.
//
// Examples:
//
//	/users/{pk}             → /users/{*}
//	/users/:id              → /users/{*}
//	/users/<int:id>         → /users/{*}
//	/Users/{userId}/        → /users/{*}
//	/users/{userId}/posts/{postId} → /users/{*}/posts/{*}
//	/api/v1/static          → /api/v1/static  (prefix handled separately)
func normalizePathForIndex(path string) string {
	path = pathParamRe.ReplaceAllString(path, "{*}")
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// stripAPIPrefix removes a leading `/api`, `/api/vN`, or `/vN` segment from an
// already-index-normalized path. Returns ("", false) when no such prefix is
// present so callers can avoid registering a duplicate alias. The returned
// path always keeps a leading slash.
func stripAPIPrefix(normalizedPath string) (string, bool) {
	m := apiPrefixRe.FindStringSubmatchIndex(normalizedPath)
	if m == nil {
		return "", false
	}
	// Group 1 (m[2]:m[3]) captured the boundary `/` or "". Strip everything up
	// to the start of group 1 so a trailing `/` boundary is preserved as the
	// new leading slash.
	stripped := normalizedPath[m[2]:]
	if stripped == "" {
		stripped = "/"
	}
	return stripped, stripped != normalizedPath
}

// urlParamNormRe matches all common path-parameter placeholder styles and
// replaces them with the sentinel <PARAM> for confidence-boosted URL-pattern
// normalization (#2588). Unlike pathParamRe (which collapses to {*} for byPath
// index lookup), this sentinel is used exclusively in normalizeURLPattern to
// decide whether two paths are structurally identical across param-syntax styles.
//
// Styles recognised:
//   - {name}, {pk}, {userId}          — curly-brace (OpenAPI / DRF router)
//   - <name>, <int:pk>, <slug:name>   — angle-bracket (Django URL conf)
//   - :name, :pk                      — colon prefix (Express / Rails)
var urlParamNormRe = regexp.MustCompile(`\{[^}]+\}|<[^>]+>|:[a-zA-Z][a-zA-Z0-9_]*`)

// normalizeURLPattern returns a canonical form of a URL path suitable for
// confidence-boosted cross-repo matching (#2588). It:
//  1. Strips a query-string suffix (everything from the first `?`).
//  2. Lowercases the path (HTTP paths are case-insensitive in practice).
//  3. Replaces all path-parameter placeholders — {name}, <name>, <name:type>,
//     :name — with the uniform sentinel <PARAM>.
//  4. Strips a trailing slash (unless the path is just "/").
//
// This is intentionally a higher-level transform than normalizePathForIndex
// (which uses {*} and is part of the byPath index key). normalizeURLPattern
// is only used by applyURLPatternNorm to decide whether a candidate link
// deserves a confidence boost, and is exported for unit-testing.
func normalizeURLPattern(path string) string {
	// 1. Strip query string.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	// 2. Lowercase.
	path = strings.ToLower(path)
	// 3. Unify param placeholders to <PARAM>.
	path = urlParamNormRe.ReplaceAllString(path, "<PARAM>")
	// 4. Strip trailing slash (preserve bare "/").
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// applyURLPatternNorm checks whether the consumer and producer canonical paths
// match after normalizeURLPattern is applied. If they do, it returns a boosted
// confidence value (urlPatternNormConfidence) and the annotation key
// "url_pattern"; otherwise it returns 0 and "".
//
// Callers MUST only invoke this when the standard byPath lookup missed
// (p == nil before the prefix-injection retry) so the boost is applied
// exclusively to pairs that are structurally equivalent but syntactically
// different.
func applyURLPatternNorm(consumerPath, producerPath string) (confidence float64, annotation string) {
	if consumerPath == "" || producerPath == "" {
		return 0, ""
	}
	normC := normalizeURLPattern(consumerPath)
	normP := normalizeURLPattern(producerPath)
	if normC == normP {
		return urlPatternNormConfidence, "url_pattern"
	}
	return 0, ""
}

// urlPatternNormConfidence is the boosted confidence score applied when two
// HTTP endpoint paths differ only in their path-parameter syntax (e.g.
// /users/{id} vs /users/<pk:int>) and normalizeURLPattern makes them identical.
// Placed in the P1 band (structural, high-signal) because a param-syntax-only
// difference is an implementation detail, not an architectural ambiguity.
const urlPatternNormConfidence = 0.95

// crossRepoPrefixCandidates is the ordered list of well-known API mount
// prefixes tried when a consumer path has no prefix of its own and the
// standard byPath probing misses (#2569). This mirrors prefixCandidates in
// internal/engine/http_endpoint_match.go (PR #2557) and is applied at the
// cross-repo linking level so that a frontend consumer emitting a raw path
// such as `/searchBuildings` can be matched to a backend producer mounted
// at `/api/v1/searchBuildings`.
//
// Order matters: more-specific (longer) prefixes are tried first so
// `/api/v1` is preferred over `/api` when both would match.
var crossRepoPrefixCandidates = []string{"/api/v1", "/api/v2", "/api", "/v1"}

// caseNormalizePathSegments produces a canonical form of a path for the
// case_style_normalized cross-repo matching strategy (#2703, broadened in
// #3169). Each segment is reduced to a case-style-agnostic canonical id via
// canonicalCaseSegment (lower-case + split on `_`/`-`/case-boundaries, joined);
// path-parameter placeholders (already collapsed to `{*}` by
// normalizePathForIndex) are passed through unchanged.
//
// Examples:
//
//	/api/v1/contracts/{*}/assigned_contacts  → /api/v1/contracts/{*}/assignedcontacts
//	/api/v1/contracts/{*}/assigned-contacts  → /api/v1/contracts/{*}/assignedcontacts
//	/api/v1/contracts/{*}/assignedContacts   → /api/v1/contracts/{*}/assignedcontacts
//	/api/v1/contracts/{*}/AssignedContacts   → /api/v1/contracts/{*}/assignedcontacts
//
// Critically, the segment structure is PRESERVED: this normalization MUST
// NOT cause `/searchBuildings` (1 segment) to match `/buildings/search`
// (2 segments). That is path-reordering, which is explicitly out of scope
// for #2703.
//
// The input is expected to already be normalizePathForIndex-canonicalised
// (placeholders collapsed to `{*}`, lower-cased). The function still applies
// lower-casing and strips `-`/`_` defensively so it can be safely called on
// any path; the resulting key is suitable for byCaseNorm bucket lookup.
func caseNormalizePathSegments(path string) string {
	if path == "" {
		return ""
	}
	// Split on `/` so segment count is preserved (empty leading segment for
	// the leading slash is intentional and contributes to structural identity).
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		if seg == "" || seg == "{*}" {
			continue
		}
		segs[i] = canonicalCaseSegment(seg)
	}
	return strings.Join(segs, "/")
}

// canonicalCaseSegment reduces a single path segment to a case-style-agnostic
// canonical id by (a) splitting on explicit separators (`_`, `-`) AND implicit
// case boundaries (lower→upper, letter→digit, digit→letter, and the
// ACRONYM→Word boundary inside runs like `HTTPServer`), and (b) lower-casing
// and concatenating the resulting word list. The split-then-join is what makes
// the form robust beyond a naive strip-separators pass: it guarantees that
// `submitElv3` (camelCase, no separator) and `submit_elv3` (snake_case) and
// `submit-elv3` (kebab-case) all reduce to the SAME canonical id `submitelv3`,
// regardless of how the digit/letter boundary is spelled on either side.
//
// Examples (single segment):
//
//	submitElv3   → submitelv3   (split: submit|Elv|3   → submit elv 3)
//	submit_elv3  → submitelv3   (split: submit|elv3    → submit elv3)
//	inspectionTypes → inspectiontypes
//	inspection_types → inspectiontypes
//	assigned-contacts → assignedcontacts
//	HTTPServer   → httpserver   (split: HTTP|Server)
//
// Because the output is the concatenation of the lower-cased words, two
// segments are equal IFF they decompose to the same ordered run of
// alphanumeric characters. This is intentionally identity-preserving: it does
// NOT merge segments (`assigned_devices` stays one segment) and it does NOT
// drop characters, so two semantically-distinct names such as
// `assigned_devices` and `assigned_and_available_devices` canonicalize to
// DIFFERENT ids and will never be cross-linked by this strategy.
func canonicalCaseSegment(seg string) string {
	var b strings.Builder
	b.Grow(len(seg))
	for _, r := range seg {
		// Explicit separators (`_`, `-`) are word boundaries that contribute no
		// character to the canonical id. Case boundaries (lower→upper, etc.)
		// need no explicit handling: lower-casing every retained rune and
		// concatenating yields the same canonical id as a split-on-boundary +
		// lower + join would, while preserving the full alphanumeric run so
		// distinct names never collapse together.
		if r == '-' || r == '_' {
			continue
		}
		b.WriteRune(toLowerRune(r))
	}
	return b.String()
}

// toLowerRune lower-cases ASCII without an allocation; non-ASCII passes through
// unchanged (path segments in practice are ASCII identifiers).
func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// rawParamRe captures the *name* inside any path-parameter placeholder so the
// param-normalization detector (#2808) can compare placeholder names across a
// consumer/producer pair. It mirrors the three styles recognised by
// urlParamNormRe but yields the bare identifier rather than a sentinel:
//   - {clientId}, {pk}, {id}        → clientId / pk / id
//   - <int:pk>, <slug:name>, <id>   → pk / name / id   (type prefix dropped)
//   - :clientId, :pk                → clientId / pk
//
// The captured name is lower-cased by the caller before comparison so
// `{clientId}` and `{ClientID}` are treated as the same param.
var rawParamRe = regexp.MustCompile(`\{([^}:]+)(?::[^}]+)?\}|<(?:[^>:]+:)?([^>]+)>|:([a-zA-Z][a-zA-Z0-9_]*)`)

// extractParamNames returns the ordered, lower-cased list of path-parameter
// placeholder names in a path, ignoring static segments. Used by
// paramOnlyMismatch (#2808) to decide whether two structurally-identical paths
// differ purely in their param names (e.g. {clientId} vs {pk}).
func extractParamNames(path string) []string {
	var out []string
	for _, m := range rawParamRe.FindAllStringSubmatch(path, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name == "" {
			name = m[3]
		}
		name = strings.TrimSpace(strings.ToLower(name))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// paramOnlyMismatch reports whether the consumer and producer paths resolve to
// the SAME structural shape (identical when every param is collapsed to {*})
// but carry at least one path-parameter whose NAME differs between the two
// sides — e.g. a frontend call to `/clients/{clientId}` matching a DRF
// endpoint defined as `/api/v1/clients/{pk}` (#2808).
//
// This is the signal that the resolution crossed a param-name boundary the
// byPath {*}-collapse silently bridged, so the emitted link can be stamped
// with the traceable `param_normalized` strategy instead of disappearing into
// the generic `exact` bucket. The producer path is API/version-prefix-stripped
// before comparison so a `/api/v1` producer and a bare consumer still line up.
//
// Over-match guard: the two paths MUST be byte-identical once params are
// collapsed to {*}. Paths that differ by a STATIC segment (e.g.
// `/clients/{pk}` vs `/users/{pk}`) return false — only genuine param-name
// differences are recognised. A pair whose every param name already matches
// (e.g. `{pk}` ↔ `{pk}`) also returns false because no normalization was
// needed.
//
// lookupKwarg, when non-empty, is the producer ViewSet's overridden
// lookup_url_kwarg / lookup_field. When the producer explicitly names its
// detail param (e.g. `building_id` instead of the default `pk`) AND the
// consumer's single param name does NOT match it, we still treat the match as
// param_normalized — but only because the structural {*}-shapes are identical;
// the kwarg is recorded for traceability and to avoid silently merging a
// genuinely-different second param. See the inline caller for the guard.
func paramOnlyMismatch(consumerPath, producerPath string) bool {
	if consumerPath == "" || producerPath == "" {
		return false
	}
	cNorm := normalizePathForIndex(consumerPath)
	pNorm := normalizePathForIndex(producerPath)
	// Strip a leading API/version prefix from the producer so a `/api/v1`
	// producer compares equal to a bare consumer path.
	if stripped, ok := stripAPIPrefix(pNorm); ok {
		pNorm = stripped
	}
	if stripped, ok := stripAPIPrefix(cNorm); ok {
		cNorm = stripped
	}
	// Over-match guard: structural shapes (params collapsed to {*}) must be
	// identical. A static-segment difference disqualifies the pair.
	if cNorm != pNorm {
		return false
	}
	cParams := extractParamNames(consumerPath)
	pParams := extractParamNames(producerPath)
	// Same param count is implied by identical {*}-collapsed shapes, but guard
	// defensively in case the two extractors disagree on a placeholder style.
	if len(cParams) != len(pParams) || len(cParams) == 0 {
		return false
	}
	// At least one positional param name must differ for this to count as a
	// param-name normalization (otherwise it is a plain exact match).
	differs := false
	for i := range cParams {
		if cParams[i] != pParams[i] {
			differs = true
			break
		}
	}
	return differs
}

// literalFillsParamSlot reports whether the consumer path can be matched to the
// producer path by treating a CONCRETE caller segment as the value occupying a
// producer path-parameter slot (#2808 "literal-fills-param"). This is distinct
// from paramOnlyMismatch: there the consumer ALSO has a param (just a
// differently-named one); here the consumer has no param at all in the slot —
// it sends a literal segment where the backend route declares a placeholder.
//
// Live example: a core-mobile call to `GET /recents/buildings` must match the
// DRF detail route `GET /api/v1/recents/{pk}` (recent_viewset.py). The caller
// segment `buildings` is a literal that fills the `{pk}` slot once the API
// prefix is stripped.
//
// Matching rules (after API/version-prefix strip on both sides):
//   - identical segment count;
//   - every position where the PRODUCER is a non-param literal must equal the
//     consumer's literal at that position (case-insensitive);
//   - at least one position where the producer is a param ({*}) must be filled
//     by a consumer LITERAL (not itself a param) — that is the "fill";
//   - any producer-param position the consumer also leaves as a param ({*}) is
//     allowed (it is just an ordinary param match, handled elsewhere) but does
//     not count toward the required fill.
//
// Over-match guards:
//   - returns false when the {*}-collapsed shapes are already identical AND the
//     consumer carries a param in every producer-param slot — that is a plain
//     exact / param_normalized match, not a literal fill, so we must not steal
//     attribution from those strategies;
//   - returns false on any static-literal divergence, so `/recents/buildings`
//     cannot match `/clients/{pk}`.
//
// The caller is responsible for the "prefer an exact static endpoint over a
// param-fill" guard: this retry only runs after the byPath / mount-prefix /
// case-normalize / url-pattern stages have all missed, so a concrete
// `/recents/buildings` producer (if one existed) would already have won.
func literalFillsParamSlot(consumerPath, producerPath string) bool {
	if consumerPath == "" || producerPath == "" {
		return false
	}
	cNorm := normalizePathForIndex(consumerPath)
	pNorm := normalizePathForIndex(producerPath)
	if stripped, ok := stripAPIPrefix(pNorm); ok {
		pNorm = stripped
	}
	if stripped, ok := stripAPIPrefix(cNorm); ok {
		cNorm = stripped
	}
	cSegs := strings.Split(cNorm, "/")
	pSegs := strings.Split(pNorm, "/")
	if len(cSegs) != len(pSegs) || len(cSegs) == 0 {
		return false
	}
	filled := false
	for i := range pSegs {
		pSeg := pSegs[i]
		cSeg := cSegs[i]
		if pSeg == "{*}" {
			// Producer param slot. A consumer literal fills it; a consumer
			// param ({*}) is an ordinary param match (does not count as a fill).
			if cSeg == "{*}" {
				continue
			}
			if cSeg == "" {
				// Empty consumer segment cannot fill a param slot.
				return false
			}
			filled = true
			continue
		}
		// Producer literal: the consumer literal MUST match exactly. (Both are
		// already lower-cased by normalizePathForIndex.)
		if cSeg != pSeg {
			return false
		}
	}
	return filled
}

// pathNormPrefixSegments is the configurable set of leading version/api prefix
// forms the path_normalized strategy (#3752, roadmap oracle-priority #9) strips
// before comparing a client path to a server path. Each entry is the normalized
// (lower-cased, leading-slash) prefix that pathNormalizeForMatch peels off when
// it is the leading segment group.
//
// This is intentionally identical in spirit to apiPrefixRe / stripAPIPrefix but
// kept as an explicit, ordered, configurable list so the path_normalized
// strategy's prefix vocabulary is self-documenting and tunable independently of
// the byPath generic-strip alias. Longer (more specific) prefixes come first so
// `/api/v1` is preferred over `/api`.
var pathNormPrefixSegments = []string{"/api/v1", "/api/v2", "/api", "/v1", "/v2"}

// pathNormalizeForMatch produces the canonical comparison key used by the
// path_normalized cross-repo strategy (#3752). It is deliberately a SEPARATE,
// stricter transform from normalizePathForIndex / normalizeURLPattern so the
// strategy's matching contract is explicit and testable in isolation:
//
//  1. Lower-case the whole path (HTTP paths are case-insensitive in practice).
//  2. Replace every path-parameter placeholder ({id}, :id, {pk}, <int:id>, …)
//     with the single uniform token `{}` — param NAMES are irrelevant.
//  3. Strip ONE leading version/api prefix segment group from pathNormPrefixSegments,
//     but NEVER when stripping would empty the path (guard #3): a bare `/api`
//     keeps its single segment so it can still match another `/api`.
//  4. Strip a trailing slash (unless the path is just "/").
//
// The returned key, together with the verb and the segment count, is the full
// identity under this strategy. Two paths are considered equivalent by the
// caller IFF their keys are equal AND they have the same number of path
// segments — pathNormSegmentCount supplies the latter so `/users/{}` can never
// be conflated with `/users/{}/orders` even though neither has a trailing slash.
func pathNormalizeForMatch(path string) string {
	if path == "" {
		return ""
	}
	// 1. Lower-case.
	path = strings.ToLower(path)
	// 2. Collapse every param placeholder to `{}`. urlParamNormRe already
	//    recognises {name}, <type:name>, and :name styles.
	path = urlParamNormRe.ReplaceAllString(path, "{}")
	// 3. Strip ONE leading version/api prefix segment group, longest first.
	for _, pfx := range pathNormPrefixSegments {
		if path == pfx {
			// Guard #3: stripping would empty the path. Leave it intact so a
			// bare `/api` can still match another bare `/api`.
			break
		}
		if strings.HasPrefix(path, pfx+"/") {
			path = path[len(pfx):]
			break
		}
	}
	// 4. Strip trailing slash (preserve bare "/").
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// pathNormSegmentCount returns the number of path segments in the
// already-prefix-agnostic normalized key produced by pathNormalizeForMatch.
// Segment count is part of the path_normalized strategy's identity: the strategy
// links two endpoints only when their normalized keys are equal AND their
// segment counts match, so `/users/{}` (2 segs after the leading slash split,
// counted here as the non-empty segments) never matches `/users/{}/orders`.
//
// It counts non-empty segments after splitting on `/`, so the leading empty
// segment from the root slash is ignored and the bare root "/" reports 0.
func pathNormSegmentCount(normalizedKey string) int {
	n := 0
	for _, seg := range strings.Split(normalizedKey, "/") {
		if seg != "" {
			n++
		}
	}
	return n
}

// pathNormResolve reports whether the consumer and producer paths match under
// the path_normalized strategy (#3752): equal normalized keys AND equal segment
// counts. Verb compatibility is checked by the caller (and is required to be an
// exact same-verb match per the strategy contract). Returns the shared
// normalized key on success for telemetry / traceability.
func pathNormResolve(consumerPath, producerPath string) (key string, ok bool) {
	if consumerPath == "" || producerPath == "" {
		return "", false
	}
	cKey := pathNormalizeForMatch(consumerPath)
	pKey := pathNormalizeForMatch(producerPath)
	if cKey == "" || pKey == "" {
		return "", false
	}
	if cKey != pKey {
		return "", false
	}
	if pathNormSegmentCount(cKey) != pathNormSegmentCount(pKey) {
		return "", false
	}
	return cKey, true
}

// distinctEndpointCount reports how many DISTINCT server endpoints a candidate
// set represents for the path_normalized ambiguity guard (#3752). Two producer
// hits are the "same endpoint" when they share the same (upper-cased verb,
// raw-canonical-path) pair — e.g. the same route emitted twice across re-index
// or via a router alias. They are DISTINCT when their raw canonical paths differ
// (even if both happen to collapse to the same path_normalized key, which is
// exactly the ambiguous case `/api/v1/users/{id}` vs `/users/{id}` both
// normalizing to a client `/users/{}`): linking either would be a guess, so the
// caller suppresses the link when this count exceeds 1.
//
// The raw canonical path (NOT the normalized key) is the identity axis on
// purpose: it is precisely the divergence the normalization erased, so counting
// on it is what makes the prefix-collision case register as ambiguous.
func distinctEndpointCount(cands []*httpEndpointHit) int {
	seen := map[string]bool{}
	for _, p := range cands {
		path := p.canonicalPath
		if path == "" {
			if _, pp, ok := parseHTTPName(p.name); ok {
				path = pp
			}
		}
		key := strings.ToUpper(p.verb) + " " + path
		seen[key] = true
	}
	return len(seen)
}

// dynamicSuffixMinStaticSegments is the floor on how many concrete (non-param)
// path segments a dynamic-baseurl suffix MUST carry before the dynamic-suffix
// matcher (#2813) will consider auto-linking it. Two static segments
// (e.g. `/schedule/confirm`) is the minimum specificity that makes a unique
// producer match defensible; a one-segment suffix like `/list` is too generic
// and is always demoted to a residual instead.
const dynamicSuffixMinStaticSegments = 2

// dynamicSuffixTemplate normalizes a dynamic-baseurl consumer path into the
// static SUFFIX template the #2813 matcher fuzzy-matches against the backend
// endpoint set. The transform is:
//
//  1. Strip exactly ONE leading dynamic-prefix segment — the first path
//     segment, when it is a `{placeholder}` (after normalizePathForIndex
//     collapses every placeholder, including the `${apiUrl}` template-literal
//     form, to `{*}`). This peels off the base-of-URL variable (`${apiUrl}`,
//     `${baseURL}`, …) and nothing more.
//  2. The remaining path is the suffix template, keyed for index lookup via
//     normalizePathForIndex (params already `{*}`, lower-cased).
//
// It returns the normalized suffix key, the count of leading STATIC (non-param)
// segments the suffix carries, and ok.
//
// ok is false — meaning the call is genuinely-runtime and MUST stay tagged
// data-flow-runtime rather than suffix-matched — when:
//   - the path does not lead with a dynamic placeholder (the ordinary
//     byPath/prefix stages own it); or
//   - after stripping the SINGLE base-URL prefix the next segment is STILL a
//     param. That is the `/{companyType}/{companyId}/branches/...` shape: the
//     base is a render-time choice (companyType) followed by another runtime
//     id, so there is no clean static anchor. We deliberately strip only one
//     segment so a second leading param is recognised as runtime rather than
//     greedily peeled away.
//
// Examples (input → suffixKey, leadingStatic, ok):
//
//	/{apiUrl}/schedule/import                 → /schedule/import, 2, true
//	/{apiUrl}/schedule/confirm/{token}        → /schedule/confirm/{*}, 2, true
//	/{apiUrl}/list                            → /list, 1, true (gate demotes it)
//	/{companyType}/{companyId}/branches/{id}  → "", 0, false (param-led suffix)
//	/{param}/{companyId}/activity             → "", 0, false (param-led suffix)
//	/schedule/import                          → "", 0, false (no dynamic prefix)
func dynamicSuffixTemplate(consumerPath string) (suffixKey string, leadingStatic int, ok bool) {
	if consumerPath == "" {
		return "", 0, false
	}
	norm := normalizePathForIndex(consumerPath)
	segs := strings.Split(norm, "/")
	// segs[0] is the empty leading-slash segment. The path must lead with a
	// single dynamic placeholder occupying the base-of-URL position.
	if len(segs) < 2 || !isDynamicPrefixSegment(segs[1]) {
		return "", 0, false
	}
	// Strip exactly ONE leading dynamic prefix segment.
	rest := segs[2:]
	if len(rest) == 0 {
		return "", 0, false
	}
	// Genuinely-runtime guard: a suffix that is STILL param-led after peeling
	// the single base-URL prefix has no static anchor (companyType/{companyId}/…).
	if rest[0] == "{*}" {
		return "", 0, false
	}
	// Count leading static (non-param) segments for the specificity score.
	for _, s := range rest {
		if s == "{*}" {
			break
		}
		leadingStatic++
	}
	suffixKey = normalizePathForIndex("/" + strings.Join(rest, "/"))
	return suffixKey, leadingStatic, true
}

// isDynamicPrefixSegment reports whether a normalized path segment is a
// dynamic placeholder occupying the base-of-URL position. After
// normalizePathForIndex a `{apiUrl}` / `<id>` / `:id` placeholder is `{*}`; a
// raw `${apiUrl}` template literal that the synthesizer did not fully collapse
// shows up as `${*}` (the leading `$` survives the `{…}` → `{*}` rewrite), so
// both forms are recognised here.
func isDynamicPrefixSegment(seg string) bool {
	return seg == "{*}" || seg == "${*}"
}

// patternTypeURLMountPoint is the synthesis marker set by
// internal/engine/django_urlconf_nested.go (#2677) on the synthetic emitted
// for every `path("prefix", include(...))` declaration. The consumer-side
// mount-prefix retry (#2702) harvests every such entity from the producer
// repos and uses its `url_prefix` as a retry candidate, so the resolution
// stays grounded in prefixes actually declared by the producer instead of
// a global hardcoded list.
const patternTypeURLMountPoint = "url_mount_point"

// staticMountPrefixFallbacks is the conservative set of well-known API
// mount prefixes tried by the consumer-side mount-prefix retry (#2702) when
// the producer repo has no url_mount_point entities of its own. Kept tiny
// on purpose — the discovered prefixes are the primary signal, this list is
// only here so a producer that hasn't yet been re-indexed by #2677 (or one
// whose include() declaration the extractor cannot see) still benefits.
var staticMountPrefixFallbacks = []string{"/api/", "/api/v1/", "/api/v2/"}

// MethodHTTP identifies this pass's cross-repo HTTP link emissions in links.json.
const MethodHTTP = "http"

// MethodHTTPSelf identifies intra-repo HTTP self-call link emissions in
// links.json (#2585). These are consumer→producer pairs where the caller and
// the endpoint definition live in the SAME repo (e.g. a Django task that
// calls its own API via requests.get). They are stored under a distinct
// method so they can be queried separately and are not confused with
// cross-repo CALLS links.
const MethodHTTPSelf = "http_self"

// httpChannel is the channel string used on every emitted link.
const httpChannel = "http"

// patternTypeProducer / patternTypeConsumer match the values set by
// the synthesis pass in internal/engine/http_endpoint_synthesis.go.
const (
	patternTypeProducer = "http_endpoint_synthesis"
	patternTypeConsumer = "http_endpoint_client_synthesis"
)

// httpSide labels a synthetic as producer or consumer.
type httpSide int

const (
	sideUnknown httpSide = iota
	sideProducer
	sideConsumer
)

// httpEndpointHit collects everything we need to emit a cross-repo
// link for one synthetic-entity appearance in one repo.
type httpEndpointHit struct {
	repo string
	// stampedID is the synthetic entity's on-disk ID (sha-hashed by
	// the indexer; differs across repos for the same logical endpoint).
	stampedID string
	// name is the canonical `http:<VERB>:<path>` string.
	name string
	// verb is taken from the synthetic's `verb` property (or parsed
	// from name as a fallback).
	verb string
	// canonicalPath is `path` from the synthetic's properties.
	canonicalPath string
	// urlPrefix is the parent include()-prefix stored on DRF router-expanded
	// entities (e.g. "/api/v1"). Used by the byPath index to also register
	// the prefix-stripped path so that consumers that call without the prefix
	// can still match. See #819.
	urlPrefix string
	// side is producer / consumer / unknown.
	side httpSide
	// handlerID is the entity ID of the producer-side handler resolved
	// via the IMPLEMENTS edge (handler → endpoint). Empty if no edge.
	handlerID string
	// callerID is the entity ID of the consumer-side caller resolved
	// via a CALLS edge OR via the `source_caller` property fallback.
	// Empty if not resolvable.
	callerID string
	// sourceFile is the synthetic's source file (used for SourceLocations).
	sourceFile string
	// framework comes from the synthetic's `framework` property.
	framework string
	// lookupKwarg is the producer ViewSet's overridden detail-route param name,
	// read from the `lookup_url_kwarg` property (preferred) or `lookup_field`
	// fallback (#2808). Empty when the ViewSet uses the DRF default (`pk`). Used
	// to annotate param_normalized links so a genuine lookup override stays
	// traceable.
	lookupKwarg string
}

// runHTTPPass implements the cross-repo HTTP route ↔ fetch matcher.
// See the file header for the contract.
func runHTTPPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "http"}

	if len(graphs) < 2 {
		// Method-segregated overwrite still runs so a previous group of
		// ≥ 2 repos that shrunk to 1 cleans up its prior HTTP and http_self entries.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodHTTP, MethodHTTPSelf), nil, rejects)
		return res, err
	}

	// Pre-compute the per-repo IMPLEMENTS / CALLS edges that target each
	// http_endpoint synthetic. We key by the local stampedID so we can
	// look the edges up while iterating entities.
	type endpointEdges struct {
		implementsFrom []string // handler entity IDs (producer side)
		callsFrom      []string // caller entity IDs (consumer side)
	}
	// repo → endpointEntityID → endpointEdges
	edgesByEndpoint := map[string]map[string]*endpointEdges{}
	for _, g := range graphs {
		m := map[string]*endpointEdges{}
		edgesByEndpoint[g.Repo] = m
		for _, e := range g.Edges {
			switch strings.ToUpper(e.Kind) {
			case "IMPLEMENTS":
				ee := m[e.ToID]
				if ee == nil {
					ee = &endpointEdges{}
					m[e.ToID] = ee
				}
				ee.implementsFrom = append(ee.implementsFrom, e.FromID)
			case "CALLS":
				ee := m[e.ToID]
				if ee == nil {
					ee = &endpointEdges{}
					m[e.ToID] = ee
				}
				ee.callsFrom = append(ee.callsFrom, e.FromID)
			}
		}
	}

	// Index: synthetic name → repo → []hit.
	hits := map[string]map[string][]*httpEndpointHit{}
	// #2702 — discovered URL mount prefixes per producer repo. Populated from
	// every entity carrying pattern_type=url_mount_point (emitted by
	// internal/engine/django_urlconf_nested.go since #2677). Drives the
	// consumer-side mount-prefix retry below.
	mountPrefixesByRepo := map[string]map[string]bool{}
	for _, g := range graphs {
		edgesForRepo := edgesByEndpoint[g.Repo]
		// Index entities-by-(kind,name,file) so source_caller refs can
		// be resolved to stamped entity IDs in the same file.
		type entKey struct{ kind, name, file string }
		entIDByKey := map[entKey]string{}
		for _, e := range g.Entities {
			// #1217: exclude all three http endpoint kind variants from the
			// entID index so synthetics don't resolve against each other.
			if isHTTPEndpointLink(e.Kind) {
				continue
			}
			k := entKey{e.Kind, e.Name, e.SourceFile}
			if _, ok := entIDByKey[k]; !ok {
				entIDByKey[k] = e.ID
			}
		}
		for _, e := range g.Entities {
			// #1217: match all three http endpoint kind variants.
			if !isHTTPEndpointLink(e.Kind) {
				continue
			}
			// #2702 — harvest mount-point prefixes declared in this repo.
			// `url_mount_point` synthetics carry the include() prefix in
			// `url_prefix` (preferred — already normalised with a leading
			// slash by the emitter) and the canonical join in `path`. Both
			// are recorded under a normalised "/<segments>/" shape so the
			// retry sweep can prepend them verbatim.
			if e.Properties != nil && e.Properties["pattern_type"] == patternTypeURLMountPoint {
				raw := e.Properties["url_prefix"]
				if raw == "" {
					raw = e.Properties["route"]
				}
				if raw == "" {
					raw = e.Properties["path"]
				}
				if pfx := normalizeMountPrefix(raw); pfx != "" {
					if mountPrefixesByRepo[g.Repo] == nil {
						mountPrefixesByRepo[g.Repo] = map[string]bool{}
					}
					mountPrefixesByRepo[g.Repo][pfx] = true
				}
				// A mount-point synthetic is not a real producer endpoint —
				// it cannot match a consumer call on its own. Skip indexing.
				continue
			}
			if e.Name == "" {
				continue
			}
			hit := &httpEndpointHit{
				repo:       g.Repo,
				stampedID:  e.ID,
				name:       e.Name,
				sourceFile: e.SourceFile,
			}
			if e.Properties != nil {
				hit.verb = e.Properties["verb"]
				hit.canonicalPath = e.Properties["path"]
				hit.framework = e.Properties["framework"]
				hit.urlPrefix = e.Properties["url_prefix"]
				// #2808 — capture the producer's overridden detail-route param
				// name. lookup_url_kwarg takes precedence over lookup_field (DRF
				// uses lookup_url_kwarg for the URL placeholder, falling back to
				// lookup_field). Empty ⇒ the DRF default `pk`.
				if lk := e.Properties["lookup_url_kwarg"]; lk != "" {
					hit.lookupKwarg = lk
				} else if lf := e.Properties["lookup_field"]; lf != "" {
					hit.lookupKwarg = lf
				}
				switch e.Properties["pattern_type"] {
				case patternTypeProducer:
					hit.side = sideProducer
				case patternTypeConsumer:
					hit.side = sideConsumer
				}
				// Resolve source_caller (consumer side) to a stamped
				// entity ID in the same file. Falls through silently
				// when missing or unresolvable.
				if ref := e.Properties["source_caller"]; ref != "" {
					if kind, name, ok := splitKindNameRef(ref); ok {
						if id := entIDByKey[entKey{kind, name, e.SourceFile}]; id != "" {
							hit.callerID = id
						}
					}
				}
			}
			// Fallbacks: derive verb / path from the canonical name if
			// the properties weren't populated for some reason.
			if hit.verb == "" || hit.canonicalPath == "" {
				if v, p, ok := parseHTTPName(e.Name); ok {
					if hit.verb == "" {
						hit.verb = v
					}
					if hit.canonicalPath == "" {
						hit.canonicalPath = p
					}
				}
			}
			// Edge-based side resolution.
			if ee := edgesForRepo[e.ID]; ee != nil {
				if len(ee.implementsFrom) > 0 {
					if hit.side == sideUnknown {
						hit.side = sideProducer
					}
					hit.handlerID = ee.implementsFrom[0]
				}
				if len(ee.callsFrom) > 0 {
					if hit.side == sideUnknown {
						hit.side = sideConsumer
					}
					if hit.callerID == "" {
						hit.callerID = ee.callsFrom[0]
					}
				}
			}
			// Final default: when no signal at all is available, treat
			// as producer-side. Most http_endpoint synthetics in
			// practice come from the producer-side scan.
			if hit.side == sideUnknown {
				hit.side = sideProducer
			}
			byRepo := hits[e.Name]
			if byRepo == nil {
				byRepo = map[string][]*httpEndpointHit{}
				hits[e.Name] = byRepo
			}
			byRepo[g.Repo] = append(byRepo[g.Repo], hit)
		}
	}

	// Verb wildcarding: build a parallel index from (normalized-path) → []hit
	// so `ANY`-verb endpoints can be matched against any specific-verb endpoint
	// with the same path shape. The key is normalized via normalizePathForIndex
	// so that placeholder names ({pk}, {param}, {id}, :id, etc.) from different
	// extractors all collapse to the same bucket key {*}. The original
	// canonicalPath is preserved on every hit object for identifier emission.
	//
	// #2558: When canonicalPath is empty, fall back to the parsed path from
	// the hit.name to avoid silent skip. This handles consumer-side hits where
	// canonicalPath may not be populated but the endpoint name carries the path.
	byPath := map[string][]*httpEndpointHit{}
	hitsProcessed := 0
	hitsCanonicalEmpty := 0
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, h := range perRepo {
				hitsProcessed++
				path := h.canonicalPath
				if path == "" {
					hitsCanonicalEmpty++
					// Fallback: parse path from the canonical name if
					// canonicalPath is empty. This mirrors the fallback
					// logic above (lines 300-308) and ensures consumer
					// hits without a populated path property still register.
					if _, p, ok := parseHTTPName(h.name); ok {
						path = p
					}
				}
				if path == "" {
					continue
				}
				key := normalizePathForIndex(path)
				byPath[key] = append(byPath[key], h)

				// #819 — also index under the prefix-stripped path when the
				// producer carries a url_prefix (set by DRF router expansion
				// via #800/#811). This lets consumers that call without the
				// API-version prefix (e.g. fetch('/buildings/') vs server at
				// '/api/v1/buildings/') still find the producer via byPath.
				// We only strip when h.urlPrefix is a valid path prefix of
				// the (possibly fallback) path to avoid false-positive strip-downs.
				if h.urlPrefix != "" && strings.HasPrefix(path, h.urlPrefix) {
					stripped := path[len(h.urlPrefix):]
					if stripped == "" {
						stripped = "/"
					}
					strippedKey := normalizePathForIndex(stripped)
					if strippedKey != key {
						byPath[strippedKey] = append(byPath[strippedKey], h)
					}
				}

				// #1409 — property-free generic API/version prefix strip.
				// Many producers (urlconf_nested_include, hand-written
				// routers, Express/gin Routers) and consumers carry an
				// `/api`, `/api/vN`, or bare `/vN` prefix WITHOUT a populated
				// url_prefix property; register a stripped alias so the two
				// sides bucket together regardless. Registering this on BOTH
				// sides means a `/api/v1/x` producer and a `/api/x` consumer
				// both also appear under `/x`.
				if genericKey, ok := stripAPIPrefix(key); ok {
					byPath[genericKey] = append(byPath[genericKey], h)
				}

				// #1496 — GraphQL root aliasing. A GraphQL service exposes one
				// HTTP endpoint (conventionally `POST /graphql`) that multiplexes
				// every field resolver. The producer side emits per-field
				// synthetics with paths like `/graphql/Query/searchProducts`
				// (verb=GRAPHQL), while a client (Apollo `new ApolloClient({uri:
				// ".../graphql"})`) only knows the transport root `/graphql`.
				// Register every GraphQL field-level producer ALSO under the
				// `/graphql` root so a client pointing at the root matches the
				// service. We key off the `/graphql/` path prefix (not framework)
				// so any GraphQL extractor benefits, and only register producer
				// hits to avoid a consumer-root entry shadowing the producer.
				if h.side == sideProducer && strings.HasPrefix(key, "/graphql/") {
					byPath["/graphql"] = append(byPath["/graphql"], h)
				}
			}
		}
	}

	// #2703 — case-normalization index. Keyed by
	// caseNormalizePathSegments(normalizePathForIndex(path)) so that producers
	// using snake_case / kebab-case / camelCase / PascalCase route segments
	// all bucket together with consumers using a different casing style.
	// Per-segment normalization preserves segment structure, so this index
	// CANNOT match a single-segment consumer (e.g. `/searchBuildings`) to a
	// multi-segment producer (`/buildings/search`) — path reordering is out
	// of scope (see #2703 acceptance criterion 4).
	//
	// Only producer hits are indexed; consumer hits probe this index when
	// the standard byPath lookup AND prefix-injection retry both miss.
	byCaseNorm := map[string][]*httpEndpointHit{}
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, h := range perRepo {
				if h.side != sideProducer {
					continue
				}
				producerPath := h.canonicalPath
				if producerPath == "" {
					if _, p, ok := parseHTTPName(h.name); ok {
						producerPath = p
					}
				}
				if producerPath == "" {
					continue
				}
				baseKey := normalizePathForIndex(producerPath)
				caseKey := caseNormalizePathSegments(baseKey)
				if caseKey != baseKey {
					byCaseNorm[caseKey] = append(byCaseNorm[caseKey], h)
				}
				// Also register the prefix-stripped form so a consumer that
				// omits the API/version prefix (the layered #2702 mount-prefix
				// case) still finds the producer via the case-norm index. This
				// composes mount-prefix + case-normalize without requiring
				// #2702 to have landed.
				if stripped, ok := stripAPIPrefix(baseKey); ok {
					strippedCase := caseNormalizePathSegments(stripped)
					if strippedCase != caseKey {
						byCaseNorm[strippedCase] = append(byCaseNorm[strippedCase], h)
					}
				}
			}
		}
	}

	// #2588 — URL-pattern normalization index. Keyed by normalizeURLPattern(path)
	// so that paths differing only in param syntax ({id} vs <pk:int>) or a
	// trailing query string (/users?foo=bar vs /users) can be matched in the
	// orphan-retry sweep below. Only producer hits are indexed here; consumer
	// hits are looked up against it during the sweep.
	byNormPattern := map[string][]*httpEndpointHit{}
	// #2808 — flat producer list for the literal-fills-param orphan sweep. A
	// literal caller segment occupying a producer param slot does not share a
	// byPath / byCaseNorm / byNormPattern key with its producer (the literal
	// stays literal under every normalization), so the sweep probes producers
	// directly via literalFillsParamSlot. Collected here to avoid re-walking
	// the nested hits map.
	var allProducers []*httpEndpointHit
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, h := range perRepo {
				if h.side != sideProducer {
					continue
				}
				producerPath := h.canonicalPath
				if producerPath == "" {
					if _, p, ok := parseHTTPName(h.name); ok {
						producerPath = p
					}
				}
				if producerPath == "" {
					continue
				}
				normKey := normalizeURLPattern(producerPath)
				byNormPattern[normKey] = append(byNormPattern[normKey], h)
				allProducers = append(allProducers, h)
			}
		}
	}

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	// #2571: per-pass counters for orphan and cross-repo-resolved consumers.
	// Both are local to this invocation — they can never accumulate across
	// successive index runs. matchedConsumers tracks consumer stampedIDs that
	// emitted at least one link; the final orphan count = total unique consumers
	// seen − matched ones, so orphan_calls ≤ total consumer entities.
	matchedConsumers := map[string]bool{} // repo::stampedID → matched
	allConsumers := map[string]bool{}     // repo::stampedID → seen

	// #2669: resolve-strategy telemetry. attempts is incremented once per
	// (consumer, producer-repo) probe in the main loop; hitsByStrategy is
	// incremented exactly once per emitted link (keyed by the strategy that
	// produced the match). The taxonomy is documented on PassResult.
	resolveAttempts := 0
	hitsByStrategy := map[string]int{}
	missesByReason := map[string]int{}

	// #2813: dynamic-baseurl static-suffix sweep bookkeeping. residualCandidates
	// counts the total ranked producer candidates emitted for ambiguous /
	// generic dynamic-baseurl suffixes (below the auto-link threshold) so the
	// resolve surface can report how much candidate signal exists.
	// dynamicSuffixCounted marks consumers the suffix sweep already classified
	// into missesByReason so the generic classification loop does not
	// double-count them.
	residualCandidates := 0
	dynamicSuffixCounted := map[string]bool{}

	// Deterministic iteration order: sort names.
	names := make([]string, 0, len(hits))
	for n := range hits {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		byRepo := hits[name]
		// Producers & consumers for this exact name.
		producers, consumers := splitSidesAcrossRepos(byRepo)

		// Verb wildcarding: if `name` has verb=ANY, also pull in any
		// specific-verb hits with the same path; conversely if `name`
		// has a specific verb, also pull in ANY-verb hits with the same
		// path. The wildcarded counterpart contributes producer /
		// consumer hits keyed by ITS own repo set.
		if verb, p, ok := parseHTTPName(name); ok {
			// Probe the byPath index under the normalized path AND its
			// generic API/version-stripped form (#1409), so a name carrying
			// `/api/v1/...` finds the `/...` bucket and vice versa. The
			// stripped alias was registered on both sides during index build.
			normKey := normalizePathForIndex(p)
			probeKeys := []string{normKey}
			if strippedKey, sok := stripAPIPrefix(normKey); sok {
				probeKeys = append(probeKeys, strippedKey)
			}
			for _, pk := range probeKeys {
				for _, h := range byPath[pk] {
					if h.name == name {
						continue
					}
					hVerb, _, _ := parseHTTPName(h.name)
					if !verbsCompatible(verb, hVerb) {
						continue
					}
					if h.side == sideProducer {
						producers = appendUnique(producers, h)
					} else if h.side == sideConsumer {
						// #1445: skip consumer hits whose canonical name has its
						// own entry in the top-level hits map. That consumer will
						// be linked (or not) in its own name-bucket iteration.
						// Pulling it into the current bucket via byPath causes
						// cross-bucket deduplication: consumerRepos picks it as
						// the first consumer for its repo, which may already have
						// a link from its own bucket, leaving the current
						// bucket's real consumers permanently unlinked.
						if _, hasOwnBucket := hits[h.name]; hasOwnBucket {
							continue
						}
						consumers = appendUnique(consumers, h)
					}
				}
			}
		}

		// #2702 — mount-prefix wildcarding. The verb-wildcarding probe above
		// only finds counterpart hits keyed by an identical (or
		// generic-stripped) normalized path; a consumer calling `/things`
		// will not surface a producer keyed `/internal/v3/things` because
		// the generic strip only handles the `/api,/api/vN,/vN` family.
		// Probe one more time using each discovered url_mount_point prefix
		// (plus the static fallback set) prepended to the bucket's name,
		// so a same-verb producer on the prefixed path is pulled in.
		//
		// This drives the actual link emission in the (cRepo, pRepo) inner
		// loop below — the per-consumer mount-prefix retry already living
		// there only runs when the bucket already contains at least one
		// producer for the target repo. Without this wildcarding step the
		// bucket for a /things-only consumer would short-circuit at the
		// `len(producers) == 0` check before the inner retry ever fires.
		if verb, p, ok := parseHTTPName(name); ok && len(consumers) > 0 {
			normKey := normalizePathForIndex(p)
			if _, hasPrefix := stripAPIPrefix(normKey); !hasPrefix && normKey != "" {
				// Build the union of mount prefixes across every repo so a
				// consumer in one repo can pull in a prefixed producer from
				// any other repo. Per-repo restriction is then applied
				// inside the per-consumer mount-prefix retry on the inner
				// loop, which is where the strategy stamp gets written.
				union := map[string]bool{}
				for _, perRepo := range mountPrefixesByRepo {
					for pfx := range perRepo {
						union[pfx] = true
					}
				}
				for _, pfx := range mountPrefixesForRepo(union) {
					probedKey := normalizePathForIndex(pfx + strings.TrimPrefix(normKey, "/"))
					for _, h := range byPath[probedKey] {
						if h.side != sideProducer {
							continue
						}
						hVerb, _, _ := parseHTTPName(h.name)
						if !verbsCompatible(verb, hVerb) {
							continue
						}
						producers = appendUnique(producers, h)
					}
				}
			}
		}

		if len(producers) == 0 || len(consumers) == 0 {
			// Record consumer hits even when unmatched — they count as orphans
			// unless covered by a later bucket.
			for _, c := range consumers {
				allConsumers[entityKey(c.repo, c.stampedID)] = true
			}
			continue
		}

		// Track all consumer hits seen so we can derive orphan counts later.
		for _, c := range consumers {
			allConsumers[entityKey(c.repo, c.stampedID)] = true
		}
		// Deterministic ordering for tie-breaking inside each verb tier.
		sort.SliceStable(producers, func(i, j int) bool { return less(producers[i], producers[j]) })
		sort.SliceStable(consumers, func(i, j int) bool { return less(consumers[i], consumers[j]) })

		// Group producers by repo so we can run the verb-aware picker
		// independently for every (consumer-repo, producer-repo) pair.
		producersByRepo := map[string][]*httpEndpointHit{}
		for _, p := range producers {
			producersByRepo[p.repo] = append(producersByRepo[p.repo], p)
		}
		// Group ALL consumers by repo — unlike the old single-consumer-per-repo
		// deduplication, we now allow every distinct consumer entity (e.g.
		// a legacy service file and a V2 service file that both call the same
		// endpoint) to receive its own cross-repo link. The emitted map keyed
		// by (source, target, method) prevents duplicate links when two
		// consumers in the same repo happen to resolve to the same producer
		// entity. (#2611)
		consumersByRepo := map[string][]*httpEndpointHit{}
		for _, h := range consumers {
			consumersByRepo[h.repo] = append(consumersByRepo[h.repo], h)
		}

		consumerRepoNames := make([]string, 0, len(consumersByRepo))
		for r := range consumersByRepo {
			consumerRepoNames = append(consumerRepoNames, r)
		}
		sort.Strings(consumerRepoNames)
		producerRepoNames := make([]string, 0, len(producersByRepo))
		for r := range producersByRepo {
			producerRepoNames = append(producerRepoNames, r)
		}
		sort.Strings(producerRepoNames)

		for _, cRepo := range consumerRepoNames {
			for _, pRepo := range producerRepoNames {
				intraRepo := cRepo == pRepo
				for _, c := range consumersByRepo[cRepo] {
					// #2669: every (consumer, producer-repo) candidacy counts as
					// one resolution attempt, regardless of whether a link is
					// ultimately emitted. Intra-repo pairs are excluded because
					// they're segregated into MethodHTTPSelf and don't reflect
					// the cross-repo orphan health metric this counter feeds.
					if !intraRepo {
						resolveAttempts++
					}
					// Verb-aware producer selection (#747):
					//   Tier 1 — producer with the SAME specific verb as the
					//            consumer (exact_verb).
					//   Tier 2 — producer with verb=ANY (any_fallback).
					//   Tier 3 — none. We skip rather than fall through to a
					//            different specific verb: DELETE/{id} must
					//            never link to PATCH/{id} just because both
					//            paths normalize the same.
					// Producers within each tier are already sorted by
					// less() above, so picking the first match is
					// deterministic.
					p, quality := pickProducerForConsumer(c, producersByRepo[pRepo])

					// #2702 — mount-prefix attribution. The wildcarding step
					// above this loop can pull a producer into producersByRepo
					// via a discovered url_mount_point prefix; when that
					// happens, the picker returns it labelled "exact" because
					// the bucket already contains the producer. Detect the
					// case retroactively by comparing the consumer's normalized
					// path to the chosen producer's: if the producer path
					// equals `<prefix> + consumer_path` for any discovered
					// prefix in the producer's repo, the resolution is a
					// mount-prefix hit, and the link must be stamped
					// accordingly so the telemetry counts it under the right
					// strategy.
					var preselectedMountPrefix string
					if p != nil && !intraRepo {
						preselectedMountPrefix = detectMountPrefix(c, p, mountPrefixesByRepo[pRepo])
					}

					// Prefix-injection retry (#2569): mirrors Tier 2 of
					// resolveCallByPath in internal/engine/http_endpoint_match.go
					// (#2557). When the standard byPath match missed AND the
					// consumer path carries no API/version prefix of its own,
					// retry by prepending each well-known prefix candidate to the
					// consumer's normalized path and probing byPath. This handles
					// the upvate pattern where the frontend extractor emits a raw
					// path (e.g. `/searchBuildings`) while the backend mounts the
					// route at `/api/v1/searchBuildings`.
					//
					// First match wins; edge is stamped with
					// Properties["prefix_normalized"] (e.g. "api/v1") so the
					// resolution is traceable in the graph.
					var prefixNormalized string
					if p == nil {
						consumerPath := c.canonicalPath
						if consumerPath == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPath = parsed
							}
						}
						normConsumerPath := normalizePathForIndex(consumerPath)
						// Only attempt when the consumer path has no API/version
						// prefix itself — a double-prefixed path would be invalid.
						if _, hasPrefix := stripAPIPrefix(normConsumerPath); !hasPrefix && normConsumerPath != "" {
							for _, pfx := range crossRepoPrefixCandidates {
								prefixedKey := pfx + normConsumerPath
								pfxCandidates := make([]*httpEndpointHit, 0)
								for _, h := range byPath[prefixedKey] {
									if h.repo == pRepo && h.side == sideProducer {
										pfxCandidates = appendUnique(pfxCandidates, h)
									}
								}
								if len(pfxCandidates) > 0 {
									sort.SliceStable(pfxCandidates, func(i, j int) bool { return less(pfxCandidates[i], pfxCandidates[j]) })
									p, quality = pickProducerForConsumer(c, pfxCandidates)
									if p != nil {
										prefixNormalized = strings.TrimPrefix(pfx, "/")
										break
									}
								}
							}
						}
					}

					// Consumer-side mount-prefix retry (#2702): when the standard
					// byPath probe AND the hardcoded prefix-injection retry both
					// miss, retry once more using prefixes actually declared in the
					// producer repo. These come from `url_mount_point` synthetics
					// (#2677) — every `path("api/v1/", include(...))` site
					// contributes one — followed by a tiny static fallback set
					// (`/api/`, `/api/v1/`, `/api/v2/`) so producers that have not
					// yet been re-indexed under #2677 still benefit.
					//
					// First match wins. The emitted link is stamped with
					// Properties["resolve_strategy"] = "mount_prefix_added" and
					// Properties["applied_mount_prefix"] so the resolution stays
					// traceable in the graph, mirroring the prefix_normalized
					// annotation used by the older retry.
					appliedMountPrefix := preselectedMountPrefix
					if p == nil {
						consumerPath := c.canonicalPath
						if consumerPath == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPath = parsed
							}
						}
						normConsumerPath := normalizePathForIndex(consumerPath)
						// Guard against double-prefixing: consumers that already
						// carry an API/version prefix were handled by the upstream
						// byPath / generic-strip path and should not pick up a
						// second one here.
						if _, hasPrefix := stripAPIPrefix(normConsumerPath); !hasPrefix && normConsumerPath != "" {
							candidates := mountPrefixesForRepo(mountPrefixesByRepo[pRepo])
							for _, pfx := range candidates {
								// pfx is normalised to "/<segments>/"; consumer
								// path starts with "/", so trim once to avoid "//".
								prefixedPath := pfx + strings.TrimPrefix(normConsumerPath, "/")
								prefixedKey := normalizePathForIndex(prefixedPath)
								pfxCandidates := make([]*httpEndpointHit, 0)
								for _, h := range byPath[prefixedKey] {
									if h.repo == pRepo && h.side == sideProducer {
										pfxCandidates = appendUnique(pfxCandidates, h)
									}
								}
								if len(pfxCandidates) == 0 {
									continue
								}
								sort.SliceStable(pfxCandidates, func(i, j int) bool { return less(pfxCandidates[i], pfxCandidates[j]) })
								p, quality = pickProducerForConsumer(c, pfxCandidates)
								if p != nil {
									appliedMountPrefix = pfx
									break
								}
							}
						}
					}

					// Case-normalization retry (#2703): when both the standard
					// byPath lookup AND the mount-prefix retry miss, attempt to
					// match the consumer path against the byCaseNorm index after
					// normalizing each segment to its canonical id (lowercase,
					// hyphens/underscores stripped). This handles the upvate
					// pattern where the frontend calls `/assignedContacts` and
					// the backend defines `/api/v1/contracts/{pk}/assigned_contacts`,
					// or `/equipment-types` ↔ `equipment_types`, etc.
					//
					// Per-segment normalization preserves segment count, so this
					// retry cannot match across reordered paths (e.g.
					// `/searchBuildings` ↔ `/buildings/search`) — those are out
					// of scope (#2703 acceptance #4).
					//
					// We probe with and without API-version prefix-injection so
					// this strategy composes with the mount-prefix retry:
					// `/assignedContacts` consumer matches
					// `/api/v1/.../assigned_contacts` producer via the
					// prefix-stripped alias that byCaseNorm registers above.
					//
					// First match wins; edge is stamped with
					// Properties["resolve_strategy"] = "case_style_normalized".
					var caseNormalized bool
					if p == nil {
						consumerPath := c.canonicalPath
						if consumerPath == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPath = parsed
							}
						}
						normConsumerPath := normalizePathForIndex(consumerPath)
						if normConsumerPath != "" {
							probeKeys := []string{caseNormalizePathSegments(normConsumerPath)}
							// Also try with each well-known API/version prefix
							// prepended so the case-normalize retry composes
							// with the prefix-injection strategy without
							// requiring an exact ordering of which retry runs
							// first.
							if _, hasPrefix := stripAPIPrefix(normConsumerPath); !hasPrefix {
								for _, pfx := range crossRepoPrefixCandidates {
									probeKeys = append(probeKeys, caseNormalizePathSegments(pfx+normConsumerPath))
								}
							}
							for _, probeKey := range probeKeys {
								if probeKey == "" {
									continue
								}
								caseCandidates := make([]*httpEndpointHit, 0)
								for _, h := range byCaseNorm[probeKey] {
									if h.repo == pRepo && h.side == sideProducer {
										caseCandidates = appendUnique(caseCandidates, h)
									}
								}
								if len(caseCandidates) == 0 {
									continue
								}
								sort.SliceStable(caseCandidates, func(i, j int) bool { return less(caseCandidates[i], caseCandidates[j]) })
								picked, q := pickProducerForConsumer(c, caseCandidates)
								if picked != nil {
									p, quality = picked, q
									caseNormalized = true
									// Reset prefixNormalized — the matched
									// strategy is case_style_normalized, not
									// prefix_stripped, even if the probe
									// key happened to carry a prefix.
									prefixNormalized = ""
									break
								}
							}
						}
					}

					// URL-pattern normalization retry (#2588): when both the standard
					// byPath lookup AND the prefix-injection retry miss, attempt to
					// match the consumer path against every producer in the target repo
					// using normalizeURLPattern. This resolves the 374-candidate case
					// where client emits /inspections/{id} and server emits
					// /api/v1/inspections/<pk:int> — different param syntax only.
					//
					// When a normalized match is found, confidence is boosted to
					// urlPatternNormConfidence (0.95) and Properties["normalization"]
					// is set to "url_pattern" so the resolution is traceable.
					var urlPatternNormAnnotation string
					var urlPatternNormConfidenceVal float64
					if p == nil {
						consumerPath := c.canonicalPath
						if consumerPath == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPath = parsed
							}
						}
						if consumerPath != "" {
							for _, candidate := range producersByRepo[pRepo] {
								if !verbsCompatible(c.verb, candidate.verb) {
									continue
								}
								producerPath := candidate.canonicalPath
								if producerPath == "" {
									if _, parsed, ok := parseHTTPName(candidate.name); ok {
										producerPath = parsed
									}
								}
								conf, ann := applyURLPatternNorm(consumerPath, producerPath)
								if conf > 0 {
									p = candidate
									quality = matchQualityAnyFallback
									urlPatternNormAnnotation = ann
									urlPatternNormConfidenceVal = conf
									break
								}
							}
						}
					}

					// Literal-fills-param retry (#2808): the last cross-repo
					// stage. When every prior strategy missed AND the consumer
					// sends a CONCRETE segment where the producer route declares
					// a path-parameter placeholder (e.g. core-mobile's
					// `GET /recents/buildings` ↔ DRF `GET /api/v1/recents/{pk}`),
					// resolve it by treating the literal as the value occupying
					// the param slot. Probing producers directly (rather than an
					// index) keeps the over-match guard in literalFillsParamSlot
					// authoritative. Running last guarantees the "prefer an exact
					// static endpoint over a param-fill" rule: a real
					// `/recents/buildings` producer would have matched in an
					// earlier stage and left p non-nil here.
					var literalParamFilled bool
					if p == nil {
						consumerPath := c.canonicalPath
						if consumerPath == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPath = parsed
							}
						}
						if consumerPath != "" {
							for _, candidate := range producersByRepo[pRepo] {
								if !verbsCompatible(c.verb, candidate.verb) {
									continue
								}
								producerPath := candidate.canonicalPath
								if producerPath == "" {
									if _, parsed, ok := parseHTTPName(candidate.name); ok {
										producerPath = parsed
									}
								}
								if literalFillsParamSlot(consumerPath, producerPath) {
									p = candidate
									quality = matchQualityAnyFallback
									literalParamFilled = true
									break
								}
							}
						}
					}

					if p == nil {
						continue
					}

					// Source / target: consumer's caller → producer's handler.
					// If either side hasn't resolved to a real entity, fall
					// back to the synthetic stampedID so the link still
					// points at something meaningful.
					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					// #2585 — intra-repo HTTP self-calls use MethodHTTPSelf so
					// they are segregated from cross-repo CALLS links.
					linkMethod := MethodHTTP
					if intraRepo {
						linkMethod = MethodHTTPSelf
					}
					id := MakeID(source, target, linkMethod)
					if emitted[id] {
						continue
					}
					emitted[id] = true
					// #2571/#2573: mark this consumer as cross-repo-resolved for
					// the per-pass counter. One consumer may link to multiple
					// producer repos; we count it resolved once it has any link.
					matchedConsumers[entityKey(c.repo, c.stampedID)] = true
					// #2808 — param-name normalization detection. When the match
					// was bridged purely across a path-parameter NAME boundary
					// (consumer `/clients/{clientId}` ↔ producer
					// `/api/v1/clients/{pk}`), reclassify it so the resolution is
					// traceable instead of vanishing into the generic `exact` /
					// `mount_prefix_added` bucket. The url_pattern + case_style_normalized
					// retries already collapse param/segment shape, so they are
					// excluded here to avoid double-attribution.
					paramNormalized := false
					if urlPatternNormAnnotation == "" && !caseNormalized {
						consumerPathPN := c.canonicalPath
						if consumerPathPN == "" {
							if _, parsed, ok := parseHTTPName(c.name); ok {
								consumerPathPN = parsed
							}
						}
						producerPathPN := p.canonicalPath
						if producerPathPN == "" {
							if _, parsed, ok := parseHTTPName(p.name); ok {
								producerPathPN = parsed
							}
						}
						paramNormalized = paramOnlyMismatch(consumerPathPN, producerPathPN)
					}
					// #2669: bucket the hit by the strategy that resolved it.
					// Order matters: url_pattern + prefix_stripped are checked
					// before the default "exact" bucket because the main loop
					// reuses the same `p` variable across retry stages.
					// param_normalized sits above mount_prefix_added/exact because
					// the param-name bridge is the more-specific characterisation
					// of the match (#2808); the mount prefix, when also present, is
					// still recorded as a link property below.
					if !intraRepo {
						switch {
						case urlPatternNormAnnotation != "":
							hitsByStrategy["url_pattern"]++
						case caseNormalized:
							hitsByStrategy["case_style_normalized"]++
						case literalParamFilled:
							hitsByStrategy["literal_param_fill"]++
						case paramNormalized:
							hitsByStrategy["param_normalized"]++
						case appliedMountPrefix != "":
							hitsByStrategy["mount_prefix_added"]++
						case prefixNormalized != "":
							hitsByStrategy["prefix_stripped"]++
						default:
							hitsByStrategy["exact"]++
						}
					}

					ident := canonicalIdentifier(c, p)
					ch := httpChannel
					confidence := ScoreImport()
					if urlPatternNormConfidenceVal > 0 {
						confidence = urlPatternNormConfidenceVal
					}
					// Intra-repo self-calls use RelationRoutesTo; cross-repo uses RelationCalls.
					relation := RelationCalls
					if intraRepo {
						relation = RelationRoutesTo
					}
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     relation,
						Method:       linkMethod,
						Confidence:   confidence,
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: quality,
					}
					if prefixNormalized != "" {
						link.Properties = map[string]string{"prefix_normalized": prefixNormalized}
					}
					if appliedMountPrefix != "" {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["resolve_strategy"] = "mount_prefix_added"
						link.Properties["applied_mount_prefix"] = appliedMountPrefix
					}
					if caseNormalized {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["resolve_strategy"] = "case_style_normalized"
					}
					// #2808 — stamp param-name normalization. Only set when the
					// link is not already attributed to a more-specific strategy
					// (case_style_normalized / url_pattern) so the resolve_strategy
					// property and the telemetry bucket stay consistent. When a
					// mount prefix also bridged the match, applied_mount_prefix is
					// retained alongside so both signals survive. The producer's
					// overridden lookup kwarg, when present, is recorded for
					// traceability of a genuine lookup_field/lookup_url_kwarg
					// override.
					if paramNormalized && urlPatternNormAnnotation == "" && !caseNormalized {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["resolve_strategy"] = "param_normalized"
						if p.lookupKwarg != "" {
							link.Properties["lookup_kwarg"] = p.lookupKwarg
						}
					}
					if urlPatternNormAnnotation != "" {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["normalization"] = urlPatternNormAnnotation
					}
					if intraRepo {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["intra_repo"] = "true"
					}
					// #3628 — honesty marker. Every link emitted from this block
					// pairs an AST-grounded consumer caller with an AST-grounded
					// producer handler matched on the canonical (verb, path) id —
					// even through prefix/case/param normalisation, both sides are
					// real endpoints. That is the highest-honesty `resolved` class.
					link.WithEdgeConfidence(ConfidenceResolved)
					fresh = append(fresh, link)
				} // end for _, c := range consumersByRepo[cRepo]
			}
		}
	}

	// #2703 — case-normalization orphan-retry sweep.
	//
	// Consumer hits that the main loop could not match (their canonical name
	// lives in a bucket without any compatible producer, and byPath
	// wildcarding could not pull them together) are retried here using the
	// byCaseNorm index. This handles the common pattern where the frontend
	// emits a camelCase route segment and the backend defines a snake_case
	// or kebab-case segment for the same conceptual endpoint.
	//
	// Per-segment normalization preserves segment count, so this sweep
	// cannot match across reordered segments — `/searchBuildings` and
	// `/buildings/search` remain orphans (out of scope per #2703 #4).
	//
	// Runs BEFORE the url_pattern sweep so the more-specific
	// case_style_normalized strategy is preferred when both would match.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if matchedConsumers[cKey] {
					continue // already resolved by main loop
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				if consumerPath == "" {
					continue
				}
				normConsumerPath := normalizePathForIndex(consumerPath)
				if normConsumerPath == "" {
					continue
				}
				// Probe the case-norm index under the consumer's normalized
				// case-key AND, when the consumer has no API/version prefix
				// of its own, every prefix-prepended variant. This composes
				// with the prefix-injection strategy (#2569 / #2702) so the
				// frontend can emit `/assignedContacts` and match a producer
				// at `/api/v1/contracts/{*}/assigned_contacts` if a producer
				// exists at that case-canonical key.
				probeKeys := []string{caseNormalizePathSegments(normConsumerPath)}
				if _, hasPrefix := stripAPIPrefix(normConsumerPath); !hasPrefix {
					for _, pfx := range crossRepoPrefixCandidates {
						probeKeys = append(probeKeys, caseNormalizePathSegments(pfx+normConsumerPath))
					}
				}
				var eligible []*httpEndpointHit
				seenCandidate := map[string]bool{}
				for _, probeKey := range probeKeys {
					if probeKey == "" {
						continue
					}
					for _, p := range byCaseNorm[probeKey] {
						if p.repo == c.repo {
							continue
						}
						if !verbsCompatible(c.verb, p.verb) {
							continue
						}
						pid := entityKey(p.repo, p.stampedID)
						if seenCandidate[pid] {
							continue
						}
						seenCandidate[pid] = true
						eligible = append(eligible, p)
					}
				}
				if len(eligible) == 0 {
					continue
				}
				sort.SliceStable(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })
				producersByRepoCN := map[string][]*httpEndpointHit{}
				for _, p := range eligible {
					producersByRepoCN[p.repo] = append(producersByRepoCN[p.repo], p)
				}
				pRepoNamesCN := make([]string, 0, len(producersByRepoCN))
				for r := range producersByRepoCN {
					pRepoNamesCN = append(pRepoNamesCN, r)
				}
				sort.Strings(pRepoNamesCN)
				allConsumers[cKey] = true
				for _, pRepo := range pRepoNamesCN {
					p, quality := pickProducerForConsumer(c, producersByRepoCN[pRepo])
					if p == nil {
						continue
					}
					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					id := MakeID(source, target, MethodHTTP)
					if emitted[id] {
						continue
					}
					emitted[id] = true
					matchedConsumers[cKey] = true
					hitsByStrategy["case_style_normalized"]++
					ident := canonicalIdentifier(c, p)
					ch := httpChannel
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     RelationCalls,
						Method:       MethodHTTP,
						Confidence:   ScoreImport(),
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: quality,
						Properties:   map[string]string{"resolve_strategy": "case_style_normalized"},
					}
					// #3628 — both endpoints are AST-grounded; only the casing
					// style of the route segments differed. resolved.
					link.WithEdgeConfidence(ConfidenceResolved)
					fresh = append(fresh, link)
				}
			}
		}
	}

	// #2588 — URL-pattern normalization orphan-retry sweep.
	//
	// Consumer hits that the main loop could not match (different entity-name
	// bucket AND byPath missed) are retried here using the byNormPattern index.
	// This resolves cases where:
	//   • the consumer emits a query-string-carrying path (/users?foo=bar vs /users)
	//   • the param syntax differs so radically that pathParamRe + byPath still
	//     misses (edge cases not covered by the inline retry inside the main loop)
	//
	// We iterate all consumer hits, skip already-matched ones, and probe
	// byNormPattern[normalizeURLPattern(consumerPath)] for same-verb producers
	// in OTHER repos.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if matchedConsumers[cKey] {
					continue // already resolved by main loop
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				if consumerPath == "" {
					continue
				}
				normKey := normalizeURLPattern(consumerPath)
				candidates := byNormPattern[normKey]
				if len(candidates) == 0 {
					continue
				}
				// Filter to other repos, verb-compatible producers.
				var eligible []*httpEndpointHit
				for _, p := range candidates {
					if p.repo == c.repo {
						continue
					}
					if !verbsCompatible(c.verb, p.verb) {
						continue
					}
					eligible = appendUnique(eligible, p)
				}
				if len(eligible) == 0 {
					continue
				}
				sort.SliceStable(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })
				// Group by producer repo and pick one per repo.
				producersByRepo2 := map[string][]*httpEndpointHit{}
				for _, p := range eligible {
					producersByRepo2[p.repo] = append(producersByRepo2[p.repo], p)
				}
				pRepoNames2 := make([]string, 0, len(producersByRepo2))
				for r := range producersByRepo2 {
					pRepoNames2 = append(pRepoNames2, r)
				}
				sort.Strings(pRepoNames2)
				allConsumers[cKey] = true
				for _, pRepo := range pRepoNames2 {
					p, _ := pickProducerForConsumer(c, producersByRepo2[pRepo])
					if p == nil {
						continue
					}
					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					id := MakeID(source, target, MethodHTTP)
					if emitted[id] {
						continue
					}
					emitted[id] = true
					matchedConsumers[cKey] = true
					// #2669: orphan-retry sweep matches are by definition the
					// url_pattern strategy (the byNormPattern index is built
					// from normalizeURLPattern keys; main loop already tried
					// the byPath + prefix strategies).
					hitsByStrategy["url_pattern"]++
					ident := canonicalIdentifier(c, p)
					ch := httpChannel
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     RelationCalls,
						Method:       MethodHTTP,
						Confidence:   urlPatternNormConfidence,
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: matchQualityAnyFallback,
						Properties:   map[string]string{"normalization": "url_pattern"},
					}
					// #3628 — both endpoints AST-grounded; only path-param syntax
					// (or a trailing query string) differed. resolved.
					link.WithEdgeConfidence(ConfidenceResolved)
					fresh = append(fresh, link)
				}
			}
		}
	}

	// #2808 — literal-fills-param orphan-retry sweep. The LAST resolution
	// stage. A consumer that sends a CONCRETE segment where the backend route
	// declares a path-parameter placeholder (core-mobile's
	// `GET/DELETE /recents/buildings` ↔ DRF `GET/DELETE /api/v1/recents/{pk}`)
	// shares no normalization key with its producer, so the byPath / byCaseNorm
	// / byNormPattern sweeps all miss it. Probe every producer in another repo
	// via literalFillsParamSlot, which carries the over-match guard.
	//
	// Running dead-last enforces the "prefer an exact static endpoint over a
	// param-fill" rule: any consumer with a real static or param producer has
	// already been marked matched by an earlier stage and is skipped here.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if matchedConsumers[cKey] {
					continue // already resolved by an earlier stage
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				if consumerPath == "" {
					continue
				}
				// Collect literal-fill-eligible producers in other repos.
				var eligible []*httpEndpointHit
				seenCandidate := map[string]bool{}
				for _, p := range allProducers {
					if p.repo == c.repo {
						continue
					}
					if !verbsCompatible(c.verb, p.verb) {
						continue
					}
					producerPath := p.canonicalPath
					if producerPath == "" {
						if _, pp, ok := parseHTTPName(p.name); ok {
							producerPath = pp
						}
					}
					if producerPath == "" {
						continue
					}
					if !literalFillsParamSlot(consumerPath, producerPath) {
						continue
					}
					pid := entityKey(p.repo, p.stampedID)
					if seenCandidate[pid] {
						continue
					}
					seenCandidate[pid] = true
					eligible = append(eligible, p)
				}
				if len(eligible) == 0 {
					continue
				}
				sort.SliceStable(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })
				producersByRepoLF := map[string][]*httpEndpointHit{}
				for _, p := range eligible {
					producersByRepoLF[p.repo] = append(producersByRepoLF[p.repo], p)
				}
				pRepoNamesLF := make([]string, 0, len(producersByRepoLF))
				for r := range producersByRepoLF {
					pRepoNamesLF = append(pRepoNamesLF, r)
				}
				sort.Strings(pRepoNamesLF)
				allConsumers[cKey] = true
				for _, pRepo := range pRepoNamesLF {
					p, quality := pickProducerForConsumer(c, producersByRepoLF[pRepo])
					if p == nil {
						continue
					}
					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					id := MakeID(source, target, MethodHTTP)
					if emitted[id] {
						continue
					}
					emitted[id] = true
					matchedConsumers[cKey] = true
					hitsByStrategy["literal_param_fill"]++
					ident := canonicalIdentifier(c, p)
					ch := httpChannel
					props := map[string]string{"resolve_strategy": "literal_param_fill"}
					if p.lookupKwarg != "" {
						props["lookup_kwarg"] = p.lookupKwarg
					}
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     RelationCalls,
						Method:       MethodHTTP,
						Confidence:   ScoreImport(),
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: quality,
						Properties:   props,
					}
					// #3628 — the producer side is AST-grounded, but the consumer's
					// concrete segment is *interpreted* as the value filling a
					// producer param slot. That binding is reconstructed, not
					// proven from a real param on the call side. inferred.
					link.WithEdgeConfidence(ConfidenceInferred)
					fresh = append(fresh, link)
				}
			}
		}
	}

	// #3752 — path_normalized orphan-retry sweep (oracle-priority #9).
	//
	// The LAST *static* resolution stage, running after byPath, mount-prefix,
	// case-style, url-pattern, param-normalized and literal-fill have all missed
	// (and before the runtime-dynamic #2813 suffix sweep). It closes the
	// upvate-bench gap where a client calls `GET /inspections/{id}` and the
	// server serves `GET /api/v1/inspections/{pk}` — a PREFIX + PARAM-NAME
	// mismatch, not a genuinely-missing endpoint — that the earlier stages did
	// not co-bucket for this particular consumer.
	//
	// Matching contract (pathNormResolve): equal pathNormalizeForMatch key
	// (version/api prefix stripped, every param collapsed to `{}`, lower-cased)
	// AND equal segment count AND the SAME specific verb. The match is fuzzy
	// (param names and the api prefix are discarded), so the link is stamped
	// confidence=heuristic — distinct from the `resolved` class the precise
	// stages emit.
	//
	// PRECISION GUARD (ambiguity → NO link): if a consumer path normalizes to
	// MORE THAN ONE distinct server endpoint (different stamped producers, or
	// the same producer serving structurally-distinct paths that happen to share
	// a normalized key), we emit NO link for that consumer in that producer repo
	// and record the ambiguity under missesByReason["path_normalized_ambiguous"].
	// Attributing one of several candidates would be a false edge, which this
	// precision-first strategy must never do.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if matchedConsumers[cKey] {
					continue // resolved by an earlier (higher-precision) stage
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				if consumerPath == "" {
					continue
				}
				allConsumers[cKey] = true

				// Collect path_normalized-eligible producers in OTHER repos,
				// requiring an EXACT same-verb match (no ANY widening — the
				// strategy contract demands verb equality). De-dupe by stamped
				// producer identity.
				var eligible []*httpEndpointHit
				seenCandidate := map[string]bool{}
				for _, p := range allProducers {
					if p.repo == c.repo {
						continue
					}
					if strings.ToUpper(p.verb) != strings.ToUpper(c.verb) || c.verb == "" {
						continue
					}
					producerPath := p.canonicalPath
					if producerPath == "" {
						if _, pp, ok := parseHTTPName(p.name); ok {
							producerPath = pp
						}
					}
					if producerPath == "" {
						continue
					}
					if _, ok := pathNormResolve(consumerPath, producerPath); !ok {
						continue
					}
					pid := entityKey(p.repo, p.stampedID)
					if seenCandidate[pid] {
						continue
					}
					seenCandidate[pid] = true
					eligible = append(eligible, p)
				}
				if len(eligible) == 0 {
					continue
				}
				sort.SliceStable(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })
				producersByRepoPN := map[string][]*httpEndpointHit{}
				for _, p := range eligible {
					producersByRepoPN[p.repo] = append(producersByRepoPN[p.repo], p)
				}
				pRepoNamesPN := make([]string, 0, len(producersByRepoPN))
				for r := range producersByRepoPN {
					pRepoNamesPN = append(pRepoNamesPN, r)
				}
				sort.Strings(pRepoNamesPN)
				for _, pRepo := range pRepoNamesPN {
					repoCands := producersByRepoPN[pRepo]
					// Ambiguity guard: a consumer that normalizes to more than
					// one DISTINCT server endpoint in this repo is ambiguous —
					// linking either one would risk a false attribution. Emit no
					// link and record the ambiguity for telemetry.
					if distinctEndpointCount(repoCands) > 1 {
						missesByReason["path_normalized_ambiguous"]++
						continue
					}
					p := repoCands[0]
					srcID := c.callerID
					if srcID == "" {
						srcID = c.stampedID
					}
					tgtID := p.handlerID
					if tgtID == "" {
						tgtID = p.stampedID
					}
					source := entityKey(c.repo, srcID)
					target := entityKey(p.repo, tgtID)
					id := MakeID(source, target, MethodHTTP)
					if emitted[id] {
						matchedConsumers[cKey] = true
						continue
					}
					emitted[id] = true
					matchedConsumers[cKey] = true
					hitsByStrategy["path_normalized"]++
					normKey, _ := pathNormResolve(consumerPath, p.canonicalPath)
					ident := canonicalIdentifier(c, p)
					ch := httpChannel
					link := Link{
						ID:           id,
						Source:       source,
						Target:       target,
						Relation:     RelationCalls,
						Method:       MethodHTTP,
						Confidence:   urlPatternNormConfidence,
						Channel:      &ch,
						Identifier:   &ident,
						DiscoveredAt: now,
						SourceLocations: [][]string{
							{c.sourceFile},
							{p.sourceFile},
						},
						MatchQuality: matchQualityAnyFallback,
						Properties: map[string]string{
							"resolve_strategy": "path_normalized",
							"normalized_path":  normKey,
						},
					}
					// #3752 — both endpoints are AST-grounded, but the match
					// discards the api/version prefix AND the param names: a
					// fuzzy structural equivalence, not a proven canonical-id
					// match. That is the `heuristic` honesty class.
					link.WithEdgeConfidence(ConfidenceHeuristic)
					fresh = append(fresh, link)
				}
			}
		}
	}

	// #2813 — dynamic-baseurl static-suffix orphan-retry sweep (PRIMARY
	// strategy). A consumer whose URL was built from a prop-drilled / env-
	// injected base (e.g. `axios.post(`${apiUrl}/schedule/import`)`) is emitted
	// with a `dynamic_baseurl` synthetic whose canonical path leads with a
	// `{placeholder}` segment (`/{apiurl}/schedule/import`). The base cannot be
	// resolved statically, but the STATIC SUFFIX carries enough signal: strip
	// the leading dynamic prefix, then fuzzy-match the suffix against the
	// backend endpoint set (reusing #2808's param/literal normalization via the
	// byPath / case-norm / literal-fill stages).
	//
	// Scoring = suffix specificity × candidate uniqueness:
	//   - exactly 1 verb-compatible producer candidate AND the suffix carries
	//     ≥ dynamicSuffixMinStaticSegments static segments → high confidence,
	//     auto-link with resolve_strategy = "dynamic_suffix_match".
	//   - multiple candidates OR a short/generic suffix → DO NOT guess. The
	//     consumer stays orphaned and is surfaced as a ranked residual by the
	//     per-repo dynamic_baseurl_endpoint enrichment candidate (#708), which
	//     archigraph-resolve consumes. We record the candidate set on the link-
	//     pass telemetry (residual_candidates) so the resolve surface can show
	//     candidate endpoints + confidence without re-deriving them.
	//
	// Genuinely-runtime values (e.g. `/{companyType}/{companyId}/branches/...`
	// where companyType is chosen at render) leave a param-led suffix after the
	// prefix strip; dynamicSuffixTemplate returns ok=false for those so they
	// stay tagged data-flow-runtime (the dynamic_baseurl miss bucket) and are
	// never force-linked. Running dead-last guarantees a consumer with any
	// exact/normalized producer was already matched by an earlier stage.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if matchedConsumers[cKey] {
					continue // resolved by an earlier stage
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				suffixKey, leadingStatic, ok := dynamicSuffixTemplate(consumerPath)
				if !ok {
					continue // not a static-suffix-resolvable dynamic baseURL
				}
				allConsumers[cKey] = true

				// Probe the byPath index for the suffix and its well-known
				// API/version-prefixed variants, restricted to verb-compatible
				// producers in OTHER repos. The prefixed probes let a bare
				// suffix `/schedule/import` find a producer mounted at
				// `/api/v1/schedule/import`.
				probeKeys := []string{suffixKey}
				if _, hasPfx := stripAPIPrefix(suffixKey); !hasPfx {
					for _, pfx := range crossRepoPrefixCandidates {
						probeKeys = append(probeKeys, normalizePathForIndex(pfx+suffixKey))
					}
				}
				var candidates []*httpEndpointHit
				seenCand := map[string]bool{}
				for _, pk := range probeKeys {
					for _, ph := range byPath[pk] {
						if ph.side != sideProducer || ph.repo == c.repo {
							continue
						}
						if !verbsCompatible(c.verb, ph.verb) {
							continue
						}
						pid := entityKey(ph.repo, ph.stampedID)
						if seenCand[pid] {
							continue
						}
						seenCand[pid] = true
						candidates = append(candidates, ph)
					}
				}
				if len(candidates) == 0 {
					// Specific suffix but no producer serves it — leave as a
					// dynamic_baseurl miss (data-flow-runtime / external). The
					// classification loop below counts it via classifyOrphanReason.
					continue
				}
				sort.SliceStable(candidates, func(i, j int) bool { return less(candidates[i], candidates[j]) })

				// Specificity × uniqueness gate. Auto-link only when exactly one
				// candidate AND the suffix is specific enough. Otherwise emit a
				// ranked residual for archigraph-resolve and stop (no edge).
				if len(candidates) != 1 || leadingStatic < dynamicSuffixMinStaticSegments {
					residualCandidates += len(candidates)
					missesByReason["dynamic_baseurl"]++
					dynamicSuffixCounted[cKey] = true
					continue
				}

				p := candidates[0]
				srcID := c.callerID
				if srcID == "" {
					srcID = c.stampedID
				}
				tgtID := p.handlerID
				if tgtID == "" {
					tgtID = p.stampedID
				}
				source := entityKey(c.repo, srcID)
				target := entityKey(p.repo, tgtID)
				id := MakeID(source, target, MethodHTTP)
				if emitted[id] {
					matchedConsumers[cKey] = true
					continue
				}
				emitted[id] = true
				matchedConsumers[cKey] = true
				hitsByStrategy["dynamic_suffix_match"]++
				ident := canonicalIdentifier(c, p)
				ch := httpChannel
				link := Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationCalls,
					Method:       MethodHTTP,
					Confidence:   ScoreImport(),
					Channel:      &ch,
					Identifier:   &ident,
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{c.sourceFile},
						{p.sourceFile},
					},
					MatchQuality: matchQualityAnyFallback,
					Properties: map[string]string{
						"resolve_strategy":  "dynamic_suffix_match",
						"dynamic_suffix":    suffixKey,
						"dynamic_prefix":    "stripped",
						"suffix_static_seg": strconv.Itoa(leadingStatic),
						// #3628 — the consumer URL was a runtime-dynamic
						// `${apiUrl}/x/y` expression; only the static suffix was
						// matched after stripping the runtime base. inferred.
						EdgeConfidenceKey: ConfidenceInferred,
					},
				}
				fresh = append(fresh, link)
			}
		}
	}

	// #2669: classify every unmatched consumer hit by likely miss reason.
	// We re-walk allConsumers (set in both the main loop and the retry sweep)
	// and exclude matchedConsumers to find the residual orphans.
	//
	// #2813: dynamic-baseurl consumers that the static-suffix sweep already
	// classified (ranked residual or no producer) are NOT re-counted here —
	// the suffix sweep incremented missesByReason["dynamic_baseurl"] for the
	// ranked-residual sub-case directly. We guard with dynamicSuffixCounted so
	// classifyOrphanReason does not double-count them.
	for _, byRepo := range hits {
		for _, perRepo := range byRepo {
			for _, c := range perRepo {
				if c.side != sideConsumer {
					continue
				}
				cKey := entityKey(c.repo, c.stampedID)
				if !allConsumers[cKey] || matchedConsumers[cKey] {
					continue
				}
				if dynamicSuffixCounted[cKey] {
					continue
				}
				consumerPath := c.canonicalPath
				if consumerPath == "" {
					if _, p, ok := parseHTTPName(c.name); ok {
						consumerPath = p
					}
				}
				missesByReason[classifyOrphanReason(consumerPath)]++
			}
		}
	}

	// #2585: replace both cross-repo (http) and intra-repo (http_self) entries
	// atomically so that removing all client calls from a repo cleans both sets.
	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodHTTP, MethodHTTPSelf), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped

	// #2571: derive OrphanCalls and CrossRepoResolved from per-pass state.
	// Both counters are computed from local maps that are zero-initialised
	// at every runHTTPPass entry, so they can never accumulate across runs.
	//
	// #2573: CrossRepoResolved is the single source of truth — it is the
	// count of consumer synthetics that had a link emitted this pass
	// (len(matchedConsumers)). OrphanCalls = total consumers seen minus
	// matched ones, ensuring orphan_calls ≤ total consumer entity count.
	res.CrossRepoResolved = len(matchedConsumers)
	res.OrphanCalls = len(allConsumers) - len(matchedConsumers)

	// #2669: surface the resolve-strategy telemetry. Empty maps are kept
	// as nil so JSON serialisation omits them rather than emitting `{}`,
	// which keeps the link-pass-stats payload compact in the common case
	// where the HTTP pass had no work to do (single-repo group, etc.).
	res.CrossRepoResolveAttempts = resolveAttempts
	if len(hitsByStrategy) > 0 {
		res.CrossRepoResolveHitsByStrategy = hitsByStrategy
	}
	if len(missesByReason) > 0 {
		res.CrossRepoResolveMissesByReason = missesByReason
	}
	// #2813: surface the ranked-residual candidate count for the resolve surface.
	res.ResidualCandidates = residualCandidates

	// Diagnostic logging: #2558 tracking of empty canonicalPath handling.
	// These counters help identify if the fallback path resolution is covering
	// consumer-side hits that would have been silently dropped in prior versions.
	if hitsProcessed > 0 {
		log.Printf("http_pass: hits_processed=%d, hits_canonical_empty=%d, links_registered=%d, orphan_calls=%d, cross_repo_resolved=%d",
			hitsProcessed, hitsCanonicalEmpty, len(fresh), res.OrphanCalls, res.CrossRepoResolved)
	}

	return res, nil
}

// splitSidesAcrossRepos partitions every hit under `byRepo` into
// producer / consumer slices. Hits with sideUnknown were already
// re-classified by runHTTPPass before reaching here (default →
// producer).
func splitSidesAcrossRepos(byRepo map[string][]*httpEndpointHit) (producers, consumers []*httpEndpointHit) {
	for _, hh := range byRepo {
		for _, h := range hh {
			switch h.side {
			case sideProducer:
				producers = append(producers, h)
			case sideConsumer:
				consumers = append(consumers, h)
			}
		}
	}
	return
}

// less is the deterministic ordering for cross-repo hit selection.
func less(a, b *httpEndpointHit) bool {
	if a.repo != b.repo {
		return a.repo < b.repo
	}
	if a.sourceFile != b.sourceFile {
		return a.sourceFile < b.sourceFile
	}
	return a.stampedID < b.stampedID
}

// appendUnique appends h to hh only if no existing entry has the same
// stampedID (within the same repo).
func appendUnique(hh []*httpEndpointHit, h *httpEndpointHit) []*httpEndpointHit {
	for _, x := range hh {
		if x.repo == h.repo && x.stampedID == h.stampedID {
			return hh
		}
	}
	return append(hh, h)
}

// sortedKeys returns the deterministic sorted key list of m.
func sortedKeys(m map[string]*httpEndpointHit) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// parseHTTPName parses `http:<VERB>:<path>` into its parts.
// Returns ok=false when the input doesn't have that shape.
func parseHTTPName(name string) (verb, path string, ok bool) {
	const prefix = "http:"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	rest := name[len(prefix):]
	i := strings.IndexByte(rest, ':')
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// verbsCompatible reports whether two verbs may refer to the same
// logical endpoint. ANY (case-insensitive) matches everything; otherwise
// the verbs must match case-insensitively.
func verbsCompatible(a, b string) bool {
	a = strings.ToUpper(a)
	b = strings.ToUpper(b)
	if a == "ANY" || b == "ANY" {
		return true
	}
	return a == b
}

// canonicalIdentifier picks the most specific verb available across
// the two sides when emitting the link's `identifier` field. If one
// side has verb=ANY and the other has GET (etc.), prefer the specific
// verb. Falls back to the consumer's verb otherwise.
func canonicalIdentifier(c, p *httpEndpointHit) string {
	verb := strings.ToUpper(c.verb)
	pVerb := strings.ToUpper(p.verb)
	if verb == "ANY" && pVerb != "" {
		verb = pVerb
	}
	if verb == "" {
		verb = pVerb
	}
	path := c.canonicalPath
	if path == "" {
		path = p.canonicalPath
	}
	return "http:" + verb + ":" + path
}

// Match-quality labels written to Link.MatchQuality.
const (
	matchQualityExactVerb   = "exact_verb"
	matchQualityAnyFallback = "any_fallback"
	matchQualityWildcard    = "wildcard"
)

// pickProducerForConsumer selects the best producer for a consumer hit
// from a single producer-repo's candidate pool. See #747.
//
// Selection tiers (first non-empty wins):
//  1. Producer with EXACT same specific verb as consumer (exact_verb).
//  2. Producer with verb=ANY (any_fallback) — Django ViewSets without
//     per-method routing legitimately emit ANY-verb endpoints; the
//     pre-fix matcher was right to consider them, just not to pick a
//     mismatched specific verb in preference.
//  3. If consumer itself is ANY, fall back to first producer (wildcard).
//  4. Otherwise: no match. We deliberately do NOT cross-link to a
//     different specific verb — that is the verb-confusion bug.
//
// `candidates` MUST already be sorted by `less()` so tier-internal tie
// breaking is deterministic.
func pickProducerForConsumer(c *httpEndpointHit, candidates []*httpEndpointHit) (*httpEndpointHit, string) {
	if len(candidates) == 0 {
		return nil, ""
	}
	cVerb := strings.ToUpper(c.verb)
	// Tier 1: exact specific-verb match.
	if cVerb != "" && cVerb != "ANY" {
		for _, p := range candidates {
			if strings.ToUpper(p.verb) == cVerb {
				return p, matchQualityExactVerb
			}
		}
		// Tier 2: ANY-verb fallback.
		for _, p := range candidates {
			if strings.ToUpper(p.verb) == "ANY" {
				return p, matchQualityAnyFallback
			}
		}
		// Tier 3: no match. Drop rather than pick a different specific
		// verb — that is exactly the bug #747 fixes.
		return nil, ""
	}
	// Consumer verb is ANY (or empty). Prefer an ANY-verb producer
	// when one exists (wildcard-on-wildcard); otherwise take the
	// first specific-verb producer (consumer is unspecified, so
	// any specific verb is a defensible match).
	for _, p := range candidates {
		if strings.ToUpper(p.verb) == "ANY" {
			return p, matchQualityWildcard
		}
	}
	return candidates[0], matchQualityWildcard
}

// dynamicBaseURLRe matches a leading `/{ident}` segment whose name doesn't
// canonically belong to a path-parameter position. Used by classifyOrphanReason
// to detect consumer paths like `/{apiUrl}/things` or `/{base_url.rstrip(`
// where the entire base of the URL is a template expression — these can
// never resolve without the runtime binding context that flowed into the
// template. (#2669)
var dynamicBaseURLRe = regexp.MustCompile(`^/\{[^}]*\}(?:/|$)`)

// classifyOrphanReason returns the stable miss-reason taxonomy key for a
// consumer path that the HTTP pass could not resolve. Two buckets are
// recognised today:
//
//   - "dynamic_baseurl"  — first segment is a `{template}` expression
//     (e.g. `/{apiUrl}/things`, `/{base_url.rstrip(`)
//     or the path is otherwise malformed by a partial
//     string-template extraction.
//   - "no_endpoint_match" — the path looks well-formed but no producer in
//     any other repo serves it. The dominant case for
//     upvate is calls to external services (third-
//     party APIs, Cognito JWKS, NYC OpenData, etc.).
//
// The classification is deliberately conservative: anything ambiguous falls
// into "no_endpoint_match" so the bucket reflects what an operator can act
// on (add the producer, fix the prefix) versus what's structurally outside
// the group's resolution domain.
func classifyOrphanReason(consumerPath string) string {
	if consumerPath == "" {
		return "no_endpoint_match"
	}
	// Anything containing an unclosed `{` (e.g. partial template extraction
	// like `/{base_url.rstrip(`) is a dynamic_baseurl miss.
	if strings.ContainsRune(consumerPath, '{') &&
		strings.Count(consumerPath, "{") != strings.Count(consumerPath, "}") {
		return "dynamic_baseurl"
	}
	if dynamicBaseURLRe.MatchString(consumerPath) {
		return "dynamic_baseurl"
	}
	return "no_endpoint_match"
}

// detectMountPrefix returns the discovered mount-prefix that bridges a
// consumer call to the chosen producer, or "" when the pair shares the same
// canonical path (i.e. no prefix bridging needed). The check normalises both
// sides via normalizePathForIndex and walks the producer's repo-local
// mount-prefix set in longest-first order so /internal/v3/ wins over
// /internal/ when both were declared.
//
// Used by the post-pick attribution step in runHTTPPass to detect when the
// mount-prefix wildcarding step (rather than the per-consumer retry) is
// responsible for surfacing the producer, so the link can be stamped with
// resolve_strategy=mount_prefix_added even though `p` was already non-nil
// out of pickProducerForConsumer.
func detectMountPrefix(c, p *httpEndpointHit, discovered map[string]bool) string {
	if c == nil || p == nil || len(discovered) == 0 {
		return ""
	}
	consumerPath := c.canonicalPath
	if consumerPath == "" {
		if _, parsed, ok := parseHTTPName(c.name); ok {
			consumerPath = parsed
		}
	}
	producerPath := p.canonicalPath
	if producerPath == "" {
		if _, parsed, ok := parseHTTPName(p.name); ok {
			producerPath = parsed
		}
	}
	if consumerPath == "" || producerPath == "" {
		return ""
	}
	cNorm := normalizePathForIndex(consumerPath)
	pNorm := normalizePathForIndex(producerPath)
	if cNorm == pNorm {
		return ""
	}
	if _, hasPrefix := stripAPIPrefix(cNorm); hasPrefix {
		return ""
	}
	for _, pfx := range mountPrefixesForRepo(discovered) {
		candidate := normalizePathForIndex(pfx + strings.TrimPrefix(cNorm, "/"))
		if candidate == pNorm {
			return pfx
		}
	}
	return ""
}

// normalizeMountPrefix coerces a raw `url_prefix` / `path` value emitted on
// a url_mount_point synthetic into the canonical "/<segments>/" shape used
// by mountPrefixesForRepo and the retry sweep (#2702). Returns "" when the
// value is empty, the root "/", or otherwise unusable. Lower-casing keeps
// the shape aligned with normalizePathForIndex so the prepended candidate
// can be probed against byPath without further canonicalisation.
func normalizeMountPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "/" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw) + "/"
}

// mountPrefixesForRepo returns the deduplicated, deterministically ordered
// list of mount-prefix candidates to try for a given producer repo. The
// discovered url_mount_point prefixes come first (longest-first so a
// `/api/v1/` mount wins over a coexisting `/api/`), followed by the static
// fallbacks (#2702) for any prefix the discovered set did not already cover.
func mountPrefixesForRepo(discovered map[string]bool) []string {
	out := make([]string, 0, len(discovered)+len(staticMountPrefixFallbacks))
	seen := map[string]bool{}
	for p := range discovered {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	// Longest-first ensures `/api/v1/` is tried before `/api/` when both
	// were discovered, mirroring the ordering invariant on
	// crossRepoPrefixCandidates. Ties break alphabetically so the order
	// stays deterministic across runs.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	for _, p := range staticMountPrefixFallbacks {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// splitKindNameRef parses "<Kind>:<Name>" into (kind, name). The Kind
// never contains a colon by construction of the synthesis pass.
func splitKindNameRef(ref string) (kind, name string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
