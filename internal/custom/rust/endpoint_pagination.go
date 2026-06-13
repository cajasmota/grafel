// endpoint_pagination.go — pagination-posture stamping for Rust web frameworks
// (#5019, child of epic #3628 cross-language fan-out, Routing/
// endpoint_pagination_posture). Sibling of endpoint_response_codes.go and
// endpoint_deprecation.go.
//
// Rust greenfield: prior to this pass every Rust HTTP-framework cell for
// endpoint_pagination_posture was `missing` (13/13). The flagship engine pass
// (internal/engine/http_endpoint_pagination.go, applyEndpointPagination) stamps
// a flat pagination contract on synthesised `http_endpoint_definition`
// entities — but Rust HTTP endpoints are emitted as `SCOPE.Operation/endpoint`
// entities by the custom .rs route extractors, so the engine pass — gated on
// Kind==http_endpoint_definition — can never reach them. Same gating issue as
// endpoint_response_codes; the resolution is identical: re-emit the endpoint op
// carrying the pagination contract from the framework's own idioms, merging onto
// the producer route op by Name via MergeWithCustom.
//
// Property contract (mirrors the flagship http_endpoint_pagination.go):
//
//	paginated         — "true" (present only when a clear pagination shape fired)
//	pagination_style  — "offset" | "page" | "cursor"
//	pagination_params — comma-joined, sorted query/param names that drive paging
//	                    (e.g. "limit,offset"); present when concrete params known
//	pagination_source — the signal that fired (evidence for the dashboard)
//
// HONEST-PARTIAL boundary (the whole point of QUALITY-FIRST here): a lone
// `limit` with no offset/page/cursor companion is ambiguous (it could be a
// business cap, a result count, a rate value) and is NOT stamped. We only stamp
// when the pagination shape is unmistakable:
//
//   - a param PAIR — (limit|take|page_size|per_page|size) together with
//     (offset|skip) → offset, or `page`/`page_number` → page;
//   - a cursor/keyset token — `cursor`, `after`, `before`, `page_token`,
//     `next_token` → cursor (unambiguous on its own);
//   - an ORM paginate shape inside the handler body — diesel/sqlx
//     `.limit(...).offset(...)` (offset), or a sea_orm `.paginate(...)` /
//     `Paginator` (page, the sea_orm paginator is page-indexed).
//
// Two ways the param names are recovered:
//
//	1. A typed extractor `Query<Struct>` / `web::Query<Struct>` handler param —
//	   the named struct's `#[derive(Deserialize)]` fields name the query params.
//	   We resolve the struct definition elsewhere in the file and read its field
//	   identifiers. (axum / actix idiom.)
//	2. Direct query-string reads in the handler body — `params.get("limit")`,
//	   `query.get("offset")`, `req.query::<Pagination>()` field access — collected
//	   as string-literal param names.
//
// Three recognised Rust route surfaces (Names match the producer extractors so
// the stamped op merges onto the plain route op by Name): axum
// `.route("/p", verb(handler))`, actix `#[get("/p")]`, rocket `#[get("/p")]`.
// The remaining handler-named frameworks (poem/warp/tide/gotham/salvo) reuse the
// SAME handler→verdict map via the producer route regexes, exactly as
// endpoint_response_codes.go does. hyper (#5101) is also recovered: a
// `match (method, path)` arm either NAMES a handler (resolved via the shared
// handler→verdict map) or is an INLINE block whose body carries the pagination
// idiom directly (resolved on the arm-body window, clipped at the next arm).
// tower has no verb+path route DSL of its own (pagination posture is app-level),
// so it is structurally not_applicable — same disposition as response_codes.
//
// Honest-partial (NEVER fabricated): a handler with NO resolvable pagination
// shape is NOT re-emitted (the plain route op from the producer extractor
// stands). A lone limit-like param is ambiguous and not stamped.
//
// Honesty: partial — heuristic regex on the handler body + the referenced query
// struct, scoped to the framework's own route idioms.
//
// Refs #5019, #3628.
package rust

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_endpoint_pagination", &rustEndpointPaginationExtractor{})
}

