// endpoint_deprecation.go — endpoint API-version + deprecation stamping for
// Scala web frameworks (#4141, child of #3628 Routing/endpoint_deprecation_versioning).
//
// Scala greenfield: prior to this pass every scala framework cell for
// endpoint_deprecation_versioning was `missing`. The flagship engine pass
// (internal/engine/http_endpoint_deprecation.go, resolveEndpointDeprecation)
// stamps the contract on synthesised `http_endpoint_definition` entities — but
// Scala HTTP endpoints are emitted as `SCOPE.Operation` entities by the custom
// .scala extractors (akka.go `path(...)` / `route:METHOD`, tapir.go
// `tapir:METHOD:path`), so the engine pass — gated on
// Kind==http_endpoint_definition — can never reach them. This is the SAME
// situation PHP Symfony/API-Platform (internal/custom/php) and Kotlin Ktor
// (internal/custom/kotlin) faced; the resolution is identical: stamp the
// contract in the CUSTOM-EXTRACTOR stage from the framework's own idioms.
//
// Scala sibling of the PHP custom-stage deprecation (#3628) and the Scala
// rate_limit.go marker pass (#4105). It emits a `SCOPE.Pattern/deprecation`
// marker per detected deprecation site carrying the IDENTICAL flat property
// contract the flagship stamps, so the graph answers "which Scala endpoints are
// deprecated, since when, replaced by what, and on which api version?":
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//	api_version            — "1" | "2" | … the numeric version (no leading 'v'),
//	                         from a /api/vN or /vN route path segment, when present
//
// Recognised Scala deprecation idioms (all four credit deprecated=true):
//
//	@deprecated("use /api/v2/users", "2.0") — the Scala stdlib annotation. NOTE
//	    the Scala arg order is (message, since) — message FIRST, since SECOND —
//	    the OPPOSITE of Java's `@Deprecated(since=)`. The 2nd positional/`since=`
//	    arg is the version; the 1st `message` arg yields the replacement hint and
//	    a `since X` fallback. Sits on the route handler (an http4s
//	    `case GET -> Root / "v1" / "users"` branch, an akka/pekko verb directive,
//	    a tapir endpoint val, or the def that implements it).
//	@deprecated <msg>  — a Scaladoc tag in the `/** … */` comment block above the
//	    route handler (message yields since/replacement).
//	// DEPRECATED <msg> — a banner line comment at the route (cross-language).
//	Sunset / Deprecation response header (RFC 8594) — `Header("Sunset", …)`,
//	    `Header.Raw(ci"Deprecation", …)`, `headers = Headers("Sunset" -> …)` set
//	    in a route response (runtime deprecation signal).
//
// api_version is path-derived (language-agnostic, mirrors the flagship
// applyEndpointAPIVersion): a `/api/vN` or `/vN` segment in either a quoted
// route literal (`"/api/v1/users"`, scalatra/cask/play colon paths) or the
// http4s/akka/zio path-DSL (`Root / "api" / "v2"`, `path("v1" / "users")`).
//
// Marker model (mirrors rate_limit.go): like the Scala rate-limit pass, the
// deprecation/version semantic is projected onto a `SCOPE.Pattern/deprecation`
// marker rather than a per-route endpoint op — Scala's akka/http4s endpoints are
// emitted as un-composed `path(...)`/`route:METHOD` fragments (akka.go), so there
// is no single composed endpoint op to attach to without fabricating a route
// binding. The marker carries the full contract as evidence; MergeWithCustom
// folds on (SourceFile, Kind, Name) since #6104. One marker per deprecation site.
//
// Honest-partial (NEVER fabricated):
//   - a route handler with NO deprecation marker → no marker emitted;
//   - a deprecation marker with no resolvable since/replacement → those props
//     are omitted (only deprecated + deprecation_source stamped);
//   - a deprecation site whose enclosing route carries no explicit version
//     segment → no api_version.
//
// Refs #4141.
package scala

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_scala_endpoint_deprecation", &scalaEndpointDeprecationExtractor{})
}

type scalaEndpointDeprecationExtractor struct{}

func (e *scalaEndpointDeprecationExtractor) Language() string {
	return "custom_scala_endpoint_deprecation"
}

