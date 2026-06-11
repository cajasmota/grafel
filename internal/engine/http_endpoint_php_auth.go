// http_endpoint_php_auth.go — Laravel reconciled auth-middleware posture stamping
// onto the synthesized http_endpoint_definition entities (#4752).
//
// synthesizeLaravel (http_endpoint_php_producer.go) parses route + group
// middleware only for PREFIX synthesis; it never stamps the reconciled AUTH
// posture, so the authposture laravel resolver had nothing to decode for classic
// route/controller middleware (only api-platform endpoints carried auth props).
// This post-pass reconciles a route's own `->middleware(...)` chain with its
// enclosing `Route::group(['middleware'=>[...]])` middleware (resolving
// `auth` / `role:` / `can:`/`permission:` tokens and a `->withoutMiddleware('auth')`
// override) and stamps the flat auth contract the resolver reads:
//
//	auth_required   — "true" | "false" (withoutMiddleware override)
//	auth_middleware — the reconciled middleware chain string the resolver decodes
//	auth_roles      — the role: literal (spatie/permission)
//	auth_permissions— the can:/permission: ability literal
//	auth_method     — "middleware" | "without"
//	route_source    — the route registration slice for the source-scan fallback
//
// Mirrors the in-place post-pass pattern applyLaravelRateLimit uses: it only
// mutates Properties on the laravel endpoints this file just produced and reuses
// the same group-span keying so route↔group reconciliation matches the producer.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// A route/controller `->middleware('auth')` / `->middleware(['auth','role:admin'])`
	// chain. Group 1 = the raw middleware argument (string or array body).
	lrAuthMwArgRe = regexp.MustCompile(`->\s*middleware\s*\(\s*(\[[^\]]*\]|'[^']*'|"[^"]*")`)
	// `->withoutMiddleware('auth')` → per-route public override.
	lrWithoutAuthRe = regexp.MustCompile(`->\s*withoutMiddleware\s*\(\s*\[?\s*['"]auth(?::[\w.-]+)?['"]`)
	// Individual middleware tokens: 'auth', 'auth:sanctum', 'role:admin', 'can:update'.
	lrMwTokenRe = regexp.MustCompile(`['"]([\w:., -]+)['"]`)
)

// lrAuthSite is one route's reconciled auth posture keyed by canonical (verb, path).
type lrAuthSite struct {
	verb string
	path string
	auth lrAuthPosture
}

// lrAuthPosture is the resolved auth posture for a Laravel route.
type lrAuthPosture struct {
	required   bool
	publicSet  bool // explicit withoutMiddleware('auth')
	middleware string
	roles      string
	perms      string
	method     string // "middleware" | "without"
}

func (p lrAuthPosture) stamp(props map[string]string) {
	if p.publicSet {
		props["auth_required"] = "false"
		props["auth_method"] = "without"
		return
	}
	if !p.required {
		return
	}
	props["auth_required"] = "true"
	props["auth_method"] = "middleware"
	if p.middleware != "" {
		props["auth_middleware"] = p.middleware
	}
	if p.roles != "" {
		props["auth_roles"] = p.roles
	}
	if p.perms != "" {
		props["auth_permissions"] = p.perms
	}
}

// resolveLaravelMiddlewareTokens classifies a list of middleware tokens into an
// auth posture: an `auth`/`auth:guard` token → authenticated; `role:admin` →
// role; `can:update` / `permission:edit` → permission/ability.
func resolveLaravelMiddlewareTokens(tokens []string) lrAuthPosture {
	var p lrAuthPosture
	var mw []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		low := strings.ToLower(t)
		switch {
		case low == "auth" || strings.HasPrefix(low, "auth:"):
			p.required = true
			mw = append(mw, t)
		case strings.HasPrefix(low, "role:") || strings.HasPrefix(low, "hasrole:"):
			p.required = true
			p.roles = firstLaravelArg(t)
			mw = append(mw, t)
		case strings.HasPrefix(low, "can:") || strings.HasPrefix(low, "permission:"):
			p.required = true
			p.perms = firstLaravelArg(t)
			mw = append(mw, t)
		}
	}
	p.middleware = strings.Join(mw, ",")
	return p
}

