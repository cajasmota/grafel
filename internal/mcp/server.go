package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/version"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// Config controls server construction.
type Config struct {
	RegistryPath string
	DebugLevel   int    // 0 = silent, 1 = summary on shutdown, 2 = per-call
	CWD          string // optional caller CWD for routing inference
}

// Server is the archigraph MCP server: state + telemetry + the underlying
// mcp-go *MCPServer*. Tests can construct one and skip ServeStdio.
type Server struct {
	State *State
	Tel   *Telemetry
	MCP   *mcpsrv.MCPServer
	cfg   Config
}

// NewServer wires everything together: loads the registry, performs an
// initial reload, and registers all tool handlers.
func NewServer(cfg Config) (*Server, error) {
	if cfg.RegistryPath == "" {
		cfg.RegistryPath = defaultRegistryPath()
	}
	reg, err := LoadRegistry(cfg.RegistryPath)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	st := NewState(reg)
	if _, err := st.Reload(); err != nil {
		return nil, fmt.Errorf("initial reload: %w", err)
	}
	tel := NewTelemetry(cfg.DebugLevel)

	srv := mcpsrv.NewMCPServer("archigraph", version.String(),
		mcpsrv.WithToolCapabilities(true))

	s := &Server{State: st, Tel: tel, MCP: srv, cfg: cfg}
	s.registerTools()
	return s, nil
}

// ServeStdio runs the MCP server on stdio until the connection closes.
func (s *Server) ServeStdio() error {
	defer func() {
		if s.cfg.DebugLevel >= 1 {
			fmt.Fprintln(os.Stderr, "archigraph mcp summary:")
			fmt.Fprintln(os.Stderr, s.Tel.SnapshotJSON())
		}
	}()
	return mcpsrv.ServeStdio(s.MCP)
}

// reloadBeforeCall is the shared mtime-based lazy refresh hook.
func (s *Server) reloadBeforeCall() {
	n, _ := s.State.Reload()
	s.Tel.MarkReload(n)
}

// inferCWD returns the caller-provided cwd from the request arguments if any,
// falling back to the configured CWD on the server.
func (s *Server) inferCWD(req mcpapi.CallToolRequest) string {
	args := req.GetArguments()
	if v, ok := args["cwd"]; ok {
		if str, ok := v.(string); ok && str != "" {
			return str
		}
	}
	return s.cfg.CWD
}

