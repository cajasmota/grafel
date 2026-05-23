# Token Default Limits Shrink Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Shrink default response limits on all list-returning MCP tools and extend the `capByRenderedBytes` token-budget pattern to every list tool, enforcing a default `token_budget=800` per call.

**Architecture:** Lower five numeric defaults in handler code + schema registration; extend `capByRenderedBytes` from `endpoint_tools.go` into `flow_tools.go` and `traces.go`; wire `token_budget` parameter through `archigraph_find_callers`, `archigraph_find_callees`, `archigraph_expand`, and `archigraph_traces(action=list)`; add test coverage; update SCHEMA.md.

**Tech Stack:** Go 1.21+, `internal/mcp` package, `github.com/mark3labs/mcp-go`, Go test (`testing` stdlib). No new deps.

---

## File Map

| File | What changes |
|------|-------------|
| `internal/mcp/server.go` | Lower `DefaultNumber` for `limit` on `archigraph_endpoints` (50→20), `archigraph_traces` (25→10); lower `DefaultNumber` for `depth` on `archigraph_find_callers`, `archigraph_find_callees`, `archigraph_expand` (2→1); add `token_budget` param to those four tools. |
| `internal/mcp/tools.go` | Lower BM25 candidate fetch 50→10; lower `keep[:25]` → `keep[:10]` in `handleQueryGraph`. |
| `internal/mcp/flow_tools.go` | Accept `token_budget` in `handleFindCallers` and `handleFindCallees`; apply `capByRenderedBytes` before emitting callers/callees slices. |
| `internal/mcp/traces.go` | Accept `token_budget` in `handleTracesList`; apply `capByRenderedBytes` before emitting `items`. Lower default `limit` 25→10. |
| `internal/mcp/endpoint_tools.go` | Lower default `limit` 50→20 in `handleEndpointDefinitions` and `handleEndpointCalls`. |
| `internal/mcp/SCHEMA.md` | Update default values in parameter tables for all changed tools; add `token_budget` rows. |
| `internal/mcp/budget_test.go` | Add `TestTokenBudgetEnforced_*` tests proving `capByRenderedBytes` caps large payloads. |

---

## Task 1 — Lower defaults in `server.go` tool registrations

**Files:**
- Modify: `internal/mcp/server.go:136-165,175-191,340-368`

- [ ] **Step 1: Open server.go and read the current DefaultNumber calls for the target tools**

  The five registration blocks to touch are:
  - `archigraph_find` — no `limit` param (controlled by keep[:25] inside handler — done in Task 3).
  - `archigraph_expand` — `depth` DefaultNumber(2) → 1; add `token_budget` DefaultNumber(800).
  - `archigraph_traces` — `limit` DefaultNumber(25) → 10; add `token_budget` DefaultNumber(800).
  - `archigraph_endpoints` — `limit` DefaultNumber(50) → 20.
  - `archigraph_find_callers` — `depth` DefaultNumber(1) — already 1 in current code ✓; add `token_budget` DefaultNumber(800).
  - `archigraph_find_callees` — `depth` DefaultNumber(1) — already 1 in current code ✓; add `token_budget` DefaultNumber(800).

  Current `archigraph_expand` registration (lines 157-164):
  ```go
  s.MCP.AddTool(mcpapi.NewTool("archigraph_expand",
      mcpapi.WithDescription("Return neighbors of a node up to a given depth."),
      mcpapi.WithString("node", mcpapi.Required()),
      mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
      mcpapi.WithArray("repo_filter"),
      mcpapi.WithAny("group"),
      mcpapi.WithAny("cwd"),
  ), s.wrap("archigraph_expand", s.handleGetNeighbors))
  ```

- [ ] **Step 2: Write the failing test that will catch the old defaults**

  In `internal/mcp/budget_test.go`, add after the existing tests:

  ```go
  // TestDefaultLimitsReduced verifies that the tool schema defaults for
  // depth/limit on the token-economy tools are at the narrower values
  // introduced in #1738.
  func TestDefaultLimitsReduced(t *testing.T) {
      tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
      if err != nil {
          t.Fatalf("create temp registry: %v", err)
      }
      if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
          t.Fatalf("write temp registry: %v", err)
      }
      tmp.Close()

      srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
      if err != nil {
          t.Fatalf("new server: %v", err)
      }

      byName := srv.MCP.ListTools()

      cases := []struct {
          tool  string
          param string
          want  float64
      }{
          {"archigraph_expand", "depth", 1},
          {"archigraph_traces", "limit", 10},
          {"archigraph_endpoints", "limit", 20},
          {"archigraph_find_callers", "depth", 1},
          {"archigraph_find_callees", "depth", 1},
      }

      for _, tc := range cases {
          t.Run(tc.tool+"/"+tc.param, func(t *testing.T) {
              st, ok := byName[tc.tool]
              if !ok {
                  t.Fatalf("tool %q not registered", tc.tool)
              }
              // Walk InputSchema properties to find the param default.
              props, ok := st.Tool.InputSchema.Properties[tc.param]
              if !ok {
                  t.Fatalf("tool %q has no param %q", tc.tool, tc.param)
              }
              // mcp-go stores defaults as map[string]any in the JSON schema.
              propsMap, ok := props.(map[string]any)
              if !ok {
                  t.Fatalf("tool %q param %q: unexpected schema shape %T", tc.tool, tc.param, props)
              }
              got, ok := propsMap["default"]
              if !ok {
                  t.Fatalf("tool %q param %q: no default in schema", tc.tool, tc.param)
              }
              gotF, ok := got.(float64)
              if !ok {
                  t.Fatalf("tool %q param %q: default is %T not float64", tc.tool, tc.param, got)
              }
              if gotF != tc.want {
                  t.Errorf("tool %q param %q: default = %v, want %v", tc.tool, tc.param, gotF, tc.want)
              }
          })
      }
  }
  ```

