// JS/TS request-input → sink dataflow sniffer (#3628 area #22).
//
// SCOPED def→use tracking inside one function body, plus one hop into a
// directly-called local function. See dataflow.go for the contract and the
// honest-partial boundary.
//
// Sources recognised:
//   - req.body.X / req.query.X / req.params.X         (Express / generic)
//   - request.body.X / request.query.X                (Hono / generic)
//   - ctx.request.body.X                              (Koa)
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
	"strings"
)

func init() { RegisterDataFlowSniffer("jsts", sniffDataFlowJSTS) }

// dfJstsSourceFieldRe captures a request-input read with a STATIC field
// name. Group 1 = field. Anchored on the canonical receivers. Dynamic
// access (`req.body[k]`) is intentionally NOT matched (honest-partial).
var dfJstsSourceFieldRe = regexp.MustCompile(
	`\b(?:req|request)\s*\.\s*(?:body|query|params)\s*\.\s*([A-Za-z_$][\w$]*)\b` +
		`|\bctx\s*\.\s*request\s*\.\s*body\s*\.\s*([A-Za-z_$][\w$]*)\b`,
)

// dfJstsSourceAnyRe matches the source receiver prefix without requiring a
// field, used to detect whole-object pass-through (`res.json(req.body)`).
var dfJstsSourceAnyRe = regexp.MustCompile(
	`\b(?:req|request)\s*\.\s*(?:body|query|params)\b|\bctx\s*\.\s*request\s*\.\s*body\b`,
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

func sniffDataFlowJSTS(content string) []DataFlow {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	headers := scanJSTSFuncHeaders(content)
	bodies := jstsFuncBodies(content, headers)

	var out []DataFlow
	for _, b := range bodies {
		out = append(out, analyzeJSTSBody(lines, b, bodies)...)
	}
	return out
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
		end := jstsMatchBraceEnd(lines, h.Line)
		if end == 0 {
			continue
		}
		out = append(out, jstsFuncBody{Name: h.Name, Start: h.Line, End: end})
	}
	return out
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

// analyzeJSTSBody runs the scoped def→use over a single function body and
// returns the flows that reach a sink (intra-fn or one-hop).
func analyzeJSTSBody(lines []string, b jstsFuncBody, all []jstsFuncBody) []DataFlow {
	// tainted: var name -> source field (may be "" for whole-object).
	tainted := map[string]taintInfo{}
	var out []DataFlow

	for ln := b.Start; ln <= b.End && ln <= len(lines); ln++ {
		line := lines[ln-1]

		// Chain-breaking reassignment: a bare `name = <expr>` where expr is
		// NOT a source/tainted reference removes the taint. (honest-partial)
		if m := dfJstsBareAssignRe.FindStringSubmatch(line); m != nil {
			name, rhs := m[1], m[2]
			if _, was := tainted[name]; was && !jstsRHSCarriesTaint(rhs, tainted) {
				delete(tainted, name)
				// fall through: a bare assignment is not also a decl below.
			}
		}

		// Propagation via declaration: const y = <source or tainted>.
		if m := dfJstsConstAssignRe.FindStringSubmatch(line); m != nil {
			name, rhs := m[1], m[2]
			if fld, ok := jstsRHSSourceField(rhs, tainted); ok {
				tainted[name] = taintInfo{field: fld, line: ln}
				continue
			}
			// Declared from a non-source expr: ensure no stale taint.
			delete(tainted, name)
		}

		// Sinks. A sink fires if any of its argument text references a
		// source directly or a tainted variable.
		out = appendJSTSSinkFlows(out, lines, b, ln, line, tainted, all)
	}
	return out
}

type taintInfo struct {
	field string
	line  int
}

// jstsRHSSourceField returns (field, true) when rhs is a request-input read
// or a reference to a tainted variable. The field is the source field name
// (possibly ""), preserving provenance across the assignment.
func jstsRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	if m := dfJstsSourceFieldRe.FindStringSubmatch(rhs); m != nil {
		if m[1] != "" {
			return m[1], true
		}
		return m[2], true
	}
	if dfJstsSourceAnyRe.MatchString(rhs) {
		return "", true
	}
	// Reference to an existing tainted var, e.g. `const z = y;`.
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return info.field, true
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

// appendJSTSSinkFlows detects sinks on `line` and, for each, emits a flow
// if a tainted value or a direct source reaches its argument list. Also
// performs the single one-hop expansion into a local function call.
func appendJSTSSinkFlows(out []DataFlow, lines []string, b jstsFuncBody, ln int, line string, tainted map[string]taintInfo, all []jstsFuncBody) []DataFlow {
	// Direct sinks in this body.
	for _, s := range []struct {
		re   *regexp.Regexp
		kind DataFlowSinkKind
	}{
		{dfJstsDBWriteRe, DataFlowSinkDBWrite},
		{dfJstsRespRe, DataFlowSinkResponse},
		{dfJstsHTTPCallRe, DataFlowSinkHTTPCall},
	} {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := line[m[2]:m[3]]
			args := jstsCallArgs(lines, ln, m[2])
			if fld, ok := jstsArgsTainted(args, tainted); ok {
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

	// One-hop: a call `helper(<tainted-or-source>)` where helper is a local
	// function. Bind the tainted argument into helper's matching positional
	// parameter and look for a sink inside helper's body (one level only).
	for _, call := range jstsLocalCalls(line) {
		callee := jstsBodyByName(all, call.name)
		if callee == nil || callee.Name == b.Name {
			continue
		}
		// Which positional args carry taint?
		for pos, argExpr := range call.args {
			fld, ok := jstsExprTainted(argExpr, tainted)
			if !ok {
				continue
			}
			param := jstsParamName(lines, callee.Start, pos)
			if param == "" {
				continue
			}
			// Scan the callee body for a sink that uses `param` directly.
			for cln := callee.Start; cln <= callee.End && cln <= len(lines); cln++ {
				cline := lines[cln-1]
				out = appendJSTSHopSink(out, lines, b, callee, param, fld, ln, cln, cline)
			}
		}
	}
	return out
}

// appendJSTSHopSink emits a one-hop flow when a sink inside the callee uses
// the bound parameter directly.
func appendJSTSHopSink(out []DataFlow, lines []string, origin jstsFuncBody, callee *jstsFuncBody, param, fld string, srcLine, cln int, cline string) []DataFlow {
	for _, s := range []struct {
		re   *regexp.Regexp
		kind DataFlowSinkKind
	}{
		{dfJstsDBWriteRe, DataFlowSinkDBWrite},
		{dfJstsRespRe, DataFlowSinkResponse},
		{dfJstsHTTPCallRe, DataFlowSinkHTTPCall},
	} {
		for _, m := range s.re.FindAllStringSubmatchIndex(cline, -1) {
			callName := cline[m[2]:m[3]]
			args := jstsCallArgs(lines, cln, m[2])
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

// jstsArgsTainted reports whether the argument text references a source
// directly or any tainted variable; returns the associated field.
func jstsArgsTainted(args string, tainted map[string]taintInfo) (string, bool) {
	return jstsExprTainted(args, tainted)
}

// jstsExprTainted reports whether expr references a request source directly
// or a tainted variable, returning the field name when known.
func jstsExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	if m := dfJstsSourceFieldRe.FindStringSubmatch(expr); m != nil {
		if m[1] != "" {
			return m[1], true
		}
		return m[2], true
	}
	if dfJstsSourceAnyRe.MatchString(expr) {
		return "", true
	}
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return info.field, true
		}
	}
	return "", false
}

// jstsCallArgs returns the argument text of the call whose `(` begins at or
// after byte offset openByte on line ln, spanning until the matching `)`
// (possibly across lines). Returns the inner text.
func jstsCallArgs(lines []string, ln, openByte int) string {
	// Concatenate from the `(` to the matching `)`.
	var sb strings.Builder
	depth := 0
	started := false
	for i := ln - 1; i < len(lines); i++ {
		s := lines[i]
		start := 0
		if i == ln-1 {
			// find the first '(' at/after openByte
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

// jstsLocalCalls extracts candidate local-function calls on a line with
// their top-level positional argument expressions.
func jstsLocalCalls(line string) []jstsLocalCall {
	var out []jstsLocalCall
	for _, m := range dfJstsLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		// skip known sink/keyword callees handled directly
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
// function whose header is on headerLine. Destructured params are skipped
// (return "" → no one-hop binding, honest-partial).
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
	p := strings.TrimSpace(params[pos])
	// strip a TS type annotation / default.
	if i := strings.IndexAny(p, ":="); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	if !dfReSimpleIdent.MatchString(p) {
		return "" // destructured / rest / complex — drop
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

func dfMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