// registerTools registers every tool handler on the MCP server.
func (s *Server) registerTools() {
	s.MCP.AddTool(mcpapi.NewTool("archigraph_whoami",
		mcpapi.WithDescription("Return the inferred archigraph group + repo for the caller session."),
		mcpapi.WithString("cwd", mcpapi.Description("Optional caller working directory.")),
		mcpapi.WithString("group", mcpapi.Description("Optional explicit group override.")),
	), s.wrap("archigraph_whoami", s.handleWhoami))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_search",
		mcpapi.WithDescription("BM25-ranked graph query, optionally expanded by BFS to a depth."),
		mcpapi.WithString("question", mcpapi.Required(), mcpapi.Description("Natural-language query.")),
		mcpapi.WithString("mode", mcpapi.DefaultString("bfs"), mcpapi.Description("Traversal mode: bfs|dfs|none.")),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(3), mcpapi.Description("BFS depth from each match.")),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800), mcpapi.Description("Max approximate tokens in rendered output.")),
		mcpapi.WithArray("context_filter", mcpapi.WithStringItems(), mcpapi.Description("Edge-kind filter (e.g. CALLS, IMPORTS).")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems(), mcpapi.Description("Repo names to scope. Use '*' for full dump.")),
		mcpapi.WithBoolean("full", mcpapi.DefaultBool(false), mcpapi.Description("Return raw JSON instead of compact text.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_search", s.handleQueryGraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_describe",
		mcpapi.WithDescription("Look up an entity by id, qualified name, or label."),
		mcpapi.WithString("label_or_id", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_describe", s.handleGetNode))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_related",
		mcpapi.WithDescription("Return neighbors of a node out to a given depth."),
		mcpapi.WithString("node", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_related", s.handleGetNeighbors))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_trace",
		mcpapi.WithDescription("Confidence-weighted shortest path between two nodes (cross-repo aware)."),
		mcpapi.WithString("source", mcpapi.Required()),
		mcpapi.WithString("target", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_trace", s.handleShortestPath))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_clusters",
		mcpapi.WithDescription("List Louvain communities across the loaded graphs."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_list_clusters", s.handleListCommunities))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_save_finding",
		mcpapi.WithDescription("Persist a question/answer pair to the group's memory directory."),
		mcpapi.WithString("question", mcpapi.Required()),
		mcpapi.WithString("answer", mcpapi.Required()),
		mcpapi.WithString("type", mcpapi.DefaultString("note")),
		mcpapi.WithArray("nodes", mcpapi.WithStringItems()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_save_finding", s.handleSaveResult))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_findings",
		mcpapi.WithDescription("List previously saved findings for the resolved group, newest-first."),
		mcpapi.WithString("entity_id", mcpapi.Description("Optional entity ID, prefixed ID, qname, or label to filter by.")),
		mcpapi.WithString("since", mcpapi.Description("Optional RFC3339 timestamp; only findings saved at or after this time are returned.")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50), mcpapi.Description("Max findings to return.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_list_findings", s.handleListFindings))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_source",
		mcpapi.WithDescription("Return source-file snippet for a node from disk."),
		mcpapi.WithString("node_id", mcpapi.Required()),
		mcpapi.WithNumber("context_lines", mcpapi.DefaultNumber(20)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_get_source", s.handleGetNodeSource))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_recent_activity",
		mcpapi.WithDescription("Return entities whose source files were modified after a given time."),
		mcpapi.WithString("since", mcpapi.Description("RFC3339 timestamp.")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_recent_activity", s.handleRecentActivity))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_link_candidates",
		mcpapi.WithDescription("List pending cross-repo link candidates."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("channel"),
		mcpapi.WithString("method"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_list_link_candidates", s.handleListLinkCandidates))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_resolve_link_candidate",
		mcpapi.WithDescription("Accept or reject a cross-repo link candidate."),
		mcpapi.WithString("candidate_id", mcpapi.Required()),
		mcpapi.WithString("decision", mcpapi.Required(), mcpapi.Description("accept|reject")),
		mcpapi.WithString("reason"),
		mcpapi.WithString("override_target"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_resolve_link_candidate", s.handleResolveLinkCandidate))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_enrichment_candidates",
		mcpapi.WithDescription("List pending enrichment candidates for a repo."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("kind"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_list_enrichment_candidates", s.handleListEnrichmentCandidates))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_submit_enrichment",
		mcpapi.WithDescription("Submit an enrichment resolution."),
		mcpapi.WithString("candidate_id", mcpapi.Required()),
		mcpapi.WithString("value", mcpapi.Required()),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(1)),
		mcpapi.WithString("reason"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_submit_enrichment", s.handleSubmitEnrichment))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_reject_enrichment",
		mcpapi.WithDescription("Reject an enrichment candidate."),
		mcpapi.WithString("candidate_id", mcpapi.Required()),
		mcpapi.WithString("reason", mcpapi.Required()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_reject_enrichment", s.handleRejectEnrichment))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_graph_stats",
		mcpapi.WithDescription("Corpus-level metrics for the resolved group."),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
	), s.wrap("archigraph_graph_stats", s.handleGraphStats))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_telemetry",
		mcpapi.WithDescription("Server uptime, per-tool counters, reload counts."),
	), s.wrap("archigraph_get_telemetry", s.handleGetTelemetry))
}

// wrap is the shared handler middleware: telemetry + lazy reload + panic guard.
func (s *Server) wrap(name string, fn func(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)) mcpsrv.ToolHandlerFunc {
	return func(ctx context.Context, req mcpapi.CallToolRequest) (res *mcpapi.CallToolResult, err error) {
		end := s.Tel.Begin(name)
		defer func() {
			isErr := err != nil || (res != nil && res.IsError)
			end(isErr)
		}()
		s.reloadBeforeCall()
		return fn(ctx, req)
	}
}
