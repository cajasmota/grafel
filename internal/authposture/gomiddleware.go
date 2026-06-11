// gomiddleware.go — the Go HTTP middleware auth-posture resolver (#4543;
// replaces the go-middleware stub).
//
// Go HTTP frameworks (Gin / Echo / Chi / net-http) express endpoint
// authorization through MIDDLEWARE CHAINS rather than structured RBAC
// decorators. Middleware is attached at three nesting levels:
//
//   - ROUTER level — `r.Use(AuthMiddleware())` / `e.Use(middleware.JWT(...))` /
//     `r.Use(AuthCtx)` applies to every route on the router.
//   - GROUP level   — `group := r.Group("/api"); group.Use(jwtAuth())` /
//     `r.Group(func(r chi.Router){ r.Use(Auth); r.Get(...) })` applies to the
//     routes registered under that group/sub-router.
//   - ROUTE level   — `r.GET("/x", AuthRequired(), handler)` /
//     `AuthMiddleware(handler)` (net/http wrapper) applies to that one route.
//
// HONEST HEURISTIC LIMIT (#4543): Go middleware carries NO standard auth
// annotation — a middleware is "auth" only by its IDENTIFIER. This resolver
// therefore classifies by NAME (auth / jwt / authenticate / requireAuth /
// bearer / session → authenticated; RequireRole("admin") / permission → role).
// A route that reaches NO recognisable auth-ish middleware is reported
// `unknown`, NOT `public`: name-heuristics cannot prove a route is intentionally
// open, so we UNDER-claim rather than emit a false-public that would mask a real
// RBAC regression in the diff. An explicit public marker (auth_required=false /
// a `Public`/`NoAuth`/`Anonymous` middleware) is the only path to `public`.
//
// The engine does not yet stamp the reconciled Go middleware chain as structured
// auth props onto the endpoint (only the gRPC-Go path stamps auth_*; see the
// prop-stamping follow-up filed alongside this resolver — like #4742-4745). So
// this resolver reads any generic auth_* props when present and otherwise
// degrades to scanning the route-registration / handler source. Output
// normalises into the shared {Kind, Literal} vocabulary so the diff core
// compares a Go posture against the Django oracle or a NestJS posture directly.
package authposture

import (
	"regexp"
	"strings"
)

type goMiddlewareResolver struct{}

func (goMiddlewareResolver) Name() string { return "go-middleware" }

var (
	// RequireRole("admin") / WithRole(`editor`) / RoleRequired("admin") — a role
	// middleware naming a role literal. Captures the role.
	goRoleMwRe = regexp.MustCompile(`(?i)(?:Require|With|Has|Check|Ensure)Roles?\s*\(\s*\[?\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`)
	// RequirePermission("export") / WithPermission(`x`) — a permission/ability
	// middleware naming an action literal.
	goPermMwRe = regexp.MustCompile(`(?i)(?:Require|With|Has|Check|Ensure)(?:Permission|Ability|Scope)s?\s*\(\s*\[?\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`)
	// RequireAdmin / RequireSuperuser / IsSuperAdmin / AdminOnly — superuser gate.
	goSuperuserMwRe = regexp.MustCompile(`(?i)(?:Require|Ensure|Is|Must)(?:Admin|Superuser|SuperAdmin)|Admin(?:Only|Required)|SuperuserOnly`)
	// auth-ish middleware name in a chain (.Use(...) / inline arg). Conservative
	// name set: auth / authenticate / jwt / bearer / requireAuth / session /
	// loginRequired / oauth / token. Word-boundary-ish to avoid "author".
	goAuthMwRe = regexp.MustCompile(`(?i)\b(?:auth(?:enticate(?:d|User)?|Required|Middleware|Ctx|Guard|N)?|jwt(?:Auth|Middleware)?|bearer(?:Auth|Token)?|requireAuth|requireLogin|loginRequired|ensureAuth(?:enticated)?|session(?:Auth)?|oauth2?|verifyToken|tokenAuth)\b`)
	// explicit PUBLIC / open middleware markers — only these earn a `public`.
	// Case-SENSITIVE exported-identifier form (Go middleware convention) so a
	// lowercase path segment like `r.Group("/public")` does NOT count as a public
	// middleware. Optional package qualifier (e.g. mw.AllowAnonymous).
	goPublicMwRe = regexp.MustCompile(`\b(?:\w+\.)?(?:Public|NoAuth|Anonymous|AllowAnonymous|SkipAuth|OptionalAuth|Unauthenticated)\b`)
	// chain-attachment forms we scan inside source: .Use(...) and inline route args.
	goUseRe = regexp.MustCompile(`\.Use\s*\(`)
)

