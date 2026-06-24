// Home-rolled / custom auth-check recognition for JS/TS endpoints (#5499).
//
// The #2852 resolver (http_endpoint_jsts_auth.go) recognises auth that is wired
// at the ROUTE-REGISTRATION or DECORATOR surface: Express middleware chains,
// NestJS `@UseGuards`, Hapi route auth, etc. But a very large family of real
// apps — especially meta-framework route handlers, server actions, loaders and
// page handlers (Next.js, Remix, SvelteKit, Nuxt) and plain hand-written
// Express/Koa handlers — does NOT gate at that surface at all. Instead the
// handler BODY opens with an inline authorization check:
//
//	export async function POST(req) {
//	  const user = await requireUser()          // throws/redirects if anon
//	  ...
//	}
//
//	export async function action({ request }) {
//	  const session = await getServerSession()
//	  if (!session) throw redirect('/login')     // explicit guard
//	  ...
//	}
//
// These never reach the route/decorator resolver, so the endpoint resolved to
// method="unknown" even though it is genuinely protected. This pass recovers
// the posture from the handler body's OPENING statements and stamps the same
// AuthPolicy property contract (auth_required / auth_method="check" /
// auth_guard / auth_roles / auth_permissions / auth_policy source chain) — i.e.
// the AUTHORIZES relation from the endpoint to its auth-check callee.
//
// Gating (no false positives): an ordinary call is NOT auth. A body opener
// counts only when (a) it calls a name in the auth-idiom set
// (require*/authorize/assertCan/checkPermission/getServerSession/... — see
// jsAuthCheckCalleeRe), or (b) it is an `if (!session|!user|...) throw|redirect`
// guard over a recognised session/user binding. Both are confined to the first
// few statements of the body (an auth check guards the handler; a call buried
// deep in business logic is not a gate).
//
// Honest-partial: a dynamically-dispatched check (`this.authService.check()` on
// a runtime-resolved service, a check behind an indirection) is recognised only
// when its leaf callee name is in the idiom set; a check assembled at runtime is
// left to the route/decorator resolvers and noted as out of scope.
//
// Refs #5499 (#5479).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// jsAuthCheckCalleeRe matches a call whose leaf callee is a recognised
// home-rolled auth-check idiom. Anchored to the leaf (after the last `.`) so
// `auth.requireUser()` and `this.authz.assertCan(...)` both match on the leaf.
// Group 1 = the leaf callee name, group 2 = the raw argument list.
//
// The set is the require*/authorize/can/assert*/checkPermission/getSession
// family the ticket calls out, plus the common spellings that recur across
// hand-rolled and meta-framework auth helpers. The trailing `\b...\(` pins it
// to a call, not a bare identifier.
var jsAuthCheckCalleeRe = regexp.MustCompile(
	`(?:^|[^\w$])(` +
		`require(?:User|Auth|Authentication|Session|Login|Admin|Role|Permission|Permissions|Scope|Scopes)?|` +
		`ensureAuth(?:enticated)?|ensureLoggedIn|ensureSignedIn|` +
		`authorize|authorization|authenticate|` +
		`assertCan|assertAuth(?:enticated|orized)?|assertUser|assertSession|assertPermission|` +
		`checkAuth|checkPermission|checkPermissions|checkScope|checkRole|checkAccess|` +
		`can|cannot|hasPermission|hasRole|hasScope|hasAccess|` +
		`getServerSession|getSession|getCurrentUser|getAuthUser|getUser|currentUser|` +
		`protect|protectRoute|guard|verifyAuth|verifyToken|verifyJwt|verifySession|` +
		`isAuthenticated|isAuthorized|mustBeAuthenticated|withAuth` +
		`)\s*\(([^)]*)\)`,
)

