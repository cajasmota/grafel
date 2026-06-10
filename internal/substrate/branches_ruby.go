// Branch inventory analyzer for Ruby — begin/rescue, modifier + block guards,
// env-gates, render-status returns (#4444, extends #4423/#4435/#4434, epic
// #4419 capability 4).
//
// branches.go added the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer; branches_jsts_
// java_go.go generalized it to the three brace-delimited languages via the
// shared braceBlockBody walker. Ruby is neither indentation-scoped (Python) nor
// brace-delimited (JS/TS/Java/Go): its blocks open with a keyword (`begin`,
// `def`, `if`, `do`, `case`, ...) and close with a matching `end`. So this file
// adds a Ruby-local end-keyword-depth walker (endBlockBody) instead of reusing
// braceBlockBody, while REUSING the shared outcome lattice, the guardKind /
// matchEnvVar / httpStatusNameToCode helpers, and the same opt-in contract.
//
// Ruby control-flow shapes classified:
//   - `begin ... rescue E => e ... end` — the rescue handler body. `raise`
//     re-raise → raise; `return`/a value-yielding `render`/`head` → return_value
//     /redirect; a log-only / bare body → swallow (the audit-critical silent
//     failure path), mirroring the Python except classifier.
//   - leading guards in both forms: the trailing-modifier `return/raise unless
//     COND` & `... if COND`, and the block `if/unless COND ... return ... end`.
//   - env-gates: a guard whose condition reads `ENV['X']` / `ENV.fetch('X')` /
//     `Rails.env` is surfaced as env_gate with env_var set.
//   - returns.status from `render status: :unprocessable_entity` /
//     `render status: 409` / `head 404` / `head :not_found` / `redirect_to`.
//
// Opt-in contract is unchanged: this runs only when the effects MCP tool is
// called with include="branches", so the default effects payload is
// byte-for-byte unchanged. Classification is conservative — a branch is only
// surfaced when it provably alters control flow (raise / return / render /
// head / redirect); plain branching `if`s are skipped.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterBranchAnalyzer("ruby", analyzeBranchesRuby) }

var (
	// rubyRescueRe — a `rescue` clause header. Captures the exception
	// type/binding text after the keyword (`StandardError => e`, `=> e`, ``).
	rubyRescueRe = regexp.MustCompile(`^(\s*)rescue\b\s*(.*)$`)

	// rubyBlockGuardRe — a block-form leading guard: `if COND` / `unless COND`
	// at the start of a statement (NOT a trailing modifier, which has code
	// before the keyword). Captures the keyword and the condition.
	rubyBlockGuardRe = regexp.MustCompile(`^\s*(if|unless)\b\s+(.*?)\s*(?:then)?\s*$`)

	// rubyModifierGuardRe — the trailing-modifier guard form Ruby idiom prefers:
	// `return X if COND` / `raise Y unless COND` / `head 404 if COND`. Captures
	// the statement (group 1), the keyword (group 2), and the condition (3).
	rubyModifierGuardRe = regexp.MustCompile(`^\s*(return\b.*?|raise\b.*?|render\b.*?|head\b.*?|redirect_to\b.*?)\s+(if|unless)\s+(.*?)\s*$`)

	// control-altering statement detectors inside a block/handler body.
	rubyRaiseRe    = regexp.MustCompile(`(^|\b)raise\b|\bfail\b`)
	rubyReturnRe   = regexp.MustCompile(`(^|\b)return\b`)
	rubyRenderRe   = regexp.MustCompile(`\brender\b|\bhead\b`)
	rubyRedirectRe = regexp.MustCompile(`\bredirect_to\b|\bredirect\b`)

	// rubyEnvRefRe — ENV['X'] / ENV["X"] / ENV.fetch('X') / ENV.fetch("X",..) /
	// Rails.env (surfaced as the symbolic env name "Rails.env").
	rubyEnvRefRe = regexp.MustCompile(
		`\bENV\s*\[\s*['"]([^'"]+)['"]\s*\]` +
			`|\bENV\s*\.\s*fetch\s*\(\s*['"]([^'"]+)['"]` +
			`|\b(Rails\.env)\b`)

	// rubyStatusSymRe — `status: :unprocessable_entity` / `head :not_found` /
	// `render status: :ok`. The Rails status symbol → numeric code.
	rubyStatusSymRe = regexp.MustCompile(`(?:status\s*:\s*|head\s+):([a-z_]+)\b`)
	// rubyStatusNumRe — `status: 409` / `head 404` / `head(500)`.
	rubyStatusNumRe = regexp.MustCompile(`(?:status\s*:\s*|head\s*\(?\s*)(\d{3})\b`)
)

