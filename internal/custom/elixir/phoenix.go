package elixir

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_phoenix", &phoenixExtractor{})
}

type phoenixExtractor struct{}

func (e *phoenixExtractor) Language() string { return "custom_elixir_phoenix" }

var (
	rePhoenixHTTPRoute = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|options|head)\s+"([^"]+)"`,
	)
	rePhoenixLiveRoute = regexp.MustCompile(
		`(?m)^\s*live\s+"([^"]+)"`,
	)
	rePhoenixResources = regexp.MustCompile(
		`(?m)^\s*resources\s+"([^"]+)"`,
	)
	rePhoenixScope = regexp.MustCompile(
		`(?m)^\s*scope\s+"([^"]+)"`,
	)
	rePhoenixPipeline = regexp.MustCompile(
		`(?m)^\s*pipeline\s+:([a-z_]+)\s+do`,
	)
	rePhoenixPlug = regexp.MustCompile(
		`(?m)^\s*plug\s+:?(\w+)`,
	)
	rePhoenixLiveView = regexp.MustCompile(
		`(?m)use\s+Phoenix\.LiveView\b`,
	)
	rePhoenixLiveComponent = regexp.MustCompile(
		`(?m)use\s+Phoenix\.LiveComponent\b`,
	)
	rePhoenixModuleDecl = regexp.MustCompile(
		`(?m)^defmodule\s+([\w.]+)`,
	)
	rePhoenixLiveViewHandler = regexp.MustCompile(
		`(?m)def\s+(mount|handle_event|handle_info|handle_params|render)\s*\(`,
	)
	rePhoenixControllerAction = regexp.MustCompile(
		`(?m)def\s+(index|show|new|create|edit|update|delete|action)\s*\(`,
	)
	// pipe_through [:browser, :auth] / pipe_through :api
	rePhoenixPipeThrough = regexp.MustCompile(
		`(?m)^\s*pipe_through\s+(\[[^\]]*\]|:[a-z_]+)`,
	)
	// A plug line inside a pipeline body: plug :name / plug Module / plug Module, opts
	rePhoenixPlugLine = regexp.MustCompile(
		`(?m)^\s*plug\s+(:?[\w.]+)`,
	)
	// A Guardian implementation module: `use Guardian, otp_app: :my_app`.
	// Distinct from `Guardian.Plug.*` router plugs — this is the token
	// issuer/verifier module that implements the Guardian behaviour callbacks.
	reGuardianUse = regexp.MustCompile(
		`(?m)^\s*use\s+Guardian\b`,
	)
	// Guardian behaviour callbacks implemented in a `use Guardian` module.
	reGuardianCallback = regexp.MustCompile(
		`(?m)^\s*def\s+(subject_for_token|resource_from_claims|build_claims|after_encode_and_sign|on_verify|on_revoke)\s*\(`,
	)
)

// elixirPipeline holds an ordered list of plug invocations parsed from a
// Phoenix `pipeline :name do ... end` block.
type elixirPipeline struct {
	name  string
	plugs []string
	// plugLines holds the full trimmed plug source line for each plug (parallel
	// to plugs), so role/option literals (`plug :require_role, :editor`) survive
	// for the #4751 auth-stamping role extraction.
	plugLines []string
	line      int
}

