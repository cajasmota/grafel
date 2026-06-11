// Branch inventory facet (#4423, epic #4419 capability 4).
//
// The effect substrate (effects.go + effect_sinks_*.go) collapses a branchy
// function to a SET of effect KINDS — it tells a porting/audit agent THAT a
// function does db_write + http_out, but not WHICH branches exist, what each
// one returns, or which env var gates which path. For a faithful port the
// agent must reproduce every exception handler, early-return guard, and
// env-conditional with its exact outcome.
//
// This file adds a second consumer of the same per-function source window the
// effect sniffers walk: a branch classifier that enumerates the control-flow
// decision points of a single function and classifies each one's OUTCOME
// (raise / return a value / redirect / swallow). It is opt-in — the effects
// MCP tool only computes it when include="branches" is requested — so the
// default effects output is byte-for-byte unchanged (no regression risk).
//
// Design: this is a long-term, general CFG-shaped walk, NOT a two-function
// pattern match. It tracks indentation to scope `if`/`except`/`elif` blocks
// to a function body and inspects each block's immediate statements to decide
// the outcome. Python is the flagship (the oracle stack); the BranchFacet
// schema and the outcome lattice are language-neutral so other languages can
// register their own classifier (see BranchAnalyzerFor; the JS/TS + Java + Go
// classifiers live in branches_jsts_java_go.go, added in #4434, ref epic
// #4419).
package substrate

import (
	"regexp"
	"sort"
	"strings"
)

// BranchKind names the syntactic shape of a control-flow decision point.
type BranchKind string

const (
	// BranchExcept is an exception handler (`except E:` / `catch (e)` /
	// `rescue E`). Its condition is the caught exception type(s).
	BranchExcept BranchKind = "except"
	// BranchEarlyReturn is a guard that returns/raises out of the function
	// before the main body (`if <cond>: return ...`). The canonical
	// validation guard (`if not email_availability: return 409`).
	BranchEarlyReturn BranchKind = "early_return"
	// BranchEnvGate is an early_return/guard whose condition READS an
	// environment / settings variable (`if not settings.PROD: ...`). It is
	// surfaced as its own kind because env-coupling is the single most
	// important thing a porting agent must reproduce verbatim — a missed
	// env-gate silently changes behaviour between environments.
	BranchEnvGate BranchKind = "env_gate"
	// BranchGuard is a conditional that alters control flow (raise/return)
	// but is neither the leading early-return nor an env-gate — e.g. a mid
	// body `if user is None: return ...`.
	BranchGuard BranchKind = "guard"
)

// BranchOutcome is what a branch DOES when taken.
type BranchOutcome string

const (
	// OutcomeRaise — the branch raises/throws an exception.
	OutcomeRaise BranchOutcome = "raise"
	// OutcomeReturnValue — the branch returns a value (often a Response /
	// HTTP status).
	OutcomeReturnValue BranchOutcome = "return_value"
	// OutcomeRedirect — the branch redirects / reroutes (HttpResponseRedirect,
	// redirect(), res.redirect(), 3xx Location).
	OutcomeRedirect BranchOutcome = "redirect"
	// OutcomeSwallow — catch-and-continue: an except handler that logs /
	// passes / continues WITHOUT re-raising or returning. The classic
	// silent-failure path an audit must flag.
	OutcomeSwallow BranchOutcome = "swallow"
)

// BranchReturns carries the cheaply-derivable shape of a return_value/raise
// branch: an HTTP status code when one is evident in the returned expression,
// and a brief shape descriptor (e.g. "Response{error}").
type BranchReturns struct {
	// Status is the HTTP status code (e.g. "409", "200") when the branch's
	// return expression names one (status=status.HTTP_409_CONFLICT,
	// status=409, NewToolResultError, res.status(500)). Empty otherwise.
	Status string `json:"status,omitempty"`
	// Shape is a short descriptor of the returned payload's shape, e.g.
	// "Response{error}" or "dict{success}". Empty when not cheaply derivable.
	Shape string `json:"shape,omitempty"`
}

