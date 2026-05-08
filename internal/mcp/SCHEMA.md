# archigraph MCP — Tool & Schema Reference

Canonical contract for the archigraph MCP server's tool surface, request/response
shapes, and the entity / relationship vocabulary it exposes.

This document is referenced by ADR-0002 (clean-room MCP server in Go) as the
public contract for all tools the server registers. It is the source of truth
for clients (Claude Code, Windsurf, etc.) and tracks the implementation in
`internal/mcp/server.go` and `internal/mcp/tools.go`.

---

## Overview

- **Server name (as advertised to MCP clients):** `archigraph`
- **Transport:** stdio
- **Process model:** one server per machine, multiple registered groups, lazy
  mtime-driven reload before every tool call. See ADR-0004.
- **Tool count:** 17, all prefixed `archigraph_*` to avoid client-side
  collisions when other MCP servers are installed alongside (Refs #62).
- **State:** in-memory `Document`s loaded from per-repo `.archigraph/graph.json`
  files; no database. See ADR-0006.
- **Routing:** every tool that touches graph data resolves a group via the
  `group` arg → CWD marker → singleton fallback cascade. See ADR-0008.
- **Cross-repo IDs:** prefixed `<repo>::<localId>` when the call spans multiple
  repos, bare `<localId>` when the call is single-repo-scoped. See ADR-0009.

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
| [`archigraph_search`](#archigraph_search) | BM25-ranked graph query, optionally BFS-expanded. |
| [`archigraph_describe`](#archigraph_describe) | Look up an entity by id, qualified name, or label. |
| [`archigraph_related`](#archigraph_related) | Return neighbors of a node out to a given depth. |
| [`archigraph_trace`](#archigraph_trace) | Confidence-weighted shortest path between two nodes. |
| [`archigraph_list_clusters`](#archigraph_list_clusters) | List Louvain communities across the loaded graphs. |
| [`archigraph_save_finding`](#archigraph_save_finding) | Persist a Q/A pair to the group's memory directory. |
| [`archigraph_list_findings`](#archigraph_list_findings) | List previously saved findings, optionally filtered by entity or time. |
| [`archigraph_get_source`](#archigraph_get_source) | Return source-file snippet for a node from disk. |
| [`archigraph_recent_activity`](#archigraph_recent_activity) | Entities whose source files were modified after a given time. |
| [`archigraph_list_link_candidates`](#archigraph_list_link_candidates) | List pending cross-repo link candidates. |
| [`archigraph_resolve_link_candidate`](#archigraph_resolve_link_candidate) | Accept or reject a cross-repo link candidate. |
| [`archigraph_list_enrichment_candidates`](#archigraph_list_enrichment_candidates) | List pending enrichment candidates for a repo. |
| [`archigraph_submit_enrichment`](#archigraph_submit_enrichment) | Submit an enrichment resolution. |
| [`archigraph_reject_enrichment`](#archigraph_reject_enrichment) | Reject an enrichment candidate. |
| [`archigraph_graph_stats`](#archigraph_graph_stats) | Corpus-level metrics for the resolved group. |
| [`archigraph_get_telemetry`](#archigraph_get_telemetry) | Server uptime, per-tool counters, reload counts. |

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

### `archigraph_search`

BM25-ranked graph query across every repo in scope, optionally BFS-expanded
from each top hit. The default rendering is compact text optimised for an LLM
context budget; pass `full=true` for raw JSON.

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

### `archigraph_describe`

Look up an entity by ID, prefixed cross-repo ID, qualified name, or label.

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

### `archigraph_related`

Return BFS neighbours of a node out to a given depth, plus any cross-repo
overlay edges that originate from that node.

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

### `archigraph_list_clusters`

List Louvain communities pre-baked into each repo's `graph.json` (see
ADR-0005).

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

### `archigraph_list_link_candidates`

List pending cross-repo link candidates from the group's
`<group>-link-candidates.json` file.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | `[]` | Returns candidates whose source OR target lives in any of these repos. |
| `channel` | string | no | — | Filter by channel label. |
| `method` | string | no | — | Filter by detection method. |
| `limit` | number | no | `10` | Max candidates returned. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array of `LinkCandidate` records (id, source, target, kind,
confidence, channel, method).

---

### `archigraph_resolve_link_candidate`

Accept or reject a single cross-repo link candidate. On accept, the candidate
is appended to the group's links file (`<group>-links.json`); on reject, it
is dropped. Either decision removes the candidate from the pending list.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `candidate_id` | string | yes | — | Candidate ID from `list_link_candidates`. |
| `decision` | string | yes | — | `accept` \| `reject`. |
| `reason` | string | no | — | Free-form audit string. |
| `override_target` | string | no | — | When accepting, override the candidate's target ID with this prefixed ID. |
| `group`, `cwd` | string | no | — | Common args. |

**Output**

```json
{ "candidate_id": "lc-abc123", "decision": "accept" }
```

---

### `archigraph_list_enrichment_candidates`

List pending enrichment candidates per repo (per-repo
`enrichment-candidates.json`).

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `repo_filter` | string[] | no | `[]` | Common arg. |
| `kind` | string | no | — | Filter by candidate kind (e.g. `purpose`). |
| `limit` | number | no | `10` | Max candidates returned. |
| `group`, `cwd` | string | no | — | Common args. |

**Output** — JSON array:

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

---

### `archigraph_submit_enrichment`

Submit a resolution for a pending enrichment candidate. Appends to the repo's
`enrichment-resolutions.json` and removes the candidate from the pending list.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `candidate_id` | string | yes | — | Candidate ID. |
| `value` | string | yes | — | The agent's answer. |
| `confidence` | number | no | `1` | Confidence in `[0,1]`. |
| `reason` | string | no | — | Free-form audit string. |
| `group`, `cwd` | string | no | — | Common args. |

**Output**

```json
{ "candidate_id": "ec-1", "decision": "accept" }
```

---

### `archigraph_reject_enrichment`

Reject a pending enrichment candidate; appends to the repo's enrichment
rejections log and removes the candidate from the pending list.

**Inputs**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `candidate_id` | string | yes | — | Candidate ID. |
| `reason` | string | yes | — | Required rejection reason. |
| `group`, `cwd` | string | no | — | Common args. |

**Output**

```json
{ "candidate_id": "ec-1", "decision": "reject" }
```

---

### `archigraph_graph_stats`

Corpus-level metrics for the resolved group: per-repo entity / relationship /
community counts, plus group-level totals and any unavailable repos (with
load errors).

**Inputs** — common args only.

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
    "archigraph_search":   { "calls": 42, "errors": 1, "p50_ms": 8.2, "p95_ms": 31.7 },
    "archigraph_describe": { "calls": 17, "errors": 0, "p50_ms": 1.1, "p95_ms": 2.3 }
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
| `SCOPE.TestCoverage` | `TestCoverage` | Test-coverage units (test fn → covered entity). |
| `SCOPE.DeprecationAnnotation` | `DeprecationAnnotation` | Deprecation markers (`@deprecated`, `Obsolete`). |
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
| `CONSUMES_QUEUE` | Consumer reads from a queue / topic. |
| `TRIGGERS_LAMBDA` | Trigger source fires a serverless function. |
| `READS_TABLE` | DataAccess reads from a Datastore. |
| `WRITES_TABLE` | DataAccess writes to a Datastore. |
| `TESTS` | Test entity exercises another entity. |

The full list of edge kinds the agent may pass to `archigraph_search`'s
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
  filtered by `entity_id` or `since`. `archigraph_describe` and
  `archigraph_trace` also auto-attach matching findings under a `findings`
  field of their response (Refs #59). Ingestion back into the graph proper
  is still out of scope for v1.0.
- **No deduplication beyond the filename hash:** repeated calls with the
  same Q/A in the same UTC second collapse to one file; otherwise a fresh
  file is written.

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
- Source — `internal/mcp/server.go`, `internal/mcp/tools.go`
- Issues — #52 (initial rename), #62 (`archigraph_*` prefix), #57 (this doc)
