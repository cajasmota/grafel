// endpoint_deprecation.go — endpoint deprecation + API-version stamping for
// Kotlin web frameworks (#4136, child of epic #3628 cross-language fan-out).
//
// Greenfield: Kotlin carried NO endpoint_deprecation_versioning coverage.
// Kotlin sibling of the engine flagship pass
// (internal/engine/http_endpoint_deprecation.go) and the PHP custom-extractor
// port (internal/custom/php/helpers.go). The flagship engine pass stamps a flat
// property contract on synthesised http_endpoint_definition endpoints; Kotlin
// producer endpoints are SCOPE.Operation entities emitted by the custom .kt
// route extractors (ktor_routes.go / routing.go) instead, so the engine pass
// cannot reach them. To keep the Kotlin deprecation surface complete and
// consistent, this extractor stamps the IDENTICAL property contract at the
// source from the Kotlin framework idioms.
//
// Property contract (mirrors the flagship http_endpoint_deprecation.go EXACTLY):
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence for the dashboard)
//	api_version            — "1" | "2" | … numeric version from the route path
//
// Two recognised Kotlin surfaces (same as rate_limit_endpoint.go, so the
// stamped endpoint op merges onto the plain route op by Name):
//
//	Ktor DSL — a verb handler get/post/...("/path") { … } inside a routing{} /
//	           route("/prefix"){} block. Deprecation is read from the
//	           @Deprecated("use /api/v2/...", ReplaceWith(...)) annotation or a
//	           KDoc `* @deprecated …` tag in the doc/annotation region
//	           immediately ABOVE the verb call, OR from a
//	           call.response.header("Sunset"/"Deprecation", …) write in the
//	           handler lambda body. Enclosing route("/prefix") prefixes compose
//	           into the canonical path (same naming as ktor_routes.go).
//
//	Spring-Boot-Kotlin — a @<Verb>Mapping handler inside a @RestController /
//	           @Controller. Deprecation is read from the @Deprecated annotation /
//	           KDoc @deprecated above the @<Verb>Mapping, OR a
//	           response.setHeader("Sunset"/"Deprecation", …) in the fun body.
//	           Class-level @RequestMapping prefix composes (same naming as
//	           routing.go's Spring extractor).
//
// api_version is path-derived for BOTH surfaces: an explicit /api/v{N} or /v{N}
// segment in the composed canonical path pins api_version (mirrors the flagship
// apiVersionFromPath). Honest-partial: a versionless route carries no
// api_version; a route with no deprecation marker carries no `deprecated` (never
// fabricated — `deprecated=false` is never written).
//
// Like the sibling rate-limit pass this adds NO new node kind beyond the
// endpoint op it re-emits with the contract attached; MergeWithCustom /
// downstream dedup folds it onto the plain route op sharing the same Name.
//
// Refs #4136, #3628.
package kotlin

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
	extractor.Register("custom_kotlin_endpoint_deprecation", &kotlinEndpointDeprecationExtractor{})
}

type kotlinEndpointDeprecationExtractor struct{}

func (e *kotlinEndpointDeprecationExtractor) Language() string {
	return "custom_kotlin_endpoint_deprecation"
}

// --- shared message parsing (mirrors flagship depSinceRe / depReplacementRe) --

// ktDepSinceRe extracts a "since X" / "as of X" version/date from a free-text
// deprecation message (mirrors the flagship depSinceRe).
var ktDepSinceRe = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// ktDepReplacementRe extracts a "use X instead" / "replaced by X" hint (mirrors
// the flagship depReplacementRe).
var ktDepReplacementRe = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// ktParseDeprecationMessage pulls an optional since-version and replacement hint
// out of a free-text deprecation message. Both are honest-partial: an absent
// signal yields an empty string (never fabricated). Mirrors the flagship parser.
func ktParseDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := ktDepSinceRe.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := ktDepReplacementRe.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}

// --- api_version (path-derived, mirrors flagship apiVersionFromPath) ---------

const (
	ktAPIVersionMin = 1
	ktAPIVersionMax = 99
)

