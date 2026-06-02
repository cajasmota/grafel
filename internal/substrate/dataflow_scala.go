// Scala request-input → sink dataflow sniffer (#3628 area #22, epic #3872,
// audit #3887, #3991). Extends the universal cross-language dataflow pass to
// an 8th language (py/jsts/go/ruby/java/php/csharp already have one).
//
// SCOPED def→use tracking inside one method body, followed through up to
// DataFlowMaxHops local (same-file) method-call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an imported / external
// callee. See dataflow.go for the contract and the honest-partial boundary.
// Mirrors dataflow_java.go / dataflow_python.go / dataflow_jsts.go exactly:
// same DataFlow/source/sink model, same DATA_FLOWS_TO emission, same
// precision-over-recall discipline.
//
// Source vocabulary is aligned with taint_sites_scala.go (do NOT duplicate —
// the request-input shapes there are the source surface here). Unlike Spring,
// a Scala handler reads request input INSIDE the body, not via an annotated
// parameter, so taint is seeded at the read site (`val x = request.body…`) and
// for a whole-request expression flowed straight into a sink:
//
//   - Play          : `request.body` / `request.queryString` / `request.headers`
//     / `request.getQueryString(...)` / `parse.json` extractor results.
//   - Akka / Pekko  : `entity(as[T]) { dto => … }` binds `dto`; `parameter("q")`
//     / `formField("f")` bind the directive result.
//   - http4s / tapir: `req.as[T]` / `req.params` / `req.bodyText`.
//     The accessed field name is captured when statically knowable
//     (`request.getQueryString("q")` → "q"; `dto.email` → "email").
//
// Sinks recognised (aligned with effect_sinks_scala.go write subset):
//   - db_write : Slick `q += …` / `q.insertOrUpdate` / `.update` / `.delete` /
//     `.forceInsert`, Doobie `sql"INSERT…".update.run` / `.transact`, Quill
//     `run(query[T].insert(…))`, JPA `em.persist` / `merge` / `remove`, when an
//     argument carries taint.
//   - response : Play `Ok(<tainted>)` / `Created(...)` / `BadRequest(...)`,
//     Akka/Pekko `complete(<tainted>)`, and `def handler = Ok(<tainted>)`
//     expression bodies.
//   - http_call: outbound sttp `basicRequest.post(...).body(<tainted>)`, http4s
//     client `client.expect(...)` / `Request(...).withEntity(<tainted>)`,
//     requests-scala `requests.post(..., data = <tainted>)` — a CONSUMES_API
//     site, with a tainted body.
//
// HONEST-PARTIAL (precision over recall): a request read that never reaches a
// sink (logged / discarded), a sink fed by a constant with no request
// provenance, reassignment that breaks the chain, embedded-expression args that
// cannot be bound positionally, >DataFlowMaxHops depth, and recursion are
// DROPPED — never fabricated. A whole-request flow with no derivable field is
// emitted with field="".
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("scala", sniffDataFlowScalaEx, continueDataFlowScala)
}

// sniffDataFlowScala preserves the legacy in-file-only entry point.
func sniffDataFlowScala(content string) []DataFlow { return sniffDataFlowScalaEx(content).Flows }

// ---- source recognition (aligned with taint_sites_scala.go) ----

// dfScalaReqAccessRe matches a request-input read whose result is the RHS of a
// binding or flows into a sink. Group 1 (when present) is the statically-known
// field for a keyed accessor (`request.getQueryString("q")` → "q",
// `request.headers.get("X")` → "X", `req.params("id")` → "id").
var dfScalaReqAccessRe = regexp.MustCompile(
	`\b(?:request|req)\s*\.\s*(?:body|queryString|rawQueryString|cookies|headers)\b` +
		`|\b(?:request|req)\s*\.\s*getQueryString\s*\(\s*"([^"]*)"` +
		`|\b(?:request|req)\s*\.\s*headers\s*\.\s*(?:get|apply)\s*\(\s*"([^"]*)"` +
		`|\b(?:request|req)\s*\.\s*params\s*\(\s*"([^"]*)"` +
		`|\b(?:request|req)\s*\.\s*(?:bodyText|bodyAsText|multiParams)\b` +
		`|\breq\s*\.\s*as\s*\[` +
		`|\bparse\s*\.\s*(?:json|urlFormEncoded|text|tolerantJson|multipartFormData)\b`,
)

