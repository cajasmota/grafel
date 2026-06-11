// Go response-shape extraction for Gin / Echo / Chi handlers.
//
// Patterns recognized inside a handler body:
//
//   - c.JSON(http.StatusOK, gin.H{"a": 1, "b": "x"})    Gin map literal
//   - c.JSON(200, &MyDto{A: 1, B: "x"})                 typed struct
//   - c.JSON(http.StatusBadRequest, gin.H{"error":...}) error path
//   - c.JSON(200, structInstance)                        free variable
//   - render.JSON(w, r, payload)                         Chi via go-chi/render
//   - json.NewEncoder(w).Encode(payload)                 Chi stdlib
//   - return ctx.JSON(http.StatusOK, dto)                Echo
//
// For typed responses (`&MyDto{...}` or a named identifier whose type
// resolves to a struct in this file), the struct's exported fields are
// walked into response_schema.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// goRouteRe matches the canonical Gin / Echo / Chi / Fiber route registration,
// in BOTH the upper-case (gin/echo) and idiomatic title-case (chi/fiber/echo)
// method-name spellings:
//
//	r.GET("/path", handlerFunc)        // gin / echo
//	router.POST("/users/:id", h.Create)
//	r.Get("/users", h.List)            // chi / fiber (idiomatic title-case)
//	app.Delete("/users/:id", deleteUser)
//
// Group 1 is the RECEIVER variable (the router or route-group on which the
// verb method is invoked), group 2 is the verb, group 3 is the path, group 4
// is the handler identifier (may be qualified, e.g. `h.Create`). The handler
// is the bare or last-component name so the shape extractor can locate its
// definition in the same file. The receiver name lets synthesis resolve a
// `r.Group("/v1")` prefix and prepend it to the route path (#4408).
//
// The title-case spelling is matched here (it was previously only matched by
// the ROUTES_TO-edge pass in go_routes.go) so that idiomatic chi/fiber/echo
// handlers receive an http_endpoint_definition entity — which the
// response-codes / pagination enrichment passes (#3920) then stamp.
var goRouteRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Get|Post|Put|Patch|Delete|Head|Options)\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]` + "`" + `?\s*,\s*([\w.]+)`,
)

// goGroupAssignRe captures a gin/echo/chi route-GROUP assignment so its path
// prefix can be prepended to every route registered on the group variable
// (#4408). Recognised shapes:
//
//	v1 := r.Group("/api/v1")              // gin / echo
//	admin := v1.Group("/admin", authMW)   // gin extra-middleware args
//	g = base.Group("/g")                  // = reassignment
//
// Group 1 = the assigned group variable, group 2 = the parent receiver
// (router or an enclosing group), group 3 = the literal prefix. Echo's
// `e.Group("/x")` and chi's `r.Route("/x", fn)` differ — echo uses this
// `.Group(` spelling; chi's closure-based `r.Route(` is captured by
// goChiRouteHeadRe and resolved positionally (#4782).
var goGroupAssignRe = regexp.MustCompile(
	`\b(\w+)\s*:?=\s*(\w+)\s*\.\s*Group\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]*)["` + "`" + `]` + "`" + `?`,
)

// goChiRouteHeadRe captures the HEAD of a chi closure-based sub-router mount:
//
//	r.Route("/api", func(r chi.Router) {        // canonical
//	router.Route("/v1", func(sub chi.Router) {  // arbitrary param name
//	r.Route("/admin", func(r chi.Router) { ... })
//
// chi sub-routers nest via a closure (NOT the `.Group(` spelling): the
// `Route(prefix, fn)` mounts `fn`'s body under `prefix`, and the router
// param passed into `fn` (often `r`, shadowing the outer router) is the
// receiver every route inside the closure registers on. Because the param
// is commonly re-`r`, a name-keyed prefix map (as goGroupPrefixIndex uses)
// would be ambiguous — so chi prefixes are resolved POSITIONALLY instead:
// the closure body's byte span carries the prefix, and a route's prefix is
// the concatenation of every enclosing Route-span's segment (#4782).
//
// Group 1 = the literal mount prefix. The trailing `{` anchors the closure
// body so we can brace-match its extent.
var goChiRouteHeadRe = regexp.MustCompile(
	`\b\w+\s*\.\s*Route\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]*)["` + "`" + `]` + "`" + `?\s*,\s*func\s*\([^)]*\)\s*\{`,
)

