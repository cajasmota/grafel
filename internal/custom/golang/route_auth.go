// route_auth.go — endpoint-protection stamping for Go gin/echo route ops
// (#3734, child of #3628 area #6; sibling of #3696's Python auth_endpoint.go).
//
// #3696 established the flat per-endpoint auth contract: stamp the route
// `SCOPE.Operation/endpoint` entity itself with
//
//	auth_required   — "true" | "false"
//	auth_method     — "middleware"
//	auth_guard      — the recognised auth-middleware symbol (MCP signal key)
//	auth_kind       — coarse kind (jwt/oauth/basic/session/rbac/api_key/auth)
//	auth_confidence — "high" (route/group-direct) | "medium" (engine-wide .Use)
//
// gin/echo already emit auth *Pattern* entities for `.Use(...)` chains
// (helpers.go), but never bind the protection to a specific route op, and the
// route regexes stop at the path — so inline route middleware
// (`e.GET("/me", h, jwtMiddleware)`) is invisible. This resolver closes that
// gap by binding two route-level signals to the endpoint op:
//
//	group-level — a route group constructed with an auth middleware argument,
//	              e.g. `authorized := r.Group("/", AuthRequired())` (gin) /
//	              `g := e.Group("/admin", jwtMiddleware)` (echo). Every route
//	              registered on that group var inherits auth_required (HIGH —
//	              the binding is direct to the group the route is on).
//	inline      — an auth middleware passed in a route registration's argument
//	              chain after the path, e.g. `r.GET("/me", AuthRequired(), h)` /
//	              `e.GET("/admin", h, jwtMiddleware)` (echo accepts trailing
//	              middleware). HIGH — bound to the exact route.
//
// A weaker engine-wide `.Use(authMw)` registration applies to every route on
// that engine; we surface it at MEDIUM confidence only when no stronger
// route/group signal is present, mirroring the JS/TS app-level inheritance.
//
// Honest-partial: middleware resolved dynamically (a slice built at runtime, a
// conditional `.Use`) is out of scope; auth classification reuses the heuristic
// classifyAuthMiddleware catalog (helpers.go), so this is `partial`-grade
// data-flow, consistent with the rest of the Go auth surface.
package golang

import (
	"regexp"
	"strings"
)

// goRouteAuth is the resolved auth posture for one route op.
type goRouteAuth struct {
	Required   bool
	Guard      string // the recognised auth-middleware symbol (evidence)
	Kind       string // coarse auth kind from classifyAuthMiddleware
	Confidence string // "high" | "medium"
	found      bool
}

// stamp writes the resolved posture onto a route op's Properties map using the
// #3696 flat contract. No-op when no auth signal was found (leaves the op's
// posture unstamped — the default-deny/allow decision is the consumer's).
func (a goRouteAuth) stamp(props map[string]string) {
	if props == nil || !a.found {
		return
	}
	props["auth_required"] = "true"
	props["auth_method"] = "middleware"
	props["auth_confidence"] = a.Confidence
	if a.Guard != "" {
		props["auth_guard"] = a.Guard
		// Also stamp the recognised middleware symbol as the reconciled chain
		// (auth_middleware) so the cross-group auth-posture resolver
		// (internal/authposture/gomiddleware.go) decodes role/superuser from a
		// named guard like RequireAdmin / RequireRole("editor") in the LIVE diff
		// instead of degrading to a bare authenticated posture (#4746).
		props["auth_middleware"] = a.Guard
	}
	if a.Kind != "" {
		props["auth_kind"] = a.Kind
	}
}

// reGoGroupDecl matches a route-group construction with its full argument list:
//
//	authorized := r.Group("/", AuthRequired())
//	g := e.Group("/admin", jwtMiddleware, logger)
//
// Group 1 = the new group var, group 2 = the path literal, group 3 = the raw
// trailing args (middleware chain, may be empty). The args region is captured
// up to the line end; balanced-paren trimming happens in the caller.
var reGoGroupDecl = regexp.MustCompile(
	`(?m)(\w+)\s*:?=\s*\w+\.Group\s*\(\s*"([^"]*)"\s*((?:,[^\n\r]*)?)\)`,
)

