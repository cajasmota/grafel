// Branch inventory analyzer for C# (#4445, extends #4423/#4435/#4434, epic
// #4419 capability 4).
//
// branches.go defines the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer.
// branches_jsts_java_go.go (#4434) added the shared brace-block walker
// (braceBlockBody) and analyzers for the three other brace-delimited languages
// plus the shared classifiers (classifyBraceExceptOutcome /
// classifyBraceGuardOutcome / attachBraceReturns / guardKind / matchEnvVar /
// afterCloseParen / httpStatusNameToCode). C# is brace-scoped too, so this file
// reuses every one of those helpers READ-ONLY and only adds the C#-specific
// header regexes, env-var idioms, and HTTP-status extractor.
//
// C# idioms covered (per the ticket):
//   - try/catch — `catch (Exception e)` / bare `catch` — classified raise (re
//     throw) / return_value (returns an IActionResult) / swallow (log-only or
//     empty);
//   - if-guards — leading guard → early_return, mid-body → guard, conservative
//     (only surfaced when the block throws / returns / redirects);
//   - env-gates — Environment.GetEnvironmentVariable("X"),
//     Configuration["X"] / _config["X"] (IConfiguration indexer),
//     Configuration.GetValue<...>("X") / GetSection("X");
//   - status — StatusCode(NNN) / Results.StatusCode(NNN) /
//     response.StatusCode = NNN, the ControllerBase helpers
//     (BadRequest/NotFound/Conflict/Unauthorized/...), the
//     StatusCodes.StatusNNNName / HttpStatusCode.Name enums, and
//     [ProducesResponseType(NNN)] return-shape hints.
//
// Same opt-in contract: this runs only when the effects MCP tool is called with
// include="branches", so the default effects payload is byte-for-byte unchanged.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterBranchAnalyzer("csharp", analyzeBranchesCSharp) }

var (
	// csCatchRe — `catch (Exception e)` / `catch (FooException)` / bare `catch`,
	// possibly preceded by the closing brace of the try (`} catch`).
	csCatchRe = regexp.MustCompile(`^\s*}?\s*catch\s*(\(([^)]*)\))?\s*(?:when\s*\([^)]*\))?\s*\{?`)
	// csIfRe — `if (cond) ...`, possibly preceded by `} else `.
	csIfRe = regexp.MustCompile(`^\s*(?:\}\s*else\s+)?if\s*\((.*)\)\s*(.*)$`)

	csThrowRe  = regexp.MustCompile(`(^|\b)throw\b`)
	csReturnRe = regexp.MustCompile(`(^|\b)return\b`)
	// csRedirectRe — Redirect(...) / RedirectToAction(...) / LocalRedirect(...) /
	// Results.Redirect(...) / RedirectResult.
	csRedirectRe = regexp.MustCompile(`\b(?:Local|Permanent)?Redirect(?:ToAction|ToRoute|Permanent)?\s*\(|\bRedirectResult\b`)
	csLogCallRe  = regexp.MustCompile(`\b(?:_?[Ll]ogger|Log|Console)\s*\.\s*\w+\s*\(`)

	// csEnvRefRe — the env/config idioms, in tried order:
	//   Environment.GetEnvironmentVariable("X")
	//   <cfg>["X"]            (IConfiguration indexer — Configuration["X"], _config["X"])
	//   <cfg>.GetValue<T>("X")
	//   <cfg>.GetSection("X")
	csEnvRefRe = regexp.MustCompile(
		`\bEnvironment\s*\.\s*GetEnvironmentVariable\s*\(\s*"([^"]+)"` +
			`|(?:[Cc]onfig(?:uration)?|_config(?:uration)?|cfg)\s*\[\s*"([^"]+)"\s*\]` +
			`|\bGetValue\s*<[^>]*>\s*\(\s*"([^"]+)"` +
			`|\bGetSection\s*\(\s*"([^"]+)"`)

	// csStatusCallRe — StatusCode(NNN) / Results.StatusCode(NNN) /
	// .StatusCode = NNN / Problem(statusCode: NNN) / sendError-likes.
	csStatusCallRe = regexp.MustCompile(`\bStatusCode\s*(?:=\s*|\(\s*(?:statusCode\s*:\s*)?)(\d{3})\b`)
	csProblemRe    = regexp.MustCompile(`\bProblem\s*\([^)]*?\bstatusCode\s*:\s*(\d{3})\b`)
	// csStatusEnumRe — StatusCodes.Status409Conflict / HttpStatusCode.Conflict.
	csStatusCodesRe   = regexp.MustCompile(`\bStatusCodes\s*\.\s*Status(\d{3})[A-Za-z]*`)
	csHttpStatusEnum  = regexp.MustCompile(`\bHttpStatusCode\s*\.\s*([A-Za-z]+)`)
	csProducesRespRe  = regexp.MustCompile(`\[ProducesResponseType\s*\([^)]*?\b(\d{3})\b`)
	// csHelperStatusRe — ControllerBase result helpers mapped to their status.
	csHelperStatusRe = regexp.MustCompile(`\b(BadRequest|NotFound|Conflict|Unauthorized|Forbid|NoContent|Created|CreatedAtAction|CreatedAtRoute|Accepted|UnprocessableEntity|Ok)\s*\(`)
)

