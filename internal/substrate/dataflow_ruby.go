// Ruby request-input → sink dataflow sniffer (#3628 area #22, epic #3872).
//
// SCOPED def→use tracking inside one method body (`def … end`), followed
// through up to DataFlowMaxHops local (same-file) method-call hops, PLUS
// cross-file boundary emission for a tainted value that escapes into an
// imported / external callee. See dataflow.go for the contract and the
// honest-partial boundary. Mirrors dataflow_python.go / dataflow_jsts.go.
//
// Sources recognised (static key only):
//   - Rails / Sinatra / Grape : params[:x] / params['x'] / params.fetch(:x)
//   - Rails strong params      : params.require(:user).permit(:name, :email)
//     (each permitted attribute is a source field)
//   - request.body / request.body.read                 (raw body, field "")
//
// Strong-params binding: a local assigned from a `.permit(...)` chain
//
//	(`user_params = params.require(:user).permit(:name)`) is tainted, and a
//	later member read `user_params[:name]` recovers the field. A whole-hash
//	mass-assignment of such a var (`User.create(user_params)`) flows with an
//	empty field (honest-partial — the individual attribute is not derivable
//	at the call site).
//
// Sinks recognised:
//   - db_write : <Model>.create( / .update( / .save / <Model>.new(…).save /
//     ActiveRecord::Base.connection.execute( (raw SQL)
//   - response : render json: / render plain: / render(…)
//   - http_call: Net::HTTP.* / Faraday.* / HTTParty.* / RestClient.* with a
//     tainted argument (outbound body)
//
// Honest-partial: dynamic keys (`params[k]`), whole-hash mass-assignment of
// the raw `params` hash, ambiguous splat args, >DataFlowMaxHops depth and
// recursion are DROPPED (or flow with field=""), never fabricated.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("ruby", sniffDataFlowRubyEx, continueDataFlowRuby)
}

// sniffDataFlowRuby preserves the legacy in-file-only entry point.
func sniffDataFlowRuby(content string) []DataFlow { return sniffDataFlowRubyEx(content).Flows }

// dfRbSourceFieldRe captures a request-input read with a STATIC symbol/string
// key. Groups (in order) hold the key for the various access forms. Dynamic
// keys (`params[k]`) do not match (honest-partial).
//
//	params[:x] / params['x'] / params["x"]              → group 1
//	params.fetch(:x) / params.fetch('x')                → group 2
//	params.require(:m).permit(... :x ...)               → handled separately
var dfRbSourceFieldRe = regexp.MustCompile(
	`\bparams\s*\[\s*(?::([A-Za-z_][\w]*)|['"]([A-Za-z_][\w]*)['"])\s*\]` +
		`|\bparams\s*\.\s*fetch\s*\(\s*(?::([A-Za-z_][\w]*)|['"]([A-Za-z_][\w]*)['"])`,
)

// dfRbSourceAnyRe matches a source receiver without requiring a static key,
// for whole-object pass-through (`render json: params`, `request.body`).
var dfRbSourceAnyRe = regexp.MustCompile(
	`\bparams\b|\brequest\s*\.\s*body\b`,
)

// dfRbDynamicIndexRe matches a DYNAMIC-key params access `params[expr]` whose
// key is NOT a static symbol/string literal (e.g. `params[k]`,
// `params[idx + 1]`). Such an access is honest-partial: the field is not
// statically derivable, so it must NOT be treated as a source (dropped),
// even though the bare `params` token would otherwise match dfRbSourceAnyRe.
var dfRbDynamicIndexRe = regexp.MustCompile(`\bparams\s*\[\s*(?:[^:'"\]\s]|\s)`)

// rbAnySourceWhole reports whether expr is a whole-object source use
// (`params` / `request.body`) that is NOT merely a dynamic-key index access.
// A dynamic-key `params[k]` is honest-partial and must be dropped.
func rbAnySourceWhole(expr string) bool {
	if !dfRbSourceAnyRe.MatchString(expr) {
		return false
	}
	// If the ONLY params usage is a dynamic index and there is no static-key
	// or permit source and no request.body, it's not a usable source.
	if dfRbDynamicIndexRe.MatchString(expr) &&
		!dfRbSourceFieldRe.MatchString(expr) &&
		!dfRbPermitRe.MatchString(expr) &&
		!dfRbRequestBodyRe.MatchString(expr) &&
		!dfRbWholeParamsRe.MatchString(expr) {
		return false
	}
	return true
}

// dfRbRequestBodyRe matches a raw `request.body` read.
var dfRbRequestBodyRe = regexp.MustCompile(`\brequest\s*\.\s*body\b`)

