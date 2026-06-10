// archigraph_stub_detector MCP tool (#4425, epic #4419 capability F).
//
// Detects v3-rewrite endpoints that LOOK implemented (right path, right DTO,
// green shape-tests) but return hardcoded / empty values where the behavioral
// oracle COMPUTES. Shape-and-presence tooling is blind to this — the response
// shape is correct, only the provenance is wrong (a constant where a DB read
// should be). The one observable that separates a stub from a real handler is
// the SIDE-EFFECT PROFILE.
//
// Signature:
//
//	stub_detector(
//	  group_v3:     "<v3-rewrite group>",   (required)
//	  group_oracle: "<oracle group>",       (required)
//	  endpoint:     "GET /api/orders/{id}", (optional — limit to one endpoint;
//	                                          matched on normalized method+path)
//	)
//
// Result (per linked endpoint):
//
//	{
//	  "endpoint":       "GET /api/orders/{id}",
//	  "signals": {
//	    "returns_literal":    "yes"|"no"|"unknown",
//	    "no_effects_v3":      "yes"|"no"|"unknown",
//	    "oracle_has_effects": "yes"|"no"|"unknown",
//	    "no_input_use":       "yes"|"no"|"unknown"
//	  },
//	  "verdict":        "likely_stub"|"thin"|"implemented",
//	  "confidence":     0.70,
//	  "v3_effects":     [...],
//	  "oracle_effects": [...],
//	  "rationale":      "..."
//	}
//
// # The join
//
// The two endpoints live in DIFFERENT groups, so there is no single cross-repo
// link record spanning them. We join on the structural identity of an HTTP
// endpoint: normalized METHOD + PATH (path-params canonicalised to {*}, lower-
// cased, trailing-slash and a leading /api[/vN] prefix stripped — the same
// canonicalisation the cross-repo HTTP link pass uses). An oracle endpoint
// with the same normalized (method, path) is the v3 endpoint's counterpart.
//
// # The effects contrast (the load-bearing signal)
//
// For each endpoint we compute the EFFECTIVE effects: the transitive union of
// effect kinds (db_read/db_write/http_out/fs/…) reachable from the endpoint's
// handler over downstream CALLS — the SAME computation the dashboard side-
// effects aggregation (#4489) and the `effects` MCP tool use, reading the same
// <group>-links-effects.json sidecar. The strongest stub signal is the cross-
// graph contrast: the oracle counterpart COMPUTES (non-empty effects) while the
// v3 endpoint is PURE (empty). The pure scoring + threshold lives in
// internal/stubdetector, unit-tested independently of MCP.
//
// Conservatism: when both sides are pure the endpoint is reported "thin", not
// a stub (nothing for the oracle to compute either); when either side has no
// effect data at all we cannot assert the contrast and report "thin" with low
// confidence rather than risk a false flag.
package mcp

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/stubdetector"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// stubHandlerResolveEdgeKinds are the producer-side edge kinds that link a
// backend handler to an http_endpoint_definition. Mirrors the dashboard
// resolver (internal/dashboard/paths_handler_resolve.go) so the same handler-
// rooting logic feeds the effects walk on the MCP side. IMPLEMENTS is the
// DRF/Nest shape; ROUTES_TO / SERVES cover Spring and other frameworks where
// the definition points at the handler directly.
var stubHandlerResolveEdgeKinds = map[string]bool{
	"IMPLEMENTS": true,
	"ROUTES_TO":  true,
	"SERVES":     true,
}

// stubEffectsMaxDepth / stubEffectsMaxNodes bound the transitive CALLS walk so
// a pathological graph can never blow up the request. Mirrors the dashboard's
// effectiveEffectsMaxDepth/Nodes.
const (
	stubEffectsMaxDepth = 12
	stubEffectsMaxNodes = 1500
)

