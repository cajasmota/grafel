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
//     archigraph_endpoint_definitions — list definition-side entities only
//     archigraph_endpoint_calls       — list call-site entities only
//     archigraph_endpoint_stats       — counts of each kind + orphan summary
//
// # #1745 additions (on top of #1650 + #1751)
//   - Triple-path dedupe: Properties["path"] and Properties["verb"] are hoisted
//     to top-level Method/Path and stripped from the bag; Name is suppressed
//     when it would duplicate "<verb> <path>" or just "<path>".
//   - format="terse" (default) | "full" explicit param (alias for verbose=bool).
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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// endpointDefItem is the package-level shape for a definition row, used by
// both handleEndpointDefinitions and renderTerseDefinitions.
type endpointDefItem struct {
	EntityID   string            `json:"entity_id"`
	Name       string            `json:"name,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Repo       string            `json:"repo"`
	SourceFile string            `json:"source_file,omitempty"`
	StartLine  int               `json:"start_line,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// endpointCallItem is the package-level shape for a call-site row.
type endpointCallItem struct {
	EntityID          string            `json:"entity_id"`
	Name              string            `json:"name,omitempty"`
	Kind              string            `json:"kind,omitempty"`
	Repo              string            `json:"repo"`
	SourceFile        string            `json:"source_file,omitempty"`
	StartLine         int               `json:"start_line,omitempty"`
	Method            string            `json:"method,omitempty"`
	Path              string            `json:"path,omitempty"`
	MatchedDefinition string            `json:"matched_definition,omitempty"`
	OrphanHint        string            `json:"orphan_hint,omitempty"`
	Properties        map[string]string `json:"properties,omitempty"`
}

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
	// #1703: "topic" is a caller-facing umbrella that covers all messaging-channel
	// kinds.  search_entities(kind_filter="topic") must match SCOPE.Queue,
	// SCOPE.Topic, Queue, Topic, and their dot-suffixed variants so the returned
	// entity_ids can be passed to topic_detail without "found:false".
	"topic": {
		"topic",
		"scope.topic",
		"queue",
		"scope.queue",
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
// archigraph_endpoints — action-dispatch bundle (#1281)
// Replaces: endpoint_definitions, endpoint_calls, endpoint_stats
// ---------------------------------------------------------------------------

// handleEndpoints dispatches on action= to the appropriate endpoint handler.
func (s *Server) handleEndpoints(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "definitions":
		return s.handleEndpointDefinitions(ctx, req)
	case "calls":
		return s.handleEndpointCalls(ctx, req)
	case "stats":
		return s.handleEndpointStats(ctx, req)
	default:
		return mcpapi.NewToolResultError(
			"unknown action " + action + " (allowed: definitions, calls, stats)",
		), nil
	}
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_definitions
// ---------------------------------------------------------------------------

// handleEndpointDefinitions lists http_endpoint_definition entities (and the
// legacy http_endpoint kind when Sub-A has not yet landed). This tool returns
// ONLY definition-side entries — no call-sites.
//
// #1650 overhaul:
//   - server-side path_contains + method filters
//   - default terse rendering (one-line entries, no repeated path fields)
//   - limit defaults to 50 and caps the RENDERED size, not just record count
//   - hard byte budget so a single call cannot overflow the harness token cap
//
// #1745: format="terse"|"full" explicit param; triple-path dedupe.
//
// Tool name: archigraph_endpoint_definitions
func (s *Server) handleEndpointDefinitions(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 20)
	pathContains := strings.ToLower(argString(req, "path_contains", ""))
	method := strings.ToUpper(argString(req, "method", ""))
	// format="terse"|"full" is the preferred control; verbose=bool kept for
	// backward compatibility. format takes precedence when set explicitly.
	format := strings.ToLower(argString(req, "format", ""))
	verbose := argBool(req, "verbose", false)
	if format == "full" {
		verbose = true
	} else if format == "terse" {
		verbose = false
	}

	var out []endpointDefItem
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDefinitionKind(e.Kind) {
				continue
			}
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			p := e.Properties["path"]
			m := e.Properties["verb"]
			if pathContains != "" && !strings.Contains(strings.ToLower(p), pathContains) {
				continue
			}
			if method != "" && !strings.EqualFold(m, method) {
				continue
			}
			it := endpointDefItem{
				EntityID:   prefixedID(r.Repo, e.ID),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				Method:     m,
				Path:       p,
			}
			if verbose {
				it.Kind = e.Kind
				// Triple-path dedupe (#1745): suppress Name when it duplicates
				// the Method+Path combination already expressed by top-level fields.
				if !isRedundantName(e.Name, m, p) {
					it.Name = e.Name
				}
				// Strip path/verb from Properties — already on top-level fields.
				it.Properties = dedupeEndpointProperties(e.Properties)
			}
			out = append(out, it)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	total := len(out)
	offset := argInt(req, "offset", 0)
	out = pageSlice(out, offset, limit)

	// Token-budget guard: shed entries from the tail until under budget.
	// default 800 tokens ≈ 3,200 bytes; hard ceiling 64 KB.
	tokenBudget := argInt(req, "token_budget", 800)
	if tokenBudget < 100 {
		tokenBudget = 100
	}
	budgetBytes := tokenBudget * 4
	if budgetBytes > 64*1024 {
		budgetBytes = 64 * 1024
	}
	preCapLen := len(out)
	out = capByRenderedBytes(out, budgetBytes, !verbose)

	resp := map[string]any{
		"definitions":   out,
		"count":         len(out),
		"total":         total,
		"offset":        offset,
		"truncated":     offset+len(out) < total,
		"format":        formatLabel(verbose),
		"path_contains": pathContains,
		"method":        method,
		"token_budget":  tokenBudget,
		"note":          "format=terse (default) returns one-line 'lines' entries. Use path_contains/method to narrow; limit/token_budget cap rendered size.",
	}
	if preCapLen > len(out) {
		resp["truncation_note"] = fmt.Sprintf(
			"response capped at token_budget=%d (~%d bytes); %d definitions omitted — use path_contains/method to narrow or pass a larger token_budget",
			tokenBudget, budgetBytes, preCapLen-len(out),
		)
	}
	if !verbose {
		resp["lines"] = renderTerseDefinitions(out)
	}
	return jsonResult(resp), nil
}

// terseLine is the minimal struct each terse renderer feeds into for
// homogeneous handling. Both definitions and calls map their items onto it.
type terseLine struct {
	Method     string
	Path       string
	SourceFile string
	StartLine  int
	Repo       string
	Name       string // for calls: caller symbol name
}

func renderTerseLines(lines []terseLine) []string {
	out := make([]string, 0, len(lines))
	for _, it := range lines {
		var b strings.Builder
		if it.Method != "" {
			b.WriteString(it.Method)
			b.WriteString(" ")
		}
		if it.Path != "" {
			b.WriteString(it.Path)
		}
		if it.Name != "" {
			b.WriteString("  → ")
			b.WriteString(it.Name)
		}
		if it.SourceFile != "" {
			b.WriteString("  ")
			b.WriteString(it.SourceFile)
			if it.StartLine > 0 {
				b.WriteString(":")
				b.WriteString(strconv.Itoa(it.StartLine))
			}
		}
		if it.Repo != "" {
			b.WriteString("  (")
			b.WriteString(it.Repo)
			b.WriteString(")")
		}
		out = append(out, b.String())
	}
	return out
}

// renderTerseDefinitions adapts the definition item slice for renderTerseLines.
func renderTerseDefinitions(items []endpointDefItem) []string {
	lines := make([]terseLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, terseLine{
			Method:     it.Method,
			Path:       it.Path,
			SourceFile: it.SourceFile,
			StartLine:  it.StartLine,
			Repo:       it.Repo,
			Name:       it.Name,
		})
	}
	return renderTerseLines(lines)
}