// dfRbWholeParamsRe matches a `params` use that is a whole-hash reference
// (not immediately followed by a `[` index or a `.` member chain that would
// be a dynamic access) — e.g. `render json: params`, `User.create(params)`.
var dfRbWholeParamsRe = regexp.MustCompile(`\bparams\b\s*(?:[^[.\w]|$)`)

// dfRbPermitRe matches a strong-params chain
// `params.require(:model).permit(:a, :b, ...)` OR a bare
// `params.permit(:a, :b)`. The whole match is the source; permitted fields
// are extracted from the permit(...) argument list separately.
var dfRbPermitRe = regexp.MustCompile(
	`\bparams\s*\.\s*(?:require\s*\(\s*:?['"]?[A-Za-z_][\w]*['"]?\s*\)\s*\.\s*)?permit\s*\(`,
)

// dfRbDBWriteRe matches an ActiveRecord write. Group 1 = the callee text.
var dfRbDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_][\w:]*\s*\.\s*(?:create|create!|update|update!|update_all|insert|insert_all|upsert|save|save!))\s*[\(!]` +
		`|\b([A-Za-z_][\w:]*\s*\.\s*new)\s*\(`,
)

// dfRbRawSQLRe matches a raw SQL execute. Group 1 = callee.
var dfRbRawSQLRe = regexp.MustCompile(
	`\b((?:ActiveRecord::Base\s*\.\s*connection|[A-Za-z_][\w]*\s*\.\s*connection)\s*\.\s*(?:execute|exec_query|exec_update|exec_insert|exec_delete))\s*\(`,
)

// dfRbRespRe matches a Rails/Sinatra response emission. Group 1 = callee.
var dfRbRespRe = regexp.MustCompile(
	`\b(render|redirect_to|send_data|send_file)\b`,
)

// dfRbHTTPCallRe matches an outbound HTTP call. Group 1 = callee.
var dfRbHTTPCallRe = regexp.MustCompile(
	`\b((?:Net::HTTP|Faraday|HTTParty|RestClient|Excon)\s*\.\s*(?:get|post|put|patch|delete|head|request|new))\s*[\(.]`,
)

// dfRbAssignRe captures `name = <rhs>` (group 1 name, 2 rhs). Excludes
// `==`/`<=`/augmented by requiring a non-`=` first rhs char.
var dfRbAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_][\w]*)\s*=\s*([^=].*)$`,
)

// dfRbSinkSpecs is the ordered sink table reused at every scan depth.
var dfRbSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfRbDBWriteRe, DataFlowSinkDBWrite},
	{dfRbRawSQLRe, DataFlowSinkDBWrite},
	{dfRbRespRe, DataFlowSinkResponse},
	{dfRbHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowRubyEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := rubyFuncBodies(lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := rbWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		r := walkRbBody(ctx, b, map[string]taintInfo{})
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowRuby continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks.
// Function/SourceField/SourceLine on returned flows are placeholders that the
// links pass rewrites to the true origin handler.
func continueDataFlowRuby(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := rubyFuncBodies(lines)
	callee := rbBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := rubyParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := rbWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkRbBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// rubyFuncBody is a method's line span (1-indexed, inclusive).
type rubyFuncBody struct {
	Name  string
	Start int // the `def` line
	End   int // the matching `end` line
}

// dfRbBlockOpenRe matches a leading block-opening keyword on a line (after
// optional whitespace). These require a matching `end`. `do`/`{` blocks are
// approximated via the do/brace handling in rubyFuncBodies.
var dfRbBlockOpenRe = regexp.MustCompile(
	`^\s*(?:def|class|module|if|unless|while|until|for|case|begin)\b`,
)

// dfRbPostfixRe matches a line that ENDS with a modifier keyword usage but is
// not a block opener — i.e. a postfix `... if cond` / `... unless cond` /
// `... while cond`. Such lines must NOT increment block depth.
var dfRbPostfixRe = regexp.MustCompile(
	`\S.*\b(?:if|unless|while|until)\b\s+\S+\s*$`,
)

// dfRbDoRe matches a trailing `do` (optionally with block params) that opens a
// block needing `end` (`each do |x|`).
var dfRbDoRe = regexp.MustCompile(`\bdo\b(\s*\|[^|]*\|)?\s*$`)

// dfRbEndRe matches a line that is exactly `end` (possibly indented, possibly
// with a trailing modifier such as `end if x` — rare; treated as a closer).
var dfRbEndRe = regexp.MustCompile(`^\s*end\b`)

// rubyFuncBodies computes def→end spans by scanning the file and tracking
// keyword block depth. Each `def` opens a span closed at its matching `end`.
// Nested classes/modules/conditionals inside the method are accounted for so
// the matching `end` is found correctly. Single-line `def x; …; end` is
// handled by detecting the trailing `end` on the same line.
func rubyFuncBodies(lines []string) []rubyFuncBody {
	var out []rubyFuncBody
	// Stack of open `def` records awaiting their `end`. depth is the block
	// nesting level (relative to file start) at which the def was opened.
	type openDef struct {
		name  string
		start int
		depth int
	}
	var stack []openDef
	depth := 0

	for i, raw := range lines {
		ln := i + 1
		line := stripRubyLineNoise(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect a method header on this line.
		if m := rubyFuncHeaderRe.FindStringSubmatch(line); m != nil {
			// Single-line def with a trailing `end` (`def x; ...; end`).
			if strings.Contains(line, ";") && dfRbEndRe.MatchString(reverseTailToken(line)) {
				out = append(out, rubyFuncBody{Name: m[1], Start: ln, End: ln})
				continue
			}
			stack = append(stack, openDef{name: m[1], start: ln, depth: depth})
			depth++
			continue
		}

		// Count `end` tokens that close a block (handle multiple `end` on a
		// line conservatively as one — Ruby rarely stacks `end`s on a line).
		if dfRbEndRe.MatchString(line) {
			depth--
			if depth < 0 {
				depth = 0
			}
			// Close any def whose opening depth equals the current depth.
			for len(stack) > 0 && stack[len(stack)-1].depth == depth {
				od := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				out = append(out, rubyFuncBody{Name: od.name, Start: od.start, End: ln})
			}
			continue
		}

		// Block-opening keyword (not a postfix modifier).
		if dfRbBlockOpenRe.MatchString(line) && !dfRbPostfixRe.MatchString(line) {
			depth++
			continue
		}
		// Trailing `do` opens a block.
		if dfRbDoRe.MatchString(line) {
			depth++
			continue
		}
	}
	// Flush any unterminated defs to EOF (defensive — malformed input).
	for _, od := range stack {
		out = append(out, rubyFuncBody{Name: od.name, Start: od.start, End: len(lines)})
	}
	return out
}

// stripRubyLineNoise removes a trailing `# comment` (not inside a string, best
// effort) so block-keyword detection isn't fooled by comments.
func stripRubyLineNoise(line string) string {
	inS, inD := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '\'':
			if !inD {
				inS = !inS
			}
		case '"':
			if !inS {
				inD = !inD
			}
		case '#':
			if !inS && !inD {
				return line[:i]
			}
		}
	}
	return line
}

// reverseTailToken returns the trailing token segment after the last `;` so a
// single-line `def x; body; end` can be checked for a closing `end`.
func reverseTailToken(line string) string {
	if i := strings.LastIndexByte(line, ';'); i >= 0 {
		return line[i+1:]
	}
	return line
}

// rbWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type rbWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []rubyFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkRbBody is the unified forward pass over a method body.
func walkRbBody(ctx rbWalkCtx, b rubyFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		raw := ctx.lines[ln-1]
		line := stripRubyLineNoise(raw)

		rbTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, rbDirectSinks(ctx, ln, line, tainted)...)

		r := rbFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// rbTrackTaint applies one line's assignment effects to the taint map.
func rbTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	if m := dfRbAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[1], m[2]
		if fld, ok := rbRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else {
			delete(tainted, name) // reassigned to non-source → drop taint
		}
	}
}

// rbRHSSourceField returns (field, true) when rhs is a request-input read or
// a reference to a tainted variable. A strong-params permit chain taints the
// var with field="" (the individual permitted attribute is recovered later at
// a `var[:field]` read).
func rbRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if dfRbPermitRe.MatchString(rhs) {
		return "", true
	}
	if m := dfRbSourceFieldRe.FindStringSubmatch(rhs); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g, true
			}
		}
		return "", true
	}
	if rbAnySourceWhole(rhs) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return rbTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfRbTaintedIndexRe captures `<ident>[:key]` / `<ident>['key']` for lifting a