type rustEndpointPaginationExtractor struct{}

func (e *rustEndpointPaginationExtractor) Language() string {
	return "custom_rust_endpoint_pagination"
}

// --- pagination param vocabularies + classifier -------------------------------
//
// Rust-local copy of the cross-language vocabularies in
// internal/engine/http_endpoint_pagination.go (the engine package is not
// importable here, the same way endpoint_response_codes.go keeps a local
// StatusCode table). Keep the two in sync.

var (
	rustPaginationLimitParams  = map[string]bool{"limit": true, "take": true, "page_size": true, "per_page": true, "pagesize": true, "perpage": true, "first": true, "last": true, "size": true, "count": true}
	rustPaginationOffsetParams = map[string]bool{"offset": true, "skip": true, "start": true}
	rustPaginationPageParams   = map[string]bool{"page": true, "page_number": true, "pagenumber": true, "pageindex": true, "page_index": true, "pagenum": true}
	rustPaginationCursorParams = map[string]bool{"cursor": true, "after": true, "before": true, "page_token": true, "pagetoken": true, "next_token": true, "nexttoken": true, "next_cursor": true, "starting_after": true, "ending_before": true}
)

func rustIsPaginationParam(name string) bool {
	return rustPaginationLimitParams[name] || rustPaginationOffsetParams[name] ||
		rustPaginationPageParams[name] || rustPaginationCursorParams[name]
}

// rustPaginationVerdict is the resolved pagination posture for one endpoint.
type rustPaginationVerdict struct {
	paginated bool
	style     string // "offset" | "page" | "cursor"
	params    []string
	source    string
}

// rustClassifyParamShape resolves a verdict from a SET of param names (already
// lower-cased). Honest core: a lone limit-like param is ambiguous and yields
// (false). A cursor token, a page index, or a limit+offset pair is a clear
// shape. Mirrors the flagship classifyParamShape.
func rustClassifyParamShape(present map[string]bool, source string) (rustPaginationVerdict, bool) {
	var hasLimit, hasOffset, hasPage, hasCursor bool
	var limitName, offsetName, pageName, cursorName string
	for name := range present {
		switch {
		case rustPaginationCursorParams[name]:
			hasCursor = true
			if cursorName == "" || name == "cursor" {
				cursorName = name
			}
		case rustPaginationOffsetParams[name]:
			hasOffset = true
			if offsetName == "" || name == "offset" {
				offsetName = name
			}
		case rustPaginationPageParams[name]:
			hasPage = true
			if pageName == "" || name == "page" {
				pageName = name
			}
		case rustPaginationLimitParams[name]:
			hasLimit = true
			if limitName == "" || name == "limit" {
				limitName = name
			}
		}
	}

	switch {
	case hasCursor:
		params := []string{cursorName}
		if hasLimit {
			params = append(params, limitName)
		}
		return rustPaginationVerdict{paginated: true, style: "cursor", params: params, source: source}, true
	case hasOffset:
		params := []string{offsetName}
		if hasLimit {
			params = append(params, limitName)
		}
		return rustPaginationVerdict{paginated: true, style: "offset", params: params, source: source}, true
	case hasPage:
		params := []string{pageName}
		if hasLimit {
			params = append(params, limitName)
		}
		return rustPaginationVerdict{paginated: true, style: "page", params: params, source: source}, true
	default:
		return rustPaginationVerdict{}, false
	}
}