// ktAPIVersionPatterns recognise an explicit API version SEGMENT in a canonical
// route path. First-match-wins. The trailing `(?:/|$)` anchor keeps
// `/apiv2something` (no segment boundary) from matching — a version is only a
// version when it is its own path segment. Mirrors endpointAPIVersionPatterns.
var ktAPIVersionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)/api/v(\d+)(?:/|$)`),
	regexp.MustCompile(`(?i)/v(\d+)(?:/|$)`),
}

// ktAPIVersionFromPath returns the numeric API version named by an explicit
// version segment in path, and whether one was found in range.
func ktAPIVersionFromPath(path string) (int, bool) {
	for _, re := range ktAPIVersionPatterns {
		m := re.FindStringSubmatch(path)
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false
		}
		if v < ktAPIVersionMin || v > ktAPIVersionMax {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// --- deprecation marker recognition -----------------------------------------

// ktDeprecatedAnnotationRe matches a Kotlin @Deprecated annotation with its
// optional message + ReplaceWith argument list. Group 1 = the argument body
// (the "message", ReplaceWith("..."), level=…). Kotlin's stdlib @Deprecated has
// the shape @Deprecated("msg", ReplaceWith("expr"), level=…).
var ktDeprecatedAnnotationRe = regexp.MustCompile(`@Deprecated\b(?:\s*\(([^)]{0,400})\))?`)

// ktDeprecatedMsgRe / ktReplaceWithRe pull the human message and the
// ReplaceWith expression out of a @Deprecated argument body.
var (
	ktDeprecatedMsgRe = regexp.MustCompile(`^\s*"([^"]{0,300})"`)
	ktReplaceWithRe   = regexp.MustCompile(`ReplaceWith\s*\(\s*"([^"]{0,300})"`)
)

// ktKDocDeprecatedRe matches a KDoc `@deprecated <message>` tag (lowercase), the
// Kotlin doc-comment deprecation convention, with its trailing message.
var ktKDocDeprecatedRe = regexp.MustCompile(`@deprecated\b([^\n*]{0,300})`)

// ktDeprecationHeaderRe matches a handler setting a Sunset / Deprecation HTTP
// response header (RFC 8594). Covers the Kotlin shapes:
// `call.response.header("Sunset", …)` (Ktor) and
// `response.setHeader("Deprecation", …)` / `headers.add("Sunset", …)` (Spring),
// plus a bracket/quote form. Mirrors the flagship deprecationHeaderRe shape.
var ktDeprecationHeaderRe = regexp.MustCompile(`(?i)["'\(\[]\s*(sunset|deprecation)\s*["'\)\]]`)

// ktDepVerdict is the resolved deprecation state for one endpoint (mirrors the
// flagship deprecationVerdict).
type ktDepVerdict struct {
	deprecated  bool
	since       string
	replacement string
	source      string
}

// ktResolveAnnotationDeprecation inspects a doc/annotation region (the lines
// immediately above a handler) for a @Deprecated annotation or a KDoc
// @deprecated tag and returns the resolved verdict. Honest-partial: no marker →
// (zero, false).
func ktResolveAnnotationDeprecation(region string) (ktDepVerdict, bool) {
	if m := ktDeprecatedAnnotationRe.FindStringSubmatch(region); m != nil {
		v := ktDepVerdict{deprecated: true, source: "@Deprecated"}
		args := m[1]
		if args != "" {
			// Human message (first positional string literal).
			if msgM := ktDeprecatedMsgRe.FindStringSubmatch(args); msgM != nil {
				s, r := ktParseDeprecationMessage(msgM[1])
				v.since = s
				v.replacement = r
			}
			// ReplaceWith("expr") is the canonical Kotlin replacement hint and
			// wins over a free-text "use X" parse when present.
			if rwM := ktReplaceWithRe.FindStringSubmatch(args); rwM != nil {
				v.replacement = strings.TrimSpace(rwM[1])
			}
		}
		// A KDoc @deprecated tag in the same region can carry a since/replacement
		// the annotation omitted (honest-partial fill, never overwrite).
		if jm := ktKDocDeprecatedRe.FindStringSubmatch(region); jm != nil {
			s, r := ktParseDeprecationMessage(strings.TrimSpace(strings.Trim(jm[1], " \t*/")))
			if v.since == "" {
				v.since = s
			}
			if v.replacement == "" {
				v.replacement = r
			}
		}
		return v, true
	}
	if jm := ktKDocDeprecatedRe.FindStringSubmatch(region); jm != nil {
		v := ktDepVerdict{deprecated: true, source: "KDoc @deprecated"}
		v.since, v.replacement = ktParseDeprecationMessage(strings.TrimSpace(strings.Trim(jm[1], " \t*/")))
		return v, true
	}
	return ktDepVerdict{}, false
}

// ktResolveHeaderDeprecation inspects a handler body window for a Sunset /
// Deprecation response-header write. Honest-partial: no header → (zero, false).
func ktResolveHeaderDeprecation(body string) (ktDepVerdict, bool) {
	if m := ktDeprecationHeaderRe.FindStringSubmatch(body); m != nil {
		return ktDepVerdict{
			deprecated: true,
			source:     ktTitleHeader(m[1]) + " response header",
		}, true
	}
	return ktDepVerdict{}, false
}

