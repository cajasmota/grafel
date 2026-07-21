// Phase 3 MCP tools (#2774).
//
// Four new tools, each reading the sidecar JSON written by the
// corresponding link pass in internal/links/:
//
//   - grafel_pure_functions      ← <group>-links-pure-functions.json
//   - grafel_module_cycles       ← <group>-links-module-cycles.json
//   - grafel_def_use             ← <group>-links-def-use.json
//   - grafel_template_patterns   ← <group>-links-template-patterns.json
//
// All four handlers share the same shape: load the sidecar for the
// resolved group, project into the wire format, apply optional filters,
// cap to a limit. When the sidecar is missing the handler returns an
// empty result with `source="missing"` and a note explaining the user
// must re-run the link passes (mirrors the dead-code tool's contract
// when reachability has never been computed).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// sidecarPath returns the canonical on-disk path for a Phase 3 sidecar
// of the given suffix (e.g. "pure-functions"). Mirrors
// reachabilitySidecarPath in dead_code.go.
func sidecarPath(group, suffix string) string {
	// Prefer $HOME so tests using t.Setenv("HOME", tmpDir) resolve the same
	// sidecar location on every OS — on Windows os.UserHomeDir() reads
	// USERPROFILE and ignores HOME, so a HOME-only test would write to a dir
	// the tool never reads, making the sidecar look "missing" (which then
	// cascades into the nil interface-conversion panic downstream).
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(home, ".grafel", "groups", group+"-links-"+suffix+".json")
}

// loadSidecar reads + json-decodes the sidecar at path into v; ok=false
// on any I/O or decode error (missing file is the common case).
func loadSidecar(path string, v any) bool {
	if path == "" {
		return false
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(buf, v) == nil
}

// ---------------------------------------------------------------------
// 3A — grafel_pure_functions
// ---------------------------------------------------------------------

type pureSidecarEntry struct {
	Repo       string  `json:"repo"`
	EntityID   string  `json:"entity_id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	SourceFile string  `json:"source_file,omitempty"`
	Confidence float64 `json:"confidence"`
}

type pureSidecarDoc struct {
	Version int                `json:"version"`
	Method  string             `json:"method"`
	Total   int                `json:"total"`
	Entries []pureSidecarEntry `json:"entries"`
}

func (s *Server) handlePureFunctions(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	limit := argInt(req, "limit", 200)

	var doc pureSidecarDoc
	path := sidecarPath(groupName, "pure-functions")
	if !loadSidecar(path, &doc) {
		return jsonResult(map[string]any{
			"pure_functions": []any{},
			"count":          0,
			"total":          0,
			"source":         "missing",
			"note":           "Pure-function sidecar absent — run the link passes to generate it (#2774 Phase 3A).",
		}), nil
	}
	out := make([]map[string]any, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		if len(repoFilter) > 0 && !repoFilter[e.Repo] {
			continue
		}
		out = append(out, map[string]any{
			"entity_id":   prefixedID(e.Repo, e.EntityID),
			"name":        e.Name,
			"kind":        e.Kind,
			"repo":        e.Repo,
			"source_file": e.SourceFile,
			"confidence":  e.Confidence,
		})
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"pure_functions": out,
		"count":          len(out),
		"total":          total,
		"truncated":      total > len(out),
		"source":         "sidecar",
		"note": "Function-like entities with no detected sinks per Phase 1A effect classification. " +
			"Confidence is low (0.30) — absence of detection does not prove absence of effect (#2774 Phase 3A).",
	}), nil
}

// ---------------------------------------------------------------------
// 3B — grafel_module_cycles
// ---------------------------------------------------------------------

type moduleCycleSidecarMember struct {
	Repo       string `json:"repo"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file,omitempty"`
}

type moduleCycleSidecar struct {
	ID      int                        `json:"id"`
	Size    int                        `json:"size"`
	Members []moduleCycleSidecarMember `json:"members"`
}

type moduleCycleSidecarDoc struct {
	Version    int                  `json:"version"`
	Method     string               `json:"method"`
	TotalNodes int                  `json:"total_nodes"`
	TotalEdges int                  `json:"total_edges"`
	Cycles     []moduleCycleSidecar `json:"cycles"`
}

// handleModuleCyclesSidecar is the Phase 3B SCC reader. The pre-existing
// handleModuleCycles in module_gds_tools.go computes SCCs on demand from
// the in-memory graph and is bundled under grafel_module_analysis;
// this handler reads the persistent sidecar emitted by the new Phase 3B
// link pass, so callers get the same SCC view without paying the
// recompute cost on each call and entities carry a stamped
// `module_cycle_id` property usable from any other query.
func (s *Server) handleModuleCyclesSidecar(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	minSize := argInt(req, "min_size", 2)
	limit := argInt(req, "limit", 100)

	var doc moduleCycleSidecarDoc
	path := sidecarPath(groupName, "module-cycles")
	if !loadSidecar(path, &doc) {
		return jsonResult(map[string]any{
			"cycles": []any{},
			"count":  0,
			"source": "missing",
			"note":   "Module-cycle sidecar absent — run the link passes to generate it (#2774 Phase 3B).",
		}), nil
	}
	out := make([]map[string]any, 0, len(doc.Cycles))
	for _, c := range doc.Cycles {
		if c.Size < minSize {
			continue
		}
		members := make([]map[string]any, 0, len(c.Members))
		keep := false
		for _, m := range c.Members {
			if len(repoFilter) == 0 || repoFilter[m.Repo] {
				keep = true
			}
			members = append(members, map[string]any{
				"entity_id":   prefixedID(m.Repo, m.EntityID),
				"name":        m.Name,
				"kind":        m.Kind,
				"repo":        m.Repo,
				"source_file": m.SourceFile,
			})
		}
		if !keep {
			continue
		}
		out = append(out, map[string]any{
			"id":      c.ID,
			"size":    c.Size,
			"members": members,
		})
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"cycles":      out,
		"count":       len(out),
		"total":       total,
		"truncated":   total > len(out),
		"total_nodes": doc.TotalNodes,
		"total_edges": doc.TotalEdges,
		"source":      "sidecar",
		"note": "Strongly-connected components over IMPORTS edges (Tarjan SCC, size >= min_size). " +
			"Per-repo only in Phase 3B; cross-repo IMPORTS cycles are out of scope (#2774).",
	}), nil
}

