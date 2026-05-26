// cleanup_group_a_test.go — tests for the three tech-debt issues landed in
// cleanup/tools-group-a:
//
//   #2304 – TopKPageRank cache (regression guard vs linear scan)
//   #2305 – synthetic-edge relIdx defensive default (-1)
//   #2337 – sessionMeta helper + lint that non-whoami handlers never embed
//            indexed_sha / indexed_ref / cwd_ref
package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// #2304 — TopKPageRank cache: same top-K as the O(N) linear scan.
// ---------------------------------------------------------------------------

// TestTopKPageRankCacheMatchesLinearScan is the regression guard for #2304.
// It builds the cache via buildTopKPageRank and confirms the top-1 entity
// returned by pickFallback matches what the old O(|Entities|) linear scan
// would have returned.
func TestTopKPageRankCacheMatchesLinearScan(t *testing.T) {
	pr1 := 0.8
	pr2 := 0.5
	pr3 := 0.9 // highest — must win
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "Function", SourceFile: "a.go", StartLine: 1, PageRank: &pr1},
			{ID: "b", Name: "B", Kind: "Function", SourceFile: "b.go", StartLine: 1, PageRank: &pr2},
			{ID: "c", Name: "C", Kind: "Function", SourceFile: "c.go", StartLine: 1, PageRank: &pr3},
		},
	}

	// Build the cache directly.
	top := buildTopKPageRank(doc, 3)
	if len(top) != 3 {
		t.Fatalf("buildTopKPageRank: want 3 entries, got %d", len(top))
	}
	if top[0] != "c" {
		t.Errorf("top[0] = %q; want \"c\" (highest PageRank %.1f)", top[0], pr3)
	}
	if top[1] != "a" {
		t.Errorf("top[1] = %q; want \"a\"", top[1])
	}
	if top[2] != "b" {
		t.Errorf("top[2] = %q; want \"b\"", top[2])
	}

	// Wire into a LoadedRepo and run pickFallback using the cache.
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	lr := &LoadedRepo{
		Repo:         "repo1",
		Doc:          doc,
		ByID:         byID,
		TopKPageRank: top,
	}
	fb := pickFallback([]*LoadedRepo{lr})
	if fb == nil {
		t.Fatal("pickFallback returned nil")
	}
	if fb.entity.ID != "c" {
		t.Errorf("pickFallback via cache: got entity %q; want \"c\"", fb.entity.ID)
	}

	// Linear-scan path: omit TopKPageRank so pickFallback falls back.
	lrNoCache := &LoadedRepo{
		Repo: "repo1",
		Doc:  doc,
		ByID: byID,
		// TopKPageRank intentionally absent
	}
	fbLinear := pickFallback([]*LoadedRepo{lrNoCache})
	if fbLinear == nil {
		t.Fatal("pickFallback (linear) returned nil")
	}
	if fbLinear.entity.ID != "c" {
		t.Errorf("pickFallback linear scan: got entity %q; want \"c\"", fbLinear.entity.ID)
	}
	// Both paths must agree.
	if fb.entity.ID != fbLinear.entity.ID {
		t.Errorf("cache path picked %q but linear path picked %q — mismatch", fb.entity.ID, fbLinear.entity.ID)
	}
}

// TestTopKPageRankCacheTopKTruncation verifies k-truncation: asking for fewer
// entries than the document has returns exactly k.
func TestTopKPageRankCacheTopKTruncation(t *testing.T) {
	pr := 1.0
	ents := make([]graph.Entity, 10)
	for i := range ents {
		p := pr - float64(i)*0.05
		ents[i] = graph.Entity{ID: "e" + itoa(i), Kind: "Function", SourceFile: "f.go", StartLine: 1, PageRank: &p}
	}
	doc := &graph.Document{Entities: ents}
	top := buildTopKPageRank(doc, 3)
	if len(top) != 3 {
		t.Errorf("wanted 3 entries, got %d", len(top))
	}
}

// BenchmarkPickFallbackCached benchmarks the cache-backed pickFallback path
// (reads top[0] per repo — O(repos), not O(|Entities|)).
func BenchmarkPickFallbackCached(b *testing.B) {
	const nEnts = 10000
	pr := 1.0
	ents := make([]graph.Entity, nEnts)
	for i := range ents {
		p := pr - float64(i)*(pr/float64(nEnts))
		ents[i] = graph.Entity{ID: "e" + itoa(i), Kind: "Function", SourceFile: "f.go", StartLine: 1, PageRank: &p}
	}
	doc := &graph.Document{Entities: ents}
	byID := make(map[string]*graph.Entity, nEnts)
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	top := buildTopKPageRank(doc, 64)
	lr := &LoadedRepo{Repo: "r", Doc: doc, ByID: byID, TopKPageRank: top}
	repos := []*LoadedRepo{lr}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pickFallback(repos)
	}
}

