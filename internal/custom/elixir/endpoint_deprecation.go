// endpoint_deprecation.go — endpoint API-version + deprecation stamping for
// Elixir web frameworks (#4146, child of #3628 Routing/
// endpoint_deprecation_versioning).
//
// Elixir greenfield: prior to this pass every elixir framework cell for
// endpoint_deprecation_versioning was `missing`. The flagship engine pass
// (internal/engine/http_endpoint_deprecation.go, resolveEndpointDeprecation)
// stamps the contract on synthesised `http_endpoint_definition` entities — but
// Elixir HTTP endpoints are emitted as `SCOPE.Operation` entities by the custom
// .ex extractors (phoenix.go `get "/path"` → `METHOD path`, controller actions
// → `action:<name>`), so the engine pass — gated on
// Kind==http_endpoint_definition — can never reach them. This is the SAME
// situation Scala akka/http4s (internal/custom/scala/endpoint_deprecation.go,
// #4141), PHP Symfony/API-Platform (internal/custom/php), and Kotlin Ktor
// (internal/custom/kotlin) faced; the resolution is identical: stamp the
// contract in the CUSTOM-EXTRACTOR stage from the framework's own idioms.
//
// Elixir sibling of the Scala custom-stage deprecation (#4141) and the Elixir
// rate_limit.go marker pass (#4099). It emits a `SCOPE.Pattern/deprecation`
// marker per detected deprecation site carrying the IDENTICAL flat property
// contract the flagship stamps, so the graph answers "which Elixir endpoints are
// deprecated, since when, replaced by what, and on which api version?":
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//	api_version            — "1" | "2" | … the numeric version (no leading 'v'),
//	                         from a /api/vN or /vN route path segment, when present
//
// Recognised Elixir deprecation idioms (all credit deprecated=true):
//
//	@deprecated "use /api/v2/users" — the Elixir module attribute. Placed
//	    immediately above a Phoenix controller `def <action>(conn, _params)` or a
//	    Plug route handler. The free-text message yields the replacement hint
//	    (`use X`) and a `since X` version fallback. This is the canonical Elixir
//	    deprecation signal (the compiler emits a warning at the def below it).
//	@doc deprecated: "use the v2 endpoint" — the @doc-metadata form (keyword
//	    `deprecated:` inside an @doc attribute). Message parsed the same way.
//	# DEPRECATED <msg> — a banner line comment at the route (cross-language).
//	Sunset / Deprecation response header (RFC 8594) — a
//	    `put_resp_header(conn, "sunset", ...)` / `put_resp_header(conn,
//	    "deprecation", ...)` write in a route/action body (runtime deprecation
//	    signal).
//
// api_version is path-derived (language-agnostic, mirrors the flagship
// applyEndpointAPIVersion): a `/api/vN` or `/vN` segment in a quoted route or
// scope literal near the deprecation site (`scope "/api/v1"`, `get "/v2/users"`).
// In a Phoenix router the version usually lives on the enclosing `scope`, so the
// nearest quoted `/api/vN` or `/vN` literal in the surrounding window is used.
//
// Marker model (mirrors rate_limit.go extractPlugAttack): like the Elixir
// rate-limit PlugAttack pass, the deprecation/version semantic is projected onto
// a `SCOPE.Pattern/deprecation` marker rather than a per-route endpoint op. A
// Phoenix `@deprecated` attribute sits on a controller `def <action>` (the
// router route lives in a SEPARATE file), and a Sunset header is written in an
// action body — neither is the composed route op, so attaching the contract to a
// route would fabricate a binding. The marker carries the full contract as
// evidence; MergeWithCustom folds it onto the base op when (SourceFile, Kind,
// Name) all match (#6104). One marker per deprecation site.
//
// Honest-partial (NEVER fabricated):
//   - a route/action with NO deprecation marker → no marker emitted;
//   - a deprecation marker with no resolvable since/replacement → those props
//     are omitted (only deprecated + deprecation_source stamped);
//   - a deprecation site whose surrounding region carries no explicit version
//     segment → no api_version.
//
// Refs #4146.
package elixir

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
	extractor.Register("custom_elixir_endpoint_deprecation", &elixirEndpointDeprecationExtractor{})
}