// rendered/capByRenderedBytes operate on item via its terse-line size; we use
// JSON-marshal of the slice as a proxy for token cost.
func capByRenderedBytes[T any](items []T, maxBytes int, _ bool) []T {
	if maxBytes <= 0 {
		return items
	}
	data, err := json.Marshal(items)
	if err != nil || len(data) <= maxBytes {
		return items
	}
	// Binary search the largest prefix that fits.
	lo, hi := 0, len(items)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		data, _ := json.Marshal(items[:mid])
		if len(data) <= maxBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return items[:lo]
}

// ---------------------------------------------------------------------------
// #1745 helpers — triple-path dedupe + format label
// ---------------------------------------------------------------------------

// isRedundantName reports whether a raw entity Name duplicates the information
// already expressed by the Method and Path top-level fields.
//
// Redundant patterns (case-insensitive):
//
//	Name == path
//	Name == "VERB path"                   (common extractor output)
//	Name == "VERB path (…)"               (with trailing annotation)
//	Name == "VERB path → HandlerName"     (with arrow suffix)
func isRedundantName(name, method, path string) bool {
	if name == "" || path == "" {
		return false
	}
	nameLow := strings.ToLower(strings.TrimSpace(name))
	pathLow := strings.ToLower(strings.TrimSpace(path))
	if nameLow == pathLow {
		return true
	}
	if method != "" {
		verbPath := strings.ToLower(method) + " " + pathLow
		if nameLow == verbPath {
			return true
		}
		if strings.HasPrefix(nameLow, verbPath+" (") {
			return true
		}
		if strings.HasPrefix(nameLow, verbPath+" →") {
			return true
		}
	}
	return false
}