- [ ] **Step 3: Run the test to verify it fails with the current defaults**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestDefaultLimitsReduced -v 2>&1 | head -40
  ```

  Expected: FAIL — `archigraph_expand depth: default = 2, want 1` and `archigraph_traces limit: default = 25, want 10` and `archigraph_endpoints limit: default = 50, want 20`.

- [ ] **Step 4: Update `archigraph_expand` registration in `server.go`**

  Change (in the `archigraph_expand` block, ~line 157):
  ```go
  mcpapi.WithNumber("depth", mcpapi.DefaultNumber(2)),
  ```
  To:
  ```go
  mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
  mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
  ```

- [ ] **Step 5: Update `archigraph_traces` registration in `server.go`**

  Change (in the `archigraph_traces` block, ~line 177, the limit param):
  ```go
  mcpapi.WithNumber("limit", mcpapi.DefaultNumber(25)),
  ```
  To:
  ```go
  mcpapi.WithNumber("limit", mcpapi.DefaultNumber(10)),
  mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
  ```

- [ ] **Step 6: Update `archigraph_endpoints` registration in `server.go`**

  Change (in the `archigraph_endpoints` block, ~line 340):
  ```go
  mcpapi.WithNumber("limit", mcpapi.DefaultNumber(50)),
  ```
  To:
  ```go
  mcpapi.WithNumber("limit", mcpapi.DefaultNumber(20)),
  ```

- [ ] **Step 7: Add `token_budget` to `archigraph_find_callers` registration in `server.go`**

  Change (in the `archigraph_find_callers` block, ~line 354):
  ```go
  s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callers",
      mcpapi.WithDescription("Inbound callers of an entity up to N hops."),
      mcpapi.WithString("entity_id", mcpapi.Required()),
      mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
      mcpapi.WithAny("group"),
      mcpapi.WithAny("cwd"),
  ), s.wrap("archigraph_find_callers", s.handleFindCallers))
  ```
  To:
  ```go
  s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callers",
      mcpapi.WithDescription("Inbound callers of an entity up to N hops."),
      mcpapi.WithString("entity_id", mcpapi.Required()),
      mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
      mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
      mcpapi.WithAny("group"),
      mcpapi.WithAny("cwd"),
  ), s.wrap("archigraph_find_callers", s.handleFindCallers))
  ```

- [ ] **Step 8: Add `token_budget` to `archigraph_find_callees` registration in `server.go`**

  Change (in the `archigraph_find_callees` block, ~line 362):
  ```go
  s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callees",
      mcpapi.WithDescription("Outbound callees of an entity up to N hops."),
      mcpapi.WithString("entity_id", mcpapi.Required()),
      mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
      mcpapi.WithAny("group"),
      mcpapi.WithAny("cwd"),
  ), s.wrap("archigraph_find_callees", s.handleFindCallees))
  ```
  To:
  ```go
  s.MCP.AddTool(mcpapi.NewTool("archigraph_find_callees",
      mcpapi.WithDescription("Outbound callees of an entity up to N hops."),
      mcpapi.WithString("entity_id", mcpapi.Required()),
      mcpapi.WithNumber("depth", mcpapi.DefaultNumber(1)),
      mcpapi.WithNumber("token_budget", mcpapi.DefaultNumber(800)),
      mcpapi.WithAny("group"),
      mcpapi.WithAny("cwd"),
  ), s.wrap("archigraph_find_callees", s.handleFindCallees))
  ```

- [ ] **Step 9: Run the schema default test — should now PASS**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestDefaultLimitsReduced -v 2>&1 | head -40
  ```

  Expected: PASS for all sub-tests.

- [ ] **Step 10: Run the handshake budget test — should still PASS**

  Adding three `token_budget` params increases the handshake by ~100 chars (~25 tokens). The budget_test ceiling is 3,200.

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestMCPHandshakeBudget -v 2>&1 | head -20
  ```

  Expected: PASS. If it fails with "exceeds ceiling", bump `tokenCeiling` in `budget_test.go` to the measured value + 100 and add a comment `// 2026-05-23 (#1738): +token_budget param on expand/callers/callees/traces`.

- [ ] **Step 11: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/server.go internal/mcp/budget_test.go && git commit -m "feat(#1738): lower default depth/limit + add token_budget schema params"
  ```

---

## Task 2 — Lower defaults in `tools.go` (archigraph_find)

**Files:**
- Modify: `internal/mcp/tools.go:316,409-410`

The `archigraph_find` handler fetches `bm25Hits := r.BM25.Search(question, 50)` and `keep[:25]`. The issue says "top-50 → top-10".

- [ ] **Step 1: Write a failing test for the find-tool limit**

  Add to `internal/mcp/budget_test.go` after `TestDefaultLimitsReduced`:

  ```go
  // TestFindDefaultBudgetParam verifies that archigraph_find has a token_budget
  // default of 800 in its registered schema (#1738).
  func TestFindDefaultBudgetParam(t *testing.T) {
      tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
      if err != nil {
          t.Fatalf("create temp registry: %v", err)
      }
      if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
          t.Fatalf("write temp registry: %v", err)
      }
      tmp.Close()

      srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
      if err != nil {
          t.Fatalf("new server: %v", err)
      }
      byName := srv.MCP.ListTools()
      st, ok := byName["archigraph_find"]
      if !ok {
          t.Fatal("archigraph_find not registered")
      }
      props, ok := st.Tool.InputSchema.Properties["token_budget"]
      if !ok {
          t.Fatal("archigraph_find has no token_budget param")
      }
      propsMap, _ := props.(map[string]any)
      got, _ := propsMap["default"].(float64)
      if got != 800 {
          t.Errorf("archigraph_find token_budget default = %v, want 800", got)
      }
  }
  ```

  Note: `archigraph_find` already HAS `token_budget` as a registered param (server.go line 141). This test will PASS immediately. That is correct — the existing default is already 800. The work for this task is reducing the BM25 fetch count (50→10) and the keep cap (25→10) inside the handler.

- [ ] **Step 2: Run the test to verify it passes (confirm token_budget already present)**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestFindDefaultBudgetParam -v 2>&1
  ```

  Expected: PASS.