// goFrameworkFromImports returns the framework name based on package
// imports observable in the source. Falls back to "gin" when none of
// the three explicit markers match (Gin is the most common, and the
// response-shape extractor treats gin/echo/chi identically).
func goFrameworkFromImports(content string) string {
	switch {
	case strings.Contains(content, "github.com/labstack/echo"):
		return "echo"
	case strings.Contains(content, "github.com/go-chi/chi"):
		return "chi"
	case strings.Contains(content, "github.com/gofiber/fiber"):
		return "fiber"
	case strings.Contains(content, "github.com/gin-gonic/gin"):
		return "gin"
	}
	return "gin"
}

// goFileImportsHTTPRouter reports whether the file imports one of the supported
// Go HTTP router libraries whose registration DSL uses title-case verb methods
// (chi / fiber / echo / gin). Used to gate the title-case `.Get(`/`.Post(` route
// match so it does not fire on unrelated `.Get(` calls (maps, caches, etc.).
func goFileImportsHTTPRouter(content string) bool {
	return strings.Contains(content, "github.com/labstack/echo") ||
		strings.Contains(content, "github.com/go-chi/chi") ||
		strings.Contains(content, "github.com/gofiber/fiber") ||
		strings.Contains(content, "github.com/gin-gonic/gin")
}

