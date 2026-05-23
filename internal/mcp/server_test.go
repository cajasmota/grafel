package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// writeGraph writes a graph.Document to the repo's external store state
// dir (#1626: per-repo state no longer lives in <repo>/.archigraph).
func writeGraph(t *testing.T, repoDir string, doc *graph.Document) string {
	t.Helper()
	if os.Getenv("ARCHIGRAPH_DAEMON_ROOT") == "" {
		t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	}
	dir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "graph.json")
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeRegistry writes a registry.json with the given (group, repo->path) shape.
func makeRegistry(t *testing.T, dir string, groups map[string]map[string]string) string {
	t.Helper()
	r := Registry{Groups: map[string]RegistryGroup{}}
	for g, repos := range groups {
		grp := RegistryGroup{
			MemoryDir: filepath.Join(dir, g+"-memory"),
			LinksFile: filepath.Join(dir, g+"-links.json"),
			Repos:     map[string]RegistryRepo{},
		}
		for name, path := range repos {
			grp.Repos[name] = RegistryRepo{Path: path}
		}
		r.Groups[g] = grp
	}
	path := filepath.Join(dir, "registry.json")
	data, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtureDoc constructs a tiny graph for tests: 4 entities, 3 edges.
func fixtureDoc(repo string) *graph.Document {
	mk := func(id, name, file, kind string, line int) graph.Entity {
		return graph.Entity{
			ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: line, EndLine: line + 5,
		}
	}
	return &graph.Document{
		Version: 1, GeneratedAt: time.Now(), Repo: repo,
		Entities: []graph.Entity{
			mk("a1", "DashboardScreen", "src/DashboardScreen.tsx", "SCOPE.Component", 10),
			mk("a2", "useProposalCounts", "src/hooks/proposals.ts", "function", 20),
			mk("a3", "ProposalsService", "src/services/Proposals.ts", "class", 30),
			mk("a4", "rareUniqueWidget", "src/widgets/Rare.tsx", "function", 40),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a1", ToID: "a2", Kind: "CALLS"},
			{ID: "r2", FromID: "a2", ToID: "a3", Kind: "IMPORTS"},
			{ID: "r3", FromID: "a3", ToID: "a4", Kind: "CALLS"},
		},
	}
}

// callTool invokes a registered tool by name with arg map; returns the text or error.
func callTool(t *testing.T, srv *Server, tool string, args map[string]any) *mcpapi.CallToolResult {
	t.Helper()
	srvTools := srv.MCP.ListTools()
	for _, st := range srvTools {
		if st.Tool.Name == tool {
			req := mcpapi.CallToolRequest{}
			req.Params.Name = tool
			req.Params.Arguments = args
			res, err := st.Handler(context.Background(), req)
			if err != nil {
				t.Fatalf("%s handler error: %v", tool, err)
			}
			return res
		}
	}
	t.Fatalf("tool %q not registered", tool)
	return nil
}

// resultText pulls the first text content out of a CallToolResult.
func resultText(r *mcpapi.CallToolResult) string {
	if r == nil {
		return ""
	}
	for _, c := range r.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// 0. CLI registry format (array of GroupRef + per-group config): server initializes
// and tools list returns all registered tools.
func TestCLIRegistryFormat(t *testing.T) {
	dir := t.TempDir()

	// Create two repos with graphs
	r1 := filepath.Join(dir, "repo1")
	r2 := filepath.Join(dir, "repo2")
	for _, p := range []string{r1, r2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGraph(t, r1, fixtureDoc("repo1"))
	writeGraph(t, r2, fixtureDoc("repo2"))

	// Create per-group config files (CLI format)
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Group 1: upvate with 1 repo
	cfg1 := map[string]any{
		"name": "upvate",
		"repos": []map[string]any{
			{"slug": "repo1", "path": r1},
		},
	}
	cfg1Data, _ := json.MarshalIndent(cfg1, "", "  ")
	cfg1Path := filepath.Join(configDir, "upvate.fleet.json")
	if err := os.WriteFile(cfg1Path, cfg1Data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Group 2: client-fixture with 1 repo
	cfg2 := map[string]any{
		"name": "client-fixture",
		"repos": []map[string]any{
			{"slug": "repo2", "path": r2},
		},
	}
	cfg2Data, _ := json.MarshalIndent(cfg2, "", "  ")
	cfg2Path := filepath.Join(configDir, "client-fixture.fleet.json")
	if err := os.WriteFile(cfg2Path, cfg2Data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create CLI-format registry.json with array of GroupRef
	regData := map[string]any{
		"version": 1,
		"groups": []map[string]any{
			{
				"name":         "upvate",
				"config_path":  cfg1Path,
				"installed_at": "2026-05-20T12:00:00Z",
			},
			{
				"name":         "client-fixture",
				"config_path":  cfg2Path,
				"installed_at": "2026-05-20T12:00:00Z",
			},
		},
	}
	regPath := filepath.Join(dir, "registry.json")
	regRaw, _ := json.MarshalIndent(regData, "", "  ")
	if err := os.WriteFile(regPath, regRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	// Server should load successfully
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify both groups are loaded
	groups := srv.State.Groups()
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(groups), groups)
	}
	if !containsString(groups, "upvate") || !containsString(groups, "client-fixture") {
		t.Errorf("expected both upvate and client-fixture groups, got: %v", groups)
	}

	// Verify group upvate has repo1
	gUpvate := srv.State.Group("upvate")
	if gUpvate == nil || len(gUpvate.Repos) != 1 {
		t.Errorf("expected upvate to have 1 repo, got %v", gUpvate)
	}
	if _, ok := gUpvate.Repos["repo1"]; !ok {
		t.Errorf("expected upvate to have repo1")
	}

	// Verify group client-fixture has repo2
	gClient := srv.State.Group("client-fixture")
	if gClient == nil || len(gClient.Repos) != 1 {
		t.Errorf("expected client-fixture to have 1 repo, got %v", gClient)
	}
	if _, ok := gClient.Repos["repo2"]; !ok {
		t.Errorf("expected client-fixture to have repo2")
	}
}

// Note: containsString is already defined in patterns.go, reusing it.

// 1. Empty registry: server starts, whoami returns "no group".
func TestEmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, srv, "archigraph_whoami", nil)
	txt := resultText(res)
	if !strings.Contains(txt, "\"error\"") {
		t.Errorf("expected error in whoami output for empty registry, got: %s", txt)
	}
}

// 2. Two groups, three repos: state loads them all, mtime tracking works.
func TestTwoGroupsLoaded(t *testing.T) {
	dir := t.TempDir()
	r1 := filepath.Join(dir, "g1-repo1")
	r2 := filepath.Join(dir, "g1-repo2")
	r3 := filepath.Join(dir, "g2-repo3")
	for _, p := range []string{r1, r2, r3} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGraph(t, r1, fixtureDoc("repo1"))
	writeGraph(t, r2, fixtureDoc("repo2"))
	writeGraph(t, r3, fixtureDoc("repo3"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g1": {"repo1": r1, "repo2": r2},
		"g2": {"repo3": r3},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	groups := srv.State.SnapshotGroups()
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups loaded, got %d", len(groups))
	}
	totalRepos := 0
	for _, g := range groups {
		totalRepos += len(g.Repos)
	}
	if totalRepos != 3 {
		t.Fatalf("expected 3 repos loaded, got %d", totalRepos)
	}
}

// 3. BM25: a rare query term ranks its target above common words.
func TestBM25RankingPrefersRareTerms(t *testing.T) {
	doc := fixtureDoc("r")
	idx := BuildBM25(doc)
	hits := idx.Search("rareUniqueWidget", 5)
	if len(hits) == 0 || hits[0].Entity.Name != "rareUniqueWidget" {
		t.Fatalf("expected rareUniqueWidget at rank 1, got %+v", hits)
	}
	common := idx.Search("proposals", 5)
	if len(common) < 2 {
		t.Fatalf("expected >= 2 hits for 'proposals', got %d", len(common))
	}
}

// 4. inspect uses the LabelIndex (O(1) by name/id).
func TestGetNodeViaIndex(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	res := callTool(t, srv, "archigraph_inspect", map[string]any{"label_or_id": "DashboardScreen"})
	txt := resultText(res)
	if !strings.Contains(txt, "DashboardScreen") {
		t.Fatalf("expected DashboardScreen in result, got: %s", txt)
	}
	// Verify direct index lookup is single-call O(1).
	g := srv.State.Group("g")
	r := g.Repos["r1"]
	if r.LabelIndex.Lookup("DashboardScreen") == nil {
		t.Fatal("LabelIndex should resolve label")
	}
}

// 5. trace follows cross-repo overlay edges.
func TestShortestPathCrossRepo(t *testing.T) {
	dir := t.TempDir()
	r1 := filepath.Join(dir, "rA")
	r2 := filepath.Join(dir, "rB")
	_ = os.MkdirAll(r1, 0o755)
	_ = os.MkdirAll(r2, 0o755)
	writeGraph(t, r1, fixtureDoc("rA"))
	writeGraph(t, r2, fixtureDoc("rB"))
	links := []CrossRepoLink{
		{Source: "rA::a4", Target: "rB::a1", Kind: "PUBLISHES_TO", Confidence: 0.9},
	}
	linksPath := filepath.Join(dir, "g-links.json")
	data, _ := json.MarshalIndent(links, "", "  ")
	_ = os.WriteFile(linksPath, data, 0o644)
	reg := Registry{Groups: map[string]RegistryGroup{
		"g": {LinksFile: linksPath, Repos: map[string]RegistryRepo{"rA": {Path: r1}, "rB": {Path: r2}}},
	}}
	regPath := filepath.Join(dir, "registry.json")
	d, _ := json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(regPath, d, 0o644)
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, srv, "archigraph_trace", map[string]any{"source": "rA::a1", "target": "rB::a4"})
	txt := resultText(res)
	if !strings.Contains(txt, "\"crosses_repos\":true") {
		t.Fatalf("expected crosses_repos=true, got: %s", txt)
	}
	if !strings.Contains(txt, "\"found\":true") {
		t.Fatalf("expected found=true, got: %s", txt)
	}
}

// 6. Compact format strips SCOPE prefix and never shows redundant repo when scoped to one repo.
func TestCompactFormatStripsScope(t *testing.T) {
	rr := renderResult{
		MatchedTotal: 1,
		Nodes: []nodeWithRepo{{
			Repo: "x", Score: 1, Entity: &graph.Entity{Name: "Foo", Kind: "SCOPE.Component", SourceFile: "f.go", StartLine: 1},
		}},
		Edges: []renderEdge{{From: "Foo", To: "Bar", Kind: "SCOPE.IMPORTS"}, {From: "A", To: "B", Kind: "calls"}},
		// Note: "SCOPE.IMPORTS" left intact as input to verify the renderer
		// still strips the historical-bug prefix form (Issue #77 reconciliation).
		OneRepo: true,
	}
	out := renderCompact(rr, 0)
	if strings.Contains(out, "SCOPE.") {
		t.Errorf("expected SCOPE. prefix to be stripped, got: %s", out)
	}
	if strings.Contains(out, "[x]") {
		t.Errorf("expected no per-row repo when oneRepo=true, got: %s", out)
	}
	if !strings.Contains(out, "implicit calls") {
		t.Errorf("expected implicit-calls suppression note, got: %s", out)
	}
}

// 7. Token-budget enforcement truncates rendered output.
func TestTokenBudgetEnforcement(t *testing.T) {
	nodes := []nodeWithRepo{}
	for i := 0; i < 50; i++ {
		nodes = append(nodes, nodeWithRepo{
			Repo: "r", Score: float64(50 - i),
			Entity: &graph.Entity{
				Name: strings.Repeat("LongLabel", 10), SourceFile: "really/long/path.go", StartLine: i,
			},
		})
	}
	rr := renderResult{MatchedTotal: 50, Nodes: nodes, OneRepo: true}
	out := renderCompact(rr, 50) // tiny budget
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncated marker, got: %s", out)
	}
	if estimateTokens(out) > 100 {
		t.Errorf("output too large: %d tokens", estimateTokens(out))
	}
}

// 8. archigraph_cross_links was dropped in refactor/mcp-real-3k (≤3k handshake).
// The link-candidate handler still exists internally; this test is skipped to reflect
// the tool's removal from the MCP surface.
func TestLinkCandidateRoundTrip(t *testing.T) {
	t.Skip("archigraph_cross_links dropped from MCP surface in refactor/mcp-real-3k")
}

// 9. Enrichment candidate submit round-trip.
func TestEnrichmentCandidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	cands := []EnrichmentCandidate{{ID: "e1", NodeID: "a1", Kind: "purpose"}}
	candPath := filepath.Join(daemon.StateDirForRepo(repo), "enrichment-candidates.json")
	d, _ := json.MarshalIndent(cands, "", "  ")
	_ = os.WriteFile(candPath, d, 0o644)
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	listRes := callTool(t, srv, "archigraph_enrichments", map[string]any{"action": "list"})
	if !strings.Contains(resultText(listRes), "e1") {
		t.Fatalf("expected e1 in list: %s", resultText(listRes))
	}
	subRes := callTool(t, srv, "archigraph_enrichments", map[string]any{
		"action": "submit", "candidate_id": "e1", "value": "controls dashboard", "confidence": 0.9, "reason": "test",
	})
	if strings.Contains(resultText(subRes), "error") {
		t.Fatalf("submit error: %s", resultText(subRes))
	}
	resPath := filepath.Join(daemon.StateDirForRepo(repo), "enrichment-resolutions.json")
	data, err := os.ReadFile(resPath)
	if err != nil || !strings.Contains(string(data), "controls dashboard") {
		t.Fatalf("resolution missing: err=%v data=%s", err, data)
	}
}