type elixirEndpointDeprecationExtractor struct{}

func (e *elixirEndpointDeprecationExtractor) Language() string {
	return "custom_elixir_endpoint_deprecation"
}

var (
	// reElixirDeprecatedAttr matches the Elixir `@deprecated "<message>"` module
	// attribute. Group 1 = the quoted message. The compiler attribute form takes a
	// single string literal; a bare `@deprecated` with no string is also matched
	// (group 1 empty) and still credits deprecated=true.
	reElixirDeprecatedAttr = regexp.MustCompile(`@deprecated\b(?:\s+"([^"]{0,300})")?`)

	// reElixirDocDeprecated matches the `@doc deprecated: "<message>"` keyword
	// form (deprecation metadata inside an @doc attribute). Group 1 = the message.
	reElixirDocDeprecated = regexp.MustCompile(`@doc\s+deprecated:\s*"([^"]{0,300})"`)

	// reElixirDeprecatedComment matches a `# DEPRECATED` banner line comment at a
	// route (case-insensitive). Group 1 = the trailing message.
	reElixirDeprecatedComment = regexp.MustCompile(`(?i)#\s*DEPRECATED\b([^\n]{0,200})`)

	// reElixirSunsetHeader matches a Sunset / Deprecation response-header write via
	// Plug's `put_resp_header(conn, "sunset", ...)`. Group 1 = the header name.
	reElixirSunsetHeader = regexp.MustCompile(`(?i)put_resp_header\s*\(\s*[^,]+,\s*"(sunset|deprecation)"`)

	// reElixirVersionLiteralAPI matches an `/api/vN` version segment inside a
	// quoted route / scope literal. Group 1 = the numeric version.
	reElixirVersionLiteralAPI = regexp.MustCompile(`(?i)/api/v(\d+)(?:/|"|$)`)
	// reElixirVersionLiteralV matches a bare `/vN` version segment. Group 1 = the
	// numeric version.
	reElixirVersionLiteralV = regexp.MustCompile(`(?i)/v(\d+)(?:/|"|$)`)
)

const (
	elixirAPIVersionMin = 1
	elixirAPIVersionMax = 99
)

// reElixirRouteAnchor marks a route/action-bearing line so a deprecation marker
// anchors only on real endpoint surfaces. We treat as a route-anchor: a Phoenix
// router verb (`get "/..."`), a `resources "/..."` declaration, a `scope "/..."`
// block, a Phoenix controller action `def <action>(conn`, a generic
// `def <name>(conn` Plug handler, and a `match _` Plug route.
var reElixirRouteAnchor = regexp.MustCompile(
	`(?m)(?:` +
		`^\s*(?:get|post|put|patch|delete|options|head|live)\s+"` + // phoenix router verb
		`|^\s*resources\s+"` + // phoenix resources
		`|^\s*scope\s+"` + // phoenix scope block
		`|^\s*match\s+` + // plug router match
		`|\bdef\s+[a-z_][\w]*[?!]?\s*\(\s*conn\b` + // controller action / plug handler taking conn
		`)`)