// ---------------------------------------------------------------------
// 3C — grafel_def_use
// ---------------------------------------------------------------------

type defUseChainSidecar struct {
	Var     string `json:"var"`
	DefLine int    `json:"def_line"`
	UseLine int    `json:"use_line"`
}

type defUseSidecarEntry struct {
	Repo       string               `json:"repo"`
	EntityID   string               `json:"entity_id"`
	Name       string               `json:"name"`
	SourceFile string               `json:"source_file,omitempty"`
	Chains     []defUseChainSidecar `json:"chains"`
}

type defUseSidecarDoc struct {
	Version int                  `json:"version"`
	Method  string               `json:"method"`
	Total   int                  `json:"total_chains"`
	Entries []defUseSidecarEntry `json:"entries"`
}

func (s *Server) handleDefUse(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	entityFilter := strings.TrimSpace(argString(req, "entity_id", ""))
	limit := argInt(req, "limit", 50)

	// #3834 (MRO T4): an inherited-member STUB has no body, so it owns NO
	// def-use chains — a def_use query on it would dead-end empty. Resolve the
	// stub to its in-repo DEFINING member and ALSO accept that member's id, so
	// def_use on `ChildService.handle` returns the base method's reaching-def
	// chains. mroDefiningID carries the resolved defining id (local) so matched
	// entries can be tagged resolved_via_inherits. External (pack) members have
	// no in-repo body and thus no def-use chains — honest dead-end, no retarget.
	mroDefiningID, mroDefiningClass := defUseMRORetarget(lg, entityFilter)

	var doc defUseSidecarDoc
	path := sidecarPath(groupName, "def-use")
	if !loadSidecar(path, &doc) {
		return jsonResult(map[string]any{
			"entries": []any{},
			"count":   0,
			"total":   0,
			"source":  "missing",
			"note":    "Def-use sidecar absent — run the link passes to generate it (#2774 Phase 3C).",
		}), nil
	}

	out := make([]map[string]any, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		if len(repoFilter) > 0 && !repoFilter[e.Repo] {
			continue
		}
		matchedViaInherits := false
		if entityFilter != "" {
			eid := prefixedID(e.Repo, e.EntityID)
			direct := eid == entityFilter || e.EntityID == entityFilter
			viaMRO := mroDefiningID != "" && e.EntityID == mroDefiningID
			if !direct && !viaMRO {
				continue
			}
			matchedViaInherits = viaMRO && !direct
		}
		chains := make([]map[string]any, len(e.Chains))
		for i, c := range e.Chains {
			chains[i] = map[string]any{
				"var":      c.Var,
				"def_line": c.DefLine,
				"use_line": c.UseLine,
			}
		}
		entry := map[string]any{
			"entity_id":   prefixedID(e.Repo, e.EntityID),
			"name":        e.Name,
			"repo":        e.Repo,
			"source_file": e.SourceFile,
			"chain_count": len(e.Chains),
			"chains":      chains,
		}
		if matchedViaInherits {
			// The chains belong to the base/mixin DEFINING member, surfaced
			// because the queried entity inherits it (#3834). Mark it so the
			// consumer knows these are the inherited member's chains, not the
			// stub's own (the stub has none).
			entry["resolved_via_inherits"] = true
			entry["queried_entity_id"] = entityFilter
			if mroDefiningClass != "" {
				entry["defining_class"] = mroDefiningClass
			}
		}
		out = append(out, entry)
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"entries":      out,
		"count":        len(out),
		"total":        total,
		"truncated":    total > len(out),
		"total_chains": doc.Total,
		"source":       "sidecar",
		"note": "Intra-procedural reaching-definitions (last-write-wins). " +
			"Inter-procedural / SSA-phi precision is out of Phase 3C scope (#2774).",
	}), nil
}