// dedupeEndpointProperties returns a copy of props with "path" and "verb"
// removed — they are already promoted to top-level Path/Method fields.
func dedupeEndpointProperties(props map[string]string) map[string]string {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]string, len(props))
	for k, v := range props {
		switch strings.ToLower(k) {
		case "path", "verb":
			// already on top-level fields — skip
		default:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// formatLabel returns the canonical format string for response metadata.
func formatLabel(verbose bool) string {
	if verbose {
		return "full"
	}
	return "terse"
}

// ---------------------------------------------------------------------------
// archigraph_endpoint_calls
// ---------------------------------------------------------------------------

// handleEndpointCalls lists http_endpoint_call entities — call-sites that
// invoke an HTTP endpoint (i.e. the FETCHES-edge source entities). For each
// call-site that has no matching definition anywhere in the group, a reasoning
// hint is included.
//
// #1745: format="terse"|"full" explicit param; triple-path dedupe.
//
// Tool name: archigraph_endpoint_calls
func (s *Server) handleEndpointCalls(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 20)
	orphanOnly := argBool(req, "orphan_only", false)
	pathContains := strings.ToLower(argString(req, "path_contains", ""))
	method := strings.ToUpper(argString(req, "method", ""))
	format := strings.ToLower(argString(req, "format", ""))
	verbose := argBool(req, "verbose", false)
	if format == "full" {
		verbose = true
	} else if format == "terse" {
		verbose = false
	}

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

	// Cross-repo link resolution (#1650 follow-up to iter2 #1615): a call may
	// be matched by a cross-repo link entry (lg.Links) instead of an in-repo
	// FETCHES target. Build a quick set of sources covered by links so we
	// don't flag them as orphans.
	linkedSources := map[string]bool{}
	for _, l := range lg.Links {
		linkedSources[l.Source] = true
	}

	var out []endpointCallItem
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
			p := e.Properties["path"]
			m := e.Properties["verb"]
			if pathContains != "" && !strings.Contains(strings.ToLower(p), pathContains) {
				continue
			}
			if method != "" && !strings.EqualFold(m, method) {
				continue
			}

			eid := prefixedID(r.Repo, e.ID)

			// Determine if this call-site has a matched definition.
			matched := ""
			orphanHint := ""
			if fe, ok := callerToTarget[eid]; ok {
				if definitionIDs[fe.toID] || definitionIDs[prefixedID(r.Repo, fe.toID)] {
					matched = fe.toID
				} else if linkedSources[eid] {
					// Resolved via cross-repo links pass.
					matched = "cross_repo_link"
				} else {
					urlPattern := fe.path
					if urlPattern == "" {
						urlPattern = p
					}
					if urlPattern != "" {
						orphanHint = "this call to " + urlPattern + " has no matching definition — see orphan_callers"
					} else {
						orphanHint = "this call has no matching definition — see orphan_callers"
					}
				}
			} else if linkedSources[eid] {
				matched = "cross_repo_link"
			} else {
				if p != "" {
					orphanHint = "this call to " + p + " has no matching definition — see orphan_callers"
				}
			}

			if orphanOnly && orphanHint == "" {
				continue
			}

			it := endpointCallItem{
				EntityID:          eid,
				Repo:              r.Repo,
				SourceFile:        e.SourceFile,
				StartLine:         e.StartLine,
				Method:            m,
				Path:              p,
				MatchedDefinition: matched,
				OrphanHint:        orphanHint,
			}
			if verbose {
				it.Kind = e.Kind
				// Triple-path dedupe: suppress Name when it duplicates Method+Path.
				if !isRedundantName(e.Name, m, p) {
					it.Name = e.Name
				}
				it.Properties = dedupeEndpointProperties(e.Properties)
			}
			out = append(out, it)
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
	offset := argInt(req, "offset", 0)
	out = pageSlice(out, offset, limit)
	// Token-budget guard: shed entries from the tail until under budget.
	tokenBudget := argInt(req, "token_budget", 800)
	if tokenBudget < 100 {
		tokenBudget = 100
	}
	budgetBytes := tokenBudget * 4
	if budgetBytes > 64*1024 {
		budgetBytes = 64 * 1024
	}
	preCapLen := len(out)
	out = capByRenderedBytes(out, budgetBytes, !verbose)
	resp := map[string]any{
		"calls":         out,
		"count":         len(out),
		"total":         total,
		"offset":        offset,
		"truncated":     offset+len(out) < total,
		"format":        formatLabel(verbose),
		"path_contains": pathContains,
		"method":        method,
		"token_budget":  tokenBudget,
		"note":          "format=terse (default) returns one-line 'lines' entries. path_contains/method narrow server-side; cross-repo link matches surface as matched_definition=\"cross_repo_link\".",
	}
	if preCapLen > len(out) {
		resp["truncation_note"] = fmt.Sprintf(
			"response capped at token_budget=%d (~%d bytes); %d calls omitted — use path_contains/method to narrow or pass a larger token_budget",
			tokenBudget, budgetBytes, preCapLen-len(out),
		)
	}
	if !verbose {
		lines := make([]terseLine, 0, len(out))
		for _, it := range out {
			lines = append(lines, terseLine{
				Method:     it.Method,
				Path:       it.Path,
				SourceFile: it.SourceFile,
				StartLine:  it.StartLine,
				Repo:       it.Repo,
			})
		}
		resp["lines"] = renderTerseLines(lines)
	}
	return jsonResult(resp), nil
}

// pageSlice returns the [offset, offset+limit) window of s, clamped to bounds.
// A limit<=0 means "no limit" (return everything from offset onward).
func pageSlice[T any](s []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return s[:0]
	}
	s = s[offset:]
	if limit > 0 && len(s) > limit {
		s = s[:limit]
	}
	return s
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
		Repo              string `json:"repo"`
		Definitions       int    `json:"definitions"`
		Calls             int    `json:"calls"`
		LegacyKind        int    `json:"legacy_kind"` // entities whose kind is plain "http_endpoint" (not split yet)
		OrphanCalls       int    `json:"orphan_calls"`
		CrossRepoResolved int    `json:"cross_repo_resolved"`
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

	// #1650: fold cross-repo link resolutions into orphan accounting. The
	// link pass writes <repo>::<localId> on the source side; collect those so
	// a FETCHES whose ToID is unresolved intra-repo but whose FromID is
	// covered by a cross-repo link is NOT counted as orphan.
	linkedSources := map[string]bool{}
	for _, l := range lg.Links {
		linkedSources[l.Source] = true
	}

	var perRepo []repoStats
	totalDefs, totalCalls, totalLegacy, totalOrphans, totalCross := 0, 0, 0, 0, 0

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

		// Count orphan call-sites: FETCHES edges whose ToID is not a definition
		// AND whose FromID isn't covered by a cross-repo link entry.
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "FETCHES" {
				continue
			}
			resolvedIntra := definitionIDs[rel.ToID] || definitionIDs[prefixedID(r.Repo, rel.ToID)]
			if resolvedIntra {
				continue
			}
			srcPrefixed := prefixedID(r.Repo, rel.FromID)
			if linkedSources[srcPrefixed] {
				rs.CrossRepoResolved++
				continue
			}
			rs.OrphanCalls++
		}

		totalDefs += rs.Definitions
		totalCalls += rs.Calls
		totalLegacy += rs.LegacyKind
		totalOrphans += rs.OrphanCalls
		totalCross += rs.CrossRepoResolved
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
			"definitions":         totalDefs,
			"calls":               totalCalls,
			"legacy_kind":         totalLegacy,
			"orphan_calls":        totalOrphans,
			"cross_repo_resolved": totalCross,
			"cross_repo_links":    len(lg.Links),
		},
		"per_repo": perRepo,
		"migrated": migrated,
		"note":     note,
	}), nil
}