- [ ] **Step 3: Write a test proving that find returns ≤10 top matches by default**

  The handler uses `keep[:25]` after fetching 50 BM25 hits. To test this we need a fixture with >10 entities. Add to `internal/mcp/budget_test.go`:

  ```go
  // TestFindTopNReduced verifies that archigraph_find returns at most 10
  // entities by default after the #1738 limit reduction (was 25/50).
  func TestFindTopNReduced(t *testing.T) {
      // Build a doc with 20 named entities.
      entities := make([]graph.Entity, 20)
      for i := range entities {
          entities[i] = graph.Entity{
              ID:         fmt.Sprintf("e%02d", i),
              Name:       fmt.Sprintf("Widget%02d", i),
              Kind:       "Function",
              SourceFile: fmt.Sprintf("src/w%02d.go", i),
              StartLine:  i + 1,
          }
      }
      doc := &graph.Document{Entities: entities}

      srv := mcp.NewTestServerWithDoc(t, doc)
      req := mcpapi.CallToolRequest{}
      req.Params.Arguments = map[string]any{
          "question": "Widget",
          "mode":     "none", // skip BFS expansion so we see raw match count
          "full":     true,   // JSON mode gives us the exact matches slice
          "group":    "test",
      }
      res, err := srv.HandleFind(context.Background(), req)
      if err != nil {
          t.Fatalf("handler error: %v", err)
      }
      if res.IsError {
          t.Fatalf("tool error: %v", res.Content)
      }
      var out map[string]any
      for _, c := range res.Content {
          if tc, ok := c.(mcpapi.TextContent); ok {
              json.Unmarshal([]byte(tc.Text), &out)
          }
      }
      matches, _ := out["matches"].([]any)
      if len(matches) > 10 {
          t.Errorf("archigraph_find returned %d matches, want ≤10 (default limit)", len(matches))
      }
  }
  ```

  **Note:** `mcp.NewTestServerWithDoc` and `srv.HandleFind` are package-internal names. Because `budget_test.go` is in `package mcp_test` (external test package), we need exported helpers. Check the existing pattern: `budget_test.go` imports `mcp` and calls `mcp.NewServer`. The internal test helpers (`newTestServerWithDoc`, `handleQueryGraph`) are only accessible from `package mcp` (white-box tests). The correct approach is to write this test in a NEW file `internal/mcp/find_limit_test.go` using `package mcp` (white-box). See Task 4.

  **For now:** Skip this step. The handler-level test will be written in Task 4 as a white-box test.

- [ ] **Step 4: Lower BM25 fetch count in `handleQueryGraph` (tools.go line 316)**

  Change:
  ```go
  bm25Hits := r.BM25.Search(question, 50)
  ```
  To:
  ```go
  bm25Hits := r.BM25.Search(question, 10)
  ```

  Also change the semantic search count on line 326:
  ```go
  semIDs := r.Semantic.Search(qVec, 50)
  ```
  To:
  ```go
  semIDs := r.Semantic.Search(qVec, 10)
  ```

- [ ] **Step 5: Lower the `keep` cap in `handleQueryGraph` (tools.go ~line 409)**

  Change:
  ```go
  if len(keep) > 25 {
      keep = keep[:25]
  }
  ```
  To:
  ```go
  if len(keep) > 10 {
      keep = keep[:10]
  }
  ```

- [ ] **Step 6: Build to verify no compile errors**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go build ./... 2>&1
  ```

  Expected: no output (clean build).

- [ ] **Step 7: Run all MCP tests**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 60s 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 8: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/tools.go && git commit -m "feat(#1738): shrink archigraph_find BM25 fetch 50→10, keep cap 25→10"
  ```

---

## Task 3 — Lower endpoint handler defaults + enforce token_budget

**Files:**
- Modify: `internal/mcp/endpoint_tools.go:211,389`

The `handleEndpointDefinitions` and `handleEndpointCalls` handlers read `limit := argInt(req, "limit", 50)`. The schema default was changed in Task 1 to 20; now the handler fallback (used when the schema wires no value) must also become 20.

The `maxRenderBytes = 64*1024` hard budget stays as-is. We also need to wire `token_budget` through so callers can pass a tighter or looser byte budget. The `capByRenderedBytes` pattern is already present in both handlers; we just need to make the byte budget come from `token_budget` (treating 1 token ≈ 4 bytes → budget_bytes = token_budget * 4) with a floor of 800 tokens (3200 bytes) and the existing 64 KB as the ceiling for unconstrained calls.

- [ ] **Step 1: Write a failing test that the default limit is 20, not 50**

  Add to `internal/mcp/endpoint_tools_test.go`:

  ```go
  // TestEndpointDefaultLimit verifies that without an explicit limit=N, both
  // handleEndpointDefinitions and handleEndpointCalls return at most 20 items
  // (#1738 default reduction from 50).
  func TestEndpointDefaultLimit(t *testing.T) {
      // Build a doc with 30 endpoint definitions.
      entities := make([]graph.Entity, 30)
      for i := range entities {
          entities[i] = graph.Entity{
              ID:         fmt.Sprintf("ep%02d", i),
              Name:       fmt.Sprintf("GET /api/v1/resource/%02d", i),
              Kind:       "http_endpoint_definition",
              SourceFile: fmt.Sprintf("routes/r%02d.go", i),
              StartLine:  i + 1,
              Properties: map[string]string{
                  "verb": "GET",
                  "path": fmt.Sprintf("/api/v1/resource/%02d", i),
              },
          }
      }
      doc := &graph.Document{Entities: entities}
      srv := newTestServerWithDoc(t, doc)

      // No limit= arg → should use default of 20.
      out := callEndpointTool(t, srv.handleEndpointDefinitions, map[string]any{"group": "test"})
      defs, ok := out["definitions"].([]any)
      if !ok {
          t.Fatalf("definitions missing from response")
      }
      if len(defs) > 20 {
          t.Errorf("handleEndpointDefinitions returned %d items, want ≤20 (default limit)", len(defs))
      }
  }
  ```

  This test requires `"fmt"` import. Check if `endpoint_tools_test.go` already imports it — if not, add it.

