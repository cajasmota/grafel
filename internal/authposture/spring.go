// spring.go — the Spring Security auth-posture resolver (#4708; replaces the
// spring-security stub) plus the EFFECTIVE-GUARD PRECEDENCE ladder (#4674 — the
// Spring analog of the NestJS effective-guard fix #4667/#4676 and the DRF
// method▸class▸global fix #4675).
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
// EFFECTIVE PRECEDENCE (most-specific wins, mirroring how Spring Security itself
// evaluates a request — #4674):
//
//	1. METHOD level — a @PreAuthorize/@PostAuthorize/@Secured/@RolesAllowed/
//	   @PermitAll/@DenyAll annotation on the HANDLER method. Applies to THAT
//	   endpoint only; siblings without their own annotation inherit the class
//	   one. Read from the method-level props the engine stamps (auth_expression /
//	   pre_authorize / secured / roles_allowed / auth_roles) or the engine's
//	   already-resolved effective auth_guard stamp, with a handler-source
//	   fallback.
//	2. CLASS level — the same annotations on the @Controller/@RestController
//	   class. Applies to handlers WITHOUT their own method annotation. Read from
//	   the spring_class_* props the engine stamps from the controller annotation
//	   block (spring_class_pre_authorize / spring_class_secured /
//	   spring_class_roles_allowed).
//	3. GLOBAL level — a Spring Security SecurityFilterChain / HttpSecurity
//	   `authorizeHttpRequests` rule (`.requestMatchers("/path").hasRole(...) /
//	   .authenticated() / .permitAll()`) or a legacy WebSecurityConfigurerAdapter
//	   `antMatchers(...)` rule whose ant-pattern matches THIS route. Applies only
//	   when NEITHER a method nor a class annotation covers the handler. The engine
//	   matches the route's ant-pattern (where statically recoverable) and stamps
//	   the winning rule's authorization expression into spring_global_authorization.
//	   GLOBAL matching is best-effort: dynamically-built matchers, regex matchers,
//	   and SpEL access() rules the engine cannot statically resolve are NOT
//	   matched (documented gap) — the resolver then reports KindUnknown rather
//	   than false-public.
//
// The annotation literal is read first from the reconciled props the engine
// stamps and falls back to scanning the handler source. Output normalises into
// the shared {Kind, Literal} vocabulary so the diff core compares a Spring
// posture against the Django oracle or a NestJS posture directly.
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
	// Authenticated-only: the SpEL forms isAuthenticated()/isFullyAuthenticated()
	// AND the HttpSecurity DSL form `.authenticated()` / `.fullyAuthenticated()`
	// (the global filter-chain vocabulary, #4674).
	springIsAuthRe = regexp.MustCompile(`\bis(?:Fully)?Authenticated\s*\(\s*\)|\.\s*(?:fully)?[aA]uthenticated\s*\(\s*\)`)
	springAnonymousRe    = regexp.MustCompile(`\bisAnonymous\s*\(\s*\)|\bpermitAll\b`)

	// Annotation forms on raw source.
	springPreAuthorizeRe = regexp.MustCompile(`@(?:Pre|Post)Authorize\s*\(\s*["']([^"']+)["']`)
	springSecuredRe      = regexp.MustCompile(`@Secured\s*\(\s*(?:\{\s*)?["']([^"']+)["']`)
	springRolesAllowedRe = regexp.MustCompile(`@RolesAllowed\s*\(\s*(?:\{\s*)?["']([^"']+)["']`)
	springPermitAllAnnRe = regexp.MustCompile(`@PermitAll\b`)
	springDenyAllAnnRe   = regexp.MustCompile(`@DenyAll\b`)
)

// Resolve decodes a Spring Security auth signal. Recognises the framework when
// the entity carries a Spring auth prop OR a @PreAuthorize/@Secured/@RolesAllowed
// annotation in its source. It then resolves the EFFECTIVE posture down the
// most-specific-wins ladder (method ▸ class ▸ global, #4674).
func (s springSecurityResolver) Resolve(sig Signal) (Posture, bool) {
	if !s.recognises(sig) {
		return Posture{}, false
	}

	// (1) METHOD level — most specific, wins over class + global.
	if p, ok := s.methodPosture(sig); ok {
		return p, true
	}
	// (2) CLASS level — applies to handlers without their own method annotation.
	if p, ok := s.classPosture(sig); ok {
		return p, true
	}
	// (3) GLOBAL level — SecurityFilterChain / HttpSecurity rule matched to the
	// route (best-effort; absent when no statically-recoverable matcher applies).
	if p, ok := s.globalPosture(sig); ok {
		return p, true
	}

	// Recognised as Spring-secured but no method/class/global grant decodable →
	// unknown (never false-public).
	return Posture{Kind: KindUnknown, Detail: "Spring handler with no decodable auth annotation or matching filter-chain rule"}, true
}