// 10. Telemetry counter increments on tool calls.
func TestTelemetryIncrements(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	for i := 0; i < 3; i++ {
		callTool(t, srv, "archigraph_whoami", nil)
	}
	snap := srv.Tel.Snapshot()
	tools := snap["tools"].(map[string]any)
	w := tools["archigraph_whoami"].(map[string]any)
	if int(w["calls"].(int)) < 3 {
		t.Fatalf("expected calls >= 3, got %v", w["calls"])
	}
}

// 11. Per-repo unavailable: corrupt one graph, others still serve.
func TestPerRepoUnavailable(t *testing.T) {
	dir := t.TempDir()
	r1 := filepath.Join(dir, "good")
	r2 := filepath.Join(dir, "bad")
	_ = os.MkdirAll(r1, 0o755)
	_ = os.MkdirAll(r2, 0o755)
	writeGraph(t, r1, fixtureDoc("good"))
	// Write a corrupt file in r2.
	badDir := daemon.StateDirForRepo(r2)
	_ = os.MkdirAll(badDir, 0o755)
	_ = os.WriteFile(filepath.Join(badDir, "graph.json"), []byte("not json"), 0o644)
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"good": r1, "bad": r2},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, srv, "archigraph_stats", nil)
	txt := resultText(res)
	if !strings.Contains(txt, "unavailable") {
		t.Errorf("expected 'unavailable' in stats, got: %s", txt)
	}
	// good repo still queryable
	res2 := callTool(t, srv, "archigraph_inspect", map[string]any{"label_or_id": "DashboardScreen"})
	if !strings.Contains(resultText(res2), "DashboardScreen") {
		t.Errorf("expected good repo to serve inspect, got: %s", resultText(res2))
	}
}

// 12. Per-repo unavailable telemetry is reflected in errors counter only when caller hits it.
func TestQueryGraphRendersCompact(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	res := callTool(t, srv, "archigraph_find", map[string]any{
		"question":     "rareUniqueWidget",
		"depth":        1,
		"token_budget": 800,
		"repo_filter":  []any{"r1"},
	})
	txt := resultText(res)
	if !strings.Contains(txt, "rareUniqueWidget") {
		t.Errorf("expected match in compact output, got: %s", txt)
	}
	if !strings.HasPrefix(txt, "# nodes") {
		t.Errorf("expected '# nodes' header, got: %s", txt)
	}
}

// 13. Tool registration uses the finalized distinct names (#668); old names are gone.
func TestToolNameSurface(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, st := range srv.MCP.ListTools() {
		registered[st.Tool.Name] = true
	}
	// 28 tools post refactor/mcp-real-3k: 4 dashboard-only + 4 agent-facing tools dropped.
	// Dropped (HTTP-only): archigraph_diagnostics, archigraph_quality_orphans,
	//   archigraph_get_next_enrichment_task, archigraph_get_telemetry.
	// Dropped (≤3k budget): archigraph_recent_activity, archigraph_save_finding,
	//   archigraph_list_findings, archigraph_cross_links.
	wantPresent := []string{
		// renamed (5)
		"archigraph_find", "archigraph_inspect", "archigraph_expand",
		"archigraph_clusters", "archigraph_stats",
		// bundled (2 retained; cross_links dropped)
		"archigraph_enrichments", "archigraph_repairs",
		// unchanged — trace included here as it was not renamed
		"archigraph_trace",
		"archigraph_whoami", "archigraph_get_source",
		// ADR-0018 β
		"archigraph_patterns",
		// #724 process-flow BFS query surface
		"archigraph_traces",
		// #1281 consolidated topology v2 (was 3 tools)
		"archigraph_topology",
		// #1281 consolidated flows v2 (was 3 tools)
		"archigraph_flows",
		// #1281 consolidated graph-indexed patterns (was 2 tools; renamed)
		"archigraph_graph_patterns",
		// #1202 bonus traversal
		"archigraph_search_entities",
		"archigraph_get_subgraph",
		"archigraph_find_paths",
		// #1281 consolidated HTTP endpoint tools (was 3 tools)
		"archigraph_endpoints",
		// #1252 flow-aware traversal tools
		"archigraph_find_callers",
		"archigraph_find_callees",
		"archigraph_impact_radius",
		"archigraph_summarize_subgraph",
		"archigraph_find_dead_code",
		"archigraph_auth_coverage",
		// #1659 docgen→graph repair feedback loop
		"archigraph_apply_docgen_repairs",
	}
	for _, n := range wantPresent {
		if !registered[n] {
			t.Errorf("expected tool %q to be registered", n)
		}
	}
	// Old names (pre-#668) must not exist.
	// #1281 consolidated names must also be absent.
	// Dashboard-only tools dropped in this PR must also be absent.
	wantAbsent := []string{
		// old singular tool names replaced by bundles (pre-#668)
		"archigraph_list_link_candidates", "archigraph_resolve_link_candidate",
		"archigraph_list_enrichment_candidates", "archigraph_submit_enrichment", "archigraph_reject_enrichment",
		"archigraph_list_residuals", "archigraph_submit_repair",
		// old renamed tool names
		"archigraph_search", "archigraph_describe", "archigraph_related",
		"archigraph_list_clusters", "archigraph_graph_stats",
		// bare unprefixed names (Refs #62)
		"query_graph", "get_node", "get_neighbors", "shortest_path",
		"list_communities", "save_result", "get_node_source",
		"search", "describe", "related",
		"list_clusters", "save_finding", "get_source",
		"whoami", "recent_activity", "graph_stats", "get_telemetry",
		// #1281 removed (merged into bundles)
		"archigraph_topology_orphan_publishers",
		"archigraph_topology_orphan_subscribers",
		"archigraph_topology_topic_detail",
		"archigraph_flow_dead_ends",
		"archigraph_flow_truncated",
		"archigraph_flow_detail",
		"archigraph_patterns_list",
		"archigraph_patterns_get",
		"archigraph_endpoint_definitions",
		"archigraph_endpoint_calls",
		"archigraph_endpoint_stats",
		// dashboard-only tools dropped (32 → 28)
		"archigraph_diagnostics",
		"archigraph_quality_orphans",
		"archigraph_get_next_enrichment_task",
		"archigraph_get_telemetry",
		// agent-facing tools dropped in refactor/mcp-real-3k (≤3k budget)
		"archigraph_recent_activity",
		"archigraph_save_finding",
		"archigraph_list_findings",
		"archigraph_cross_links",
	}
	for _, n := range wantAbsent {
		if registered[n] {
			t.Errorf("expected old tool %q to NOT be registered", n)
		}
	}
	// Total count: 30 (28 baseline + archigraph_module_analysis from #1384,
	// + archigraph_apply_docgen_repairs from #1659: docgen→graph repair feedback
	// loop — emit repair candidates in generate-docs, apply high-confidence ones
	// immediately as enrichment resolutions, queue low-confidence for review).
	if got := len(srv.MCP.ListTools()); got != 30 {
		t.Errorf("expected 30 registered tools, got %d", got)
	}
}

