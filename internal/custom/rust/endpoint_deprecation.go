// endpoint_deprecation.go — endpoint deprecation + API-version stamping for Rust
// web frameworks (#4152, child of epic #3628 cross-language fan-out, Routing/
// endpoint_deprecation_versioning).
//
// Rust greenfield: prior to this pass every Rust HTTP-framework cell for
// endpoint_deprecation_versioning was `missing`. The flagship engine pass
// (internal/engine/http_endpoint_deprecation.go, resolveEndpointDeprecation)
// stamps a flat property contract on synthesised `http_endpoint_definition`
// entities — but Rust HTTP endpoints are emitted as `SCOPE.Operation/endpoint`
// entities by the custom .rs route extractors (axum.go `.route("/p", verb(h))`,
// actix_web.go `#[get("/p")]` macros, rocket.go `#[get("/p")]` macros), so the
// engine pass — gated on Kind==http_endpoint_definition — can never reach them.
// This is the SAME situation Kotlin Ktor/Spring (internal/custom/kotlin,
// #4136), Scala akka/http4s (internal/custom/scala, #4141) and PHP Symfony
// (internal/custom/php) faced; the resolution is identical: re-emit the endpoint
// op carrying the contract in the CUSTOM-EXTRACTOR stage from the framework's
// own idioms, merging onto the producer route op by Name.
//
// Property contract (mirrors the flagship http_endpoint_deprecation.go EXACTLY):
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//	api_version            — "1" | "2" | … numeric version from the route path
//
// Three recognised Rust surfaces (Names match the producer extractors so the
// stamped op merges onto the plain route op via MergeWithCustom, which keys on
// (SourceFile, Kind, Name) since #6104):
//
//	axum — `.route("/path", get(handler))`. The route names a handler by symbol;
//	    the Rust `#[deprecated(since = "2.0", note = "use /api/v2/users")]`
//	    attribute sits on the `fn handler` definition ELSEWHERE in the file (not
//	    at the route site), so we build a handler→verdict map from every
//	    `#[deprecated(...)]`-annotated `fn` and attach it to the routes that name
//	    that handler. A Sunset/Deprecation response-header write in the handler
//	    body is also read. Path is the composed (nest-prefixed) route literal;
//	    api_version is path-derived.
//
//	actix-web / rocket — `#[get("/path")]` attribute macro directly above the
//	    `fn handler`. The `#[deprecated(...)]` attribute, a `/// `-rustdoc
//	    `@deprecated` tag, or a `// DEPRECATED` banner sits in the SAME attribute
//	    region as the route macro (above the fn); a Sunset/Deprecation header in
//	    the fn body is also read. Path is the (scope/mount-prefixed) macro path.
//
// api_version is path-derived for all three surfaces: an explicit `/api/v{N}` or
// `/v{N}` segment in the composed canonical path pins api_version (mirrors the
// flagship apiVersionFromPath). The composed path mirrors the producer
// extractors' joins so the Name matches: axum nest prefix (axum.go
// axumRouteNestPrefix), actix web::scope prefix (actix_web.go actixScopePrefix)
// + macro-paths-unscoped rule, rocket .mount prefix (rocket.go mountPrefix).
//
// Recognised Rust deprecation idioms (each credits deprecated=true):
//
//	#[deprecated(since = "2.0", note = "use /api/v2/users")] — the Rust stdlib
//	    attribute. The `since =` named arg is the version; the `note =` arg yields
//	    the replacement hint and a `since X` free-text fallback. A bare
//	    `#[deprecated]` (no args) still credits deprecated=true. The first/second
//	    positional string args are also accepted (since FIRST, note SECOND — the
//	    Rust order).
//	/// @deprecated <msg> / //! @deprecated — a rustdoc `@deprecated` tag (or the
//	    `# Deprecated` rustdoc heading) above the handler (msg → since/replacement).
//	// DEPRECATED <msg> — a banner line comment at the handler (cross-language).
//	Sunset / Deprecation response header (RFC 8594) — a
//	    `headers.insert("Sunset", …)` / `.header("Deprecation", …)` /
//	    `HeaderName::from_static("sunset")` write in the handler body.
//
// Honest-partial (NEVER fabricated):
//   - a route handler with NO deprecation marker AND no version segment → not
//     re-emitted (the plain route op from the producer extractor stands);
//   - a deprecation marker with no resolvable since/replacement → those props are
//     omitted (only deprecated + deprecation_source stamped);
//   - a versionless route → no api_version. `deprecated=false` is never written.
//
// Honesty: partial — heuristic regex on source text gated on a deprecation/
// version signal and the framework's own route idioms so an unrelated
// `#[deprecated]` on a non-route helper fn is not mis-attributed to an endpoint.
//
// Refs #4152, #3628.
package rust

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
	extractor.Register("custom_rust_endpoint_deprecation", &rustEndpointDeprecationExtractor{})
}

