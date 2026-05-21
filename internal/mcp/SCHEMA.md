# archigraph MCP — Tool & Schema Reference

Canonical contract for the archigraph MCP server's tool surface, request/response
shapes, and the entity / relationship vocabulary it exposes.

This document is referenced by ADR-0002 (clean-room MCP server in Go) as the
public contract for all tools the server registers. It is the source of truth
for clients (Claude Code, Windsurf, etc.) and tracks the implementation in
`internal/mcp/server.go` and `internal/mcp/tools.go`.

> **Source of truth: `internal/mcp/server.go` `AddTool` calls — keep this file
> in sync whenever tools are added, renamed, or removed.**

---

## Overview

- **Server name (as advertised to MCP clients):** `archigraph`
- **Transport:** stdio
- **Process model:** one server per machine, multiple registered groups, lazy
  mtime-driven reload before every tool call. See ADR-0004.
- **Tool count:** 15, all prefixed `archigraph_*` to avoid client-side
  collisions when other MCP servers are installed alongside (Refs #62).
  (19 tools prior to #668; 4 saved by 3 action-dispatch bundles.)
- **State:** in-memory `Document`s loaded from per-repo `.archigraph/graph.json`
  files; no database. See ADR-0006.
- **Routing:** every tool that touches graph data resolves a group via the
  `group` arg → CWD marker → singleton fallback cascade. See ADR-0008.
- **Cross-repo IDs:** prefixed `<repo>::<localId>` when the call spans multiple
  repos, bare `<localId>` when the call is single-repo-scoped. See ADR-0009.
- **No backwards compat for old names:** ADR-0017 (no-backcompat guarantee).
  Agents using pre-#668 tool names will receive a clear "tool not found" error.

### Stability policy

The tool surface evolves additively. New tools and new optional arguments may
land in any minor release. **Removing a tool, removing/renaming an argument,
or changing the meaning of an existing argument** requires a major version
bump (and a deprecation warning lap in the prior minor).

### Environment variables

| Variable | Effect |
|----------|--------|
| `ARCHIGRAPH_MCP_DEBUG` | `0` silent (default), `1` print per-tool summary on shutdown, `2` per-call telemetry. Read by `cmd/archigraph/mcp.go`. |
| `ARCHIGRAPH_VERBOSE` | When `1`, the indexer (`archigraph index`) prints per-language relationship breakdowns. Indexer-side; the MCP server itself does not read this. |

The registry path defaults to `~/.archigraph/registry.json` and can be
overridden via the `--registry` CLI flag.

---

## Tools

All tools are prefixed `archigraph_`. Common arguments are documented once
below; per-tool tables omit them unless the semantics differ.

### Common arguments

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `group` | string | (resolved) | Explicit group override. Skips CWD inference. |
| `cwd` | string | (resolved) | Caller working directory; if omitted, the server falls back to the configured CWD on the process. |
| `repo_filter` | string[] | `[]` | Repos to scope to. `[]` means every loaded repo in the resolved group. `["*"]` is treated as "all". |

### Tool index

| Tool | One-line description |
|------|----------------------|
| [`archigraph_whoami`](#archigraph_whoami) | Return the inferred group + repo for the caller session. |
| [`archigraph_find`](#archigraph_find) | BM25-ranked graph query, optionally BFS-expanded. |
| [`archigraph_inspect`](#archigraph_inspect) | Look up an entity by id, qualified name, or label. |
| [`archigraph_expand`](#archigraph_expand) | Return neighbors of a node out to a given depth. |
| [`archigraph_trace`](#archigraph_trace) | Confidence-weighted shortest path between two nodes. |
| [`archigraph_traces`](#archigraph_traces) | Process-flow traces (action: list\|get\|follow). |
| [`archigraph_clusters`](#archigraph_clusters) | List Louvain communities across the loaded graphs. |
| [`archigraph_stats`](#archigraph_stats) | Corpus-level metrics for the resolved group. |
| [`archigraph_enrichments`](#archigraph_enrichments) | Manage enrichment candidates (action: list\|submit\|reject). |
| [`archigraph_cross_links`](#archigraph_cross_links) | Manage cross-repo link candidates (action: list\|accept\|reject). |
| [`archigraph_repairs`](#archigraph_repairs) | Manage residual-edge repair queue (action: list\|submit). |
| [`archigraph_save_finding`](#archigraph_save_finding) | Persist a Q/A pair to the group's memory directory. |
| [`archigraph_list_findings`](#archigraph_list_findings) | List previously saved findings, optionally filtered. |
| [`archigraph_get_source`](#archigraph_get_source) | Return source-file snippet for a node from disk. |
| [`archigraph_recent_activity`](#archigraph_recent_activity) | Entities whose source files were modified after a given time. |
| [`archigraph_get_telemetry`](#archigraph_get_telemetry) | Server uptime, per-tool counters, reload counts. |
| [`archigraph_endpoint_definitions`](#archigraph_endpoint_definitions) | List HTTP endpoint handler/route definitions. |
| [`archigraph_endpoint_calls`](#archigraph_endpoint_calls) | List HTTP endpoint call-sites with orphan detection. |
| [`archigraph_endpoint_stats`](#archigraph_endpoint_stats) | Count http\_endpoint\_definition/call/legacy entities + orphan callers. |

---

### `archigraph_whoami`

Return the inferred archigraph group + repo for the caller session. Useful as a
self-orientation call when an agent is uncertain which group is in scope. See
ADR-0008 for the resolution cascade.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `cwd` | string | no | (server) | Optional caller working directory. |
| `group` | string | no | — | Optional explicit group override. |

**Output** — JSON object:

```json
{
  "group": "example-group",
  "repo": "mobile-app",
  "source": "cwd-marker",
  "registry_path": "/Users/me/.archigraph/registry.json"
}
```

`source` is one of `explicit`, `cwd-marker`, `singleton`, `none`. On failure
the call still returns 200 with `error` populated.

---

### `archigraph_find`

BM25-ranked graph query across every repo in scope, optionally BFS-expanded
from each top hit. The default rendering is compact text optimised for an LLM
context budget; pass `full=true` for raw JSON.

Previously named `archigraph_search` (renamed in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `question` | string | yes | — | Natural-language query. |
| `mode` | string | no | `bfs` | Traversal mode: `bfs` \| `dfs` \| `none`. |
| `depth` | number | no | `3` | BFS depth from each match. |
| `token_budget` | number | no | `800` | Max approximate tokens in compact output. |
| `context_filter` | string[] | no | `[]` | Edge-kind filter (see [Relationship Types](#relationship-types)). |
| `repo_filter` | string[] | no | `[]` | Repo names to scope. `["*"]` requests a full dump. |
| `full` | boolean | no | `false` | Return raw JSON instead of compact text. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — text (default) or JSON when `full=true`:

```json
{
  "matches": [
    {
      "id": "mobile-app::a1b2c3d4e5f60718",
      "label": "OrderViewSet",
      "repo": "mobile-app",
      "score": 12.31,
      "source_file": "core/views/order.py",
      "start_line": 42,
      "kind": "Component"
    }
  ]
}
```

**Notes**

- "Always-1" rule: if no BM25 hits matched but repos contain entities, the
  highest-PageRank entity is returned as a single-result fallback.
- Smart scoping: when no `repo_filter` is set and the group has more than
  one repo, the compact renderer returns a per-repo top-3 summary.
- IDs are prefixed `<repo>::<localId>` when the result spans multiple repos
  (ADR-0009).
- `kind` is the SCOPE-stripped form (`Component` not `SCOPE.Component`); see
  ADR-0003 and [Entity Kinds](#entity-kinds).

---

### `archigraph_inspect`

Look up an entity by ID, prefixed cross-repo ID, qualified name, or label.

Previously named `archigraph_describe` (renamed in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `label_or_id` | string | yes | — | Entity ID, `<repo>::<localId>`, qualified name (case-insensitive), or label (case-insensitive). |
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON object:

```json
{
  "id": "a1b2c3d4e5f60718",
  "label": "OrderViewSet",
  "qualified_name": "core.views.order.OrderViewSet",
  "kind": "Component",
  "source_file": "core/views/order.py",
  "start_line": 42,
  "end_line": 130,
  "language": "python",
  "repo": "mobile-app",
  "pagerank": 0.0142,
  "community_id": 7,
  "properties": { "framework": "drf" }
}
```

If the call resolves to a single repo, `id` is local; otherwise it is prefixed.
Returns a tool error when no entity matches.

The response also carries a `findings` array — every saved finding (see
`archigraph_save_finding`) whose `nodes` list references this entity (in either
local or `<repo>::<localId>` form). Empty array when no findings reference the
entity. See [`archigraph_list_findings`](#archigraph_list_findings) for explicit
retrieval. (Refs #59.)

---

### `archigraph_expand`

Return BFS neighbours of a node out to a given depth, plus any cross-repo
overlay edges that originate from that node.

Previously named `archigraph_related` (renamed in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node` | string | yes | — | Entity ID, prefixed cross-repo ID, qualified name, or label. |
| `depth` | number | no | `2` | BFS depth. |
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array of neighbour records:

```json
[
  {
    "id": "mobile-app::deadbeef00112233",
    "label": "OrderSerializer",
    "depth": 1,
    "source_file": "core/serializers/order.py",
    "start_line": 11
  },
  {
    "id": "api-backend::cafef00d44556677",
    "label": "OrderModel",
    "depth": 1,
    "cross_repo": true,
    "kind": "USES"
  }
]
```

Cross-repo overlay entries carry `cross_repo: true` and the link `kind`.

---

### `archigraph_trace`

Confidence-weighted shortest path between two nodes (Dijkstra over
`-log(confidence)` weights). Aware of cross-repo overlay links from
ADR-0007 / ADR-0009.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `source` | string | yes | — | Source node (ID, prefixed ID, qname, or label). |
| `target` | string | yes | — | Target node (same forms as `source`). |
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON object:

```json
{
  "path": ["mobile-app::aaaa", "mobile-app::bbbb", "api-backend::cccc"],
  "edges": [
    {"kind": "CALLS"},
    {"kind": "PUBLISHES_TO"}
  ],
  "weakest_link_confidence": 0.7,
  "length": 2,
  "crosses_repos": true,
  "found": true
}
```

Intra-repo edges are weighted at confidence `0.95`; cross-repo overlay edges
use the link's recorded confidence (default `0.7` if unset). Returns
`{"found": false, "path": null}` on no-path.

The response also carries a `findings` array — every saved finding whose
`nodes` list references any node along the resolved `path`. (Refs #59.)

---

### `archigraph_traces`

Process-flow query surface (#724). Surfaces the `SCOPE.Process` entities
emitted by the indexer's Pass 7 BFS over the CALLS graph from
heuristically-detected entry points (route handlers, `main`, framework
lifecycle hooks). Each Process is a linearized call chain with
`STEP_IN_PROCESS` edges (step_index ordered) and an `ENTRY_POINT_OF`
edge from the entry function.

Three sub-actions selected via the required `action` argument:

- `list` — return top-ranked Processes for the resolved group, sorted
  cross-stack first then by step count. Optional `cross_stack_only=true`
  filters to chains that traverse an HTTP boundary.
- `get` — return the full step chain for one `process_id` (bare or
  `repo::local` prefixed). Steps include node id, name, kind,
  source_file, and start_line.
- `follow` — ad-hoc forward BFS from any `entry_point_id`. Useful for
  probing entities that weren't selected as pre-computed entry points.
  Honours `max_depth` (≤10) and `branching_factor` (≤4) caps.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `action` | string | yes | — | `list` \| `get` \| `follow` |
| `process_id` | string | conditional | — | (`get`) Process entity id. |
| `entry_point_id` | string | conditional | — | (`follow`) Entity id of the entry function. |
| `max_depth` | number | no | `8` | (`follow`) BFS depth cap. Clamped to ≤10. |
| `branching_factor` | number | no | `3` | (`follow`) Per-step branch cap. Clamped to ≤4. |
| `cross_stack_only` | bool | no | `false` | (`list`) Only return cross-stack Processes. |
| `limit` | number | no | `25` | (`list`) Max processes returned. |
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** (action=list) — JSON object:

```json
{
  "count": 2,
  "processes": [
    {
      "process_id": "cf-d::proc:df0cd633e7f8f7f4",
      "repo": "cf-d",
      "label": "OrdersPublicController.processOrder → Correlative",
      "entry_id": "b95e636c1955e82f",
      "entry_name": "OrdersPublicController.processOrder",
      "terminal_id": "d358909b92891554",
      "step_count": 7,
      "cross_stack": true,
      "chain_labels": ["OrdersPublicController.processOrder", "OrdersService.processOrderByEcwidNumber", "..."],
      "source_file": "src/main/java/.../OrdersPublicController.java"
    }
  ]
}
```

---

### `archigraph_clusters`

List Louvain communities pre-baked into each repo's `graph.json` (see
ADR-0005).

Previously named `archigraph_list_clusters` (renamed in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array:

```json
[
  {
    "repo": "mobile-app",
    "id": 3,
    "size": 47,
    "modularity": 0.412,
    "top_entities": ["OrderViewSet", "OrderSerializer", "OrderModel"]
  }
]
```

---

### `archigraph_stats`

Corpus-level metrics for the resolved group: per-repo entity / relationship /
community counts, plus group-level totals and any unavailable repos (with
load errors).

Previously named `archigraph_graph_stats` (renamed in #668).

**Inputs** — common args only. When `repo_filter` is supplied, totals,
the `repos` array, and `cross_repo_links` are scoped to the named repos
(a link counts if either endpoint is in the filter). `["*"]` and `[]`
both mean "every loaded repo".

**Output**

```json
{
  "entities": 12345,
  "relationships": 67890,
  "cross_repo_links": 17,
  "repos": [
    { "repo": "mobile-app", "entities": 4321, "relationships": 12000, "communities": 23 }
  ],
  "unavailable": ["legacy-tools: open .archigraph/graph.json: no such file"]
}
```

---

### `archigraph_enrichments`

Manage enrichment candidates via a single action-dispatch interface. Combines
the former `archigraph_list_enrichment_candidates`, `archigraph_submit_enrichment`,
and `archigraph_reject_enrichment` tools (bundled in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `action` | string | yes | — | `list` \| `submit` \| `reject` |
| `repo_filter` | string[] | no | `[]` | **(list)** Repos to scope. |
| `kind` | string | no | — | **(list)** Filter by candidate kind (e.g. `purpose`). |
| `limit` | number | no | `10` | **(list)** Max candidates returned. |
| `candidate_id` | string | cond. | — | **(submit\|reject)** Candidate ID. |
| `value` | string | cond. | — | **(submit)** Agent's resolution value. |
| `confidence` | number | no | `1` | **(submit)** Confidence in `[0,1]`. |
| `reason` | string | no/cond. | — | **(submit)** Optional audit note. **(reject)** Required rejection reason. |
| `group`, `cwd` | string | no | — | Common args. |

**Output (action=list)** — JSON array:

```json
[
  {
    "id": "ec-1",
    "node_id": "mobile-app::aaaa1111bbbb2222",
    "kind": "purpose",
    "hint": "Likely the auth-token serializer.",
    "repo": "mobile-app"
  }
]
```

**Output (action=submit)**

```json
{ "candidate_id": "ec-1", "decision": "accept" }
```

**Output (action=reject)**

```json
{ "candidate_id": "ec-1", "decision": "reject" }
```

---

### `archigraph_cross_links`

Manage cross-repo link candidates via a single action-dispatch interface.
Combines the former `archigraph_list_link_candidates` and
`archigraph_resolve_link_candidate` tools (bundled in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `action` | string | yes | — | `list` \| `accept` \| `reject` |
| `repo_filter` | string[] | no | `[]` | **(list)** Returns candidates whose source OR target is in these repos. |
| `channel` | string | no | — | **(list)** Filter by channel label. |
| `method` | string | no | — | **(list)** Filter by detection method. |
| `limit` | number | no | `10` | **(list)** Max candidates returned. |
| `candidate_id` | string | cond. | — | **(accept\|reject)** Candidate ID. |
| `reason` | string | no | — | **(reject)** Free-form audit string. |
| `override_target` | string | no | — | **(accept)** Override the candidate's target ID with this prefixed ID. |
| `group`, `cwd` | string | no | — | Common args. |

**Output (action=list)** — JSON array of `LinkCandidate` records (id, source,
target, kind, confidence, channel, method).

**Output (action=accept\|reject)**

```json
{ "candidate_id": "lc-abc123", "decision": "accept" }
```

---

### `archigraph_repairs`

Manage the residual-edge repair queue (ADR-0015) via a single action-dispatch
interface. Combines the former `archigraph_list_residuals` and
`archigraph_submit_repair` tools (bundled in #668).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `action` | string | yes | — | `list` \| `submit` |
| `repo_filter` | string[] | no | `[]` | **(list)** Repos to scope. |
| `limit` | number | no | `20` | **(list)** Max residuals returned. |
| `offset` | number | no | `0` | **(list)** Pagination offset. |
| `residual_id` | string | cond. | — | **(submit)** `er:<hex16>` identifier from `action=list`. |
| `resolution` | string | cond. | — | **(submit)** `bind_to_entity` \| `reclassify_as_external` \| `reclassify_as_dynamic` \| `reclassify_as_resolved` \| `abandon` |
| `target_entity_id` | string | no | — | **(submit)** Required when `resolution=bind_to_entity`. |
| `module` | string | no | — | **(submit)** Required when `resolution=reclassify_as_external`. |
| `new_target` | string | no | — | **(submit)** Required when `resolution=reclassify_as_resolved`. |
| `dynamic_reason` | string | no | — | **(submit)** Reason for dynamic dispatch classification. |
| `abandon_reason` | string | no | — | **(submit)** Reason for abandoning repair. |
| `confidence` | number | no | `0.0` | **(submit)** Agent confidence in `[0,1]`. |
| `reasoning` | string | no | — | **(submit)** Free-form agent reasoning. |
| `source` | string | no | `mcp_submit_repair` | **(submit)** Audit source tag. |
| `repo` | string | no | — | **(submit)** Optional repo override; defaults to repo that owns `residual_id`. |
| `group`, `cwd` | string | no | — | Common args. |

**Output (action=list)**

```json
{
  "residuals": [
    {
      "edge_id": "er:deadbeef00000001",
      "relation": "CALLS",
      "original_stub": "save",
      "disposition": "DispositionBugResolver",
      "from_entity": { "id": "a1", "name": "DashboardScreen", "kind": "Component" }
    }
  ],
  "total": 1,
  "offset": 0,
  "limit": 20
}
```

**Output (action=submit)**

```json
{
  "residual_id": "er:deadbeef00000001",
  "repo": "mobile-app",
  "resolution": "bind_to_entity",
  "repair_count": 1,
  "resolved_at": "2026-05-19T12:00:00Z"
}
```

---

### `archigraph_save_finding`

Persist a question/answer pair into the resolved group's memory directory as a
timestamped JSON file. The MCP does not interpret the contents; this is a
durable agent scratchpad.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `question` | string | yes | — | Caller's question. |
| `answer` | string | yes | — | Caller's answer / synthesis. |
| `type` | string | no | `note` | Free-form classifier (e.g. `note`, `decision`, `bug`). |
| `nodes` | string[] | no | `[]` | Entity IDs the finding references. |
| `repo_filter` | string[] | no | `[]` | Repos the finding references. |
| `group`, `cwd` | string | no | — | Common args. |

**Output**

```json
{ "path": "/Users/me/.archigraph/groups/example-memory/20260509T020131Z-1a2b3c4d.json" }
```

See [Save_finding semantics](#save_finding-semantics) below for full storage
layout.

---

### `archigraph_list_findings`

Read previously saved findings back. Counterpart to `archigraph_save_finding`;
makes the agent scratchpad discoverable across sessions (Refs #59).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `entity_id` | string | no | — | Filter to findings whose `nodes` reference this entity (accepts ID, prefixed ID, qualified name, or label). |
| `since` | string | no | — | RFC3339 timestamp; only findings with `saved_at >= since` are returned. |
| `limit` | number | no | `50` | Max findings to return. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array, newest-first:

```json
[
  {
    "question": "How does authentication flow from mobile to backend?",
    "answer":   "...",
    "type":     "note",
    "nodes":    ["mobile-app::aaaa", "api-backend::bbbb"],
    "saved_at": "2026-05-09T02:01:31Z",
    "path":     "/Users/me/.archigraph/groups/example-memory/20260509T020131Z-1a2b3c4d.json"
  }
]
```

Findings are read from the same memory directory `archigraph_save_finding`
writes to. Files that fail to parse as JSON are silently skipped.

---

### `archigraph_get_source`

Return the source-file snippet for a node from disk, with `context_lines`
above and below the entity's recorded `[start_line, end_line]` range.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node_id` | string | yes | — | Entity ID, prefixed ID, qname, or label. |
| `context_lines` | number | no | `20` | Lines of context above/below the entity. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — text, line-numbered:

```text
   23  class OrderViewSet(viewsets.ModelViewSet):
   24      queryset = Order.objects.all()
   25      serializer_class = OrderSerializer
```

Returns a tool error if the source file cannot be opened.

---

### `archigraph_recent_activity`

Return entities whose source files were modified after a given time, sorted
by mtime descending.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `since` | string | no | (epoch) | RFC3339 timestamp. |
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `limit` | number | no | `50` | Max rows returned. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array:

```json
[
  {
    "repo": "mobile-app",
    "id": "mobile-app::a1b2c3d4e5f60718",
    "label": "OrderViewSet",
    "file": "core/views/order.py",
    "mtime": "2026-05-08T14:31:02Z"
  }
]
```

---

### `archigraph_get_telemetry`

Server uptime, per-tool call counters, error counts, and lazy-reload counts.
Does NOT take a `group`/`cwd` — it is global to the server process.

**Inputs** — none.

**Output** — JSON object produced by `Telemetry.Snapshot()`:

```json
{
  "uptime_ms": 1234567,
  "reload_count": 12,
  "files_reloaded": 38,
  "tools": {
    "archigraph_find":    { "calls": 42, "errors": 1, "p50_ms": 8.2, "p95_ms": 31.7 },
    "archigraph_inspect": { "calls": 17, "errors": 0, "p50_ms": 1.1, "p95_ms": 2.3 }
  }
}
```

---

## Entity Kinds

Internal entity kinds use the `SCOPE.*` namespace (ADR-0003). The MCP
rendering layer **strips** the `SCOPE.` prefix when surfacing kinds to the
agent (so `kind` in tool output is `Component`, not `SCOPE.Component`). The
on-disk `graph.json` keeps the namespaced form.

| SCOPE kind | Agent-visible kind | Used for |
|------------|--------------------|----------|
| `SCOPE.Operation` | `Operation` | Functions, methods, callable units. |
| `SCOPE.Component` | `Component` | Classes, controllers, viewsets, modules. |
| `SCOPE.Class` | `Class` | Class declarations (when extractor distinguishes from generic Component). |
| `SCOPE.Function` | `Function` | Function declarations (when extractor distinguishes from generic Operation). |
| `SCOPE.Schema` | `Schema` | Type schemas, proto messages, GraphQL types, struct/record definitions. |
| `SCOPE.Variable` | `Variable` | Module-level variables, constants, config keys. |
| `SCOPE.Reference` | `Reference` | Stub references to external symbols pre-resolution. |
| `SCOPE.Pattern` | `Pattern` | Behavioural patterns (decorators, hooks-of-hooks, etc.). |
| `SCOPE.Evolution` | `Evolution` | Version / migration markers tracked across history. |
| `SCOPE.Endpoint` | `Endpoint` | HTTP endpoints, RPC methods, gRPC services. |
| `SCOPE.Route` | `Route` | Framework route declarations (URL conf, router.register). |
| `SCOPE.Service` | `Service` | Service definitions (proto, NestJS service, microservice boundary). |
| `SCOPE.View` | `View` | Framework views (Django CBV, Rails view). |
| `SCOPE.UIComponent` | `UIComponent` | UI components (React, Vue, Razor, Blazor). |
| `SCOPE.JSX` | `JSX` | JSX/TSX subtree fragments. |
| `SCOPE.Stylesheet` | `Stylesheet` | CSS/SCSS/styled-component declarations. |
| `SCOPE.Queue` | `Queue` | SQS / Pub/Sub / Kafka queue or topic resources. |
| `SCOPE.Event` | `Event` | Event-bus events, domain events. |
| `SCOPE.Datastore` | `Datastore` | Databases, tables, collections, caches. |
| `SCOPE.DataAccess` | `DataAccess` | Repository / DAO / ORM accessor units. |
| `SCOPE.ExternalAPI` | `ExternalAPI` | Calls into third-party HTTP / SDK surfaces. |
| `SCOPE.InfraResource` | `InfraResource` | IaC-defined deployed resources (S3 bucket, Lambda fn, ECS service). |
| `SCOPE.CodeBlock` | `CodeBlock` | Anonymous block / lambda / closure. |
| `SCOPE.Document` | `Document` | Markdown / RST / ADoc documents. |
| `SCOPE.Heading` | `Heading` | In-document headings (markdown extractor). |
| `SCOPE.External` | `External` | Synthesised placeholder for an external package or symbol. |
| `SCOPE.Project` | `Project` | Project-level marker entity (one per repo / project root). |
| `SCOPE.Config` | `Config` | Config files, env vars, auth/CORS/connection-pool/logging policies. |
| `SCOPE.Model` | `Model` | Domain / data model entities (Django/Rails/ActiveRecord etc.). |
| `SCOPE.ScopeUnknown` | `ScopeUnknown` | Catch-all when extractor cannot classify. |

---

## Relationship Types

Relationship `kind` is a closed enum (ADR-0003). All edges are directed
(`from_id` → `to_id`).

| Kind | Meaning |
|------|---------|
| `CALLS` | Operation invokes another Operation. |
| `IMPORTS` | File or module imports another. |
| `EXTENDS` | Class extends another class / inherits. |
| `IMPLEMENTS` | Class implements an interface / protocol. |
| `USES` | Entity references another by type or value. |
| `USES_HOOK` | Component uses a React-style hook (or analogue). |
| `CONTAINS` | Container relationship (file → entity, class → method). |
| `DEPENDS_ON` | Coarse dependency (package → package, module → module). |
| `REFERENCES` | Symbolic reference, weaker than `USES` (e.g. doc reference). |
| `ROUTES_TO` | Router/route declaration points at a handler (DRF router, Spring `@GetMapping`, Express route). |
| `SERVES` | Endpoint serves a route, view, or resource. |
| `PUBLISHES_TO` | Producer writes to a queue / topic / event bus. |
| `RENDERS` | UI Component renders another Component (React / Vue / JSX subtree). |
| `RETURNS` | Operation/Function returns a Schema or typed value. |
| `TESTS` | Test entity exercises another entity. |

The full list of edge kinds the agent may pass to `archigraph_find`'s
`context_filter` is the union of the above plus any `SCOPE.*`-prefixed
forms emitted by extractors that haven't been stripped — the filter
matches both forms.

---

## Disposition tags

Every resolver-touched relationship endpoint is classified into exactly one
disposition. Dispositions are an *internal* signal surfaced through the
indexer's verbose log (`ARCHIGRAPH_VERBOSE=1`) and through enrichment
candidate generation; the MCP does not (yet) expose them as a first-class
filter.

| Disposition | Meaning |
|-------------|---------|
| `resolved` | Stub was rewritten to a 16-char graph entity ID. Healthy. |
| `external-known` | Endpoint points at an `ext:<pkg>` placeholder and the package is on the static external-package allowlist (django, react, fmt, …). |
| `external-unknown` | Endpoint points at `ext:<pkg>` but the package is NOT on the allowlist. Likely an uncatalogued real external dep. |
| `dynamic` | Stub matches a per-language dynamic-dispatch pattern (reflection, dynamic import, env-driven names, template-built strings). Not a bug; intrinsically static-unresolvable. |
| `bug-extractor` | Stub of form `Kind:Name` where the graph has zero entities with that Name. An extractor SHOULD have emitted an entity but didn't. |
| `bug-resolver` | Stub points at a Name that DOES exist in the graph (potentially under different kinds), but the resolver couldn't disambiguate. |
| `unclassified` | Catch-all. Should be `0` in production runs; non-zero values warrant investigation. |

The bug-rate metric is `(bug-extractor + bug-resolver) / total endpoints`.

---

## Cross-repo ID format

Per ADR-0009, archigraph uses two-layer ID namespacing:

- **Index layer (per repo):** `entity.id` is a 16-char hex hash local to the
  repo. IDs are NOT prefixed in `graph.json`. Each entity carries a `repo`
  attribute set from `--repo-tag`.
- **MCP composition layer:** when a tool response spans multiple repos, IDs
  are prefixed `<repo>::<localId>`. When the call is single-repo-scoped
  (a single-repo `repo_filter`, or a single-repo group), IDs are returned
  bare.
- **Cross-repo links / candidates files:** ALWAYS use the prefixed form on
  both endpoints, so files are self-describing.

Agents that round-trip IDs between calls SHOULD preserve the prefix when one
is present. Stripping is safe only when the receiving call's scope is
unambiguously single-repo.

---

## `archigraph_save_finding` semantics

`archigraph_save_finding` writes a JSON document to the resolved group's
memory directory:

- **Default location:** `~/.archigraph/groups/<group>-memory/`. Override per
  group via `memory_dir` in `registry.json`.
- **Filename:** `<UTC RFC3339-compact>-<sha256(question+answer)[0:8]>.json`,
  e.g. `20260509T020131Z-1a2b3c4d.json`. The hash provides idempotency for
  identical Q/A pairs called minutes apart.
- **Body shape:**

```json
{
  "question":     "How does authentication flow from mobile to backend?",
  "answer":       "...",
  "type":         "note",
  "nodes":        ["mobile-app::aaaa", "api-backend::bbbb"],
  "repo_filter":  ["mobile-app", "api-backend"],
  "saved_at":     "2026-05-09T02:01:31Z"
}
```

- **Reading API:** `archigraph_list_findings` reads them back, optionally
  filtered by `entity_id` or `since`. `archigraph_inspect` and
  `archigraph_trace` also auto-attach matching findings under a `findings`
  field of their response (Refs #59). Ingestion back into the graph proper
  is still out of scope for v1.0.
- **No deduplication beyond the filename hash:** repeated calls with the
  same Q/A in the same UTC second collapse to one file; otherwise a fresh
  file is written.

---

### `archigraph_endpoint_definitions`

> Added in #1220 (Sub-D of paths v2 epic #1115).

List HTTP endpoint handler/route definitions. Returns entities of kind
`http_endpoint_definition` as well as the legacy `http_endpoint` kind when the
graph has not yet been re-indexed with Sub-A (#1217). Client-synthesis entities
(call-side) are excluded.

**Backward-compatibility note:** the legacy `http_endpoint` kind remains valid
in all kind-filter parameters across the server and will transparently expand to
both `http_endpoint_definition` and `http_endpoint_call`.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | [] | Repos to scope. |
| `limit` | number | no | 200 | Max results. |
| `group` / `cwd` | string | no | — | Standard routing args. |

**Output** — JSON object:

```json
{
  "definitions": [
    {
      "entity_id": "repo1::ep1",
      "name": "POST /api/v1/orders",
      "kind": "http_endpoint_definition",
      "repo": "repo1",
      "source_file": "routes/orders.go",
      "start_line": 42,
      "method": "POST",
      "path": "/api/v1/orders"
    }
  ],
  "count": 1,
  "total": 1,
  "truncated": false,
  "note": "http_endpoint kind is deprecated; prefer http_endpoint_definition for handler/route entities."
}
```

---

### `archigraph_endpoint_calls`

> Added in #1220 (Sub-D of paths v2 epic #1115).

List HTTP endpoint call-sites (consumer side of FETCHES edges). Returns entities
of kind `http_endpoint_call` plus legacy `http_endpoint` entities whose
`pattern_type` is `http_endpoint_client_synthesis`. Call-sites with no matching
definition in the group receive an `orphan_hint` field containing a reasoning
note.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | [] | Repos to scope. |
| `orphan_only` | boolean | no | false | When true, return only call-sites with no matching definition. |
| `limit` | number | no | 200 | Max results. |
| `group` / `cwd` | string | no | — | Standard routing args. |

**Output** — JSON object:

```json
{
  "calls": [
    {
      "entity_id": "repo1::call1",
      "name": "fetchOrders",
      "kind": "http_endpoint_call",
      "repo": "repo1",
      "source_file": "services/orders.go",
      "start_line": 99,
      "method": "POST",
      "path": "/api/v1/orders",
      "matched_definition": "repo1::ep1",
      "orphan_hint": ""
    }
  ],
  "count": 1,
  "total": 1,
  "truncated": false,
  "note": "http_endpoint kind is deprecated; prefer http_endpoint_call for consumer-side call-site entities."
}
```

When `orphan_hint` is non-empty it reads: `"this call to /some/path has no matching definition — see orphan_callers"`.

---

### `archigraph_endpoint_stats`

> Added in #1220 (Sub-D of paths v2 epic #1115).

Return a count breakdown of all HTTP-endpoint kind variants per repo, plus the
number of orphan call-sites (FETCHES edges whose target is not a definition
entity). Use to assess Sub-A (#1217) migration progress.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | [] | Repos to scope. |
| `group` / `cwd` | string | no | — | Standard routing args. |

**Output** — JSON object:

```json
{
  "totals": {
    "definitions":  12,
    "calls":         8,
    "legacy_kind":   0,
    "orphan_calls":  2
  },
  "per_repo": [
    {
      "repo": "orders-service",
      "definitions": 7,
      "calls": 5,
      "legacy_kind": 0,
      "orphan_calls": 1
    }
  ],
  "migrated": true,
  "note": ""
}
```

`migrated: true` means no legacy `http_endpoint` entities remain — all have been
split into `http_endpoint_definition` / `http_endpoint_call` by Sub-A (#1217).
When `migrated: false`, `note` contains a migration reminder.

---

## See also

- ADR-0001 — Go-native single-binary distribution
- ADR-0002 — Clean-room MCP server in Go
- ADR-0003 — SCOPE entity taxonomy
- ADR-0004 — Single MCP process per machine
- ADR-0005 — Pre-baked graph attributes during indexing
- ADR-0006 — In-memory JSON persistence (no graph database)
- ADR-0007 — Doc-as-bridge for cross-repo and dynamic connections
- ADR-0008 — Caller-CWD aware routing for multi-group setups
- ADR-0009 — Cross-repo ID namespacing (`<repo>::<localId>`)
- ADR-0015 — Residual-edge repair flow (now `archigraph_repairs`)
- ADR-0017 — No backwards compatibility guarantee for tool renames
- Source — `internal/mcp/server.go`, `internal/mcp/tools.go`
- Issues — #52 (initial rename), #62 (`archigraph_*` prefix), #57 (this doc), #661 (SCHEMA.md stale), #668 (tool rename + bundle)