func analyzeBranchesRuby(funcSource string, startLine int) []BranchFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	lines := strings.Split(funcSource, "\n")
	var out []BranchFacet
	firstGuardSeen := false

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		if strings.TrimSpace(raw) == "" {
			continue
		}
		absLine := startLine + i

		// rescue handler — its body is the run of statements until the next
		// rescue / else / ensure / end at the same `begin`/`def` nesting.
		if m := rubyRescueRe.FindStringSubmatch(raw); m != nil {
			exc := strings.TrimSpace(m[2])
			cond := "rescue"
			if exc != "" {
				cond = "rescue " + exc
			}
			body := rubyRescueBody(lines, i)
			outcome := classifyRubyExceptOutcome(body)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachRubyReturns(&bf, body)
			out = append(out, bf)
			continue
		}

		// trailing-modifier guard: `return X if COND` / `raise Y unless COND`.
		if m := rubyModifierGuardRe.FindStringSubmatch(raw); m != nil {
			stmt := strings.TrimSpace(m[1])
			kw := m[2]
			condExpr := strings.TrimSpace(m[3])
			outcome, alters := classifyRubyStmtOutcome(stmt)
			if !alters {
				continue
			}
			cond := kw + " " + condExpr
			envVar := matchEnvVar(rubyEnvRefRe, condExpr)
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachRubyReturns(&bf, []string{stmt})
			out = append(out, bf)
			continue
		}

		// block-form guard: `if COND` / `unless COND` ... `end`.
		if m := rubyBlockGuardRe.FindStringSubmatch(raw); m != nil {
			kw := m[1]
			condExpr := strings.TrimSpace(m[2])
			body := endBlockBody(lines, i)
			outcome, alters := classifyRubyGuardOutcome(body)
			if !alters {
				continue
			}
			cond := kw + " " + condExpr
			envVar := matchEnvVar(rubyEnvRefRe, condExpr)
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachRubyReturns(&bf, body)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// rubyBlockOpenerRe matches a keyword that opens a new `end`-closed block when
// it leads a statement: begin / def / if / unless / case / while / until / for
// / class / module / do (trailing) and `... do |x|`. Used by endBlockBody to
// track nesting depth so a nested block's `end` does not prematurely close the
// guard block.
var rubyBlockOpenerRe = regexp.MustCompile(`^\s*(?:begin|def|class|module|case|while|until|for|if|unless)\b`)

// rubyDoOpenerRe matches a trailing `do` / `do |args|` block opener (e.g.
// `items.each do |i|`), which also requires a matching `end`.
var rubyDoOpenerRe = regexp.MustCompile(`\bdo\b(\s*\|[^|]*\|)?\s*$`)

// rubyModifierKwRe detects a leading-keyword line that is actually a one-line
// trailing-modifier statement (`return x if y`) and therefore does NOT open an
// `end`-closed block. Such a line must not increment nesting depth.
var rubyModifierKwRe = regexp.MustCompile(`^\s*(?:if|unless|while|until)\b`)

// rubyOpensBlock reports whether a line opens a new `end`-closed block. A
// leading if/unless/while/until that carries a trailing modifier on the SAME
// line (`x if y`) — i.e. has code before the keyword — is handled by the caller
// (it never reaches here as an opener candidate). A bare `if COND` /
// `case x` / `begin` / `def` / trailing-`do` opens a block.
func rubyOpensBlock(line string) bool {
	if rubyBlockOpenerRe.MatchString(line) {
		return true
	}
	if rubyDoOpenerRe.MatchString(line) {
		return true
	}
	return false
}

// rubyClosesBlock reports whether a line is a standalone `end` (optionally with
// a trailing modifier or method chain like `end.tap { }`), i.e. it closes one
// nesting level.
var rubyEndRe = regexp.MustCompile(`^\s*end\b`)

// endBlockBody returns the body statements of the `end`-closed block whose
// header is at lines[headerIdx] (an `if`/`unless`/`begin`/... opener). It walks
// forward tracking keyword/`end` nesting depth and collects every line strictly
// inside the block — excluding the header and the closing `end`. This is the
// Ruby analogue of pyBlockBody / braceBlockBody.
//
// A trailing-modifier guard (`return x if y`) is NOT an opener and is handled
// by the modifier path before this is called, so the header here always opens a
// real block.
func endBlockBody(lines []string, headerIdx int) []string {
	depth := 1 // the header opens the first level
	var body []string
	for j := headerIdx + 1; j < len(lines); j++ {
		ln := lines[j]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		// A standalone `end` at the current outermost level closes our block.
		if rubyEndRe.MatchString(ln) {
			depth--
			if depth == 0 {
				break
			}
			body = append(body, ln)
			continue
		}
		// `elsif` / `else` / `ensure` at our block's top level are siblings of
		// the body, not new nesting — include them as body lines (they don't
		// change depth) so a guard whose return lives after an `else` is still
		// seen.
		if depth == 1 && rubyClauseKw(trimmed) {
			body = append(body, ln)
			continue
		}
		body = append(body, ln)
		// Count a nested block opener — but only when it is NOT a trailing
		// modifier (`do x if cond` style) masquerading as an opener.
		if rubyOpensBlock(ln) && !rubyIsModifierLine(ln) {
			depth++
		}
	}
	return body
}

// rubyClauseKw reports whether a trimmed line begins an in-block clause keyword
// (elsif/else/ensure/when) that does not open a new `end`-scoped level.
func rubyClauseKw(trimmed string) bool {
	for _, kw := range []string{"elsif", "else", "ensure", "when", "in "} {
		if strings.HasPrefix(trimmed, kw) {
			return true
		}
	}
	return false
}

// rubyIsModifierLine reports whether a leading-if/unless/while/until line is in
// fact a single-line trailing-modifier statement (it has a control-altering
// statement BEFORE the keyword), which does not open an `end`-closed block.
func rubyIsModifierLine(line string) bool {
	if !rubyModifierKwRe.MatchString(line) {
		// Not a leading conditional keyword — a `do`/`begin`/`def` opener is
		// always a real block.
		return false
	}
	// A pure `if COND` (block opener) has nothing but the condition after the
	// keyword. The modifier regex requires a statement before the keyword, so
	// re-use it: if the WHOLE line matches the modifier shape it is a modifier.
	return rubyModifierGuardRe.MatchString(line)
}

// rubyRescueBody returns the statements of a `rescue` clause: the run from the
// line after the rescue header until the next rescue/else/ensure/end that
// belongs to the SAME begin/def block (depth 0). Nested blocks inside the
// handler are skipped over via end-keyword depth tracking.
func rubyRescueBody(lines []string, rescueIdx int) []string {
	depth := 0
	var body []string
	for j := rescueIdx + 1; j < len(lines); j++ {
		ln := lines[j]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if depth == 0 {
			// A sibling rescue/else/ensure or the closing end terminates this
			// handler.
			if rubyEndRe.MatchString(ln) ||
				strings.HasPrefix(trimmed, "rescue") ||
				trimmed == "else" || strings.HasPrefix(trimmed, "ensure") {
				break
			}
		}
		if rubyEndRe.MatchString(ln) {
			depth--
			if depth < 0 {
				break
			}
			body = append(body, ln)
			continue
		}
		body = append(body, ln)
		if rubyOpensBlock(ln) && !rubyIsModifierLine(ln) {
			depth++
		}
	}
	return body
}

// classifyRubyStmtOutcome classifies a single trailing-modifier statement
// (`return ...`, `raise ...`, `render ...`, `head ...`, `redirect_to ...`).
func classifyRubyStmtOutcome(stmt string) (BranchOutcome, bool) {
	switch {
	case rubyRaiseRe.MatchString(stmt):
		return OutcomeRaise, true
	case rubyRedirectRe.MatchString(stmt):
		return OutcomeRedirect, true
	case rubyReturnRe.MatchString(stmt):
		return OutcomeReturnValue, true
	case rubyRenderRe.MatchString(stmt):
		return OutcomeReturnValue, true
	}
	return "", false
}

// classifyRubyGuardOutcome inspects an if/unless block body and returns its
// outcome + whether it provably alters control flow (raise/return/render/head/
// redirect). A guard that does none is not surfaced (conservative), mirroring
// the Python/brace analyzers.
func classifyRubyGuardOutcome(body []string) (BranchOutcome, bool) {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		if rubyRaiseRe.MatchString(ln) {
			return OutcomeRaise, true
		}
		if rubyRedirectRe.MatchString(ln) {
			return OutcomeRedirect, true
		}
		if rubyReturnRe.MatchString(ln) {
			if rubyRedirectRe.MatchString(joined) {
				return OutcomeRedirect, true
			}
			return OutcomeReturnValue, true
		}
		if rubyRenderRe.MatchString(ln) {
			return OutcomeReturnValue, true
		}
	}
	return "", false
}

