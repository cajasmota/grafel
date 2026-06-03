// Endpoint API-version + deprecation stamping (epic #3628).
//
// Two language-agnostic enrichment passes that run at the tail of
// applyHTTPEndpointSynthesis, AFTER every per-language route synthesizer has
// emitted its http_endpoint_definition entities for the current file. Both
// mutate Properties on the just-emitted producer endpoints in place — they
// never add or remove entities, so they cannot regress upstream synthesis.
//
// They answer two graph questions the endpoint surface could not previously
// answer:
//
//   - "What is on v1 vs v2?"        → api_version property (from the route path)
//   - "Which endpoints are deprecated?" → deprecated / deprecated_since /
//     deprecated_replacement properties
//
// Design (mirrors the #3696 auth-protection stamping in
// http_endpoint_jsts_auth.go): resolve a flat property contract from the
// route's OWN path + the annotation/decorator that decorates its handler in
// the source file, and stamp it onto the EXISTING endpoint op entity. No
// parallel deprecation node is created (the pre-existing _cross_deprecation
// extractor emits standalone SCOPE.Pattern markers; this pass instead projects
// the SAME semantic onto the routable endpoint so the endpoint surface is
// self-describing).
//
// HONEST-PARTIAL: an ambiguous or dynamic version segment yields no
// api_version; an endpoint with no deprecation marker carries no `deprecated`
// property at all (we never fabricate `deprecated=false`).
//
// Property contract stamped on http_endpoint_definition:
//
//	api_version            — "1" | "2" | … (the numeric version, no leading 'v')
//	deprecated             — "true"  (present only when a marker was found)
//	deprecated_since       — version/date string from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//
// Refs #3628.
package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// API version (path-derived) — language-agnostic
// ---------------------------------------------------------------------------