// firstLaravelArg returns the first colon-arg of a middleware token, dropping a
// trailing CSV tail: `role:admin,editor` → "admin", `can:update,post` → "update".
func firstLaravelArg(token string) string {
	if c := strings.IndexByte(token, ':'); c >= 0 {
		arg := token[c+1:]
		if comma := strings.IndexByte(arg, ','); comma >= 0 {
			arg = arg[:comma]
		}
		return strings.TrimSpace(arg)
	}
	return ""
}

// lrMwTokens extracts the quoted middleware tokens from a `->middleware(...)`
// argument region (handles both the single-string and array forms).
func lrMwTokens(arg string) []string {
	var out []string
	for _, m := range lrMwTokenRe.FindAllStringSubmatch(arg, -1) {
		if t := strings.TrimSpace(m[1]); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// indexLaravelRouteAuth pairs each `Route::<verb>(` registration with its own
// `->middleware(...)` / `->withoutMiddleware('auth')` chain plus the middleware of
// every enclosing group, keyed by the SAME canonical (verb, path) the producer
// stamps. Per-route middleware reconciles WITH (adds to) the group middleware;
// a `->withoutMiddleware('auth')` override opens the route.
func indexLaravelRouteAuth(content string, groupSpans []lrGroupSpan, groupMw map[int][]string) []lrAuthSite {
	verbMatches := lrRouteVerbRe.FindAllStringSubmatchIndex(content, -1)
	if len(verbMatches) == 0 {
		return nil
	}
	var out []lrAuthSite
	for i, m := range verbMatches {
		spanEnd := len(content)
		if i+1 < len(verbMatches) {
			spanEnd = verbMatches[i+1][0]
		}
		if semi := strings.IndexByte(content[m[0]:spanEnd], ';'); semi >= 0 {
			spanEnd = m[0] + semi
		}
		span := content[m[0]:spanEnd]

		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := ""
		if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			raw = content[m[6]:m[7]]
		}
		if raw == "" {
			continue
		}
		prefix := ""
		var tokens []string
		for gi, gs := range groupSpans {
			if m[0] >= gs.bodyStart && m[0] < gs.bodyEnd {
				prefix = prefix + "/" + strings.Trim(gs.prefix, "/")
				tokens = append(tokens, groupMw[gi]...)
			}
		}
		if prefix != "" {
			raw = prefix + "/" + strings.TrimLeft(raw, "/")
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)

		// Per-route ->middleware(...) tokens reconcile with the group middleware.
		for _, am := range lrAuthMwArgRe.FindAllStringSubmatch(span, -1) {
			tokens = append(tokens, lrMwTokens(am[1])...)
		}
		posture := resolveLaravelMiddlewareTokens(tokens)
		// A per-route withoutMiddleware('auth') opens the route (highest precedence).
		if lrWithoutAuthRe.MatchString(span) {
			posture = lrAuthPosture{publicSet: true, method: "without"}
		}
		if !posture.required && !posture.publicSet {
			continue
		}
		out = append(out, lrAuthSite{verb: verb, path: canonical, auth: posture})
	}
	return out
}

// indexLaravelGroupMiddleware returns the middleware token list of each group
// span (keyed by its index in groupSpans), so a route can union its enclosing
// groups' middleware. Group spans whose options carry no middleware key map to
// an empty list.
func indexLaravelGroupMiddleware(content string, groupSpans []lrGroupSpan) map[int][]string {
	out := make(map[int][]string, len(groupSpans))
	for gi, gs := range groupSpans {
		// Scan a bounded window before the group body for its middleware key.
		winStart := gs.bodyStart - 600
		if winStart < 0 {
			winStart = 0
		}
		win := content[winStart:gs.bodyStart]
		if m := lrRouteGroupMwRe.FindStringSubmatch(win); m != nil {
			// The whole options array may carry a list; re-scan the window for all
			// quoted tokens after the `middleware` key.
			mwIdx := strings.LastIndex(win, "middleware")
			if mwIdx >= 0 {
				out[gi] = lrMwTokens(win[mwIdx:])
			} else {
				out[gi] = lrMwTokens(strings.Join(m[1:], " "))
			}
		}
	}
	return out
}

// indexLaravelGroupAuthRoutes scans Route::group(['middleware'=>[...]]) calls
// (prefix-LESS auth groups that lrExtractGroupSpans does not return), enumerates
// the Route::<verb>( registrations nested in each group body, and returns them as
// path-keyed auth sites. Mirrors indexLaravelGroupThrottles so a
// `Route::group(['middleware'=>['auth']], fn)` with no prefix still protects its
// inner routes.
func indexLaravelGroupAuthRoutes(content string, groupSpans []lrGroupSpan) []lrAuthSite {
	var out []lrAuthSite
	for _, m := range lrRouteGroupMwRe.FindAllStringSubmatchIndex(content, -1) {
		winEnd := m[1] + 400
		if winEnd > len(content) {
			winEnd = len(content)
		}
		tokens := lrMwTokens(content[m[0]:winEnd])
		posture := resolveLaravelMiddlewareTokens(tokens)
		if !posture.required {
			continue
		}
		bodyOpen := -1
		for i := m[1]; i < len(content) && i < m[1]+1000; i++ {
			if content[i] == '{' {
				bodyOpen = i
				break
			}
		}
		if bodyOpen < 0 {
			continue
		}
		bodyEnd := lrFindMatchingBrace(content, bodyOpen)
		if bodyEnd < 0 {
			continue
		}
		for _, vm := range lrRouteVerbRe.FindAllStringSubmatchIndex(content, -1) {
			if len(vm) < 8 || vm[0] < bodyOpen+1 || vm[0] >= bodyEnd {
				continue
			}
			verb := strings.ToUpper(content[vm[2]:vm[3]])
			raw := ""
			if vm[4] >= 0 {
				raw = content[vm[4]:vm[5]]
			} else if vm[6] >= 0 {
				raw = content[vm[6]:vm[7]]
			}
			if raw == "" {
				continue
			}
			prefix := ""
			for _, gs := range groupSpans {
				if vm[0] >= gs.bodyStart && vm[0] < gs.bodyEnd {
					prefix = prefix + "/" + strings.Trim(gs.prefix, "/")
				}
			}
			if prefix != "" {
				raw = prefix + "/" + strings.TrimLeft(raw, "/")
			}
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
			out = append(out, lrAuthSite{verb: verb, path: canonical, auth: posture})
		}
	}
	return out
}

// applyLaravelAuth resolves and stamps the reconciled auth posture on every
// Laravel synthetic backend endpoint synthesizeLaravel emitted (#4752). It mutates
// Properties in place and never adds or removes entities. `before` is the
// entity-slice length captured before the PHP synthesizers ran.
func applyLaravelAuth(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}
	if !strings.Contains(content, "middleware") {
		return
	}
	groupSpans := lrExtractGroupSpans(content)
	groupMw := indexLaravelGroupMiddleware(content, groupSpans)
	groupSites := indexLaravelGroupAuthRoutes(content, groupSpans)
	routeSites := indexLaravelRouteAuth(content, groupSpans, groupMw)
	if len(groupSites) == 0 && len(routeSites) == 0 {
		return
	}
	// Group sites first, then overlay per-route sites (per-route middleware /
	// withoutMiddleware override takes precedence for the same key).
	byKey := make(map[string]lrAuthPosture, len(groupSites)+len(routeSites))
	for _, s := range groupSites {
		byKey[s.verb+" "+s.path] = s.auth
	}
	for _, s := range routeSites {
		byKey[s.verb+" "+s.path] = s.auth
	}
	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path || e.Properties == nil {
			continue
		}
		if e.Properties["framework"] != "laravel" {
			continue
		}
		key := e.Properties["verb"] + " " + e.Properties["path"]
		if a, ok := byKey[key]; ok {
			a.stamp(e.Properties)
		}
	}
}
