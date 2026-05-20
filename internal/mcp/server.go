package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/version"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// mcpInstructions is the handshake text returned to MCP clients on initialize.
// It tells agents to call archigraph_whoami first and act on suggested_action.
// The doc-gen flow (pattern discovery, repair sweep, ORM query extraction,
// response shape extraction) must run before substantive graph queries are
// reliable — this nudge ensures agents prompt the user when it hasn't run yet.
const mcpInstructions = `archigraph — code graph MCP server

On first connect in a session:
  1. Call archigraph_whoami (with cwd= set to the caller's working directory).
  2. Check the suggested_action field in the response.
  3. If suggested_action is "run /generate-docs": proactively suggest to the
     user that they trigger documentation generation before substantive queries.
     Say something like: "I noticed archigraph is connected but documentation
     hasn't been generated yet — want me to run /generate-docs now? It enables
     pattern discovery, repair sweep, ORM query mapping, and response shape
     extraction, which makes subsequent graph queries much more accurate."
  4. If suggested_action starts with "refresh docs": surface that N files have
     changed and offer to refresh. Example: "N files have changed since docs
     were last generated — want me to refresh them?"
  5. If suggested_action mentions "pattern candidates" or "repair candidates":
     offer to review them after the user's immediate task is addressed.
  6. If suggested_action is "none — graph is healthy": proceed normally.

Set ARCHIGRAPH_WHOAMI_NUDGE=quiet to suppress doc-state fields (e.g. in CI).`

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
// Tool count: 14 (9 renamed/bundled + 5 unchanged: whoami, save_finding,
// list_findings, get_source, recent_activity, get_telemetry).
func (s *Server) registerTools() {
	// -----------------------------------------------------------------------
	// Unchanged tools (5)
	// -----------------------------------------------------------------------

	s.MCP.AddTool(mcpapi.NewTool("archigraph_whoami",
		mcpapi.WithDescription("Return the inferred archigraph group + repo for the caller session."),
		mcpapi.WithString("cwd", mcpapi.Description("Optional caller working directory.")),
		mcpapi.WithString("group", mcpapi.Description("Optional explicit group override.")),
	), s.wrap("archigraph_whoami", s.handleWhoami))

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

	// -----------------------------------------------------------------------
	// Renamed tools (5): search→find, describe→inspect, related→expand,
	//                     list_clusters→clusters, graph_stats→stats
	// -----------------------------------------------------------------------

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find",
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
	), s.wrap("archigraph_find", s.handleQueryGraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_inspect",
		mcpapi.WithDescription("Look up an entity by id, qualified name, or label."),
		mcpapi.WithString("label_or_id", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_inspect", s.handleGetNode))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_expand",
		mcpapi.WithDescription("Return neighbors of a node out to a given depth."),
		mcpapi.WithString("node", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_expand", s.handleGetNeighbors))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_trace",
		mcpapi.WithDescription("Confidence-weighted shortest path between two nodes (cross-repo aware)."),
		mcpapi.WithString("source", mcpapi.Required()),
		mcpapi.WithString("target", mcpapi.Required()),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_trace", s.handleShortestPath))

	// archigraph_traces — process-flow query surface (#724).
	// action=list  → ranked Process entities loaded for the group
	// action=get   → full step chain for one Process
	// action=follow→ ad-hoc forward BFS from any entry_point_id
	s.MCP.AddTool(mcpapi.NewTool("archigraph_traces",
		mcpapi.WithDescription("Process-flow traces. action=list: ranked Processes; action=get: full step chain; action=follow: ad-hoc BFS from an entry point."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|get|follow")),
		mcpapi.WithString("process_id", mcpapi.Description("(get) Process entity id; bare or repo-prefixed.")),
		mcpapi.WithString("entry_point_id", mcpapi.Description("(follow) Entity id of the entry function.")),
		mcpapi.WithNumber("max_depth", mcpapi.DefaultNumber(8), mcpapi.Description("(follow) BFS depth cap (≤10).")),
		mcpapi.WithNumber("branching_factor", mcpapi.DefaultNumber(3), mcpapi.Description("(follow) Per-step branch cap (≤4).")),
		mcpapi.WithBoolean("cross_stack_only", mcpapi.DefaultBool(false), mcpapi.Description("(list) Only return Processes that traverse an HTTP boundary.")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(25), mcpapi.Description("(list) Max processes returned.")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems()),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_traces", s.handleTraces))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_clusters",
		mcpapi.WithDescription("List Louvain communities across the loaded graphs."),
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

	// -----------------------------------------------------------------------
	// Bundled tools (3 bundles, each dispatches on action=)
	// -----------------------------------------------------------------------

	// archigraph_enrichments — bundles: list_enrichment_candidates,
	//   submit_enrichment, reject_enrichment. action: list|submit|reject.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_enrichments",
		mcpapi.WithDescription("Manage enrichment candidates. action=list: list pending; action=submit: resolve a candidate; action=reject: reject a candidate."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|submit|reject")),
		// list args
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems(), mcpapi.Description("(list) Repos to scope.")),
		mcpapi.WithString("kind", mcpapi.Description("(list) Filter by candidate kind.")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10), mcpapi.Description("(list) Max candidates returned.")),
		// submit/reject args
		mcpapi.WithString("candidate_id", mcpapi.Description("(submit|reject) Candidate ID.")),
		mcpapi.WithString("value", mcpapi.Description("(submit) Agent's resolution value.")),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(1), mcpapi.Description("(submit) Confidence in [0,1].")),
		mcpapi.WithString("reason", mcpapi.Description("(submit) Optional audit note. (reject) Required rejection reason.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_enrichments", s.handleEnrichments))

	// archigraph_get_next_enrichment_task — returns the highest-priority
	// EnrichmentTask (1 entity, N pending actions) so agents can work
	// task-by-task instead of candidate-by-candidate. Issue #1134.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_next_enrichment_task",
		mcpapi.WithDescription("Return the next highest-priority enrichment task: one entity with all its pending enrichment actions (describe_entity, classify_domain, describe_role, …). Each action has a candidate_id that can be resolved via archigraph_enrichments action=submit. Use this instead of action=list when you want to enrich one entity completely before moving to the next."),
		mcpapi.WithString("kind", mcpapi.Description("Optional: filter to tasks that have at least one action of this kind (e.g. 'describe_entity').")),
		mcpapi.WithBoolean("overdue_only", mcpapi.DefaultBool(false), mcpapi.Description("When true, return only tasks whose oldest pending action is >7 days old.")),
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems(), mcpapi.Description("Repos to consider; empty means all.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_get_next_enrichment_task", s.handleGetNextEnrichmentTask))

	// archigraph_cross_links — bundles: list_link_candidates,
	//   resolve_link_candidate. action: list|accept|reject.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_cross_links",
		mcpapi.WithDescription("Manage cross-repo link candidates. action=list: list pending; action=accept: accept a candidate; action=reject: reject a candidate."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|accept|reject")),
		// list args
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems(), mcpapi.Description("(list) Returns candidates whose source OR target is in these repos.")),
		mcpapi.WithString("channel", mcpapi.Description("(list) Filter by channel label.")),
		mcpapi.WithString("method", mcpapi.Description("(list) Filter by detection method.")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10), mcpapi.Description("(list) Max candidates returned.")),
		// accept/reject args
		mcpapi.WithString("candidate_id", mcpapi.Description("(accept|reject) Candidate ID.")),
		mcpapi.WithString("reason", mcpapi.Description("(reject) Free-form audit string.")),
		mcpapi.WithString("override_target", mcpapi.Description("(accept) Override the candidate's target ID with this prefixed ID.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_cross_links", s.handleCrossLinks))

	// archigraph_repairs — bundles: list_residuals, submit_repair.
	//   action: list|submit.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_repairs",
		mcpapi.WithDescription("Manage residual-edge repair queue (ADR-0015). action=list: list pending residuals; action=submit: submit a repair."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("list|submit")),
		// list args
		mcpapi.WithArray("repo_filter", mcpapi.WithStringItems(), mcpapi.Description("(list) Repos to scope.")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(20), mcpapi.Description("(list) Max residuals returned.")),
		mcpapi.WithNumber("offset", mcpapi.DefaultNumber(0), mcpapi.Description("(list) Pagination offset.")),
		mcpapi.WithBoolean("include_stale", mcpapi.DefaultBool(false), mcpapi.Description("(list) When true, return stale repairs from repair_stats.json instead of active residuals. Stale repairs are repairs whose edge_id no longer matches any current candidate — the source moved since the repair was submitted.")),
		// submit args
		mcpapi.WithString("residual_id", mcpapi.Description("(submit) er:<hex16> identifier from action=list.")),
		mcpapi.WithString("resolution", mcpapi.Description("(submit) bind_to_entity|reclassify_as_external|reclassify_as_dynamic|reclassify_as_resolved|abandon")),
		mcpapi.WithString("target_entity_id", mcpapi.Description("(submit) Required when resolution=bind_to_entity.")),
		mcpapi.WithString("module", mcpapi.Description("(submit) Required when resolution=reclassify_as_external.")),
		mcpapi.WithString("new_target", mcpapi.Description("(submit) Required when resolution=reclassify_as_resolved.")),
		mcpapi.WithString("dynamic_reason"),
		mcpapi.WithString("abandon_reason"),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(0.0), mcpapi.Description("(submit) Agent confidence in [0,1].")),
		mcpapi.WithString("reasoning"),
		mcpapi.WithString("source", mcpapi.DefaultString("mcp_submit_repair")),
		mcpapi.WithString("repo", mcpapi.Description("(submit) Optional repo name override; defaults to the repo that owns residual_id.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_repairs", s.handleRepairs))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_telemetry",
		mcpapi.WithDescription("Server uptime, per-tool counters, reload counts."),
	), s.wrap("archigraph_get_telemetry", s.handleGetTelemetry))

	// -----------------------------------------------------------------------
	// archigraph_patterns — ADR-0018, PR β
	// action=query|record (refine|apply|reject|promote reserved for PR γ)
	// -----------------------------------------------------------------------
	s.MCP.AddTool(mcpapi.NewTool("archigraph_patterns",
		mcpapi.WithDescription("Agent-learned pattern store (ADR-0018). action=query: find patterns by task description; action=record: store a new pattern with exemplars."),
		mcpapi.WithString("action", mcpapi.Required(), mcpapi.Description("query|record (refine|apply|reject|promote in γ)")),
		// query args
		mcpapi.WithString("text", mcpapi.Description("(query) Natural-language task description.")),
		mcpapi.WithString("category", mcpapi.Description("(query|record) code|process|team|tooling|architecture")),
		mcpapi.WithBoolean("include_candidates", mcpapi.DefaultBool(false), mcpapi.Description("(query) Include is_candidate=true patterns.")),
		mcpapi.WithBoolean("include_private", mcpapi.DefaultBool(false), mcpapi.Description("(query) Include private anti-patterns (archigraph-patterns-sync only).")),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10), mcpapi.Description("(query) Max patterns returned.")),
		// record args
		mcpapi.WithObject("trigger", mcpapi.Description("(record) {natural_language, keywords[], target_entity_kinds[]}")),
		mcpapi.WithArray("steps", mcpapi.WithStringItems(), mcpapi.Description("(record) Ordered recipe steps.")),
		mcpapi.WithArray("anti_patterns", mcpapi.Description("(record) [{do_not, reason, private}]")),
		mcpapi.WithArray("exemplars", mcpapi.WithStringItems(), mcpapi.Description("(record) Required: ≥1 entity id as canonical examples.")),
		mcpapi.WithBoolean("as_candidate", mcpapi.DefaultBool(false), mcpapi.Description("(record) Emit is_candidate=true (subagent discovery path).")),
		mcpapi.WithString("proposer_subagent", mcpapi.Description("(record) Subagent identifier for convergence audit.")),
		mcpapi.WithString("documentation_url", mcpapi.Description("(record) Slot for Phase-6 doc-gen URL; leave empty on initial record.")),
		// shared optional
		mcpapi.WithObject("scope", mcpapi.Description("Explicit scope override: {repos, module_paths, languages, stacks, entity_kinds}.")),
		mcpapi.WithString("group"),
		mcpapi.WithString("cwd"),
	), s.wrap("archigraph_patterns", s.handlePatterns))
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