// endpointAPIVersionPatterns recognise an explicit API version SEGMENT in a
// canonical route path. Priority is first-match-wins. The trailing `(?:/|$)`
// anchor is what keeps `/apiv2something` (no segment boundary) from matching —
// a version is only a version when it is its own path segment.
var endpointAPIVersionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)/api/v(\d+)(?:/|$)`),
	regexp.MustCompile(`(?i)/v(\d+)(?:/|$)`),
}

const (
	endpointAPIVersionMin = 1
	endpointAPIVersionMax = 99
)

// applyEndpointAPIVersion stamps `api_version` on every producer endpoint at
// index >= before in `entities` that belongs to `path`, deriving the version
// from the endpoint's own canonical `path` property. Honest-partial: a path
// with no explicit version segment (or an out-of-range number) is left
// untouched — no api_version property is added.
func applyEndpointAPIVersion(lang, content string, entities []types.EntityRecord, path string, before int) {
	if before < 0 || before >= len(entities) {
		return
	}
	normLang := normaliseDeprecationLang(lang)
	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		routePath := e.Properties["path"]
		if routePath == "" {
			continue
		}
		if v, ok := apiVersionFromPath(routePath); ok {
			e.Properties["api_version"] = strconv.Itoa(v)
			continue
		}
		// Honest-partial fallback: when the route path carries no explicit
		// version segment, a language-specific declarative version attribute can
		// still pin the version. ASP.NET Core's [ApiVersion("2.0")] (Asp.Versioning)
		// on the controller/action sets the version even though the conventional
		// route is `api/[controller]` with no /vN segment (#3628 C# port).
		if normLang == "csharp" && content != "" {
			if v, ok := csharpAPIVersionFromAttribute(content, e); ok {
				e.Properties["api_version"] = strconv.Itoa(v)
			}
		}
	}
}

// apiVersionFromPath returns the numeric API version named by an explicit
// version segment in path, and whether one was found in range.
func apiVersionFromPath(path string) (int, bool) {
	for _, re := range endpointAPIVersionPatterns {
		m := re.FindStringSubmatch(path)
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false
		}
		if v < endpointAPIVersionMin || v > endpointAPIVersionMax {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Deprecation (handler-annotation derived) — per-language signals
// ---------------------------------------------------------------------------

// deprecationVerdict is the resolved deprecation state for one endpoint.
type deprecationVerdict struct {
	deprecated  bool
	since       string
	replacement string
	source      string // evidence: which signal fired
}

// applyEndpointDeprecation stamps deprecation properties on every producer
// endpoint at index >= before in `entities` that belongs to `path`. The
// deprecation signal is resolved from the source file region that decorates
// the endpoint's handler.
func applyEndpointDeprecation(lang, content, path string, entities []types.EntityRecord, before int) {
	if content == "" || before < 0 || before >= len(entities) {
		return
	}
	normLang := normaliseDeprecationLang(lang)

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		v := resolveEndpointDeprecation(normLang, content, e)
		if !v.deprecated {
			continue
		}
		e.Properties["deprecated"] = "true"
		if v.since != "" {
			e.Properties["deprecated_since"] = v.since
		}
		if v.replacement != "" {
			e.Properties["deprecated_replacement"] = v.replacement
		}
		if v.source != "" {
			e.Properties["deprecation_source"] = v.source
		}
	}
}

func normaliseDeprecationLang(lang string) string {
	low := strings.ToLower(lang)
	switch low {
	case "typescript", "javascript_typescript":
		return "javascript"
	case "kotlin":
		return "java"
	}
	return low
}

// resolveEndpointDeprecation inspects the source region that decorates the
// endpoint's handler for a deprecation marker. The region is the contiguous
// block of decorator / annotation / comment lines immediately ABOVE the
// handler declaration (StartLine, set by the synthesizers to the def line),
// which is exactly where @deprecated / @Deprecated / deprecated=True / a
// `// DEPRECATED` comment / a `.. deprecated::` docstring line live. A
// Sunset/Deprecation response-header write is searched in the handler body.
func resolveEndpointDeprecation(lang, content string, e *types.EntityRecord) deprecationVerdict {
	// Primary anchor: the handler def line (StartLine), set by the decorator-
	// based synthesizers (FastAPI, Flask, DRF, …). When it is unset (0) — as for
	// the Express/Spring composed passes that emit with plain emit — fall back to
	// the route's declaration line, located by its own path literal in the source.
	anchorLine := e.StartLine
	if anchorLine <= 0 {
		anchorLine = routeDeclarationLine(content, e.Properties["path"], e.Properties["verb"])
	}
	region, handlerStart := handlerDecoratorRegion(content, anchorLine)

	switch lang {
	case "javascript":
		if v, ok := jsDeprecationVerdict(region); ok {
			return v
		}
	case "java":
		if v, ok := javaDeprecationVerdict(region); ok {
			return v
		}
		// Javalin documents deprecation via the @OpenApi(deprecated = true) flag
		// rather than the standard @Deprecated annotation (#3858).
		if v, ok := reactiveDeprecationVerdict(region); ok {
			return v
		}
	case "python":
		if v, ok := pythonDeprecationVerdict(region, content, handlerStart); ok {
			return v
		}
	case "go":
		// Go endpoints emit with StartLine=0 and a group prefix that is not
		// composed into the canonical path, so neither the decorator-region nor
		// the path-anchored fallback above locates the handler func. Resolve the
		// handler func by NAME from the endpoint's `source_handler` ref instead,
		// then read the `// Deprecated:` godoc above it and scan its OWN body for
		// a Sunset/Deprecation header (no cross-handler leak). #4094.
		if v, ok := goDeprecationVerdict(content, e); ok {
			return v
		}
		return deprecationVerdict{}
	case "ruby":
		// Ruby (Sinatra) marks a deprecated verb block with a YARD `# @deprecated`
		// tag or a `# Deprecated:` doc comment immediately above the `get '/x' do`
		// line (the handler-decorator region). Rails controller-action comments
		// live in a separate file and are honest-partial (#3628 Ruby port).
		if v, ok := rubyDeprecationVerdict(region); ok {
			return v
		}
	case "csharp":
		// ASP.NET Core marks a deprecated action/controller with the standard
		// [Obsolete("…")] attribute, the ApiExplorer [Deprecated] attribute, or
		// an [ApiVersion(..., Deprecated = true)] flag (#3628 C# port).
		if v, ok := csharpDeprecationVerdict(region); ok {
			return v
		}
	default:
		// Generic cross-language fallback: a leading-comment DEPRECATED marker
		// is recognisable in every language's decorator/comment region.
		if v, ok := genericCommentDeprecationVerdict(region); ok {
			return v
		}
	}

	// Cross-language: a Sunset / Deprecation response header set in the handler
	// body is a runtime deprecation signal regardless of language.
	if v, ok := responseHeaderDeprecationVerdict(content, handlerStart); ok {
		return v
	}
	// Cross-language: a `// DEPRECATED` / `# DEPRECATED` comment in the
	// decorator region applies to every language.
	if v, ok := genericCommentDeprecationVerdict(region); ok {
		return v
	}
	return deprecationVerdict{}
}

