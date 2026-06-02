// route_middleware.go — ordered middleware-chain binding for Go gin/echo route
// ops (child of #3628; sibling of #3734's route_auth.go).
//
// #3734 bound only the AUTH middleware to a route op (stamps auth_*). The
// shared helpers (helpers.go) ALSO parse the full `.Use(...)` chain in
// registration order, but `emitMiddlewareChain` writes each middleware as a
// STANDALONE `SCOPE.Pattern` entity (`mw_order` 0,1,2,...) that is never bound
// to a specific endpoint. So for Go the graph could not answer "what
// middleware runs before THIS route, in order?" — the JS/TS pass
// (http_endpoint_jsts_middleware.go, #2853) already stamps a `middleware_chain`
// property on every endpoint; this resolver brings gin/echo to parity.
//
// It resolves three middleware scopes and binds the ORDERED, deduped chain to
// the route op via the same property contract the JS/TS pass uses:
//
//	middleware_chain  — JSON array of {name, expr, scope, order, auth_kind?},
//	                    OUTERMOST-first (engine → group → route), so index 0 is
//	                    the first middleware a request traverses.
//	middleware_count  — decimal count of resolved middleware (>0 ⇒ chain bound).
//	middleware_names  — comma-joined middleware symbols in chain order.
//	middleware_scope  — "route" | "group" | "engine" | combinations joined by
//	                    "+" (e.g. "engine+route") describing which scopes
//	                    contributed.
//
// Scope model (request traversal order, outermost first):
//
//	engine — `r.Use(mw)` on the engine var: wraps every route. Runs first.
//	group  — `g := r.Group("/x", mw)` construction args + `g.Use(mw)` on the
//	         group var: wraps every route registered on that group.
//	route  — inline middleware in the route registration, e.g. gin
//	         `r.GET("/me", mw, h)` / echo `e.GET("/me", h, mw)`. Innermost,
//	         runs last before the handler.
//
// Honest-partial: a middleware built dynamically (a slice assembled at runtime,
// a conditional `.Use`) is NOT resolvable statically and is skipped — no
// fabricated order. String-literal mount prefixes (`app.Use("/api", mw)`) are
// dropped by parseMiddlewareChain, so they never inflate the order index.
package golang

import (
	"encoding/json"
	"strconv"
	"strings"
)

// goMiddlewareEntry is one resolved middleware bound to an endpoint, carrying
// its name, raw expression, originating scope, and 0-based position within the
// final outermost-first chain.
type goMiddlewareEntry struct {
	Name     string `json:"name"`
	Expr     string `json:"expr"`
	Scope    string `json:"scope"` // "engine" | "group" | "route"
	Order    int    `json:"order"` // position in the bound chain (0 = first traversed)
	AuthKind string `json:"auth_kind,omitempty"`
}

const (
	goMWScopeRoute  = "route"
	goMWScopeGroup  = "group"
	goMWScopeEngine = "engine"
)

// goRouteMiddlewareIndex holds the per-file resolved middleware scopes: the
// engine-wide chain, each group var's chain, and each route's own inline chain.
type goRouteMiddlewareIndex struct {
	// engine is the chain registered on an engine var via `.Use(...)`
	// (file-scope, applies to every route). Outermost.
	engine []goMiddlewareEntry
	// groupVars maps a group var → the ordered chain bound to it (its
	// construction-arg middleware followed by any `g.Use(...)` middleware).
	groupVars map[string][]goMiddlewareEntry
	// inline maps "<VERB> <ownPath>" → the route's inline middleware chain.
	inline map[string][]goMiddlewareEntry
	// groupVarSet tracks which receivers are group vars, so a `.Use` on a group
	// var is classified group-scope rather than engine-scope.
	groupVarSet map[string]bool
}

