package cpp

// endpoint_deprecation.go — endpoint API-version + deprecation stamping for
// C/C++ HTTP frameworks (#4147, child of #3628 Routing/endpoint_deprecation_versioning).
//
// C/C++ greenfield: prior to this pass every C/C++ HTTP-framework cell for
// endpoint_deprecation_versioning was `missing`. The flagship engine pass
// (internal/engine/http_endpoint_deprecation.go, resolveEndpointDeprecation)
// stamps the contract on synthesised `http_endpoint_definition` entities — but
// C++ HTTP endpoints are emitted as `SCOPE.Operation/endpoint` entities by the
// custom .cpp route extractors (drogon_routes.go `ADD_METHOD_TO`, crow_routes.go
// `CROW_ROUTE`, oatpp_routes.go `ENDPOINT(...)`, pistache `Routes::Get`, …), so
// the engine pass — gated on Kind==http_endpoint_definition — can never reach
// them. This is the SAME situation PHP Symfony/API-Platform (internal/custom/php),
// Kotlin Ktor (internal/custom/kotlin, #4139) and Scala (internal/custom/scala,
// #4144) faced; the resolution is identical: stamp the contract in the
// CUSTOM-EXTRACTOR stage from the framework's own idioms.
//
// C/C++ sibling of the Scala custom-stage deprecation (#4144) and the C++
// rate_limit.go marker pass (#4115). It emits a `SCOPE.Pattern/deprecation`
// marker per detected deprecation site carrying the IDENTICAL flat property
// contract the flagship stamps, so the graph answers "which C/C++ endpoints are
// deprecated, since when, replaced by what, and on which api version?":
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//	api_version            — "1" | "2" | … the numeric version (no leading 'v'),
//	                         from a /api/vN or /vN route path segment, when present
//
// Recognised C/C++ deprecation idioms (all three credit deprecated=true):
//
//	[[deprecated("use /api/v2/users")]] — the C++14 standard attribute on a route
//	    handler method/function. The single string argument yields the replacement
//	    hint and a `since X` fallback. The plain `[[deprecated]]` (no argument)
//	    still credits deprecated=true.
//	// DEPRECATED <msg> / // @deprecated <msg> — a banner line comment at the
//	    route (cross-language; the message yields since/replacement).
//	Sunset / Deprecation response header (RFC 8594) — `resp->addHeader("Sunset",
//	    …)`, `addHeader("Deprecation", …)`, `setHeader("Sunset", …)` set in a
//	    route response (a runtime deprecation signal).
//
// api_version is path-derived (language-agnostic, mirrors the flagship
// applyEndpointAPIVersion): a `/api/vN` or `/vN` segment in a quoted route
// literal (`"/api/v1/users"`) near the deprecation site.
//
// Marker model (mirrors rate_limit.go in this same package): the deprecation/
// version semantic is projected onto a `SCOPE.Pattern/deprecation` marker rather
// than mutating a route op — the deprecation signal (an attribute on the handler
// def, a banner comment, a header write) is not on the same line as the route
// macro, so there is no single composed endpoint op to attach to without
// fabricating a route binding. The marker carries the full contract as evidence;
// MergeWithCustom dedups by Name. One marker per deprecation site.
//
// Honest-partial (NEVER fabricated):
//   - a route handler with NO deprecation marker → no marker emitted;
//   - a deprecation marker with no resolvable since/replacement → those props
//     are omitted (only deprecated + deprecation_source stamped);
//   - a deprecation site whose enclosing route carries no explicit version
//     segment → no api_version.
//
// Honest scope note: C++ web-handler deprecation is materially less common than
// in the flagship languages — most C++ services version at the gateway / route
// path and rarely annotate handlers. This pass credits the frameworks whose
// route extractors the marker can anchor on (drogon, crow, oatpp, pistache,
// poco, restbed, restinio, cpprestsdk); raw event-loop libs (libuv/libev/
// libevent) and boost.asio have no route DSL to anchor on and stay missing.
//
// Refs #4147.

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_endpoint_deprecation", &cppEndpointDeprecationExtractor{})
}

type cppEndpointDeprecationExtractor struct{}

func (e *cppEndpointDeprecationExtractor) Language() string {
	return "custom_cpp_endpoint_deprecation"
}