// ---------------------------------------------------------------------
// #3867 — grafel_data_flows
// ---------------------------------------------------------------------
//
// Surfaces the request-input → sink DATA_FLOWS_TO edges computed by the
// data-flow link pass (internal/links/dataflow_pass.go). Before #3867 those
// edges lived ONLY in the sidecar with no reader, so `neighbors
// fields:[data_flows_to]` returned nothing. The pass now also emits them into
// the main links document (so they ride the cross-repo/overlay edge surface),
// and this tool projects the sidecar so a caller gets the full per-flow
// provenance — the tainted request `field`, the `sink_kind`, the resolved
// `sink`, and the inter-procedural `hop_path` — that a bare edge-kind filter
// cannot carry.

// dataFlowLinkSidecar mirrors the subset of internal/links.Link the data-flow
// sidecar persists (it serialises the full Link, we read what we project).
type dataFlowLinkSidecar struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Relation   string            `json:"relation"`
	Method     string            `json:"method"`
	Confidence float64           `json:"confidence"`
	Properties map[string]string `json:"properties,omitempty"`
}

type dataFlowSidecarDoc struct {
	Version int                   `json:"version"`
	Method  string                `json:"method"`
	Total   int                   `json:"total_flows"`
	Links   []dataFlowLinkSidecar `json:"links"`
}

// dataFlowEndpointRepo extracts the "<repo>" prefix from a links-pass
// "<repo>::<localId>" endpoint key. Synthetic `sink:` residues (no repo
// prefix) yield "" — they are kept but never repo-filtered out.
func dataFlowEndpointRepo(key string) string {
	if i := strings.Index(key, "::"); i > 0 {
		return key[:i]
	}
	return ""
}

// dbSinkEdgeKinds is the set of live-graph DB-access relationship kinds that
// represent a node reaching a database sink. handleDataFlows projects edges of
// these kinds as `sink_kind=db` data flows so that DB access surfaced ONLY as a
// graph edge — never picked up by the request-input→sink taint sniffer that
// fills the data-flow sidecar — still appears under `data_flows(sink_kind=db)`
// (#4299, follow-up to #4288).
//
// The set mirrors the DB-access subset of semanticEdgeKinds (internal/mcp/
// tools.go) — every kind here is a genuine read/write/query/join against a
// table or collection, NOT structural scaffolding and NOT a non-DB semantic
// edge (THROWS, RENDERS, PUBLISHES_TO, …):
//
//	READS_FROM / WRITES_TO         — explicit DB read/write access edges
//	QUERIES                        — ORM query call-site → model/table (#723)
//	ACCESSES_TABLE / MODIFIES_TABLE — relational table read / mutation
//	JOINS_COLLECTION               — Mongo $lookup / cross-collection join (#3426)
//	GRAPH_RELATES                  — Neo4j graph-DB join analogue of the above (#3611)
//
// The map value is the synthesised effect-style `sink_kind` reported for the
// projected flow — db_read for pure reads, db_write for writes/mutations, and
// the join kinds default to db_read (a $lookup/relationship traversal reads the
// joined collection). All match the `db` sink_kind filter (see sinkKindMatchesDB).
var dbSinkEdgeKinds = map[string]string{
	string(types.RelationshipKindReadsFrom):       "db_read",
	string(types.RelationshipKindWritesTo):        "db_write",
	string(types.RelationshipKindQueries):         "db_read",
	string(types.RelationshipKindAccessesTable):   "db_read",
	string(types.RelationshipKindModifiesTable):   "db_write",
	string(types.RelationshipKindJoinsCollection): "db_read",
	string(types.RelationshipKindGraphRelates):    "db_read",
}