func (e *elixirEndpointDeprecationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.elixir_endpoint_deprecation.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: the file must mention a deprecation OR a Sunset/Deprecation
	// header idiom; otherwise there is nothing to stamp.
	if !strings.Contains(src, "deprecated") && !strings.Contains(src, "DEPRECATED") &&
		!strings.Contains(src, "sunset") && !strings.Contains(src, "Sunset") &&
		!strings.Contains(src, "deprecation") && !strings.Contains(src, "Deprecation") {
		return nil, nil
	}

	framework := detectElixirDepFramework(src)

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
			"provenance", "INFERRED_FROM_ELIXIR_DEPRECATION",
			"deprecated", "true",
			"deprecation_source", m.source,
		)
		if m.since != "" {
			ent.Properties["deprecated_since"] = m.since
		}
		if m.replacement != "" {
			ent.Properties["deprecated_replacement"] = m.replacement
		}
		if v, ok := elixirAPIVersionNear(src, m.offset); ok {
			ent.Properties["api_version"] = strconv.Itoa(v)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// elixirDepSite is a resolved deprecation site: the byte offset of the marker,
// the evidence source, and the honest-partial since/replacement.
type elixirDepSite struct {
	offset      int
	source      string
	since       string
	replacement string
}

// findDeprecationSites scans the source for every deprecation marker. The four
// signals are checked in priority order (attribute richest → @doc deprecated: →
// banner comment → response header); each MATCH yields one site, de-duplicated
// by line so two signals on one line do not double-emit.
func (e *elixirEndpointDeprecationExtractor) findDeprecationSites(src string) []elixirDepSite {
	var out []elixirDepSite
	claimed := make(map[int]bool) // line numbers already claimed by a richer signal

	claim := func(off int) bool {
		ln := lineOf(src, off)
		if claimed[ln] {
			return false
		}
		claimed[ln] = true
		return true
	}

	// 1. @deprecated "<message>" module attribute. Gate on a route anchor within a
	//    small window so a @deprecated on a non-route helper does not leak. The
	//    @doc deprecated: form is handled separately below; skip a match whose
	//    preceding token is `@doc` (it is the @doc keyword form, not the attribute).
	for _, m := range reElixirDeprecatedAttr.FindAllStringSubmatchIndex(src, -1) {
		if elixirIsDocDeprecated(src, m[0]) {
			continue
		}
		if !elixirNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := ""
		if m[2] >= 0 {
			msg = src[m[2]:m[3]]
		}
		since, repl := parseElixirDeprecationMessage(msg)
		out = append(out, elixirDepSite{offset: m[0], source: "@deprecated", since: since, replacement: repl})
	}

	// 2. @doc deprecated: "<message>" keyword form.
	for _, m := range reElixirDocDeprecated.FindAllStringSubmatchIndex(src, -1) {
		if !elixirNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := src[m[2]:m[3]]
		since, repl := parseElixirDeprecationMessage(msg)
		out = append(out, elixirDepSite{offset: m[0], source: "@doc deprecated:", since: since, replacement: repl})
	}

	// 3. `# DEPRECATED` banner comment at a route.
	for _, m := range reElixirDeprecatedComment.FindAllStringSubmatchIndex(src, -1) {
		if !elixirNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		msg := strings.TrimSpace(src[m[2]:m[3]])
		since, repl := parseElixirDeprecationMessage(msg)
		out = append(out, elixirDepSite{offset: m[0], source: "comment # DEPRECATED", since: since, replacement: repl})
	}

	// 4. Sunset / Deprecation response header (RFC 8594). A runtime deprecation
	//    signal — credited regardless of an attribute. Gated on a route anchor so
	//    an unrelated `"deprecation"` string elsewhere does not leak.
	for _, m := range reElixirSunsetHeader.FindAllStringSubmatchIndex(src, -1) {
		if !elixirNearRouteAnchor(src, m[0]) {
			continue
		}
		if !claim(m[0]) {
			continue
		}
		hdr := titleElixirHeader(src[m[2]:m[3]])
		out = append(out, elixirDepSite{offset: m[0], source: hdr + " response header"})
	}

	return out
}

// elixirIsDocDeprecated reports whether the `@deprecated` occurrence at off is
// actually the `@doc deprecated:` keyword form (the `deprecated` token here is
// preceded on its line by `@doc`), which the dedicated @doc pass handles.
func elixirIsDocDeprecated(src string, off int) bool {
	ls := lineStartOffsetElixir(src, off)
	prefix := strings.TrimSpace(src[ls:off])
	return strings.HasSuffix(prefix, "@doc")
}

// lineStartOffsetElixir returns the byte offset of the start of the line
// containing off.
func lineStartOffsetElixir(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	i := strings.LastIndexByte(src[:off], '\n')
	if i < 0 {
		return 0
	}
	return i + 1
}

// elixirNearRouteAnchor reports whether a route/action anchor appears within a
// small byte window around off. Keeps a `@deprecated` on a non-route helper from
// emitting a route-deprecation marker.
func elixirNearRouteAnchor(src string, off int) bool {
	const win = 400
	lo := off - win
	if lo < 0 {
		lo = 0
	}
	hi := off + win
	if hi > len(src) {
		hi = len(src)
	}
	return reElixirRouteAnchor.MatchString(src[lo:hi])
}

// elixirAPIVersionNear resolves a path-derived api_version from the route region
// around a deprecation site. It scans a window around off for a `/api/vN` or
// `/vN` version segment in a quoted route / scope literal. To avoid mistaking the
// replacement path named in the deprecation MESSAGE (`@deprecated "use
// /api/v2/users"`) for the route's own version, the message line is excluded
// from the scanned region. Honest-partial: no version segment → (0,false).
func elixirAPIVersionNear(src string, off int) (int, bool) {
	const win = 400
	lo := off - win
	if lo < 0 {
		lo = 0
	}
	hi := off + win
	if hi > len(src) {
		hi = len(src)
	}
	region := src[lo:hi]

	// Exclude the deprecation site's own line from the scan: its message often
	// names a replacement path (`use /api/v2/users`) whose version is NOT the
	// route's current version. The route/scope version segment lives on a
	// SURROUNDING line (the enclosing `scope "/api/v1"` or the `get "/v1/..."`).
	siteLine := lineOf(src, off)
	var b strings.Builder
	regionStartLine := lineOf(src, lo)
	for i, ln := range strings.Split(region, "\n") {
		if regionStartLine+i == siteLine {
			continue
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	scan := b.String()

	if m := reElixirVersionLiteralAPI.FindStringSubmatch(scan); m != nil {
		if v, ok := elixirParseVersion(m[1]); ok {
			return v, true
		}
	}
	if m := reElixirVersionLiteralV.FindStringSubmatch(scan); m != nil {
		if v, ok := elixirParseVersion(m[1]); ok {
			return v, true
		}
	}
	return 0, false
}

func elixirParseVersion(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	if v < elixirAPIVersionMin || v > elixirAPIVersionMax {
		return 0, false
	}
	return v, true
}

// detectElixirDepFramework labels the deprecation marker by the web framework in
// the file. Phoenix is the default (router/controller idioms); a file that uses
// raw Plug routing without Phoenix is labelled `plug`.
func detectElixirDepFramework(src string) string {
	switch {
	case strings.Contains(src, "Phoenix") ||
		strings.Contains(src, "use MyAppWeb") ||
		regexp.MustCompile(`(?m)^\s*pipeline\s+:`).MatchString(src) ||
		regexp.MustCompile(`(?m)^\s*scope\s+"`).MatchString(src):
		return "phoenix"
	case strings.Contains(src, "use Plug.Router") || strings.Contains(src, "Plug.Conn"):
		return "plug"
	default:
		return "phoenix"
	}
}

// titleElixirHeader normalises a lower-cased header name to title case
// (sunset → Sunset, deprecation → Deprecation).
func titleElixirHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- Shared message parsing (mirrors the flagship parseDeprecationMessage) ---

// reElixirDepSince extracts a "since X" / "as of X" version/date from a
// free-text deprecation message.
var reElixirDepSince = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// reElixirDepReplacement extracts a "use X" / "replaced by X" / "prefer X"
// replacement hint from a free-text message.
var reElixirDepReplacement = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// parseElixirDeprecationMessage pulls an optional since-version + replacement
// hint out of a free-text deprecation message. Both honest-partial (empty when
// absent, never fabricated). Mirrors the flagship parser exactly.
func parseElixirDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := reElixirDepSince.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := reElixirDepReplacement.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}
