# When to use archigraph MCP vs grep

archigraph MCP gives you a navigable, accurate map of the code; grep gives you raw pattern matches.
Use MCP for structural questions: who calls X? what is the flow? where does Y live in the graph?
Use grep for raw enumeration: every `if err != nil`, every import line, every TODO.
Pair them: MCP narrows the search space; grep verifies edge-property questions MCP can't answer yet.

## Three concrete examples

**MCP-good — structural navigation:**
"Which services call `OrderService.CreateOrder`?" → `archigraph_find` + `archigraph_find_callers` gives you
the precise call graph with repo context, in one round-trip. grep would require you to know every
caller file location across every repo in the group.

**grep-good — raw enumeration:**
"List every `if err != nil` block that is missing a `log.Error` call." → grep is the right tool.
archigraph models control flow at the entity level, not at the statement level. Raw text search on
the source files is faster and more complete for this class of pattern.

**Paired — search space reduction then raw verify:**
"Does any service leak the internal `User.PasswordHash` field in an HTTP response?" →
1. MCP: `archigraph_find entity_type=http_endpoint_definition` + `archigraph_find_paths` to identify
   every endpoint that touches `User`. Narrows a 500-file repo to 8 handlers.
2. grep: search only those 8 handler files for `PasswordHash` to confirm whether it appears in any
   serialisation path.

---

For archigraph developer workflow, architecture decisions, and contributing guidelines see
[`docs/adrs/`](docs/adrs/) and [`AGENTS.md`](AGENTS.md).

<!-- archigraph:mcp-usage:start v=1 -->

## archigraph MCP

This repo is part of archigraph group **archigraph**. archigraph is an architecture knowledge graph available via MCP. When you (an AI coding agent) need to understand how this codebase fits together, prefer the archigraph MCP tools over `grep` + reading files.

### When to use archigraph instead of grep

| Question shape | Prefer |
|---|---|
| "Where is `X` defined?" | `archigraph_find` |
| "What does `X` look like + its neighbors?" | `archigraph_inspect` |
| "Who calls `X`?" | `archigraph_expand` / `archigraph_find_callers` |
| "End-to-end flow when user does X?" | `archigraph_traces` |
| "How does the frontend talk to the backend?" | `archigraph_cross_links` |
| "Show me the source of `X`" | `archigraph_get_source` |

### When grep IS still better

- Substring search across all files for non-entity strings (comments, TODOs).
- Anything where you need raw file contents in bulk.

### Anti-patterns

- Don't read an entire file to find one function — `archigraph_inspect` returns it directly.
- Don't glob for a class name across the repo — `archigraph_find` indexes it.
- Don't traverse imports manually — `archigraph_expand` does it via the IMPORTS edge.

The full agent guide is delivered automatically in the MCP `instructions` handshake when you connect.

_Do not edit between the markers — this block is auto-updated by `archigraph install`._

<!-- archigraph:mcp-usage:end -->