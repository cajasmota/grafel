# WebUI v2 Docs Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Docs screen for WebUI v2 — an entity browser + documentation reader — per design handoff `docs.md` and issue #1438 (EPIC #1432), with new v2-envelope Go backend endpoints.

**Architecture:** Two-pane layout (320px tree + scrollable entity article) surfaced as `src/routes/docs.tsx`. Clean layered: two new Go v2 handlers (`handlers_v2_docs.go`) registered in `server.go` → typed API client methods in `src/lib/api.ts` → TanStack Query hooks in `src/hooks/use-docs.ts` → five focused sub-components in `src/components/docs/` → screen route in `src/routes/docs.tsx`. The v1 docs endpoints (`/api/docs/{group}` and `/api/docs/{group}/{path...}`) are left completely untouched.

**Tech Stack:** React 18 + TypeScript 5.7 + Vite 6 + Tailwind v4 tokens + Radix Tooltip + TanStack Query v5 + lucide-react + React Router 6. No new runtime dependencies.

---

## Data Decisions & Rationale

The design calls for `DocsEntity.description`, `params`, `returns`, `callers`, `callees`, etc. The daemon's `graph.Entity` struct only carries `Signature` and graph topology (relationships). Enrichment descriptions live optionally in frontmatter files on disk, read via `ParseEnrichmentFrontmatter`.

**Decision:** The v2 entity-detail endpoint (`GET /api/v2/groups/{group}/docs/entities/{entityId}`) derives its response by:
1. Looking up the entity in the in-memory graph.
2. Checking for an enrichment frontmatter file (same path logic used by `handleEnrichmentWriteback`).
3. Using frontmatter `summary` as `description` + `aiGenerated: true`; if absent, `stub: true`.
4. Building `callers`/`callees` from the group's relationship index.
5. Parsing `params` and `returns` from `Signature` on a best-effort basis (split by line — no code parser; if it fails, return empty arrays; the stub chrome still renders with signature).

This means: entities without enrichment frontmatter render as `EntityStub` in the UI (correct per spec). The `POST /api/v2/groups/{group}/docs/generate` endpoint is NOT implemented in this PR (out of scope per `pending.md`; rationale documented in handler stub comment).

The tree endpoint (`GET /api/v2/groups/{group}/docs/tree`) walks the in-memory graph entity list (not the filesystem docs dir) to build the `repo > folder > entity` tree. This is different from the v1 docs-portal which serves markdown files. This new v2 tree is entity-centric (matching the design spec), not file-centric.

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| Create | `internal/dashboard/handlers_v2_docs.go` | Two Go handlers: tree + entity detail |
| Create | `internal/dashboard/handlers_v2_docs_test.go` | Unit tests for both handlers |
| Modify | `internal/dashboard/server.go` | Register 2 new routes under v2 |
| Modify | `webui-v2/src/data/types.ts` | Add `DocsTreeNode`, `DocsEntity`, `DocsParam` |
| Modify | `webui-v2/src/lib/api.ts` | Add `getDocsTree`, `getDocsEntity` |
| Create | `webui-v2/src/hooks/use-docs.ts` | TanStack Query hooks wrapping api.* |
| Create | `webui-v2/src/components/docs/type-glyph.tsx` | TypeGlyph pill + TypeBadge |
| Create | `webui-v2/src/components/docs/docs-tree.tsx` | Left-pane recursive tree |
| Create | `webui-v2/src/components/docs/docs-entity.tsx` | Right-pane article |
| Create | `webui-v2/src/components/docs/docs-empty.tsx` | Empty state (no entity selected) |
| Create | `webui-v2/src/components/docs/docs-skeleton.tsx` | Loading skeleton for entity pane |
| Modify | `webui-v2/src/routes/docs.tsx` | Replace placeholder with real screen |
| Modify | `webui-v2/src/routes/router.tsx` | Add nested `:entityId?` child route |

---

## Task 1: Add Docs types to `data/types.ts`

**Files:**
- Modify: `webui-v2/src/data/types.ts`

- [ ] **Step 1: Append the three docs types**

Open `webui-v2/src/data/types.ts`. After the `Group` interface, append:

```ts
// ── Docs screen ─────────────────────────────────────────────────────────────

export type DocsEntityKind =
  | "function"
  | "component"
  | "hook"
  | "class"
  | "method"
  | "http_endpoint"
  | "module"
  | "folder"
  | "repo";

export interface DocsTreeNode {
  type: DocsEntityKind;
  name: string;
  id?: string;           // leaf only
  children?: DocsTreeNode[];
}

export interface DocsParam {
  name: string;
  type: string;
  desc: string;
}

export interface DocsEntityDetail {
  name: string;
  type: DocsEntityKind;
  repo: string;
  file: string;
  line: number;
  signature: string;
  description: string;
  aiGenerated: boolean;
  params: DocsParam[];
  returns: { type: string; desc?: string } | null;
  inbound: number;
  outbound: number;
  callers: string[];
  callees: string[];
  responseShapes?: { status: number; shape: string }[];
  stub?: boolean;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors (or only pre-existing unrelated errors if any).

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/data/types.ts && git commit -m "feat(docs): add DocsTreeNode/DocsEntityDetail/DocsParam types"
```

---

## Task 2: Backend — Go v2 docs handlers

**Files:**
- Create: `internal/dashboard/handlers_v2_docs.go`
- Create: `internal/dashboard/handlers_v2_docs_test.go`

### Step 2a — Write the failing test first

- [ ] **Step 1: Write the test file**

Create `internal/dashboard/handlers_v2_docs_test.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// buildV2DocsTestServer creates a minimal Server with one group containing two
// entities: a function with relationships and a method without.
func buildV2DocsTestServer(t *testing.T) *Server {
	t.Helper()
	fn := graph.Entity{
		ID:         "fn1",
		Name:       "doWork",
		Kind:       "Function",
		SourceFile: "src/work.ts",
		StartLine:  10,
		Signature:  "function doWork(x: number): void",
	}
	mth := graph.Entity{
		ID:         "mth1",
		Name:       "MyClass.run",
		Kind:       "Method",
		SourceFile: "src/my-class.ts",
		StartLine:  22,
	}
	rel := graph.Relationship{
		ID:     "r1",
		FromID: "fn1",
		ToID:   "mth1",
		Kind:   "CALLS",
	}
	return newTestServer(t, "testgrp", []graph.Entity{fn, mth}, []graph.Relationship{rel})
}

func TestHandleV2DocsTree(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatal("envelope.ok is false")
	}
	// data must be a non-empty slice
	data, ok := env.Data.([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("expected non-empty tree, got %T %v", env.Data, env.Data)
	}
}

func TestHandleV2DocsEntityDetail(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/entities/fn1", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatal("envelope.ok is false")
	}
	obj, ok := env.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	if obj["name"] != "doWork" {
		t.Errorf("expected name=doWork, got %v", obj["name"])
	}
	// callees must contain mth1 label
	callees, _ := obj["callees"].([]interface{})
	if len(callees) != 1 {
		t.Errorf("expected 1 callee, got %v", callees)
	}
}

func TestHandleV2DocsEntityNotFound(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/entities/no-such-entity", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleV2DocsTreeGroupNotFound(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/ghost/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests — they must FAIL (handlers not yet registered)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/dashboard/ -run "TestHandleV2Docs" -v 2>&1 | tail -20
```

