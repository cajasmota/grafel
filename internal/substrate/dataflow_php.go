// PHP request-input → sink dataflow sniffer (#3628 area #22, epic #3872,
// ticket #3966). PHP is the LAST language to gain a dataflow sniffer; this
// COMPLETES the cross-language DATA_FLOWS_TO generalization (py/jsts/go/ruby/
// java/php). C# (dataflow_csharp, #3960) is tracked separately.
//
// SCOPED def→use tracking inside one method body (`function … { … }`),
// followed through up to DataFlowMaxHops local (same-file) method-call hops,
// PLUS cross-file boundary emission for a tainted value that escapes into an
// imported / external callee. See dataflow.go for the contract and the
// honest-partial boundary. Mirrors dataflow_ruby.go / dataflow_jsts.go.
//
// Sources recognised (static key only):
//   - Laravel : $request->input('x') / request('x') / $request->x /
//     $request->query('x') / $request->all() / $request->validated()
//   - Symfony : $request->request->get('x') / $request->query->get('x') /
//     $request->get('x')
//   - Superglobals : $_POST['x'] / $_GET['x'] / $_REQUEST['x']
//
// `$request->all()` / `$request->validated()` taint the bound var with an
// EMPTY field (the individual key is not derivable at the call site —
// honest-partial). A later static member read recovers a field where one is
// statically present.
//
// Sinks recognised:
//   - db_write : Eloquent Model::create([…]) / ::update / ::insert /
//     ::firstOrCreate / ::updateOrCreate / $m->save() / $m->update([…]) /
//     Doctrine $em->persist(…), raw SQL DB::insert / DB::statement /
//     $pdo->query / $pdo->prepare / $stmt->execute with a tainted value
//   - response : return response()->json($x) / response($x) / view(…) /
//     return $x (in a controller — approximated by response()/view only)
//   - http_call: Guzzle $client->post|get|put|patch|request(…, [tainted]) ,
//     Http::post(…) (Laravel HTTP client) with a tainted argument
//
// Honest-partial (precision over recall): dynamic keys ($request->input($k)),
// $request->all()/validated() whole-array mass-assignment → field="";
// reassignment that breaks the chain, ambiguous splat/spread args, variadic
// params, recursion/cycle, and >DataFlowMaxHops depth are DROPPED, never
// fabricated.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("php", sniffDataFlowPHPEx, continueDataFlowPHP)
}

// sniffDataFlowPHP preserves the legacy in-file-only entry point.
func sniffDataFlowPHP(content string) []DataFlow { return sniffDataFlowPHPEx(content).Flows }

// --- source regexes ---------------------------------------------------------

// dfPhpSourceFieldRe captures a request-input read with a STATIC string key.
// Groups (in order) hold the key for the various access forms. Dynamic keys
// (`$request->input($k)`) do not match (honest-partial).
//
//	$request->input('x') / ->query('x') / ->post('x') / ->get('x')      → 1
//	$request->request->get('x') / ->query->get('x')                     → 2
//	request('x')                                                        → 3
//	$_POST['x'] / $_GET['x'] / $_REQUEST['x'] / $_COOKIE['x']           → 4
//	$request->x  (dynamic property access of a known request var)       → 5
var dfPhpSourceFieldRe = regexp.MustCompile(
	`\$\w+\s*->\s*(?:input|query|post|get|json|header|cookie|old)\s*\(\s*['"]([A-Za-z_][\w.]*)['"]` +
		`|\$\w+\s*->\s*(?:request|query|attributes|cookies|headers|files)\s*->\s*get\s*\(\s*['"]([A-Za-z_][\w.]*)['"]` +
		`|\brequest\s*\(\s*['"]([A-Za-z_][\w.]*)['"]` +
		`|\$_(?:POST|GET|REQUEST|COOKIE)\s*\[\s*['"]([A-Za-z_][\w.]*)['"]\s*\]` +
		`|\$request\s*->\s*([A-Za-z_]\w*)\s*(?:[;,)\]]|$)`,
)

// dfPhpWholeSourceRe matches a whole-request source access that carries NO
// statically-derivable key: `$request->all()`, `$request->validated()`,
// `$request->only([...])`, `$request->except([...])`, a bare `$request`, or
// `request()`. Such a source taints with field="".
var dfPhpWholeSourceRe = regexp.MustCompile(
	`\$\w+\s*->\s*(?:all|validated|only|except|collect|toArray)\s*\(` +
		`|\brequest\s*\(\s*\)` +
		`|\$_(?:POST|GET|REQUEST)\b`,
)

