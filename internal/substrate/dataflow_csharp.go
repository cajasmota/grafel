// C# request-input → sink dataflow sniffer (#3628 area #22, epic #3872, #3960).
//
// SCOPED def→use tracking inside one method body, followed through up to
// DataFlowMaxHops local (same-file) method-call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an imported / external callee.
// See dataflow.go for the contract and the honest-partial boundary. Mirrors
// dataflow_java.go exactly (the closest analog — OOP, attribute-annotated
// handler parameters, property member access): same DataFlow/source/sink model,
// same DATA_FLOWS_TO emission, same precision-over-recall discipline. This is
// the LAST language of the cross-language dataflow generalization (py / jsts /
// golang / ruby / java / php already shipped).
//
// Sources recognised — ASP.NET Core / MVC controller-action PARAMETERS bound
// from the request the moment the action is entered:
//   - [FromBody] UserDto dto            → root `dto` tainted, field "" (the
//     field is recovered from a later `dto.Email` property access)
//   - [FromQuery] string q              → root `q` tainted, field "q"
//   - [FromRoute] int id                → root `id` tainted, field "id"
//   - [FromForm] string name            → field "name"
//   - [FromHeader(Name="X")] string t   → field "X" (attribute literal), else
//     the parameter name
//
// AND in-body request accessors seeded as local roots:
//   - var q = Request.Query["x"];        → q tainted, field "x"
//   - var v = Request.Form["x"];         → field "x"
//   - var id = Request.RouteValues["id"];→ field "id"
//
// An attribute literal (`[FromQuery(Name="q")]` / `[FromHeader(Name="X")]`)
// wins as the field; otherwise a scalar binder ([FromQuery]/[FromRoute]/
// [FromForm]/[FromHeader]) infers the field from the parameter name, and a
// whole-object [FromBody] carries field "" until a static property access
// derives the concrete field (`dto.Email` → "Email").
//
// Propagation: a local assigned from a tainted value (`var e = dto.Email;`)
// carries taint; a direct pass-through into a sink flows; a pass into a local
// method binds positionally and continues the bounded walk (≤ DataFlowMaxHops),
// recording the callee chain in HopPath.
//
// Sinks recognised:
//   - db_write : EF Core `<ctx>.<Set>.Add / AddAsync / AddRange / Update /
//     UpdateRange / Remove / RemoveRange / Attach`, `<ctx>.SaveChanges /
//     SaveChangesAsync`, Dapper `<conn>.Execute / ExecuteAsync` with a tainted
//     param, when an argument carries taint.
//   - response : `return Ok(x)`, `return Json(x)`, `return Content(x)`,
//     `return <tainted>;`, `Ok(x)` / `Json(x)` builders carrying a tainted arg.
//   - http_call: `<client>.PostAsync / PutAsync / PatchAsync / SendAsync /
//     PostAsJsonAsync / PutAsJsonAsync` (outbound, with a tainted body) — a
//     CONSUMES_API site.
//
// HONEST-PARTIAL (precision over recall): dynamic member access, whole-object
// flows (field=""), ambiguous/varargs/params positions, reassignment that
// breaks the chain, >DataFlowMaxHops depth, and recursion are DROPPED or flow
// with field="", never fabricated. A non-request parameter (an injected
// service) is NOT a source.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("csharp", sniffDataFlowCSharpEx, continueDataFlowCSharp)
}

// sniffDataFlowCSharp preserves the legacy in-file-only entry point.
func sniffDataFlowCSharp(content string) []DataFlow { return sniffDataFlowCSharpEx(content).Flows }