// stubPathParamRe / stubAPIPrefixRe mirror the links-pass canonicalisation
// (internal/links/http_pass.go normalizePathForIndex + stripAPIPrefix) so the
// oracle↔v3 join keys identically to how the cross-repo HTTP link pass buckets
// route shapes. Kept local to avoid widening the links package API.
var (
	stubPathParamRe = regexp.MustCompile(`\{[^}]+\}|:[a-zA-Z][a-zA-Z0-9_]*|<[^>]+>`)
	stubAPIPrefixRe = regexp.MustCompile(`^/(?:api(?:/v\d+)?|v\d+)(/|$)`)
)

// stubJoinKey is the normalized (method, path) identity used to match a v3
// endpoint to its oracle counterpart.
type stubJoinKey struct {
	method string
	path   string
}

// handleStubDetector implements archigraph_stub_detector.
func (s *Server) handleStubDetector(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	v3Group := argString(req, "group_v3", "")
	oracleGroup := argString(req, "group_oracle", "")
	if v3Group == "" || oracleGroup == "" {
		return mcpapi.NewToolResultError("group_v3 and group_oracle are both required"), nil
	}

	lgV3 := s.State.Group(v3Group)
	if lgV3 == nil {
		return mcpapi.NewToolResultError("group_v3 " + v3Group + " not loaded"), nil
	}
	lgOracle := s.State.Group(oracleGroup)
	if lgOracle == nil {
		return mcpapi.NewToolResultError("group_oracle " + oracleGroup + " not loaded"), nil
	}

	// Optional single-endpoint filter — matched on the normalized join key.
	var filter *stubJoinKey
	if raw := strings.TrimSpace(argString(req, "endpoint", "")); raw != "" {
		k := parseEndpointFilter(raw)
		filter = &k
	}

	v3Effs, v3SidecarOK := loadEffectsSidecar(v3Group)
	oracleEffs, oracleSidecarOK := loadEffectsSidecar(oracleGroup)

	// A group has effect DATA when its effect-propagation pass ran: either the
	// links-effects sidecar loaded, or some entity carries a stamped effects
	// property (the in-process run case). Only then can an empty effect closure
	// be read as genuinely "pure" rather than "not analysed" — the honesty
	// gate that stops an unindexed group from looking like all-stubs.
	v3HasEffectData := v3SidecarOK || groupHasEffectProps(lgV3)
	oracleHasEffectData := oracleSidecarOK || groupHasEffectProps(lgOracle)

	// Build the oracle endpoint index keyed by normalized (method, path) once.
	oracleIdx := buildEndpointEffectsIndex(lgOracle, oracleEffs, oracleHasEffectData)

	// Enumerate v3 endpoints, join, score.
	results := make([]stubdetector.Result, 0)
	unlinked := make([]string, 0)

	for _, r := range sortedRepos(lgV3) {
		if r.Doc == nil {
			continue
		}
		hres := buildStubHandlerResolution(r)
		callsAdj := r.getCallsAdj()
		byID := r.getByID()

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDefinitionKind(e.Kind) {
				continue
			}
			if e.Properties["pattern_type"] == patternTypeHTTPEndpointClientSynthesis {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(e.Properties["verb"]))
			rawPath := e.Properties["path"]
			key := stubJoinKey{method: method, path: normalizeStubPath(rawPath)}
			if filter != nil && (filter.method != key.method || filter.path != key.path) {
				continue
			}

			label := endpointLabel(method, rawPath)

			oracleEntry, ok := oracleIdx[key]
			if !ok {
				unlinked = append(unlinked, label)
				continue
			}

			v3Eff := computeEndpointEffects(r.Repo, e, hres, callsAdj, byID, v3Effs, v3HasEffectData)

			results = append(results, stubdetector.Score(stubdetector.Input{
				Endpoint:      label,
				V3Effects:     v3Eff,
				OracleEffects: oracleEntry,
				// returns_literal / no_input_use are best-effort source-derived
				// signals not yet computed here — reported Unknown so they never
				// produce a false flag (the effects contrast carries the verdict).
				// Wiring a per-language return-literal analyzer is tracked as the
				// next increment on this tool.
				ReturnsLiteral: stubdetector.Unknown,
				NoInputUse:     stubdetector.Unknown,
			}))
		}
	}

	// Deterministic order: likely_stub first (highest confidence), then by
	// endpoint label.
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := verdictRank(results[i].Verdict), verdictRank(results[j].Verdict)
		if ri != rj {
			return ri < rj
		}
		if results[i].Confidence != results[j].Confidence {
			return results[i].Confidence > results[j].Confidence
		}
		return results[i].Endpoint < results[j].Endpoint
	})

	likelyStubs := 0
	for _, r := range results {
		if r.Verdict == stubdetector.VerdictLikelyStub {
			likelyStubs++
		}
	}

	sort.Strings(unlinked)
	return jsonResult(map[string]any{
		"group_v3":       v3Group,
		"group_oracle":   oracleGroup,
		"linked_count":   len(results),
		"likely_stubs":   likelyStubs,
		"results":        resultsToJSON(results),
		"unlinked_v3":    unlinked, // v3 endpoints with no oracle counterpart on (method,path)
		"join":           "normalized method+path (path-params → {*}, /api[/vN] prefix stripped)",
		"effects_source": "transitive downstream CALLS union over <group>-links-effects.json sidecar (same as the effects tool / dashboard side-effects)",
	}), nil
}

