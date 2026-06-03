// Go request-input → sink dataflow sniffer (#3628 area #22, #3943).
//
// SCOPED def→use tracking inside one function body, followed through up to
// DataFlowMaxHops local (same-file) call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an imported (non-local)
// callee. See dataflow.go for the contract and the honest-partial boundary.
// Mirrors the python (dataflow_python.go) and JS/TS (dataflow_jsts.go)
// sniffers exactly: same DataFlow/source/sink model, same DATA_FLOWS_TO
// emission, same precision-over-recall discipline.
//
// Sources recognised (request inputs, STATIC key only):
//   - gin     : c.Query("q") / c.PostForm("x") / c.Param("id") /
//     c.ShouldBindJSON(&dto) / c.Bind(&dto)
//   - echo    : c.QueryParam("q") / c.FormValue("x") / c.Param("id") /
//     c.Bind(&dto)
//   - chi     : chi.URLParam(r,"id") / r.FormValue("x") /
//     r.URL.Query().Get("x")
//   - net/http: r.FormValue("x") / r.URL.Query().Get("x") /
//     json.NewDecoder(r.Body).Decode(&dto)
//
// BIND SOURCES are different from key-getter sources: a call such as
// `c.ShouldBindJSON(&dto)` / `c.Bind(&dto)` / `json.NewDecoder(r.Body).
// Decode(&dto)` does not return the value — it populates the struct pointed
// to by `&dto`. The pointed-to identifier (`dto`) becomes a request-derived
// root (the same role `req.body` plays in Express, or `@Body() dto` in
// NestJS). The source field is then recovered from a later static member
// access off the root (`dto.Email` → field "Email"), exactly as the jsts
// sniffer lifts `dto.email` for a `@Body() dto` param.
//
// Sinks recognised:
//   - DB write : <recv>.Create( / .Save( / .Updates( / .Update( /
//     .Exec( / .Query( / .NamedExec( (gorm / database/sql / sqlx),
//     when an argument carries taint.
//   - response : c.JSON( / c.String( / w.Write( / json.NewEncoder(w).Encode(
//   - http_call: http.Post( / http.PostForm( / <client>.Do( (outbound, with
//     a tainted body/argument) — treated as a CONSUMES_API site.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("go", sniffDataFlowGoEx, continueDataFlowGo)
}

// sniffDataFlowGo preserves the legacy in-file-only entry point.
func sniffDataFlowGo(content string) []DataFlow { return sniffDataFlowGoEx(content).Flows }

// dfGoSourceFieldRe captures a request-input read with a STATIC string key.
// Each receiver alternative places the key in its own group so exactly one
// is non-empty for a match. Dynamic keys (`c.Query(k)`) do NOT match
// (honest-partial). Covers gin/echo/chi/net-http key getters.
//
//	g1: gin    c.Query / c.PostForm / c.Param("k")
//	g2: echo   c.QueryParam / c.FormValue / c.Param("k")
//	g3: chi    chi.URLParam(r, "k")
//	g4: http   r.FormValue("k") / req.FormValue("k")
//	g5: http   r.URL.Query().Get("k") / req.URL.Query().Get("k")
//	g6: fasthttp ctx.QueryArgs().Peek("k") / ctx.PostArgs().Peek("k")
//	g7: fasthttp ctx.FormValue("k")
//	g8: iris   ctx.URLParam("k") / ctx.PostValue("k") / ctx.FormValue("k")
//	g9: beego  c.GetString("k") / c.Ctx.Input.Param("k")
//	g10: revel c.Params.Get("k")
var dfGoSourceFieldRe = regexp.MustCompile(
	`\bc\s*\.\s*(?:Query|PostForm|Param)\s*\(\s*"([^"]+)"` +
		`|\bc\s*\.\s*(?:QueryParam|FormValue|Param)\s*\(\s*"([^"]+)"` +
		`|\bchi\s*\.\s*URLParam\s*\(\s*[A-Za-z_]\w*\s*,\s*"([^"]+)"` +
		`|\b(?:r|req)\s*\.\s*FormValue\s*\(\s*"([^"]+)"` +
		`|\b(?:r|req)\s*\.\s*URL\s*\.\s*Query\s*\(\s*\)\s*\.\s*Get\s*\(\s*"([^"]+)"` +
		`|\bctx\s*\.\s*(?:QueryArgs|PostArgs)\s*\(\s*\)\s*\.\s*Peek\s*\(\s*"([^"]+)"` +
		`|\bctx\s*\.\s*FormValue\s*\(\s*"([^"]+)"` +
		`|\bctx\s*\.\s*(?:URLParam|PostValue)\s*\(\s*"([^"]+)"` +
		`|\bc\s*\.\s*(?:GetString|Ctx\s*\.\s*Input\s*\.\s*Param)\s*\(\s*"([^"]+)"` +
		`|\bc\s*\.\s*Params\s*\.\s*Get\s*\(\s*"([^"]+)"`,
)

