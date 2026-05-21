package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/version"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// mcpInstructions is the handshake text returned to MCP clients on initialize.
// It tells agents to call archigraph_whoami first and act on suggested_action.
const mcpInstructions = `archigraph code-graph MCP. Call archigraph_whoami on connect (cwd= set to caller dir); act on suggested_action. Set ARCHIGRAPH_WHOAMI_NUDGE=quiet to suppress doc-state (CI).`

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

	// activityBroker fans MCP tool call events to SSE subscribers (epic #1157).
	// Optional: when nil, events are silently dropped.
	activityBroker *MCPActivityBroker
}

// SetActivityBroker wires the MCP activity broker into the server so that
// every tool call emits a real-time MCPActivityEvent to subscribers. Call
// this from the daemon entrypoint before ServeStdio.
func (s *Server) SetActivityBroker(b *MCPActivityBroker) {
	s.activityBroker = b
}

// ActivityBroker returns the wired broker, or nil when not set.
func (s *Server) ActivityBroker() *MCPActivityBroker {
	return s.activityBroker
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
		mcpsrv.WithToolCapabilities(true),
		mcpsrv.WithInstructions(mcpInstructions))

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
// Source of truth: AddTool calls below — keep internal/mcp/SCHEMA.md in sync.
// Tool count: 33 (#1281: 9→4 bundles; #1293: desc trim; #1312: +archigraph_quality_cycles; #1314: +archigraph_auth_coverage; #1322: +archigraph_secrets; #1323: +archigraph_test_coverage; #1333: desc trimmed to ≤80 chars for budget gate; #1334: +archigraph_license_audit).
// Dropped (HTTP-only): archigraph_diagnostics, archigraph_quality_orphans,
//   archigraph_get_next_enrichment_task, archigraph_get_telemetry.
func (s *Server) registerTools() {
	s.MCP.AddTool(mcpapi.NewTool("archigraph_whoami",
		mcpapi.WithDescription("Return inferred group + repo for the caller session."),
		mcpapi.WithString("cwd"),
		mcpapi.WithString("group"),
	), s.wrap("archigraph_whoami", s.handleWhoami))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_save_finding",
		mcpapi.WithDescription("Persist a question/answer pair to the group's memory."),
		mcpapi.WithString("question", mcpapi.Required()),
		mcpapi.WithString("answer", mcpapi.Required()),
		mcpapi.WithString("type", mcpapi.DefaultString("note")),
		mcpapi.WithArray("nodes", mcpapi.WithStringItems()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_save_finding", s.handleSaveResult))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_findings",
		mcpapi.WithDescription("List saved findings for the resolved group, newest-first."),
		mcpapi.WithString("entity_id"),
		mcpapi.WithString("since"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_list_findings", s.handleListFindings))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_source",
		mcpapi.WithDescription("Return source-file snippet for a node."),
		mcpapi.WithString("node_id", mcpapi.Required()),
		mcpapi.WithNumber("context_lines", mcpapi.DefaultNumber(20)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_get_source", s.handleGetNodeSource))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_recent_activity",
		mcpapi.WithDescription("Return entities modified after a given time."),
		mcpapi.WithString("since"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_recent_activity", s.handleRecentActivity))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find",
		mcpapi.WithDescription("BM25 graph query with optional BFS expansion."),
		mcpapi.WithString("question", mcpapi.Required()),
		mcpapi.WithString("mode", mcpapi.DefaultString("bfs")),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(3)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithArray("context_filter", mcpapi.WithStringItems()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithBoolean("full", mcpapi.DefaultBool(false)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_find", s.handleQueryGraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_inspect",
		mcpapi.WithDescription("Look up an entity by id, qualified name, or label."),
		mcpapi.WithString("label_or_id", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_inspect", s.handleGetNode))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_expand",
		mcpapi.WithDescription("Return neighbors of a node up to a given depth."),
		mcpapi.WithString("node", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_expand", s.handleGetNeighbors))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_trace",
		mcpapi.WithDescription("Confidence-weighted shortest path between two nodes."),
		mcpapi.WithString("source", mcpapi.Required()),
		mcpapi.WithString("target", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_trace", s.handleShortestPath))

	// archigraph_traces — process-flow query surface (#724).
	// action=list|get|follow
	s.MCP.AddTool(mcpapi.NewTool("archigraph_traces",
		mcpapi.WithDescription("Process-flow traces: list=ranked, get=steps, follow=BFS."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|get|follow")),
		mcpapi.WithString("process_id"),
		mcpapi.WithString("entry_point_id"),
		mcpapi.WithNumber("max_depth", mcpapi.DefaultNumber(8)),
		mcpapi.WithNumber("branching_factor", mcpapi.DefaultNumber(3)),
		mcpapi.WithBoolean("cross_stack_only", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(25)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_traces", s.handleTraces))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_clusters",
		mcpapi.WithDescription("List Louvain communities across loaded graphs."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_clusters", s.handleListCommunities))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_stats",
		mcpapi.WithDescription("Corpus-level metrics for the resolved group."),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
	), s.wrap("archigraph_stats", s.handleGraphStats))

	// archigraph_enrichments — action: list|submit|reject.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_enrichments",
		mcpapi.WithDescription("Enrichment candidates: list=pending, submit=resolve, reject=discard."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|submit|reject")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("kind"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithString("candidate_id"),
		mcpapi.WithString("value"),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(1)),
		mcpapi.WithString("reason"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_enrichments", s.handleEnrichments))

	// archigraph_get_next_enrichment_task dropped (use enrichments(action=list,limit=1) instead).

	// archigraph_cross_links — action: list|accept|reject.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_cross_links",
		mcpapi.WithDescription("Cross-repo link candidates: list=pending, accept=confirm, reject=discard."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|accept|reject")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("channel"),
		mcpapi.WithString("method"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithString("candidate_id"),
		mcpapi.WithString("reason"),
		mcpapi.WithString("override_target"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_cross_links", s.handleCrossLinks))

	// archigraph_repairs — action: list|submit. ADR-0015 residual-edge repair.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_repairs",
		mcpapi.WithDescription("Residual-edge repair queue: list=pending, submit=resolve."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|submit")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(20)),
		mcpapi.WithNumber("offset", mcpapi.DefaultNumber(0)),
		mcpapi.WithBoolean("include_stale", mcpapi.DefaultBool(false)),
		mcpapi.WithString("residual_id"),
		mcpapi.WithString("resolution", mcpapi.Description("bind_to_entity|reclassify_as_external|reclassify_as_dynamic|reclassify_as_resolved|abandon")),
		mcpapi.WithString("target_entity_id"),
		mcpapi.WithString("module"),
		mcpapi.WithString("new_target"),
		mcpapi.WithString("dynamic_reason"),
		mcpapi.WithString("abandon_reason"),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(0.0)),
		mcpapi.WithString("reasoning"),
		mcpapi.WithString("source", mcpapi.DefaultString("mcp_submit_repair")),
		mcpapi.WithString("repo"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_repairs", s.handleRepairs))

	// archigraph_get_telemetry dropped (dashboard-only; use HTTP /api/telemetry instead).

	// archigraph_patterns — ADR-0018. action=query|record.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_patterns",
		mcpapi.WithDescription("Agent pattern store: query=find by task, record=store with exemplars."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("query|record")),
		mcpapi.WithString("text"),
		mcpapi.WithString("category"),
		mcpapi.WithBoolean("include_candidates", mcpapi.DefaultBool(false)),
		mcpapi.WithBoolean("include_private", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithObject("trigger"),
		mcpapi.WithArray("steps", mcpapi.WithStringItems()),
		mcpapi.WithArray("anti_patterns"),
		mcpapi.WithArray("exemplars", mcpapi.WithStringItems()),
		mcpapi.WithBoolean("as_candidate", mcpapi.DefaultBool(false)),
		mcpapi.WithString("proposer_subagent"),
		mcpapi.WithString("documentation_url"),
		mcpapi.WithObject("scope"),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_patterns", s.handlePatterns))

	// archigraph_topology — message-channel topology (#1281). action=orphan_publishers|orphan_subscribers|topic_detail.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_topology",
		mcpapi.WithDescription("Message-channel topology: publisher/subscriber orphans and topic detail."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("orphan_publishers|orphan_subscribers|topic_detail")),
		mcpapi.WithString("topic_id"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_topology", s.handleTopology))

	// archigraph_flows — flow-process diagnostics (#1281). action=dead_ends|truncated|detail.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_flows",
		mcpapi.WithDescription("Flow-process diagnostics: dead_ends, truncated, detail."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("dead_ends|truncated|detail")),
		mcpapi.WithString("process_id"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_flows", s.handleFlows))

	// archigraph_diagnostics dropped (dashboard-only; use HTTP /api/diagnostics instead).

	// archigraph_quality_orphans dropped (measurement; use archigraph_find_dead_code for agents).

	// archigraph_graph_patterns — indexer-extracted patterns (#1281). action=list|get.
	// Distinct from archigraph_patterns (agent-learned store).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_graph_patterns",
		mcpapi.WithDescription("Indexer-extracted patterns (not agent store): list=browse, get=detail."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|get")),
		mcpapi.WithBoolean("needs_attention", mcpapi.DefaultBool(false)),
		mcpapi.WithString("status"),
		mcpapi.WithNumber("confidence_min", mcpapi.DefaultNumber(0)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
		mcpapi.WithString("pattern_id"),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_graph_patterns", s.handleGraphPatterns))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_search_entities",
		mcpapi.WithDescription("Substring search over entity names; ranked matches with source locations."),
		mcpapi.WithString("query", mcpapi.Required()),
		mcpapi.WithString("kind_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(30)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_search_entities", s.handleSearchEntities))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_subgraph",
		mcpapi.WithDescription("All nodes and edges within N hops of an entity."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_get_subgraph", s.handleGetSubgraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_paths",
		mcpapi.WithDescription("Shortest path between two entities with confidence."),
		mcpapi.WithString("from", mcpapi.Required()),
		mcpapi.WithString("to", mcpapi.Required()),
		mcpapi.WithNumber("max_hops", mcpapi.DefaultNumber(5)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_find_paths", s.handleFindPaths))

	// archigraph_endpoints — HTTP surface (#1281). action=definitions|calls|stats.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_endpoints",
		mcpapi.WithDescription("HTTP endpoint surface: definitions=routes, calls=call-sites, stats=counts."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("definitions|calls|stats")),
		mcpapi.WithBoolean("orphan_only", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(200)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_endpoints", s.handleEndpoints))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callers",
		mcpapi.WithDescription("Inbound callers of an entity up to N hops."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_find_callers", s.handleFindCallers))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callees",
		mcpapi.WithDescription("Outbound callees of an entity up to N hops."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_find_callees", s.handleFindCallees))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_impact_radius",
		mcpapi.WithDescription("Inbound blast-radius: affected entities with risk_score [0,1]."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("hops", mcpapi.DefaultNumber(2)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_impact_radius", s.handleImpactRadius))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_summarize_subgraph",
		mcpapi.WithDescription("Markdown summary of callers + callees within N hops."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_summarize_subgraph", s.handleSummarizeSubgraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_dead_code",
		mcpapi.WithDescription("Entities with no project edges — dead code or extraction gap candidates."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("kind_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_find_dead_code", s.handleFindDeadCode))

	// archigraph_quality_cycles — import cycle detection (#1312).
	// Runs Tarjan SCC on IMPORTS edges; each SCC > 1 = circular dependency.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_quality_cycles",
		mcpapi.WithDescription("Detect import cycles via Tarjan SCC; returns members, weakest edge, fix hint."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_quality_cycles", s.handleQualityCycles))

	// archigraph_auth_coverage — security audit (#1314).
	// Walk all http_endpoint_definition entities and flag those without auth
	// decorators/middleware.  Severity: error (sensitive/IDOR), warn (public), info (covered).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_auth_coverage",
		mcpapi.WithDescription("Security audit: flag HTTP endpoints missing auth (severity, IDOR risk)."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithBoolean("only_missing", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(200)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_auth_coverage", s.handleAuthCoverage))

	// #1323: test-coverage graph — link Test entities to the code they exercise.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_test_coverage",
		mcpapi.WithDescription("Find production entities with no TESTS edge, ranked by severity."),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("severity", mcpapi.Description("Filter uncovered list: high|medium|low (empty=all)")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithBoolean("top_directories", mcpapi.DefaultBool(false)),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_test_coverage", s.handleTestCoverage))

	// archigraph_secrets — hardcoded secret detector (#1322).
	// Walks source files; flags API keys, passwords, JWT tokens, and other
	// high-entropy credentials. Test fixtures and opt-out comments are suppressed.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_secrets",
		mcpapi.WithDescription("Scan source files for hardcoded secrets; returns masked findings by severity."),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
		mcpapi.WithString("severity",
			mcpapi.Description("Minimum severity to include (critical|high|medium|low). Default: all."),
		),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(200),
			mcpapi.Description("Maximum number of findings to return."),
		),
	), s.wrap("archigraph_secrets", s.handleSecrets))

	// archigraph_license_audit — dependency license detector (#1334).
	// Reads ExternalPackage entities, detects SPDX licenses, flags incompatible
	// combinations (GPL/AGPL in MIT project = error; LGPL/MPL = warn).
	// Transitive npm deps surfaced when include_transitive=true.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_license_audit",
		mcpapi.WithDescription("Detect dependency licenses; flag GPL/AGPL incompatibility, weak-copyleft."),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
		mcpapi.WithBoolean("include_transitive", mcpapi.DefaultBool(false),
			mcpapi.Description("Include transitive/indirect dependencies. Default: direct only."),
		),
		mcpapi.WithString("severity",
			mcpapi.Description("Filter output: error|warn|info|unknown (empty = all). "+
				"error=strong-copyleft incompatibility, warn=weak-copyleft."),
		),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(500),
			mcpapi.Description("Maximum number of dependency findings to return."),
		),
	), s.wrap("archigraph_license_audit", s.handleLicenseAudit))
}

// wrap is the shared handler middleware: telemetry + lazy reload + panic guard
// + MCP activity event emission (epic #1157, Phase 1).
func (s *Server) wrap(name string, fn func(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)) mcpsrv.ToolHandlerFunc {
	return func(ctx context.Context, req mcpapi.CallToolRequest) (res *mcpapi.CallToolResult, err error) {
		end := s.Tel.Begin(name)
		defer func() {
			isErr := err != nil || (res != nil && res.IsError)
			end(isErr)
		}()
		s.reloadBeforeCall()
		res, err = fn(ctx, req)
		s.emitActivity(ctx, name, req, res)
		return res, err
	}
}

// emitActivity publishes a MCPActivityEvent to the activity broker (when
// wired). It is called after every tool handler returns. The agent_id is
// derived from the "archigraph-agent-id" context value when set, or falls
// back to the User-Agent extracted at session accept time.
func (s *Server) emitActivity(_ context.Context, toolName string, req mcpapi.CallToolRequest, res *mcpapi.CallToolResult) {
	if s.activityBroker == nil {
		return
	}
	args := req.GetArguments()
	// Build a safe copy of args (values are already JSON-friendly interface{}s).
	argsCopy := make(map[string]any, len(args))
	for k, v := range args {
		argsCopy[k] = v
	}
	event := MCPActivityEvent{
		ToolName:  toolName,
		QueryArgs: argsCopy,
		Timestamp: 0, // broker will fill this in
	}
	// Extract node/edge IDs from the result content when present.
	if res != nil && !res.IsError {
		event.ReturnedNodeIDs, event.ReturnedEdgeIDs = extractIDs(res)
	}
	s.activityBroker.Publish(event)
}

// extractIDs attempts to pull entity IDs and edge IDs out of a tool result's
// JSON content. It is best-effort: returns nil slices on any parse failure.
// mcp-go stores []Content where each element may be TextContent, ImageContent,
// etc. We type-assert to mcpapi.TextContent and parse the text as JSON.
func extractIDs(res *mcpapi.CallToolResult) (nodeIDs, edgeIDs []string) {
	if res == nil || len(res.Content) == 0 {
		return
	}
	for _, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok || tc.Text == "" {
			continue
		}
		// Parse the text body as JSON and probe for known ID-bearing fields.
		var payload map[string]any
		if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
			continue
		}
		nodeIDs = append(nodeIDs, collectScalarIDs(payload,
			"entity_id", "node_id", "pattern_id", "topic_id", "process_id")...)
		nodeIDs = append(nodeIDs, collectSliceIDs(payload,
			"results", "nodes", "steps", "orphans", "patterns", "orphan_publishers",
			"orphan_subscribers", "dead_ends", "truncated_flows", "publishers",
			"subscribers", "exemplars",
			"callers", "callees", "affected", "dead_code", "dependencies")...)
		edgeIDs = append(edgeIDs, collectSliceIDs(payload, "edges")...)
	}
	return dedup(nodeIDs), dedup(edgeIDs)
}

// collectScalarIDs extracts scalar string values for the given keys from a
// JSON payload map.
func collectScalarIDs(m map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// collectSliceIDs extracts entity_id / from_id / to_id strings from an
// array value at each key in m.
func collectSliceIDs(m map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		arr, ok := v.([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"entity_id", "node_id", "from_id", "to_id", "pattern_id", "topic_id", "process_id"} {
				if s, ok := obj[field].(string); ok && s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// dedup removes duplicate strings preserving order.
func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