- [ ] **Step 2: Run the test to verify it fails**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestEndpointDefaultLimit -v 2>&1 | head -20
  ```

  Expected: FAIL — `handleEndpointDefinitions returned 30 items, want ≤20`.

- [ ] **Step 3: Update `handleEndpointDefinitions` handler default (endpoint_tools.go ~line 211)**

  Change:
  ```go
  limit := argInt(req, "limit", 50)
  ```
  To:
  ```go
  limit := argInt(req, "limit", 20)
  ```

- [ ] **Step 4: Update `handleEndpointCalls` handler default (endpoint_tools.go ~line 389)**

  Change:
  ```go
  limit := argInt(req, "limit", 50)
  ```
  To:
  ```go
  limit := argInt(req, "limit", 20)
  ```

- [ ] **Step 5: Wire token_budget into both endpoint handlers**

  In `handleEndpointDefinitions`, after the existing `const maxRenderBytes = 64 * 1024` line (~line 269), change to:

  ```go
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4 // 4 chars/token conservative estimate
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  out = capByRenderedBytes(out, budgetBytes, !verbose)
  ```

  Remove the old `const maxRenderBytes = 64 * 1024` line and the `out = capByRenderedBytes(out, maxRenderBytes, !verbose)` call that follows it, replacing them with the above block.

  Apply the same change in `handleEndpointCalls` (~line 537), replacing the existing:
  ```go
  const maxRenderBytes = 64 * 1024
  out = capByRenderedBytes(out, maxRenderBytes, !verbose)
  ```
  With:
  ```go
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  out = capByRenderedBytes(out, budgetBytes, !verbose)
  ```

  Also add `"token_budget"` to the response map in both handlers so callers can see the effective budget:
  - In `handleEndpointDefinitions` response map: add `"token_budget": tokenBudget`.
  - In `handleEndpointCalls` response map: add `"token_budget": tokenBudget`.

- [ ] **Step 6: Run the endpoint default limit test — should PASS**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestEndpointDefaultLimit -v 2>&1 | head -20
  ```

  Expected: PASS.

- [ ] **Step 7: Run all MCP tests**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 60s 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 8: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/endpoint_tools.go internal/mcp/endpoint_tools_test.go && git commit -m "feat(#1738): endpoint handlers default limit 50→20, wire token_budget"
  ```

---

## Task 4 — Enforce token_budget in `flow_tools.go` (callers/callees)

**Files:**
- Modify: `internal/mcp/flow_tools.go:32-157,167-286`
- Modify: `internal/mcp/flow_tools_test.go` (add budget tests)

`handleFindCallers` and `handleFindCallees` return unbounded slices today. We apply `capByRenderedBytes` after sorting.

- [ ] **Step 1: Write failing tests for token_budget enforcement in callers/callees**

  Add to `internal/mcp/flow_tools_test.go`:

  ```go
  // TestFindCallers_TokenBudgetEnforced verifies that when token_budget is set
  // to a very small value, the callers slice is capped and a truncation note
  // is present (#1738).
  func TestFindCallers_TokenBudgetEnforced(t *testing.T) {
      // Build a chain A -> B -> ... -> Z (26 callers of the terminal node).
      entities := []graph.Entity{{ID: "target", Name: "Target", Kind: "Function", SourceFile: "t.go", StartLine: 1}}
      rels := []graph.Relationship{}
      for i := 0; i < 25; i++ {
          cid := fmt.Sprintf("caller%02d", i)
          entities = append(entities, graph.Entity{
              ID:         cid,
              Name:       fmt.Sprintf("Caller%02d", i),
              Kind:       "Function",
              SourceFile: fmt.Sprintf("c%02d.go", i),
              StartLine:  i + 1,
          })
          rels = append(rels, graph.Relationship{FromID: cid, ToID: "target", Kind: "CALLS"})
      }
      doc := minDoc(entities, rels)
      srv := newTestServerWithDoc(t, doc)

      // Pass a very tight budget (100 tokens = 400 bytes) — should cap the slice.
      out := callFlowTool(t, srv.handleFindCallers, map[string]any{
          "entity_id":    "target",
          "depth":        float64(1),
          "token_budget": float64(100), // very small — forces truncation
          "group":        "test",
      })
      count, _ := out["count"].(float64)
      truncNote, _ := out["truncation_note"].(string)
      if int(count) >= 25 {
          t.Errorf("expected callers to be capped by token_budget, got count=%v", count)
      }
      if truncNote == "" {
          t.Errorf("expected truncation_note to be set when budget is exceeded")
      }
  }

  // TestFindCallees_TokenBudgetEnforced verifies the same for callees.
  func TestFindCallees_TokenBudgetEnforced(t *testing.T) {
      entities := []graph.Entity{{ID: "root", Name: "Root", Kind: "Function", SourceFile: "r.go", StartLine: 1}}
      rels := []graph.Relationship{}
      for i := 0; i < 25; i++ {
          cid := fmt.Sprintf("callee%02d", i)
          entities = append(entities, graph.Entity{
              ID:         cid,
              Name:       fmt.Sprintf("Callee%02d", i),
              Kind:       "Function",
              SourceFile: fmt.Sprintf("c%02d.go", i),
              StartLine:  i + 1,
          })
          rels = append(rels, graph.Relationship{FromID: "root", ToID: cid, Kind: "CALLS"})
      }
      doc := minDoc(entities, rels)
      srv := newTestServerWithDoc(t, doc)

      out := callFlowTool(t, srv.handleFindCallees, map[string]any{
          "entity_id":    "root",
          "depth":        float64(1),
          "token_budget": float64(100),
          "group":        "test",
      })
      count, _ := out["count"].(float64)
      truncNote, _ := out["truncation_note"].(string)
      if int(count) >= 25 {
          t.Errorf("expected callees to be capped by token_budget, got count=%v", count)
      }
      if truncNote == "" {
          t.Errorf("expected truncation_note to be set when budget is exceeded")
      }
  }
  ```

  Add `"fmt"` to imports in `flow_tools_test.go` if not already present.

- [ ] **Step 2: Run the tests to verify they fail**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run "TestFindCallers_TokenBudgetEnforced|TestFindCallees_TokenBudgetEnforced" -v 2>&1 | head -30
  ```

  Expected: FAIL (count not capped, no truncation_note).

