// fastapi.go — the FastAPI auth-posture resolver (#4709; replaces the fastapi
// stub).
//
// FastAPI expresses endpoint authorization through security DEPENDENCIES injected
// into the path-operation signature, which the engine stamps onto the endpoint /
// handler (with a raw-source fallback):
//
//   - Depends(get_current_user) / Depends(require_login)  → authenticated-only.
//   - Security(get_current_user, scopes=["items:read"])    → scope grant on the
//     first scope.
//   - Depends(require_role("admin")) / RoleChecker(["admin"]) → role grant.
//   - Depends(require_superuser) / get_current_active_superuser → superuser.
//   - no security dependency                                → public (a FastAPI
//     path-op with NO Depends/Security gate is open).
//
// The dependency is read first from the reconciled props the engine stamps
// (auth_required / auth_scopes / auth_roles / security_dependency / depends) and
// falls back to scanning the path-op source for Depends(...) / Security(...).
// Output normalises into the shared {Kind, Literal} vocabulary.
package authposture

import (
	"regexp"
	"strings"
)

type fastAPIResolver struct{}

func (fastAPIResolver) Name() string { return "fastapi" }

var (
	// Security(dep, scopes=["a","b"]) — capture the first scope literal.
	fastapiSecurityScopeRe = regexp.MustCompile(`Security\s*\([^)]*scopes\s*=\s*\[\s*["']([^"']+)["']`)
	// A bare Security(...) with no scopes is authenticated-only.
	fastapiSecurityRe = regexp.MustCompile(`\bSecurity\s*\(`)
	// Depends(<dep>) — capture the dependency callable name.
	fastapiDependsRe = regexp.MustCompile(`Depends\s*\(\s*([A-Za-z_][\w.]*)`)
	// require_role("admin") / RoleChecker(["admin"]) inside a Depends.
	fastapiRoleCallRe = regexp.MustCompile(`(?:require_role|RoleChecker|has_role)\s*\(\s*\[?\s*["']([^"']+)["']`)

	// Superuser-naming dependency callables.
	fastapiSuperuserRe = regexp.MustCompile(`(?i)superuser|is_admin|require_admin`)
	// Authenticated-naming dependency callables.
	fastapiAuthDepRe = regexp.MustCompile(`(?i)current_user|current_active_user|require_login|require_auth|get_user|authenticated`)
)

// Resolve decodes a FastAPI auth signal. Recognises the framework when the entity
// carries a FastAPI auth prop OR a Depends(...)/Security(...) call in its source.
// Reconciled props win; source dependencies are the fallback.
func (f fastAPIResolver) Resolve(sig Signal) (Posture, bool) {
	scopes := sig.prop("auth_scopes")
	roles := sig.prop("auth_roles")
	dep := firstNonEmpty(sig.prop("security_dependency"), sig.prop("depends"))
	authReq := sig.prop("auth_required")

	isFastAPI := strings.Contains(strings.ToLower(sig.Framework), "fastapi") ||
		scopes != "" || roles != "" || dep != "" || authReq != "" ||
		hasFastAPISecurity(sig.Source)
	if !isFastAPI {
		return Posture{}, false
	}

	// (1) Reconciled scope / role / dependency props — tightest first.
	if v := sig.prop("require_superuser"); v == "true" {
		return Posture{Kind: KindSuperuser, Detail: "require_superuser (reconciled prop)"}, true
	}
	if roles != "" {
		return Posture{Kind: KindRole, Literal: firstCSV(roles), Detail: "auth_roles=" + roles}, true
	}
	if scopes != "" {
		return Posture{Kind: KindScope, Literal: firstCSV(scopes), Detail: "auth_scopes=" + scopes}, true
	}
	if dep != "" {
		if p, ok := postureFromFastAPIDep(dep, "security_dependency="+dep); ok {
			return p, true
		}
	}

	// (2) Source-dependency fallback.
	if src := sig.Source; src != "" {
		if m := fastapiSecurityScopeRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindScope, Literal: m[1], Detail: "Security(scopes=[" + m[1] + ", ...])"}, true
		}
		if m := fastapiRoleCallRe.FindStringSubmatch(src); m != nil {
			return Posture{Kind: KindRole, Literal: m[1], Detail: "Depends(role check " + m[1] + ")"}, true
		}
		if m := fastapiDependsRe.FindStringSubmatch(src); m != nil {
			if p, ok := postureFromFastAPIDep(m[1], "Depends("+m[1]+")"); ok {
				return p, true
			}
		}
		if fastapiSecurityRe.MatchString(src) {
			return Posture{Kind: KindAuthenticated, Detail: "Security(...) (no scopes)"}, true
		}
	}

	// (3) Reconciled auth_required posture without a decodable dependency.
	switch authReq {
	case "true":
		return Posture{Kind: KindAuthenticated, Detail: "auth_required=true (no decodable dependency)"}, true
	case "false":
		return Posture{Kind: KindPublic, Detail: "auth_required=false (no security dependency)"}, true
	}

	// Recognised as FastAPI but no decodable gate. A path-op with NO security
	// dependency at all is genuinely public; but since we got here via a weak
	// hint, classify unknown (never false-public) so a missed dependency does not
	// masquerade as an intentional open endpoint.
	return Posture{Kind: KindUnknown, Detail: "FastAPI path-op with no decodable security dependency"}, true
}

// postureFromFastAPIDep classifies a security-dependency callable name.
func postureFromFastAPIDep(dep, detail string) (Posture, bool) {
	if fastapiSuperuserRe.MatchString(dep) {
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	if fastapiAuthDepRe.MatchString(dep) {
		return Posture{Kind: KindAuthenticated, Detail: detail}, true
	}
	// A dependency we cannot name-classify still GATES the endpoint — but report
	// unknown so the diff never false-equivalents a custom dependency.
	return Posture{Kind: KindUnknown, Detail: detail + " (uninterpreted dependency)"}, true
}

// hasFastAPISecurity reports whether src carries a Depends(...)/Security(...).
func hasFastAPISecurity(src string) bool {
	if src == "" {
		return false
	}
	return fastapiDependsRe.MatchString(src) || fastapiSecurityRe.MatchString(src)
}