// authPlugMethod classifies a plug name/module into an auth method.
// Returns ("", "") when the plug is not auth-related.
func authPlugMethod(plug string) (provider, method string) {
	p := strings.TrimPrefix(plug, ":")
	lower := strings.ToLower(p)
	switch {
	case strings.HasPrefix(p, "Guardian.Plug.") || strings.HasPrefix(lower, "guardian"):
		// Guardian verifies JWTs (header) or session-stored tokens.
		if strings.Contains(lower, "verifysession") || strings.Contains(lower, "loadsession") {
			return "guardian", "session"
		}
		return "guardian", "jwt"
	case strings.HasPrefix(p, "Pow.Plug.") || strings.HasPrefix(lower, "pow"):
		return "pow", "session"
	case strings.Contains(lower, "ensureauthenticated") || strings.Contains(lower, "verifyheader") || strings.Contains(lower, "verifyissuer"):
		return "guardian", "jwt"
	case lower == "authenticate" || lower == "authenticated" || lower == "require_auth" ||
		lower == "require_authenticated_user" || lower == "ensure_authenticated" ||
		strings.Contains(lower, "auth"):
		// Generic custom auth plug; default to session unless name hints at token/jwt.
		switch {
		case strings.Contains(lower, "jwt") || strings.Contains(lower, "token") || strings.Contains(lower, "bearer"):
			return "custom", "token"
		default:
			return "custom", "session"
		}
	}
	return "", ""
}

// phoenixCRUDRoutes are the 8 REST routes for resources.
var phoenixCRUDRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/new"},
	{"GET", "/:id"},
	{"GET", "/:id/edit"},
	{"PATCH", "/:id"},
	{"PUT", "/:id"},
	{"DELETE", "/:id"},
}