// resultsToJSON renders the scored results into the public JSON shape, mapping
// the Tristate signals to their string form.
func resultsToJSON(results []stubdetector.Result) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"endpoint": r.Endpoint,
			"signals": map[string]any{
				"returns_literal":    r.Signals.ReturnsLiteral.String(),
				"no_effects_v3":      r.Signals.NoEffectsV3.String(),
				"oracle_has_effects": r.Signals.OracleHasEffects.String(),
				"no_input_use":       r.Signals.NoInputUse.String(),
			},
			"verdict":        string(r.Verdict),
			"confidence":     r.Confidence,
			"v3_effects":     r.V3Effects,
			"oracle_effects": r.OracleEffects,
			"rationale":      r.Rationale,
		})
	}
	return out
}

func verdictRank(v stubdetector.Verdict) int {
	switch v {
	case stubdetector.VerdictLikelyStub:
		return 0
	case stubdetector.VerdictThin:
		return 1
	default:
		return 2
	}
}

// stubHandlerResolution maps an endpoint-definition entity ID to the local
// handler entity IDs that implement it (the effects-walk roots). Mirrors the
// dashboard repoEntityIndex.handlerOf but built for the MCP LoadedRepo.
type stubHandlerResolution struct {
	handlerOf map[string][]string
}

// buildStubHandlerResolution walks the repo's IMPLEMENTS/ROUTES_TO/SERVES edges
// once and records, per definition ID, the handler IDs that implement it.
func buildStubHandlerResolution(r *LoadedRepo) *stubHandlerResolution {
	res := &stubHandlerResolution{handlerOf: map[string][]string{}}
	byID := r.getByID()
	isDef := func(id string) bool {
		e := byID[id]
		return e != nil && isDefinitionKind(e.Kind)
	}
	isHandler := func(id string) bool {
		e := byID[id]
		return e != nil && !isDefinitionKind(e.Kind)
	}
	for i := range r.Doc.Relationships {
		rel := &r.Doc.Relationships[i]
		if !stubHandlerResolveEdgeKinds[rel.Kind] {
			continue
		}
		switch {
		case isDef(rel.ToID) && isHandler(rel.FromID): // handler --IMPLEMENTS--> def
			res.handlerOf[rel.ToID] = appendUniqueStr(res.handlerOf[rel.ToID], rel.FromID)
		case isDef(rel.FromID) && isHandler(rel.ToID): // def --ROUTES_TO--> handler
			res.handlerOf[rel.FromID] = appendUniqueStr(res.handlerOf[rel.FromID], rel.ToID)
		}
	}
	return res
}

// resolveStubHandlers returns the handler entity IDs to root the effects walk
// at for a definition. Falls back to the definition itself (frameworks that
// record the route directly on a real function, or unresolved handlers) so the
// walk always has a root.
func (h *stubHandlerResolution) resolveStubHandlers(def *graph.Entity) []string {
	if ids := h.handlerOf[def.ID]; len(ids) > 0 {
		return ids
	}
	return []string{def.ID}
}

