// elixir_routes.go — deep Elixir HTTP routing synthesis (#3468).
//
// Phoenix verb/resources/scope synthesis lives in phoenix_routes.go
// (synthesizePhoenix, #2692). This file extends Elixir endpoint synthesis to
// the remaining first-class Elixir HTTP surfaces so they emit canonical
// `http:<VERB>:<path>` (and `http:GRAPHQL:/...`) synthetics with handler
// attribution, on par with the TS/JS bar:
//
//   - synthesizePhoenixLive  — Phoenix LiveView `live "/path", Module, :action`
//   - synthesizePlugRouter   — Plug.Router `get|post|... "/path" do ... end`
//   - synthesizeCowboy       — Cowboy dispatch `{"/path", Handler, []}`
//   - synthesizeAbsinthe      — Absinthe GraphQL `field :name` under query/mutation/subscription
//
// Each is gated on a cheap file-level signal so it is a no-op on unrelated
// Elixir files, and each asserts a specific (verb, canonical-path) — never a
// bare length check.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Phoenix LiveView — `live "/path", PageLive, :action`
// ---------------------------------------------------------------------------
//
// LiveView routes are declared in the Phoenix router with the `live` macro:
//
//	scope "/", MyAppWeb do
//	    live "/dashboard", DashboardLive, :index
//	    live "/users/:id", UserLive.Show, :show
//	end
//
// A `live` route is reachable over HTTP via an initial GET render (the
// subsequent state lives over the LiveView WebSocket channel). We synthesise
// the initial GET endpoint so the route shows up in the HTTP surface, stamping
// `route_type=live` and attributing the live-module `:action` as the handler.
//
// The `:action` atom is optional in the `live` macro; when absent we still
// emit the GET endpoint with the module basename as the handler hint.
var exPhoenixLiveRe = regexp.MustCompile(
	`(?m)^\s*live\s+"([^"\r\n]+)"\s*,\s*([A-Za-z_][\w.]*)` +
		`(?:\s*,\s*:([A-Za-z_]\w*))?`,
)

// synthesizePhoenixLive emits the initial-GET endpoint for each `live` route
// in a Phoenix router file, composing the active `scope` prefix (reusing the
// Phoenix scope-stack walker).
func synthesizePhoenixLive(content string, emit phoenixEmitFn) {
	if !strings.Contains(content, "live ") && !strings.Contains(content, "live\t") {
		return
	}
	for _, m := range exPhoenixLiveRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := content[m[2]:m[3]]
		modRef := content[m[4]:m[5]]
		action := ""
		if m[6] >= 0 {
			action = content[m[6]:m[7]]
		}
		prefix := phoenixScopePrefixAt(content, m[0])
		full := joinPathFragments(prefix, raw)
		canonical := httproutes.Canonicalize(httproutes.FrameworkPhoenix, full)
		if canonical == "" {
			continue
		}
		refName := action
		if refName == "" {
			refName = exModuleBasename(modRef)
		}
		emit("GET", canonical, "phoenix_live", "SCOPE.Operation", refName, phoenixControllerHint(modRef))
	}
}

// exModuleBasename returns the final dotted segment of an Elixir module
// reference, lower-cased nothing (kept as-is). `MyAppWeb.UserLive.Show` → `Show`.
func exModuleBasename(modRef string) string {
	if i := strings.LastIndexByte(modRef, '.'); i >= 0 {
		return modRef[i+1:]
	}
	return modRef
}

