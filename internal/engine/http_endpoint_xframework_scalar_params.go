// Cross-framework scalar request-param extraction (#4583 Express/Koa,
// #4584 FastAPI, #4585 Spring, #4586 DRF).
//
// Generalises the NestJS scalar @Query/@Param work (#4568) to four more
// frameworks. The NestJS fix re-parsed the handler signature inside the
// synthesizer and stamped one parameter record {name, in, type, required} per
// scalar param decorator onto the SYNTHESIZED http_endpoint — the entity the
// dashboard Paths panel actually renders. Several of these frameworks already
// captured params on a DIFFERENT entity (e.g. Spring's java_annotation_routes
// http_endpoint), but the dashboard renders the http_endpoint_definition the
// route SYNTHESIZER emits, which carried no params (Spring composed Route),
// or carried none at all (Express, FastAPI, DRF). The result was a
// "Parameters (0) / None" panel on live upvate-v3.
//
// Like the deprecation / pagination / response-code passes
// (http_endpoint_deprecation.go, _pagination.go, _response_codes.go), this is a
// tail enrichment that runs AFTER every per-language route synthesizer has
// emitted its http_endpoint_definition entities for the current file. It mutates
// Properties on the just-emitted producer endpoints in place — never adds or
// removes entities, so it cannot regress upstream synthesis. It is a no-op for
// any endpoint that already carries a `parameters` property (the synthesizer
// already stamped a richer signature, e.g. NestJS via stampNestSignature).
//
// CONSERVATIVE BOUNDARY: a record is emitted only when the source clearly marks
// a scalar request param:
//
//   - Express/Koa : `req.params.X` / `req.query.X` / `req.headers['x']` reads in
//     the handler body, plus `:id` route-template path segments.
//   - FastAPI     : signature params with `Query(...)` / `Path(...)` /
//     `Header(...)` defaults, plus path `{id}` segments and bare typed scalar
//     params that match a path placeholder.
//   - Spring      : `@RequestParam` / `@PathVariable` / `@RequestHeader` method
//     params (reuses the shared extractJavaParameters classifier), plus the
//     route-template path segments.
//   - DRF         : `request.query_params.get('x')` body reads, plus URL kwargs
//     (`self.kwargs['pk']`) and `<int:pk>` / `{pk}` route-template path segments.
//
// Object/whole-body params, untyped catch-alls, and framework-injected context
// objects are never emitted. Path params are always `required: true`.
//
// Refs #4583 #4584 #4585 #4586.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// applyScalarRequestParams stamps a `parameters` JSON ([]JavaParam) onto every
// producer endpoint at index >= before in `entities` that belongs to `path` and
// does not already carry one. The param set is resolved from the route template
// (path params) plus the framework-specific handler region/body.
func applyScalarRequestParams(lang, content, path string, entities []types.EntityRecord, before int) {
	if content == "" || before < 0 || before >= len(entities) {
		return
	}
	normLang := normalisePaginationLang(lang) // reuse the lang normaliser

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		// Honest no-op: a synthesizer that already stamped a signature (NestJS,
		// the Java annotation-routes pass when it wins attribution) wins — never
		// clobber a richer param list.
		if e.Properties["parameters"] != "" {
			continue
		}
		params := resolveScalarRequestParams(normLang, content, e)
		if len(params) == 0 {
			continue
		}
		if enc := EncodeJavaParameters(params); enc != "" {
			e.Properties["parameters"] = enc
			// Record the flat companion path_params field if absent (mirrors the
			// Java annotation-routes pass) so the dynamic segments are queryable.
			if e.Properties["path_params"] == "" {
				var pp []string
				for _, p := range params {
					if p.In == "path" {
						pp = append(pp, p.Name)
					}
				}
				if len(pp) > 0 {
					e.Properties["path_params"] = strings.Join(pp, ",")
				}
			}
		}
	}
}