- [ ] **Step 3: Update `handleFindCallers` to read and enforce `token_budget`**

  In `flow_tools.go`, in `handleFindCallers`, BEFORE the final `return jsonResult(result), nil` (after the `sort.Slice` call at ~line 127):

  ```go
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  originalCount := len(callers)
  callers = capByRenderedBytes(callers, budgetBytes, false)
  result := map[string]any{
      "entity_id":   prefixedID(r.Repo, target),
      "entity_name": rootName,
      "repo":        r.Repo,
      "depth":       depth,
      "callers":     callers,
      "count":       len(callers),
  }
  if originalCount > len(callers) {
      result["truncation_note"] = fmt.Sprintf(
          "response capped at token_budget=%d (~%d bytes); %d callers omitted — pass a larger token_budget or reduce depth",
          tokenBudget, budgetBytes, originalCount-len(callers),
      )
  }
  if len(callers) == 0 && originalCount == 0 {
      result["result"] = "no_incoming_edges"
      result["note"] = "Graph shows no callers for this entity within the requested depth. Do not infer a relationship — report the absence."
  }
  return jsonResult(result), nil
  ```

  Remove the old `result` map construction that follows the sort (lines ~143-155).

  Also add `"fmt"` to the imports at the top of `flow_tools.go` if not already present (it already is).

- [ ] **Step 4: Update `handleFindCallees` to read and enforce `token_budget`**

  Apply the same pattern in `handleFindCallees`, replacing the `result` map construction (~lines 271-284):

  ```go
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  originalCount := len(callees)
  callees = capByRenderedBytes(callees, budgetBytes, false)
  result := map[string]any{
      "entity_id":   prefixedID(r.Repo, target),
      "entity_name": rootName,
      "repo":        r.Repo,
      "depth":       depth,
      "callees":     callees,
      "count":       len(callees),
  }
  if originalCount > len(callees) {
      result["truncation_note"] = fmt.Sprintf(
          "response capped at token_budget=%d (~%d bytes); %d callees omitted — pass a larger token_budget or reduce depth",
          tokenBudget, budgetBytes, originalCount-len(callees),
      )
  }
  if len(callees) == 0 && originalCount == 0 {
      result["result"] = "no_outgoing_edges"
      result["note"] = "Graph shows no callees for this entity. Do not infer a relationship — report the absence."
  }
  return jsonResult(result), nil
  ```

  Remove the old `result` map at lines ~271-284.

- [ ] **Step 5: Run the failing tests — should now PASS**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run "TestFindCallers_TokenBudgetEnforced|TestFindCallees_TokenBudgetEnforced" -v 2>&1 | head -30
  ```

  Expected: PASS.

- [ ] **Step 6: Run all flow tests to catch regressions**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run "TestFindCallers|TestFindCallees" -v 2>&1 | tail -30
  ```

  Expected: all PASS.

