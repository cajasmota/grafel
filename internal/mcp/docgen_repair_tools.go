package mcp

// archigraph_apply_docgen_repairs — MCP tool for the docgen→graph repair
// feedback loop (#1659).
//
// The tool reads docgen-repairs.jsonl from each repo in the resolved group,
// partitions by confidence (≥0.8 → apply immediately, <0.8 → pending queue),
// and returns a per-repo summary with fidelity before/after.
//
// This is the apply-path companion to the skill-side emission described in
// skills/archigraph-tech-docs/SKILL.md § "Docgen Repair Feedback Contract".

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleApplyDocgenRepairs implements archigraph_apply_docgen_repairs.
//
// Parameters (all optional):
//
//	repo_filter []string — restrict to named repos; empty/["*"] = all
//	dry_run bool — when true, report what would be applied but do not write
func (s *Server) handleApplyDocgenRepairs(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	dryRun := argBool(req, "dry_run", false)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type repoResult struct {
		Repo           string                             `json:"repo"`
		Applied        int                                `json:"applied"`
		Queued         int                                `json:"queued"`
		Skipped        int                                `json:"skipped"`
		FidelityBefore *float64                           `json:"fidelity_before,omitempty"`
		FidelityAfter  *float64                           `json:"fidelity_after,omitempty"`
		FidelityDelta  *float64                           `json:"fidelity_delta,omitempty"`
		RepairsApplied []enrichment.DocgenRepairCandidate `json:"repairs_applied,omitempty"`
		RepairsQueued  []enrichment.DocgenRepairCandidate `json:"repairs_queued,omitempty"`
		Error          string                             `json:"error,omitempty"`
	}

	results := make([]repoResult, 0, len(repos))
	totalApplied, totalQueued := 0, 0

	// Sort repos for deterministic output.
	sort.Slice(repos, func(i, j int) bool { return repos[i].Repo < repos[j].Repo })

	for _, r := range repos {
		res := repoResult{Repo: r.Repo}

		stateDir := daemon.StateDirForRepo(r.Path)

		// Count total / bug edges from the loaded document for fidelity calc.
		totalEdges, bugEdges := countEdgesForFidelity(r)

		if dryRun {
			// Preview mode: read candidates but do not write.
			cands, skipped, err := enrichment.ReadDocgenRepairs(stateDir)
			if err != nil {
				res.Error = fmt.Sprintf("read docgen-repairs.jsonl: %v", err)
				results = append(results, res)
				continue
			}
			res.Skipped = skipped
			for _, c := range cands {
				if c.Confidence >= enrichment.HighConfidenceThreshold {
					res.Applied++
					res.RepairsApplied = append(res.RepairsApplied, c)
				} else {
					res.Queued++
					res.RepairsQueued = append(res.RepairsQueued, c)
				}
			}
			if totalEdges > 0 {
				before := 1.0 - float64(bugEdges)/float64(totalEdges)
				res.FidelityBefore = &before
				lift := countEdgeRepairsFromCandidates(res.RepairsApplied)
				adj := bugEdges - lift
				if adj < 0 {
					adj = 0
				}
				after := 1.0 - float64(adj)/float64(totalEdges)
				res.FidelityAfter = &after
				delta := after - before
				res.FidelityDelta = &delta
			}
		} else {
			// Live apply.
			stats, err := enrichment.ApplyDocgenRepairsToResolutions(stateDir, totalEdges, bugEdges)
			if err != nil {
				res.Error = fmt.Sprintf("apply: %v", err)
				results = append(results, res)
				continue
			}
			res.Applied = stats.Applied
			res.Queued = stats.Queued
			res.Skipped = stats.Skipped
			res.FidelityBefore = stats.FidelityBefore
			res.FidelityAfter = stats.FidelityAfter
			res.RepairsApplied = stats.RepairsApplied
			res.RepairsQueued = stats.RepairsQueued
			if stats.FidelityBefore != nil && stats.FidelityAfter != nil {
				delta := *stats.FidelityAfter - *stats.FidelityBefore
				res.FidelityDelta = &delta
			}
		}

		totalApplied += res.Applied
		totalQueued += res.Queued
		results = append(results, res)
	}

	return jsonResult(map[string]any{
		"dry_run":       dryRun,
		"repos":         results,
		"total_applied": totalApplied,
		"total_queued":  totalQueued,
		"threshold":     enrichment.HighConfidenceThreshold,
	}), nil
}

