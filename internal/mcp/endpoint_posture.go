// endpoint_posture.go — grafel_endpoint_posture MCP tool (deploy-9 caps
// surfacing).
//
// # Background
//
// Several #3628 cross-language capabilities are populated in the graph but are
// NOT discoverable through any MCP tool, so an MCP consumer (the rewrite agent)
// reports them "not populated" even though the underlying nodes/edges/props
// exist. The data lives in:
//
//   - error_flow              : THROWS / CATCHES edges from a callable to
//     SCOPE.ExceptionType nodes (Name "exception:<Type>").
//   - feature_flag_gating     : GATED_BY edges from a callable to
//     SCOPE.FeatureFlag nodes (ID "feature:<key>").
//   - rate_limit_stamping     : rate_limit / rate_limited / rate_limit_scope /
//     rate_limit_source properties on endpoints.
//   - deprecation/versioning  : deprecated / deprecated_since /
//     deprecated_replacement / api_version /
//     deprecation_source properties on
//     http_endpoint_definition entities.
//   - gRPC/tRPC interceptor   : auth_required / auth_method (e.g.
//     auth                      "grpc_interceptor") / auth_middleware /
//     trpc_middleware / auth_guard / auth_roles /
//     auth_scopes / auth_confidence properties.
//
// None of these is an ENTITY FIELD, so inspect(fields=[throws,catches]) returns
// empty and search(kind=Config,"feature") matches the wrong kind. This tool adds
// a single, discoverable read-only SURFACE that assembles a callable's /
// endpoint's "posture" from exactly those nodes, edges, and properties — the
// natural query an MCP consumer runs to answer "does the NestJS port preserve
// the Django endpoint's thrown exceptions / rate limits / deprecation / feature
// gates / auth?".
//
// It reads only the shared cross-language node/edge KINDS (SCOPE.ExceptionType,
// SCOPE.FeatureFlag, THROWS, CATCHES, GATED_BY) and shared property keys, so it
// works identically across all 12 languages — nothing here is framework- or
// language-specific.
//
// # Modes
//
//   - entity_id set  → per-entity posture (resolver mirrors grafel_effects /
//     grafel_inspect: prefixed id, label, qualified name; ambiguity returns
//     a chooser).
//   - entity_id unset → repo-wide scan: every endpoint/callable that carries a
//     NON-EMPTY posture facet (a throw/catch, a feature gate, a rate limit, a
//     deprecation/version marker, or auth). Endpoints with nothing notable are
//     omitted so the response stays focused.
//
// The tool is strictly read-only and adds no new node/edge kinds.
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// posture facet edge / property key constants — kept here (the values mirror
// internal/types/kinds.go and the engine property stamps) so the surface has a
// single, documented contract and does not silently drift on a string typo.
const (
	edgeThrows  = "THROWS"
	edgeCatches = "CATCHES"
	edgeGatedBy = "GATED_BY"

	kindExceptionType = "SCOPE.ExceptionType"
	kindFeatureFlag   = "SCOPE.FeatureFlag"
)

// postureRateLimitKeys are the endpoint property keys that, when present and
// non-empty, indicate the endpoint is rate limited. rate_limited is the boolean
// marker; the others carry detail. Kept in one slice so the per-entity and
// scan paths agree on what "has a rate limit" means.
var postureRateLimitKeys = []string{"rate_limited", "rate_limit", "rate_limit_scope", "rate_limit_source"}

// postureDeprecationKeys are the endpoint property keys for the
// deprecation/versioning facet.
var postureDeprecationKeys = []string{"deprecated", "deprecated_since", "deprecated_replacement", "api_version", "deprecation_source"}

// postureAuthKeys are the endpoint/method property keys for the auth facet
// (covers HTTP guards/middleware AND gRPC/tRPC interceptor auth — they share
// the same property surface).
var postureAuthKeys = []string{"auth_required", "auth_method", "auth_middleware", "trpc_middleware", "auth_guard", "auth_roles", "auth_scopes", "auth_confidence"}

