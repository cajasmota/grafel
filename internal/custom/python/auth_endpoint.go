// auth_endpoint.go — endpoint-protection extraction for Python web frameworks
// (#3628 area #6 child: authz endpoint protection).
//
// The meta-audit (#3628) found the per-framework `auth_coverage` rows were
// unverified placeholders: the FastAPI / Flask / DRF custom extractors emitted
// route endpoints but never recorded WHICH endpoints are protected and BY WHAT.
// The parity oracle (Django → NestJS rewrite) needs the auth requirement on the
// endpoint itself to compare guard-for-guard.
//
// This file resolves a per-endpoint auth posture from static route-level signals
// and stamps it onto the route endpoint entity using the same flat property
// contract the Java (java_auth_policy.go) and JS/TS (http_endpoint_jsts_auth.go)
// resolvers already write, so archigraph_auth_coverage and the security
// dashboard light up uniformly:
//
//	auth_required   — "true" | "false"
//	auth_method     — "dependency" | "decorator" | "permission_classes"
//	auth_confidence — "high" | "medium" | "low"
//	auth_guard      — the recognised guard symbol (dependency fn / decorator /
//	                  permission class) — the MCP signal-2 key
//	auth_scopes     — comma-joined OAuth2 scopes (FastAPI Security(scopes=[...]))
//	auth_roles      — comma-joined roles (where a role-bearing guard is named)
//
// Supported surfaces:
//
//	FastAPI — a route handler whose signature carries `= Depends(get_current_user)`
//	          or `= Security(get_user, scopes=["items:read"])`, or a router/route
//	          `dependencies=[Depends(verify_token)]` kwarg. The dependency
//	          function is the guard; Security scopes are captured.
//	Flask   — a `@login_required` / `@roles_required('admin')` /
//	          `@permission_required('x')` decorator on the route (Flask-Login /
//	          Flask-Security / Flask-Principal).
//	DRF     — per-method `@permission_classes([IsAdminUser])` /
//	          `@action(..., permission_classes=[IsAuthenticated])` decorators.
//	          (Class-level permission_classes are stamped on the CLASS entity by
//	          django_drf_permissions.go; this covers the per-route decorator.)
//
// Honest-partial: a dynamic guard (a dependency resolved at runtime, a
// permission class assembled in get_permissions()) is out of scope here and is
// handled — for DRF class-level — by django_drf_permissions.go.
package python

import (
	"regexp"
	"sort"
	"strings"
)

// pyEndpointAuth is the resolved auth posture for one route endpoint.
type pyEndpointAuth struct {
	Required    bool
	Method      string // "dependency" | "decorator" | "permission_classes"
	Guard       string // primary guard symbol (evidence)
	Scopes      []string
	Roles       []string
	Permissions []string // fine-grained permission strings (DRF/Flask args)
	Confidence  string   // "high" | "medium" | "low"
	// Decorator is the Flask auth-decorator name (`roles_required`,
	// `permission_required`, …). Stamped as `auth_decorator` (#4752) so the flask
	// resolver can decode admin/superuser-by-decorator-name (e.g.
	// `@admin_required`) even when the decorator carries no explicit role arg.
	Decorator string
	found     bool // an explicit auth signal was recognised
	publicSet bool // an explicit public marker was recognised (AllowAny)
}

