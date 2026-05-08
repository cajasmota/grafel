# ADR-005: Pre-bake graph attributes during indexing

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

archigraph's value proposition rests on giving agents fast, structured answers to architecture questions. A subset of those answers benefits substantially from classical graph algorithms:

- **Community detection** (Louvain, Leiden) groups closely-coupled subsystems, useful for "what cluster does this module belong to?".
- **Betweenness centrality** identifies architectural choke-points — code that sits on many paths between communities.
- **Surprise edges** (edges that bridge otherwise-distant communities) flag unexpected coupling worth surfacing in answers.

These algorithms are O(N · E) or worse on a graph of tens of thousands of nodes and edges. Running them at MCP query time would add hundreds of milliseconds to every relevant call and would scale badly as repos grow. Caching the result of the first query is one option, but unpredictable cold-query latency is its own UX problem.

The indexer already touches every node and edge during graph construction. It is the natural place to amortize this cost, especially since indexing already runs incrementally on file save (see ADR-004 on the watcher model).

## Decision

`archigraph index` computes community membership, centrality, and surprise scores during indexing, using `gonum/graph` algorithms, and bakes the results into the output JSON as **node attributes**:

- `community_id` — Louvain community label.
- `centrality` — normalized betweenness centrality.
- `surprise` — score on incident edges, exposed as a node-level summary (max, sum, count) plus per-edge attributes on the edge objects.

The MCP server reads these attributes directly from the loaded graph. It never recomputes them. The MCP binary therefore has no graph-algorithm dependency at runtime — `gonum/graph` is imported by the indexer subcommand only, and the MCP subcommand is leaner because of it.

When a save-event watcher triggers an incremental re-index (see ADR-004), the indexer re-runs the relevant algorithms. For very large graphs we constrain re-runs to changed connected components when correctness allows; otherwise we re-compute globally. The cost is amortized across the session and is invisible to the agent.

This decision interacts with ADR-006: because graphs are persisted as JSON on disk, the baked attributes survive process restarts without recomputation.

## Consequences

### Positive
- Query-time cost for centrality / community queries is microseconds (attribute read), not seconds (algorithm run).
- MCP server stays small; no algorithm code is loaded into the long-lived process from ADR-004.
- Attribute values are auditable: the JSON on disk shows exactly what the agent sees.
- Indexing-time cost is acceptable because indexing happens off the critical path of agent interaction.

### Negative
- Indexing is slower per repo than it would be without the attributes. For the typical 10k-file repo, the added cost is on the order of seconds.
- Attributes can drift from ground truth between an edit and the next re-index. The watcher's incremental re-index keeps drift bounded to seconds, which is acceptable.
- Adding a new algorithm is an indexer change plus a schema change in the JSON, not a hot-swappable MCP feature.

### Neutral
- The choice of `gonum/graph` ties archigraph to that library's algorithm set for v1.0; replacing it later is contained to the indexer.
- The attribute schema is part of `SCHEMA.md` and follows the same versioning rules as the entity taxonomy in ADR-003.

## Alternatives considered

- **Compute on first query, cache in memory** — rejected: unpredictable cold-query latency, and the cache is lost on process restart.
- **Compute on demand per query** — rejected: too slow for interactive use, and the same algorithm runs many times per session.
- **Separate offline batch job (cron-style)** — rejected: introduces staleness windows that conflict with the save-event watcher model in ADR-004 and adds operational complexity.
- **Push the algorithms into a graph database** — rejected: ADR-006 documents why we are not running a graph database; reintroducing one for a single use case is not justified.