// rustUniqueSortedParams de-dups + sorts param names for a stable property.
func rustUniqueSortedParams(in []string) []string {
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

// --- signal regexes -----------------------------------------------------------

// rustQueryTypeRe matches a typed query-extractor handler param naming a struct:
// `Query<Pagination>`, `web::Query<Params>`, `Query(params): Query<Pagination>`.
// Group 1 = the struct type name (final segment).
var rustQueryTypeRe = regexp.MustCompile(`\bQuery\s*<\s*([A-Za-z_]\w*)\b`)

// rustStructFieldRe matches a `pub? name: Type` field declaration inside a
// struct body. Anchored at a line start OR just after a comma (single-line
// structs like `struct P { limit: u32, offset: u32 }` declare several fields on
// one line). Group 1 = the field identifier. A leading `#[...]` field attribute
// line is skipped naturally (it has no `ident:` at that position).
var rustStructFieldRe = regexp.MustCompile(`(?m)(?:^|,)\s*(?:pub\s+(?:\([^)]*\)\s*)?)?([a-z_]\w*)\s*:`)

// rustQueryLiteralReadRe matches a string-literal query read in a handler body:
// `params.get("limit")`, `query.get("offset")`, `q.get("cursor")`,
// `get_query_param("page")`. Group 1 = the param name.
var rustQueryLiteralReadRe = regexp.MustCompile(`\.?\b(?:get|get_query_param|query_param)\s*\(\s*"([A-Za-z_][\w-]*)"`)

// rustDieselLimitRe / rustDieselOffsetRe match the diesel/sqlx `.limit(...)` /
// `.offset(...)` query-builder calls (a literal or a variable arg, both count —
// the pair is the offset-pagination signal).
var rustDieselLimitRe = regexp.MustCompile(`\.\s*limit\s*\(`)
var rustDieselOffsetRe = regexp.MustCompile(`\.\s*offset\s*\(`)

// rustSeaOrmPaginateRe matches a sea_orm `.paginate(db, page_size)` call or a
// `Paginator` / `PaginatorTrait` reference — the sea_orm page-indexed paginator.
var rustSeaOrmPaginateRe = regexp.MustCompile(`\.\s*paginate\s*\(|\bPaginator(?:Trait)?\b`)

// --- verdict resolution -------------------------------------------------------

// rustResolveQueryStructParams resolves the param names declared by a
// `Query<Struct>` typed extractor referenced in a handler signature region, by
// locating the struct definition elsewhere in `src` and reading its fields.
func rustResolveQueryStructParams(src, region string) map[string]bool {
	present := map[string]bool{}
	for _, m := range rustQueryTypeRe.FindAllStringSubmatch(region, -1) {
		structName := m[1]
		body := rustStructBodyByName(src, structName)
		if body == "" {
			continue
		}
		for _, fm := range rustStructFieldRe.FindAllStringSubmatch(body, -1) {
			name := strings.ToLower(fm[1])
			if rustIsPaginationParam(name) {
				present[name] = true
			}
		}
	}
	return present
}

// rustStructBodyByName returns the brace-delimited body of `struct <name> {…}`,
// locating the struct by name then delegating to the shared rustStructBody
// brace-matcher (fw_validation.go).
func rustStructBodyByName(src, name string) string {
	re := regexp.MustCompile(`\bstruct\s+` + regexp.QuoteMeta(name) + `\b`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	body, _, ok := rustStructBody(src, loc[0])
	if !ok {
		return ""
	}
	return body
}

// rustResolvePagination inspects a handler signature region + body for a
// pagination shape. Order: ORM paginate shapes (unambiguous) → typed Query
// struct fields → literal query reads. Honest-partial: no clear shape → (false).
func rustResolvePagination(src, region, body string) (rustPaginationVerdict, bool) {
	// sea_orm paginate / Paginator → page style (page-indexed paginator).
	if rustSeaOrmPaginateRe.MatchString(body) {
		return rustPaginationVerdict{paginated: true, style: "page", params: []string{"page"}, source: "sea_orm Paginator"}, true
	}
	// diesel/sqlx .limit().offset() PAIR → offset style.
	if rustDieselLimitRe.MatchString(body) && rustDieselOffsetRe.MatchString(body) {
		return rustPaginationVerdict{paginated: true, style: "offset", params: []string{"limit", "offset"}, source: "diesel/sqlx limit/offset"}, true
	}
	// Typed Query<Struct> param fields.
	if present := rustResolveQueryStructParams(src, region); len(present) > 0 {
		if v, ok := rustClassifyParamShape(present, "Query<…> struct"); ok {
			return v, true
		}
	}
	// Literal query-string reads in the body.
	present := map[string]bool{}
	for _, m := range rustQueryLiteralReadRe.FindAllStringSubmatch(body, -1) {
		name := strings.ToLower(strings.ReplaceAll(m[1], "-", "_"))
		if rustIsPaginationParam(name) {
			present[name] = true
		}
	}
	if v, ok := rustClassifyParamShape(present, "query params"); ok {
		return v, true
	}
	return rustPaginationVerdict{}, false
}

// rustStampPagination writes the flat pagination contract onto an endpoint.
// No-op when the verdict is not paginated.
func rustStampPagination(e *types.EntityRecord, v rustPaginationVerdict) bool {
	if !v.paginated {
		return false
	}
	setProps(e, "paginated", "true")
	if v.style != "" {
		setProps(e, "pagination_style", v.style)
	}
	if len(v.params) > 0 {
		setProps(e, "pagination_params", strings.Join(rustUniqueSortedParams(v.params), ","))
	}
	if v.source != "" {
		setProps(e, "pagination_source", v.source)
	}
	return true
}

// rustPaginationHandlerWindow returns a window covering both the handler
// SIGNATURE (where the `Query<Struct>` typed param sits) and the body (where the
// ORM/literal idioms live), clipped at the next sibling fn.
func rustPaginationHandlerWindow(src string, bodyStart int) string {
	return rustRespBodyWindow(src, bodyStart)
}

// --- extractor entry point ----------------------------------------------------

func (e *rustEndpointPaginationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_endpoint_pagination.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: a pagination surface must mention a pagination idiom.
	if !strings.Contains(src, "Query<") && !strings.Contains(src, ".limit(") &&
		!strings.Contains(src, ".offset(") && !strings.Contains(src, ".paginate(") &&
		!strings.Contains(src, "Paginator") && !strings.Contains(src, ".get(") {
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
	for _, ent := range e.extractHandlerNamed(src, file) {
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// --- axum surface -------------------------------------------------------------

func (e *rustEndpointPaginationExtractor) extractAxum(src string, file extractor.FileInput) []types.EntityRecord {
	if !strings.Contains(src, ".route") && !strings.Contains(src, ".nest") {
		return nil
	}

	// Build handler-name → verdict from every fn body (signature + body window).
	handlerVerdicts := map[string]rustPaginationVerdict{}
	for _, fm := range rustDepFnRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[fm[2]:fm[3]]
		window := rustPaginationHandlerWindow(src, fm[1])
		if v, ok := rustResolvePagination(src, window, window); ok {
			handlerVerdicts[name] = v
		}
	}
	if len(handlerVerdicts) == 0 {
		return nil
	}

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
			verdict, ok := handlerVerdicts[handler]
			if !ok {
				continue
			}
			seen[name] = true
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_PAGINATION",
				"http_method", method, "route_path", fullPath, "handler_name", handler)
			if prefix != "" {
				setProps(&ent, "nest_prefix", prefix)
			}
			rustStampPagination(&ent, verdict)
			out = append(out, ent)
		}
	}
	return out
}

// --- actix-web / rocket macro surface -----------------------------------------

func (e *rustEndpointPaginationExtractor) extractMacroFramework(src string, file extractor.FileInput, framework string) []types.EntityRecord {
	switch framework {
	case "actix_web":
		if !strings.Contains(src, "actix") && !strings.Contains(src, "web::") &&
			!strings.Contains(src, "Responder") && !strings.Contains(src, "HttpResponse") {
			return nil
		}
	case "rocket":
		if !strings.Contains(src, "rocket") && !strings.Contains(src, "routes!") &&
			!strings.Contains(src, "#[launch]") {
			return nil
		}
	}

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

		handler, bodyStart := rustFnAfter(src, m[1])
		if bodyStart < 0 {
			continue
		}

		fullPath := path
		if framework == "rocket" {
			fullPath = rustJoinPaths(mountPrefix[handler], path)
		}
		name := method + " " + fullPath
		if seen[name] {
			continue
		}

		window := rustPaginationHandlerWindow(src, bodyStart)
		verdict, ok := rustResolvePagination(src, window, window)
		if !ok {
			continue
		}
		seen[name] = true

		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework,
			"provenance", "INFERRED_FROM_"+strings.ToUpper(framework)+"_PAGINATION",
			"http_method", method, "route_pattern", fullPath)
		if handler != "" {
			setProps(&ent, "handler_name", handler)
		}
		if framework == "rocket" && mountPrefix[handler] != "" {
			setProps(&ent, "mount_prefix", mountPrefix[handler])
		}
		rustStampPagination(&ent, verdict)
		out = append(out, ent)
	}
	return out
}

