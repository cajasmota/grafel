// nestjs.go — the NestJS auth-posture resolver (ticket #4422, flagship pair).
//
// NestJS expresses endpoint authorization through guard decorators plus the
// reconciled posture properties the engine stamps onto the handler/endpoint:
//
//   - auth_required   : "true" when a guard enforces authentication.
//   - auth_method     : the guard mechanism (e.g. "jwt", "guard").
//   - auth_guard      : the guard class name(s) (e.g. "PageGuard", "RolesGuard").
//   - auth_roles      : comma-joined role literals from @Roles(...).
//   - auth_scopes     : comma-joined scope literals.
//
// plus the v3-specific @Require* decorator literals the rewrite uses to mirror
// the Django page/action grants:
//
//   - @RequirePage("client_admin")   → page grant on that slug.
//   - @RequireAction("export")       → action grant on that codename.
//   - @Public()                      → public.
//   - @Authenticated() / @UseGuards  → authenticated-only.
//   - @RequireSuperuser()            → superuser.
//
// The decorator literals are read from the require_page / require_action /
// require_superuser / is_public properties the engine reconciled (these are the
// "now-accurate reconciled posture" the ticket references), with a source-text
// fallback that scans the handler decorators directly when the properties are
// absent. The output normalises into the same {Kind, Literal} vocabulary the
// Django resolver targets, so the diff core compares them directly.
package authposture

import (
	"regexp"
	"strings"
)

type nestJSResolver struct{}

func (nestJSResolver) Name() string { return "nestjs" }

var (
	requirePageRe       = regexp.MustCompile(`@RequirePage\s*\(\s*["']([^"']+)["']`)
	requireActionRe     = regexp.MustCompile(`@RequireAction\s*\(\s*["']([^"']+)["']`)
	requireRoleRe       = regexp.MustCompile(`@(?:Roles|RequireRole)\s*\(\s*["']([^"']+)["']`)
	requireSuperuserRe  = regexp.MustCompile(`@RequireSuperuser\s*\(`)
	publicDecoratorRe   = regexp.MustCompile(`@Public\s*\(`)
	authenticatedDecoRe = regexp.MustCompile(`@Authenticated\s*\(|@UseGuards\s*\(`)

	// guardArgRe captures the FIRST argument of an engine-stamped guard decorator
	// such as `@RequirePage(PermissionPage.Buildings)` or
	// `@RequireAction(PermissionAction.Lite)`. The arg is frequently a TS enum
	// member (PermissionPage.Buildings), not a quoted literal, so this matches the
	// identifier/enum form too — the quoted-literal regexes above only fire on the
	// raw-source fallback path where decorators carry string literals.
	guardPageArgRe   = regexp.MustCompile(`@Require(?:Any)?Page\s*\(\s*([^,)]+)`)
	guardActionArgRe = regexp.MustCompile(`@RequireAction\s*\(\s*([^,)]+)`)
)

// guardLiteral normalises an engine-stamped guard argument into the shared
// posture literal. For an enum member (`PermissionPage.Buildings`) the literal
// is the member name (`Buildings`); for a quoted/bare slug it is the slug as
// written. This is what the diff core compares (after NormalizeKey) against the
// Django page/action slug, so an enum member and the Django slug it mirrors
// align under separator/case folding.
func guardLiteral(arg string) string {
	arg = strings.TrimSpace(arg)
	arg = strings.Trim(arg, `"'`+"`")
	if i := strings.LastIndexByte(arg, '.'); i >= 0 {
		arg = arg[i+1:]
	}
	return strings.TrimSpace(arg)
}

// resolveEffectiveGuard decodes the EFFECTIVE guard the ENGINE already resolved
// for this endpoint (handler method-decorator ▸ class controller-decorator ▸
// global APP_GUARD — the engine stamps the most-specific winner into auth_guard
// per `Reflector.getAllAndOverride([handler, class])`). The resolver's job here
// is ONLY to decode that already-effective decorator text into the shared
// {Kind, Literal} vocabulary — it must NOT re-collapse a per-handler
// @RequirePage/@RequireAction down to plain authenticated (the #4667 bug: the
// resolver ignored auth_guard's decorator text and every page/action grant fell
// through to KindAuthenticated, masking RBAC drift against the Django oracle).
func resolveEffectiveGuard(sig Signal) (Posture, bool) {
	guard := sig.prop("auth_guard")
	if guard == "" {
		return Posture{}, false
	}
	// Tightest grant first, matching the property/source priority order.
	if requireSuperuserRe.MatchString(guard) {
		return Posture{Kind: KindSuperuser, Detail: "auth_guard=" + guard}, true
	}
	if m := guardPageArgRe.FindStringSubmatch(guard); m != nil {
		if lit := guardLiteral(m[1]); lit != "" {
			return Posture{Kind: KindPage, Literal: lit, Detail: "auth_guard=" + guard}, true
		}
	}
	if m := guardActionArgRe.FindStringSubmatch(guard); m != nil {
		if lit := guardLiteral(m[1]); lit != "" {
			return Posture{Kind: KindAction, Literal: lit, Detail: "auth_guard=" + guard}, true
		}
	}
	if m := requireRoleRe.FindStringSubmatch(guard); m != nil {
		return Posture{Kind: KindRole, Literal: m[1], Detail: "auth_guard=" + guard}, true
	}
	// A recognised guard with no decodable page/action/role literal (e.g.
	// @Authenticated(), @AuthenticatedOrInternalKey(), @UseGuards(JwtGuard)) is an
	// authenticated-only gate.
	return Posture{Kind: KindAuthenticated, Detail: "auth_guard=" + guard}, true
}