// buildGoRouteMiddlewareIndex scans a Go source file once and resolves every
// route/group/engine middleware chain. Framework-shared: gin and echo use the
// same `.Group` / `.VERB` / `.Use` shapes (echo trails middleware after the
// handler, which splitTopLevelArgs/parseMiddlewareChain handle position-
// independently — every non-string arg that is not the handler is middleware,
// but to avoid swallowing the handler we keep inline middleware to the args the
// auth resolver already treats as a chain: see goMiddlewareFromRouteArgs).
func buildGoRouteMiddlewareIndex(src string) goRouteMiddlewareIndex {
	idx := goRouteMiddlewareIndex{
		groupVars:   map[string][]goMiddlewareEntry{},
		inline:      map[string][]goMiddlewareEntry{},
		groupVarSet: map[string]bool{},
	}

	// Group construction: `g := r.Group("/x", mw1, mw2)` → g inherits mw1,mw2.
	for _, m := range reGoGroupDecl.FindAllStringSubmatchIndex(src, -1) {
		groupVar := src[m[2]:m[3]]
		idx.groupVarSet[groupVar] = true
		args := src[m[6]:m[7]]
		for _, e := range goMiddlewareFromArgChain(args, goMWScopeGroup) {
			idx.groupVars[groupVar] = append(idx.groupVars[groupVar], e)
		}
	}

	// Inline route middleware: `r.GET("/me", mw, h)` / `e.GET("/me", h, mw)`.
	for _, m := range reGoRouteAuthScan.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[m[4]:m[5]])
		path := src[m[6]:m[7]]
		args := src[m[8]:m[9]]
		mws := goMiddlewareFromRouteArgs(args)
		if len(mws) > 0 {
			idx.inline[verb+" "+path] = mws
		}
	}

	// `.Use(...)` calls: engine-scope when on an engine var (or an unknown var
	// that is not a group), group-scope when on a known group var.
	for _, uc := range findUseCalls(src) {
		entries := goMiddlewareFromUseArgs(uc.Args)
		if len(entries) == 0 {
			continue
		}
		if idx.groupVarSet[uc.Recv] {
			for _, e := range entries {
				e.Scope = goMWScopeGroup
				idx.groupVars[uc.Recv] = append(idx.groupVars[uc.Recv], e)
			}
			continue
		}
		// Engine var, or an unrecognised receiver that is not a group: treat as
		// engine-wide (file-scope) middleware.
		for _, e := range entries {
			e.Scope = goMWScopeEngine
			idx.engine = append(idx.engine, e)
		}
	}

	return idx
}

// resolve returns the ordered, deduped middleware chain bound to one route op,
// OUTERMOST-first: engine-wide → the route's group var → the route's own inline
// chain. Each entry's Order is its final 0-based index in the returned chain.
func (idx goRouteMiddlewareIndex) resolve(routerVar, verb, ownPath string) []goMiddlewareEntry {
	var chain []goMiddlewareEntry
	chain = append(chain, idx.engine...)
	if g, ok := idx.groupVars[routerVar]; ok {
		chain = append(chain, g...)
	}
	if r, ok := idx.inline[verb+" "+ownPath]; ok {
		chain = append(chain, r...)
	}
	chain = dedupeGoMiddleware(chain)
	for i := range chain {
		chain[i].Order = i
	}
	return chain
}

// dedupeGoMiddleware removes duplicate (scope, expr) entries preserving order.
// A middleware registered both engine-wide and inline keeps its first (outer)
// occurrence — it runs once at the outer position.
func dedupeGoMiddleware(in []goMiddlewareEntry) []goMiddlewareEntry {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := in[:0:0]
	for _, e := range in {
		k := e.Expr
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// goMiddlewareFromArgChain parses a trailing-argument region (the text after a
// path literal in a `.Group(...)` construction, e.g. `, Logger(), AuthMw()`)
// into middleware entries with the given scope. The leading comma is tolerated.
func goMiddlewareFromArgChain(args, scope string) []goMiddlewareEntry {
	args = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), ","))
	if args == "" {
		return nil
	}
	var out []goMiddlewareEntry
	for _, a := range parseMiddlewareChain(args) {
		out = append(out, goMiddlewareEntry{
			Name:     a.Name,
			Expr:     a.Expr,
			Scope:    scope,
			AuthKind: a.AuthKind,
		})
	}
	return out
}

// goMiddlewareFromUseArgs parses a `.Use(...)` argument list into engine/group
// middleware entries (scope is assigned by the caller based on the receiver).
func goMiddlewareFromUseArgs(args string) []goMiddlewareEntry {
	var out []goMiddlewareEntry
	for _, a := range parseMiddlewareChain(args) {
		out = append(out, goMiddlewareEntry{
			Name:     a.Name,
			Expr:     a.Expr,
			AuthKind: a.AuthKind,
		})
	}
	return out
}

