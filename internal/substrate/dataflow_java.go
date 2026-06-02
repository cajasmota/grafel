// Java request-input → sink dataflow sniffer (#3628 area #22, epic #3872, #3958).
//
// SCOPED def→use tracking inside one method body, followed through up to
// DataFlowMaxHops local (same-file) method-call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an imported / external
// callee. See dataflow.go for the contract and the honest-partial boundary.
// Mirrors dataflow_python.go / dataflow_jsts.go / dataflow_golang.go /
// dataflow_ruby.go exactly: same DataFlow/source/sink model, same
// DATA_FLOWS_TO emission, same precision-over-recall discipline.
//
// Sources recognised — Spring MVC / WebFlux controller-method PARAMETERS
// annotated with a request-binding annotation (the value is request-derived
// the moment the handler is entered, exactly as `@Body() dto` is in NestJS):
//   - @RequestBody UserDto dto          → root `dto` tainted, field "" (the
//     field is recovered from a later `dto.getEmail()` / `dto.email` access)
//   - @RequestParam("q") String q       → root `q` tainted, field "q"
//   - @PathVariable("id") Long id        → root `id` tainted, field "id"
//   - @RequestHeader("X-Tok") String t   → root `t` tainted, field "X-Tok"
//   - @RequestPart / @ModelAttribute / @CookieValue (same shape)
//
// The annotation's literal value (`@RequestParam("q")`) is the source field.
// When no literal is given (`@RequestParam String q` — Spring infers the name
// from the parameter), the parameter NAME is used as the field. A whole-object
// `@RequestBody dto` carries field "" until a static member / getter access
// derives the concrete field (`dto.getEmail()` → "email"; `dto.email` →
// "email").
//
// Propagation: a local assigned from a tainted value
// (`String e = dto.getEmail();`) carries taint; a direct pass-through into a
// sink flows; a pass into a local method binds positionally and continues the
// bounded walk (≤ DataFlowMaxHops), recording the callee chain in HopPath.
//
// Sinks recognised:
//   - db_write : <repo>.save / saveAll / saveAndFlush / delete* / insert /
//     entityManager.persist / merge / remove, jdbcTemplate.update /
//     batchUpdate / execute (JPA / Spring Data / JDBC), when an argument
//     carries taint.
//   - response : ResponseEntity.ok( / status(...).body( / new ResponseEntity(,
//     ServerResponse.ok().bodyValue( (WebFlux), and `return <tainted>;` from a
//     handler whose declared body is the tainted value.
//   - http_call: restTemplate.postForObject / postForEntity / exchange / put,
//     webClient...bodyValue( (outbound, with a tainted body) — a CONSUMES_API
//     site.
//
// HONEST-PARTIAL (precision over recall): dynamic member access, whole-object
// flows (field=""), ambiguous/varargs positions, reassignment that breaks the
// chain, >DataFlowMaxHops depth, and recursion are DROPPED or flow with
// field="", never fabricated.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("java", sniffDataFlowJavaEx, continueDataFlowJava)
}

// sniffDataFlowJava preserves the legacy in-file-only entry point.
func sniffDataFlowJava(content string) []DataFlow { return sniffDataFlowJavaEx(content).Flows }

// dfJavaRequestParamRe matches ONE Spring request-binding parameter inside a
// method signature: a binding annotation (group 1 = annotation name), an
// optional string-literal value (group 2 = the request-input field name), the
// parameter TYPE token (skipped), and the bound parameter identifier (group 3
// = the tainted root). The annotation's option list is tolerated non-greedily
// so `@RequestParam(value="q", required=false) String q` still binds `q` to
// field "" (no bare positional literal) — the param-name fallback then applies.
//
//	@RequestBody UserDto dto              → g1=RequestBody g2=""  g3=dto
//	@RequestParam("q") String q           → g1=RequestParam g2=q g3=q
//	@PathVariable("id") Long id            → g1=PathVariable g2=id g3=id
//	@RequestHeader(name="X") String x      → g1=RequestHeader g2="" g3=x
//	@Valid @RequestBody UserDto dto        → leading @Valid tolerated
var dfJavaRequestParamRe = regexp.MustCompile(
	`@(RequestBody|RequestParam|PathVariable|RequestHeader|RequestPart|ModelAttribute|CookieValue)\b` +
		`(?:\s*\(\s*(?:(?:value|name)\s*=\s*)?(?:"([^"]*)")?[^)]*\))?` +
		`\s+(?:final\s+)?` +
		`(?:@[A-Za-z_$][\w$]*(?:\([^)]*\))?\s+)*` +
		`[A-Za-z_$][\w$<>\[\],.?\s]*?\s+` +
		`([A-Za-z_$][\w$]*)\b`,
)

