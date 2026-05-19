# ADR-0018: Agent-learned patterns

- **Status**: Proposed
- **Date**: 2026-05-19
- **Deciders**: Jorge Cajas
- **Related**: ADR-0004 (single MCP process / routing cascade), ADR-0007 (doc-as-bridge), ADR-0015 (residual repair / enrichment loop), ADR-0017 (daemon architecture)

## Context

Agents working in a codebase today have no persistent, grounded memory of codebase-specific conventions. Every new session rediscovers the same facts:

- Which middleware pattern this service uses for HTTP handlers.
- Which test fixture factory goes with which database layer.
- Which config file must be updated when adding a new language to the pipeline.

The existing memory surfaces fall short in different ways:

- **CLAUDE.md / AGENTS.md / .cursorrules** — human-authored, stale quickly, not grounded in actual code exemplars, not queryable via the graph.
- **`archigraph_find` + `archigraph_related`** — excellent for entity lookup, but have no concept of a *recipe*: an ordered set of steps to perform when the agent needs to add, modify, or delete a class of thing.
- **`archigraph_enrichments` findings field** (per-group unstructured memory from ADR-0015's enrichment loop) — captures one-off notes per entity; no lifecycle, no scope filtering, no discovery by task description, no reinforcement.

The result is that agents repeat mistakes already observed, skip steps already learned, and produce diffs that contradict patterns already established in the codebase — forcing human correction that could have been avoided.

This ADR adds **Patterns**: first-class graph entities that store codebase-specific recipes, link to real code exemplars, and improve automatically as agents apply and correct them.

## Decision

Patterns are first-class graph entities. They are stored per-group (alongside enrichment-resolutions.json and repair.json), queryable via the same BM25 + graph-traversal machinery already used by all other archigraph tools, and managed through a single new MCP tool (`archigraph_patterns`) with five actions.

The architectural rule: **the indexer owns structure that is statically visible; the agent owns recipes that require task-level context.** Patterns are the agent's side of that divide — not inferred from ASTs, but learned from observation and reinforced over time.

### MCP tool surface

One new tool, `archigraph_patterns`, with an `action` argument matching the shape already established by `archigraph_enrichments`, `archigraph_cross_links`, and `archigraph_repairs`:

| Action  | Description |
|---------|-------------|
| `query`  | Find patterns applicable to a task description and caller context. Returns ranked matches with steps, anti-patterns, exemplar links, and confidence. |
| `record` | Create a new pattern from observed work. Takes `as_candidate: bool` arg. `as_candidate=true` emits a `PatternCandidate` (subagent path). `as_candidate=false` creates a Pattern directly (agent-task path). Requires at least one exemplar (entity ID in the group's graph). Scope auto-derived from exemplars unless overridden. |
| `refine` | Update an existing pattern's steps, anti-patterns, or scope without changing its confidence. For corrections that don't reflect a success or failure. |
| `apply`  | Mark that a pattern was applied to a task. Records success/failure; updates confidence and `last_applied`. Writes a `CREATED_BY` edge from the produced entity back to the pattern. |
| `reject` | Flag a pattern as stale or wrong. Decreases confidence; does not delete — pattern is retained for audit and may be refined. |
| `promote` | Coordinator-driven action invoked during Phase 4 aggregation. Takes a `candidate_id`; sets `is_candidate=false` and surfaces to user for final approval. |

No other tools are modified. The routing cascade from ADR-0004 (explicit `group` arg → CWD walk → singleton fallback) applies to `archigraph_patterns` identically to every other tool.

### Pattern entity schema

```
Pattern {
  id:   string                   // sha256(group + trigger.natural_language)[:16]
  kind: "Pattern"

  trigger: {
    natural_language:  string    // free-text description of when to apply
    keywords:          []string  // BM25 index terms
    target_entity_kinds: []string // optional: "Function", "Class", "Service", …
  }

  steps: []string                // ordered recipe steps (markdown prose OK)

  anti_patterns: [{
    do_not:  string
    reason:  string
    private: bool                // if true: excluded from CLAUDE.md export
  }]

  scope: {
    repos:         []string      // empty = all repos in group
    module_paths:  []string      // empty = all paths
    languages:     []string      // empty = all languages
    stacks:        []string      // e.g. "go/chi", "python/django", "ts/react"
    entity_kinds:  []string      // what the pattern produces
  }

  category: "code" | "process" | "team" | "tooling" | "architecture"

  confidence:      float         // [0.0, 1.0]
  observations:    int
  last_validated:  timestamp
  last_applied:    timestamp
  documentation_url: string      // optional: pointer to the doc section this pattern maps to (written by /generate-docs integration)

  is_candidate:        bool         // true until convergence + user-approval; default true on record(as_candidate=true)
  convergence_count:   int          // number of independent subagents that proposed this candidate
  proposer_subagents:  []string     // identifiers of subagents that contributed observations (for audit)
}
```

Storage: JSON at `<group>/.archigraph/patterns.json` (same directory convention as `enrichment-resolutions.json` and `repair.json`). Migration to FlatBuffers follows ADR-0016's phase model — not required for v1.

### Edge types

Eight new edge types are added to the graph schema:

**Outgoing from Pattern:**

| Edge kind       | Target        | Written by   | Meaning |
|-----------------|---------------|--------------|---------|
| `EXEMPLAR`      | Entity        | `record`     | A real code example of this pattern in use. |
| `TOUCHES`       | Entity        | `record`     | An entity the pattern's steps read or modify (broader than EXEMPLAR). |
| `ANTI_EXEMPLAR` | Entity        | `record`     | A real code example of the anti-pattern; here's what not to do. |
| `SUPERSEDES`    | Pattern       | `record`     | This pattern replaces an older one. |
| `CONFLICTS_WITH`| Pattern       | `record`     | These two patterns cannot both be applied to the same target. |
| `CO_APPLIES_WITH`| Pattern      | `record`     | These two patterns are typically applied together. |
| `PREREQUISITE`  | Pattern       | `record`     | This other pattern must be satisfied before this one can apply. |

**Incoming to Pattern:**

| Edge kind    | Source  | Written by | Meaning |
|--------------|---------|------------|---------|
| `CREATED_BY` | Entity  | `apply`    | This entity was produced using the linked pattern. Enables reverse navigation: given any entity, find the recipe that created it. |

All eight kinds are append-only additions to the schema's edge-kind enum (same invariant as the FlatBuffers field-ID discipline in ADR-0016).

### Scope matching

A pattern matches a query if **all present scope constraints are satisfied** by the caller's context. An absent constraint is a wildcard.

Caller context is derived automatically from CWD via the ADR-0004 routing cascade. The agent may pass an explicit `scope` override in the `query` action to narrow or broaden the match.

**Ranking when multiple patterns match:**

1. **Specificity** — count of non-empty scope fields. More fields = more specific = ranked higher.
2. Within the same specificity: `confidence × recency_score` where `recency_score = 1.0 / (1 + days_since_last_applied / 30)`.
3. Same again: surface as a list and let the agent choose. Do not auto-select in silence.

### Default scope on record

Narrow by default. Scope is auto-derived from the exemplar entities passed to `record`:

- All exemplars in the same repo → `scope.repos = [that repo]`.
- All exemplars share a common path prefix → `scope.module_paths = [that prefix]`.
- All exemplars are the same language → `scope.languages = [that language]`.
- Stack detection: a lightweight inference pass over the exemplars' import graph (already in memory) tags the stack. Agent may override.

To intentionally create a broad pattern, the agent passes `scope.repos=[]` (or any empty field) to clear the auto-derived constraint.

### Discovery: doc-generation integration

Pattern detection has three entry points:

1. **Primary — doc-generation skill (`/generate-docs`).** The doc-gen pass dispatches multiple subagents over disjoint codebase slices. No single subagent sees enough samples to call a pattern with confidence. The phased workflow uses convergence across subagents as the signal:

   - **Phase 1 (discovery, per subagent):** each subagent scans its assigned slice, noting structural recurrences. If a subagent sees ≥`per_subagent_threshold` (default 2) instances of a candidate shape within its slice, it emits a `PatternCandidate` via `record(as_candidate=true)`.
   - **Phase 2 (domain Q&A):** unchanged.
   - **Phase 3 (generation):** write docs.
   - **Phase 4 (aggregation + promotion, new):** the coordinator runs a convergence pass:
     1. List all candidates emitted during Phase 1.
     2. Cluster by similarity (BM25 cosine on trigger text ≥ `cluster_similarity_threshold`, default 0.8; min one overlapping exemplar).
     3. For each cluster with ≥ `convergence_threshold` (default 3) independent proposer subagents, merge into a single candidate (union of exemplars, best-scored trigger text) and call `promote(candidate_id)`.
     4. Surface promoted candidates to the user for final approval. User approval flips `is_candidate=false`.
   - **Phase 5 (cross-link, new):** approved patterns receive `documentation_url` populated with the matching doc-section anchor.

   Non-convergent candidates persist with `is_candidate=true` for future runs; they may converge in the next doc-gen cycle. The `archigraph patterns gc` command (v1.1) prunes candidates older than `candidate_decay_days` (default 90).

2. **Secondary — `/archigraph-patterns-discover` standalone skill.** Same detection logic as Phases 1 + 4, no doc emission. For users refreshing patterns without regenerating docs.

3. **Tertiary — agent-task observation.** When an agent completes a task and `query` returned no applicable pattern, it can call `record(as_candidate=false)` directly to formalize what it just did. Sample size of one, immediacy of now. The most fragile path; relies on the agent's own confidence rather than convergence.

The three entry points coexist. Doc-gen is primary because convergence across slices is the strongest signal and the exploration cost is already paid.

### Confidence model

| Event                      | Delta    | Notes |
|----------------------------|----------|-------|
| New pattern created        | +0.0     | starts at 0.4 |
| `apply(success=true)`      | +0.10    | cap at 1.0 |
| `apply(success=false)`     | −0.15    | |
| `reject`                   | −0.30    | |
| `refine`                   | none     | refinement is neutral on confidence |
| Time decay                 | −0.05 / 30 days since `last_applied` | floor at 0.2 |

**Silent-apply threshold** (agent may apply a pattern without surfacing it to the user when confidence is at or above this value; configurable per group):

| Category                  | Default threshold |
|---------------------------|-------------------|
| process / architecture    | 0.8               |
| code / tooling            | 0.65              |
| team                      | 0.5               |

Below threshold the agent surfaces the pattern and asks for confirmation before applying.

### Configuration

Tunable defaults via `archigraph patterns config <key>=<value>`:

| Setting                          | Default | Purpose |
|----------------------------------|---------|---------|
| `per_subagent_threshold`         | 2       | Minimum observations within a single subagent's slice before emitting a candidate |
| `convergence_threshold`          | 3       | Minimum independent subagents required to promote a candidate to a Pattern |
| `cluster_similarity_threshold`   | 0.8     | BM25 cosine threshold for clustering similar candidates during aggregation |
| `candidate_decay_days`           | 90      | Auto-prune non-convergent candidates after this many days (via `gc` subcommand) |
| `silent_apply_threshold.<category>` | varies (see Confidence model) | Per-category confidence threshold for silent apply vs prompt-to-confirm |

### Lifecycle flows

**Flow 1 — Discover on task completion.**
Agent receives a task, queries `archigraph_patterns(action=query, ...)` and gets no match. Agent performs the work. After the diff lands successfully, agent calls `archigraph_patterns(action=record, ...)` with the produced entities as exemplars. Pattern starts at confidence 0.4.

**Flow 2 — Reinforce on successful apply.**
Agent queries and finds a matching pattern. Follows the steps. Task succeeds. Agent calls `archigraph_patterns(action=apply, pattern_id=..., success=true, produced_entity_id=...)`. Confidence ratchets up; `CREATED_BY` edge written from the new entity to the pattern.

**Flow 3 — Refine on correction.**
Human reviews the diff and corrects a step. Agent calls `archigraph_patterns(action=refine, pattern_id=..., steps=[...updated...])`. Confidence unchanged — refinement reflects learning, not failure.

**Flow 4 — Reject stale pattern.**
Agent applies a pattern, task fails because the codebase has moved on. Agent calls `archigraph_patterns(action=apply, success=false)` (confidence −0.15). If the agent determines the pattern is fundamentally wrong, it follows up with `archigraph_patterns(action=reject, pattern_id=..., reason=...)` (confidence −0.30 further). Pattern is retained for audit; confidence decay will eventually drop it below the discovery threshold.

### Skills and CLAUDE.md sync

Two skill files (markdown, loaded by the agent host's skill mechanism):

- **`/archigraph-patterns-discover`** — opt-in capture. Agent scans recent work in the current group, identifies recurring action sequences, and proposes candidate patterns for the owner to approve before `record` is called.
- **`/archigraph-patterns-sync`** — diff and merge with version-controlled `CLAUDE.md` / `AGENTS.md`. Patterns are exported inside a marker-wrapped region:
  ```
  <!-- archigraph:patterns:start v=1 -->
  ...generated pattern summaries...
  <!-- archigraph:patterns:end -->
  ```
  The marker convention follows the gfleet pattern established in AI-Memory. Private anti-patterns (`anti_patterns[].private=true`) are **never exported** — the sync skill must enforce this as a hard constraint.

## Consequences

### Positive

- Patterns are grounded in real code: every recipe links to actual exemplars in the graph, not to human prose.
- Self-correcting: confidence rises with successful applies, falls with failures and rejections, decays with disuse.
- Per-scope precision: a pattern scoped to `go/chi` never fires in a Django module. False matches across stacks/repos/paths are structurally impossible once scope is set correctly.
- Reverse navigation: given any entity, `archigraph_related(entity_id)` can now surface `CREATED_BY` → Pattern → `EXEMPLAR` → peer entities. Agents can understand *why* code was written the way it was.
- Free reuse of BM25 + graph traversal: no new query infrastructure. `archigraph_patterns(action=query)` delegates to the same index the other tools use.
- Lightweight bootstrap: no upfront scrape or migration. Pattern count is zero on first install; the corpus grows organically as agents work.
- Complementary to ADR-0015 repair: repair closes structural graph gaps; patterns close recipe gaps. Neither replaces the other.

### Negative

- Adds 1 entity kind (`Pattern`) and 8 edge kinds to the schema. Under ADR-0016's FlatBuffers discipline these are append-only enum additions — safe, but they grow the schema surface.
- Stack detection is a new lightweight inference layer. It reuses existing import-graph data but requires a small new pass. Incorrect detection silently narrows scope too aggressively; mitigated by `query` returning the matched scope fields for inspection.
- Anti-pattern export sanitization is a hard requirement. A `private=true` anti-pattern leaking to CLAUDE.md would expose internal team conventions. The `/archigraph-patterns-sync` skill must enforce this as a hard constraint. **`private=true` means "never export to version-controlled files" — it does not affect `query` visibility.** All agents connected to the daemon see all patterns. The privacy boundary is the on-disk version-controlled artifact, not the in-memory query surface.
- Per-pattern confidence is harder to reason about than a single global threshold. Two patterns covering the same task may have wildly different confidence values. Mitigated by surfacing both and letting the agent (or user) choose when rankings tie.
- `patterns.json` grows unboundedly on long-lived groups. A future `archigraph patterns gc` command (out of scope for v1) prunes patterns below a configurable floor confidence and not applied in N days.

### Neutral

- Patterns are per-group, matching the existing scope convention for enrichments, repairs, and cross-links.
- Storage is JSON initially (same as enrichments). Migration to FlatBuffers is out of scope for v1 but follows the same phase model as ADR-0016.
- The `archigraph_patterns` tool follows the same action-arg shape as `archigraph_enrichments` and `archigraph_repairs`. Agents already familiar with those tools acquire this one with minimal additional context.

## Alternatives considered

- **Patterns as unstructured findings inside `archigraph_enrichments`.** Rejected: findings are keyed to a single entity; patterns describe multi-entity recipes. There is no `expand` path from a finding to related code; no scope filtering; no reinforcement loop.
- **Patterns in CLAUDE.md only (human-authored).** Rejected: human-authored docs are the current state. They stale quickly, are not queryable, and are not grounded in current exemplars. CLAUDE.md sync (via `/archigraph-patterns-sync`) is an *output* of this feature, not a replacement for it.
- **Patterns as a global (cross-group) library.** Rejected for v1: privacy and scope concerns. A team anti-pattern should not leak to another group's query results. Revisit in v1.1 when there is evidence of genuinely cross-codebase reusable patterns.
- **Patterns embedded as entity properties (no new entity kind).** Rejected: patterns describe recipes across multiple entities. Attaching a recipe to a single entity produces an unstructured blob with no traversal path to the other entities involved, no relationship edges, and no confidence lifecycle.
- **Single unified curation tool (`archigraph_curate(target=enrichment|link|repair|pattern, ...)`).**  Considered as a way to unify all four curation flows under one tool. Rejected: the action-arg sets for each flow differ substantially (e.g. `record` requires exemplar entity IDs and scope; `submit_repair` requires `edge_id` and `resolution`). A unified schema either mandates many optional fields or silently ignores arguments that don't apply to the selected target — both are worse than four narrow tools.

## Implementation sequence

Do not queue these PRs until the current HTTP overhaul and the Java/Python chain-fix-2 work land.

| PR  | Scope |
|-----|-------|
| α   | Pattern entity kind; per-group `patterns.json` storage; schema only (no MCP wiring). |
| β   | `archigraph_patterns` MCP tool: `query` and `record` actions; BM25 index integration; scope derivation from exemplars. |
| γ   | Lifecycle: `refine`, `apply`, `reject`; confidence model; time decay; `CREATED_BY` edge write on apply. |
| δ   | Skills: `/generate-docs` integration (Phase 4 + 5 added — pattern proposal + cross-link); `/archigraph-patterns-discover` standalone; `/archigraph-patterns-sync` for CLAUDE.md export. Private anti-pattern sanitization. `archigraph patterns` CLI subcommand. |

## Open questions

1. ~~**Stale-pattern GC**~~ — **RESOLVED 2026-05-19**: Retain stale patterns as dormant rather than auto-pruning. Emit a count of dormant patterns in `archigraph status`. Add a `archigraph patterns gc` CLI subcommand in v1.1 if user-driven pruning is needed. Avoids accidental loss of patterns the agent may still want to revive.
2. ~~**Conflicting pattern detection**~~ — **RESOLVED 2026-05-19**: When `record` creates a pattern that overlaps scope with an existing one for the same `target_entity_kinds`, auto-write a `CONFLICTS_WITH` edge between the two and surface the conflict in the `record` response so the agent (or user) can explicitly reconcile via `refine` or `reject`. Do not block the record.
3. ~~**Private anti-pattern visibility scope**~~ — **RESOLVED 2026-05-19**: `private=true` means "never export to version-controlled files (CLAUDE.md / AGENTS.md)". It does not affect `query` visibility. All agents connected to the daemon see all patterns regardless of the `private` flag. The privacy boundary is the on-disk version-controlled artifact, not the in-memory query surface. Caller-identity ACL semantics are explicitly out of scope.
4. ~~**Pattern versioning**~~ — **RESOLVED 2026-05-19 (v1 + v1.1 sequenced)**: v1 ships last-writer-wins. `refine` mutates in place. The only history is whatever lands in CLAUDE.md exports (tracked by git). v1.1 (when a real use case emerges) lights up the existing `SUPERSEDES` edge for major refines: `refine` with `major=true` creates a NEW Pattern with `SUPERSEDES → old_pattern`. Old pattern stays in the graph, demoted in query ranking. New lifecycle actions `history(id)` (traverses `SUPERSEDES`) and `rollback(id, to=old_id)` (creates a new pattern copying old state, supersedes current). No schema change required — the `SUPERSEDES` edge is already defined in v1; v1.1 only adds the lifecycle actions.