// dfCSharpFromAttrRe matches ONE ASP.NET Core request-binding parameter inside
// an action signature: a [From*] attribute (group 1 = binder kind), an optional
// `(Name="literal")` / `("literal")` value (group 2 = the request-input field
// name), the parameter TYPE token (skipped), and the bound parameter identifier
// (group 3 = the tainted root).
//
//	[FromBody] UserDto dto              → g1=Body  g2=""  g3=dto
//	[FromQuery] string q               → g1=Query g2=""  g3=q
//	[FromQuery(Name="q")] string x     → g1=Query g2=q   g3=x
//	[FromRoute] int id                 → g1=Route g2=""  g3=id
//	[FromHeader(Name="X-Tok")] string t→ g1=Header g2=X-Tok g3=t
var dfCSharpFromAttrRe = regexp.MustCompile(
	`\[\s*From(Body|Query|Form|Header|Route)\b` +
		`(?:\s*\(\s*(?:Name\s*=\s*)?(?:"([^"]*)")?[^)]*\))?` +
		`\s*\]\s*` +
		`(?:\[[^\]]*\]\s*)*` +
		`[A-Za-z_][\w<>\[\],.?\s]*?\s+` +
		`([A-Za-z_][\w]*)\b`,
)

// dfCSharpAnyFromAttrRe is a cheap presence check for any [From*] binding
// attribute in a signature, used to confirm a method is an action handler.
var dfCSharpAnyFromAttrRe = regexp.MustCompile(
	`\[\s*From(?:Body|Query|Form|Header|Route)\b`,
)

// dfCSharpRequestAccessRe matches an in-body request accessor whose key literal
// is the source field: `Request.Query["x"]`, `Request.Form["x"]`,
// `Request.RouteValues["id"]`, `Request.Headers["x"]`, `Request.Cookies["x"]`.
// Group 1 = the bucket, group 2 = the key literal (the field).
var dfCSharpRequestAccessRe = regexp.MustCompile(
	`\b(?:Request|HttpContext\s*\.\s*Request)\s*\.\s*(Query|Form|RouteValues|Headers|Cookies)\s*\[\s*"([^"]*)"\s*\]`,
)

