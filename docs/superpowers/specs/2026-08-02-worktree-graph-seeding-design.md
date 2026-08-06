# Worktree / branch graph seeding — design

**Status:** approved design, not yet implemented
**Epic:** #5954 (reduce indexing peak RSS and wall time)
**Date:** 2026-08-02
**Baseline commit:** `e43dbeab9`

## Problem

grafel stores **one complete graph per `(repo, ref)` pair**. This is deliberate and documented (`docs/user-guide/multi-branch.md`): *"Each `(repo, ref)` slot stores one `graph.fb` file."*

The consequence is that creating a branch or worktree and indexing it costs a **full index of the entire corpus**, even when a single file differs from its parent.

Epic #5954's driver is concurrent AI agents, each working in its own git worktree, on a 16GB laptop shared with the user's real work. Every one of those agents currently pays the full price.

### Measured, 2026-08-02, `e43dbeab9`, archigraph across 6 worktrees

| N agents | machine peak | per-agent | wall |
|---|---|---|---|
| 1 | 1057 MB | 1057 MB | 43 s |
| 2 | 2185 MB | ~1090 MB | 68 s |
| 3 | 2830 MB | ~1125 MB | 105 s |
| 6 | 6042 MB | ~1010 MB | 195 s |

Peak scales **exactly linearly** at ~1.0–1.1 GB per agent. Per-process peak is flat across N — there is no sharing between refs today, in memory or on disk.

(The apparent sublinearity at N=3 and N=6 is peaks staggering in time, not sharing. It must not be modelled as sublinear.)

The user's framing of the goal, which is the correct one: *"users don't care about serving or engine, they just see that grafel is using half of the machine RAM."* The metric is the sum of all grafel processes at any instant.

## Why seeding is possible at all

> **CORRECTION (2026-08-02, found during implementation).** The paragraph below verified the **wrong function** and its conclusion was false as written. Preserved rather than deleted, because the mistake is instructive: it was treated as the make-or-break fact of the whole design and it was checked against a function the shipping path does not call.
>
> ~~**Entity IDs are stable across refs and worktrees.** `EntityRecord.ComputeID` (`internal/types/entity.go:85`) is `sha256(OrgID + ProjectID + SourceFile + Kind + Name)[:16]`, and `SourceFile` is repo-relative, so a byte-identical file produces identical IDs in parent and child. The absolute worktree path does not enter the hash.~~

`types.EntityRecord.ComputeID` is **not** the function the graph is built with. Every producer calls `graph.EntityID(repoTag, kind, name, sourceFile)` (`internal/graph/graph.go:259`), which hashes **`repoTag` first**, and `Index()` defaults `repoTag` to `filepath.Base(absRepo)` (`cmd/grafel/index.go:603`) — **the worktree directory's own name**.

So by default a worktree's entities land in a *different ID space* from its parent's. A seeded graph would be a hybrid of two ID spaces with every cross-file edge between them dangling — silently.

The #5964 diagnostic probe had already recorded this as `repo_tag_mismatch` and deferred it. It was read and still missed.

**What makes seeding viable is therefore a pin, not a property.** Seeding refuses to proceed without a resolved repo tag, and the tag is persisted in the state dir and honoured on the **full** index path as well — otherwise a seeded graph and a from-scratch graph are not comparable, and the parity gate below would be asserting nothing.

`SourceFile` being repo-relative is still true and still necessary; it is simply not sufficient.

## Prior art — read this before implementing

- **#3652 "clone-from-parent" was attempted and killed.** Its failure mode: it hardcoded `"main"` as the parent ref instead of resolving the parent's actual HEAD.
- **#5964 landed a diagnostic probe only** — its commit message says "zero behaviour change". It resolves and logs `src = StateDirForRepo(parent)` and `dst = StateDirForRepoRef(child, branch)` once per worktree activation, deliberately stopping at instrumentation to size this follow-up. Critically, it resolves the parent's **actual** HEAD ref via `gitmeta.Capture`, specifically to avoid the #3652 bug class.

This design is the intended continuation of #5964, with #3652's landmine already mapped and solved.

## Design

When a worktree or new branch is detected:

1. **Resolve the parent** — parent path and its *actual* current HEAD ref, via the `gitmeta.Capture` path #5964 established. Never assume `main`.
2. **Seed** — copy the parent ref's `graph.<gen>.fb` into the child's state dir, `StateDirForRepoRef(child.Path, child.Branch)`.
3. **Stamp** — record the source `(parent ref, generation)` on the child.
4. **Diff** — the changed file set. This must be **committed diff plus uncommitted working-tree changes**: `git diff --name-only <parentRef>..<childRef>` alone covers only committed history, and an agent's in-progress edits are exactly what is *not* committed. Untracked files that the classifier would index must be included too.
5. **Incrementally index only those files** against the seeded graph.
6. **Write a full `graph.fb`**, exactly as today.

The read path, storage format, cache keys (`GetForRepoRef` / `InvalidateForRepoRef`), tier system, and Pass-4 semantics are **all unchanged**. This is an index-side optimisation with no downstream blast radius — that is the reason for choosing it over the alternatives.

### What it buys, and what it does not

> **MEASURED (2026-08-02).** The table below predicted the primary win would be peak memory. **It is not.** Measured on a 2500-file fixture with a 3-file delta:
>
> | | full index | seed + delta |
> |---|---|---|
> | wall clock | 7.252 s | **1.245 s (0.17×)** |
> | peak live heap | 105.3 MB | **138.8 MB (1.32× — worse)** |
>
> (800-file fixture: heap 1.15×, wall 0.83×. Machine state unchanged across the run.)
>
> **Seeding is O(delta) in work and wall clock, but not in peak memory.** The seeded pass re-extracts only the delta, but still materialises the *whole parent graph* to merge the unchanged portion forward, holding it alongside the document being built — so peak tracks graph size and lands slightly above a full index.
>
> **Consequence for epic #5954:** this change does **not** reduce the ~1.0–1.1 GB per-process budget that the epic's concurrent-agent driver is about. The memory win requires **task #16** (stop the incremental path materialising the prior graph via `LoadGraphFromDir` → `MultiReader`), which is promoted from a nice-to-have to the critical follow-up. C and #16 together give O(delta) in both dimensions; C alone gives it in one.

| | predicted | actual |
|---|---|---|
| Indexing peak per new ref | O(delta) instead of O(corpus) | **1.32× a full index — no win** |
| Wall time per new ref | ~43 s → seconds | **0.17× — confirmed** |
| Disk per ref | unchanged — still a full snapshot | unchanged |
| Serving RSS | unchanged — still N full graphs resident | unchanged |

Disk and serving remain linear in N. Fixing those requires the overlay design recorded below.

## Failure modes

Seeding introduces a way to be **silently wrong**: if the diff is computed against the wrong parent ref, or the parent's graph is stale, the child's graph contains entities from code that is not in the child's tree. That is worse than being slow, and it is invisible.

This is the defect class that produced #6070 (install broken for two weeks behind an untriaged red), #6085, and five successive scheduler defects. The design must not treat the seed optimistically.

**The seed must be provably tied to the parent generation it came from.** The recorded `(parent ref, generation)` stamp is verified before the seeded graph is used. If verification fails, fall back to a full index.

### Fallback policy — decided

Fall back to a full index, **and log a visible reason**: parent not indexed / generation moved / diff failed / stamp mismatch.

A silent fallback is rejected. A seed that systematically never fires would be invisible, and the result would look like normal behaviour while delivering none of the benefit. This repo lost two weeks to exactly that shape — a red CI leg that everyone learned to read as "just broken".

## Correctness gate — decided

**A parity test: index a branch both ways — seeded and from scratch — and assert the graphs match semantically.**

This is the only check that can prove seeding neither lost nor invented anything.

Two constraints on how it is written, both learned the hard way:

- **It must compare semantic digests (entity and relationship sets), not bytes.** Per #6083, whole-file `cmp` on `graph.fb` is an invalid instrument: two runs of the same binary can differ by ~86.6 MB because a single length change cascades through every subsequent flatbuffer offset.
- **Counts alone are insufficient.** A bug that loses N edges and invents N others passes a count assertion. #6085 is itself a count bug, which demonstrates the point.

## Dependencies