// jsAuthCheckLeafSet is the canonical set of recognised leaf callee names. It
// gates the regex's broad `require` alternation: a bare `require('fs')` (the
// CommonJS module loader) must NOT be read as an auth check. Only `require`
// FOLLOWED BY an auth noun (requireUser/requireAuth/...) or one of the explicit
// auth verbs counts. We enforce this here rather than in the regex so the
// callee-name allow-list is auditable in one place.
var jsAuthCheckLeafSet = map[string]bool{
	"requireUser": true, "requireAuth": true, "requireAuthentication": true,
	"requireSession": true, "requireLogin": true, "requireAdmin": true,
	"requireRole": true, "requirePermission": true, "requirePermissions": true,
	"requireScope": true, "requireScopes": true,
	"ensureAuth": true, "ensureAuthenticated": true, "ensureLoggedIn": true,
	"ensureSignedIn": true,
	"authorize": true, "authorization": true, "authenticate": true,
	"assertCan": true, "assertAuth": true, "assertAuthenticated": true,
	"assertAuthorized": true, "assertUser": true, "assertSession": true,
	"assertPermission": true,
	"checkAuth": true, "checkPermission": true, "checkPermissions": true,
	"checkScope": true, "checkRole": true, "checkAccess": true,
	"can": true, "hasPermission": true, "hasRole": true, "hasScope": true,
	"hasAccess": true,
	"getServerSession": true, "getSession": true, "getCurrentUser": true,
	"getAuthUser": true,
	"protect": true, "protectRoute": true, "guard": true,
	"verifyAuth": true, "verifyToken": true, "verifyJwt": true,
	"verifySession": true,
	"isAuthenticated": true, "isAuthorized": true, "mustBeAuthenticated": true,
	"withAuth": true,
}

// jsAuthCheckRoleBearing maps a recognised check callee to the bucket its first
// quoted literal argument belongs to: a role check (`requireRole('admin')`,
// `hasRole('user')`) names a ROLE; a permission/scope check
// (`checkPermission('orders:delete')`, `can('delete','Order')`,
// `requireScope('read')`) names a PERMISSION/SCOPE. Anything else carries no
// structured grant (the literal, if any, is opaque) and is captured as the
// guard symbol only.
var jsAuthCheckRoleCallees = map[string]bool{
	"requireRole": true, "checkRole": true, "hasRole": true,
}
var jsAuthCheckScopeCallees = map[string]bool{
	"requireScope": true, "requireScopes": true, "checkScope": true,
	"hasScope": true, "verifyScope": true,
}
var jsAuthCheckPermCallees = map[string]bool{
	"requirePermission": true, "requirePermissions": true, "checkPermission": true,
	"checkPermissions": true, "hasPermission": true, "assertPermission": true,
	"assertCan": true, "can": true,
}

// jsAuthGuardIfRe matches an early-return / early-throw guard over a session or
// user binding: `if (!session) ...`, `if (!user) ...`, `if (!ctx.session) ...`,
// `if (!auth) ...`. Group 1 = the negated binding name (leaf). The follow-up
// `throw`/`redirect`/`return` is confirmed by jsAuthGuardActionRe on the
// remainder of the statement.
var jsAuthGuardIfRe = regexp.MustCompile(
	`\bif\s*\(\s*!\s*(?:[\w$]+\.)*(session|user|auth|currentUser|loggedIn|isAuthenticated|isAuth|token|jwt|principal|identity|account)\b[^)]*\)\s*(.*)`,
)

// jsAuthGuardActionRe confirms the body of an `if (!session)` guard performs an
// auth-rejecting action — throwing, redirecting to a login/auth route, or
// returning a 401/403 / unauthorized response. A guard that merely logs or
// continues is NOT an auth gate.
var jsAuthGuardActionRe = regexp.MustCompile(
	`(?i)\b(?:throw\b|redirect\s*\(|return\s+(?:NextResponse\.)?(?:redirect|json|new\s+Response|unauthorized|forbidden)|res\.(?:status\s*\(\s*(?:401|403)|sendStatus\s*\(\s*(?:401|403)|redirect)|reply\.(?:code\s*\(\s*(?:401|403)|redirect)|signIn\s*\(|notFound\s*\()`,
)

// jsAuthGuardLoginRedirectRe is a narrower confirmation for a redirect-based
// guard: a redirect whose target string looks like a login / sign-in / auth
// route. Used so a generic `redirect('/home')` after `if(!session)` (rare) is
// still treated as a guard, while keeping the action set honest.
var jsAuthGuardLoginRedirectRe = regexp.MustCompile(
	`(?i)redirect\s*\(\s*['"` + "`" + `][^'"` + "`" + `]*(?:login|signin|sign-in|auth|unauthorized|403|401)`,
)

// authBodyResult is the resolved home-rolled posture for one handler body.
type authBodyResult struct {
	guard       string   // the recognised check callee or guard binding (evidence)
	roles       []string // literal roles (requireRole('admin'))
	permissions []string // literal permissions (checkPermission('x'), can('delete'))
	scopes      []string // literal scopes (requireScope('read'))
	line        int      // 1-based line of the recognised check
	text        string   // source-chain evidence text
}

