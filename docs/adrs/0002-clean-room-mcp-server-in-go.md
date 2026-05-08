# ADR-002: First-principles MCP server in Go (no derived code)

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

archigraph exposes its graph to AI agents through a Model Context Protocol (MCP) server. The implementation choice here has license, audit, and maintenance implications that outlast the initial coding effort: a server built by adapting code from another OSS MCP server inherits attribution requirements, makes every public release carry upstream notices, ties divergence decisions to an original implementation we have to keep reasoning about, and complicates security audits because not all of the code is something we wrote.

archigraph wants zero attribution overhead in distributed binaries and a codebase that is fully owned end-to-end. The MCP specification is public and stable enough to implement against directly, and a maintained third-party Go MCP library handles the transport layer as a normal module dependency. The behavioral contract that agents see — tool names, argument shapes, response schemas — is fully captured in our own `SCHEMA.md`, which doubles as the source of truth for tests.

## Decision

The archigraph MCP server is written from scratch in Go using a maintained third-party Go MCP library (`mark3labs/mcp-go` at the time of writing). Implementation derives from exactly three sources:

1. The published MCP specification.
2. The chosen Go library's public API documentation.
3. archigraph's own `SCHEMA.md` and ADRs.

No code, comments, structure, or naming is copied or transliterated from any other MCP-server implementation. Contributors are instructed in `CONTRIBUTING.md` to work only from the three sources above and not to read other MCP-server source trees while implementing archigraph's server. The tool surface (see ADR-003 for the entity taxonomy and ADR-008 for routing) is specified in our own documents and tested against our own behavioral fixtures.

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

- **Fork an existing MCP server with attribution** — rejected: attribution obligations propagate into every binary release, the codebase carries audit overhead for code archigraph did not write, and divergence decisions get tangled with an unrelated upstream's roadmap.
- **Write the MCP server in Python** — rejected: would split the binary distribution story (see ADR-001), forcing users to install Python alongside the Go binary. Defeats the single-artifact goal.
- **Implement the MCP wire protocol from scratch without a library** — rejected: the MCP spec is broad enough that re-implementing the transport layer is not a good use of time, and `mark3labs/mcp-go` is well-maintained.
- **Defer the MCP server to v1.1 and ship CLI-only at v1.0** — rejected: the AI-agent integration is the primary user value; shipping without it would invert the project's positioning.