// routeDeclarationLine returns the 1-based source line on which the route for
// `canonicalPath` is declared, or 0 if it cannot be located. The endpoint's
// canonical path is composed (e.g. Spring `/api/v1` + `@GetMapping("/old")` →
// `/api/v1/old`), so the source literal is matched on the route's LAST path
// segment, which is what the verb annotation / registration call carries. The
// match is anchored on a quoted occurrence of the segment to avoid colliding
// with the same token elsewhere in prose.
func routeDeclarationLine(content, canonicalPath, _ string) int {
	seg := lastPathSegment(canonicalPath)
	if seg == "" {
		return 0
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if routeLineMatchesSegment(line, seg) {
			return i + 1 // 1-based
		}
	}
	return 0
}

// lastPathSegment returns the final non-empty, non-parameter path segment of a
// canonical path. Parameter segments (`{id}`, `:id`, `<id>`) are skipped — the
// verb annotation conventionally anchors on a literal segment, and a parameter
// placeholder is too generic to locate reliably.
func lastPathSegment(p string) string {
	parts := strings.Split(p, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(parts[i])
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, ":") || strings.HasPrefix(s, "<") {
			continue
		}
		return s
	}
	return ""
}

// routeLineMatchesSegment reports whether a source line declares a route whose
// path literal ends in `seg` — i.e. the segment appears quoted, immediately
// preceded by a `/` or a quote (so `"/old"`, `"/orders/old"`, `'/v1/old'`
// match but the bare word `old` in a comment does not).
func routeLineMatchesSegment(line, seg string) bool {
	for _, q := range []string{`"`, `'`, "`"} {
		// "<seg>"  or  "/<seg>"  at the end of a quoted path literal.
		if idx := strings.Index(line, "/"+seg+q); idx >= 0 {
			return true
		}
		if idx := strings.Index(line, q+seg+q); idx >= 0 {
			return true
		}
	}
	return false
}

// handlerDecoratorRegion returns (decorator-region, handler-line-byte-offset)
// for a handler at 1-based line `startLine`. The region is the contiguous run
// of decorator (`@`), attribute (`#[`/`[`), and comment (`//`, `#`, `*`, `/**`)
// lines immediately above the handler line, PLUS the handler signature line
// itself (so an inline `@Deprecated` on the same line as the method, and the
// signature, are both in scope). handlerStart is the byte offset of the handler
// line, used to scope a response-header body scan.
func handlerDecoratorRegion(content string, startLine int) (string, int) {
	if startLine <= 0 {
		return "", 0
	}
	lines := strings.Split(content, "\n")
	if startLine > len(lines) {
		startLine = len(lines)
	}
	// 1-based startLine → 0-based index of the handler line.
	hIdx := startLine - 1
	if hIdx < 0 || hIdx >= len(lines) {
		return "", 0
	}
	// Walk upward collecting decorator/annotation/comment lines.
	top := hIdx
	for top > 0 {
		prev := strings.TrimSpace(lines[top-1])
		if prev == "" ||
			strings.HasPrefix(prev, "@") ||
			strings.HasPrefix(prev, "#[") ||
			strings.HasPrefix(prev, "[") ||
			strings.HasPrefix(prev, "//") ||
			strings.HasPrefix(prev, "#") ||
			strings.HasPrefix(prev, "*") ||
			strings.HasPrefix(prev, "/*") {
			top--
			continue
		}
		break
	}
	region := strings.Join(lines[top:hIdx+1], "\n")

	// Byte offset of the handler line start.
	off := 0
	for i := 0; i < hIdx && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	return region, off
}

