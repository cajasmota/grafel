# Pass 3a — Generation-time repair (in-prose hook)

A **hook**, not a standalone pass. Every writer subagent in Passes 3, 4, 5, 6, and 12 runs this check immediately before emitting prose that describes an entity. The hook is the primary site for `bind_to_entity` resolutions — Pass 1a deliberately defers those here because the writer has full local subgraph context via `archigraph_expand`. It also catches residuals discovered late (after Pass 1a) that the earlier sweep missed.

## When the hook fires

Every time a writer is about to write a paragraph about an entity `E`, run:

```
archigraph_repairs(action=list, repo_filter=[<repo of E>], limit=20)
```

Filter the result client-side to residuals whose `from_entity.id == E.id`. If the filtered set is empty, write the prose as planned and continue.

## When the hook acts

For each residual where `from_entity.id == E.id`:

1. Inspect the residual's `original_stub`, `relation`, and the entity's neighborhood (`archigraph_expand(node=E.id, depth=1)` or `archigraph_inspect`).
2. Decide whether you can submit a repair **without asking the user**. The same auto-resolve criteria from Pass 1a apply:
   - Unambiguous binding target visible in the local subgraph, OR
   - Matches an active template in `repair-templates.json`, OR
   - Recognised third-party module.
3. If auto-resolve is possible: `archigraph_repairs(action=submit, ...)` with `source="generate-docs/pass-3a"`, then write the prose **as if the edge were resolved** — describe the target the repair points at, not the original stub.
4. If auto-resolve is not possible: write the prose with an explicit "runtime-resolved" callout (see template below). Do **not** silently drop the edge.

## Prose template for unresolved residuals

When an outbound edge cannot be repaired in-pass, the writer surfaces it as a documented dynamic edge so readers see what static analysis could not:

```markdown
> **Runtime-resolved edge.** `<E.name>` calls `<original_stub>` via <relation>. Static analysis cannot bind this target because <one of: the URL is built from a template literal | the target depends on tenant context | the dispatch is via a string registry | the callee is a third-party API archigraph has not catalogued>. If you know the binding, run `/archigraph-repair` to annotate it; the graph will remember on subsequent re-indexes.
```

The phrasing must match this shape so downstream readers (and ADR-0007's doc-as-bridge resolver) can recognise it as an annotated dynamic edge.

## What NOT to do

- Do **not** invent a target entity to make the prose flow nicely. If the static graph doesn't have it and the agent can't repair it, the callout is the right output.
- Do **not** call `archigraph_repairs(action=submit)` with `reasoning` that's just the entity name. Reasoning must be a sentence (R7 in the trust model treats short reasoning as suspicious).
- Do **not** spam the user with questions mid-prose-generation. The user-interactive surface for residuals is Pass 1b. If a residual escaped Pass 1b, this hook either repairs it silently or documents it as runtime-resolved — never breaks out to ask.
- Do **not** modify the writer's existing output template structure. The "Runtime-resolved edge" callout is inserted as an admonition inside whatever section was already going to describe the entity's outbound calls.

## Throughput considerations

Phase 1 of #732 ships the synchronous in-prose hook. On a 5k-residual graph this adds one `archigraph_repairs(action=list)` per entity. Mitigations:

- Cache the per-repo residual list at the start of each writer subagent's run; refresh only if a submit was made.
- Skip the hook for entities whose `from_entity.id` does not appear in the cached residual set — most entities will have zero residuals.
- Batch-submit is out of scope for Phase 1 (see ADR-0015 risks §3).

## Reporting

Writer subagents append a line to their existing pass report:

> `pass-3a hook: <N> entities scanned, <M> residuals seen, <A> auto-repaired, <D> documented as runtime-resolved.`

The orchestrator aggregates these into the final `repair-sweep.md` under a "Generation-time repairs" section.

## Cross-link to patterns

When a pattern's recipe references an entity with unresolved outbound edges, the pattern-record flow (`archigraph_patterns(action=record)`) must run this hook against the exemplar before storing. This prevents pattern exemplars from being authoritative references to dangling targets. See ADR-0018 §"Exemplar integrity" for the broader rationale.

## Invariants

- The hook runs at every "describe-this-entity" boundary. Never skip for performance — the cache makes it cheap.
- Writes go through `archigraph_repairs(action=submit)` only. No direct file writes.
- The hook is idempotent: running it twice on the same entity produces zero new submits because the second pass sees `total == 0` for that `from_entity.id`.
- Source attribution: every submit from this pass uses `source="generate-docs/pass-3a"` so the audit trail distinguishes sweep-time from generation-time repairs.
