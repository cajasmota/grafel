# ADR-0020: Multi-branch and worktree graph snapshots

- **Status**: Accepted (2026-05-26)
- **Date**: 2026-05-26
- **Deciders**: Jorge Cajas
- **Related**: ADR-0017 (daemon architecture), ADR-0016 (binary graph format), ADR-0009 (cross-repo ID namespacing), epic #2087, #2098
- **Shipped in**: #2126, #2129, #2132, #2137, #2138, #2139, #2143, #2144, #2146, #2148, #2158, #2219, #2220, #2221, #2222, #2223, #2224

## Context

Until this ADR, the grafel daemon indexed exactly one git ref per registered
repository — whichever ref was current when the last `grafel index` or
`grafel rebuild` ran. This design has two concrete problems.

**Branch-switch context loss.** Developers work on feature branches,
hotfixes, and release cuts simultaneously. When a developer runs `git
checkout feature/foo`, the graph stored on disk still reflects the previous
branch. Any MCP query after the switch returns stale structure — entities
that no longer exist in the working tree, or missing entities that the
branch introduces. The agent has no way to know the graph is stale without
manually triggering a full reindex.

**Worktree blindness.** `git worktree add` creates a parallel checkout of the
same repo at a different path, allowing concurrent work on two branches. The
daemon had no concept of linked worktrees: it indexed the primary checkout
only, and any work done inside a linked worktree was invisible to MCP queries
until the user manually indexed the secondary path as a separate group.

The product target is install-and-forget (ADR-0017): the graph stays correct
without manual intervention. The single-ref design cannot meet that target for
multi-branch workflows.