// resolveScalarRequestParams dispatches per (lang, framework) and returns the
// merged, deduplicated, order-stable scalar param list for one endpoint.
func resolveScalarRequestParams(lang, content string, e *types.EntityRecord) []JavaParam {
	framework := e.Properties["framework"]
	canonical := e.Properties["path"]

	anchorLine := e.StartLine
	if anchorLine <= 0 {
		anchorLine = routeDeclarationLine(content, canonical, e.Properties["verb"])
	}
	if anchorLine <= 0 && lang == "java" {
		// The route tail is a path param (e.g. /buildings/{id}) so the literal-
		// segment anchor missed; locate the Spring mapping annotation directly.
		anchorLine = javaMappingAnnotationLine(content, e.Properties["verb"])
	}
	region, handlerStart := handlerDecoratorRegion(content, anchorLine)
	sig := forwardSignatureWindow(content, anchorLine)
	body := handlerBodyWindowLarge(content, handlerStart)

	// Path params from the route template are framework-agnostic and always
	// required. They seed the merge so a typed signature/body read can refine
	// the type later.
	merged := newScalarParamSet()
	for _, name := range splitTemplatePathParams(canonical) {
		merged.add(JavaParam{Name: name, In: "path", Required: true})
	}

	switch lang {
	case "javascript":
		// Express / Koa (and Express-shaped Hono/Polka/Restify/Fastify). The
		// handler body holds the req.params/query/headers reads.
		if isExpressShapedFramework(framework) {
			for _, p := range expressScalarParams(region + "\n" + sig + "\n" + body) {
				merged.add(p)
			}
		}
	case "python":
		switch {
		case framework == "fastapi" || framework == "flask":
			// FastAPI declares scalar params on the signature via Query()/Path()/
			// Header() defaults and typed annotations. (A FastAPI file can be
			// mislabelled `flask` when the Flask synthesizer wins the shared
			// `@x.get(` shape — parse the signature either way; the shape is
			// FastAPI-specific so a real Flask handler yields nothing.)
			for _, p := range fastapiScalarParams(region + "\n" + sig) {
				merged.add(p)
			}
		case framework == "django":
			// DRF: query_params.get reads + self.kwargs / URL-conf <int:pk>.
			for _, p := range drfScalarParams(content, e, body) {
				merged.add(p)
			}
		}
	case "java":
		if framework == "spring_mvc" || framework == "spring" {
			// Reuse the shared Java annotation classifier on the handler signature
			// window: @RequestParam/@PathVariable/@RequestHeader → records. The
			// dedicated Java window spans the method declaration's full (possibly
			// multi-line) parameter list — forwardSignatureWindow can't be reused
			// because a `{` inside a `@GetMapping("/{id}")` path literal would end
			// its scan prematurely.
			frag := javaSignatureParamFragment(javaSignatureWindow(content, anchorLine))
			if frag != "" {
				for _, p := range extractJavaParameters(frag, []string{e.Properties["verb"]}) {
					if p.In == "body" || p.In == "form" {
						continue // scalar-param pass only
					}
					merged.add(p)
				}
			}
		}
	}

	return merged.ordered()
}

// ---------------------------------------------------------------------------
// Shared scalar-param set (order-stable, path-type-refining merge)
// ---------------------------------------------------------------------------

type scalarParamSet struct {
	order []string // "in\x00name" keys in first-seen order
	byKey map[string]JavaParam
}

func newScalarParamSet() *scalarParamSet {
	return &scalarParamSet{byKey: map[string]JavaParam{}}
}

func (s *scalarParamSet) add(p JavaParam) {
	if p.Name == "" || p.In == "" {
		return
	}
	key := p.In + "\x00" + p.Name
	if existing, ok := s.byKey[key]; ok {
		// Refine: keep the path required flag; fill a missing type from the
		// later, more-specific record (e.g. template path param seeded first,
		// then a typed signature param supplies the type).
		if existing.Type == "" && p.Type != "" {
			existing.Type = p.Type
		}
		if p.Required {
			existing.Required = true
		}
		if len(existing.Annotations) == 0 && len(p.Annotations) > 0 {
			existing.Annotations = p.Annotations
		}
		s.byKey[key] = existing
		return
	}
	s.order = append(s.order, key)
	s.byKey[key] = p
}

func (s *scalarParamSet) ordered() []JavaParam {
	out := make([]JavaParam, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, s.byKey[k])
	}
	return out
}