type rustEndpointDeprecationExtractor struct{}

func (e *rustEndpointDeprecationExtractor) Language() string {
	return "custom_rust_endpoint_deprecation"
}

// --- shared message parsing (mirrors flagship depSinceRe / depReplacementRe) ---

// rustDepSinceRe extracts a "since X" / "as of X" version/date from a free-text
// deprecation message (mirrors the flagship depSinceRe).
var rustDepSinceRe = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// rustDepReplacementRe extracts a "use X" / "replaced by X" / "prefer X" hint
// from a free-text message (mirrors the flagship depReplacementRe).
var rustDepReplacementRe = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// rustParseDeprecationMessage pulls an optional since-version and replacement
// hint out of a free-text deprecation message. Both honest-partial (empty when
// absent, never fabricated). Mirrors the flagship parser exactly.
func rustParseDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := rustDepSinceRe.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := rustDepReplacementRe.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}

// --- api_version (path-derived, mirrors flagship apiVersionFromPath) ----------

const (
	rustAPIVersionMin = 1
	rustAPIVersionMax = 99
)

// rustAPIVersionPatterns recognise an explicit API version SEGMENT in a route
// path. First-match-wins. The trailing `(?:/|$)` anchor keeps `/apiv2something`
// (no segment boundary) from matching — a version is only a version when it is
// its own path segment. Mirrors endpointAPIVersionPatterns.
var rustAPIVersionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)/api/v(\d+)(?:/|$)`),
	regexp.MustCompile(`(?i)/v(\d+)(?:/|$)`),
}

// rustAPIVersionFromPath returns the numeric API version named by an explicit
// version segment in path, and whether one was found in range.
func rustAPIVersionFromPath(path string) (int, bool) {
	for _, re := range rustAPIVersionPatterns {
		m := re.FindStringSubmatch(path)
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false
		}
		if v < rustAPIVersionMin || v > rustAPIVersionMax {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// --- deprecation marker recognition -------------------------------------------

var (
	// rustDeprecatedAttrRe matches a Rust `#[deprecated]` attribute with its
	// optional argument list. Group 1 = the (optional) parenthesised arg body.
	// Rust's stdlib attribute is `#[deprecated(since = "2.0", note = "msg")]`.
	// A bare `#[deprecated]` (no args) still credits deprecated=true.
	rustDeprecatedAttrRe = regexp.MustCompile(`#\[\s*deprecated\b(?:\s*\(([^\]]{0,400})\))?`)

	// rustSinceArgRe / rustNoteArgRe pull the `since = "X"` / `note = "X"` named
	// args out of a #[deprecated(...)] arg body.
	rustSinceArgRe = regexp.MustCompile(`\bsince\s*=\s*"([^"]{0,80})"`)
	rustNoteArgRe  = regexp.MustCompile(`\bnote\s*=\s*"([^"]{0,300})"`)

	// rustStringArgRe matches a quoted string-literal argument (positional fallback).
	rustStringArgRe = regexp.MustCompile(`"([^"]{0,300})"`)

	// rustRustdocDeprecatedRe matches a rustdoc `@deprecated <msg>` tag (in a
	// `///` / `//!` / `/** */` doc comment). Group 1 = trailing message.
	rustRustdocDeprecatedRe = regexp.MustCompile(`@deprecated\b([^\n*]{0,300})`)

	// rustBannerDeprecatedRe matches a `// DEPRECATED` / `/// DEPRECATED` banner
	// line comment (case-insensitive). Group 1 = trailing message.
	rustBannerDeprecatedRe = regexp.MustCompile(`(?i)(?://+|/\*+|\*)\s*DEPRECATED\b([^\n*]{0,300})`)

	// rustDeprecationHeaderRe matches a handler setting a Sunset / Deprecation
	// HTTP response header (RFC 8594). Covers the Rust shapes
	// `headers.insert("Sunset", …)`, `.header("Deprecation", …)`,
	// `HeaderName::from_static("sunset")`, `("Sunset", …)` tuple-header. Group 1 =
	// the header name. The quote/paren delimiters keep it from matching a bare
	// word. Mirrors the flagship deprecationHeaderRe shape.
	rustDeprecationHeaderRe = regexp.MustCompile(`(?i)["']\s*(sunset|deprecation)\s*["']`)
)

