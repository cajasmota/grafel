// Python request-input → sink dataflow sniffer (#3628 area #22).
//
// SCOPED def→use tracking inside one function body, plus one hop into a
// directly-called local (module-level) function. See dataflow.go for the
// contract and the honest-partial boundary.
//
// Sources recognised (static key only):
//   - request.data['x'] / request.data.get('x')        (DRF body)
//   - request.GET.get('x') / request.POST.get('x')      (Django)
//   - request.json['x'] / request.json.get('x')         (Flask/generic)
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

func init() { RegisterDataFlowSniffer("python", sniffDataFlowPython) }

// dfPySourceFieldRe captures a request-input read with a STATIC string
// key. Group 1/2/3/4 hold the key depending on the access form. Dynamic
// keys (`request.data[k]`) do not match (honest-partial).
var dfPySourceFieldRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:data|json)\s*\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]` +
		`|\brequest\s*\.\s*(?:data|json|GET|POST)\s*\.\s*get\s*\(\s*['"]([A-Za-z_][\w]*)['"]` +
		`|\bserializer\s*\.\s*validated_data\s*\[\s*['"]([A-Za-z_][\w]*)['"]\s*\]` +
		`|\bserializer\s*\.\s*validated_data\s*\.\s*get\s*\(\s*['"]([A-Za-z_][\w]*)['"]`,
)

// dfPySourceAnyRe matches a source receiver without requiring a static
// key, for whole-object pass-through (`return Response(request.data)`).
var dfPySourceAnyRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:data|json|GET|POST)\b|\bserializer\s*\.\s*validated_data\b`,
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

// dfPyAssignRe captures `NAME = <rhs>` (group 1 name, group 2 rhs).
// Excludes augmented/compare via the `[^=]` after `=`.
var dfPyAssignRe = regexp.MustCompile(
	`^(\s*)([A-Za-z_][\w]*)\s*=\s*([^=].*)$`,
)

func sniffDataFlowPython(content string) []DataFlow {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	headers := scanPyFuncHeaders(content)
	bodies := pyFuncBodies(lines, headers)

	var out []DataFlow
	for _, b := range bodies {
		out = append(out, analyzePyBody(lines, b, bodies)...)
	}
	return out
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

func analyzePyBody(lines []string, b pyFuncBody, all []pyFuncBody) []DataFlow {
	tainted := map[string]taintInfo{}
	var out []DataFlow

	for ln := b.Start; ln <= b.End && ln <= len(lines); ln++ {
		// Only consider lines strictly inside the body (indent > def indent).
		if ln != b.Start {
			if strings.TrimSpace(lines[ln-1]) != "" && leadingWS(lines[ln-1]) <= b.Indent {
				break
			}
		}
		line := lines[ln-1]

		// Assignment propagation / chain-breaking reassignment.
		if m := dfPyAssignRe.FindStringSubmatch(line); m != nil {
			name, rhs := m[2], m[3]
			if fld, ok := pyRHSSourceField(rhs, tainted); ok {
				tainted[name] = taintInfo{field: fld, line: ln}
			} else {
				delete(tainted, name) // reassigned to non-source → drop taint
			}
		}

		out = appendPySinkFlows(out, lines, b, ln, line, tainted, all)
	}
	return out
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

func appendPySinkFlows(out []DataFlow, lines []string, b pyFuncBody, ln int, line string, tainted map[string]taintInfo, all []pyFuncBody) []DataFlow {
	for _, s := range []struct {
		re   *regexp.Regexp
		kind DataFlowSinkKind
	}{
		{dfPyDBWriteRe, DataFlowSinkDBWrite},
		{dfPyRespRe, DataFlowSinkResponse},
		{dfPyHTTPCallRe, DataFlowSinkHTTPCall},
	} {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := line[m[2]:m[3]]
			args := pyCallArgs(lines, ln, m[2])
			if fld, ok := pyExprTainted(args, tainted); ok {
				out = append(out, DataFlow{
					Function:    b.Name,
					SourceField: fld,
					SourceLine:  ln,
					SinkKind:    s.kind,
					SinkName:    callee,
					SinkLine:    ln,
				})
			}
		}
	}

	// One-hop into a local function call helper(tainted).
	for _, call := range pyLocalCalls(line) {
		callee := pyBodyByName(all, call.name)
		if callee == nil || callee.Name == b.Name {
			continue
		}
		for pos, argExpr := range call.args {
			fld, ok := pyExprTainted(argExpr, tainted)
			if !ok {
				continue
			}
			param := pyParamName(lines, callee.Start, pos)
			if param == "" {
				continue
			}
			for cln := callee.Start; cln <= callee.End && cln <= len(lines); cln++ {
				out = appendPyHopSink(out, lines, b, callee, param, fld, ln, cln, lines[cln-1])
			}
		}
	}
	return out
}

func appendPyHopSink(out []DataFlow, lines []string, origin pyFuncBody, callee *pyFuncBody, param, fld string, srcLine, cln int, cline string) []DataFlow {
	for _, s := range []struct {
		re   *regexp.Regexp
		kind DataFlowSinkKind
	}{
		{dfPyDBWriteRe, DataFlowSinkDBWrite},
		{dfPyRespRe, DataFlowSinkResponse},
		{dfPyHTTPCallRe, DataFlowSinkHTTPCall},
	} {
		for _, m := range s.re.FindAllStringSubmatchIndex(cline, -1) {
			callName := cline[m[2]:m[3]]
			args := pyCallArgs(lines, cln, m[2])
			if dfReWholeIdent(param).MatchString(args) {
				out = append(out, DataFlow{
					Function:    origin.Name,
					SourceField: fld,
					SourceLine:  srcLine,
					SinkKind:    s.kind,
					SinkName:    callName,
					SinkLine:    cln,
					HopVia:      callee.Name,
				})
			}
		}
	}
	return out
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
// positional index aligns with call-site args. Complex params → "".
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
	if pos >= len(params) {
		return ""
	}
	p := strings.TrimSpace(params[pos])
	if i := strings.IndexAny(p, ":="); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	if strings.HasPrefix(p, "*") || !dfReSimpleIdent.MatchString(p) {
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