func (e *phoenixExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.phoenix_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "phoenix"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "elixir" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// #4751 — resolve scope ▸ pipe_through ▸ pipeline ▸ plug so each route can be
	// stamped with its effective auth_pipelines + auth_plugs (+ role literal). The
	// router source is also stamped as router_source for the source-scan fallback.
	pipelinesByName := map[string]elixirPipeline{}
	for _, pl := range parsePhoenixPipelines(src) {
		pipelinesByName[pl.name] = pl
	}
	scopeSpans := parsePhoenixScopeSpans(src)
	stampRouteAuth := func(ent *types.EntityRecord, routeOff int) {
		res := resolvePhoenixRouteAuth(routeOff, scopeSpans, pipelinesByName)
		if len(res.pipelines) > 0 {
			ent.Properties["auth_pipelines"] = strings.Join(res.pipelines, ",")
		}
		if len(res.plugs) > 0 {
			ent.Properties["auth_plugs"] = strings.Join(res.plugs, ",")
		}
		if res.role != "" {
			ent.Properties["auth_roles"] = res.role
		}
		// router_source — the resolver's source-scan fallback reads this when the
		// structured pipeline/plug props above don't resolve (dynamic pipe_through).
		ent.Properties["router_source"] = phoenixRouterSlice(src, routeOff)
	}

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range rePhoenixHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_ROUTE",
			"http_method", method, "route_path", path)
		stampRouteAuth(&ent, m[0])
		add(ent)
	}

	// 2. live routes -> SCOPE.Operation/endpoint
	for _, m := range rePhoenixLiveRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("LIVE "+path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_ROUTE",
			"route_path", path, "route_type", "live")
		stampRouteAuth(&ent, m[0])
		add(ent)
	}

	// 3. resources -> CRUD expansion
	for _, m := range rePhoenixResources.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ln := lineOf(src, m[0])
		for _, cr := range phoenixCRUDRoutes {
			routePath := path + cr.suffix
			name := cr.method + " " + routePath
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_RESOURCES",
				"http_method", cr.method, "route_path", routePath)
			stampRouteAuth(&ent, m[0])
			add(ent)
		}
	}

	// 4. scope blocks -> SCOPE.Pattern
	for _, m := range rePhoenixScope.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("scope:"+path, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_SCOPE",
			"scope_path", path)
		add(ent)
	}

	// 5. pipeline declarations -> SCOPE.Pattern (with ordered plug list + auth)
	pipelines := parsePhoenixPipelines(src)
	for _, pl := range pipelines {
		ent := makeEntity("pipeline:"+pl.name, "SCOPE.Pattern", "pipeline", file.Path, file.Language, pl.line)
		props := []string{
			"framework", "phoenix",
			"provenance", "INFERRED_FROM_PHOENIX_PIPELINE",
			"pipeline_name", pl.name,
			"plug_chain", strings.Join(pl.plugs, " -> "),
			"plug_count", itoa(len(pl.plugs)),
		}
		// Classify auth: scan the ordered plug list for an auth plug.
		for _, pg := range pl.plugs {
			if prov, meth := authPlugMethod(pg); prov != "" {
				props = append(props,
					"auth", "true",
					"auth_plug", pg,
					"auth_provider", prov,
					"auth_method", meth)
				break
			}
		}
		setProps(&ent, props...)
		add(ent)
	}

	// 6. plug declarations -> SCOPE.Pattern/middleware (with order within pipeline)
	for _, pl := range pipelines {
		for idx, plugName := range pl.plugs {
			ent := makeEntity("plug:"+plugName, "SCOPE.Pattern", "middleware", file.Path, file.Language, pl.line)
			props := []string{
				"framework", "phoenix",
				"provenance", "INFERRED_FROM_PHOENIX_PLUG",
				"plug_name", plugName,
				"pipeline_name", pl.name,
				"plug_order", itoa(idx),
			}
			if prov, meth := authPlugMethod(plugName); prov != "" {
				props = append(props, "auth", "true", "auth_provider", prov, "auth_method", meth)
			}
			setProps(&ent, props...)
			add(ent)
		}
	}

	// 6b. top-level plug declarations (endpoint plugs, not inside a pipeline)
	//     captured as flat middleware for backward compatibility.
	for _, m := range rePhoenixPlug.FindAllStringSubmatchIndex(src, -1) {
		plugName := src[m[2]:m[3]]
		ent := makeEntity("plug:"+plugName, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		props := []string{
			"framework", "phoenix",
			"provenance", "INFERRED_FROM_PHOENIX_PLUG",
			"plug_name", plugName,
		}
		if prov, meth := authPlugMethod(plugName); prov != "" {
			props = append(props, "auth", "true", "auth_provider", prov, "auth_method", meth)
		}
		setProps(&ent, props...)
		add(ent)
	}

	// 6c. pipe_through bindings -> record which pipelines a scope applies.
	for _, m := range rePhoenixPipeThrough.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		names := parsePipeThroughList(raw)
		ent := makeEntity("pipe_through:"+strings.Join(names, ","), "SCOPE.Pattern", "pipe_through", file.Path, file.Language, lineOf(src, m[0]))
		props := []string{
			"framework", "phoenix",
			"provenance", "INFERRED_FROM_PHOENIX_PIPE_THROUGH",
			"pipelines", strings.Join(names, " -> "),
		}
		// Cross-reference: does any bound pipeline carry auth?
		for _, n := range names {
			for _, pl := range pipelines {
				if pl.name != n {
					continue
				}
				for _, pg := range pl.plugs {
					if prov, meth := authPlugMethod(pg); prov != "" {
						props = append(props, "auth", "true", "auth_provider", prov, "auth_method", meth)
						break
					}
				}
			}
		}
		setProps(&ent, props...)
		add(ent)
	}

	// 7. LiveView module -> SCOPE.UIComponent
	liveViewMatches := rePhoenixLiveView.FindAllStringIndex(src, -1)
	for _, m := range liveViewMatches {
		// Find preceding defmodule
		prefix := src[:m[0]]
		cm := rePhoenixModuleDecl.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			moduleName := cm[len(cm)-1][1]
			ent := makeEntity(moduleName, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_VIEW")
			add(ent)
		}
	}

	// 8. LiveComponent module -> SCOPE.UIComponent
	for _, m := range rePhoenixLiveComponent.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		cm := rePhoenixModuleDecl.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			moduleName := cm[len(cm)-1][1]
			ent := makeEntity(moduleName, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_COMPONENT")
			add(ent)
		}
	}

	// 9. LiveView handlers -> SCOPE.Operation/function
	for _, m := range rePhoenixLiveViewHandler.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ent := makeEntity(handler, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_VIEW_HANDLER",
			"handler_type", handler)
		add(ent)
	}

	// 10. Controller actions -> SCOPE.Operation/function
	for _, m := range rePhoenixControllerAction.FindAllStringSubmatchIndex(src, -1) {
		action := src[m[2]:m[3]]
		ent := makeEntity("action:"+action, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_CONTROLLER_ACTION",
			"action_name", action)
		add(ent)
	}

	// 11. Guardian implementation module -> SCOPE.Component/auth (#3511).
	//     `use Guardian` marks the token issuer/verifier module. We record the
	//     enclosing defmodule as an auth component with method=jwt and the list
	//     of implemented Guardian behaviour callbacks, so the graph carries the
	//     auth-provider definition (not just the router-plug usage).
	for _, m := range reGuardianUse.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		cm := rePhoenixModuleDecl.FindAllStringSubmatch(prefix, -1)
		moduleName := "GuardianImpl"
		if len(cm) > 0 {
			moduleName = cm[len(cm)-1][1]
		}
		var callbacks []string
		for _, cbm := range reGuardianCallback.FindAllStringSubmatch(src, -1) {
			callbacks = append(callbacks, cbm[1])
		}
		callbacks = uniqueStrings(callbacks)
		ent := makeEntity(moduleName, "SCOPE.Component", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "guardian",
			"provenance", "INFERRED_FROM_GUARDIAN_USE",
			"auth", "true",
			"auth_provider", "guardian",
			"auth_method", "jwt",
			"guardian_callbacks", strings.Join(callbacks, ","),
			"guardian_callback_count", itoa(len(callbacks)))
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// parsePhoenixPipelines walks `pipeline :name do ... end` blocks and returns
// each pipeline with its ordered list of plug invocations. Nesting is shallow
// in idiomatic router files, so we match `do`/`end` by line scanning from the
// pipeline header until the matching `end` at the header's indentation.
func parsePhoenixPipelines(src string) []elixirPipeline {
	var out []elixirPipeline
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		mm := rePhoenixPipeline.FindStringSubmatch(lines[i])
		if mm == nil {
			continue
		}
		name := mm[1]
		headerIndent := indentWidth(lines[i])
		pl := elixirPipeline{name: name, line: i + 1}
		for j := i + 1; j < len(lines); j++ {
			ln := lines[j]
			trimmed := strings.TrimSpace(ln)
			// Matching `end` at (or below) the header indentation closes the block.
			if trimmed == "end" && indentWidth(ln) <= headerIndent {
				break
			}
			if pm := rePhoenixPlugLine.FindStringSubmatch(ln); pm != nil {
				pl.plugs = append(pl.plugs, pm[1])
				pl.plugLines = append(pl.plugLines, strings.TrimSpace(ln))
			}
		}
		out = append(out, pl)
	}
	return out
}