// dfScalaEntityBindRe matches `entity(as[T]) { dto =>` — the bound lambda
// parameter (group 1) is the request body, tainted with field "" (recovered
// later from `dto.email`).
var dfScalaEntityBindRe = regexp.MustCompile(
	`\bentity\s*\(\s*as\[[^\]]*\]\s*\)\s*\{\s*([A-Za-z_][\w]*)\s*=>`,
)

// dfScalaDirectiveBindRe matches `parameter("q") { q =>` / `formField("f") { f
// =>` — the bound lambda parameter (group 2) is the extracted value, tainted
// with the directive key (group 1) as its field.
var dfScalaDirectiveBindRe = regexp.MustCompile(
	`\b(?:parameter|formField|headerValueByName)\s*\(\s*"([^"]*)"\s*\)\s*\{\s*([A-Za-z_][\w]*)\s*=>`,
)

// ---- sink recognition (aligned with effect_sinks_scala.go write subset) ----

// dfScalaDBWriteRe matches a Slick / Doobie / Quill / JPA write. Group 1 = the
// callee text. A Slick `q += row` / `q.insertOrUpdate(row)`, a Doobie
// `sql"…".update`, a Quill `run(...)`, JPA `em.persist`.
var dfScalaDBWriteRe = regexp.MustCompile(
	`\b((?:entityManager|em)\s*\.\s*(?:persist|merge|remove|refresh))\s*\(` +
		`|\b([A-Za-z_][\w]*\s*\.\s*(?:insertOrUpdate|insertAll|forceInsert|forceInsertAll|delete|deleteWhere|update))\s*\(` +
		`|\b([A-Za-z_][\w]*\s*\+\+?=)\s*` +
		`|\b(run)\s*\(`,
)

// dfScalaRespRe matches a Play / Akka-Pekko response-body emission. Group 1 =
// the callee text. Play result helpers (`Ok`, `Created`, …) and Akka/Pekko
// `complete(...)`.
var dfScalaRespRe = regexp.MustCompile(
	`\b(Ok|Created|Accepted|BadRequest|NotFound|InternalServerError|Forbidden|Unauthorized)\s*\(` +
		`|\b(complete)\s*\(`,
)

// dfScalaHTTPCallRe matches an outbound HTTP call carrying a tainted body.
// Group 1 = callee. sttp `.body(...)` / `.post(...)`, http4s client
// `client.expect` / `.withEntity`, requests-scala `requests.post`.
var dfScalaHTTPCallRe = regexp.MustCompile(
	`\b(basicRequest\s*\.\s*(?:post|put|patch|delete))\s*\(` +
		`|((?:[A-Za-z_][\w]*|\))\s*\.\s*withEntity)\s*\(` +
		`|\b((?:client|Client)\s*\.\s*(?:expect|fetchAs|run))\s*\(` +
		`|\b(requests\s*\.\s*(?:post|put|patch|delete))\s*\(` +
		`|((?:[A-Za-z_][\w]*|\))\s*\.\s*body)\s*\(`,
)

// dfScalaSinkSpecs is the ordered sink table reused at every scan depth.
var dfScalaSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfScalaDBWriteRe, DataFlowSinkDBWrite},
	{dfScalaRespRe, DataFlowSinkResponse},
	{dfScalaHTTPCallRe, DataFlowSinkHTTPCall},
}

// ---- local binding recognition ----

// dfScalaDeclAssignRe captures a Scala local binding `val|var name [: T] = rhs`
// (group 1 = name, group 2 = rhs). `lazy` is tolerated. The `[^=]` guards
// against `==`.
var dfScalaDeclAssignRe = regexp.MustCompile(
	`^\s*(?:lazy\s+)?(?:val|var)\s+([A-Za-z_][\w]*)\s*(?::\s*[A-Za-z_][\w$<>\[\],.?\s]*?)?\s*=\s*([^=].*?)\s*$`,
)