// goMiddlewareFromRouteArgs parses an inline route registration's trailing args
// (everything after the path) into route-scope middleware, DROPPING the handler.
//
// Both gin (`r.GET(path, mw..., handler)`) and echo (`e.GET(path, handler,
// mw...)`) place exactly one handler among the args; every other non-string
// callable arg is middleware. We cannot positionally know which arg is the
// handler across both frameworks, so we use the same conservative rule the JS/TS
// pass uses: when there are ≥2 args, the chain is every arg EXCEPT the one that
// looks most like a bare handler. To stay honest and framework-agnostic we keep
// every recognised middleware-shaped arg and drop a single trailing/leading bare
// identifier as the handler:
//   - gin: handler is LAST  → drop the last arg.
//   - echo: handler is FIRST → drop the first arg.
//
// We detect the gin-vs-echo ordering by middleware shape: a call expression
// (`Mw()` / `pkg.Mw(...)`) is unambiguously middleware; a bare identifier may be
// either. We therefore drop exactly one bare-identifier arg — preferring the
// last (gin) and falling back to the first (echo) — and keep the rest. This is a
// heuristic; dynamically-built chains are out of scope (honest-partial).
func goMiddlewareFromRouteArgs(args string) []goMiddlewareEntry {
	args = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), ","))
	if args == "" {
		return nil
	}
	parts := splitTopLevelArgs(args)
	// Keep only non-string callable args.
	type cand struct {
		expr   string
		isCall bool // ends with a call: `Mw()` / `pkg.New(x)`
	}
	var cands []cand
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || isStringLiteral(p) {
			continue
		}
		head := reMiddlewareCallHead.FindString(p)
		if head == "" {
			continue
		}
		cands = append(cands, cand{expr: p, isCall: strings.Contains(p, "(")})
	}
	if len(cands) <= 1 {
		// Only the handler (or nothing) — no inline middleware chain.
		return nil
	}
	// Drop exactly one handler. Prefer dropping a trailing bare identifier (gin);
	// else a leading bare identifier (echo); else the last arg.
	dropIdx := -1
	if !cands[len(cands)-1].isCall {
		dropIdx = len(cands) - 1
	} else if !cands[0].isCall {
		dropIdx = 0
	} else {
		dropIdx = len(cands) - 1
	}
	var out []goMiddlewareEntry
	for i, c := range cands {
		if i == dropIdx {
			continue
		}
		head := reMiddlewareCallHead.FindString(c.expr)
		out = append(out, goMiddlewareEntry{
			Name:     head,
			Expr:     c.expr,
			Scope:    goMWScopeRoute,
			AuthKind: classifyAuthMiddleware(c.expr),
		})
	}
	return out
}

// stampGoMiddlewareChain writes the resolved ordered chain onto a route op's
// Properties map using the JS/TS-parity contract. No-op when the chain is empty
// so an unprotected/un-wrapped route is left unstamped.
func stampGoMiddlewareChain(props map[string]string, chain []goMiddlewareEntry) {
	if props == nil || len(chain) == 0 {
		return
	}
	if encoded := encodeGoMiddlewareChain(chain); encoded != "" {
		props["middleware_chain"] = encoded
	}
	props["middleware_count"] = strconv.Itoa(len(chain))
	props["middleware_names"] = goMiddlewareNames(chain)
	props["middleware_scope"] = goMiddlewareScope(chain)
}

// encodeGoMiddlewareChain JSON-encodes the chain for the middleware_chain prop.
func encodeGoMiddlewareChain(chain []goMiddlewareEntry) string {
	b, err := json.Marshal(chain)
	if err != nil {
		return ""
	}
	return string(b)
}

// goMiddlewareNames returns the comma-joined middleware symbols in chain order.
func goMiddlewareNames(chain []goMiddlewareEntry) string {
	names := make([]string, 0, len(chain))
	for _, e := range chain {
		names = append(names, e.Name)
	}
	return strings.Join(names, ",")
}

// goMiddlewareScope returns the "+"-joined set of contributing scopes in
// outermost-first order (engine, group, route).
func goMiddlewareScope(chain []goMiddlewareEntry) string {
	var has [3]bool // engine, group, route
	for _, e := range chain {
		switch e.Scope {
		case goMWScopeEngine:
			has[0] = true
		case goMWScopeGroup:
			has[1] = true
		case goMWScopeRoute:
			has[2] = true
		}
	}
	var parts []string
	if has[0] {
		parts = append(parts, goMWScopeEngine)
	}
	if has[1] {
		parts = append(parts, goMWScopeGroup)
	}
	if has[2] {
		parts = append(parts, goMWScopeRoute)
	}
	return strings.Join(parts, "+")
}