// permitted-attribute field from a tainted strong-params var.
var dfRbTaintedIndexRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\[\s*:?['"]?([A-Za-z_][\w]*)['"]?\s*\]`)

// rbTaintedField resolves the source field for a reference to tainted var
// `name`. When the taint root carries no field (a permit chain / whole params
// hash) and `expr` indexes it (`user_params[:email]`), the index key is lifted
// as the field. The known field always wins when present.
func rbTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfRbTaintedIndexRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// rbDirectSinks emits flows for sinks on `line` whose args carry taint.
func rbDirectSinks(ctx rbWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfRbSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := rbFirstGroupText(line, m)
			if callee == "" {
				continue
			}
			// The sink argument region: for a paren call, the parenthesised
			// args; for `render json: x` / `.save` (no parens), the rest of
			// the line after the callee.
			openIdx := strings.IndexByte(line[m[0]:], '(')
			var args string
			if openIdx >= 0 {
				args = rubyCallArgs(ctx.lines, ln, m[0]+openIdx)
			} else {
				args = line[m[1]:]
			}
			fld, ok := rbExprTainted(args, tainted)
			if !ok {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			out = append(out, DataFlow{
				Function:    ctx.origin,
				SourceField: field,
				SourceLine:  ctx.srcLine,
				SinkKind:    s.kind,
				SinkName:    normalizeRbCallee(callee),
				SinkLine:    ln,
				HopVia:      firstOf(ctx.hopPath),
				HopPath:     dupStrings(ctx.hopPath),
			})
		}
	}
	return out
}

// rbFirstGroupText returns the first non-empty submatch text for match indices
// m (FindAllStringSubmatchIndex form), or the whole-match text if no group.
func rbFirstGroupText(line string, m []int) string {
	for g := 1; g*2+1 < len(m); g++ {
		if m[g*2] >= 0 {
			return line[m[g*2]:m[g*2+1]]
		}
	}
	return line[m[0]:m[1]]
}

// normalizeRbCallee collapses internal whitespace around `.`/`::` so a sink
// rendered across spacing (`User . create`) reads canonically (`User.create`).
func normalizeRbCallee(s string) string {
	s = strings.TrimSpace(s)
	s = dfRbDotSpaceRe.ReplaceAllString(s, ".")
	s = dfRbColonSpaceRe.ReplaceAllString(s, "::")
	return s
}

var (
	dfRbDotSpaceRe   = regexp.MustCompile(`\s*\.\s*`)
	dfRbColonSpaceRe = regexp.MustCompile(`\s*::\s*`)
)

// rbFollowCalls handles each local-call on `line`: recurse into a same-file
// method (bounded + cycle-guarded) or record a cross-file boundary.
func rbFollowCalls(ctx rbWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range rubyLocalCalls(line) {
		if rubyArgsHaveSplat(call.args) {
			continue // *args make positions unreliable — drop
		}
		for pos, argExpr := range call.args {
			fld, bare := rbArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := rbBodyByName(ctx.bodies, call.name)
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
			param := rubyParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkRbBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// rbExprTainted reports whether expr references a request source directly or a
// tainted variable, returning the field name when known.
func rbExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if m := dfRbSourceFieldRe.FindStringSubmatch(expr); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g, true
			}
		}
		return "", true
	}
	if dfRbPermitRe.MatchString(expr) {
		return "", true
	}
	if rbAnySourceWhole(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return rbTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// rbArgBareTaint reports whether argExpr is EXACTLY a tainted reference (a
// request-source read, a strong-params var, or a bare tainted identifier),
// not embedded in a larger expression and not a keyword/hash argument. This
// precision guard keeps positional binding sound. Returns the field.
//
// A Ruby keyword/hash argument form (`name: x`) is rejected for positional
// binding (drop — honest-partial), mirroring the python kwarg guard.
func rbArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if dfRbKwargRe.MatchString(e) {
		return "", false
	}
	if rbWholeExprIsSource(e) {
		if m := dfRbSourceFieldRe.FindStringSubmatch(e); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g, true
				}
			}
		}
		return "", true
	}
	if dfReSimpleIdent.MatchString(e) {
		if info, ok := tainted[e]; ok {
			return info.field, true
		}
	}
	// A static index off a tainted root (`user_params[:email]`) is a clean
	// positional value: bind it and lift the index key as the field.
	if m := dfRbTaintedIndexWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// dfRbKwargRe matches a Ruby keyword/hash argument form `name: ...` (a label).