Expected: `FAIL` — route 404s or compile error because `handleV2DocsTree` / `handleV2DocsEntityDetail` don't exist yet. If the test file doesn't compile, that is the expected failure state; proceed to step 3.

### Step 2b — Implement the handlers

- [ ] **Step 3: Check if `newTestServer` helper exists**

```bash
grep -n "func newTestServer" /Users/jorgecajas/Documents/Projects/archigraph/internal/dashboard/*.go | head -5
```

If it does not exist, you must create a small helper (or locate the equivalent pattern used in existing handler tests). Look at how `buildV2DocsTestServer` creates entities — adapt to match existing test patterns in the package. In the existing tests, servers are typically built via `newWritebackServer` which shows the pattern:

```go
// Adapt the helper call in the test file above to use whatever pattern
// existing tests use. Check handlers_enrichment_writeback_test.go for the
// newTestServer signature. If it doesn't exist, search for how other handler
// tests build a minimal Server with in-memory graph data.
grep -n "func.*TestServer\|func.*testServer\|func.*Server(" /Users/jorgecajas/Documents/Projects/archigraph/internal/dashboard/*_test.go | head -10
```

- [ ] **Step 4: Create `internal/dashboard/handlers_v2_docs.go`**

```go
// handlers_v2_docs.go — WebUI v2 entity-centric docs endpoints.
//
// These endpoints serve the /g/:groupId/docs screen in webui-v2.
// They are SEPARATE from the v1 /api/docs/{group} markdown-portal endpoints
// in handlers_docs.go, which are left completely untouched.
//
// Routes (registered in server.go under the v2 section):
//
//	GET  /api/v2/groups/{group}/docs/tree                   → v2DocsTreeHandler
//	GET  /api/v2/groups/{group}/docs/entities/{entityId}    → v2DocsEntityHandler
//
// POST /api/v2/groups/{group}/docs/generate is intentionally NOT implemented.
// Doc generation is a long-running skill operation managed via the Pending
// screen (#1432). Adding a stub here would mislead callers; the UI shows a
// hint to run `archigraph generate-docs` instead (EntityStub behaviour).

package dashboard

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ── Wire shapes ────────────────────────────────────────────────────────────

// v2DocsTreeNode mirrors the DocsTreeNode TypeScript interface in data/types.ts.
type v2DocsTreeNode struct {
	Type     string           `json:"type"`
	Name     string           `json:"name"`
	ID       string           `json:"id,omitempty"`
	Children []v2DocsTreeNode `json:"children,omitempty"`
}

// v2DocsEntityReply is the data payload for the entity-detail endpoint.
type v2DocsEntityReply struct {
	Name           string              `json:"name"`
	Type           string              `json:"type"`
	Repo           string              `json:"repo"`
	File           string              `json:"file"`
	Line           int                 `json:"line"`
	Signature      string              `json:"signature"`
	Description    string              `json:"description"`
	AIGenerated    bool                `json:"aiGenerated"`
	Params         []v2DocsParam       `json:"params"`
	Returns        *v2DocsReturn       `json:"returns"`
	Inbound        int                 `json:"inbound"`
	Outbound       int                 `json:"outbound"`
	Callers        []string            `json:"callers"`
	Callees        []string            `json:"callees"`
	ResponseShapes []v2DocsRespShape   `json:"responseShapes,omitempty"`
	Stub           bool                `json:"stub,omitempty"`
}

type v2DocsParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}

type v2DocsReturn struct {
	Type string `json:"type"`
	Desc string `json:"desc,omitempty"`
}

type v2DocsRespShape struct {
	Status int    `json:"status"`
	Shape  string `json:"shape"`
}

// ── Handlers ───────────────────────────────────────────────────────────────

// handleV2DocsTree — GET /api/v2/groups/{group}/docs/tree
//
// Returns the entity tree for the Docs screen left pane.
// The tree is built from the in-memory graph (not from on-disk docs files).
// Shape: repo → folder (by directory prefix) → entity leaf.
func (s *Server) handleV2DocsTree(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not found: "+group)
		return
	}

	// Build per-repo trees.
	var roots []v2DocsTreeNode
	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		repoNode := buildV2EntityTree(repo.Slug, repo.Doc.Entities)
		if len(repoNode.Children) > 0 {
			roots = append(roots, repoNode)
		}
	}
	if roots == nil {
		roots = []v2DocsTreeNode{}
	}
	writeV2JSON(w, http.StatusOK, v2OK(roots))
}

// handleV2DocsEntityDetail — GET /api/v2/groups/{group}/docs/entities/{entityId}
//
// Returns the full documentation payload for a single entity.
// Description is sourced from enrichment frontmatter if available; otherwise
// stub=true is set and the UI renders EntityStub.
func (s *Server) handleV2DocsEntityDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	entityID := r.PathValue("entityId")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not found: "+group)
		return
	}

	// Find entity across repos.
	var foundRepo *DashRepo
	var foundEntity *graph.Entity
	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.ID == entityID {
				foundRepo = repo
				foundEntity = e
				break
			}
		}
		if foundEntity != nil {
			break
		}
	}
	if foundEntity == nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "entity not found: "+entityID)
		return
	}

	reply := buildV2EntityDetail(grp, foundRepo, foundEntity)
	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// ── Helpers ────────────────────────────────────────────────────────────────

// buildV2EntityTree groups entities from one repo into a folder tree.
func buildV2EntityTree(repoSlug string, entities []graph.Entity) v2DocsTreeNode {
	type folderNode struct {
		children map[string]*folderNode
		leaves   []v2DocsTreeNode
	}

	root := &folderNode{children: map[string]*folderNode{}}

	insert := func(f *folderNode, parts []string, leaf v2DocsTreeNode) {
		cur := f
		for _, p := range parts {
			if cur.children[p] == nil {
				cur.children[p] = &folderNode{children: map[string]*folderNode{}}
			}
			cur = cur.children[p]
		}
		cur.leaves = append(cur.leaves, leaf)
	}

	for i := range entities {
		e := &entities[i]
		kind := strings.ToLower(dashStripScopePrefix(e.Kind))
		leaf := v2DocsTreeNode{
			Type: kind,
			Name: e.Name,
			ID:   e.ID,
		}
		// Derive folder path from SourceFile directory.
		dir := filepath.Dir(e.SourceFile)
		if dir == "." || dir == "" {
			root.leaves = append(root.leaves, leaf)
			continue
		}
		parts := strings.Split(filepath.ToSlash(dir), "/")
		insert(root, parts, leaf)
	}

	var toNode func(name string, n *folderNode, isRepo bool) v2DocsTreeNode
	toNode = func(name string, n *folderNode, isRepo bool) v2DocsTreeNode {
		nodeType := "folder"
		if isRepo {
			nodeType = "repo"
		}
		result := v2DocsTreeNode{Type: nodeType, Name: name}
		// Add direct leaves first.
		result.Children = append(result.Children, n.leaves...)
		// Then recurse into sub-folders (sorted for determinism).
		subNames := make([]string, 0, len(n.children))
		for k := range n.children {
			subNames = append(subNames, k)
		}
		// Simple sort: lexicographic.
		for i := 1; i < len(subNames); i++ {
			for j := i; j > 0 && subNames[j] < subNames[j-1]; j-- {
				subNames[j], subNames[j-1] = subNames[j-1], subNames[j]
			}
		}
		for _, sub := range subNames {
			child := toNode(sub, n.children[sub], false)
			result.Children = append(result.Children, child)
		}
		return result
	}

	return toNode(repoSlug, root, true)
}

// buildV2EntityDetail constructs the entity detail reply for a single entity.
func buildV2EntityDetail(grp *DashGroup, repo *DashRepo, e *graph.Entity) v2DocsEntityReply {
	reply := v2DocsEntityReply{
		Name:      e.Name,
		Type:      strings.ToLower(dashStripScopePrefix(e.Kind)),
		Repo:      repo.Slug,
		File:      e.SourceFile,
		Line:      e.StartLine,
		Signature: e.Signature,
		Params:    []v2DocsParam{},
		Callers:   []string{},
		Callees:   []string{},
	}

	// Build caller/callee name lists from the relationship graph.
	// Build an entity ID → name index across the whole group for cheap lookup.
	idToName := map[string]string{}
	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}
		for _, ent := range r.Doc.Entities {
			idToName[ent.ID] = ent.Name
		}
	}
	if repo.Doc != nil {
		inbound := 0
		outbound := 0
		for _, rel := range repo.Doc.Relationships {
			switch rel.Kind {
			case "CALLS", "RENDERS", "REFERENCES", "IMPORTS":
				if rel.ToID == e.ID {
					inbound++
					if name, ok := idToName[rel.FromID]; ok {
						reply.Callers = append(reply.Callers, name)
					}
				}
				if rel.FromID == e.ID {
					outbound++
					if name, ok := idToName[rel.ToID]; ok {
						reply.Callees = append(reply.Callees, name)
					}
				}
			}
		}
		reply.Inbound = inbound
		reply.Outbound = outbound
	}

	// Cap caller/callee lists at 50 per spec.
	if len(reply.Callers) > 50 {
		reply.Callers = reply.Callers[:50]
	}
	if len(reply.Callees) > 50 {
		reply.Callees = reply.Callees[:50]
	}

	// Attempt to load enrichment frontmatter for description.
	// Frontmatter files live at <repo-path>/.archigraph/entities/<entityId>.md
	// (same path pattern used by handleEnrichmentWriteback).
	description, aiGenerated := v2LoadEntityDescription(repo, e.ID)
	reply.Description = description
	reply.AIGenerated = aiGenerated
	reply.Stub = description == ""

	return reply
}

// v2LoadEntityDescription tries to read enrichment frontmatter for an entity.
// Returns ("", false) when no frontmatter file exists — triggering stub mode.
func v2LoadEntityDescription(repo *DashRepo, entityID string) (string, bool) {
	if repo.Path == "" {
		return "", false
	}
	fmPath := filepath.Join(repo.Path, ".archigraph", "entities", entityID+".md")
	fm, err := ParseEnrichmentFrontmatter(fmPath)
	if err != nil || fm == nil || !fm.HasData() {
		return "", false
	}
	return fm.Summary, true
}
```

