// java_method_security.go — Method-level security annotation extraction (#3347).
//
// Delivers auth_coverage deepening for three frameworks where the
// java_auth_policy.go class-level handler already fires for Spring Boot, but
// where method-level annotations on WebFlux reactive handlers, JAX-RS resource
// methods, and Micronaut HTTP methods were previously untracked.
//
// Capability cells upgraded:
//
//	lang.java.framework.spring-webflux → Auth/auth_coverage  partial → full
//	lang.java.framework.jaxrs          → Auth/auth_coverage  missing → partial
//	lang.java.framework.micronaut      → Auth/auth_coverage  missing → partial
//
// Patterns detected per framework:
//
//	Spring WebFlux:
//	  @PreAuthorize("hasRole('ADMIN')")  on a method
//	  @Secured({"ROLE_ADMIN","ROLE_USER"}) on a method
//	  @RolesAllowed({"ADMIN"})           on a method
//	  @PermitAll / @DenyAll              on a method
//
//	JAX-RS:
//	  @RolesAllowed({"ADMIN"})  on a resource method or class
//	  @PermitAll                on a method or class
//	  @DenyAll                  on a method or class
//
//	Micronaut:
//	  @Secured("ADMIN")         on a method or class (Micronaut Security)
//	  @PermitAll                on a method
//	  @DenyAll                  on a method
//	  @RolesAllowed({"ADMIN"})  on a method (borrowed from Jakarta EE)
//
// Note: cross-file security configuration (SecurityFilterChain, application.yml
// security blocks) is beyond single-file regex; those cells stay partial.
package java

import (
	"regexp"
	"strings"
)

// ── framework gates ───────────────────────────────────────────────────────────

