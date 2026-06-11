// phoenix.go — the Phoenix (Elixir) auth-posture resolver (#4544; replaces the
// phoenix stub).
//
// Phoenix expresses endpoint authorization through PLUG PIPELINES declared in the
// router and applied to route scopes via `pipe_through`:
//
//	pipeline :auth do
//	  plug :require_authenticated_user      # → authenticated
//	end
//	pipeline :admin do
//	  plug :require_admin                   # → role/superuser
//	end
//	scope "/dashboard", MyApp do
//	  pipe_through [:browser, :auth]        # routes here are authenticated
//	  get "/", DashboardController, :index
//	end
//	scope "/", MyApp do
//	  pipe_through :browser                 # no auth pipeline → public
//	  get "/", PageController, :index
//	end
//
// DECODE (#4544): resolve which PIPELINE(S) a route's scope pipes through, then
// the PLUGS in those pipelines:
//
//   - `plug :require_authenticated_user` / `plug MyApp.Auth.Pipeline` /
//     `plug Guardian.Plug.EnsureAuthenticated` / `plug :ensure_auth`     → authenticated.
//   - `plug :require_admin` / `plug :require_role, :editor` / Bodyguard  → role.
//   - `plug :require_superuser` / `:require_root`                        → superuser.
//   - a scope whose pipe_through names NO auth-bearing pipeline           → public.
//
// PRECEDENCE: tightest plug across the route's pipelines wins (role ▸ authenticated);
// a route in a scope with no auth pipeline is public.
//
// HONEST HEURISTIC LIMIT (#4544): like Go middleware, a Phoenix plug is "auth"
// only by its NAME — there is no standard annotation. We classify by the plug
// identifier. When the engine has NOT stamped the route's reconciled pipeline /
// plug chain (it does not today — see the prop-stamping follow-up filed
// alongside this resolver, like #4742-4745), we degrade to scanning the router
// source. A route we cannot tie to a recognisable auth plug is reported
// `unknown` rather than `public` UNLESS an explicit no-auth pipe_through /
// public scope marker is present — we under-claim rather than false-public.
//
// Output normalises into the shared {Kind, Literal} vocabulary so the diff core
// compares a Phoenix posture against the Django oracle or a NestJS posture directly.
package authposture

import (
	"regexp"
	"strings"
)

type phoenixResolver struct{}

func (phoenixResolver) Name() string { return "phoenix" }

var (
	// plug :require_role, :editor  /  plug RequireRole, role: "editor"  → role literal.
	phoenixRolePlugRe = regexp.MustCompile(`(?i)plug\s+[:\w.]*require_?role\w*\s*,?\s*:?["` + "`" + `]?(\w[\w.-]*)`)
	// plug :require_admin / :require_superuser / :require_root → role/superuser.
	phoenixAdminPlugRe = regexp.MustCompile(`(?i)plug\s+:?(require_?(admin|superuser|root|super_?admin)|ensure_?admin)`)
	// authenticated-bearing plugs: require_authenticated_user, ensure_auth,
	// Guardian.Plug.EnsureAuthenticated, Pow auth, :authenticate_user, etc.
	phoenixAuthPlugRe = regexp.MustCompile(`(?i)plug\s+[:\w.]*?(require_?authenticated|ensure_?auth(?:enticated)?|authenticate_?user|require_?login|guardian\.plug\.ensureauthenticated|pow\.plug|require_?user|verify_?session|fetch_?current_?user.*verify)`)
	// the route's resolved pipeline list (prop) e.g. "browser,auth".
	// pipe_through [:browser, :auth] / pipe_through :auth.
	phoenixPipeThroughRe = regexp.MustCompile(`pipe_through\s+\[?\s*([^\]\n]+)`)
)

