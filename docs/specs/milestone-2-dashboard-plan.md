# Milestone 2 — Web Dashboard: Implementation Plan

**Spec sources:** Issues #26 (DASH-1, closed), #27 (DASH-2), #28 (DASH-3, closed), #29 (DASH-4, closed), #30 (DASH-5), #40 (Epic), #426 (Layer-2 names, open); `internal/types/kinds.go`; `internal/mcp/server.go` + `tools.go`; `internal/engine/process_flow.go`; `internal/links/phantom_edges.go`.

**Status at plan date (2026-05-20):** DASH-1 shipped. Binary embeds SPA assets via `go:embed`. REST routes present: `GET /api/registry`, `GET /api/groups/{group}/graph`, `GET /api/groups/{group}/repos/{repo}/graph`, `POST /api/admin/groups`, `POST /api/admin/groups/{group}/repos`. All MCP tools registered and callable.

---

## Section 1 — Architecture Overview

### Frontend ↔ Backend topology

```
Browser SPA (React + TypeScript)
     │
     │  REST calls over localhost HTTP
     ▼
Go HTTP server  (internal/dashboard/server.go)
     │
     ├─ /api/*         REST handlers (new wrappers call internal/mcp/*.go logic)
     ├─ /ws/events     WebSocket push (new — re-index / watcher events)
     └─ /              go:embed static assets (SPA shell + compiled JS)

MCP tools are NOT called directly by the browser.
The REST layer is a thin adapter over the same handler functions
that the MCP tools call. Both paths share internal/mcp/State.
```

The critical observation: `internal/mcp/tools.go` already exposes `handleQueryGraph`, `handleGetNode`, `handleGetNeighbors`, `handleTraces`, `handleListCommunities`, `handleGraphStats`, `handlePatterns`, `handleRepairs`, `handleEnrichments`, `handleCrossLinks`, `handleGetNodeSource`, `handleSaveResult`, `handleListFindings`, and `handleRecentActivity` as ordinary Go methods on `*mcp.Server`. REST adapters are thin wrappers that parse HTTP query params and delegate to those same handlers. Zero logic duplication is possible if the REST layer constructs a synthetic `mcp.CallToolRequest` from HTTP params.

### State-management approach

**Decision: React Query + URL state + minimal Zustand.**

Justification:
- React Query handles server state (fetch, cache, background refetch, loading / error states) with zero boilerplate. It eliminates the need for global Redux-style stores for server data.
- URL state (via `useSearchParams`) handles shareable selection: selected entity ID, active group, active repo filter, edge-kind filter, active route. Deep-linking is free.
- Zustand (one small store, `<2 kB`) handles the two genuinely cross-component concerns that are not URL-serializable: (a) the current hover target in the 3D canvas (not URL-worthy, high-frequency), (b) the graph camera position during a "zoom to node" animation.
- No Redux, no Context-as-global-state. The architecture stays LEGO: every component gets data from a hook; hooks get data from React Query or Zustand selectors.

### Routing structure

```
/              → redirect to /graph
/graph         → Surface 1: Graph Viewer
/flows         → Surface 2: Process Flow Explorer
/topology      → Surface 3: Broker Topology
/api           → Surface 4: API & Contracts Explorer
/docs          → Surface 5: Docs Portal
/admin         → Admin UI (DASH-4, already scoped)
```

All routes are nested under a `<AppShell>` that renders the top nav, group picker, and sidebar. `react-router-dom` v6 with `createBrowserRouter`.

### Folder layout

```
frontend/
  src/
    api/          # typed fetch wrappers — one file per REST resource group
    components/   # presentational only; named by surface or by shared primitive
      graph/
      flows/
      topology/
      api-explorer/
      docs/
      shared/     # KindIcon, KindBadge, SourceSnippet, ChainStep, etc.
    hooks/        # all logic; named use<Noun> or use<Surface><Noun>
    routes/       # one file per top-level route; thin: mounts hooks, passes props
    types/        # TypeScript type contracts (Section 2)
    lib/          # pure utilities: color palette, lunr index builder, mermaid init
    store/        # Zustand store(s); one file per domain slice
  index.html
  vite.config.ts
  tsconfig.json
```

Components in `routes/` are the only "container" components allowed. They call 1-2 hooks and pass data down to presentational children. No component may call `fetch` directly.

### Build pipeline

**Vite** (not Next.js). Reasoning:
- Output is a static SPA embedded via `go:embed`. Next.js static export adds complexity with no benefit — there is no SSR requirement and the Go server already handles routing.
- Vite produces a deterministic `dist/` output that maps cleanly to the `static/` directory watched by `go:embed`.
- The dashboard already has a `static/` directory with `go:embed` wired in `internal/dashboard/static.go`.

Build command: `vite build --outDir ../internal/dashboard/static`. The Go build then embeds the compiled output. Development: `vite dev` proxies `/api` to the Go server running on the chosen port.

---

## Section 2 — Type Contracts

All types live in `frontend/src/types/`. The authoritative source is `internal/types/kinds.go`. The recommended sync strategy is **`tygo`** (open source Go-to-TypeScript type generator): run `tygo gen` in CI targeting `kinds.go` to emit `EntityKind` and `RelationshipKind` union types automatically. All other types are hand-maintained against the JSON shapes returned by REST handlers.

### Entity kinds (auto-generated from kinds.go)

```typescript
// Auto-generated by tygo from internal/types/kinds.go — do not hand-edit.
export type EntityKind =
  | "Operation" | "Component" | "Class" | "Function"
  | "Schema" | "Variable" | "Reference" | "Pattern"
  | "Evolution" | "Endpoint" | "Route" | "Service"
  | "View" | "UIComponent" | "JSX" | "Stylesheet"
  | "Queue" | "Event" | "Datastore" | "DataAccess"
  | "ExternalAPI" | "InfraResource" | "CodeBlock"
  | "Document" | "Heading" | "ScopeUnknown"
  | "External" | "Project" | "Config" | "Model"
  | "AgentPattern" | "MessageTopic" | "Process";
// Note: MCP layer strips "SCOPE." prefix (ADR-0003).
```

### Relationship kinds (auto-generated)

```typescript
export type RelationshipKind =
  | "CALLS" | "IMPORTS" | "EXTENDS" | "IMPLEMENTS"
  | "USES" | "USES_HOOK" | "CONTAINS" | "DEPENDS_ON"
  | "REFERENCES" | "ROUTES_TO" | "SERVES" | "PUBLISHES_TO"
  | "TESTS" | "HAS_PROPS" | "ACCESSES_TABLE" | "INJECTED_INTO"
  | "READS_FROM" | "WRITES_TO" | "RENDERS" | "RETURNS"
  | "TAGGED_AS"
  | "EXEMPLAR" | "TOUCHES" | "ANTI_EXEMPLAR" | "SUPERSEDES"
  | "CONFLICTS_WITH" | "CO_APPLIES_WITH" | "PREREQUISITE" | "CREATED_BY"
  | "PLATFORM_VARIANT_OF" | "QUERIES" | "FETCHES"
  | "SUBSCRIBES_TO" | "TRANSFORMS"
  | "WS_SUBSCRIBES_TO" | "WS_EMITS" | "WS_CONNECTS"
  | "STREAMS_FROM" | "STREAMS_TO"
  | "GRAPHQL_SUBSCRIBES" | "GRAPHQL_PUBLISHES"
  | "STEP_IN_PROCESS" | "ENTRY_POINT_OF";
```

### Core wire types (hand-maintained)