var methodSecurityWebFluxFrameworks = map[string]bool{
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var methodSecurityJaxrsFrameworks = map[string]bool{
	"jaxrs": true, "jax-rs": true,
	"microprofile": true, "eclipse-microprofile": true,
	"open_liberty": true, "payara": true,
}

var methodSecurityMicronautFrameworks = map[string]bool{
	"micronaut": true, "micronaut-core": true, "micronaut_core": true,
}

// ── security annotation regexes ───────────────────────────────────────────────

// msPreAuthorizeRE matches @PreAuthorize("expr") before a method.
// Group 1 = expression; Group 2 = method name.
var msPreAuthorizeRE = regexp.MustCompile(
	`(?s)@PreAuthorize\s*\(\s*"([^"]+)"\s*\)\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// msSecuredRE matches @Secured({"ROLE_X",...}) or @Secured("ROLE_X") on a method.
// Group 1 = roles text; Group 2 = method name.
var msSecuredRE = regexp.MustCompile(
	`(?s)@Secured\s*\(\s*(\{[^}]*\}|"[^"]+")\s*\)\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// msRolesAllowedRE matches @RolesAllowed({"X","Y"}) or @RolesAllowed("X") on a method.
// Group 1 = roles text; Group 2 = method name.
var msRolesAllowedRE = regexp.MustCompile(
	`(?s)@RolesAllowed\s*\(\s*(\{[^}]*\}|"[^"]+")\s*\)\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// msPermitAllRE matches @PermitAll on a method.
// Group 1 = method name.
var msPermitAllRE = regexp.MustCompile(
	`(?s)@PermitAll\b\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// msDenyAllRE matches @DenyAll on a method.
// Group 1 = method name.
var msDenyAllRE = regexp.MustCompile(
	`(?s)@DenyAll\b\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// msMicronautSecuredMethodRE matches Micronaut @Secured("ROLE_ADMIN") or
// @Secured(SecurityRule.IS_AUTHENTICATED) on a method.
// Group 1 = security expression/role; Group 2 = method name.
var msMicronautSecuredMethodRE = regexp.MustCompile(
	`(?s)@Secured\s*\(\s*(?:SecurityRule\.)?(\w+|"[^"]+")\s*\)\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public|protected|private|)\s+(?:static\s+)?` +
		`(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

// ── Extractor ─────────────────────────────────────────────────────────────────

// ExtractJavaMethodSecurity runs the method-level security annotation extractor.
// It fires for spring_webflux, jaxrs, and micronaut framework identifiers.
func ExtractJavaMethodSecurity(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" && ctx.Language != "kotlin" {
		return result
	}
	isWebFlux := methodSecurityWebFluxFrameworks[ctx.Framework]
	isJaxRS := methodSecurityJaxrsFrameworks[ctx.Framework]
	isMicronaut := methodSecurityMicronautFrameworks[ctx.Framework]
	if !isWebFlux && !isJaxRS && !isMicronaut {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	fw := ctx.Framework
	seenRefs := make(map[string]bool)

	// Quick-exit: no security annotations present.
	hasSecurityAnnotation := strings.Contains(source, "@PreAuthorize") ||
		strings.Contains(source, "@Secured") ||
		strings.Contains(source, "@RolesAllowed") ||
		strings.Contains(source, "@PermitAll") ||
		strings.Contains(source, "@DenyAll")
	if !hasSecurityAnnotation {
		return result
	}

	// ── @PreAuthorize ────────────────────────────────────────────────────────

	if isWebFlux {
		for _, m := range msPreAuthorizeRE.FindAllStringSubmatchIndex(source, -1) {
			expr := source[m[2]:m[3]]
			method := source[m[4]:m[5]]
			ownerCls := findEnclosingClass(source, m[0])
			roles := parseSpringSecurityExpression(expr)
			ref := "scope:pattern:spring_method_security:" + fp + ":" + ownerCls + "." + method
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: ownerCls + "." + method, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_PRE_AUTHORIZE",
				Ref:        ref,
				Properties: map[string]any{
					"security_annotation": "PreAuthorize",
					"expression":          expr,
					"roles":               roles,
					"auth_required":       true,
					"framework":           fw,
				},
			})
		}
	}

	// ── @Secured ─────────────────────────────────────────────────────────────

	if isWebFlux {
		for _, m := range msSecuredRE.FindAllStringSubmatchIndex(source, -1) {
			if len(m) < 6 || m[4] < 0 {
				continue
			}
			rolesText := source[m[2]:m[3]]
			methodName := source[m[4]:m[5]]
			ownerCls := findEnclosingClass(source, m[0])
			roles := parseRolesText(rolesText)
			ref := "scope:pattern:method_security_secured:" + fp + ":" + ownerCls + "." + methodName
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: ownerCls + "." + methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_SECURED",
				Ref:        ref,
				Properties: map[string]any{
					"security_annotation": "Secured",
					"roles":               roles,
					"auth_required":       true,
					"framework":           fw,
				},
			})
		}
	}

	if isMicronaut {
		for _, m := range msMicronautSecuredMethodRE.FindAllStringSubmatchIndex(source, -1) {
			if len(m) < 6 || m[4] < 0 {
				continue
			}
			rolesText := source[m[2]:m[3]]
			methodName := source[m[4]:m[5]]
			ownerCls := findEnclosingClass(source, m[0])
			roles := parseRolesText(rolesText)
			ref := "scope:pattern:method_security_mn_secured:" + fp + ":" + ownerCls + "." + methodName
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: ownerCls + "." + methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_MICRONAUT_SECURED",
				Ref:        ref,
				Properties: map[string]any{
					"security_annotation": "Secured",
					"roles":               roles,
					"auth_required":       true,
					"framework":           fw,
				},
			})
		}
	}

	// ── @RolesAllowed ─────────────────────────────────────────────────────────

	if isJaxRS || isMicronaut || isWebFlux {
		for _, m := range msRolesAllowedRE.FindAllStringSubmatchIndex(source, -1) {
			rolesText := source[m[2]:m[3]]
			method := source[m[4]:m[5]]
			ownerCls := findEnclosingClass(source, m[0])
			roles := parseRolesText(rolesText)
			ref := "scope:pattern:method_security_roles_allowed:" + fp + ":" + ownerCls + "." + method
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: ownerCls + "." + method, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_ROLES_ALLOWED",
				Ref:        ref,
				Properties: map[string]any{
					"security_annotation": "RolesAllowed",
					"roles":               roles,
					"auth_required":       true,
					"framework":           fw,
				},
			})
		}
	}

	// ── @PermitAll ────────────────────────────────────────────────────────────

	for _, m := range msPermitAllRE.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:pattern:method_security_permit_all:" + fp + ":" + ownerCls + "." + method
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + method, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_PERMIT_ALL",
			Ref:        ref,
			Properties: map[string]any{
				"security_annotation": "PermitAll",
				"auth_required":       false,
				"framework":           fw,
			},
		})
	}

	// ── @DenyAll ──────────────────────────────────────────────────────────────

	for _, m := range msDenyAllRE.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		ownerCls := findEnclosingClass(source, m[0])
		ref := "scope:pattern:method_security_deny_all:" + fp + ":" + ownerCls + "." + method
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerCls + "." + method, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DENY_ALL",
			Ref:        ref,
			Properties: map[string]any{
				"security_annotation": "DenyAll",
				"auth_required":       true,
				"framework":           fw,
			},
		})
	}

	return result
}

// ── helpers ───────────────────────────────────────────────────────────────────

// msRoleStringRE matches a single quoted role string.
var msRoleStringRE = regexp.MustCompile(`"([^"]+)"`)

// parseRolesText extracts individual role names from a @RolesAllowed /
// @Secured argument text, which may be a single string "ADMIN" or an
// array {"ADMIN","USER"}.
func parseRolesText(text string) []string {
	var roles []string
	for _, m := range msRoleStringRE.FindAllStringSubmatch(text, -1) {
		if r := strings.TrimSpace(m[1]); r != "" {
			roles = append(roles, r)
		}
	}
	return roles
}

// parseSpringSecurityExpression extracts role names from a SpEL expression
// like "hasRole('ADMIN') or hasAnyRole('USER','MOD')".
var msSpELRoleRE = regexp.MustCompile(`'([^']+)'`)

func parseSpringSecurityExpression(expr string) []string {
	var roles []string
	for _, m := range msSpELRoleRE.FindAllStringSubmatch(expr, -1) {
		if r := strings.TrimSpace(m[1]); r != "" {
			roles = append(roles, r)
		}
	}
	return roles
}
