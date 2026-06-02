// Python request-input → sink dataflow sniffer (#3628 area #22).
//
// SCOPED def→use tracking inside one function body, followed through up to
// DataFlowMaxHops local (module-level) call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an imported callee. See
// dataflow.go for the contract and the honest-partial boundary.
//
// Sources recognised (static key only):
//   - request.data['x'] / request.data.get('x')        (DRF body)
//   - request.GET['x'] / request.GET.get('x')           (Django query)
//   - request.POST['x'] / request.POST.get('x')         (Django form)
//   - request.json['x'] / request.json.get('x')         (Flask/generic)
//   - request.form['x'] / request.form.get('x')         (Flask form)
//   - request.args.get('x') / request.args['x']         (Flask query)
//   - request.values.get('x') / request.values['x']     (Flask combined)
//   - serializer.validated_data['x'] / .get('x')        (DRF)
//
// Sinks recognised:
//   - DB write : <Model>.objects.create( / <obj>.save( / <repo>.insert(
//   - response : return Response( / return JsonResponse(
//   - http_call: requests.get|post|put|delete( / httpx.* / session.*
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("python", sniffDataFlowPythonEx, continueDataFlowPython)
}

// sniffDataFlowPython preserves the legacy in-file-only entry point.
func sniffDataFlowPython(content string) []DataFlow { return sniffDataFlowPythonEx(content).Flows }

// dfPySourceFieldRe captures a request-input read with a STATIC string
// key. Group 1/2/3/4 hold the key depending on the access form. Dynamic
// keys (`request.data[k]`) do not match (honest-partial).
var dfPySourceFieldRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:data|json|form|args|values|GET|POST)\s*\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]` +
		`|\brequest\s*\.\s*(?:data|json|form|args|values|GET|POST)\s*\.\s*get\s*\(\s*['"]([A-Za-z_][\w]*)['"]` +
		`|\bserializer\s*\.\s*validated_data\s*\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]` +
		`|\bserializer\s*\.\s*validated_data\s*\.\s*get\s*\(\s*['"]([A-Za-z_][\w]*)['"]`,
)

// dfPySourceAnyRe matches a source receiver without requiring a static
// key, for whole-object pass-through (`return Response(request.data)`).
var dfPySourceAnyRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:data|json|form|args|values|GET|POST)\b|\bserializer\s*\.\s*validated_data\b`,
)

// dfPyDBWriteRe matches an ORM write. Group 1 = the callee text.
var dfPyDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_][\w.]*\.objects\.create|[A-Za-z_][\w.]*\.(?:save|insert|create|update))\s*\(`,
)

// dfPyRespRe matches a response emission. Group 1 = callee.
var dfPyRespRe = regexp.MustCompile(
	`\b((?:Response|JsonResponse|HttpResponse))\s*\(`,
)

// dfPyHTTPCallRe matches an outbound HTTP call. Group 1 = callee.
var dfPyHTTPCallRe = regexp.MustCompile(
	`\b((?:requests|httpx|session|client)\s*\.\s*(?:get|post|put|delete|patch))\s*\(`,
)

// dfPyAssignRe captures `NAME = <rhs>` (group 1 indent, 2 name, 3 rhs).
// Excludes augmented/compare via the `[^=]` after `=`.
var dfPyAssignRe = regexp.MustCompile(
	`^(\s*)([A-Za-z_][\w]*)\s*=\s*([^=].*)$`,
)

// dfPySinkSpecs is the ordered sink table reused at every scan depth.
var dfPySinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfPyDBWriteRe, DataFlowSinkDBWrite},
	{dfPyRespRe, DataFlowSinkResponse},
	{dfPyHTTPCallRe, DataFlowSinkHTTPCall},
}

func sniffDataFlowPythonEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := scanPyFuncHeaders(content)
	bodies := pyFuncBodies(lines, headers)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := pyWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		r := walkPyBody(ctx, b, map[string]taintInfo{})
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowPython continues a bounded hop walk inside this file: it
// binds the tainted value into fnName's paramIndex-th parameter and walks.
// Function/SourceField/SourceLine on returned flows are placeholders that the
// links pass rewrites to the true origin handler.
func continueDataFlowPython(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	headers := scanPyFuncHeaders(content)
	bodies := pyFuncBodies(lines, headers)
	callee := pyBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := pyParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := pyWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkPyBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// pyFuncBody is a function's line span (1-indexed, inclusive) with the
// indentation of the def header.
type pyFuncBody struct {
	Name   string
	Start  int
	End    int
	Indent int
}

// pyFuncBodies computes indentation-delimited spans for each header.
func pyFuncBodies(lines []string, headers []funcHeader) []pyFuncBody {
	var out []pyFuncBody
	for _, h := range headers {
		if h.Line < 1 || h.Line > len(lines) {
			continue
		}
		// Normalize: the multiline `^` anchor can place the header match on
		// the preceding (often blank) line. Snap to the actual `def` line.
		defLine := h.Line
		for defLine <= len(lines) && !dfPyDefLineRe.MatchString(lines[defLine-1]) {
			defLine++
		}
		if defLine > len(lines) {
			continue
		}
		indent := leadingWS(lines[defLine-1])
		end := defLine
		for i := defLine; i < len(lines); i++ {
			ln := lines[i]
			if strings.TrimSpace(ln) == "" {
				end = i + 1
				continue
			}
			if leadingWS(ln) <= indent {
				break
			}
			end = i + 1
		}
		out = append(out, pyFuncBody{Name: h.Name, Start: defLine, End: end, Indent: indent})
	}
	return out
}

// leadingWS returns the count of leading whitespace columns (tab=1).
func leadingWS(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// pyWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type pyWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []pyFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkPyBody is the unified forward pass over a function body.
func walkPyBody(ctx pyWalkCtx, b pyFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for ln := b.Start; ln <= b.End && ln <= len(ctx.lines); ln++ {
		// Only consider lines strictly inside the body (indent > def indent).
		if ln != b.Start {
			if strings.TrimSpace(ctx.lines[ln-1]) != "" && leadingWS(ctx.lines[ln-1]) <= b.Indent {
				break
			}
		}
		line := ctx.lines[ln-1]

		pyTrackTaint(tainted, line, ln)

		res.Flows = append(res.Flows, pyDirectSinks(ctx, ln, line, tainted)...)

		r := pyFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// pyTrackTaint applies one line's assignment effects to the taint map.
func pyTrackTaint(tainted map[string]taintInfo, line string, ln int) {
	if m := dfPyAssignRe.FindStringSubmatch(line); m != nil {
		name, rhs := m[2], m[3]
		if fld, ok := pyRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
		} else {
			delete(tainted, name) // reassigned to non-source → drop taint
		}
	}
}

// pyRHSSourceField returns (field, true) when rhs is a request-input read
// or a reference to a tainted variable.
func pyRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if m := dfPySourceFieldRe.FindStringSubmatch(rhs); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g, true
			}
		}
		return "", true
	}
	if dfPySourceAnyRe.MatchString(rhs) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return info.field, true
		}
	}
	return "", false
}

// pyDirectSinks emits flows for sinks on `line` whose args carry taint.
func pyDirectSinks(ctx pyWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfPySinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := line[m[2]:m[3]]
			args := pyCallArgs(ctx.lines, ln, m[2])
			if fld, ok := pyExprTainted(args, tainted); ok {
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

// pyFollowCalls handles each local-call on `line`: recurse into a local
// module function (bounded + cycle-guarded) or record a cross-file boundary.
func pyFollowCalls(ctx pyWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range pyLocalCalls(line) {
		if pyArgsHaveStar(call.args) {
			continue // *args/**kwargs make positions unreliable — drop
		}
		for pos, argExpr := range call.args {
			if _, ok := pyExprTainted(argExpr, tainted); !ok {
				continue
			}
			// Skip keyword args (`name=value`) for positional binding; only a
			// bare positional tainted arg binds soundly.
			fld, bare := pyArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := pyBodyByName(ctx.bodies, call.name)
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
			param := pyParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkPyBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// pyExprTainted reports whether expr references a request source or a
// tainted variable, returning the field when known.
func pyExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if m := dfPySourceFieldRe.FindStringSubmatch(expr); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g, true
			}
		}
		return "", true
	}
	if dfPySourceAnyRe.MatchString(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return info.field, true
		}
	}
	return "", false
}

