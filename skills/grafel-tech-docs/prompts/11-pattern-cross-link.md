# Pass 11 — Pattern cross-link (Phase 5 of ADR-0018)

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_subgraph`, `grafel_orient (view=overview)`,
`grafel_orient (view=clusters)`, `grafel_orient (view=me)`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_orient (view=me)` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

For every pattern approved in Pass 10 (or refined in this run), populate its `documentation_url` field so the graph holds a forward pointer to the markdown emitted in Pass 12.

## Procedure

For each approved pattern `p`:

1. Compute the canonical doc URL:

   ```
   docs/patterns/<category>/<pattern-id>.md
   ```

   The `grafel_patterns(action=get, id=<id>)` response includes both `category` and `id`; the relative path is fully determined from the pattern itself. The renderer in `internal/agentpatterns/docs.go` exposes `DocPathFor(p)` so the value is consistent on both ends.

2. Refine the pattern with the URL:

   ```
   grafel_patterns(action=refine, pattern_id=<id>, changes={ "set_documentation_url": "<computed url>" })
   ```

   Refinement is confidence-neutral, so this call does not perturb the lifecycle.

3. Cross-link from the prose. When Pass 4 (cluster) or Pass 6 (cross-cutting) prose touches an entity that has an incoming `CREATED_BY` edge from a Pattern, add an inline link at first mention:

   ```
   When adding a handler, follow the [endpoint pattern](../patterns/code/<id>.md).
   ```

   This is the doc-as-bridge convention from ADR-0007: the prose carries the link, the graph carries the relationship; doc generators on both sides stay loosely coupled.

## Constraints

- DO NOT write `documentation_url` for `is_candidate=true` patterns. Their docs are not generated.
- DO NOT delete an existing `documentation_url` when re-running — refinement only overwrites it with the new computed URL.
- DO NOT chase `SUPERSEDES` edges here. Superseded patterns keep their old documentation_url until the user runs `grafel patterns delete` or v1.1's history surface lights up.

---

**[pass-11 telemetry]** Print at end of this pass:
```
[pass-11] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