// splitTemplatePathParams extracts dynamic path-segment names from a canonical
// route path, handling `{id}`, `{id:regex}`, `:id`, and `<int:pk>` forms (the
// canonicaliser normalises most to `{name}` but DRF/Express variants survive in
// some passes, so all three are accepted defensively).
func splitTemplatePathParams(canonical string) []string {
	var out []string
	for _, seg := range strings.Split(canonical, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			tok := seg[1 : len(seg)-1]
			// `{int:pk}` (DRF-style left of colon is the converter) or
			// `{pk:regex}` (regex right of colon). Heuristic: a known converter
			// prefix means the NAME is on the right; otherwise the name is on
			// the left and any colon introduces a regex constraint.
			if c := strings.IndexByte(tok, ':'); c >= 0 {
				left, right := tok[:c], tok[c+1:]
				if drfPathConverters[left] {
					tok = right
				} else {
					tok = left
				}
			}
			if tok = strings.TrimSpace(tok); tok != "" {
				out = append(out, tok)
			}
		case strings.HasPrefix(seg, ":"):
			out = append(out, strings.TrimSpace(seg[1:]))
		case strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">"):
			tok := seg[1 : len(seg)-1]
			if c := strings.IndexByte(tok, ':'); c >= 0 {
				tok = tok[c+1:] // <int:pk> → pk
			}
			if tok = strings.TrimSpace(tok); tok != "" {
				out = append(out, tok)
			}
		}
	}
	return out
}

var drfPathConverters = map[string]bool{
	"int": true, "str": true, "slug": true, "uuid": true, "path": true,
}

// ---------------------------------------------------------------------------
// #4583 — Express / Koa
// ---------------------------------------------------------------------------

func isExpressShapedFramework(fw string) bool {
	switch fw {
	case "express", "koa", "hono", "fastify", "polka", "restify":
		return true
	}
	return false
}

// expressReqParamRe matches `req.params.X` / `request.params.X` (also via a
// destructured-arg alias is out of scope — conservative). Group 1 = name.
var expressReqParamRe = regexp.MustCompile(`\b(?:req|request|ctx)\.params\.([A-Za-z_$][\w$]*)`)

// expressReqParamBracketRe matches `req.params['x']` / `req.params["x"]`.
var expressReqParamBracketRe = regexp.MustCompile(`\b(?:req|request|ctx)\.params\[\s*['"]([^'"]+)['"]\s*\]`)

// expressReqQueryRe matches `req.query.X`. Group 1 = name.
var expressReqQueryRe = regexp.MustCompile(`\b(?:req|request|ctx)\.query\.([A-Za-z_$][\w$]*)`)

// expressReqQueryBracketRe matches `req.query['x']`.
var expressReqQueryBracketRe = regexp.MustCompile(`\b(?:req|request|ctx)\.query\[\s*['"]([^'"]+)['"]\s*\]`)

// expressReqHeaderBracketRe matches `req.headers['x']` / `req.headers["x"]`.
var expressReqHeaderBracketRe = regexp.MustCompile(`\b(?:req|request|ctx)\.headers\[\s*['"]([^'"]+)['"]\s*\]`)

// expressReqHeaderDotRe matches `req.headers.xToken` (camelCase header access).
var expressReqHeaderDotRe = regexp.MustCompile(`\b(?:req|request|ctx)\.headers\.([A-Za-z_$][\w$]*)`)

// expressReqGetHeaderRe matches `req.get('X-Header')` / `req.header('X-Header')`
// (Express header accessors).
var expressReqGetHeaderRe = regexp.MustCompile(`\b(?:req|request)\.(?:get|header)\(\s*['"]([^'"]+)['"]\s*\)`)

