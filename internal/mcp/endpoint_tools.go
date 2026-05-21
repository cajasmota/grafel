// endpoint_tools.go — MCP tools for HTTP endpoint kinds (#1220).
//
// # Backward-compatibility aliasing
//
// Sub-A (#1217) splits the single "http_endpoint" kind into two finer-grained
// kinds:
//
//	http_endpoint_definition — the handler/route that defines an HTTP endpoint
//	http_endpoint_call       — a call-site (FETCHES edge source) that invokes one
//
// This file provides:
//   - expandKindAlias: normalises a caller-supplied kind string so that the
//     legacy "http_endpoint" value transparently expands to both new kinds. Any
//     query that already uses "http_endpoint_definition" or "http_endpoint_call"
//     continues to work as-is (no expansion needed).
//   - matchesKindFilter: a drop-in replacement for the old
//     strings.EqualFold(e.Kind, kindFilter) guard used by handleQualityOrphans
//     and handleSearchEntities. It calls expandKindAlias so those tools gain
//     alias support without further changes.
//   - Three new focused tools:
//       archigraph_endpoint_definitions — list definition-side entities only
//       archigraph_endpoint_calls       — list call-site entities only
//       archigraph_endpoint_stats       — counts of each kind + orphan summary
//
// Migration path (for agents and external callers)
//
//	Old value          Still works?  New preferred values
//	──────────────────────────────────────────────────────
//	http_endpoint      YES (alias)   http_endpoint_definition, http_endpoint_call
//	http_endpoint_def… YES (exact)   (unchanged)
//	http_endpoint_cal… YES (exact)   (unchanged)
//
// The legacy value "http_endpoint" is NOT removed from tool descriptions; it
// remains a valid input and will always be recognised via alias expansion.
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Kind alias expansion
// ---------------------------------------------------------------------------

// kindAliases maps legacy / umbrella kind names to the canonical kinds that
// should be matched when the user supplies the legacy name. Lookup is
// case-insensitive (normalise to lower-case before consulting the map).
//
// NOTE: keep in sync with internal/types/kinds.go when new splits land.
var kindAliases = map[string][]string{
	// http_endpoint was split into definition + call in Sub-A (#1217).
	// When Sub-A is not yet deployed, both new kind names may be absent from
	// the graph — the query returns empty results in that case, which is
	// correct and safe.
	"http_endpoint": {
		"http_endpoint",
		"http_endpoint_definition",
		"http_endpoint_call",
	},
}

// expandKindAlias returns the set of kind strings that a caller-supplied kind
// value should match. If the kind has a registered alias, the expanded set is
// returned; otherwise a single-element slice containing the original kind is
// returned. The comparison is case-insensitive.
func expandKindAlias(kind string) []string {
	if kind == "" {
		return nil
	}
	if expanded, ok := kindAliases[strings.ToLower(kind)]; ok {
		return expanded
	}
	return []string{kind}
}