var (
	// reCppDeprecatedAttr matches the C++14 `[[deprecated]]` / `[[deprecated("msg")]]`
	// standard attribute. Also tolerates the GNU `[[gnu::deprecated(...)]]` and
	// MSVC-ish `[[deprecated("msg")]]` spellings. Group 1 = the (optional) quoted
	// message argument.
	reCppDeprecatedAttr = regexp.MustCompile(`\[\[\s*(?:gnu::)?deprecated\b(?:\s*\(\s*"([^"]{0,300})"\s*\))?\s*\]\]`)

	// reCppDeprecatedComment matches a `// DEPRECATED` / `// @deprecated` /
	// `/* DEPRECATED` / `* @deprecated` banner comment (case-insensitive on the
	// keyword). Group 1 = the trailing message up to end-of-line.
	reCppDeprecatedComment = regexp.MustCompile(`(?i)(?://|/\*|\*)\s*@?deprecated\b([^\n*]{0,200})`)

	// reCppSunsetHeader matches a Sunset / Deprecation response-header write in a
	// C++ route response. Group 1 = the header name. Covers:
	//   resp->addHeader("Sunset", …) / setHeader("Deprecation", …)        (drogon/oatpp putHeader)
	//   response.headers().add("Sunset", …)                               (cpprestsdk)
	//   resp.headers().add<Http::Header::Raw>("Deprecation", …)           (pistache templated add)
	// The header name is matched as a quoted "sunset"/"deprecation" string preceded
	// by a header-write verb (add/set/put/insert/append, optionally with a
	// `<…>` template argument) OR by a `Header(` constructor.
	reCppSunsetHeader = regexp.MustCompile(
		`(?i)(?:(?:add|set|put|insert|append)\w*(?:\s*<[^>]*>)?|[Hh]eader)\s*\(\s*"(sunset|deprecation)"`)

	// reCppVersionLiteralAPI matches an `/api/vN` version segment inside a quoted
	// route literal. Group 1 = the numeric version.
	reCppVersionLiteralAPI = regexp.MustCompile(`(?i)/api/v(\d+)(?:/|"|$)`)

	// reCppVersionLiteralV matches a bare `/vN` version segment inside a quoted
	// route literal. Group 1 = the numeric version.
	reCppVersionLiteralV = regexp.MustCompile(`(?i)/v(\d+)(?:/|"|$)`)

	// reCppRouteAnchor marks a route-bearing region so a deprecation marker can
	// anchor on a genuine HTTP route (and a `[[deprecated]]` on a non-route helper
	// does not leak). Covers every C/C++ HTTP framework with a route DSL:
	//   drogon  — ADD_METHOD_TO / METHOD_ADD / registerHandler
	//   crow    — CROW_ROUTE / CROW_BP_ROUTE
	//   oatpp   — ENDPOINT("GET", ...)
	//   pistache— Routes::Get / Routes::Post / Routes::bind
	//   restbed — set_method_handler / Resource
	//   restinio— router->http_get / http_post
	//   poco    — HTTPRequestHandler / handleRequest
	//   cpprestsdk — support( / methods::GET
	reCppRouteAnchor = regexp.MustCompile(
		`(?:` +
			`\bADD_METHOD_TO\b|\bMETHOD_ADD\b|\bregisterHandler\b` + // drogon
			`|\bCROW_ROUTE\b|\bCROW_BP_ROUTE\b` + // crow
			`|\bENDPOINT\s*\(` + // oatpp
			`|\bRoutes::(?:Get|Post|Put|Delete|Patch|Head|Options|bind)\b` + // pistache
			`|\bset_method_handler\b` + // restbed
			`|\bhttp_(?:get|post|put|delete|patch|head|options)\b` + // restinio
			`|\bHTTPRequestHandler\b|\bhandleRequest\b` + // poco
			`|\bmethods::[A-Z]+\b|\bsupport\s*\(` + // cpprestsdk
			`)`)
)

const (
	cppAPIVersionMin = 1
	cppAPIVersionMax = 99
)

