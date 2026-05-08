# ADR-002: Clean-room MCP server in Go (no derived code)

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

archigraph exposes its graph to AI agents through a Model Context Protocol (MCP) server. Earlier internal MCP-server work in this problem space was derived from upstream code under permissive open-source licenses. While permissive licenses allow reuse with attribution, they create a non-trivial lineage debt: every public release must carry attribution notices, contribution provenance must be tracked, and any divergence from upstream behavior has to be reasoned about against the original implementation.

For a public OSS launch under archigraph's own name we want zero lineage debt. The server should be implementable from publicly available specifications and from a third-party Go library's own public API, without referencing any prior implementation's source code. This keeps the codebase auditable, releases unencumbered, and the project's narrative clean.

The MCP specification is itself public and stable enough to implement against directly. The behavioral contract that agents will see — tool names, argument shapes, response schemas — is captured in our own `SCHEMA.md`, which doubles as the source of truth for tests.

## Decision

The archigraph MCP server is written from scratch in Go using a maintained third-party Go MCP library (`mark3labs/mcp-go` at the time of writing). Implementation derives from exactly three sources:

1. The published MCP specification.
2. The chosen Go library's public API documentation.
3. archigraph's own `SCHEMA.md` and ADRs.

No code, comments, structure, or naming is copied or transliterated from any other MCP-server implementation. Contributors are instructed in `CONTRIBUTING.md` not to consult prior implementations when working on the server. The tool surface (see ADR-003 for the entity taxonomy and ADR-008 for routing) is specified in our own documents and tested against our own behavioral fixtures.

## Consequences

### Positive
- Zero attribution requirements in distributed binaries.
- No upstream-divergence pressure; we evolve the server on archigraph's schedule.
- Codebase is small, idiomatic Go, fully owned.
- Easier to reason about security: no transitive code we did not write.
- Compatible with whatever license archigraph picks for v1.0 without compatibility caveats.

### Negative
- Roughly three to five extra days of implementation work versus forking an existing server.
- We re-encounter problems other implementations have already solved (transport edge cases, lifecycle handling, error surfacing). The Go MCP library absorbs most of these but not all.
- Tests for the MCP layer must be built up from scratch.

### Neutral
- The third-party Go MCP library is itself under a permissive license; we depend on it as a normal Go module without copying its code into our tree, which is the standard and uncontroversial form of OSS dependency.
- If the Go MCP library becomes unmaintained, we can swap implementations because our server code is decoupled from it through our own service interfaces.

## Alternatives considered

- **Fork an existing MCP server with attribution** — rejected: attribution would name external projects we do not want to mention in archigraph's release narrative, and lineage debt accumulates over time.
- **Write the MCP server in Python** — rejected: would split the binary distribution story (see ADR-001), forcing users to install Python alongside the Go binary. Defeats the single-artifact goal.
- **Implement the MCP wire protocol from scratch without a library** — rejected: the MCP spec is broad enough that re-implementing the transport layer is not a good use of time, and `mark3labs/mcp-go` is well-maintained.
- **Defer the MCP server to v1.1 and ship CLI-only at v1.0** — rejected: the AI-agent integration is the primary user value; shipping without it would invert the project's positioning.