// dfJavaAnyRequestAnnotRe is a cheap presence check for any request-binding
// annotation in a signature, used to confirm a method is a Spring handler.
var dfJavaAnyRequestAnnotRe = regexp.MustCompile(
	`@(?:RequestBody|RequestParam|PathVariable|RequestHeader|RequestPart|ModelAttribute|CookieValue)\b`,
)

// dfJavaDBWriteRe matches a JPA / Spring Data / JDBC write. Group 1 = callee.
// Mirrors effect_sinks_java.go javaDBWriteRe (write subset).
var dfJavaDBWriteRe = regexp.MustCompile(
	`\b((?:entityManager|em)\s*\.\s*(?:persist|merge|remove|refresh))\s*\(` +
		`|\b([A-Za-z_$][\w$]*\s*\.\s*(?:save|saveAll|saveAndFlush|delete|deleteAll|deleteById|deleteInBatch|insert))\s*\(` +
		`|\b((?:jdbcTemplate|namedJdbcTemplate|namedParameterJdbcTemplate)\s*\.\s*(?:update|batchUpdate|execute))\s*\(`,
)

// dfJavaRespRe matches a Spring MVC / WebFlux response-body emission whose
// argument carries taint. Group 1 = callee text. `return <tainted>;` is
// handled separately (dfJavaReturnRe) so a bare returned value is captured.
var dfJavaRespRe = regexp.MustCompile(
	`\b(ResponseEntity\s*\.\s*ok)\s*\(` +
		`|\b(ResponseEntity\s*\.\s*(?:status|created|accepted|badRequest)\s*\([^)]*\)\s*\.\s*body)\s*\(` +
		`|\b(new\s+ResponseEntity)\s*\(` +
		`|\b(ServerResponse\s*\.\s*ok\s*\(\s*\)\s*\.\s*(?:bodyValue|body))\s*\(`,
)

// dfJavaHTTPCallRe matches an outbound HTTP call carrying a tainted body.
// Group 1 = callee. Mirrors effect_sinks_java.go javaHTTPRe (write verbs).
var dfJavaHTTPCallRe = regexp.MustCompile(
	`\b((?:restTemplate|webClient|client)\s*\.\s*(?:postForObject|postForEntity|put|patch|exchange|execute|bodyValue))\s*\(`,
)

// dfJavaReturnRe captures a `return <expr>;` statement (group 1 = the returned
// expression). A handler that returns a tainted value emits a response sink.
var dfJavaReturnRe = regexp.MustCompile(
	`^\s*return\s+(.+?)\s*;\s*$`,
)