// computeEndpointEffects computes the EFFECTIVE effects of an endpoint: the
// transitive union of effect kinds reachable from its handler over downstream
// CALLS. Uses the SAME effect source as the `effects` tool / dashboard side-
// effects: the sidecar (keyed by prefixed id) with an entity-property fallback.
//
// Resolved is true when this endpoint's effect closure can be trusted as
// COMPLETE: either a per-entity effect record was found in the closure (sidecar
// entry or stamped property), OR the group as a whole has effect data
// (groupHasEffectData) so an empty closure legitimately means "pure" rather than
// "not analysed". This is the honesty gate from the effects contract — an
// unindexed group (no effect data at all) yields Resolved=false so it never
// reads as a stub.
func computeEndpointEffects(
	repoSlug string,
	def *graph.Entity,
	hres *stubHandlerResolution,
	callsAdj map[string][]string,
	byID map[string]*graph.Entity,
	sidecar map[string]effectsSidecarEntry,
	groupHasEffectData bool,
) stubdetector.Effects {
	roots := hres.resolveStubHandlers(def)

	kindSet := map[string]bool{}
	// The group-level gate: when the effect pass ran for this group, an empty
	// closure is a trustworthy "pure". A per-entity hit below also flips it.
	resolved := groupHasEffectData

	visited := make(map[string]bool, len(roots))
	queue := make([]string, 0, len(roots))
	for _, id := range roots {
		if !visited[id] {
			visited[id] = true
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 && len(visited) <= stubEffectsMaxNodes {
		cur := queue[0]
		queue = queue[1:]

		effs, ok := effectsForLocalEntity(repoSlug, cur, byID, sidecar)
		if ok {
			resolved = true
			for _, k := range effs {
				kindSet[k] = true
			}
		}

		// Depth is bounded by the node cap + visited guard; the dashboard uses a
		// depth counter too, but a node cap alone bounds the walk and keeps this
		// simpler. Stop expanding once the node budget is hit.
		for _, to := range callsAdj[cur] {
			if visited[to] {
				continue
			}
			if len(visited) >= stubEffectsMaxNodes {
				break
			}
			visited[to] = true
			queue = append(queue, to)
		}
	}

	kinds := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return stubdetector.Effects{Kinds: kinds, Resolved: resolved}
}

// effectsForLocalEntity returns the effect kinds for a local entity, mirroring
// the source precedence of the `effects` tool + dashboard aggregation: the
// canonical sidecar (keyed by prefixed id) first, then the effect-propagation
// properties stamped on the entity. ok reports whether ANY effect record was
// found (even an empty one) so the caller can mark the closure "resolved".
func effectsForLocalEntity(
	repoSlug, local string,
	byID map[string]*graph.Entity,
	sidecar map[string]effectsSidecarEntry,
) ([]string, bool) {
	pid := prefixedID(repoSlug, local)
	if entry, ok := sidecar[pid]; ok {
		return entry.Effects, true
	}
	if e := byID[local]; e != nil && e.Properties != nil {
		if raw := strings.TrimSpace(e.Properties["effects"]); raw != "" {
			return splitNonEmpty(raw), true
		}
	}
	return nil, false
}

// buildEndpointEffectsIndex builds a normalized-(method,path) → effective-
// effects index for every endpoint definition in a group. Used to look up the
// oracle counterpart of a v3 endpoint. When two oracle endpoints normalise to
// the same key their effect kinds are unioned and Resolved OR-ed.
func buildEndpointEffectsIndex(lg *LoadedGroup, sidecar map[string]effectsSidecarEntry, groupHasEffectData bool) map[stubJoinKey]stubdetector.Effects {
	idx := map[stubJoinKey]stubdetector.Effects{}
	for _, r := range sortedRepos(lg) {
		if r.Doc == nil {
			continue
		}
		hres := buildStubHandlerResolution(r)
		callsAdj := r.getCallsAdj()
		byID := r.getByID()
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDefinitionKind(e.Kind) {
				continue
			}
			if e.Properties["pattern_type"] == patternTypeHTTPEndpointClientSynthesis {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(e.Properties["verb"]))
			key := stubJoinKey{method: method, path: normalizeStubPath(e.Properties["path"])}
			eff := computeEndpointEffects(r.Repo, e, hres, callsAdj, byID, sidecar, groupHasEffectData)
			if existing, ok := idx[key]; ok {
				idx[key] = mergeStubEffects(existing, eff)
			} else {
				idx[key] = eff
			}
		}
	}
	return idx
}

// mergeStubEffects unions two effect views (for endpoints that normalise to the
// same join key). Resolved is OR-ed; kinds are unioned + sorted.
func mergeStubEffects(a, b stubdetector.Effects) stubdetector.Effects {
	set := map[string]bool{}
	for _, k := range a.Kinds {
		set[k] = true
	}
	for _, k := range b.Kinds {
		set[k] = true
	}
	kinds := make([]string, 0, len(set))
	for k := range set {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return stubdetector.Effects{Kinds: kinds, Resolved: a.Resolved || b.Resolved}
}

// normalizeStubPath canonicalises an endpoint path for cross-group joining:
// path-params → {*}, lower-cased, trailing slash stripped, and a leading
// /api[/vN] prefix removed. Mirrors links.normalizePathForIndex + stripAPIPrefix
// so v3 and oracle route shapes bucket identically regardless of param names,
// case, trailing-slash, or api-prefix convention.
func normalizeStubPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = stubPathParamRe.ReplaceAllString(path, "{*}")
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	// Strip a leading /api[/vN] or /vN prefix so /api/v1/orders/{*} buckets with
	// /orders/{*}.
	if m := stubAPIPrefixRe.FindStringSubmatchIndex(path); m != nil {
		stripped := path[m[2]:]
		if stripped == "" {
			stripped = "/"
		}
		if !strings.HasPrefix(stripped, "/") {
			stripped = "/" + stripped
		}
		path = stripped
	}
	return path
}

// parseEndpointFilter parses a "GET /api/orders/{id}" filter into a normalized
// join key. A bare path with no leading verb leaves method empty (matches any
// method on that path).
func parseEndpointFilter(raw string) stubJoinKey {
	raw = strings.TrimSpace(raw)
	method := ""
	path := raw
	if fields := strings.Fields(raw); len(fields) == 2 {
		method = strings.ToUpper(fields[0])
		path = fields[1]
	}
	return stubJoinKey{method: method, path: normalizeStubPath(path)}
}

// endpointLabel renders a human "VERB /path" label, falling back gracefully
// when the verb or path is missing.
func endpointLabel(method, path string) string {
	method = strings.TrimSpace(method)
	path = strings.TrimSpace(path)
	switch {
	case method != "" && path != "":
		return method + " " + path
	case path != "":
		return path
	case method != "":
		return method
	default:
		return "(unknown endpoint)"
	}
}

// sortedRepos returns a group's repos in deterministic slug order.
func sortedRepos(lg *LoadedGroup) []*LoadedRepo {
	slugs := make([]string, 0, len(lg.Repos))
	for s := range lg.Repos {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	out := make([]*LoadedRepo, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, lg.Repos[s])
	}
	return out
}

// groupHasEffectProps reports whether ANY entity in the group carries a stamped
// "effects" property — the in-process effect-propagation marker. Used as the
// fallback "the effect pass ran" signal when the on-disk sidecar is absent
// (e.g. an in-memory test store or a pre-persist link run). Scans until the
// first hit, so it is cheap in the common (sidecar-present or has-effects)
// case. When neither the sidecar nor any property exists the group is treated
// as un-analysed and no endpoint can be flagged a stub.
func groupHasEffectProps(lg *LoadedGroup) bool {
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			p := r.Doc.Entities[i].Properties
			if p != nil && strings.TrimSpace(p["effects"]) != "" {
				return true
			}
		}
	}
	return false
}

// appendUniqueStr appends s to slice only if absent.
func appendUniqueStr(slice []string, s string) []string {
	for _, e := range slice {
		if e == s {
			return slice
		}
	}
	return append(slice, s)
}