- [ ] **Step 7: Run full MCP test suite**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 60s 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 8: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/flow_tools.go internal/mcp/flow_tools_test.go && git commit -m "feat(#1738): enforce token_budget in find_callers/find_callees with truncation_note"
  ```

---

## Task 5 — Enforce token_budget in `archigraph_expand` (`handleGetNeighbors`)

**Files:**
- Modify: `internal/mcp/tools.go:692-771`
- Modify: `internal/mcp/flow_tools_test.go` or a new `internal/mcp/expand_budget_test.go`

`handleGetNeighbors` returns a raw `[]map[string]any` slice with no limit or budget enforcement today.

- [ ] **Step 1: Write a failing test for expand token_budget**

  Add to `internal/mcp/flow_tools_test.go` (it's already package mcp):

  ```go
  // TestExpand_TokenBudgetEnforced verifies that archigraph_expand caps its
  // output when token_budget is tight (#1738).
  func TestExpand_TokenBudgetEnforced(t *testing.T) {
      // Build a star graph: root connected to 30 leaf nodes.
      entities := []graph.Entity{{ID: "root", Name: "Root", Kind: "Function", SourceFile: "r.go", StartLine: 1}}
      rels := []graph.Relationship{}
      for i := 0; i < 30; i++ {
          lid := fmt.Sprintf("leaf%02d", i)
          entities = append(entities, graph.Entity{
              ID:         lid,
              Name:       fmt.Sprintf("Leaf%02d", i),
              Kind:       "Function",
              SourceFile: fmt.Sprintf("l%02d.go", i),
              StartLine:  i + 1,
          })
          rels = append(rels, graph.Relationship{FromID: "root", ToID: lid, Kind: "CALLS"})
      }
      doc := minDoc(entities, rels)
      srv := newTestServerWithDoc(t, doc)

      out := callFlowTool(t, srv.handleGetNeighbors, map[string]any{
          "node":         "root",
          "depth":        float64(1),
          "token_budget": float64(100), // forces truncation
          "group":        "test",
      })
      neighbors, _ := out["neighbors"].([]any)
      if neighbors == nil {
          // expand returns a raw array, not a map with "neighbors" key — handle both.
          t.Fatal("unexpected response shape from handleGetNeighbors")
      }
      if len(neighbors) >= 30 {
          t.Errorf("expected neighbors capped by token_budget, got %d", len(neighbors))
      }
  }
  ```

  **Note:** `handleGetNeighbors` currently returns a raw JSON array (`return jsonResult(out), nil`) not a map. The test needs to call it and check the raw array size. Use a different helper that returns a `[]any`:

  ```go
  func callExpandTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) []any {
      t.Helper()
      req := mcpapi.CallToolRequest{}
      req.Params.Arguments = args
      res, err := fn(context.Background(), req)
      if err != nil {
          t.Fatalf("handler error: %v", err)
      }
      if res == nil {
          t.Fatal("nil result")
      }
      if res.IsError {
          t.Fatalf("tool error: %v", res.Content)
      }
      for _, c := range res.Content {
          if tc, ok := c.(mcpapi.TextContent); ok {
              var raw any
              if err := json.Unmarshal([]byte(tc.Text), &raw); err != nil {
                  t.Fatalf("unmarshal: %v", err)
              }
              // Result may be a raw array or a map (no-edge signal).
              if arr, ok := raw.([]any); ok {
                  return arr
              }
              // no_edges case — return empty
              return []any{}
          }
      }
      t.Fatal("no text content")
      return nil
  }
  ```

  Revise `TestExpand_TokenBudgetEnforced` to use `callExpandTool`:

  ```go
  func TestExpand_TokenBudgetEnforced(t *testing.T) {
      entities := []graph.Entity{{ID: "root", Name: "Root", Kind: "Function", SourceFile: "r.go", StartLine: 1}}
      rels := []graph.Relationship{}
      for i := 0; i < 30; i++ {
          lid := fmt.Sprintf("leaf%02d", i)
          entities = append(entities, graph.Entity{
              ID:         lid,
              Name:       fmt.Sprintf("Leaf%02d", i),
              Kind:       "Function",
              SourceFile: fmt.Sprintf("l%02d.go", i),
              StartLine:  i + 1,
          })
          rels = append(rels, graph.Relationship{FromID: "root", ToID: lid, Kind: "CALLS"})
      }
      doc := minDoc(entities, rels)
      srv := newTestServerWithDoc(t, doc)

      result := callExpandTool(t, srv.handleGetNeighbors, map[string]any{
          "node":         "root",
          "depth":        float64(1),
          "token_budget": float64(100),
          "group":        "test",
      })
      if len(result) >= 30 {
          t.Errorf("expected neighbors capped by token_budget, got %d", len(result))
      }
  }
  ```

- [ ] **Step 2: Run the test to verify it fails**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestExpand_TokenBudgetEnforced -v 2>&1 | head -20
  ```

  Expected: FAIL (30 neighbors returned, no truncation).

- [ ] **Step 3: Update `handleGetNeighbors` in `tools.go` to enforce `token_budget`**

  In `handleGetNeighbors`, before the final `return jsonResult(out), nil` (after the cross-repo link additions, ~line 770):

  ```go
  // #1738: apply token-budget cap. capByRenderedBytes needs a typed slice.
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  out = capByRenderedBytes(out, budgetBytes, false)
  ```

  The `out` variable is `[]map[string]any`. `capByRenderedBytes` is generic (`[T any]`) so it accepts this type without changes.

- [ ] **Step 4: Run the test — should now PASS**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestExpand_TokenBudgetEnforced -v 2>&1 | head -20
  ```

  Expected: PASS.

- [ ] **Step 5: Run all MCP tests**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 60s 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 6: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/tools.go internal/mcp/flow_tools_test.go && git commit -m "feat(#1738): enforce token_budget in archigraph_expand"
  ```

---

## Task 6 — Enforce token_budget in `traces.go` (action=list)

**Files:**
- Modify: `internal/mcp/traces.go:59-144`
- Modify: `internal/mcp/traces_test.go` (add budget test)

- [ ] **Step 1: Write a failing test for traces list token_budget**

  Add to `internal/mcp/traces_test.go`:

  ```go
  // TestTracesList_TokenBudgetEnforced verifies that archigraph_traces action=list
  // caps output at the token_budget (#1738).
  func TestTracesList_TokenBudgetEnforced(t *testing.T) {
      // Build a doc with 20 SCOPE.Process entities.
      entities := make([]graph.Entity, 20)
      for i := range entities {
          entities[i] = graph.Entity{
              ID:   fmt.Sprintf("proc%02d", i),
              Name: fmt.Sprintf("Process%02d", i),
              Kind: "SCOPE.Process",
              Properties: map[string]string{
                  "step_count":  "10",
                  "cross_stack": "false",
                  "entry_id":    fmt.Sprintf("e%02d", i),
                  "entry_name":  fmt.Sprintf("Entry%02d", i),
                  "terminal_id": fmt.Sprintf("t%02d", i),
              },
          }
      }
      doc := &graph.Document{Entities: entities}
      srv := newTestServerWithDoc(t, doc)

      // Tight budget forces truncation.
      req := mcpapi.CallToolRequest{}
      req.Params.Arguments = map[string]any{
          "action":       "list",
          "token_budget": float64(100),
          "min_steps":    float64(0), // include all flows
          "group":        "test",
      }
      res, err := srv.handleTraces(context.Background(), req)
      if err != nil {
          t.Fatalf("handler error: %v", err)
      }
      if res.IsError {
          t.Fatalf("tool error: %v", res.Content)
      }
      var out map[string]any
      for _, c := range res.Content {
          if tc, ok := c.(mcpapi.TextContent); ok {
              json.Unmarshal([]byte(tc.Text), &out)
          }
      }
      count, _ := out["count"].(float64)
      if int(count) >= 20 {
          t.Errorf("traces list returned %v items, want <20 (budget cap)", count)
      }
      truncNote, _ := out["truncation_note"].(string)
      if truncNote == "" {
          t.Errorf("expected truncation_note when budget is exceeded")
      }
  }
  ```

  Check `traces_test.go` imports — add `"fmt"`, `"encoding/json"`, `"context"`, `"testing"`, and `mcpapi "github.com/mark3labs/mcp-go/mcp"` if missing.

