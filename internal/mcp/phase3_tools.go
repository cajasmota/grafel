// Phase 3 MCP tools (#2774).
//
// Four new tools, each reading the sidecar JSON written by the
// corresponding link pass in internal/links/:
//
//   - archigraph_pure_functions      ← <group>-links-pure-functions.json
//   - archigraph_module_cycles       ← <group>-links-module-cycles.json
//   - archigraph_def_use             ← <group>-links-def-use.json
//   - archigraph_template_patterns   ← <group>-links-template-patterns.json
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

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// sidecarPath returns the canonical on-disk path for a Phase 3 sidecar
// of the given suffix (e.g. "pure-functions"). Mirrors
// reachabilitySidecarPath in dead_code.go.
func sidecarPath(group, suffix string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-links-"+suffix+".json")
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
// 3A — archigraph_pure_functions
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
// 3B — archigraph_module_cycles
// ---------------------------------------------------------------------

type moduleCycleSidecarMember struct {
	Repo       string `json:"repo"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file,omitempty"`
}

type moduleCycleSidecar struct {
	ID      int                       `json:"id"`
	Size    int                       `json:"size"`
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
// the in-memory graph and is bundled under archigraph_module_analysis;
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
// 3C — archigraph_def_use
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
	groupName, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	entityFilter := strings.TrimSpace(argString(req, "entity_id", ""))
	limit := argInt(req, "limit", 50)

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
		if entityFilter != "" {
			eid := prefixedID(e.Repo, e.EntityID)
			if eid != entityFilter && e.EntityID != entityFilter {
				continue
			}
		}
		chains := make([]map[string]any, len(e.Chains))
		for i, c := range e.Chains {
			chains[i] = map[string]any{
				"var":      c.Var,
				"def_line": c.DefLine,
				"use_line": c.UseLine,
			}
		}
		out = append(out, map[string]any{
			"entity_id":   prefixedID(e.Repo, e.EntityID),
			"name":        e.Name,
			"repo":        e.Repo,
			"source_file": e.SourceFile,
			"chain_count": len(e.Chains),
			"chains":      chains,
		})
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
// 3D — archigraph_template_patterns
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