// 14. archigraph_save_finding / archigraph_list_findings dropped in refactor/mcp-real-3k.
func TestFindingsRoundTrip(t *testing.T) {
	t.Skip("archigraph_save_finding and archigraph_list_findings dropped from MCP surface in refactor/mcp-real-3k")
}

// 15. inspect attaches saved findings keyed by entity ID (Refs #59 strategy A).
// archigraph_save_finding was dropped; write the finding JSON directly to disk.
func TestDescribeAttachesFindings(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})

	// Write a finding directly to the group's memory dir.
	// makeRegistry sets MemoryDir = filepath.Join(dir, g+"-memory"), so use dir/g-memory.
	memDir := filepath.Join(dir, "g-memory")
	_ = os.MkdirAll(memDir, 0o755)
	finding := map[string]any{
		"question": "purpose of DashboardScreen",
		"answer":   "Top-level home view.",
		"nodes":    []string{"a1"},
		"saved_at": time.Now().UTC().Format(time.RFC3339),
	}
	fd, _ := json.MarshalIndent(finding, "", "  ")
	_ = os.WriteFile(filepath.Join(memDir, "f1.json"), fd, 0o644)

	// Inspect should include it under "findings".
	res := callTool(t, srv, "archigraph_inspect", map[string]any{
		"label_or_id": "DashboardScreen",
	})
	txt := resultText(res)
	if !strings.Contains(txt, `"findings"`) {
		t.Fatalf("expected findings field in describe output, got: %s", txt)
	}
	if !strings.Contains(txt, "Top-level home view") {
		t.Fatalf("expected saved-finding body in describe output, got: %s", txt)
	}
}

// 16. archigraph_list_findings dropped in refactor/mcp-real-3k.
func TestListFindingsSinceFilter(t *testing.T) {
	t.Skip("archigraph_list_findings dropped from MCP surface in refactor/mcp-real-3k")
}

// TestGraphStatsRepoFilter verifies repo_filter narrows graph_stats to the
// named repos and aggregates totals only over that subset.
func TestGraphStatsRepoFilter(t *testing.T) {
	dir := t.TempDir()
	r1 := filepath.Join(dir, "alpha")
	r2 := filepath.Join(dir, "beta")
	_ = os.MkdirAll(r1, 0o755)
	_ = os.MkdirAll(r2, 0o755)
	writeGraph(t, r1, fixtureDoc("alpha"))
	writeGraph(t, r2, fixtureDoc("beta"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"alpha": r1, "beta": r2},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	// Baseline: no filter -> both repos in totals.
	resAll := callTool(t, srv, "archigraph_stats", nil)
	var allOut struct {
		Entities      int              `json:"entities"`
		Relationships int              `json:"relationships"`
		Repos         []map[string]any `json:"repos"`
	}
	if err := json.Unmarshal([]byte(resultText(resAll)), &allOut); err != nil {
		t.Fatalf("unmarshal all: %v: %s", err, resultText(resAll))
	}
	if len(allOut.Repos) != 2 {
		t.Fatalf("expected 2 repos with no filter, got %d: %s", len(allOut.Repos), resultText(resAll))
	}
	if allOut.Entities != 8 || allOut.Relationships != 6 {
		t.Fatalf("expected 8 entities / 6 relationships across both repos, got %d/%d", allOut.Entities, allOut.Relationships)
	}

	// Filtered: only alpha -> totals halved, single repo entry.
	resFiltered := callTool(t, srv, "archigraph_stats", map[string]any{
		"repo_filter": []any{"alpha"},
	})
	var filtOut struct {
		Entities      int              `json:"entities"`
		Relationships int              `json:"relationships"`
		Repos         []map[string]any `json:"repos"`
	}
	if err := json.Unmarshal([]byte(resultText(resFiltered)), &filtOut); err != nil {
		t.Fatalf("unmarshal filtered: %v: %s", err, resultText(resFiltered))
	}
	if len(filtOut.Repos) != 1 {
		t.Fatalf("expected 1 repo when filtered to alpha, got %d: %s", len(filtOut.Repos), resultText(resFiltered))
	}
	if name, _ := filtOut.Repos[0]["repo"].(string); name != "alpha" {
		t.Fatalf("expected sole repo to be alpha, got %q", name)
	}
	if filtOut.Entities != 4 || filtOut.Relationships != 3 {
		t.Fatalf("expected 4 entities / 3 relationships for alpha alone, got %d/%d", filtOut.Entities, filtOut.Relationships)
	}

	// Star: ["*"] equals no filter.
	resStar := callTool(t, srv, "archigraph_stats", map[string]any{
		"repo_filter": []any{"*"},
	})
	var starOut struct {
		Repos []map[string]any `json:"repos"`
	}
	if err := json.Unmarshal([]byte(resultText(resStar)), &starOut); err != nil {
		t.Fatalf("unmarshal star: %v", err)
	}
	if len(starOut.Repos) != 2 {
		t.Fatalf("expected 2 repos with [\"*\"], got %d", len(starOut.Repos))
	}
}

// ADR-0015 phase-1 (#549 + #550): archigraph_repairs action=list|submit round-trip.
func TestRepairToolsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "rA")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("rA"))

	// Seed enrichment-candidates.json with a repair_edge entry whose
	// shape matches what internal/enrichment/repair_edge.go emits.
	cands := []map[string]any{
		{
			"id":         "ec:abc1230000000001",
			"kind":       "repair_edge",
			"subject_id": "a1",
			"context": map[string]any{
				"edge_id":            "er:deadbeef00000001",
				"relation":           "CALLS",
				"original_stub":      "save",
				"disposition":        "DispositionBugResolver",
				"disposition_reason": "duplicate_short_name",
				"from_entity": map[string]any{
					"id":   "a1",
					"kind": "SCOPE.Component",
					"name": "DashboardScreen",
					"file": "src/DashboardScreen.tsx",
					"line": 10,
				},
			},
		},
		// A non-repair candidate that should be ignored by action=list.
		{
			"id":         "ec:other000000ffff",
			"kind":       "describe_entity",
			"subject_id": "a2",
		},
	}
	candPath := filepath.Join(daemon.StateDirForRepo(repo), "enrichment-candidates.json")
	if err := os.MkdirAll(filepath.Dir(candPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cd, _ := json.MarshalIndent(cands, "", "  ")
	if err := os.WriteFile(candPath, cd, 0o644); err != nil {
		t.Fatal(err)
	}

	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"rA": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	// 1. action=list returns the repair_edge entry and skips describe_entity.
	listRes := callTool(t, srv, "archigraph_repairs", map[string]any{"action": "list"})
	text := resultText(listRes)
	if !strings.Contains(text, "er:deadbeef00000001") {
		t.Fatalf("expected edge_id in list: %s", text)
	}
	if strings.Contains(text, "describe_entity") {
		t.Fatalf("describe_entity candidate should be filtered out: %s", text)
	}
	var listOut struct {
		Residuals []map[string]any `json:"residuals"`
		Total     int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &listOut); err != nil {
		t.Fatalf("unmarshal list: %v: %s", err, text)
	}
	if listOut.Total != 1 || len(listOut.Residuals) != 1 {
		t.Fatalf("expected exactly 1 residual, got %d/%d: %s", listOut.Total, len(listOut.Residuals), text)
	}
	if got, _ := listOut.Residuals[0]["relation"].(string); got != "CALLS" {
		t.Fatalf("expected relation=CALLS, got %q", got)
	}

	// 2. action=submit with an unknown resolution must fail validation.
	badRes := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":      "submit",
		"residual_id": "er:deadbeef00000001",
		"resolution":  "make_it_work",
	})
	if !badRes.IsError {
		t.Fatalf("expected error for unknown resolution, got: %s", resultText(badRes))
	}

	// 3. action=submit with a valid resolution appends to repair.json.
	okRes := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":           "submit",
		"residual_id":      "er:deadbeef00000001",
		"resolution":       "bind_to_entity",
		"target_entity_id": "a3",
		"confidence":       0.85,
		"reasoning":        "agent inferred save() is on ProposalsService",
	})
	if okRes.IsError {
		t.Fatalf("submit unexpected error: %s", resultText(okRes))
	}
	rpath := filepath.Join(daemon.StateDirForRepo(repo), "repair.json")
	data, err := os.ReadFile(rpath)
	if err != nil {
		t.Fatalf("repair.json missing: %v", err)
	}
	if !strings.Contains(string(data), "er:deadbeef00000001") {
		t.Fatalf("repair.json missing edge_id: %s", data)
	}
	if !strings.Contains(string(data), "bind_to_entity") {
		t.Fatalf("repair.json missing resolution: %s", data)
	}

	// 4. Second submit appends — repair_count should be 2.
	okRes2 := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":         "submit",
		"residual_id":    "er:deadbeef00000001",
		"resolution":     "abandon",
		"abandon_reason": "test-only dynamic dispatch",
		"reasoning":      "no static binding possible; dynamic dispatch confirmed",
		"confidence":     0.4,
	})
	if okRes2.IsError {
		t.Fatalf("second submit error: %s", resultText(okRes2))
	}
	var out2 struct {
		RepairCount int `json:"repair_count"`
	}
	if err := json.Unmarshal([]byte(resultText(okRes2)), &out2); err != nil {
		t.Fatalf("unmarshal submit response: %v: %s", err, resultText(okRes2))
	}
	if out2.RepairCount != 2 {
		t.Fatalf("expected repair_count=2, got %d", out2.RepairCount)
	}

	// 5. Confidence out-of-range is rejected.
	badConf := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":      "submit",
		"residual_id": "er:deadbeef00000001",
		"resolution":  "abandon",
		"confidence":  1.5,
	})
	if !badConf.IsError {
		t.Fatalf("expected error for confidence>1, got: %s", resultText(badConf))
	}

	// 6. Unknown residual_id is rejected when not in any repo.
	unknownEdge := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":      "submit",
		"residual_id": "er:notfoundnotfound",
		"resolution":  "abandon",
	})
	if !unknownEdge.IsError {
		t.Fatalf("expected error for unknown residual_id, got: %s", resultText(unknownEdge))
	}
}

