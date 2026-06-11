// spring.go — the Spring Security auth-posture resolver (#4708; replaces the
// spring-security stub).
//
// Spring expresses method/class authorization through annotations the engine
// stamps onto the handler/controller, with a raw-source fallback:
//
//   - @PreAuthorize("hasRole('ADMIN')")        → role grant on ADMIN.
//   - @PreAuthorize("hasAuthority('export')")  → action/authority grant.
//   - @PreAuthorize("hasAnyRole('A','B')")     → role grant on the first role.
//   - @PreAuthorize("isAuthenticated()")       → authenticated-only.
//   - @PreAuthorize("permitAll()") / permitAll → public.
//   - @Secured("ROLE_ADMIN")                   → role grant (ROLE_ prefix folded).
//   - @RolesAllowed("ADMIN")                   → role grant (JSR-250).
//   - @PreAuthorize("hasRole('SUPERUSER') ...) → superuser when the role names it.
//
// The annotation literal is read first from the reconciled props the engine
// stamps (auth_expression / auth_roles / pre_authorize / secured / roles_allowed)
// and falls back to scanning the handler source. Output normalises into the
// shared {Kind, Literal} vocabulary so the diff core compares a Spring posture
// against the Django oracle or a NestJS posture directly.
package authposture

import (
	"regexp"
	"strings"
)

type springSecurityResolver struct{}

func (springSecurityResolver) Name() string { return "spring-security" }

var (
	springHasRoleRe      = regexp.MustCompile(`has(?:Any)?Role\s*\(\s*["']([^"']+)["']`)
	springHasAuthorityRe = regexp.MustCompile(`has(?:Any)?Authority\s*\(\s*["']([^"']+)["']`)
	springPermitAllRe    = regexp.MustCompile(`\bpermitAll\b`)
	springDenyAllRe      = regexp.MustCompile(`\bdenyAll\b`)
	springIsAuthRe       = regexp.MustCompile(`\bisAuthenticated\s*\(\s*\)|\bisFullyAuthenticated\s*\(\s*\)`)
	springAnonymousRe    = regexp.MustCompile(`\bisAnonymous\s*\(\s*\)|\bpermitAll\b`)

	// Annotation forms on raw source.
	springPreAuthorizeRe = regexp.MustCompile(`@PreAuthorize\s*\(\s*["']([^"']+)["']`)
	springSecuredRe      = regexp.MustCompile(`@Secured\s*\(\s*(?:\{\s*)?["']([^"']+)["']`)
	springRolesAllowedRe = regexp.MustCompile(`@RolesAllowed\s*\(\s*(?:\{\s*)?["']([^"']+)["']`)
)

// Resolve decodes a Spring Security auth signal. Recognises the framework when
// the entity carries a Spring auth prop OR a @PreAuthorize/@Secured/@RolesAllowed
// annotation in its source. Reconciled props win; source annotations are the
// fallback.
func (s springSecurityResolver) Resolve(sig Signal) (Posture, bool) {
	expr := firstNonEmpty(
		sig.prop("auth_expression"),
		sig.prop("pre_authorize"),
		sig.prop("preauthorize"),
	)
	secured := firstNonEmpty(sig.prop("secured"), sig.prop("roles_allowed"))
	roles := sig.prop("auth_roles")

	hasAnyProp := expr != "" || secured != "" || roles != "" ||
		sig.prop("auth_method") == "spring-security"
	hasSource := hasSpringAnnotation(sig.Source)
	if !hasAnyProp && !hasSource {
		return Posture{}, false
	}

	// (1) Reconciled @PreAuthorize expression — richest signal.
	if expr != "" {
		if p, ok := postureFromSpringExpression(expr, "auth_expression="+expr); ok {
			return p, true
		}
	}
	// (2) @Secured / @RolesAllowed reconciled role literal.
	if secured != "" {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(firstCSV(secured)),
			Detail: "secured/roles_allowed=" + secured}, true
	}
	// (3) Bare auth_roles list.
	if roles != "" {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(firstCSV(roles)),
			Detail: "auth_roles=" + roles}, true
	}

	// (4) Source-annotation fallback — same priority order.
	if src := sig.Source; src != "" {
		if m := springPreAuthorizeRe.FindStringSubmatch(src); m != nil {
			if p, ok := postureFromSpringExpression(m[1], "@PreAuthorize("+m[1]+")"); ok {
				return p, true
			}
		}
		if m := springSecuredRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]),
				Detail: "@Secured(" + m[1] + ")"}, true
		}
		if m := springRolesAllowedRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]),
				Detail: "@RolesAllowed(" + m[1] + ")"}, true
		}
	}

	// Recognised as Spring-secured but no decodable grant → unknown (never
	// false-public).
	return Posture{Kind: KindUnknown, Detail: "Spring handler with no decodable auth annotation"}, true
}

// postureFromSpringExpression decodes a Spring Security SpEL expression
// (@PreAuthorize body) into the shared vocabulary.
func postureFromSpringExpression(expr, detail string) (Posture, bool) {
	// denyAll is the tightest — treat as superuser-equivalent (nothing short of
	// the strongest grant passes).
	if springDenyAllRe.MatchString(expr) {
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	if m := springHasRoleRe.FindStringSubmatch(expr); m != nil {
		role := stripRolePrefix(m[1])
		if strings.EqualFold(role, "SUPERUSER") || strings.EqualFold(role, "ROOT") {
			return Posture{Kind: KindSuperuser, Detail: detail}, true
		}
		return Posture{Kind: KindRole, Literal: role, Detail: detail}, true
	}
	if m := springHasAuthorityRe.FindStringSubmatch(expr); m != nil {
		return Posture{Kind: KindAction, Literal: m[1], Detail: detail}, true
	}
	if springPermitAllRe.MatchString(expr) || springAnonymousRe.MatchString(expr) {
		return Posture{Kind: KindPublic, Detail: detail}, true
	}
	if springIsAuthRe.MatchString(expr) {
		return Posture{Kind: KindAuthenticated, Detail: detail}, true
	}
	// A SpEL expression we recognise as Spring but cannot map to a concrete
	// grant (e.g. a custom bean call `@customAuth.check(#id)`) is an
	// authenticated-only gate at minimum (it gates the method) — but classify as
	// unknown so the diff never false-equivalents a custom rule.
	return Posture{Kind: KindUnknown, Detail: detail + " (uninterpreted SpEL)"}, true
}

// hasSpringAnnotation reports whether src carries a recognisable Spring Security
// method annotation.
func hasSpringAnnotation(src string) bool {
	if src == "" {
		return false
	}
	return springPreAuthorizeRe.MatchString(src) ||
		springSecuredRe.MatchString(src) ||
		springRolesAllowedRe.MatchString(src)
}

// stripRolePrefix folds the conventional Spring "ROLE_" authority prefix so a
// @Secured("ROLE_ADMIN") and a @PreAuthorize("hasRole('ADMIN')") align on the
// same "ADMIN" literal (Spring's hasRole auto-prepends ROLE_).
func stripRolePrefix(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, "ROLE_")
}

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}
