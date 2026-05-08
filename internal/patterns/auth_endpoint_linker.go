package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// authEndpointLinker detects auth middleware, guards, and annotations applied to endpoints.
// Matches Python auth_endpoint_linker.py.
type authEndpointLinker struct{}

var (
	authExpressUseMiddlewareRE  = regexp.MustCompile(`(?:app|router)\s*\.\s*use\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)`)
	authExpressRouteMiddlewareRE = regexp.MustCompile(`(?:app|router)\s*\.\s*(?:get|post|put|patch|delete|all)\s*\(\s*["'][^"']*["']\s*,\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*,`)
	authExpressPassportRE       = regexp.MustCompile(`passport\s*\.\s*authenticate\s*\(\s*["']([^"']+)["']`)
	authExpressVerifyTokenRE    = regexp.MustCompile(`\bverifyToken\b`)
	authSpringPreAuthorizeRE    = regexp.MustCompile(`@PreAuthorize\s*\(\s*(?:"([^"]*)"|'([^']*)')\s*\)`)
	authSpringSecuredRE         = regexp.MustCompile(`@Secured\s*\(\s*\{?(?:"([^"]*)"|'([^']*)')`)
	authSpringRolesAllowedRE    = regexp.MustCompile(`@RolesAllowed\s*\(\s*(?:"([^"]*)"|'([^']*)')`)
	authNestJSUseGuardsRE       = regexp.MustCompile(`@UseGuards\s*\(\s*([A-Za-z_$][A-Za-z0-9_$,\s]*)\s*\)`)
	authFastAPIDependsRE        = regexp.MustCompile(`Depends\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	authASPNetAuthorizeBareRE   = regexp.MustCompile(`\[Authorize\]`)
	authASPNetAuthorizeRolesRE  = regexp.MustCompile(`\[Authorize\s*\(\s*Roles\s*=\s*["']([^"']+)["']\s*\)\]`)
	authASPNetAuthorizePolicyRE = regexp.MustCompile(`\[Authorize\s*\(\s*Policy\s*=\s*["']([^"']+)["']\s*\)\]`)
)

var authExpressNonAuthTokens = map[string]bool{
	"cors": true, "json": true, "urlencoded": true, "static": true,
	"cookieParser": true, "compression": true, "helmet": true, "morgan": true,
}

var authFastAPIAuthDeps = map[string]bool{
	"get_current_user": true, "get_current_active_user": true, "oauth2_scheme": true,
	"verify_token": true, "get_current_superuser": true, "authenticate": true, "require_auth": true,
}

var authImportTokens = []string{
	"passport", "passport-jwt", "passport-local", "passport-google-oauth20",
	"@nestjs/passport", "AuthGuard", "JwtAuthGuard", "UseGuards",
	"PreAuthorize", "Secured", "RolesAllowed", "EnableMethodSecurity",
	"fastapi.security", "oauth2_scheme", "get_current_user",
	"Microsoft.AspNetCore.Authorization", "Authorize",
}

var authSourceTokens = []string{
	"passport.authenticate(", "verifyToken",
	"@PreAuthorize(", "@Secured(", "@RolesAllowed(", "@UseGuards(",
	"Depends(get_current_user", "Depends(oauth2_scheme",
	"[Authorize]", "[Authorize(",
}

func (a *authEndpointLinker) Category() string { return "auth_endpoint" }

func (a *authEndpointLinker) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range authImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	for _, tok := range authSourceTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (a *authEndpointLinker) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Express: router.use(authMiddleware)
	for _, m := range authExpressUseMiddlewareRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if authExpressNonAuthTokens[name] {
			continue
		}
		key := "express:use:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_express_"+name, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "express", "middleware_name": name}))
	}

	// Express: router.get("/path", authMiddleware, handler)
	for _, m := range authExpressRouteMiddlewareRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if authExpressNonAuthTokens[name] {
			continue
		}
		key := "express:route:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_express_route_"+name, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "express", "middleware_name": name}))
	}

	// Express: passport.authenticate("strategy")
	for _, m := range authExpressPassportRE.FindAllStringSubmatchIndex(src, -1) {
		strategy := src[m[2]:m[3]]
		key := "express:passport:" + strategy
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_express_passport_"+strategy, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "express", "annotation_name": "passport.authenticate", "middleware_name": strategy}))
	}

	// Express: verifyToken
	if authExpressVerifyTokenRE.MatchString(src) {
		key := "express:verifyToken"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"auth_policy_express_verifyToken", "SCOPE.Config", "auth_policy", language, 1,
				map[string]string{"kind": "auth_policy", "framework": "express", "middleware_name": "verifyToken"}))
		}
	}

	// Spring: @PreAuthorize
	for _, m := range authSpringPreAuthorizeRE.FindAllStringSubmatchIndex(src, -1) {
		expr := ""
		if m[2] >= 0 {
			expr = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			expr = src[m[4]:m[5]]
		}
		key := "spring:pre_authorize:" + expr
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_spring_pre_authorize", "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "spring", "annotation_name": "@PreAuthorize", "middleware_name": expr}))
	}

	// Spring: @Secured
	for _, m := range authSpringSecuredRE.FindAllStringSubmatchIndex(src, -1) {
		role := ""
		if m[2] >= 0 {
			role = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			role = src[m[4]:m[5]]
		}
		key := "spring:secured:" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_spring_secured", "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "spring", "annotation_name": "@Secured", "middleware_name": role}))
	}

	// Spring: @RolesAllowed
	for _, m := range authSpringRolesAllowedRE.FindAllStringSubmatchIndex(src, -1) {
		role := ""
		if m[2] >= 0 {
			role = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			role = src[m[4]:m[5]]
		}
		key := "spring:roles_allowed:" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_spring_roles_allowed", "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "spring", "annotation_name": "@RolesAllowed", "middleware_name": role}))
	}

	// NestJS: @UseGuards(Guard1, Guard2)
	for _, m := range authNestJSUseGuardsRE.FindAllStringSubmatchIndex(src, -1) {
		guardsRaw := src[m[2]:m[3]]
		for _, g := range strings.Split(guardsRaw, ",") {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			key := "nestjs:" + g
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"auth_policy_nestjs_"+g, "SCOPE.Config", "auth_policy", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "auth_policy", "framework": "nestjs", "annotation_name": "@UseGuards", "middleware_name": g}))
		}
	}

	// FastAPI: Depends(auth_fn)
	for _, m := range authFastAPIDependsRE.FindAllStringSubmatchIndex(src, -1) {
		dep := src[m[2]:m[3]]
		if !authFastAPIAuthDeps[dep] {
			continue
		}
		key := "fastapi:" + dep
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_fastapi_"+dep, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "fastapi", "middleware_name": dep}))
	}

	// ASP.NET: [Authorize(Roles="...")]
	for _, m := range authASPNetAuthorizeRolesRE.FindAllStringSubmatchIndex(src, -1) {
		roles := src[m[2]:m[3]]
		key := "aspnet:roles:" + roles
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_aspnet_roles_"+roles, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "aspnet", "annotation_name": "[Authorize(Roles)]", "middleware_name": roles}))
	}

	// ASP.NET: [Authorize(Policy="...")]
	for _, m := range authASPNetAuthorizePolicyRE.FindAllStringSubmatchIndex(src, -1) {
		policy := src[m[2]:m[3]]
		key := "aspnet:policy:" + policy
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"auth_policy_aspnet_policy_"+policy, "SCOPE.Config", "auth_policy", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "auth_policy", "framework": "aspnet", "annotation_name": "[Authorize(Policy)]", "middleware_name": policy}))
	}

	// ASP.NET: [Authorize] bare (first occurrence only)
	if authASPNetAuthorizeBareRE.MatchString(src) {
		key := "aspnet:authorize_bare"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"auth_policy_aspnet_authorize", "SCOPE.Config", "auth_policy", language, 1,
				map[string]string{"kind": "auth_policy", "framework": "aspnet", "annotation_name": "[Authorize]", "middleware_name": "Authorize"}))
		}
	}

	return results
}

func init() {
	Register(&authEndpointLinker{})
}