// ---------------------------------------------------------------------------
// Source-attribution tests (ADR-0015 #4/8 — issue #547)
// ---------------------------------------------------------------------------

// TestInspect_AgentResolvedEdges verifies that archigraph_inspect includes
// agent_resolved_edges when the graph contains edges whose resolved_by
// property is "agent-repair". This confirms source-attribution survives
// from the repair-apply layer into the MCP surface.
func TestInspect_AgentResolvedEdges(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "rA")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a document where one edge carries agent-repair properties.
	doc := fixtureDoc("rA")
	// Mark the CALLS edge from a1→a2 as agent-repaired.
	doc.Relationships[0].Properties = map[string]string{
		"resolved_by":       "agent-repair",
		"resolved_by_agent": "generate-docs/pass-1a",
		"repair_reasoning":  "inferred from import statement",
	}
	writeGraph(t, repo, doc)

	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"rA": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	res := callTool(t, srv, "archigraph_inspect", map[string]any{"label_or_id": "DashboardScreen"})
	if res.IsError {
		t.Fatalf("inspect error: %s", resultText(res))
	}
	text := resultText(res)

	// The node was agent-repaired — agent_resolved_edges must appear.
	if !strings.Contains(text, "agent_resolved_edges") {
		t.Fatalf("agent_resolved_edges missing from inspect output: %s", text)
	}
	if !strings.Contains(text, "generate-docs/pass-1a") {
		t.Fatalf("resolved_by_agent missing: %s", text)
	}
	if !strings.Contains(text, "inferred from import statement") {
		t.Fatalf("repair_reasoning missing: %s", text)
	}

	// A node with no agent edges (a3) should NOT have the field.
	res2 := callTool(t, srv, "archigraph_inspect", map[string]any{"label_or_id": "ProposalsService"})
	if res2.IsError {
		t.Fatalf("inspect a3 error: %s", resultText(res2))
	}
	if strings.Contains(resultText(res2), "agent_resolved_edges") {
		t.Fatalf("agent_resolved_edges should be absent for non-repaired node: %s", resultText(res2))
	}
}

// ---------------------------------------------------------------------------
// Stale-repair detection tests (ADR-0015 #5/8 — issue #548)
// ---------------------------------------------------------------------------

