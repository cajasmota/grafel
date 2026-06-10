// Branch inventory analyzer for Rust — the `?` operator, `return Err`/`Ok`,
// `match`-arm `Err(e) =>` handlers, `if let Err`/guard clauses, std::env::var
// gates, StatusCode statuses, and panic! (#4447, extends #4423/#4435/#4434,
// epic #4419 capability 4).
//
// branches.go defined the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer.
// branches_jsts_java_go.go added the brace-delimited JS/TS, Java, and Go
// analyzers plus the shared braceBlockBody / stripLeadingCloser / guardKind /
// matchEnvVar / httpStatusNameToCode helpers. Rust is brace-scoped too, so this
// file REUSES those helpers READ-ONLY and only contributes a Rust-shaped
// classifier in its own init().
//
// Rust's control-flow surface differs from the C-family languages in ways the
// classifier must model so a porting agent reproduces every failure path:
//
//   - The `?` operator (`let x = thing()?;`) is an implicit early return: on an
//     `Err`/`None` it returns that variant out of the function. There is no
//     brace block — it is a one-liner — so it is surfaced as an early_return
//     whose outcome is `raise` (it propagates the error).
//   - `return Err(...)` / `panic!(...)` / `.unwrap()`/`.expect(...)` raise;
//     `return Ok(...)` / a bare `return <expr>` returns a value.
//   - A `match` arm `Err(e) => { ... }` (or `Err(e) => return ...`) is the
//     idiomatic explicit handler — the Rust analogue of an except/catch — so it
//     is surfaced as an `except` branch and classified raise/return/swallow.
//   - `if let Err(e) = ...` / `if let Ok(..) = ..` and ordinary `if <cond>`
//     guards scope a brace block like the other languages.
//   - env-gates read std::env::var("X") / env::var("X") / env::var_os("X").
//   - statuses come from StatusCode::NNN, StatusCode::NAME (axum/actix enum),
//     and `.status(NNN)` builder calls.
//
// Same opt-in contract: this runs only when the effects MCP tool is called with
// include="branches", so the default effects payload is byte-for-byte unchanged.
// Classification is conservative — a branch is only surfaced when it provably
// alters control flow (returns / raises via ? / panics / writes a status).
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterBranchAnalyzer("rust", analyzeBranchesRust)
}

var (
	// rustMatchErrArmRe — a match arm handling the error variant:
	// `Err(e) => { ... }` or `Err(e) => return ...,`. Also matches `None =>`
	// (the Option failure arm). Group 2 is the optional inline arm body after
	// `=>`.
	rustMatchErrArmRe = regexp.MustCompile(`^\s*(Err\s*(?:\([^)]*\))?|None)\s*=>\s*(.*)$`)

	// rustIfLetErrRe — `if let Err(e) = expr { ... }` / `if let None = ..`.
	// The whole condition is captured for env-var sniffing; the leading-`}`
	// (`} else if let ..`) is tolerated.
	rustIfLetRe = regexp.MustCompile(`^\s*}?\s*(?:else\s+)?if\s+let\s+(.*?)\s*=\s*(.*?)\s*\{\s*$`)

	// rustIfRe — ordinary guard `if <cond> {`. Rust requires the brace on the
	// header line (no parens around the condition). A leading `} else ` closer
	// is tolerated.
	rustIfRe = regexp.MustCompile(`^\s*}?\s*(?:else\s+)?if\s+(.*?)\s*\{\s*$`)

	// rustQuestionRe — a line containing the `?` try operator on a call/expr
	// (`let v = f()?;`, `f().await?;`, `bar()?`). Excludes `?` inside a string
	// (handled by stripBraceNoise) and the `?` of a turbofish/closure (rare in
	// the statement position we scan). We require a `?` immediately before `;`,
	// `.`, ` `, or end-of-trimmed-line preceded by `)` or an identifier.
	rustQuestionRe = regexp.MustCompile(`[\w)\]]\?(?:;|\.|\s|$)`)

	rustReturnRe   = regexp.MustCompile(`(^|\b)return\b`)
	rustReturnErr  = regexp.MustCompile(`\breturn\s+Err\b|\bErr\s*\(`)
	rustPanicRe    = regexp.MustCompile(`\bpanic\s*!|\bunreachable\s*!|\btodo\s*!|\bunimplemented\s*!|\.\s*(?:unwrap|expect)\s*\(`)
	rustRedirectRe = regexp.MustCompile(`\bRedirect\s*::\s*\w+\s*\(|\bredirect\s*\(|\bLocation\b`)

	// rustEnvRefRe — std::env::var("X"), env::var("X"), env::var_os("X"),
	// std::env::var_os("X"). Group 1 / 2 hold the name.
	rustEnvRefRe = regexp.MustCompile(
		`\b(?:std\s*::\s*)?env\s*::\s*var(?:_os)?\s*\(\s*"([^"]+)"`)

	// rustStatusCallRe — `.status(NNN)`, `StatusCode::from_u16(NNN)`,
	// numeric status literal in a builder.
	rustStatusCallRe = regexp.MustCompile(`\.\s*status\s*\(\s*(\d{3})\b|StatusCode\s*::\s*from_u16\s*\(\s*(\d{3})\b|\bStatusCode\s*::\s*(\d{3})\b`)
	// rustStatusEnumRe — axum/actix `StatusCode::BAD_REQUEST` etc.
	rustStatusEnumRe = regexp.MustCompile(`StatusCode\s*::\s*([A-Z][A-Z_]+)\b`)
)