// rustDepVerdict is the resolved deprecation state for one endpoint (mirrors the
// flagship deprecationVerdict).
type rustDepVerdict struct {
	deprecated  bool
	since       string
	replacement string
	source      string
}

// rustParseDeprecatedAttr extracts since + replacement from a #[deprecated(...)]
// arg body. Named `since =` / `note =` args take priority; positional fallback
// is (since FIRST, note SECOND) — the Rust order. The note yields the
// replacement hint and a `since X` free-text fallback.
func rustParseDeprecatedAttr(args string) (since, replacement string) {
	if args == "" {
		return "", ""
	}
	var note string
	if m := rustSinceArgRe.FindStringSubmatch(args); m != nil {
		since = m[1]
	}
	if m := rustNoteArgRe.FindStringSubmatch(args); m != nil {
		note = m[1]
	}
	// Positional fallback: first quoted = since, second quoted = note.
	if since == "" || note == "" {
		strs := rustStringArgRe.FindAllStringSubmatch(args, -1)
		if since == "" && len(strs) >= 1 {
			since = strs[0][1]
		}
		if note == "" && len(strs) >= 2 {
			note = strs[1][1]
		}
	}
	// The note yields the replacement hint and a since-fallback.
	s, r := rustParseDeprecationMessage(note)
	replacement = r
	if since == "" {
		since = s
	}
	return since, replacement
}

// rustResolveRegionDeprecation inspects an attribute/doc region (the lines around
// a route macro / handler) for a #[deprecated] attribute, a rustdoc @deprecated
// tag, or a // DEPRECATED banner and returns the resolved verdict. Priority:
// attribute (richest) → rustdoc → banner. Honest-partial: no marker → (zero,
// false).
func rustResolveRegionDeprecation(region string) (rustDepVerdict, bool) {
	if m := rustDeprecatedAttrRe.FindStringSubmatch(region); m != nil {
		v := rustDepVerdict{deprecated: true, source: "#[deprecated]"}
		v.since, v.replacement = rustParseDeprecatedAttr(m[1])
		// A rustdoc @deprecated tag in the same region can carry a since/replacement
		// the attribute omitted (honest-partial fill, never overwrite).
		if jm := rustRustdocDeprecatedRe.FindStringSubmatch(region); jm != nil {
			s, r := rustParseDeprecationMessage(strings.TrimSpace(strings.Trim(jm[1], " \t*/")))
			if v.since == "" {
				v.since = s
			}
			if v.replacement == "" {
				v.replacement = r
			}
		}
		return v, true
	}
	if jm := rustRustdocDeprecatedRe.FindStringSubmatch(region); jm != nil {
		v := rustDepVerdict{deprecated: true, source: "rustdoc @deprecated"}
		v.since, v.replacement = rustParseDeprecationMessage(strings.TrimSpace(strings.Trim(jm[1], " \t*/")))
		return v, true
	}
	if bm := rustBannerDeprecatedRe.FindStringSubmatch(region); bm != nil {
		v := rustDepVerdict{deprecated: true, source: "comment // DEPRECATED"}
		v.since, v.replacement = rustParseDeprecationMessage(strings.TrimSpace(strings.Trim(bm[1], " \t*/")))
		return v, true
	}
	return rustDepVerdict{}, false
}