// dfJavaDeclAssignRe captures a typed local declaration with initialiser
// `Type name = <rhs>;` (group 1 = name, group 2 = rhs). The leading type token
// is required (Java locals are declared); a bare `name = rhs` reassignment is
// matched by dfJavaBareAssignRe. `final` and generics in the type are
// tolerated. The `[^=]` guards against matching `==`.
var dfJavaDeclAssignRe = regexp.MustCompile(
	`^\s*(?:final\s+)?[A-Za-z_$][\w$<>\[\],.?\s]*?\s+([A-Za-z_$][\w$]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfJavaBareAssignRe captures `name = <rhs>` with no declared type (a
// reassignment), used to break taint when a tainted local is overwritten.
var dfJavaBareAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_$][\w$]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfJavaSinkSpecs is the ordered sink table reused at every scan depth.
var dfJavaSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfJavaDBWriteRe, DataFlowSinkDBWrite},
	{dfJavaRespRe, DataFlowSinkResponse},
	{dfJavaHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowJavaEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := javaFuncBodies(content, lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := javaWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		// Seed Spring request-binding params as request-derived roots so a
		// `@RequestBody dto` / `@RequestParam("q") String q` parameter is
		// tainted on handler entry (mirrors the NestJS decorator-param seed).
		seed := javaRequestParamTaints(lines, b)
		r := walkJavaBody(ctx, b, seed)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowJava continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks. The
// returned flows' Function/SourceField/SourceLine are placeholders the links
// pass rewrites to the true origin handler.
func continueDataFlowJava(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := javaFuncBodies(content, lines)
	callee := javaBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := javaParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := javaWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkJavaBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// javaFuncBody is a method's line span (1-indexed, inclusive).
type javaFuncBody struct {
	Name  string
	Start int // header line (the line carrying `name(`)
	End   int // line of the matching `}`
}

// javaFuncBodies computes brace-balanced spans for every method/constructor
// header in the file. It merges the shared scanJavaFuncHeaders set (which the
// other Java T1 sniffers use) with Spring controller handlers whose multi-line
// / annotated parameter lists the shared single-line regex can miss, then
// snaps each header to the line carrying the name token and balances braces
// with the shared jstsMatchBraceEnd. De-duplicated by start line.
func javaFuncBodies(content string, lines []string) []javaFuncBody {
	seen := map[int]bool{}
	var out []javaFuncBody
	addHeader := func(name string, headerLine int) {
		start := javaSnapHeaderLine(lines, headerLine, name)
		if seen[start] {
			return
		}
		end := jstsMatchBraceEnd(lines, start)
		if end == 0 {
			return
		}
		seen[start] = true
		out = append(out, javaFuncBody{Name: name, Start: start, End: end})
	}
	for _, h := range scanJavaFuncHeaders(content) {
		addHeader(h.Name, h.Line)
	}
	for _, h := range javaSpringHandlerHeaders(content, lines) {
		addHeader(h.Name, h.Line)
	}
	return out
}

// dfJavaMethodNameRe matches the `NAME(` token that opens a method parameter
// list at, or shortly after, the start of a line. Used to recover Spring
// handler headers whose annotated/multi-line signatures the shared
// javaMethodHeaderRe (which stops at the first inner `)`) can miss.
var dfJavaMethodNameRe = regexp.MustCompile(`(?m)([A-Za-z_$][\w$]*)\s*\(`)

// javaSpringHandlerHeaders scans for controller-method headers whose parameter
// list declares a Spring request-binding annotation. These are the dataflow
// handlers; non-handler methods are unaffected (the param block must contain a
// request annotation AND the signature must reach an opening `{`).
func javaSpringHandlerHeaders(content string, lines []string) []funcHeader {
	var out []funcHeader
	for _, m := range dfJavaMethodNameRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		if name == "" || javaControlKeyword(name) {
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
		if !dfJavaAnyRequestAnnotRe.MatchString(sig) {
			continue
		}
		out = append(out, funcHeader{Line: line, Name: name})
	}
	return out
}

// javaSnapHeaderLine returns the 1-indexed line at/after `line` whose text
// contains `name` immediately followed (ignoring spaces) by `(` — the real
// header line. Falls back to `line` if not found in a small window.
func javaSnapHeaderLine(lines []string, line int, name string) int {
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

// javaWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type javaWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []javaFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkJavaBody is the unified forward pass over a method body. The taint map is
// pre-seeded (request params, or a cross-file continuation) or empty.
func walkJavaBody(ctx javaWalkCtx, b javaFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	// Skip the header line itself (its annotations/params are the seed, not a
	// statement) so a `@RequestParam` literal isn't mistaken for a sink arg.
	for ln := b.Start + 1; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		javaTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, javaDirectSinks(ctx, ln, line, tainted)...)

		r := javaFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// javaTrackTaint applies one line's declaration/assignment effects to the
// taint map (last-write-wins).
func javaTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	if m := dfJavaDeclAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if name == "return" { // `return x;` is not a declaration
			return
		}
		if fld, ok := javaRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else if _, was := tainted[name]; was {
			delete(tainted, name) // declared/initialised to non-source → drop
		}
		return
	}
	if m := dfJavaBareAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if fld, ok := javaRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else if _, was := tainted[name]; was {
			delete(tainted, name) // reassigned to non-source → drop taint
		}
	}
}

// javaRHSSourceField returns (field, true) when rhs derives from a tainted
// value (a reference to a tainted root, optionally via a getter / member),
// preserving provenance.
func javaRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return javaTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfJavaGetterRe captures `<root>.getXxx()` — group 1 = root, group 2 = the
// getter's property (`getEmail` → email after lower-casing the first letter).
var dfJavaGetterRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\.\s*get([A-Z][\w$]*)\s*\(`)

// dfJavaMemberRe captures `<root>.field` — group 1 = root, group 2 = field
// (direct field access, e.g. a public field or a record component `dto.email`).
var dfJavaMemberRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\.\s*([A-Za-z_$][\w$]*)\b`)

// javaTaintedField resolves the source field for a reference to tainted root
// `name` as it appears in expr. A known field (a @RequestParam("q") literal or
// param-name) always wins. Otherwise, when expr accesses a getter
// (`dto.getEmail()`) or a direct field (`dto.email`) off the root, that member
// is lifted as the field — getter `getEmail` → "email" (first letter
// lower-cased), direct member `email` → "email".
func javaTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfJavaGetterRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return javaDecapitalize(m[2])
		}
	}
	for _, m := range dfJavaMemberRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			// Skip a getter spelled `getX` that the getter regex already saw;
			// here m[2] is the literal member token (`get` would be wrong).
			if strings.HasPrefix(m[2], "get") && len(m[2]) > 3 && m[2][3] >= 'A' && m[2][3] <= 'Z' {
				continue
			}
			return m[2]
		}
	}
	return ""
}

// javaDecapitalize lower-cases the first letter of a getter property name
// (`Email` → "email"). A leading run of capitals (an acronym like `URL`) is
// left as-is per the JavaBeans spec, but the common single-cap case is handled.
func javaDecapitalize(s string) string {
	if s == "" {
		return s
	}
	if len(s) >= 2 && s[1] >= 'A' && s[1] <= 'Z' {
		return s // `URL` → URL (acronym), per JavaBeans Introspector
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// javaDirectSinks emits flows for sinks on `line` whose args carry taint,
// including a `return <tainted>;` response.
func javaDirectSinks(ctx javaWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfJavaSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := javaSinkCallee(line, m)
			if callee == "" {
				continue
			}
			open := strings.Index(line[m[0]:], "(")
			if open < 0 {
				continue
			}
			args := jstsCallArgs(ctx.lines, ln, m[0]+open)
			if fld, ok := javaExprTainted(args, tainted); ok {
				out = append(out, javaMakeFlow(ctx, fld, s.kind, normalizeJavaCallee(callee), ln))
			}
		}
	}
	// `return <tainted>;` from a handler is a response-body emission.
	if m := dfJavaReturnRe.FindStringSubmatch(line); m != nil {
		if fld, ok := javaArgBareTaint(m[1], tainted); ok {
			out = append(out, javaMakeFlow(ctx, fld, DataFlowSinkResponse, "return", ln))
		}
	}
	return out
}

// javaMakeFlow builds a DataFlow, preferring an already-carried field (from a
// cross-file continuation) over the locally-derived one.
func javaMakeFlow(ctx javaWalkCtx, fld string, kind DataFlowSinkKind, sink string, ln int) DataFlow {
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

// javaSinkCallee returns the non-empty captured callee text from a sink match
// (the sink regexes use alternation groups; exactly one is set per match).
func javaSinkCallee(line string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 && m[i+1] >= 0 {
			return line[m[i]:m[i+1]]
		}
	}
	return ""
}

// normalizeJavaCallee collapses internal whitespace around `.` so a sink
// rendered across spacing reads canonically (`repo . save` → `repo.save`).
func normalizeJavaCallee(s string) string {
	s = strings.TrimSpace(s)
	return dfJavaDotSpaceRe.ReplaceAllString(s, ".")
}

var dfJavaDotSpaceRe = regexp.MustCompile(`\s*\.\s*`)

// javaFollowCalls handles each local-call on `line`: recurse into a same-file
// method (bounded + cycle-guarded) or record a cross-file boundary. Position
// binding is EXACT — an ambiguous/varargs arg drops (honest-partial).
func javaFollowCalls(ctx javaWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range javaLocalCalls(line) {
		for pos, argExpr := range call.args {
			fld, bare := javaArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := javaBodyByName(ctx.bodies, call.name)
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
			param := javaParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkJavaBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// javaExprTainted reports whether expr references a tainted variable, returning
// the field when derivable.
func javaExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return javaTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// dfJavaTaintedGetterWholeRe matches an expr that is SOLELY `ident.getXxx()`.
// Group 1 = root, group 2 = property.
var dfJavaTaintedGetterWholeRe = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\s*\.\s*get([A-Z][\w$]*)\s*\(\s*\)$`)

// dfJavaTaintedMemberWholeRe matches an expr that is SOLELY `ident.member`.
// Group 1 = root, group 2 = member.
var dfJavaTaintedMemberWholeRe = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\s*\.\s*([A-Za-z_$][\w$]*)$`)

// dfJavaNewObjRe matches a constructor argument `new Type(...)` so a tainted
// value wrapped in an entity construction (`repo.save(new User(dto.getEmail()))`)
// is recognised as a tainted sink argument and the field is lifted.
var dfJavaNewObjRe = regexp.MustCompile(`^new\s+[A-Za-z_$][\w$<>\[\].]*\s*\(`)

// javaArgBareTaint reports whether argExpr is EXACTLY a tainted value — a bare
// tainted identifier, a getter/member access off a tainted root
// (`dto.getEmail()` / `dto.email`), or a constructor/whole-object wrapping a
// tainted leaf (`new User(dto.getEmail())`) — not embedded in arithmetic or a
// nested non-constructor call. Precision guard for sound positional binding.
func javaArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if e == "" {
		return "", false
	}
	// A constructor call wrapping a tainted value (`new User(dto.getEmail())`)
	// is a real tainted sink argument — bind the tainted leaf and lift its
	// field. Inspect the constructor's own arguments.
	if dfJavaNewObjRe.MatchString(e) {
		open := strings.IndexByte(e, '(')
		if open >= 0 {
			inner := jstsCallArgs([]string{e}, 1, open)
			for _, part := range jstsSplitArgs(inner) {
				if fld, ok := javaArgBareTaint(part, tainted); ok {
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
	// `dto.getEmail()` — getter off a tainted root.
	if m := dfJavaTaintedGetterWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return javaDecapitalize(m[2]), true
		}
		return "", false
	}
	// `dto.email` — direct member off a tainted root.
	if m := dfJavaTaintedMemberWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// javaLocalCall is a parsed `name(arg0, arg1, …)` call to a bare identifier.
type javaLocalCall struct {
	name string
	args []string
}

// dfJavaLocalCallRe matches a call to a bare identifier (potential local fn).
var dfJavaLocalCallRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\(`)

// javaLocalCalls extracts candidate bare-identifier method calls on a line with
// their top-level positional argument expressions. Method calls (`obj.foo(`),
// `new Type(`, and control keywords are skipped — only a same-class
// bare-identifier call is a hop / boundary candidate.
func javaLocalCalls(line string) []javaLocalCall {
	var out []javaLocalCall
	for _, m := range dfJavaLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue // method call — not a bare-ident call
			}
			if strings.HasSuffix(prev, "new") {
				continue // constructor — handled as a sink arg, not a hop
			}
		}
		if javaControlKeyword(name) {
			continue
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, javaLocalCall{name: name, args: args})
	}
	return out
}

