package mcp

// dashboard_api.go — exported, request-envelope-free entry points that let the
// dashboard backend reuse the EXACT posture + effective-contract computation the
// archigraph_endpoint_posture and archigraph_effective_contract MCP tools run
// (#4254, epic #4249).
//
// Both surfaces (the MCP tool and the dashboard Paths detail pane) MUST agree
// byte-for-byte on what an endpoint's posture / a ViewSet's per-verb contract
// is. The only way to guarantee that is for both to call the same code. The MCP
// handlers operate on the internal *LoadedRepo / *LoadedGroup model, which the
// dashboard does not hold — but a LoadedRepo needs nothing more than a repo slug
// and a *graph.Document (every derived index builds lazily on first use). So
// these helpers wrap the dashboard's already-loaded graph.Documents into a
// throwaway LoadedGroup and delegate to buildPosturePayload /
// computeEffectiveContract.
//
// The returned types (PosturePayload, EffectiveContractResult, ...) are exported
// aliases of the internal wire structs, so the dashboard serialises the IDENTICAL
// JSON shape the MCP tools emit — no re-derivation, no drift.

import "github.com/cajasmota/archigraph/internal/graph"

// PosturePayload is the exported per-endpoint posture shape (error_flow / THROWS,
// rate_limit, deprecation, feature_gates, auth). Identical to what
// archigraph_endpoint_posture returns per entity.
type PosturePayload = posturePayload

// ErrorFlow is the exported THROWS/CATCHES facet.
type ErrorFlow = errorFlow

// EffectiveContractResult is the exported top-level effective-contract envelope.
type EffectiveContractResult = effectiveContractResult

// EffectiveContractGroup is the exported per-ViewSet group.
type EffectiveContractGroup = effectiveContractGroup

// EffectiveContract is the exported per-verb contract.
type EffectiveContract = effectiveContract

// loadedGroupFromDocs builds a throwaway LoadedGroup over the supplied in-memory
// graph.Documents (keyed by repo slug). Each LoadedRepo carries only Repo + Doc;
// every derived index (adjacency, byID, EXTENDS walk) is built lazily by the
// existing getters the moment the computation touches it. Documents that are nil
// are skipped.
func loadedGroupFromDocs(group string, docs map[string]*graph.Document) *LoadedGroup {
	lg := &LoadedGroup{Name: group, Repos: make(map[string]*LoadedRepo, len(docs))}
	for slug, doc := range docs {
		if doc == nil {
			continue
		}
		lg.Repos[slug] = &LoadedRepo{Repo: slug, Doc: doc}
	}
	return lg
}

// EndpointPostureForEntity computes the full posture (error_flow, rate_limit,
// deprecation, feature_gates, auth) for a single endpoint entity in repo `slug`
// of the supplied in-memory document set, reusing buildPosturePayload — the same
// assembly archigraph_endpoint_posture runs. Returns (payload, true) when the
// entity is found, (zero, false) otherwise. The HasPosture field reports whether
// any facet is non-empty (the honest-empty signal the dashboard renders as
// "none").
func EndpointPostureForEntity(group string, docs map[string]*graph.Document, slug, entityID string) (PosturePayload, bool) {
	lg := loadedGroupFromDocs(group, docs)
	r, ok := lg.Repos[slug]
	if !ok || r.Doc == nil {
		return PosturePayload{}, false
	}
	byID := r.getByID()
	e, ok := byID[entityID]
	if !ok || e == nil {
		return PosturePayload{}, false
	}
	return buildPosturePayload(r, e), true
}

// EffectiveContractForTarget resolves `target` (a ViewSet/controller entity_id,
// prefixed id, or bare class name) to its grouped per-verb effective contract
// across the supplied document set, reusing computeEffectiveContract — the same
// MRO/baseknowledge-pack-aware resolution archigraph_effective_contract runs.
// When nothing resolves the result's Groups is empty and Note explains why
// (honest-partial — never a fabricated contract).
func EffectiveContractForTarget(group string, docs map[string]*graph.Document, target string) EffectiveContractResult {
	lg := loadedGroupFromDocs(group, docs)
	return computeEffectiveContract(lg, target)
}