// dfPhpDynamicInputRe matches a DYNAMIC-key input read whose key is NOT a
// static string literal (e.g. `$request->input($key)`). Such an access is
// honest-partial: the field is not statically derivable, so it is NOT a usable
// keyed source (it would only flow as a whole-value with field="").
var dfPhpDynamicInputRe = regexp.MustCompile(
	`\$\w+\s*->\s*(?:input|query|post|get)\s*\(\s*\$`,
)

// --- sink regexes -----------------------------------------------------------

// dfPhpDBWriteRe matches an Eloquent / Doctrine write. Group 1 = callee text.
var dfPhpDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_]\w*\s*::\s*(?:create|update|insert|insertOrIgnore|upsert|firstOrCreate|updateOrCreate|fill|forceCreate))\s*\(` +
		`|(\$\w+\s*->\s*(?:save|update|fill|persist|push|insert|create|forceFill))\s*\(`,
)

// dfPhpRawSQLRe matches a raw-SQL write. Group 1 = callee text.
var dfPhpRawSQLRe = regexp.MustCompile(
	`\b(DB\s*::\s*(?:insert|update|delete|statement|unprepared|raw))\s*\(` +
		`|(\$\w+\s*->\s*(?:query|prepare|exec|execute|executeQuery|executeStatement))\s*\(`,
)

// dfPhpRespRe matches a response/view emission. Group 1 = callee text.
var dfPhpRespRe = regexp.MustCompile(
	`\b(response\s*\(\s*\)\s*->\s*(?:json|make|stream))\s*\(` +
		`|\b(response)\s*\(` +
		`|\b(view)\s*\(` +
		`|\b(json_encode)\s*\(`,
)

// dfPhpHTTPCallRe matches an outbound HTTP call. Group 1 = callee text.
var dfPhpHTTPCallRe = regexp.MustCompile(
	`(\$\w+\s*->\s*(?:request|post|get|put|patch|delete|send|sendAsync))\s*\(` +
		`|\b(Http\s*::\s*(?:post|get|put|patch|delete|send|withBody|asJson))\s*\(`,
)

// dfPhpSinkSpecs is the ordered sink table reused at every scan depth.
var dfPhpSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfPhpDBWriteRe, DataFlowSinkDBWrite},
	{dfPhpRawSQLRe, DataFlowSinkDBWrite},
	{dfPhpRespRe, DataFlowSinkResponse},
	{dfPhpHTTPCallRe, DataFlowSinkHTTPCall},
}

// dfPhpAssignRe captures `$name = <rhs>` (group 1 name without $, 2 rhs).
// Excludes `==`/augmented assigns by requiring a non-`=` first rhs char.
var dfPhpAssignRe = regexp.MustCompile(
	`^\s*\$([A-Za-z_]\w*)\s*=\s*([^=].*)$`,
)

// dfPhpVarRe is a whole PHP variable token ($name).
var dfPhpVarRe = regexp.MustCompile(`^\$[A-Za-z_]\w*$`)

func sniffDataFlowPHPEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := phpFuncBodies(content, lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := phpWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		r := walkPhpBody(ctx, b, map[string]taintInfo{})
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowPHP continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks.
// Function/SourceField/SourceLine on returned flows are placeholders that the
// links pass rewrites to the true origin handler.
func continueDataFlowPHP(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := phpFuncBodies(content, lines)
	callee := phpBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := phpParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := phpWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkPhpBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// --- function body model ----------------------------------------------------

// phpFuncBody is a method's line span (1-indexed, inclusive). Start is the
// header (`function name(`) line; End is the matching `}` line.
type phpFuncBody struct {
	Name  string
	Start int
	End   int
}

// phpFuncBodies computes brace-balanced spans for each `function name(`
// header, reusing the shared phpFuncHeaderRe. A header whose body brace cannot
// be balanced within the file is skipped (conservative — drop, never guess).
func phpFuncBodies(content string, lines []string) []phpFuncBody {
	var out []phpFuncBody
	for _, m := range phpFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		start := lineOfOffset(content, m[2])
		end := phpMatchBraceEnd(lines, start)
		if end == 0 {
			continue
		}
		out = append(out, phpFuncBody{Name: name, Start: start, End: end})
	}
	return out
}

// phpMatchBraceEnd finds the line of the `}` that closes the first `{` at/after
// startLine. Returns 0 if unbalanced (drop the body). PHP allows an abstract
// `function f();` with no body — that yields 0 and is dropped. String/comment
// content is not parsed out — a tolerable imprecision for the scoped pass.
func phpMatchBraceEnd(lines []string, startLine int) int {
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
			// A `;` before any `{` means an abstract/interface declaration with
			// no body — give up on this header.
			if !seen && r == ';' {
				return 0
			}
		}
	}
	return 0
}

func phpBodyByName(all []phpFuncBody, name string) *phpFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}

// --- walk -------------------------------------------------------------------

// phpWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type phpWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []phpFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkPhpBody is the unified forward pass over a method body.
func walkPhpBody(ctx phpWalkCtx, b phpFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := stripPhpLineNoise(ctx.lines[ln-1])

		phpTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, phpDirectSinks(ctx, ln, line, tainted)...)

		r := phpFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// stripPhpLineNoise removes a trailing `// comment` / `# comment` (not inside a
// string, best effort) so detection isn't fooled by comments.
func stripPhpLineNoise(line string) string {
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
		case '/':
			if !inS && !inD && i+1 < len(line) && line[i+1] == '/' {
				return line[:i]
			}
		}
	}
	return line
}

// phpTrackTaint applies one line's assignment effects to the taint map.
func phpTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	m := dfPhpAssignRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	name, rhs := m[1], m[2]
	if fld, ok := phpRHSSourceField(rhs, tainted); ok {
		tainted[name] = taintInfo{field: fld, line: ln}
	} else {
		delete(tainted, name) // reassigned to non-source → drop taint
	}
}

// phpRHSSourceField returns (field, true) when rhs is a request-input read or a
// reference to a tainted variable. A whole-request source (`$request->all()`)
// taints with field="".
func phpRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if !dfPhpDynamicInputRe.MatchString(rhs) {
		if m := dfPhpSourceFieldRe.FindStringSubmatch(rhs); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g, true
				}
			}
			return "", true
		}
	}
	if dfPhpWholeSourceRe.MatchString(rhs) {
		return "", true
	}
	for name, info := range tainted {
		if dfPhpVarUseRe(name).MatchString(rhs) {
			return phpTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfPhpVarUseRe builds a whole-variable matcher for `$name`.
func dfPhpVarUseRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\$` + regexp.QuoteMeta(name) + `\b`)
}

// dfPhpArrayKeyRe captures `$name['key']` / `$name["key"]` for lifting a field
// from a tainted whole-request var (`$data['email']`).
var dfPhpArrayKeyRe = regexp.MustCompile(`\$(\w+)\s*\[\s*['"]([A-Za-z_][\w.]*)['"]\s*\]`)

// phpTaintedField resolves the source field for a reference to tainted var
// `name`. When the taint root carries no field (whole-request) and `expr`
// indexes it (`$data['email']`), the index key is lifted as the field. The
// known field always wins when present.
func phpTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfPhpArrayKeyRe.FindAllStringSubmatch(expr, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// phpDirectSinks emits flows for sinks on `line` whose args carry taint.
func phpDirectSinks(ctx phpWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfPhpSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee, calleeEnd := phpFirstGroup(line, m)
			if callee == "" {
				continue
			}
			// Find the `(` that begins the SINK's argument list: it is the first
			// `(` at/after the end of the matched callee. For `response()->json(`
			// the callee text ends at `json`, so this lands on the json args —
			// not the empty `response()` parens.
			openIdx := strings.IndexByte(line[calleeEnd:], '(')
			if openIdx < 0 {
				continue
			}
			args := phpCallArgs(ctx.lines, ln, calleeEnd+openIdx)
			fld, ok := phpExprTainted(args, tainted)
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
				SinkName:    normalizePhpCallee(callee),
				SinkLine:    ln,
				HopVia:      firstOf(ctx.hopPath),
				HopPath:     dupStrings(ctx.hopPath),
			})
		}
	}
	return out
}

// phpFirstGroup returns the first non-empty submatch text for match indices m
// (FindAllStringSubmatchIndex form) AND the byte offset just past it. Falls
// back to the whole match when no group is set.
func phpFirstGroup(line string, m []int) (string, int) {
	for g := 1; g*2+1 < len(m); g++ {
		if m[g*2] >= 0 {
			return line[m[g*2]:m[g*2+1]], m[g*2+1]
		}
	}
	return line[m[0]:m[1]], m[1]
}

var (
	dfPhpDotSpaceRe   = regexp.MustCompile(`\s*->\s*`)
	dfPhpColonSpaceRe = regexp.MustCompile(`\s*::\s*`)
)

// normalizePhpCallee collapses internal whitespace around `->`/`::` so a sink
// rendered across spacing (`User :: create`) reads canonically.
func normalizePhpCallee(s string) string {
	s = strings.TrimSpace(s)
	s = dfPhpDotSpaceRe.ReplaceAllString(s, "->")
	s = dfPhpColonSpaceRe.ReplaceAllString(s, "::")
	return s
}

// phpExprTainted reports whether expr references a request source directly or a
// tainted variable, returning the field name when known.
func phpExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if !dfPhpDynamicInputRe.MatchString(expr) {
		if m := dfPhpSourceFieldRe.FindStringSubmatch(expr); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g, true
				}
			}
			return "", true
		}
	}
	if dfPhpWholeSourceRe.MatchString(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfPhpVarUseRe(name).MatchString(expr) {
			return phpTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// phpFollowCalls handles each local-call on `line`: recurse into a same-file
// function (bounded + cycle-guarded) or record a cross-file boundary.
func phpFollowCalls(ctx phpWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range phpLocalCalls(line) {
		if phpArgsHaveSplat(call.args) {
			continue // ...$args make positions unreliable — drop
		}
		for pos, argExpr := range call.args {
			fld, bare := phpArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := phpBodyByName(ctx.bodies, call.name)
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
			param := phpParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkPhpBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// phpArgBareTaint reports whether argExpr is EXACTLY a tainted reference (a
// request-source read, a tainted variable, or a static index off a tainted
// var), not embedded in a larger expression. This precision guard keeps
// positional binding sound. Returns the field.
func phpArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	// A whole-expr request source.
	if phpWholeExprIsSource(e) {
		if !dfPhpDynamicInputRe.MatchString(e) {
			if m := dfPhpSourceFieldRe.FindStringSubmatch(e); m != nil {
				for _, g := range m[1:] {
					if g != "" {
						return g, true
					}
				}
			}
		}
		return "", true
	}
	// A bare tainted variable ($name).
	if dfPhpVarRe.MatchString(e) {
		if info, ok := tainted[strings.TrimPrefix(e, "$")]; ok {
			return info.field, true
		}
	}
	// A static index off a tainted root (`$data['email']`) is a clean
	// positional value: bind it and lift the index key as the field.
	if m := dfPhpArrayKeyWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// dfPhpArrayKeyWholeRe matches an expr that is SOLELY `$ident['key']`. Group 1 =
// root identifier (without $), group 2 = key.
var dfPhpArrayKeyWholeRe = regexp.MustCompile(`^\$(\w+)\s*\[\s*['"]([A-Za-z_][\w.]*)['"]\s*\]$`)

// phpWholeExprIsSource reports the expr is SOLELY a request-source access.
func phpWholeExprIsSource(e string) bool {
	loc := dfPhpSourceFieldRe.FindStringIndex(e)
	if loc == nil {
		loc = dfPhpWholeSourceRe.FindStringIndex(e)
	}
	if loc == nil {
		return false
	}
	pre := strings.TrimSpace(e[:loc[0]])
	if pre != "" {
		return false
	}
	post := strings.TrimSpace(e[loc[1]:])
	if post == "" {
		return true
	}
	// The whole-source / call forms only matched a prefix (up to `(` or `->`);
	// accept a balanced call-close tail (`)` / `])`).
	return strings.HasSuffix(post, ")") || strings.HasSuffix(post, "]")
}

// phpArgsHaveSplat reports whether any arg is a spread (`...$x`).
func phpArgsHaveSplat(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(strings.TrimSpace(a), "...") {
			return true
		}
	}
	return false
}

// phpLocalCall is a parsed `name(arg0, arg1, ...)` call.
type phpLocalCall struct {
	name string
	args []string
}

// dfPhpLocalCallRe matches a call to a bare identifier or `$this->method(` /
// `self::method(` (potential local method hop). Group 1 (when present) is a
// `$this->` / `self::` / `static::` receiver prefix; group 2 is the name.
var dfPhpLocalCallRe = regexp.MustCompile(
	`(\$this\s*->\s*|self\s*::\s*|static\s*::\s*)?\b([A-Za-z_]\w*)\s*\(`,
)

// phpLocalCalls extracts candidate local function/method calls on a line with
// their top-level positional argument expressions. A free-function call
// (`helper(...)`) and a `$this->method(...)` / `self::method(...)` call both
// bind into a same-file `function name(` body. Other method calls
// (`$obj->foo(`) cannot be resolved positionally here and are skipped.
func phpLocalCalls(line string) []phpLocalCall {
	var out []phpLocalCall
	for _, m := range dfPhpLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[4]:m[5]]
		hasRecv := m[2] >= 0
		// When there is no $this->/self:: receiver, reject a call preceded by
		// `->`/`::`/`$` (a method/property/variable-function call we can't
		// resolve positionally) and any `new` constructor.
		if !hasRecv && m[4] > 0 {
			prev := strings.TrimRight(line[:m[4]], " \t")
			if strings.HasSuffix(prev, "->") || strings.HasSuffix(prev, "::") ||
				strings.HasSuffix(prev, "$") || strings.HasSuffix(prev, "new") {
				continue
			}
		}
		if phpControlKeyword(name) {
			continue
		}
		args := jstsSplitArgs(phpCallArgs([]string{line}, 1, m[4]))
		out = append(out, phpLocalCall{name: name, args: args})
	}
	return out
}

// phpControlKeyword reports whether name is a PHP keyword / common builtin that
// is never a local-method hop candidate (also covers recognised sinks so a
// sink call is not also chased as a hop).
func phpControlKeyword(name string) bool {
	switch name {
	case "if", "elseif", "else", "while", "for", "foreach", "switch", "case",
		"return", "echo", "print", "isset", "empty", "unset", "list", "array",
		"new", "function", "use", "match", "do", "try", "catch", "finally",
		"throw", "response", "view", "request", "json_encode", "json_decode",
		"compact", "dd", "dump", "abort", "redirect", "collect":
		return true
	}
	return false
}

// phpCallArgs returns the argument text of the call whose `(` begins at/after
// byte anchor on line ln, spanning until the matching `)`.
func phpCallArgs(lines []string, ln, anchor int) string {
	var sb strings.Builder
	depth := 0
	started := false
	for i := ln - 1; i < len(lines); i++ {
		s := lines[i]
		start := 0
		if i == ln-1 {
			a := dfMin(anchor, len(s))
			if a < 0 {
				a = 0
			}
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

// phpParamName returns the name (without $) of the pos-th positional parameter
// of the function whose header is on headerLine. A variadic param (`...$x`)
// anywhere makes positional binding ambiguous → "". Type hints, by-ref `&`,
// nullable `?`, and defaults are stripped.
func phpParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	line := lines[headerLine-1]
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return ""
	}
	// Balance to the matching close paren across the (single) header line.
	depth := 0
	close := -1
	for i := open; i < len(line); i++ {
		switch line[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				close = i
			}
		}
		if close >= 0 {
			break
		}
	}
	if close < 0 {
		return ""
	}
	params := jstsSplitArgs(line[open+1 : close])
	for _, p := range params {
		if strings.Contains(p, "...") {
			return "" // variadic → ambiguous positions
		}
	}
	if pos >= len(params) {
		return ""
	}
	return phpParamIdent(params[pos])
}

// dfPhpParamVarRe captures the `$name` of a parameter declaration, ignoring
// type hints, `?`, `&`, and a default value.
var dfPhpParamVarRe = regexp.MustCompile(`\$([A-Za-z_]\w*)`)

// phpParamIdent extracts the bare variable name from a single parameter decl.
func phpParamIdent(p string) string {
	m := dfPhpParamVarRe.FindStringSubmatch(strings.TrimSpace(p))
	if m == nil {
		return ""
	}
	return m[1]
}
