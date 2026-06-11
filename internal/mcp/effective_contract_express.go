package mcp

// effective_contract_express.go — the Express / Fastify (and Koa / Hapi / Hono)
// effective-contract resolver (#4710).
//
// Express is the LOOSEST stack — handlers are plain functions, requests are
// untyped `req.body` / `req.query` / `req.params` accesses, and structured RBAC
// is rare. This resolver is best-effort and documents what is recoverable vs
// not. It composes from signals that ALREADY exist on the graph:
//
//   - REQUEST SHAPE: the handler's VALIDATES edge to a schema-library DTO
//     (`dto:<schemaVar>` — zod z.object / joi / yup / celebrate, the #3073/#4635
//     extraction) and its FIELD members (SCOPE.Schema/field). For Fastify the
//     route `schema.body` JSON-schema fields land as the same field members via
//     `request_body_type`. Both paths are tried.
//
//   - RESPONSE SHAPE + PER-BRANCH STATUS: the effects-branches facet — the JSTS
//     branch analyzer (substrate, analyzeBranchesJSTS) over the handler body,
//     which decodes res.status(NNN).json(...) / reply.code(NNN) / res.sendStatus
//     numeric statuses. The NestJS named-exception map does NOT apply — Express
//     uses numeric statuses directly.
//
//   - AUTH POSTURE: the shared authposture registry's express resolver (#4710
//     companion) — middleware-chain based (passport, requireAuth/ensureAuth
//     middleware). LOWER priority: many Express apps carry no structured RBAC, so
//     this frequently resolves to authenticated-only or unknown (honest-partial).
//
// RECOVERABLE vs GAP (honest-partial):
//   - Recoverable: validation-schema body fields when a zod/joi/Fastify schema is
//     LINKED to the handler; per-branch numeric res.status statuses; a coarse
//     middleware auth posture.
//   - Gap: untyped `req.body` with NO validation schema yields NO request fields
//     (nothing to recover — Express's looseness); req.query/req.params scalars are
//     only surfaced when stamped as path_params/query_params; per-permission RBAC
//     is usually absent.

import (
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// expressContractResolver composes effective contracts for Express/Fastify/Koa/
// Hapi/Hono handlers.
type expressContractResolver struct{}

func (expressContractResolver) Name() string { return "express" }

// Resolve gathers every Express/Fastify http_endpoint_definition whose group leaf
// (handler-function leaf or router/module stem) matches wantLeaf, composes each
// endpoint's contract, and groups them. ok=false when no Express/Fastify endpoint
// matches (so the registry tries the next resolver).
func (x expressContractResolver) Resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool) {
	type groupKey struct{ repo, group string }
	groups := map[groupKey]*effectiveContractGroup{}
	var order []groupKey

	for _, r := range reposToConsider(lg, nil) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isServerEndpointDefinition(e) {
				continue
			}
			if !isExpressEndpoint(e) {
				continue
			}
			handler := frameworkHandlerEntity(r, e)
			grp := fastapiGroupLeaf(e, handler) // same flat-function grouping
			if grp == "" || strings.ToLower(grp) != wantLeaf {
				continue
			}
			c := composeExpressContract(r, e, handler)
			key := groupKey{repo: r.Repo, group: grp}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{Class: grp, Framework: expressFrameworkLabel(e), Repo: r.Repo}
				groups[key] = g
				order = append(order, key)
			}
			g.Handlers = append(g.Handlers, c)
		}
	}
	if len(groups) == 0 {
		return nil, false
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].group < order[j].group
	})
	out := make([]effectiveContractGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		sortEffectiveContracts(g.Handlers)
		out = append(out, *g)
	}
	return out, true
}

// expressFrameworks is the set of bare-name Node HTTP frameworks this resolver
// owns (so the group Framework label is honest about which one it is).
var expressFrameworks = map[string]bool{
	"express": true, "fastify": true, "koa": true, "hapi": true, "hono": true,
}

