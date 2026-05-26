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

// ---------------------------------------------------------------------------
// #2313: named constants — eliminate magic string repetition
// ---------------------------------------------------------------------------

// patternTypeHTTPEndpointClientSynthesis is the pattern_type value that marks
// an http_endpoint entity as a generated client stub rather than a server-side
// definition. Repeated across all three endpoint handlers; a single constant
// prevents silent divergence.
const patternTypeHTTPEndpointClientSynthesis = "http_endpoint_client_synthesis"

// kindFETCHES is the edge kind that records a call-site's relationship to the
// endpoint it invokes. Orphan detection in all three handlers checks for this
// edge kind; the constant prevents the five-site magic-string drift. (#2336)
const kindFETCHES = "FETCHES"

// ---------------------------------------------------------------------------
// #2314: typed endpoint-kind classifier — replaces three separate predicate
// functions that each repeated stripScopePrefix + ToLower.
// ---------------------------------------------------------------------------

// endpointKindCategory classifies an entity's kind for endpoint-tool routing.
type endpointKindCategory int

const (
	endpointKindNone       endpointKindCategory = iota // not an HTTP endpoint kind
	endpointKindDefinition                              // server-side handler / route
	endpointKindCall                                    // call-site / FETCHES-edge source
	endpointKindLegacy                                  // plain "http_endpoint" (pre-Sub-A)
)

// classifyEndpointKind returns the category for the given raw kind string.
// The comparison is case-insensitive and scope-prefix-stripped (e.g.
// "SCOPE.http_endpoint_call" → endpointKindCall).
func classifyEndpointKind(kind string) endpointKindCategory {
	k := strings.ToLower(stripScopePrefix(kind))
	switch k {
	case "http_endpoint_definition":
		return endpointKindDefinition
	case "http_endpoint_call":
		return endpointKindCall
	case "http_endpoint":
		return endpointKindLegacy
	default:
		return endpointKindNone
	}
}

// isHTTPEndpointKind reports whether kind is any recognised HTTP-endpoint kind.
func isHTTPEndpointKind(kind string) bool {
	return classifyEndpointKind(kind) != endpointKindNone
}

// isDefinitionKind reports whether kind represents a handler/route definition.
func isDefinitionKind(kind string) bool {
	c := classifyEndpointKind(kind)
	return c == endpointKindDefinition || c == endpointKindLegacy
}

// isCallKind reports whether kind represents a call-site (consumer side).
func isCallKind(kind string) bool {
	return classifyEndpointKind(kind) == endpointKindCall
}

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
// #2336: endpointResolution — shared orphan / linked-source-target / definitionIDs
// helper extracted from the parallel logic in all three endpoint handlers.
// ---------------------------------------------------------------------------

// endpointResolution holds the precomputed lookup structures that all three
// endpoint handlers need for orphan detection and cross-repo link accounting.
// Build it once per handler invocation via newEndpointResolution.
type endpointResolution struct {
	// definitionIDs holds every prefixed AND bare entity ID that classifies as a
	// definition (excluding client-synthesis patterns). Used to determine whether
	// a FETCHES edge target is a known definition.
	definitionIDs map[string]bool

	// linkedSources holds the Source-side IDs from lg.Links — call-sites that
	// are resolved via the cross-repo link pass and must NOT be counted as orphans.
	linkedSources map[string]bool

	// linkedTargets holds the Target-side IDs from lg.Links — definition-side
	// IDs that are reachable from a cross-repo caller and must NOT be counted
	// as orphan definitions. Only populated when orphanOnly=true.
	linkedTargets map[string]bool
}

