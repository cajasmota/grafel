// C / C++ request-input → sink dataflow sniffer (#4049, epic #3872, audit
// #3883). Extends the universal cross-language dataflow pass to a NINTH
// language — csharp / go / java / jsts / php / python / ruby / scala already
// have one, and C/C++ was the only mainstream language in the substrate set
// still missing a connected source→sink DATA_FLOWS_TO lane.
//
// SCOPED def→use tracking inside one function body, followed through up to
// DataFlowMaxHops local (same-file) call hops, PLUS cross-file boundary
// emission for a tainted value that escapes into an external callee. See
// dataflow.go for the contract and the honest-partial boundary. Mirrors
// dataflow_csharp.go (brace-balanced bodies) and dataflow_scala.go (request
// input is read INSIDE the body, not via an annotated parameter): same
// DataFlow/source/sink model, same DATA_FLOWS_TO emission, same
// precision-over-recall discipline.
//
// Source vocabulary is aligned with taint_sites_c_cpp.go (do NOT duplicate —
// the request-input shapes there are the source surface here). A C++ web
// handler reads request input inside the body, so taint is seeded at the read
// site (`auto id = req.url_params.get("id");`). The accessed field name is
// captured when statically knowable (`req.url_params.get("id")` → "id";
// `req->getParameter("name")` → "name"); a whole-body read (`req.body()`,
// `req->getBody()`, raw `argv[1]`, `recv(...)`) seeds field "".
//
//   - Crow      : `req.body`, `req.url_params.get("k")`, `req.get_header_value("h")`
//   - Drogon    : `req->getParameter("k")`, `req->getBody()`, `req->body()`
//   - oat++      : `request->getQueryParameter("k")`, `request->readBodyToString()`
//   - Pistache  : `request.body()`, `request.query().get("k")`
//   - POCO      : `request.getParameter("k")`, `form.get("k")`
//   - raw C     : `argv[n]`, `recv(...)` / `recvfrom(...)` into a buffer
//
// Sinks recognised (aligned with effect_sinks_c_cpp.go write subset):
//   - db_write : libpqxx `txn.exec(...)` / `txn.exec_params(...)` / `W.exec0(...)`,
//     mongocxx `coll.insert_one(...)` / `.update_one(...)` / `.replace_one(...)`,
//     sqlpp11 / soci `sql << "..."`, raw `sqlite3_exec(...)` / `PQexec(...)` /
//     `mysql_query(...)`, when an argument carries taint.
//   - response : Crow `res.write(...)` / `res.set_body(...)`, Drogon / oat++
//     callback `callback(resp)`, `res.body = <tainted>` member assignment.
//   - http_call: outbound cpr `cpr::Post(...)` / `cpr::Put(...)` with a tainted
//     body, libcurl `curl_easy_setopt(h, CURLOPT_POSTFIELDS, <tainted>)` — a
//     CONSUMES_API site.
//
// COMMAND exec (`system(<tainted>)` / `exec*` / `popen`) is recorded as a
// db_write-class data sink so a request value reaching a shell is surfaced;
// it is the highest-value C/C++ flow and effect_sinks_c_cpp.go models the same
// primitives under fs_write.
//
// HONEST-PARTIAL (precision over recall): a request read that never reaches a
// sink (logged / discarded), a sink fed by a constant with no request
// provenance, reassignment that breaks the chain, embedded-expression args that
// cannot be bound positionally, >DataFlowMaxHops depth, and recursion are
// DROPPED — never fabricated. A whole-body flow with no derivable field is
// emitted with field="".
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterDataFlowSnifferEx("c-cpp", sniffDataFlowCCPPEx, continueDataFlowCCPP)
}

// sniffDataFlowCCPP preserves the legacy in-file-only entry point.
func sniffDataFlowCCPP(content string) []DataFlow { return sniffDataFlowCCPPEx(content).Flows }

// ---- source recognition (aligned with taint_sites_c_cpp.go) ----