// Resolve decodes a NestJS auth signal. Recognises the framework when the entity
// carries any Nest auth property OR Nest @Require*/@Public/@UseGuards decorators
// in its source. Property-derived posture wins; source decorators are the
// fallback.
func (n nestJSResolver) Resolve(sig Signal) (Posture, bool) {
	isNest := sig.prop("require_page") != "" ||
		sig.prop("require_action") != "" ||
		sig.prop("require_superuser") != "" ||
		sig.prop("is_public") != "" ||
		sig.prop("auth_guard") != "" ||
		sig.prop("auth_required") != "" ||
		hasNestDecorator(sig.Source)
	if !isNest {
		return Posture{}, false
	}

	// (1) Property-derived reconciled posture — tightest grant first.
	if sig.prop("require_superuser") == "true" {
		return Posture{Kind: KindSuperuser, Detail: "@RequireSuperuser (reconciled prop)"}, true
	}
	if v := sig.prop("require_page"); v != "" {
		return Posture{Kind: KindPage, Literal: v, Detail: "@RequirePage(" + v + ") (reconciled prop)"}, true
	}
	if v := sig.prop("require_action"); v != "" {
		return Posture{Kind: KindAction, Literal: v, Detail: "@RequireAction(" + v + ") (reconciled prop)"}, true
	}
	if v := sig.prop("auth_roles"); v != "" {
		return Posture{Kind: KindRole, Literal: firstCSV(v), Detail: "auth_roles=" + v}, true
	}
	if v := sig.prop("auth_scopes"); v != "" {
		return Posture{Kind: KindScope, Literal: firstCSV(v), Detail: "auth_scopes=" + v}, true
	}
	if sig.prop("is_public") == "true" {
		return Posture{Kind: KindPublic, Detail: "@Public (reconciled prop)"}, true
	}
	// Explicit public verdict from the engine (@Public()/@AllowAnonymous → the
	// metadata pass stamps auth_required=false with no guard). Must win over the
	// authenticated fallback so a deliberately-public handler is not mislabelled.
	if sig.prop("auth_required") == "false" && sig.prop("auth_guard") == "" {
		return Posture{Kind: KindPublic, Detail: "auth_required=false (engine public verdict)"}, true
	}
	// (1b) EFFECTIVE guard decode (#4667). The engine already resolved the
	// most-specific guard (handler ▸ class ▸ global) and stamped the winning
	// decorator text into auth_guard. Decode its page/action/role/superuser
	// literal here — DO NOT collapse a per-handler @RequirePage/@RequireAction to
	// plain authenticated (that downgrade masked RBAC drift vs the Django oracle).
	if p, ok := resolveEffectiveGuard(sig); ok {
		return p, true
	}
	if sig.prop("auth_required") == "true" {
		return Posture{Kind: KindAuthenticated, Detail: "auth_required=true (no decodable guard literal)"}, true
	}

	// (2) Source-decorator fallback — same priority order.
	if src := sig.Source; src != "" {
		if requireSuperuserRe.MatchString(src) {
			return Posture{Kind: KindSuperuser, Detail: "@RequireSuperuser (decorator)"}, true
		}
		if m := requirePageRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindPage, Literal: m[1], Detail: "@RequirePage(" + m[1] + ")"}, true
		}
		if m := requireActionRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindAction, Literal: m[1], Detail: "@RequireAction(" + m[1] + ")"}, true
		}
		if m := requireRoleRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindRole, Literal: m[1], Detail: "@Roles(" + m[1] + ")"}, true
		}
		if publicDecoratorRe.MatchString(src) {
			return Posture{Kind: KindPublic, Detail: "@Public (decorator)"}, true
		}
		if authenticatedDecoRe.MatchString(src) {
			return Posture{Kind: KindAuthenticated, Detail: "@Authenticated/@UseGuards (decorator)"}, true
		}
	}

	// Recognised as Nest but no decodable grant → unknown (never false-public).
	return Posture{Kind: KindUnknown, Detail: "NestJS handler with no decodable auth decorator"}, true
}

// hasNestDecorator reports whether src carries a recognisable Nest auth decorator.
func hasNestDecorator(src string) bool {
	if src == "" {
		return false
	}
	return requirePageRe.MatchString(src) ||
		requireActionRe.MatchString(src) ||
		requireSuperuserRe.MatchString(src) ||
		requireRoleRe.MatchString(src) ||
		publicDecoratorRe.MatchString(src) ||
		authenticatedDecoRe.MatchString(src)
}

// firstCSV returns the first comma-separated token, trimmed.
func firstCSV(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