// dfScalaBareAssignRe captures `name = rhs` (a `var` reassignment, no keyword),
// used to break taint when a tainted local is overwritten.
var dfScalaBareAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_][\w]*)\s*=\s*([^=].*?)\s*$`,
)

func sniffDataFlowScalaEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := scalaFuncBodies(content, lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := scalaWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		// Seed directive-bound lambda parameters (Akka/Pekko `entity(as[T]) {
		// dto => }`, `parameter("q") { q => }`) as request-derived roots. A
		// Play handler instead reads `request.body` inside the body, seeded by
		// scalaTrackTaint at the read site.
		seed := scalaDirectiveTaints(ctx.lines, b)
		r := walkScalaBody(ctx, b, seed)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowScala continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks. The
// returned flows' Function/SourceField/SourceLine are placeholders the links
// pass rewrites to the true origin handler.
func continueDataFlowScala(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := scalaFuncBodies(content, lines)
	callee := scalaBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := scalaParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := scalaWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkScalaBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// scalaFuncBody is a method's line span (1-indexed, inclusive).
type scalaFuncBody struct {
	Name  string
	Start int // header line (the line carrying `def name`)
	End   int // line of the matching `}`, or Start for an expression body
}

// scalaFuncBodies computes spans for every `def` header. A brace body
// (`def f(...) = { … }` / `def f(...) { … }`) is balanced with the shared
// jstsMatchBraceEnd; a single-expression body (`def f(...): T = Ok(x)`) spans
// the header line only. De-duplicated by start line.
func scalaFuncBodies(content string, lines []string) []scalaFuncBody {
	seen := map[int]bool{}
	var out []scalaFuncBody
	for _, h := range scanScalaFuncHeaders(content) {
		start := h.Line
		if start < 1 || start > len(lines) || seen[start] {
			continue
		}
		seen[start] = true
		end := start
		if scalaHeaderOpensBrace(lines, start) {
			if be := jstsMatchBraceEnd(lines, start); be != 0 {
				end = be
			}
		}
		out = append(out, scalaFuncBody{Name: h.Name, Start: start, End: end})
	}
	return out
}

// scalaHeaderOpensBrace reports whether the def whose header is on `line` has a
// brace body. The header line, or the next non-empty continuation, carries `{`
// before any terminating expression — a heuristic that holds for the common
// `def f(...) = {` and `def f(...) {` forms. A `= expr` body has no `{`.
func scalaHeaderOpensBrace(lines []string, line int) bool {
	for i := line; i <= line+1 && i <= len(lines); i++ {
		if i < 1 {
			continue
		}
		s := lines[i-1]
		if strings.ContainsRune(s, '{') {
			return true
		}
		// A `=` with non-brace text after it on the header line is an
		// expression body; stop looking.
		if eq := strings.Index(s, "="); eq >= 0 {
			rest := strings.TrimSpace(s[eq+1:])
			if rest != "" {
				return false
			}
		}
	}
	return false
}

// scalaWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type scalaWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []scalaFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkScalaBody is the unified forward pass over a method body. The taint map
// is pre-seeded (directive params, or a cross-file continuation) or empty (a
// Play handler that reads `request.body` here).
func walkScalaBody(ctx scalaWalkCtx, b scalaFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		scalaTrackTaint(tainted, line, ln, &ctx)

		res.Flows = append(res.Flows, scalaDirectSinks(ctx, ln, line, tainted)...)

		r := scalaFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// scalaReqAccessField returns (field, true) when rhs reads a request input,
// lifting the statically-known key when present (else "").
func scalaReqAccessField(rhs string) (string, bool) {
	m := dfScalaReqAccessRe.FindStringSubmatch(rhs)
	if m == nil {
		return "", false
	}
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i], true
		}
	}
	return "", true
}

// scalaTrackTaint applies one line's binding effects to the taint map
// (last-write-wins): a `val x = <request-source>` taints x; a `val y =
// <tainted-ref>` propagates; a binding/reassignment to a non-source drops it.
func scalaTrackTaint(tainted map[string]taintInfo, line string, ln int, ctx *scalaWalkCtx) {
	apply := func(name, rhs string) {
		if name == "" || name == "return" {
			return
		}
		// A direct request-input read seeds taint (Play `request.body`, etc.).
		if fld, ok := scalaReqAccessField(rhs); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			if ctx.srcLine == 0 {
				ctx.srcLine = ln
			}
			return
		}
		// Propagation from an already-tainted value.
		if fld, ok := scalaRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			return
		}
		// Bound/assigned to a non-source → drop any prior taint.
		if _, was := tainted[name]; was {
			delete(tainted, name)
		}
	}
	if m := dfScalaDeclAssignRe.FindStringSubmatch(line); m != nil {
		apply(m[1], m[2])
		return
	}
	if m := dfScalaBareAssignRe.FindStringSubmatch(line); m != nil {
		apply(m[1], m[2])
	}
}