// dfCCPPReqAccessRe matches a request-input read whose result is the RHS of a
// binding or flows into a sink. The first non-empty submatch (when present) is
// the statically-known field for a keyed accessor (`req.url_params.get("id")`
// → "id", `req->getParameter("name")` → "name"). A whole-body read
// (`req.body()`, `req->getBody()`, `argv[…]`, `recv(...)`) matches with no
// captured field → field "".
var dfCCPPReqAccessRe = regexp.MustCompile(
	`\b(?:req|request)\s*(?:\.|->)\s*(?:url_params\s*\.\s*get|get_param|getParameter|getQueryParameter|get_header_value|getHeader)\s*\(\s*"([^"]*)"` +
		`|\b(?:req|request)\s*(?:\.|->)\s*query\s*\(\s*\)\s*\.\s*get\s*\(\s*"([^"]*)"` +
		`|\b(?:form|params)\s*\.\s*get\s*\(\s*"([^"]*)"` +
		`|\b(?:req|request)\s*(?:\.|->)\s*(?:body|getBody|readBodyToString)\s*\(\s*\)` +
		`|\b(?:req|request)\s*(?:\.|->)\s*body\b` +
		`|\bargv\s*\[` +
		`|\b(?:recv|recvfrom|recvmsg)\s*\(`,
)

// ---- sink recognition (aligned with effect_sinks_c_cpp.go write subset) ----