// javaRequestParamTaints returns the taint seed for a handler whose signature
// declares Spring request-binding parameters. Each bound parameter identifier
// becomes a tainted root; the field is the annotation's literal value
// (`@RequestParam("q")` → "q"), else the parameter NAME for a scalar binder
// (`@RequestParam String q` → "q"), else "" for a whole-object `@RequestBody`
// (recovered later from a getter/member). Returns an empty map for a
// non-handler method, so plain methods are unaffected.
func javaRequestParamTaints(lines []string, b javaFuncBody) map[string]taintInfo {
	out := map[string]taintInfo{}
	if b.Start < 1 || b.Start > len(lines) {
		return out
	}
	open := strings.IndexByte(lines[b.Start-1], '(')
	if open < 0 {
		return out
	}
	sig := jstsCallArgs(lines, b.Start, open)
	if sig == "" || !strings.Contains(sig, "@") {
		return out
	}
	for _, m := range dfJavaRequestParamRe.FindAllStringSubmatch(sig, -1) {
		annot, literal, name := m[1], m[2], m[3]
		if name == "" {
			continue
		}
		field := literal
		// A scalar binder (param / path / header / cookie) with no literal
		// infers the request key from the parameter name; @RequestBody /
		// @ModelAttribute bind a whole object whose field is derived later.
		if field == "" && annot != "RequestBody" && annot != "ModelAttribute" {
			field = name
		}
		out[name] = taintInfo{field: field, line: b.Start}
	}
	return out
}

