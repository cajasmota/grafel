// v2_paths_posture.go — Paths detail-pane "Posture" + "Effective contract"
// surface for WebUI v2 (#4254, epic #4249).
//
// Route:
//
//	GET /api/v2/groups/:id/paths/:hash/posture → v2PathPostureResponse
//
// This is the LAZY sibling of the main path-detail route: the Paths detail pane
// fetches it only when a path is opened, so the (large) paths-list and the
// path-detail payloads stay lean. It surfaces two graph-derived facets the flat
// auth_policy never exposed:
//
//   - posture: per endpoint entity — deprecation/api_version, rate-limit,
//     THROWS/error-flow exception list, feature-gates, and (gRPC/tRPC-aware)
//     auth — assembled by the SAME code archigraph_endpoint_posture runs
//     (internal/mcp.EndpointPostureForEntity → buildPosturePayload).
//   - effective_contract: per-verb status codes, serializer, permissions, and an
//     MRO-inherited flag for handlers resolved through a mixin EXTENDS chain —
//     resolved by the SAME code archigraph_effective_contract runs
//     (internal/mcp.EffectiveContractForTarget → computeEffectiveContract).
//
// HONESTY: posture facets are frequently empty (a route with no throws / no rate
// limit / no deprecation); those endpoints are still returned but with empty
// facets so the UI can render "none" rather than fabricating. effective_contract
// is null for non-ViewSet endpoints (NestJS/Go/GraphQL routes that carry no DRF
// router-expansion or pack-known base) — the contract result simply has no
// groups and the UI shows the empty state.

package dashboard

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Wire types — mirror webui-v2/src/data/types.ts
// ---------------------------------------------------------------------------

// v2PathPostureResponse is the payload for
// GET /api/v2/groups/:id/paths/:hash/posture. Both sections are honest-empty:
// Endpoints may all carry empty facets, and Contract may have zero groups.
type v2PathPostureResponse struct {
	PathHash string `json:"path_hash"`
	Path     string `json:"path"`
	// Endpoints is one posture row per matched endpoint entity (a path can have
	// several verbs / repos). Rows with no posture facet are kept (HasPosture
	// false) so the pane can show "none" rather than dropping the verb.
	Endpoints []mcp.PosturePayload `json:"endpoints"`
	// Contract is the resolved per-verb effective contract grouped by owning
	// ViewSet, or null when no DRF/pack-known ViewSet backs this path. Note
	// carries the honest-partial explanation when no groups resolved.
	Contract *mcp.EffectiveContractResult `json:"contract"`
}