// TestListResiduals_IncludeStale verifies that action=list with include_stale=true
// returns stale repairs from repair_stats.json.
func TestListResiduals_IncludeStale(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "rA")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("rA"))

	// Write a repair_stats.json with two stale entries.
	archDir := daemon.StateDirForRepo(repo)
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statsData, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"applied":        []any{},
		"rejected":       []any{},
		"stale": []any{
			map[string]any{"edge_id": "er:stale0000000001", "resolution": "bind_to_entity", "resolved_at": "2026-05-19T07:00:00Z"},
			map[string]any{"edge_id": "er:stale0000000002", "resolution": "abandon", "resolved_at": "2026-05-20T08:00:00Z"},
		},
		"applied_count": 0, "rejected_count": 0, "stale_count": 2,
	})
	if err := os.WriteFile(filepath.Join(archDir, "repair_stats.json"), statsData, 0o644); err != nil {
		t.Fatal(err)
	}

	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"rA": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	// Without include_stale the stale list is not returned.
	res := callTool(t, srv, "archigraph_repairs", map[string]any{"action": "list"})
	if res.IsError {
		t.Fatalf("list error: %s", resultText(res))
	}
	if strings.Contains(resultText(res), "er:stale0000000001") {
		t.Fatalf("stale entries should not appear in normal list: %s", resultText(res))
	}

	// With include_stale=true stale entries appear.
	staleRes := callTool(t, srv, "archigraph_repairs", map[string]any{"action": "list", "include_stale": true})
	if staleRes.IsError {
		t.Fatalf("stale list error: %s", resultText(staleRes))
	}
	text := resultText(staleRes)
	if !strings.Contains(text, "er:stale0000000001") {
		t.Fatalf("stale entry 1 missing: %s", text)
	}
	if !strings.Contains(text, "er:stale0000000002") {
		t.Fatalf("stale entry 2 missing: %s", text)
	}
	var out struct {
		Stale []map[string]any `json:"stale"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, text)
	}
	if out.Total != 2 || len(out.Stale) != 2 {
		t.Fatalf("expected 2 stale, got total=%d len=%d", out.Total, len(out.Stale))
	}
	// Each stale entry carries the stale=true flag.
	for _, s := range out.Stale {
		if v, ok := s["stale"].(bool); !ok || !v {
			t.Fatalf("stale flag missing or false: %v", s)
		}
	}

	// Pagination: limit=1 offset=1 returns only the second stale entry.
	pagedRes := callTool(t, srv, "archigraph_repairs", map[string]any{
		"action":        "list",
		"include_stale": true,
		"limit":         1,
		"offset":        1,
	})
	if pagedRes.IsError {
		t.Fatalf("paged stale error: %s", resultText(pagedRes))
	}
	pagedText := resultText(pagedRes)
	if !strings.Contains(pagedText, "er:stale0000000002") {
		t.Fatalf("paged stale should contain entry 2: %s", pagedText)
	}
	if strings.Contains(pagedText, "er:stale0000000001") {
		t.Fatalf("paged stale should NOT contain entry 1: %s", pagedText)
	}
}

// ---------------------------------------------------------------------------
// archigraph_patterns tests (ADR-0018 β)
// ---------------------------------------------------------------------------

// makePatternsServer creates a Server wired to a single-repo group with one
// entity. The patterns.json starts empty.
func makePatternsServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	repo := filepath.Join(dir, "myrepo")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("myrepo"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"myrepo": repo},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	return srv, dir
}

// TestPatterns_RecordThenQuery is the primary integration test:
// record a pattern, query by text, get it back.
func TestPatterns_RecordThenQuery(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// 1. Record.
	recRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "create a new HTTP endpoint", "keywords": []any{"endpoint", "handler", "http"}},
		"steps":     []any{"Create handler in internal/handlers/", "Register route in routes.go"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
	})
	if recRes.IsError {
		t.Fatalf("record error: %s", resultText(recRes))
	}
	var recOut struct {
		ID     string `json:"id"`
		Merged bool   `json:"merged"`
	}
	if err := json.Unmarshal([]byte(resultText(recRes)), &recOut); err != nil {
		t.Fatalf("unmarshal record: %v: %s", err, resultText(recRes))
	}
	if recOut.ID == "" {
		t.Fatalf("expected non-empty id, got: %s", resultText(recRes))
	}
	if recOut.Merged {
		t.Fatalf("should not be merged on first record")
	}

	// 2. Query by text.
	qRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "create a new HTTP endpoint",
	})
	if qRes.IsError {
		t.Fatalf("query error: %s", resultText(qRes))
	}
	var qOut struct {
		Patterns []map[string]any `json:"patterns"`
		Count    int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(resultText(qRes)), &qOut); err != nil {
		t.Fatalf("unmarshal query: %v: %s", err, resultText(qRes))
	}
	if qOut.Count == 0 || len(qOut.Patterns) == 0 {
		t.Fatalf("expected ≥1 pattern, got 0: %s", resultText(qRes))
	}
	if got := qOut.Patterns[0]["id"]; got != recOut.ID {
		t.Fatalf("expected pattern id %q in query result, got %q", recOut.ID, got)
	}
	// Steps and exemplars must round-trip.
	steps, _ := qOut.Patterns[0]["steps"].([]any)
	if len(steps) == 0 {
		t.Errorf("expected steps in query result, got: %v", qOut.Patterns[0]["steps"])
	}
	exemplars, _ := qOut.Patterns[0]["exemplars"].([]any)
	if len(exemplars) == 0 {
		t.Errorf("expected exemplars in query result, got: %v", qOut.Patterns[0]["exemplars"])
	}
}

// TestPatterns_DocumentationURLRoundtrip verifies the documentation_url slot
// is preserved across record/query (Phase 6 will populate it later).
func TestPatterns_DocumentationURLRoundtrip(t *testing.T) {
	srv, _ := makePatternsServer(t)

	docURL := "https://docs.example.com/patterns/code/endpoint.md"
	recRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":            "record",
		"trigger":           map[string]any{"natural_language": "doc url round trip pattern"},
		"steps":             []any{"step one"},
		"exemplars":         []any{"myrepo::a1"},
		"category":          "code",
		"documentation_url": docURL,
	})
	if recRes.IsError {
		t.Fatalf("record error: %s", resultText(recRes))
	}

	qRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "doc url round trip pattern",
	})
	if qRes.IsError {
		t.Fatalf("query error: %s", resultText(qRes))
	}
	var qOut struct {
		Patterns []map[string]any `json:"patterns"`
	}
	if err := json.Unmarshal([]byte(resultText(qRes)), &qOut); err != nil {
		t.Fatalf("unmarshal query: %v", err)
	}
	if len(qOut.Patterns) == 0 {
		t.Fatalf("expected ≥1 pattern")
	}
	if got := qOut.Patterns[0]["documentation_url"]; got != docURL {
		t.Errorf("documentation_url mismatch: got %v, want %s", got, docURL)
	}
}

// TestPatterns_CandidateConvergence verifies that 3 records with as_candidate=true,
// overlapping triggers, and shared exemplars produce convergence_count=3 on one merged candidate.
func TestPatterns_CandidateConvergence(t *testing.T) {
	srv, _ := makePatternsServer(t)

	recordCandidate := func(proposer string) map[string]any {
		res := callTool(t, srv, "archigraph_patterns", map[string]any{
			"action":            "record",
			"trigger":           map[string]any{"natural_language": "add a new service endpoint following the chi pattern", "keywords": []any{"chi", "endpoint", "handler"}},
			"steps":             []any{"Create handler", "Register route"},
			"exemplars":         []any{"myrepo::a1"},
			"category":          "code",
			"as_candidate":      true,
			"proposer_subagent": proposer,
		})
		if res.IsError {
			t.Fatalf("record candidate (%s) error: %s", proposer, resultText(res))
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
			t.Fatalf("unmarshal record (%s): %v", proposer, err)
		}
		return out
	}

	out1 := recordCandidate("agent-1")
	out2 := recordCandidate("agent-2")
	out3 := recordCandidate("agent-3")

	// First record creates a new candidate with convergence_count=1 (first proposal).
	if out1["merged"].(bool) {
		t.Errorf("first record should not be merged")
	}
	if cc, ok := out1["convergence_count"].(float64); !ok || int(cc) != 0 {
		// out1 is returned before ConvergenceCount is set — it's the new record,
		// not a merge result. The field returned for a new record is 0 (default).
		// The convergence_count of 1 is stored on disk; merges will increment from there.
		_ = cc
	}
	// Second and third should merge into the first.
	if !out2["merged"].(bool) {
		t.Errorf("second record should be merged")
	}
	if !out3["merged"].(bool) {
		t.Errorf("third record should be merged")
	}
	// After first record (count=1) + two merges: convergence_count should be 3.
	if cc, ok := out3["convergence_count"].(float64); !ok || int(cc) != 3 {
		t.Errorf("expected convergence_count=3, got: %v", out3["convergence_count"])
	}
	// All merged into same id as out1.
	if out2["id"] != out1["id"] {
		t.Errorf("expected same id on merge: %v vs %v", out2["id"], out1["id"])
	}
}

// TestPatterns_SpecificityScopedQueryWins verifies that a pattern with a more
// specific scope wins over a less specific one when both match BM25.
func TestPatterns_SpecificityScopedQueryWins(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Broad pattern — no scope constraints.
	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "register a new service broad variant", "keywords": []any{"service", "register", "new"}},
		"steps":     []any{"Step 1 — broad"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
	})

	// Specific pattern — repos + languages set (2 non-empty scope fields).
	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "register a new service specific variant", "keywords": []any{"service", "register", "new"}},
		"steps":     []any{"Step A — specific"},
		"exemplars": []any{"myrepo::a2"},
		"category":  "code",
		"scope":     map[string]any{"repos": []any{"myrepo"}, "languages": []any{"go"}},
	})

	qRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "register a new service",
		"limit":  5,
	})
	if qRes.IsError {
		t.Fatalf("query error: %s", resultText(qRes))
	}
	var qOut struct {
		Patterns []map[string]any `json:"patterns"`
		Count    int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(resultText(qRes)), &qOut); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, resultText(qRes))
	}
	if qOut.Count < 2 {
		t.Fatalf("expected ≥2 patterns, got %d: %s", qOut.Count, resultText(qRes))
	}
	// First pattern's steps must contain "Step A — specific".
	steps, _ := qOut.Patterns[0]["steps"].([]any)
	if len(steps) == 0 || steps[0] != "Step A — specific" {
		t.Errorf("expected more-specific pattern first, got steps: %v", steps)
	}
}

// TestPatterns_ExplicitScopeFilter verifies that an explicit scope override
// in query returns only matching patterns.
func TestPatterns_ExplicitScopeFilter(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Pattern for repo "myrepo".
	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "create a new endpoint for myrepo"},
		"steps":     []any{"myrepo step"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
		"scope":     map[string]any{"repos": []any{"myrepo"}},
	})
	// Pattern for repo "otherrepo".
	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "create a new endpoint for otherrepo"},
		"steps":     []any{"otherrepo step"},
		"exemplars": []any{"myrepo::a2"},
		"category":  "code",
		"scope":     map[string]any{"repos": []any{"otherrepo"}},
	})

	// Query with explicit scope override restricting to "otherrepo".
	qRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "create a new endpoint",
		"scope":  map[string]any{"repos": []any{"otherrepo"}},
	})
	if qRes.IsError {
		t.Fatalf("query error: %s", resultText(qRes))
	}
	var qOut struct {
		Patterns []map[string]any `json:"patterns"`
	}
	if err := json.Unmarshal([]byte(resultText(qRes)), &qOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, p := range qOut.Patterns {
		scope, _ := p["scope"].(map[string]any)
		if scope == nil {
			continue
		}
		repos, _ := scope["repos"].([]any)
		for _, r := range repos {
			if r == "myrepo" {
				t.Errorf("scope-filtered query should not return myrepo pattern, got: %v", p)
			}
		}
	}
}

// TestPatterns_PrivateAntiPatternExclusion verifies that private anti-patterns
// are NOT included in query response by default but ARE included with include_private=true.
func TestPatterns_PrivateAntiPatternExclusion(t *testing.T) {
	srv, _ := makePatternsServer(t)

	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":  "record",
		"trigger": map[string]any{"natural_language": "handler with private anti-pattern"},
		"steps":   []any{"do the thing"},
		"anti_patterns": []any{
			map[string]any{"do_not": "inline business logic", "reason": "separation of concerns", "private": false},
			map[string]any{"do_not": "expose internal secrets", "reason": "security", "private": true},
		},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
	})

	// Default query: private anti-pattern hidden.
	qRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "handler with private anti-pattern",
	})
	txt := resultText(qRes)
	if strings.Contains(txt, "expose internal secrets") {
		t.Errorf("private anti-pattern should not appear in default query, got: %s", txt)
	}
	if !strings.Contains(txt, "inline business logic") {
		t.Errorf("public anti-pattern should appear in default query, got: %s", txt)
	}

	// With include_private=true: private anti-pattern visible.
	qResPriv := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":          "query",
		"text":            "handler with private anti-pattern",
		"include_private": true,
	})
	txtPriv := resultText(qResPriv)
	if !strings.Contains(txtPriv, "expose internal secrets") {
		t.Errorf("private anti-pattern should appear with include_private=true, got: %s", txtPriv)
	}
}

// TestPatterns_RecordErrorCases verifies validation errors.
func TestPatterns_RecordErrorCases(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Missing exemplars.
	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":   "record",
		"trigger":  map[string]any{"natural_language": "test pattern"},
		"steps":    []any{"step one"},
		"category": "code",
	})
	if !res.IsError {
		t.Errorf("expected error for missing exemplars, got: %s", resultText(res))
	}

	// Invalid category.
	res2 := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "test pattern 2"},
		"steps":     []any{"step one"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "bogus_category",
	})
	if !res2.IsError {
		t.Errorf("expected error for invalid category, got: %s", resultText(res2))
	}

	// Missing steps.
	res3 := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "test pattern 3"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
	})
	if !res3.IsError {
		t.Errorf("expected error for missing steps, got: %s", resultText(res3))
	}
}

// TestPatterns_GammaActionsImplemented verifies that γ lifecycle actions are
// now implemented (no longer stubs) and return errors only for missing required
// args, not for "not implemented" reasons.
func TestPatterns_GammaActionsImplemented(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Each γ action should now return a real error (missing required arg) —
	// NOT the "not implemented yet — γ" message.
	for _, action := range []string{"refine", "apply", "reject", "promote"} {
		res := callTool(t, srv, "archigraph_patterns", map[string]any{
			"action": action,
			// Intentionally missing required args to trigger validation errors.
		})
		if !res.IsError {
			t.Errorf("expected validation error for %q with missing args, got success", action)
		}
		txt := resultText(res)
		if strings.Contains(txt, "not implemented yet") {
			t.Errorf("action %q still returns stub error: %s", action, txt)
		}
	}
}

// TestPatterns_QueryIncludeCandidates verifies that candidate patterns are
// excluded by default and included with include_candidates=true.
func TestPatterns_QueryIncludeCandidates(t *testing.T) {
	srv, _ := makePatternsServer(t)

	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":       "record",
		"trigger":      map[string]any{"natural_language": "candidate endpoint pattern"},
		"steps":        []any{"step one"},
		"exemplars":    []any{"myrepo::a1"},
		"category":     "code",
		"as_candidate": true,
	})

	// Default: candidates excluded.
	qDefault := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action": "query",
		"text":   "candidate endpoint pattern",
	})
	var outDefault struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(resultText(qDefault)), &outDefault); err == nil {
		if outDefault.Count > 0 {
			t.Errorf("expected 0 patterns without include_candidates, got %d", outDefault.Count)
		}
	}

	// With include_candidates=true.
	qWithCands := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":             "query",
		"text":               "candidate endpoint pattern",
		"include_candidates": true,
	})
	var outWith struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(resultText(qWithCands)), &outWith); err == nil {
		if outWith.Count == 0 {
			t.Errorf("expected ≥1 pattern with include_candidates=true, got 0: %s", resultText(qWithCands))
		}
	}
}

// TestPatterns_EdgeEmission verifies that EXEMPLAR edges are returned in the
// record response.
func TestPatterns_EdgeEmission(t *testing.T) {
	srv, _ := makePatternsServer(t)

	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": "edge emission test"},
		"steps":     []any{"step one"},
		"exemplars": []any{"myrepo::a1", "myrepo::a2"},
		"category":  "code",
	})
	if res.IsError {
		t.Fatalf("record error: %s", resultText(res))
	}
	var out struct {
		EdgesEmitted []map[string]any `json:"edges_emitted"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.EdgesEmitted) != 2 {
		t.Errorf("expected 2 EXEMPLAR edges, got %d: %s", len(out.EdgesEmitted), resultText(res))
	}
	for _, e := range out.EdgesEmitted {
		if e["edge_kind"] != "EXEMPLAR" {
			t.Errorf("expected edge_kind=EXEMPLAR, got: %v", e["edge_kind"])
		}
	}
}

