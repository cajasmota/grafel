package mcp

// effective_contract_fastapi.go — the FastAPI effective-contract resolver
// (#4709).
//
// Composes the per-endpoint full contract for a FastAPI path-operation function
// from signals that ALREADY exist on the graph — it re-extracts nothing:
//
//   - REQUEST SHAPE: the endpoint's `request_body_type` prop (the Pydantic body
//     model parameter, stamped by the FastAPI request/response extractor) plus
//     that model's FIELD members (the #4613 Python/Pydantic DTO field membership
//     — SCOPE.Schema/field entities carrying field_name/field_type/parent_class/
//     optional). Query()/Path()/Header() scalars surface from path_params /
//     query_params when the route extractor stamped them.
//
//   - RESPONSE SHAPE + PER-BRANCH STATUS: the effects-branches facet — the Python
//     branch analyzer (substrate, analyzeBranchesPython) over the path-op body,
//     which decodes raised HTTPException(status_code=NNN) branches and DRF-style
//     status.HTTP_NNN constants. The path-op decorator's status_code= success
//     default and response_model= are surfaced from the endpoint props.
//
//   - AUTH POSTURE: the shared authposture registry's fastapi resolver (#4709
//     companion) decoding Depends(get_current_user) / Security(..., scopes=[...])
//     security dependencies, normalised into the shared {Kind, Literal}
//     vocabulary.
//
// FastAPI path-ops are FLAT functions (no controller class), so endpoints group
// by the handler-function leaf OR the router/module stem the caller named — see
// fastapiGroupLeaf.
//
// RECOVERABLE vs GAP (honest-partial):
//   - Recoverable: Pydantic body fields (+ types/optionality), per-branch
//     HTTPException statuses, the decorator status_code success default and
//     response_model shape, Depends/Security auth posture.
//   - Gap: Query/Path/Header scalar TYPES are not on the flat props (names only,
//     and only when path_params/query_params were stamped); response BODY shape
//     per branch is only as rich as analyzeBranchesPython's descriptor.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// fastAPIContractResolver composes effective contracts for FastAPI path-ops.
type fastAPIContractResolver struct{}

func (fastAPIContractResolver) Name() string { return "fastapi" }

// Resolve gathers every FastAPI http_endpoint_definition whose group leaf (the
// handler-function leaf or the module/router stem) matches wantLeaf, composes
// each endpoint's contract, and groups them. ok=false when no FastAPI endpoint
// matches (so the registry tries the next resolver).
func (f fastAPIContractResolver) Resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool) {
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
			if !isFastAPIEndpoint(e) {
				continue
			}
			handler := frameworkHandlerEntity(r, e)
			grp := fastapiGroupLeaf(e, handler)
			if grp == "" || strings.ToLower(grp) != wantLeaf {
				continue
			}
			c := composeFastAPIContract(r, e, handler)
			key := groupKey{repo: r.Repo, group: grp}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{Class: grp, Framework: "fastapi", Repo: r.Repo}
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

// isFastAPIEndpoint reports whether an endpoint definition belongs to FastAPI.
func isFastAPIEndpoint(e *graph.Entity) bool {
	fw := endpointFramework(e)
	if strings.Contains(fw, "fastapi") {
		return true
	}
	if fw == "python" {
		p := e.PropsSnapshot()
		if p["response_model"] != "" || p["status_code"] != "" ||
			p["request_body_type"] != "" {
			return true
		}
	}
	return false
}

// fastapiGroupLeaf returns the grouping leaf for a FastAPI endpoint: the handler
// function's own leaf name (path-ops are flat functions), falling back to the
// module/router stem (the endpoint's controller/module prop or its source-file
// base name). This lets the caller resolve a contract by passing either the
// path-op function name or the router/module that owns it.
func fastapiGroupLeaf(ep *graph.Entity, handler *graph.Entity) string {
	if handler != nil {
		qn := handler.QualifiedName
		if qn == "" {
			qn = handler.Name
		}
		if leaf := leafAfterDot(qn); leaf != "" {
			return leaf
		}
	}
	if c := ep.PropGet("controller"); c != "" {
		return leafAfterDot(c)
	}
	if m := ep.PropGet("module"); m != "" {
		return leafAfterDot(m)
	}
	return moduleStemFromPath(ep.SourceFile)
}

// moduleStemFromPath returns the base file stem of a source path
// ("app/routers/users.py" → "users"), the conventional FastAPI router module
// name. Empty for an empty path.
func moduleStemFromPath(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		p = p[:i]
	}
	return p
}

// composeFastAPIContract builds the effectiveContract for one FastAPI path-op.
func composeFastAPIContract(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) effectiveContract {
	c := effectiveContract{
		Framework: "fastapi",
		Verb:      strings.ToUpper(ep.PropGet("verb")),
		Path:      ep.PropGet("path"),
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

	c.RequestFields = composeFrameworkRequestFields(r, ep, handler)
	c.ResponseBranches = composeFastAPIResponseBranches(r, ep, handler)
	if rm := ep.PropGet("response_model"); rm != "" {
		c.Serializer = rm
	}
	applyFrameworkAuthPosture(r, "fastapi", ep, handler, &c)
	applyFrameworkStatusSplit(&c)
	return c
}

// composeFastAPIResponseBranches augments the analyzer-derived branches with the
// path-op decorator's `status_code=` success default — the success status is
// declared on the decorator (200/201), NOT inside the body, so the branch
// analyzer never sees it. The decorator default is merged as the success branch
// (carrying the response_model shape when declared) unless the body already
// produced a same-status branch.
func composeFastAPIResponseBranches(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) []contractResponseBranch {
	branches := composeFrameworkResponseBranches(r, handler)

	if sc := strings.TrimSpace(ep.PropGet("status_code")); sc != "" {
		if code, err := strconv.Atoi(sc); err == nil && code > 0 {
			present := false
			for _, b := range branches {
				if b.Status == code {
					present = true
					break
				}
			}
			if !present {
				shape := ep.PropGet("response_model")
				branches = append(branches, contractResponseBranch{
					Status:  code,
					Shape:   shape,
					Outcome: "return_value",
				})
			}
		}
	}

	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Status != branches[j].Status {
			return branches[i].Status < branches[j].Status
		}
		return branches[i].Shape < branches[j].Shape
	})
	return branches
}