// authBodyOpenWindow caps how far into a handler body we look for the opening
// auth check. An auth gate is, by construction, one of the first statements;
// scanning the whole body would mis-read an authorization call buried in
// business logic as a route gate. ~600 bytes comfortably covers the leading
// `const x = await requireUser()` / `if(!session) redirect(...)` openers across
// formatting styles without reaching into deep handler logic.
const authBodyOpenWindow = 600

// recognizeAuthBodyOpener inspects the OPENING of a handler body for a
// home-rolled auth check. `body` is the source from just after the handler's
// opening `{`. Returns (result, true) on a recognised check. baseLine is the
// 1-based line of the body open, used to compute the check's absolute line.
func recognizeAuthBodyOpener(body string, baseLine int) (authBodyResult, bool) {
	window := body
	if len(window) > authBodyOpenWindow {
		window = window[:authBodyOpenWindow]
	}

	// 1. Direct auth-check call: `const u = await requireUser()`,
	//    `await authorize(ctx, 'orders:read')`, `assertCan('delete', order)`.
	if m := jsAuthCheckCalleeRe.FindStringSubmatchIndex(window); m != nil {
		leaf := window[m[2]:m[3]]
		args := window[m[4]:m[5]]
		if jsAuthCheckLeafSet[leaf] {
			res := authBodyResult{
				guard: leaf,
				line:  baseLine + strings.Count(window[:m[2]], "\n"),
				text:  "body-check: " + leaf + "(" + strings.TrimSpace(args) + ")",
			}
			classifyAuthCheckArgs(leaf, args, &res)
			return res, true
		}
	}

	// 2. Early-guard over a session/user binding: `if (!session) throw ...` /
	//    `if (!user) redirect('/login')`.
	if m := jsAuthGuardIfRe.FindStringSubmatchIndex(window); m != nil {
		binding := window[m[2]:m[3]]
		rest := window[m[4]:m[5]]
		if jsAuthGuardActionRe.MatchString(rest) || jsAuthGuardLoginRedirectRe.MatchString(rest) {
			return authBodyResult{
				guard: "!" + binding,
				line:  baseLine + strings.Count(window[:m[0]], "\n"),
				text:  "body-guard: if(!" + binding + ") " + strings.TrimSpace(firstLine(rest)),
			}, true
		}
	}

	return authBodyResult{}, false
}

// classifyAuthCheckArgs routes a check's first quoted literal(s) to the role /
// permission / scope bucket per the callee's family, so a parameterised check
// carries the specific required grant. A check with no string literal (a
// dynamic guard) carries no grant — never fabricated.
func classifyAuthCheckArgs(leaf, args string, res *authBodyResult) {
	var lits []string
	for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(args, -1) {
		if tok := strings.TrimSpace(q[1]); tok != "" {
			lits = append(lits, tok)
		}
	}
	if len(lits) == 0 {
		return
	}
	switch {
	case jsAuthCheckRoleCallees[leaf]:
		res.roles = append(res.roles, lits...)
	case jsAuthCheckScopeCallees[leaf]:
		res.scopes = append(res.scopes, lits...)
	case jsAuthCheckPermCallees[leaf]:
		// casl `can('delete', 'Order')` — the action (first literal) is the
		// permission; the subject is not a grant string.
		if leaf == "can" {
			res.permissions = append(res.permissions, lits[0])
		} else {
			res.permissions = append(res.permissions, lits...)
		}
	}
}

// authBodyEvidenceSymbol returns a compact symbol for the MCP signal-1
// auth_guard property from a body-check source-chain text: `body-check:
// requireUser(...)` → `requireUser`, `body-guard: if(!session) ...` →
// `!session`.
func authBodyEvidenceSymbol(text string) string {
	text = strings.TrimPrefix(text, "body-check: ")
	if rest, ok := strings.CutPrefix(text, "body-guard: if(!"); ok {
		if i := strings.IndexByte(rest, ')'); i >= 0 {
			return "!" + strings.TrimSpace(rest[:i])
		}
		return "!" + rest
	}
	if i := strings.IndexByte(text, '('); i >= 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}

// firstLine returns the first line of s (for compact evidence text).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// jsHandlerBodyRe matches a function/handler whose body we scan for an opening
// auth check, covering the shapes meta-framework and hand-rolled handlers use:
//
//	export async function GET(req) { ...        (Next.js App Router route handler)
//	export const action = async ({request}) => { ...   (Remix/SvelteKit action)
//	async function handler(req, res) { ...       (Next.js Pages API / Express)
//	export default async function handler(...) { ...
//	  findOne(@Param() id) { ...                 (Nest handler method — body gate)
//
// Group 1 = the handler name (when a named function/const), used as the lookup
// key. The match index of the opening `{` is recovered separately so the body
// span can be isolated with findMatchingBrace.
var jsHandlerBodyRe = regexp.MustCompile(
	`(?m)(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\([^;{]*\)\s*(?::[^={;]+)?\{` +
		`|(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?\([^;{]*\)\s*(?::[^=>{;]+)?=>\s*\{`,
)