// dfCCPPDBWriteRe matches a libpqxx / mongocxx / soci / raw-C DB write. Each
// alternation captures the callee text in its own group (exactly one set per
// match). libpqxx `txn.exec(...)`, mongocxx `coll.insert_one(...)`, raw
// `sqlite3_exec(...)` / `PQexec(...)` / `mysql_query(...)`, plus a `system()` /
// `exec*` / `popen` command-exec sink (highest-value C/C++ data sink).
var dfCCPPDBWriteRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*\s*(?:\.|->)\s*(?:exec|exec0|exec1|exec_params|exec_params0|exec_params1|prepared))\s*\(` +
		`|\b([A-Za-z_][\w]*\s*(?:\.|->)\s*(?:insert_one|insert_many|update_one|update_many|replace_one|delete_one|delete_many))\s*\(` +
		`|\b((?:sqlite3_exec|PQexec|PQexecParams|mysql_query))\s*\(` +
		`|((?:^|[^.\w>])(?:system|popen|_popen|execl|execlp|execle|execv|execvp|execvpe))\s*\(`,
)

// dfCCPPRespRe matches a response-body emission whose argument carries taint.
// Crow `res.write(...)` / `res.set_body(...)`, Drogon / oat++ `callback(resp)`.
// A `res.body = <tainted>` member assignment is handled in csharp-style direct
// member-assign detection (dfCCPPRespAssignRe).
var dfCCPPRespRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*\s*(?:\.|->)\s*(?:write|set_body|send|end))\s*\(` +
		`|\b(callback)\s*\(`,
)

// dfCCPPRespAssignRe matches a `res.body = <rhs>` / `res->body = <rhs>` response
// member assignment. Group 1 = the response member receiver+field text.
var dfCCPPRespAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_][\w]*\s*(?:\.|->)\s*body)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfCCPPHTTPCallRe matches an outbound HTTP call carrying a tainted body. cpr
// `cpr::Post(...)` / `cpr::Put(...)` / `cpr::Patch(...)`, libcurl
// `curl_easy_setopt(h, CURLOPT_POSTFIELDS, ...)`.
var dfCCPPHTTPCallRe = regexp.MustCompile(
	`\b(cpr\s*::\s*(?:Post|Put|Patch|Delete))\s*\(` +
		`|\b(curl_easy_setopt)\s*\([^,]+,\s*CURLOPT_(?:POSTFIELDS|COPYPOSTFIELDS|POSTFIELDSIZE)\b`,
)

// dfCCPPSinkSpecs is the ordered sink table reused at every scan depth.
var dfCCPPSinkSpecs = []struct {
	re   *regexp.Regexp
	kind DataFlowSinkKind
}{
	{dfCCPPDBWriteRe, DataFlowSinkDBWrite},
	{dfCCPPRespRe, DataFlowSinkResponse},
	{dfCCPPHTTPCallRe, DataFlowSinkHTTPCall},
}

// ---- local binding recognition ----

// dfCCPPDeclAssignRe captures a C/C++ local declaration with initialiser
// `auto|Type name = rhs;` (group 1 = name, group 2 = rhs). The leading type
// token (or `auto`/`const`) is required; a bare `name = rhs` reassignment is
// matched by dfCCPPBareAssignRe. The `[^=]` guards against `==`. Reference /
// pointer / namespace / template type tokens are tolerated in the type slot.
// The type and the name MUST be separated by whitespace or a pointer/ref
// sigil, so a bare `id = rhs;` reassignment is NOT mis-parsed as a declaration
// (it falls through to dfCCPPBareAssignRe, which correctly drops taint).
var dfCCPPDeclAssignRe = regexp.MustCompile(
	`^\s*(?:const\s+)?(?:auto|[A-Za-z_][\w:<>,]*(?:\s*[*&,]\s*[A-Za-z_][\w:<>,]*)*)\s*[*&]?(?:\s+|\s*[*&]\s*)([A-Za-z_][\w]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

// dfCCPPBareAssignRe captures `name = rhs;` with no declared type (a
// reassignment), used to break taint when a tainted local is overwritten.
var dfCCPPBareAssignRe = regexp.MustCompile(
	`^\s*([A-Za-z_][\w]*)\s*=\s*([^=].*?)\s*;?\s*$`,
)

func sniffDataFlowCCPPEx(content string) DataFlowResult {
	if content == "" {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := ccppFuncBodies(content, lines)

	var res DataFlowResult
	for _, b := range bodies {
		ctx := ccppWalkCtx{
			origin:  b.Name,
			bodies:  bodies,
			lines:   lines,
			visited: map[string]bool{b.Name: true},
		}
		r := walkCCPPBody(ctx, b, map[string]taintInfo{})
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// continueDataFlowCCPP continues a bounded hop walk inside this file: it binds
// the tainted value into fnName's paramIndex-th parameter and walks. The
// returned flows' Function/SourceField/SourceLine are placeholders the links
// pass rewrites to the true origin handler.
func continueDataFlowCCPP(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult {
	if content == "" || hopsUsed >= DataFlowMaxHops {
		return DataFlowResult{}
	}
	lines := strings.Split(content, "\n")
	bodies := ccppFuncBodies(content, lines)
	callee := ccppBodyByName(bodies, fnName)
	if callee == nil {
		return DataFlowResult{}
	}
	param := ccppParamName(lines, callee.Start, paramIndex)
	if param == "" {
		return DataFlowResult{}
	}
	ctx := ccppWalkCtx{
		origin:   fnName, // placeholder; links pass rewrites
		field:    field,
		hopsUsed: hopsUsed,
		bodies:   bodies,
		lines:    lines,
		visited:  map[string]bool{fnName: true},
	}
	return walkCCPPBody(ctx, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
}

// ccppFuncBody is a function's line span (1-indexed, inclusive).
type ccppFuncBody struct {
	Name  string
	Start int // header line (the line carrying `name(`)
	End   int // line of the matching `}`
}

// ccppFuncBodies computes brace-balanced spans for every function header in the
// file, reusing the shared scanCCPPFuncHeaders set (the same header scanner the
// effect/taint substrate uses). Each header is balanced with jstsMatchBraceEnd
// and de-duplicated by start line.
func ccppFuncBodies(content string, lines []string) []ccppFuncBody {
	seen := map[int]bool{}
	var out []ccppFuncBody
	for _, h := range scanCCPPFuncHeaders(content) {
		start := h.Line
		if start < 1 || start > len(lines) || seen[start] {
			continue
		}
		end := jstsMatchBraceEnd(lines, start)
		if end == 0 {
			continue
		}
		seen[start] = true
		out = append(out, ccppFuncBody{Name: h.Name, Start: start, End: end})
	}
	return out
}

// ccppWalkCtx threads the bounded multi-hop walk's state. hopPath/visited are
// COPIED on each descent so sibling branches stay isolated.
type ccppWalkCtx struct {
	origin   string
	field    string
	srcLine  int
	hopsUsed int
	bodies   []ccppFuncBody
	lines    []string
	visited  map[string]bool
	hopPath  []string
}

// walkCCPPBody is the unified forward pass over a function body. The taint map
// is pre-seeded (a cross-file continuation) or empty (a handler that reads
// `req.body()` here, seeded by ccppTrackTaint at the read site).
func walkCCPPBody(ctx ccppWalkCtx, b ccppFuncBody, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	// Skip the header line itself (its params are the signature, not a
	// statement) so a parameter declaration isn't mistaken for a binding.
	for ln := b.Start + 1; ln <= b.End && ln <= len(ctx.lines); ln++ {
		line := ctx.lines[ln-1]

		ccppTrackTaint(tainted, line, ln, &ctx)

		res.Flows = append(res.Flows, ccppDirectSinks(ctx, ln, line, tainted)...)

		r := ccppFollowCalls(ctx, ln, line, tainted)
		res.Flows = append(res.Flows, r.Flows...)
		res.Boundaries = append(res.Boundaries, r.Boundaries...)
	}
	return res
}

// ccppReqAccessField returns (field, true) when rhs reads a request input,
// lifting the statically-known key when present (else "").
func ccppReqAccessField(rhs string) (string, bool) {
	m := dfCCPPReqAccessRe.FindStringSubmatch(rhs)
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

// ccppTrackTaint applies one line's binding effects to the taint map
// (last-write-wins): a `auto x = <request-source>` taints x; a `auto y =
// <tainted-ref>` propagates; a binding/reassignment to a non-source drops it.
func ccppTrackTaint(tainted map[string]taintInfo, line string, ln int, ctx *ccppWalkCtx) {
	apply := func(name, rhs string) {
		if name == "" || name == "return" {
			return
		}
		// A direct request-input read seeds taint (Crow `req.url_params.get`, …).
		if fld, ok := ccppReqAccessField(rhs); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			if ctx.srcLine == 0 {
				ctx.srcLine = ln
			}
			return
		}
		// Propagation from an already-tainted value.
		if fld, ok := ccppRHSSourceField(rhs, tainted); ok {
			tainted[name] = taintInfo{field: fld, line: ln}
			return
		}
		// Bound/assigned to a non-source → drop any prior taint.
		if _, was := tainted[name]; was {
			delete(tainted, name)
		}
	}
	if m := dfCCPPDeclAssignRe.FindStringSubmatch(line); m != nil {
		apply(m[1], m[2])
		return
	}
	if m := dfCCPPBareAssignRe.FindStringSubmatch(line); m != nil {
		apply(m[1], m[2])
	}
}

// ccppRHSSourceField returns (field, true) when rhs derives from a tainted
// value (a reference to a tainted root, optionally via a member), preserving
// provenance.
func ccppRHSSourceField(rhs string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(rhs) {
			return ccppTaintedField(rhs, name, info), true
		}
	}
	return "", false
}