// countEdgesForFidelity returns (totalEdges, bugEdges) from the loaded repo
// document for use in fidelity calculations.
//
// Scope: IMPORTS edges only. This matches the audit.AuditPath scope used by
// appendRebuildHistory to compute health-history.bug_rate, which ensures that
// post-resolver improvements (e.g. ResolveGoInTreeImports rewriting in-tree
// Go package paths to hex entity IDs) are immediately visible in
// archigraph_stats.fidelity. The previous wider scope (CALLS + IMPORTS +
// REFERENCES) masked resolver progress because the much larger CALLS and
// REFERENCES universes diluted the per-improvement signal.
//
// Bug edges are those whose ToID is still a raw stub — not a hex entity ID
// (16 lowercase hex chars) and not an ext:-qualified external reference.
func countEdgesForFidelity(r *LoadedRepo) (total, bugs int) {
	if r == nil || r.Doc == nil {
		return 0, 0
	}
	for i := range r.Doc.Relationships {
		rel := &r.Doc.Relationships[i]
		// Scope: IMPORTS only — matches audit.AuditPath and health-history.bug_rate.
		if rel.Kind != "IMPORTS" {
			continue
		}
		total++
		// A relationship is "buggy" (unresolved) when its ToID still looks like
		// a raw stub — it was not bound to a hex entity ID or ext:-qualified
		// external. The resolver writes these as bug:* prefixes on failed
		// resolution, or the ToID remains a bare name/qname string.
		if isBugEdgeToID(rel.ToID) {
			bugs++
		}
	}
	return total, bugs
}

// isBugEdgeToID returns true for ToID values that represent unresolved stubs.
// Mirrors the heuristic used by internal/resolve (DispositionBugExtractor):
// a hex ID (16 chars) or an ext:-prefixed ID are both resolved; everything
// else is unresolved.
func isBugEdgeToID(toID string) bool {
	if toID == "" {
		return false
	}
	// Resolved: hex entity ID (16 lowercase hex chars).
	if len(toID) == 16 && isHexString(toID) {
		return false
	}
	// Resolved: ext:-qualified external.
	if len(toID) > 4 && toID[:4] == "ext:" {
		return false
	}
	// Everything else is a raw stub → unresolved → bug.
	return true
}