// isExpressEndpoint reports whether an endpoint definition belongs to an Express-
// family Node framework. NestJS is explicitly EXCLUDED — it has its own flagship
// resolver and runs first; only NON-Nest TS/JS endpoints fall here.
func isExpressEndpoint(e *graph.Entity) bool {
	fw := endpointFramework(e)
	for name := range expressFrameworks {
		if strings.Contains(fw, name) {
			return true
		}
	}
	// A bare TS/JS endpoint with NO Nest signature is an Express-family handler.
	if fw == "typescript" || fw == "javascript" || fw == "ts" || fw == "js" {
		if isNestJSEndpoint(e) {
			return false
		}
		return true
	}
	return false
}

// expressFrameworkLabel returns the specific Node framework slug for the group
// label, defaulting to "express" when only the language is known.
func expressFrameworkLabel(e *graph.Entity) string {
	fw := endpointFramework(e)
	for name := range expressFrameworks {
		if strings.Contains(fw, name) {
			return name
		}
	}
	return "express"
}

// composeExpressContract builds the effectiveContract for one Express/Fastify
// handler.
func composeExpressContract(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) effectiveContract {
	c := effectiveContract{
		Framework: expressFrameworkLabel(ep),
		Verb:      strings.ToUpper(ep.Properties["verb"]),
		Path:      ep.Properties["path"],
		Kind:      "explicit",
	}
	if handler != nil {
		hq := handler.QualifiedName
		if hq == "" {
			hq = handler.Name
		}
		c.Handler = leafAfterDot(hq)
		c.SourceClass = fastapiGroupLeaf(ep, handler)
	}

	c.RequestFields = composeExpressRequestFields(r, ep, handler)
	c.ResponseBranches = composeFrameworkResponseBranches(r, handler)
	applyFrameworkAuthPosture(r, "express", ep, handler, &c)
	applyFrameworkStatusSplit(&c)
	return c
}

// composeExpressRequestFields resolves the request shape: the handler's VALIDATES
// edge to a validation-schema DTO (zod/joi/celebrate, the #3073/#4635 extraction)
// AND the endpoint's `request_body_type` (Fastify schema.body). When neither is
// linked the request is untyped `req.body` and NO fields are recoverable — the
// honest Express gap.
func composeExpressRequestFields(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) []contractField {
	var fields []contractField
	seen := map[string]bool{}
	add := func(f contractField) {
		k := f.In + "\x00" + f.Name
		if f.Name == "" || seen[k] {
			return
		}
		seen[k] = true
		fields = append(fields, f)
	}

	// (1) Handler VALIDATES → dto:<schemaVar> (zod/joi/celebrate schema fields).
	if handler != nil {
		adj := r.getAdjacency()
		for _, ed := range adj.Outgoing(handler.ID) {
			if !strings.EqualFold(ed.kind, "VALIDATES") {
				continue
			}
			rel := relPropsFor(r, ed.relIdx)
			dtoType := rel["dto"]
			if dtoType == "" {
				dtoType = strings.TrimPrefix(ed.target, "dto:")
			}
			in := decoratorToIn(rel["method"]) // body|query|param from req.* target
			for _, mf := range dtoFieldsByProperty(r, dtoType) {
				mf.In = in
				mf.DTO = dtoType
				add(mf)
			}
		}
	}

	// (2) Fastify route schema.body → request_body_type DTO fields.
	if dtoType := ep.Properties["request_body_type"]; dtoType != "" {
		for _, mf := range dtoFieldsByProperty(r, dtoType) {
			mf.In = "body"
			mf.DTO = dtoType
			add(mf)
		}
	}

	// (3) Scalar path/query params, where stamped.
	for _, p := range scalarParamsFromProps(ep) {
		add(p)
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].In != fields[j].In {
			return fields[i].In < fields[j].In
		}
		return fields[i].Name < fields[j].Name
	})
	return fields
}