func (e *cppEndpointDeprecationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_endpoint_deprecation.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "cpp" {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: the file must mention a deprecation OR a Sunset/Deprecation
	// header idiom; otherwise there is nothing to stamp.
	if !strings.Contains(src, "deprecated") && !strings.Contains(src, "DEPRECATED") &&
		!strings.Contains(src, "Sunset") && !strings.Contains(src, "sunset") &&
		!strings.Contains(src, "Deprecation") {
		return nil, nil
	}

	framework := detectCPPFramework(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, m := range e.findDeprecationSites(src) {
		ln := lineOf(src, m.offset)
		name := "deprecation:" + m.source + ":" + strconv.Itoa(ln)
		ent := makeEntity(name, "SCOPE.Pattern", "deprecation", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", framework,
			"kind", "deprecation",
			"provenance", "INFERRED_FROM_CPP_DEPRECATION",
			"deprecated", "true",
			"deprecation_source", m.source,
		)
		if m.since != "" {
			ent.Properties["deprecated_since"] = m.since
		}
		if m.replacement != "" {
			ent.Properties["deprecated_replacement"] = m.replacement
		}
		if v, ok := cppAPIVersionNear(src, m.offset); ok {
			ent.Properties["api_version"] = strconv.Itoa(v)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// cppDepSite is a resolved deprecation site: the byte offset of the marker, the
// evidence source, and the honest-partial since/replacement.
type cppDepSite struct {
	offset      int
	source      string
	since       string
	replacement string
}

// findDeprecationSites scans the source for every route-relevant deprecation
// marker. The three signals are checked in priority order ([[deprecated]]
// attribute richest → banner comment → response header); each MATCH near a route
// anchor yields one site, de-duplicated by line so a richer signal wins.
func (e *cppEndpointDeprecationExtractor) findDeprecationSites(src string) []cppDepSite {
	var out []cppDepSite
	claimed := make(map[int]bool) // line numbers already claimed by a richer signal

	claim := func(off int) bool {
		ln := lineOf(src, off)
		if claimed[ln] {
			return false
		}
		claimed[ln] = true
		return true
	}

	// 1. [[deprecated("msg")]] standard attribute on a route handler. Gate on a
	//    route anchor within a small window so a [[deprecated]] on a non-route
	//    helper does not leak.
	for _, m := range reCppDeprecatedAttr.FindAllStringSubmatchIndex(src, -1) {
		if !cppNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		var msg string
		if m[2] >= 0 {
			msg = src[m[2]:m[3]]
		}
		since, repl := parseCppDeprecationMessage(msg)
		out = append(out, cppDepSite{offset: m[0], source: "[[deprecated]]", since: since, replacement: repl})
	}

	// 2. `// DEPRECATED` / `// @deprecated` banner comment at a route.
	for _, m := range reCppDeprecatedComment.FindAllStringSubmatchIndex(src, -1) {
		// Skip a comment occurrence that is actually inside a [[deprecated]]
		// attribute token already claimed above (the regex's `*`/`//` prefix
		// cannot precede a `[[`, so this is belt-and-suspenders via line claim).
		if !cppNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := strings.TrimSpace(strings.Trim(src[m[2]:m[3]], " \t*/"))
		since, repl := parseCppDeprecationMessage(msg)
		out = append(out, cppDepSite{offset: m[0], source: "comment // DEPRECATED", since: since, replacement: repl})
	}

	// 3. Sunset / Deprecation response header (RFC 8594). A runtime deprecation
	//    signal — credited regardless of an attribute. Gated on a route anchor so
	//    an unrelated `"Deprecation"` string elsewhere does not leak.
	for _, m := range reCppSunsetHeader.FindAllStringSubmatchIndex(src, -1) {
		if !cppNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		hdr := titleCppHeader(src[m[2]:m[3]])
		out = append(out, cppDepSite{offset: m[0], source: hdr + " response header"})
	}

	return out
}

// cppNearRouteAnchor reports whether a route anchor (any of the eight C/C++ HTTP
// framework route DSLs) appears within a small byte window around off. Keeps a
// `[[deprecated]]` / banner / header on a non-route surface from emitting a
// route-deprecation marker.
func cppNearRouteAnchor(src string, off int) bool {
	const win = 600
	lo := off - win
	if lo < 0 {
		lo = 0
	}
	hi := off + win
	if hi > len(src) {
		hi = len(src)
	}
	return reCppRouteAnchor.MatchString(src[lo:hi])
}

// cppAPIVersionNear resolves a path-derived api_version from the route region
// around a deprecation site. It scans a window around off for a `/api/vN` or
// `/vN` version segment in a quoted route literal. Honest-partial: no version
// segment → (0,false).
//
// The deprecation MESSAGE often names the replacement path
// (`[[deprecated("use /api/v2/users")]]`), which would otherwise be mistaken for
// the route's own version. We therefore scan the region for ALL `/api/vN` (then
// `/vN`) literals and pick the SMALLEST version present — the route's own
// (older) version is what is being deprecated, and any replacement named in the
// message points at a NEWER (larger) version. This honestly recovers the
// deprecated endpoint's api_version rather than the replacement's.
func cppAPIVersionNear(src string, off int) (int, bool) {
	const win = 600
	lo := off - win
	if lo < 0 {
		lo = 0
	}
	hi := off + win
	if hi > len(src) {
		hi = len(src)
	}
	region := src[lo:hi]

	if v, ok := cppMinVersion(reCppVersionLiteralAPI, region); ok {
		return v, true
	}
	if v, ok := cppMinVersion(reCppVersionLiteralV, region); ok {
		return v, true
	}
	return 0, false
}

// cppMinVersion returns the smallest valid version captured by re across region.
func cppMinVersion(re *regexp.Regexp, region string) (int, bool) {
	best := -1
	for _, m := range re.FindAllStringSubmatch(region, -1) {
		if v, ok := cppParseVersion(m[1]); ok {
			if best < 0 || v < best {
				best = v
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

func cppParseVersion(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	if v < cppAPIVersionMin || v > cppAPIVersionMax {
		return 0, false
	}
	return v, true
}

// titleCppHeader normalises a lower-cased header name to title case
// (sunset → Sunset, deprecation → Deprecation).
func titleCppHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- Shared message parsing (mirrors the flagship parseDeprecationMessage) ---

// reCppDepSince extracts a "since X" / "as of X" version/date from a free-text
// deprecation message.
var reCppDepSince = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// reCppDepReplacement extracts a "use X" / "replaced by X" / "prefer X" /
// "see X" replacement hint from a free-text message.
var reCppDepReplacement = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// parseCppDeprecationMessage pulls an optional since-version + replacement hint
// out of a free-text deprecation message. Both honest-partial (empty when
// absent, never fabricated). Mirrors the flagship parser exactly.
func parseCppDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := reCppDepSince.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := reCppDepReplacement.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}
