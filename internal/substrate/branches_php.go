// Branch inventory analyzer for PHP (#4448, extends #4423/#4435/#4434, epic
// #4419 capability 4).
//
// branches.go added the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer (an
// indentation-scoped CFG walk). branches_jsts_java_go.go generalized the facet
// to the brace-delimited languages and shipped the shared braceBlockBody helper
// (a brace-depth walk that scopes each branch to its own block). PHP is also
// brace-scoped, so this file registers a PHP analyzer that REUSES braceBlockBody
// (and its companions stripLeadingCloser / afterCloseParen / the shared
// classify* + attachBraceReturns helpers) read-only — it adds NO shared-file
// edits, only PHP-specific regexes and a single init().
//
// PHP shapes covered, mirroring the Python outcome lattice exactly:
//   - try/catch — `} catch (\Exception $e) {` → re-throw=raise / return=
//     return_value / log-only-or-empty=swallow;
//   - if-guards — leading early_return + mid-body guard, both braced
//     (`if (...) { return ...; }`) and brace-less (`if (...) return ...;`);
//   - env-gates — getenv('X') / $_ENV['X'] / $_SERVER['X'] / env('X') (the
//     Laravel config helper) → kind env_gate with env_var surfaced;
//   - status — response()->setStatusCode(NNN) / http_response_code(NNN) /
//     ->setStatusCode(NNN) / abort(NNN) / response(.., NNN) / json(.., NNN) /
//     new HttpException(NNN) / Response::HTTP_NAME (enum→code) → returns.status.
//
// Same opt-in contract: this runs only when the effects MCP tool is called with
// include="branches", so the default effects payload is byte-for-byte unchanged.
// Classification is conservative — a branch is only surfaced when it provably
// alters control flow (returns / throws / redirects / writes an HTTP status);
// plain branching `if`s are skipped.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterBranchAnalyzer("php", analyzeBranchesPHP) }

var (
	// phpCatchRe matches a catch clause, with or without a leading `}` closing
	// the try block (`} catch (\Exception $e) {`). The capture is the caught
	// type(s) + variable.
	phpCatchRe = regexp.MustCompile(`^\s*}?\s*catch\s*\(([^)]*)\)\s*\{?`)
	// phpIfHeadRe matches an `if`/`elseif`/`} else if` keyword up to (and
	// including) the opening `(` of the condition. The condition itself is then
	// extracted by paren-matching (phpSplitIfCond) so that an inline brace-less
	// body containing its own parens — `if ($id <= 0) throw new Exc(400);` — is
	// not swallowed by a greedy `\((.*)\)`.
	phpIfHeadRe = regexp.MustCompile(`^\s*(?:\}\s*)?(?:else\s*)?(?:else\s*)?(?:elseif|else\s+if|if)\s*\(`)

	phpThrowRe  = regexp.MustCompile(`(^|\b)throw\b`)
	phpReturnRe = regexp.MustCompile(`(^|\b)return\b`)
	// phpRedirectRe — redirect()/->redirect()/RedirectResponse/header('Location:').
	phpRedirectRe = regexp.MustCompile(`\bredirect\s*\(|\.\s*redirect\s*\(|->\s*redirect\s*\(|\bRedirectResponse\b|header\s*\(\s*['"]Location:`)
	// phpLogCallRe — a catch body that only logs.
	phpLogCallRe = regexp.MustCompile(`\b(?:Log|logger|error_log)\s*(?:::|->)?\s*\w*\s*\(`)

	// phpEnvRefRe — getenv('X'), $_ENV['X'], $_SERVER['X'], env('X') (Laravel).
	// Group order: getenv, $_ENV, $_SERVER, env() helper.
	phpEnvRefRe = regexp.MustCompile(
		`\bgetenv\s*\(\s*['"]([^'"]+)['"]` +
			`|\$_ENV\s*\[\s*['"]([^'"]+)['"]\s*\]` +
			`|\$_SERVER\s*\[\s*['"]([^'"]+)['"]\s*\]` +
			`|\benv\s*\(\s*['"]([^'"]+)['"]`)

	// phpStatusCallRe — setStatusCode(NNN), http_response_code(NNN),
	// abort(NNN), setStatusCode(NNN), withStatus(NNN), response(.., NNN),
	// json(.., NNN). The numeric status is captured from whichever shape hits.
	phpStatusCallRe = regexp.MustCompile(
		`\bhttp_response_code\s*\(\s*(\d{3})\b` +
			`|\b(?:setStatusCode|withStatus|setStatus|sendError)\s*\(\s*(\d{3})\b` +
			`|\babort\s*\(\s*(\d{3})\b` +
			`|new\s+HttpException\s*\(\s*(\d{3})\b` +
			`|\.\s*(?:json|make|create)\s*\([^;]*?,\s*(\d{3})\b` +
			`|->\s*(?:json|make|create)\s*\([^;]*?,\s*(\d{3})\b` +
			`|\bresponse\s*\([^;]*?,\s*(\d{3})\b`)
	// phpHTTPStatusEnumRe — Response::HTTP_NOT_FOUND / JsonResponse::HTTP_OK.
	phpHTTPStatusEnumRe = regexp.MustCompile(`::\s*HTTP_([A-Z_]+)\b`)
)