// dfCSharpDBWriteRe matches an EF Core / Dapper write. The captured group is the
// callee text. Mirrors effect_sinks_csharp.go csharpDBWriteRe (write subset),
// but anchored on a receiver so the callee renders canonically.
//
//	_context.Users.Add(    → g1 = _context.Users.Add
//	_context.SaveChanges(  → g2 = _context.SaveChanges
//	connection.Execute(sql,→ g3 = connection.Execute  (Dapper)
var dfCSharpDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*(?:\s*\.\s*[A-Za-z_][\w]*)*\s*\.\s*(?:Add|AddAsync|AddRange|AddRangeAsync|Update|UpdateRange|Remove|RemoveRange|Attach))\s*\(` +
		`|\b([A-Za-z_][\w]*(?:\s*\.\s*[A-Za-z_][\w]*)*\s*\.\s*(?:SaveChanges|SaveChangesAsync|ExecuteSqlRaw|ExecuteSqlRawAsync|ExecuteSqlInterpolated|ExecuteSqlInterpolatedAsync))\s*\(` +
		`|\b([A-Za-z_][\w]*\s*\.\s*(?:Execute|ExecuteAsync|ExecuteScalar|ExecuteScalarAsync|ExecuteNonQuery|ExecuteNonQueryAsync))\s*\(`,
)

// dfCSharpRespRe matches an MVC action response-body builder whose argument
// carries taint. Group is the callee text. `return <tainted>;` is handled
// separately (dfCSharpReturnRe) so a bare returned value is captured.
//
//	Ok(x)         → g1 = Ok
//	Json(x)       → g2 = Json
//	Content(x)    → g3 = Content
//	return Ok(x)  → the `return ` prefix is tolerated by matching `Ok(` anywhere.
var dfCSharpRespRe = regexp.MustCompile(
	`\b(Ok|Created|CreatedAtAction|CreatedAtRoute|Accepted|AcceptedAtAction)\s*\(` +
		`|\b(Json|Content|PartialView|View)\s*\(` +
		`|\b(StatusCode)\s*\([^,]*,`,
)

// dfCSharpHTTPCallRe matches an outbound HTTP call carrying a tainted body.
// Group 1 = callee. Mirrors effect_sinks_csharp.go csharpHTTPRe (write verbs).
var dfCSharpHTTPCallRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*\s*\.\s*(?:PostAsync|PutAsync|PatchAsync|DeleteAsync|SendAsync|PostAsJsonAsync|PutAsJsonAsync|PatchAsJsonAsync))\s*\(`,
)

// dfCSharpReturnRe captures a `return <expr>;` statement (group 1 = the returned
// expression). A handler that returns a tainted value emits a response sink.
var dfCSharpReturnRe = regexp.MustCompile(
	`^\s*return\s+(.+?)\s*;\s*$`,
)

// dfCSharpDeclAssignRe captures a typed/var local declaration with initialiser
// `Type name = <rhs>;` (group 1 = name, group 2 = rhs). The leading type token
// (or `var`) is required; a bare `name = rhs` reassignment is matched by
// dfCSharpBareAssignRe. The `[^=]` guards against matching `==`.
var dfCSharpDeclAssignRe = regexp.MustCompile(
	`^\s*(?:[A-Za-z_][\w<>\[\],.?\s]*?|var)\s+([A-Za-z_][\w]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfCSharpBareAssignRe captures `name = <rhs>` with no declared type (a
// reassignment), used to break taint when a tainted local is overwritten.
var dfCSharpBareAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_][\w]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfCSharpSinkSpecs is the ordered sink table reused at every scan depth.
var dfCSharpSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfCSharpDBWriteRe, DataFlowSinkDBWrite},
	{dfCSharpRespRe, DataFlowSinkResponse},
	{dfCSharpHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowCSharpEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := csharpFuncBodies(content, lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := csharpWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		// Seed [From*] action params as request-derived roots so a
		// [FromBody] dto / [FromQuery] string q parameter is tainted on entry.
		seed := csharpRequestParamTaints(lines, b)
		r := walkCSharpBody(ctx, b, seed)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowCSharp continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks. The
// returned flows' Function/SourceField/SourceLine are placeholders the links
// pass rewrites to the true origin handler.
func continueDataFlowCSharp(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := csharpFuncBodies(content, lines)
	callee := csharpBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := csharpParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := csharpWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkCSharpBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// csharpFuncBody is a method's line span (1-indexed, inclusive).
type csharpFuncBody struct {
	Name  string
	Start int // header line (the line carrying `name(`)
	End   int // line of the matching `}`
}

// csharpFuncBodies computes brace-balanced spans for every method/constructor
// header in the file, merging the shared scanCSharpFuncHeaders set with
// attribute-annotated action handlers whose multi-line parameter lists the
// single-line header regex can miss. Each header is snapped to the line carrying
// the name token and balanced with jstsMatchBraceEnd. De-duplicated by start.
func csharpFuncBodies(content string, lines []string) []csharpFuncBody {
	seen := map[int]bool{}
	var out []csharpFuncBody
	addHeader := func(name string, headerLine int) {
		start := csharpSnapHeaderLine(lines, headerLine, name)
		if seen[start] {
			return
		}
		end := jstsMatchBraceEnd(lines, start)
		if end == 0 {
			return
		}
		seen[start] = true
		out = append(out, csharpFuncBody{Name: name, Start: start, End: end})
	}
	for _, h := range scanCSharpFuncHeaders(content) {
		addHeader(h.Name, h.Line)
	}
	for _, h := range csharpActionHandlerHeaders(content, lines) {
		addHeader(h.Name, h.Line)
	}
	return out
}

// dfCSharpMethodNameRe matches the `NAME(` token that opens a method parameter
// list. Used to recover action handlers whose attribute / multi-line signatures
// the shared header regex can miss.
var dfCSharpMethodNameRe = regexp.MustCompile(`([A-Za-z_][\w]*)\s*\(`)

// csharpActionHandlerHeaders scans for controller-action headers whose parameter
// list declares a [From*] binding attribute. Non-handler methods are unaffected
// (the param block must contain a [From*] attribute AND reach an opening `{`).
func csharpActionHandlerHeaders(content string, lines []string) []funcHeader {
	var out []funcHeader
	for _, m := range dfCSharpMethodNameRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if name == "" || csharpControlKeyword(name) {
			continue
		}
		line := lineOfOffset(content, m[2])
		if line < 1 || line > len(lines) {
			continue
		}
		open := strings.IndexByte(lines[line-1], '(')
		if open < 0 {
			continue
		}
		sig := jstsCallArgs(lines, line, open)
		if !dfCSharpAnyFromAttrRe.MatchString(sig) {
			continue
		}
		out = append(out, funcHeader{Line: line, Name: name})
	}
	return out
}

// csharpSnapHeaderLine returns the 1-indexed line at/after `line` whose text
// contains `name` immediately followed (ignoring spaces) by `(` — the real
// header line. Falls back to `line` if not found in a small window.
func csharpSnapHeaderLine(lines []string, line int, name string) int {
	for i := line; i <= line+2 && i <= len(lines); i++ {
		if i < 1 {
			continue
		}
		s := lines[i-1]
		idx := 0
		for {
			j := strings.Index(s[idx:], name)
			if j < 0 {
				break
			}
			at := idx + j
			rest := strings.TrimLeft(s[at+len(name):], " \t")
			if strings.HasPrefix(rest, "(") {
				return i
			}
			idx = at + len(name)
		}
	}
	return line
}

// csharpWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type csharpWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []csharpFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkCSharpBody is the unified forward pass over a method body. The taint map is
// pre-seeded (request params, or a cross-file continuation) or empty.
func walkCSharpBody(ctx csharpWalkCtx, b csharpFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	// Skip the header line itself (its attributes/params are the seed, not a
	// statement) so a [FromQuery] literal isn't mistaken for a sink arg.
	for ln := b.Start + 1; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		csharpTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, csharpDirectSinks(ctx, ln, line, tainted)...)

		r := csharpFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// csharpTrackTaint applies one line's declaration/assignment effects to the
// taint map (last-write-wins), including seeding from an in-body request
// accessor (`var q = Request.Query["x"];`).
func csharpTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	if m := dfCSharpDeclAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if name == "return" { // `return x;` is not a declaration
			return
		}
		if fld, ok := csharpRequestAccessField(rhs); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			return
		}
		if fld, ok := csharpRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else if _, was := tainted[name]; was {
			delete(tainted, name) // declared/initialised to non-source → drop
		}
		return
	}
	if m := dfCSharpBareAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if fld, ok := csharpRequestAccessField(rhs); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			return
		}
		if fld, ok := csharpRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else if _, was := tainted[name]; was {
			delete(tainted, name) // reassigned to non-source → drop taint
		}
	}
}

// csharpRequestAccessField returns (field, true) when rhs is solely an in-body
// request accessor (`Request.Query["x"]`) whose key literal is the field.
func csharpRequestAccessField(rhs string) (string, bool) {
	e := strings.TrimSpace(rhs)
	m := dfCSharpRequestAccessRe.FindStringSubmatch(e)
	if m == nil {
		return "", false
	}
	// Require the accessor to BE the whole rhs (allowing a trailing `.ToString()`
	// or cast is out of scope — honest-partial; only a bare accessor binds).
	full := dfCSharpRequestAccessWholeRe.FindStringSubmatch(e)
	if full == nil {
		return "", false
	}
	return full[2], true
}

// dfCSharpRequestAccessWholeRe matches an rhs that is SOLELY a request accessor.
var dfCSharpRequestAccessWholeRe = regexp.MustCompile(
	`^(?:Request|HttpContext\s*\.\s*Request)\s*\.\s*(Query|Form|RouteValues|Headers|Cookies)\s*\[\s*"([^"]*)"\s*\]$`,
)

// csharpRHSSourceField returns (field, true) when rhs derives from a tainted
// value (a reference to a tainted root, optionally via a property / member),
// preserving provenance.
func csharpRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return csharpTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfCSharpMemberRe captures `<root>.Property` — group 1 = root, group 2 = the
// property (C# uses properties, not getX() getters, so `dto.Email` → "Email").
var dfCSharpMemberRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\.\s*([A-Za-z_][\w]*)\b`)

// csharpTaintedField resolves the source field for a reference to tainted root
// `name` as it appears in expr. A known field (a [FromQuery("q")] literal or
// param-name) always wins. Otherwise, when expr accesses a property
// (`dto.Email`) off the root, that property is lifted as the field. A method
// call (`dto.Build()`) is NOT a field — it is skipped.
func csharpTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfCSharpMemberRe.FindAllStringSubmatchIndex(expr, -1) {
		root := expr[m[2]:m[3]]
		if root != name {
			continue
		}
		member := expr[m[4]:m[5]]
		// Skip a method call `dto.Build(...)` — a property access has no `(`.
		rest := strings.TrimLeft(expr[m[5]:], " \t")
		if strings.HasPrefix(rest, "(") {
			continue
		}
		return member
	}
	return ""
}

// csharpDirectSinks emits flows for sinks on `line` whose args carry taint,
// including a `return <tainted>;` response.
func csharpDirectSinks(ctx csharpWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfCSharpSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := csharpSinkCallee(line, m)
			if callee == "" {
				continue
			}
			open := strings.Index(line[m[0]:], "(")
			if open < 0 {
				continue
			}
			args := jstsCallArgs(ctx.lines, ln, m[0]+open)
			if fld, ok := csharpExprTainted(args, tainted); ok {
				out = append(out, csharpMakeFlow(ctx, fld, s.kind, normalizeCSharpCallee(callee), ln))
			}
		}
	}
	// `return <tainted>;` from a handler is a response-body emission (only when
	// the returned value is a BARE tainted value, not wrapped in a builder the
	// sink table already handled above).
	if m := dfCSharpReturnRe.FindStringSubmatch(line); m != nil {
		if fld, ok := csharpArgBareTaint(m[1], tainted); ok {
			out = append(out, csharpMakeFlow(ctx, fld, DataFlowSinkResponse, "return", ln))
		}
	}
	return out
}

// csharpMakeFlow builds a DataFlow, preferring an already-carried field (from a
// cross-file continuation) over the locally-derived one.
func csharpMakeFlow(ctx csharpWalkCtx, fld string, kind DataFlowSinkKind, sink string, ln int) DataFlow {
	field := ctx.field
	if field == "" {
		field = fld
	}
	return DataFlow{
		Function:    ctx.origin,
		SourceField: field,
		SourceLine:  ctx.srcLine,
		SinkKind:    kind,
		SinkName:    sink,
		SinkLine:    ln,
		HopVia:      firstOf(ctx.hopPath),
		HopPath:     dupStrings(ctx.hopPath),
	}
}

// csharpSinkCallee returns the non-empty captured callee text from a sink match
// (the sink regexes use alternation groups; exactly one is set per match).
func csharpSinkCallee(line string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 && m[i+1] >= 0 {
			return line[m[i]:m[i+1]]
		}
	}
	return ""
}

// normalizeCSharpCallee collapses internal whitespace around `.` so a sink
// rendered across spacing reads canonically (`_context . Users . Add` →
// `_context.Users.Add`).
func normalizeCSharpCallee(s string) string {
	s = strings.TrimSpace(s)
	return dfCSharpDotSpaceRe.ReplaceAllString(s, ".")
}

var dfCSharpDotSpaceRe = regexp.MustCompile(`\s*\.\s*`)

// csharpFollowCalls handles each local-call on `line`: recurse into a same-file
// method (bounded + cycle-guarded) or record a cross-file boundary. Position
// binding is EXACT — an ambiguous/params arg drops (honest-partial).
func csharpFollowCalls(ctx csharpWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range csharpLocalCalls(line) {
		for pos, argExpr := range call.args {
			fld, bare := csharpArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := csharpBodyByName(ctx.bodies, call.name)
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
			param := csharpParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkCSharpBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// csharpExprTainted reports whether expr references a tainted variable, returning
// the field when derivable.
func csharpExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return csharpTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// dfCSharpTaintedMemberWholeRe matches an expr that is SOLELY `ident.Member`.
// Group 1 = root, group 2 = member.
var dfCSharpTaintedMemberWholeRe = regexp.MustCompile(`^([A-Za-z_][\w]*)\s*\.\s*([A-Za-z_][\w]*)$`)

// dfCSharpNewObjRe matches a constructor argument `new Type(...)` / `new Type {`
// so a tainted value wrapped in an object construction
// (`_context.Users.Add(new User { Email = dto.Email })`) is recognised as a
// tainted sink argument and the field is lifted.
var dfCSharpNewObjRe = regexp.MustCompile(`^new\s+[A-Za-z_][\w<>\[\].]*\s*[({]`)

// csharpArgBareTaint reports whether argExpr is EXACTLY a tainted value — a bare
// tainted identifier, a property access off a tainted root (`dto.Email`), or a
// constructor / object-initializer wrapping a tainted leaf
// (`new User { Email = dto.Email }`) — not embedded in arithmetic or a nested
// non-constructor call. Precision guard for sound positional binding.
func csharpArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if e == "" {
		return "", false
	}
	// An object construction wrapping a tainted value (`new User { Email =
	// dto.Email }` / `new User(dto.Email)`) is a real tainted sink argument —
	// bind the tainted leaf and lift its field. Inspect the construction body.
	if dfCSharpNewObjRe.MatchString(e) {
		return csharpNewObjTaint(e, tainted)
	}
	// Bare tainted identifier.
	if dfReSimpleIdent.MatchString(e) {
		if info, ok := tainted[e]; ok {
			return info.field, true
		}
		return "", false
	}
	// `dto.Email` — property off a tainted root (no trailing call).
	if m := dfCSharpTaintedMemberWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// csharpNewObjTaint inspects a `new Type(...)` / `new Type { ... }` expression
// for a tainted leaf, lifting the first one's field. For an object initializer
// it scans the `Prop = <tainted>` assignments; for a constructor it scans the
// positional arguments.
func csharpNewObjTaint(e string, tainted map[string]taintInfo) (string, bool) {
	// Object initializer `new User { Email = dto.Email, ... }`.
	if brace := strings.IndexByte(e, '{'); brace >= 0 {
		inner := e[brace+1:]
		if close := strings.LastIndexByte(inner, '}'); close >= 0 {
			inner = inner[:close]
		}
		for _, part := range strings.Split(inner, ",") {
			if eq := strings.IndexByte(part, '='); eq >= 0 {
				if fld, ok := csharpArgBareTaint(part[eq+1:], tainted); ok {
					return fld, true
				}
			}
		}
		return "", false
	}
	// Constructor `new User(dto.Email)`.
	if open := strings.IndexByte(e, '('); open >= 0 {
		args := jstsSplitArgs(jstsCallArgs([]string{e}, 1, open))
		for _, part := range args {
			if fld, ok := csharpArgBareTaint(part, tainted); ok {
				return fld, true
			}
		}
	}
	return "", false
}

// csharpLocalCall is a parsed `name(arg0, arg1, …)` call to a bare identifier.
type csharpLocalCall struct {
	name string
	args []string
}

// dfCSharpLocalCallRe matches a call to a bare identifier (potential local fn).
var dfCSharpLocalCallRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

// csharpLocalCalls extracts candidate bare-identifier method calls on a line
// with their top-level positional argument expressions. Member calls (`obj.Foo(`),
// `new Type(`, and control keywords are skipped — only a same-class
// bare-identifier call is a hop / boundary candidate.
func csharpLocalCalls(line string) []csharpLocalCall {
	var out []csharpLocalCall
	for _, m := range dfCSharpLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue // member call — not a bare-ident call
			}
			if strings.HasSuffix(prev, "new") {
				continue // constructor — handled as a sink arg, not a hop
			}
		}
		if csharpControlKeyword(name) || csharpRespBuilderName(name) {
			continue // control keyword or a response builder (a sink, not a hop)
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, csharpLocalCall{name: name, args: args})
	}
	return out
}

// csharpRespBuilderName reports whether name is an MVC response builder that the
// sink table owns — it must not be treated as a local-method hop / boundary.
func csharpRespBuilderName(name string) bool {
	switch name {
	case "Ok", "Created", "CreatedAtAction", "CreatedAtRoute", "Accepted",
		"AcceptedAtAction", "Json", "Content", "PartialView", "View",
		"StatusCode", "BadRequest", "NotFound", "Unauthorized":
		return true
	}
	return false
}

// csharpRequestParamTaints returns the taint seed for an action whose signature
// declares [From*] request-binding parameters. Each bound parameter identifier
// becomes a tainted root; the field is the attribute's literal value
// ([FromQuery(Name="q")] → "q"), else the parameter NAME for a scalar binder
// ([FromQuery] string q → "q"), else "" for a whole-object [FromBody] (recovered
// later from a property access). Returns an empty map for a non-action method,
// so plain methods (and injected-service params) are unaffected.
func csharpRequestParamTaints(lines []string, b csharpFuncBody) map[string]taintInfo {
	out := map[string]taintInfo{}
	if b.Start < 1 || b.Start > len(lines) {
		return out
	}
	open := strings.IndexByte(lines[b.Start-1], '(')
	if open < 0 {
		return out
	}
	sig := jstsCallArgs(lines, b.Start, open)
	if sig == "" || !strings.Contains(sig, "[") {
		return out
	}
	for _, m := range dfCSharpFromAttrRe.FindAllStringSubmatch(sig, -1) {
		binder, literal, name := m[1], m[2], m[3]
		if name == "" {
			continue
		}
		field := literal
		// A scalar binder (query / route / form / header) with no literal infers
		// the request key from the parameter name; [FromBody] binds a whole
		// object whose field is derived later from a property access.
		if field == "" && binder != "Body" {
			field = name
		}
		out[name] = taintInfo{field: field, line: b.Start}
	}
	return out
}

// csharpParamName returns the name of the pos-th positional parameter of the
// method whose header is on headerLine. C# parameters may carry leading
// attributes (`[FromQuery] string q`), generic/array/nullable types, and
// default values; the trailing identifier before a `,`/`)`/`=` is the name. A
// params parameter (`params T[] xs`) makes positions past it unreliable → "".
func csharpParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	open := strings.IndexByte(lines[headerLine-1], '(')
	if open < 0 {
		return ""
	}
	sig := jstsCallArgs(lines, headerLine, open)
	params := jstsSplitArgs(sig)
	if pos < 0 || pos >= len(params) {
		return ""
	}
	for _, p := range params {
		if strings.Contains(p, "params ") {
			return "" // params array — ambiguous positions
		}
	}
	return csharpParamIdent(params[pos])
}

// dfCSharpParamIdentRe captures the parameter identifier — the trailing token
// before an optional `= default` — after any attributes and the type.
var dfCSharpParamIdentRe = regexp.MustCompile(`([A-Za-z_][\w]*)\s*(?:=[^,]*)?\s*$`)

// csharpParamIdent extracts the parameter name from one parameter declaration,
// stripping any attribute prefixes, the (possibly generic/nullable) type token,
// and a trailing default value.
func csharpParamIdent(decl string) string {
	d := strings.TrimSpace(decl)
	if d == "" {
		return ""
	}
	m := dfCSharpParamIdentRe.FindStringSubmatch(d)
	if m == nil {
		return ""
	}
	return m[1]
}

// csharpBodyByName returns the body with the given name, or nil.
func csharpBodyByName(all []csharpFuncBody, name string) *csharpFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