// ---- JS / TS ---------------------------------------------------------------

// jsJSDocDeprecatedTagRe captures a JSDoc / line `@deprecated` tag with its
// optional trailing message (up to end of line / comment).
var jsJSDocDeprecatedTagRe = regexp.MustCompile(`@deprecated\b([^\n*]{0,200})`)

// jsOpenAPIDeprecatedRe captures an OpenAPI-style `deprecated: true` route flag.
var jsOpenAPIDeprecatedRe = regexp.MustCompile(`(?i)\bdeprecated\s*[:=]\s*true\b`)

func jsDeprecationVerdict(region string) (deprecationVerdict, bool) {
	if m := jsJSDocDeprecatedTagRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "jsdoc @deprecated"}
		msg := strings.TrimSpace(strings.Trim(m[1], " \t*/"))
		v.since, v.replacement = parseDeprecationMessage(msg)
		return v, true
	}
	if jsOpenAPIDeprecatedRe.MatchString(region) {
		return deprecationVerdict{deprecated: true, source: "deprecated: true"}, true
	}
	return deprecationVerdict{}, false
}

// ---- Java / Kotlin ---------------------------------------------------------

// javaDeprecatedAnnotationRe matches a standalone @Deprecated annotation
// (optionally with a since=/forRemoval= attribute list).
var javaDeprecatedAnnotationRe = regexp.MustCompile(`@Deprecated\b(?:\s*\(([^)]{0,200})\))?`)

// javaSinceAttrRe / javaJavadocSinceRe pull the deprecation since/replacement
// out of the annotation attribute list and the adjacent @deprecated Javadoc.
var javaSinceAttrRe = regexp.MustCompile(`since\s*=\s*"([^"]{0,80})"`)
var javaJavadocDeprecatedRe = regexp.MustCompile(`@deprecated\b([^\n*]{0,200})`)

func javaDeprecationVerdict(region string) (deprecationVerdict, bool) {
	m := javaDeprecatedAnnotationRe.FindStringSubmatch(region)
	if m == nil {
		return deprecationVerdict{}, false
	}
	v := deprecationVerdict{deprecated: true, source: "@Deprecated"}
	if m[1] != "" {
		if sm := javaSinceAttrRe.FindStringSubmatch(m[1]); sm != nil {
			v.since = sm[1]
		}
	}
	// The @deprecated Javadoc tag (lowercase) commonly carries the human
	// message with a "use X instead" replacement hint.
	if jm := javaJavadocDeprecatedRe.FindStringSubmatch(region); jm != nil {
		s, r := parseDeprecationMessage(strings.TrimSpace(strings.Trim(jm[1], " \t*/")))
		if v.since == "" {
			v.since = s
		}
		if r != "" {
			v.replacement = r
		}
	}
	return v, true
}

// ---- Python ----------------------------------------------------------------

// pyDeprecatedDecoratorRe matches a `@deprecated` decorator (typing_extensions /
// deprecated package), optionally `@deprecated("message")`.
var pyDeprecatedDecoratorRe = regexp.MustCompile(`@deprecated\b(?:\s*\(\s*["']([^"']{0,200})["'])?`)

// pyDRFDeprecatedKwargRe matches a DRF / drf-spectacular `deprecated=True`
// kwarg (on @extend_schema / a schema declaration in the decorator region).
var pyDRFDeprecatedKwargRe = regexp.MustCompile(`(?i)\bdeprecated\s*=\s*True\b`)

// pyDocstringDeprecatedRe matches a Sphinx `.. deprecated:: <version>`
// directive inside the handler docstring.
var pyDocstringDeprecatedRe = regexp.MustCompile(`\.\.\s*deprecated\s*::\s*([^\n]{0,80})`)