// errorFlow is the resolved error-contract facet: the exception TYPE names a
// callable can raise (throws) and handle (catches), de-duplicated and sorted.
type errorFlow struct {
	Throws  []string `json:"throws,omitempty"`
	Catches []string `json:"catches,omitempty"`
}

func (e errorFlow) empty() bool { return len(e.Throws) == 0 && len(e.Catches) == 0 }

// posturePayload is the per-entity wire shape returned by the tool.
type posturePayload struct {
	EntityID    string            `json:"entity_id"`
	Name        string            `json:"name,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Repo        string            `json:"repo"`
	SourceFile  string            `json:"source_file,omitempty"`
	StartLine   int               `json:"start_line,omitempty"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	ErrorFlow   *errorFlow        `json:"error_flow,omitempty"`
	RateLimit   map[string]string `json:"rate_limit,omitempty"`
	Deprecation map[string]string `json:"deprecation,omitempty"`
	FeatureGate []string          `json:"feature_gates,omitempty"`
	Auth        map[string]string `json:"auth,omitempty"`
	// HasPosture is true when at least one facet is non-empty. Used by the
	// scan mode to decide whether to include the entity.
	HasPosture bool `json:"has_posture"`
}

// handleEndpointPosture implements grafel_endpoint_posture. With entity_id
// it returns the single entity's posture; without it, the repo-wide scan.
func (s *Server) handleEndpointPosture(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	key := argString(req, "entity_id", "")
	if key == "" {
		return s.endpointPostureScan(req, repos)
	}

	// Cross-repo prefixed ID? Resolve repo first for unambiguous lookup.
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if e := r.LabelIndex.ByID(local); e != nil {
				return jsonResult(buildPosturePayload(r, e)), nil
			}
		}
	}

	type matchPair struct {
		ent  *graph.Entity
		repo *LoadedRepo
	}
	var matches []matchPair
	for _, r := range repos {
		if r.LabelIndex == nil {
			continue
		}
		for _, hit := range r.LabelIndex.LookupAll(key) {
			matches = append(matches, matchPair{ent: hit, repo: r})
		}
	}
	if len(matches) == 0 {
		return mcpapi.NewToolResultError("not found: " + key), nil
	}
	if len(matches) > 1 {
		out := make([]map[string]any, 0, len(matches))
		for _, m := range matches {
			out = append(out, map[string]any{
				"id":             prefixedID(m.repo.Repo, m.ent.ID),
				"qualified_name": m.ent.QualifiedName,
				"label":          m.ent.Name,
				"kind":           m.ent.Kind,
				"repo":           m.repo.Repo,
				"source_file":    m.ent.SourceFile,
			})
		}
		return jsonResult(map[string]any{
			"ambiguous":     true,
			"entity_id":     key,
			"matches":       out,
			"how_to_choose": "Re-call grafel_endpoint_posture with the prefixed id field (e.g. \"repo::1234abcd\").",
		}), nil
	}
	return jsonResult(buildPosturePayload(matches[0].repo, matches[0].ent)), nil
}