func analyzeBranchesRust(funcSource string, startLine int) []BranchFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	lines := strings.Split(funcSource, "\n")
	var out []BranchFacet
	firstGuardSeen := false

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		absLine := startLine + i
		clean := stripBraceNoise(raw) // ignore `?`/braces inside literals/comments

		// `match` arm handling the error variant — the Rust except analogue.
		if m := rustMatchErrArmRe.FindStringSubmatch(strings.TrimSpace(clean)); m != nil {
			cond := strings.TrimSpace(m[1]) + " =>"
			var body []string
			inline := strings.TrimSpace(m[2])
			inline = strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(inline, ",")), "{")
			if strings.Contains(m[2], "{") {
				body = braceBlockBody(lines, i, m[2])
			} else if inline != "" {
				body = []string{inline}
			}
			outcome := classifyRustExceptOutcome(body)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachBraceReturns(&bf, body, rustStatusFromBody, rustRedirectRe)
			out = append(out, bf)
			continue
		}

		// `if let Err(..) = ..` / `if let None = ..` guard.
		if m := rustIfLetRe.FindStringSubmatch(raw); m != nil {
			pattern := strings.TrimSpace(m[1])
			scrutinee := strings.TrimSpace(m[2])
			cond := "if let " + pattern + " = " + scrutinee
			body := braceBlockBody(lines, i, "{")
			outcome, alters := classifyRustGuardOutcome(body)
			if !alters {
				continue
			}
			envVar := matchEnvVar(rustEnvRefRe, m[2])
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, rustStatusFromBody, rustRedirectRe)
			out = append(out, bf)
			continue
		}

		// ordinary `if <cond> {` guard (must come after the if-let branch).
		if m := rustIfRe.FindStringSubmatch(raw); m != nil {
			condExpr := strings.TrimSpace(m[1])
			cond := "if " + condExpr
			body := braceBlockBody(lines, i, "{")
			outcome, alters := classifyRustGuardOutcome(body)
			if !alters {
				continue
			}
			envVar := matchEnvVar(rustEnvRefRe, m[1])
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, rustStatusFromBody, rustRedirectRe)
			out = append(out, bf)
			continue
		}

		// `?` try operator — an implicit early-return that propagates Err/None.
		// Skip lines that are themselves a branch header (handled above) or a
		// comment; require the `?` in a statement position.
		if rustQuestionRe.MatchString(clean) && !strings.HasPrefix(trimmed, "//") {
			cond := strings.TrimRight(trimmed, " ;")
			envVar := matchEnvVar(rustEnvRefRe, clean)
			kind := guardKind(envVar, &firstGuardSeen)
			// `?` propagates an error variant out of the function — a raise.
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: OutcomeRaise, EnvVar: envVar, Line: absLine}
			// A `?` rarely names a status, but attach one if the line does.
			attachBraceReturns(&bf, []string{clean}, rustStatusFromBody, rustRedirectRe)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// classifyRustGuardOutcome inspects an if / if-let block body. panic!/unwrap/
// expect or `return Err` → raise; a `?` inside the block → raise (propagates);
// a `return Ok(..)`/bare return → return_value; a redirect → redirect. A block
// that does none of these is not surfaced (conservative).
func classifyRustGuardOutcome(body []string) (BranchOutcome, bool) {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		clean := stripBraceNoise(ln)
		if rustPanicRe.MatchString(clean) {
			return OutcomeRaise, true
		}
		if rustReturnRe.MatchString(clean) {
			if rustReturnErr.MatchString(clean) {
				return OutcomeRaise, true
			}
			if rustRedirectRe.MatchString(joined) {
				return OutcomeRedirect, true
			}
			return OutcomeReturnValue, true
		}
		if rustQuestionRe.MatchString(clean) {
			return OutcomeRaise, true
		}
		if rustRedirectRe.MatchString(clean) {
			return OutcomeRedirect, true
		}
	}
	return "", false
}

// classifyRustExceptOutcome classifies a `match` Err/None arm. A re-raise
// (panic!/return Err/`?`) → raise; a `return Ok(..)`/value return → return_value;
// a redirect → redirect; an arm that only logs / produces a fallback value with
// no error propagation → swallow (the audit-critical recover-and-continue path).
func classifyRustExceptOutcome(body []string) BranchOutcome {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		clean := stripBraceNoise(ln)
		if rustPanicRe.MatchString(clean) {
			return OutcomeRaise
		}
		if rustReturnRe.MatchString(clean) {
			if rustReturnErr.MatchString(clean) {
				return OutcomeRaise
			}
			if rustRedirectRe.MatchString(joined) {
				return OutcomeRedirect
			}
			return OutcomeReturnValue
		}
		if rustQuestionRe.MatchString(clean) {
			return OutcomeRaise
		}
		if rustRedirectRe.MatchString(clean) {
			return OutcomeRedirect
		}
	}
	// No re-raise / return / redirect → swallow (recover-and-continue).
	return OutcomeSwallow
}

func rustStatusFromBody(joined string) string {
	if m := rustStatusCallRe.FindStringSubmatch(joined); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	if m := rustStatusEnumRe.FindStringSubmatch(joined); m != nil {
		if code := httpStatusNameToCode(m[1]); code != "" {
			return code
		}
	}
	return ""
}