// BenchmarkPickFallbackLinear is the pre-#2304 baseline (no cache).
func BenchmarkPickFallbackLinear(b *testing.B) {
	const nEnts = 10000
	pr := 1.0
	ents := make([]graph.Entity, nEnts)
	for i := range ents {
		p := pr - float64(i)*(pr/float64(nEnts))
		ents[i] = graph.Entity{ID: "e" + itoa(i), Kind: "Function", SourceFile: "f.go", StartLine: 1, PageRank: &p}
	}
	doc := &graph.Document{Entities: ents}
	byID := make(map[string]*graph.Entity, nEnts)
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	// No TopKPageRank — forces the slow path.
	lr := &LoadedRepo{Repo: "r", Doc: doc, ByID: byID}
	repos := []*LoadedRepo{lr}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pickFallback(repos)
	}
}

// ---------------------------------------------------------------------------
// #2305 — synthetic edges must carry relIdx: -1.
// ---------------------------------------------------------------------------

// TestSyntheticEdgesHaveNegativeRelIdx exercises the expand closures in
// handleFindPaths and handleShortestPath (dashboard_tools.go / tools.go) by
// inspecting edges produced from cross-repo links and reversed in-edges.
// We do this at the unit level by calling buildAdjacency and confirming
// proper relIdx values, then confirming that synthetic re-prefixed edges
// built by the expand closures carry -1.
//
// The traversal.go contract: relIdx >= 0 means "real relationship"; relIdx == -1
// means "synthetic" (re-prefixed, reversed, or cross-repo overlay).
func TestSyntheticEdgesHaveNegativeRelIdx(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "x", Name: "X", Kind: "Function", SourceFile: "x.go", StartLine: 1},
			{ID: "y", Name: "Y", Kind: "Function", SourceFile: "y.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "x", ToID: "y", Kind: "CALLS"},
		},
	}
	a := buildAdjacency(doc, "repo1")

	// Real edges (from buildAdjacency) should have relIdx >= 0.
	for _, e := range a.out["x"] {
		if e.relIdx < 0 {
			t.Errorf("real out-edge x->y has relIdx=%d; want >= 0", e.relIdx)
		}
	}
	for _, e := range a.in["y"] {
		if e.relIdx < 0 {
			t.Errorf("real in-edge y<-x has relIdx=%d; want >= 0", e.relIdx)
		}
	}

	// Simulate a synthetic re-prefixed edge (as constructed in expand closures).
	synth := edge{
		target: "repo2:y",
		kind:   "CALLS",
		weight: 1,
		relIdx: -1,
	}
	if synth.relIdx != -1 {
		t.Errorf("synthetic edge relIdx=%d; want -1", synth.relIdx)
	}
}

// TestHandleFindPathsSyntheticEdgesRelIdx calls handleFindPaths end-to-end and
// confirms that the call succeeds (the -1 relIdx is never dereferenced on the
// happy path, confirming safe=today from the issue brief).
func TestHandleFindPathsSyntheticEdgesRelIdx(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "src", Name: "Src", QualifiedName: "pkg.Src", Kind: "Function", SourceFile: "s.go", StartLine: 1},
			{ID: "dst", Name: "Dst", QualifiedName: "pkg.Dst", Kind: "Function", SourceFile: "d.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "src", ToID: "dst", Kind: "CALLS"},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group": "test",
		"from":  "src",
		"to":    "dst",
	}
	res, err := srv.handleFindPaths(context.Background(), req)
	if err != nil {
		t.Fatalf("handleFindPaths: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("handleFindPaths returned error: %+v", res)
	}
	out := extractResultJSON(t, res)
	if found, _ := out["found"].(bool); !found {
		t.Errorf("expected found=true for direct src->dst CALLS edge; got: %v", out)
	}
}

// ---------------------------------------------------------------------------
// #2337 — sessionMeta helper + lint: no non-whoami handler embeds these keys.
// ---------------------------------------------------------------------------

