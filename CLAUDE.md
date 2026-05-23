# When to use archigraph MCP vs grep

archigraph MCP gives you a navigable, accurate map of the code; grep gives you raw pattern matches.
Use MCP for structural questions: who calls X? what is the flow? where does Y live in the graph?
Use grep for raw enumeration: every `if err != nil`, every import line, every TODO.
Pair them: MCP narrows the search space; grep verifies edge-property questions MCP can't answer yet.

## Three concrete examples

**MCP-good — structural navigation:**
"Which services call `OrderService.CreateOrder`?" → `archigraph_find` + `archigraph_callers` gives you
the precise call graph with repo context, in one round-trip. grep would require you to know every
caller file location across every repo in the group.

**grep-good — raw enumeration:**
"List every `if err != nil` block that is missing a `log.Error` call." → grep is the right tool.
archigraph models control flow at the entity level, not at the statement level. Raw text search on
the source files is faster and more complete for this class of pattern.

**Paired — search space reduction then raw verify:**
"Does any service leak the internal `User.PasswordHash` field in an HTTP response?" →
1. MCP: `archigraph_find entity_type=http_endpoint_definition` + `archigraph_paths` to identify
   every endpoint that touches `User`. Narrows a 500-file repo to 8 handlers.
2. grep: search only those 8 handler files for `PasswordHash` to confirm whether it appears in any
   serialisation path.

---

For archigraph developer workflow, architecture decisions, and contributing guidelines see
[`docs/adrs/`](docs/adrs/) and [`AGENTS.md`](AGENTS.md).