// rustResolveHeaderDeprecation inspects a handler body window for a Sunset /
// Deprecation response-header write. Honest-partial: no header → (zero, false).
func rustResolveHeaderDeprecation(body string) (rustDepVerdict, bool) {
	if m := rustDeprecationHeaderRe.FindStringSubmatch(body); m != nil {
		return rustDepVerdict{
			deprecated: true,
			source:     rustTitleHeader(m[1]) + " response header",
		}, true
	}
	return rustDepVerdict{}, false
}

// rustTitleHeader upper-cases the first rune of a lower-cased header name
// ("sunset" → "Sunset"). Mirrors the flagship titleHeaderName.
func rustTitleHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// rustStampDeprecation writes the flat deprecation contract onto an endpoint
// entity from a resolved verdict. since/replacement are honest-partial (omitted
// when empty). No-op when the verdict is not deprecated.
func rustStampDeprecation(e *types.EntityRecord, v rustDepVerdict) {
	if !v.deprecated {
		return
	}
	setProps(e, "deprecated", "true")
	if v.source != "" {
		setProps(e, "deprecation_source", v.source)
	}
	if v.since != "" {
		setProps(e, "deprecated_since", v.since)
	}
	if v.replacement != "" {
		setProps(e, "deprecated_replacement", v.replacement)
	}
}

// rustStampAPIVersion writes api_version onto an endpoint entity from its path,
// when an explicit version segment is present. Honest-partial: no segment →
// no-op.
func rustStampAPIVersion(e *types.EntityRecord, path string) {
	if v, ok := rustAPIVersionFromPath(path); ok {
		setProps(e, "api_version", strconv.Itoa(v))
	}
}

// --- extractor entry point ----------------------------------------------------