func analyzeBranchesCSharp(funcSource string, startLine int) []BranchFacet {
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
		if m := csCatchRe.FindStringSubmatch(raw); m != nil {
			cond := "catch"
			if strings.TrimSpace(m[2]) != "" {
				cond = "catch (" + strings.TrimSpace(m[2]) + ")"
			}
			body := braceBlockBody(lines, i, afterCloseParen(raw))
			outcome := classifyBraceExceptOutcome(body, csThrowRe, csReturnRe, csRedirectRe, csLogCallRe)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachBraceReturns(&bf, body, csStatusFromBody, csRedirectRe)
			out = append(out, bf)
			continue
		}

		// if guard.
		if m := csIfRe.FindStringSubmatch(raw); m != nil {
			cond := "if (" + strings.TrimSpace(m[1]) + ")"
			body := braceBlockBody(lines, i, m[2])
			outcome, alters := classifyBraceGuardOutcome(body, csThrowRe, csReturnRe, csRedirectRe)
			if !alters {
				continue
			}
			envVar := matchEnvVar(csEnvRefRe, m[1])
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, csStatusFromBody, csRedirectRe)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// csStatusFromBody extracts an HTTP status from a C# branch body. Tries the
// explicit StatusCode(NNN) / Results.StatusCode(NNN) / .StatusCode = NNN forms,
// then Problem(statusCode: NNN), then the StatusCodes.StatusNNN and
// HttpStatusCode.Name enums, then the ControllerBase result helpers
// (BadRequest()/NotFound()/...), and finally the [ProducesResponseType(NNN)]
// attribute hint. Empty when none match.
func csStatusFromBody(joined string) string {
	if m := csStatusCallRe.FindStringSubmatch(joined); m != nil {
		return m[1]
	}
	if m := csProblemRe.FindStringSubmatch(joined); m != nil {
		return m[1]
	}
	if m := csStatusCodesRe.FindStringSubmatch(joined); m != nil {
		return m[1]
	}
	if m := csHttpStatusEnum.FindStringSubmatch(joined); m != nil {
		if code := httpStatusNameToCode(strings.ToUpper(camelToSnake(m[1]))); code != "" {
			return code
		}
	}
	if m := csHelperStatusRe.FindStringSubmatch(joined); m != nil {
		if code := csHelperToCode(m[1]); code != "" {
			return code
		}
	}
	if m := csProducesRespRe.FindStringSubmatch(joined); m != nil {
		return m[1]
	}
	return ""
}

// csHelperToCode maps the common ControllerBase result-helper names to their
// HTTP status. Conservative — only the helpers a porting agent most needs.
func csHelperToCode(name string) string {
	switch name {
	case "Ok":
		return "200"
	case "Created", "CreatedAtAction", "CreatedAtRoute":
		return "201"
	case "Accepted":
		return "202"
	case "NoContent":
		return "204"
	case "BadRequest":
		return "400"
	case "Unauthorized":
		return "401"
	case "Forbid":
		return "403"
	case "NotFound":
		return "404"
	case "Conflict":
		return "409"
	case "UnprocessableEntity":
		return "422"
	}
	return ""
}
