# Concepts

This page explains the mental model behind archigraph: what the graph contains, how it gets built, and what happens when the indexer cannot fully resolve a relationship.

---

## The knowledge graph

archigraph represents a codebase as a directed graph of **entities** (nodes) connected by **edges** (relationships). The graph is built once during indexing and reloaded from disk on every tool call (lazy, mtime-driven). There is no external database — the graph lives in memory as a loaded `.archigraph/graph.fb` file per repo.

Multiple repos can be indexed together as a **group**. Cross-repo edges are stored as overlay links with a confidence score (default 0.7 for cross-repo edges vs. 0.95 for intra-repo edges).

---

## Entity kinds

The indexer extracts entities using tree-sitter grammars plus per-language resolver slices. Entity kinds are defined in [ADR-0003](adrs/0003-scope-entity-taxonomy.md).

Common kinds you will encounter:

| Kind | What it represents |
|------|-------------------|
| `Component` | Class, struct, module, or named type |
| `Operation` | Function, method, or handler |
| `Schema` | Database model, serializer, or data shape |
| `Queue` | Message-bus topic, queue, or stream |
| `http_endpoint_definition` | Server-side HTTP route handler |
| `http_endpoint_call` | Client-side HTTP call-site |
| `Process` | Pre-computed BFS flow chain from an entry point |

Each entity carries: a stable ID, a qualified name, a source file and line range, a PageRank score (pre-baked at index time), a Louvain community ID, and an optional set of saved findings.

---

## Edge kinds

Edges represent relationships extracted by the resolvers. Common kinds:

| Kind | Meaning |
|------|---------|
| `CALLS` | A calls B (function call, method dispatch) |
| `IMPORTS` | A imports B (module-level dependency) |
| `USES` | A uses B (field access, type reference) |
| `PUBLISHES_TO` | A publishes a message to topic B |
| `SUBSCRIBES_TO` | A subscribes to topic B |
| `EXTENDS` | A extends or implements B |
| `OVERRIDES` | A overrides method B |

Each edge has a **confidence** value between 0 and 1. Statically resolved edges default to 0.95. Edges that required heuristic resolution or cross-repo linking are lower.

---

## Residual edges

Not every call can be resolved at index time. Dynamic dispatch, string-keyed lookups, reflection, and cross-repo calls where the target repo has not been indexed all produce **residual edges** — stubs in the graph that point to an unresolved target.

The indexer records these stubs in the enrichment queue rather than dropping them. The `/archigraph-resolve` skill surfaces the queue, automatically resolves unambiguous cases via pattern matching, and walks you through the rest interactively.

Residual edges are not failures — they are honest signals that the relationship exists but the target is not yet certain. A high residual count on a specific entity means that entity has significant dynamic behavior that is worth resolving manually.

---

## Repair and enrichment

Beyond residual edges, the enrichment queue holds three types of candidates:

- **Residual repairs** — unresolved stubs from the indexer (`archigraph_repairs`)
- **Cross-repo link candidates** — edges where the target might be in a sibling repo (`archigraph_cross_links`)
- **Enrichment candidates** — `http_endpoint`, `process_flow`, and `message_topic` entities that the indexer flagged for LLM annotation (`archigraph_enrichments`)

The dashboard **Pending** surface (`/g/:groupId/pending`) shows the full queue tiered by priority (Critical / High / Medium / Low).

The `/archigraph-graph-enrich` skill emits YAML frontmatter for HTTP endpoints, flows, and topics — this makes the **Paths**, **Flows**, and **Topology** dashboard panels display data.

---

## Groups and repos

A **group** is archigraph's unit of multi-repo management. A group has:
- A slug (name) used in all CLI commands and MCP routing
- One or more repo entries, each pointing at a local directory
- A `cross-repo-links.yaml` file that declares known cross-repo relationships

The daemon resolves which group to use for a given agent session by matching the agent's working directory against registered repo paths (see [ADR-0008](adrs/0008-caller-cwd-aware-routing-for-multi-group-setups.md)).

---

## Multi-branch support

archigraph stores one graph snapshot per `(repo, ref)` pair. When you switch branches, the daemon detects the change via `.git/HEAD` watch and loads the appropriate snapshot. Snapshots are tiered HOT/WARM/COLD to keep RAM bounded.

See [user-guide/multi-branch.md](user-guide/multi-branch.md) for the full guide.

---

## Patterns

As agents work with the graph, they can save findings via `archigraph_save_finding`. Over time, recurring structural patterns are extracted and stored as first-class entities. These are visible in the **Patterns** dashboard surface and manageable via the `/archigraph-patterns-discover` and `/archigraph-patterns-sync` skills.

See [ADR-0018](adrs/0018-agent-learned-patterns.md) for the full design.

---

## Dashboard surfaces

The dashboard is embedded in the daemon at `http://127.0.0.1:47274`. After
selecting a group, every surface is nested under `/g/:groupId/` — so the
Pending view, for example, is `/g/:groupId/pending`, not a bare `/pending`.
The available surfaces (routes defined in `webui-v2/src/routes/router.tsx`):

| Surface | Path | Shows |
|---------|------|-------|
| Graph | `/g/:groupId/graph` | Entity/edge graph explorer |
| Flows | `/g/:groupId/flows` | Pre-computed process flows |
| Event-flows | `/g/:groupId/event-flows` | Message-bus / event-driven flows |
| Topology | `/g/:groupId/topology` | Topic/broker/service topology |
| Paths | `/g/:groupId/paths` | HTTP endpoint surface and pairings |
| Links | `/g/:groupId/links` | Cross-repo link candidates |
| GraphQL | `/g/:groupId/graphql` | GraphQL schema surface |
| IaC | `/g/:groupId/iac` | Infrastructure-as-code resources |
| Docs | `/g/:groupId/docs` | Generated documentation viewer |
| Security | `/g/:groupId/security` | Security findings |
| Taint | `/g/:groupId/taint` | Taint / data-flow reachability |
| DI | `/g/:groupId/di` | Dependency-injection wiring |
| Error-flow | `/g/:groupId/errorflow` | Error / exception propagation |
| Quality | `/g/:groupId/quality` | Extraction-quality metrics |
| Settings | `/g/:groupId/settings` | Per-group daemon settings |
| Pending | `/g/:groupId/pending` | Residual / repair / enrichment queue |
| Operations | `/g/:groupId/operations` | Index/rebuild operation log |
| Compare | `/g/:groupId/compare` | Structural ref-to-ref diff |
| Missing | `/g/:groupId/missing` | Unresolved / missing targets |

Most surfaces light up only after the graph is indexed and (for Paths, Flows,
and Topology) enriched via `/archigraph-graph-enrich`.