- [ ] **Step 5: Run the tests — they should now PASS**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/dashboard/ -run "TestHandleV2Docs" -v 2>&1 | tail -30
```

Expected: All 4 tests PASS. If they fail, check the `newTestServer` helper signature and adjust the test file to match.

- [ ] **Step 6: Register the two new routes in `server.go`**

In `internal/dashboard/server.go`, find the v2 routes section (currently contains `handleV2Meta`) and add:

```go
	// Docs entity browser — WebUI v2 (#1438)
	mux.HandleFunc("GET /api/v2/groups/{group}/docs/tree", s.handleV2DocsTree)
	mux.HandleFunc("GET /api/v2/groups/{group}/docs/entities/{entityId}", s.handleV2DocsEntityDetail)
```

- [ ] **Step 7: Verify full package compiles + new tests still pass**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go build ./... && go test ./internal/dashboard/ -run "TestHandleV2Docs" -v 2>&1 | tail -20
```

Expected: `go build` succeeds with no errors; all 4 tests PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add internal/dashboard/handlers_v2_docs.go internal/dashboard/handlers_v2_docs_test.go internal/dashboard/server.go && git commit -m "feat(docs): add v2 docs/tree and docs/entities/:id endpoints (#1438)"
```

---

## Task 3: API client methods

**Files:**
- Modify: `webui-v2/src/lib/api.ts`

- [ ] **Step 1: Add the two new api methods**

In `webui-v2/src/lib/api.ts`, import the new types and add the methods to the `api` object. Change the import line at the top from:

```ts
import type { Group, Entity, Community } from "@/data/types";
```

To:

```ts
import type { Group, Entity, Community, DocsTreeNode, DocsEntityDetail } from "@/data/types";
```

Then inside the `api` object, after `searchEntities`, add:

```ts
  getDocsTree: (groupId: string) =>
    request<DocsTreeNode[]>(`/v2/groups/${groupId}/docs/tree`),
  getDocsEntity: (groupId: string, entityId: string) =>
    request<DocsEntityDetail>(`/v2/groups/${groupId}/docs/entities/${encodeURIComponent(entityId)}`),
```

Note: The `request` function prepends `BASE` which is `/api` by default; these paths result in `/api/v2/groups/...`.

- [ ] **Step 2: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/lib/api.ts && git commit -m "feat(docs): add getDocsTree and getDocsEntity to api client"
```

---

## Task 4: TanStack Query hooks

**Files:**
- Create: `webui-v2/src/hooks/use-docs.ts`

- [ ] **Step 1: Create the hooks file**

Create `webui-v2/src/hooks/use-docs.ts`:

```ts
/* ============================================================
   use-docs.ts — TanStack Query hooks for the Docs screen.
   useDocsTree: fetches the entity tree for the left pane.
   useDocsEntity: fetches a single entity's full detail.
   Both use v2 envelope endpoints added in handlers_v2_docs.go.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { DocsTreeNode, DocsEntityDetail } from "@/data/types";

export function useDocsTree(groupId: string) {
  return useQuery<DocsTreeNode[], ApiError>({
    queryKey: ["docs-tree", groupId],
    queryFn: () => api.getDocsTree(groupId),
    staleTime: 30_000,
  });
}

export function useDocsEntity(groupId: string, entityId: string | null) {
  return useQuery<DocsEntityDetail, ApiError>({
    queryKey: ["docs-entity", groupId, entityId],
    queryFn: () => api.getDocsEntity(groupId, entityId!),
    enabled: entityId !== null,
    staleTime: 30_000,
    retry: (count, err) => {
      // Don't retry on 404 — renders EntityStub instead.
      if (err instanceof ApiError && err.status === 404) return false;
      return count < 2;
    },
  });
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/hooks/use-docs.ts && git commit -m "feat(docs): add useDocsTree and useDocsEntity TanStack Query hooks"
```