// dfGoSourceAnyRe matches a key-getter source receiver WITHOUT requiring a
// static key, for whole-value pass-through and dynamic-key detection. (A
// dynamic key still produces a tainted value with an unknown field — the
// flow is real, only the field is dropped, mirroring py/jsts whole-object.)
var dfGoSourceAnyRe = regexp.MustCompile(
	`\bc\s*\.\s*(?:Query|PostForm|Param|QueryParam|FormValue|GetString)\s*\(` +
		`|\bc\s*\.\s*Ctx\s*\.\s*Input\s*\.\s*Param\s*\(` +
		`|\bc\s*\.\s*Params\s*\.\s*Get\s*\(` +
		`|\bchi\s*\.\s*URLParam\s*\(` +
		`|\b(?:r|req)\s*\.\s*FormValue\s*\(` +
		`|\b(?:r|req)\s*\.\s*URL\s*\.\s*Query\s*\(\s*\)\s*\.\s*Get\s*\(` +
		`|\bctx\s*\.\s*(?:QueryArgs|PostArgs)\s*\(\s*\)\s*\.\s*Peek\s*\(` +
		`|\bctx\s*\.\s*(?:FormValue|URLParam|PostValue)\s*\(`,
)

// dfGoBindRe matches a BIND source: a call that populates the struct pointed
// to by its `&NAME` argument from the request body. Group 1 = the bound
// root identifier (`dto` for `c.ShouldBindJSON(&dto)`). The field is
// recovered later from a static member access off that root (`dto.Email`).
// Covers gin ShouldBind*/Bind*, echo Bind, and stdlib json Decode(&dto).
// Extended idiom arms:
//   - hertz : c.BindAndValidate(&x) / c.Bind(&x) (Bind already covered above)
//   - iris  : ctx.ReadJSON(&x) / ctx.ReadForm(&x) / ctx.ReadQuery(&x)
//   - revel : c.Params.Bind(&x, "k")
//   - go-zero: httpx.Parse(r, &req) — the bound root is the SECOND arg
var dfGoBindRe = regexp.MustCompile(
	`\bc\s*\.\s*(?:ShouldBind(?:JSON|Query|XML|YAML|Header|Uri)?|Bind(?:JSON|Query|XML|YAML|Header|Uri|AndValidate)?)\s*\(\s*&\s*([A-Za-z_]\w*)\b` +
		`|\.\s*Decode\s*\(\s*&\s*([A-Za-z_]\w*)\b` +
		`|\bctx\s*\.\s*(?:ReadJSON|ReadForm|ReadQuery|ReadXML|ReadYAML)\s*\(\s*&\s*([A-Za-z_]\w*)\b` +
		`|\bc\s*\.\s*Params\s*\.\s*Bind\s*\(\s*&\s*([A-Za-z_]\w*)\b` +
		`|\bhttpx\s*\.\s*Parse\s*\(\s*[A-Za-z_]\w*\s*,\s*&\s*([A-Za-z_]\w*)\b`,
)