```typescript
// Derived from serializeEntity() in internal/mcp/tools.go.
export interface Entity {
  id: string;                  // prefixed (<repo>::<local>) in multi-repo context
  label: string;               // e.Name
  qualified_name: string;
  kind: EntityKind;
  source_file: string;
  start_line: number;
  end_line: number;
  language: string;
  repo: string;
  pagerank?: number;
  community_id?: number;
  properties?: Record<string, unknown>; // framework, verb, path, broker, etc.
  findings?: Finding[];
}

// Derived from graph.Relationship in internal/graph/. Phantom edges
// carry cross_repo + target_repo from internal/links/phantom_edges.go.
export interface Relationship {
  from_id: string;
  to_id: string;
  kind: RelationshipKind;
  cross_repo?: boolean;        // true on phantom edges (#769)
  target_repo?: string;        // slug of target repo for phantom edges
  link_method?: "http" | "kafka_topic" | "ws_channel";
  properties?: Record<string, unknown>;
}

// Derived from handleTracesList / handleTracesGet in internal/mcp/traces.go.
export interface ProcessStep {
  entity_id: string;
  label: string;
  source_file: string;
  start_line: number;
  repo: string;
  step_index: number;
  edge_kind: RelationshipKind;
}

export interface Process {
  process_id: string;
  repo: string;
  label: string;
  entry_id: string;
  entry_name: string;
  terminal_id: string;
  step_count: number;
  cross_stack: boolean;
  crosses_external_lib?: boolean;
  terminal_is_phantom?: boolean;
  chain_labels: string[];
  source_file?: string;
  steps?: ProcessStep[];       // populated on action=get, absent on action=list
}

// Derived from handleListCommunities in internal/mcp/tools.go.
export interface Community {
  repo: string;
  id: number;
  size: number;
  modularity?: number;
  auto_name?: string;          // TF-IDF layer (#28 / #425)
  agent_name?: string;         // agent-resolved layer (#426; optional)
  top_entities: string[];      // top entity IDs
}

// Derived from CrossRepoLink type in internal/mcp/tools.go.
export interface LinkRecord {
  source: string;              // prefixed ID
  target: string;              // prefixed ID
  kind: RelationshipKind;
  confidence: number;
  channel?: string;
  method?: string;
}

// Derived from agentpatterns package (ADR-0018).
export interface Pattern {
  id: string;
  title: string;
  description: string;
  category: "code" | "process" | "team" | "tooling" | "architecture";
  is_candidate: boolean;
  is_private?: boolean;
  exemplar_ids?: string[];     // EXEMPLAR edges
  anti_exemplar_ids?: string[];
  touches_ids?: string[];      // TOUCHES edges
  supersedes_ids?: string[];
  conflicts_with_ids?: string[];
  co_applies_with_ids?: string[];
  prerequisite_ids?: string[];
}

// Derived from enrichment.Repair in internal/enrichment/.
export interface RepairResidual {
  residual_id: string;
  repo: string;
  action: "bind_to_entity" | "reclassify_as_external" | "reclassify_as_dynamic"
        | "reclassify_as_resolved" | "abandon";
  confidence: number;
  reasoning?: string;
  resolved_at?: string;        // RFC3339
}

// Derived from http_endpoint entity properties (internal/mcp/routing.go, wave-1 PRs).
export interface HttpEndpoint {
  id: string;
  repo: string;
  verb: string;                // GET, POST, etc.
  path: string;
  handler: string;             // function/method label
  framework?: string;          // express, fastapi, gin, etc.
  response_keys?: string[];    // from response_shape extraction
  dynamic_response?: boolean;  // true when shape was runtime-only
  is_webhook?: boolean;
  webhook_provider?: string;   // github, stripe, etc.
  status_codes?: number[];
  inbound_fetches?: string[];  // entity IDs of FETCHES sources
  outbound_queries?: string[]; // entity IDs of DB QUERIES from handler
  language?: string;
}

// Derived from MessageTopic entity kind + PUBLISHES_TO / SUBSCRIBES_TO / TRANSFORMS.
export interface MessageTopic {
  id: string;
  repo: string;
  label: string;               // topic name
  broker: string;              // kafka | rabbitmq | sqs | pubsub | nats
  producers: string[];         // entity IDs that PUBLISHES_TO this topic
  consumers: string[];         // entity IDs that SUBSCRIBES_TO this topic
  transforms_to?: string[];    // target topic IDs (TRANSFORMS edge)
}

// Queue entity (SCOPE.Queue) — used for RabbitMQ / SQS queue nodes.
export interface QueueNode {
  id: string;
  repo: string;
  label: string;
  broker: string;
  producers: string[];
  consumers: string[];
}

// ChannelEvent entity for WebSocket / SSE / GraphQL subscription events.
export interface ChannelEvent {
  id: string;
  repo: string;
  label: string;
  channel_type: "websocket" | "sse" | "graphql_subscription";
  emitters: string[];          // WS_EMITS / STREAMS_TO / GRAPHQL_PUBLISHES sources
  subscribers: string[];       // WS_SUBSCRIBES_TO / STREAMS_FROM / GRAPHQL_SUBSCRIBES
}

// Scheduled job (properties on SCOPE.Component or SCOPE.Function entities).
export interface ScheduledJob {
  id: string;
  repo: string;
  label: string;
  schedule: string;            // cron expression
  handler: string;
  framework?: string;          // celery, apscheduler, cron, etc.
  source_file: string;
  start_line: number;
}

// Saved finding from handleSaveResult.
export interface Finding {
  question: string;
  answer: string;
  type: string;
  nodes?: string[];
  saved_at: string;
}
```

---

## Section 2.5 — Graph Level-of-Detail Strategy

The a/b/c group and similar large corpora exceed 25k nodes. Full-graph rendering at that scale is not viable. This section specifies the LoD tiers that `useGraphLoD` enforces. Both `<GraphCanvas3D>` and `<GraphCanvas2D>` must consume `useGraphLoD`'s output rather than receiving raw node arrays directly.

### LoD tiers

| Tier | Trigger condition | What renders |
|---|---|---|
| **Zoom-out** | ≥ 5 000 visible nodes | Community CENTROIDS only — one sphere per community, size ∝ member count, color = community color |
| **Mid-zoom** | 1 000 – 4 999 visible nodes | Community centroids + top-K god-nodes (≤ 50 per community, ranked by centrality) |
| **Zoom-in** | < 1 000 visible nodes | Full member expansion within the camera frustum only |

### Always-visible override

Regardless of tier, the selected node and its 1-hop neighbors are always rendered. The LoD derivation must check the current selection before culling.

### Edge culling rule

An edge renders only when **both** endpoints are visible at the current LoD tier. Edges to culled nodes are suppressed entirely — no dangling stubs.

### Hard render cap

When the unfiltered node count exceeds 20 000 and no active filter is applied, the canvas renders nothing. An `<EmptyState>` prompt is shown: "Apply a filter to render this graph." The `useGraphLoD` hook returns `lodLevel: "blocked"` in this state. No partial render is attempted above the cap.

### Hook contract

```
useGraphLoD(zoomLevel, viewport, communityTree)
  → { visibleNodeIds: Set<string>, visibleEdgeIds: Set<string>, lodLevel: "zoom-out" | "mid" | "zoom-in" | "blocked" }
```

Pure derivation from inputs. No side effects. Memoized on `(zoomLevel, viewport, communityTree)` identity. Both canvas components receive `visibleNodeIds` and `visibleEdgeIds` as their data source; raw node arrays are never passed directly to canvas components.

### Backend support required

The `/api/graph/{group}` endpoint must support a `lod` query parameter:

- `lod=centroids` — returns community-centroid summary objects only (no member nodes). Each centroid carries: `community_id`, `size`, `auto_name`, `agent_name`, `color`, `centroid_position` (derived from member PageRank-weighted average), and `top_entity_ids` (top 3 by centrality).
- `lod=mid` — returns centroids + top-50 god-nodes per community (full entity shape for those nodes).
- `lod=full` — returns all nodes (existing behavior; only safe below 20k).

**Decision: use the `lod=` query param on the existing `/api/graph/{group}` route** (not a separate `/centroids` sub-route). Rationale: keeps the URL surface small; the frontend already caches by `(group, lod)` key pair in React Query.