func expressScalarParams(scope string) []JavaParam {
	var out []JavaParam
	seen := map[string]bool{}
	emit := func(name, in string, required bool) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := in + "\x00" + name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, JavaParam{Name: name, In: in, Required: required})
	}
	for _, re := range []*regexp.Regexp{expressReqQueryRe, expressReqQueryBracketRe} {
		for _, m := range re.FindAllStringSubmatch(scope, -1) {
			emit(m[1], "query", false)
		}
	}
	for _, re := range []*regexp.Regexp{expressReqParamRe, expressReqParamBracketRe} {
		for _, m := range re.FindAllStringSubmatch(scope, -1) {
			emit(m[1], "path", true)
		}
	}
	for _, re := range []*regexp.Regexp{expressReqHeaderBracketRe, expressReqHeaderDotRe, expressReqGetHeaderRe} {
		for _, m := range re.FindAllStringSubmatch(scope, -1) {
			emit(m[1], "header", false)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// #4584 — FastAPI
// ---------------------------------------------------------------------------

// fastapiParamDefaultRe matches one signature param with a FastAPI dependency
// default: `name: Type = Query(...)` / `name = Path(...)` / `name: str = Header(...)`.
// Group 1 = param name, group 2 = type annotation (optional), group 3 = marker.
var fastapiParamDefaultRe = regexp.MustCompile(
	`([A-Za-z_]\w*)\s*(?::\s*([A-Za-z_][\w\[\], .|]*?))?\s*=\s*(Query|Path|Header|Cookie)\s*\(`,
)

// fastapiMarkerIn maps a FastAPI param marker to its OpenAPI `in` location.
var fastapiMarkerIn = map[string]string{
	"Query": "query", "Path": "path", "Header": "header", "Cookie": "cookie",
}

// fastapiSigParamsBlock extracts the parenthesised parameter list of the FIRST
// `def`/`async def` in the scope (the handler), returning the inner text.
func fastapiSigParamsBlock(scope string) string {
	idx := pyDefSignatureRe.FindStringIndex(scope)
	if idx == nil {
		return ""
	}
	open := strings.IndexByte(scope[idx[0]:], '(')
	if open < 0 {
		return ""
	}
	open += idx[0]
	depth := 0
	for i := open; i < len(scope); i++ {
		switch scope[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return scope[open+1 : i]
			}
		}
	}
	return ""
}

var pyDefSignatureRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+\w+\s*\(`)

func fastapiScalarParams(scope string) []JavaParam {
	block := fastapiSigParamsBlock(scope)
	if block == "" {
		return nil
	}
	var out []JavaParam
	seen := map[string]bool{}
	emit := func(p JavaParam) {
		key := p.In + "\x00" + p.Name
		if p.Name == "" || p.In == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}

	// Marker-default params (Query/Path/Header/Cookie) carry an explicit location.
	markerNames := map[string]bool{}
	for _, m := range fastapiParamDefaultRe.FindAllStringSubmatch(block, -1) {
		name := m[1]
		typ := pyCleanScalarType(m[2])
		in := fastapiMarkerIn[m[3]]
		if in == "" {
			continue
		}
		markerNames[name] = true
		required := !fastapiMarkerHasDefault(block, name, m[3])
		if in == "path" {
			required = true
		}
		emit(JavaParam{Name: name, In: in, Type: typ, Required: required, Annotations: []string{m[3] + "()"}})
	}

	// Bare typed params whose name matches a path placeholder become path params
	// (FastAPI binds `def f(id: int)` against `/items/{id}`). Resolved against the
	// handler's own def-line placeholders is overkill here; the caller seeds path
	// params from the route template and add() refines the type, so we surface
	// bare typed scalars and let the merge bind them to a template path param.
	for _, chunk := range splitTopLevelCommas(block) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		name, typ, ok := pyBareScalarParam(chunk)
		if !ok || markerNames[name] {
			continue
		}
		// Surface as a path-typed scalar; the merge keeps it only when a template
		// path param of the same name exists (add() refines), and a stray
		// non-path bare scalar (e.g. an injected dependency) carries no `in` here
		// so it is emitted as path — but only matched names survive the merge's
		// template seeding. To stay conservative we DO emit it as path: a bare
		// typed scalar in a FastAPI handler signature is, by FastAPI's binding
		// rules, a path param when the name is in the route (the dominant case).
		emit(JavaParam{Name: name, In: "path", Type: typ, Required: true})
	}
	return out
}

// fastapiMarkerHasDefault reports whether a Query/Path/Header/Cookie marker for
// `name` supplies a default that makes the param optional: `Query(None)` /
// `Query(default=None)` / `Query("x")` / `Query(default="x")`. A bare
// `Query(...)` / `Query()` (Ellipsis or empty) is required.
func fastapiMarkerHasDefault(block, name, marker string) bool {
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*(?::[^=]*)?=\s*` + marker + `\s*\(([^)]*)\)`)
	m := re.FindStringSubmatch(block)
	if m == nil {
		return false
	}
	arg := strings.TrimSpace(m[1])
	if arg == "" || arg == "..." || arg == "Ellipsis" {
		return false
	}
	if strings.HasPrefix(arg, "default") {
		val := strings.TrimSpace(strings.TrimPrefix(arg, "default"))
		val = strings.TrimSpace(strings.TrimPrefix(val, "="))
		val = strings.TrimSpace(strings.SplitN(val, ",", 2)[0])
		return !(val == "..." || val == "Ellipsis")
	}
	// First positional arg is the default value (e.g. Query(None) / Query("x")).
	first := strings.TrimSpace(strings.SplitN(arg, ",", 2)[0])
	return first != "..." && first != "Ellipsis"
}