// pyArgBareTaint reports whether argExpr is EXACTLY a tainted value (a
// request-source read or a bare tainted identifier), not embedded in a
// larger expression and not a keyword argument. Precision guard for sound
// positional binding. Returns the field.
func pyArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	// Reject keyword argument form `name=expr` (a positional binding cannot
	// be derived from it soundly).
	if dfPyKwargRe.MatchString(e) {
		return "", false
	}
	if pyWholeExprIsSource(e) {
		if m := dfPySourceFieldRe.FindStringSubmatch(e); m != nil {
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
	return "", false
}

// dfPyKwargRe matches a keyword-argument form `name=...` (but not `==`).
var dfPyKwargRe = regexp.MustCompile(`^[A-Za-z_][\w]*\s*=[^=]`)

// pyWholeExprIsSource reports the expr is SOLELY a request-source access.
func pyWholeExprIsSource(e string) bool {
	loc := dfPySourceFieldRe.FindStringIndex(e)
	if loc == nil {
		loc = dfPySourceAnyRe.FindStringIndex(e)
	}
	if loc == nil {
		return false
	}
	return strings.TrimSpace(e[:loc[0]]) == "" && strings.TrimSpace(e[loc[1]:]) == ""
}

// pyArgsHaveStar reports whether any arg is a *args / **kwargs unpack.
func pyArgsHaveStar(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(strings.TrimSpace(a), "*") {
			return true
		}
	}
	return false
}

// pyCallArgs returns the argument text of the call whose `(` begins at/after
// byte anchor on line ln, spanning until the matching `)`.
func pyCallArgs(lines []string, ln, anchor int) string {
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

type pyLocalCall struct {
	name string
	args []string
}

var dfPyLocalCallRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

// dfPyDefLineRe matches a real `def`/`async def` line for header snapping.
var dfPyDefLineRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+[A-Za-z_]`)

func pyLocalCalls(line string) []pyLocalCall {
	var out []pyLocalCall
	for _, m := range dfPyLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		// skip method calls (`obj.foo(`) and keywords/builtins.
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			if strings.HasSuffix(prev, ".") {
				continue
			}
		}
		switch name {
		case "if", "for", "while", "return", "print", "len", "range", "def", "self":
			continue
		}
		args := jstsSplitArgs(pyCallArgs([]string{line}, 1, m[2]))
		out = append(out, pyLocalCall{name: name, args: args})
	}
	return out
}

// pyParamName returns the pos-th positional parameter name of the function
// whose header is on headerLine. A leading `self`/`cls` is skipped so the
// positional index aligns with call-site args. Complex / *args params → "".
func pyParamName(lines []string, headerLine, pos int) string {
	if headerLine < 1 || headerLine > len(lines) {
		return ""
	}
	line := lines[headerLine-1]
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return ""
	}
	close := strings.LastIndexByte(line, ')')
	if close < 0 || close < open {
		return ""
	}
	params := jstsSplitArgs(line[open+1 : close])
	// Drop a leading self/cls receiver.
	if len(params) > 0 {
		p0 := strings.TrimSpace(params[0])
		if p0 == "self" || p0 == "cls" {
			params = params[1:]
		}
	}
	// A *args / **kwargs parameter makes positions ambiguous past it.
	for _, p := range params {
		if strings.HasPrefix(strings.TrimSpace(p), "*") {
			return ""
		}
	}
	if pos >= len(params) {
		return ""
	}
	p := strings.TrimSpace(params[pos])
	if i := strings.IndexAny(p, ":="); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	if !dfReSimpleIdent.MatchString(p) {
		return ""
	}
	return p
}

func pyBodyByName(all []pyFuncBody, name string) *pyFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