// isHexString returns true if every byte in s is a lowercase hex digit.
func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// countEdgeRepairsFromCandidates returns the number of candidates that
// directly reduce the unresolved-edge count (resolve_ref, add_edge,
// label_external).
func countEdgeRepairsFromCandidates(repairs []enrichment.DocgenRepairCandidate) int {
	n := 0
	for _, r := range repairs {
		switch r.Type {
		case enrichment.DocgenRepairResolveRef,
			enrichment.DocgenRepairAddEdge,
			enrichment.DocgenRepairLabelExternal:
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Unresolved-import breakdown (breakdown="unresolved_imports")
// ---------------------------------------------------------------------------

// UnresolvedBreakdown holds the three supplementary fields returned when
// breakdown="unresolved_imports" is requested on archigraph_stats.
type UnresolvedBreakdown struct {
	ByDisposition map[string]int    `json:"unresolved_imports_by_disposition"`
	ByLanguage    map[string]int    `json:"unresolved_imports_by_language"`
	TopRoots      []importRootEntry `json:"unresolved_imports_top_roots"`
}

// importRootEntry is one row in the top-N import roots table.
type importRootEntry struct {
	Root        string `json:"root"`
	Count       int    `json:"count"`
	Disposition string `json:"disposition"`
}

// unresolvedImportDisposition derives a human-readable disposition label for a
// single unresolved edge using signals available in the serialised graph
// document (ToID shape + Relationship.Properties). It does NOT reproduce the
// full resolver Disposition enum; it maps to a coarser taxonomy that is useful
// for triage:
//
//   - "proto_generated"        — source_module / ToID matches proto/grpc patterns.
//   - "cross_repo"             — source_module contains a "/" (Go-style module
//     path) or "." separated segments that look like an org/repo prefix, and
//     the root is not a stdlib/well-known package.
//   - "same_package_unqualified" — ToID is a bare unqualified name with no
//     separator characters (no ".", "/", ":", "#", " ").
//   - "external_unknown"       — dotted path / package that is not in-project
//     but does not match the above categories.
//   - "other"                  — catch-all.
//
// Limitation: without the resolver's per-import binding the cross_repo /
// external_unknown boundary is heuristic. A dedicated per-edge disposition
// property on the graph schema (tracked as a separate enhancement) would make
// this exact. See discussion in #1837.
func unresolvedImportDisposition(rel *graph.Relationship) string {
	toID := rel.ToID
	srcMod := ""
	if rel.Properties != nil {
		srcMod = rel.Properties["source_module"]
	}

	// Proto/gRPC generated code: proto package paths, protobuf imports,
	// grpc stubs.
	if matchesProto(toID) || matchesProto(srcMod) {
		return "proto_generated"
	}

	// Cross-repo heuristic: a Go-style module path (contains "/") or a
	// multi-segment dotted path whose root segment looks like an org prefix.
	// We use source_module when available (IMPORTS edges) and fall back to
	// ToID (CALLS/REFERENCES edges).
	ref := srcMod
	if ref == "" {
		ref = toID
	}
	if strings.Contains(ref, "/") {
		return "cross_repo"
	}

	// Same-package unqualified: ToID has no separator → bare symbol name.
	if !strings.ContainsAny(toID, "./:# ") && toID != "" {
		return "same_package_unqualified"
	}

	// Dotted paths without "/" are treated as external package references
	// (Python, JS, etc.).
	if strings.Contains(ref, ".") {
		return "external_unknown"
	}

	return "other"
}

// matchesProto returns true when s looks like a proto/gRPC import path.
func matchesProto(s string) bool {
	if s == "" {
		return false
	}
	sl := strings.ToLower(s)
	return strings.Contains(sl, "proto") ||
		strings.Contains(sl, "grpc") ||
		strings.Contains(sl, ".pb.") ||
		strings.HasSuffix(sl, "_pb") ||
		strings.HasSuffix(sl, "_pb2") ||
		strings.HasSuffix(sl, "_grpc")
}

// importRoot extracts the first path segment from an import reference.
// For "opentelemetry.trace" → "opentelemetry".
// For "github.com/org/repo/pkg" → "github.com" (full first slash-segment).
// For a bare name like "myFunc" → "myFunc".
func importRoot(rel *graph.Relationship) string {
	ref := rel.ToID
	if rel.Properties != nil {
		if sm := rel.Properties["source_module"]; sm != "" {
			ref = sm
		}
	}
	if ref == "" {
		return ""
	}
	// slash-first (Go module paths)
	if idx := strings.Index(ref, "/"); idx > 0 {
		return ref[:idx]
	}
	// dot-first (Python, TS, etc.)
	if idx := strings.Index(ref, "."); idx > 0 {
		return ref[:idx]
	}
	return ref
}

// computeUnresolvedBreakdown iterates the unresolved IMPORTS edges across all
// repos in repos and builds the three breakdown maps. repos is the
// already-filtered slice used by handleGraphStats.
//
// Scope: IMPORTS edges only — consistent with countEdgesForFidelity and
// audit.AuditPath. The previous wider scope (CALLS + IMPORTS + REFERENCES)
// meant that the breakdown did not reflect post-resolver state for import
// resolutions (e.g. ResolveGoInTreeImports) because CALLS/REFERENCES stubs
// with unrelated dispositions dominated the counts. Narrowing to IMPORTS only
// ensures the breakdown tracks the same universe as fidelity_import_bug.
//
// The top-roots list is capped at topN (default 10) entries, sorted by count
// descending. Ties are broken alphabetically by root name for stable output.
func computeUnresolvedBreakdown(repos []*LoadedRepo, topN int) UnresolvedBreakdown {
	byDisp := map[string]int{}
	byLang := map[string]int{}
	// rootKey → (count, disposition of the majority)
	type rootStat struct {
		count      int
		dispCounts map[string]int
	}
	roots := map[string]*rootStat{}

	for _, r := range repos {
		if r == nil || r.Doc == nil {
			continue
		}
		byID := r.getByID()
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			// Scope: IMPORTS only — matches countEdgesForFidelity and audit.AuditPath.
			if rel.Kind != "IMPORTS" {
				continue
			}
			if !isBugEdgeToID(rel.ToID) {
				continue
			}

			disp := unresolvedImportDisposition(rel)
			byDisp[disp]++

			// Language: from the source entity when available, otherwise from
			// rel.Properties["language"].
			lang := ""
			if ent := byID[rel.FromID]; ent != nil {
				lang = strings.ToLower(ent.Language)
			}
			if lang == "" && rel.Properties != nil {
				lang = strings.ToLower(rel.Properties["language"])
			}
			if lang == "" {
				lang = "unknown"
			}
			byLang[lang]++

			// Top roots.
			root := importRoot(rel)
			if root == "" {
				root = "(empty)"
			}
			rs := roots[root]
			if rs == nil {
				rs = &rootStat{dispCounts: map[string]int{}}
				roots[root] = rs
			}
			rs.count++
			rs.dispCounts[disp]++
		}
	}

	// Build sorted top-N list.
	type kv struct {
		root  string
		count int
		disp  string
	}
	flat := make([]kv, 0, len(roots))
	for root, rs := range roots {
		// Pick the dominant disposition for this root.
		bestDisp, bestCount := "other", 0
		for d, c := range rs.dispCounts {
			if c > bestCount || (c == bestCount && d < bestDisp) {
				bestDisp = d
				bestCount = c
			}
		}
		flat = append(flat, kv{root, rs.count, bestDisp})
	}
	sort.Slice(flat, func(i, j int) bool {
		if flat[i].count != flat[j].count {
			return flat[i].count > flat[j].count
		}
		return flat[i].root < flat[j].root
	})
	if len(flat) > topN {
		flat = flat[:topN]
	}
	topRoots := make([]importRootEntry, len(flat))
	for i, f := range flat {
		topRoots[i] = importRootEntry{Root: f.root, Count: f.count, Disposition: f.disp}
	}

	return UnresolvedBreakdown{
		ByDisposition: byDisp,
		ByLanguage:    byLang,
		TopRoots:      topRoots,
	}
}