// BranchFacet is one enumerated branch in a function. It is the unit the
// `branches` facet returns. JSON shape is the public contract consumed by the
// effects MCP tool.
type BranchFacet struct {
	// Kind is the syntactic shape (except / early_return / env_gate / guard).
	Kind BranchKind `json:"kind"`
	// Condition is the verbatim guard/handler condition text, e.g.
	// "except Exception as e", "if not settings.PROD",
	// "if email_availability['available'] is False and not upsert_flag".
	Condition string `json:"condition"`
	// Outcome is what taking the branch does (raise / return_value /
	// redirect / swallow).
	Outcome BranchOutcome `json:"outcome"`
	// Returns carries the status + shape for return_value/raise branches.
	// Nil when nothing was cheaply derivable.
	Returns *BranchReturns `json:"returns,omitempty"`
	// EnvVar is the environment / settings variable name read by the branch
	// condition (e.g. "ECB_PDF_PIPELINE_ENABLED", "PROD", "API_KEY") when the
	// condition reads settings.* / os.environ[...] / process.env.*. Empty
	// otherwise.
	EnvVar string `json:"env_var,omitempty"`
	// Line is the 1-indexed source line of the branch header, relative to the
	// file the function lives in (the analyzer is given absolute line numbers
	// so callers can cross-reference get_source output).
	Line int `json:"line"`
}

// BranchAnalyzerFn is the per-language contract. Input: the raw source of a
// SINGLE function body window plus the 1-indexed file line at which that
// window starts (so emitted Line values are absolute). Output: every branch
// in source order. Stateless and pure — identical input yields identical
// output so graph/MCP output stays deterministic.
type BranchAnalyzerFn func(funcSource string, startLine int) []BranchFacet

var branchRegistry = map[string]BranchAnalyzerFn{}

// RegisterBranchAnalyzer installs a per-language branch analyzer. Mirrors
// RegisterEffectSniffer so a language can ship branch classification
// independently of its effect sniffer.
func RegisterBranchAnalyzer(lang string, fn BranchAnalyzerFn) {
	if lang == "" || fn == nil {
		return
	}
	branchRegistry[lang] = fn
}

// BranchAnalyzerFor returns the per-language branch analyzer, or nil when none
// is registered (the caller then omits the facet — honest-partial: absence of
// a classifier is reported as "not yet supported", never as "no branches").
func BranchAnalyzerFor(lang string) BranchAnalyzerFn {
	return branchRegistry[lang]
}

