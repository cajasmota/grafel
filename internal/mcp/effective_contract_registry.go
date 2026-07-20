package mcp

// effective_contract_registry.go — the framework-PLUGGABLE effective-contract
// resolver registry (#4601).
//
// grafel_effective_contract was DRF-only: computeEffectiveContract found a
// ViewSet's drf_router_expanded routes and projected the engine-stamped
// effective_* props (effective_contract.go / effective_contract_tool.go). For
// any non-DRF stack — notably the NestJS acme-v3 rewrite — it returned an
// empty result, so the rewrite agent could not get a per-endpoint full contract
// from the tool and fell back to scraping the dashboard.
//
// This file generalises the tool into a registry of per-framework resolvers.
// Each resolver composes the SAME contract structure (status set, request
// fields, per-branch response shapes, auth) from signals that ALREADY exist on
// the graph for its framework — nothing is re-extracted. The DRF path is
// preserved BYTE-FOR-BYTE: computeEffectiveContract still runs the DRF
// router-expanded + class-fallback synthesis first, and the registry resolvers
// only run when that produced nothing for the target (so an index that already
// resolves under DRF is unchanged).
//
// Adding Spring / FastAPI / Express / … is a matter of registering one more
// contractResolver (follow-up tickets ref #4601); the diff/parity consumers
// downstream never change because every resolver emits the shared
// effectiveContract shape.

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// contractResolver composes the per-endpoint effective contract for ONE
// framework from graph signals that already exist. Resolve scans lg for
// endpoints attributed to the target controller/group and returns one
// effectiveContractGroup per (repo, controller). ok=false ⇒ this resolver does
// not recognise the target as belonging to its framework (so the registry tries
// the next one).
type contractResolver interface {
	// Name is the framework slug ("nestjs", "spring", …).
	Name() string
	// Resolve composes the contract groups for the target within lg. wantLeaf is
	// the lower-cased controller/group leaf name the caller resolved the target
	// to. Returns (groups, true) when this framework owns the target, else
	// (nil, false).
	Resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool)
}

// contractResolverRegistry is the ordered set of non-DRF resolvers tried (in
// order) when the DRF path yields no groups. NewContractResolverRegistry wires
// the flagship NestJS resolver; every other framework is a follow-up ticket.
type contractResolverRegistry struct {
	resolvers []contractResolver
}

// newContractResolverRegistry builds the default registry. NestJS is the
// flagship non-DRF resolver (#4601/#4711); Spring (#4708), FastAPI (#4709) and
// Express/Fastify (#4710) slot in after it without touching any consumer.
//
// ORDER MATTERS for the TS/JS pair: nestJS MUST precede express, because the
// Express resolver's bare-TS/JS catch-all (a TS endpoint with no Nest signature
// is Express-family) would otherwise claim NestJS endpoints. isExpressEndpoint
// excludes Nest endpoints defensively, but registration order is the primary
// guard. Spring (Java) and FastAPI (Python) cannot collide with the JS pair.
func newContractResolverRegistry() *contractResolverRegistry {
	return &contractResolverRegistry{resolvers: []contractResolver{
		nestJSContractResolver{},  // flagship (#4601/#4711)
		springContractResolver{},  // #4708 — Java @RestController
		fastAPIContractResolver{}, // #4709 — Python path-ops
		expressContractResolver{}, // #4710 — Node bare-name frameworks
	}}
}

// resolve runs the registry over the target, returning the first resolver's
// groups that recognised it.
func (r *contractResolverRegistry) resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool) {
	for _, res := range r.resolvers {
		if groups, ok := res.Resolve(lg, target, wantLeaf); ok && len(groups) > 0 {
			return groups, true
		}
	}
	return nil, false
}

// isServerEndpointDefinition reports whether e is a server-side HTTP endpoint
// definition a framework resolver should consider: an http_endpoint definition
// kind (reusing the shared isHTTPEndpointDefinition predicate) that is neither a
// router-expanded DRF route (handled by the DRF projection path) nor a generated
// client stub.
func isServerEndpointDefinition(e *graph.Entity) bool {
	if e == nil || isRouterExpandedRoute(e) {
		return false
	}
	if e.PropGet("pattern_type") == patternTypeHTTPEndpointClientSynthesis {
		return false
	}
	return isHTTPEndpointDefinition(e)
}

// endpointFramework returns the lower-cased framework hint for an endpoint
// entity, from its `framework` property, falling back to the entity Language.
func endpointFramework(e *graph.Entity) string {
	if e == nil {
		return ""
	}
	if fw := strings.ToLower(strings.TrimSpace(e.PropGet("framework"))); fw != "" {
		return fw
	}
	return strings.ToLower(strings.TrimSpace(e.Language))
}