// matchesKindFilter reports whether entity e matches kindFilter, respecting
// alias expansion. An empty kindFilter always returns true (no filtering).
//
// Use this instead of strings.EqualFold(e.Kind, kindFilter) everywhere a kind
// filter is applied to graph entities.
func matchesKindFilter(e *graph.Entity, kindFilter string) bool {
	if kindFilter == "" {
		return true
	}
	for _, k := range expandKindAlias(kindFilter) {
		if strings.EqualFold(e.Kind, k) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// isHTTPEndpointKind — shared predicate used by all three endpoint tools
// ---------------------------------------------------------------------------

// isHTTPEndpointKind reports whether kind (lowercased, scope-prefix stripped)
// is any of the recognised HTTP-endpoint kinds.
func isHTTPEndpointKind(kind string) bool {
	k := strings.ToLower(stripScopePrefix(kind))
	return k == "http_endpoint" ||
		k == "http_endpoint_definition" ||
		k == "http_endpoint_call"
}

// isDefinitionKind reports whether kind represents a handler/route definition.
func isDefinitionKind(kind string) bool {
	k := strings.ToLower(stripScopePrefix(kind))
	return k == "http_endpoint" || k == "http_endpoint_definition"
}

// isCallKind reports whether kind represents a call-site (consumer side).
func isCallKind(kind string) bool {
	k := strings.ToLower(stripScopePrefix(kind))
	return k == "http_endpoint_call"
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_definitions
// ---------------------------------------------------------------------------

// handleEndpointDefinitions lists http_endpoint_definition entities (and the
// legacy http_endpoint kind when Sub-A has not yet landed). This tool returns
// ONLY definition-side entries — no call-sites.
//
// Tool name: archigraph_endpoint_definitions
func (s *Server) handleEndpointDefinitions(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 200)
	group := argString(req, "group", "")
	_ = group

	type item struct {
		EntityID   string            `json:"entity_id"`
		Name       string            `json:"name"`
		Kind       string            `json:"kind"`
		Repo       string            `json:"repo"`
		SourceFile string            `json:"source_file,omitempty"`
		StartLine  int               `json:"start_line,omitempty"`
		Method     string            `json:"method,omitempty"`
		Path       string            `json:"path,omitempty"`
		Properties map[string]string `json:"properties,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDefinitionKind(e.Kind) {
				continue
			}
			// Exclude entities whose pattern_type marks them as client-synthesis
			// (consumer-side) — those belong in archigraph_endpoint_calls.
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			out = append(out, item{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       e.Kind,
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				Method:     e.Properties["verb"],
				Path:       e.Properties["path"],
				Properties: e.Properties,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"definitions": out,
		"count":       len(out),
		"total":       total,
		"truncated":   total > len(out),
		"note":        "http_endpoint kind is deprecated; prefer http_endpoint_definition for handler/route entities.",
	}), nil
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_calls
// ---------------------------------------------------------------------------

// handleEndpointCalls lists http_endpoint_call entities — call-sites that
// invoke an HTTP endpoint (i.e. the FETCHES-edge source entities). For each
// call-site that has no matching definition anywhere in the group, a reasoning
// hint is included.
//
// Tool name: archigraph_endpoint_calls
func (s *Server) handleEndpointCalls(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 200)
	orphanOnly := argBool(req, "orphan_only", false)

	// Build a set of all definition-side entity IDs so we can detect
	// call-sites with no matching definition (orphan callers).
	definitionIDs := map[string]bool{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isDefinitionKind(e.Kind) && e.Properties["pattern_type"] != "http_endpoint_client_synthesis" {
				definitionIDs[prefixedID(r.Repo, e.ID)] = true
				definitionIDs[e.ID] = true // bare form for same-repo lookups
			}
		}
	}

	type item struct {
		EntityID         string            `json:"entity_id"`
		Name             string            `json:"name"`
		Kind             string            `json:"kind"`
		Repo             string            `json:"repo"`
		SourceFile       string            `json:"source_file,omitempty"`
		StartLine        int               `json:"start_line,omitempty"`
		Method           string            `json:"method,omitempty"`
		Path             string            `json:"path,omitempty"`
		MatchedDefinition string           `json:"matched_definition,omitempty"`
		OrphanHint       string            `json:"orphan_hint,omitempty"`
		Properties       map[string]string `json:"properties,omitempty"`
	}

	// Build FETCHES edge map: callerID → toID (definition target).
	type fetchesEdge struct {
		toID string
		path string
	}
	callerToTarget := map[string]fetchesEdge{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "FETCHES" {
				continue
			}
			key := prefixedID(r.Repo, rel.FromID)
			if _, exists := callerToTarget[key]; !exists {
				fe := fetchesEdge{toID: rel.ToID}
				if rel.Properties != nil {
					fe.path = rel.Properties["path"]
				}
				callerToTarget[key] = fe
			}
		}
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			// Accept explicit call kind OR client-synthesis http_endpoint.
			isCall := isCallKind(e.Kind) ||
				(isDefinitionKind(e.Kind) && e.Properties["pattern_type"] == "http_endpoint_client_synthesis")
			if !isCall {
				continue
			}

			eid := prefixedID(r.Repo, e.ID)

			// Determine if this call-site has a matched definition.
			matched := ""
			orphanHint := ""
			if fe, ok := callerToTarget[eid]; ok {
				if definitionIDs[fe.toID] || definitionIDs[prefixedID(r.Repo, fe.toID)] {
					matched = fe.toID
				} else {
					// No matching definition found — produce a reasoning hint.
					urlPattern := fe.path
					if urlPattern == "" {
						urlPattern = e.Properties["path"]
					}
					if urlPattern != "" {
						orphanHint = "this call to " + urlPattern + " has no matching definition — see orphan_callers"
					} else {
						orphanHint = "this call has no matching definition — see orphan_callers"
					}
				}
			} else {
				// No FETCHES edge at all — possibly an isolated call-site.
				urlPattern := e.Properties["path"]
				if urlPattern != "" {
					orphanHint = "this call to " + urlPattern + " has no matching definition — see orphan_callers"
				}
			}

			if orphanOnly && orphanHint == "" {
				continue
			}

			out = append(out, item{
				EntityID:          eid,
				Name:              e.Name,
				Kind:              e.Kind,
				Repo:              r.Repo,
				SourceFile:        e.SourceFile,
				StartLine:         e.StartLine,
				Method:            e.Properties["verb"],
				Path:              e.Properties["path"],
				MatchedDefinition: matched,
				OrphanHint:        orphanHint,
				Properties:        e.Properties,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		// Orphans first, then by repo + name.
		iOrphan := out[i].OrphanHint != ""
		jOrphan := out[j].OrphanHint != ""
		if iOrphan != jOrphan {
			return iOrphan // orphans first
		}
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})

	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"calls":     out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
		"note":      "http_endpoint kind is deprecated; prefer http_endpoint_call for consumer-side call-site entities.",
	}), nil
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_stats
// ---------------------------------------------------------------------------

// handleEndpointStats returns a count breakdown of each HTTP-endpoint kind
// across the group, plus a summary of orphan call-sites (calls with no
// matching definition).
//
// Tool name: archigraph_endpoint_stats
func (s *Server) handleEndpointStats(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type repoStats struct {
		Repo        string `json:"repo"`
		Definitions int    `json:"definitions"`
		Calls       int    `json:"calls"`
		LegacyKind  int    `json:"legacy_kind"` // entities whose kind is plain "http_endpoint" (not split yet)
		OrphanCalls int    `json:"orphan_calls"`
	}

	// Build definition-ID set first (needed for orphan detection below).
	definitionIDs := map[string]bool{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isDefinitionKind(e.Kind) && e.Properties["pattern_type"] != "http_endpoint_client_synthesis" {
				definitionIDs[e.ID] = true
				definitionIDs[prefixedID(r.Repo, e.ID)] = true
			}
		}
	}

	var perRepo []repoStats
	totalDefs, totalCalls, totalLegacy, totalOrphans := 0, 0, 0, 0

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		rs := repoStats{Repo: r.Repo}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			k := strings.ToLower(stripScopePrefix(e.Kind))
			switch {
			case k == "http_endpoint_definition":
				rs.Definitions++
			case k == "http_endpoint_call":
				rs.Calls++
			case k == "http_endpoint":
				// Pre-Sub-A entity; count separately.
				rs.LegacyKind++
				if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
					rs.Calls++ // treat client-synthesis as a call
				} else {
					rs.Definitions++ // treat producer as a definition
				}
			}
		}

		// Count orphan call-sites: FETCHES edges whose ToID is not a definition.
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "FETCHES" {
				continue
			}
			if !definitionIDs[rel.ToID] && !definitionIDs[prefixedID(r.Repo, rel.ToID)] {
				rs.OrphanCalls++
			}
		}

		totalDefs += rs.Definitions
		totalCalls += rs.Calls
		totalLegacy += rs.LegacyKind
		totalOrphans += rs.OrphanCalls
		perRepo = append(perRepo, rs)
	}

	sort.Slice(perRepo, func(i, j int) bool { return perRepo[i].Repo < perRepo[j].Repo })

	migrated := totalLegacy == 0
	note := ""
	if !migrated {
		note = "graph still contains legacy http_endpoint kind — run the indexer after Sub-A (#1217) lands to split into http_endpoint_definition / http_endpoint_call"
	}

	return jsonResult(map[string]any{
		"totals": map[string]any{
			"definitions":  totalDefs,
			"calls":        totalCalls,
			"legacy_kind":  totalLegacy,
			"orphan_calls": totalOrphans,
		},
		"per_repo":  perRepo,
		"migrated":  migrated,
		"note":      note,
	}), nil
}