// stamp writes the resolved posture onto an endpoint Properties map, mirroring
// the Java/JS-TS flat contract. It is a no-op when no auth signal was found AND
// no explicit-public marker was set, leaving the endpoint's posture "unknown"
// (the caller/detector decides default-deny vs default-allow per repo).
func (a pyEndpointAuth) stamp(props map[string]string) {
	if props == nil {
		return
	}
	if !a.found && !a.publicSet {
		return
	}
	if a.found {
		props["auth_required"] = "true"
		props["auth_method"] = a.Method
		props["auth_confidence"] = a.Confidence
		if a.Guard != "" {
			props["auth_guard"] = a.Guard
		}
		if len(a.Scopes) > 0 {
			s := append([]string(nil), a.Scopes...)
			sort.Strings(s)
			props["auth_scopes"] = strings.Join(s, ",")
		}
		if len(a.Roles) > 0 {
			r := append([]string(nil), a.Roles...)
			sort.Strings(r)
			props["auth_roles"] = strings.Join(r, ",")
		}
		if len(a.Permissions) > 0 {
			p := append([]string(nil), a.Permissions...)
			sort.Strings(p)
			props["auth_permissions"] = strings.Join(p, ",")
			// #4752 — the flask resolver reads `auth_page` as a permission alias;
			// mirror the permission into it so a Flask-Principal
			// `@permission_required('export')` decodes structurally.
			props["auth_page"] = props["auth_permissions"]
		}
		// #4752 — stamp the decorator name so the flask resolver decodes
		// admin/superuser-by-name (`@admin_required` / `@superuser_required`) where
		// the decorator carries no explicit role/permission arg.
		if a.Decorator != "" {
			props["auth_decorator"] = a.Decorator
		}
		return
	}
	// Explicit public (AllowAny / @permission_classes([AllowAny])).
	props["auth_required"] = "false"
	props["auth_method"] = a.Method
	props["auth_confidence"] = a.Confidence
}

// ---------------------------------------------------------------------------
// FastAPI: Depends / Security in the route signature + dependencies= kwarg
// ---------------------------------------------------------------------------

// faSecurityCallRe captures a `Security(dep, scopes=[...])` call so we can pull
// both the dependency function and its OAuth2 scopes. Group 1 = dependency
// symbol, group 2 = the raw scopes list (may be empty).
var faSecurityCallRe = regexp.MustCompile(
	`Security\s*\(\s*([A-Za-z_][\w.]*)\s*(?:,[^)]*scopes\s*=\s*\[([^\]]*)\])?[^)]*\)`)

// faDependsCallRe captures a `Depends(dep)` call. Group 1 = dependency symbol.
// A bare `Depends()` (no arg, FastAPI infers from the type annotation) yields
// no guard symbol but still counts as a dependency-injection site.
var faDependsCallRe = regexp.MustCompile(`Depends\s*\(\s*([A-Za-z_][\w.]*)?\s*\)`)

// faQuotedRe pulls quoted tokens out of a scopes / roles list.
var faQuotedRe = regexp.MustCompile(`['"]([^'"]+)['"]`)

// faAuthDependencyNames recognises whether a FastAPI dependency function is an
// AUTH dependency (vs. a plain DI dependency like `get_db`). FastAPI has no
// syntactic auth marker — auth is a convention — so we recognise the dominant
// auth-dependency naming idioms. A Security(...) call is *always* auth
// (OAuth2/scopes), regardless of name.
var faAuthDependencyRe = regexp.MustCompile(
	`(?i)(?:current[_]?user|active[_]?user|authenticated|auth|oauth|jwt|token|login|` +
		`verify[_]?token|get[_]?user|require[_]?|principal|identity|api[_]?key|bearer|session[_]?user|scopes?)`)