// synthesizeGoRouters scans a Go file for HTTP route registrations
// against a Gin, Echo, or Chi router and emits one http_endpoint per
// (verb, path) pair. The handler identifier is recorded so the response
// shape extractor can walk back to the handler body.
func synthesizeGoRouters(content string, emit emitFn) {
	if !strings.Contains(content, ".GET(") && !strings.Contains(content, ".POST(") &&
		!strings.Contains(content, ".PUT(") && !strings.Contains(content, ".PATCH(") &&
		!strings.Contains(content, ".DELETE(") && !strings.Contains(content, ".HEAD(") &&
		!strings.Contains(content, ".OPTIONS(") &&
		!strings.Contains(content, ".Get(") && !strings.Contains(content, ".Post(") &&
		!strings.Contains(content, ".Put(") && !strings.Contains(content, ".Patch(") &&
		!strings.Contains(content, ".Delete(") && !strings.Contains(content, ".Head(") &&
		!strings.Contains(content, ".Options(") {
		return
	}
	framework := goFrameworkFromImports(content)
	// Title-case verb spellings (`.Get(`, `.Post(`, …) are common on non-router
	// receivers too (a map/cache `.Get("k", v)`), so they only count as routes
	// when the file actually imports a known Go HTTP router. Upper-case spellings
	// (`.GET(`) are router-specific and need no such gate. This keeps the
	// false-positive rate near zero while unlocking idiomatic chi/fiber/echo.
	hasRouterImport := goFileImportsHTTPRouter(content)
	// #4408 — resolve route-group prefixes (`v1 := r.Group("/v1")`, including
	// nested groups `admin := v1.Group("/admin")`) so a route registered on a
	// group variable synthesizes at its fully-prefixed path.
	groupPrefix := goGroupPrefixIndex(content)
	// #4782 — resolve chi closure-based sub-router prefixes (`r.Route("/api",
	// func(r chi.Router){ ... })`), accumulated transitively across nesting.
	// Unlike gin/echo groups these are keyed by the closure body's byte span
	// (the router param is commonly re-`r`, shadowing the parent), so a route
	// at byte offset P inherits every enclosing Route-span's prefix.
	chiSpans := goChiRouteSpans(content)
	locs := goRouteRe.FindAllStringSubmatchIndex(content, -1)
	for _, loc := range locs {
		m := groupSubmatchStrings(content, loc)
		if len(m) < 5 {
			continue
		}
		routePos := loc[0]
		recv := m[1]
		rawVerb := m[2]
		// Normalise the verb to upper-case so the endpoint key is canonical
		// regardless of the title-case (chi/fiber) vs upper-case (gin/echo)
		// method-name spelling at the call site.
		verb := strings.ToUpper(rawVerb)
		// Title-case spelling without a router import → not a route (gate).
		if rawVerb != verb && !hasRouterImport {
			continue
		}
		raw := m[3]
		handler := m[4]
		// Use the last `.`-separated component so a `h.Create` style
		// handler resolves to `Create` in the same file's func decls.
		if i := strings.LastIndex(handler, "."); i >= 0 {
			handler = handler[i+1:]
		}
		// Prepend the enclosing route-group prefix (if the route was registered
		// on a `r.Group("/v1")` variable). The prefix is already accumulated
		// across nested groups by goGroupPrefixIndex (#4408).
		if pfx := groupPrefix[recv]; pfx != "" {
			raw = joinPathFragments(pfx, raw)
		}
		// Prepend the accumulated chi closure-router prefix for any `r.Route(...)`
		// closures enclosing this route's call site (#4782). Independent of the
		// gin/echo group resolution above — a route can be inside a chi Route
		// closure whose param was reassigned, so we key off byte position.
		if pfx := chiEnclosingPrefix(chiSpans, routePos); pfx != "" {
			raw = joinPathFragments(pfx, raw)
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, raw)
		// #4382 — the handler argument is an ANONYMOUS / INLINE func literal
		// (`r.GET("/x", func(c *gin.Context) {...})`). The `([\w.]+)` handler
		// group greedily captures the bare `func` keyword, which is NOT an
		// addressable handler symbol — emitting it as a named Controller ref
		// produces a bridge to a non-existent `func` def, leaving the endpoint
		// a graph ISLAND. Signal InlineHandler (empty refName) so makeEmit
		// synthesizes a stable inline-handler entity + merge-stable bridge.
		refKind := "Controller"
		if isGoInlineHandlerToken(handler) {
			handler = ""
			refKind = inlineHandlerRefKind
		}
		emit(verb, canonical, framework, refKind, handler)
	}
}

// goGroupPrefixIndex scans a Go file for gin/echo route-group assignments
// (`v1 := r.Group("/v1")`, `admin := v1.Group("/admin")`) and returns a map
// from each group VARIABLE to its fully-accumulated path prefix, composing
// nested groups (`admin` → "/v1/admin") via joinPathFragments (#4408).
//
// Resolution is order-independent: assignments are collected first, then each
// variable's prefix is resolved by walking its parent chain. The root router
// (`r := gin.Default()`) is not a group, so it contributes no prefix and a
// route on `r` directly is left unprefixed. A cycle guard (bounded by the
// number of groups) prevents pathological self-referential bindings from
// looping. Best-effort: a group whose prefix is built from a non-literal
// (a `RouterGroup` passed into a setup func, a computed string) is not
// statically recoverable and is simply omitted — the route then synthesizes
// at its own path, the pre-#4408 behaviour, rather than a wrong path.
func goGroupPrefixIndex(content string) map[string]string {
	if !strings.Contains(content, ".Group(") {
		return nil
	}
	type binding struct {
		parent string // receiver the group was derived from
		seg    string // this group's own (normalized) path segment
	}
	bindings := map[string]binding{}
	for _, m := range goGroupAssignRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		varName := m[1]
		parent := m[2]
		seg := normalizeMountPrefix(m[3])
		// A later assignment to the same variable wins (mirrors Go's last-write
		// semantics in linear setup code). Self-assignment (`g = g.Group(...)`)
		// keeps the earlier binding as the parent via the recorded parent name.
		bindings[varName] = binding{parent: parent, seg: seg}
	}
	if len(bindings) == 0 {
		return nil
	}
	resolved := map[string]string{}
	var resolve func(name string, depth int) string
	resolve = func(name string, depth int) string {
		if p, ok := resolved[name]; ok {
			return p
		}
		b, ok := bindings[name]
		if !ok {
			// `name` is the root router (or an unknown receiver): no prefix.
			return ""
		}
		if depth > len(bindings) {
			// Cycle guard: stop accumulating and treat as root-relative.
			return b.seg
		}
		full := joinPathFragments(resolve(b.parent, depth+1), b.seg)
		if full == "/" {
			full = ""
		}
		resolved[name] = full
		return full
	}
	for name := range bindings {
		resolve(name, 0)
	}
	return resolved
}

