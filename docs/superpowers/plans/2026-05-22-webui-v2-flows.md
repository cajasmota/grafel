# WebUI v2 Flows Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Flows (process-flow explorer) screen in webui-v2, replacing the PlaceholderScreen with a full layered-DAG implementation per the design doc at `docs/screens/flows.md`.

**Architecture:** Static data via a new `useFlows` / `useFlowDetail` / `useFlowDeadEnds` hook family calling the existing v1 `/api/flows/{group}` endpoints (wrapped in a thin v2 envelope via a new `v2_flows.go`). The React component is `webui-v2/src/routes/flows.tsx` using Lego primitives from `@/components/ui`; the DAG is a pure-CSS/SVG absolute-positioned single-column layout — no physics, no dragging.

**Tech Stack:** React 18 + TypeScript 5.7, Vite 6, Tailwind v4, TanStack Query, lucide-react, Zustand, React Router 6 (existing webui-v2 stack); Go 1.22 net/http for backend handler.

---

## Task 1: Extend data/types.ts with Flows domain types

**Files:**
- Modify: `webui-v2/src/data/types.ts` (append at bottom)

- [ ] **Step 1: Add flow type definitions**

Append to `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2/src/data/types.ts`:

```ts
// ─── Flows (Process Flow Explorer) ────────────────────────────────────────────

export type EntryKind =
  | "http_handler"
  | "message_consumer"
  | "kafka_consumer"
  | "scheduled_task"
  | "component_render"
  | "test"
  | "cli_command"
  | "ws_handler"
  | "function";

export type StepKind =
  | "http_fetch"
  | "db_query"
  | "db_write"
  | "message_publish"
  | "message_consume"
  | "transform"
  | "validation"
  | "side_effect"
  | "external_lib"
  | "test_assert"
  | "component_render"
  | "render"
  | "function_call"
  | "unknown";

export type FlowRelationshipKind =
  | "CALLS"
  | "FETCHES"
  | "QUERIES"
  | "PUBLISHES_TO"
  | "SUBSCRIBES_TO"
  | "RENDERS"
  | "REFERENCES";

export interface FlowEnrichment {
  ai_summary?: string;
  preconditions?: string[];
  expected_outcome?: string;
  writes_db_table?: string[];
  publishes_to?: string[];
  external_calls?: string[];
  read_sources?: string[];
  write_sinks?: string[];
  linked_endpoint_id?: string;
  linked_topic_id?: string;
  gaps?: string[];
  rank?: number;
}

export interface ProcessStep {
  entity_id: string;
  name: string;
  kind: string;
  step_index: number;
  source_file: string;
  start_line?: number;
  repo: string;
  edge_kind: FlowRelationshipKind | null;
  step_kind?: StepKind;
  side_effects?: string[];
}

export interface Process {
  process_id: string;
  label: string;
  repo: string;
  entry_id: string;
  entry_name: string;
  entry_kind: EntryKind;
  entry_module?: string;
  terminal_id: string;
  step_count: number;
  cross_stack: boolean;
  is_cross_repo?: boolean;
  crosses_external_lib?: boolean;
  terminal_is_phantom?: boolean;
  chain_labels: string[];
  source_file?: string;
  priority_hint?: "high" | "medium" | "low";
  dominant_step_kind?: FlowRelationshipKind;
  complexity_score?: number;
  steps?: ProcessStep[];
  flow_side_effects?: string[];
  enrichment?: FlowEnrichment;
  docgen_status?: "enriched" | "pending" | "stale";
  source_snippets?: Record<string, string>;
}

export interface FlowDeadEnd {
  process_id: string;
  process_name: string;
  repo: string;
  reason: "no_useful_sink" | "single_step" | "unresolved_callee" | "phantom_terminal" | "dead_end";
  step_count: number;
  dead_end_step_id?: string;
  dead_end_step_name?: string;
  cross_stack?: boolean;
}

export interface EntryKindGroup {
  kind: EntryKind;
  count: number;
}

export interface FlowsListResponse {
  processes: Process[];
  count: number;
  entry_kind_groups: EntryKindGroup[];
}

export interface FlowDetailResponse {
  process: Process;
  chain_entities: ProcessStep[];
  source_snippets: Record<string, string>;
}

export interface FlowDeadEndsResponse {
  dead_ends: FlowDeadEnd[];
}
```

- [ ] **Step 2: Verify types compile**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors (or only pre-existing errors unrelated to new types).