// endpointPostureScan returns the posture of every endpoint/callable that has a
// non-empty posture facet across the considered repos. Endpoints with nothing
// notable are omitted. Supports the same path_contains / method narrowing as
// the endpoints tool plus a facet filter (facet=error_flow|rate_limit|
// deprecation|feature_flag|auth).
func (s *Server) endpointPostureScan(req mcpapi.CallToolRequest, repos []*LoadedRepo) (*mcpapi.CallToolResult, error) {
	pathContains := strings.ToLower(argString(req, "path_contains", ""))
	method := strings.ToUpper(argString(req, "method", ""))
	facet := strings.ToLower(strings.TrimSpace(argString(req, "facet", "")))
	limit := argInt(req, "limit", 50)
	offset := argInt(req, "offset", 0)

	var out []posturePayload
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// #5928 collect-then-process: buildPosturePayload→resolveErrorFlow/
		// resolveFeatureGates call r.getAdjacency()/r.getByIDOne, which re-lock the
		// same readerMu the flag-ON forEachEntity scan holds across its whole pass —
		// calling them INSIDE the closure self-deadlocks on the live-Reader path.
		// Collect the candidate entities in-scan (rmu held); build each posture AFTER
		// the scan returns (rmu released). forEachEntity materializes heap-safe entity
		// copies flag-ON (and yields stable &Doc.Entities[i] flag-OFF), so retaining
		// the pointers past the scan is safe. Visitation order is preserved, so the
		// pre-sort `out` order (and therefore the final result) is byte-identical.
		var candidates []*graph.Entity
		r.forEachEntity(func(e *graph.Entity) bool {
			// Skip the synthetic convergence nodes themselves — they are the
			// targets of the edges, never the subject of a posture query.
			if strings.EqualFold(e.Kind, kindExceptionType) || strings.EqualFold(e.Kind, kindFeatureFlag) {
				return true
			}
			candidates = append(candidates, e)
			return true
		})
		for _, e := range candidates {
			p := buildPosturePayload(r, e)
			if !p.HasPosture {
				continue
			}
			if facet != "" && !postureHasFacet(p, facet) {
				continue
			}
			if pathContains != "" && !strings.Contains(strings.ToLower(p.Path), pathContains) {
				continue
			}
			if method != "" && !strings.EqualFold(p.Method, method) {
				continue
			}
			out = append(out, p)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].EntityID < out[j].EntityID
	})

	total := len(out)
	out = pageSlice(out, offset, limit)
	if out == nil {
		out = []posturePayload{}
	}
	return jsonResult(map[string]any{
		"count":         len(out),
		"total":         total,
		"offset":        offset,
		"truncated":     offset+len(out) < total,
		"path_contains": pathContains,
		"method":        method,
		"facet":         facet,
		"endpoints":     out,
	}), nil
}

// postureHasFacet reports whether payload p carries the named facet.
func postureHasFacet(p posturePayload, facet string) bool {
	switch facet {
	case "error_flow", "errorflow", "error":
		return p.ErrorFlow != nil
	case "rate_limit", "ratelimit", "rate":
		return len(p.RateLimit) > 0
	case "deprecation", "deprecated", "version", "versioning":
		return len(p.Deprecation) > 0
	case "feature_flag", "feature_flags", "feature", "gating":
		return len(p.FeatureGate) > 0
	case "auth", "grpc_auth", "grpc", "trpc":
		return len(p.Auth) > 0
	default:
		return true
	}
}

// buildPosturePayload assembles the full posture for one entity from its
// outbound THROWS/CATCHES/GATED_BY edges (resolved to ExceptionType /
// FeatureFlag node names) and its posture properties. Centralised so the
// per-entity and scan paths emit byte-identical shapes.
func buildPosturePayload(r *LoadedRepo, e *graph.Entity) posturePayload {
	p := posturePayload{
		EntityID:   prefixedID(r.Repo, e.ID),
		Name:       e.Name,
		Kind:       e.Kind,
		Repo:       r.Repo,
		SourceFile: e.SourceFile,
		StartLine:  e.StartLine,
	}
	if e.PropLen() > 0 {
		p.Method = e.PropGet("verb")
		p.Path = e.PropGet("path")
	}

	// --- error_flow: resolve THROWS / CATCHES edge targets to type names. ---
	ef := resolveErrorFlow(r, e.ID)
	if !ef.empty() {
		p.ErrorFlow = &ef
	}

	// --- feature_flag_gating: resolve GATED_BY edge targets to flag keys. ---
	if gates := resolveFeatureGates(r, e.ID); len(gates) > 0 {
		p.FeatureGate = gates
	}

	// --- property-derived facets. ---
	p.RateLimit = collectProps(e.PropsSnapshot(), postureRateLimitKeys)
	p.Deprecation = collectProps(e.PropsSnapshot(), postureDeprecationKeys)
	p.Auth = collectProps(e.PropsSnapshot(), postureAuthKeys)

	p.HasPosture = p.ErrorFlow != nil ||
		len(p.FeatureGate) > 0 ||
		len(p.RateLimit) > 0 ||
		len(p.Deprecation) > 0 ||
		len(p.Auth) > 0
	return p
}

