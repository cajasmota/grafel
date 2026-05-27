package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/archigraph/internal/version"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// reloadDebounceInterval is the minimum wall-clock gap between consecutive
// reloadBeforeCall() executions. Any call arriving within this window is
// served from the already-loaded in-memory state without re-statting disk
// or forking git subprocesses. 200ms matches the watcher debounce granularity
// so no real index update is silenced, but N rapid-fire tool calls (e.g.
// 100×whoami in a benchmark) pay the reload cost at most once per interval.
//
// Env override: ARCHIGRAPH_RELOAD_DEBOUNCE_MS (0 = disabled, unlimited re-check).
const reloadDebounceInterval = 200 * time.Millisecond

// mcpInstructions is the handshake text returned to MCP clients on initialize.
// It tells agents to call archigraph_whoami first and act on suggested_action.
const mcpInstructions = `archigraph code-graph MCP. Call archigraph_whoami on connect (cwd= set to caller dir); act on suggested_action. Set ARCHIGRAPH_WHOAMI_NUDGE=quiet to suppress doc-state (CI).`

// sentinelToolName is the single tool returned when the caller's cwd is not
// covered by any registered archigraph group (#1769).
const sentinelToolName = "archigraph_status"

// sentinelToolDescription is the description of the sentinel tool shown in the
// Claude Code handshake when no registered group covers the session cwd.
// Note: mid-session group registration is NOT reflected here; that requires
// notifications/tools/list_changed (tracked in #1772).
const sentinelToolDescription = "Archigraph: no indexed group covers cwd. Run install or cd to a repo."

// Config controls server construction.
type Config struct {
	RegistryPath string
	DebugLevel   int    // 0 = silent, 1 = summary on shutdown, 2 = per-call
	CWD          string // optional caller CWD for routing inference
}

// Server is the archigraph MCP server: state + telemetry + the underlying
// mcp-go *MCPServer*. Tests can construct one and skip ServeStdio.
type Server struct {
	State   *State
	Tel     *Telemetry
	SessMet *SessionMetrics // per-tool session metrics + daily rollup (#2192)
	MCP     *mcpsrv.MCPServer
	cfg     Config

	// activityBroker fans MCP tool call events to SSE subscribers (epic #1157).
	// Optional: when nil, events are silently dropped.
	activityBroker *MCPActivityBroker

	// reloadDebounceMu / reloadLastAt implement the per-call reload debounce
	// (#2550). reloadLastAt is the Unix-nano timestamp of the most recent
	// reloadBeforeCall() that actually ran. Subsequent calls within
	// reloadDebounceInterval are skipped. atomic int64 for the fast-path
	// read; mu serialises the slow path so exactly one goroutine enters
	// reloadLocked() when the window expires.
	reloadDebounceMu sync.Mutex
	reloadLastAt     atomic.Int64 // Unix nano; 0 = never run
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

	// Build metrics dir (~/.archigraph/metrics/). Best-effort: if home dir
	// resolution fails, metricsDir is left empty and rollups are skipped.
	metricsDir := ""
	if home, herr := os.UserHomeDir(); herr == nil {
		metricsDir = filepath.Join(home, ".archigraph", "metrics")
	}
	sessMet := NewSessionMetrics(newSessionID(), metricsDir)

	srv := mcpsrv.NewMCPServer("archigraph", version.String(),
		mcpsrv.WithToolCapabilities(true),
		mcpsrv.WithInstructions(mcpInstructions))

	s := &Server{State: st, Tel: tel, SessMet: sessMet, MCP: srv, cfg: cfg}
	s.registerTools()
	return s, nil
}

// newSessionID returns a compact session identifier derived from current time
// and process ID. A UUID library is not pulled in — collision probability is
// negligible for local daemon use.
func newSessionID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
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

// Stop cleanly shuts down the server. It flushes session metrics (best-effort)
// before returning. Called during daemon graceful shutdown to persist daily
// rollups to ~/.archigraph/metrics/ (issue #2530).
func (s *Server) Stop() {
	if s.SessMet != nil {
		s.SessMet.FlushRollup()
	}
}

// reloadBeforeCall is the shared mtime-based lazy refresh hook.
//
// #1772: when the registry signature (group→repo set) mutated since the
// previous reload, emit notifications/tools/list_changed so MCP clients
// re-issue tools/list.
//
// #2550: debounce — skip the reload if one completed within the last
// reloadDebounceInterval. The debounce eliminates the per-call overhead
// of stat'ing every repo's graph file and forking git subprocesses via
// gitmeta.Capture (called transitively from daemon.FindGraphFile →
// daemon.StateDirForRepo → gitmeta.Capture). Before this fix every tool
// — including trivial ones like whoami — paid ~500ms because the
// reloadLocked scan ran on every call.
func (s *Server) reloadBeforeCall() {
	now := time.Now().UnixNano()
	last := s.reloadLastAt.Load()
	if last != 0 && time.Duration(now-last) < reloadDebounceInterval {
		// Fast path: within debounce window — skip reload entirely.
		return
	}

	// Slow path: at most one goroutine runs reloadLocked() per window.
	// We re-check after acquiring the mutex in case another goroutine
	// already updated reloadLastAt while we were waiting.
	s.reloadDebounceMu.Lock()
	defer s.reloadDebounceMu.Unlock()
	now = time.Now().UnixNano()
	last = s.reloadLastAt.Load()
	if last != 0 && time.Duration(now-last) < reloadDebounceInterval {
		return
	}

	n, surfaceChanged, _ := s.State.ReloadAndSurfaceChanged()
	s.reloadLastAt.Store(time.Now().UnixNano())
	s.Tel.MarkReload(n)
	if surfaceChanged && s.MCP != nil {
		// Best-effort: failures are silently ignored. The notification carries
		// no payload — clients respond by re-issuing tools/list.
		s.MCP.SendNotificationToAllClients(mcpapi.MethodNotificationToolsListChanged, nil)
	}
}