---

## Task 2: Add flows API methods to lib/api.ts

**Files:**
- Modify: `webui-v2/src/lib/api.ts` (append in the api object)

- [ ] **Step 1: Add flows API calls (appended to api object)**

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2/src/lib/api.ts`, add inside the `api = { ... }` object before the closing brace:

```ts
  // --- Flows (Process Flow Explorer) ---
  listFlows: (groupId: string, params?: { tab?: string; search?: string; limit?: number }) => {
    const q = new URLSearchParams();
    if (params?.search) q.set("search", params.search);
    if (params?.limit) q.set("limit", String(params.limit));
    if (params?.tab === "crossrepo") q.set("cross_stack_only", "false");
    const qs = q.toString() ? `?${q.toString()}` : "";
    return request<FlowsListResponse>(`/flows/${groupId}${qs}`);
  },
  getFlowDetail: (groupId: string, processId: string) =>
    request<FlowDetailResponse>(`/flows/${groupId}/${encodeURIComponent(processId)}`),
  listFlowDeadEnds: (groupId: string) =>
    request<FlowDeadEndsResponse>(`/flows/${groupId}/dead-ends`),
  listFlowTruncated: (groupId: string) =>
    request<FlowsListResponse>(`/flows/${groupId}/truncated`),
  generateFlowDocs: (groupId: string, processId: string) =>
    request<{ status: string; message: string }>(`/flows/${groupId}/${encodeURIComponent(processId)}/trigger-enrichment`, { method: "POST" }),
