// archigraph_control_flow MCP tool — #4822, control-flow epic #4820 part (b).
//
// Returns an ON-DEMAND, NOT-PERSISTED per-function control-flow graph (CFG): the
// function's basic blocks (start / decision / loop / process / return / throw /
// end) and the control-flow edges between them (seq / branch_true / branch_false
// / loop_back / exit), with the predicate text on decision/loop nodes and the
// side-effect annotations (db_read/db_write/http_out/…) on process nodes. It
// also surfaces the function's cyclomatic complexity.
//
// This feeds the flowchart view (#4819) and a complexity/control-flow query
// surface. The CFG is built for the one requested function at call time and
// thrown away — no basic-block entities are written to the graph (the graph
// stays lean). A light in-memory cache keyed by (entity id, source hash) skips
// the rebuild on repeated, unchanged calls (substrate.BuildControlFlowGraphCached).
//
// Token control (#2828): the response is parameterised by a `detail` level so
// callers pull only what they need —
//
//	outline    → node shapes + lines + complexity, no conditions/effects/labels
//	decisions  → outline + condition text on decision/loop nodes (default)
//	data       → decisions + effect annotations on process nodes
//	full       → data + node labels (the trimmed source line per node)
//
// Languages: Python + JS/TS first (matching part (a)'s validated set). Other
// languages return a degenerate start→process→end CFG with supported=false until
// extended (epic #4820 / #4830 follow-ups). Read-only, deterministic.
package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/substrate"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// cfgDetail enumerates the supported detail levels.
type cfgDetail int

const (
	cfgOutline cfgDetail = iota
	cfgDecisions
	cfgData
	cfgFull
)

func parseCFGDetail(s string) cfgDetail {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "outline":
		return cfgOutline
	case "data":
		return cfgData
	case "full":
		return cfgFull
	case "", "decisions":
		return cfgDecisions
	default:
		return cfgDecisions
	}
}

// handleControlFlow implements archigraph_control_flow. It resolves a function
// entity (by id / qualified name / label / cross-repo prefixed id, mirroring
// archigraph_effects), reads its source window, builds the on-demand CFG, and
// serialises it at the requested detail level.
func (s *Server) handleControlFlow(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	key := argString(req, "entity_id", "")
	if key == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	detail := parseCFGDetail(argString(req, "detail", "decisions"))

	lr, e, ambiguous, ok := resolveFunctionEntity(lg, repos, key)
	if ambiguous != nil {
		return jsonResult(ambiguous), nil
	}
	if !ok {
		return mcpapi.NewToolResultError(fmt.Sprintf("not found: %s", key)), nil
	}

	out := buildControlFlowPayload(lr, e, detail)
	return jsonResult(out), nil
}

// resolveFunctionEntity resolves an entity key the same way handleEffects does:
// prefixed-id fast path, then label/qname/id lookup across the considered repos.
// Returns (repo, entity, nil, true) on a single hit; (nil,nil,ambiguousPayload,
// false) when more than one entity matches; (nil,nil,nil,false) when none do.
func resolveFunctionEntity(lg *LoadedGroup, repos []*LoadedRepo, key string) (*LoadedRepo, *graph.Entity, map[string]any, bool) {
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if ent, ok := r.LabelIndex.ByID[local]; ok {
				return r, ent, nil, true
			}
		}
	}
	type matchPair struct {
		ent  *graph.Entity
		repo *LoadedRepo
	}
	var matches []matchPair
	for _, r := range repos {
		for _, hit := range r.LabelIndex.LookupAll(key) {
			matches = append(matches, matchPair{ent: hit, repo: r})
		}
	}
	switch {
	case len(matches) == 0:
		return nil, nil, nil, false
	case len(matches) > 1:
		list := make([]map[string]any, 0, len(matches))
		for _, m := range matches {
			list = append(list, map[string]any{
				"id":             prefixedID(m.repo.Repo, m.ent.ID),
				"qualified_name": m.ent.QualifiedName,
				"label":          m.ent.Name,
				"repo":           m.repo.Repo,
				"source_file":    m.ent.SourceFile,
			})
		}
		return nil, nil, map[string]any{
			"ambiguous":     true,
			"entity_id":     key,
			"matches":       list,
			"how_to_choose": "Re-call archigraph_control_flow with the prefixed id field (e.g. \"repo:1234abcd\").",
		}, false
	default:
		return matches[0].repo, matches[0].ent, nil, true
	}
}

// buildControlFlowPayload reads the entity's source window, builds the on-demand
// CFG, and serialises it at the requested detail level.
func buildControlFlowPayload(lr *LoadedRepo, e *graph.Entity, detail cfgDetail) map[string]any {
	lang := substrate.LanguageForPath(e.SourceFile)
	out := map[string]any{
		"entity_id": prefixedID(lr.Repo, e.ID),
		"resolved": map[string]any{
			"id":             prefixedID(lr.Repo, e.ID),
			"name":           e.Name,
			"kind":           e.Kind,
			"qualified_name": e.QualifiedName,
			"repo":           lr.Repo,
			"source_file":    e.SourceFile,
		},
		"language": lang,
	}

	start, end := branchSourceSpan(e)
	if start <= 0 {
		out["supported"] = false
		out["note"] = "entity has no source span (start_line); cannot build a CFG."
		return out
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil || src == "" {
		out["supported"] = false
		out["note"] = "source window unreadable; cannot build a CFG."
		return out
	}

	g := substrate.BuildControlFlowGraphCached(prefixedID(lr.Repo, e.ID), lang, src, start)
	out["supported"] = g.Supported
	out["cyclomatic_complexity"] = g.Cyclomatic
	out["branch_count"] = g.BranchCount
	if !g.Supported {
		out["note"] = fmt.Sprintf(
			"on-demand CFG: no block detector for language %q yet (epic #4820); returning a degenerate graph. Validated languages: python, jsts.",
			lang)
	}
	out["nodes"] = cfgNodesToJSON(g.Nodes, detail)
	out["edges"] = cfgEdgesToJSON(g.Edges)
	return out
}

// cfgNodesToJSON serialises nodes, including only the fields the detail level
// asks for (token control, #2828).
func cfgNodesToJSON(nodes []substrate.CFGNode, detail cfgDetail) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{
			"id":    n.ID,
			"shape": string(n.Shape),
		}
		if n.Line > 0 {
			m["line"] = n.Line
		}
		if detail >= cfgDecisions && n.Condition != "" {
			m["condition"] = n.Condition
		}
		if detail >= cfgData && len(n.Effects) > 0 {
			effs := make([]map[string]any, 0, len(n.Effects))
			for _, ef := range n.Effects {
				em := map[string]any{"effect": ef.Effect}
				if ef.Sink != "" {
					em["sink"] = ef.Sink
				}
				effs = append(effs, em)
			}
			m["effects"] = effs
		}
		if detail >= cfgFull && n.Label != "" {
			m["label"] = n.Label
		}
		out = append(out, m)
	}
	return out
}

func cfgEdgesToJSON(edges []substrate.CFGEdge) []map[string]any {
	out := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		out = append(out, map[string]any{
			"from": e.From,
			"to":   e.To,
			"kind": string(e.Kind),
		})
	}
	return out
}
