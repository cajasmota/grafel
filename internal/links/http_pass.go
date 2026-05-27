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

// caseNormalizePathSegments produces a canonical form of a path for
// case-normalized cross-repo matching (#2703). Each segment is lower-cased
// and stripped of `-` and `_` characters; path-parameter placeholders
// (already collapsed to `{*}` by normalizePathForIndex) are passed through
// unchanged.
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
		// Lower-case + strip `-` and `_` so camelCase, snake_case, kebab-case
		// and PascalCase all collapse to the same canonical id.
		var b strings.Builder
		b.Grow(len(seg))
		for _, r := range strings.ToLower(seg) {
			if r == '-' || r == '_' {
				continue
			}
			b.WriteRune(r)
		}
		segs[i] = b.String()
	}
	return strings.Join(segs, "/")
}

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

					// Case-normalization retry (#2703): when both the standard
					// byPath lookup AND the prefix-injection retry miss, attempt to
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
					// this strategy composes with #2702 (mount-prefix retry):
					// `/assignedContacts` consumer matches `/api/v1/.../assigned_contacts`
					// producer via the prefix-stripped alias that byCaseNorm
					// registers above.
					//
					// First match wins; edge is stamped with
					// Properties["resolve_strategy"] = "case_normalized".
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
									// strategy is case_normalized, not
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
					// #2669: bucket the hit by the strategy that resolved it.
					// Order matters: url_pattern + prefix_stripped are checked
					// before the default "exact" bucket because the main loop
					// reuses the same `p` variable across retry stages.
					if !intraRepo {
						switch {
						case urlPatternNormAnnotation != "":
							hitsByStrategy["url_pattern"]++
						case caseNormalized:
							hitsByStrategy["case_normalized"]++
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
					if caseNormalized {
						if link.Properties == nil {
							link.Properties = map[string]string{}
						}
						link.Properties["resolve_strategy"] = "case_normalized"
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
	// case_normalized strategy is preferred when both would match.
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
					hitsByStrategy["case_normalized"]++
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
						Properties:   map[string]string{"resolve_strategy": "case_normalized"},
					}
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
					fresh = append(fresh, link)
				}
			}
		}
	}

	// #2669: classify every unmatched consumer hit by likely miss reason.
	// We re-walk allConsumers (set in both the main loop and the retry sweep)
	// and exclude matchedConsumers to find the residual orphans.
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
//                          (e.g. `/{apiUrl}/things`, `/{base_url.rstrip(`)
//                          or the path is otherwise malformed by a partial
//                          string-template extraction.
//   - "no_endpoint_match" — the path looks well-formed but no producer in
//                           any other repo serves it. The dominant case for
//                           upvate is calls to external services (third-
//                           party APIs, Cognito JWKS, NYC OpenData, etc.).
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

// splitKindNameRef parses "<Kind>:<Name>" into (kind, name). The Kind
// never contains a colon by construction of the synthesis pass.
func splitKindNameRef(ref string) (kind, name string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
