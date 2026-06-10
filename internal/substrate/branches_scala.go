// Branch inventory analyzer for Scala (#4449, extends #4423/#4435/#4434, epic
// #4419 capability 4).
//
// branches.go added the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer (an
// indentation-scoped CFG walk). branches_jsts_java_go.go generalized the facet
// to the brace-delimited JS/TS, Java, and Go languages and contributed the
// shared braceBlockBody / stripBraceNoise / classifyBrace* helpers. This file
// registers the Scala analyzer alongside them in the SAME
// substrate.RegisterBranchAnalyzer registry — in its own init() so it does not
// touch any shared file (five sibling languages land concurrently).
//
// Scala is brace-scoped (`try { ... } catch { case e: T => ... }`, `if (cond) {
// ... }`), so the analyzer reuses braceBlockBody READ-ONLY exactly like the
// JS/TS/Java/Go analyzers. The forms it classifies, mirroring the ticket:
//
//   - try/catch — Scala spells the handler `catch { case e: T => ... }`. The
//     catch BLOCK is brace-scoped; its body is classified with the shared
//     except lattice (re-throw=raise / return or a Status response=return_value
//     / Left/Failure error value=return_value / log-only=swallow).
//   - if-guards — `if (cond) { return ... }` / brace-less `if (cond) return ...`,
//     including the leading-guard → early_return slot, mirroring every sibling.
//   - Either/Try error branches — a branch yielding `Left(...)` / `Failure(...)`
//     is a control-altering error outcome (return_value), and a `Failure(new
//     Exception)` / explicit `throw` is a raise. Scala methods are
//     expression-oriented, so a guard need not literally `return`.
//   - env-gates — `sys.env.get("X")` / `sys.env("X")` / `System.getenv("X")` /
//     `sys.props("x")` conditions surface env_var and the env_gate kind.
//   - status — `Status(NNN)` / `.withStatus(NNN)` (http4s/akka) plus the named
//     Play/Akka results (Ok / BadRequest / NotFound / Conflict / ...) and
//     `HttpStatus.NAME`, mapped to a numeric returns.status.
//   - throw → raise.
//
// Same opt-in contract: the analyzer runs only when the effects MCP tool is
// called with include="branches"; the default effects payload is byte-for-byte
// unchanged. Classification is conservative — a branch is surfaced only when it
// provably alters control flow (returns / throws / yields an error value /
// writes a status); plain branching `if`s are skipped.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterBranchAnalyzer("scala", analyzeBranchesScala) }