The implementation was delivered as a phased epic (#2087) covering storage
layout (PH1a–c), LRU tiered hibernation (PH2, PH2a), worktree discovery (PH3),
dashboard UI (PH4), graph diff view (PH5), disk eviction (PH6), clone-from-parent
(PH7), and embedding deduplication (PH8). The CLI `--ref` surface and dashboard
`?ref=` query support shipped as a follow-on epic (#2098).

## Decision

**Index and retain one graph snapshot per `(repoPath, ref)` pair, stored in a
per-ref sub-directory, with an LRU + tiered hibernation policy to bound RAM and
disk usage.**

### Storage layout

Per-ref artifacts live under:

```
~/.grafel/store/<slug>-<hash>/refs/<ref-safe>/graph.fb
~/.grafel/store/<slug>-<hash>/refs/<ref-safe>/graph.json  (opt-in)
~/.grafel/store/<slug>-<hash>/refs/<ref-safe>/graph-stats.json
~/.grafel/store/<slug>-<hash>/refs/<ref-safe>/embeddings.bin
```

`<ref-safe>` is the ref name percent-encoded with `%2F` for `/` so that branch
names like `feature/foo` map to a valid directory name on all platforms
(`feature%2Ffoo`). Encode/decode is provided by `RefSafeEncode` /
`RefSafeDecode` in `internal/daemon/state_path.go`.

`StateDirForRepo` (the pre-ADR-0020 helper that resolved to the flat-layout
slot) is now a thin wrapper that reads the current HEAD via `gitmeta.Capture`
and delegates to `StateDirForRepoRef(repoPath, ref)`. All indexer write paths,
reader paths (MCP, dashboard, doctor, status, watch, links), and
enrichment/repair persistence go through these helpers.

Legacy flat-layout slots (`graph.fb` at the top level of a `<slug>-<hash>/`
directory) are migrated automatically by `MigrateToRefStore(storeDir)` at
daemon startup (before the first RPC is served). The migration reads the
`indexed_ref` field from `graph.json` metadata to determine the correct
sub-directory; slots with no metadata fall back to `refs/_unknown/`. Migration
is idempotent (#2126).

### HOT / WARM / COLD tier state machine

Every `(repoPath, ref)` slot has a tier managed by `internal/daemon/tier.Manager`:

| Tier    | In-RAM? | Disk present? | Transition rule                          |
|---------|---------|---------------|------------------------------------------|
| HOT     | yes     | yes           | Registered after indexing or access      |
| WARM    | yes     | yes           | 5 min idle with no MCP / dashboard query |
| COLD    | no      | yes           | 1 h idle; mmap released, GC eligible     |
| EXPIRED | no      | no            | Disk eviction (non-pinned only)          |

The default-branch slot for a repository (detected via `tier.IsDefaultBranch`
using `git symbolic-ref refs/remotes/origin/HEAD` with a fallback to the common
`main`/`master` names) is pinned HOT and never reaches EXPIRED. Feature-branch
and worktree slots are subject to the full state machine.

Worktree-class slots use a more aggressive WARM→COLD window (30 min instead of
1 h) on the assumption that linked worktrees have higher churn and shorter
useful lifetimes.

A cold wake (COLD→HOT) is triggered by the first MCP query or dashboard HTTP
request targeting the slot. The reload callback re-mmap's `graph.fb` by calling
`daemonMCPCache.Get`; the measured cold-wake latency is ≤70 ms on a 108 MB
graph and ≤121 ms on a 52 MB graph (#2137).

Disk eviction (`COLD→EXPIRED`) deletes the `refs/<ref-safe>/` sub-directory for
non-pinned slots when they pass the EXPIRED TTL. Freed bytes are logged (#2144).

### .git/HEAD watching and ref-switch signalling

The daemon's phase-B fsnotify watcher (ADR-0017 §Algorithm scheduling) was
extended to watch `.git/HEAD` in addition to source files. A HEAD change event:

1. Reads the new ref name via `gitmeta.Capture`.
2. Marks the old slot as WARM (triggers the idle timer immediately).
3. Triggers an incremental reindex for the new ref's slot. If the new ref has
   never been indexed, a full index runs (#2129). Graph seeding (see "Worktree
   graph seeding" below) is wired to worktree ACTIVATION, not to this
   HEAD-switch path.

Git hooks (`post-checkout`, `post-merge`, `post-rewrite`) installed by
`grafel install-hooks` send an explicit signal to the daemon via the
Unix-domain socket, providing a faster path than waiting for the fsnotify HEAD
event. The hooks are idempotent and co-exist with husky / lefthook / pre-commit
managed setups. The `pre-push` hook (existing) is retained unchanged (#2222).

### Worktree auto-discovery

At daemon startup and on every fsnotify cycle, the daemon calls
`worktree.Discover(repoPath)` which runs `git worktree list --porcelain` and
returns the set of linked worktrees for the primary checkout. Each discovered
worktree is registered as an ephemeral child with its own `(path, ref)` slot.
Worktree slots receive `SlotKindWorktree` in the tier manager, applying the
aggressive eviction policy. Worktrees that are removed between two discovery
cycles have their slots transitioned to COLD (#2138, watcher pause/resume #2139).

### Worktree graph seeding

> **Status correction (#5964).** This section previously described a
> *"clone-from-parent optimisation"* in the present indicative, as though it
> shipped. It did not. That attempt (#3652) was written, found broken, and
> deleted in `65427c5a8`; `GRAFEL_CLONE_MAX_FILES` has never existed in the
> code, and the "~5 s -> ~180 ms" figure was never measured against a shipped
> implementation. The original text is preserved, collapsed, at the end of this
> section rather than deleted: an ADR asserting a guarantee the code does not
> deliver is a defect class this repo has paid for repeatedly, and removing the
> claim silently would hide that it was ever made.

When a linked worktree is activated (`worktree.Watcher.OnActivate`), the daemon
seeds the worktree's `(path, ref)` slot from its parent's graph before the
initial reindex is enqueued, so that reindex becomes an incremental pass over
the delta instead of a full index of the corpus. Implemented in
`internal/daemon/worktree_seed.go`. The read path, storage format, cache keys,
tier system and Pass-4 semantics are unchanged - this is index-side only.

1. **Resolve the parent.** `WorktreeChild.ParentPath` (recorded at discovery),
   falling back to `gitmeta.ResolveCommonDir` matched against the watcher's own
   parents provider. The parent's **actual** current ref is then read via
   `gitmeta.CaptureCached` - never a hardcoded `"main"`. Resolving a hardcoded
   ref against the *worktree's* own path was the bug that killed #3652: wrong
   store root and wrong ref name, so it always read an empty directory.
2. **Pin the repo tag.** `graph.EntityID` hashes the repo tag first, and
   `Index()` defaults it to `filepath.Base(repoPath)` - for a worktree, the
   worktree directory's own name, which disagrees with the parent's slug.
   Without a pin a seeded graph would mix two id spaces, leaving every
   cross-file edge between them dangling. The pin (`repo-tag`, in the state
   dir) is written whether or not seeding succeeds, because a *full* index of
   the worktree must use the same tag for the two graphs to be comparable at
   all.
3. **Copy and stamp.** The parent's active graph generation (single-file or
   segment-set) **and its `file-index.json` diff manifest** are copied into
   `StateDirForRepoRef(child.Path, child.Branch)`. The manifest is not
   optional: it is the baseline the child's changed-file set is computed
   against, and a graph seeded without it is silently wrong. The copy is
   bracketed by a generation-stability check and recorded in
   `seed-provenance.json` with a SHA-256 of every artifact copied. The
   `current` pointer is published **last**, so a torn copy leaves inert bytes
   rather than a resolvable, wrong graph.
4. **Verify before use.** The consuming pass re-hashes every stamped artifact
   and re-reads the pointer (`VerifySeededGraph`) before the seed is used. The
   stamp is consumed at the moment verification succeeds, since the pass then
   writes its own generation into the same dir. Both paths that can reach a
   seeded dir consume it - the incremental callback and `daemonSchedulerIndex` -
   because the scheduler skips the incremental callback entirely when
   `GRAFEL_INCREMENTAL_REINDEX=0`, a documented and supported setting.
   Belt and braces, a pointer naming a generation *newer* than the stamp is
   classified `seed_superseded_by_child_graph` (benign: the child built its own
   graph, so the stamp is merely stale), and `DiscardSeed` **refuses** to touch
   such a dir. Without both, a stale stamp would make `DiscardSeed` remove the
   `current` pointer and render a full corpus reindex the child legitimately
   paid for unresolvable - throwing away exactly the cost this work exists to
   eliminate.
5. **Index the delta.** The existing content-hash + git machinery
   (`diff.FilterWithGit`) recomputes the changed set from the child's working
   tree. Because the baseline is content hashes rather than a commit range,
   committed changes, uncommitted working-tree edits and untracked files are
   all covered by one mechanism - a `git diff <parentRef>..<childRef>` alone
   would miss the last two, which are exactly an agent's in-progress work.

`diff.FilterWithGit` was extended as part of this work. It previously trusted
`git diff --name-only HEAD` and never hashed a file git did not report, which
is sound only while the manifest was written at the commit the repo is on now.
A seeded state dir violates that by construction (the manifest is the
parent's), so `FilterWithGit` now unions in the `manifest.GitCommit..HEAD`
range diff, and falls back to a full hash comparison when that range cannot be
resolved (unreachable commit after a gc, shallow clone, or history rewrite).
The parity test below found this: without the union, a file changed by a
*commit* on the child branch was never re-extracted and its stale entities
survived. The same hole affected any `Index()`+`WithIncremental` run after a
HEAD advance; `internal/extractors.TryIncremental` had its own equivalent
(#5710).

**Fallback policy.** Every failed precondition - parent not indexed, parent
without a diff manifest, generation moved mid-copy, copy error, child already
indexed, no repo tag to pin, stamp mismatch - falls back to a full index **and
logs a named reason**. A silent fallback is explicitly rejected: a seed that
systematically never fired would be invisible.

**Correctness gate.** `TestWorktreeSeedParity_SeededGraphMatchesAFullIndex`
(`cmd/grafel/`) indexes the same worktree twice - seeded, and from scratch -
and diffs the full entity and relationship identity sets both ways. Byte
comparison of `graph.fb` is invalid (#6083: one length change cascades through
every FlatBuffers offset), and counts alone are insufficient - a bug that loses
N rows and invents N others passes a count assertion, and #6085 was itself a
count bug.

**Measured, not inferred. Read this before quoting a benefit.** Seeding is
O(delta) in work; its wall-clock win is **scale-dependent**, and it is **never a
memory win**. 16 GB macOS laptop, 3 changed files, swap steady across every run
(`TestWorktreeSeedPeakHeapAndWallClock`, opt-in via `GRAFEL_SEED_HEAP_MEASURE=1`;
peak is live heap `next_gc / (1 + GOGC/100)`, not RSS - macOS RSS counts
`MADV_FREE` pages and is an upper bound, not a footprint):

| files | full index | seed + delta | heap | wall |
|---|---|---|---|---|
| 400 | 60.7 MB / 515 ms | 62.3 MB / 533 ms | 1.03x | **1.03x - no win** |
| 1200 | 75.8 MB / 997 ms | 91.3 MB / 668 ms | 1.20x | 0.67x |
| 2500 | 105.3 MB / 7.252 s | 138.8 MB / 1.245 s | 1.32x | 0.17x |

The wall-clock crossover sits between 400 and 1200 files; below it seeding is
break-even or marginally worse. The heap ratio degrades monotonically with graph
size.

This is **structural, not a fixture artifact**. The seeded pass re-extracts 3 of
N files, but it must materialise the *whole parent graph* to merge the
unchanged-file portion forward (`internal/extractors/incremental.go` calls
`graph.LoadGraphFromDir`; the `Index()` path does the same via
`mergeIncrementalPrevDoc` plus the carry-forward entity slice) and holds it
alongside the document being built. Peak heap therefore tracks **graph size**,
not delta size, and lands *above* a full index.

**So: "seed instead of full index" is a latency optimisation, not a memory one.
It does NOT reduce the per-process memory budget that epic #5954 exists to
reduce** - the ~1.0-1.1 GB per concurrent indexing process is untouched by this
change, and anyone sizing concurrent per-worktree indexing must budget for the
graph, not for the delta. Reducing that peak is separate work.

**One-time entity-id change for already-indexed worktrees.** The repo-tag pin is
written on activation, so a worktree that was already indexed under the default
tag (its own directory basename) has every entity id re-minted from the parent's
slug on its next full index. No hybrid graph arises - the whole graph is
rewritten in one pass - but ids that external state may have cached do change
once. Repos that are not worktrees never get a pin and are entirely unaffected.

**Not covered.** Seeding is wired to worktree activation only. An in-place
branch switch on a single checkout (the `.git/HEAD` watch path above) is not
seeded from the previous ref.

<details>
<summary>Superseded: the #3652 "clone-from-parent optimisation" as this ADR
previously described it. NEVER SHIPPED.</summary>

> When a new ref has never been indexed (no `graph.fb` present), the daemon
> checks whether a close ancestor ref's graph can be seeded. If the diff between
> the ancestor and the new ref touches ≤ `GRAFEL_CLONE_MAX_FILES` files
> (default 20, hard cap 100), the optimisation:
>
> 1. Copies the parent's `graph.fb` and side-cars to the new ref's store dir.
> 2. Patches the metadata header (`indexed_ref`, `indexed_sha`, `computed_at`)
>    via a streaming read-modify-write (no in-place FlatBuffers mutation).
> 3. Re-extracts only the changed files: removes stale entities/relationships,
>    runs the language extractor callback, merges new results.
> 4. Persists the updated `graph.fb` atomically.
>
> Typical speedup on a 6 k-entity repo with 5 changed files: ~5 s → ~180 ms.
> If any precondition fails, the partially-built graph is removed and a full
> reindex runs (#2146).

</details>

### CLI `--ref` flag

The following CLI subcommands accept a `--ref <name>` flag to target a specific
indexed ref instead of the current HEAD:

```
grafel status   [--ref <name>]
grafel rebuild  [--ref <name>]
grafel index    [--ref <name>]
grafel list     [--ref <name>]
grafel doctor   [--ref <name>]
grafel remove   [--ref <name>]
```

`grafel status` also accepts `--all-refs` to display all indexed refs for
a group, with HOT/WARM/COLD tier annotation per slot (#2219).

`grafel branches` lists every indexed ref for a group, with tier, size-on-disk,
last-indexed timestamp, and entity count (#2144).

### Dashboard `?ref=` query parameter

Six read-only API endpoints accept an optional `?ref=<name>` query parameter:

```
GET /api/groups/:g/stats?ref=<ref>
GET /api/groups/:g/repos/:r/entities?ref=<ref>
GET /api/groups/:g/repos/:r/relationships?ref=<ref>
GET /api/groups/:g/repos/:r/cross-repo-edges?ref=<ref>
GET /api/groups/:g/repos/:r/orphans?ref=<ref>
GET /api/groups/:g/repos/:r/patterns?ref=<ref>
GET /api/groups/:g/refs                  (listing; no ?ref=)
```

`?ref=` semantics:

- Absent or `@current` → current HEAD ref (backward-compatible default).
- `<name>` → that specific ref's graph (404 if not indexed).
- `@all` → aggregate across all indexed refs for the repo/group.
- Invalid → HTTP 400 `{"error": "invalid ref", "available": [...]}`.

The topbar ref-selector (added in PH4, #2143) provides a UI dropdown that
writes the `?ref=` param into the URL and persists the selection across
navigation. Dashboard surfaces that read `?ref=` update automatically (#2220).

### WebSocket ref-filtered subscription

`WSEvent` now carries a `Ref` field. The server side evaluates per-connection
subscription filters set by `subscribe` client→server messages; a client
subscribed to `{group: "X", ref: "feature/foo"}` receives only events for that
(group, ref) pair. Clients that send no subscription message continue to receive
all events (firehose mode, backward-compatible default). The dashboard's
connection manager subscribes to the active `?ref=` ref when one is selected
(#2221).

### Graph diff view

`GET /api/v2/groups/:group/repos/:repo/diff?refA=<A>&refB=<B>` computes the
structural diff between two indexed refs. The response lists added, removed, and
modified entities and relationships, plus a summary counter block. Results are
cached in a 10-entry in-process LRU keyed by `(group, repo, refA-sha, refB-sha)`
(#2148).

### Cross-repo link cache keying

The cross-repo candidate pipeline cross-matches entities from repo A against
repo B. With multi-ref indexing, this cache must be keyed by
`(repoA, refA, repoB, refB)` — a 4-tuple — to avoid serving stale results
after a ref switch. A secondary index maps `(repo, ref)` to affected cache
entries for O(affected) invalidation on ref switch. `CrossLinkCache.InvalidateRepo(repo,
oldRef)` is called by the daemon immediately after every successful ref switch
detected via HEAD watch or git hook (#2224).

### Embedding deduplication

The content-hash dedupe cache introduced in ADR-0019 (Pass 9 / embeddings.bin)
was extended in PH8 (#2158) to key by `(entity-content-hash, ref)` so that
identical entities shared across branches (the common case for non-modified
files) reuse the same embedding vector and skip the encoder entirely. This
bounds the embedding re-encode cost on branch creation to only the files that
actually differ.

## Consequences

### Positive

- Branch switches are transparent to AI agents: the graph is correct for the
  currently checked-out ref without any manual step.
- Multi-branch PR review workflows become first-class: an agent can query two
  refs and ask for the structural diff in a single session.
- Linked worktrees are indexed automatically alongside the primary checkout.
- Cold-wake latency is sub-100 ms for HOT → WARM transitions; ~1 s for a warm
  reload from disk; ~5–10 s for a full cold reindex (mitigated by clone-from-parent
  on small diffs).
- The default-branch graph is always pinned HOT; there is no penalty for an
  idle branch-feature switch back to `main`.

### Negative / trade-offs

- **Storage growth is linear in indexed branches.** Each `(repo, ref)` slot adds
  one `graph.fb` (typically 5–25 MB per repo). The disk eviction policy (EXPIRED
  TTL) bounds growth for long-lived feature branches that are no longer accessed.
  Users can inspect and manually evict with `grafel branches --evict <ref>`.
- **Ref-switch latency window.** There is a brief window between a `git checkout`
  and the daemon's HEAD watch firing where MCP queries land on the old ref's
  graph. The git-hook path reduces this to near-zero when hooks are installed.
- **Clone-from-parent is best-effort.** It is skipped when the ancestor diff
  exceeds `GRAFEL_CLONE_MAX_FILES`, when the merge-base is too old (>7 days
  by default), or when any step errors. The fallback is a full reindex.
- **Worktree discovery requires `git worktree list`.** This is a subprocess call
  at daemon startup and on each watcher cycle. On machines with hundreds of
  registered repos this adds latency; the call is debounced alongside the
  existing fsnotify debounce.

### Migration

Existing installs are migrated automatically at next daemon startup by
`MigrateToRefStore`. No user action is required. The migration is idempotent and
safe to interrupt — a partial migration leaves the old flat slot untouched, so
the daemon can still serve from it on restart.

## Alternatives considered

**Single-ref-at-a-time (previous design).** The daemon indexed only the current
HEAD and discarded the old ref's data on branch switch. Rejected: poor
multi-branch UX — developers cannot query a feature branch while working on
another branch, and PR review requires a manual `grafel rebuild` step.

**Ref-on-demand only (no background watch).** Index a ref only when an MCP query
or CLI command explicitly targets it, with no background HEAD watching. Rejected:
cold start on every branch switch; the install-and-forget contract requires that
the graph is ready before the developer asks their first question on a branch.

**Federated daemons (one daemon per branch).** Run a separate daemon process for
each active ref. Rejected: too much complexity; process-level isolation multiplies
socket, PID file, and MCP registration management by the number of active refs;
memory is not shared across the daemon boundary, defeating the mmap-sharing
benefit of ADR-0017.

**Ref-indexed JSON store.** Keep `graph.json` as the canonical artifact (pre-ADR-0016)
with a `refs/` sub-directory layout. Rejected: graph.json parse cost (130 ms /
50 MB allocs per open on the 11 MB fixture) multiplied by ref count would
dominate dashboard and MCP latency; ADR-0016's mmap'd FlatBuffers is the correct
foundation for per-ref multi-reader access.