// ---------------------------------------------------------------------------
// Plug.Router — `get|post|put|patch|delete|options|head "/path" do ... end`
// ---------------------------------------------------------------------------
//
// Plug.Router is the bare-metal routing DSL underneath Phoenix:
//
//	defmodule MyApp.Router do
//	    use Plug.Router
//	    plug :match
//	    plug :dispatch
//
//	    get "/health", do: send_resp(conn, 200, "ok")
//	    get "/users/:id" do
//	        send_resp(conn, 200, ...)
//	    end
//	    post "/users", do: ...
//	    forward "/admin", to: AdminRouter
//	end
//
// Unlike Phoenix, the handler is the inline `do` block, not a controller
// action, so there is no cross-file handler_file hint — the synthetic is
// attributed to the router module + verb. Paths use `:name` colon params.
var (
	exPlugRouterUseRe = regexp.MustCompile(`(?m)use\s+Plug\.Router\b`)
	exPlugVerbRe      = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|options|head)\s+"([^"\r\n]+)"`,
	)
	exPlugForwardRe = regexp.MustCompile(
		`(?m)^\s*forward\s+"([^"\r\n]+)"`,
	)
	exModuleDeclRe = regexp.MustCompile(`(?m)^\s*defmodule\s+([\w.]+)`)
)

// synthesizePlugRouter emits one endpoint per verb route in a Plug.Router
// module. `forward "/prefix", to: Mod` is emitted as an ANY mount so the
// downstream surface records the delegation point.
func synthesizePlugRouter(content string, emit emitFn) {
	if !exPlugRouterUseRe.MatchString(content) {
		return
	}
	router := "PlugRouter"
	if mm := exModuleDeclRe.FindStringSubmatch(content); len(mm) > 1 {
		router = mm[1]
	}
	for _, m := range exPlugVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkPlug, raw)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "plug", "SCOPE.Component", router)
	}
	for _, m := range exPlugForwardRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		raw := content[m[2]:m[3]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkPlug, raw)
		if canonical == "" {
			continue
		}
		emit("ANY", canonical, "plug_forward", "SCOPE.Component", router)
	}
}

// ---------------------------------------------------------------------------
// Cowboy dispatch — `{"/path", Handler, InitialState}`
// ---------------------------------------------------------------------------
//
// Cowboy is the Erlang HTTP server. Routes are declared as a compiled dispatch
// table of host/path rules:
//
//	dispatch = :cowboy_router.compile([
//	    {:_, [
//	        {"/", MyApp.IndexHandler, []},
//	        {"/users/:id", MyApp.UserHandler, []},
//	        {"/ws", MyApp.SocketHandler, []}
//	    ]}
//	])
//
// Each `{"/path", Handler, _}` triple is a route. Cowboy paths use the
// Phoenix-style `:name` colon binding (and `[...]` optional segments, which we
// pass through). The HTTP verb is not encoded in the dispatch table (the
// handler's `init/2` dispatches on `:cowboy_req.method`), so we synthesise an
// ANY endpoint attributed to the handler module.
var exCowboyRouteRe = regexp.MustCompile(
	`(?m)\{\s*"([^"\r\n]+)"\s*,\s*([A-Za-z_][\w.]*)\s*,`,
)

// synthesizeCowboy emits an ANY endpoint per dispatch-table route, attributed
// to the handler module. Gated on a `:cowboy_router` / `cowboy_router` signal
// so plain Elixir tuples elsewhere are not misread as routes.
func synthesizeCowboy(content string, emit emitFn) {
	if !strings.Contains(content, "cowboy_router") && !strings.Contains(content, "cowboy_handler") {
		return
	}
	for _, m := range exCowboyRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := content[m[2]:m[3]]
		handler := content[m[4]:m[5]]
		// Skip the host wildcard `:_` rule and obviously non-path strings.
		if raw == "" || !strings.HasPrefix(raw, "/") {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkCowboy, raw)
		if canonical == "" {
			continue
		}
		emit("ANY", canonical, "cowboy", "SCOPE.Component", handler)
	}
}

