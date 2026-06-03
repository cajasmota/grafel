// Endpoint pagination-posture stamping (epic #3628).
//
// A language-agnostic enrichment pass that runs at the tail of
// applyHTTPEndpointSynthesis, AFTER every per-language route synthesizer has
// emitted its http_endpoint_definition entities for the current file. Like the
// API-version / deprecation / rate-limit passes (see
// http_endpoint_deprecation.go), it mutates Properties on the just-emitted
// producer endpoints in place — it never adds or removes entities, so it cannot
// regress upstream synthesis.
//
// It answers the graph question the endpoint surface could not previously
// answer with confidence: "which list endpoints paginate, and HOW?".
//
// Property contract stamped on http_endpoint_definition:
//
//	paginated         — "true"  (present only when a clear pagination shape fired)
//	pagination_style  — "offset" | "page" | "cursor" | "keyset"
//	pagination_params — comma-joined, sorted query/param names that drive paging
//	                    (e.g. "limit,offset"); present when concrete params are known
//	pagination_source — the signal that fired (evidence for the dashboard)
//
// HONEST-PARTIAL boundary (the whole point of QUALITY-FIRST here): a lone
// `limit` with no offset/page/cursor companion is ambiguous (it could be a
// business cap, a result count, a rate value) and is NOT stamped. We only stamp
// when the pagination shape is unmistakable:
//
//   - a recognised pagination CLASS (DRF PageNumberPagination / LimitOffsetPagination
//     / CursorPagination, including a settings-level DEFAULT_PAGINATION_CLASS);
//   - a Spring `Pageable` handler param or a `Page<…>` return type;
//   - a param PAIR — (limit|take|page_size|per_page) together with
//     (offset|skip)  → offset/keyset, or `page`/`page_number` → page;
//   - a cursor/keyset token — `cursor`, `after`, `before`, `page_token`,
//     `next_token` → cursor;
//   - an ORM paginate shape inside the handler body — Sequelize
//     findAll({limit, offset}) / Prisma { take, skip } / Prisma `.cursor()` /
//     a Django `Paginator(qs, n)` / fastapi-pagination `paginate(...)`.
//
// Refs #3628.
package engine

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// paginationVerdict is the resolved pagination posture for one endpoint.
type paginationVerdict struct {
	paginated bool
	style     string // "offset" | "page" | "cursor" | "keyset"
	params    []string
	source    string // evidence: which signal fired
}

// applyEndpointPagination stamps pagination properties on every producer
// endpoint at index >= before in `entities` that belongs to `path`. The signal
// is resolved from the source region that decorates the endpoint's handler plus
// the handler body, and (for DRF) from class / settings-level defaults present
// in the file.
func applyEndpointPagination(lang, content, path string, entities []types.EntityRecord, before int) {
	if content == "" || before < 0 || before >= len(entities) {
		return
	}
	normLang := normalisePaginationLang(lang)

	// File-level DRF default (settings.py DEFAULT_PAGINATION_CLASS) applies to
	// every endpoint in scope that has no closer signal. Resolved once per file.
	var fileDefault *paginationVerdict
	if normLang == "python" {
		if v, ok := drfDefaultPaginationVerdict(content); ok {
			fileDefault = &v
		}
	}

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		v, ok := resolveEndpointPagination(normLang, content, e)
		if !ok && fileDefault != nil {
			v, ok = *fileDefault, true
		}
		if !ok || !v.paginated {
			continue
		}
		e.Properties["paginated"] = "true"
		if v.style != "" {
			e.Properties["pagination_style"] = v.style
		}
		if len(v.params) > 0 {
			e.Properties["pagination_params"] = strings.Join(uniqueSorted(v.params), ",")
		}
		if v.source != "" {
			e.Properties["pagination_source"] = v.source
		}
	}
}

func normalisePaginationLang(lang string) string {
	low := strings.ToLower(lang)
	switch low {
	case "typescript", "javascript_typescript":
		return "javascript"
	case "kotlin":
		return "java"
	}
	return low
}