// dfGoDBWriteRe matches an ORM / database/sql write call. Group 1 = callee.
// gorm: db.Create/Save/Updates/Update; database/sql + sqlx: db.Exec/Query/
// NamedExec/MustExec/NamedQuery.
var dfGoDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*\s*\.\s*(?:Create|Save|Updates|Update|Exec|ExecContext|Query|QueryContext|QueryRow|QueryRowContext|NamedExec|MustExec|NamedQuery))\s*\(`,
)

// dfGoRespRe matches a response-body emission. Group 1 = callee.
//   - c.JSON( / c.String( / c.XML( / c.Data(        (gin/echo)
//   - w.Write( / rw.Write(                            (net/http)
//   - json.NewEncoder(w).Encode(                      (net/http)
var dfGoRespRe = regexp.MustCompile(
	`\b(c\s*\.\s*(?:JSON|String|XML|Data|IndentedJSON|PureJSON|AsciiJSON))\s*\(` +
		`|\b((?:w|rw)\s*\.\s*Write)\s*\(` +
		`|\b(json\s*\.\s*NewEncoder\s*\([A-Za-z_]\w*\)\s*\.\s*Encode)\s*\(`,
)

// dfGoHTTPCallRe matches an outbound HTTP call. Group 1 = callee.
//   - http.Post( / http.PostForm(
//   - <client>.Do(
var dfGoHTTPCallRe = regexp.MustCompile(
	`\b(http\s*\.\s*(?:Post|PostForm))\s*\(` +
		`|\b([A-Za-z_][\w]*\s*\.\s*Do)\s*\(`,
)

// dfGoAssignRe captures `NAME := <rhs>` or `NAME = <rhs>` (group 1 = name,
// group 2 = rhs). Single-target only (multi-assign `a, b := …` is handled
// separately / dropped). The `[^=]` after `=` excludes `==`.
var dfGoAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_]\w*)\s*(?::=|=)\s*([^=].*)$`,
)

// dfGoSinkSpecs is the ordered sink table reused at every scan depth.
var dfGoSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfGoDBWriteRe, DataFlowSinkDBWrite},
	{dfGoRespRe, DataFlowSinkResponse},
	{dfGoHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowGoEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := scanGoFuncHeaders(content)
	bodies := goFuncBodies(lines, headers)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := goWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		r := walkGoBody(ctx, b, map[string]taintInfo{})
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowGo continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks. The
// returned flows' Function/SourceField/SourceLine are placeholders the links
// pass rewrites to the true origin handler.
func continueDataFlowGo(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := scanGoFuncHeaders(content)
	bodies := goFuncBodies(lines, headers)
	callee := goBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := goParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := goWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkGoBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// goFuncBody is a function's line span (1-indexed, inclusive).
type goFuncBody struct {
	Name  string
	Start int // header line (the `func … {` line)
	End   int // line of the matching `}`
}

// goFuncBodies computes brace-balanced spans for each header. A header whose
// body brace can't be balanced within the file is skipped (conservative).
func goFuncBodies(lines []string, headers []funcHeader) []goFuncBody {
	var out []goFuncBody
	for _, h := range headers {
		if h.Line < 1 || h.Line > len(lines) {
			continue
		}
		end := jstsMatchBraceEnd(lines, h.Line)
		if end == 0 {
			continue
		}
		out = append(out, goFuncBody{Name: h.Name, Start: h.Line, End: end})
	}
	return out
}

// goWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type goWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []goFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkGoBody is the unified forward pass over a function body.
func walkGoBody(ctx goWalkCtx, b goFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		goTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, goDirectSinks(ctx, ln, line, tainted)...)

		r := goFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// goTrackTaint applies one line's assignment/bind effects to the taint map.
func goTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	// BIND sources taint the pointed-to root in place (no assignment form):
	// `c.ShouldBindJSON(&dto)` makes `dto` request-derived. Field is "" here;
	// it is recovered later from a `dto.Field` member access.
	if m := dfGoBindRe.FindStringSubmatch(line); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				tainted[g] = taintInfo{field: "", line: ln}
			}
		}
	}
	// Single-target assignment (`x := c.Query("q")` / `x = tainted`).
	if m := dfGoAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if fld, ok := goRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else if _, was := tainted[name]; was {
			delete(tainted, name) // reassigned to non-source → drop taint
		}
	}
}

// goRHSSourceField returns (field, true) when rhs is a request-input read or
// a reference to a tainted variable, preserving provenance.
func goRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if m := dfGoSourceFieldRe.FindStringSubmatch(rhs); m != nil {
		return dfGoFirstGroup(m), true
	}
	if dfGoSourceAnyRe.MatchString(rhs) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return goTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfGoFirstGroup returns the first non-empty capture group of a
// dfGoSourceFieldRe submatch (the static key), or "" (whole-value).
func dfGoFirstGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// dfGoTaintedMemberRe matches an expression that is SOLELY an identifier
// followed by a single static member access (`dto.Email`). Group 1 = root.
var dfGoTaintedMemberRe = regexp.MustCompile(`^([A-Za-z_]\w*)\s*\.\s*[A-Za-z_]\w*$`)

// dfGoIdentMemberRe captures an identifier (g1) followed by a static member
// access (g2) anywhere in an expression (`dto.Email` → ("dto","Email")).
var dfGoIdentMemberRe = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\.\s*([A-Za-z_]\w*)`)

// goTaintedField resolves the source field for a reference to tainted `name`
// (known field info.field) as it appears in expr. When the root carries no
// field of its own (a bind root `dto` or a whole-value source) and expr
// accesses a static member (`dto.Email`), the member is lifted as the field.
// A known key/source field always wins when present.
func goTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfGoIdentMemberRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// goDirectSinks emits flows for sinks on `line` whose args carry taint.
func goDirectSinks(ctx goWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfGoSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := goSinkCallee(line, m)
			if callee == "" {
				continue
			}
			// The sink's `(` is at the byte just past the last captured group;
			// find it from the callee's end so multi-line args are spanned.
			open := strings.Index(line[m[0]:], "(")
			if open < 0 {
				continue
			}
			args := jstsCallArgs(ctx.lines, ln, m[0]+open)
			if fld, ok := goExprTainted(args, tainted); ok {
				field := ctx.field
				if field == "" {
					field = fld
				}
				out = append(out, DataFlow{
					Function:    ctx.origin,
					SourceField: field,
					SourceLine:  ctx.srcLine,
					SinkKind:    s.kind,
					SinkName:    strings.Join(strings.Fields(callee), ""),
					SinkLine:    ln,
					HopVia:      firstOf(ctx.hopPath),
					HopPath:     dupStrings(ctx.hopPath),
				})
			}
		}
	}
	return out
}

// goSinkCallee returns the non-empty captured callee text from a sink match
// (the sink regexes use alternation groups; exactly one is set per match).
func goSinkCallee(line string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 && m[i+1] >= 0 {
			return line[m[i]:m[i+1]]
		}
	}
	return ""
}

// goFollowCalls handles each local-call on `line`: recurse into a same-file
// function (bounded + cycle-guarded) or record a cross-file boundary.
// Position binding is EXACT — an ambiguous arg drops (honest-partial).
func goFollowCalls(ctx goWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range goLocalCalls(line) {
		if goArgsHaveVariadic(call.args) {
			continue // `x...` spread makes positions unreliable — drop
		}
		for pos, argExpr := range call.args {
			fld, bare := goArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := goBodyByName(ctx.bodies, call.name)
			if callee == nil {
				if ctx.hopsUsed+len(ctx.hopPath) >= DataFlowMaxHops {
					continue
				}
				res.Boundaries = append(res.Boundaries, DataFlowBoundary{
					Function:    ctx.origin,
					SourceField: field,
					SourceLine:  ctx.srcLine,
					Callee:      call.name,
					ArgIndex:    pos,
					HopPath:     dupStrings(ctx.hopPath),
					CallLine:    ln,
				})
				continue
			}
			if ctx.hopsUsed+len(ctx.hopPath) >= DataFlowMaxHops {
				continue
			}
			if ctx.visited[callee.Name] {
				continue // recursion / cycle — drop
			}
			param := goParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkGoBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// goExprTainted reports whether expr references a request source directly or
// a tainted variable, returning the field when known.
func goExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if m := dfGoSourceFieldRe.FindStringSubmatch(expr); m != nil {
		return dfGoFirstGroup(m), true
	}
	if dfGoSourceAnyRe.MatchString(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return goTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// goArgBareTaint reports whether argExpr is EXACTLY a tainted value (a
// request-source read, a bare tainted identifier, a `&tainted` address-of, or
// a static member access off a tainted root `dto.Email`), not embedded in a
// larger expression. Precision guard for sound positional binding. Returns
// the field. `helper(x)` / `helper(&item)` / `db.Create(&Item{Name:q})` bind
// the tainted leaf; `helper(x + 1)` / `helper(f(x))` do NOT.
func goArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	// Address-of unwrap: `&dto` binds the same taint as `dto`.
	e = strings.TrimSpace(strings.TrimPrefix(e, "&"))

	// A composite literal carrying a tainted value in a field
	// (`Item{Name: q}` / `&User{Email: dto.Email}`) is a real tainted sink
	// argument — bind the tainted leaf and lift its field.
	if strings.Contains(e, "{") && strings.Contains(e, "}") {
		return goCompositeTaint(e, tainted)
	}
	if goWholeExprIsSource(e) {
		if m := dfGoSourceFieldRe.FindStringSubmatch(e); m != nil {
			return dfGoFirstGroup(m), true
		}
		return "", true
	}
	if dfReSimpleIdent.MatchString(e) {
		if info, ok := tainted[e]; ok {
			return info.field, true
		}
	}
	if m := dfGoTaintedMemberRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			return goTaintedField(e, m[1], info), true
		}
	}
	return "", false
}

// goCompositeTaint inspects a composite-literal argument (`T{...}` or
// `&T{...}`) for a tainted value bound to one of its fields, returning the
// derived source field. The field name preferred is the request-source field
// of the tainted leaf (e.g. `User{Email: dto.Email}` → "Email" via the
// member; `Item{Name: q}` where `q := c.Query("name")` → "name"). Honest-
// partial: only a bare tainted leaf inside the literal counts.
func goCompositeTaint(e string, tainted map[string]taintInfo) (string, bool) {
	open := strings.Index(e, "{")
	close := strings.LastIndex(e, "}")
	if open < 0 || close <= open {
		return "", false
	}
	inner := e[open+1 : close]
	for _, part := range jstsSplitArgs(inner) {
		kv := strings.SplitN(part, ":", 2)
		val := part
		if len(kv) == 2 {
			val = kv[1]
		}
		if fld, ok := goArgBareTaint(val, tainted); ok {
			return fld, true
		}
	}
	return "", false
}

// goWholeExprIsSource reports the expr is SOLELY a request-source access.
func goWholeExprIsSource(e string) bool {
	loc := dfGoSourceFieldRe.FindStringIndex(e)
	if loc == nil {
		loc = dfGoSourceAnyRe.FindStringIndex(e)
	}
	if loc == nil {
		return false
	}
	return strings.TrimSpace(e[:loc[0]]) == "" && strings.TrimSpace(e[loc[1]:]) == ""
}

// goArgsHaveVariadic reports whether any arg is a `x...` spread, which makes
// positional indices past it unreliable → the call is dropped.
func goArgsHaveVariadic(args []string) bool {
	for _, a := range args {
		if strings.HasSuffix(strings.TrimSpace(a), "...") {
			return true
		}
	}
	return false
}

// goLocalCall is a parsed `name(arg0, arg1, …)` call to a bare identifier.
type goLocalCall struct {
	name string
	args []string
}

// dfGoLocalCallRe matches a call to a bare identifier (potential local fn).
var dfGoLocalCallRe = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)

// goLocalCalls extracts candidate bare-identifier function calls on a line
// with their top-level positional argument expressions. Method calls
// (`x.foo(`) are skipped — only a same-file bare-identifier call is a hop /
// boundary candidate.
func goLocalCalls(line string) []goLocalCall {
	var out []goLocalCall
	for _, m := range dfGoLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue // method call — not a bare-ident call
			}
		}
		if goControlKeyword(name) {
			continue
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, goLocalCall{name: name, args: args})
	}
	return out
}

// goControlKeyword reports whether name is a Go keyword / builtin that must
// not be treated as a local function call.
func goControlKeyword(name string) bool {
	switch name {
	case "if", "for", "switch", "select", "return", "go", "defer", "func",
		"range", "make", "new", "len", "cap", "append", "copy", "delete",
		"panic", "recover", "print", "println", "close", "string", "int",
		"int64", "float64", "bool", "byte", "rune", "error", "map", "chan":
		return true
	}
	return false
}

// goParamName returns the name of the pos-th positional parameter of the
// function whose header is on headerLine. Go groups same-typed params
// (`func f(a, b string)`), so the parameter list is expanded to one name per
// position. A variadic (`…T`) or a receiver-only/complex signature → "".
func goParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	line := lines[headerLine-1]
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return ""
	}
	close := goMatchParen(line, open)
	if close < 0 {
		return ""
	}
	names := goExpandParams(line[open+1 : close])
	if pos < 0 || pos >= len(names) {
		return ""
	}
	return names[pos]
}

// goMatchParen returns the index of the `)` matching the `(` at openByte, or
// -1. Only scans the single header line (Go signatures are single-line in
// the overwhelming majority; a multi-line signature drops, honest-partial).
func goMatchParen(line string, openByte int) int {
	depth := 0
	for i := openByte; i < len(line); i++ {
		switch line[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// goExpandParams turns a Go parameter list into one identifier per positional
// slot. Go shares a trailing type across a run of names: `a, b string, c int`
// → ["a","b","c"]. After comma-splitting, a group that is a single bare
// identifier (`a`) is a NAME whose type is supplied by a later group in the
// same run; a group `name Type` closes the run with its own name. An unnamed
// type-only signature (`func f(string, int)` — every group a lone NON-name
// token) yields "" per slot (no binding, honest-partial), as does a variadic
// or blank `_` slot.
func goExpandParams(sig string) []string {
	groups := jstsSplitArgs(sig)
	out := make([]string, len(groups))
	for i, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || strings.Contains(g, "...") {
			out[i] = "" // empty / variadic — ambiguous slot
			continue
		}
		fields := strings.Fields(g)
		switch {
		case len(fields) >= 2:
			// `name Type` (or `name *T` / `name T` etc.) — a named slot.
			n := fields[0]
			if dfReSimpleIdent.MatchString(n) && n != "_" {
				out[i] = n
			}
		case dfReSimpleIdent.MatchString(g) && g != "_":
			// A lone identifier. It is a NAME (its type is on a later group in
			// the run) ONLY if some later group carries a type (≥2 fields).
			// Otherwise the whole signature is type-only and this is a TYPE.
			if goLaterGroupCarriesType(groups, i) {
				out[i] = g
			}
		}
	}
	return out
}

// goLaterGroupCarriesType reports whether a group after index i in the same
// parameter list has ≥2 space-separated tokens (i.e. supplies a shared type
// for a leading run of bare-identifier names). Used to disambiguate a lone
// identifier between "named param sharing a later type" and "unnamed type".
func goLaterGroupCarriesType(groups []string, i int) bool {
	for j := i + 1; j < len(groups); j++ {
		if len(strings.Fields(strings.TrimSpace(groups[j]))) >= 2 {
			return true
		}
	}
	return false
}

// goBodyByName returns the body with the given name, or nil.
func goBodyByName(all []goFuncBody, name string) *goFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