// indexAuthBodyChecks scans every recognisable handler body in the file for an
// opening home-rolled auth check and returns the results keyed BOTH by handler
// name and by the handler's 1-based start line (so callers can attribute by
// whichever the synthetic endpoint carries). Nest handler methods are indexed
// via the existing nestHandlerMethodRe; top-level/exported handlers via
// jsHandlerBodyRe.
func indexAuthBodyChecks(content string) (byName map[string]authBodyResult, byLine map[int]authBodyResult) {
	byName = map[string]authBodyResult{}
	byLine = map[int]authBodyResult{}
	if !authBodyFastPath(content) {
		return byName, byLine
	}

	record := func(name string, declOff int) {
		open := strings.IndexByte(content[declOff:], '{')
		if open < 0 {
			return
		}
		open += declOff
		close := findMatchingBrace(content, open)
		if close < 0 {
			close = len(content)
		}
		body := content[open+1 : close]
		baseLine := lineAtOffset(content, open)
		res, ok := recognizeAuthBodyOpener(body, baseLine)
		if !ok {
			return
		}
		if name != "" {
			if _, exists := byName[name]; !exists {
				byName[name] = res
			}
		}
		byLine[lineAtOffset(content, declOff)] = res
	}

	// Top-level / exported function + arrow handlers (meta-framework & plain).
	for _, loc := range jsHandlerBodyRe.FindAllStringSubmatchIndex(content, -1) {
		name := ""
		if loc[2] >= 0 {
			name = content[loc[2]:loc[3]]
		} else if loc[4] >= 0 {
			name = content[loc[4]:loc[5]]
		}
		record(name, loc[0])
	}

	// Nest handler methods (class methods — `findOne(@Param() id) { ... }`).
	for _, loc := range nestHandlerMethodRe.FindAllStringSubmatchIndex(content, -1) {
		record(content[loc[2]:loc[3]], loc[0])
	}

	return byName, byLine
}

// authBodyFastPath is the cheap substring gate: skip the (relatively expensive)
// body scan unless the file mentions at least one auth-check idiom token.
func authBodyFastPath(content string) bool {
	for _, tok := range []string{
		"require", "authorize", "authenticate", "assertCan", "assert",
		"checkPermission", "checkAuth", "getServerSession", "getSession",
		"currentUser", "getCurrentUser", "isAuthenticated", "ensureAuth",
		"hasPermission", "hasRole", "protect", "verifyAuth", "verifyToken",
		"if (!session", "if(!session", "if (!user", "if(!user", "if (!auth", "if(!auth",
	} {
		if strings.Contains(content, tok) {
			return true
		}
	}
	return false
}

// authBodyPolicy builds an AuthPolicy from a resolved home-rolled body check.
// method="check" (the route is gated by an inline authorization check in the
// handler body) at HIGH confidence — the binding is route-direct (the check is
// IN the handler), not an inherited file-scope default.
func authBodyPolicy(res authBodyResult, file string) AuthPolicy {
	return AuthPolicy{
		Required: true, Method: "check", Confidence: "high",
		Roles:       dedupeNonEmpty(res.roles),
		Permissions: dedupeNonEmpty(res.permissions),
		Scopes:      dedupeNonEmpty(res.scopes),
		SourceChain: []AuthSignal{{
			Kind: "check", Text: res.text, File: file, Line: res.line,
		}},
	}
}

// authBodyEndpointKey attributes a body-check result to a synthetic endpoint by
// the endpoint's source_handler name first, then its StartLine. Returns
// (policy, true) when a body check covers this endpoint.
func authBodyEndpointKey(
	e *types.EntityRecord, byName map[string]authBodyResult, byLine map[int]authBodyResult, file string,
) (AuthPolicy, bool) {
	if handler := nestHandlerName(e.Properties["source_handler"]); handler != "" {
		if res, ok := byName[handler]; ok {
			return authBodyPolicy(res, file), true
		}
	}
	if e.StartLine > 0 {
		if res, ok := byLine[e.StartLine]; ok {
			return authBodyPolicy(res, file), true
		}
	}
	return AuthPolicy{}, false
}