// resolveEndpointPagination inspects the decorator region + handler body for a
// pagination shape. Order matters: an explicit recognised class / Spring
// Pageable / cursor token is unambiguous and wins; the param-pair heuristic and
// ORM shapes come next; everything else is left unstamped (honest-partial).
func resolveEndpointPagination(lang, content string, e *types.EntityRecord) (paginationVerdict, bool) {
	anchorLine := e.StartLine
	if anchorLine <= 0 {
		anchorLine = routeDeclarationLine(content, e.Properties["path"], e.Properties["verb"])
	}
	region, handlerStart := handlerDecoratorRegion(content, anchorLine)
	body := handlerBodyWindow(content, handlerStart)

	switch lang {
	case "python":
		if v, ok := drfClassPaginationVerdict(region, content, e); ok {
			return v, true
		}
		if v, ok := pythonPaginationVerdict(region, body); ok {
			return v, true
		}
	case "java":
		// For Spring the route annotation (@GetMapping) is the anchor and the
		// `Pageable` param / `Page<…>` return live on the handler SIGNATURE line
		// just below it — outside the upward decorator region. Include a small
		// forward window covering the signature.
		sig := forwardSignatureWindow(content, anchorLine)
		if v, ok := springPaginationVerdict(region + "\n" + sig); ok {
			return v, true
		}
		// JAX-RS / Jakarta REST, Quarkus, Micronaut, MicroProfile, Helidon,
		// Dropwizard use a different pagination surface than Spring (#3857):
		// Micronaut Pageable/Page<…> (Micronaut Data) + JAX-RS @QueryParam /
		// Micronaut @QueryValue limit/offset/cursor pairs. A class-level-@Path
		// JAX-RS resource with no method-level @Path anchors at the CLASS line
		// (routeDeclarationLine matches the class @Path), so the handler signature
		// carrying the @QueryParam annotations sits further down than the small
		// forward-signature window reaches — include the larger body window too.
		jaxrsScope := region + "\n" + sig + "\n" + handlerBodyWindowLarge(content, handlerStart)
		if v, ok := jaxrsPaginationVerdict(jaxrsScope); ok {
			return v, true
		}
	case "javascript":
		if v, ok := jsPaginationVerdict(region, body); ok {
			return v, true
		}
	case "go":
		// Go route registration and handler are SEPARATE functions, so the
		// StartLine-anchored body window above does not reach the handler. Locate
		// the handler func by its source_handler reference and scan its real body
		// for query-param reads (mirrors response_shape_go.go).
		if v, ok := goPaginationVerdict(content, e); ok {
			return v, true
		}
	}

	// Cross-language: a recognised pagination param shape declared in the
	// endpoint's own `parameters` / `parameter_schema` properties (set by the
	// route synthesizers) is language-agnostic.
	if v, ok := paramPropertyPaginationVerdict(e); ok {
		return v, true
	}
	return paginationVerdict{}, false
}

// uniqueSorted de-dups and sorts param names for a stable, comparable property.
func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Shared param-shape classifier
// ---------------------------------------------------------------------------

// Canonical pagination param vocabularies. A "limit-like" param caps the page
// size; an "offset-like" param skips a count; a "page-like" param indexes a
// page; a "cursor-like" param is an opaque keyset token.
var (
	paginationLimitParams  = map[string]bool{"limit": true, "take": true, "page_size": true, "per_page": true, "pagesize": true, "perpage": true, "first": true, "last": true, "size": true, "count": true}
	paginationOffsetParams = map[string]bool{"offset": true, "skip": true, "start": true}
	paginationPageParams   = map[string]bool{"page": true, "page_number": true, "pagenumber": true, "pageindex": true, "page_index": true, "pagenum": true}
	paginationCursorParams = map[string]bool{"cursor": true, "after": true, "before": true, "page_token": true, "pagetoken": true, "next_token": true, "nexttoken": true, "next_cursor": true, "starting_after": true, "ending_before": true}
)