// dfCCPPMemberRe captures `<root>.member` / `<root>->member` — group 1 = root,
// group 2 = member.
var dfCCPPMemberRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*(?:\.|->)\s*([A-Za-z_][\w]*)\b`)

// ccppTaintedField resolves the source field for a reference to tainted root
// `name` as it appears in expr. A known field always wins; otherwise a direct
// member access off the root (`dto.email` / `dto->email`) is lifted as the
// field. A method call (`dto.build()`) is NOT a field — it is skipped.
func ccppTaintedField(expr, name string, info taintInfo) string {
	if info.field != "" {
		return info.field
	}
	for _, m := range dfCCPPMemberRe.FindAllStringSubmatchIndex(expr, -1) {
		root := expr[m[2]:m[3]]
		if root != name {
			continue
		}
		member := expr[m[4]:m[5]]
		// Skip a method call `dto.build(...)` — a member field has no `(`.
		rest := strings.TrimLeft(expr[m[5]:], " \t")
		if strings.HasPrefix(rest, "(") {
			continue
		}
		return member
	}
	return ""
}

// ccppDirectSinks emits flows for sinks on `line` whose args carry taint,
// including a `res.body = <tainted>` response member assignment.
func ccppDirectSinks(ctx ccppWalkCtx, ln int, line string, tainted map[string]taintInfo) []DataFlow {
	var out []DataFlow
	for _, s := range dfCCPPSinkSpecs {
		for _, m := range s.re.FindAllStringSubmatchIndex(line, -1) {
			callee := ccppSinkCallee(line, m)
			if callee == "" {
				continue
			}
			open := strings.Index(line[m[0]:], "(")
			if open < 0 {
				continue
			}
			args := jstsCallArgs(ctx.lines, ln, m[0]+open)
			if fld, ok := ccppExprTainted(args, tainted); ok {
				out = append(out, ccppMakeFlow(ctx, fld, s.kind, normalizeCCPPCallee(callee), ln))
			}
		}
	}
	// `res.body = <tainted>;` is a response-body emission.
	if m := dfCCPPRespAssignRe.FindStringSubmatch(line); m != nil {
		if fld, ok := ccppArgBareTaint(m[2], tainted); ok {
			out = append(out, ccppMakeFlow(ctx, fld, DataFlowSinkResponse, normalizeCCPPCallee(m[1]), ln))
		}
	}
	return out
}

// ccppMakeFlow builds a DataFlow, preferring an already-carried field (from a
// cross-file continuation) over the locally-derived one.
func ccppMakeFlow(ctx ccppWalkCtx, fld string, kind DataFlowSinkKind, sink string, ln int) DataFlow {
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

// ccppSinkCallee returns the non-empty captured callee text from a sink match
// (the sink regexes use alternation groups; exactly one is set per match).
func ccppSinkCallee(line string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 && m[i+1] >= 0 {
			return line[m[i]:m[i+1]]
		}
	}
	return ""
}

// normalizeCCPPCallee collapses internal whitespace around `.` / `->` and trims
// a leading non-identifier byte the command-exec alternation may capture
// (`(?:^|[^.\w>])system` keeps the boundary char).
func normalizeCCPPCallee(s string) string {
	s = strings.TrimSpace(s)
	s = dfCCPPArrowSpaceRe.ReplaceAllString(s, "->")
	s = dfCCPPDotSpaceRe.ReplaceAllString(s, ".")
	// Strip a single leading boundary char (e.g. `;system` → `system`).
	if len(s) > 0 {
		c := s[0]
		if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			s = strings.TrimLeft(s, " \t;,(){}=+-*/&|")
		}
	}
	return strings.TrimSpace(s)
}

var dfCCPPArrowSpaceRe = regexp.MustCompile(`\s*->\s*`)
var dfCCPPDotSpaceRe = regexp.MustCompile(`\s*\.\s*`)

// ccppFollowCalls handles each local-call on `line`: recurse into a same-file
// function (bounded + cycle-guarded) or record a cross-file boundary. Position
// binding is EXACT — an ambiguous arg drops (honest-partial).
func ccppFollowCalls(ctx ccppWalkCtx, ln int, line string, tainted map[string]taintInfo) DataFlowResult {
	var res DataFlowResult
	for _, call := range ccppLocalCalls(line) {
		for pos, argExpr := range call.args {
			fld, bare := ccppArgBareTaint(argExpr, tainted)
			if !bare {
				continue
			}
			field := ctx.field
			if field == "" {
				field = fld
			}
			callee := ccppBodyByName(ctx.bodies, call.name)
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
			param := ccppParamName(ctx.lines, callee.Start, pos)
			if param == "" {
				continue
			}
			child := ctx
			child.hopPath = append(dupStrings(ctx.hopPath), callee.Name)
			child.visited = dupVisited(ctx.visited)
			child.visited[callee.Name] = true
			child.field = field
			r := walkCCPPBody(child, *callee, map[string]taintInfo{param: {field: field, line: callee.Start}})
			res.Flows = append(res.Flows, r.Flows...)
			res.Boundaries = append(res.Boundaries, r.Boundaries...)
		}
	}
	return res
}

// ccppExprTainted reports whether expr references a tainted variable, returning
// the field when derivable.
func ccppExprTainted(expr string, tainted map[string]taintInfo) (string, bool) {
	for name, info := range tainted {
		if dfReWholeIdent(name).MatchString(expr) {
			return ccppTaintedField(expr, name, info), true
		}
	}
	return "", false
}

// dfCCPPTaintedMemberWholeRe matches an expr that is SOLELY `ident.member` or
// `ident->member`. Group 1 = root, group 2 = member.
var dfCCPPTaintedMemberWholeRe = regexp.MustCompile(`^([A-Za-z_][\w]*)\s*(?:\.|->)\s*([A-Za-z_][\w]*)$`)

// dfCCPPCtorRe matches an object-construction argument `Type{...}` / `Type(...)`
// (capitalised or namespaced) so a tainted value wrapped in entity construction
// (`coll.insert_one(make_document(dto.email))`) is recognised and the field
// lifted. A leading `make_` builder (mongocxx bsoncxx) is also admitted.
var dfCCPPCtorRe = regexp.MustCompile(`^(?:[A-Za-z_][\w:]*\s*)?(?:[A-Z][\w:]*|make_[\w]+)\s*[({]`)

// ccppArgBareTaint reports whether argExpr is EXACTLY a tainted value — a bare
// tainted identifier, a member access off a tainted root (`dto.email` /
// `dto->email`), or a constructor / builder wrapping a tainted leaf
// (`User(dto.email)` / `make_document(... dto.email ...)`) — not embedded in
// arithmetic or string concatenation. Precision guard for sound positional
// binding.
func ccppArgBareTaint(argExpr string, tainted map[string]taintInfo) (string, bool) {
	e := strings.TrimSpace(argExpr)
	if e == "" {
		return "", false
	}
	// A constructor / builder wrapping a tainted value.
	if dfCCPPCtorRe.MatchString(e) {
		open := strings.IndexAny(e, "({")
		if open >= 0 {
			last := e[len(e)-1]
			if last == ')' || last == '}' {
				inner := e[open+1 : len(e)-1]
				for _, part := range jstsSplitArgs(inner) {
					if fld, ok := ccppArgBareTaint(part, tainted); ok {
						return fld, true
					}
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
	// `dto.email` / `dto->email` — direct member off a tainted root.
	if m := dfCCPPTaintedMemberWholeRe.FindStringSubmatch(e); m != nil {
		if info, ok := tainted[m[1]]; ok {
			if info.field != "" {
				return info.field, true
			}
			return m[2], true
		}
	}
	return "", false
}

// ccppLocalCall is a parsed `name(arg0, arg1, …)` call to a bare identifier.
type ccppLocalCall struct {
	name string
	args []string
}

// dfCCPPLocalCallRe matches a call to a bare identifier (potential local fn).
var dfCCPPLocalCallRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\s*\(`)

// ccppLocalCalls extracts candidate bare-identifier calls on a line with their
// top-level positional argument expressions. Member calls (`obj.foo(` /
// `obj->foo(`), capitalised constructors, namespaced calls (`cpr::Post`), and
// control / sink keywords are skipped — only a lower-case bare-identifier call
// is a hop / boundary candidate.
func ccppLocalCalls(line string) []ccppLocalCall {
	var out []ccppLocalCall
	for _, m := range dfCCPPLocalCallRe.FindAllStringSubmatchIndex(line, -1) {
		name := line[m[2]:m[3]]
		if name == "" {
			continue
		}
		// A Capitalised name is a constructor / type — handled as a sink arg.
		if name[0] >= 'A' && name[0] <= 'Z' {
			continue
		}
		if m[2] > 0 {
			prev := strings.TrimRight(line[:m[2]], " \t")
			// Member call (`obj.foo(` / `obj->foo(`) or namespaced (`ns::foo(`).
			if strings.HasSuffix(prev, ".") || strings.HasSuffix(prev, ">") || strings.HasSuffix(prev, ":") {
				continue
			}
		}
		if ccppControlKeyword(name) || ccppSinkKeyword(name) {
			continue
		}
		args := jstsSplitArgs(jstsCallArgs([]string{line}, 1, m[2]))
		out = append(out, ccppLocalCall{name: name, args: args})
	}
	return out
}

// ccppSinkKeyword reports whether name is a bare-identifier sink the sink table
// owns (a command-exec / response primitive) — it must not be treated as a
// local-function hop / boundary.
func ccppSinkKeyword(name string) bool {
	switch name {
	case "system", "popen", "_popen", "execl", "execlp", "execle",
		"execv", "execvp", "execvpe", "callback", "sqlite3_exec",
		"PQexec", "PQexecParams", "mysql_query", "curl_easy_setopt":
		return true
	}
	return false
}

// ccppParamName returns the name of the pos-th positional parameter of the
// function whose header is on headerLine. C/C++ parameters are `Type name`
// (possibly const / ref / pointer / template / default-valued); the trailing
// identifier before a `,`/`)`/`=` is the name. A variadic `...` parameter makes
// positions past it unreliable → "".
func ccppParamName(lines []string, headerLine, pos int) string {
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
			return "" // variadic — ambiguous positions
		}
	}
	return ccppParamIdent(params[pos])
}