// reGoRouteAuthScan matches a route registration and captures everything after
// the path so the inline middleware chain (between path and handler) can be
// inspected:
//
//	r.GET("/me", AuthRequired(), getMe)
//	e.POST("/admin", h, jwtMiddleware)
//
// Group 1 = router var, 2 = verb, 3 = path, 4 = the trailing args (everything
// after the path up to the registration's close paren, captured line-bounded).
var reGoRouteAuthScan = regexp.MustCompile(
	`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|CONNECT|TRACE|Any)\s*\(\s*"([^"]*)"\s*((?:,[^\n\r]*)?)\)`,
)

// goRouteAuthIndex holds the per-file resolved auth signals: which group vars
// are auth-protected, which (verb,path) routes carry inline auth middleware,
// and whether the engine has a global auth `.Use`.
type goRouteAuthIndex struct {
	// groupVars maps a group var → its resolved group-level auth posture.
	groupVars map[string]goRouteAuth
	// inline maps "<VERB> <path>" (the route's *own* path, pre-prefix) → posture.
	inline map[string]goRouteAuth
	// engineWide is the MEDIUM posture from an engine-level `.Use(authMw)`, or a
	// zero value when none was found.
	engineWide goRouteAuth
}

// buildGoRouteAuthIndex scans a Go source file once and resolves every
// route-level / group-level / engine-level auth signal. It is framework-shared
// (gin and echo use identical .Group / .VERB / .Use shapes).
func buildGoRouteAuthIndex(src string) goRouteAuthIndex {
	idx := goRouteAuthIndex{
		groupVars: map[string]goRouteAuth{},
		inline:    map[string]goRouteAuth{},
	}

	// Group-level: `g := r.Group("/x", authMw)` → g is protected.
	for _, m := range reGoGroupDecl.FindAllStringSubmatchIndex(src, -1) {
		groupVar := src[m[2]:m[3]]
		args := src[m[6]:m[7]]
		if a, ok := authFromArgChain(args); ok {
			a.Confidence = "high"
			idx.groupVars[groupVar] = a
		}
	}

	// Inline route middleware: `r.GET("/me", authMw, h)` → that route protected.
	for _, m := range reGoRouteAuthScan.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[m[4]:m[5]])
		path := src[m[6]:m[7]]
		args := src[m[8]:m[9]]
		if a, ok := authFromArgChain(args); ok {
			a.Confidence = "high"
			idx.inline[verb+" "+path] = a
		}
	}

	// Engine-wide `.Use(authMw)` → MEDIUM inheritance for every route.
	for _, uc := range findUseCalls(src) {
		for _, mw := range parseMiddlewareChain(uc.Args) {
			if mw.AuthKind != "" {
				idx.engineWide = goRouteAuth{
					Required: true, found: true, Confidence: "medium",
					Guard: mw.Name, Kind: mw.AuthKind,
				}
				break
			}
		}
		if idx.engineWide.found {
			break
		}
	}

	return idx
}

// resolve returns the auth posture for one route op, applying precedence:
// inline route middleware (HIGH) > the route's group var (HIGH) > engine-wide
// `.Use` (MEDIUM) > none. `routerVar` is the var the route was registered on
// (may be a protected group var); `verb`+`ownPath` identify the route's own
// registration (the un-prefixed path the inline index is keyed by).
func (idx goRouteAuthIndex) resolve(routerVar, verb, ownPath string) goRouteAuth {
	if a, ok := idx.inline[verb+" "+ownPath]; ok && a.found {
		return a
	}
	if a, ok := idx.groupVars[routerVar]; ok && a.found {
		return a
	}
	if idx.engineWide.found {
		return idx.engineWide
	}
	return goRouteAuth{}
}

// authFromArgChain inspects a trailing-argument region (the text after a path
// literal, e.g. `, AuthRequired(), getMe`) for a recognised auth middleware and
// returns the first match's posture. The leading comma and the final handler
// are tolerated — each top-level arg is classified independently.
func authFromArgChain(args string) (goRouteAuth, bool) {
	args = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), ","))
	if args == "" {
		return goRouteAuth{}, false
	}
	for _, part := range splitTopLevelArgs(args) {
		part = strings.TrimSpace(part)
		if part == "" || isStringLiteral(part) {
			continue
		}
		if kind := classifyAuthMiddleware(part); kind != "" {
			head := reMiddlewareCallHead.FindString(part)
			if head == "" {
				head = part
			}
			return goRouteAuth{
				Required: true, found: true,
				Guard: head, Kind: kind,
			}, true
		}
	}
	return goRouteAuth{}, false
}