func pythonDeprecationVerdict(region, content string, handlerStart int) (deprecationVerdict, bool) {
	if m := pyDeprecatedDecoratorRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "@deprecated"}
		if m[1] != "" {
			v.since, v.replacement = parseDeprecationMessage(m[1])
		}
		return v, true
	}
	if pyDRFDeprecatedKwargRe.MatchString(region) {
		return deprecationVerdict{deprecated: true, source: "deprecated=True"}, true
	}
	// `.. deprecated:: <version>` in the handler docstring (the first lines of
	// the function body, just after handlerStart).
	body := pythonHandlerBody(content, handlerStart)
	if m := pyDocstringDeprecatedRe.FindStringSubmatch(body); m != nil {
		return deprecationVerdict{
			deprecated: true,
			since:      strings.TrimSpace(m[1]),
			source:     ".. deprecated::",
		}, true
	}
	return deprecationVerdict{}, false
}

// pythonHandlerBody returns a bounded window of source starting at handlerStart
// (the def line) covering the signature + docstring region. 1200 bytes is
// generous enough for a multi-line signature and a leading docstring without
// bleeding into sibling functions in the common case.
func pythonHandlerBody(content string, handlerStart int) string {
	if handlerStart < 0 || handlerStart >= len(content) {
		return ""
	}
	end := handlerStart + 1200
	if end > len(content) {
		end = len(content)
	}
	return content[handlerStart:end]
}

// ---- Go ---------------------------------------------------------------------

// goDeprecatedGodocRe matches Go's idiomatic deprecation marker — a
// `// Deprecated:` line in the doc comment block, with its optional trailing
// message (the convention recognised by `go vet`/`staticcheck`/pkg.go.dev).
var goDeprecatedGodocRe = regexp.MustCompile(`(?i)//\s*Deprecated:\s*([^\n]{0,200})`)

// goDeprecationVerdict resolves the deprecation state for a Go endpoint by
// locating its handler FUNC in the source (by the `source_handler` ref) rather
// than by the route registration line. It credits deprecated=true when:
//
//   - the handler func carries a `// Deprecated:` godoc comment immediately
//     above its `func` declaration (the Go-idiomatic marker), or
//   - the handler func body sets a `Sunset` / `Deprecation` response header
//     (RFC 8594) — scoped to THIS func's body so it never leaks to a sibling.
//
// Honest-partial: an endpoint whose handler cannot be located, or whose handler
// carries neither marker, yields no verdict (deprecated is left absent).
func goDeprecationVerdict(content string, e *types.EntityRecord) (deprecationVerdict, bool) {
	handler := goHandlerName(e)
	if handler == "" {
		return deprecationVerdict{}, false
	}
	re := goFuncOpenRe(handler)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return deprecationVerdict{}, false
	}
	// `// Deprecated:` godoc: the contiguous comment block immediately above the
	// func declaration line.
	doc := goDocCommentRegion(content, loc[0])
	if m := goDeprecatedGodocRe.FindStringSubmatch(doc); m != nil {
		v := deprecationVerdict{deprecated: true, source: "// Deprecated: godoc"}
		v.since, v.replacement = parseDeprecationMessage(strings.TrimSpace(m[1]))
		return v, true
	}
	// Response-header signal, scoped to THIS handler's own body.
	body := findGoHandlerBody(content, handler)
	if body != "" {
		if m := deprecationHeaderRe.FindStringSubmatch(body); m != nil {
			return deprecationVerdict{
				deprecated: true,
				source:     titleHeaderName(m[1]) + " response header",
			}, true
		}
	}
	return deprecationVerdict{}, false
}

// goHandlerName returns the bare handler function name recorded on a Go endpoint
// (`source_handler="Controller:<name>"`), or "" when unavailable. A qualified
// ref (`h.Create`) is reduced to its last component, matching the bare/last
// spelling the Go synthesizers record.
func goHandlerName(e *types.EntityRecord) string {
	ref := e.Properties["source_handler"]
	if ref == "" {
		return ""
	}
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		ref = ref[i+1:]
	}
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[i+1:]
	}
	return strings.TrimSpace(ref)
}