// resolveErrorFlow walks the entity's outbound THROWS / CATCHES edges and
// resolves each target ExceptionType node to its bare type name (stripping the
// "exception:" prefix the synthetic node carries on its Name). De-duplicated and
// sorted for stable output.
func resolveErrorFlow(r *LoadedRepo, localID string) errorFlow {
	adj := r.getAdjacency()
	throws := map[string]bool{}
	catches := map[string]bool{}
	for _, ed := range adj.Outgoing(localID) {
		var bucket map[string]bool
		switch {
		case strings.EqualFold(ed.kind, edgeThrows):
			bucket = throws
		case strings.EqualFold(ed.kind, edgeCatches):
			bucket = catches
		default:
			continue
		}
		tgtEnt, _ := r.getByIDOne(ed.target)
		name := exceptionTypeName(tgtEnt, ed.target)
		if name != "" {
			bucket[name] = true
		}
	}
	return errorFlow{Throws: sortedNonEmpty(throws), Catches: sortedNonEmpty(catches)}
}

// exceptionTypeName returns the bare exception type name for a THROWS/CATCHES
// target. Prefers the resolved entity's Name (stripped of the "exception:"
// synthetic prefix); falls back to the raw target id when the node is not in
// this repo (cross-repo / unresolved), itself stripped of the prefix.
func exceptionTypeName(e *graph.Entity, rawTarget string) string {
	if e != nil && e.Name != "" {
		return strings.TrimPrefix(e.Name, "exception:")
	}
	return strings.TrimPrefix(rawTarget, "exception:")
}

// resolveFeatureGates walks the entity's outbound GATED_BY edges and resolves
// each target FeatureFlag node to its flag key (stripping the "feature:"
// synthetic id/name prefix). De-duplicated and sorted.
func resolveFeatureGates(r *LoadedRepo, localID string) []string {
	adj := r.getAdjacency()
	keys := map[string]bool{}
	for _, ed := range adj.Outgoing(localID) {
		if !strings.EqualFold(ed.kind, edgeGatedBy) {
			continue
		}
		tgtEnt, _ := r.getByIDOne(ed.target)
		key := featureFlagKey(tgtEnt, ed.target)
		if key != "" {
			keys[key] = true
		}
	}
	return sortedNonEmpty(keys)
}

// featureFlagKey returns the flag key for a GATED_BY target. Prefers the
// resolved node's Name, falling back to its id; both are stripped of the
// "feature:" synthetic prefix.
func featureFlagKey(e *graph.Entity, rawTarget string) string {
	if e != nil {
		if e.Name != "" {
			return strings.TrimPrefix(e.Name, "feature:")
		}
		if e.ID != "" {
			return strings.TrimPrefix(e.ID, "feature:")
		}
	}
	return strings.TrimPrefix(rawTarget, "feature:")
}

// collectProps returns the subset of props whose key is in keys and whose value
// is non-empty. Returns nil (not an empty map) when nothing matches so the
// payload's omitempty drops the facet entirely.
func collectProps(props map[string]string, keys []string) map[string]string {
	if len(props) == 0 {
		return nil
	}
	var out map[string]string
	for _, k := range keys {
		if v, ok := props[k]; ok && v != "" {
			if out == nil {
				out = make(map[string]string, len(keys))
			}
			out[k] = v
		}
	}
	return out
}

// sortedNonEmpty returns the set's keys sorted; nil when empty (so omitempty
// drops the field). Distinct from the package's sortedKeys, which returns a
// non-nil empty slice.
func sortedNonEmpty(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	return sortedKeys(m)
}