// groupSubmatchStrings extracts the submatch strings for one match location
// (produced by FindAllStringSubmatchIndex) from content. Returns a slice
// parallel to FindStringSubmatch where index 0 is the whole match. Unmatched
// optional groups (offset -1) yield "".
func groupSubmatchStrings(content string, loc []int) []string {
	out := make([]string, len(loc)/2)
	for i := 0; i < len(loc)/2; i++ {
		s, e := loc[2*i], loc[2*i+1]
		if s < 0 || e < 0 {
			out[i] = ""
			continue
		}
		out[i] = content[s:e]
	}
	return out
}

// chiRouteSpan is a chi `r.Route(prefix, func(...){ … })` closure body, located
// by its byte extent so enclosing prefixes can be accumulated positionally.
type chiRouteSpan struct {
	start int    // byte offset of the closure body open brace `{`
	end   int    // byte offset just past the matching close brace `}`
	seg   string // this Route's own (normalized) mount prefix segment
}

// goChiRouteSpans scans a Go file for chi closure-based sub-router mounts
// (`r.Route("/api", func(r chi.Router){ ... })`) and returns one span per
// closure, each carrying its mount prefix segment and the byte extent of its
// body. Spans nest naturally (a child Route closure's span lies inside its
// parent's), so chiEnclosingPrefix composes them transitively. A closure whose
// body brace is unbalanced (truncated source) is skipped. Returns nil when the
// file uses no chi `.Route(` mounts so the common path stays allocation-free.
func goChiRouteSpans(content string) []chiRouteSpan {
	if !strings.Contains(content, ".Route(") {
		return nil
	}
	var spans []chiRouteSpan
	for _, loc := range goChiRouteHeadRe.FindAllStringSubmatchIndex(content, -1) {
		// loc[1] is the offset just past the head match, i.e. just past the
		// opening `{` of the closure body. The body open brace is the last
		// char of the head match.
		open := loc[1] - 1
		seg := normalizeMountPrefix(content[loc[2]:loc[3]])
		if close, ok := goMatchBrace(content, open); ok {
			spans = append(spans, chiRouteSpan{start: open, end: close + 1, seg: seg})
		}
	}
	return spans
}