var dfRbKwargRe = regexp.MustCompile(`^[A-Za-z_][\w]*:\s`)

// dfRbTaintedIndexWholeRe matches an expr that is SOLELY `ident[:key]` (no
// surrounding operators). Group 1 = root identifier, group 2 = key.
var dfRbTaintedIndexWholeRe = regexp.MustCompile(`^([A-Za-z_][\w]*)\s*\[\s*:?['"]?([A-Za-z_][\w]*)['"]?\s*\]$`)

// rbWholeExprIsSource reports the expr is SOLELY a request-source access.
func rbWholeExprIsSource(e string) bool {
	loc := dfRbSourceFieldRe.FindStringIndex(e)
	if loc == nil {
		loc = dfRbPermitRe.FindStringIndex(e)
	}
	if loc == nil {
		loc = dfRbSourceAnyRe.FindStringIndex(e)
	}
	if loc == nil {
		return false
	}
	// For permit chains the match ends at `permit(`; accept a trailing `)`.
	pre := strings.TrimSpace(e[:loc[0]])
	post := strings.TrimSpace(e[loc[1]:])
	if pre != "" {
		return false
	}
	if post == "" {
		return true
	}
	// permit(...) tail — only matched a prefix; accept if the remainder is a
	// balanced argument list close.
	return dfRbPermitRe.MatchString(e) && strings.HasSuffix(post, ")")
}

