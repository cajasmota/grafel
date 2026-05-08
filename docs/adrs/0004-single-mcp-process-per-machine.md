# ADR-004: Single MCP process per machine, not per project

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

In the standard MCP integration model, an IDE or agent host reads a per-project `.mcp.json` and spawns one MCP server process per configured project. For a developer with 10+ active repositories, this produces 10+ idle MCP processes, each holding its own graph in memory, none of which can share state.

The per-project model has predictable failure modes at this scale:

- **Memory waste**: each idle process held tens of MB; the total across projects became significant on developer laptops.
- **No cross-project queries**: an agent working in repo A could not query repo B without spinning up its server and switching context.
- **Cold-start churn**: every IDE launch forked all configured MCP servers; warm-up amortization was lost when an IDE restarted.
- **Configuration sprawl**: each project's `.mcp.json` had to be kept in sync; onboarding a new repo required editing config.

archigraph organizes repos into **groups** (a group is an arbitrary collection of repos the user wants to query together — typically a microservices org, a monorepo's sub-projects, or a multi-repo product). The natural unit of MCP-server scope is therefore the machine's full set of registered groups, not any single project.

## Decision

archigraph runs **one long-lived MCP process per machine**. The process loads `~/.archigraph/registry.json` on startup, holds graphs for every registered group in memory, and routes each tool call to the appropriate group via the resolution cascade defined in ADR-008.

IDE and agent-host configuration uses a **single global MCP entry**, not per-project entries. Users configure archigraph once in their host's global MCP config; opening any repo registered in `~/.archigraph/registry.json` makes that repo's graph queryable through the same connection.

Routing a tool call to the right group uses three signals (full detail in ADR-008):

1. Explicit `group` argument on the tool call.
2. Caller's working-directory metadata, walked upward to find a `.archigraph/group.json` marker.
3. Singleton-group fallback when only one group is registered.

A new `archigraph_whoami` MCP tool returns the inferred group + repo for the current caller session, useful for agent self-orientation.

File watchers remain **per-repo** but are not part of this process. They are short-lived units invoked by save events (or by the IDE's file-change hook), which write incremental updates to disk that the long-lived MCP process picks up via mtime polling.

## Consequences

### Positive
- One process, one binary load, one warm graph cache for the entire developer session.
- Memory footprint is dominated by the graphs themselves, not by N copies of the runtime.
- Cross-group queries are possible without spawning anything new.
- Single global config; new repos are registered by `archigraph register`, not by editing IDE config.
- Cold start cost is paid once per machine session.

### Negative
- A bug in the MCP process now affects every project simultaneously rather than one at a time. Crash-resilience and graceful restart matter more than in the per-project model.
- The `group` argument adds a small surface to every tool. The cascade in ADR-008 hides this from agents in the common case.
- Long-running processes accumulate state; we need disciplined release of graph memory when groups are unregistered.

### Neutral
- Watchers being separate processes is a design choice this ADR endorses but does not solely justify; see the indexing-on-save behavior documented in `INDEXING.md`.
- The single-process model interacts with ADR-006 (in-memory + JSON persistence): the process is also the only consumer of the on-disk JSON, simplifying concurrency.

## Alternatives considered

- **One MCP process per project** (the default model) — rejected: the memory and configuration sprawl described above.
- **One MCP process per group** — rejected: middle-ground that retains most of the cold-start and config-sprawl problems while losing cross-group queries.
- **A daemon plus thin per-project clients** — rejected: doubles the binary surface and requires an IPC protocol on top of MCP, with no clear benefit over a single process holding everything.
- **Stateless MCP process that loads graphs on every call** — rejected: latency is unacceptable for interactive agent use, and the in-memory model from ADR-006 already amortizes load cost across the session.