// classifyParamShape resolves a pagination verdict from a SET of param names
// (already lower-cased). It is the honest core: a lone limit-like param is
// ambiguous and yields (false). A cursor token, a page index, or a
// limit+offset pair is a clear shape.
func classifyParamShape(present map[string]bool, source string) (paginationVerdict, bool) {
	var hasLimit, hasOffset, hasPage, hasCursor bool
	var limitName, offsetName, pageName, cursorName string
	for name := range present {
		switch {
		case paginationCursorParams[name]:
			hasCursor = true
			if cursorName == "" || name == "cursor" {
				cursorName = name
			}
		case paginationOffsetParams[name]:
			hasOffset = true
			if offsetName == "" || name == "offset" {
				offsetName = name
			}
		case paginationPageParams[name]:
			hasPage = true
			if pageName == "" || name == "page" {
				pageName = name
			}
		case paginationLimitParams[name]:
			hasLimit = true
			if limitName == "" || name == "limit" {
				limitName = name
			}
		}
	}

	switch {
	case hasCursor:
		// A cursor/keyset token is unambiguous on its own. Include a companion
		// limit-like param if present.
		params := []string{cursorName}
		if hasLimit {
			params = append(params, limitName)
		}
		return paginationVerdict{paginated: true, style: "cursor", params: params, source: source}, true
	case hasOffset:
		// offset implies a limit/offset windowing scheme (keyset-by-offset).
		params := []string{offsetName}
		if hasLimit {
			params = append(params, limitName)
		}
		return paginationVerdict{paginated: true, style: "offset", params: params, source: source}, true
	case hasPage:
		params := []string{pageName}
		if hasLimit {
			params = append(params, limitName)
		}
		return paginationVerdict{paginated: true, style: "page", params: params, source: source}, true
	default:
		// Only a limit-like param (or nothing) → ambiguous → not stamped.
		return paginationVerdict{}, false
	}
}

// ---------------------------------------------------------------------------
// Python — DRF pagination classes + settings default + param/ORM shapes
// ---------------------------------------------------------------------------

// drfPaginationClassStyle maps a recognised DRF pagination class name to its
// style. The keys are matched as whole identifiers in source.
var drfPaginationClassStyle = map[string]string{
	"PageNumberPagination":  "page",
	"LimitOffsetPagination": "offset",
	"CursorPagination":      "cursor",
}

// drfDefaultParamFor returns the conventional query params a DRF class reads, so
// pagination_params is populated even when the view sets only the class.
func drfDefaultParamsFor(style string) []string {
	switch style {
	case "page":
		return []string{"page"}
	case "offset":
		return []string{"limit", "offset"}
	case "cursor":
		return []string{"cursor"}
	}
	return nil
}

// drfClassPaginationVerdict resolves a `pagination_class = <Class>` assignment
// in the view class that owns the endpoint's handler. The decorator region of a
// DRF @action / method is inside the class body, so we scan the enclosing class
// — practically, the whole file is scanned for any pagination_class assignment;
// when a file declares exactly one, it is attributed to its endpoints. (A file
// with multiple distinct classes is handled by preferring the nearest preceding
// assignment to the handler.)
func drfClassPaginationVerdict(region, content string, e *types.EntityRecord) (paginationVerdict, bool) {
	// Nearest preceding `pagination_class = X` above the handler line wins.
	anchorLine := e.StartLine
	if anchorLine <= 0 {
		anchorLine = routeDeclarationLine(content, e.Properties["path"], e.Properties["verb"])
	}
	if cls, ok := nearestPaginationClass(content, anchorLine); ok {
		if style, known := drfPaginationClassStyle[cls]; known {
			return paginationVerdict{
				paginated: true,
				style:     style,
				params:    drfDefaultParamsFor(style),
				source:    "pagination_class=" + cls,
			}, true
		}
	}
	return paginationVerdict{}, false
}