// phoenixScopeSpan is one `scope "/path" do ... end` block with its byte span
// and the pipeline names its (first) pipe_through resolves to. Spans may nest;
// the innermost enclosing scope's pipe_through wins for a route.
type phoenixScopeSpan struct {
	start    int // byte offset of the scope header
	end      int // byte offset of the matching `end`
	pipeline []string
}

// rePhoenixScopeHeader matches a `scope ... do` header (with or without a path).
var rePhoenixScopeHeader = regexp.MustCompile(`(?m)^\s*scope\b.*\bdo\s*$`)

// parsePhoenixScopeSpans returns every scope block with its byte span and the
// pipeline names of the FIRST pipe_through declared directly in the scope body
// (#4751). Spans are line-scanned by indentation, matching parsePhoenixPipelines.
func parsePhoenixScopeSpans(src string) []phoenixScopeSpan {
	var out []phoenixScopeSpan
	lines := strings.Split(src, "\n")
	// Precompute the byte offset of each line start.
	offsets := make([]int, len(lines)+1)
	off := 0
	for i, ln := range lines {
		offsets[i] = off
		off += len(ln) + 1 // +1 for the split newline
	}
	offsets[len(lines)] = off
	for i := 0; i < len(lines); i++ {
		if !rePhoenixScopeHeader.MatchString(lines[i]) {
			continue
		}
		headerIndent := indentWidth(lines[i])
		span := phoenixScopeSpan{start: offsets[i], end: len(src)}
		for j := i + 1; j < len(lines); j++ {
			ln := lines[j]
			trimmed := strings.TrimSpace(ln)
			if trimmed == "end" && indentWidth(ln) <= headerIndent {
				span.end = offsets[j]
				break
			}
			if span.pipeline == nil {
				if pm := rePhoenixPipeThrough.FindStringSubmatch(ln); pm != nil {
					span.pipeline = parsePipeThroughList(pm[1])
				}
			}
		}
		out = append(out, span)
	}
	return out
}