// classifyRubyExceptOutcome classifies a rescue handler body. `raise`/`fail`
// (re-raise) → raise; a `return`/`render`/`head` (value-yielding) →
// return_value (or redirect for redirect_to); a body that only logs / is empty
// → swallow — the catch-and-continue silent-failure path an audit must flag.
func classifyRubyExceptOutcome(body []string) BranchOutcome {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		if rubyRaiseRe.MatchString(ln) {
			return OutcomeRaise
		}
		if rubyRedirectRe.MatchString(ln) {
			return OutcomeRedirect
		}
		if rubyReturnRe.MatchString(ln) {
			if rubyRedirectRe.MatchString(joined) {
				return OutcomeRedirect
			}
			return OutcomeReturnValue
		}
		if rubyRenderRe.MatchString(ln) {
			return OutcomeReturnValue
		}
	}
	// No re-raise / return / render / redirect anywhere → swallow.
	return OutcomeSwallow
}

// attachRubyReturns derives returns.status from a Ruby branch body for
// return_value/raise/redirect branches: a Rails status symbol (`status: :ok`,
// `head :not_found`) is mapped to its numeric code, or a numeric status is read
// directly. Only attaches when a status is found.
func attachRubyReturns(bf *BranchFacet, body []string) {
	if bf.Outcome != OutcomeReturnValue && bf.Outcome != OutcomeRaise && bf.Outcome != OutcomeRedirect {
		return
	}
	joined := strings.Join(body, "\n")
	status := ""
	if m := rubyStatusNumRe.FindStringSubmatch(joined); m != nil {
		status = m[1]
	} else if m := rubyStatusSymRe.FindStringSubmatch(joined); m != nil {
		status = railsStatusSymToCode(m[1])
	}
	if status != "" {
		bf.Returns = &BranchReturns{Status: status}
	}
}

