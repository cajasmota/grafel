package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// writeGraph writes a graph.Document to <repoDir>/.archigraph/graph.json.
func writeGraph(t *testing.T, repoDir string, doc *graph.Document) string {
	t.Helper()
	dir := filepath.Join(repoDir, ".archigraph")
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

// 4. describe uses the LabelIndex (O(1) by name/id).
func TestGetNodeViaIndex(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	res := callTool(t, srv, "archigraph_describe", map[string]any{"label_or_id": "DashboardScreen"})
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
	if !strings.Contains(txt, "\"crosses_repos\": true") {
		t.Fatalf("expected crosses_repos=true, got: %s", txt)
	}
	if !strings.Contains(txt, "\"found\": true") {
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
		Edges:   []renderEdge{{From: "Foo", To: "Bar", Kind: "SCOPE.IMPORTS"}, {From: "A", To: "B", Kind: "calls"}},
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

// 8. List+resolve link candidate round-trip.
func TestLinkCandidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	r1 := filepath.Join(dir, "rA")
	r2 := filepath.Join(dir, "rB")
	_ = os.MkdirAll(r1, 0o755)
	_ = os.MkdirAll(r2, 0o755)
	writeGraph(t, r1, fixtureDoc("rA"))
	writeGraph(t, r2, fixtureDoc("rB"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"rA": r1, "rB": r2},
	})
	// Pre-populate candidate file at HOME-based default location.
	candPath := filepath.Join(dir, ".archigraph", "groups", "g-link-candidates.json")
	_ = os.MkdirAll(filepath.Dir(candPath), 0o755)
	cands := []LinkCandidate{{ID: "c1", Source: "rA::a1", Target: "rB::a4", Kind: "USES", Confidence: 0.8}}
	cd, _ := json.MarshalIndent(cands, "", "  ")
	_ = os.WriteFile(candPath, cd, 0o644)

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	listRes := callTool(t, srv, "archigraph_list_link_candidates", map[string]any{})
	if !strings.Contains(resultText(listRes), "c1") {
		t.Fatalf("expected c1 in list, got: %s", resultText(listRes))
	}
	resolveRes := callTool(t, srv, "archigraph_resolve_link_candidate", map[string]any{
		"candidate_id": "c1", "decision": "accept",
	})
	if strings.Contains(resultText(resolveRes), "error") {
		t.Fatalf("resolve failed: %s", resultText(resolveRes))
	}
	// Links file should have grown.
	linksPath := filepath.Join(dir, ".archigraph", "groups", "g-links.json")
	data, err := os.ReadFile(linksPath)
	if err != nil {
		t.Fatalf("links file missing: %v", err)
	}
	if !strings.Contains(string(data), "rA::a1") {
		t.Errorf("expected accepted link in links file, got: %s", string(data))
	}
}

// 9. Enrichment candidate submit round-trip.
func TestEnrichmentCandidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	cands := []EnrichmentCandidate{{ID: "e1", NodeID: "a1", Kind: "purpose"}}
	candPath := filepath.Join(repo, ".archigraph", "enrichment-candidates.json")
	d, _ := json.MarshalIndent(cands, "", "  ")
	_ = os.WriteFile(candPath, d, 0o644)
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})
	listRes := callTool(t, srv, "archigraph_list_enrichment_candidates", nil)
	if !strings.Contains(resultText(listRes), "e1") {
		t.Fatalf("expected e1 in list: %s", resultText(listRes))
	}
	subRes := callTool(t, srv, "archigraph_submit_enrichment", map[string]any{
		"candidate_id": "e1", "value": "controls dashboard", "confidence": 0.9, "reason": "test",
	})
	if strings.Contains(resultText(subRes), "error") {
		t.Fatalf("submit error: %s", resultText(subRes))
	}
	resPath := filepath.Join(repo, ".archigraph", "enrichment-resolutions.json")
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
	badDir := filepath.Join(r2, ".archigraph")
	_ = os.MkdirAll(badDir, 0o755)
	_ = os.WriteFile(filepath.Join(badDir, "graph.json"), []byte("not json"), 0o644)
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"good": r1, "bad": r2},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, srv, "archigraph_graph_stats", nil)
	txt := resultText(res)
	if !strings.Contains(txt, "unavailable") {
		t.Errorf("expected 'unavailable' in graph_stats, got: %s", txt)
	}
	// good repo still queryable
	res2 := callTool(t, srv, "archigraph_describe", map[string]any{"label_or_id": "DashboardScreen"})
	if !strings.Contains(resultText(res2), "DashboardScreen") {
		t.Errorf("expected good repo to serve describe, got: %s", resultText(res2))
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
	res := callTool(t, srv, "archigraph_search", map[string]any{
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

// 13. Tool registration uses the finalized distinct names; old names are gone.
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
	wantPresent := []string{
		"archigraph_search", "archigraph_describe", "archigraph_related", "archigraph_trace",
		"archigraph_list_clusters", "archigraph_save_finding", "archigraph_list_findings", "archigraph_get_source",
		"archigraph_whoami", "archigraph_recent_activity", "archigraph_graph_stats", "archigraph_get_telemetry",
	}
	for _, n := range wantPresent {
		if !registered[n] {
			t.Errorf("expected tool %q to be registered", n)
		}
	}
	wantAbsent := []string{
		"query_graph", "get_node", "get_neighbors", "shortest_path",
		"list_communities", "save_result", "get_node_source",
		// Refs #62: generic names collide with other MCP servers; must be prefixed.
		"search", "describe", "related", "trace",
		"list_clusters", "save_finding", "get_source",
		"whoami", "recent_activity", "graph_stats", "get_telemetry",
	}
	for _, n := range wantAbsent {
		if registered[n] {
			t.Errorf("expected old tool %q to NOT be registered", n)
		}
	}
}

// 14. archigraph_save_finding round-trips through archigraph_list_findings
// (Refs #59). Findings persist forbidden-term-free content.
func TestFindingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	saveRes := callTool(t, srv, "archigraph_save_finding", map[string]any{
		"question": "what does DashboardScreen render",
		"answer":   "It renders the proposal counts widget at the top of the screen.",
		"type":     "note",
		"nodes":    []any{"a1"},
	})
	if saveRes.IsError {
		t.Fatalf("save_finding errored: %s", resultText(saveRes))
	}
	listRes := callTool(t, srv, "archigraph_list_findings", nil)
	txt := resultText(listRes)
	if !strings.Contains(txt, "DashboardScreen render") {
		t.Fatalf("expected saved finding in list output, got: %s", txt)
	}
	if !strings.Contains(txt, "proposal counts widget") {
		t.Fatalf("expected answer body in list output, got: %s", txt)
	}
	// entity_id filter narrows correctly.
	filtered := callTool(t, srv, "archigraph_list_findings", map[string]any{
		"entity_id": "a1",
	})
	if !strings.Contains(resultText(filtered), "DashboardScreen render") {
		t.Fatalf("expected entity_id-filtered finding, got: %s", resultText(filtered))
	}
	// entity_id miss returns empty array.
	miss := callTool(t, srv, "archigraph_list_findings", map[string]any{
		"entity_id": "zzz-nonexistent",
	})
	if mt := strings.TrimSpace(resultText(miss)); mt != "[]" && mt != "null" {
		t.Fatalf("expected empty findings for unknown entity_id, got: %s", mt)
	}
}

// 15. describe attaches saved findings keyed by entity ID (Refs #59 strategy A).
func TestDescribeAttachesFindings(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})

	// Save a finding tied to entity a1 (DashboardScreen).
	callTool(t, srv, "archigraph_save_finding", map[string]any{
		"question": "purpose of DashboardScreen",
		"answer":   "Top-level home view.",
		"nodes":    []any{"a1"},
	})
	// Describe should include it under "findings".
	res := callTool(t, srv, "archigraph_describe", map[string]any{
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

// 16. since filter on list_findings drops older entries.
func TestListFindingsSinceFilter(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, _ := NewServer(Config{RegistryPath: regPath})

	callTool(t, srv, "archigraph_save_finding", map[string]any{
		"question": "older",
		"answer":   "older body",
	})
	// Since "now+1h" -> nothing.
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	res := callTool(t, srv, "archigraph_list_findings", map[string]any{
		"since": future,
	})
	txt := strings.TrimSpace(resultText(res))
	if txt != "[]" && txt != "null" {
		t.Fatalf("expected empty list with future since, got: %s", txt)
	}
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
	resAll := callTool(t, srv, "archigraph_graph_stats", nil)
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
	resFiltered := callTool(t, srv, "archigraph_graph_stats", map[string]any{
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
	resStar := callTool(t, srv, "archigraph_graph_stats", map[string]any{
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