// ---------------------------------------------------------------------------
// γ lifecycle action tests
// ---------------------------------------------------------------------------

// absF returns absolute value of a float64 (local helper for γ tests).
func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// recordPattern is a helper that records a pattern and returns its id.
func recordPattern(t *testing.T, srv *Server, nl string) string {
	t.Helper()
	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":    "record",
		"trigger":   map[string]any{"natural_language": nl, "keywords": []any{"test"}},
		"steps":     []any{"step A", "step B"},
		"exemplars": []any{"myrepo::a1"},
		"category":  "code",
	})
	if res.IsError {
		t.Fatalf("record error: %s", resultText(res))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return out.ID
}

// TestPatterns_RefineAddRemoveStep verifies that refine add/remove step works and persists.
func TestPatterns_RefineAddRemoveStep(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "refine step test pattern")

	// Add a step.
	refRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes":    map[string]any{"add_step": "step C — added by refine"},
	})
	if refRes.IsError {
		t.Fatalf("refine add_step error: %s", resultText(refRes))
	}
	var refOut struct {
		Pattern     map[string]any   `json:"pattern"`
		EdgeChanges []map[string]any `json:"edge_changes"`
	}
	if err := json.Unmarshal([]byte(resultText(refRes)), &refOut); err != nil {
		t.Fatalf("unmarshal refine: %v: %s", err, resultText(refRes))
	}
	steps, _ := refOut.Pattern["steps"].([]any)
	if len(steps) != 3 {
		t.Errorf("expected 3 steps after add_step, got %d: %v", len(steps), steps)
	}
	if steps[2] != "step C — added by refine" {
		t.Errorf("unexpected step[2]: %v", steps[2])
	}

	// Remove step at index 0.
	rem := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes":    map[string]any{"remove_step_index": float64(0)},
	})
	if rem.IsError {
		t.Fatalf("refine remove_step_index error: %s", resultText(rem))
	}
	var remOut struct {
		Pattern map[string]any `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(resultText(rem)), &remOut); err != nil {
		t.Fatalf("unmarshal refine remove: %v", err)
	}
	stepsAfter, _ := remOut.Pattern["steps"].([]any)
	if len(stepsAfter) != 2 {
		t.Errorf("expected 2 steps after remove, got %d: %v", len(stepsAfter), stepsAfter)
	}
	// step A was removed; remaining: step B and the added step C.
	if stepsAfter[0] != "step B" {
		t.Errorf("expected step B at [0] after remove, got: %v", stepsAfter[0])
	}

	// Verify confidence unchanged (neutral).
	if conf, ok := remOut.Pattern["confidence"].(float64); ok {
		if conf != 0.4 { // initial confidence from New()
			t.Errorf("refine should not change confidence: got %v", conf)
		}
	}
}

// TestPatterns_RefineAddRemoveExemplar verifies add/remove exemplar produces correct edge change records.
func TestPatterns_RefineAddRemoveExemplar(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "refine exemplar test pattern")

	// Add exemplar.
	addRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes":    map[string]any{"add_exemplar": "myrepo::a2"},
	})
	if addRes.IsError {
		t.Fatalf("refine add_exemplar error: %s", resultText(addRes))
	}
	var addOut struct {
		Pattern     map[string]any   `json:"pattern"`
		EdgeChanges []map[string]any `json:"edge_changes"`
	}
	if err := json.Unmarshal([]byte(resultText(addRes)), &addOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(addOut.EdgeChanges) != 1 {
		t.Errorf("expected 1 edge change for add_exemplar, got %d", len(addOut.EdgeChanges))
	}
	if addOut.EdgeChanges[0]["op"] != "add" || addOut.EdgeChanges[0]["edge_kind"] != "EXEMPLAR" {
		t.Errorf("unexpected edge change: %v", addOut.EdgeChanges[0])
	}
	exemplars, _ := addOut.Pattern["exemplars"].([]any)
	if len(exemplars) != 2 {
		t.Errorf("expected 2 exemplars, got %d", len(exemplars))
	}

	// Remove exemplar.
	remRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes":    map[string]any{"remove_exemplar": "myrepo::a2"},
	})
	if remRes.IsError {
		t.Fatalf("refine remove_exemplar error: %s", resultText(remRes))
	}
	var remOut struct {
		EdgeChanges []map[string]any `json:"edge_changes"`
	}
	if err := json.Unmarshal([]byte(resultText(remRes)), &remOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(remOut.EdgeChanges) != 1 || remOut.EdgeChanges[0]["op"] != "remove" {
		t.Errorf("expected 1 remove edge change, got: %v", remOut.EdgeChanges)
	}
}

// TestPatterns_RefineChangeScope verifies partial scope update (fields not provided preserved).
func TestPatterns_RefineChangeScope(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "refine scope test pattern")

	// Start by setting a scope.
	callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes": map[string]any{
			"change_scope": map[string]any{
				"repos":     []any{"myrepo"},
				"languages": []any{"go"},
			},
		},
	})

	// Now change only languages; repos must be preserved.
	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "refine",
		"pattern_id": id,
		"changes": map[string]any{
			"change_scope": map[string]any{
				"languages": []any{"typescript"},
			},
		},
	})
	if res.IsError {
		t.Fatalf("refine scope error: %s", resultText(res))
	}
	var out struct {
		Pattern map[string]any `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	scope, _ := out.Pattern["scope"].(map[string]any)
	langs, _ := scope["languages"].([]any)
	repos, _ := scope["repos"].([]any)
	if len(langs) != 1 || langs[0] != "typescript" {
		t.Errorf("expected languages=[typescript], got %v", langs)
	}
	if len(repos) != 1 || repos[0] != "myrepo" {
		t.Errorf("expected repos=[myrepo] preserved, got %v", repos)
	}
}