// --- remaining handler-named HTTP frameworks ----------------------------------
//
// poem / warp / tide / gotham / salvo attribute a route to a named handler whose
// body lives elsewhere in the file (the axum situation). Identical recipe:
// build a handler→verdict map once, re-run each producer route regex, stamp the
// verdict onto routes that name a resolving handler. hyper is recovered below via
// its match-arm dispatch (#5101); tower has no route DSL (not_applicable).
// Mirrors endpoint_response_codes.go extractHandlerNamed.
func (e *rustEndpointPaginationExtractor) extractHandlerNamed(src string, file extractor.FileInput) []types.EntityRecord {
	handlerVerdicts := map[string]rustPaginationVerdict{}
	for _, fm := range rustDepFnRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[fm[2]:fm[3]]
		window := rustPaginationHandlerWindow(src, fm[1])
		if v, ok := rustResolvePagination(src, window, window); ok {
			handlerVerdicts[name] = v
		}
	}
	if len(handlerVerdicts) == 0 {
		return nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	emit := func(framework, method, path, handler string, off int) {
		verdict, ok := handlerVerdicts[handler]
		if !ok {
			return
		}
		name := method + " " + path
		if seen[name] {
			return
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, off))
		setProps(&ent, "framework", framework,
			"provenance", "INFERRED_FROM_"+strings.ToUpper(framework)+"_PAGINATION",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		rustStampPagination(&ent, verdict)
		out = append(out, ent)
	}

	if strings.Contains(src, ".at") {
		for _, m := range rePoemAt.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			methodRouter := src[m[4]:m[5]]
			for _, vm := range rePoemVerb.FindAllStringSubmatch(methodRouter, -1) {
				emit("poem", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
		for _, m := range reTideAt.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			verbChain := src[m[4]:m[5]]
			for _, vm := range reTideVerb.FindAllStringSubmatch(verbChain, -1) {
				emit("tide", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	if strings.Contains(src, "route.") || strings.Contains(src, ".associate") {
		for _, m := range reGothamRoute.FindAllStringSubmatchIndex(src, -1) {
			emit("gotham", strings.ToUpper(src[m[2]:m[3]]), rustNormalizePath(src[m[4]:m[5]]), src[m[6]:m[7]], m[0])
		}
		for _, m := range reGothamAssociate.FindAllStringSubmatchIndex(src, -1) {
			path := rustNormalizePath(src[m[2]:m[3]])
			body := src[m[4]:m[5]]
			for _, vm := range reGothamAssocVerb.FindAllStringSubmatch(body, -1) {
				emit("gotham", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	if strings.Contains(src, "Router") || strings.Contains(src, "router") {
		for _, m := range reSalvoPath.FindAllStringSubmatchIndex(src, -1) {
			var withPath, dotPath string
			if m[2] >= 0 {
				withPath = rustNormalizePath(src[m[2]:m[3]])
			}
			if m[4] >= 0 {
				dotPath = rustNormalizePath(src[m[4]:m[5]])
			}
			path := rustJoinPaths(withPath, dotPath)
			if path != "" && !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			verbChain := src[m[6]:m[7]]
			for _, vm := range reSalvoVerb.FindAllStringSubmatch(verbChain, -1) {
				emit("salvo", strings.ToUpper(vm[1]), path, vm[2], m[0])
			}
		}
	}

	if strings.Contains(src, "warp::") {
		for _, m := range reWarpChain.FindAllStringSubmatchIndex(src, -1) {
			blob := src[m[0]:m[1]]
			handler := src[m[2]:m[3]]
			method := "GET"
			if mm := reWarpChainMethod.FindStringSubmatch(blob); mm != nil {
				method = strings.ToUpper(mm[1])
			}
			path := ""
			if pm := reWarpPathMacroIn.FindStringSubmatch(blob); pm != nil {
				path = normWarpPath(pm[1])
			} else if pf := reWarpPathFn.FindStringSubmatch(blob); pf != nil {
				path = "/" + strings.Trim(pf[1], "/")
			}
			if path == "" {
				continue
			}
			emit("warp", method, path, handler, m[0])
		}
	}

	// hyper — #5101. `match (req.method(), path) { (&Method::GET, "/p") => handler(req) }`.
	// Two arm shapes, mirroring endpoint_response_codes.go's hyper recovery:
	//   1. NAMED-handler arm (`=> handler(req)`) — the RHS names a handler whose
	//      body lives elsewhere; resolve via the shared handlerVerdicts map (the
	//      map is already built above from every fn body), exactly like axum/poem.
	//   2. INLINE-block arm (`=> { … .limit().offset() … }`) — the pagination idiom
	//      is written directly in the arm body (no separate handler fn). Resolve the
	//      arm body window directly, hard-clipped at the NEXT match arm so a sibling
	//      arm's pagination signal never bleeds in. Reuses rustHyperRespArmRe /
	//      rustHyperRespInlineArmRe / rustHyperArmBoundaryRe (endpoint_response_codes.go).
	// Honest-partial holds: an arm whose handler/body resolves no clear pagination
	// shape is NOT re-emitted (the producer route op stands).
	if strings.Contains(src, "Method::") {
		// Named-handler arms: `=> handler(req)`. emit() looks the handler up in the
		// shared verdict map and skips it (no-op) when unresolved.
		for _, m := range rustHyperRespArmRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[m[2]:m[3]])
			path := rustNormalizePath(src[m[4]:m[5]])
			handler := src[m[6]:m[7]]
			emit("hyper", method, path, handler, m[0])
		}
		// Inline-block arms: `=> { … }`. Resolve the arm body window directly.
		for _, m := range rustHyperRespInlineArmRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[m[2]:m[3]])
			path := rustNormalizePath(src[m[4]:m[5]])
			name := method + " " + path
			if seen[name] {
				continue
			}
			// m[1] is just past the matched `{`; clip the window at the next arm so a
			// sibling arm's pagination signal never bleeds into this verdict.
			window := rustRespBodyWindow(src, m[1])
			if loc := rustHyperArmBoundaryRe.FindStringIndex(window); loc != nil {
				window = window[:loc[0]]
			}
			verdict, ok := rustResolvePagination(src, window, window)
			if !ok {
				continue
			}
			seen[name] = true
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "hyper",
				"provenance", "INFERRED_FROM_HYPER_PAGINATION",
				"http_method", method, "route_pattern", path)
			rustStampPagination(&ent, verdict)
			out = append(out, ent)
		}
	}

	return out
}