// ---------------------------------------------------------------------------
// Absinthe GraphQL — `query/mutation/subscription do field :name ... end`
// ---------------------------------------------------------------------------
//
// Absinthe is the Elixir GraphQL toolkit. Schemas are defined with a macro DSL:
//
//	defmodule MyAppWeb.Schema do
//	    use Absinthe.Schema
//
//	    query do
//	        field :users, list_of(:user) do
//	            resolve &Resolvers.list_users/3
//	        end
//	        field :user, :user do
//	            arg :id, non_null(:id)
//	            resolve &Resolvers.get_user/3
//	        end
//	    end
//
//	    mutation do
//	        field :create_user, :user do
//	            resolve &Resolvers.create_user/3
//	        end
//	    end
//	end
//
// We map each top-level `field :name` under a `query`/`mutation`/`subscription`
// block to `http:GRAPHQL:/graphql/<Root>/<field>` (Query/Mutation/Subscription),
// mirroring the Strawberry/Apollo convention (#3066). The resolver reference
// (when present as `resolve &Mod.fun/3`) is recorded as the handler.
var (
	exAbsintheRootRe  = regexp.MustCompile(`^\s*(query|mutation|subscription)\s+do\b`)
	exAbsintheFieldRe = regexp.MustCompile(`^\s*field\s+:([A-Za-z_]\w*)`)
)

// exLineOpensBlock reports whether an Elixir source line opens a new `do`
// block (ends with `do` or `do:`-less trailing `do`). Inline `do:` keyword
// forms (`field :x, do: ...`) do NOT open a multi-line block.
func exLineOpensBlock(line string) bool {
	t := strings.TrimRight(line, " \t\r")
	if strings.HasSuffix(t, " do") || strings.HasSuffix(t, "\tdo") || t == "do" {
		return true
	}
	return false
}

// exLineClosesBlock reports whether a line is a bare `end` (closing a block).
func exLineClosesBlock(line string) bool {
	t := strings.TrimSpace(line)
	return t == "end" || strings.HasPrefix(t, "end ") || strings.HasPrefix(t, "end\t")
}

// absintheRootName maps the block keyword to the GraphQL root type name used
// in the synthetic path.
func absintheRootName(block string) string {
	switch block {
	case "query":
		return "Query"
	case "mutation":
		return "Mutation"
	case "subscription":
		return "Subscription"
	}
	if block == "" {
		return block
	}
	return strings.ToUpper(block[:1]) + block[1:]
}

// synthesizeAbsinthe emits a GraphQL field endpoint per `field :name` declared
// directly inside a `query`/`mutation`/`subscription` block. Nested field
// blocks (e.g. object types) are excluded by tracking block depth: only fields
// at depth 1 (immediately inside the root block) are emitted.
func synthesizeAbsinthe(content string, emit emitFn) {
	if !strings.Contains(content, "Absinthe") && !strings.Contains(content, "absinthe") {
		return
	}
	// Single line-oriented scan. We track the current root block (query /
	// mutation / subscription) and the `do`/`end` nesting depth *inside* it.
	// A `field :name` at depth 1 (directly in the root body) is an endpoint;
	// deeper fields belong to nested object/field blocks and are skipped.
	var curRoot string
	depth := 0 // do-block depth relative to (and including) the open root block

	for _, line := range strings.Split(content, "\n") {
		// Root-block opener?
		if rm := exAbsintheRootRe.FindStringSubmatch(line); rm != nil && curRoot == "" {
			curRoot = rm[1]
			depth = 1
			continue
		}
		if curRoot == "" {
			continue
		}

		// Field directly in the root body (depth 1) → endpoint. Check before
		// adjusting depth so a `field :x do` line is attributed at its own
		// depth, then opens a nested block.
		if depth == 1 {
			if fm := exAbsintheFieldRe.FindStringSubmatch(line); fm != nil {
				fieldName := fm[1]
				root := absintheRootName(curRoot)
				path := "/graphql/" + root + "/" + fieldName
				canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
				emit("GRAPHQL", canonical, "absinthe", "SCOPE.Operation", root+"."+fieldName)
			}
		}

		// Depth bookkeeping. A line can both close and (rarely) open; handle
		// close first, then open.
		if exLineClosesBlock(line) {
			depth--
			if depth == 0 {
				curRoot = ""
			}
			continue
		}
		if exLineOpensBlock(line) {
			depth++
		}
	}
}