// goMatchBrace returns the byte offset of the `}` that closes the `{` at
// open, accounting for nested braces. It is brace-counting only (it does not
// skip braces inside strings/comments) — adequate for route-setup code, where
// brace-bearing string literals in a router DSL are vanishingly rare and a
// mismatch merely degrades to the pre-#4782 (unprefixed) behaviour via ok=false.
func goMatchBrace(content string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// chiEnclosingPrefix composes the mount prefixes of every chi Route closure
// whose body span encloses byte offset pos, outermost-first, so a route nested
// `r.Route("/api", …r.Route("/v1", …r.Get("/users")))` resolves to
// `/api/v1/users` (#4782). Spans are emitted in source order, so an enclosing
// (parent) Route head always precedes its children — sorting by start offset
// yields outermost-to-innermost composition.
func chiEnclosingPrefix(spans []chiRouteSpan, pos int) string {
	if len(spans) == 0 {
		return ""
	}
	prefix := ""
	for _, s := range spans {
		if pos > s.start && pos < s.end {
			prefix = joinPathFragments(prefix, s.seg)
		}
	}
	if prefix == "/" {
		return ""
	}
	return prefix
}

// goFuncOpenRe locates the brace that opens a Go function or method
// named `handler`. We support both top-level `func name(` and receiver
// methods `func (r *T) name(`.
func goFuncOpenRe(handler string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?m)^func\s*(?:\(\s*\w+\s+\*?\w+\s*\)\s*)?` + regexp.QuoteMeta(handler) + `\s*\(`,
	)
}

// goJSONCallRe matches the standard Gin/Echo response idiom:
//
//	c.JSON(<status>, <payload>)
//
// where `<status>` is either an int literal or http.StatusXxx and
// `<payload>` is what we want to inspect.
var goJSONCallRe = regexp.MustCompile(`\b\w+\s*\.\s*JSON\s*\(`)

// goStatusLiteralRe captures both `200` and `http.StatusOK` arguments.
var goStatusLiteralRe = regexp.MustCompile(`^(?:(\d{3})|http\.Status([A-Z][A-Za-z]+))$`)

func extractGoShape(src, handler, framework string) shape {
	var sh shape
	if handler == "" {
		return sh
	}
	body := findGoHandlerBody(src, handler)
	if body == "" {
		return sh
	}
	// Walk every c.JSON(...) call in the body.
	for _, idx := range goJSONCallRe.FindAllStringIndex(body, -1) {
		paren := idx[1] - 1
		args := extractArgList(body, paren)
		if len(args) < 2 {
			continue
		}
		status := parseGoStatusArg(args[0])
		payload := strings.TrimSpace(args[1])
		applyGoPayload(src, payload, status, &sh)
	}
	// render.JSON(w, r, payload).
	for _, m := range regexp.MustCompile(`\brender\s*\.\s*JSON\s*\(`).FindAllStringIndex(body, -1) {
		paren := m[1] - 1
		args := extractArgList(body, paren)
		if len(args) < 3 {
			continue
		}
		applyGoPayload(src, strings.TrimSpace(args[2]), 200, &sh)
	}
	// json.NewEncoder(w).Encode(payload).
	for _, m := range regexp.MustCompile(`json\.NewEncoder\([^)]*\)\.Encode\s*\(`).FindAllStringIndex(body, -1) {
		paren := m[1] - 1
		args := extractArgList(body, paren)
		if len(args) < 1 {
			continue
		}
		applyGoPayload(src, strings.TrimSpace(args[0]), 200, &sh)
	}
	return sh
}

func findGoHandlerBody(src, handler string) string {
	re := goFuncOpenRe(handler)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	// Find the `{` after the closing `)` of the signature.
	open := strings.Index(src[loc[1]:], "{")
	if open < 0 {
		return ""
	}
	braceIdx := loc[1] + open
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return ""
	}
	return src[braceIdx+1 : end]
}

func parseGoStatusArg(arg string) int {
	arg = strings.TrimSpace(arg)
	m := goStatusLiteralRe.FindStringSubmatch(arg)
	if m == nil {
		return 0
	}
	if m[1] != "" {
		if n, err := atoi(m[1]); err == nil {
			return n
		}
	}
	if m[2] != "" {
		return goHTTPStatusFromName(m[2])
	}
	return 0
}

// goHTTPStatusFromName maps the http package's constant suffix
// ("OK", "BadRequest", "NotFound", …) to its numeric code. Only the
// common subset is needed for status code emission.
func goHTTPStatusFromName(name string) int {
	switch name {
	case "OK":
		return 200
	case "Created":
		return 201
	case "Accepted":
		return 202
	case "NoContent":
		return 204
	case "BadRequest":
		return 400
	case "Unauthorized":
		return 401
	case "Forbidden":
		return 403
	case "NotFound":
		return 404
	case "Conflict":
		return 409
	case "UnprocessableEntity":
		return 422
	case "InternalServerError":
		return 500
	}
	return 0
}

// applyGoPayload classifies a payload expression as a literal map, a
// typed struct literal, or a free variable, and updates `sh`.
func applyGoPayload(src, payload string, status int, sh *shape) {
	// gin.H{...} / map[string]interface{}{...} / map[string]any{...}.
	if strings.HasPrefix(payload, "gin.H{") ||
		strings.HasPrefix(payload, "map[string]interface{}") ||
		strings.HasPrefix(payload, "map[string]any") ||
		strings.HasPrefix(payload, "echo.Map{") ||
		strings.HasPrefix(payload, "fiber.Map{") {
		brace := strings.Index(payload, "{")
		if brace < 0 {
			sh.dynamicResponse = true
			return
		}
		end := findMatchingBracket(payload, brace)
		if end < 0 {
			sh.dynamicResponse = true
			return
		}
		// Wrap in dict-form for extractDictKeys (it expects `{...}`).
		keys := extractDictKeys(payload[brace : end+1])
		if len(keys) > 0 {
			sh.knownResponse = true
			if status >= 400 || looksLikeError(payload) {
				sh.errorKeys = append(sh.errorKeys, keys...)
			} else {
				sh.responseKeys = append(sh.responseKeys, keys...)
			}
			recordStatus(sh, status, false)
			return
		}
	}
	// `&MyDto{A: 1, ...}` or `MyDto{A: 1, ...}` typed literal.
	if m := regexp.MustCompile(`^&?([A-Z]\w*)\s*\{`).FindStringSubmatch(payload); len(m) >= 2 {
		dto := m[1]
		schema := walkGoStructFields(src, dto)
		if len(schema) > 0 {
			if sh.responseSchema == nil {
				sh.responseSchema = schema
			}
			for k := range schema {
				sh.responseKeys = append(sh.responseKeys, k)
			}
			sh.knownResponse = true
			recordStatus(sh, status, false)
			return
		}
	}
	// Bare identifier — try to resolve as a same-file struct or fall through.
	if id := regexp.MustCompile(`^[A-Z]\w*$`).FindString(payload); id != "" {
		schema := walkGoStructFields(src, id)
		if len(schema) > 0 {
			sh.responseSchema = schema
			for k := range schema {
				sh.responseKeys = append(sh.responseKeys, k)
			}
			sh.knownResponse = true
			recordStatus(sh, status, false)
			return
		}
	}
	sh.dynamicResponse = true
	recordStatus(sh, status, looksLikeError(payload))
}

// walkGoStructFields locates `type X struct { ... }` and returns
// {fieldName -> typeToken} for every exported field. The fieldName uses
// the json tag when present (so the externally-visible key matches the
// JSON shape).
var goStructFieldRe = regexp.MustCompile(`(?m)^[ \t]+([A-Z]\w*)\s+([\w*\.\[\]]+)(?:\s+` + "`" + `([^` + "`" + `]*)` + "`" + `)?`)
var goJSONTagRe = regexp.MustCompile(`json:"([^",]+)`)

func walkGoStructFields(src, name string) map[string]string {
	re := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `\s+struct\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	braceIdx := loc[1] - 1
	end := findMatchingBracket(src, braceIdx)
	if end < 0 {
		return nil
	}
	body := src[braceIdx+1 : end]
	out := map[string]string{}
	for _, m := range goStructFieldRe.FindAllStringSubmatch(body, -1) {
		fname := m[1]
		ftype := m[2]
		tag := ""
		if len(m) >= 4 {
			tag = m[3]
		}
		// Prefer the json tag when it names the wire field explicitly.
		if tag != "" {
			if jt := goJSONTagRe.FindStringSubmatch(tag); len(jt) >= 2 && jt[1] != "-" {
				fname = jt[1]
			}
		}
		out[fname] = ftype
	}
	return out
}
