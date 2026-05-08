# ADR-0009: Cross-repo ID namespacing — `<repo>::<localId>`

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

A code knowledge graph that spans multiple repositories must handle entity-ID collisions. A function named `formatTime` exists in a mobile app and a frontend app; both are real and distinct, but they cannot share a graph identity. Without a namespacing strategy, the same ID maps to two different code locations and traversal results conflate them.

Three places where collision matters:

1. **Per-repo extraction** — each `archigraph index <repo>` runs in isolation; the indexer doesn't know other repos exist.
2. **MCP server holding multiple repos** — single process, in-memory `Dict[repo, Graph]`, must serve queries that span repos without conflating IDs.
3. **Cross-repo link file** — references entities in two different repos by ID; one or both endpoints must carry their repo name.

## Decision

**Two-layer namespacing**:

1. **Index layer (per repo)**: each repo's `graph.json` carries `entity.id` as a local ID (a hash of source file + line + name within that repo). IDs are NOT prefixed at index time. Each entity also carries a `repo` attribute set from the `--repo-tag <slug>` CLI flag (default: repo directory name).

2. **MCP composition layer**: the MCP server, when serving cross-repo views, prefixes IDs in its responses as `<repo>::<localId>`. When a tool call sets `repo_filter` to a single repo, IDs are returned unprefixed (the agent already knows the scope).

3. **Cross-repo link file**: the `<group>-links.json` file (and similarly the candidates / rejections files) ALWAYS uses prefixed IDs. Both source and target are `<repo>::<localId>` strings. This makes link entries self-describing across repos.

The local-ID hash is collision-stable within a single repo (identical functions in the same repo produce the same ID — desirable for re-indexing). Across repos, the same hash MAY collide; that's safe because the `repo` attribute disambiguates and the MCP composition layer prefixes.

## Consequences

### Positive

- `archigraph index` is repo-local — no global state, no cross-repo dependency at index time. Watchers can rebuild one repo's graph without touching others.
- Re-indexing produces stable IDs (deterministic hash) — agents that cached IDs can continue using them after a rebuild as long as the source location didn't change.
- The MCP composition layer is the only place that knows about cross-repo scope. Single point of namespace control. Easier to evolve.
- Cross-repo links file format is self-describing and unambiguous.

### Negative

- The MCP must always know which repo each loaded graph belongs to. Today this comes from the directory name in the graphs-dir (`<repo>.json` symlinks); breaks if a user renames a graph file out-of-band.
- Agents that copy IDs from one MCP response to another must preserve prefixes (or strip when scope is clear). Not hard, but a soft contract.

### Neutral

- Local IDs are not human-readable (hash form). `qualified_name` field on each entity (which IS human-readable, e.g. `core.views.OrderViewSet`) provides the alternative for tooling that wants names.

## Alternatives considered

- **Globally-unique IDs at index time** (e.g. UUIDv5 over `repo + file + line + name`). Rejected — pushes the namespace concern into the indexer, prevents independent repo rebuilds, makes link-table format larger because every reference is a UUID.
- **Repo prefix at index time** (every ID written as `<repo>::<local>` in `graph.json`). Rejected — bigger files, repo-name leakage in single-repo views, painful when renaming a repo (every ID changes).
- **Soft string match** (no IDs, identify entities by `(qualified_name, source_file, line)` tuple). Rejected — fragile under refactors that rename or move things.