// scalaRHSSourceField returns (field, true) when rhs derives from a tainted
// value (a reference to a tainted root, optionally via a member), preserving
// provenance.
func scalaRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return scalaTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfScalaMemberRe captures `<root>.member` — group 1 = root, group 2 = member.
var dfScalaMemberRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\.\s*([A-Za-z_][\w]*)\b`)

// scalaTaintedField resolves the source field for a reference to tainted root
// `name` as it appears in expr. A known field always wins; otherwise a direct
// member access off the root (`dto.email`) is lifted as the field.
func scalaTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfScalaMemberRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// scalaDirectSinks emits flows for sinks on `line` whose args carry taint.
func scalaDirectSinks(ctx scalaWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfScalaSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := scalaSinkCallee(line, m)
			if callee == "" {
				continue
			}
			open := strings.Index(line[m[0]:], "(")
			if open < 0 {
				// A Slick `q += row` write has no `(`; bind the rest of line.
				if strings.Contains(callee, "+=") {
					rhs := strings.TrimSpace(line[m[1]:])
					if fld, ok := scalaExprTainted(rhs, tainted); ok {
						out = append(out, scalaMakeFlow(ctx, fld, s.kind, normalizeScalaCallee(callee), ln))
					}
				}
				continue
			}
			args := jstsCallArgs(ctx.lines, ln, m[0]+open)
			if fld, ok := scalaExprTainted(args, tainted); ok {
				out = append(out, scalaMakeFlow(ctx, fld, s.kind, normalizeScalaCallee(callee), ln))
			}
		}
	}
	return out
}

// scalaMakeFlow builds a DataFlow, preferring an already-carried field (from a
// cross-file continuation) over the locally-derived one.
func scalaMakeFlow(ctx scalaWalkCtx, fld string, kind DataFlowSinkKind, sink string, ln int) DataFlow {
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

// scalaSinkCallee returns the non-empty captured callee text from a sink match.
func scalaSinkCallee(line string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 && m[i+1] >= 0 {
			return line[m[i]:m[i+1]]
		}
	}
	return ""
}

// normalizeScalaCallee collapses internal whitespace around `.` and `+=`.
func normalizeScalaCallee(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ")") // a `…).body` chain — keep just the tail call
	s = strings.TrimLeft(s, ". ")
	s = dfScalaDotSpaceRe.ReplaceAllString(s, ".")
	return strings.Join(strings.Fields(s), " ")
}

var dfScalaDotSpaceRe = regexp.MustCompile(`\s*\.\s*`)

// scalaFollowCalls handles each local-call on `line`: recurse into a same-file
// method (bounded + cycle-guarded) or record a cross-file boundary. Position
// binding is EXACT — an ambiguous arg drops (honest-partial).
func scalaFollowCalls(ctx scalaWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range scalaLocalCalls(line) {
		for pos, argExpr := range call.args {
			fld, bare := scalaArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := scalaBodyByName(ctx.bodies, call.name)
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
			param := scalaParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkScalaBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// scalaExprTainted reports whether expr references a tainted variable,
// returning the field when derivable.
func scalaExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return scalaTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// dfScalaTaintedMemberWholeRe matches an expr that is SOLELY `ident.member`.
var dfScalaTaintedMemberWholeRe = regexp.MustCompile(`^([A-Za-z_][\w]*)\s*\.\s*([A-Za-z_][\w]*)$`)

// dfScalaNewObjRe matches a constructor argument `new Type(...)` or a case-class
// apply `Type(...)` capitalised — so a tainted value wrapped in entity
// construction (`repo += User(dto.email)`) is recognised and the field lifted.
var dfScalaNewObjRe = regexp.MustCompile(`^(?:new\s+)?[A-Z][\w]*\s*\(`)

// scalaArgBareTaint reports whether argExpr is EXACTLY a tainted value — a bare
// tainted identifier, a member access off a tainted root (`dto.email`), or a
// case-class / constructor wrapping a tainted leaf (`User(dto.email)`) — not
// embedded in arithmetic or string interpolation. Precision guard for sound
// positional binding.
func scalaArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if e == "" {
		return "", false
	}
	// A constructor / case-class apply wrapping a tainted value.
	if dfScalaNewObjRe.MatchString(e) {
		open := strings.IndexByte(e, '(')
		if open >= 0 && strings.HasSuffix(e, ")") {
			inner := e[open+1 : len(e)-1]
			for _, part := range jstsSplitArgs(inner) {
				if fld, ok := scalaArgBareTaint(part, tainted); ok {
					return fld, true
				}
			}
		}
		return "", false
	}
	// Bare tainted identifier.
	if dfReSimpleIdent.MatchString(e) {
		if info, ok := tainted[e]; ok {
			return info.field, true
		}
		return "", false
	}
	// `dto.email` — direct member off a tainted root.
	if m := dfScalaTaintedMemberWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// scalaLocalCall is a parsed `name(arg0, arg1, …)` call to a bare identifier.
type scalaLocalCall struct {
	name string
	args []string
}

// dfScalaLocalCallRe matches a call to a bare identifier (potential local fn).
var dfScalaLocalCallRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

// scalaControlKeyword reports whether name is a Scala control / keyword token
// that must not be treated as a local-function hop.
func scalaControlKeyword(name string) bool {
	switch name {
	case "if", "for", "while", "match", "case", "yield", "return", "throw",
		"new", "def", "val", "var", "do", "else", "try", "catch", "finally",
		"map", "flatMap", "foreach", "filter", "withFilter", "println", "print":
		return true
	}
	return false
}

// scalaLocalCalls extracts candidate bare-identifier calls on a line with their
// top-level positional argument expressions. Method calls (`obj.foo(`), case-
// class applies (Capitalised), and control keywords are skipped — only a
// lower-case bare-identifier call is a hop / boundary candidate.
func scalaLocalCalls(line string) []scalaLocalCall {
	var out []scalaLocalCall
	for _, m := range dfScalaLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if name == "" {
			continue
		}
		// A Capitalised name is a constructor / case-class apply — handled as a
		// sink arg, not a hop.
		if name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue // method call — not a bare-ident call
			}
		}
		if scalaControlKeyword(name) {
			continue
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, scalaLocalCall{name: name, args: args})
	}
	return out
}

// scalaDirectiveTaints seeds taint for Akka/Pekko HTTP value-extracting
// directives in the body whose bound lambda parameter is request-derived:
// `entity(as[T]) { dto => }` (field "", recovered later) and `parameter("q") {
// q => }` (field "q"). Returns an empty map for a handler with no directive
// binds, so a plain method is unaffected.
func scalaDirectiveTaints(lines []string, b scalaFuncBody) map[string]taintInfo {
	out := map[string]taintInfo{}
	for ln := b.Start; ln <= b.End && ln <= len(lines); ln++ {
		line := lines[ln-1]
		for _, m := range dfScalaEntityBindRe.FindAllStringSubmatch(line, -1) {
			if m[1] != "" {
				out[m[1]] = taintInfo{field: "", line: ln}
			}
		}
		for _, m := range dfScalaDirectiveBindRe.FindAllStringSubmatch(line, -1) {
			if m[2] != "" {
				out[m[2]] = taintInfo{field: m[1], line: ln}
			}
		}
	}
	return out
}

// scalaParamName returns the name of the pos-th positional parameter of the
// method whose header is on headerLine. Scala parameters are `name: Type`
// (the leading identifier before `:` is the name); the call binds positionally.
func scalaParamName(lines []string, headerLine, pos int) string {
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
	return scalaParamIdent(params[pos])
}

// dfScalaParamLeadingIdentRe captures the leading identifier of a Scala
// parameter declaration `name: Type` — the parameter name.
var dfScalaParamLeadingIdentRe = regexp.MustCompile(`^\s*(?:implicit\s+)?([A-Za-z_][\w]*)\s*:`)

// scalaParamIdent extracts the parameter name from one parameter declaration.
func scalaParamIdent(decl string) string {
	if m := dfScalaParamLeadingIdentRe.FindStringSubmatch(decl); m != nil {
		return m[1]
	}
	return ""
}

// scalaBodyByName returns the body with the given name, or nil.
func scalaBodyByName(all []scalaFuncBody, name string) *scalaFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