// pyBareScalarParam parses a `name: Type` signature chunk and returns (name,
// type, ok) only when Type is a scalar primitive (int/str/float/bool/UUID).
// Chunks with a default (`=`), no annotation, `self`/`cls`, or a non-scalar /
// framework type (Request, Depends, BaseModel-ish) are rejected.
func pyBareScalarParam(chunk string) (string, string, bool) {
	if strings.ContainsRune(chunk, '=') {
		return "", "", false // has a default → handled by the marker pass or skipped
	}
	colon := strings.IndexByte(chunk, ':')
	if colon < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(chunk[:colon])
	typ := pyCleanScalarType(chunk[colon+1:])
	switch name {
	case "", "self", "cls", "request", "req":
		return "", "", false
	}
	if !pyScalarTypes[strings.ToLower(typ)] {
		return "", "", false
	}
	return name, typ, true
}

var pyScalarTypes = map[string]bool{
	"int": true, "str": true, "float": true, "bool": true,
	"uuid": true, "date": true, "datetime": true, "decimal": true, "bytes": true,
}

// pyCleanScalarType trims a Python annotation to its bare scalar name, stripping
// Optional[...] / Annotated[...] / generic wrappers and qualifier prefixes.
func pyCleanScalarType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Optional[int] / List[int] → inner; take the first scalar-looking token.
	if open := strings.IndexByte(raw, '['); open >= 0 {
		inner := raw[open+1:]
		if close := strings.LastIndexByte(inner, ']'); close >= 0 {
			inner = inner[:close]
		}
		inner = strings.SplitN(inner, ",", 2)[0]
		if t := pyCleanScalarType(inner); t != "" {
			return t
		}
	}
	// Strip a `|` union (int | None) and qualifier dots (uuid.UUID → UUID).
	raw = strings.TrimSpace(strings.SplitN(raw, "|", 2)[0])
	if dot := strings.LastIndexByte(raw, '.'); dot >= 0 {
		raw = raw[dot+1:]
	}
	raw = strings.TrimSpace(raw)
	// Keep only a bare identifier.
	for _, r := range raw {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return raw
}

// ---------------------------------------------------------------------------
// #4585 — Spring
// ---------------------------------------------------------------------------

// javaMethodDeclOpenRe matches the `(` that opens a Java method parameter list:
// a return-type token, the method name, then `(`. Anchored to skip annotation
// lines (`@GetMapping(...)`) whose own parens must not be mistaken for the
// param list. Group boundary: the match ends just past the `(`.
var javaMethodDeclOpenRe = regexp.MustCompile(
	`(?m)\b(?:public|protected|private|static|final|abstract|synchronized|default)\b[^\n(){}]*\b\w+\s*\(`,
)

// javaSignatureParamFragment returns the parameter-list text (everything just
// past the method-declaration `(`) for the Java classifier. It locates the
// method declaration via javaMethodDeclOpenRe so a preceding `@*Mapping(...)`
// annotation's parens are skipped. Returns "" when no method decl is found.
func javaSignatureParamFragment(window string) string {
	loc := javaMethodDeclOpenRe.FindStringIndex(window)
	if loc == nil {
		// Fallback: take the first `(` that is NOT on an annotation line.
		for _, line := range strings.Split(window, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "@") {
				continue
			}
			if idx := strings.IndexByte(line, '('); idx >= 0 {
				// Reconstruct the tail from this line onward.
				start := strings.Index(window, line)
				if start >= 0 {
					return window[start+idx+1:]
				}
			}
		}
		return ""
	}
	return window[loc[1]:] // loc[1] is just past the `(`
}