- [ ] **Step 2: Run the test to verify it fails**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestTracesList_TokenBudgetEnforced -v 2>&1 | head -30
  ```

  Expected: FAIL (count = 20, no truncation_note).

- [ ] **Step 3: Update `handleTracesList` in `traces.go` to lower default and enforce budget**

  Current code (after sort, ~line 136):
  ```go
  if len(items) > limit {
      items = items[:limit]
  }
  return jsonResult(map[string]any{
      "processes": items,
      "count":     len(items),
  }), nil
  ```

  Replace with:
  ```go
  if len(items) > limit {
      items = items[:limit]
  }
  tokenBudget := argInt(req, "token_budget", 800)
  if tokenBudget < 100 {
      tokenBudget = 100
  }
  budgetBytes := tokenBudget * 4
  if budgetBytes > 64*1024 {
      budgetBytes = 64 * 1024
  }
  originalCount := len(items)
  items = capByRenderedBytes(items, budgetBytes, false)
  resp := map[string]any{
      "processes": items,
      "count":     len(items),
  }
  if originalCount > len(items) {
      resp["truncation_note"] = fmt.Sprintf(
          "response capped at token_budget=%d (~%d bytes); %d processes omitted — pass a larger token_budget or use limit=N",
          tokenBudget, budgetBytes, originalCount-len(items),
      )
  }
  return jsonResult(resp), nil
  ```

  Also change the default `limit` from 25 to 10 in `handleTracesList` (~line 64):
  ```go
  limit := argInt(req, "limit", 25)
  if limit <= 0 {
      limit = 25
  }
  ```
  To:
  ```go
  limit := argInt(req, "limit", 10)
  if limit <= 0 {
      limit = 10
  }
  ```

  Add `"fmt"` to imports in `traces.go` if not present.

- [ ] **Step 4: Run the failing test — should PASS**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestTracesList_TokenBudgetEnforced -v 2>&1 | head -20
  ```

  Expected: PASS.

- [ ] **Step 5: Run all traces tests**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestTraces -v 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 6: Run full MCP test suite**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 60s 2>&1 | tail -20
  ```

  Expected: all PASS.

- [ ] **Step 7: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/traces.go internal/mcp/traces_test.go && git commit -m "feat(#1738): traces list default limit 25→10, enforce token_budget"
  ```

---

## Task 7 — Update SCHEMA.md

**Files:**
- Modify: `internal/mcp/SCHEMA.md`

- [ ] **Step 1: Update `archigraph_find` table**

  Locate the `archigraph_find` parameters table (~line 239). The `token_budget` row already exists with default `800`. No change needed there. No `limit` param exists — the "top-N" is an internal BM25 fetch count, not exposed in the schema. Add a note bullet:

  After the existing notes (after the "IDs are prefixed" bullet), add:
  ```
  - **Token economy (#1738):** Internal candidate pool reduced from 50→10; BFS expansion capped at 10 seed nodes. Pass `token_budget=N` to adjust the compact-text byte budget (default 800 tokens ≈ 3,200 bytes).
  ```