// resolveFastAPIRouteAuth scans a FastAPI route block (the decorator line
// through the handler signature) for Depends()/Security() auth dependencies and
// the route-level `dependencies=[...]` kwarg. `signature` is the handler def's
// parameter region; `decoratorArgs` is the route decorator's argument list
// (where `dependencies=[Depends(...)]` may appear).
func resolveFastAPIRouteAuth(signature, decoratorArgs string) pyEndpointAuth {
	var a pyEndpointAuth

	// 1. Security(dep, scopes=[...]) anywhere in the signature → always auth.
	if m := faSecurityCallRe.FindStringSubmatch(signature); m != nil {
		a.found = true
		a.Method = "dependency"
		a.Confidence = "high"
		a.Guard = leafSymbol(m[1])
		if len(m) > 2 && m[2] != "" {
			for _, q := range faQuotedRe.FindAllStringSubmatch(m[2], -1) {
				if tok := strings.TrimSpace(q[1]); tok != "" {
					a.Scopes = append(a.Scopes, tok)
				}
			}
		}
		return a
	}

	// 2. dependencies=[Depends(verify_token)] route/router kwarg → auth when the
	//    dependency name reads as an auth guard.
	if g, ok := faDependenciesKwargAuth(decoratorArgs); ok {
		a.found = true
		a.Method = "dependency"
		a.Confidence = "high"
		a.Guard = g
		return a
	}

	// 3. Depends(get_current_user) in the signature → auth when the dependency
	//    name reads as an auth guard. A plain `Depends(get_db)` is NOT auth.
	for _, m := range faDependsCallRe.FindAllStringSubmatch(signature, -1) {
		dep := m[1]
		if dep == "" {
			continue
		}
		if faAuthDependencyRe.MatchString(leafSymbol(dep)) {
			a.found = true
			a.Method = "dependency"
			a.Confidence = "high"
			a.Guard = leafSymbol(dep)
			return a
		}
	}

	return a
}