The community-centroid aggregator is new Go code with no existing MCP analog. It must be implemented as part of Phase 1 backend work (see Section 8 update).

---

## Section 3 — REST Endpoint Catalog

**Design principle:** endpoints are shaped by **frontend surface data needs**, not by MCP tool count. Each endpoint returns exactly what a surface needs for its first paint. Internally, an endpoint may call 2–5 MCP handler functions and reshape their output — that shaping logic lives in the REST adapter layer, not in the frontend.

### Surface-oriented endpoint catalog

| Endpoint | Surface | Method | Returns | Internally calls |
|---|---|---|---|---|
| `GET /api/dashboard/init` | global | GET | `{registry, groups[], settings}` | reads registry + group metadata |
| `GET /api/graph/{group}?lod=centroids\|mid\|full` | 1 (Graph) | GET | `{nodes, edges, communities}` LoD-aware | `archigraph_clusters`, `archigraph_stats`, plus community-centroid computation (new) |
| `GET /api/graph/{group}/entity/{id}` | 1 (inspector) | GET | `{entity, neighbors[], inboundEdges[], outboundEdges[]}` | `archigraph_inspect` + `archigraph_expand` |
| `GET /api/flows/{group}?entry={id}&cross_stack_only=true&limit=50` | 2 (Flows list) | GET | `{processes: Process[]}` with chain step entities expanded | `archigraph_traces` |
| `GET /api/flows/{group}/{processId}` | 2 (Flow detail) | GET | `{process, chainEntities[], sourceSnippets[]}` | `archigraph_traces` + `archigraph_inspect` per step + `archigraph_get_source` per step |
| `GET /api/topology/{group}` | 3 (Topology) | GET | `{topics[], queues[], channels[], producers, consumers}` | filter on broker entity kinds + their edges |
| `GET /api/paths/{group}?prefix=&q=&page=&size=&repo=&framework=&status_code=&is_webhook=` | 4 (API explorer) | GET | `{paths: PathRow[], tree: PathTreeNode[], total, hasMore}` paginated, grouped by path | `archigraph_find` + http_endpoint entity scan + path-grouping aggregator (new) |
| `GET /api/paths/{group}/{pathHash}` | 4 (Path detail) | GET | `{path, verbs[], handlers[], responseShapes[], inboundFetches[], outboundQueries[]}` | combines `archigraph_inspect` + `archigraph_expand` + `archigraph_get_source` per handler |
| `GET /api/docs/{group}` | 5 (Docs portal) | GET | `{tree, recentFiles[]}` | filesystem scan of group docs path |
| `GET /api/docs/{group}/{path}` | 5 (Doc page) | GET | `{markdown, navTree, breadcrumbs}` | filesystem read + nav derivation |
| `GET /api/docs/{group}/{path}?include=hovercards` | 5 (Doc page with symbol cards) | GET | `{markdown, navTree, breadcrumbs, hovercards: {symbol → EntityCard}}` | filesystem read + symbol resolution for backticked identifiers |
| `GET /api/search/{group}?q=` | global typeahead | GET | `{entities[], docs[], paths[]}` | `archigraph_find` + doc index |
| `GET /api/patterns/{group}` | cross-surface | GET | `{patterns: Pattern[]}` with exemplars resolved | `archigraph_patterns(action=query)` + entity expansion |
| `GET /api/repairs/{group}` | admin sub-surface | GET | `{residuals, openCount, autoResolvableCount}` | `archigraph_repairs(action=list)` |
| `WebSocket /ws/events` | global | WS | push: `{type: "reindex_started\|completed\|watcher_event", group, repo}` | server-side watcher integration |

### Supporting endpoints (admin, mutations, low-level)

| Endpoint | Method | Notes |
|---|---|---|
| `GET /api/registry` | GET | Already shipped in DASH-1. Returns groups + repos. |
| `GET /api/groups/{group}/graph` | GET | Already shipped. Superseded by `/api/graph/{group}?lod=full` for new surfaces; kept for backward compat. |
| `GET /api/groups/{group}/repos/{repo}/graph` | GET | Already shipped. Single-repo graph. |
| `GET /api/groups/{group}/communities` | GET | Thin wrapper: reads communities array from graph.json. |
| `GET /api/groups/{group}/god-nodes` | GET | Top N by combined pagerank+betweenness. |
| `GET /api/groups/{group}/links` | GET | Returns X-links.json. |
| `GET /api/groups/{group}/topics` | GET | MessageTopic + Queue entities + broker metadata. Backing store for `/api/topology/{group}`. |
| `GET /api/groups/{group}/scheduled-jobs` | GET | Entities with `schedule` property set. |
| `GET /api/source` | GET | `node_id`, `group`, `context_lines` → `{ source, language, start_line }`. Shared by multiple surfaces. |
| `GET /api/findings` | GET | `group`, `entity_id`, `since`, `limit` → `{ findings: Finding[] }` |
| `POST /api/findings` | POST | Save finding. |
| `GET /api/enrichments` | GET | Enrichment candidate list. |
| `POST /api/enrichments` | POST | Submit / reject enrichment decision. |
| `GET /api/cross-links` | GET | Cross-repo link candidates. |
| `POST /api/cross-links` | POST | Accept / reject cross-link. |
| `POST /api/admin/groups/{group}/rebuild` | POST | Trigger rebuild. **NEW Go code required.** |
| `DELETE /api/admin/groups/{group}/repos/{slug}` | DELETE | Unregister. **NEW Go code required.** |
| `POST /api/admin/groups/{group}/repos/{slug}/reindex` | POST | Re-index single repo. **NEW Go code required.** |

### New backend logic required (not simple MCP delegation)

Two endpoints require aggregation logic that has no existing MCP analog:

1. **Community-centroid aggregator** (backing `GET /api/graph/{group}?lod=centroids|mid`) — computes centroid positions from PageRank-weighted member coordinates, builds the centroid summary shape. New Go code in `internal/dashboard/`. Estimated effort: 1.5 days.