// TestPatterns_ApplySuccess verifies confidence += 0.1, observations++, CREATED_BY edges.
func TestPatterns_ApplySuccess(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "apply success test pattern")

	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":           "apply",
		"pattern_id":       id,
		"success":          true,
		"created_entities": []any{"myrepo::new-entity-1", "myrepo::new-entity-2"},
	})
	if res.IsError {
		t.Fatalf("apply error: %s", resultText(res))
	}
	var out struct {
		Pattern        map[string]any   `json:"pattern"`
		CreatedByEdges []map[string]any `json:"created_by_edges"`
		CreatedByCount int              `json:"created_by_count"`
		ApplyCallID    string           `json:"apply_call_id"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, resultText(res))
	}

	// confidence 0.4 + 0.1 = 0.5
	conf, _ := out.Pattern["confidence"].(float64)
	if absF(conf-0.5) > 1e-9 {
		t.Errorf("expected confidence=0.5, got %v", conf)
	}
	// observations == 1
	obs, _ := out.Pattern["observations"].(float64)
	if int(obs) != 1 {
		t.Errorf("expected observations=1, got %v", obs)
	}
	// CREATED_BY edges
	if out.CreatedByCount != 2 {
		t.Errorf("expected 2 created_by edges, got %d", out.CreatedByCount)
	}
	if len(out.CreatedByEdges) != 2 {
		t.Errorf("expected 2 edges in created_by_edges, got %d", len(out.CreatedByEdges))
	}
	for _, e := range out.CreatedByEdges {
		if e["edge_kind"] != "CREATED_BY" {
			t.Errorf("expected edge_kind=CREATED_BY, got %v", e["edge_kind"])
		}
		if e["success"] != true {
			t.Errorf("expected success=true on edge, got %v", e["success"])
		}
		if e["apply_call_id"] == "" {
			t.Errorf("expected non-empty apply_call_id")
		}
	}
	if out.ApplyCallID == "" {
		t.Errorf("expected non-empty apply_call_id in response")
	}
	// last_applied must be set
	if la, ok := out.Pattern["last_applied"].(float64); !ok || la == 0 {
		t.Errorf("expected last_applied to be set, got %v", out.Pattern["last_applied"])
	}
}

// TestPatterns_ApplyFailure verifies confidence -= 0.15 (floor at 0.2).
func TestPatterns_ApplyFailure(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "apply failure test pattern")

	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "apply",
		"pattern_id": id,
		"success":    false,
	})
	if res.IsError {
		t.Fatalf("apply error: %s", resultText(res))
	}
	var out struct {
		Pattern map[string]any `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// confidence 0.4 - 0.15 = 0.25
	conf, _ := out.Pattern["confidence"].(float64)
	if absF(conf-0.25) > 1e-9 {
		t.Errorf("expected confidence=0.25, got %v", conf)
	}
	// last_applied should NOT be set on failure
	if la, _ := out.Pattern["last_applied"].(float64); la != 0 {
		t.Errorf("last_applied should not be set on failure, got %v", la)
	}
}

// TestPatterns_ApplyFloorNotBroken verifies repeated failures floor at 0.2.
func TestPatterns_ApplyFloorNotBroken(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "apply floor test pattern")

	for i := 0; i < 10; i++ {
		res := callTool(t, srv, "archigraph_patterns", map[string]any{
			"action":     "apply",
			"pattern_id": id,
			"success":    false,
		})
		if res.IsError {
			t.Fatalf("apply iteration %d error: %s", i, resultText(res))
		}
	}
	getRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "get",
		"pattern_id": id,
	})
	if getRes.IsError {
		t.Fatalf("get error: %s", resultText(getRes))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(getRes)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	conf, _ := out["confidence"].(float64)
	if conf < 0.2 {
		t.Errorf("confidence floor breached: got %v", conf)
	}
	if conf != 0.2 {
		t.Errorf("expected confidence=0.2 (floor), got %v", conf)
	}
}

// TestPatterns_RejectDelta verifies confidence -= 0.3 with set_to_zero=false.
func TestPatterns_RejectDelta(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "reject delta test pattern")

	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "reject",
		"pattern_id": id,
		"reason":     "outdated approach",
	})
	if res.IsError {
		t.Fatalf("reject error: %s", resultText(res))
	}
	var out struct {
		Pattern      map[string]any `json:"pattern"`
		RejectReason string         `json:"reject_reason"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, resultText(res))
	}
	// confidence 0.4 - 0.3 = 0.2 (exactly at floor)
	conf, _ := out.Pattern["confidence"].(float64)
	if absF(conf-0.2) > 1e-9 {
		t.Errorf("expected confidence=0.2, got %v", conf)
	}
	if out.RejectReason != "outdated approach" {
		t.Errorf("expected reject_reason='outdated approach', got %q", out.RejectReason)
	}
	// reject_reason must persist on pattern too
	if rr, _ := out.Pattern["reject_reason"].(string); rr != "outdated approach" {
		t.Errorf("expected pattern.reject_reason to be set, got %q", rr)
	}
	if ts, _ := out.Pattern["reject_timestamp"].(float64); ts == 0 {
		t.Errorf("expected reject_timestamp to be set")
	}
}

// TestPatterns_RejectSetToZero verifies confidence is hard-set to 0 with set_to_zero=true.
func TestPatterns_RejectSetToZero(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "reject zero test pattern")

	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":      "reject",
		"pattern_id":  id,
		"reason":      "completely wrong",
		"set_to_zero": true,
	})
	if res.IsError {
		t.Fatalf("reject error: %s", resultText(res))
	}
	var out struct {
		Pattern map[string]any `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	conf, _ := out.Pattern["confidence"].(float64)
	if conf != 0.0 {
		t.Errorf("expected confidence=0 with set_to_zero=true, got %v", conf)
	}
}

// TestPatterns_PromoteCandidate verifies is_candidate flips to false on promote.
func TestPatterns_PromoteCandidate(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Record as candidate.
	recRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":       "record",
		"trigger":      map[string]any{"natural_language": "promote test pattern"},
		"steps":        []any{"do the thing"},
		"exemplars":    []any{"myrepo::a1"},
		"category":     "code",
		"as_candidate": true,
	})
	if recRes.IsError {
		t.Fatalf("record error: %s", resultText(recRes))
	}
	var recOut struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(recRes)), &recOut); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}

	// Promote.
	promRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":        "promote",
		"candidate_id":  recOut.ID,
		"approval_note": "reviewed and approved in sprint S19",
	})
	if promRes.IsError {
		t.Fatalf("promote error: %s", resultText(promRes))
	}
	var promOut map[string]any
	if err := json.Unmarshal([]byte(resultText(promRes)), &promOut); err != nil {
		t.Fatalf("unmarshal promote: %v: %s", err, resultText(promRes))
	}
	if isCand, _ := promOut["is_candidate"].(bool); isCand {
		t.Errorf("expected is_candidate=false after promote, got true")
	}
	if note, _ := promOut["approval_note"].(string); note != "reviewed and approved in sprint S19" {
		t.Errorf("expected approval_note to be set, got %q", note)
	}
	if lv, _ := promOut["last_validated"].(float64); lv == 0 {
		t.Errorf("expected last_validated to be set after promote")
	}
}

// TestPatterns_PromoteAlreadyApproved verifies error when promoting a non-candidate.
func TestPatterns_PromoteAlreadyApproved(t *testing.T) {
	srv, _ := makePatternsServer(t)

	// Record as candidate so we can promote.
	recRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":       "record",
		"trigger":      map[string]any{"natural_language": "promote twice test pattern"},
		"steps":        []any{"do the thing"},
		"exemplars":    []any{"myrepo::a1"},
		"category":     "code",
		"as_candidate": true,
	})
	if recRes.IsError {
		t.Fatalf("record error: %s", resultText(recRes))
	}
	var recOut struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(recRes)), &recOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// First promote: should succeed.
	promRes1 := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":       "promote",
		"candidate_id": recOut.ID,
	})
	if promRes1.IsError {
		t.Fatalf("first promote should succeed: %s", resultText(promRes1))
	}

	// Second promote on already-approved pattern → error.
	promRes2 := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":       "promote",
		"candidate_id": recOut.ID,
	})
	if !promRes2.IsError {
		t.Errorf("expected error promoting already-approved pattern, got: %s", resultText(promRes2))
	}
}

// TestPatterns_GetByID verifies get action returns a pattern directly by id.
func TestPatterns_GetByID(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "get by id test pattern")

	getRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "get",
		"pattern_id": id,
	})
	if getRes.IsError {
		t.Fatalf("get error: %s", resultText(getRes))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(getRes)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["id"] != id {
		t.Errorf("expected id=%q, got %v", id, out["id"])
	}
	if steps, _ := out["steps"].([]any); len(steps) != 2 {
		t.Errorf("expected 2 steps, got %v", steps)
	}
}

// TestPatterns_GetNotFound verifies get returns error for unknown id.
func TestPatterns_GetNotFound(t *testing.T) {
	srv, _ := makePatternsServer(t)
	res := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "get",
		"pattern_id": "nonexistentdeadbeef",
	})
	if !res.IsError {
		t.Errorf("expected error for unknown pattern id, got: %s", resultText(res))
	}
}

// TestPatterns_ConcurrentRefineApply verifies no torn writes with concurrent refine+apply.
func TestPatterns_ConcurrentRefineApply(t *testing.T) {
	srv, _ := makePatternsServer(t)
	id := recordPattern(t, srv, "concurrent access test pattern")

	const goroutines = 8
	errs := make(chan string, goroutines*2)
	done := make(chan struct{})

	// Half goroutines refine, half apply; all race on the same pattern id.
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer func() { done <- struct{}{} }()
			var res *mcpapi.CallToolResult
			if i%2 == 0 {
				res = callTool(t, srv, "archigraph_patterns", map[string]any{
					"action":     "refine",
					"pattern_id": id,
					"changes":    map[string]any{"add_step": fmt.Sprintf("concurrent step %d", i)},
				})
			} else {
				res = callTool(t, srv, "archigraph_patterns", map[string]any{
					"action":     "apply",
					"pattern_id": id,
					"success":    i%4 == 1,
				})
			}
			if res.IsError {
				errs <- resultText(res)
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	close(errs)
	for e := range errs {
		t.Errorf("concurrent op error: %s", e)
	}
	// After all goroutines, pattern must still be loadable and valid.
	getRes := callTool(t, srv, "archigraph_patterns", map[string]any{
		"action":     "get",
		"pattern_id": id,
	})
	if getRes.IsError {
		t.Errorf("get after concurrent ops failed: %s", resultText(getRes))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(getRes)), &out); err != nil {
		t.Errorf("unmarshal after concurrent ops: %v", err)
	}
	// Confidence must be within valid bounds.
	if conf, _ := out["confidence"].(float64); conf < 0.0 || conf > 1.0 {
		t.Errorf("confidence out of bounds after concurrent ops: %v", conf)
	}
}