```

Also add the import for the new types at the top of api.ts:

```ts
import type { Group, Entity, Community, FlowsListResponse, FlowDetailResponse, FlowDeadEndsResponse } from "@/data/types";
```

- [ ] **Step 2: Verify no TypeScript errors**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

Expected: no new errors.

---

## Task 3: Create use-flows.ts hook

**Files:**
- Create: `webui-v2/src/hooks/use-flows.ts`

- [ ] **Step 1: Write the hook file**

```ts
/* ============================================================
   hooks/use-flows.ts — data hooks for the Flows screen.

   Three hooks:
     useFlows(groupId, tab, search)   — list rail
     useFlowDetail(groupId, pid)      — detail panel (enabled when pid != null)
     useFlowDeadEnds(groupId)         — dead-ends tab
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export function useFlows(
  groupId: string,
  tab: string,
  search: string,
) {
  return useQuery({
    queryKey: ["flows", groupId, tab, search],
    queryFn: () => api.listFlows(groupId, { tab, search, limit: 200 }),
    staleTime: 30_000,
    enabled: tab !== "deadends" && tab !== "truncated",
  });
}

export function useFlowDeadEnds(groupId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["flows-deadends", groupId],
    queryFn: () => api.listFlowDeadEnds(groupId),
    staleTime: 30_000,
    enabled,
  });
}

export function useFlowTruncated(groupId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["flows-truncated", groupId],
    queryFn: () => api.listFlowTruncated(groupId),
    staleTime: 30_000,
    enabled,
  });
}

export function useFlowDetail(groupId: string, processId: string | null) {
  return useQuery({
    queryKey: ["flow-detail", groupId, processId],
    queryFn: () => api.getFlowDetail(groupId, processId!),
    staleTime: 60_000,
    enabled: processId != null,
  });
}
```

- [ ] **Step 2: Verify compiles**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npx tsc --noEmit 2>&1 | head -20
```

---

## Task 4: Build the flows.tsx screen

**Files:**
- Modify: `webui-v2/src/routes/flows.tsx` (full replacement)

This is the main component. Key sub-components inline in the same file to keep it focused:
- `PathString` — highlights `{segment}` patterns amber
- `EntryKindIcon` / `StepKindIcon` — SVG icons from lucide or inline SVG
- `FlowRow` — list rail row
- `DeadEndRow` — dead-end row  
- `GroupBlock` — grouped entry-kind header + rows
- `ListRail` — left 400px panel
- `FlowDag` — SVG layered DAG
- `StepInspector` — inline step detail
- `DetailSections` — AI insights, side effects, sinks
- `DetailPanel` — right panel
- `FlowsScreen` — root

- [ ] **Step 1: Write flows.tsx**

Full file: `webui-v2/src/routes/flows.tsx` — see Task 4 implementation block below.

- [ ] **Step 2: Build verification**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npm run build 2>&1 | tail -20
```

Expected: "built in Xs" with no errors.

---

## Task 5: Backend — v2_flows.go wrapper

**Files:**
- Create: `internal/dashboard/v2_flows.go`
- Modify: `internal/dashboard/server.go` (append v2 flows routes)

- [ ] **Step 1: Write v2_flows.go**

```go
// v2_flows.go — v2 envelope wrappers for the flows surface.
//
// These handlers are thin proxies: they call the same graph-data path as the
// v1 /api/flows/* handlers but wrap the response in the v2 { ok, data } envelope.
// The frontend uses /api/flows/* (v1 path) directly via request() — the v2
// wrappers are for future migration and for having canonical v2 paths.
//
// Routes registered in server.go:
//   GET /api/v2/groups/:id/flows
//   GET /api/v2/groups/:id/flows/dead-ends
//   GET /api/v2/groups/:id/flows/truncated
//   GET /api/v2/groups/:id/flows/:processId
//   POST /api/v2/groups/:id/flows/:processId/trigger-enrichment

package dashboard

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// handleV2FlowsList — GET /api/v2/groups/{group}/flows
func (s *Server) handleV2FlowsList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	q := r.URL.Query()
	crossOnly := q.Get("cross_stack_only") == "true"
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)

	type ProcessItem struct {
		ProcessID        string                 `json:"process_id"`
		Repo             string                 `json:"repo"`
		Label            string                 `json:"label"`
		EntryID          string                 `json:"entry_id"`
		EntryName        string                 `json:"entry_name"`
		EntryKind        string                 `json:"entry_kind"`
		EntryModule      string                 `json:"entry_module,omitempty"`
		TerminalID       string                 `json:"terminal_id"`
		TerminalIsPhantom bool                  `json:"terminal_is_phantom,omitempty"`
		StepCount        int                    `json:"step_count"`
		CrossStack       bool                   `json:"cross_stack"`
		IsCrossRepo      bool                   `json:"is_cross_repo,omitempty"`
		ChainLabels      []string               `json:"chain_labels"`
		SourceFile       string                 `json:"source_file,omitempty"`
		PriorityHint     string                 `json:"priority_hint"`
		ComplexityScore  interface{}            `json:"complexity_score,omitempty"`
		DocgenStatus     string                 `json:"docgen_status"`
		Enrichment       *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	}

	var items []ProcessItem
	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			cs := e.Properties["cross_stack"] == "true"
			if crossOnly && !cs {
				continue
			}
			pid := dashPrefixedID(repo.Slug, e.ID)
			sc, _ := strconv.Atoi(e.Properties["step_count"])
			entID := e.Properties["entry_id"]
			ek := inferEntryKind(grp, entID)
			fm, summary := extractFlowDocs(group, e.ID, docgenState)
			item := ProcessItem{
				ProcessID:    pid,
				Repo:         repo.Slug,
				Label:        e.Name,
				EntryID:      entID,
				EntryName:    e.Properties["entry_name"],
				EntryKind:    ek,
				EntryModule:  entryModuleFromPath(e.SourceFile),
				TerminalID:   e.Properties["terminal_id"],
				StepCount:    sc,
				CrossStack:   cs,
				IsCrossRepo:  cs,
				ChainLabels:  splitChainLabels(e.Properties["chain_labels"]),
				SourceFile:   e.SourceFile,
				PriorityHint: priorityHint(ek),
				DocgenStatus: docgenStatus(fm, summary),
				Enrichment:   fm,
			}
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].StepCount != items[j].StepCount {
			return items[i].StepCount > items[j].StepCount
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}

	kindCounts := map[string]int{}
	for _, it := range items {
		kindCounts[it.EntryKind]++
	}
	type kindGroup struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
	}
	entryKindGroups := make([]kindGroup, 0, len(kindCounts))
	for k, v := range kindCounts {
		entryKindGroups = append(entryKindGroups, kindGroup{Kind: k, Count: v})
	}
	sort.Slice(entryKindGroups, func(i, j int) bool {
		if entryKindGroups[i].Count != entryKindGroups[j].Count {
			return entryKindGroups[i].Count > entryKindGroups[j].Count
		}
		return entryKindGroups[i].Kind < entryKindGroups[j].Kind
	})

	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"processes":         items,
		"count":             len(items),
		"entry_kind_groups": entryKindGroups,
	}))
}

// handleV2FlowDeadEnds — GET /api/v2/groups/{group}/flows/dead-ends
func (s *Server) handleV2FlowDeadEnds(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	// Delegate to v1 handler logic by forwarding the response.
	// For v2 we re-read the data and wrap in v2 envelope.
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	type DeadEndItem struct {
		ProcessID        string `json:"process_id"`
		ProcessName      string `json:"process_name"`
		Repo             string `json:"repo"`
		Reason           string `json:"reason"`
		StepCount        int    `json:"step_count"`
		DeadEndStepID    string `json:"dead_end_step_id,omitempty"`
		DeadEndStepName  string `json:"dead_end_step_name,omitempty"`
		CrossStack       bool   `json:"cross_stack,omitempty"`
	}

	var items []DeadEndItem
	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			reason := e.Properties["dead_end_reason"]
			if reason == "" {
				continue
			}
			sc, _ := strconv.Atoi(e.Properties["step_count"])
			items = append(items, DeadEndItem{
				ProcessID:       dashPrefixedID(repo.Slug, e.ID),
				ProcessName:     e.Name,
				Repo:            repo.Slug,
				Reason:          reason,
				StepCount:       sc,
				DeadEndStepID:   e.Properties["dead_end_step_id"],
				DeadEndStepName: e.Properties["dead_end_step_name"],
				CrossStack:      e.Properties["cross_stack"] == "true",
			})
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"dead_ends": items,
		"count":     len(items),
	}))
}

// handleV2FlowTruncated — GET /api/v2/groups/{group}/flows/truncated
func (s *Server) handleV2FlowTruncated(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	// Validate group exists.
	if _, err := s.graphs.GetGroup(group); err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// Truncated flows are empty in current data — return the same empty slice
	// the v1 handler returns, wrapped in v2 envelope.
	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"processes": []any{},
		"count":     0,
	}))
}
```

- [ ] **Step 2: Register v2 flow routes in server.go**

In `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/internal/dashboard/server.go`, after the existing v2 routes block (after line `mux.HandleFunc("POST /api/v2/groups", s.handleV2CreateGroup)`), add:

```go
	// --- v2 flows routes (thin v2-envelope wrappers) ---
	// NOTE: /dead-ends and /truncated must be registered before the /{processId}
	// wildcard so the more-specific patterns win.
	mux.HandleFunc("GET /api/v2/groups/{group}/flows", s.handleV2FlowsList)
	mux.HandleFunc("GET /api/v2/groups/{group}/flows/dead-ends", s.handleV2FlowDeadEnds)
	mux.HandleFunc("GET /api/v2/groups/{group}/flows/truncated", s.handleV2FlowTruncated)
```

- [ ] **Step 3: Go build verification**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 4: Commit backend**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && git add internal/dashboard/v2_flows.go internal/dashboard/server.go && git commit -m "feat(api): v2 envelope wrappers for flows endpoints"
```

---

## Task 6: Playwright screenshots

**Files:**
- Create: screenshot files at `/Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/1441-flows-light.png` and `1441-flows-dark.png`

- [ ] **Step 1: Install deps and start dev server**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npm install 2>&1 | tail -5
```

- [ ] **Step 2: Start vite on isolated port 47299 in background**

Set `VITE_PORT=47299` and start:
```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && VITE_PORT=47299 npm run dev -- --port 47299 &
```
Wait ~3 seconds for it to be ready, then take screenshots with Playwright.

- [ ] **Step 3: Take screenshots via Playwright**

Use `mcp__playwright__browser_navigate` to navigate to `http://localhost:47299/`, then screenshot.

- [ ] **Step 4: Kill dev server**

```bash
pkill -f "vite.*47299" 2>/dev/null || true
```

---

## Task 7: Final commit and PR

- [ ] **Step 1: Stage all webui-v2 changes**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && git add webui-v2/src/ && git status
```

- [ ] **Step 2: Verify dashboard/ has zero diff**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && git diff HEAD -- dashboard/
```

Expected: empty output.

- [ ] **Step 3: Build one more time**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows/webui-v2 && npm run build 2>&1 | tail -10
```

- [ ] **Step 4: Commit frontend**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && git commit -m "feat(flows): WebUI v2 Flows screen — layered DAG process-flow explorer (#1441)"
```

- [ ] **Step 5: Push branch and open PR**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/webui-flows && git push -u origin feat/webui-v2-flows
```

Then open PR with gh targeting main.