// phoenixRouteAuth is the resolved auth context for one route: the pipeline names
// it pipes through, the auth plugs across those pipelines, and a role literal
// (from `plug :require_role, :editor`) when present.
type phoenixRouteAuth struct {
	pipelines []string
	plugs     []string
	role      string
}

// rePhoenixRolePlug matches a role-bearing plug `plug :require_role, :editor` /
// `plug RequireRole, role: "editor"`; group 1 = the role literal.
var rePhoenixRolePlug = regexp.MustCompile(`(?i)plug\s+[:\w.]*require_?role\w*\s*,?\s*(?:role:\s*)?:?["` + "`" + `]?(\w[\w.-]*)`)

// resolvePhoenixRouteAuth finds the innermost scope enclosing the route at
// routeOff, resolves its pipe_through pipeline names to the named pipelines, and
// collects the auth plugs (and any role literal) across those pipelines (#4751).
func resolvePhoenixRouteAuth(routeOff int, scopes []phoenixScopeSpan, pipelines map[string]elixirPipeline) phoenixRouteAuth {
	var res phoenixRouteAuth
	// Innermost (smallest, latest-starting) enclosing scope with a pipe_through.
	best := -1
	for i, s := range scopes {
		if routeOff < s.start || routeOff >= s.end || len(s.pipeline) == 0 {
			continue
		}
		if best < 0 || s.start > scopes[best].start {
			best = i
		}
	}
	if best < 0 {
		return res
	}
	res.pipelines = scopes[best].pipeline
	for _, name := range res.pipelines {
		pl, ok := pipelines[name]
		if !ok {
			continue
		}
		for idx, pg := range pl.plugs {
			if prov, _ := authPlugMethod(pg); prov != "" {
				res.plugs = append(res.plugs, pg)
			}
			// Role literal from the FULL plug line (`plug :require_role, :editor`),
			// since pg is only the plug name token.
			if res.role == "" && idx < len(pl.plugLines) {
				if m := rePhoenixRolePlug.FindStringSubmatch(pl.plugLines[idx]); m != nil {
					res.role = m[1]
				}
			}
		}
	}
	return res
}

// phoenixRouterSlice returns a bounded source slice around a route registration
// for the router_source source-scan fallback (#4751/#4752). It includes a window
// before the route (to carry the enclosing scope's pipe_through) and after.
func phoenixRouterSlice(src string, routeOff int) string {
	const before = 512
	const after = 256
	start := routeOff - before
	if start < 0 {
		start = 0
	}
	end := routeOff + after
	if end > len(src) {
		end = len(src)
	}
	return strings.TrimSpace(src[start:end])
}

// parsePipeThroughList normalises `pipe_through :api` or
// `pipe_through [:browser, :auth]` into a slice of bare pipeline names.
func parsePipeThroughList(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	var names []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		p = strings.TrimPrefix(p, ":")
		if p != "" {
			names = append(names, p)
		}
	}
	return names
}

func indentWidth(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