// newEndpointResolution builds the shared resolution structures for repos using
// the cross-repo links from lg. When orphanOnly is false, linkedTargets is left
// nil (avoids the allocation for callers that don't need it).
func newEndpointResolution(repos []*LoadedRepo, lg *LoadedGroup, orphanOnly bool) endpointResolution {
	defIDs := make(map[string]bool)
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isDefinitionKind(e.Kind) && e.Properties["pattern_type"] != patternTypeHTTPEndpointClientSynthesis {
				defIDs[prefixedID(r.Repo, e.ID)] = true
				defIDs[e.ID] = true
			}
		}
	}

	linkedSrc := make(map[string]bool, len(lg.Links))
	for _, l := range lg.Links {
		linkedSrc[l.Source] = true
	}

	var linkedTgt map[string]bool
	if orphanOnly {
		linkedTgt = make(map[string]bool, len(lg.Links))
		for _, l := range lg.Links {
			if l.Target != "" {
				linkedTgt[l.Target] = true
			}
		}
	}

	return endpointResolution{
		definitionIDs: defIDs,
		linkedSources: linkedSrc,
		linkedTargets: linkedTgt,
	}
}

// isOrphanDefinition reports whether the endpoint-definition entity with the
// given local ID (within repo r) has no inbound client-call edges. An endpoint
// is orphan when:
//
//   - it has no inbound FETCHES edge in its own repo (the semantic
//     "client → endpoint" edge in this graph; see handleEndpointCalls), AND
//   - it is not the target of any cross-repo Link in lg.Links.
//
// Other inbound edge kinds (CONTAINS, DECLARES, …) are intentionally ignored —
// they describe structure, not API consumption. (#2292)
//
// res.linkedTargets must have been populated (orphanOnly=true passed to
// newEndpointResolution); passing a resolution with nil linkedTargets is
// allowed and treated as "no cross-repo callers".
func isOrphanDefinition(r *LoadedRepo, localID string, res endpointResolution) bool {
	prefixed := prefixedID(r.Repo, localID)
	if res.linkedTargets[prefixed] || res.linkedTargets[localID] {
		return false
	}
	for _, e := range r.Adjacency.Incoming(localID) {
		if strings.EqualFold(e.kind, kindFETCHES) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// #2315: respondPaginated — generic paged response helper used by both
// handleEndpointDefinitions and handleEndpointCalls. Avoids ~80 lines of
// copy-paste (token budget, pageSlice, sort callback, response envelope).
//
// #2344: refactored to accept PaginationOpts instead of mcpapi.CallToolRequest
// directly. Callers build a PaginationOpts from req; this decouples the helper
// from the MCP request shape and makes it trivially testable in isolation.
// ---------------------------------------------------------------------------

// PaginationOpts carries the pagination and filter parameters extracted from an
// MCP CallToolRequest. Callers construct this from req before calling
// respondPaginated, keeping the helper free of MCP-API coupling.
type PaginationOpts struct {
	Offset       int
	Limit        int
	TokenBudget  int
	Verbose      bool
	PathContains string
	Method       string
}

// Format returns the canonical format label ("full" or "terse") for this opts.
func (o PaginationOpts) Format() string { return formatLabel(o.Verbose) }

// FromRequest builds a fully-populated PaginationOpts from an MCP
// CallToolRequest. Format-precedence resolution is internalised here so callers
// need not thread verbose/pathContains/method as explicit params:
//
//   - format="full"  → Verbose=true  (takes precedence over verbose bool)
//   - format="terse" → Verbose=false (takes precedence over verbose bool)
//   - format unset   → Verbose=argBool(req,"verbose",false)
//   - path_contains and method are read and normalised (lower/upper) here.
func (PaginationOpts) FromRequest(req mcpapi.CallToolRequest) PaginationOpts {
	format := strings.ToLower(argString(req, "format", ""))
	verbose := argBool(req, "verbose", false)
	if format == "full" {
		verbose = true
	} else if format == "terse" {
		verbose = false
	}
	return PaginationOpts{
		Offset:       argInt(req, "offset", 0),
		Limit:        argInt(req, "limit", 20),
		TokenBudget:  argInt(req, "token_budget", 800),
		Verbose:      verbose,
		PathContains: strings.ToLower(argString(req, "path_contains", "")),
		Method:       strings.ToUpper(argString(req, "method", "")),
	}
}

// paginatedResponse is the wire shape returned by respondPaginated. The
// handler fills in the mode-specific payload keys (definitions / calls / lines)
// before serialising.
type paginatedResponse struct {
	Count        int    `json:"count"`
	Total        int    `json:"total"`
	Offset       int    `json:"offset"`
	Truncated    bool   `json:"truncated"`
	Format       string `json:"format"`
	PathContains string `json:"path_contains"`
	Method       string `json:"method"`
	TokenBudget  int    `json:"token_budget"`
}

// respondPaginated applies the standard token-budget + page-slice pipeline to
// items, returning the trimmed slice and a populated paginatedResponse envelope.
// The caller is responsible for marshalling the slice into the appropriate
// response key ("definitions", "calls", or "lines").
//
// #2344: opts replaces the raw mcpapi.CallToolRequest parameter, decoupling
// this helper from the MCP wire type. Build opts via paginationOptsFromReq.
func respondPaginated[T any](
	opts PaginationOpts,
	items []T,
	total int,
) ([]T, paginatedResponse, string) {
	tokenBudget := opts.TokenBudget
	if tokenBudget < 100 {
		tokenBudget = 100
	}
	budgetBytes := tokenBudget * 4
	if budgetBytes > 64*1024 {
		budgetBytes = 64 * 1024
	}

	paged := pageSlice(items, opts.Offset, opts.Limit)
	preCapLen := len(paged)
	paged = capByRenderedBytes(paged, budgetBytes, !opts.Verbose)

	env := paginatedResponse{
		Count:        len(paged),
		Total:        total,
		Offset:       opts.Offset,
		Truncated:    opts.Offset+len(paged) < total,
		Format:       formatLabel(opts.Verbose),
		PathContains: opts.PathContains,
		Method:       opts.Method,
		TokenBudget:  tokenBudget,
	}

	truncationNote := ""
	if preCapLen > len(paged) {
		truncationNote = fmt.Sprintf(
			"response capped at token_budget=%d (~%d bytes); %d items omitted — use path_contains/method to narrow or pass a larger token_budget",
			tokenBudget, budgetBytes, preCapLen-len(paged),
		)
	}
	return paged, env, truncationNote
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
// #2288: terse mode emits lines only (no definitions struct array duplication).
//
// Tool name: archigraph_endpoint_definitions
func (s *Server) handleEndpointDefinitions(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	pOpts := PaginationOpts{}.FromRequest(req)
	pathContains := pOpts.PathContains
	method := pOpts.Method
	verbose := pOpts.Verbose
	orphanOnly := argBool(req, "orphan_only", false)

	// #2292: orphan_only=true filters to endpoint definitions with no inbound
	// client-call edges. In this graph the edge kind from a call-site to its
	// definition is FETCHES (see handleEndpointCalls / handleEndpointStats);
	// "CALLS" in the issue text refers to that semantic edge — the literal
	// graph kind is FETCHES. Other inbound edge kinds (CONTAINS, DECLARES, …)
	// do NOT count, so a route nested inside a router with a CONTAINS edge but
	// no FETCHES caller is still an orphan from the API-call perspective.
	//
	// Cross-repo callers: if the definition is the target of an entry in
	// lg.Links (which records cross-repo HTTP link resolutions), it is also
	// NOT orphan, matching the accounting in handleEndpointStats.
	//
	// #2336: use shared endpointResolution helper (orphanOnly=true populates
	// linkedTargets; false skips the allocation).
	res := newEndpointResolution(repos, lg, orphanOnly)

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
			if e.Properties["pattern_type"] == patternTypeHTTPEndpointClientSynthesis {
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
			if orphanOnly && !isOrphanDefinition(r, e.ID, res) {
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

	// #2315: use respondPaginated helper (token budget + page + cap).
	// #2360: pOpts already built via PaginationOpts{}.FromRequest(req) above.
	out, env, truncationNote := respondPaginated(pOpts, out, total)

	resp := map[string]any{
		"count":         env.Count,
		"total":         env.Total,
		"offset":        env.Offset,
		"truncated":     env.Truncated,
		"format":        env.Format,
		"path_contains": env.PathContains,
		"method":        env.Method,
		"orphan_only":   orphanOnly,
		"token_budget":  env.TokenBudget,
		// #2317: "note" field removed — schema lives in the tool description,
		// not in runtime responses (reduces wire bytes on every call).
	}
	if verbose {
		resp["definitions"] = out
	} else {
		resp["lines"] = renderTerseDefinitions(out)
	}
	if truncationNote != "" {
		resp["truncation_note"] = truncationNote
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
// #2311: terse mode (default) emits lines only — mirrors the #2288/#2309 fix
// for handleEndpointDefinitions. The `calls` struct array is only present when
// format=full or verbose=true.
//
// Tool name: archigraph_endpoint_calls
func (s *Server) handleEndpointCalls(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	pOpts := PaginationOpts{}.FromRequest(req)
	orphanOnly := argBool(req, "orphan_only", false)
	pathContains := pOpts.PathContains
	method := pOpts.Method
	verbose := pOpts.Verbose

	// #2336: use shared endpointResolution helper.
	// orphanOnly=false → linkedTargets not populated (not needed for call handler).
	res := newEndpointResolution(repos, lg, false)

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
			if rel.Kind != kindFETCHES {
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

	var out []endpointCallItem
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			// Accept explicit call kind OR client-synthesis http_endpoint.
			isCall := isCallKind(e.Kind) ||
				(isDefinitionKind(e.Kind) && e.Properties["pattern_type"] == patternTypeHTTPEndpointClientSynthesis)
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
				if res.definitionIDs[fe.toID] || res.definitionIDs[prefixedID(r.Repo, fe.toID)] {
					matched = fe.toID
				} else if res.linkedSources[eid] {
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
			} else if res.linkedSources[eid] {
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

	// #2315: use respondPaginated helper (token budget + page + cap).
	// #2360: pOpts already built via PaginationOpts{}.FromRequest(req) above.
	out, env, truncationNote := respondPaginated(pOpts, out, total)

	// #2311: mirror the #2288/#2309 fix — terse mode (default) emits lines
	// only. The `calls` struct array is only present in format=full / verbose=true.
	// #2317: "note" field dropped from runtime response.
	resp := map[string]any{
		"count":         env.Count,
		"total":         env.Total,
		"offset":        env.Offset,
		"truncated":     env.Truncated,
		"format":        env.Format,
		"path_contains": env.PathContains,
		"method":        env.Method,
		"token_budget":  env.TokenBudget,
	}
	if verbose {
		resp["calls"] = out
	} else {
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
	if truncationNote != "" {
		resp["truncation_note"] = truncationNote
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

	// #2336: use shared endpointResolution helper.
	res := newEndpointResolution(repos, lg, false)

	var perRepo []repoStats
	totalDefs, totalCalls, totalLegacy, totalOrphans, totalCross := 0, 0, 0, 0, 0

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		rs := repoStats{Repo: r.Repo}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			switch classifyEndpointKind(e.Kind) {
			case endpointKindDefinition:
				rs.Definitions++
			case endpointKindCall:
				rs.Calls++
			case endpointKindLegacy:
				// Pre-Sub-A entity; count separately.
				rs.LegacyKind++
				if e.Properties["pattern_type"] == patternTypeHTTPEndpointClientSynthesis {
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
			if rel.Kind != kindFETCHES {
				continue
			}
			resolvedIntra := res.definitionIDs[rel.ToID] || res.definitionIDs[prefixedID(r.Repo, rel.ToID)]
			if resolvedIntra {
				continue
			}
			srcPrefixed := prefixedID(r.Repo, rel.FromID)
			if res.linkedSources[srcPrefixed] {
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
	// migration_note is non-empty only when legacy http_endpoint entities are
	// still present in the graph (i.e. the Sub-A (#1217) indexer pass has not
	// run yet). This is intentionally distinct from the "note" fields removed
	// from handleEndpointDefinitions and handleEndpointCalls in #2317 — those
	// were static schema prose. This field is a dynamic migration-hint that is
	// only emitted when the graph is in a transitional state, so it carries its
	// own weight in the response and is named accordingly.
	migrationNote := ""
	if !migrated {
		migrationNote = "graph still contains legacy http_endpoint kind — run the indexer after Sub-A (#1217) lands to split into http_endpoint_definition / http_endpoint_call"
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
		"per_repo":        perRepo,
		"migrated":        migrated,
		"migration_note":  migrationNote,
	}), nil
}