// dfCCPPParamIdentRe captures the parameter identifier — the trailing token
// before an optional `= default` — after the (possibly const/ref/pointer/
// template) type. A trailing `[]` array suffix is tolerated.
var dfCCPPParamIdentRe = regexp.MustCompile(`([A-Za-z_][\w]*)\s*(?:\[\s*\])?\s*(?:=[^,]*)?\s*$`)

// ccppParamIdent extracts the parameter name from one parameter declaration,
// stripping the type token and a trailing default value. A bare type with no
// name (`void`, `int`) returns "" — that position cannot be bound.
func ccppParamIdent(decl string) string {
	d := strings.TrimSpace(decl)
	if d == "" || d == "void" {
		return ""
	}
	m := dfCCPPParamIdentRe.FindStringSubmatch(d)
	if m == nil {
		return ""
	}
	// A single-token decl with no whitespace is a bare type (`int`) → no name.
	name := m[1]
	if strings.TrimSpace(strings.TrimSuffix(d, name)) == "" && !strings.ContainsAny(d, " \t*&") {
		// `id` alone could be a name in a K&R-style or macro; but with no type
		// token it is ambiguous — drop for soundness.
		return ""
	}
	return name
}

// ccppBodyByName returns the body with the given name, or nil.
func ccppBodyByName(all []ccppFuncBody, name string) *ccppFuncBody {
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
