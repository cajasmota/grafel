# ADR-006: In-memory + JSON persistence (no graph database)

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

archigraph stores a graph per repo, plus cross-repo overlay edges per group. The total data set on a developer machine is on the order of tens of thousands of nodes and edges per repo, multiplied by perhaps a dozen repos in the largest groups. The data is read frequently (every MCP query) and written incrementally (every file save).

The conventional choice for storing a graph is a graph database — Neo4j, Memgraph, JanusGraph, or similar. Each provides a query language, persistent storage, and an indexing layer. Each also imposes operational burden:

- A background daemon that must be started, kept healthy, and stopped cleanly.
- Schema or index management, sometimes with explicit migrations.
- A multi-tenant story for users with many groups.
- For some options, a Docker dependency or a JVM, in tension with ADR-001's single-binary distribution goal.
- Memory and disk overhead that often dwarfs the graph itself for small data sets.

The MCP process from ADR-004 is the **only** consumer of the graph. There is no other service reading or writing concurrently. This rules out the main reason a database exists: shared multi-process access. For a single-process consumer with bounded data size, an in-memory representation backed by simple file persistence is both faster and operationally simpler.

## Decision

archigraph holds graphs in memory using `gonum/graph` data structures, indexed by group and repo (`map[group]map[repo]*Graph` in the broad sense). Persistence is **JSON files on disk**, one per repo, plus a separate `<group>-links.json` for cross-repo overlay edges. The on-disk layout lives under `~/.archigraph/groups/<group>/` and is human-readable.

The MCP process loads JSON on startup and watches mtime to reload changed files. Writes are produced exclusively by the indexer (initial and incremental). The MCP process never writes to the graph files; this gives a clean writer/reader separation and avoids file-locking complexity.

Memory budget for a typical 10k-file repo is well under 200 MB including baked attributes from ADR-005. For larger graphs we lazy-load per-repo on first reference and evict by LRU when total memory exceeds a configured cap (default: 75% of available RAM at startup).

## Consequences

### Positive
- No daemon, no Docker, no JVM, no schema migrations.
- On-disk format is JSON and diffable; users can `cat` and inspect.
- Single binary stays single (consistent with ADR-001).
- MCP query latency is bounded by in-memory traversal, not network or IPC.
- Reproducibility: the JSON files are the entire state; copy them to another machine and the graph is identical.

### Negative
- No declarative query language. All queries are Go code in the MCP server. We accept this because the agent-facing query surface is high-level (semantic tools, not Cypher).
- Memory footprint scales with codebase size; very large monorepos may exceed practical limits and require sharding or per-repo lazy load.
- JSON parse cost on cold start is non-trivial for large graphs; a future binary format could replace JSON without changing this ADR's core decision.
- Cross-repo overlay edges sit in a separate file, requiring care to keep consistent with per-repo files; the indexer owns this invariant.

### Neutral
- Concurrency model is single-writer (indexer) / single-reader (MCP process); no general concurrent-access machinery is needed.
- The choice composes with ADR-005: baked attributes are stored as JSON fields, no runtime computation in the MCP process.

## Alternatives considered

- **Neo4j** — rejected: JVM dependency, Docker-friendly but heavyweight, and the query-language benefit is not what archigraph needs.
- **Memgraph** — rejected: no first-class native Windows install at the required version, conflicts with ADR-001's cross-platform goal.
- **Embedded SQLite with a property-graph schema** — rejected: SQL is not a graph query language, and the storage win over JSON is small at our data scale.
- **DuckDB / Parquet** — rejected: optimized for analytical workloads, not point-graph traversals; query patterns do not match.
- **Custom binary format (BoltDB, Badger)** — deferred: a viable evolution from JSON if cold-start parse cost becomes a bottleneck. Not justified for v1.0.