- [ ] **Step 2: Update `archigraph_expand` table (~line 344)**

  Change the `depth` default row from `2` → `1` and add a `token_budget` row:

  | `depth` | number | no | `1` | BFS depth. Default reduced from 2 (#1738). |
  | `token_budget` | number | no | `800` | Max approximate tokens; response is capped via binary-search rendering. |

- [ ] **Step 3: Update `archigraph_traces` table (~line 445)**

  Change the `limit` default row from `25` → `10` and add a `token_budget` row:

  | `limit` | number | no | `10` | (`list`) Max processes returned. Default reduced from 25 (#1738). |
  | `token_budget` | number | no | `800` | (`list`) Response byte cap; items are shed from the tail. |

- [ ] **Step 4: Update `archigraph_endpoints` description table (~line 340 of server.go registration, reflected in SCHEMA.md)**

  In the SCHEMA.md `archigraph_endpoints` section (find via grep for "### `archigraph_endpoints`"), update the `limit` row default from `50` → `20` and add a `token_budget` row. Also update `archigraph_endpoint_definitions` and `archigraph_endpoint_calls` legacy sections if they exist (they show `200` — those are different legacy tool entries, leave them unless grep shows they also need updating).

  The unified `archigraph_endpoints` schema in SCHEMA.md shows:

  | `limit` | number | no | `50` | Max results in this page. |

  Change to:

  | `limit` | number | no | `20` | Max results in this page. Default reduced from 50 (#1738). |
  | `token_budget` | number | no | `800` | Response byte cap; items shed from tail when exceeded. |

- [ ] **Step 5: Update `archigraph_find_callers` and `archigraph_find_callees` tables**

  Find the `### \`archigraph_find_callers\`` and `### \`archigraph_find_callees\`` sections. Add a `token_budget` row to each parameter table:

  | `token_budget` | number | no | `800` | Max approximate tokens; callers/callees list is capped via binary-search rendering (#1738). |

  The `depth` default is already `1` in both tables (confirmed from current server.go). Verify and leave as-is.

- [ ] **Step 6: Build to verify no issues introduced**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go build ./... && go vet ./... 2>&1
  ```

  Expected: no output.

- [ ] **Step 7: Commit**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git add internal/mcp/SCHEMA.md && git commit -m "docs(#1738): update SCHEMA.md with narrowed defaults and token_budget params"
  ```

---

## Task 8 — Final verification and PR

**Files:**
- None modified — verification only, then PR creation.

- [ ] **Step 1: Run the full MCP test suite one last time**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -timeout 120s -count=1 2>&1 | tail -30
  ```

  Expected: all PASS, no FAIL lines.

- [ ] **Step 2: Run go vet**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go vet ./... 2>&1
  ```

  Expected: no output.

- [ ] **Step 3: Run the handshake budget test to confirm the ceiling is not broken**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && go test ./internal/mcp/... -run TestMCPHandshakeBudget -v 2>&1
  ```

  Expected: PASS. If it fails, bump `tokenCeiling` by the measured delta and add a comment.

- [ ] **Step 4: Push and open PR**

  ```bash
  cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/token-default-limits && git push -u origin feat/default-limits-shrink
  ```

  Then open PR to main:

  ```bash
  gh pr create \
    --title "[#1738] Token sprint Tier-1.2: shrink default limits + enforce token_budget across list tools" \
    --body "$(cat <<'EOF'
  ## What

  Implements token sprint Tier-1.2 (#1738): narrows default response limits on every list-returning MCP tool and extends the `capByRenderedBytes` byte-budget enforcement pattern to all of them.

  ## Changes

  ### Default limit reductions
  | Tool | Parameter | Old default | New default |
  |------|-----------|-------------|-------------|
  | `archigraph_find` | BM25 candidate pool (internal) | 50 | 10 |
  | `archigraph_find` | BFS seed keep cap (internal) | 25 | 10 |
  | `archigraph_endpoints` | `limit` | 50 | 20 |
  | `archigraph_traces` (list) | `limit` | 25 | 10 |
  | `archigraph_expand` | `depth` | 2 | 1 |

  ### New `token_budget` enforcement
  All six list-returning tools now accept `token_budget=N` (default 800 tokens ≈ 3,200 bytes) and hard-cap their response via binary-search rendering (`capByRenderedBytes`):
  - `archigraph_endpoints` (definitions + calls actions)
  - `archigraph_find_callers`
  - `archigraph_find_callees`
  - `archigraph_expand`
  - `archigraph_traces` (list action)

  When the budget is exceeded the response includes a `truncation_note` field explaining how many items were omitted and how to get more.

  ## Tests added
  - `TestDefaultLimitsReduced` — schema-level assertion that all five default values are at the narrowed values.
  - `TestEndpointDefaultLimit` — handler-level test with 30 fixture endpoints; verifies ≤20 returned.
  - `TestFindCallers_TokenBudgetEnforced` — 25-caller fixture, budget=100 tokens, asserts cap + truncation_note.
  - `TestFindCallees_TokenBudgetEnforced` — same for callees.
  - `TestExpand_TokenBudgetEnforced` — 30-leaf star graph, budget=100 tokens, asserts cap.
  - `TestTracesList_TokenBudgetEnforced` — 20 Process entities, budget=100 tokens, asserts cap + truncation_note.

  ## SCHEMA.md
  Parameter tables updated with new defaults and `token_budget` rows for all affected tools.

  Fixes #1738

  Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
  EOF
  )"
  ```

- [ ] **Step 5: Verify PR URL is returned and share it**

  ```bash
  gh pr view --json url -q .url
  ```

---

## Self-Review

### Spec Coverage Check

| Requirement | Task |
|-------------|------|
| `archigraph_find` default top-50 → top-10 | Task 2 (BM25 fetch + keep cap) |
| `archigraph_endpoints` default 50 → 20 | Task 1 (schema) + Task 3 (handler) |
| `archigraph_find_callers`/`find_callees` default depth=2 → depth=1 | Task 1 (note: already 1 in code, confirmed) |
| `archigraph_expand` depth=2 → depth=1 | Task 1 (schema) — handler reads depth from arg |
| `archigraph_traces` action=list default 25 → 10 | Task 1 (schema) + Task 6 (handler) |
| `capByRenderedBytes` extended to all list tools | Tasks 3, 4, 5, 6 |
| Default `token_budget=800` per call | Tasks 3–6 |
| Hard-enforce via binary-search rendering | Tasks 3–6 (uses existing `capByRenderedBytes`) |
| Stop emitting items when budget exceeded | Tasks 3–6 (capByRenderedBytes truncates tail) |
| Include truncation note | Tasks 4, 6 (callers, callees, traces — expand is raw array, note not added there yet) |
| Update in-tool descriptions + SCHEMA.md | Task 7 |
| go build/vet + mcp tests green | Tasks 2, 3, 4, 5, 6, 8 |
| Tests that token_budget is enforced | Tasks 3, 4, 5, 6 |

### Gap: `archigraph_expand` truncation note

`handleGetNeighbors` returns a raw `[]map[string]any` (not a wrapper object), so there is no `truncation_note` field in the response shape. To stay consistent with the spec and other tools, Task 5 Step 3 should be updated: instead of returning a raw array when truncated, wrap in a response object:

```go
if originalLen > len(out) {
    return jsonResult(map[string]any{
        "neighbors":       out,
        "count":           len(out),
        "truncation_note": fmt.Sprintf("capped at token_budget=%d; %d neighbors omitted", tokenBudget, originalLen-len(out)),
    }), nil
}
return jsonResult(out), nil
```

This is a backward-compatible change: if not truncated, the raw array form is preserved. Add this to Task 5 Step 3.

### Gap: `archigraph_find` does not expose `limit` in schema

The BM25 fetch count (50→10) is internal. The schema already exposes `token_budget`. No schema change needed for the internal fetch count — this is intentional per the spec ("opt-in to more via `limit=N`"). The spec mentions `archigraph_find default top-50 → top-10` which refers to the internal candidate pool, not a schema `limit` param. Handled correctly in Task 2.

### Placeholder Scan

No TBD/TODO/placeholder text found in plan.

### Type Consistency

- `capByRenderedBytes[T any]` — generic, works on any slice type. Confirmed used on `[]endpointDefItem`, `[]endpointCallItem`, `[]caller`, `[]callee`, `[]map[string]any`, `[]listItem`. All correct.
- `argInt(req, "token_budget", 800)` — consistent across all handlers.
- `truncation_note` — consistent field name across callers, callees, traces. Expand uses same name in the wrapped object path.
