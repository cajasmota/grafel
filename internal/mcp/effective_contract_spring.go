package mcp

// effective_contract_spring.go — the Spring effective-contract resolver (#4708).
//
// Composes the per-endpoint full contract for a Spring (@RestController /
// @Controller) handler from signals that ALREADY exist on the graph — it
// re-extracts nothing:
//
//   - REQUEST SHAPE: the endpoint's `request_body_type` prop (the @RequestBody
//     DTO, stamped by the Spring route extractor) plus that DTO's FIELD members
//     (the #4613 Java DTO field membership — SCOPE.Schema/field entities carrying
//     field_name/field_type/parent_class/optional). @PathVariable /
//     @RequestParam scalars surface from the endpoint's `path_params` /
//     `query_params` props.
//
//   - RESPONSE SHAPE + PER-BRANCH STATUS: the effects-branches facet — the Java
//     branch analyzer (substrate, analyzeBranchesJava) over the handler body,
//     which already decodes ResponseEntity.status(NNN) / response.setStatus(NNN)
//     and HttpStatus.NAME (httpStatusNameToCode), and unwraps ResponseEntity<T> /
//     Optional<T> for the response shape.
//
//   - AUTH POSTURE: the shared authposture registry's spring-security resolver
//     (#4708 companion) decoding @PreAuthorize / @Secured / @RolesAllowed from the
//     handler/class, normalised into the shared {Kind, Literal} vocabulary.
//
// The emitted effectiveContract is the SAME structure the DRF / NestJS resolvers
// return, so the cross-group parity tools consume Spring contracts identically.
//
// RECOVERABLE vs GAP (honest-partial):
//   - Recoverable: @RequestBody DTO body fields (+ types/optionality), path-param
//     names, per-branch ResponseEntity/HttpStatus statuses, @PreAuthorize posture.
//   - Gap: @RequestParam query scalars are only surfaced when the route extractor
//     stamped `query_params` (path_params is the reliable one); response BODY
//     shape per branch is only as rich as analyzeBranchesJava's shape descriptor.

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// springContractResolver composes effective contracts for Spring controllers.
type springContractResolver struct{}

func (springContractResolver) Name() string { return "spring" }

// Resolve gathers every Spring http_endpoint_definition whose owning controller
// leaf matches wantLeaf, composes each endpoint's request/response/auth contract,
// and groups them by (repo, controller). ok=false when no Spring endpoint is
// attributed to the target (so the registry tries the next resolver).
func (s springContractResolver) Resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool) {
	type groupKey struct{ repo, class string }
	groups := map[groupKey]*effectiveContractGroup{}
	var order []groupKey

	for _, r := range reposToConsider(lg, nil) {
		if r.Doc == nil {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isServerEndpointDefinition(e) {
				return true
			}
			if !isSpringEndpoint(e) {
				return true
			}
			handler := frameworkHandlerEntity(r, e)
			controller := frameworkControllerLeaf(e, handler)
			if controller == "" || strings.ToLower(controller) != wantLeaf {
				return true
			}
			c := composeSpringContract(r, e, handler)
			key := groupKey{repo: r.Repo, class: controller}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{Class: controller, Framework: "spring", Repo: r.Repo}
				groups[key] = g
				order = append(order, key)
			}
			g.Handlers = append(g.Handlers, c)
			return true
		})
	}
	if len(groups) == 0 {
		return nil, false
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].class < order[j].class
	})
	out := make([]effectiveContractGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		sortEffectiveContracts(g.Handlers)
		out = append(out, *g)
	}
	return out, true
}

// isSpringEndpoint reports whether an endpoint definition belongs to Spring.
// Recognised by an explicit spring* framework prop OR (for a Java endpoint with
// no framework hint) by a Spring-characteristic prop the route extractor stamps.
func isSpringEndpoint(e *graph.Entity) bool {
	fw := endpointFramework(e)
	if strings.Contains(fw, "spring") {
		return true
	}
	if fw == "java" || fw == "kotlin" {
		p := e.PropsSnapshot()
		if p["request_body_type"] != "" || p["path_params"] != "" ||
			p["parameters"] != "" || p["api_responses"] != "" ||
			strings.Contains(strings.ToLower(p["route_decorator"]), "mapping") {
			return true
		}
	}
	return false
}

// composeSpringContract builds the effectiveContract for one Spring endpoint.
func composeSpringContract(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) effectiveContract {
	c := effectiveContract{
		Framework: "spring",
		Verb:      strings.ToUpper(ep.PropGet("verb")),
		Path:      ep.PropGet("path"),
		Kind:      "explicit",
	}
	if handler != nil {
		hq := handler.QualifiedName
		if hq == "" {
			hq = handler.Name
		}
		c.Handler = leafAfterDot(prefixBeforeDot(hq)) + "." + leafAfterDot(hq)
		c.SourceClass = leafAfterDot(prefixBeforeDot(hq))
	}

	c.RequestFields = composeFrameworkRequestFields(r, ep, handler)
	c.ResponseBranches = composeFrameworkResponseBranches(r, handler)
	applyFrameworkAuthPosture(r, "spring-security", ep, handler, &c)
	applyFrameworkStatusSplit(&c)
	return c
}