// recognises reports whether the signal belongs to the Spring resolver: it
// carries a Spring auth prop (method, class, or global) or a Spring annotation
// in the handler source.
func (s springSecurityResolver) recognises(sig Signal) bool {
	return s.methodExpr(sig) != "" ||
		s.methodSecured(sig) != "" ||
		sig.prop("auth_roles") != "" ||
		sig.prop("auth_guard") != "" ||
		sig.prop("auth_method") == "spring-security" ||
		s.classExpr(sig) != "" || s.classSecured(sig) != "" ||
		sig.prop("spring_global_authorization") != "" ||
		hasSpringAnnotation(sig.Source)
}

// --- (1) METHOD level -------------------------------------------------------

func (s springSecurityResolver) methodExpr(sig Signal) string {
	return firstNonEmpty(
		sig.prop("auth_expression"),
		sig.prop("pre_authorize"),
		sig.prop("preauthorize"),
		sig.prop("post_authorize"),
	)
}

func (s springSecurityResolver) methodSecured(sig Signal) string {
	return firstNonEmpty(sig.prop("secured"), sig.prop("roles_allowed"))
}

// methodPosture resolves the method-level (handler) posture: reconciled
// @PreAuthorize expression ▸ @Secured/@RolesAllowed role literal ▸ bare
// auth_roles ▸ the engine's effective auth_guard stamp ▸ a handler-source
// annotation. Returns ok=false when the handler carries no method-level signal
// (the caller then falls to class, then global).
func (s springSecurityResolver) methodPosture(sig Signal) (Posture, bool) {
	// (1a) Reconciled @PreAuthorize expression — richest signal.
	if expr := s.methodExpr(sig); expr != "" {
		if p, ok := postureFromSpringExpression(expr, "auth_expression="+expr); ok {
			return p, true
		}
	}
	// (1b) @Secured / @RolesAllowed reconciled role literal.
	if secured := s.methodSecured(sig); secured != "" {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(firstCSV(secured)),
			Detail: "secured/roles_allowed=" + secured}, true
	}
	// (1c) Bare auth_roles list (engine-flattened @PreAuthorize/@Secured roles).
	if roles := sig.prop("auth_roles"); roles != "" {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(firstCSV(roles)),
			Detail: "auth_roles=" + roles}, true
	}
	// (1d) EFFECTIVE auth_guard stamp. When the engine resolves the most-specific
	// (method ▸ class) Spring annotation it can stamp the winning annotation text
	// into auth_guard; decode it the same way (mirrors the NestJS effective-guard
	// decode #4667). This carries the SpEL/annotation text, NOT a guard class.
	if guard := sig.prop("auth_guard"); guard != "" {
		if p, ok := decodeSpringGuardText(guard, "auth_guard="+guard); ok {
			return p, true
		}
	}
	// (1e) Source-annotation fallback on the HANDLER body — same priority order.
	if p, ok := decodeSpringSource(sig.Source, "method"); ok {
		return p, true
	}
	return Posture{}, false
}

// --- (2) CLASS level --------------------------------------------------------

func (s springSecurityResolver) classExpr(sig Signal) string {
	return firstNonEmpty(
		sig.prop("spring_class_pre_authorize"),
		sig.prop("spring_class_post_authorize"),
		sig.prop("class_pre_authorize"),
	)
}

func (s springSecurityResolver) classSecured(sig Signal) string {
	return firstNonEmpty(
		sig.prop("spring_class_secured"),
		sig.prop("spring_class_roles_allowed"),
		sig.prop("class_secured"),
		sig.prop("class_roles_allowed"),
	)
}

// classPosture resolves the controller-class-level posture, applied to handlers
// that carry no method-level annotation of their own. Read from the spring_class_*
// props the engine stamps from the @Controller/@RestController annotation block.
func (s springSecurityResolver) classPosture(sig Signal) (Posture, bool) {
	if expr := s.classExpr(sig); expr != "" {
		if p, ok := postureFromSpringExpression(expr, "class @PreAuthorize("+expr+")"); ok {
			return p, true
		}
	}
	if secured := s.classSecured(sig); secured != "" {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(firstCSV(secured)),
			Detail: "class @Secured/@RolesAllowed=" + secured}, true
	}
	return Posture{}, false
}