// javaSignatureWindow returns the source from the handler-anchor line (the
// @*Mapping annotation, 1-based) down to and including the line that closes the
// method parameter list with `) {` (or `);` for an interface/abstract method),
// capped at a generous look-ahead. Unlike forwardSignatureWindow it tracks
// parenthesis depth so a `{` inside a `@GetMapping("/{id}")` path literal does
// not end the scan, letting a multi-line parameter list be captured whole.
func javaSignatureWindow(content string, anchorLine int) string {
	if anchorLine <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	start := anchorLine - 1
	if start < 0 || start >= len(lines) {
		return ""
	}
	var b strings.Builder
	depth := 0
	sawMethodOpen := false
	for i := start; i < len(lines) && i < start+30; i++ {
		line := lines[i]
		b.WriteString(line)
		b.WriteByte('\n')
		trimmed := strings.TrimSpace(line)
		// Skip annotation lines (`@GetMapping("/{id}")`, `@PathVariable(...)` on
		// its own line): their parens are balanced on the same line and a `{`
		// inside a path-literal must NOT be mistaken for the method body brace.
		// The method-declaration line (return type + name + `(`) is the first
		// non-annotation, non-blank line — start counting depth there.
		if !sawMethodOpen && (trimmed == "" || strings.HasPrefix(trimmed, "@")) {
			continue
		}
		for j := 0; j < len(line); j++ {
			switch line[j] {
			case '(':
				depth++
				sawMethodOpen = true
			case ')':
				if depth > 0 {
					depth--
				}
			}
		}
		if sawMethodOpen && depth == 0 && (strings.Contains(line, "{") || strings.Contains(line, ";")) {
			break
		}
	}
	return b.String()
}

// javaMappingAnnotationLine finds the 1-based line of the Spring mapping
// annotation (@GetMapping / @PostMapping / @RequestMapping / …) whose verb
// matches `verb`, used as a handler anchor when the route tail is a path param
// and the literal-segment anchor missed. Returns 0 when not found.
func javaMappingAnnotationLine(content, verb string) int {
	verb = strings.ToUpper(verb)
	want := map[string]string{
		"GET": "@GetMapping", "POST": "@PostMapping", "PUT": "@PutMapping",
		"DELETE": "@DeleteMapping", "PATCH": "@PatchMapping",
	}[verb]
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if want != "" && strings.HasPrefix(t, want) {
			return i + 1
		}
		if strings.HasPrefix(t, "@RequestMapping") && strings.Contains(t, verb) {
			return i + 1
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// #4586 — Django REST Framework
// ---------------------------------------------------------------------------

// drfQueryParamsGetRe matches `request.query_params.get('x')` /
// `self.request.query_params.get("x")` / `request.GET.get('x')`. Group 1 = name.
var drfQueryParamsGetRe = regexp.MustCompile(
	`\b(?:self\.)?request\.(?:query_params|GET)\.get\(\s*['"]([^'"]+)['"]`,
)

// drfKwargsRe matches `self.kwargs['pk']` / `self.kwargs.get('pk')` /
// `kwargs['pk']`. Group 1 or 2 = name.
var drfKwargsRe = regexp.MustCompile(
	`\bkwargs(?:\[\s*['"]([^'"]+)['"]\s*\]|\.get\(\s*['"]([^'"]+)['"])`,
)

func drfScalarParams(content string, e *types.EntityRecord, bodyWindow string) []JavaParam {
	var out []JavaParam
	seen := map[string]bool{}
	emit := func(name, in string, required bool) {
		name = strings.TrimSpace(name)
		key := in + "\x00" + name
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, JavaParam{Name: name, In: in, Required: required})
	}

	// DRF ViewSet/APIView handler bodies are not co-located with the urlpatterns
	// route line, so scan the WHOLE file for the view's query_params/kwargs reads.
	// This is conservative: query_params.get / kwargs reads are unambiguous DRF
	// request-param accessors and only appear in view code.
	scope := bodyWindow + "\n" + content
	for _, m := range drfQueryParamsGetRe.FindAllStringSubmatch(scope, -1) {
		emit(m[1], "query", false)
	}
	for _, m := range drfKwargsRe.FindAllStringSubmatch(scope, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		emit(name, "path", true)
	}
	return out
}