// handleV2PathPosture — GET /api/v2/groups/:id/paths/:hash/posture
//
// Lazily serves the Posture + Effective-contract sections for the Paths detail
// pane, reusing the MCP endpoint_posture + effective_contract computation
// server-side (no re-derivation — same code path the MCP tools run, so the two
// surfaces never drift).
func (s *Server) handleV2PathPosture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pathHash := r.PathValue("hash")
	if id == "" || pathHash == "" {
		writeV2Err(w, http.StatusBadRequest, "params_required", "group id and path hash required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	// Build the in-memory document set the MCP helpers wrap, keyed by repo slug
	// (the SAME slug both packages use for prefixed entity ids).
	docs := make(map[string]*graph.Document)
	for _, repo := range sortedRepos(grp) {
		docs[repo.Slug] = repo.Doc
	}

	// Per-repo handler-resolution index, built lazily and reused across the
	// matched endpoint definitions of the same repo (#1646 pattern).
	repoIdx := map[string]*repoEntityIndex{}

	var pathStr string
	var postures []mcp.PosturePayload
	// contractTargets collects the distinct ViewSet/handler class names this
	// path resolves to, so we issue one effective_contract resolution per owning
	// ViewSet (deduplicated). Lower-cased leaf is the resolution key.
	contractTargets := []string{}
	seenTarget := map[string]bool{}

	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)
			isHTTP := types.IsHTTPEndpointKind(kind) ||
				strings.EqualFold(kind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTP {
				continue
			}
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if hashStr(path) != pathHash {
				continue
			}
			if pathStr == "" {
				pathStr = path
			}

			// --- Posture: assemble from the endpoint entity itself AND its
			// resolved handler. The posture facets (THROWS/CATCHES/GATED_BY edges,
			// rate-limit / deprecation / auth props) may live on either the
			// synthetic definition OR the real handler method, so we surface both
			// and keep whichever carries posture. We always emit at least the
			// endpoint row (HasPosture may be false → UI shows "none").
			if p, ok := mcp.EndpointPostureForEntity(grp.Name, docs, repo.Slug, e.ID); ok {
				postures = append(postures, p)
			}

			idx := repoIdx[repo.Slug]
			if idx == nil {
				idx = buildRepoEntityIndex(repo)
				repoIdx[repo.Slug] = idx
			}
			for _, h := range idx.resolveHandlers(e) {
				if h.ID == e.ID {
					continue
				}
				// A resolved handler that carries posture of its own (the real
				// viewset method's THROWS/rate-limit/etc.) — surface it too.
				if hp, ok := mcp.EndpointPostureForEntity(grp.Name, docs, repo.Slug, h.ID); ok && hp.HasPosture {
					postures = append(postures, hp)
				}
				// Effective-contract target: the owning class of the resolved
				// handler ("RoleViewSet.create" → "RoleViewSet").
				if cls := owningClassOfHandler(h); cls != "" {
					key := strings.ToLower(cls)
					if !seenTarget[key] {
						seenTarget[key] = true
						contractTargets = append(contractTargets, cls)
					}
				}
			}
			// Also consider the endpoint's own drf_view_method attribution (the
			// router-expanded route records "ViewSet.method" directly), so a path
			// whose handler did not re-resolve still yields a contract target.
			if dvm := e.Properties["drf_view_method"]; dvm != "" {
				if cls := classFromDotted(dvm); cls != "" {
					key := strings.ToLower(cls)
					if !seenTarget[key] {
						seenTarget[key] = true
						contractTargets = append(contractTargets, cls)
					}
				}
			}
		}
	}

	if pathStr == "" {
		writeV2Err(w, http.StatusNotFound, "path_not_found", "path not found: "+pathHash)
		return
	}

	// Deterministic, deduplicated posture rows (a verb's endpoint + handler can
	// produce two rows for the same entity id across repeated path entities).
	postures = dedupePostures(postures)
	sort.Slice(postures, func(i, j int) bool {
		if postures[i].Path != postures[j].Path {
			return postures[i].Path < postures[j].Path
		}
		if postures[i].Method != postures[j].Method {
			return postures[i].Method < postures[j].Method
		}
		return postures[i].EntityID < postures[j].EntityID
	})
	if postures == nil {
		postures = []mcp.PosturePayload{}
	}

	// --- Effective contract: resolve each distinct owning ViewSet and merge the
	// groups. Reuses the MRO/baseknowledge-pack-aware computation the MCP tool
	// runs. Null when no ViewSet backs this path (non-DRF endpoints).
	var contract *mcp.EffectiveContractResult
	sort.Strings(contractTargets)
	for _, target := range contractTargets {
		res := mcp.EffectiveContractForTarget(grp.Name, docs, target)
		if len(res.Groups) == 0 {
			continue
		}
		if contract == nil {
			merged := res
			merged.Target = pathStr
			contract = &merged
			continue
		}
		contract.Groups = append(contract.Groups, res.Groups...)
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2PathPostureResponse{
		PathHash:  pathHash,
		Path:      pathStr,
		Endpoints: postures,
		Contract:  contract,
	}))
}

// owningClassOfHandler returns the class/ViewSet leaf that owns a resolved
// handler entity, for use as an effective_contract resolution target. Prefers
// the handler's qualified name ("app.views.RoleViewSet.create" → "RoleViewSet"),
// then its plain name, then any drf_view_method attribution. Empty when no class
// can be derived (a free-function handler).
func owningClassOfHandler(h *graph.Entity) string {
	if h == nil {
		return ""
	}
	if cls := classFromDotted(h.QualifiedName); cls != "" {
		return cls
	}
	if cls := classFromDotted(h.Name); cls != "" {
		return cls
	}
	if h.Properties != nil {
		if cls := classFromDotted(h.Properties["drf_view_method"]); cls != "" {
			return cls
		}
	}
	return ""
}

// classFromDotted returns the class leaf from a dotted "Owner.method" (or
// "pkg.Owner.method") string: the second-to-last dotted segment. Returns "" when
// the input has no dot (a bare name with no method, which carries no class
// attribution on its own).
func classFromDotted(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip any "<repo>::" scope prefix first.
	if sc := strings.LastIndex(s, "::"); sc >= 0 {
		s = s[sc+2:]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// dedupePostures removes duplicate posture rows by entity id (the same handler
// can be reached through several path entities / verbs). First occurrence wins.
func dedupePostures(in []mcp.PosturePayload) []mcp.PosturePayload {
	if len(in) == 0 {
		return in
	}
	seen := map[string]bool{}
	out := in[:0:0]
	for _, p := range in {
		if seen[p.EntityID] {
			continue
		}
		seen[p.EntityID] = true
		out = append(out, p)
	}
	return out
}