// --- (3) GLOBAL level -------------------------------------------------------

// globalPosture resolves the Spring Security HttpSecurity / SecurityFilterChain
// authorization rule the engine matched to THIS route (by ant-pattern, where
// statically recoverable). The matched rule's authorization expression is
// stamped into spring_global_authorization, e.g.:
//
//	spring_global_authorization = `requestMatchers("/admin/**").hasRole("ADMIN")`
//	spring_global_authorization = `antMatchers("/public/**").permitAll()`
//	spring_global_authorization = `requestMatchers("/api/**").authenticated()`
//
// Best-effort by design (#4674): the engine only matches statically-recoverable
// ant-patterns; dynamic/regex matchers and SpEL access() rules are not matched,
// in which case the prop is absent and this returns ok=false (the caller then
// reports KindUnknown — never false-public).
func (s springSecurityResolver) globalPosture(sig Signal) (Posture, bool) {
	rule := sig.prop("spring_global_authorization")
	if rule == "" {
		return Posture{}, false
	}
	if p, ok := decodeSpringGuardText(rule, "global filter-chain rule: "+rule); ok {
		return p, true
	}
	// A matched global rule we recognise as Spring but cannot decode (e.g. a
	// custom .access(beanRef) rule) is at minimum an authenticated gate, but we
	// classify it unknown so the diff never false-equivalents a custom rule.
	return Posture{Kind: KindUnknown, Detail: "global filter-chain rule matched but uninterpreted: " + rule}, true
}

// --- shared decoders --------------------------------------------------------

// decodeSpringGuardText decodes an engine-stamped annotation/rule text into the
// shared vocabulary. It accepts both the @PreAuthorize SpEL body forms
// (hasRole/hasAuthority/permitAll/...) AND the HttpSecurity DSL fluent forms
// (.hasRole("X")/.permitAll()/.authenticated()/.denyAll()), since both express
// the same authorization vocabulary. Returns ok=false when no token is found.
func decodeSpringGuardText(text, detail string) (Posture, bool) {
	// Explicit @PreAuthorize/@Secured annotation forms embedded in the text.
	if m := springPreAuthorizeRe.FindStringSubmatch(text); m != nil {
		return postureFromSpringExpression(m[1], detail)
	}
	if m := springSecuredRe.FindStringSubmatch(text); m != nil {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]), Detail: detail}, true
	}
	if m := springRolesAllowedRe.FindStringSubmatch(text); m != nil {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]), Detail: detail}, true
	}
	if springPermitAllAnnRe.MatchString(text) {
		return Posture{Kind: KindPublic, Detail: detail}, true
	}
	if springDenyAllAnnRe.MatchString(text) {
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	// SpEL / HttpSecurity-DSL expression body (shared vocabulary).
	return postureFromSpringExpression(text, detail)
}

// decodeSpringSource scans a raw handler/controller source body for a Spring
// annotation in @PreAuthorize ▸ @Secured ▸ @RolesAllowed ▸ @PermitAll ▸ @DenyAll
// priority order. tier is "method" or "class" (for the detail string only).
func decodeSpringSource(src, tier string) (Posture, bool) {
	if src == "" {
		return Posture{}, false
	}
	if m := springPreAuthorizeRe.FindStringSubmatch(src); m != nil {
		return postureFromSpringExpression(m[1], tier+" @PreAuthorize("+m[1]+")")
	}
	if m := springSecuredRe.FindStringSubmatch(src); m != nil {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]),
			Detail: tier + " @Secured(" + m[1] + ")"}, true
	}
	if m := springRolesAllowedRe.FindStringSubmatch(src); m != nil {
		return Posture{Kind: KindRole, Literal: stripRolePrefix(m[1]),
			Detail: tier + " @RolesAllowed(" + m[1] + ")"}, true
	}
	if springPermitAllAnnRe.MatchString(src) {
		return Posture{Kind: KindPublic, Detail: tier + " @PermitAll"}, true
	}
	if springDenyAllAnnRe.MatchString(src) {
		return Posture{Kind: KindSuperuser, Detail: tier + " @DenyAll"}, true
	}
	return Posture{}, false
}

// postureFromSpringExpression decodes a Spring Security SpEL expression
// (@PreAuthorize body) — or an equivalent HttpSecurity DSL fragment — into the
// shared vocabulary.
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
		springRolesAllowedRe.MatchString(src) ||
		springPermitAllAnnRe.MatchString(src) ||
		springDenyAllAnnRe.MatchString(src)
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