// Resolve decodes a Phoenix auth signal. Recognises the framework when the
// entity carries a Phoenix/Elixir framework hint OR a recognisable plug /
// pipe_through construct in its props or source. Reconciled props win; router
// source scanning is the fallback.
func (ph phoenixResolver) Resolve(sig Signal) (Posture, bool) {
	fw := strings.ToLower(firstNonEmpty(sig.Framework, sig.prop("framework")))
	// Reconciled props the engine MAY stamp in future (prop-stamping follow-up):
	//   auth_pipelines — the route's resolved pipe_through pipeline names.
	//   auth_plugs     — the plugs across those pipelines.
	pipelines := firstNonEmpty(sig.prop("auth_pipelines"), sig.prop("pipe_through"), sig.prop("phoenix_pipelines"))
	plugs := firstNonEmpty(sig.prop("auth_plugs"), sig.prop("phoenix_plugs"), sig.prop("plugs"))
	roles := sig.prop("auth_roles")
	authReq := sig.prop("auth_required")
	src := sig.Source

	isPhoenix := strings.Contains(fw, "phoenix") || strings.Contains(fw, "elixir") ||
		strings.Contains(fw, "plug") || strings.Contains(fw, "guardian") ||
		pipelines != "" || plugs != "" || roles != "" || authReq != "" ||
		hasPhoenixAuthPlug(src) || phoenixPipeThroughRe.MatchString(src)
	if !isPhoenix {
		return Posture{}, false
	}

	// (1) EXPLICIT PUBLIC — auth_required=false, or a route piping through ONLY
	// non-auth pipelines (no auth plug reachable). Only an explicit/derivable
	// no-auth signal earns `public`.
	if authReq == "false" {
		return Posture{Kind: KindPublic, Detail: "auth_required=false (no auth pipeline in pipe_through)"}, true
	}

	// (2) Reconciled role prop.
	if roles != "" {
		if r := firstCSV(roles); isPhoenixSuperuserName(r) {
			return Posture{Kind: KindSuperuser, Detail: "auth_roles=" + roles}, true
		}
		return Posture{Kind: KindRole, Literal: firstCSV(roles), Detail: "auth_roles=" + roles}, true
	}

	// (3) Reconciled plugs prop — classify the tightest plug.
	if plugs != "" {
		if p, ok := decodePhoenixPlugs(plugs, "auth_plugs="+plugs); ok {
			return p, true
		}
	}

	// (4) Source fallback — scan the router. Resolve the scope's pipe_through to
	// the named pipelines, then the plugs in those pipelines.
	if src != "" {
		if p, ok := decodePhoenixSource(src); ok {
			return p, true
		}
	}

	// (5) Reconciled auth_required=true without a decodable role.
	if authReq == "true" {
		return Posture{Kind: KindAuthenticated, Detail: "auth_required=true (auth pipeline, no decodable role)"}, true
	}

	// Recognised as Phoenix but no auth plug reachable from the route's scope.
	// HONEST under-claim: report unknown (never false-public on a name-heuristic).
	return Posture{Kind: KindUnknown, Detail: "Phoenix route with no recognisable auth plug in its pipe_through pipelines (name-heuristic limit)"}, true
}

// decodePhoenixPlugs classifies a plug list / plug source fragment in
// superuser/admin ▸ role ▸ authenticated priority.
func decodePhoenixPlugs(plugs, detail string) (Posture, bool) {
	if phoenixAdminPlugRe.MatchString(plugs) {
		// :require_admin etc. — admin is a superuser-class gate in Phoenix idiom,
		// but a named role plug (require_role, :x) is a plain role; check role first.
		if m := phoenixRolePlugRe.FindStringSubmatch(plugs); m != nil && !isPhoenixSuperuserName(m[1]) {
			return Posture{Kind: KindRole, Literal: strings.TrimSpace(m[1]), Detail: detail}, true
		}
		return Posture{Kind: KindSuperuser, Detail: detail}, true
	}
	if m := phoenixRolePlugRe.FindStringSubmatch(plugs); m != nil {
		role := strings.TrimSpace(m[1])
		if isPhoenixSuperuserName(role) {
			return Posture{Kind: KindSuperuser, Detail: detail}, true
		}
		return Posture{Kind: KindRole, Literal: role, Detail: detail}, true
	}
	if phoenixAuthPlugRe.MatchString(plugs) {
		return Posture{Kind: KindAuthenticated, Detail: detail}, true
	}
	return Posture{}, false
}

