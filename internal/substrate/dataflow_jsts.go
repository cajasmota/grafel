// JS/TS request-input → sink dataflow sniffer (#3628 area #22).
//
// SCOPED def→use tracking inside one function body, followed through up to
// DataFlowMaxHops local-call hops, PLUS cross-file boundary emission for a
// tainted value that escapes into an imported (non-local) callee. See
// dataflow.go for the contract and the honest-partial boundary.
//
// Sources recognised:
//   - req.body.X / req.query.X / req.params.X         (Express / generic)
//   - request.body.X / request.query.X / request.params.X
//     (Fastify / Hono / generic)
//   - request.payload.X                               (Hapi)
//   - ctx.request.body.X / ctx.request.query.X        (Koa)
//   - ctx.query.X / ctx.params.X                      (Koa)
//   - @Body()/@Query()/@Param()/@Headers()/@Req()/@Request() decorated
//     controller-method parameters                    (NestJS, #3902)
//
// NestJS sources are PARAMETERS, not member accesses: a parameter decorated
// `@Body() dto` makes the identifier `dto` a request-derived root (the same
// role `req.body` plays in Express). At handler entry the decorated params
// are seeded into the taint map. The source field is the decorator's literal
// key when present (`@Body('email') x` → field "email") or, failing that,
// the member accessed off the param (`dto.email` → field "email").
//
// Sinks recognised:
//   - DB write : <recv>.create( / <recv>.save( / <recv>.insert( /
//     prisma.<m>.create( / repo.save(
//   - response : res.json( / res.send( / response.json(
//   - http_call: axios( / axios.get|post|put|delete( / fetch( — treated as
//     an outbound CONSUMES_API call site
package substrate

import (
	"regexp"
	"sort"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("jsts", sniffDataFlowJSTSEx, continueDataFlowJSTS)
}

// sniffDataFlowJSTS preserves the legacy in-file-only entry point.
func sniffDataFlowJSTS(content string) []DataFlow { return sniffDataFlowJSTSEx(content).Flows }

// dfJstsSourceFieldRe captures a request-input read with a STATIC field
// name. Group 1 = field. Anchored on the canonical receivers. Dynamic
// access (`req.body[k]`) is intentionally NOT matched (honest-partial).
var dfJstsSourceFieldRe = regexp.MustCompile(
	`\b(?:req|request)\s*\.\s*(?:body|query|params|payload)\s*\.\s*([A-Za-z_$][\w$]*)\b` +
		`|\bctx\s*\.\s*request\s*\.\s*(?:body|query)\s*\.\s*([A-Za-z_$][\w$]*)\b` +
		`|\bctx\s*\.\s*(?:query|params)\s*\.\s*([A-Za-z_$][\w$]*)\b`,
)

// dfJstsFirstGroup returns the first non-empty capture group of a
// dfJstsSourceFieldRe submatch (the recognised receiver alternatives each use
// a distinct group, so exactly one is non-empty for a match). Returns "" when
// none captured a static field (whole-object source).
func dfJstsFirstGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// dfJstsSourceAnyRe matches the source receiver prefix without requiring a
// field, used to detect whole-object pass-through (`res.json(req.body)`).
var dfJstsSourceAnyRe = regexp.MustCompile(
	`\b(?:req|request)\s*\.\s*(?:body|query|params|payload)\b` +
		`|\bctx\s*\.\s*request\s*\.\s*(?:body|query)\b` +
		`|\bctx\s*\.\s*(?:query|params)\b`,
)

// dfJstsNestJSParamRe matches a NestJS controller-method parameter injected
// by a request decorator (#3902). Group 1 = the decorator's optional string
// literal key (the request-input field, e.g. `id` in `@Param('id')`); group
// 2 = the bound parameter identifier (the tainted root). The decorator's
// param list is matched non-greedily so a quoted key is captured but other
// option objects are tolerated. Mirrors the security taint-site recogniser
// (taint_sites_jsts.go jstsSourceNestJSDecoratorRe) but additionally lifts
// the literal key as field provenance.
var dfJstsNestJSParamRe = regexp.MustCompile(
	`@(?:Body|Query|Param|Headers|Req|Request)\s*\(\s*(?:['"]([\w$-]+)['"])?[^)]*\)\s*` +
		`([A-Za-z_$][\w$]*)`,
)

