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
- **Tool count:** 29 (as of this PR), all prefixed `archigraph_*` to avoid client-side
  collisions when other MCP servers are installed alongside (Refs #62).
  Prior history: 19 tools → #668 bundled 3 action-dispatch tools (saved 4) → 39 tools
  after #1202/#1220/#1252 additions → #1281 merged 9 tools into 4 bundles → 32 tools
  → dropped 4 dashboard-only tools → 28 tools → #1314 added auth_coverage → 29 tools
  → #1384 (epic #1380) added `archigraph_module_analysis` (action-dispatched
  cycles|centrality|all over the aggregated module graph).
- **Handshake token ceiling:** 3,100 (bumped from 3,000 in #1384 to seat
  `archigraph_module_analysis`; current measurement 3,085 tokens).
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
| `MCP_WIRE_FORMAT` | `toon` (default) or `json`. Controls whether list-of-record responses use TOON encoding or fall back to minified JSON arrays in the `items` field. See [Wire Format](#wire-format) below. |

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

### #1281 deprecation notice

The following tools were **removed** in #1281 and merged into action-dispatch bundles.
Agents using these names will receive a "tool not found" error — update to the new bundled form.

| Removed tool | Replacement |
|---|---|
| `archigraph_topology_orphan_publishers` | `archigraph_topology(action=orphan_publishers)` |
| `archigraph_topology_orphan_subscribers` | `archigraph_topology(action=orphan_subscribers)` |
| `archigraph_topology_topic_detail` | `archigraph_topology(action=topic_detail, topic_id=…)` |
| `archigraph_flow_dead_ends` | `archigraph_flows(action=dead_ends)` |
| `archigraph_flow_truncated` | `archigraph_flows(action=truncated)` |
| `archigraph_flow_detail` | `archigraph_flows(action=detail, process_id=…)` |
| `archigraph_patterns_list` | `archigraph_graph_patterns(action=list)` |
| `archigraph_patterns_get` | `archigraph_graph_patterns(action=get, pattern_id=…)` |
| `archigraph_endpoint_definitions` | `archigraph_endpoints(action=definitions)` |
| `archigraph_endpoint_calls` | `archigraph_endpoints(action=calls)` |
| `archigraph_endpoint_stats` | `archigraph_endpoints(action=stats)` |

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
| ~~`archigraph_get_telemetry`~~ | Dropped — HTTP-only. |
| [`archigraph_patterns`](#archigraph_patterns) | Agent-learned pattern store (action: query\|record\|refine\|apply\|reject\|promote\|get). |
| ~~`archigraph_get_next_enrichment_task`~~ | Dropped — use `enrichments(action=list,limit=1)`. |
| [`archigraph_topology`](#archigraph_topology) | Message-channel topology (action: orphan\_publishers\|orphan\_subscribers\|topic\_detail). |
| [`archigraph_flows`](#archigraph_flows) | Flow-process diagnostics (action: dead\_ends\|truncated\|detail). |
| ~~`archigraph_diagnostics`~~ | Dropped — HTTP-only (`/api/diagnostics`). |
| ~~`archigraph_quality_orphans`~~ | Dropped — use `archigraph_find_dead_code`. |
| [`archigraph_graph_patterns`](#archigraph_graph_patterns) | Indexer-extracted graph patterns (action: list\|get). |
| [`archigraph_search_entities`](#archigraph_search_entities) | Full-text substring search across entity names. |
| [`archigraph_get_subgraph`](#archigraph_get_subgraph) | All nodes and edges within N hops of an entity. |
| [`archigraph_find_paths`](#archigraph_find_paths) | Shortest path between two entities. |
| [`archigraph_endpoints`](#archigraph_endpoints) | HTTP endpoint surface (action: definitions\|calls\|stats). |
| [`archigraph_find_callers`](#archigraph_find_callers) | Inbound call graph up to N hops. |
| [`archigraph_find_callees`](#archigraph_find_callees) | Outbound call graph up to N hops. |
| [`archigraph_impact_radius`](#archigraph_impact_radius) | Blast-radius analysis with per-entity risk score. |
| [`archigraph_summarize_subgraph`](#archigraph_summarize_subgraph) | Markdown summary of entity call neighbourhood. |
| [`archigraph_find_dead_code`](#archigraph_find_dead_code) | Entities with 0 inbound/outbound project edges. |
| [`archigraph_auth_coverage`](#archigraph_auth_coverage) | Security audit: flag HTTP endpoints missing auth decorators/middleware. |
| [`archigraph_secrets`](#archigraph_secrets) | Security scan: detect hardcoded API keys, passwords, JWT tokens, and other credentials in source files. |
| [`archigraph_module_analysis`](#archigraph_module_analysis) | Module-level SCC + PageRank + betweenness over the aggregated module graph (action: cycles\|centrality\|all). |

---

### `archigraph_module_analysis`

Module-level graph data-science (#1384, part of epic #1380). Runs SCC,
PageRank, and betweenness over the **aggregated module graph** — the
bird's-eye-view counterpart to the entity-level tools.

The module graph is computed by collapsing every entity-level edge `A → B`
onto the pair `(module(A), module(B))`, dropping intra-module self-edges,
and accumulating per-pair weight = count. When the input document already
contains synthetic `Module` containers and `DEPENDS_ON` edges between them
(post-#1383 documents), those pre-aggregated edges are used directly
(honouring their `weight` property).

**Args:**

| Field | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `action` | string | no | `all` | `cycles` / `centrality` / `all`. |
| `repo_filter` | string\[\] | no | all | Restrict to listed repo slugs (read from args even though omitted from schema for handshake-token economy). |
| `top_n` | int | no | 10 | Top-N for centrality rankings. |
| `limit` | int | no | 50 | Max SCCs returned. |
| `min_size` | int | no | 2 | Minimum SCC size to report (cycles action only). |
| `group` | string | no | inferred | See routing rules above. |
| `cwd` | string | no | inferred | See routing rules above. |

**Returns (action=all):**

```jsonc
{
  "sccs": [                       // module-level cycles, ≥ min_size
    {
      "repo": "...",
      "id": 0,                    // deterministic per repo
      "size": 3,
      "members": ["repo::M_A", "repo::M_B", "repo::M_C"],
      "member_names": ["mod_a", "mod_b", "mod_c"],
      "edges": [ { "from_module": "repo::M_A", "to_module": "repo::M_B", "weight": 12 } ]
    }
  ],
  "top_pagerank": [               // top-N modules by PageRank, across repos
    { "repo": "...", "module_id": "repo::M_X", "module_name": "...",
      "pagerank": 0.0421, "betweenness": 0.0117,
      "in_degree": 8, "out_degree": 2, "in_cycle": false }
  ],
  "top_betweenness": [ /* same shape, sorted by betweenness */ ],
  "summaries": [                  // per-repo aggregate counts
    { "repo": "...", "num_modules": N, "num_module_edges": N,
      "num_sccs": N, "largest_scc_size": N, "modules_in_cycle": N }
  ]
}
```

**Returns (action=cycles):** `{ "cycles": [...], "count": N, "total": N, "truncated": bool, "repos_scanned": N }`.

**Returns (action=centrality):** `{ "repos": [ { "repo": "...", "top_pagerank": [...], "top_betweenness": [...], ... } ], "count": N }`.

Scores are rounded to 4 decimal places (same determinism policy as entity-level
algorithms, see issue #481). The synthetic `_external` bucket (entities that
lack a `module` property) is excluded from the module graph — including it
would pollute SCC and centrality with noise.

The HTTP equivalent is `GET /api/v2/groups/{group}/modules/analysis` on the
dashboard server (same payload shape, v2 envelope).

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
| `include_noise` | boolean | no | `false` | Keep synthetic nodes (file/module container components, inferred class-hierarchy shadows, raw `SCOPE.Pattern` nodes, built-in `Process` nodes). Excluded by default (#1614). |
| `group`, `cwd` | string | no | — | Common args. |

By default results are **de-noised and re-ranked** (#1614): file/module container
components, inferred class-hierarchy shadows, raw Pattern nodes and array-built-in
Process nodes are dropped, and real **lined** entities (`start_line > 0`) rank above
lineless route/resource entities — both above any retained synthetic node. The same
filtering applies to the `full=true` JSON dump. Set `include_noise=true` to recover
the unfiltered list.

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

**Span guard (#1614):** when `end_line <= start_line` or either is `0` (common
for synthetic / shadow / route entities), the span is clamped to a fixed
fallback window (`start_line + 60`). A **hard cap of 200 emitted lines** is then
applied unconditionally, so `get_source` can never dump an entire file no matter
what span the entity records.

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
| `limit` | number | no | 200 | Max results in this page (0 = no cap). |
| `offset` | number | no | 0 | Pagination offset (#1614) — page through every route with `offset`+`limit` to enumerate all paths. |
| `group` / `cwd` | string | no | — | Standard routing args. |

The response carries `total`, `offset`, and `truncated` so a caller can answer
"which endpoints exist" by paging until `truncated` is `false`.

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
| `limit` | number | no | 200 | Max results in this page (0 = no cap). |
| `offset` | number | no | 0 | Pagination offset (#1614) — page with `offset`+`limit` to enumerate every call-site path. |
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

---

### `archigraph_auth_coverage`

Security audit tool (#1314). Walk every `http_endpoint_definition` entity in the group and
determine whether it is covered by an auth decorator, middleware, or guard.

**Detection signals (applied in priority order)**

1. Entity property `auth_decorator` / `auth_middleware` / `auth_guard` set by an extractor.
2. `TAGGED_AS` relationship from the endpoint to an `auth_policy` entity.
3. An `auth_policy` entity (emitted by the pattern extractor) shares the same source file.

**Auth annotations recognised (per framework)**

| Framework | Recognised markers |
|-----------|-------------------|
| Django | `@login_required`, `@permission_required`, `@user_passes_test` |
| DRF | `permission_classes = [IsAuthenticated]` |
| Flask | `@login_required`, `@jwt_required`, `@roles_required` |
| FastAPI | `Depends(get_current_user)`, `Depends(oauth2_scheme)` |
| Express | `requireAuth`, `authMiddleware`, `verifyToken`, `passport.authenticate` |
| NestJS | `@UseGuards(JwtAuthGuard)` |
| Spring | `@PreAuthorize`, `@Secured`, `@RolesAllowed` |
| ASP.NET | `[Authorize]`, `[Authorize(Roles=...)]`, `[Authorize(Policy=...)]` |
| Rails | `before_action :authenticate_user!` |

**Severity rules**

| Condition | Severity |
|-----------|----------|
| Auth present | `info` |
| No auth + sensitive operation (payment/delete/admin/…) | `error` |
| No auth + IDOR-risk path (`{user_id}`, `:account_id`, …) | `error` |
| No auth + anything else | `warn` |

**Default-allow vs default-deny**

If ≥ 80 % of endpoints in a repo are covered, the repo is classified as `default-deny`
(auth is the norm). Otherwise `default-allow` (auth is the exception — higher risk posture).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `group` | string | no | inferred | Group name (registry key). |
| `cwd` | string | no | — | CWD for group inference. |
| `repo_filter` | string[] | no | all | Limit to specific repos. |
| `only_missing` | bool | no | `false` | When true, return only endpoints where `has_auth=false`. |
| `limit` | int | no | `200` | Max endpoints returned. |

**Output**

```json
{
  "endpoints": [
    {
      "entity_id": "myrepo::a1b2c3d4e5f6a7b8",
      "name": "delete_user",
      "repo": "myrepo",
      "source_file": "api/users.py",
      "start_line": 42,
      "method": "DELETE",
      "path": "/api/users/{user_id}",
      "has_auth": false,
      "auth_evidence": "",
      "severity": "error",
      "sensitive_op": true,
      "idor_risk": true,
      "sensitive_terms": "delete, user_id"
    }
  ],
  "count": 1,
  "total": 1,
  "truncated": false,
  "repo_summaries": [
    {
      "repo": "myrepo",
      "total": 12,
      "covered": 10,
      "uncovered": 2,
      "coverage_rate": 0.833,
      "default_policy": "default-deny",
      "error_count": 1,
      "warn_count": 1
    }
  ],
  "overall_coverage": 0.833,
  "note": "..."
}
```

---

### `archigraph_secrets`

Hardcoded-secret detector (#1322). Walks every source file in each repo of the group and flags
lines that appear to contain embedded credentials: AWS access keys, GitHub tokens, JWT tokens,
Stripe keys, SendGrid keys, Slack tokens, generic high-entropy assignments, and password literals.

**Suppression rules**

- Files in test directories (`/test/`, `/tests/`, `/testdata/`, `__tests__`, `*.test.*`, `*_test.go`, etc.) are skipped entirely.
- Lines with the opt-out comment `// archigraph: ignore-secret` are skipped.
- Values that match common placeholder patterns (`example`, `changeme`, `REPLACE_ME`, all-same-char sequences, well-known AWS documentation keys) are suppressed.

**Severity grades**

| Severity | Patterns |
|----------|----------|
| `critical` | AWS access key (`AKIA…`), AWS secret key, PEM private key block |
| `high` | GitHub token (`ghp_`/`gho_`/`ghs_`), JWT, Stripe live key, SendGrid API key, Slack token |
| `medium` | Generic `api_key=`, `secret_key=`, `password=` assignments, high-entropy catch-all |
| `low` | Other keyword matches |

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `group` | string | no | inferred | Group name (registry key). |
| `cwd` | string | no | — | CWD for group inference. |
| `severity` | string | no | all | Minimum severity to include (`critical`\|`high`\|`medium`\|`low`). |
| `limit` | int | no | `200` | Maximum number of findings returned. |

**Output**

```json
{
  "scanned_repos": 3,
  "total_findings": 5,
  "truncated": false,
  "by_severity": { "critical": 1, "high": 2, "medium": 2, "low": 0 },
  "files": [
    {
      "repo": "backend",
      "file": "config/settings.go",
      "count": 2,
      "severity": "critical",
      "findings": [
        {
          "repo": "backend",
          "file": "config/settings.go",
          "line": 14,
          "kind": "aws_access_key",
          "masked_value": "AKIA****ABCD",
          "severity": "critical",
          "suggested_env_var": "AWS_KEY"
        }
      ]
    }
  ],
  "tip": "Add '// archigraph: ignore-secret' to suppress a specific line. Replace hardcoded values with the suggested env var."
}
```

---

---

## Handshake Token Budget

The MCP `initialize` response carries every tool definition. Keeping it small
reduces the token cost paid on every new agent session.

| Metric | Value |
|--------|-------|
| Measured baseline (2026-05-21, 32 tools) | **4,219 tokens** |
| Ceiling (baseline + 7 %) | **4,500 tokens** |
| Estimation method | conservative 4 chars/token |
| Tool description limit | **80 characters** |

### Enforcement

`make mcp-audit` runs `cmd/mcp-audit` which:

1. Instantiates the MCP server against an empty registry.
2. Measures the JSON-serialised size of every tool definition.
3. Applies the 4-chars/token estimate + 512-byte envelope overhead.
4. Fails (exit 1) if the total exceeds `AUDIT_CEILING` (default 4,500).
5. Fails if any tool description exceeds 80 characters.

The pre-merge CI workflow runs `make mcp-audit` as a required gate.

### Adding a new tool

When you add a tool, run `make mcp-audit` before submitting. If the ceiling is
exceeded, either shorten existing descriptions or open a budget-increase PR with
a comment explaining the token cost and why it is justified.

### Override ceiling

```sh
AUDIT_CEILING=4200 make mcp-audit          # stricter gate
AUDIT_BASELINE=4219 make mcp-audit         # show delta from measured baseline
go run ./cmd/mcp-audit -json               # machine-readable JSON report
```

---

---

## Wire Format

### TOON encoding (#1672)

Since #1672 the MCP server applies a last-step JSON→TOON conversion for
**list-of-record tool payloads** before bytes leave the daemon. Internal code
stays JSON throughout; the conversion happens only in `Server.wrap` via
`injectElapsedMS` → `recordsToTOON`.

#### What changes

| Response shape | Before #1672 | After #1672 (default) |
|----------------|-------------|----------------------|
| JSON array of homogeneous records | `{"items":[{...},{...}], "count":N, "elapsed_ms":M}` | `{"items":"[!schema {f1,f2}]\n{v1,v2}\n{v1,v2}\n", "count":N, "elapsed_ms":M}` |
| JSON object (single entity) | unchanged | unchanged |
| Non-JSON / compact-text payloads | unchanged | unchanged |

#### TOON format

A TOON-encoded block starts with a schema declaration followed by one row per
record:

```
[!schema {field1,field2,field3}]
{value1,value2,value3}
{value1,value2,value3}
```

- Keys are **sorted alphabetically** for determinism.
- Strings are escaped: `\`, `,`, `{`, `}` are preceded by a backslash.
- Numbers and booleans are emitted bare.
- Nested objects/arrays fall back to their minified JSON form (single cell,
  escaped as a string).
- An agent reading TOON treats each `{...}` line as a row and the schema line
  as the column names.

#### Detection rule

`recordsToTOON` converts only when **every element** of the array is a
`map[string]any` with **the same key set** (same count, same keys). Any
mismatch — including missing keys, extra keys, or non-object elements — falls
back to the minified JSON array in `items`.

#### Opt-out

Set `MCP_WIRE_FORMAT=json` to receive `items` as a raw JSON array (same
shape as #1663 minified JSON). This env var is read at call time so it can be
toggled without restarting the daemon.

#### Token savings

Measured on a representative 40-record endpoint payload (8 fields per row):

| Format | Tokens (≈chars/4) | Savings vs JSON |
|--------|-------------------|-----------------|
| Minified JSON array | 2,270 | — |
| TOON-encoded | 1,388 | **~39%** |

For list-heavy tools (`archigraph_endpoints`, `archigraph_topology`,
`archigraph_auth_coverage`, `archigraph_find_dead_code`, etc.) this is
additive on top of the minification savings from #1663.

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