// faDependenciesKwargAuth inspects a route/router-decorator argument list for a
// `dependencies=[Depends(x), Security(y, scopes=[...])]` kwarg and reports the
// first recognised auth guard. Returns (guard, true) on a match.
func faDependenciesKwargAuth(decoratorArgs string) (string, bool) {
	idx := strings.Index(decoratorArgs, "dependencies")
	if idx < 0 {
		return "", false
	}
	region := decoratorArgs[idx:]
	if sm := faSecurityCallRe.FindStringSubmatch(region); sm != nil {
		return leafSymbol(sm[1]), true
	}
	for _, m := range faDependsCallRe.FindAllStringSubmatch(region, -1) {
		if m[1] != "" && faAuthDependencyRe.MatchString(leafSymbol(m[1])) {
			return leafSymbol(m[1]), true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Flask: @login_required / @roles_required('x') / @permission_required('x')
// ---------------------------------------------------------------------------

// flAuthDecoratorRe captures a Flask-Login / Flask-Security / Flask-Principal
// auth decorator on a route. Group 1 = decorator name, group 2 = the raw
// argument list (roles/permissions, may be empty for @login_required).
var flAuthDecoratorRe = regexp.MustCompile(
	`@(login_required|fresh_login_required|roles_required|roles_accepted|` +
		`permission_required|auth_required|auth\.login_required|token_required|jwt_required|` +
		`admin_required|superuser_required|staff_required|requires_auth)\b\s*(?:\(([^)]*)\))?`)

// resolveFlaskDecoratorAuth scans the decorator block preceding a Flask route
// handler for a recognised auth decorator. `decoratorBlock` is the text between
// the route decorator and the `def` (the stacked decorators).
func resolveFlaskDecoratorAuth(decoratorBlock string) pyEndpointAuth {
	var a pyEndpointAuth
	m := flAuthDecoratorRe.FindStringSubmatch(decoratorBlock)
	if m == nil {
		return a
	}
	a.found = true
	a.Method = "decorator"
	a.Confidence = "high"
	a.Guard = m[1]
	a.Decorator = m[1]
	if len(m) > 2 && m[2] != "" {
		// `@roles_required('admin')` / `@roles_accepted(...)` name roles;
		// `@permission_required('app.delete_order')` names a fine-grained
		// permission. Route each decorator's quoted args to the right bucket so
		// the captured value answers "what permission does this route require?".
		toPermissions := m[1] == "permission_required"
		for _, q := range faQuotedRe.FindAllStringSubmatch(m[2], -1) {
			tok := strings.TrimSpace(q[1])
			if tok == "" {
				continue
			}
			if toPermissions {
				a.Permissions = append(a.Permissions, tok)
			} else {
				a.Roles = append(a.Roles, tok)
			}
		}
	}
	return a
}

// ---------------------------------------------------------------------------
// DRF: per-method @permission_classes([...]) / @action(permission_classes=[...])
// ---------------------------------------------------------------------------

// drfPermissionClassesRe captures a `@permission_classes([IsAdminUser])`
// decorator. Group 1 = the raw class list.
var drfPermissionClassesRe = regexp.MustCompile(`@permission_classes\s*\(\s*\[([^\]]*)\]`)

// drfActionPermsRe captures the `permission_classes=[...]` kwarg inside an
// `@action(...)` (or `@api_view`-adjacent) decorator. Group 1 = the raw list.
var drfActionPermsRe = regexp.MustCompile(`permission_classes\s*=\s*\[([^\]]*)\]`)

// drfPublicPermissions are permission classes that grant anonymous access; an
// endpoint whose ONLY permission classes are public markers is explicitly open.
var drfPublicPermissions = map[string]bool{
	"AllowAny": true,
}

// resolveDRFDecoratorAuth scans a decorator block for a per-method DRF
// permission declaration. Returns a posture that is `found` (protected) when a
// non-public permission class is present, or `publicSet` when the only
// permission class is AllowAny.
func resolveDRFDecoratorAuth(decoratorBlock string) pyEndpointAuth {
	var a pyEndpointAuth
	var raw string
	if m := drfPermissionClassesRe.FindStringSubmatch(decoratorBlock); m != nil {
		raw = m[1]
	} else if m := drfActionPermsRe.FindStringSubmatch(decoratorBlock); m != nil {
		raw = m[1]
	} else {
		return a
	}

	classes := drfPermClassList(raw)
	if len(classes) == 0 {
		return a
	}
	nonPublic := make([]string, 0, len(classes))
	for _, c := range classes {
		if !drfPublicPermissions[c] {
			nonPublic = append(nonPublic, c)
		}
	}
	if len(nonPublic) == 0 {
		// Only AllowAny → explicitly public.
		a.publicSet = true
		a.Method = "permission_classes"
		a.Confidence = "high"
		return a
	}
	a.found = true
	a.Method = "permission_classes"
	a.Confidence = "high"
	a.Guard = nonPublic[0]
	// Role-bearing permission classes (IsAdminUser → ADMIN) are surfaced as a
	// guard only; DRF permission classes are opaque policies, not named roles,
	// so we do not synthesise speculative role strings.
	//
	// BUT a parameterised permission/scope class carries the SPECIFIC required
	// grant as a literal arg — `HasPermission('orders.delete')`,
	// `HasScope('read')` — so capture those into permissions/scopes.
	a.Permissions = append(a.Permissions, drfPermissionArgs(raw)...)
	a.Scopes = append(a.Scopes, drfScopeArgs(decoratorBlock, raw)...)
	return a
}

// drfScopeClassRe matches an OAuth2 scope permission class in a
// permission_classes list (django-oauth-toolkit `TokenHasScope` /
// `TokenHasReadWriteScope`, DRF-style `HasScope`). Group 1 = the class name,
// group 2 = its optional call args (`HasScope('read')`).
var drfScopeClassRe = regexp.MustCompile(`\b(TokenHas\w*Scope\w*|HasScope|HasAnyScope)\b\s*(?:\(([^)]*)\))?`)

// drfPermClassCallRe matches a permission-class entry that carries explicit
// literal args, e.g. `HasPermission('orders.delete')` /
// `RequiresPermission("app.edit")`. Group 1 = class name, group 2 = raw args.
var drfPermClassCallRe = regexp.MustCompile(`\b(\w*Permission\w*)\s*\(([^)]*)\)`)

// drfRequiredScopesRe matches a `required_scopes = ['read', 'write']` attribute
// that django-oauth-toolkit's TokenHasScope reads. Group 1 = the raw list. The
// attribute may sit alongside the view in the same decorator block region.
var drfRequiredScopesRe = regexp.MustCompile(`required_scopes\s*=\s*\[([^\]]*)\]`)

// drfPermissionArgs extracts the literal permission strings carried as call
// args by a parameterised permission class (`HasPermission('orders.delete')`).
// A scope class (TokenHasScope/HasScope) is handled by drfScopeArgs instead, so
// it is skipped here to avoid double-classifying a scope as a permission.
func drfPermissionArgs(raw string) []string {
	var out []string
	for _, m := range drfPermClassCallRe.FindAllStringSubmatch(raw, -1) {
		if drfScopeClassRe.MatchString(m[1]) {
			continue
		}
		for _, q := range faQuotedRe.FindAllStringSubmatch(m[2], -1) {
			if tok := strings.TrimSpace(q[1]); tok != "" {
				out = append(out, tok)
			}
		}
	}
	return out
}

// drfScopeArgs extracts the OAuth2 scopes an endpoint requires. Two sources:
// an inline scope-class call arg (`HasScope('read')`) and a `required_scopes`
// attribute that the TokenHasScope permission class consults. The
// `required_scopes` attribute is captured only when a scope permission class is
// actually present (otherwise the kwarg is unrelated noise).
func drfScopeArgs(decoratorBlock, raw string) []string {
	var out []string
	scopeClassPresent := false
	for _, m := range drfScopeClassRe.FindAllStringSubmatch(raw, -1) {
		scopeClassPresent = true
		if len(m) > 2 && m[2] != "" {
			for _, q := range faQuotedRe.FindAllStringSubmatch(m[2], -1) {
				if tok := strings.TrimSpace(q[1]); tok != "" {
					out = append(out, tok)
				}
			}
		}
	}
	if scopeClassPresent {
		if rm := drfRequiredScopesRe.FindStringSubmatch(decoratorBlock); rm != nil {
			for _, q := range faQuotedRe.FindAllStringSubmatch(rm[1], -1) {
				if tok := strings.TrimSpace(q[1]); tok != "" {
					out = append(out, tok)
				}
			}
		}
	}
	return out
}

// drfPermClassList parses a permission-class list literal into leaf class
// names, dropping module qualifiers and call args
// (`permissions.IsAdminUser` → `IsAdminUser`, `HasScope("x")` → `HasScope`).
func drfPermClassList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if leaf := leafSymbol(strings.TrimSpace(part)); leaf != "" {
			out = append(out, leaf)
		}
	}
	return out
}

// pyCallArgRegion returns the source span from `open` (the index just after an
// open paren) to its matching close paren, tracking nested parens / brackets /
// braces and skipping string literals. Used to isolate a handler def's
// parameter region so multi-line signatures (the FastAPI norm) are scanned in
// full. Bounded so a missing close paren can't run away.
func pyCallArgRegion(source string, open int) string {
	depth := 1
	i := open
	for i < len(source) && i-open < 8192 {
		c := source[i]
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return source[open:i]
			}
		case '"', '\'':
			i = pySkipString(source, i, c)
			continue
		}
		i++
	}
	end := open + 8192
	if end > len(source) {
		end = len(source)
	}
	return source[open:end]
}

// pySkipString advances past a quoted string literal starting at `i` (whose
// quote char is q), honouring backslash escapes. Returns the index of the char
// after the closing quote (or len(source) if unterminated).
func pySkipString(source string, i int, q byte) int {
	i++
	for i < len(source) {
		c := source[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == q {
			return i + 1
		}
		i++
	}
	return len(source)
}

// leafSymbol reduces a dotted / called reference to its bare leaf identifier:
// `permissions.IsAdminUser` → `IsAdminUser`, `deps.get_user()` → `get_user`.
func leafSymbol(ref string) string {
	ref = strings.TrimSpace(ref)
	if paren := strings.IndexByte(ref, '('); paren >= 0 {
		ref = ref[:paren]
	}
	if dot := strings.LastIndexByte(ref, '.'); dot >= 0 {
		ref = ref[dot+1:]
	}
	return strings.TrimSpace(ref)
}