// dfJstsDBWriteRe matches an ORM write call. Group 1 = the callee text.
var dfJstsDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$.]*\.(?:create|save|insert|update))\s*\(`,
)

// dfJstsRespRe matches a response-body emission. Group 1 = callee.
var dfJstsRespRe = regexp.MustCompile(
	`\b((?:res|response)\s*\.\s*(?:json|send))\s*\(`,
)

// dfJstsHTTPCallRe matches an outbound HTTP call. Group 1 = callee.
var dfJstsHTTPCallRe = regexp.MustCompile(
	`\b(axios(?:\s*\.\s*(?:get|post|put|delete|patch))?|fetch)\s*\(`,
)

// dfJstsConstAssignRe captures `const|let|var NAME = ...` (group 1 = name),
// for taint propagation. Reassignment is handled by the caller.
var dfJstsConstAssignRe = regexp.MustCompile(
	`^\s*(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(.*)$`,
)

// dfJstsBareAssignRe captures a bare `NAME = ...` (no decl keyword), used to
// detect chain-breaking reassignment of a previously-tainted variable.
var dfJstsBareAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_$][\w$]*)\s*=\s*([^=].*)$`,
)

// dfJstsDestructureRe captures an object-destructuring declaration:
// `const { a, b: c } = rhs`. Group 1 = the brace-enclosed binding list,
// group 2 = the right-hand side. A rest element (`...x`) or computed key
// (`[k]: v`) makes the binding list non-static; such forms are filtered per
// element by jstsDestructureFields rather than rejected wholesale here.
var dfJstsDestructureRe = regexp.MustCompile(
	`^\s*(?:const|let|var)\s*\{\s*([^{}]*?)\s*\}\s*=\s*(.*)$`,
)

// dfJstsDestructEntryRe captures one destructuring entry: `prop` or
// `prop: local` (with optional default after `=`, ignored). Group 1 = the
// source property name, group 2 = the optional rebound local name. Entries
// using a rest (`...x`), a computed key, or a nested pattern do not match and
// are skipped (honest-partial).
var dfJstsDestructEntryRe = regexp.MustCompile(
	`^\s*([A-Za-z_$][\w$]*)\s*(?::\s*([A-Za-z_$][\w$]*))?\s*(?:=[^,]*)?$`,
)

// dfJstsSinkSpecs is the ordered sink table reused at every scan depth.
var dfJstsSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfJstsDBWriteRe, DataFlowSinkDBWrite},
	{dfJstsRespRe, DataFlowSinkResponse},
	{dfJstsHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowJSTSEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := jstsAllHeaders(content)
	bodies := jstsFuncBodies(content, headers)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := jstsWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		// Seed NestJS decorator-injected params as request-derived roots so a
		// `@Body() dto`/`@Query('q') q` parameter is tainted on handler entry,
		// exactly as if it had been assigned from `req.body` (#3902).
		seed := jstsNestJSParamTaints(lines, b)
		r := walkJSTSBody(ctx, b, seed)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// dfJstsNestJSMethodHeaderRe matches the start of a class-method header whose
// name token opens a parameter list: `[async ][public|private|protected ]NAME(`
// at the start of a line. The shared jstsFuncHeaderRe deliberately rejects a
// param list containing `)` (its `[^)\n]*` stops at the first inner paren), so
// a NestJS method like `create(@Body() dto)` is NOT seen as a header there.
// This dedicated matcher recovers those headers for the dataflow pass only;
// candidates are confirmed by checking the parameter block actually contains
// a request decorator (jstsNestJSHeaders), so plain methods are unaffected.
var dfJstsNestJSMethodHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:async\s+)?(?:public\s+|private\s+|protected\s+)?(?:async\s+)?` +
		`([A-Za-z_$][\w$]*)\s*\(`,
)

// jstsNestJSHeaders scans for NestJS controller-method headers — methods whose
// parameter list contains an @Body/@Query/@Param/@Headers/@Req/@Request
// decorator. These are the dataflow handlers the shared header regex misses.
// The returned headers carry the method name and its 1-indexed line.
func jstsNestJSHeaders(content string) []funcHeader {
	lines := strings.Split(content, "\n")
	var out []funcHeader
	for _, m := range dfJstsNestJSMethodHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if name == "" || jstsControlKeyword(name) || name == "constructor" || name == "function" {
			continue
		}
		line := lineOfOffset(content, m[2])
		open := strings.IndexByte(lines[line-1], '(')
		if open < 0 {
			continue
		}
		// The param block must contain a request decorator AND close with `{`
		// (an actual method body), else this is a call site, not a header.
		sig := jstsCallArgs(lines, line, open)
		if !dfJstsNestJSParamRe.MatchString(sig) {
			continue
		}
		out = append(out, funcHeader{Line: line, Name: name})
	}
	return out
}

// jstsAllHeaders merges the shared JS/TS headers with the NestJS
// controller-method headers the shared regex misses, de-duplicated by line so
// a method seen by both is counted once. Order is by line for determinism.
func jstsAllHeaders(content string) []funcHeader {
	base := scanJSTSFuncHeaders(content)
	seen := make(map[int]bool, len(base))
	for _, h := range base {
		seen[h.Line] = true
	}
	for _, h := range jstsNestJSHeaders(content) {
		if seen[h.Line] {
			continue
		}
		seen[h.Line] = true
		base = append(base, h)
	}
	sort.Slice(base, func(i, j int) bool { return base[i].Line < base[j].Line })
	return base
}

// jstsNestJSParamTaints returns the taint seed for a function body whose
// signature declares NestJS request-decorator parameters. Each decorated
// parameter identifier becomes a tainted root; the seed field is the
// decorator's literal key when present (`@Param('id') id` → field "id"),
// otherwise "" (the field is recovered later from a member access such as
// `dto.email`). The parameter block is read from the header line through the
// matching `)` so multi-line signatures (one decorated param per line, the
// idiomatic NestJS form) are covered. Returns an empty map when no decorator
// params are present, so non-NestJS handlers are unaffected.
func jstsNestJSParamTaints(lines []string, b jstsFuncBody) map[string]taintInfo {
	if b.Start < 1 || b.Start > len(lines) {
		return map[string]taintInfo{}
	}
	open := strings.IndexByte(lines[b.Start-1], '(')
	if open < 0 {
		return map[string]taintInfo{}
	}
	sig := jstsCallArgs(lines, b.Start, open)
	if sig == "" || !strings.Contains(sig, "@") {
		return map[string]taintInfo{}
	}
	out := map[string]taintInfo{}
	for _, m := range dfJstsNestJSParamRe.FindAllStringSubmatch(sig, -1) {
		key, name := m[1], m[2]
		if name == "" {
			continue
		}
		out[name] = taintInfo{field: key, line: b.Start}
	}
	return out
}

// continueDataFlowJSTS continues a bounded hop walk inside this file: it
// binds the tainted value into fnName's paramIndex-th parameter and walks.
// hopsUsed is the number of hops already consumed reaching this file (so the
// continuation honours DataFlowMaxHops). The returned flows' Function /
// SourceField / SourceLine are placeholders; the links pass rewrites them to
// the true origin handler.
func continueDataFlowJSTS(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := jstsAllHeaders(content)
	bodies := jstsFuncBodies(content, headers)
	callee := jstsBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := jstsParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{} // ambiguous/destructured param — drop
	}
	ctx := jstsWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkJSTSBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// jstsFuncBody is a function's line span (1-indexed, inclusive).
type jstsFuncBody struct {
	Name  string
	Start int // line of the `{` opening (== header line in practice)
	End   int // line of the matching `}`
}

// jstsFuncBodies computes brace-balanced spans for each header. Conservative:
// a header whose body brace can't be balanced within the file is skipped.
func jstsFuncBodies(content string, headers []funcHeader) []jstsFuncBody {
	lines := strings.Split(content, "\n")
	var out []jstsFuncBody
	for _, h := range headers {
		// The (?m)^\s* header regex can place a function preceded by a blank
		// line one line early (the \s* eats the prior newline). Snap Start to
		// the line that actually contains the function's name token so param
		// reading and the brace scan are aligned.
		start := jstsSnapHeaderLine(lines, h.Line, h.Name)
		end := jstsMatchBraceEnd(lines, start)
		if end == 0 {
			continue
		}
		out = append(out, jstsFuncBody{Name: h.Name, Start: start, End: end})
	}
	return out
}

// jstsSnapHeaderLine returns the 1-indexed line at/after `line` whose text
// contains `name` followed (ignoring spaces) by `(` — the real header line.
// Falls back to `line` if not found within a small window.
func jstsSnapHeaderLine(lines []string, line int, name string) int {
	for i := line; i <= line+2 && i <= len(lines); i++ {
		s := lines[i-1]
		if idx := strings.Index(s, name); idx >= 0 {
			rest := strings.TrimLeft(s[idx+len(name):], " \t")
			if strings.HasPrefix(rest, "(") {
				return i
			}
		}
	}
	return line
}

// jstsMatchBraceEnd finds the line of the `}` that closes the first `{`
// at/after startLine. Returns 0 if unbalanced (drop the body). String and
// comment content is not parsed out — a tolerable imprecision for the
// scoped pass; an unbalanced count simply drops the function.
func jstsMatchBraceEnd(lines []string, startLine int) int {
	depth := 0
	seen := false
	for i := startLine - 1; i < len(lines); i++ {
		for _, r := range lines[i] {
			switch r {
			case '{':
				depth++
				seen = true
			case '}':
				depth--
				if seen && depth == 0 {
					return i + 1
				}
			}
		}
	}
	return 0
}

// jstsWalkCtx threads the bounded multi-hop walk's immutable-ish state.
// hopPath/visited are COPIED (not shared) on each descent so sibling
// branches do not pollute one another.
type jstsWalkCtx struct {
	origin   string // originating handler name (or placeholder, cross-file)
	field    string // carried source field provenance ("" if unknown)
	srcLine  int    // request-source line in the origin handler (0 cross-file)
	hopsUsed int    // hops already consumed reaching this body
	bodies   []jstsFuncBody
	lines    []string
	visited  map[string]bool // callee names on the active path (cycle guard)
	hopPath  []string        // ordered callee chain to this body
}

// walkJSTSBody is the unified forward pass over a function body. The taint
// map is pre-seeded (cross-file continuation) or empty (handler, which reads
// its request sources here). Returns reached sinks + cross-file boundaries.
func walkJSTSBody(ctx jstsWalkCtx, b jstsFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		jstsTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, jstsDirectSinks(ctx, ln, line, tainted)...)

		r := jstsFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// jstsTrackTaint applies one line's assignment effects to the taint map.
func jstsTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	if m := dfJstsBareAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if _, was := tainted[name]; was && !jstsRHSCarriesTaint(rhs, tainted) {
			delete(tainted, name)
		}
	}
	if m := dfJstsDestructureRe.FindStringSubmatch(line); m != nil {
		jstsTrackDestructure(tainted, m[1], m[2], ln)
		return
	}
	if m := dfJstsConstAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if fld, ok := jstsRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else {
			delete(tainted, name)
		}
	}
}

// jstsTrackDestructure seeds taint for an object-destructuring declaration
// `const { <bindings> } = <rhs>` when the rhs is a request source or a tainted
// whole-object root (NestJS `@Body() dto`). Each static binding becomes a new
// taint root whose field is the destructured property — `const {email} = dto`
// → `email` tainted with field "email"; `const {email: e} = dto` → `e` with
// field "email". When the root already carries a field of its own (e.g.
// `@Body('user') user` → `const {name} = user`), that field is preserved
// (the destructured prop is a sub-field we do not deepen — honest-partial).
// Rest/computed/nested elements are skipped per-element. A non-tainted rhs is
// a no-op (the bindings are simply untracked).
func jstsTrackDestructure(tainted map[string]taintInfo, bindings, rhs string, ln int) {
	rootField, ok := jstsRHSSourceField(rhs, tainted)
	if !ok {
		return
	}
	for _, raw := range strings.Split(bindings, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		em := dfJstsDestructEntryRe.FindStringSubmatch(entry)
		if em == nil {
			continue // rest / computed / nested — skip (honest-partial)
		}
		prop, rebind := em[1], em[2]
		local := prop
		if rebind != "" {
			local = rebind
		}
		field := rootField
		if field == "" {
			field = prop // whole-object root: the property IS the field
		}
		tainted[local] = taintInfo{field: field, line: ln}
	}
}

// jstsDirectSinks emits flows for sinks on `line` whose args carry taint.
func jstsDirectSinks(ctx jstsWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfJstsSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := line[m[2]:m[3]]
			args := jstsCallArgs(ctx.lines, ln, m[2])
			if fld, ok := jstsArgsTainted(args, tainted); ok {
				field := ctx.field
				if field == "" {
					field = fld
				}
				out = append(out, DataFlow{
					Function:    ctx.origin,
					SourceField: field,
					SourceLine:  ctx.srcLine,
					SinkKind:    s.kind,
					SinkName:    callee,
					SinkLine:    ln,
					HopVia:      firstOf(ctx.hopPath),
					HopPath:     dupStrings(ctx.hopPath),
				})
			}
		}
	}
	return out
}

// jstsFollowCalls handles each local-call on `line`: if the callee is a
// function defined in this file and the hop bound + cycle guards allow,
// recurse into it; otherwise (non-local callee) record a cross-file boundary
// candidate. Position binding is EXACT — a spread / ambiguous arg drops.
func jstsFollowCalls(ctx jstsWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range jstsLocalCalls(line) {
		// A spread token anywhere in the arg list makes positions unreliable.
		if jstsArgsHaveSpread(call.args) {
			continue
		}
		for pos, argExpr := range call.args {
			if _, ok := jstsExprTainted(argExpr, tainted); !ok {
				continue
			}
			// Require the arg to be EXACTLY the tainted value (not embedded in
			// a larger expression) so positional binding stays sound.
			fld, bare := jstsArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := jstsBodyByName(ctx.bodies, call.name)
			if callee == nil {
				// Non-local callee → cross-file boundary candidate (only with
				// hop budget remaining for the next hop).
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
			// Local hop. Guards: depth bound, cycle/recursion, param binding.
			if ctx.hopsUsed+len(ctx.hopPath) >= DataFlowMaxHops {
				continue // beyond bound — drop
			}
			if ctx.visited[callee.Name] {
				continue // recursion / cycle — stop, drop
			}
			param := jstsParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue // destructured / ambiguous param — drop
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkJSTSBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

type taintInfo struct {
	field string
	line  int
}

// dfJstsIdentMemberRe captures an identifier (group 1) immediately followed
// by a static member access (group 2): for `dto.email` it yields ("dto",
// "email"). Used to recover the request-input field for a tainted NestJS
// param whose decorator carried no literal key (`@Body() dto` → `dto.email`).
var dfJstsIdentMemberRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\.\s*([A-Za-z_$][\w$]*)`)

// jstsTaintedField resolves the source field for a reference to the tainted
// variable `name` (known field `info.field`) as it appears in `expr`. When
// the taint root carries no field of its own (`@Body() dto`, or a whole
// request object) and `expr` accesses a static member of it (`dto.email`),
// the member name is lifted as the field. The known decorator/source field
// always wins when present.
func jstsTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfJstsIdentMemberRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// jstsRHSSourceField returns (field, true) when rhs is a request-input read
// or a reference to a tainted variable. The field is the source field name
// (possibly ""), preserving provenance across the assignment.
func jstsRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if m := dfJstsSourceFieldRe.FindStringSubmatch(rhs); m != nil {
		return dfJstsFirstGroup(m), true
	}
	if dfJstsSourceAnyRe.MatchString(rhs) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return jstsTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// jstsRHSCarriesTaint reports whether rhs references a source or a tainted
// var (used to decide whether a reassignment preserves or breaks taint).
func jstsRHSCarriesTaint(rhs string, tainted map[string]taintInfo) bool {
	_, ok := jstsRHSSourceField(rhs, tainted)
	return ok
}

// jstsArgsTainted reports whether the argument text references a source
// directly or any tainted variable; returns the associated field.
func jstsArgsTainted(args string, tainted map[string]taintInfo) (string, bool) {
	return jstsExprTainted(args, tainted)
}

// jstsExprTainted reports whether expr references a request source directly
// or a tainted variable, returning the field name when known.
func jstsExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if m := dfJstsSourceFieldRe.FindStringSubmatch(expr); m != nil {
		return dfJstsFirstGroup(m), true
	}
	if dfJstsSourceAnyRe.MatchString(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return jstsTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// jstsArgBareTaint reports whether argExpr is EXACTLY a tainted reference (a
// request-source read or a bare tainted identifier), not embedded in a
// larger expression. This precision guard keeps positional binding sound:
// `helper(x)` / `helper(req.body.x)` bind; `helper(x + 1)` / `helper({x})` /
// `helper(f(x))` do NOT (drop — honest-partial). Returns the field.
func jstsArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if jstsWholeExprIsSource(e) {
		if m := dfJstsSourceFieldRe.FindStringSubmatch(e); m != nil {
			return dfJstsFirstGroup(m), true
		}
		return "", true
	}
	if dfReSimpleIdent.MatchString(e) {
		if info, ok := tainted[e]; ok {
			return info.field, true
		}
	}
	// A static member access off a tainted root (`dto.email`) is itself a
	// clean positional value: bind it and lift the member as the field when
	// the root carried none (NestJS `@Body() dto` → `dto.email`, #3902).
	if m := dfJstsTaintedMemberRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			return jstsTaintedField(e, m[1], info), true
		}
	}
	return "", false
}

// dfJstsTaintedMemberRe matches an expression that is SOLELY an identifier
// followed by a single static member access (`dto.email`), anchored so it is
// the whole expression (no surrounding operators). Group 1 = root identifier.
var dfJstsTaintedMemberRe = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\s*\.\s*[A-Za-z_$][\w$]*$`)

// jstsWholeExprIsSource reports the expr is SOLELY a request-source access
// (optionally a member chain) with no surrounding operators.
func jstsWholeExprIsSource(e string) bool {
	loc := dfJstsSourceFieldRe.FindStringIndex(e)
	if loc == nil {
		loc = dfJstsSourceAnyRe.FindStringIndex(e)
	}
	if loc == nil {
		return false
	}
	return strings.TrimSpace(e[:loc[0]]) == "" && strings.TrimSpace(e[loc[1]:]) == ""
}

// jstsArgsHaveSpread reports whether any argument is a spread (`...x`),
// which makes positional indices unreliable → the whole call is dropped.
func jstsArgsHaveSpread(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(strings.TrimSpace(a), "...") {
			return true
		}
	}
	return false
}

// jstsCallArgs returns the argument text of the call whose `(` begins at or
// after byte offset openByte on line ln, spanning until the matching `)`
// (possibly across lines). Returns the inner text.
func jstsCallArgs(lines []string, ln, openByte int) string {
	var sb strings.Builder
	depth := 0
	started := false
	for i := ln - 1; i < len(lines); i++ {
		s := lines[i]
		start := 0
		if i == ln-1 {
			idx := strings.IndexByte(s[min(openByte, len(s)):], '(')
			if idx < 0 {
				return ""
			}
			start = min(openByte, len(s)) + idx
		}
		for j := start; j < len(s); j++ {
			c := s[j]
			if c == '(' {
				depth++
				if depth == 1 {
					started = true
					continue
				}
			} else if c == ')' {
				depth--
				if depth == 0 {
					return sb.String()
				}
			}
			if started {
				sb.WriteByte(c)
			}
		}
		sb.WriteByte(' ')
		if i-(ln-1) > 40 { // bound the scan
			break
		}
	}
	return sb.String()
}

// jstsLocalCall is a parsed `name(arg0, arg1, ...)` call.
type jstsLocalCall struct {
	name string
	args []string
}

// dfJstsLocalCallRe matches a call to a bare identifier (potential local fn).
var dfJstsLocalCallRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\(`)