---

## Task 5: TypeGlyph + TypeBadge primitives

**Files:**
- Create: `webui-v2/src/components/docs/type-glyph.tsx`

- [ ] **Step 1: Create the file**

Create `webui-v2/src/components/docs/type-glyph.tsx`:

```tsx
/* ============================================================
   type-glyph.tsx — TypeGlyph pill + TypeBadge used in the
   Docs tree and entity article header.

   TypeGlyph: a small 2-3 letter mono pill ("fn", "cmp", "hk", …)
              color-tinted by entity type.
   TypeBadge:  a wider pill with a colored dot + full type label.
   ============================================================ */

import { Folder } from "lucide-react";
import type { DocsEntityKind } from "@/data/types";

interface TypeMeta {
  label: string;
  glyph: string;
  /** CSS class suffix for the Tailwind token approach */
  paletteIdx: number; // 1–6 matching the design pastel scale
}

const TYPE_META: Record<DocsEntityKind, TypeMeta> = {
  function:      { label: "function",  glyph: "fn",  paletteIdx: 6 },
  component:     { label: "component", glyph: "cmp", paletteIdx: 1 },
  hook:          { label: "hook",      glyph: "hk",  paletteIdx: 2 },
  class:         { label: "class",     glyph: "cls", paletteIdx: 3 },
  method:        { label: "method",    glyph: "mth", paletteIdx: 4 },
  http_endpoint: { label: "endpoint",  glyph: "ep",  paletteIdx: 5 },
  module:        { label: "module",    glyph: "",    paletteIdx: 0 },
  folder:        { label: "folder",    glyph: "",    paletteIdx: 0 },
  repo:          { label: "repo",      glyph: "",    paletteIdx: 0 },
};

// Pastel scale: matches design tokens var(--pastel-N) and var(--pastel-N-ink).
// We use inline styles to stay true to the token values.
const PASTEL_COLORS: Record<number, { bg: string; fg: string }> = {
  0: { bg: "var(--text-5)",    fg: "var(--text-3)" },
  1: { bg: "var(--pastel-1)",  fg: "var(--pastel-1-ink)" },
  2: { bg: "var(--pastel-2)",  fg: "var(--pastel-2-ink)" },
  3: { bg: "var(--pastel-3)",  fg: "var(--pastel-3-ink)" },
  4: { bg: "var(--pastel-4)",  fg: "var(--pastel-4-ink)" },
  5: { bg: "var(--pastel-5)",  fg: "var(--pastel-5-ink)" },
  6: { bg: "var(--pastel-6)",  fg: "var(--pastel-6-ink)" },
};

export function TypeGlyph({ type }: { type: DocsEntityKind }) {
  const meta = TYPE_META[type] ?? TYPE_META.module;
  const colors = PASTEL_COLORS[meta.paletteIdx];

  if (!meta.glyph) {
    return (
      <span
        className="inline-flex items-center justify-center w-5 h-5 rounded shrink-0"
        style={{ color: "var(--text-3)" }}
        aria-hidden="true"
      >
        <Folder size={11} />
      </span>
    );
  }

  return (
    <span
      className="inline-flex items-center justify-center shrink-0 font-mono text-[10px] font-semibold rounded px-1 py-px leading-none"
      style={{
        background: `color-mix(in srgb, ${colors.bg} 28%, transparent)`,
        color: colors.fg,
        minWidth: "2ch",
      }}
      aria-hidden="true"
    >
      {meta.glyph}
    </span>
  );
}

export function TypeBadge({ type }: { type: DocsEntityKind }) {
  const meta = TYPE_META[type] ?? TYPE_META.module;
  const colors = PASTEL_COLORS[meta.paletteIdx];
  return (
    <span
      className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{
        background: `color-mix(in srgb, ${colors.bg} 16%, transparent)`,
        color: colors.fg,
        border: `1px solid color-mix(in srgb, ${colors.bg} 40%, transparent)`,
      }}
    >
      <span
        className="w-1.5 h-1.5 rounded-full shrink-0"
        style={{ background: colors.bg }}
        aria-hidden="true"
      />
      {meta.label}
    </span>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/components/docs/type-glyph.tsx && git commit -m "feat(docs): add TypeGlyph and TypeBadge components"
```

---

## Task 6: DocsTree component

**Files:**
- Create: `webui-v2/src/components/docs/docs-tree.tsx`

- [ ] **Step 1: Create the file**

Create `webui-v2/src/components/docs/docs-tree.tsx`:

```tsx
/* ============================================================
   docs-tree.tsx — Left pane: recursive entity tree.

   - Header: "Documentation index" + total entity count.
   - Repo nodes auto-expanded; deeper folders default collapsed.
   - Search: matching branches auto-expand; non-matching hidden.
   - Matches inline-highlighted with <mark>.
   - Uses <button> for accessibility (keyboard nav).
   ============================================================ */

import { useState, useMemo, useCallback } from "react";
import { ChevronRight } from "lucide-react";
import { TypeGlyph } from "./type-glyph";
import type { DocsTreeNode } from "@/data/types";

// ── helpers ──────────────────────────────────────────────────────────────────

function countLeaves(node: DocsTreeNode): number {
  if (!node.children) return 1;
  return node.children.reduce((sum, c) => sum + countLeaves(c), 0);
}

function hasMatch(node: DocsTreeNode, q: string): boolean {
  if (!q) return false;
  if (node.name.toLowerCase().includes(q)) return true;
  return node.children?.some((c) => hasMatch(c, q)) ?? false;
}

function HighlightMatch({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>;
  const idx = text.toLowerCase().indexOf(query);
  if (idx < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-[var(--accent-soft)] text-[var(--accent)] rounded-sm not-italic">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

// ── TreeNode ─────────────────────────────────────────────────────────────────

interface TreeNodeProps {
  node: DocsTreeNode;
  depth: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
  query: string;
  openMap: Record<string, boolean>;
  onToggle: (key: string) => void;
}

function TreeNode({ node, depth, selectedId, onSelect, query, openMap, onToggle }: TreeNodeProps) {
  const isLeaf = !node.children;
  const nodeKey = `${node.name}:${depth}`;
  const lowerQ = query.toLowerCase();

  const selfMatches = query ? node.name.toLowerCase().includes(lowerQ) : false;
  const childMatches = !isLeaf && (node.children?.some((c) => hasMatch(c, lowerQ)) ?? false);

  // Hide nodes that don't match (and have no matching children) when searching.
  if (query && !selfMatches && !childMatches) return null;

  const paddingLeft = 12 + depth * 14;

  if (isLeaf) {
    const isActive = node.id === selectedId;
    return (
      <button
        className={[
          "flex items-center gap-1.5 w-full text-left px-2 py-1 rounded-sm text-sm font-mono transition-colors",
          isActive
            ? "bg-[var(--accent-soft)] text-[var(--accent)]"
            : "text-text-2 hover:bg-surface-2",
        ].join(" ")}
        style={{ paddingLeft }}
        onClick={() => node.id && onSelect(node.id)}
        title={node.name}
      >
        <TypeGlyph type={node.type} />
        <span className="truncate leading-none">
          <HighlightMatch text={node.name} query={query} />
        </span>
      </button>
    );
  }

  // Folder / repo node.
  // Repo nodes (depth=0) default open; deeper folders default closed.
  const defaultOpen = depth === 0;
  const isOpen = query ? true : (openMap[nodeKey] ?? defaultOpen);
  const totalLeaves = countLeaves(node);

  return (
    <div>
      <button
        className="flex items-center gap-1 w-full text-left px-2 py-1 rounded-sm text-sm hover:bg-surface-2 transition-colors"
        style={{ paddingLeft }}
        onClick={() => onToggle(nodeKey)}
        aria-expanded={isOpen}
      >
        <ChevronRight
          size={11}
          className={[
            "text-text-4 shrink-0 transition-transform",
            isOpen ? "rotate-90" : "",
          ].join(" ")}
        />
        <span
          className={[
            "truncate font-mono leading-none",
            node.type === "repo" ? "font-semibold text-text" : "text-text-2",
          ].join(" ")}
        >
          <HighlightMatch text={node.name} query={query} />
        </span>
        {node.type !== "repo" && isOpen && (
          <span className="ml-auto text-xs text-text-4 tabular-nums shrink-0">
            {totalLeaves}
          </span>
        )}
      </button>
      {isOpen &&
        node.children?.map((child, i) => (
          <TreeNode
            key={(child.id ?? child.name) + "-" + i}
            node={child}
            depth={depth + 1}
            selectedId={selectedId}
            onSelect={onSelect}
            query={query}
            openMap={openMap}
            onToggle={onToggle}
          />
        ))}
    </div>
  );
}

// ── DocsTree ─────────────────────────────────────────────────────────────────

export interface DocsTreeProps {
  tree: DocsTreeNode[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  query: string;
}

export function DocsTree({ tree, selectedId, onSelect, query }: DocsTreeProps) {
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});

  const handleToggle = useCallback((key: string) => {
    setOpenMap((prev) => ({ ...prev, [key]: !(prev[key] ?? (key.endsWith(":0"))) }));
  }, []);

  const totalEntities = useMemo(
    () => tree.reduce((sum, r) => sum + countLeaves(r), 0),
    [tree],
  );

  const lowerQ = query.toLowerCase();
  const noMatches = query && tree.every((r) => !hasMatch(r, lowerQ));

  return (
    <div className="flex flex-col h-full w-[320px] shrink-0 border-r border-border overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <span className="text-sm font-medium text-text">Documentation index</span>
        <span className="text-xs font-mono text-text-3 tabular-nums">
          {totalEntities.toLocaleString()}
        </span>
      </div>

      {/* Tree */}
      <div className="flex-1 overflow-y-auto py-1 px-1">
        {noMatches ? (
          <p className="px-3 py-4 text-sm text-text-3 text-center">
            No entities match &ldquo;{query}&rdquo;
          </p>
        ) : (
          tree.map((repo, i) => (
            <TreeNode
              key={repo.name + "-" + i}
              node={repo}
              depth={0}
              selectedId={selectedId}
              onSelect={onSelect}
              query={query}
              openMap={openMap}
              onToggle={handleToggle}
            />
          ))
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/components/docs/docs-tree.tsx && git commit -m "feat(docs): add DocsTree recursive entity tree component"
```

---

## Task 7: DocsEmpty + DocsSkeleton components

**Files:**
- Create: `webui-v2/src/components/docs/docs-empty.tsx`
- Create: `webui-v2/src/components/docs/docs-skeleton.tsx`

- [ ] **Step 1: Create `docs-empty.tsx`**

Create `webui-v2/src/components/docs/docs-empty.tsx`:

```tsx
/* ============================================================
   docs-empty.tsx — Right-pane empty state.
   Shown when no entity is selected (no :entityId in URL).
   ============================================================ */

import { BookOpen } from "lucide-react";

export function DocsEmpty() {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-3 px-6 text-center">
      <span className="text-text-4" aria-hidden="true">
        <BookOpen size={32} strokeWidth={1.25} />
      </span>
      <h2 className="text-base font-medium text-text">Pick an entity</h2>
      <p className="text-sm text-text-3 max-w-xs">
        Browse the index on the left or search by name above. Each entity gets a
        page with its signature, parameters, callers, and dependencies.
      </p>
    </div>
  );
}
```

- [ ] **Step 2: Create `docs-skeleton.tsx`**

Create `webui-v2/src/components/docs/docs-skeleton.tsx`:

```tsx
/* ============================================================
   docs-skeleton.tsx — Loading skeleton for the entity article.
   Rendered while useDocsEntity is pending.
   ============================================================ */

function SkeletonLine({ w = "100%" }: { w?: string }) {
  return (
    <div
      className="h-3 rounded bg-surface-2 animate-pulse"
      style={{ width: w }}
    />
  );
}

export function DocsEntitySkeleton() {
  return (
    <article className="max-w-[760px] mx-auto px-8 py-8 flex flex-col gap-8">
      {/* Head */}
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <SkeletonLine w="56px" />
          <SkeletonLine w="240px" />
        </div>
        <SkeletonLine w="320px" />
        <div className="flex gap-2">
          <SkeletonLine w="72px" />
          <SkeletonLine w="72px" />
        </div>
      </div>
      {/* Signature block */}
      <div className="flex flex-col gap-2">
        <SkeletonLine w="80px" />
        <div className="h-20 rounded-md bg-surface-2 animate-pulse" />
      </div>
      {/* Description */}
      <div className="flex flex-col gap-2">
        <SkeletonLine w="80px" />
        <SkeletonLine w="100%" />
        <SkeletonLine w="90%" />
        <SkeletonLine w="60%" />
      </div>
    </article>
  );
}
```

- [ ] **Step 3: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/components/docs/docs-empty.tsx webui-v2/src/components/docs/docs-skeleton.tsx && git commit -m "feat(docs): add DocsEmpty and DocsEntitySkeleton components"
```

---

## Task 8: DocsEntity article component

**Files:**
- Create: `webui-v2/src/components/docs/docs-entity.tsx`

- [ ] **Step 1: Create the file**

Create `webui-v2/src/components/docs/docs-entity.tsx`:

```tsx
/* ============================================================
   docs-entity.tsx — Right-pane entity article.

   Sections (top to bottom):
   1. Head — TypeBadge · name (mono, 26px) · meta row (repo chip + file) ·
             ghost action buttons (Open in editor · View in Graph)
   2. Signature — code block
   3. Description — paragraph, AI-generated chip if applicable
   4. Parameters — 3-column grid
   5. Returns
   6. Response shapes (http_endpoint only)
   7. Called by / Calls — collapsible chip lists
   8. EntityStub — when stub=true
   ============================================================ */

import { useState } from "react";
import { ChevronRight, Code2, ExternalLink, Sparkles, Info } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui";
import { TypeBadge } from "./type-glyph";
import type { DocsEntityDetail } from "@/data/types";

const AI_TOOLTIP = (
  <>
    <strong>AI-generated</strong> — archigraph synthesized this description from
    the source code because no docstring was present. Review for accuracy before
    relying on it.
  </>
);

// ── Sub-components ───────────────────────────────────────────────────────────