func (e *rustEndpointDeprecationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_endpoint_deprecation.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: a deprecation/version surface must mention a marker OR a
	// versioned path. api_version can apply even without a deprecation marker, so
	// allow a /v{N} path through.
	hasMarker := strings.Contains(src, "deprecated") ||
		strings.Contains(src, "DEPRECATED") ||
		strings.Contains(src, "Sunset") || strings.Contains(src, "sunset") ||
		strings.Contains(src, "Deprecation")
	hasVersion := strings.Contains(src, "/v")
	if !hasMarker && !hasVersion {
		return nil, nil
	}

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

	for _, ent := range e.extractAxum(src, file) {
		add(ent)
	}
	for _, ent := range e.extractMacroFramework(src, file, "actix_web") {
		add(ent)
	}
	for _, ent := range e.extractMacroFramework(src, file, "rocket") {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// --- axum surface -------------------------------------------------------------

// rustDepFnRe matches a `fn <name>(` definition so we can attach a #[deprecated]
// attribute (parsed from the attribute region above the fn) to the handler the
// axum route names. Group 1 = the function name.
var rustDepFnRe = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+(\w+)\s*[(<]`)

// extractAxum re-emits the deprecation/version-stamped endpoint op for every
// `.route("/p", verb(handler))` whose handler fn carries a #[deprecated]
// attribute / rustdoc tag / banner, or whose handler body sets a Sunset/
// Deprecation header, or whose composed path is versioned. The Name matches
// axum.go (`METHOD fullPath`) so the stamped op merges onto the plain route op.
func (e *rustEndpointDeprecationExtractor) extractAxum(src string, file extractor.FileInput) []types.EntityRecord {
	// Gate on an axum route signal so we no-op on actix/rocket-only files.
	if !strings.Contains(src, ".route") && !strings.Contains(src, ".nest") {
		return nil
	}

	// Build handler-name → verdict from every #[deprecated]-annotated fn and from
	// a Sunset/Deprecation header in the fn body.
	handlerDep := map[string]rustDepVerdict{}
	for _, fm := range rustDepFnRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[fm[2]:fm[3]]
		region := rustAttrRegionAbove(src, fm[0])
		if v, ok := rustResolveRegionDeprecation(region); ok {
			handlerDep[name] = v
			continue
		}
		body := rustBodyWindow(src, fm[1])
		if v, ok := rustResolveHeaderDeprecation(body); ok {
			handlerDep[name] = v
		}
	}

	// Recompute the nest-prefix map exactly as axum.go does so composed paths /
	// Names match.
	nestPrefix := map[string]string{}
	for _, m := range reAxumNest.FindAllStringSubmatchIndex(src, -1) {
		nestPrefix[src[m[4]:m[5]]] = rustNormalizePath(src[m[2]:m[3]])
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range reAxumRoute.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		methodRouter := src[m[4]:m[5]]
		prefix := axumRouteNestPrefix(src, m[0], nestPrefix)
		fullPath := rustJoinPaths(prefix, path)
		for _, vm := range reAxumMethodRouter.FindAllStringSubmatch(methodRouter, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
			name := method + " " + fullPath
			if seen[name] {
				continue
			}
			verdict, dep := handlerDep[handler]
			_, ver := rustAPIVersionFromPath(fullPath)
			if !dep && !ver {
				continue // leave the plain route op to axum.go
			}
			seen[name] = true
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_DEPRECATION",
				"http_method", method, "route_path", fullPath, "handler_name", handler)
			if prefix != "" {
				setProps(&ent, "nest_prefix", prefix)
			}
			rustStampDeprecation(&ent, verdict)
			rustStampAPIVersion(&ent, fullPath)
			out = append(out, ent)
		}
	}
	return out
}

// --- actix-web / rocket macro surface -----------------------------------------

// rustMacroVerbRe matches an actix/rocket attribute-macro route
// `#[get("/path")...]` with the path literal and any trailing kwargs. Group 1 =
// verb, group 2 = path. Mirrors the producer extractors' macro regex (path is
// the first string literal).
var rustMacroVerbRe = regexp.MustCompile(
	`#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"[^\]]*\)\]`)

// extractMacroFramework re-emits the deprecation/version-stamped endpoint op for
// every actix/rocket attribute-macro route whose attribute region (above the
// route macro, where #[deprecated]/rustdoc/banner sit) carries a marker, whose
// handler body sets a Sunset/Deprecation header, or whose composed path is
// versioned. Names match the producer extractors so the stamped op merges.
//
// Path composition mirrors the producer:
//   - rocket: prefix from `.mount("/p", routes![handler])` keyed by handler name;
//   - actix:  attribute-macro paths are NOT scope-prefixed (actix_web.go rule).
func (e *rustEndpointDeprecationExtractor) extractMacroFramework(src string, file extractor.FileInput, framework string) []types.EntityRecord {
	switch framework {
	case "actix_web":
		// Gate on an actix signal so we no-op on rocket-only files.
		if !strings.Contains(src, "actix") && !strings.Contains(src, "HttpResponse") &&
			!strings.Contains(src, "web::") && !strings.Contains(src, "Responder") {
			return nil
		}
	case "rocket":
		if !strings.Contains(src, "rocket") && !strings.Contains(src, "routes!") &&
			!strings.Contains(src, "#[launch]") {
			return nil
		}
	}

	// rocket: handler → mount prefix (mirrors rocket.go mountPrefix).
	mountPrefix := map[string]string{}
	if framework == "rocket" {
		for _, mm := range reRocketMount.FindAllStringSubmatch(src, -1) {
			prefix := rustNormalizePath(mm[1])
			for _, h := range strings.Split(mm[2], ",") {
				h = strings.TrimSpace(h)
				if idx := strings.LastIndex(h, "::"); idx >= 0 {
					h = h[idx+2:]
				}
				if h != "" {
					mountPrefix[h] = prefix
				}
			}
		}
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	for _, m := range rustMacroVerbRe.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := rustNormalizePath(src[m[4]:m[5]])

		// Resolve the handler fn name that follows this macro (for rocket mount
		// prefixing) + the body window for a header marker.
		handler, bodyStart := rustFnAfter(src, m[1])

		fullPath := path
		if framework == "rocket" {
			fullPath = rustJoinPaths(mountPrefix[handler], path)
		}
		name := method + " " + fullPath
		if seen[name] {
			continue
		}

		// Deprecation: the attribute region around the route macro (where a sibling
		// #[deprecated]/rustdoc/banner sits), then a Sunset/Deprecation header in
		// the handler body.
		region := rustMacroAttrRegion(src, m[0])
		verdict, dep := rustResolveRegionDeprecation(region)
		if !dep && bodyStart >= 0 {
			verdict, dep = rustResolveHeaderDeprecation(rustBodyWindow(src, bodyStart))
		}
		_, ver := rustAPIVersionFromPath(fullPath)
		if !dep && !ver {
			continue
		}
		seen[name] = true

		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		provFW := framework
		setProps(&ent, "framework", provFW,
			"provenance", "INFERRED_FROM_"+strings.ToUpper(framework)+"_DEPRECATION",
			"http_method", method, "route_pattern", fullPath)
		if handler != "" {
			setProps(&ent, "handler_name", handler)
		}
		if framework == "rocket" && mountPrefix[handler] != "" {
			setProps(&ent, "mount_prefix", mountPrefix[handler])
		}
		rustStampDeprecation(&ent, verdict)
		rustStampAPIVersion(&ent, fullPath)
		out = append(out, ent)
	}
	return out
}

// --- region helpers -----------------------------------------------------------

// rustAttrRegionAbove returns the contiguous run of attribute (`#[...]`), doc
// (`///`, `//!`, `/**`, `*`) and line-comment (`//`) lines immediately ABOVE the
// item at byte offset itemOff — where a #[deprecated] attribute / rustdoc tag /
// banner lives — including the item line itself.
func rustAttrRegionAbove(src string, itemOff int) string {
	if itemOff <= 0 || itemOff > len(src) {
		return ""
	}
	lines := strings.Split(src[:itemOff], "\n")
	end := len(lines) - 1 // partial item line
	top := end
	for top > 0 {
		prev := strings.TrimSpace(lines[top-1])
		if prev == "" ||
			strings.HasPrefix(prev, "#[") ||
			strings.HasPrefix(prev, "#![") ||
			strings.HasPrefix(prev, "//") ||
			strings.HasPrefix(prev, "*") ||
			strings.HasPrefix(prev, "/*") {
			top--
			continue
		}
		break
	}
	return strings.Join(lines[top:], "\n")
}

// rustMacroAttrRegion returns the attribute/doc region surrounding a route macro:
// the run of attribute/doc/comment lines ABOVE the macro (a sibling #[deprecated]
// is conventionally stacked with the route macro) PLUS the route-macro line and
// the immediately following attribute lines (a #[deprecated] can also sit BELOW
// the route macro, above the fn). Bounded so an unrelated attribute on the next
// item does not leak.
func rustMacroAttrRegion(src string, macroOff int) string {
	above := rustAttrRegionAbove(src, macroOff)
	// Append the lines from the macro down to (and including) the fn signature,
	// which captures a #[deprecated] stacked between the route macro and the fn.
	rest := src[macroOff:]
	if fm := rustDepFnRe.FindStringIndex(rest); fm != nil {
		return above + "\n" + rest[:fm[1]]
	}
	// No fn found nearby: include a small bounded window.
	end := 400
	if end > len(rest) {
		end = len(rest)
	}
	return above + "\n" + rest[:end]
}

// rustFnAfter returns the name of the first `fn <name>(` definition at/after
// fromOff and the byte offset just past its signature opener (for a body
// window). Returns ("", -1) when no fn follows within a bounded window.
func rustFnAfter(src string, fromOff int) (name string, bodyStart int) {
	if fromOff < 0 || fromOff >= len(src) {
		return "", -1
	}
	end := fromOff + 600
	if end > len(src) {
		end = len(src)
	}
	window := src[fromOff:end]
	if fm := rustDepFnRe.FindStringSubmatchIndex(window); fm != nil {
		return window[fm[2]:fm[3]], fromOff + fm[1]
	}
	return "", -1
}

// rustBodyWindow returns a bounded window of a handler body starting at bodyStart
// (kept small to avoid attributing a sibling handler's header write to this
// endpoint). Mirrors the flagship handlerBodyWindow size.
func rustBodyWindow(src string, bodyStart int) string {
	if bodyStart < 0 || bodyStart >= len(src) {
		return ""
	}
	end := bodyStart + 1000
	if end > len(src) {
		end = len(src)
	}
	return src[bodyStart:end]
}