// railsStatusSymToCode maps the common Rails HTTP status symbols (the keys
// Rack::Utils::SYMBOL_TO_STATUS_CODE exposes, used by `render status: :sym` and
// `head :sym`) to their numeric code. Conservative — the codes a porting agent
// most needs; unknown symbols yield "".
func railsStatusSymToCode(sym string) string {
	switch sym {
	case "ok":
		return "200"
	case "created":
		return "201"
	case "accepted":
		return "202"
	case "no_content":
		return "204"
	case "moved_permanently":
		return "301"
	case "found":
		return "302"
	case "see_other":
		return "303"
	case "not_modified":
		return "304"
	case "temporary_redirect":
		return "307"
	case "bad_request":
		return "400"
	case "unauthorized":
		return "401"
	case "forbidden":
		return "403"
	case "not_found":
		return "404"
	case "method_not_allowed":
		return "405"
	case "not_acceptable":
		return "406"
	case "conflict":
		return "409"
	case "gone":
		return "410"
	case "unprocessable_entity", "unprocessable_content":
		return "422"
	case "too_many_requests":
		return "429"
	case "internal_server_error":
		return "500"
	case "not_implemented":
		return "501"
	case "bad_gateway":
		return "502"
	case "service_unavailable":
		return "503"
	case "gateway_timeout":
		return "504"
	}
	return ""
}
