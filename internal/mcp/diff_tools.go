// diff_tools.go — MCP tool: archigraph_diff_refs (PH5 of #2087 / #2093).
//
// Enables agents to compare two indexed git refs for a single repo without
// going through the HTTP API. The diff computation is the same pure-Go
// algorithm used by the dashboard endpoint (internal/graph.DiffDocs).
//
// Usage:
//
//	archigraph_diff_refs(group="mygroup", repo="myrepo", ref_a="main", ref_b="feat/x")
//
// Returns a JSON blob matching the dashboard wire format:
//
//	{ summary, entities: { added, removed, modified }, relationships: { added, removed } }
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleDiffRefs implements the archigraph_diff_refs MCP tool.
//
// Arguments:
//   - group   (string, required)
//   - repo    (string, required)
//   - ref_a   (string, required) — "before" ref
//   - ref_b   (string, required) — "after" ref
func (s *Server) handleDiffRefs(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName := argString(req, "group", "")
	if groupName == "" {
		// Try resolving from cwd.
		cwd := s.inferCWD(req)
		groupName, _ = groupFromRegistryWithCandidates(s.State, cwd)
	}
	if groupName == "" {
		return mcpapi.NewToolResultError("group is required; pass group= or run from inside a registered repo"), nil
	}

	repoSlug := argString(req, "repo", "")
	if repoSlug == "" {
		return mcpapi.NewToolResultError("repo is required"), nil
	}
	refA := argString(req, "ref_a", "")
	refB := argString(req, "ref_b", "")
	if refA == "" || refB == "" {
		return mcpapi.NewToolResultError("ref_a and ref_b are required"), nil
	}

	// Resolve the repo filesystem path from the registry.
	repoPath, err := diffToolRepoPath(groupName, repoSlug)
	if err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("repo lookup failed: %v", err)), nil
	}

	// Same-ref fast path.
	if refA == refB {
		result := graph.DiffResult{
			Group: groupName,
			Repo:  repoSlug,
			RefA:  refA,
			RefB:  refB,
		}
		result.Entities.Added = []graph.DiffEntityEntry{}
		result.Entities.Removed = []graph.DiffEntityEntry{}
		result.Entities.Modified = []graph.DiffEntityEntry{}
		result.Relationships.Added = []graph.DiffRelEntry{}
		result.Relationships.Removed = []graph.DiffRelEntry{}
		return diffToolJSON(result)
	}

	// Load docA.
	dirA := daemon.StateDirForRepoRef(repoPath, refA)
	docA, err := graph.LoadGraphFromDir(dirA)
	if err != nil {
		return mcpapi.NewToolResultError(
			fmt.Sprintf("could not load graph for %s@%s: %v (run `archigraph index` on that branch first)", repoSlug, refA, err),
		), nil
	}

	// Load docB.
	dirB := daemon.StateDirForRepoRef(repoPath, refB)
	docB, err := graph.LoadGraphFromDir(dirB)
	if err != nil {
		return mcpapi.NewToolResultError(
			fmt.Sprintf("could not load graph for %s@%s: %v (run `archigraph index` on that branch first)", repoSlug, refB, err),
		), nil
	}

	result := graph.DiffDocs(docA, docB)
	result.Group = groupName
	result.Repo = repoSlug
	result.RefA = refA
	result.RefB = refB

	return diffToolJSON(result)
}

// diffToolJSON serialises the DiffResult as a JSON MCP tool result.
func diffToolJSON(r graph.DiffResult) (*mcpapi.CallToolResult, error) {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return mcpapi.NewToolResultError("failed to serialise diff: " + err.Error()), nil
	}
	return mcpapi.NewToolResultText(string(raw)), nil
}

// diffToolRepoPath resolves the filesystem path for a repo from the
// registry. Mirrors the same lookup used by handlers_v2_diff.go, but
// callable from the MCP package without a circular import.
func diffToolRepoPath(groupName, repoSlug string) (string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return "", fmt.Errorf("registry: %w", err)
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == groupName {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		return "", fmt.Errorf("group %q not registered", groupName)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return "", fmt.Errorf("load group config: %w", err)
	}
	for _, r := range cfg.Repos {
		if r.Slug == repoSlug {
			return r.Path, nil
		}
	}
	return "", fmt.Errorf("repo %q not found in group %q", repoSlug, groupName)
}