// BranchLanguages returns the slugs of every registered branch analyzer in
// sorted order.
func BranchLanguages() []string {
	out := make([]string, 0, len(branchRegistry))
	for k := range branchRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func init() { RegisterBranchAnalyzer("python", analyzeBranchesPython) }

// braceLangs is the set of brace-delimited languages whose function body is
// bounded by a balanced `{`…`}` block. ClampToFunctionBody uses braceBodyEnd
// for these; Python uses bodyEndPython and ruby (`end`-delimited) uses
// bodyEndRuby. Languages absent from every family are not clamped at the source
// level yet — see ClampToFunctionBody.
var braceLangs = map[string]bool{
	"jsts": true, "java": true, "go": true, "php": true,
	"csharp": true, "kotlin": true, "scala": true, "rust": true,
}

// ClampToFunctionBody trims a (possibly padded) source window down to the
// SINGLE function/method body that begins at its first non-blank header line,
// so a branch analyzer never bleeds into the sibling defs that follow.
//
// This is the language-general companion to the per-analyzer Python clamp
// (bodyEndPython): the effects MCP tool pads StartLine+N when an entity's
// EndLine is missing/zero (#4488/#4666), and EVERY brace-language analyzer
// (jsts/java/go/php/csharp/kotlin/scala/rust) walks its whole input window —
// so without this clamp the padded tail's sibling methods are mis-attributed
// to the target method. Clamping ONCE at the source level fixes all current
// and future analyzers uniformly.
//
// Behaviour by language family:
//   - Python: clamp to the dedent boundary (bodyEndPython).
//   - Brace languages: clamp to the matching `}` of the header's own block.
//   - Ruby (`end`-delimited): clamp to the `end` closing the method's `def`
//     (bodyEndRuby).
//   - Anything else: returned unchanged — honest no-op until a body-end
//     detector is implemented for it.
//
// It is idempotent and safe on an already-tight window: an exact single-method
// body has no trailing sibling, so the boundary is len(lines) and the source is
// returned unchanged.
func ClampToFunctionBody(src, lang string) string {
	if strings.TrimSpace(src) == "" {
		return src
	}
	lines := strings.Split(src, "\n")
	var end int
	switch {
	case lang == "python":
		end = bodyEndPython(lines)
	case braceLangs[lang]:
		end = braceBodyEnd(lines)
	case lang == "ruby":
		end = bodyEndRuby(lines)
	default:
		return src // no body-end detector for this language yet
	}
	if end >= len(lines) {
		return src
	}
	return strings.Join(lines[:end], "\n")
}

// --- Python branch analyzer ---------------------------------------------
//
// Walk model: Python's significant indentation makes a line-oriented walk a
// faithful CFG for branch enumeration. For each `except`/`elif`/`if` header we
// determine the body block as the run of following lines indented deeper than
// the header, then classify the FIRST control-altering statement in that block
// (return / raise / redirect / log-and-continue). This is general — it keys on
// statement shape, not on the two oracle functions.

var (
	pyExceptRe = regexp.MustCompile(`^(\s*)(except\b[^:]*):`)
	pyIfRe     = regexp.MustCompile(`^(\s*)((?:el)?if\b.*?):\s*(.*)$`)

	// pyReturnRe / pyRaiseRe detect the block's control-altering statement.
	pyReturnStmtRe = regexp.MustCompile(`^\s*return\b\s*(.*)$`)
	pyRaiseStmtRe  = regexp.MustCompile(`^\s*raise\b`)
	pyContinueRe   = regexp.MustCompile(`^\s*(continue|pass|break)\b`)

	// pyRedirectRe — redirect outcomes.
	pyRedirectRe = regexp.MustCompile(`\b(?:HttpResponseRedirect|HttpResponsePermanentRedirect|redirect)\s*\(|\bRedirect\b`)

	// pyEnvRefRe captures the env/settings var name a condition reads.
	// Group order tried: settings.NAME, os.environ["NAME"]/.get("NAME"),
	// os.getenv("NAME"), getenv("NAME").
	pyEnvRefRe = regexp.MustCompile(
		`\bsettings\s*\.\s*([A-Za-z_][\w]*)` +
			`|\bos\s*\.\s*environb?\s*(?:\[\s*['"]([^'"]+)['"]\s*\]|\.\s*get\s*\(\s*['"]([^'"]+)['"])` +
			`|\b(?:os\s*\.\s*)?getenv\s*\(\s*['"]([^'"]+)['"]`,
	)

	// pyHTTPStatusRe extracts an HTTP status from a return/raise expression:
	// status.HTTP_409_CONFLICT, status=409, HTTP_200_OK, and the FastAPI
	// HTTPException(status_code=404, ...) / JSONResponse(status_code=201) form
	// (#4709 — the `status_code=NNN` kwarg the FastAPI/Starlette stack uses).
	pyHTTPStatusConstRe = regexp.MustCompile(`HTTP_(\d{3})_[A-Z_]+`)
	pyHTTPStatusNumRe   = regexp.MustCompile(`status(?:_code)?\s*=\s*(\d{3})\b`)
)

func analyzeBranchesPython(funcSource string, startLine int) []BranchFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	lines := strings.Split(funcSource, "\n")
	// Scope the walk to the method's OWN body. The source window the effects
	// tool hands us starts at the entity's StartLine (the `def` header) but its
	// EndLine can be missing/zero — in which case branchSourceSpan pads a
	// generous tail that overshoots into the SIBLING methods/defs that follow
	// (the #4419/#4488 over-capture: create_contact's branch list bled into
	// update_contact and beyond). bodyEndPython finds the method's dedent
	// boundary — the first non-blank line indented at or below the `def`
	// header's own indent (a sibling `def`, a decorator, or the class body) —
	// and we walk only up to there. This is self-correcting: even a 400-line
	// padded window is clamped to exactly this method's body.
	bodyEnd := bodyEndPython(lines)
	var out []BranchFacet
	// firstGuardSeen lets us label the LEADING guard as early_return and
	// later same-shape guards as guard (mid-body). env-gates override both.
	firstGuardSeen := false

	for i := 0; i < bodyEnd; i++ {
		raw := lines[i]
		if strings.TrimSpace(raw) == "" {
			continue
		}
		absLine := startLine + i

		// except handler.
		if m := pyExceptRe.FindStringSubmatch(raw); m != nil {
			indent := m[1]
			cond := strings.TrimSpace(m[2])
			body := pyBlockBody(lines, i, len(indent))
			outcome := classifyPyExceptOutcome(body)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachPyReturns(&bf, body)
			out = append(out, bf)
			continue
		}

		// if / elif guard.
		if m := pyIfRe.FindStringSubmatch(raw); m != nil {
			indent := m[1]
			cond := strings.TrimSpace(m[2])
			inline := strings.TrimSpace(m[3]) // `if x: return y` single-line form
			var body []string
			if inline != "" {
				body = []string{inline}
			} else {
				body = pyBlockBody(lines, i, len(indent))
			}
			outcome, alters := classifyPyGuardOutcome(body)
			if !alters {
				// A plain branching `if` that neither returns nor raises is
				// not a porting-critical branch for this facet; skip it so we
				// don't drown the agent in every conditional.
				continue
			}
			envVar := pyEnvVar(cond)
			kind := BranchGuard
			switch {
			case envVar != "":
				kind = BranchEnvGate
			case !firstGuardSeen:
				kind = BranchEarlyReturn
			}
			firstGuardSeen = true
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachPyReturns(&bf, body)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// bodyEndPython returns the exclusive line index at which the method/function
// whose `def` header is the first non-blank line of `lines` ends — i.e. the
// index of the first line AFTER the header that dedents to or below the
// header's own indentation (a sibling `def`/`class`, a trailing decorator, or
// the enclosing class/module body). When the window is exactly the method body
// (no trailing siblings) this is len(lines). This is the single source of truth
// for the method-boundary scope: it stops the branch walk from bleeding into
// adjacent `@action` methods when the entity's EndLine is missing and the
// source window was padded.
//
// It is robust to leading decorators (`@action(...)`) preceding the `def`: the
// header is the first `def`/`async def` line, and its indent — not a
// decorator's — is the boundary indent.
func bodyEndPython(lines []string) int {
	// Locate the def header and record its indent. Leading decorator lines
	// (`@action(...)`, possibly multi-line) precede the `def` in the source
	// window — skip them so the boundary indent is the `def`'s own, never a
	// decorator's. We only fall back to a bare-body anchor (first non-blank,
	// non-decorator line) when there is NO def at all in the window.
	headerIdx := -1
	headerIndent := 0
	bareIdx := -1
	bareIndent := 0
	inDecorator := false
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if pyFuncDefRe.MatchString(ln) {
			headerIdx = i
			headerIndent = leadingWS(ln)
			break
		}
		// Track (possibly multi-line) decorators so their continuation lines
		// aren't mistaken for the bare-body anchor.
		trimmed := strings.TrimSpace(ln)
		if inDecorator {
			inDecorator = !decoratorComplete(trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			inDecorator = !decoratorComplete(trimmed)
			continue
		}
		// First real (non-decorator) non-blank line and still no def → remember
		// it as the bare-body anchor, but keep scanning in case a def follows.
		if bareIdx < 0 {
			bareIdx = i
			bareIndent = leadingWS(ln)
		}
	}
	if headerIdx < 0 {
		// No def in the window — anchor on the first bare-body line so we still
		// clamp at the first dedent below it. No anchor at all → whole window.
		if bareIdx < 0 {
			return len(lines)
		}
		headerIdx = bareIdx
		headerIndent = bareIndent
	}
	for j := headerIdx + 1; j < len(lines); j++ {
		ln := lines[j]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if leadingWS(ln) <= headerIndent {
			return j
		}
	}
	return len(lines)
}

// pyFuncDefRe matches a Python function/method header (`def f(` /
// `async def f(`), ignoring leading indentation.
var pyFuncDefRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+[A-Za-z_]\w*\s*\(`)

// decoratorComplete reports whether a decorator line that has been seen so far
// (joined trimmed text) is syntactically complete — i.e. it opened no
// parenthesis, or every `(` it opened has been closed. A multi-line
// `@shared_task(\n  ...\n)` decorator is incomplete until the closing `)`.
// Cheap paren-balance, sufficient for the well-formed windows the effects
// pipeline feeds in (decorator args rarely embed unbalanced parens in strings).
func decoratorComplete(trimmed string) bool {
	depth := 0
	for _, r := range trimmed {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth <= 0
}

// pyBlockBody returns the statements belonging to the block whose header is at
// lines[headerIdx] with indentation headerIndent — i.e. the run of subsequent
// non-blank lines indented strictly deeper than the header, stopping at the
// first line indented ≤ headerIndent.
func pyBlockBody(lines []string, headerIdx, headerIndent int) []string {
	var body []string
	for j := headerIdx + 1; j < len(lines); j++ {
		ln := lines[j]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if leadingWS(ln) <= headerIndent {
			break
		}
		body = append(body, ln)
	}
	return body
}

// classifyPyGuardOutcome inspects an if/elif block body and returns its
// outcome + whether it alters control flow (return/raise/redirect). A guard
// that does none of these is not surfaced.
func classifyPyGuardOutcome(body []string) (BranchOutcome, bool) {
	for _, ln := range body {
		if pyRaiseStmtRe.MatchString(ln) {
			return OutcomeRaise, true
		}
		if pyReturnStmtRe.MatchString(ln) {
			if pyRedirectRe.MatchString(strings.Join(body, "\n")) {
				return OutcomeRedirect, true
			}
			return OutcomeReturnValue, true
		}
		if pyRedirectRe.MatchString(ln) {
			return OutcomeRedirect, true
		}
	}
	return "", false
}

// classifyPyExceptOutcome classifies an except handler. An except that
// re-raises or returns is raise/return_value; one that only logs / passes /
// continues is a SWALLOW (catch-and-continue) — the audit-critical case.
func classifyPyExceptOutcome(body []string) BranchOutcome {
	for _, ln := range body {
		if pyRaiseStmtRe.MatchString(ln) {
			return OutcomeRaise
		}
		if pyReturnStmtRe.MatchString(ln) {
			if pyRedirectRe.MatchString(strings.Join(body, "\n")) {
				return OutcomeRedirect
			}
			return OutcomeReturnValue
		}
		if pyRedirectRe.MatchString(ln) {
			return OutcomeRedirect
		}
	}
	// No re-raise / return / redirect anywhere in the handler → swallow.
	return OutcomeSwallow
}

// pyEnvVar returns the env/settings variable name a condition reads, or "".
func pyEnvVar(cond string) string {
	m := pyEnvRefRe.FindStringSubmatch(cond)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// attachPyReturns derives returns.status + returns.shape from a branch body
// for return_value/raise branches. Only attaches when something is found.
func attachPyReturns(bf *BranchFacet, body []string) {
	if bf.Outcome != OutcomeReturnValue && bf.Outcome != OutcomeRaise && bf.Outcome != OutcomeRedirect {
		return
	}
	joined := strings.Join(body, "\n")
	r := &BranchReturns{}
	if m := pyHTTPStatusConstRe.FindStringSubmatch(joined); m != nil {
		r.Status = m[1]
	} else if m := pyHTTPStatusNumRe.FindStringSubmatch(joined); m != nil {
		r.Status = m[1]
	}
	if shape := pyReturnShape(body); shape != "" {
		r.Shape = shape
	}
	if r.Status != "" || r.Shape != "" {
		bf.Returns = r
	}
}

// pyReturnShape produces a brief descriptor of the returned payload. For a DRF
// `Response({...}, ...)` it reports `Response{key1,key2}`; for a bare dict it
// reports `dict{...}`. Best-effort and cheap — empty when not derivable.
func pyReturnShape(body []string) string {
	joined := strings.Join(body, " ")
	// Wrapper call name immediately before a "(" then a "{".
	wrapper := ""
	if idx := strings.Index(joined, "Response("); idx >= 0 {
		wrapper = "Response"
	}
	// First brace-delimited dict literal keys.
	keys := pyDictKeys(joined)
	switch {
	case wrapper != "" && len(keys) > 0:
		return wrapper + "{" + strings.Join(keys, ",") + "}"
	case wrapper != "":
		return wrapper
	case len(keys) > 0:
		return "dict{" + strings.Join(keys, ",") + "}"
	}
	return ""
}

func pyDictKeys(s string) []string {
	open := strings.Index(s, "{")
	if open < 0 {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	for _, m := range pyDictKeyRe.FindAllStringSubmatch(s[open:], -1) {
		k := m[1]
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		if len(keys) >= 4 { // bound the descriptor
			break
		}
	}
	return keys
}