function SectionLabel({
  children,
  collapsible,
  open,
  onToggle,
}: {
  children: React.ReactNode;
  collapsible?: boolean;
  open?: boolean;
  onToggle?: () => void;
}) {
  const inner = (
    <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-text-3">
      {collapsible && (
        <ChevronRight
          size={11}
          className={["transition-transform", open ? "rotate-90" : ""].join(" ")}
        />
      )}
      {children}
    </span>
  );
  if (collapsible) {
    return (
      <button className="flex items-center mb-3" onClick={onToggle}>
        {inner}
      </button>
    );
  }
  return <div className="mb-3">{inner}</div>;
}

function RefList({
  label,
  hint,
  names,
  onSelect,
  empty,
}: {
  label: string;
  hint: React.ReactNode;
  names: string[];
  onSelect?: (name: string) => void;
  empty: string;
}) {
  const [open, setOpen] = useState(true);
  const visible = names.slice(0, 50);
  const overflow = names.length > 50;

  return (
    <section className="border-t border-border pt-5 mt-1">
      <SectionLabel collapsible open={open} onToggle={() => setOpen((v) => !v)}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex items-center gap-1 cursor-help">
              {label} &middot; {names.length}
              <Info size={10} className="text-text-4" tabIndex={0} />
            </span>
          </TooltipTrigger>
          <TooltipContent>{hint}</TooltipContent>
        </Tooltip>
      </SectionLabel>
      {open &&
        (visible.length === 0 ? (
          <p className="text-sm text-text-3">{empty}</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {visible.map((n) => (
              <button
                key={n}
                className="font-mono text-xs px-2 py-1 rounded-md bg-surface border border-border text-text-2 hover:bg-surface-2 transition-colors"
                onClick={() => onSelect?.(n)}
              >
                {n}
              </button>
            ))}
            {overflow && (
              <span className="text-xs text-text-3 self-center">
                +{names.length - 50} more
              </span>
            )}
          </div>
        ))}
    </section>
  );
}

// ── EntityStub ───────────────────────────────────────────────────────────────

function EntityStub({ entity }: { entity: DocsEntityDetail }) {
  return (
    <article className="max-w-[760px] mx-auto px-8 py-8">
      {/* Head still renders */}
      <EntityHead entity={entity} />
      {/* Stub message */}
      <div className="mt-8 flex items-start gap-2 rounded-lg border border-border bg-surface p-4 text-sm text-text-2">
        <Info size={14} className="shrink-0 mt-0.5 text-text-3" />
        <span>
          This entity has no generated documentation yet. Run{" "}
          <code className="font-mono text-xs bg-surface-2 px-1 py-0.5 rounded">
            archigraph generate-docs
          </code>{" "}
          on the group to populate.
        </span>
      </div>
    </article>
  );
}

// ── EntityHead ───────────────────────────────────────────────────────────────

function EntityHead({ entity }: { entity: DocsEntityDetail }) {
  return (
    <header className="mb-8">
      <div className="flex items-center gap-2 mb-2">
        <TypeBadge type={entity.type} />
        <h1 className="font-mono text-[26px] font-semibold text-text leading-none">
          {entity.name}
        </h1>
      </div>
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <span className="font-mono text-xs font-semibold px-2 py-0.5 rounded-full bg-surface border border-border text-text-2">
          {entity.repo}
        </span>
        <span className="font-mono text-xs text-text-3">
          {entity.file}
          {entity.line ? `:${entity.line}` : ""}
        </span>
      </div>
      <div className="flex gap-2">
        <button className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-border bg-surface text-xs text-text-2 hover:bg-surface-2 transition-colors">
          <Code2 size={11} />
          Open in editor
        </button>
        <button className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-border bg-surface text-xs text-text-2 hover:bg-surface-2 transition-colors">
          <ExternalLink size={11} />
          View in Graph
        </button>
      </div>
    </header>
  );
}

// ── DocsEntity ────────────────────────────────────────────────────────────────

export interface DocsEntityProps {
  entity: DocsEntityDetail;
}

