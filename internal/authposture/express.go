// express.go — the Express/Fastify auth-posture resolver (#4710; new registry
// member, no prior stub).
//
// Express-family Node frameworks express authorization through MIDDLEWARE in the
// route handler chain rather than structured RBAC — many apps have no
// per-permission grants at all. The engine stamps the middleware chain onto the
// endpoint/handler (auth_middleware / middleware / auth_required), with a
// raw-source fallback scanning the route registration:
//
//   - passport.authenticate(...) / requireAuth / ensureAuthenticated / isAuth →
//     authenticated-only.
//   - requireRole("admin") / hasRole("admin") / checkRole(...)                → role.
//   - requireScope("items:read") / checkScope(...)                            → scope.
//   - requireAdmin / requireSuperuser / isSuperAdmin                          → superuser.
//   - no auth middleware                                                       → public.
//
// LOWER priority by design (#4710): Express RBAC is frequently absent, so this
// resolver resolves to authenticated-only or public in the common case and only
// surfaces a role/scope when a recognisable guard middleware names one. Output
// normalises into the shared {Kind, Literal} vocabulary.
package authposture

import (
	"regexp"
	"strings"
)

type expressResolver struct{}

func (expressResolver) Name() string { return "express" }

var (
	expressRoleRe      = regexp.MustCompile(`(?:require_?Role|has_?Role|check_?Role)\s*\(\s*\[?\s*["']([^"']+)["']`)
	expressScopeRe     = regexp.MustCompile(`(?:require_?Scope|has_?Scope|check_?Scope)\s*\(\s*\[?\s*["']([^"']+)["']`)
	expressSuperuserRe = regexp.MustCompile(`(?i)require_?(?:Admin|Superuser)|is_?Super(?:Admin)?|ensure_?Admin`)
	expressAuthMwRe    = regexp.MustCompile(`(?i)passport\.authenticate|require_?Auth|ensure_?Auth(?:enticated)?|is_?Auth(?:enticated)?|jwt(?:Auth)?|verify_?Token|authenticate\b`)
)

// Resolve decodes an Express/Fastify auth signal. Recognises the framework when
// the entity carries an Express/Fastify framework hint OR a recognisable auth
// middleware prop/source. Reconciled props win; source middleware is the
// fallback.
func (x expressResolver) Resolve(sig Signal) (Posture, bool) {
	fw := strings.ToLower(sig.Framework)
	mw := firstNonEmpty(sig.prop("auth_middleware"), sig.prop("middleware"))
	roles := sig.prop("auth_roles")
	scopes := sig.prop("auth_scopes")
	authReq := sig.prop("auth_required")

	isExpress := strings.Contains(fw, "express") || strings.Contains(fw, "fastify") ||
		strings.Contains(fw, "koa") || strings.Contains(fw, "hapi") || strings.Contains(fw, "hono") ||
		mw != "" || roles != "" || scopes != "" || authReq != "" ||
		hasExpressAuthMiddleware(sig.Source)
	if !isExpress {
		return Posture{}, false
	}

	// (1) Reconciled role / scope props — tightest first.
	if roles != "" {
		return Posture{Kind: KindRole, Literal: firstCSV(roles), Detail: "auth_roles=" + roles}, true
	}
	if scopes != "" {
		return Posture{Kind: KindScope, Literal: firstCSV(scopes), Detail: "auth_scopes=" + scopes}, true
	}
	// (2) Middleware chain prop — classify the named guard.
	if mw != "" {
		if p, ok := postureFromExpressMiddleware(mw, "auth_middleware="+mw); ok {
			return p, true
		}
	}

	// (3) Source-middleware fallback.
	if src := sig.Source; src != "" {
		if m := expressRoleRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindRole, Literal: m[1], Detail: "requireRole(" + m[1] + ")"}, true
		}
		if m := expressScopeRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindScope, Literal: m[1], Detail: "requireScope(" + m[1] + ")"}, true
		}
		if expressSuperuserRe.MatchString(src) {
			return Posture{Kind: KindSuperuser, Detail: "requireAdmin/Superuser middleware"}, true
		}
		if expressAuthMwRe.MatchString(src) {
			return Posture{Kind: KindAuthenticated, Detail: "auth middleware (passport/requireAuth/jwt)"}, true
		}
	}

	// (4) Reconciled auth_required without a decodable middleware.
	switch authReq {
	case "true":
		return Posture{Kind: KindAuthenticated, Detail: "auth_required=true (no decodable middleware)"}, true
	case "false":
		return Posture{Kind: KindPublic, Detail: "auth_required=false (no auth middleware)"}, true
	}

	// Recognised as Express but no decodable guard → unknown (never false-public:
	// an Express route with NO middleware is open, but a weak hint that landed us
	// here must not masquerade as an intentional public endpoint).
	return Posture{Kind: KindUnknown, Detail: "Express/Fastify handler with no decodable auth middleware"}, true
}

// postureFromExpressMiddleware classifies a middleware chain string.
func postureFromExpressMiddleware(mw, detail string) (Posture, bool) {
	if m := expressRoleRe.FindStringSubmatch(mw); m != nil {
		return Posture{Kind: KindRole, Literal: m[1], Detail: detail}, true
	}
	if m := expressScopeRe.FindStringSubmatch(mw); m != nil {
		return Posture{Kind: KindScope, Literal: m[1], Detail: detail}, true
	}
	if expressSuperuserRe.MatchString(mw) {
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	if expressAuthMwRe.MatchString(mw) {
		return Posture{Kind: KindAuthenticated, Detail: detail}, true
	}
	return Posture{Kind: KindUnknown, Detail: detail + " (uninterpreted middleware)"}, true
}

// hasExpressAuthMiddleware reports whether src carries recognisable auth
// middleware.
func hasExpressAuthMiddleware(src string) bool {
	if src == "" {
		return false
	}
	return expressAuthMwRe.MatchString(src) ||
		expressRoleRe.MatchString(src) ||
		expressScopeRe.MatchString(src) ||
		expressSuperuserRe.MatchString(src)
}