// inferCWD returns the best available working-directory hint for a tool call.
// Resolution order (ADR-0008 / #1746):
//  1. "cwd" argument in the request (set by the bridge from _meta.cwd or bridge startup dir).
//  2. CWD configured on the server at construction time (set by callers that
//     know the cwd at build time, e.g. bench-mcp or tests).
//  3. os.Getwd() of the daemon/server process — covers the stdio transport
//     ("archigraph mcp serve" launched directly from the user's project dir).
func (s *Server) inferCWD(req mcpapi.CallToolRequest) string {
	args := req.GetArguments()
	if v, ok := args["cwd"]; ok {
		if str, ok := v.(string); ok && str != "" {
			return str
		}
	}
	if s.cfg.CWD != "" {
		return s.cfg.CWD
	}
	// Last resort: use the process working directory. On the stdio transport
	// the daemon/server process inherits the cwd of the spawning CLI, so this
	// is the user's project directory when they run `archigraph mcp serve`.
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// ListToolsForCWD implements the cwd-gate for the tools/list handshake (#1769).
//
// Decision matrix:
//   - cwd resolves to exactly one registered group → full tool list (current behavior).
//   - cwd is inside multiple groups (ambiguous) → full tool list; agent must pass group= explicitly.
//   - cwd outside ALL registered groups → sentinel only (archigraph_status).
//   - cwd resolves to a group but that group has 0 repos loaded → sentinel + hint to rebuild.
//
// The returned slice contains daemon.MCPToolEntry values ready for wire serialisation.
// When the full list is returned, the caller owns filtering/sorting.
//
// cwd may be empty — in that case the singleton-group fallback in resolveGroup
// applies when there is exactly one registered group (same as tool-call routing).
func (s *Server) ListToolsForCWD(cwd string) ([]MCPToolEntry, error) {
	// Reload lazily so the registry is fresh (same as any tool call).
	s.reloadBeforeCall()

	// Determine group coverage for the given cwd.
	group, candidates := groupFromRegistryWithCandidates(s.State, cwd)

	var groupEmpty bool
	if group != "" {
		// Check if the resolved group has any repos loaded.
		if grp, ok := s.State.registry.Groups[group]; !ok || len(grp.Repos) == 0 {
			groupEmpty = true
		}
	}

	// Three paths that lead to the sentinel:
	//   1. No groups are registered at all.
	//   2. Group resolved but has 0 repos (empty group, needs rebuild).
	//   3. No group covers cwd AND multiple groups registered AND none is singleton.
	//      (Sentinel here would be unhelpful — handled below.)
	unregistered := len(s.State.registry.Groups) == 0

	// cwd is outside ALL registered group paths (group == "" and not ambiguous).
	noDirectMatch := group == "" && len(candidates) == 0 && len(s.State.registry.Groups) > 0

	if noDirectMatch {
		// #2620: Singleton-group fallback — when exactly one group is registered,
		// use it as the implicit default regardless of cwd. This makes the bridge
		// usable from hosts (Windsurf JetBrains, Codex) that launch with cwd=/
		// or any directory that is not under a registered repo path.
		if len(s.State.registry.Groups) == 1 {
			for gname, grp := range s.State.registry.Groups {
				group = gname
				if len(grp.Repos) == 0 {
					groupEmpty = true
				}
				break
			}
		} else {
			// Multiple groups registered and cwd is unmatched: return the full tool
			// catalog anyway. Each tool that requires a group will error at call time
			// with a "specify group= (registered groups: ...)" message. Downgrading
			// to sentinel here makes the bridge completely unusable (#2620).
			return s.fullToolList()
		}
	}

	if unregistered || groupEmpty {
		return s.sentinelToolList(cwd, group, groupEmpty), nil
	}

	// Ambiguous or unambiguous match (including singleton fallback): return the full catalog.
	return s.fullToolList()
}

// sentinelEmptyInputSchema is the canonical MCP-compliant inputSchema for a
// no-argument tool. Strict MCP clients (e.g. Claude Code with Zod validation)
// reject a tool whose tools/list entry omits "inputSchema" entirely (#2257).
// We pre-marshal this once to avoid repeated allocations on every list call.
var sentinelEmptyInputSchema = json.RawMessage(`{"type":"object","properties":{},"required":[]}`)

// sentinelToolList returns the single archigraph_status sentinel entry with a
// context-aware description based on cwd, the (empty) group, and nearby groups.
func (s *Server) sentinelToolList(cwd, group string, groupEmpty bool) []MCPToolEntry {
	desc := sentinelToolDescription
	if groupEmpty && group != "" {
		desc = fmt.Sprintf("Archigraph: group %q is registered but has no repos indexed. Run `archigraph index` inside a repo in this group, then restart. (Mid-session registration requires restart — see #1772.)", group)
	}
	return []MCPToolEntry{
		{
			Name:        sentinelToolName,
			Description: desc,
			InputSchema: sentinelEmptyInputSchema,
		},
	}
}

// MCPToolEntry mirrors daemon.MCPToolEntry but is defined here to avoid an
// import cycle. The daemon package wraps these into its own MCPToolEntry type.
// Fields must stay in sync with daemon.MCPToolEntry.
type MCPToolEntry struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// fullToolList converts the registered tool map to MCPToolEntry values for
// wire serialisation. The tool map comes from the underlying mcp-go server.
// The sentinel tool (archigraph_status) is excluded from the full list — it
// is only surfaced when the cwd-gate fires (#1769).
func (s *Server) fullToolList() ([]MCPToolEntry, error) {
	toolMap := s.MCP.ListTools()

	names := make([]string, 0, len(toolMap))
	for n := range toolMap {
		if n == sentinelToolName {
			continue // exclude sentinel from full handshake
		}
		names = append(names, n)
	}
	// Stable sort so callers get deterministic output.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	out := make([]MCPToolEntry, 0, len(names))
	for _, name := range names {
		st := toolMap[name]
		raw, err := json.Marshal(st.Tool)
		if err != nil {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		entry := MCPToolEntry{Name: name}
		if v, ok := m["description"]; ok {
			_ = json.Unmarshal(v, &entry.Description)
		}
		if v, ok := m["inputSchema"]; ok {
			entry.InputSchema = v
		}
		out = append(out, entry)
	}
	return out, nil
}

// registerTools registers every tool handler on the MCP server.
// Source of truth: AddTool calls below — keep internal/mcp/SCHEMA.md in sync.
// Tool count: 43 (#1281: 9→4 bundles; #1293: desc trim; #1312: +quality_cycles; #1314: +auth_coverage;
//
//	#1322: +secrets; #1323: +test_coverage; #1333: desc ≤80 chars;
//	refactor/mcp-real-3k: ≤3k handshake; #2424: +cross_links; #2426: +save_finding,+list_findings;
//	#2427: +license_audit re-wired — no HTTP route found in internal/dashboard/;
//	#2474: +archigraph_persona_event persona lifecycle telemetry).
//
// Dropped (HTTP-only): archigraph_diagnostics, archigraph_quality_orphans,
//
//	archigraph_get_next_enrichment_task, archigraph_get_telemetry.
//
// Dropped (agent-facing, superseded): archigraph_recent_activity (superseded by MCPActivityBroker SSE/HTTP).
func (s *Server) registerTools() {
	s.MCP.AddTool(mcpapi.NewTool("archigraph_whoami",
		mcpapi.WithDescription("Infer group/repo/ref for caller (cwd_resolved_to, indexed_ref, is_worktree)."),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("ref"),
	), s.wrap("archigraph_whoami", s.handleWhoami))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_get_source",
		mcpapi.WithDescription("Return source for a node; accepts id, qualified_name, or label."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("context_lines", mcpapi.DefaultNumber(20)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_get_source", s.handleGetNodeSource))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find",
		mcpapi.WithDescription("BM25 graph query. cwd-repo default; cross_repo=true spans all. min_score>=0.15."),
		mcpapi.WithString("query", mcpapi.Required()),
		mcpapi.WithString("mode", mcpapi.DefaultString("bfs")),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(3)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithBoolean("cross_repo", mcpapi.DefaultBool(false)), // #2643: opt-in to search all repos
		mcpapi.WithBoolean("full", mcpapi.DefaultBool(false)),
		mcpapi.WithBoolean("include_noise", mcpapi.DefaultBool(false)),
		// verbose=true (default false), min_score (default 0.15), max_results (default 50, ceiling 200)
		// read from request map to stay under token ceiling (#1921 / #1807).
		mcpapi.WithArray("context_filter"), // #2318: documented in SCHEMA.md, was missing from schema declaration
		mcpapi.WithArray("fields"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref; defaults to CWD HEAD ref
	), s.wrap("archigraph_find", s.handleQueryGraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_inspect",
		mcpapi.WithDescription("Look up entity by id/qname/label; line-precise calls+called_by. verbose=true."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		// verbose=true (default false) read from request map to stay under token ceiling.
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithArray("fields"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"),                // PH1c: optional git ref; defaults to CWD HEAD ref
		mcpapi.WithAny("include_unresolved"), // #2640: when true, include unresolved calls[] with annotation
	), s.wrap("archigraph_inspect", s.handleGetNode))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_expand",
		mcpapi.WithDescription("Deprecated alias of archigraph_neighbors. Returns neighbors of entity_id."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		// 'node' kept as a deprecated alias for one release cycle (#1916).
		mcpapi.WithString("node"),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithArray("fields"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_expand", s.handleGetNeighbors))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_trace",
		mcpapi.WithDescription("Confidence-weighted shortest path between two nodes."),
		mcpapi.WithString("source", mcpapi.Required()),
		mcpapi.WithString("target", mcpapi.Required()),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_trace", s.handleShortestPath))

	// archigraph_traces — process-flow query surface (#724).
	// action=list|get|follow — defaults to "list" when omitted.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_traces",
		mcpapi.WithDescription("Process-flow traces. action=list|get|follow (default: list)."),
		mcpapi.WithString("action"),
		mcpapi.WithAny("process_id"),
		mcpapi.WithAny("entry_point_id"),
		mcpapi.WithNumber("max_depth", mcpapi.DefaultNumber(8)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		// min_steps, cross_stack_only, verbose (default false) are accepted as
		// optional args read from the request map to stay under token ceiling.
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_traces", s.handleTraces))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_clusters",
		mcpapi.WithDescription("List Louvain communities across loaded graphs."),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithNumber("top_entities_limit", mcpapi.DefaultNumber(3)), // #2318: added by PR #2310, was missing from schema
		mcpapi.WithNumber("min_size", mcpapi.DefaultNumber(20)),          // #2318: added by PR #2310, was missing from schema
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_clusters", s.handleListCommunities))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_stats",
		mcpapi.WithDescription("Corpus-level metrics. breakdown=unresolved_imports adds edge taxonomy."),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithString("breakdown", mcpapi.Description("Taxonomy breakdown. Supported: \"unresolved_imports\".")),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_stats", s.handleGraphStats))

	// archigraph_enrichments — action: list|submit|reject.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_enrichments",
		mcpapi.WithDescription("Enrichment candidates: list=pending, submit=resolve, reject=discard."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("kind"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithAny("candidate_id"),
		mcpapi.WithAny("value"),
		mcpapi.WithNumber("confidence", mcpapi.DefaultNumber(1)),
		mcpapi.WithAny("reason"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_enrichments", s.handleEnrichments))

	// archigraph_get_next_enrichment_task dropped (use enrichments(action=list,limit=1) instead).

	// archigraph_cross_links — action=list|accept|reject (#2424).
	// Per-action optional args read from the request map but undeclared to
	// stay under the token ceiling (#1639 pattern): channel, method,
	// limit, repo_filter, candidate_id, override_target.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_cross_links",
		mcpapi.WithDescription("Cross-repo link candidates: list=pending, accept|reject=resolve."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_cross_links", s.handleCrossLinks))

	// archigraph_repairs — action: list|submit. ADR-0015 residual-edge repair.
	// Submit-only optional args are read from request map but undeclared in
	// schema to keep the handshake token budget under its ceiling (#1639 pattern,
	// #1756): residual_id (string), resolution (string — bind_to_entity|
	// reclassify_as_external|reclassify_as_dynamic|reclassify_as_resolved|
	// abandon), target_entity_id (string), module (string), new_target (string),
	// dynamic_reason (string), abandon_reason (string), confidence (number 0-1),
	// reasoning (string), repo (string — override when residual_id is ambiguous),
	// source (string — audit tag, default "mcp_submit_repair").
	s.MCP.AddTool(mcpapi.NewTool("archigraph_repairs",
		mcpapi.WithDescription("Residual-edge repair queue: list=pending, submit=resolve."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(20)),
		mcpapi.WithNumber("offset", mcpapi.DefaultNumber(0)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_repairs", s.handleRepairs))

	// archigraph_apply_docgen_repairs — docgen→graph repair feedback loop (#1659).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_apply_docgen_repairs",
		mcpapi.WithDescription("Docgen feedback: apply repair candidates to graph enrichments."),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithBoolean("dry_run"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_apply_docgen_repairs", s.handleApplyDocgenRepairs))

	// archigraph_get_telemetry dropped (dashboard-only; use HTTP /api/telemetry instead).

	// archigraph_patterns — ADR-0018. action=query|record.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_patterns",
		mcpapi.WithDescription("Agent pattern store: query=find by task, record=store with exemplars."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithAny("text"),
		mcpapi.WithAny("category"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
		mcpapi.WithArray("steps"),
		mcpapi.WithArray("exemplars"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_patterns", s.handlePatterns))

	// archigraph_topology — message-channel topology (#1281). action=orphan_publishers|orphan_subscribers|topic_detail.
	// verbose=true (default false) read from request map to stay under token ceiling.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_topology",
		mcpapi.WithDescription("Message-channel topology: orphans and topic detail."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithAny("topic_id"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_topology", s.handleTopology))

	// archigraph_flows — flow-process diagnostics (#1281). action=dead_ends|truncated|detail.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_flows",
		mcpapi.WithDescription("Flow-process diagnostics: dead_ends, truncated, detail."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithAny("process_id"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_flows", s.handleFlows))

	// archigraph_diagnostics dropped (dashboard-only; use HTTP /api/diagnostics instead).

	// archigraph_quality_orphans dropped (measurement; use archigraph_find_dead_code for agents).

	// archigraph_graph_patterns — indexer-extracted patterns (#1281). action=list|get.
	// Distinct from archigraph_patterns (agent-learned store).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_graph_patterns",
		mcpapi.WithDescription("Indexer-extracted patterns (not agent store): list=browse, get=detail."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithBoolean("needs_attention", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("status"),
		mcpapi.WithNumber("confidence_min", mcpapi.DefaultNumber(0)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
		mcpapi.WithAny("pattern_id"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_graph_patterns", s.handleGraphPatterns))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_search_entities",
		mcpapi.WithDescription("Substring search over entity names; ranked matches with source locations."),
		mcpapi.WithString("query", mcpapi.Required()),
		mcpapi.WithAny("kind_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(30)),
		mcpapi.WithBoolean("include_noise", mcpapi.DefaultBool(false)),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithArray("fields"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_search_entities", s.handleSearchEntities))

	// archigraph_subgraph — unified subgraph tool (#1754).
	// Folds archigraph_get_subgraph + archigraph_summarize_subgraph into one
	// entry point; discriminated by format="raw"|"markdown".
	s.MCP.AddTool(mcpapi.NewTool("archigraph_subgraph",
		mcpapi.WithDescription("Nodes+edges within N hops (format=raw) or Markdown summary (format=markdown)."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
		mcpapi.WithString("format", mcpapi.DefaultString("raw")),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_subgraph", s.handleSubgraph))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_paths",
		mcpapi.WithDescription("Shortest path between two entities with confidence."),
		mcpapi.WithString("from", mcpapi.Required()),
		mcpapi.WithString("to", mcpapi.Required()),
		mcpapi.WithNumber("max_hops", mcpapi.DefaultNumber(5)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_find_paths", s.handleFindPaths))

	// archigraph_endpoints — HTTP surface (#1281, overhaul #1650, filter+dedupe #1745).
	// action=definitions|calls|stats; path_contains+method filter BEFORE limit.
	// format="terse" (default) returns one-line "lines" entries; "full" returns
	// per-record structs with kind + deduplicated properties (path/verb stripped).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_endpoints",
		mcpapi.WithDescription("HTTP endpoints: definitions|calls|stats. path_contains+method filter first."),
		mcpapi.WithString("action", mcpapi.Required()),
		mcpapi.WithBoolean("orphan_only", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(20)),
		mcpapi.WithNumber("offset", mcpapi.DefaultNumber(0)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithAny("path_contains"),
		mcpapi.WithAny("method"),
		mcpapi.WithAny("format"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_endpoints", s.handleEndpoints))

	// archigraph_neighbors — folds find_callers + find_callees into one tool
	// (#1753, #1742). direction=in returns callers, out returns callees, both
	// returns the union. find_callers / find_callees stay as deprecated aliases.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_neighbors",
		mcpapi.WithDescription("Graph neighbors of entity_id. direction=in|out|both."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithString("direction", mcpapi.DefaultString("both")),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithArray("fields"),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_neighbors", s.handleNeighbors))

	// verbose=true (default false) read from request map to stay under token ceiling.
	// Deprecated alias for archigraph_neighbors(direction=in) (#1753).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callers",
		mcpapi.WithDescription("Deprecated: use archigraph_neighbors(direction=in). Inbound callers."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_find_callers", s.handleFindCallers))

	// Deprecated alias for archigraph_neighbors(direction=out) (#1753).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callees",
		mcpapi.WithDescription("Deprecated: use archigraph_neighbors(direction=out). Outbound callees."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
		mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_find_callees", s.handleFindCallees))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_impact_radius",
		mcpapi.WithDescription("Inbound blast-radius: affected entities with risk_score [0,1]."),
		mcpapi.WithString("entity_id", mcpapi.Required()),
		mcpapi.WithNumber("hops", mcpapi.DefaultNumber(2)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_impact_radius", s.handleImpactRadius))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_find_dead_code",
		mcpapi.WithDescription("Entities with no project edges — dead code or extraction gap candidates."),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("kind_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_find_dead_code", s.handleFindDeadCode))

	// archigraph_quality_cycles — import cycle detection (#1312).
	// Runs Tarjan SCC on IMPORTS edges; each SCC > 1 = circular dependency.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_quality_cycles",
		mcpapi.WithDescription("Detect import cycles via Tarjan SCC; weakest edge, fix hint."),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_quality_cycles", s.handleQualityCycles))

	// archigraph_auth_coverage — security audit (#1314).
	// Walk all http_endpoint_definition entities and flag those without auth
	// decorators/middleware.  Severity: error (sensitive/IDOR), warn (public), info (covered).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_auth_coverage",
		mcpapi.WithDescription("Security audit: flag HTTP endpoints missing auth (severity, IDOR risk)."),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithBoolean("only_missing", mcpapi.DefaultBool(false)),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(200)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_auth_coverage", s.handleAuthCoverage))

	// #1323: test-coverage graph — link Test entities to the code they exercise.
	// #1774: entity_id param added for single-entity focused queries.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_test_coverage",
		mcpapi.WithDescription("Find production entities with no TESTS edge, ranked by severity."),
		mcpapi.WithString("entity_id"),
		mcpapi.WithArray("repo_filter"),
		mcpapi.WithAny("severity"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(100)),
		mcpapi.WithBoolean("top_directories", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_test_coverage", s.handleTestCoverage))

	// archigraph_module_analysis — module-level GDS (#1384, epic #1380).
	// action=cycles|centrality|all over the aggregated module graph: SCCs,
	// PageRank, betweenness. Bird's-eye view alongside entity-level tools.
	// Optional args read from request map but undeclared in schema to keep
	// the handshake token budget under its ceiling (#1639 pattern): top_n,
	// limit, min_size, repo_filter (slice form).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_module_analysis",
		mcpapi.WithDescription("Module-level SCC+PageRank+betweenness: cycles|centrality|all."),
		mcpapi.WithString("action", mcpapi.DefaultString("all")),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("ref"), // PH1c: optional git ref
	), s.wrap("archigraph_module_analysis", s.handleModuleAnalysis))

	// archigraph_secrets — hardcoded secret detector (#1322).
	// Walks source files; flags API keys, passwords, JWT tokens, and other
	// high-entropy credentials. Test fixtures and opt-out comments are suppressed.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_secrets",
		mcpapi.WithDescription("Scan for hardcoded secrets; masked findings by severity."),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
		mcpapi.WithAny("severity"),
		mcpapi.WithNumber("limit", mcpapi.DefaultNumber(200)),
	), s.wrap("archigraph_secrets", s.handleSecrets))

	// archigraph_save_finding + archigraph_list_findings (#2426).
	// Optional args read from the request map but undeclared to stay under
	// the token ceiling (#1639 pattern):
	//   save_finding: type, nodes, repo_filter.
	//   list_findings: since, entity_id, limit.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_save_finding",
		mcpapi.WithDescription("Persist a Q&A finding to the group memory store."),
		mcpapi.WithString("question", mcpapi.Required()),
		mcpapi.WithString("answer", mcpapi.Required()),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_save_finding", s.handleSaveResult))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_list_findings",
		mcpapi.WithDescription("List findings from the group memory store."),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_list_findings", s.handleListFindings))

	// archigraph_license_audit — re-wired (#2427): no HTTP route found in internal/dashboard/.
	// Optional args read from the request map but undeclared to stay under
	// the token ceiling (#1639 pattern): include_transitive, severity, limit.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_license_audit",
		mcpapi.WithDescription("Audit dependency licenses; flag GPL/AGPL conflicts."),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_license_audit", s.handleLicenseAudit))

	// archigraph_diff_refs — PH5 (#2093): compare two indexed git refs.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_diff_refs",
		mcpapi.WithDescription("Diff two indexed git refs: added/removed/modified entities + relationships."),
		mcpapi.WithString("group"),
		mcpapi.WithString("repo", mcpapi.Required()),
		mcpapi.WithString("ref_a", mcpapi.Required()),
		mcpapi.WithString("ref_b", mcpapi.Required()),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_diff_refs", s.handleDiffRefs))

	// archigraph_docgen_* — docgen-via-local-staging tools (epic #2207, issue #2214).
	// start_run: create or resume a per-group staging run.
	// status:    inspect in-flight run (files written + SHAs).
	// validate:  lint frontmatter + cross-links (read-only).
	// promote:   atomic staging → canonical rename; SSG guard.
	// abort:     rm -rf staging, release lock.
	// list:      read-only enumeration of canonical docs.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_start_run",
		mcpapi.WithDescription("Start or resume a docgen staging run for a group. Returns run_id + staging_path."),
		mcpapi.WithString("group", mcpapi.Required()),
		mcpapi.WithBoolean("resume", mcpapi.DefaultBool(true)),
		mcpapi.WithBoolean("no_git", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_start_run", s.handleDocgenStartRun))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_status",
		mcpapi.WithDescription("Inspect an in-flight docgen run: files written, SHA-256 per file."),
		mcpapi.WithString("run_id", mcpapi.Required()),
		mcpapi.WithBoolean("no_git", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_status", s.handleDocgenStatus))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_validate",
		mcpapi.WithDescription("Validate staging run: frontmatter + cross-links. Read-only, no file writes."),
		mcpapi.WithString("run_id", mcpapi.Required()),
		mcpapi.WithBoolean("no_git", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_validate", s.handleDocgenValidate))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_promote",
		mcpapi.WithDescription("Atomic promote: staging → canonical. Blocks SSG scaffolding. Rotates previous."),
		mcpapi.WithString("run_id", mcpapi.Required()),
		mcpapi.WithBoolean("force", mcpapi.DefaultBool(false)),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_promote", s.handleDocgenPromote))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_abort",
		mcpapi.WithDescription("Abort a staging run: rm -rf staging, release per-group lock. Canonical safe."),
		mcpapi.WithString("run_id", mcpapi.Required()),
		mcpapi.WithAny("group"),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_abort", s.handleDocgenAbort))

	s.MCP.AddTool(mcpapi.NewTool("archigraph_docgen_list",
		mcpapi.WithDescription("List canonical doc files for a group under ~/.archigraph/docs/<group>/."),
		mcpapi.WithString("group", mcpapi.Required()),
		mcpapi.WithAny("cwd"),
	), s.wrap("archigraph_docgen_list", s.handleDocgenList))

	// archigraph_persona_event — persona lifecycle telemetry (#2474).
	// Personas call this at session start (event_type=invoke) and on each
	// Consult-Out (event_type=consult_out, target_persona=<name>). Group-agnostic.
	// Appends to ~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl (LOCAL ONLY).
	s.MCP.AddTool(mcpapi.NewTool("archigraph_persona_event",
		mcpapi.WithDescription("Record a persona lifecycle event (invoke/consult_out/save_finding). LOCAL ONLY."),
		mcpapi.WithString("persona", mcpapi.Required()),
		mcpapi.WithString("event_type", mcpapi.Required()),
		mcpapi.WithAny("target_persona"),
		mcpapi.WithNumber("depth", mcpapi.DefaultNumber(0)),
		mcpapi.WithArray("chain"),
		mcpapi.WithAny("metadata"),
	), s.wrap("archigraph_persona_event", s.handlePersonaEvent))

	// archigraph_mcp_metrics — per-tool session metrics + daily rollup (#2192).
	// Returns in-memory per-tool counters (calls, errors, p50/p95 ms) for the
	// current daemon session, plus up to N days of persisted daily rollups from
	// ~/.archigraph/metrics/. Group-agnostic; no cwd routing needed.
	s.MCP.AddTool(mcpapi.NewTool("archigraph_mcp_metrics",
		mcpapi.WithDescription("Current session tool-call metrics (counts, p50/p95 ms) + last N days rollups."),
		mcpapi.WithNumber("days", mcpapi.DefaultNumber(3)),
	), s.wrap("archigraph_mcp_metrics", s.handleMCPMetrics))

	// archigraph_status — cwd-gate sentinel (#1769).
	// Registered as a real callable tool so agents can invoke it and receive
	// guidance. Excluded from the full handshake returned to indexed sessions
	// (see fullToolList). Shown ONLY when cwd is outside all registered groups.
	s.MCP.AddTool(mcpapi.NewTool(sentinelToolName,
		mcpapi.WithDescription(sentinelToolDescription),
	), s.wrap(sentinelToolName, s.handleStatus))
}

// handleMCPMetrics is the handler for archigraph_mcp_metrics (#2192).
//
// Returns current in-memory session metrics (per-tool call count, error rate,
// p50/p95 latency) plus the last `days` days of persisted daily rollup records
// from ~/.archigraph/metrics/mcp-YYYY-MM-DD.jsonl.
//
// The tool is safe to call at any time; it never modifies state.
func (s *Server) handleMCPMetrics(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	days := argInt(req, "days", 3)
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}

	snap := s.SessMet.Snapshot()

	// Read rollup history. Best-effort: on error return what we have.
	rollups, _ := ReadRollups(s.SessMet.metricsDir, days)

	return jsonResult(map[string]any{
		"session":        snap,
		"rollup_days":    days,
		"rollup_records": rollups,
		"rollup_dir":     s.SessMet.metricsDir,
	}), nil
}

// handleStatus is the handler for archigraph_status (#1769). It returns a
// human-readable paragraph explaining the caller's cwd relative to the
// registered groups, and what action would resolve the mismatch.
func (s *Server) handleStatus(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cwd := s.inferCWD(req)
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	group, candidates := groupFromRegistryWithCandidates(s.State, cwd)

	var msg string
	switch {
	case len(s.State.registry.Groups) == 0:
		msg = fmt.Sprintf("Archigraph has no groups registered. Run `archigraph install` inside a repo, then restart Claude Code. (cwd: %s)", cwd)
	case group != "":
		// Group resolved — check if it has repos.
		if grp, ok := s.State.registry.Groups[group]; !ok || len(grp.Repos) == 0 {
			msg = fmt.Sprintf("Archigraph: cwd %q resolves to group %q, but that group has no repos indexed. Run `archigraph index` inside a repo in this group, then restart.", cwd, group)
		} else {
			msg = fmt.Sprintf("Archigraph: cwd %q is covered by group %q (%d repo(s)). The full tool set is available — call archigraph_whoami for details.", cwd, group, len(grp.Repos))
		}
	case len(candidates) > 1:
		msg = fmt.Sprintf("Archigraph: cwd %q matches multiple groups (%v). Pass group= explicitly to any tool to disambiguate.", cwd, candidates)
	default:
		// Build a list of registered group names for guidance.
		known := make([]string, 0, len(s.State.registry.Groups))
		for g := range s.State.registry.Groups {
			known = append(known, g)
		}
		msg = fmt.Sprintf("Archigraph: cwd %q is not under any registered group (registered: %v). cd into a registered repo or run `archigraph install` here. Note: new groups registered mid-session are not reflected until restart (#1772).", cwd, known)
	}

	// Append the MCP + grep pairing philosophy so agents onboarding via
	// archigraph_status understand how to use MCP alongside grep (#1836).
	const pairingPhilosophy = "\n\n" +
		"Pairing philosophy — " +
		"archigraph MCP gives you a navigable, accurate map of the code; grep gives you raw pattern matches. " +
		"Use MCP for structural questions: who calls X? what is the flow? where does Y live in the graph? " +
		"Use grep for raw enumeration: every `if err != nil`, every import line, every TODO. " +
		"Pair them: MCP narrows the search space; grep verifies edge-property questions MCP can't answer yet."
	return mcpapi.NewToolResultText(msg + pairingPhilosophy), nil
}

// wrap is the shared handler middleware: telemetry + lazy reload + panic guard
// + MCP activity event emission (epic #1157, Phase 1).
func (s *Server) wrap(name string, fn func(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)) mcpsrv.ToolHandlerFunc {
	return func(ctx context.Context, req mcpapi.CallToolRequest) (res *mcpapi.CallToolResult, err error) {
		start := time.Now()
		end := s.Tel.Begin(name)
		defer func() {
			isErr := err != nil || (res != nil && res.IsError)
			end(isErr)
			// Record into session metrics (#2192). Done in defer so elapsed
			// is measured over the full call including the deferred cleanup.
			if s.SessMet != nil {
				s.SessMet.Record(name, time.Since(start), isErr)
			}
		}()
		s.reloadBeforeCall()
		// Install a per-call id collector so render helpers can record the
		// entity ids they surface (markdown tools have no machine-readable ids
		// in their wire output). emitActivity drains it afterwards.
		ctx, collector := withIDCollector(ctx)
		res, err = fn(ctx, req)
		// #1650/#1687: stamp every tool payload with elapsed_ms — including error
		// responses — so callers can benchmark latency regardless of outcome.
		// Success responses: JSON object/array or non-JSON text (see appendElapsedTrailer).
		// Error responses: plain-text content; we append the trailing comment so
		// the same regex parser works for both paths.
		elapsed := time.Since(start).Milliseconds()
		if res != nil {
			fl := fieldsArg(req)
			// #2287 + #2328: single-marshal path. When the handler used
			// jsonResult(v), v is carried on res.StructuredContent.
			// finalizeDeferred builds the envelope on the structured
			// value (apply fields=, TOON-encode items, inject
			// elapsed_ms) and marshals ONCE here, replacing the eager
			// marshal in res.Content[0]. This eliminates the legacy
			// parse step entirely — including the fields= case, which
			// post-#2328 also rides the fast path via reflection-aware
			// applyFieldsToValue.
			//
			// Drain the deferred slot so StructuredContent does not
			// leak into the wire envelope (omitempty is set but we
			// clear it for safety).
			deferredV, hasDeferred := takeDeferred(res)
			if hasDeferred {
				if text, ferr := finalizeDeferred(deferredV, elapsed, fl); ferr == nil {
					res.Content = []mcpapi.Content{mcpapi.NewTextContent(text)}
				} else {
					// finalize shouldn't fail for shapes that survived
					// jsonResult's eager marshal, but if it does fall
					// back to an error result so the caller learns.
					res = mcpapi.NewToolResultError("marshal: " + ferr.Error())
				}
			} else {
				// Non-deferred fallback (markdown handlers, error
				// results, hand-built TextContent). The only remaining
				// responsibility is appending an elapsed_ms trailer to
				// plain-text bodies (errors and markdown). JSON-shaped
				// results ride the deferred fast path (above) and never
				// reach here in production.
				res = appendElapsedTrailer(res, elapsed)
			}
			// #1740: intern repeated entity IDs to short handles (@1, @2, …).
			// Runs after elapsed-ms injection so the _id_table lands in the
			// same top-level JSON object that carries elapsed_ms.
			// Opt-out: MCP_NO_ID_INTERNING=1.
			res = applyIDInterning(res)
		}
		s.emitActivity(ctx, name, req, res, collector)
		return res, err
	}
}

// appendElapsedTrailer appends a `# elapsed_ms=N` trailing comment line to the
// first TextContent item in res. Used for non-deferred results (markdown
// handlers, error responses) where the payload is plain text, not JSON.
// JSON-shaped results always ride the deferred fast path (finalizeDeferred)
// and never reach this function in production.
func appendElapsedTrailer(res *mcpapi.CallToolResult, ms int64) *mcpapi.CallToolResult {
	if res == nil || len(res.Content) == 0 {
		return res
	}
	for i, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok || tc.Text == "" {
			continue
		}
		res.Content[i] = mcpapi.NewTextContent(tc.Text + fmt.Sprintf("\n# elapsed_ms=%d\n", ms))
		return res
	}
	return res
}

// emitActivity publishes a MCPActivityEvent to the activity broker (when
// wired). It is called after every tool handler returns. The agent_id is
// derived from the "archigraph-agent-id" context value when set, or falls
// back to the User-Agent extracted at session accept time.
func (s *Server) emitActivity(_ context.Context, toolName string, req mcpapi.CallToolRequest, res *mcpapi.CallToolResult, collector *idCollector) {
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

	if res != nil && !res.IsError {
		// Resolve the touched entity ids in priority order:
		//  1. Ids explicitly recorded by the handler / render helpers (covers
		//     markdown-formatted tools like archigraph_find whose wire output
		//     carries no machine-readable ids).
		//  2. Ids parsed out of a JSON result body (covers structured tools).
		//  3. The request's own id-bearing arguments (covers single-entity
		//     tools like inspect / get_source / expand even when neither of the
		//     above fires).
		nodeIDs, edgeIDs := collector.drain()
		jn, je := extractIDs(res)
		nodeIDs = append(nodeIDs, jn...)
		edgeIDs = append(edgeIDs, je...)
		nodeIDs = append(nodeIDs, idsFromArgs(args)...)
		event.ReturnedNodeIDs = dedup(nodeIDs)
		event.ReturnedEdgeIDs = dedup(edgeIDs)
	}
	s.activityBroker.Publish(event)
}

// idsFromArgs harvests entity ids that the caller passed in as arguments.
// These are the exact nodes a single-entity tool (inspect, get_source,
// expand, impact_radius, …) operated on, so they are a sound fallback when
// the rendered result exposes no ids of its own. Free-text fields such as
// "question" or "label" are intentionally excluded — they are not ids.
func idsFromArgs(args map[string]any) []string {
	var out []string
	for _, k := range []string{
		"node_id", "entity_id", "target_entity_id", "from_id", "to_id",
	} {
		if v, ok := args[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	// label_or_id is an id only when it carries the "<repo>::<local>" prefix;
	// a bare label would not match any graph node id, so skip it then.
	if v, ok := args["label_or_id"].(string); ok && v != "" {
		if r, _ := splitPrefixed(v); r != "" {
			out = append(out, v)
		}
	}
	return out
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
			"id", "entity_id", "node_id", "pattern_id", "topic_id", "process_id")...)
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
			for _, field := range []string{"id", "entity_id", "node_id", "from_id", "to_id", "pattern_id", "topic_id", "process_id"} {
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