// sinkKindMatchesDB reports whether a `sink_kind` filter value should include
// graph-edge-projected DB sinks. An empty filter (no narrowing) and the generic
// "db" both match; the concrete effect kinds db_read / db_write match their own
// projected flows. Any other filter (http, queue, …) excludes them.
func sinkKindMatchesDB(filter, projected string) bool {
	switch filter {
	case "", "db":
		return true
	default:
		return filter == projected
	}
}

func (s *Server) handleDataFlows(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	entityFilter := strings.TrimSpace(argString(req, "entity_id", ""))
	sinkKindFilter := strings.ToLower(strings.TrimSpace(argString(req, "sink_kind", "")))
	limit := argInt(req, "limit", 100)

	var doc dataFlowSidecarDoc
	path := sidecarPath(groupName, "data-flow")
	sidecarPresent := loadSidecar(path, &doc)

	out := make([]map[string]any, 0, len(doc.Links))
	for _, l := range doc.Links {
		srcRepo := dataFlowEndpointRepo(l.Source)
		if len(repoFilter) > 0 && !repoFilter[srcRepo] {
			continue
		}
		if entityFilter != "" && l.Source != entityFilter {
			continue
		}
		props := l.Properties
		if props == nil {
			props = map[string]string{}
		}
		if sinkKindFilter != "" && strings.ToLower(props["sink_kind"]) != sinkKindFilter {
			continue
		}
		rec := map[string]any{
			"id":         l.ID,
			"from":       l.Source,
			"to":         l.Target,
			"relation":   l.Relation,
			"confidence": l.Confidence,
			"field":      props["field"],
			"sink_kind":  props["sink_kind"],
			"sink":       props["sink"],
		}
		if v := props["hop_via"]; v != "" {
			rec["hop_via"] = v
		}
		if v := props["hop_path"]; v != "" {
			rec["hop_path"] = v
		}
		if v := props["hop_count"]; v != "" {
			rec["hop_count"] = v
		}
		out = append(out, rec)
	}

	// #4299: project live-graph DB-access edges (JOINS_COLLECTION and siblings)
	// as db sinks. The taint sidecar only carries request-input→sink flows the
	// sniffer could follow; a node whose only DB signal is a graph edge (e.g. a
	// Mongo $lookup recorded as JOINS_COLLECTION) never appears there. Surface
	// those edges so `data_flows(sink_kind=db)` is consistent with how
	// db_read/db_write sinks are reported and with inspect().semantic_edges.
	// Only runs when the sink_kind filter could include db sinks.
	graphProjected := 0
	if sinkKindMatchesDB(sinkKindFilter, "db_read") || sinkKindMatchesDB(sinkKindFilter, "db_write") {
		for _, r := range reposToConsider(lg, argStringSlice(req, "repo_filter")) {
			if r == nil || r.Doc == nil {
				continue
			}
			repo := r.Doc.Repo
			if len(repoFilter) > 0 && !repoFilter[repo] {
				continue
			}
			byID := r.getByID()
			r.forEachRelationship(func(rel *graph.Relationship) bool {
				sinkKind, ok := dbSinkEdgeKinds[strings.ToUpper(rel.Kind)]
				if !ok {
					return true
				}
				if !sinkKindMatchesDB(sinkKindFilter, sinkKind) {
					return true
				}
				fromID := prefixedID(repo, rel.FromID)
				if entityFilter != "" && fromID != entityFilter {
					return true
				}
				toID := rel.ToID
				if rprefix, _ := splitPrefixed(toID); rprefix == "" {
					// Bare local id (graph ToIDs are local, e.g. "Class:Inspection")
					// — repo-prefix it so the projected `to` matches the sidecar's
					// "<repo>::<localId>" endpoint shape.
					toID = prefixedID(repo, rel.ToID)
				}
				sink := toID
				if tgt := byID[rel.ToID]; tgt != nil && tgt.Name != "" {
					sink = tgt.Name
				}
				rec := map[string]any{
					"id":         rel.ID,
					"from":       fromID,
					"to":         toID,
					"relation":   rel.Kind,
					"confidence": types.EffectiveConfidence(rel.Confidence),
					"sink_kind":  sinkKind,
					"sink":       sink,
					"source":     "graph-edge",
				}
				if v := rel.PropGet("field"); v != "" {
					rec["field"] = v
				}
				out = append(out, rec)
				graphProjected++
				return true
			})
		}
	}

	if !sidecarPresent && graphProjected == 0 {
		return jsonResult(map[string]any{
			"data_flows": []any{},
			"count":      0,
			"total":      0,
			"source":     "missing",
			"note":       "Data-flow sidecar absent and no DB-access graph edges — run the link passes to generate it (#3867).",
		}), nil
	}

	// Stable order: by from-endpoint, then sink, then id.
	sort.SliceStable(out, func(i, j int) bool {
		fi, fj := fmt.Sprint(out[i]["from"]), fmt.Sprint(out[j]["from"])
		if fi != fj {
			return fi < fj
		}
		si, sj := fmt.Sprint(out[i]["sink"]), fmt.Sprint(out[j]["sink"])
		if si != sj {
			return si < sj
		}
		return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"])
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	source := "sidecar"
	switch {
	case sidecarPresent && graphProjected > 0:
		source = "sidecar+graph-edge"
	case !sidecarPresent && graphProjected > 0:
		source = "graph-edge"
	}
	return jsonResult(map[string]any{
		"data_flows": out,
		"count":      len(out),
		"total":      total,
		"truncated":  total > len(out),
		"source":     source,
		"note": "Request-input → sink DATA_FLOWS_TO edges (intra-function + bounded inter-procedural hops). " +
			"`from` is the request handler, `to` the resolved sink entity (or a synthetic sink: residue), " +
			"`field` the tainted request field, `hop_path` the inter-procedural chain. " +
			"Precision-first: a flow the sniffer did not soundly follow is never fabricated (#3867). " +
			"db sinks reached only via live-graph DB-access edges (JOINS_COLLECTION/READS_FROM/WRITES_TO/" +
			"QUERIES/ACCESSES_TABLE/MODIFIES_TABLE/GRAPH_RELATES) are also projected with source=graph-edge (#4299).",
	}), nil
}

// ---------------------------------------------------------------------
// 3D — grafel_template_patterns
// ---------------------------------------------------------------------

type templatePatternSidecarEntry struct {
	Repo       string `json:"repo"`
	SourceFile string `json:"source_file"`
	Function   string `json:"function,omitempty"`
	Line       int    `json:"line"`
	Kind       string `json:"kind"`
	Tag        string `json:"tag"`
	Literal    string `json:"literal"`
}

type templatePatternSidecarDoc struct {
	Version int                           `json:"version"`
	Method  string                        `json:"method"`
	Total   int                           `json:"total"`
	ByKind  map[string]int                `json:"by_kind"`
	Entries []templatePatternSidecarEntry `json:"entries"`
}

func (s *Server) handleTemplatePatterns(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	kindFilter := strings.ToLower(argString(req, "kind", ""))
	limit := argInt(req, "limit", 200)

	var doc templatePatternSidecarDoc
	path := sidecarPath(groupName, "template-patterns")
	if !loadSidecar(path, &doc) {
		return jsonResult(map[string]any{
			"patterns": []any{},
			"count":    0,
			"total":    0,
			"by_kind":  map[string]int{},
			"source":   "missing",
			"note":     "Template-pattern sidecar absent — run the link passes to generate it (#2774 Phase 3D).",
		}), nil
	}
	out := make([]map[string]any, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		if len(repoFilter) > 0 && !repoFilter[e.Repo] {
			continue
		}
		if kindFilter != "" && strings.ToLower(e.Kind) != kindFilter {
			continue
		}
		out = append(out, map[string]any{
			"repo":        e.Repo,
			"source_file": e.SourceFile,
			"function":    e.Function,
			"line":        e.Line,
			"kind":        e.Kind,
			"tag":         e.Tag,
			"literal":     e.Literal,
		})
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	// Stable order: byRepo, byFile, byLine.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := fmt.Sprint(out[i]["repo"]), fmt.Sprint(out[j]["repo"])
		if ri != rj {
			return ri < rj
		}
		fi, fj := fmt.Sprint(out[i]["source_file"]), fmt.Sprint(out[j]["source_file"])
		if fi != fj {
			return fi < fj
		}
		li, _ := out[i]["line"].(int)
		lj, _ := out[j]["line"].(int)
		return li < lj
	})
	return jsonResult(map[string]any{
		"patterns":  out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
		"by_kind":   doc.ByKind,
		"source":    "sidecar",
		"note": "i18n / log_format / sql template literals lifted by per-language sniffers. " +
			"Overlap with Phase 2B SQL-injection is intentional: 2B tracks taint flow into the literal, " +
			"3D catalogues the literal itself (#2774).",
	}), nil
}
