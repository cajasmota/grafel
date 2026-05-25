# Pass 3a — Generation-time repair (in-prose hook)

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use archigraph MCP tools:
`archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`,
`archigraph_clusters`, `archigraph_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `archigraph_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `archigraph_whoami` before doing anything else in this pass. If it errors:
ABORT with: "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."

## CRITICAL STORAGE DISCIPLINE
===========================
All generated documentation MUST be written under:
  `~/.archigraph/docs/<group>/...`

Determine `<group>` via the `archigraph_whoami` MCP call (the Pre-flight assertion
above). Pass it through every subsequent file write as `${OUTPUT_ROOT}`.

You are STRICTLY FORBIDDEN from writing documentation files into:
- The source repo's working tree (anywhere under `<repo>/docs/`, `<repo>/doc/`, etc.)
- The CWD unless CWD is already inside `~/.archigraph/docs/<group>/`
- Any path that is a git working directory

If you find yourself about to write to a repo path, STOP. The skill assumes
the archigraph-owned store. Writing elsewhere breaks the storage contract
and pollutes the user's source repo.

The daemon dashboard reads from `~/.archigraph/docs/<group>/` -- any output
written elsewhere is invisible to it.

### Pre-flight storage assertion -- SECOND action in this pass

Compute and verify the output root immediately after the `archigraph_whoami` call:

```bash
OUTPUT_ROOT="$HOME/.archigraph/docs/<group>/"   # substitute <group> from whoami
mkdir -p "$OUTPUT_ROOT"
echo "OUTPUT_ROOT=$OUTPUT_ROOT"
```

All file writes in this pass MUST use `${OUTPUT_ROOT}<relative-path>`. Never write to any
other location. If `mkdir -p` fails, ABORT: "Cannot create output directory at $OUTPUT_ROOT."
## CRITICAL OUTPUT DISCIPLINE
==========================
The generate-docs skill produces markdown files in the canonical store
at `~/.archigraph/docs/<group>/`. It does NOT produce:
- VitePress / Docusaurus / Sphinx / mkdocs scaffolding
- `package.json` or any build manifests for static site generators
- Any non-markdown asset that wraps the docs for publishing
- `.gitignore` entries

Publishing is downstream — handled by the archigraph dashboard or
external tooling. If you find yourself about to write a `config.ts`,
`package.json`, `mkdocs.yml`, `.vitepress/config.ts`, or any build
manifest, STOP. The skill's job is content, not infrastructure.




---


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

## Repair candidate emission (DocgenRepairCandidate)

The Pass 3a hook is also the primary site for **docgen repair emission** per
`snippets/docgen-repair-emission.md`. The hook's repair submissions go through
`archigraph_repairs(action=submit)` (the existing channel). But when the hook
sees facts about an entity that are broader than the residual being acted on —
for example, while resolving a residual for `OrderService` you notice that a
_separate_ `UNRESOLVED` stub elsewhere in the subgraph resolves to a visible
import — emit those broader facts as `DocgenRepairCandidate` entries appended to
`~/.archigraph/groups/<group>/docgen-repairs.jsonl`.

Use `source: "generate-docs/pass-3a"` in all candidates emitted from this hook.

Confidence rules: same as `snippets/docgen-repair-emission.md`. The two
channels complement each other:
- `archigraph_repairs(action=submit)` — fixes the residual in the daemon
  immediately (this pass always did this).
- `docgen-repairs.jsonl` append — feeds the post-run `archigraph_apply_docgen_repairs`
  batch for fidelity tracking and for repairs that touch entities outside the
  current residual set (e.g. kind mis-classifications, external library labels).

Do NOT double-count: if you already submitted a repair via
`archigraph_repairs(action=submit)`, you MAY also emit the same observation to
`docgen-repairs.jsonl` — the daemon deduplicates. This lets the fidelity delta
capture every discovery regardless of which path applied it.

## Reporting

Writer subagents append a line to their existing pass report:

> `pass-3a hook: <N> entities scanned, <M> residuals seen, <A> auto-repaired, <D> documented as runtime-resolved, <E> DocgenRepairCandidates emitted.`

The orchestrator aggregates these into the final `repair-sweep.md` under a "Generation-time repairs" section.

## Cross-link to patterns

When a pattern's recipe references an entity with unresolved outbound edges, the pattern-record flow (`archigraph_patterns(action=record)`) must run this hook against the exemplar before storing. This prevents pattern exemplars from being authoritative references to dangling targets. See ADR-0018 §"Exemplar integrity" for the broader rationale.

## Invariants

- The hook runs at every "describe-this-entity" boundary. Never skip for performance — the cache makes it cheap.
- Writes go through `archigraph_repairs(action=submit)` only. No direct file writes.
- The hook is idempotent: running it twice on the same entity produces zero new submits because the second pass sees `total == 0` for that `from_entity.id`.
- Source attribution: every submit from this pass uses `source="generate-docs/pass-3a"` so the audit trail distinguishes sweep-time from generation-time repairs.

---

**[pass-03a telemetry]** Print at end of this pass:
```
[pass-03a] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