var (
	// scalaCatchRe matches the Scala `catch {` handler header. The matching
	// `case e: T =>` clauses live INSIDE the catch block, so the surfaced
	// condition is simply "catch" — the precise caught types are recovered
	// from the block body if needed by downstream consumers.
	scalaCatchRe = regexp.MustCompile(`^\s*}?\s*catch\s*\{?`)
	// scalaIfRe matches `if (cond) <rest>` — the brace, an inline statement, or
	// nothing may follow. A leading `} else ` closer is tolerated.
	scalaIfRe = regexp.MustCompile(`^\s*(?:\}\s*else\s+)?if\s*\((.*)\)\s*(.*)$`)

	scalaThrowRe = regexp.MustCompile(`(^|\b)throw\b`)
	// scalaReturnRe — an explicit `return`, OR an Either/Try error value
	// (`Left(...)` / `Failure(...)`) which is Scala's idiomatic
	// expression-oriented early-out, OR a `Right(...)` / `Success(...)` /
	// status response. These are all "this branch produces a value" outcomes.
	scalaReturnRe = regexp.MustCompile(`(^|\b)return\b|\bLeft\s*\(|\bRight\s*\(|\bFailure\s*\(|\bSuccess\s*\(`)
	// scalaFailureRe — a `Failure(new SomethingException(...))` or a
	// `Failure(... Exception ...)` is the Try analogue of a raise.
	scalaFailureRe  = regexp.MustCompile(`\bFailure\s*\(\s*(?:new\s+)?\w*(?:Exception|Error|Throwable)\b`)
	scalaRedirectRe = regexp.MustCompile(`\bRedirect\s*\(|\bSeeOther\b|\bTemporaryRedirect\b|\bPermanentRedirect\b|\.\s*redirect\s*\(`)
	scalaLogCallRe  = regexp.MustCompile(`\b(?:log|logger|Logger|LOG|LOGGER)\s*\.\s*\w+\s*\(|\bprintln\s*\(`)

	// scalaEnvRefRe — sys.env.get("X") / sys.env("X") / System.getenv("X") /
	// sys.props("x") / sys.props.get("x").
	scalaEnvRefRe = regexp.MustCompile(
		`\bsys\s*\.\s*env\s*(?:\.\s*get)?\s*\(\s*"([^"]+)"` +
			`|\bSystem\s*\.\s*getenv\s*\(\s*"([^"]+)"` +
			`|\bsys\s*\.\s*props\s*(?:\.\s*get)?\s*\(\s*"([^"]+)"`)

	// scalaStatusCallRe — Status(NNN) (http4s), .withStatus(NNN) (akka-http),
	// .status(NNN), setStatus(NNN).
	scalaStatusCallRe = regexp.MustCompile(`\bStatus\s*\(\s*(\d{3})\b|\.\s*(?:withStatus|status|setStatus)\s*\(\s*(\d{3})\b`)
	// scalaHTTPStatusEnum — a Spring/akka HttpStatus.NAME / StatusCodes.NAME
	// enum reference, mapped via httpStatusNameToCode.
	scalaHTTPStatusEnum = regexp.MustCompile(`(?:HttpStatus|StatusCodes?)\s*\.\s*([A-Za-z_][\w]*)`)
)

// scalaNamedStatus maps the bare Play/akka result objects (Ok, BadRequest, ...)
// to their numeric code. These are the idiomatic Scala web result values a
// porting agent most needs reproduced; unknown names yield "".
func scalaNamedStatus(joined string) string {
	for _, m := range scalaNamedStatusRe.FindAllStringSubmatch(joined, -1) {
		if code := scalaResultNameToCode(m[1]); code != "" {
			return code
		}
	}
	return ""
}

// scalaNamedStatusRe captures an identifier that may be a Play/akka named
// result (Ok, BadRequest, NotFound, ...). It is intentionally permissive; the
// name→code lookup is what actually filters to real result names.
var scalaNamedStatusRe = regexp.MustCompile(`\b([A-Z][A-Za-z]+)\b`)

func scalaResultNameToCode(name string) string {
	switch name {
	case "Ok", "OK":
		return "200"
	case "Created":
		return "201"
	case "Accepted":
		return "202"
	case "NoContent":
		return "204"
	case "MovedPermanently":
		return "301"
	case "Found":
		return "302"
	case "SeeOther":
		return "303"
	case "NotModified":
		return "304"
	case "TemporaryRedirect":
		return "307"
	case "BadRequest":
		return "400"
	case "Unauthorized":
		return "401"
	case "Forbidden":
		return "403"
	case "NotFound":
		return "404"
	case "MethodNotAllowed":
		return "405"
	case "Conflict":
		return "409"
	case "Gone":
		return "410"
	case "UnprocessableEntity":
		return "422"
	case "TooManyRequests":
		return "429"
	case "InternalServerError":
		return "500"
	case "NotImplemented":
		return "501"
	case "BadGateway":
		return "502"
	case "ServiceUnavailable":
		return "503"
	case "GatewayTimeout":
		return "504"
	}
	return ""
}