// nearestPaginationClass returns the DRF pagination class assigned by the
// `pagination_class = X` statement that most closely precedes anchorLine in the
// file (1-based). If anchorLine is unknown (<=0) it returns the LAST assignment
// in the file when the file declares exactly one such assignment.
func nearestPaginationClass(content string, anchorLine int) (string, bool) {
	lines := strings.Split(content, "\n")
	best := ""
	bestLine := -1
	count := 0
	for i, line := range lines {
		cls, ok := paginationClassAssignment(line)
		if !ok {
			continue
		}
		count++
		ln := i + 1 // 1-based
		if anchorLine > 0 {
			if ln <= anchorLine && ln > bestLine {
				best = cls
				bestLine = ln
			}
		} else {
			best = cls // remember the last one
		}
	}
	if anchorLine > 0 {
		if best != "" {
			return best, true
		}
		// No preceding assignment, but if the file has exactly one, use it.
		if count == 1 {
			for _, line := range lines {
				if cls, ok := paginationClassAssignment(line); ok {
					return cls, true
				}
			}
		}
		return "", false
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// paginationClassAssignment parses a `pagination_class = SomePagination` line.
func paginationClassAssignment(line string) (string, bool) {
	t := strings.TrimSpace(line)
	const key = "pagination_class"
	if !strings.HasPrefix(t, key) {
		return "", false
	}
	rest := strings.TrimSpace(t[len(key):])
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	val := strings.TrimSpace(rest[1:])
	// Strip a trailing comment / call parens.
	val = strings.FieldsFunc(val, func(r rune) bool {
		return r == '#' || r == '(' || r == ' ' || r == '\t' || r == ','
	})[0]
	// Use the final dotted segment (e.g. pagination.CursorPagination).
	if idx := strings.LastIndex(val, "."); idx >= 0 {
		val = val[idx+1:]
	}
	if val == "" {
		return "", false
	}
	return val, true
}

// drfDefaultPaginationVerdict resolves a settings-level
// `DEFAULT_PAGINATION_CLASS` in a settings/config file (or any file declaring
// the REST_FRAMEWORK dict). It is the per-file fallback applied to endpoints
// with no closer signal.
func drfDefaultPaginationVerdict(content string) (paginationVerdict, bool) {
	idx := strings.Index(content, "DEFAULT_PAGINATION_CLASS")
	if idx < 0 {
		return paginationVerdict{}, false
	}
	// Look at the value to the right of the key on the same logical line.
	tail := content[idx:]
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		tail = tail[:nl]
	}
	for cls, style := range drfPaginationClassStyle {
		if strings.Contains(tail, cls) {
			return paginationVerdict{
				paginated: true,
				style:     style,
				params:    drfDefaultParamsFor(style),
				source:    "DEFAULT_PAGINATION_CLASS=" + cls,
			}, true
		}
	}
	return paginationVerdict{}, false
}

// pythonPaginationVerdict resolves param-based pagination from FastAPI Query
// params in the signature region + a Django Paginator / fastapi-pagination
// paginate() call in the body.
func pythonPaginationVerdict(region, body string) (paginationVerdict, bool) {
	// Django Paginator(qs, n) → page-style (the canonical Django param is `page`).
	if djangoPaginatorRe.MatchString(body) || djangoPaginatorRe.MatchString(region) {
		return paginationVerdict{paginated: true, style: "page", params: []string{"page"}, source: "Django Paginator"}, true
	}
	// fastapi-pagination paginate(...) call → page-style by default.
	if fastapiPaginateRe.MatchString(body) {
		return paginationVerdict{paginated: true, style: "page", params: []string{"page", "size"}, source: "fastapi-pagination paginate()"}, true
	}
	// FastAPI signature params: collect identifiers named like pagination params.
	present := pythonSignatureParams(region)
	if v, ok := classifyParamShape(present, "query params"); ok {
		return v, true
	}
	// ASGI/WSGI micro-frameworks (sanic / starlette / quart / litestar / flask)
	// read query params from the request object inside the handler body —
	// `request.args.get("limit")` / `request.query_params.get("offset")` /
	// `request.args["cursor"]` — rather than from typed signature params. Collect
	// those literal-named reads and run the same honest classifier.
	bodyParams := pythonRequestQueryParams(body)
	if v, ok := classifyParamShape(bodyParams, "request.query"); ok {
		return v, true
	}
	return paginationVerdict{}, false
}

// pythonRequestQueryParams collects pagination-shaped names read from the
// request object via `request.args.get("name")` / `request.args["name"]` /
// `request.query_params.get("name")` in the handler body.
func pythonRequestQueryParams(body string) map[string]bool {
	present := map[string]bool{}
	for _, m := range pyRequestQueryGetRe.FindAllStringSubmatch(body, -1) {
		name := strings.ToLower(m[1])
		if isPaginationParam(name) {
			present[name] = true
		}
	}
	return present
}

// pythonSignatureParams extracts the parameter identifiers from a (possibly
// multi-line) handler signature region that look like pagination params.
func pythonSignatureParams(region string) map[string]bool {
	present := map[string]bool{}
	for _, m := range pyParamDeclRe.FindAllStringSubmatch(region, -1) {
		name := strings.ToLower(m[1])
		if isPaginationParam(name) {
			present[name] = true
		}
	}
	return present
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring Pageable / Page<T>
// ---------------------------------------------------------------------------

// forwardSignatureWindow returns the few source lines at and just below the
// route-annotation anchor line (1-based), covering the handler method
// signature where a Spring `Pageable` param / `Page<…>` return type appears. It
// stops at the first `{` (method body open) or after a bounded number of lines.
func forwardSignatureWindow(content string, anchorLine int) string {
	if anchorLine <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	start := anchorLine - 1 // 0-based: the annotation line itself
	if start < 0 || start >= len(lines) {
		return ""
	}
	var b strings.Builder
	for i := start; i < len(lines) && i < start+6; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
		if strings.Contains(lines[i], "{") {
			break
		}
	}
	return b.String()
}

func springPaginationVerdict(region string) (paginationVerdict, bool) {
	if springPageableParamRe.MatchString(region) || springPageReturnRe.MatchString(region) {
		// Spring Data web binds Pageable from `page`, `size`, and `sort` query
		// params; the canonical style is page.
		return paginationVerdict{
			paginated: true,
			style:     "page",
			params:    []string{"page", "size"},
			source:    "Spring Pageable",
		}, true
	}
	return paginationVerdict{}, false
}

// ---------------------------------------------------------------------------
// JS / TS — Express/Node req.query reads + Sequelize/Prisma ORM shapes
// ---------------------------------------------------------------------------

func jsPaginationVerdict(region, body string) (paginationVerdict, bool) {
	// Prisma `.cursor(` inside the handler body → cursor style.
	if prismaCursorRe.MatchString(body) {
		params := []string{"cursor"}
		if sequelizeOrPrismaTakeRe.MatchString(body) {
			params = append(params, "take")
		}
		return paginationVerdict{paginated: true, style: "cursor", params: params, source: "Prisma cursor"}, true
	}
	// Sequelize findAll({limit, offset}) / Prisma { take, skip } → offset style.
	hasTake := sequelizeOrPrismaTakeRe.MatchString(body)
	hasSkip := sequelizeOrPrismaSkipRe.MatchString(body)
	hasLimit := sequelizeLimitRe.MatchString(body)
	hasOffset := sequelizeOffsetRe.MatchString(body)
	if (hasTake && hasSkip) || (hasLimit && hasOffset) {
		params := []string{}
		if hasOffset || hasSkip {
			if hasSkip {
				params = append(params, "skip")
			} else {
				params = append(params, "offset")
			}
		}
		if hasTake {
			params = append(params, "take")
		} else if hasLimit {
			params = append(params, "limit")
		}
		return paginationVerdict{paginated: true, style: "offset", params: params, source: "ORM limit/offset"}, true
	}
	// req.query.<param> reads in the handler body.
	present := jsQueryParams(body)
	if len(present) == 0 {
		present = jsQueryParams(region)
	}
	if v, ok := classifyParamShape(present, "req.query"); ok {
		return v, true
	}
	return paginationVerdict{}, false
}

// jsQueryParams collects pagination-shaped names read from `req.query.<name>`,
// `req.query["name"]`, or destructured `const { a, b } = req.query`.
func jsQueryParams(src string) map[string]bool {
	present := map[string]bool{}
	for _, m := range jsQueryDotRe.FindAllStringSubmatch(src, -1) {
		name := strings.ToLower(m[1])
		if isPaginationParam(name) {
			present[name] = true
		}
	}
	for _, m := range jsQueryBracketRe.FindAllStringSubmatch(src, -1) {
		name := strings.ToLower(m[1])
		if isPaginationParam(name) {
			present[name] = true
		}
	}
	// Destructuring: const { limit, offset } = req.query
	for _, m := range jsQueryDestructureRe.FindAllStringSubmatch(src, -1) {
		for _, raw := range strings.Split(m[1], ",") {
			name := strings.ToLower(strings.TrimSpace(strings.Split(raw, ":")[0]))
			name = strings.TrimSpace(strings.Split(name, "=")[0])
			if isPaginationParam(name) {
				present[name] = true
			}
		}
	}
	return present
}

// ---------------------------------------------------------------------------
// Go — gin / echo / chi / fiber / net-http query-param reads
// ---------------------------------------------------------------------------
//
// Pagination params are read from the request query string. The read idiom
// differs by framework but always names the param as a string literal:
//
//   - gin:      c.Query("limit") / c.DefaultQuery("offset","0") / c.GetQuery("cursor")
//   - echo:     c.QueryParam("limit")
//   - fiber:    c.Query("limit") / c.Query("offset","0") / c.QueryInt("page")
//   - net/http / chi (stdlib): r.URL.Query().Get("limit") /
//     r.FormValue("offset") / r.URL.Query()["page"]
//
// The collected names feed the shared classifyParamShape: a lone limit is
// ambiguous (NOT stamped); limit+offset → offset, page → page, cursor → cursor.
// HONEST-PARTIAL: a param read into a variable but named only dynamically
// (`c.Query(name)`) does not match and is skipped.

// goQueryReadRe matches a query-param read whose key is a string literal:
//
//	c.Query("limit") / c.DefaultQuery("limit", "10") / c.GetQuery("cursor")
//	c.QueryParam("limit") / c.QueryInt("page") / c.Queries() handled separately
//	r.URL.Query().Get("limit") / r.FormValue("offset")
//
// Group 1 is the param name.
var goQueryReadRe = regexp.MustCompile(
	`\.\s*(?:Query|DefaultQuery|GetQuery|QueryParam|QueryArray|QueryInt|QueryBool|DefaultQueryInt|Get|FormValue|PostFormValue|URLParam)\s*\(\s*["` + "`" + `]([A-Za-z_][\w-]*)["` + "`" + `]`,
)

// goQueryBracketRe matches `r.URL.Query()["limit"]` bracket-index reads.
var goQueryBracketRe = regexp.MustCompile(
	`\.\s*Query\s*\(\s*\)\s*\[\s*["` + "`" + `]([A-Za-z_][\w-]*)["` + "`" + `]`,
)

// goPaginationVerdict resolves param-based pagination from query-param reads in
// the Go handler body, located via the endpoint's source_handler reference.
func goPaginationVerdict(content string, e *types.EntityRecord) (paginationVerdict, bool) {
	handler := e.Properties["source_handler"]
	if idx := strings.Index(handler, ":"); idx >= 0 {
		handler = handler[idx+1:]
	}
	if handler == "" {
		return paginationVerdict{}, false
	}
	body := findGoHandlerBody(content, handler)
	if body == "" {
		return paginationVerdict{}, false
	}
	present := map[string]bool{}
	for _, re := range []*regexp.Regexp{goQueryReadRe, goQueryBracketRe} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			name := strings.ToLower(strings.ReplaceAll(m[1], "-", "_"))
			if isPaginationParam(name) {
				present[name] = true
			}
		}
	}
	return classifyParamShape(present, "query params")
}

// ---------------------------------------------------------------------------
// Cross-language: param/parameter_schema properties on the endpoint
// ---------------------------------------------------------------------------

// paramPropertyPaginationVerdict resolves pagination from the endpoint's own
// `parameters` (comma-joined names) / `parameter_schema` (JSON blob) props set
// by the route synthesizers.
func paramPropertyPaginationVerdict(e *types.EntityRecord) (paginationVerdict, bool) {
	present := map[string]bool{}
	if params := e.Properties["parameters"]; params != "" {
		for _, p := range strings.Split(params, ",") {
			name := strings.ToLower(strings.TrimSpace(p))
			if isPaginationParam(name) {
				present[name] = true
			}
		}
	}
	if schema := e.Properties["parameter_schema"]; schema != "" {
		for _, m := range schemaNameRe.FindAllStringSubmatch(schema, -1) {
			name := strings.ToLower(m[1])
			if isPaginationParam(name) {
				present[name] = true
			}
		}
	}
	if len(present) == 0 {
		return paginationVerdict{}, false
	}
	return classifyParamShape(present, "parameters")
}

func isPaginationParam(name string) bool {
	return paginationLimitParams[name] || paginationOffsetParams[name] ||
		paginationPageParams[name] || paginationCursorParams[name]
}