// Resolve decodes a Go HTTP middleware auth signal. Recognises the framework
// when the entity carries a Go-HTTP framework hint OR a recognisable auth/role
// middleware construct in its props or source. Reconciled props win; source
// (route registration / handler body) scanning is the fallback.
func (g goMiddlewareResolver) Resolve(sig Signal) (Posture, bool) {
	fw := strings.ToLower(firstNonEmpty(sig.Framework, sig.prop("framework")))
	mw := firstNonEmpty(sig.prop("auth_middleware"), sig.prop("middleware"))
	roles := sig.prop("auth_roles")
	perms := firstNonEmpty(sig.prop("auth_permissions"), sig.prop("auth_scopes"))
	authReq := sig.prop("auth_required")
	src := sig.Source

	isGo := strings.Contains(fw, "gin") || strings.Contains(fw, "echo") ||
		strings.Contains(fw, "chi") || strings.Contains(fw, "fiber") ||
		strings.Contains(fw, "go-middleware") || strings.Contains(fw, "gohttp") ||
		strings.Contains(fw, "net/http") || strings.Contains(fw, "nethttp") ||
		strings.Contains(fw, "golang") || fw == "go" ||
		mw != "" || roles != "" || perms != "" || authReq != "" ||
		hasGoAuthMiddleware(src) ||
		(goUseRe.MatchString(src) && goPublicMwRe.MatchString(src))
	if !isGo {
		return Posture{}, false
	}

	// (1) EXPLICIT PUBLIC OVERRIDE — auth_required=false or a public marker
	// middleware in the chain. Only an explicit signal earns `public`.
	if authReq == "false" {
		return Posture{Kind: KindPublic, Detail: "auth_required=false (no auth middleware in chain)"}, true
	}
	if src != "" && goPublicMwRe.MatchString(src) && !hasGoAuthMiddleware(src) {
		return Posture{Kind: KindPublic, Detail: "explicit public/anonymous middleware, no auth in chain"}, true
	}

	// (2) Reconciled role / permission props — tightest first.
	if roles != "" {
		if r := firstCSV(roles); isGoSuperuserName(r) {
			return Posture{Kind: KindSuperuser, Detail: "auth_roles=" + roles}, true
		}
		return Posture{Kind: KindRole, Literal: firstCSV(roles), Detail: "auth_roles=" + roles}, true
	}
	if perms != "" {
		return Posture{Kind: KindAction, Literal: firstCSV(perms), Detail: "auth_permissions=" + perms}, true
	}

	// (3) Middleware-chain prop — classify the named middleware.
	if mw != "" {
		if p, ok := decodeGoMiddleware(mw, "auth_middleware="+mw); ok {
			return p, true
		}
	}

	// (4) Source fallback — scan the router/group/route registration + handler.
	if src != "" {
		if p, ok := decodeGoSource(src); ok {
			return p, true
		}
	}

	// (5) Reconciled auth_required=true without a decodable role/permission.
	if authReq == "true" {
		return Posture{Kind: KindAuthenticated, Detail: "auth_required=true (auth middleware, no decodable role/permission)"}, true
	}

	// Recognised as a Go HTTP route but no recognisable auth-ish middleware
	// reaches it. HONEST under-claim: name-heuristics cannot prove the route is
	// intentionally open, so report unknown (never false-public).
	return Posture{Kind: KindUnknown, Detail: "Go HTTP route with no recognisable auth/role middleware in the reconciled chain (name-heuristic limit)"}, true
}

// decodeGoMiddleware classifies a middleware chain string (a comma/space list of
// middleware identifiers) in role ▸ permission ▸ superuser ▸ auth priority.
func decodeGoMiddleware(mw, detail string) (Posture, bool) {
	if m := goRoleMwRe.FindStringSubmatch(mw); m != nil {
		role := strings.TrimSpace(m[1])
		if isGoSuperuserName(role) {
			return Posture{Kind: KindSuperuser, Detail: detail}, true
		}
		return Posture{Kind: KindRole, Literal: role, Detail: detail}, true
	}
	if m := goPermMwRe.FindStringSubmatch(mw); m != nil {
		return Posture{Kind: KindAction, Literal: strings.TrimSpace(m[1]), Detail: detail}, true
	}
	if goSuperuserMwRe.MatchString(mw) {
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	if goPublicMwRe.MatchString(mw) && !goAuthMwRe.MatchString(mw) {
		return Posture{Kind: KindPublic, Detail: detail}, true
	}
	if goAuthMwRe.MatchString(mw) {
		return Posture{Kind: KindAuthenticated, Detail: detail}, true
	}
	return Posture{}, false
}

// decodeGoSource scans a route-registration / handler source body, honouring the
// router ▸ group ▸ route nesting by simply taking the TIGHTEST recognisable
// middleware anywhere in the reachable source (role ▸ permission ▸ superuser ▸
// auth). The engine's source slice for the endpoint already includes the
// enclosing group/router `.Use(...)` chain when available.
func decodeGoSource(src string) (Posture, bool) {
	if m := goRoleMwRe.FindStringSubmatch(src); m != nil {
		role := strings.TrimSpace(m[1])
		if isGoSuperuserName(role) {
			return Posture{Kind: KindSuperuser, Detail: "RequireRole(" + role + ")"}, true
		}
		return Posture{Kind: KindRole, Literal: role, Detail: "RequireRole(" + role + ")"}, true
	}
	if m := goPermMwRe.FindStringSubmatch(src); m != nil {
		return Posture{Kind: KindAction, Literal: strings.TrimSpace(m[1]), Detail: "RequirePermission(" + m[1] + ")"}, true
	}
	if goSuperuserMwRe.MatchString(src) {
		return Posture{Kind: KindSuperuser, Detail: "admin/superuser middleware"}, true
	}
	if goAuthMwRe.MatchString(src) {
		return Posture{Kind: KindAuthenticated, Detail: "auth/jwt/session middleware in chain (.Use / inline)"}, true
	}
	// A bare `.Use(...)` with no recognisable auth name is not classifiable.
	return Posture{}, false
}

// isGoSuperuserName reports whether a role literal denotes a superuser/root role.
func isGoSuperuserName(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "superuser", "super-admin", "superadmin", "root", "super":
		return true
	}
	return false
}

// hasGoAuthMiddleware reports whether src carries a recognisable Go auth/role
// middleware construct.
func hasGoAuthMiddleware(src string) bool {
	if src == "" {
		return false
	}
	return goRoleMwRe.MatchString(src) ||
		goPermMwRe.MatchString(src) ||
		goSuperuserMwRe.MatchString(src) ||
		goAuthMwRe.MatchString(src)
}
