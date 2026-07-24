package javascript

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// express_global_middleware.go — Express/Koa global middleware wiring (#4380).
//
// Generalizes the NestJS global-DI fix (#4329) to Express/Koa. An Express (or
// Koa) app registers cross-cutting middleware app-wide via app.use(...):
//
//	app.use(helmet())            — factory call → binds the factory `helmet`
//	app.use(cookieParser())      — factory call → binds the factory `cookieParser`
//	app.use(authMiddleware)      — bare symbol  → binds the middleware function
//	app.use(errorHandler)        — bare symbol  → binds the error-handling middleware
//	app.use('/api', apiRouter)   — mount        → binds the router at the path
//
// PRE-FIX: each app.use(...) produced only a standalone SCOPE.Pattern
// "middleware" / "mount" entity with NO edge from the app to the referenced
// middleware/router symbol, so the registered middleware function/router looked
// orphan / dead and the app-wide pipeline was invisible.
//
// POST-FIX: a synthetic `app` application entity owns an app → middleware USES
// edge for every app.use(...) registration, marked global=true with
// di_role=middleware|router and a 0-based pipeline `order`. The edge target is
// the resolved middleware/router symbol (a bare identifier, or the callee of a
// factory call), which binds to the real function/router entity through the
// real resolve.BuildIndex symbol table. Conditional/env-gated registration is
// deferred to the order epic (#4334).

// expressAppEntityName is the synthetic owner name for app-level global
// middleware wiring emitted from an Express/Koa app's app.use(...) calls.
const expressAppEntityName = "app"

// reExpressUse captures the head of an `<obj>.use(` call. Group 1 is the
// receiver identifier (app / koa / router var); the argument list is read with
// balanced-paren scanning from the captured '(' so nested factory calls such as
// rateLimit({ windowMs: 1000 }) are not truncated.
var reExpressUse = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\.\s*use\s*\(`)

// extractExpressGlobalMiddleware returns app.use(...) USES edges keyed by the
// synthetic `app` owner name (#4380). Each registration yields one
// app → middleware|router USES edge with di_role + a 0-based pipeline order.
// routerVars lets a path-mounted router (app.use('/p', r)) be role-tagged as a
// router rather than a plain middleware.
func extractExpressGlobalMiddleware(src string, routerVars map[string]bool) map[string][]types.RelationshipRecord {
	out := map[string][]types.RelationshipRecord{}
	order := 0
	for _, m := range reExpressUse.FindAllStringSubmatchIndex(src, -1) {
		open := m[1] - 1 // index of the '(' captured at the end of the match
		args := nestBalanced(src, open, '(', ')')
		params := nestSplitParams(args)
		if len(params) == 0 {
			continue
		}

		// Determine the registration shape from the first top-level argument.
		first := strings.TrimSpace(params[0])
		mounted := false
		target := first
		if isExpressStringLiteral(first) {
			// Path-mounted form: app.use('/api', router|middleware). The mount
			// path is the first arg; the bound symbol is the second.
			if len(params) < 2 {
				continue
			}
			mounted = true
			target = strings.TrimSpace(params[1])
		}

		sym := expressMiddlewareSymbol(target)
		if sym == "" {
			continue
		}

		role := "middleware"
		if mounted && routerVars[sym] {
			role = "router"
		} else if routerVars[sym] {
			role = "router"
		}

		out[expressAppEntityName] = append(out[expressAppEntityName], types.RelationshipRecord{
			FromID: expressAppEntityName,
			ToID:   sym,
			Kind:   string(types.RelationshipKindUses),
			Properties: types.Props{
				{K: "di_role", V: role},
				{K: "di_scope", V: "global"},
				{K: "framework", V: "express"},
				{K: "global", V: "true"},
				{K: "order", V: itoaJS(order)},
				{K: "via", V: "express_app_use"},
			},
		})
		order++
	}
	return out
}

// expressHasGlobalMiddleware reports whether src contains any `<obj>.use(` call,
// so the caller knows to emit the synthetic `app` owner entity.
func expressHasGlobalMiddleware(src string) bool {
	return reExpressUse.MatchString(src)
}

// expressMiddlewareSymbol resolves the middleware/router symbol an app.use(...)
// argument binds to. A bare identifier `authMiddleware` binds to the function;
// a factory call `helmet()` / `express.json()` binds to the callee leaf
// (`helmet` / `json`). Inline function/arrow expressions and config objects
// yield "" (no resolvable symbol — honest-partial).
func expressMiddlewareSymbol(arg string) string {
	a := strings.TrimSpace(arg)
	if a == "" {
		return ""
	}
	// Inline middleware: function (...) / (req,res,next) => / async ... — no
	// named symbol to bind.
	if strings.HasPrefix(a, "function") || strings.HasPrefix(a, "async") ||
		strings.HasPrefix(a, "(") || a[0] == '{' || a[0] == '[' {
		return ""
	}
	// `new Foo(...)` → Foo.
	a = strings.TrimSpace(strings.TrimPrefix(a, "new "))
	// Take the callee/identifier head up to the first call/access boundary.
	if i := strings.IndexAny(a, " ([{,;=>"); i >= 0 {
		// Preserve a dotted member path (express.json) so we can pick its leaf;
		// only cut at a real boundary char, not '.'.
		a = a[:i]
	}
	a = strings.TrimSpace(a)
	// For a dotted factory like express.json, bind to the leaf callee `json`
	// (the bare `express` namespace is never a middleware entity).
	if i := strings.LastIndexByte(a, '.'); i >= 0 {
		a = a[i+1:]
	}
	a = strings.TrimSpace(a)
	if a == "" || !isExpressIdentStart(a[0]) {
		return ""
	}
	return a
}

// isExpressStringLiteral reports whether the argument chunk is a quoted string
// literal (a route-mount path), i.e. begins with ' " or `.
func isExpressStringLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return s[0] == '\'' || s[0] == '"' || s[0] == '`'
}

// isExpressIdentStart reports whether b can start a JS identifier.
func isExpressIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// itoaJS renders a small non-negative int without pulling in strconv at the
// call sites that already use the package-local itoa convention.
func itoaJS(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