// jstsLocalCalls extracts candidate bare-identifier function calls on a line
// with their top-level positional argument expressions. A method call
// (`x.foo(`) is skipped — only bare-identifier calls are hop/boundary
// candidates (a method call cannot be resolved positionally here).
func jstsLocalCalls(line string) []jstsLocalCall {
	var out []jstsLocalCall
	for _, m := range dfJstsLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue // method call — not a bare-ident call
			}
		}
		if jstsControlKeyword(name) || name == "require" {
			continue
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, jstsLocalCall{name: name, args: args})
	}
	return out
}

// jstsSplitArgs splits top-level comma-separated arguments.
func jstsSplitArgs(s string) []string {
	var out []string
	depth := 0
	cur := strings.Builder{}
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// jstsParamName returns the name of the pos-th positional parameter of the
// function whose header is on headerLine. Destructured params return ""
// (no binding, honest-partial). A rest param (`...rest`) anywhere makes
// positional binding ambiguous → "".
func jstsParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	line := lines[headerLine-1]
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return ""
	}
	close := strings.IndexByte(line[open:], ')')
	if close < 0 {
		return ""
	}
	params := jstsSplitArgs(line[open+1 : open+close])
	if pos >= len(params) {
		return ""
	}
	for _, p := range params {
		if strings.HasPrefix(strings.TrimSpace(p), "...") {
			return "" // rest param → ambiguous positions
		}
	}
	p := strings.TrimSpace(params[pos])
	if i := strings.IndexAny(p, ":="); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	if !dfReSimpleIdent.MatchString(p) {
		return "" // destructured / complex — drop
	}
	return p
}

// jstsBodyByName returns the body with the given name, or nil.
func jstsBodyByName(all []jstsFuncBody, name string) *jstsFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}

var dfReSimpleIdent = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// dfReWholeIdent builds a whole-word matcher for a specific identifier.
func dfReWholeIdent(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

// firstOf returns the first element of a slice, or "".
func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// dupStrings returns a copy of s (so descents don't alias the parent path).
func dupStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// dupVisited returns a shallow copy of the visited set.
func dupVisited(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func dfMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