var (
	// reScalaDeprecatedAnno matches the Scala stdlib `@deprecated` annotation.
	// Group 1 = the (optional) parenthesised argument body. Scala's annotation is
	// `@deprecated(message, since)` — message FIRST, since SECOND (the reverse of
	// Java). A no-arg `@deprecated` (or `@deprecatedInheritance`/`@deprecatedName`
	// — excluded by the `\b` boundary below) still credits deprecated=true.
	reScalaDeprecatedAnno = regexp.MustCompile(`@deprecated\b(?:\s*\(([^)]{0,300})\))?`)

	// reScalaDeprecatedScaladoc matches a Scaladoc `@deprecated <message>` tag
	// (lowercase, inside a /** … */ block). Group 1 = the trailing message up to
	// end-of-line / next tag.
	reScalaDeprecatedScaladoc = regexp.MustCompile(`@deprecated\b([^\n*@]{1,200})`)

	// reScalaDeprecatedComment matches a `// DEPRECATED` / `/* DEPRECATED` /
	// `* DEPRECATED` banner comment (case-insensitive). Group 1 = trailing message.
	reScalaDeprecatedComment = regexp.MustCompile(`(?i)(?://|/\*|\*)\s*DEPRECATED\b([^\n*]{0,200})`)

	// reScalaSinceArg pulls an explicit `since = "X"` named arg out of a
	// @deprecated(...) body.
	reScalaSinceArg = regexp.MustCompile(`\bsince\s*=\s*"([^"]{0,80})"`)

	// reScalaMessageArg pulls an explicit `message = "X"` named arg out of a
	// @deprecated(...) body.
	reScalaMessageArg = regexp.MustCompile(`\bmessage\s*=\s*"([^"]{0,300})"`)

	// reScalaStringArg matches a quoted string-literal argument.
	reScalaStringArg = regexp.MustCompile(`"([^"]{0,300})"`)

	// reScalaSunsetHeader matches a Sunset / Deprecation response-header write in
	// a Scala route response. Covers `Header("Sunset", …)`, `Header.Raw(ci"Sunset",
	// …)`, `"Deprecation" -> …`, `Headers("Sunset" -> …)`, akka
	// `RawHeader("Sunset", …)`. Group 1 = the header name.
	reScalaSunsetHeader = regexp.MustCompile(`(?i)["'\(]?\s*(?:ci)?["']\s*(sunset|deprecation)\s*["']`)

	// reScalaVersionLiteral matches an `/api/vN` or `/vN` version segment inside a
	// quoted route literal. Group 1 (api/v form) or group 2 (bare /v form) = the
	// numeric version.
	reScalaVersionLiteralAPI = regexp.MustCompile(`(?i)/api/v(\d+)(?:/|"|$)`)
	reScalaVersionLiteralV   = regexp.MustCompile(`(?i)/v(\d+)(?:/|"|$)`)

	// reScalaVersionDSLAPI matches the http4s / akka path-DSL version segment
	// `"api" / "v2"` (api segment then a vN segment). Group 1 = numeric version.
	reScalaVersionDSLAPI = regexp.MustCompile(`(?i)"api"\s*/\s*"v(\d+)"`)

	// reScalaVersionDSLV matches a bare path-DSL version segment `"v1"`. Group 1 =
	// numeric version.
	reScalaVersionDSLV = regexp.MustCompile(`(?i)"v(\d+)"`)
)

const (
	scalaAPIVersionMin = 1
	scalaAPIVersionMax = 99
)

// depSiteFinder marks a route-bearing line so a deprecation marker can anchor on
// the route region around it. We treat as a route-anchor: an http4s
// `case ... -> ...` route branch, an akka/pekko verb directive (`get { … }`),
// a `path(...)` / `pathPrefix(...)` directive, a tapir `endpoint` builder, a
// scalatra/cask/play verb-with-path, and a `@deprecated`-annotated def.
var reScalaRouteAnchor = regexp.MustCompile(
	`(?m)(?:` +
		`case\s+\w+\s*->` + // http4s route branch: case GET -> Root / ...
		`|\b(?:get|post|put|delete|patch|head|options)\s*[\({]` + // akka verb directive / scalatra verb
		`|\bpath(?:Prefix|End|EndOrSingleSlash)?\s*\(` + // akka path directive
		`|@cask\.\w+` + // cask route
		`|\bendpoint\b` + // tapir endpoint builder
		`)`)