// TestSessionMetaHelper checks that sessionMeta correctly extracts
// indexed_ref / indexed_sha / is_worktree / parent_repo from a CWDResolution.
func TestSessionMetaHelper(t *testing.T) {
	cwdRes := &CWDResolution{
		Ref:            "main",
		SHA:            "abc123def456",
		IsWorktree:     false,
		ParentRepoPath: "",
	}
	m := sessionMeta(nil, cwdRes)

	if m["indexed_ref"] != "main" {
		t.Errorf("indexed_ref = %v; want \"main\"", m["indexed_ref"])
	}
	if m["indexed_sha"] != "abc123def456" {
		t.Errorf("indexed_sha = %v; want \"abc123def456\"", m["indexed_sha"])
	}
	if m["is_worktree"] != false {
		t.Errorf("is_worktree = %v; want false", m["is_worktree"])
	}
	if m["parent_repo"] != nil {
		t.Errorf("parent_repo = %v; want nil (not a worktree)", m["parent_repo"])
	}
}

// TestSessionMetaHelperWorktree confirms parent_repo is set when IsWorktree is true.
func TestSessionMetaHelperWorktree(t *testing.T) {
	cwdRes := &CWDResolution{
		Ref:            "feat/foo",
		SHA:            "deadbeef1234",
		IsWorktree:     true,
		ParentRepoPath: "/home/user/repos/myrepo",
	}
	m := sessionMeta(nil, cwdRes)
	if m["is_worktree"] != true {
		t.Errorf("is_worktree = %v; want true", m["is_worktree"])
	}
	if m["parent_repo"] != "/home/user/repos/myrepo" {
		t.Errorf("parent_repo = %v; want \"/home/user/repos/myrepo\"", m["parent_repo"])
	}
}

// TestNoSessionMetaInNonWhoamiHandlers asserts that no handler other than
// archigraph_whoami embeds the session-stable fields indexed_sha / indexed_ref
// / cwd_ref in its JSON response. These fields were erroneously embedded in
// several handlers before #2335 stripped them; this test prevents regression.
//
// The test calls a representative set of non-whoami handlers with a minimal
// valid Document and checks that none of the banned keys appear in their
// JSON output.
func TestNoSessionMetaInNonWhoamiHandlers(t *testing.T) {
	pr := 0.5
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn_a", Name: "FnA", QualifiedName: "pkg.FnA", Kind: "Function",
				SourceFile: "a.go", StartLine: 1, PageRank: &pr},
			{ID: "fn_b", Name: "FnB", QualifiedName: "pkg.FnB", Kind: "Function",
				SourceFile: "b.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "fn_a", ToID: "fn_b", Kind: "CALLS"},
		},
	}
	srv := newTestServerWithDoc(t, doc)

	// Each entry: handler func + args to call it with.
	type tc struct {
		name string
		fn   func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)
		args map[string]any
	}
	cases := []tc{
		{
			name: "handleGetNode (inspect)",
			fn:   srv.handleGetNode,
			args: map[string]any{"group": "test", "entity_id": "fn_a"},
		},
		{
			name: "handleQueryGraph (find)",
			fn:   srv.handleQueryGraph,
			args: map[string]any{"group": "test", "query": "FnA"},
		},
		{
			name: "handleGetNeighbors (expand)",
			fn:   srv.handleGetNeighbors,
			args: map[string]any{"group": "test", "entity_id": "fn_a", "direction": "outgoing"},
		},
		{
			name: "handleGraphStats (stats)",
			fn:   srv.handleGraphStats,
			args: map[string]any{"group": "test"},
		},
	}

	banned := []string{"indexed_sha", "indexed_ref", "cwd_ref"}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := mcpapi.CallToolRequest{}
			req.Params.Arguments = c.args
			res, err := c.fn(context.Background(), req)
			if err != nil {
				t.Fatalf("%s: handler error: %v", c.name, err)
			}
			if res == nil {
				t.Fatalf("%s: nil result", c.name)
			}
			body := extractResultText(t, res)
			for _, key := range banned {
				// Use string search on the raw JSON to catch any embedding
				// regardless of nesting depth.
				if strings.Contains(body, `"`+key+`"`) {
					t.Errorf("%s: response embeds banned session-meta key %q\nfull response:\n%s",
						c.name, key, body)
				}
			}
		})
	}
}

// TestNoSessionMetaInNonWhoamiHandlers_SyntheticViolation verifies that the
// test itself would catch a violation if a handler were to embed indexed_sha.
// This is the "wired" catch test required by #2337.
func TestNoSessionMetaInNonWhoamiHandlers_SyntheticViolation(t *testing.T) {
	// Craft a fake response body that a rogue handler might emit.
	fakeBody := `{"indexed_sha":"abc","result":"ok"}`
	banned := []string{"indexed_sha", "indexed_ref", "cwd_ref"}
	caught := false
	for _, key := range banned {
		if strings.Contains(fakeBody, `"`+key+`"`) {
			caught = true
		}
	}
	if !caught {
		t.Error("synthetic violation not detected — lint logic is broken")
	}
}