// rubyArgsHaveSplat reports whether any arg is a splat (`*x` / `**x`).
func rubyArgsHaveSplat(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(strings.TrimSpace(a), "*") {
			return true
		}
	}
	return false
}

// rubyCallArgs returns the argument text of the call whose `(` begins at/after
// byte anchor on line ln, spanning until the matching `)`.
func rubyCallArgs(lines []string, ln, anchor int) string {
	var sb strings.Builder
	depth := 0
	started := false
	for i := ln - 1; i < len(lines); i++ {
		s := lines[i]
		start := 0
		if i == ln-1 {
			a := dfMin(anchor, len(s))
			idx := strings.IndexByte(s[a:], '(')
			if idx < 0 {
				return ""
			}
			start = a + idx
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
		if i-(ln-1) > 40 {
			break
		}
	}
	return sb.String()
}

type rubyLocalCall struct {
	name string
	args []string
}

var dfRbLocalCallRe = regexp.MustCompile(`\b([A-Za-z_][\w]*[!?]?)\s*\(`)

// rubyLocalCalls extracts candidate bare-identifier method calls on a line
// with their top-level positional argument expressions. Method calls
// (`obj.foo(`) and common keywords are skipped.
func rubyLocalCalls(line string) []rubyLocalCall {
	var out []rubyLocalCall
	for _, m := range dfRbLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") || strings.HasSuffix(prev, ":") {
				continue // method call / symbol — not a bare-ident call
			}
		}
		if rubyControlKeyword(name) {
			continue
		}
		args := jstsSplitArgs(rubyCallArgs([]string{line}, 1, m[2]))
		out = append(out, rubyLocalCall{name: name, args: args})
	}
	return out
}

// rubyControlKeyword reports whether name is a Ruby keyword / common builtin
// that is never a local-method hop candidate.
func rubyControlKeyword(name string) bool {
	switch name {
	case "if", "unless", "while", "until", "for", "case", "when", "return",
		"yield", "puts", "print", "p", "raise", "require", "require_relative",
		"render", "redirect_to", "params", "new", "loop", "each", "map",
		"select", "reject", "define_method", "lambda", "proc", "send":
		return true
	}
	return false
}

// rubyParamName returns the pos-th positional parameter name of the method
// whose `def` header is on headerLine. Keyword params (`name:`), splat params
// (`*args`/`**kw`), and block params (`&blk`) make positional binding
// ambiguous → "". Default-valued positional params bind by their name.
func rubyParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	line := lines[headerLine-1]
	m := rubyFuncHeaderRe.FindStringIndex(line)
	if m == nil {
		return ""
	}
	// Parameters: either `def name(a, b)` or paren-less `def name a, b`.
	rest := line[m[1]:]
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") {
		close := strings.IndexByte(rest, ')')
		if close < 0 {
			return ""
		}
		rest = rest[1:close]
	} else {
		// paren-less: take up to end-of-line (best effort).
		if i := strings.IndexByte(rest, ';'); i >= 0 {
			rest = rest[:i]
		}
	}
	params := jstsSplitArgs(rest)
	for _, p := range params {
		t := strings.TrimSpace(p)
		if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "&") || strings.HasSuffix(strings.SplitN(t, " ", 2)[0], ":") {
			return "" // splat / block / keyword param → ambiguous
		}
	}
	if pos >= len(params) {
		return ""
	}
	p := strings.TrimSpace(params[pos])
	if i := strings.IndexByte(p, '='); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	if !dfReSimpleIdent.MatchString(p) {
		return ""
	}
	return p
}

func rbBodyByName(all []rubyFuncBody, name string) *rubyFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