2. **Path-grouping aggregator** (backing `GET /api/paths/{group}`) — groups 1 212 `http_endpoint` entities by unique path string, deduplicates DRF ViewSet expansion artifacts (hide `urlconf_nested_include ANY` when a `drf_router_expanded` entry exists for the same path per #792 logic), builds the `PathTreeNode` prefix tree server-side, computes per-path verb sets and multiplicity badges. New Go code in `internal/dashboard/`. Estimated effort: 1.5 days.

### Phase 1 effort re-estimate

The REST adapter layer is now larger in scope: surface-shaped endpoints each call 2–5 MCP handlers and reshape output. The previous estimate of 2 days for thin wrappers no longer holds. Updated Phase 1 backend estimate: **7–9 days** (up from 6.5). The two new aggregators account for ~3 days of that increase. Phase 2–6 frontend work gets cheaper in exchange because each surface's hook now calls one cohesive endpoint rather than assembling data from 3–4 parallel calls.

---

## Section 4 — Component and Hook Inventory per Surface

Naming convention: every component is presentational (no direct fetch). Every hook owns its data contract with the server.

---

### Surface 1 — Graph Viewer (`/graph`)

**Components:**

| Component | Responsibility |
|---|---|
| `<GraphCanvas3D>` | Wraps `3d-force-graph`. Receives `{ nodes, edges, onNodeClick, onNodeHover, cameraRef }` as props. Zero data-fetching logic. |
| `<GraphCanvas2D>` | Fallback for `prefers-reduced-motion` or low-end hardware. Same prop interface as 3D. |
| `<GraphToolbar>` | Search input, reset-view button, layout toggle (force / tree / sphere), 3D/2D toggle. Calls `useGraphCamera`, `useGraphSelection` via callbacks. |
| `<EdgeKindFilters>` | Multi-select chip row; one chip per RelationshipKind present in graph. |
| `<CommunityLegend>` | Scrollable color key: community color swatch + name (auto_name or agent_name). |
| `<NodeChip>` | Small badge: kind icon + entity label. Used inside inspector and legend. |
| `<EdgeBadge>` | Colored chip sized to relationship kind name. |
| `<EntityInspector>` | Slide-in panel. Shows entity metadata, source location, findings, and neighbor list as `<ChainStep>` rows. |
| `<GodNodeHalo>` | Visual indicator (pulsing ring overlay in canvas) for entities in top god-node list. Drawn as instanced mesh via hook-provided IDs. |
| `<GraphSearchTypeahead>` | Floating dropdown that reacts to `useSearchQuery`. Selects and zooms on pick. |

**Hooks:**

| Hook | Responsibility |
|---|---|
| `useGraphData(group, lodLevel, filters)` | Chains `useGraphLoD(...)` to determine LoD level, then calls `GET /api/graph/{group}?lod=centroids\|mid\|full` once. Server returns LoD-shaped `{nodes, edges, communities}`. Derives 3d-force-graph arrays via `visibleNodeIds` + `visibleEdgeIds`. |
| `useEntityInspector(entityId)` | Fetches `/api/inspect?id=...` + `/api/expand?node_id=...`. Drives `<EntityInspector>` data. |
| `useEdgeKindFilters()` | Reads / writes URL search param `edge_kinds`. Returns current set + toggle handler. |
| `useCommunityColors(communities)` | Derives a stable `Map<communityId, hexColor>` from community IDs using a seeded palette from `lib/colors.ts`. Stable across renders. |
| `useGraphCamera()` | Zustand slice: exposes `zoomToNode(id)`, `resetView()`, `cameraRef`. Used by toolbar and inspector. |
| `useGraphSelection()` | URL param `entity` — reads selected entity ID, exposes `select(id)`. Syncs inspector open state. |
| `useGodNodes(groupId)` | Fetches `/api/groups/{group}/god-nodes`. Returns `Set<entityId>` for canvas halo logic. |
| `useGraphSearch(q)` | Delegates to `useSearchQuery(q)` (shared primitive). |

---

### Surface 2 — Process Flow Explorer (`/flows`)

**Components:**

| Component | Responsibility |
|---|---|
| `<EntryPointPicker>` | Searchable list of Process entities from `useFlowList`. User selects one to drill in. |
| `<FlowCanvas>` | Renders the swim-lane chain. One `<FlowLane>` column per repo touched by the Process. |
| `<FlowLane>` | Single-repo column. Renders a vertical sequence of `<ChainStep>` components. |
| `<PhantomEdgeConnector>` | SVG overlay that draws cross-lane arrows for phantom edges (cross_stack=true paths). |
| `<FlowStepDetail>` | Slide-in detail panel on step click: shows `<SourceSnippet>` + entity metadata. |
| `<FlowFilters>` | Toggle: show all flows / cross-stack only. Repo filter multi-select. |
| `<FlowBreadcrumb>` | Shows current process label + entry point + terminal node. |
| `<CrossStackBadge>` | Small banner: "Crosses 2 repos via HTTP" — rendered on cross_stack=true processes. |

**Hooks:**

| Hook | Responsibility |
|---|---|
| `useFlowList(group, {entry, cross_stack_only, limit})` | GET `/api/flows/{group}?entry={id}&cross_stack_only={bool}&limit={n}`. Returns `{processes: Process[]}` with chain step entities expanded. |
| `useFlowDetail(group, processId)` | GET `/api/flows/{group}/{processId}`. Returns `{process, chainEntities[], sourceSnippets[]}`. |
| `useFlowFollow(entryPointId)` | GET `/api/traces?action=follow&entry_point_id=...`. Ad-hoc BFS, used when user clicks an entity not in the pre-computed list. |
| `useFlowFilters()` | URL params: `cross_stack_only`, `repo_filter`. |
| `useFlowStepSource(entityId)` | Delegates to `useEntitySource(entityId)` (shared). |
| `useFlowLaneLayout(process)` | Pure derivation: groups steps by repo, returns `Map<repo, ProcessStep[]>` for lane rendering. |

---

### Surface 3 — Broker Topology (`/topology`)

**Components:**

| Component | Responsibility |
|---|---|
| `<TopologyCanvas>` | Force-directed layout (2D, simpler than Surface 1) of topics/queues as hub nodes with producer/consumer spokes. Uses `d3-force` directly (lighter than 3d-force-graph for this layout). |
| `<TopicNode>` | Hub node: shows broker icon + topic name + message count if available. |
| `<ProducerSpoke>` | Colored arc from topic to producer entity. Color = repo. |
| `<ConsumerSpoke>` | Colored arc from topic to consumer entity. Color = repo. |
| `<TransformEdge>` | Topic-to-topic edge (TRANSFORMS). Dashed line. |
| `<ChannelTrack>` | Separate sub-section for WebSocket / SSE / GraphQL events. Same spoke layout but labeled with channel type. |
| `<BrokerFilter>` | Chip row: Kafka / RabbitMQ / SQS / PubSub / NATS. Toggles broker visibility. |
| `<TopicInspector>` | Slide-in: topic name, broker, producers list, consumers list, related entities. |
| `<ScheduledJobList>` | Sidebar list of `ScheduledJob` entities; separate from the main canvas. |

**Hooks:**

| Hook | Responsibility |
|---|---|
| `useTopologyData(groupId, brokerFilter)` | GET `/api/topology/{group}`. Returns `{topics[], queues[], channels[], producers, consumers}` LoD-shaped from server. Derives the d3-force node/link arrays. |
| `useBrokerFilter()` | URL param `brokers`. Multi-select toggle. |
| `useTopicInspector(topicId)` | Fetches entity neighbors for topic node. |
| `useScheduledJobs(groupId)` | GET `/api/groups/{group}/scheduled-jobs`. |
| `useChannelEvents(groupId)` | Derived from `useTopologyData` — filters to `ChannelEvent` objects. |
| `useTopologyLayout(nodes, edges)` | Runs d3-force simulation, returns stable positions. |

---

### Surface 4 — API & Contracts Explorer (`/api`)

**Scale reality (fixture-a audit, 2026-05-20):** 1 212 `http_endpoint` entities → 648 unique paths → ~1 166 unique (verb, path) routes. DRF ViewSet expansion produces 5–7 entities per path. Multiplicity histogram: 444 single-entity paths, 88×3, 90×5, 24×2, 2×4. 46 paths carry redundant `urlconf_nested_include ANY` + per-verb dupes (will be cleaned by #792). These numbers are first-class requirements, not edge cases.

**Components:**

| Component | Responsibility |
|---|---|
| `<PathTreeSidebar>` | Tree of URL prefix groups (e.g., `/api/v1/` parent nesting `/api/v1/buildings/{pk}`, `/api/v1/users`). Computed server-side and delivered by `/api/paths/{group}`; sidebar renders the `tree` field directly. Collapsible nodes. |
| `<PathRow>` | One row per unique path (NOT per entity). Shows path string, `<MultiplicityBadge>`, and inline `<VerbChip>` set for all verbs supported by that path. Clickable to drill into path detail page. |
| `<VerbChip>` | Colored chip per HTTP verb: GET=green, POST=blue, PUT=orange, DELETE=red, PATCH=yellow, ANY=gray, WS=purple. Click on a chip navigates to `<PathDetailPage>` filtered to that (verb, path). Replaces the old `<VerbBadge>`. |
| `<MultiplicityBadge>` | Shows "5 endpoints" on ViewSet rows so users understand CRUD multiplicity. Hidden on single-entity paths. |
| `<PathFilterPanel>` | Server-side filter controls: repo multi-select, framework select, status_code filter, is_webhook toggle, prefix text match. Submits params to `/api/paths/{group}?...`. |
| `<PathSearchInput>` | Typeahead on path string with prefix matching (e.g., `/buildings` matches `/api/v1/buildings/{pk}`). Server-side — calls `/api/paths/{group}?q=...`. |
| `<Pagination>` | Page controls for the path list. Server-side; 50 rows per page. |
| `<PathDetailPage>` | Full-page view for `/paths/{group}/{pathHash}`. Shows: handler entities, response shapes, inbound FETCHES (clients that call this path), outbound QUERIES (tables touched by handler). Backed by `GET /api/paths/{group}/{pathHash}`. |
| `<ResponseShapeGrid>` | Shows `response_keys[]` as a small key-type grid. If `dynamic_response=true`, shows a `<DynamicResponseNotice>`. |
| `<WebhookBadge>` | Small icon + provider label (GitHub, Stripe, etc.) for webhook endpoints. |
| `<InboundFetchList>` | List of entities that FETCHES this path. Each row is a `<NodeChip>`. |
| `<OutboundQueryList>` | List of QUERIES edges from the handler entity. |

**Hooks:**

| Hook | Responsibility |
|---|---|
| `usePathList(group, filters, page)` | GET `/api/paths/{group}?page=N&size=50&...filters`. Returns `{ paths: PathRow[], tree: PathTreeNode[], total, hasMore }`. Server-side pagination and filtering; no client-side filter on this dataset. |
| `usePathTree(group)` | Derived from `usePathList` cache — returns the `tree` field for `<PathTreeSidebar>`. No extra fetch. |
| `usePathDetail(group, pathHash)` | GET `/api/paths/{group}/{pathHash}`. Returns combined path detail shape. |
| `usePathFilters()` | URL params: `repo`, `framework`, `status_code`, `is_webhook`, `prefix`, `q`, `page`. Exposes setters; changes trigger new `usePathList` fetch. |
| `useResponseShape(entityId)` | Reads `response_keys` and `dynamic_response` from entity properties. Pure derivation, no fetch. |

**Noise suppression rule:** the `<PathFilterPanel>` (and the backing aggregator) hides `urlconf_nested_include ANY` entries when a `drf_router_expanded` entry exists for the same path. This is enforced server-side in the path-grouping aggregator. When #792 ships and deduplicates at the graph level, this suppression becomes a no-op and can be removed.

---

### Surface 5 — Docs Portal (`/docs`)

#### Stack Choice & Architecture Decision

**Why not embedded VitePress / Docusaurus / Nextra:** These frameworks bring their own build pipeline and route handlers, which would conflict with the SPA shell routing and the need for Surface 1 (Graph Viewer) deep-links from backticked code symbols. We gain 80% of VitePress's polish through `react-markdown` + well-designed Radix UI shell components while retaining full control over entity linking and integration with the rest of the dashboard.

**Stack table:**

| Concern | Pick | Why |
|---|---|---|
| Markdown rendering | `react-markdown` + `remark-gfm` | Battle-tested, plugin-driven, no build step, GFM support out of box |
| Code highlighting | `prism-react-renderer` | Same engine VitePress uses; tree-shakable by language; no startup cost |
| Mermaid diagrams | `mermaid` (lazy-loaded via React.lazy) | ~200KB uncompressed; dynamic import only when a code block of language `mermaid` exists |
| Math (if needed later) | `remark-math` + `katex` | Same as VitePress; optional dependency, lazy-loaded |
| Shell components | Radix UI primitives + Tailwind | LEGO mindset; no opinionated chrome; full control over behavior |
| Dark mode | CSS variables + `prefers-color-scheme` + manual toggle | One CSS layer; both themes auto-adapt mermaid + prism colors |
| Search | Server-side via `/api/search/{group}?q=` | Client-side lunr would ship a 5–10 MB index; server-side keeps client bundle tight |

**Bundle budget:**
- `react-markdown` + `remark-gfm`: ~50 KB gzipped
- `prism-react-renderer` + selected languages (JS, Python, Go, SQL, Bash, YAML): ~25 KB gzipped
- **Total docs-portal bundle without mermaid: ~80 KB gzipped**
- Mermaid (200 KB uncompressed) lazy-loaded only when a page contains a `mermaid` code block
- **With mermaid loaded on a page: ~280 KB total** — acceptable for a feature surface

#### UX Features: Parity + Archigraph Differentiators

**Standard polish (VitePress parity):**
- Collapsible sidebar tree with URL-persistent state
- Breadcrumbs derived from `group/repo/module/doc-file` path
- Right-rail table of contents (h2/h3 headings) with scroll-spy active-section highlight
- Auto Prev/Next page links from flat doc tree order
- Reading progress bar at top of long pages
- Code blocks with copy button, line numbers, language label, and syntax highlighting
- Mermaid diagrams rendering and adapting to current dark/light theme
- Smooth scroll-to-anchor on heading navigation
- Print stylesheet for documentation archival

**Archigraph-specific differentiators:**
- **Backticked code identifiers in headings are auto-linked:** Clicking `` `OrderViewSet` `` in a heading (or inline) deep-links to Surface 1 with that entity selected and details expanded — materializes the doc-as-bridge concept from ADR-0007
- **Cross-doc links in `<repo>::<heading>` syntax:** Server-resolved at render time to navigate to the right page with optional anchor
- **Entity hovercards on backticked symbols:** Hovering a backticked identifier (e.g., `` `UserSerializer` ``) pops a mini card showing: kind, source file, start line, and top 2–3 outbound edges
- **Pattern callouts:** `{{pattern:tenant-prefix-route}}` block syntax renders a styled callout with the pattern's exemplar entities embedded inline for context
- **Process flow diagrams:** `{{flow:<entry-entity-id>}}` renders a mini swim-lane Mermaid diagram auto-generated from `archigraph_traces` data, showing the process chain inline

#### Component Inventory

| Component | Role |
|---|---|
| `<DocsPage>` | Top-level layout: sidebar + content area + right-rail TOC |
| `<DocsSidebar>` | Collapsible tree: groups → repos → modules → doc files. Active page highlight. |
| `<DocsSidebarItem>` | Single tree node: icon + label + active indicator + collapse toggle |
| `<DocsContent>` | Content-width constraint, vertical rhythm, theme-aware |
| `<MarkdownRenderer>` | Configured `react-markdown` instance with remark-gfm, rehype-highlight, rehype-mermaid plugins |
| `<CodeBlock>` | Prism-powered code block: language label, line numbers, copy button, dark/light theme support |
| `<MermaidBlock>` | Lazy-loaded mermaid wrapper component; theme-aware (reads CSS variables for colors) |
| `<EntityLink>` | Backticked symbol → deep-link to Surface 1 + hovercard on hover |
| `<EntityHovercard>` | Mini entity detail card: kind badge, source file, start line, top 3 outbound edges |
| `<PatternCallout>` | Pattern-reference rendering with exemplar list and styling |
| `<FlowDiagram>` | Auto-generated swim-lane block; fetches process steps and renders as Mermaid flowchart |
| `<DocsBreadcrumbs>` | Path-derived crumb trail: group / repo / module / file |
| `<DocsTOC>` | Right-rail heading list with scroll-spy active highlight |
| `<DocsPrevNext>` | Bottom footer nav: prev + next doc links from tree order |
| `<DocsSearch>` | Top-bar search input + typeahead; server-side backend |
| `<ThemeToggle>` | Light/dark mode switch; persists to localStorage |

#### Hooks

| Hook | Purpose |
|---|---|
| `useDocTree(group)` | Server-fetched nav tree: `GET /api/docs/{group}` → `{ tree: DocTreeNode[], recentFiles[] }` |
| `useDocContent(group, path)` | Markdown + breadcrumbs + prev/next: `GET /api/docs/{group}/{path}` |
| `useDocsSearch(group, query)` | Debounced server search: `GET /api/search/{group}?q=...` |
| `useTocScrollSpy(headings)` | Track active heading on scroll; returns active heading ID |
| `useTheme()` | Light/dark state + persistence via localStorage + CSS variable sync |
| `useEntityHovercard(entityId)` | Lazy-fetch entity metadata for hovercard display |
| `useEntityDeepLink()` | Build URL to Surface 1 with entity selected + graph camera positioned |

#### Server-Side Prep

Existing endpoints from Section 3:
- `GET /api/docs/{group}` — tree + recent files
- `GET /api/docs/{group}/{path}` — markdown page

**NEW endpoint (add to Section 3 REST catalog):**

- `GET /api/docs/{group}/{path}?include=hovercards` — When a page contains backticked symbols, the server pre-resolves each unique symbol to a brief entity card payload (kind, source_file, start_line, top-3 outbound edges). Returns the base markdown response plus an optional `hovercards: Map<symbolLabel, EntityCard>` payload. Saves N round-trips during document load for pages with many symbol references. Estimated effort: **0.5 day** on top of existing `/api/docs` work.

**Components:**

| Component | Responsibility |
|---|---|
| `<DocsSidebar>` | Collapsible tree: group → repo → module → doc file. URL-persistent expansion state. |
| `<DocsSidebarItem>` | Single tree node: file icon + label + active indicator. |
| `<DocsRenderer>` | Renders markdown via `react-markdown` with configured plugins. |
| `<MermaidDiagram>` | Wrapper around lazy-loaded mermaid lib. Theme-aware. |
| `<DocsSearch>` | Top-bar search input + typeahead powered by `/api/search/{group}?q=...`. |
| `<DocsSearchResult>` | Single search result: doc title + excerpt + score. |
| `<CodeSymbolLink>` | Backticked symbol in markdown → clickable link to Surface 1 + hovercard on hover. |
| `<DocsHeader>` | Breadcrumb: group / repo / module / file. Dark mode toggle. |
| `<DocsBreadcrumb>` | Navigation trail from current path. |
| `<DarkModeToggle>` | Light/dark switcher; persists to localStorage. |
| `<DocsTOC>` | Right-rail heading list with scroll-spy active highlight. |
| `<DocsPrevNext>` | Footer nav: previous + next doc from tree order. |

**Hooks:**

| Hook | Responsibility |
|---|---|
| `useDocTree(groupId)` | Fetches `/api/docs/{group}` to build sidebar tree. Handles tree expansion state via URL. |
| `useDocContent(group, path)` | Fetches markdown + breadcrumbs + prev/next from `/api/docs/{group}/{path}?include=hovercards`. |
| `useDocsSearch(q)` | Server-side search: `GET /api/search/{group}?q=...&type=docs`. Returns docs results only. |
| `useTocScrollSpy(headings)` | Monitors scroll position; returns active heading ID for right-rail highlight. |
| `useTheme()` | Light/dark mode state + localStorage persistence + CSS variable sync. |
| `useEntityHovercard(entityId)` | Lazy-fetch entity metadata for hovercard pop-up. Optional pre-fetch via `include=hovercards`. |
| `useEntityDeepLink()` | Generate Surface 1 deep-link URL for a given entity ID (entity + graph camera zoom). |

---

## Section 5 — Shared Primitives (the LEGO Bricks)

Components and hooks used across multiple surfaces. These live in `components/shared/` and `hooks/` and are the foundation each surface builds on.

### Shared components

| Component | Used by | Responsibility |
|---|---|---|
| `<KindIcon kind>` | All surfaces | SVG icon per EntityKind. Maps kind strings to an icon set (lucide-react covers most; fallback to a generic node icon). |
| `<KindBadge kind>` | All surfaces | `<KindIcon>` + kind label in a colored pill. Color set from `lib/kindColors.ts`. |
| `<RepoChip repo>` | Graph, Flows, API | Small pill with repo slug. Color derived from group palette. |
| `<LanguageChip language>` | Graph, API | Language icon (devicon or similar) + language name. |
| `<FrameworkChip framework>` | API, Topology | Framework logo badge (express, fastapi, gin, etc.). |
| `<SourceSnippet nodeId, contextLines>` | Flows, Graph inspector, API detail | Calls `useEntitySource`. Renders code with syntax highlighting (highlight.js). Shows file path + line range header. |
| `<ChainStep entity, stepIndex, isCross>` | Flows, Graph inspector | Single step row: step number + `<KindBadge>` + entity label + file:line. If `isCross` renders a cross-repo indicator. |
| `<EmptyState icon, title, message>` | All surfaces | Centered empty-state illustration + message. |
| `<LoadingState>` | All surfaces | Skeleton loaders matched to the layout of the consuming surface. |
| `<ErrorBoundary fallback>` | All routes | React error boundary. Catches render errors; shows fallback + error details in dev. |
| `<Tooltip>` | All surfaces | Accessible tooltip wrapper (Radix UI `@radix-ui/react-tooltip`). |
| `<Sheet open, onClose, children>` | Graph, Flows, API | Slide-in panel abstraction (Radix UI `@radix-ui/react-dialog`). Used for all side-panel inspectors. |

### Shared hooks

| Hook | Consumers | Responsibility |
|---|---|---|
| `useRegistry()` | All routes, AppShell | React Query: GET `/api/registry`. Returns group + repo list. |
| `useGroup(groupId)` | All routes | Derived from `useRegistry()` — returns a single group's metadata. |
| `useEntityById(id)` | Graph, Flows, API | GET `/api/inspect?id=<id>`. Cached per entity ID. |
| `useEdgesFromEntity(id, kindFilter?)` | Graph inspector, Flows | GET `/api/expand?node_id=<id>`. Optionally filters by edge kind client-side. |
| `useSearchQuery(q)` | Graph toolbar, Docs | GET `/api/find?q=<q>&full=true`. Returns `Entity[]` ordered by BM25 score. |
| `useEntitySource(entityId)` | SourceSnippet, Flows step | GET `/api/source?node_id=<entityId>`. Returns source snippet. |
| `useInitData()` | AppShell | GET `/api/dashboard/init`. Fetches first-paint payload. |
| `useWatcherEvents()` | AppShell | WebSocket `/ws/events`. Pushes re-index events to React Query's invalidation queue. |

---

## Section 6 — Build Order

Rationale for the proposed order: front-load the shared layer so every subsequent phase builds on stable primitives. Put Surface 4 (API explorer) before Surfaces 2 and 3 because it exercises the full data pipeline without complex layout logic. The most layout-intensive surfaces (flows, topology) come later when the team has proven the data flow.

### Phase 1 — Skeleton, Routing, REST Scaffolding, Shared Primitives
**Effort: 3-4 days**

- Go: add REST adapter layer in `internal/dashboard/` — thin wrappers that translate HTTP query params into `mcp.CallToolRequest` and call existing handlers. Start with `/api/find`, `/api/inspect`, `/api/stats`, `/api/clusters`, `/api/source`.
- Go: add `/api/dashboard/init` endpoint.
- Go: add missing `/api/groups/{group}/endpoints`, `/api/groups/{group}/topics`, `/api/groups/{group}/communities`, `/api/groups/{group}/god-nodes`, `/api/groups/{group}/links`.
- Frontend: Vite scaffold, Tailwind, react-router-dom, React Query, Zustand, tygo code-gen.
- Frontend: `AppShell` with nav, group picker, route placeholders.
- Frontend: all shared primitives — `<KindIcon>`, `<KindBadge>`, `<RepoChip>`, `<SourceSnippet>`, `<ChainStep>`, `<EmptyState>`, `<LoadingState>`, `<ErrorBoundary>`, `<Sheet>`, `<Tooltip>`.
- Frontend: shared hooks — `useRegistry`, `useEntityById`, `useEdgesFromEntity`, `useSearchQuery`, `useEntitySource`, `useInitData`.
- Deliverable: binary boots, SPA shell loads, group picker works, entity lookup works via `/api/inspect`.

### Phase 2 — Surface 1: Graph Viewer
**Effort: 4-5 days**

- All graph viewer components and hooks per Section 4.
- `3d-force-graph` integration: instanced mesh, halo, hover highlight, dark background.
- Community coloring, edge-kind filter chips, god-node halo.
- `<EntityInspector>` side panel with source snippet and neighbor list.
- 2D canvas fallback.
- Performance: cluster-by-community view when node count > 5 000 (threshold chosen from DASH-2 benchmarks and 3d-force-graph docs; see Risk 1).
- Deliverable: loads client-fixture group at ≥30 fps, hover < 50 ms.

### Phase 3 — Surface 4: API & Contracts Explorer
**Effort: 3-4 days** (increased from 2-3; scale reality requires server-side pagination, path-grouping aggregator, and prefix-tree sidebar)

- Go: implement path-grouping aggregator in `internal/dashboard/` backing `GET /api/paths/{group}`. Must: group 1 212 entities into 648 path rows, compute per-path verb sets, build `PathTreeNode` prefix tree server-side, suppress `urlconf_nested_include ANY` duplicates, return paginated `{ paths, tree, total, hasMore }`. New code — not a thin MCP wrapper.
- Go: implement `GET /api/paths/{group}/{pathHash}` detail endpoint.
- Frontend: `<PathTreeSidebar>`, `<PathRow>`, `<VerbChip>`, `<MultiplicityBadge>`, `<PathFilterPanel>`, `<PathSearchInput>`, `<Pagination>`, `<PathDetailPage>`.
- Frontend: `usePathList`, `usePathTree`, `usePathDetail`, `usePathFilters`.
- Server-side pagination enforced at 50 rows/page. No 648-row client-side list.
- Deliverable: developer can browse all 648 paths paginated, filter by repo/framework/webhook/prefix, click a verb chip to drill into (verb, path) detail, see handler + response shapes + FETCHES callers + outbound queries.

### Phase 4 — Surface 2: Process Flow Explorer
**Effort: 3-4 days**

- All flow explorer components and hooks.
- Swim-lane layout: derive lanes from `Process.steps` grouped by `repo`. Sorted by step_index.
- PhantomEdgeConnector SVG for cross-repo arrows.
- Source snippet on step click via `<FlowStepDetail>`.
- Deliverable: can pick any cross-stack process and trace it lane by lane.

### Phase 5 — Surface 3: Broker Topology
**Effort: 2-3 days**

- Go: ensure `/api/groups/{group}/topics` returns MessageTopic + QueueNode + ChannelEvent + ScheduledJob shapes.
- d3-force hub-and-spoke layout.
- Broker filter chips, channel track, topic inspector.
- Deliverable: Kafka / RabbitMQ / SQS topology visible; WebSocket/SSE channel events in separate track.

### Phase 6 — Surface 5: Docs Portal
**Effort: 3-4 days** (increased from 2-3; richer scope includes entity hovercards, pattern callouts, flow diagrams)

- Go: add `/api/docs/{group}/{repo}/{path}` passthrough that serves markdown files from the repo's doc path.
- Go: add `/api/docs/{group}/{path}?include=hovercards` variant for pre-resolved entity cards on page load (0.5 day effort).
- Go: add `/ws/events` WebSocket for live re-index push.
- Frontend: DocsSidebar tree with collapse state, DocsTOC with scroll-spy, DocsRenderer with `react-markdown` + remark-gfm + prism-react-renderer + lazy-loaded mermaid.
- Frontend: `<CodeSymbolLink>` deep-links to Surface 1 + `<EntityHovercard>` on hover.
- Frontend: `<PatternCallout>` and `<FlowDiagram>` rendering for archigraph-specific callout blocks.
- Frontend: dark mode toggle with CSS variables, theme-aware prism + mermaid.
- Deliverable: browse all docs with sidebar + TOC, search returns results in < 200 ms, symbol click opens entity in graph with details, pattern references and flow diagrams render inline.

### Phase 6b — Admin UI (DASH-4 extension)
**Effort: 1-2 days** (DASH-4 partially closed; just needs UI shell over existing admin REST endpoints)

- Group management, repo registration, rebuild trigger, watcher status.
- Reuses shared components. No new hooks beyond `useRegistry` mutations.

**Total rough estimate: 21-29 developer-days** for a single-track build (up from 19-27; Phase 3 grew by 1-2 days due to path-grouping aggregator and pagination scope, and Phase 6 grew by 1 day due to archigraph-specific features: entity hovercards, pattern callouts, and flow diagrams). Phases 3 and 5 can be parallelized by two agents after Phase 1 completes.

---

## Section 7 — Risks and Open Decisions

### Risk 1: 3D performance ceiling

The DASH-2 spec targets ≥30 fps on ~6 000 entities. The `3d-force-graph` library uses WebGL instanced meshes and frustum culling, and its maintainer's published benchmarks show smooth rendering to roughly 10 000 nodes on mid-range hardware. Beyond that, frame rate degrades sharply without mitigation.

**Resolution: fully specified in Section 2.5.** The three-tier LoD strategy (zoom-out centroids / mid god-nodes / zoom-in full expansion), hard 20k render cap, always-visible selection override, edge culling rule, `useGraphLoD` hook contract, and backend `lod=` query param are all concrete decisions — not placeholders. The LOD mode is **required** before Phase 2 ships. The community-centroid aggregator (new Go code, 1.5 days) must be in Phase 1 blockers.

### Risk 2: Auto-name quality (Layer-1 only, #426 Layer-2 open)

Layer-1 TF-IDF names (shipped in #425) are present in `communities[].auto_name`. Layer-2 agent-resolved names (#426) are not yet emitted.

**Recommendation:** Ship the dashboard with Layer-1 names as the display label. When `agent_name` is present (after #426 ships), prefer it. The `useCommunityColors` and `<CommunityLegend>` components should already read `agent_name ?? auto_name` to be forward-compatible. Do NOT block Phase 2 on #426.

### Risk 3: WebSocket live-update debounce

The file watcher fires on every file save during active development. At high-frequency edit sessions, each save could trigger a graph re-index that pushes a `/ws/events` message, which would cause `useWatcherEvents` to invalidate all React Query cache keys simultaneously — resulting in cascading refetches.

**Recommendation:** The Go WebSocket handler must debounce push events with a 2-second trailing edge delay per group. The frontend `useWatcherEvents` hook should apply an additional 1-second debounce before calling `queryClient.invalidateQueries`. Combined, this prevents UI churn during fast edits.

### Risk 4: Auth surface in the UI

DASH-1 ships token-based auth gated in `internal/dashboard/server.go`. The token is configured in `~/.archigraph/dashboard.json` and checked via `Authorization: Bearer <token>`.

**Recommendation:** Surface auth in the UI only when `dashboard.json` sets `auth.enabled=true`. The SPA should detect a 401 response on the first `/api/dashboard/init` call and render a simple token-entry modal. Store the token in `sessionStorage` (not `localStorage`) to expire on tab close. Do NOT build a full auth flow — this is a localhost tool.

### Risk 5: Cross-repo phantom edge default visibility

Phantom edges (cross_repo=true, from `internal/links/phantom_edges.go` pass #769) connect entities across repos via HTTP / Kafka / WebSocket links. If shown by default in the graph viewer alongside intra-repo edges, the visual result is confusing — the user sees edges pointing to nodes in a different lane without spatial context.

**Recommendation:** Phantom edges are ON by default in the Process Flow Explorer (they are the purpose of the cross-lane view). In the Graph Viewer they are OFF by default but togglable via a dedicated `<EdgeKindFilters>` chip labeled "Cross-repo". `useEdgeKindFilters()` initializes with cross-repo edges excluded.

### Risk 6: Dynamic response shapes in API explorer

Most real HTTP handlers have `dynamic_response=true` (shape was inferred at runtime or not inferable statically). The `ResponseShapeGrid` will often be empty or sparse.

**Recommendation:** When `dynamic_response=true` and `response_keys` is empty, render a `<DynamicResponseNotice>` component that explains why and links to the repair workflow. When `response_keys` is populated despite `dynamic_response=true`, show keys with a "partial — inferred" label. This surfaces a genuine data-quality signal to the developer rather than hiding it.

---

## Section 8 — Backend Gaps That Block UI Development

The following Go-side changes are required. None of these are already shipped in DASH-1.

### BLOCKER — Phase 1

| Gap | Effort | Notes |
|---|---|---|
| REST adapter layer — surface-shaped endpoints (`/api/graph/{group}`, `/api/graph/{group}/entity/{id}`, `/api/flows/{group}`, `/api/flows/{group}/{processId}`, `/api/topology/{group}`, `/api/search/{group}`, `/api/patterns/{group}`, `/api/repairs/{group}`, `/api/source`, `/api/findings`, `/api/enrichments`, `/api/cross-links`) | 2.5 days | Each endpoint calls 2–5 MCP handler functions and reshapes output for the consuming surface. HTTP param parsing + JSON marshaling + output shaping. The MCP handler signatures return `*mcpapi.CallToolResult`; the REST layer unwraps and merges. Larger than the original thin-wrapper estimate because of shaping logic. |
| `/api/dashboard/init` aggregate endpoint | 0.5 day | Calls `handleListRegistry` + `handleListCommunities` + `handleGraphStats` in parallel and merges into one response. Eliminates 3 sequential waterfalls on first paint. |
| **Community-centroid aggregator** (backing `GET /api/graph/{group}?lod=centroids\|mid`) | 1.5 days | **New Go code — no existing MCP analog.** Computes PageRank-weighted centroid positions, builds centroid summary shape per Section 2.5. Required before Phase 2 (Graph Viewer) ships. |
| **Path-grouping aggregator** (backing `GET /api/paths/{group}`) | 1.5 days | **New Go code — no existing MCP analog.** Groups 1 212 entities into 648 path rows, deduplicates DRF ViewSet artifacts, builds `PathTreeNode` prefix tree server-side, returns paginated response. Required before Phase 3 (API Explorer) ships. |
| `/api/groups/{group}/topics` | 0.5 day | Filters entities by kind `MessageTopic` + `Queue` + relationship traversal for producers/consumers. Backing store for `/api/topology/{group}`. |
| `/api/groups/{group}/god-nodes` | 0.5 day | Already partially exposed in DASH-1 issue spec. Needs endpoint wired. |
| `/api/groups/{group}/links` | 0.5 day | Serves `<group>-links.json` verbatim. |
| `/api/groups/{group}/communities` | 0.5 day | Deserializes communities from graph.json. |

### BLOCKER — Phase 5 (Broker Topology)

| Gap | Effort | Notes |
|---|---|---|
| `/api/groups/{group}/scheduled-jobs` | 0.5 day | Filters entities where `properties.schedule` is set. |

### BLOCKER — Phase 6 (Docs Portal)

| Gap | Effort | Notes |
|---|---|---|
| `/api/docs/{group}/{repo}/{path}` passthrough | 0.5 day | Serves markdown files from registered repo doc paths. Path traversal check required. |
| `/api/docs/{group}/{path}?include=hovercards` variant | 0.5 day | When `include=hovercards` is set, pre-resolve all backticked code symbols found in the markdown to entity card payloads (kind, source file, start line, top 3 outbound edges). Returns base response plus optional `hovercards` map. Avoids N round-trips on page load. |
| `/ws/events` WebSocket push | 1.5 days | New HTTP upgrade handler. Connects to the existing watcher/rebuild event bus (or polls mtime on `.archigraph/` dirs). Debounce 2 s. Sends `{ type: "reindex", group, repo, timestamp }` events. |

### DEFERRABLE — Phase 6b (Admin UI)

| Gap | Effort | Notes |
|---|---|---|
| `POST /api/admin/groups/{group}/rebuild` | 0.5 day | Calls existing rebuild logic. |
| `DELETE /api/admin/groups/{group}/repos/{slug}` | 0.5 day | Calls existing unregister logic. |
| `POST /api/admin/groups/{group}/repos/{slug}/reindex` | 0.5 day | Triggers single-repo re-index. |

### DEFERRABLE — does not block any phase

| Gap | Effort | Notes |
|---|---|---|
| `name_community` enrichment candidate emission (#426) | Tracked separately | Layer-2 community names. Dashboard is forward-compatible via `agent_name ?? auto_name` fallback. |

### Summary of Phase 1 blocker effort

~7–9 developer-days of Go work must complete before the frontend Phase 1 skeleton can be fully validated against real data (up from 6.5). The two new aggregators — community-centroid and path-grouping — account for 3 days of the increase. The surface-shaped REST adapter layer adds ~0.5 day over the original thin-wrapper estimate. Payoff: Phase 2–6 frontend hooks each call one cohesive endpoint instead of assembling 3–4 parallel responses.

The frontend skeleton itself can start immediately against mock responses.

---

## Appendix A — Dependency Graph Between Phases

```
Phase 1 (skeleton + REST adapters)
    │
    ├──► Phase 2 (graph viewer)
    │       └──► Phase 4 (flow explorer)  [reuses graph canvas infra]
    │
    ├──► Phase 3 (API explorer)            [independent after Phase 1]
    │
    ├──► Phase 5 (broker topology)         [independent after Phase 1 + /topics endpoint]
    │
    └──► Phase 6 (docs portal)             [independent after Phase 1 + /ws/events]

Phase 6b (admin UI extension) — independent after Phase 1; can run parallel to any phase.
```

Phases 3, 5, and 6 can be parallelized by separate agents after Phase 1 completes. Phase 4 should follow Phase 2 since it reuses `<GraphCanvas2D>` and `useGraphCamera` for the step-click zoom-to-entity feature.

---

## Appendix B — Third-Party Dependencies (frontend)

| Package | Purpose | Notes |
|---|---|---|
| `3d-force-graph` | Surface 1 3D canvas | Vasturiano; WebGL + three.js; battle-tested at 5k–10k nodes |
| `d3-force` | Surface 3 topology layout | Lighter than full d3; no DOM manipulation needed |
| `react-router-dom` v6 | Routing | createBrowserRouter + nested routes |
| `@tanstack/react-query` v5 | Server state | React Query v5; staleTime 30s default |
| `zustand` | Cross-component UI state | Camera + hover target only; single store |
| `react-markdown` + `remark-gfm` + `rehype-highlight` + `rehype-mermaid` | Docs rendering | Standard GFM + code + diagrams |
| `lunr` | Client-side full-text search for Docs portal | ~30 kB gzipped; adequate for < 50k doc lines |
| `@radix-ui/react-dialog` | Sheet / modal | Accessible; headless |
| `@radix-ui/react-tooltip` | Tooltip | Accessible; headless |
| `lucide-react` | Icons | Tree-shakeable; covers most entity kinds |
| `highlight.js` | Source snippet syntax highlighting | Lazy-loaded per language |
| `mermaid` | Mermaid diagram rendering in docs | Lazy-loaded; isolated in `<MermaidDiagram>` |
| `react-virtual` (TanStack Virtual) | Virtualized endpoint table | Required for > 500 endpoint rows |
| `tygo` (Go CLI, dev only) | Go → TypeScript type generation | Runs in CI on `kinds.go` → emits `EntityKind` + `RelationshipKind` unions |

Tailwind CSS for styling. No component library with pre-built opinionated UI (MUI, Ant Design, etc.) — the LEGO mindset requires composing Radix primitives with Tailwind classes.

---

*Plan produced by TX-20. All five surfaces covered. Phase 1 blockers identified with effort estimates. Architectural risks enumerated with concrete recommendations. No production code written.*