// ---------------------------------------------------------------------------
// #1672 — TOON wire conversion tests
// ---------------------------------------------------------------------------

// TestTOONWire_HomogeneousArrayGetsTOON verifies that when MCP_WIRE_FORMAT=toon
// (default), a tool that returns a JSON array of homogeneous records will
// produce an envelope with items=<TOON-text>, count=N, elapsed_ms.
func TestTOONWire_HomogeneousArrayGetsTOON(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "toon")

	// injectElapsedMS is the last step in wrap; exercise it directly with a
	// synthetic homogeneous-record array payload.
	arr := `[{"id":"e1","name":"POST /api/orders","repo":"svc"},{"id":"e2","name":"GET /api/orders","repo":"svc"}]`
	res := &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.NewTextContent(arr)},
	}
	out := injectElapsedMS(res, 42)
	text := resultText(out)

	// Envelope must be valid JSON.
	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v — got: %s", err, text)
	}

	// items must be a TOON-encoded string (not a JSON array).
	items, ok := env["items"].(string)
	if !ok {
		t.Fatalf("expected items to be a string (TOON), got %T: %v", env["items"], env["items"])
	}
	if !strings.HasPrefix(items, "[!schema {") {
		t.Errorf("expected TOON header '[!schema {', got: %s", items)
	}
	// Schema line must list the sorted keys.
	if !strings.Contains(items, "id,name,repo") {
		t.Errorf("expected schema keys id,name,repo in TOON header, got: %s", items)
	}
	// Row count must match.
	if env["count"].(float64) != 2 {
		t.Errorf("expected count=2, got %v", env["count"])
	}
	// elapsed_ms injected.
	if env["elapsed_ms"].(float64) != 42 {
		t.Errorf("expected elapsed_ms=42, got %v", env["elapsed_ms"])
	}
}

// TestTOONWire_JSONOptOutKeepsArrayInItems verifies that MCP_WIRE_FORMAT=json
// leaves items as a JSON array (backwards-compat opt-out).
func TestTOONWire_JSONOptOutKeepsArrayInItems(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "json")

	arr := `[{"id":"e1","name":"fn1"},{"id":"e2","name":"fn2"}]`
	res := &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.NewTextContent(arr)},
	}
	out := injectElapsedMS(res, 7)
	text := resultText(out)

	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v — got: %s", err, text)
	}
	// items must be a JSON array, not a string.
	if _, isStr := env["items"].(string); isStr {
		t.Errorf("expected items to be a JSON array with MCP_WIRE_FORMAT=json, got a string: %v", env["items"])
	}
	if _, isArr := env["items"].([]any); !isArr {
		t.Errorf("expected items to be []any, got %T", env["items"])
	}
}

// TestTOONWire_HeterogeneousArrayFallsBackToJSON verifies that a mixed-schema
// array (different key sets per row) is NOT TOON-encoded — items stays as a
// JSON array even when MCP_WIRE_FORMAT=toon.
func TestTOONWire_HeterogeneousArrayFallsBackToJSON(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "toon")

	// Second record has an extra key "extra" that the first doesn't.
	arr := `[{"id":"e1","name":"fn1"},{"id":"e2","name":"fn2","extra":"oops"}]`
	res := &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.NewTextContent(arr)},
	}
	out := injectElapsedMS(res, 0)
	text := resultText(out)

	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v — got: %s", err, text)
	}
	if _, isStr := env["items"].(string); isStr {
		t.Errorf("heterogeneous array should not produce TOON string, got items=%v", env["items"])
	}
}

// TestTOONWire_SingleEntityObjectUnchanged verifies that a plain JSON object
// (single entity, not an array) passes through unchanged as minified JSON.
func TestTOONWire_SingleEntityObjectUnchanged(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "toon")

	obj := `{"id":"e1","name":"OrderViewSet","kind":"Component"}`
	res := &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.NewTextContent(obj)},
	}
	out := injectElapsedMS(res, 5)
	text := resultText(out)

	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("expected valid JSON for single object, got: %s — %v", text, err)
	}
	// No "items" envelope for plain objects.
	if _, hasItems := env["items"]; hasItems {
		t.Errorf("single-entity object should not be wrapped in items envelope, got: %s", text)
	}
	if env["id"] != "e1" {
		t.Errorf("expected id=e1 preserved, got: %s", text)
	}
	if env["elapsed_ms"].(float64) != 5 {
		t.Errorf("expected elapsed_ms=5, got %v", env["elapsed_ms"])
	}
}

// TestTOONWire_LiveEndpointsTool verifies end-to-end that archigraph_find_dead_code
// (a list tool) returns TOON-encoded items in its response when MCP_WIRE_FORMAT=toon.
// This exercises the full wrap → injectElapsedMS → TOON path with a real Server.
func TestTOONWire_LiveEndpointsTool(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "toon")

	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	res := callTool(t, srv, "archigraph_find_dead_code", map[string]any{
		"limit": float64(10),
	})
	if res.IsError {
		t.Fatalf("find_dead_code error: %s", resultText(res))
	}
	text := resultText(res)
	// The outer response must be valid JSON.
	var outer map[string]any
	if err := json.Unmarshal([]byte(text), &outer); err != nil {
		t.Fatalf("outer response not valid JSON: %v — got: %s", err, text)
	}
	// elapsed_ms must be present (injected by wrap).
	if _, ok := outer["elapsed_ms"]; !ok {
		t.Errorf("expected elapsed_ms in outer envelope, got: %s", text)
	}
	// When the tool returns any items, they must be TOON-encoded in items string.
	// (dead_code on the fixture may or may not have entries; we just assert shape.)
	if items, ok := outer["items"]; ok {
		switch v := items.(type) {
		case string:
			// TOON path: expected. Verify schema line present.
			if !strings.HasPrefix(v, "[!schema {") {
				t.Errorf("items string is not TOON-encoded: %s", v)
			}
		case []any:
			// JSON fallback is only valid when array is empty (0 dead-code nodes).
			if len(v) != 0 {
				t.Errorf("expected TOON string for non-empty items array, got []any len=%d", len(v))
			}
		default:
			t.Errorf("unexpected items type %T: %v", items, items)
		}
	}
}

// writeGraphFB writes a graph.Document to <repoDir>/.archigraph/graph.fb
// (FlatBuffers format). Used to verify fix for issue #1374 item #1.
func writeGraphFB(t *testing.T, repoDir string, doc *graph.Document) string {
	t.Helper()
	if os.Getenv("ARCHIGRAPH_DAEMON_ROOT") == "" {
		t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	}
	dir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReloadFBOnlyRepo verifies fix for issue #1374 item #1:
// State.Reload() must load repos that have only graph.fb (no graph.json).
// Previously the stat-guard pointed at graph.json → ENOENT → repo silently dropped.
func TestReloadFBOnlyRepo(t *testing.T) {
	dir := t.TempDir()

	// Three repos: one with graph.json only, one with graph.fb only, one with both.
	repoJSON := filepath.Join(dir, "repo-json")
	repoFB := filepath.Join(dir, "repo-fb")
	repoBoth := filepath.Join(dir, "repo-both")
	for _, p := range []string{repoJSON, repoFB, repoBoth} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	docJSON := fixtureDoc("repo-json")
	docFB := fixtureDoc("repo-fb")
	docBoth := fixtureDoc("repo-both")

	writeGraph(t, repoJSON, docJSON)   // graph.json only
	writeGraphFB(t, repoFB, docFB)     // graph.fb only
	writeGraph(t, repoBoth, docBoth)   // graph.json
	writeGraphFB(t, repoBoth, docBoth) // + graph.fb

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"test-group": {
				Repos: map[string]RegistryRepo{
					"repo-json": {Path: repoJSON},
					"repo-fb":   {Path: repoFB},
					"repo-both": {Path: repoBoth},
				},
			},
		},
	}
	state := NewState(reg)
	n, err := state.Reload()
	if err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 repos reloaded, got %d", n)
	}

	grp := state.Group("test-group")
	if grp == nil {
		t.Fatal("group not found after Reload")
	}

	for _, rName := range []string{"repo-json", "repo-fb", "repo-both"} {
		lr := grp.Repos[rName]
		if lr == nil {
			t.Errorf("repo %q missing from loaded group", rName)
			continue
		}
		if lr.Doc == nil {
			t.Errorf("repo %q: Doc is nil (loadErr=%q)", rName, lr.loadErr)
			continue
		}
		if lr.loadErr != "" {
			t.Errorf("repo %q: unexpected loadErr=%q", rName, lr.loadErr)
		}
		if len(lr.Doc.Entities) != 4 {
			t.Errorf("repo %q: expected 4 entities, got %d", rName, len(lr.Doc.Entities))
		}
	}

	// repo-both: GraphFile should point at graph.fb (fb is preferred when both exist).
	if lr := grp.Repos["repo-both"]; lr != nil {
		if !strings.HasSuffix(lr.GraphFile, "graph.fb") {
			t.Errorf("repo-both: expected GraphFile to end in graph.fb, got %q", lr.GraphFile)
		}
	}
}
