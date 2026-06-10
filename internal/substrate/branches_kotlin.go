// Branch inventory analyzer for Kotlin — a brace-delimited, JVM/Spring-flavored
// language (#4446, extends #4423/#4435/#4434, epic #4419 capability 4).
//
// branches.go added the language-neutral BranchFacet schema, the
// BranchAnalyzerFn registry, and the flagship Python analyzer.
// branches_jsts_java_go.go added the three other brace-delimited corpus
// languages plus the shared brace-block machinery (braceBlockBody,
// stripLeadingCloser/stripBraceNoise, classifyBraceExceptOutcome,
// classifyBraceGuardOutcome, attachBraceReturns, guardKind, matchEnvVar,
// afterCloseParen, httpStatusNameToCode). This file registers Kotlin alongside
// them, in its OWN init() so concurrent sibling language additions never touch a
// shared file.
//
// Kotlin shares Java's block delimiting (`{`/`}`), so it REUSES braceBlockBody
// read-only to scope each branch to its block. The control-flow surface mirrors
// the Java analyzer closely (Kotlin leans on the same Spring/Jakarta stack):
//
//   - try/catch — `try { ... } catch (e: Exception) { ... }`. Re-throw → raise,
//     return → return_value, log-only/empty → swallow.
//   - if-guards + early-return — `if (dto.email == null) return ...` /
//     `if (...) { return ResponseEntity.status(...)... }`. Kotlin permits a
//     brace-less single-statement body, handled by braceBlockBody's afterCond.
//   - env-gates — `System.getenv("X")`, `@Value("\${x}")`, `env.getProperty("x")`,
//     and Kotlin's own `System.getProperty(...)`; surfaced as env_gate with the
//     env var name.
//   - HTTP status — `ResponseEntity.status(NNN)` / `HttpStatus.NAME` (enum→code),
//     `response.setStatus(NNN)` / `sendError(NNN)`.
//
// Same opt-in contract: this runs only when archigraph_effects is called with
// include="branches", so the default effects payload is byte-for-byte unchanged.
// Classification stays conservative — a branch is surfaced only when it provably
// alters control flow (returns / throws / redirects / writes an HTTP error
// status); plain branching `if`s are skipped.
package substrate

import (
	"regexp"
	"strings"
)

func init() {
	RegisterBranchAnalyzer("kotlin", analyzeBranchesKotlin)
}

var (
	// kotlinCatchRe matches `catch (e: Exception)` (Kotlin's typed-binding form)
	// as well as a leading `}` closer (`} catch (...) {`). The capture is the
	// full binding text (`e: Exception`).
	kotlinCatchRe = regexp.MustCompile(`^\s*}?\s*catch\s*\(([^)]*)\)\s*\{?`)
	// kotlinIfRe matches `if (cond) <rest>` with an optional leading `} else `.
	kotlinIfRe = regexp.MustCompile(`^\s*(?:\}\s*else\s+)?if\s*\((.*)\)\s*(.*)$`)

	kotlinThrowRe  = regexp.MustCompile(`(^|\b)throw\b`)
	kotlinReturnRe = regexp.MustCompile(`(^|\b)return\b`)
	// kotlinRedirectRe — Spring MVC redirect shapes available from Kotlin, plus
	// servlet sendRedirect.
	kotlinRedirectRe = regexp.MustCompile(`\bsendRedirect\s*\(|\bRedirectView\b|ModelAndView\s*\(\s*["']redirect:|["']redirect:`)
	kotlinLogCallRe  = regexp.MustCompile(`\b(?:log|logger|LOG|LOGGER)\s*\.\s*\w+\s*\(|\bprintln\s*\(|\bprintStackTrace\s*\(|\bSystem\s*\.\s*(?:out|err)\s*\.`)

	// kotlinEnvRefRe — System.getenv("X"), @Value("\${x}"), env.getProperty("x"),
	// System.getProperty("x"), bare getenv("X"). The `\$` in @Value is escaped in
	// Kotlin string templates, so the source carries `${...}` literally here.
	kotlinEnvRefRe = regexp.MustCompile(
		`\bSystem\s*\.\s*getenv\s*\(\s*"([^"]+)"` +
			`|@Value\s*\(\s*"\$\{\s*([A-Za-z_][\w.]*)` +
			`|\bgetProperty\s*\(\s*"([^"]+)"` +
			`|\bgetenv\s*\(\s*"([^"]+)"`)

	// kotlinStatusCallRe — ResponseEntity.status(NNN), response.setStatus(NNN),
	// response.sendError(NNN). kotlinHttpStatusEnum maps HttpStatus.NAME → code.
	kotlinStatusCallRe   = regexp.MustCompile(`\.\s*(?:status|setStatus|sendError)\s*\(\s*(\d{3})\b`)
	kotlinHttpStatusEnum = regexp.MustCompile(`HttpStatus\s*\.\s*([A-Z_]+)`)
)

func analyzeBranchesKotlin(funcSource string, startLine int) []BranchFacet {
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

		if m := kotlinCatchRe.FindStringSubmatch(raw); m != nil {
			cond := "catch (" + strings.TrimSpace(m[1]) + ")"
			body := braceBlockBody(lines, i, afterCloseParen(raw))
			outcome := classifyBraceExceptOutcome(body, kotlinThrowRe, kotlinReturnRe, kotlinRedirectRe, kotlinLogCallRe)
			bf := BranchFacet{Kind: BranchExcept, Condition: cond, Outcome: outcome, Line: absLine}
			attachBraceReturns(&bf, body, kotlinStatusFromBody, kotlinRedirectRe)
			out = append(out, bf)
			continue
		}

		if m := kotlinIfRe.FindStringSubmatch(raw); m != nil {
			cond := "if (" + strings.TrimSpace(m[1]) + ")"
			body := braceBlockBody(lines, i, m[2])
			outcome, alters := classifyBraceGuardOutcome(body, kotlinThrowRe, kotlinReturnRe, kotlinRedirectRe)
			if !alters {
				continue
			}
			envVar := matchEnvVar(kotlinEnvRefRe, m[1])
			kind := guardKind(envVar, &firstGuardSeen)
			bf := BranchFacet{Kind: kind, Condition: cond, Outcome: outcome, EnvVar: envVar, Line: absLine}
			attachBraceReturns(&bf, body, kotlinStatusFromBody, kotlinRedirectRe)
			out = append(out, bf)
			continue
		}
	}
	return out
}

func kotlinStatusFromBody(joined string) string {
	if m := kotlinStatusCallRe.FindStringSubmatch(joined); m != nil {
		return m[1]
	}
	if m := kotlinHttpStatusEnum.FindStringSubmatch(joined); m != nil {
		if code := httpStatusNameToCode(m[1]); code != "" {
			return code
		}
	}
	return ""
}
