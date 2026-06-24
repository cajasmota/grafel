# grafel-patterns-discover

Standalone agent skill for pattern discovery without doc generation. Use when the user wants a fresh pattern audit of an indexed group but does NOT want `/generate-docs` to regenerate prose.

This skill is the secondary entry point described by [ADR-0018](../../docs/adrs/0018-agent-learned-patterns.md); the primary one is `/generate-docs` Phases 1 + 4.

## When to use this skill

- "Refresh my pattern store."
- "Audit our codebase conventions without touching the docs."
- "Did anything new converge since the last `/generate-docs` run?"

Do NOT invoke this skill on first install when no group is registered, when `grafel status` reports indexing in progress, or as part of `/generate-docs` (that path runs its own Phase 1 + 4 already).

## Inputs the skill expects

- A registered group resolvable via `grafel_orient (view=me)`.
- An up-to-date `<repo>/.grafel/graph.json` per repo. If any repo's graph is stale, stop and tell the user to run `grafel rebuild <group>` first.
- Read access to `<group>/.grafel/patterns.json` (the pattern store) and `patterns-config.json` (thresholds — defaults applied when absent).

## Procedure

### Step 1 — Slice the codebase

Dispatch one subagent per top-level module cluster (as returned by `grafel_orient (view=clusters)`). Each subagent receives:

- Its cluster's entity ID set.
- A read-only copy of the existing pattern store (so it can skip shapes already covered).

Subagents do NOT share state during Step 1.

### Step 2 — Per-subagent discovery (Phase 1 in ADR-0018 terms)

Each subagent scans its slice for structural recurrences:

1. Group entities of the same kind that share file-path prefix, naming convention, or annotation set.
2. If a candidate shape appears ≥ `per_subagent_threshold` times (default 2) within the slice, emit a `PatternCandidate` via `grafel_patterns(action=record, as_candidate=true, ...)`.
3. Include all observed entities as exemplars in the candidate payload — the coordinator needs the exemplar set for clustering.

A subagent may emit zero candidates. That is the expected outcome for slices already covered by approved patterns.

### Step 3 — Convergence + promotion (Phase 4)

The coordinator (this skill) runs after all subagents return:

1. Load every `PatternCandidate` (`is_candidate=true`) via `grafel_patterns(action=query, include_candidates=true)`.
2. Cluster by trigger similarity ≥ `cluster_similarity_threshold` (default 0.8) AND at least one overlapping exemplar.
3. For each cluster with ≥ `convergence_threshold` (default 3) distinct proposer subagents, call `grafel_patterns(action=promote, candidate_id=<id>)`.
4. Surface the promoted candidates to the user. The user's approval flips `is_candidate=false`.

Candidates that do not converge stay in the store with `is_candidate=true`. They may converge in a future run; `grafel patterns gc` prunes them after `candidate_decay_days` (default 90) if they go stale.

### Step 4 — Report

Print a structured summary:

- Patterns proposed: `<n>` candidates from `<m>` subagents.
- Patterns promoted: `<k>` (those reaching the convergence threshold).
- Patterns rejected by user: `<j>`.
- Patterns still pending: `<p>` (carried over to next run).

## Constraints

- Never call `grafel_patterns(action=record, as_candidate=false)` from this skill. Direct creation bypasses convergence and is reserved for the agent-task-observation path in ADR-0018.
- Never invoke `/generate-docs` from this skill. That is the primary entry point; chaining them deduplicates the Phase 1 scan unnecessarily.
- Never silently delete candidates. Stale-pruning is a separate operation under the user's control (`grafel patterns gc`).

## Related

- `/generate-docs` — primary pattern-discovery entry point.
- `/grafel-patterns-sync` — export the store to CLAUDE.md after discovery.
- `grafel patterns list` — inspect what landed.
