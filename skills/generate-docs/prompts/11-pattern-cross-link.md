# Pass 11 — Pattern cross-link (Phase 5 of ADR-0018)

For every pattern approved in Pass 10 (or refined in this run), populate its `documentation_url` field so the graph holds a forward pointer to the markdown emitted in Pass 12.

## Procedure

For each approved pattern `p`:

1. Compute the canonical doc URL:

   ```
   docs/patterns/<category>/<pattern-id>.md
   ```

   The `archigraph_patterns(action=get, id=<id>)` response includes both `category` and `id`; the relative path is fully determined from the pattern itself. The renderer in `internal/agentpatterns/docs.go` exposes `DocPathFor(p)` so the value is consistent on both ends.

2. Refine the pattern with the URL:

   ```
   archigraph_patterns(action=refine, pattern_id=<id>, changes={ "set_documentation_url": "<computed url>" })
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
- DO NOT chase `SUPERSEDES` edges here. Superseded patterns keep their old documentation_url until the user runs `archigraph patterns delete` or v1.1's history surface lights up.