// decodePhoenixSource resolves a router source: find the route's scope
// pipe_through pipeline names, then collect the plugs declared in those
// pipelines, then classify. When the source is a bare plug/pipeline fragment
// (no full router), it falls back to classifying any auth plug present.
func decodePhoenixSource(src string) (Posture, bool) {
	// Determine which pipelines the route pipes through.
	pipeNames := phoenixPipeThroughPipelines(src)

	// If we resolved a pipe_through, restrict classification to the plug bodies of
	// the named pipelines (most-precise). Otherwise, scan the whole fragment.
	scan := src
	if len(pipeNames) > 0 {
		bodies := phoenixPipelineBodies(src, pipeNames)
		if bodies != "" {
			scan = bodies
		} else {
			// pipe_through names pipelines we can't find a body for in this slice:
			// classify on the pipe names themselves (e.g. an `:auth` / `:admin`
			// pipe name is a strong signal) and fall back to the whole source.
			if p, ok := decodePhoenixPipeNames(pipeNames); ok {
				return p, true
			}
		}
		// A pipe_through that names ONLY clearly-non-auth pipelines (browser/api)
		// with no auth plug reachable → public.
		if !hasPhoenixAuthPlug(scan) && onlyNonAuthPipes(pipeNames) {
			return Posture{Kind: KindPublic, Detail: "pipe_through " + strings.Join(pipeNames, ",") + " (no auth pipeline)"}, true
		}
	}

	if p, ok := decodePhoenixPlugs(scan, phoenixPlugDetail(scan)); ok {
		return p, true
	}
	return Posture{}, false
}

// phoenixPipeThroughPipelines extracts the pipeline names from the first
// pipe_through in the source (e.g. `pipe_through [:browser, :auth]` → browser,auth).
func phoenixPipeThroughPipelines(src string) []string {
	m := phoenixPipeThroughRe.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	var out []string
	for _, tok := range strings.Split(m[1], ",") {
		tok = strings.TrimSpace(strings.Trim(strings.TrimSpace(tok), "[]:\"`"))
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// phoenixPipelineBodies returns the concatenated `do ... end` bodies of the named
// pipelines found in src.
func phoenixPipelineBodies(src string, names []string) string {
	var b strings.Builder
	for _, n := range names {
		re := regexp.MustCompile(`(?is)pipeline\s+:` + regexp.QuoteMeta(n) + `\s+do(.*?)\bend\b`)
		if m := re.FindStringSubmatch(src); m != nil {
			b.WriteString(m[1])
			b.WriteString("\n")
		}
	}
	return b.String()
}

// decodePhoenixPipeNames classifies on the pipeline NAMES alone, as a fallback
// when the pipeline bodies aren't in the source slice. An `:auth`/`:admin` pipe
// name is a recognised convention.
func decodePhoenixPipeNames(names []string) (Posture, bool) {
	for _, n := range names {
		ln := strings.ToLower(n)
		if strings.Contains(ln, "admin") || strings.Contains(ln, "superuser") {
			return Posture{Kind: KindSuperuser, Detail: "pipe_through :" + n}, true
		}
	}
	for _, n := range names {
		ln := strings.ToLower(n)
		if ln == "auth" || strings.Contains(ln, "authenticated") || strings.Contains(ln, "protected") || strings.Contains(ln, "secure") {
			return Posture{Kind: KindAuthenticated, Detail: "pipe_through :" + n}, true
		}
	}
	return Posture{}, false
}

// onlyNonAuthPipes reports whether every named pipeline is a recognised
// non-auth pipeline (browser / api / static / public).
func onlyNonAuthPipes(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		switch strings.ToLower(n) {
		case "browser", "api", "static", "public", "open", "graphql":
		default:
			return false
		}
	}
	return true
}

// phoenixPlugDetail renders a short detail for a classified plug scan.
func phoenixPlugDetail(scan string) string {
	if m := regexp.MustCompile(`(?i)plug\s+[:\w.]+`).FindString(scan); m != "" {
		return strings.TrimSpace(m)
	}
	return "phoenix plug"
}

// isPhoenixSuperuserName reports whether a role literal denotes a superuser role.
func isPhoenixSuperuserName(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "superuser", "super_admin", "superadmin", "root", "admin", "super":
		return true
	}
	return false
}

// hasPhoenixAuthPlug reports whether src carries a recognisable Phoenix auth plug.
func hasPhoenixAuthPlug(src string) bool {
	if src == "" {
		return false
	}
	return phoenixAuthPlugRe.MatchString(src) ||
		phoenixRolePlugRe.MatchString(src) ||
		phoenixAdminPlugRe.MatchString(src)
}