// ktTitleHeader upper-cases the first rune of a lower-cased header name
// ("sunset" → "Sunset"). Mirrors the flagship titleHeaderName.
func ktTitleHeader(s string) string {
	s = strings.ToLower(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ktStampDeprecation writes the flat deprecation contract onto an endpoint
// entity from a resolved verdict. since/replacement are honest-partial (omitted
// when empty). No-op when the verdict is not deprecated.
func ktStampDeprecation(e *types.EntityRecord, v ktDepVerdict) {
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

// ktStampAPIVersion writes api_version onto an endpoint entity from its path,
// when an explicit version segment is present. Honest-partial: no segment →
// no-op.
func ktStampAPIVersion(e *types.EntityRecord, path string) {
	if v, ok := ktAPIVersionFromPath(path); ok {
		setProps(e, "api_version", strconv.Itoa(v))
	}
}

// --- extractor entry point ---------------------------------------------------

func (e *kotlinEndpointDeprecationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_endpoint_deprecation.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: a deprecation/version surface must mention a marker OR a
	// versioned path. We only stamp a marker-bearing endpoint, but api_version
	// can apply even without a deprecation marker, so allow a /v{N} path through.
	hasMarker := strings.Contains(src, "@Deprecated") ||
		strings.Contains(src, "@deprecated") ||
		strings.Contains(src, "Sunset") ||
		strings.Contains(src, "Deprecation")
	hasVersion := strings.Contains(src, "/v") || strings.Contains(src, "/api/v")
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

	for _, ent := range e.extractKtor(src, file) {
		add(ent)
	}
	for _, ent := range e.extractSpring(src, file) {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ktDepVerbRe matches a Ktor verb handler get/post/...("/path"); group 1 = verb,
// group 2 = leaf path. Same verb set as ktor_routes.go.
var ktDepVerbRe = regexp.MustCompile(
	`\b(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]*)"`)

// ktDepRouteRe matches an enclosing route("/prefix") { opener; group 1 = prefix.
var ktDepRouteRe = regexp.MustCompile(`\broute\s*\(\s*"([^"]*)"\s*\)\s*\{`)

// extractKtor walks every Ktor verb handler, composing enclosing route("/prefix")
// prefixes, and stamps deprecation (from the @Deprecated/KDoc region above the
// verb call or a Sunset/Deprecation header in the handler lambda body) and
// api_version (path-derived). Only marker-bearing OR version-bearing handlers
// are emitted; a plain handler is skipped so it does not shadow the route op.
func (e *kotlinEndpointDeprecationExtractor) extractKtor(src string, file extractor.FileInput) []types.EntityRecord {
	// Gate on a Ktor routing signal so we no-op on Spring-only files.
	if !strings.Contains(src, "routing") && !ktDepRouteRe.MatchString(src) {
		return nil
	}
	var out []types.EntityRecord
	seen := make(map[string]bool)

	for _, v := range ktDepVerbRe.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[v[2]:v[3]])
		leaf := src[v[4]:v[5]]
		prefix := ktDepEnclosingRoutePrefix(src, v[0])
		fullPath := joinKtRoutePaths(prefix, leaf)
		name := verb + " " + fullPath
		if seen[name] {
			continue
		}

		// Deprecation: annotation/KDoc region above the verb call, then a
		// Sunset/Deprecation header in the handler lambda body.
		region := ktDocAnnotationRegion(src, v[0])
		verdict, dep := ktResolveAnnotationDeprecation(region)
		if !dep {
			body := ktHandlerBodyWindow(src, v[1])
			verdict, dep = ktResolveHeaderDeprecation(body)
		}
		_, ver := ktAPIVersionFromPath(fullPath)
		if !dep && !ver {
			// Nothing to stamp — leave the plain route op to ktor_routes.go.
			continue
		}
		seen[name] = true

		ln := lineOf(src, v[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", ln)
		setProps(&ent,
			"framework", "ktor",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_KTOR_DEPRECATION",
		)
		ktStampDeprecation(&ent, verdict)
		ktStampAPIVersion(&ent, fullPath)
		out = append(out, ent)
	}
	return out
}

// ktDepEnclosingRoutePrefix returns the composed route("/prefix") prefix path
// that brace-encloses the verb call at byte offset verbOff. Walks every
// route("/p"){ opener whose brace-balanced block contains verbOff, joining the
// prefixes outermost-first. Returns "" when no enclosing route. Reuses the
// shared brace matcher (ktRLMatchBrace) from rate_limit_endpoint.go.
func ktDepEnclosingRoutePrefix(src string, verbOff int) string {
	var prefix string
	for _, rm := range ktDepRouteRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart := rm[1] // past the opening brace
		bodyEnd, ok := ktRLMatchBrace(src, bodyStart)
		if !ok {
			continue
		}
		if rm[0] < verbOff && verbOff < bodyEnd {
			prefix = joinKtRoutePaths(prefix, src[rm[2]:rm[3]])
		}
	}
	return prefix
}

// --- Spring-Kotlin surface ---------------------------------------------------

var (
	// ktDepSpringController gates the Spring surface on a controller annotation.
	ktDepSpringController = regexp.MustCompile(`@(?:Rest)?Controller\b`)

	// ktDepSpringClassMapping matches a class-level @RequestMapping prefix
	// (positional / value= / path=). Mirrors reKtSpringClassMapping.
	ktDepSpringClassMapping = regexp.MustCompile(
		`@RequestMapping\s*(?:\(\s*(?:value\s*=\s*|path\s*=\s*)?"([^"]*)"\s*\))?`)

	// ktDepSpringVerb matches a method-level verb mapping with optional path.
	// Mirrors reKtSpringVerbMapping.
	ktDepSpringVerb = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch|Head|Options)Mapping\s*(?:\(\s*(?:value\s*=\s*|path\s*=\s*)?"([^"]*)"\s*\))?`)
)

var ktDepSpringVerbMap = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
}

// extractSpring stamps deprecation + api_version on the endpoint op of a
// @<Verb>Mapping handler inside a @RestController/@Controller. Endpoint Name
// matches routing.go's Spring extractor so they merge. Only marker-bearing OR
// version-bearing handlers are emitted.
func (e *kotlinEndpointDeprecationExtractor) extractSpring(src string, file extractor.FileInput) []types.EntityRecord {
	if !ktDepSpringController.MatchString(src) {
		return nil
	}
	classPrefix := ""
	if m := ktDepSpringClassMapping.FindStringSubmatchIndex(src); m != nil && m[2] >= 0 {
		classPrefix = src[m[2]:m[3]]
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)

	for _, m := range ktDepSpringVerb.FindAllStringSubmatchIndex(src, -1) {
		verb := ktDepSpringVerbMap[src[m[2]:m[3]]]
		methodPath := ""
		if m[4] >= 0 {
			methodPath = src[m[4]:m[5]]
		}
		fullPath := joinKtRoutePaths(classPrefix, methodPath)
		if fullPath == "" {
			fullPath = "/"
		}
		name := verb + " " + fullPath
		if seen[name] {
			continue
		}

		// Deprecation: @Deprecated/KDoc region above the @<Verb>Mapping, then a
		// Sunset/Deprecation header in the fun body that follows the mapping.
		region := ktDocAnnotationRegion(src, m[0])
		verdict, dep := ktResolveAnnotationDeprecation(region)
		if !dep {
			body := ktHandlerBodyWindow(src, m[1])
			verdict, dep = ktResolveHeaderDeprecation(body)
		}
		_, ver := ktAPIVersionFromPath(fullPath)
		if !dep && !ver {
			continue
		}
		seen[name] = true

		ln := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, "kotlin", ln)
		setProps(&ent,
			"framework", "spring-boot",
			"http_method", verb,
			"path", fullPath,
			"provenance", "INFERRED_FROM_SPRING_DEPRECATION",
		)
		ktStampDeprecation(&ent, verdict)
		ktStampAPIVersion(&ent, fullPath)
		out = append(out, ent)
	}
	return out
}

// --- shared region helpers ---------------------------------------------------

// ktDocAnnotationRegion returns the contiguous run of annotation (`@`), KDoc
// (`/**`, `*`, `*/`) and line-comment (`//`) lines immediately ABOVE the handler
// at byte offset handlerOff, where a @Deprecated annotation / KDoc @deprecated
// tag lives. Mirrors the flagship handlerDecoratorRegion (annotation/comment
// walk-up), scoped to the lines preceding handlerOff.
func ktDocAnnotationRegion(src string, handlerOff int) string {
	if handlerOff <= 0 || handlerOff > len(src) {
		return ""
	}
	lines := strings.Split(src[:handlerOff], "\n")
	// The last element is the (partial) handler line; walk upward over the
	// preceding annotation/comment lines.
	end := len(lines) - 1
	top := end
	for top > 0 {
		prev := strings.TrimSpace(lines[top-1])
		if prev == "" ||
			strings.HasPrefix(prev, "@") ||
			strings.HasPrefix(prev, "//") ||
			strings.HasPrefix(prev, "*") ||
			strings.HasPrefix(prev, "/*") {
			top--
			continue
		}
		break
	}
	// Include the handler line itself so an inline annotation on the same line is
	// in scope.
	return strings.Join(lines[top:], "\n")
}

// ktHandlerBodyWindow returns a bounded window of the handler body starting just
// after the verb/mapping match. Kept small (1000 bytes) to avoid attributing a
// sibling handler's header write to this endpoint. Mirrors the flagship
// handlerBodyWindow size.
func ktHandlerBodyWindow(src string, bodyStart int) string {
	if bodyStart < 0 || bodyStart >= len(src) {
		return ""
	}
	end := bodyStart + 1000
	if end > len(src) {
		end = len(src)
	}
	return src[bodyStart:end]
}