// goDocCommentRegion returns the contiguous run of `//` doc-comment lines (and
// blank-prefixed continuation comment lines) immediately preceding the func
// declaration that starts at byte offset funcOff. This is exactly where a Go
// `// Deprecated:` marker lives.
func goDocCommentRegion(content string, funcOff int) string {
	if funcOff <= 0 || funcOff > len(content) {
		return ""
	}
	lines := strings.Split(content[:funcOff], "\n")
	// The last element is the (partial) func line itself; walk upward over the
	// preceding comment lines.
	end := len(lines) - 1
	top := end
	for top > 0 {
		prev := strings.TrimSpace(lines[top-1])
		if strings.HasPrefix(prev, "//") {
			top--
			continue
		}
		break
	}
	if top >= end {
		return ""
	}
	return strings.Join(lines[top:end], "\n")
}

// ---- Cross-language: response-header signal --------------------------------

// deprecationHeaderRe matches a handler setting a `Sunset` or `Deprecation`
// HTTP response header (RFC 8594). Covers the common cross-framework shapes:
// `res.set('Sunset', …)`, `response.headers['Deprecation'] = …`,
// `setHeader("Sunset", …)`, `.header("Deprecation", …)`, Spring
// `.header(HttpHeaders.SUNSET, …)` and Django `response['Sunset'] = …`.
var deprecationHeaderRe = regexp.MustCompile(`(?i)["'\(\[]\s*(sunset|deprecation)\s*["'\)\]]`)

func responseHeaderDeprecationVerdict(content string, handlerStart int) (deprecationVerdict, bool) {
	body := handlerBodyWindow(content, handlerStart)
	if body == "" {
		return deprecationVerdict{}, false
	}
	if m := deprecationHeaderRe.FindStringSubmatch(body); m != nil {
		return deprecationVerdict{
			deprecated: true,
			source:     titleHeaderName(m[1]) + " response header",
		}, true
	}
	return deprecationVerdict{}, false
}

// titleHeaderName upper-cases the first rune of a lower-cased header name
// (e.g. "sunset" → "Sunset"), avoiding the deprecated strings.Title.
func titleHeaderName(s string) string {
	s = strings.ToLower(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// handlerBodyWindow returns a bounded window of the handler body starting at
// the def line. Kept small (1000 bytes) to avoid attributing a sibling
// handler's header write to this endpoint.
func handlerBodyWindow(content string, handlerStart int) string {
	if handlerStart < 0 || handlerStart >= len(content) {
		return ""
	}
	end := handlerStart + 1000
	if end > len(content) {
		end = len(content)
	}
	return content[handlerStart:end]
}

// ---- Cross-language: leading comment ---------------------------------------

// commentDeprecatedRe matches a `// DEPRECATED` / `# DEPRECATED` / `* DEPRECATED`
// banner comment (case-insensitive, word-boundary), with an optional trailing
// message.
var commentDeprecatedRe = regexp.MustCompile(`(?i)(?://|#|\*)\s*DEPRECATED\b([^\n]{0,200})`)

func genericCommentDeprecationVerdict(region string) (deprecationVerdict, bool) {
	if m := commentDeprecatedRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "comment // DEPRECATED"}
		v.since, v.replacement = parseDeprecationMessage(strings.TrimSpace(m[1]))
		return v, true
	}
	return deprecationVerdict{}, false
}

// ---- Shared message parsing ------------------------------------------------

// depSinceRe extracts a "since X" / "as of X" version/date from a free-text
// deprecation message.
var depSinceRe = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// depReplacementRe extracts a "use X instead" / "replaced by X" / "use X" hint.
var depReplacementRe = regexp.MustCompile(`(?i)\b(?:use|replaced by|prefer|see)\s+` + "`?" + `([A-Za-z0-9_./{}\-]+)` + "`?")

// parseDeprecationMessage pulls an optional since-version and replacement hint
// out of a free-text deprecation message. Both are honest-partial: an absent
// signal yields an empty string (never a fabricated value).
func parseDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := depSinceRe.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := depReplacementRe.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}