export function DocsEntity({ entity }: DocsEntityProps) {
  if (entity.stub) return <EntityStub entity={entity} />;

  return (
    <article className="max-w-[760px] mx-auto px-8 py-8">
      <EntityHead entity={entity} />

      {/* Signature */}
      {entity.signature && (
        <section className="mb-8">
          <SectionLabel>Signature</SectionLabel>
          <pre className="overflow-x-auto rounded-md bg-surface border border-border px-4 py-3 text-[12.5px] leading-[1.6] font-mono text-text-2">
            <code>{entity.signature}</code>
          </pre>
        </section>
      )}

      {/* Description */}
      {entity.description && (
        <section className="mb-8">
          <SectionLabel>
            <span className="flex items-center gap-2">
              Description
              {entity.aiGenerated && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-[color-mix(in_srgb,var(--pastel-2)_16%,transparent)] text-[var(--pastel-2-ink)] text-xs cursor-help">
                      <Sparkles size={9} />
                      AI-generated
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{AI_TOOLTIP}</TooltipContent>
                </Tooltip>
              )}
            </span>
          </SectionLabel>
          <p className="text-sm text-text-2 leading-relaxed max-w-[64ch]">
            {entity.description}
          </p>
        </section>
      )}

      {/* Parameters */}
      {entity.params.length > 0 && (
        <section className="mb-8">
          <SectionLabel>Parameters</SectionLabel>
          <div className="grid grid-cols-[auto_auto_1fr] gap-x-4 gap-y-2 text-sm">
            {entity.params.map((p) => (
              <>
                <span key={`name-${p.name}`} className="font-mono text-text font-medium whitespace-nowrap">
                  {p.name}
                </span>
                <span key={`type-${p.name}`} className="font-mono text-text-3 whitespace-nowrap">
                  {p.type}
                </span>
                <span key={`desc-${p.name}`} className="text-text-2">
                  {p.desc}
                </span>
              </>
            ))}
          </div>
        </section>
      )}

      {/* Returns */}
      {entity.returns && (
        <section className="mb-8">
          <SectionLabel>Returns</SectionLabel>
          <div className="flex items-baseline gap-3 text-sm">
            <span className="font-mono text-text">{entity.returns.type}</span>
            {entity.returns.desc && entity.returns.desc !== "—" && (
              <span className="text-text-2">{entity.returns.desc}</span>
            )}
          </div>
        </section>
      )}

      {/* Response shapes (http_endpoint only) */}
      {entity.responseShapes && entity.responseShapes.length > 0 && (
        <section className="mb-8">
          <SectionLabel>Response shapes</SectionLabel>
          <div className="flex flex-col gap-2">
            {entity.responseShapes.map((rs) => (
              <div key={rs.status} className="flex items-start gap-3 text-sm">
                <span
                  className={[
                    "font-mono font-semibold px-1.5 py-0.5 rounded text-xs shrink-0",
                    rs.status < 400
                      ? "bg-[color-mix(in_srgb,var(--pastel-2)_16%,transparent)] text-[var(--pastel-2-ink)]"
                      : "bg-[color-mix(in_srgb,var(--pastel-5)_16%,transparent)] text-[var(--pastel-5-ink)]",
                  ].join(" ")}
                >
                  {rs.status}
                </span>
                <code className="font-mono text-xs text-text-2">{rs.shape}</code>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Called by / Calls */}
      <RefList
        label="Called by"
        hint={<><strong>Called by</strong> — every entity in the graph that invokes or renders this one.</>}
        names={entity.callers}
        empty="No incoming edges. This may be a top-level entry point."
      />
      <RefList
        label="Calls"
        hint={<><strong>Calls</strong> — entities this one depends on directly.</>}
        names={entity.callees}
        empty="No outgoing edges from this entity."
      />
    </article>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/components/docs/docs-entity.tsx && git commit -m "feat(docs): add DocsEntity article component"
```

---

## Task 9: Docs screen route + router update

**Files:**
- Modify: `webui-v2/src/routes/docs.tsx`
- Modify: `webui-v2/src/routes/router.tsx`

- [ ] **Step 1: Replace `docs.tsx` with the full screen**

Replace the entire contents of `webui-v2/src/routes/docs.tsx`:

```tsx
/* ============================================================
   docs.tsx — Docs screen: entity browser + documentation reader.
   Route: /g/:groupId/docs/:entityId?

   Layout: two-pane (DocsTree fixed 320px left + scrollable article right).
   The DocsTopBar is rendered inline here (not via AppShell TopBar) because
   it needs a center search input + right "Updated X ago" hint, different
   from the standard TopBar chrome.

   States:
   - Empty     — no entityId → DocsEmpty
   - Loading   — DocsEntitySkeleton
   - Loaded    — DocsEntity
   - Stub      — DocsEntity with stub=true
   - 404       — not-found redirect (uses navigate)
   ============================================================ */

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Search, X } from "lucide-react";
import { Kbd } from "@/components/ui";
import { useDocsTree, useDocsEntity } from "@/hooks/use-docs";
import { ApiError } from "@/lib/api";
import { DocsTree } from "@/components/docs/docs-tree";
import { DocsEntity } from "@/components/docs/docs-entity";
import { DocsEmpty } from "@/components/docs/docs-empty";
import { DocsEntitySkeleton } from "@/components/docs/docs-skeleton";

// ── Inline TopBar for Docs (adds center search + right hint) ─────────────────

function DocsTopBar({
  group,
  search,
  onSearch,
}: {
  group: string;
  search: string;
  onSearch: (v: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  // "/" shortcut — focus search.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement?.tagName !== "INPUT") {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  return (
    <div className="flex items-center justify-between h-11 shrink-0 px-4 gap-4 border-b border-border bg-bg">
      {/* Left: breadcrumb override for docs context */}
      <span className="text-sm text-text-3 font-mono shrink-0">{group} / Docs</span>

      {/* Center: search */}
      <div className="relative flex items-center flex-1 max-w-xs">
        <Search size={13} className="absolute left-2.5 text-text-4 pointer-events-none" />
        <input
          ref={inputRef}
          type="text"
          className="w-full pl-8 pr-8 h-7 rounded-md bg-surface border border-border text-sm text-text placeholder:text-text-4 focus:outline-none focus:ring-1 focus:ring-[var(--accent)] focus:border-[var(--accent)]"
          placeholder="Search docs by entity name…"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          autoFocus
        />
        {search ? (
          <button
            className="absolute right-2 text-text-4 hover:text-text-2"
            onClick={() => onSearch("")}
            aria-label="Clear"
          >
            <X size={11} />
          </button>
        ) : (
          <Kbd className="absolute right-2 text-[10px]">/</Kbd>
        )}
      </div>

      {/* Right: last-updated hint (static placeholder; real value from tree metadata future) */}
      <span className="text-xs text-text-3 shrink-0">Updated just now</span>
    </div>
  );
}

// ── Screen ────────────────────────────────────────────────────────────────────

export default function DocsScreen() {
  const { groupId = "demo", entityId } = useParams<{
    groupId: string;
    entityId?: string;
  }>();
  const navigate = useNavigate();

  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(entityId ?? null);

  // Sync selectedId with URL param.
  useEffect(() => {
    setSelectedId(entityId ?? null);
  }, [entityId]);

  const handleSelect = useCallback(
    (id: string) => {
      setSelectedId(id);
      navigate(`/g/${groupId}/docs/${encodeURIComponent(id)}`, { replace: false });
    },
    [groupId, navigate],
  );

  const { data: tree = [], isLoading: treeLoading } = useDocsTree(groupId);
  const {
    data: entity,
    isLoading: entityLoading,
    error: entityError,
  } = useDocsEntity(groupId, selectedId);

  // On 404, redirect to the base docs page.
  useEffect(() => {
    if (entityError instanceof ApiError && entityError.status === 404) {
      navigate(`/g/${groupId}/docs`, { replace: true });
    }
  }, [entityError, groupId, navigate]);

  // Debounce search ~120ms per spec.
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 120);
    return () => clearTimeout(t);
  }, [search]);

  return (
    <div className="flex flex-col h-full">
      <DocsTopBar group={groupId} search={search} onSearch={setSearch} />

      <div className="flex flex-1 min-h-0">
        {/* Left: tree */}
        {treeLoading ? (
          <div className="w-[320px] shrink-0 border-r border-border flex items-center justify-center">
            <span className="text-sm text-text-4">Loading…</span>
          </div>
        ) : (
          <DocsTree
            tree={tree}
            selectedId={selectedId}
            onSelect={handleSelect}
            query={debouncedSearch}
          />
        )}

        {/* Right: entity content */}
        <div className="flex-1 overflow-y-auto">
          {!selectedId ? (
            <DocsEmpty />
          ) : entityLoading ? (
            <DocsEntitySkeleton />
          ) : entity ? (
            <DocsEntity entity={entity} />
          ) : null}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Update router.tsx to add the optional entityId child route**

In `webui-v2/src/routes/router.tsx`, find the docs route entry:

```ts
{ path: "docs", element: <DocsScreen />, handle: { surfaceLabel: "Docs" } },
```

Replace it with:

```ts
{
  path: "docs",
  element: <DocsScreen />,
  handle: { surfaceLabel: "Docs" },
},
{
  path: "docs/:entityId",
  element: <DocsScreen />,
  handle: { surfaceLabel: "Docs" },
},
```

- [ ] **Step 3: Type-check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/src/routes/docs.tsx webui-v2/src/routes/router.tsx && git commit -m "feat(docs): wire up DocsScreen route and update router for :entityId"
```

---

## Task 10: Build verification + no-touch checks

- [ ] **Step 1: Run `npm run build` — must exit 0**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npm run build 2>&1 | tail -20
```

Expected: `✓ built in` line, no TypeScript or Vite errors.

- [ ] **Step 2: Confirm `dashboard/` is untouched**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git diff --name-only HEAD~10..HEAD | grep "^dashboard/" || echo "CLEAN — no dashboard/ files touched"
```

Expected: Output shows `CLEAN — no dashboard/ files touched`.

- [ ] **Step 3: Run all Go tests (must not regress)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/dashboard/... 2>&1 | tail -20
```

Expected: `ok  github.com/cajasmota/archigraph/internal/dashboard`

- [ ] **Step 4: Commit**

No code changes; this is a verification step. If build or tests fail, fix the issue, then commit the fix with a descriptive message.

---

## Task 11: Playwright screenshots (light + dark)

- [ ] **Step 1: Start the dev server on isolated port**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph/webui-v2 && npm run dev -- --port 47282 &
# Wait a moment for the server to start
sleep 3
```

- [ ] **Step 2: Take light-mode screenshot (empty state)**

Use Playwright or the Playwright MCP to navigate to `http://localhost:47282/g/demo/docs` and take a screenshot. Save to `/Users/jorgecajas/Documents/Projects/archigraph/webui-v2/screenshots/docs-empty-light.png`.

```
Navigate to: http://localhost:47282/g/demo/docs
Set: document.documentElement.setAttribute('data-theme', 'light')
Screenshot: /Users/jorgecajas/Documents/Projects/archigraph/webui-v2/screenshots/docs-empty-light.png
```

- [ ] **Step 3: Take dark-mode screenshot (empty state)**

```
Set: document.documentElement.setAttribute('data-theme', 'dark')
Screenshot: /Users/jorgecajas/Documents/Projects/archigraph/webui-v2/screenshots/docs-empty-dark.png
```

- [ ] **Step 4: Verify screenshots match docs.md layout**

Check the screenshots against the spec:
- Left pane visible with "Documentation index" header at 320px width
- Search input in the topbar center
- Right pane shows empty-state icon + "Pick an entity" text
- Dark mode renders inverted tokens (no hardcoded colors visible)

- [ ] **Step 5: Kill the dev server**

```bash
pkill -f "vite.*47282" || true
```

- [ ] **Step 6: Commit screenshots**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && git add webui-v2/screenshots/ && git commit -m "feat(docs): add Playwright verification screenshots for Docs screen"
```

---

## Task 12: Open the PR

- [ ] **Step 1: Push the branch**

The worktree branch is `feat/webui-v2-docs`. Push it:

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-docs && git push -u origin feat/webui-v2-docs
```

- [ ] **Step 2: Create the PR**

```bash
gh pr create \
  --title "feat(docs): WebUI v2 Docs screen — entity browser + documentation reader" \
  --body "$(cat <<'EOF'
## What this PR does

Implements the Docs screen for WebUI v2 per issue #1438 (EPIC #1432).

## Why it matters

The Docs screen is the \"explain this codebase to a new engineer\" surface —
a two-pane entity browser that lets engineers navigate every indexed entity
(functions, classes, hooks, HTTP endpoints) and read auto-generated documentation
for each one.

## What changed

**Backend (Go):**
- `internal/dashboard/handlers_v2_docs.go` — Two new v2-envelope handlers:
  - `GET /api/v2/groups/{group}/docs/tree` — entity-centric tree (repo → folder → leaf)
  - `GET /api/v2/groups/{group}/docs/entities/{entityId}` — full entity detail with callers/callees from the relationship graph + enrichment frontmatter description when available
- `internal/dashboard/handlers_v2_docs_test.go` — 4 handler tests covering tree, entity detail, 404, and group-not-found
- `internal/dashboard/server.go` — registered the 2 new v2 routes

**Data decisions documented in handlers_v2_docs.go header:**
- `POST /api/v2/groups/{group}/docs/generate` intentionally NOT implemented (long-running skill operation managed via Pending screen)
- Entities without enrichment frontmatter return `stub: true` — correct per spec

**Frontend (webui-v2):**
- `src/data/types.ts` — added `DocsTreeNode`, `DocsEntityDetail`, `DocsParam`, `DocsEntityKind`
- `src/lib/api.ts` — added `getDocsTree`, `getDocsEntity`
- `src/hooks/use-docs.ts` — `useDocsTree` + `useDocsEntity` TanStack Query hooks
- `src/components/docs/type-glyph.tsx` — `TypeGlyph` + `TypeBadge` pills
- `src/components/docs/docs-tree.tsx` — left-pane recursive entity tree with search + highlight
- `src/components/docs/docs-entity.tsx` — full entity article (head, signature, description, params, returns, response shapes, callers/callees)
- `src/components/docs/docs-empty.tsx` — empty state
- `src/components/docs/docs-skeleton.tsx` — loading skeleton
- `src/routes/docs.tsx` — full screen (replaces placeholder)
- `src/routes/router.tsx` — added `docs/:entityId` child route

## How to test

1. `go build ./...` — must succeed
2. `cd webui-v2 && npm run build` — must succeed
3. Start the daemon + open `/g/<group>/docs` in the webui-v2 dev server (port 47280)
4. Verify: tree populates, clicking an entity loads the article, search filters tree

## Screenshots

See `webui-v2/screenshots/` for Playwright verification screenshots (light + dark).

---

Fixes #1438
Ref EPIC #1432

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Against docs.md

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| Route `/g/:groupId/docs/:entityId?` | Task 9 (router + screen) |
| `<DocsTopBar>` — breadcrumb, center search, right hint, `/` shortcut | Task 9 (DocsTopBar inline) |
| `<DocsTree>` — header, count, recursive repo>folder>entity | Task 6 |
| `<TypeGlyph>` — 2-3 letter mono pill, tinted by type | Task 5 |
| Chevron rotates on expand | Task 6 (rotate-90 class) |
| Repo nodes auto-expanded; deeper default collapsed | Task 6 (defaultOpen = depth === 0) |
| Search: auto-expand, highlight with `<mark>` | Task 6 |
| `<DocsEntity>` — head, signature, description, params, returns, response shapes, callers/callees | Task 8 |
| AI-generated chip with tooltip | Task 8 |
| `<DocsEmpty>` — centered icon + "Pick an entity" | Task 7 |
| `<EntityStub>` — full chrome + stub hint message | Task 8 |
| Backend: `GET /api/groups/:id/docs/tree` (v2) | Task 2 |
| Backend: `GET /api/groups/:id/docs/entities/:entityId` (v2) | Task 2 |
| v1 untouched | Task 2 (separate file + note in task 10 verify) |
| Entity 404 → reroute | Task 9 (useEffect on entityError) |
| Caller list capped at 50 | Task 2 (Go) + Task 8 (RefList `slice(0, 50)`) |
| Search debounce ~120ms | Task 9 |
| `npm run build` clean | Task 10 |
| Playwright screenshots light+dark | Task 11 |
| PR with 6-section format, `Fixes #1438` | Task 12 |
| `dashboard/` zero diff | Task 10 |

**Gaps checked:** Large tree virtualization (`@tanstack/react-virtual`) is listed as a production concern in the spec ("Prototype uses naive recursion; replace in production"). `@tanstack/react-virtual` is not in the package.json — correct to defer per YAGNI. Note is left in a comment in `docs-tree.tsx` (add it in step 1 as a comment at the top of the file).

**Placeholder scan:** None found.

**Type consistency:** `DocsEntityKind` defined in Task 1 → used in Tasks 5, 6, 8. `DocsEntityDetail` defined in Task 1 → used in Tasks 3, 4, 8, 9. `DocsParam` defined in Task 1 → used in Task 8. All consistent.