func analyzeBranchesPHP(funcSource string, startLine int) []BranchFacet {
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

		// catch handler.
		if m := phpCatchRe.FindStringSubmatch(raw); m != nil {
			cond := "catch (" + strings.TrimSpace(m[1]) + ")"
			body := braceBlockBody(lines, i, afterCloseParen(raw))
			outcome := classifyBraceExceptOutcome(body, phpThrowRe, phpReturnRe, phpRedirectRe, phpLogCallRe)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachBraceReturns(&bf, body, phpStatusFromBody, phpRedirectRe)
			out = append(out, bf)
			continue
		}

		// if / elseif guard.
		if loc := phpIfHeadRe.FindStringIndex(raw); loc != nil {
			condExpr, after, ok := phpSplitIfCond(raw, loc[1]-1)
			if !ok {
				continue
			}
			cond := "if (" + strings.TrimSpace(condExpr) + ")"
			body := braceBlockBody(lines, i, after)
			outcome, alters := classifyPHPGuardOutcome(body)
			if !alters {
				continue
			}
			envVar := matchEnvVar(phpEnvRefRe, condExpr)
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, phpStatusFromBody, phpRedirectRe)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// phpSplitIfCond, given a line and the index of the `(` that opens an if
// condition, returns the condition text (between that `(` and its matching
// `)`), the trailing text after the matching `)` (an opening `{` or an inline
// brace-less statement), and ok=false when the parens are unbalanced on this
// line. String/char-literal contents are skipped so a `)` inside a literal does
// not close the condition early — reusing stripBraceNoise's literal model via a
// parallel paren-depth walk.
func phpSplitIfCond(ln string, openIdx int) (cond, after string, ok bool) {
	runes := []rune(ln)
	if openIdx < 0 || openIdx >= len(runes) || runes[openIdx] != '(' {
		return "", "", false
	}
	depth := 0
	i := openIdx
	for i < len(runes) {
		c := runes[i]
		switch c {
		case '"', '\'', '`':
			quote := c
			i++
			for i < len(runes) {
				if runes[i] == '\\' {
					i += 2
					continue
				}
				if runes[i] == quote {
					break
				}
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return string(runes[openIdx+1 : i]), strings.TrimSpace(string(runes[i+1:])), true
			}
		}
		i++
	}
	return "", "", false
}

// classifyPHPGuardOutcome inspects an if-block body and returns its outcome +
// whether it alters control flow. Mirrors classifyBraceGuardOutcome but also
// treats a bare HTTP status-write (abort(NNN) / http_response_code(NNN) /
// ->setStatusCode(NNN)) as control-altering even without a following return —
// abort() throws internally and a status-write guard is a response branch a
// porting agent must reproduce (parallels the Go analyzer's status-write
// fall-through handling).
func classifyPHPGuardOutcome(body []string) (BranchOutcome, bool) {
	if outcome, alters := classifyBraceGuardOutcome(body, phpThrowRe, phpReturnRe, phpRedirectRe); alters {
		return outcome, true
	}
	joined := strings.Join(body, "\n")
	if strings.Contains(joined, "abort(") {
		return OutcomeRaise, true
	}
	if phpStatusFromBody(joined) != "" {
		return OutcomeReturnValue, true
	}
	return "", false
}

// phpStatusFromBody extracts an HTTP status from a PHP branch body — a numeric
// status-write call (setStatusCode / http_response_code / abort / response(..,
// NNN) / json(.., NNN) / new HttpException(NNN)) or a Symfony/Laravel
// Response::HTTP_NAME enum mapped to its code.
func phpStatusFromBody(joined string) string {
	if m := phpStatusCallRe.FindStringSubmatch(joined); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	if m := phpHTTPStatusEnumRe.FindStringSubmatch(joined); m != nil {
		if code := httpStatusNameToCode(m[1]); code != "" {
			return code
		}
	}
	return ""
}