func (e *scalaEndpointDeprecationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_endpoint_deprecation.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
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

	framework := detectScalaFramework(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
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
			"provenance", "INFERRED_FROM_SCALA_DEPRECATION",
			"deprecated", "true",
			"deprecation_source", m.source,
		)
		if m.since != "" {
			ent.Properties["deprecated_since"] = m.since
		}
		if m.replacement != "" {
			ent.Properties["deprecated_replacement"] = m.replacement
		}
		if v, ok := scalaAPIVersionNear(src, m.offset); ok {
			ent.Properties["api_version"] = strconv.Itoa(v)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// depSite is a resolved deprecation site: the byte offset of the marker, the
// evidence source, and the honest-partial since/replacement.
type depSite struct {
	offset      int
	source      string
	since       string
	replacement string
}

// findDeprecationSites scans the source for every deprecation marker. The four
// signals are checked in priority order (annotation richest → Scaladoc → banner
// comment → response header); each MATCH yields one site. A `@deprecated`
// annotation and a sibling Scaladoc tag at the same place are de-duplicated by
// offset so the richer annotation wins.
func (e *scalaEndpointDeprecationExtractor) findDeprecationSites(src string) []depSite {
	var out []depSite
	claimed := make(map[int]bool) // line numbers already claimed by a richer signal

	claim := func(off int) bool {
		ln := lineOf(src, off)
		if claimed[ln] {
			return false
		}
		claimed[ln] = true
		return true
	}

	// 1. @deprecated annotation (Scala stdlib). Must be a route-relevant
	//    deprecation — gate on a route anchor within a small window so a
	//    @deprecated on a non-route helper class does not leak.
	for _, m := range reScalaDeprecatedAnno.FindAllStringSubmatchIndex(src, -1) {
		// Exclude @deprecatedName / @deprecatedInheritance / @deprecatedOverriding:
		// the `\b` after `deprecated` already blocks them (next char is a letter),
		// but FindAllStringSubmatchIndex on the optional group can still match the
		// bare `@deprecated` prefix of those — re-check the char right after.
		after := m[1]
		if after < len(src) {
			c := src[after]
			if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
				continue
			}
		}
		// A `@deprecated` occurrence that is actually a Scaladoc tag (inside a
		// /** … */ block) is handled by the Scaladoc pass below, which parses its
		// free-text message; skip it here so the richer message-parse wins.
		if scalaIsScaladocTag(src, m[0]) {
			continue
		}
		if !scalaNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		since, repl := scalaParseAnnoArgs(src, m)
		out = append(out, depSite{offset: m[0], source: "@deprecated", since: since, replacement: repl})
	}

	// 2. Scaladoc `@deprecated <msg>` tag inside a doc comment. Only when the
	//    matched occurrence is a doc tag (preceded on its line by a `*` or `/**`),
	//    not the annotation already claimed above.
	for _, m := range reScalaDeprecatedScaladoc.FindAllStringSubmatchIndex(src, -1) {
		if !scalaIsScaladocTag(src, m[0]) {
			continue
		}
		if !scalaNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := strings.TrimSpace(strings.Trim(src[m[2]:m[3]], " \t*/"))
		since, repl := parseScalaDeprecationMessage(msg)
		out = append(out, depSite{offset: m[0], source: "@deprecated scaladoc", since: since, replacement: repl})
	}

	// 3. `// DEPRECATED` banner comment at a route.
	for _, m := range reScalaDeprecatedComment.FindAllStringSubmatchIndex(src, -1) {
		if !scalaNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := strings.TrimSpace(strings.Trim(src[m[2]:m[3]], " \t*/"))
		since, repl := parseScalaDeprecationMessage(msg)
		out = append(out, depSite{offset: m[0], source: "comment // DEPRECATED", since: since, replacement: repl})
	}

	// 4. Sunset / Deprecation response header (RFC 8594). A runtime deprecation
	//    signal — credited regardless of an annotation. Gated on a route anchor so
	//    an unrelated `"Deprecation"` string elsewhere does not leak.
	for _, m := range reScalaSunsetHeader.FindAllStringSubmatchIndex(src, -1) {
		if !scalaNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		hdr := titleScalaHeader(src[m[2]:m[3]])
		out = append(out, depSite{offset: m[0], source: hdr + " response header"})
	}

	return out
}

// scalaParseAnnoArgs extracts since + replacement from a @deprecated(...) arg
// body. Scala arg order is (message, since): the FIRST positional string is the
// message (→ replacement hint + since fallback), the SECOND positional string is
// the since version. Named `message =` / `since =` args override positionally.
func scalaParseAnnoArgs(src string, m []int) (since, replacement string) {
	if m[2] < 0 { // no argument list (bare @deprecated)
		return "", ""
	}
	body := src[m[2]:m[3]]

	var message string
	// Named args take priority.
	if sm := reScalaSinceArg.FindStringSubmatch(body); sm != nil {
		since = sm[1]
	}
	if mm := reScalaMessageArg.FindStringSubmatch(body); mm != nil {
		message = mm[1]
	}
	// Positional fallback: first quoted = message, second quoted = since.
	if message == "" || since == "" {
		strs := reScalaStringArg.FindAllStringSubmatch(body, -1)
		if message == "" && len(strs) >= 1 {
			message = strs[0][1]
		}
		if since == "" && len(strs) >= 2 {
			since = strs[1][1]
		}
	}
	// The message yields the replacement hint, and a `since X` fallback when no
	// explicit since arg was given.
	s, r := parseScalaDeprecationMessage(message)
	replacement = r
	if since == "" {
		since = s
	}
	return since, replacement
}

// scalaNearRouteAnchor reports whether a route anchor (an http4s route branch,
// an akka/scalatra verb directive, a path directive, a tapir endpoint builder,
// or a cask route) appears within a small byte window around off. Keeps a
// `@deprecated` on a non-route helper from emitting a route-deprecation marker.
func scalaNearRouteAnchor(src string, off int) bool {
	const win = 600
	lo := off - win
	if lo < 0 {
		lo = 0
	}
	hi := off + win
	if hi > len(src) {
		hi = len(src)
	}
	return reScalaRouteAnchor.MatchString(src[lo:hi])
}

// scalaIsScaladocTag reports whether the `@deprecated` occurrence at off is a
// Scaladoc tag (its line, trimmed, starts with `*` or `/**`) rather than a
// code-level annotation.
func scalaIsScaladocTag(src string, off int) bool {
	ls := lineStartOffsetScala(src, off)
	prefix := strings.TrimSpace(src[ls:off])
	return strings.HasPrefix(prefix, "*") || strings.HasPrefix(prefix, "/**") || prefix == "*"
}

// lineStartOffsetScala returns the byte offset of the start of the line
// containing off.
func lineStartOffsetScala(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	i := strings.LastIndexByte(src[:off], '\n')
	if i < 0 {
		return 0
	}
	return i + 1
}

// scalaAPIVersionNear resolves a path-derived api_version from the route region
// around a deprecation site. It scans a window around off for a `/api/vN` or
// `/vN` version segment in either a quoted route literal or the http4s/akka path
// DSL (`"api" / "v2"`, `"v1"`). Honest-partial: no version segment → (0,false).
func scalaAPIVersionNear(src string, off int) (int, bool) {
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

	// The deprecation MESSAGE often names the replacement path
	// (`@deprecated("use /api/v2/users", …)`), which would otherwise be mistaken
	// for the route's own version. Scan the path-DSL form FIRST — `Root / "api" /
	// "vN"` and `path("vN" / …)` are unambiguously the route's structure, never
	// part of a quoted message — and only fall back to a quoted `/api/vN` literal
	// when no DSL version segment is present (covers scalatra/cask/play colon
	// paths whose route IS a quoted literal).
	if m := reScalaVersionDSLAPI.FindStringSubmatch(region); m != nil {
		if v, ok := scalaParseVersion(m[1]); ok {
			return v, true
		}
	}
	if m := reScalaVersionDSLV.FindStringSubmatch(region); m != nil {
		if v, ok := scalaParseVersion(m[1]); ok {
			return v, true
		}
	}
	// Quoted-literal /api/vN (route IS a string literal, e.g. scalatra
	// get("/api/v1/users")). Only reached when the DSL form is absent.
	if m := reScalaVersionLiteralAPI.FindStringSubmatch(region); m != nil {
		if v, ok := scalaParseVersion(m[1]); ok {
			return v, true
		}
	}
	if m := reScalaVersionLiteralV.FindStringSubmatch(region); m != nil {
		if v, ok := scalaParseVersion(m[1]); ok {
			return v, true
		}
	}
	return 0, false
}

func scalaParseVersion(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	if v < scalaAPIVersionMin || v > scalaAPIVersionMax {
		return 0, false
	}
	return v, true
}

// titleScalaHeader normalises a lower-cased header name to title case
// (sunset → Sunset, deprecation → Deprecation).
func titleScalaHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- Shared message parsing (mirrors the flagship parseDeprecationMessage) ---

// reScalaDepSince extracts a "since X" / "as of X" version/date from a free-text
// deprecation message.
var reScalaDepSince = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// reScalaDepReplacement extracts a "use X" / "replaced by X" / "prefer X"
// replacement hint from a free-text message.
var reScalaDepReplacement = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// parseScalaDeprecationMessage pulls an optional since-version + replacement hint
// out of a free-text deprecation message. Both honest-partial (empty when
// absent, never fabricated). Mirrors the flagship parser exactly.
func parseScalaDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := reScalaDepSince.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := reScalaDepReplacement.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}