| Issue | Relationship |
|---|---|
| **#6085** | **CLEARED — merged as `2d8d59110`.** Was blocking: the incremental path dropped ~348k of 620k relationships on identical source. Two defects, both fixed: a staleness predicate that was wrong in *both* directions, and relationship identity never being persisted through `graph.fb`. Converged incremental now matches a full reindex exactly — 0 missing relationships, 0 missing entities, 0 extra entities. Seeding rides on this path, so it had to land first. |
| **#6088** | **RETRACTED — not a defect.** `Index` treats `--out` as a *file* path throughout, so `filepath.Dir` is correct, and the daemon never passes `--out` at all. The collision originally attributed to it was a benchmark-harness artifact. No dependency. |
| **#6087** | **Improved by this design.** `DetectRenames` goes quadratic on a *dissimilar* prior graph. Seeding makes the prior graph nearly identical — the fast case. It should still be bounded independently. |
| **Task #39** | **Overlaps.** "Incremental indexing audit — uncommitted-edit freshness." The changed-file set above must include uncommitted and untracked changes; a seeded index that only honours committed history would serve an agent stale results for its own in-progress work, which is the single most common thing an agent asks about. Whatever #39 concludes about freshness applies directly here. |

## Recorded for later: the overlay design (approach B)

Not chosen now, kept so it is not rediscovered.

A ref would become `base-pointer + delta + tombstones`, read through an overlay reader. This would cut disk and serving RSS as well as indexing, and its strongest single argument is that **a shared, read-only mmap of one base file is backed by one set of physical pages across all processes** — so N agents would share the base for free. Today every ref is a different file, so none of that sharing happens.

Two findings that bound it, established while evaluating C:

- **Pass-4 results are stored in two places.** Per-entity and inline: `pagerank: double` and `is_articulation_point: bool` (`internal/graph/schema/graph.fbs:44,48`). Root-level and whole-graph: `communities`, `louvain_modularity`, `num_articulation_points`, `denoised_communities` (`graph.fbs:110-116`). An overlaid base entity therefore **carries the base's pagerank and articulation flag into the branch**. Branches would serve approximate algorithm results unless Pass-4 is recomputed — and recomputing it is the expensive part the design exists to avoid. This is a semantic decision that must be made explicitly, not inherited by accident.
- Base generations must be pinned against GC, and a long-lived branch's delta grows until it needs re-baselining. Related machinery exists: #6080 records that generation N-1 is already retained.

### The pass-through trap (B only)

The intuitive framing — *"a child's queries just go to the base transparently, so a base reindex doesn't affect it"* — is **half right and half a correctness bug**, and the wrong half is silent.

A child forks from base generation 5 and expresses its delta against gen 5. If `main` then lands commits and reindexes to gen 6, a child that resolves reads against *"the base"* now resolves against **gen 6 — code that does not exist in the child's working tree**. The agent is told about functions absent from its checkout and call edges from code it cannot see; entities the child deleted can reappear if gen 6 reintroduced them. The agent cannot detect any of this, precisely *because* the abstraction is opaque to it.

**The rule: a child pins the exact base generation it forked from, and a base reindex must not move it.** The child is unaffected by immutability, not by pass-through. "Transparent" must mean *the agent never sees the mechanism* — never *the child follows the latest base*.

Two consequences, which are the real cost of B:

- **GC becomes reference-aware.** Base generations cannot be collected while any child pins them. Today `GCStaleGens` retains N-1 by a fixed rule; under B, retention is refcounted by live children, and a long-lived branch pins an old base indefinitely.
- **Re-baselining becomes a first-class operation.** When a branch merges or rebases onto newer `main`, it must move to the new base and recompute its delta against it. That is the moment a "cheap" branch pays its cost, and the policy for when it happens is a design decision in its own right.

The agent-facing contract is the fixed point in all of this: a subagent knows only that it is querying *a graph*. It has no concept of base, delta, generation, or pin. Any design that requires the agent to understand or react to those has failed, and any design that lets the mechanism leak wrong answers into an agent's view has failed worse — because the agent will act on them confidently.

Approach A (content-addressed per-file segments, a ref being a manifest of content hashes) remains the strongest dedup — it would share across refs *and* across repos with vendored code, with GC by refcount — at the cost of a new storage format and a migration.

## Testing

- **Parity**: seeded vs from-scratch index of the same branch, semantic digest equality. The primary gate.
- **Fallback**: each rejection path (parent unindexed, generation moved, diff failure, stamp mismatch) produces a correct full index *and* its logged reason.
- **Stamp verification**: a mutation that accepts a seed whose recorded generation does not match must fail a test.
- **The parent-ref resolution**: a test pinning that the parent's *actual* HEAD ref is used, not `main` — the regression guard for #3652.
- **Peak measurement**: seeded index peak is O(delta). Measured with the live-heap metric (`next_gc / (1 + GOGC/100)`), not RSS — macOS RSS counts `MADV_FREE` pages and is an upper bound, not a footprint.