// javaParamName returns the name of the pos-th positional parameter of the
// method whose header is on headerLine. Spring parameters may carry leading
// annotations (`@RequestParam("q") String q`) and generic/array types; the
// last identifier token before a `,` or `)` is the parameter name. A varargs
// parameter (`Type... xs`) makes positions past it unreliable → "".
func javaParamName(lines []string, headerLine, pos int) string {
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
		if strings.Contains(p, "...") {
			return "" // varargs — ambiguous positions
		}
	}
	return javaParamIdent(params[pos])
}

// dfJavaParamTrailingIdentRe captures the trailing identifier of a parameter
// declaration — the parameter name — after any annotations and the type. It
// anchors at end so a generic type's inner identifiers are not mistaken.
var dfJavaParamTrailingIdentRe = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*$`)

// javaParamIdent extracts the parameter name from one parameter declaration,
// stripping any annotation prefixes and the (possibly generic) type token.
func javaParamIdent(decl string) string {
	d := strings.TrimSpace(decl)
	if d == "" {
		return ""
	}
	m := dfJavaParamTrailingIdentRe.FindStringSubmatch(d)
	if m == nil {
		return ""
	}
	return m[1]
}

// javaBodyByName returns the body with the given name, or nil.
func javaBodyByName(all []javaFuncBody, name string) *javaFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