func analyzeBranchesScala(funcSource string, startLine int) []BranchFacet {
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

		// catch handler — `} catch {` / `catch {`. Match before the if-guard
		// (a `catch` line never matches scalaIfRe, but keep ordering explicit).
		if scalaCatchRe.MatchString(raw) {
			body := braceBlockBody(lines, i, afterCatchBrace(raw))
			outcome := classifyScalaExceptOutcome(body)
			bf := BranchFacet{Kind: BranchExcept, Condition: "catch", Outcome: outcome, Line: absLine}
			attachBraceReturns(&bf, body, scalaStatusFromBody, scalaRedirectRe)
			out = append(out, bf)
			continue
		}

		// if-guard.
		if m := scalaIfRe.FindStringSubmatch(raw); m != nil {
			cond := "if (" + strings.TrimSpace(m[1]) + ")"
			body := braceBlockBody(lines, i, m[2])
			outcome, alters := classifyScalaGuardOutcome(body)
			if !alters {
				continue
			}
			envVar := matchEnvVar(scalaEnvRefRe, m[1])
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, scalaStatusFromBody, scalaRedirectRe)
			out = append(out, bf)
			continue
		}
	}
	return out
}

// afterCatchBrace returns the text after the `catch` keyword on a header line,
// so braceBlockBody can detect the opening `{` (it may be on the same line:
// `catch { case e => ... }`) or fall through to the next line.
func afterCatchBrace(ln string) string {
	idx := strings.Index(ln, "catch")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(ln[idx+len("catch"):])
	// A leading `{` is what braceBlockBody scans for; return the remainder
	// verbatim so an inline `catch { case e => return x }` is still walked.
	return rest
}

// classifyScalaExceptOutcome classifies a `catch { case ... }` block body. A
// re-throw (throw / Failure(new XException)) → raise; a return / Status / Right
// / Left value → return_value; a redirect → redirect; a body that only logs or
// is empty → swallow (the audit-critical silent-failure path).
func classifyScalaExceptOutcome(body []string) BranchOutcome {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		if scalaThrowRe.MatchString(ln) || scalaFailureRe.MatchString(ln) {
			return OutcomeRaise
		}
		if scalaReturnRe.MatchString(ln) {
			if scalaRedirectRe.MatchString(joined) {
				return OutcomeRedirect
			}
			return OutcomeReturnValue
		}
		if scalaRedirectRe.MatchString(ln) {
			return OutcomeRedirect
		}
	}
	// A bare status response with no return/Left/Right (e.g. `BadRequest(...)`
	// as the last expression) is still a control-altering value outcome.
	if scalaStatusFromBody(joined) != "" {
		return OutcomeReturnValue
	}
	return OutcomeSwallow
}

// classifyScalaGuardOutcome inspects an if-block body and returns its outcome +
// whether it alters control flow. Beyond the brace-language throw/return/
// redirect lattice it also treats an Either/Try error value (Left/Failure) or a
// bare status response as a control-altering outcome — Scala guards are
// expression-oriented and need not literally `return`.
func classifyScalaGuardOutcome(body []string) (BranchOutcome, bool) {
	joined := strings.Join(body, "\n")
	for _, ln := range body {
		if scalaThrowRe.MatchString(ln) || scalaFailureRe.MatchString(ln) {
			return OutcomeRaise, true
		}
		if scalaReturnRe.MatchString(ln) {
			if scalaRedirectRe.MatchString(joined) {
				return OutcomeRedirect, true
			}
			return OutcomeReturnValue, true
		}
		if scalaRedirectRe.MatchString(ln) {
			return OutcomeRedirect, true
		}
	}
	if scalaStatusFromBody(joined) != "" {
		return OutcomeReturnValue, true
	}
	return "", false
}

// scalaStatusFromBody derives an HTTP status from a Scala branch body: a numeric
// Status(NNN) / .withStatus(NNN), then an HttpStatus/StatusCodes enum name, then
// a bare Play/akka named result (Ok / BadRequest / ...). Empty when none found.
func scalaStatusFromBody(joined string) string {
	if m := scalaStatusCallRe.FindStringSubmatch(joined); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	if m := scalaHTTPStatusEnum.FindStringSubmatch(joined); m != nil {
		name := m[1]
		if code := httpStatusNameToCode(strings.ToUpper(camelToSnake(name))); code != "" {
			return code
		}
		if code := scalaResultNameToCode(name); code != "" {
			return code
		}
	}
	return scalaNamedStatus(joined)
}
