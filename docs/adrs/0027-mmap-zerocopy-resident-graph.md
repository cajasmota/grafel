# ADR-0027: mmap + zero-copy resident graph

- **Status**: **Proposed**
- **Date**: 2026-07-21
- **Deciders**: Jorge Cajas
- **Related**: ADR-0016 (binary graph format — FlatBuffers v2), ADR-0017 (single-binary daemon architecture), ADR-0024 (decouple MCP serving from the engine — serve/engine split; this ADR is serve-side only), ADR-0026 (fbwriter sharding — deferred; corpus is 0.404 GiB on disk today).
- **Epic**: #5850 (memory north-star).
- **Companion**: [`0027-mmap-zerocopy-resident-graph-plan.md`](0027-mmap-zerocopy-resident-graph-plan.md) — the ordered PR plan.

---

## Context / Problem

`serve` already keeps the graph's mmap open for the process lifetime, then
**ignores it**. On reload, `reloadLocked` opens `fbreader.Open(graph.fb)` and
stashes the handle on `LoadedRepo.Reader`
(`internal/mcp/state.go:945`, struct field `internal/mcp/state.go:274`),
closing it only on reload-swap (`state.go:910`), eviction (`state.go:972`),
or `Close()` (`state.go:1107`). But **no query path reads through
`lr.Reader`**. Every handler reads the fully-materialized heap
`*graph.Document` — `lr.Doc.Entities` / `lr.Doc.Relationships` — which is a
complete, redundant heap copy of bytes the kernel already has mapped.

That materialized `*graph.Document` is the memory north-star's biggest single
line item: on the corpus (287,091 entities · 1,335,957 relationships,
ADR-0026) the heap `Document` is on the order of **~1 GB resident**, while the
on-disk `graph.fb` it was decoded from is **0.404 GiB** and is *already mapped
into the same address space* via `lr.Reader`. We pay for the bytes twice, and
the heap copy is the expensive one: it is a live pointer-graph the GC must
scan on every cycle, whereas the mmap is off-heap, demand-paged, and evictable
by the kernel under pressure.

The problem is **multiplied ×worktree** (ADR-0020): every indexed
branch/worktree snapshot of a repo is its own `LoadedRepo` with its own
materialized `Document`. A developer with a repo plus three active worktrees
pays the ~1 GB four times resident, even though only the branch they are
querying is hot.

The CLI one-shot path is not the problem — `loadFBDocument`
(`internal/graph/load.go:269`) opens, copies into a `*Document`, and munmaps
immediately (`defer r.Close()`, `load.go:274`). That is correct for a
one-shot. The resident-server path is where the redundant copy persists for
the process lifetime and scales with worktrees.

### Why we can't just delete the copy today — the borrow/reload crux

The materialized copy is load-bearing for **memory safety**, not just for
convenience. The read path is deliberately lock-free:

1. A handler calls `State.Group(name)`, which takes `s.mu`, copies the
   `*LoadedGroup` pointer, and **releases the lock on return**
   (`internal/mcp/state.go:1029-1037`).
2. The handler then reads `grp.Repos[...].Doc`, the derived indexes
   (`getByID`/`getAdjacency`/…, `state.go:473,513`), etc. **entirely
   lock-free**, for the whole duration of the tool call.
3. Concurrently, `reloadLocked` (`state.go:784`) — content-hash-gated by an
   FNV-1a compare (`hashGraphFile`, `state.go:1123`) — can fire, close the old
   reader (munmap, `state.go:910`), swap `lr.Doc`, re-arm the lazy indexes
   (`resetIndexes()`), and reopen the reader (`state.go:945`).

Today this is safe **only because the old heap `*Document` stays alive**: the
in-flight lock-free reader still holds a Go pointer into it, so the GC cannot
reclaim it until that goroutine returns. There is **no refcount, epoch, or
generation** gating the swap — GC reachability is the entire safety argument.

Zero-copy **breaks this argument**. If handlers read bytes aliased out of the
mmap, then the safety of an in-flight read depends on the *mmap pages* staying
mapped — and the GC does not track mmap pages. A concurrent `reloadLocked`
that calls `lr.Reader.Close()` (munmap) while a query still aliases those
bytes is a **use-after-unmap → SIGSEGV/SIGBUS**. We cannot go zero-copy
without first building an explicit lifetime protocol that the GC used to give
us for free.

### Half-built infrastructure we can learn from (but not reuse as-is)

`internal/daemon/mcp/graph_cache.go` is a refcounted mmap LRU
(`Borrow`/`release`, `Entry.refs`) keyed on the absolute `graph.fb` path. But
its `removeLocked` (`graph_cache.go:329-340`) **does not wait for `refs==0`
before munmapping** — the refcount is decorative w.r.t. safe reclaim, and the
code comment concedes the SIGBUS risk and explicitly suggests "swap to an
epoch-based reclaim." It is used only for warming/eviction, never as a query
surface. It is a cautionary prototype, not a foundation.

### The bytes are already aliasable

The generated FlatBuffers accessors already return **aliases** into the mapped
buffer, not copies: `fbgraph.Entity.Name()`, `Relationship.FromId()`, etc.
return `[]byte` via `_tab.ByteVector(...)`
(`internal/graph/fbgraph/Entity.go:48,88,96,104`,
`Relationship.go:44-63`). A zero-copy string is one `unsafe.String(&bv[0],
len(bv))` away. FlatBuffers guarantees ≥8-byte buffer alignment and stores
strings length-prefixed and byte-addressable, so `unsafe.String` over a
ByteVector is well-defined. The physical mechanism is already there; only the
**lifetime discipline and the read-surface plumbing** are missing.

---

## Decision

Re-architect the serve-side resident graph so queries read **through the
already-open mmap** instead of a redundant heap `*Document`, using an
**interface seam** so the migration stays 100% build-green at every step and
the memory win is guarded behind a measurable flag until cutover.

### The interface seam

Introduce two read-only interfaces in `package graph`:

```go
type EntityView interface {
    ID() string
    Kind() string
    Name() string
    QualifiedName() string
    Subtype() string
    SourceFile() string
    Language() string
    Signature() string
    StartLine() int
    EndLine() int
    EmbeddingRef() string
    Tags() []string
    Property(key string) (string, bool)
    Properties() map[string]string
    CommunityID() (int, bool)
    PageRank() (float64, bool)
    Centrality() (float64, bool)
    IsGodNode() bool
    IsArticulationPoint() bool
    Confidence() float64
    // ...one method per field currently read as a struct field.
}

type RelationshipView interface {
    ID() string
    FromID() string
    ToID() string
    Kind() string
    Property(key string) (string, bool)
    // ...
}
```

- The existing materialized `graph.Entity` / `graph.Relationship` implement
  these interfaces **trivially** — each method returns the corresponding
  struct field (`func (e *Entity) Name() string { return e.Name_ }` after a
  mechanical field rename, or a thin wrapper type). This is a behavior-neutral
  no-op at runtime.
- Consumers migrate from **direct struct-field reads** (`e.Name`,
  `doc.Relationships[i].Kind`) to **interface method calls** (`e.Name()`),
  **package-by-package**, while the build stays green because the concrete
  type still satisfies the interface. This is the bulk of the work: ~3,700
  direct read sites across 468 files that iterate `.Entities`/`.Relationships`
  (dashboard ~1097, mcp ~1023, engine ~741, resolve ~368, links ~347, docgen
  ~171).
- **Final cutover**: provide an `mmapEntityView` / `mmapRelationshipView`
  implementation backed by `fbgraph.Entity` / `fbgraph.Relationship`
  accessors (returning `unsafe.String` aliases for cold fields), flip the
  loader to stop materializing the `*Document`, and drop the now-unread struct
  fields. **The memory win lands only at cutover** — every migration PR before
  it is behavior- and memory-neutral, which is exactly what keeps them small
  and reviewable.

This is the "full (a)-endpoint solution" — serve entirely from mmap — but
decomposed so no single PR is a big-bang rewrite.

### Structure of the work

**Foundation (sequential — F1 → F2 → F3):**

- **F1 — deferred-unmap lifetime.** Build the explicit protocol that replaces
  GC-reachability as the safety argument for lock-free reads over mmap. This
  is the centerpiece; see [Lifetime design](#lifetime-design-the-deferred-unmap-mechanism).
- **F2 — the view interfaces + resident hot index.** Land the `EntityView`/
  `RelationshipView` interfaces with the materialized impl, and ensure the
  **hot resident index** (id→handle, the Tier-2a int32 CSR adjacency in
  `internal/mcp/traversal.go:103-107`, the label lookup `getByID`/`LabelIndex`)
  can be built and survive **without** a materialized `Document` — i.e. from
  handles, not from `*Entity` pointers.
- **F3 — mmap-backed view impl behind a flag.** Provide the mmap-backed
  `EntityView`/`RelationshipView` and a `GRAFEL_SERVE_FROM_MMAP` flag (env +
  fleet config) that flips serve-from-mmap on/off **per repo load**, so we can
  A/B the resident-memory and latency deltas on the same binary.

**Migration (parallel, bite-sized):** one PR per consumer package; big
packages sub-split by file-group/feature into ~150–300-site chunks
(~15–20 PRs total). Each is behavior-neutral and build/test green. See the
companion plan.

**Cutover (sequential):** flip the loader to mmap-backed views, stop
materializing, drop the struct fields, and **measure resident** with the
membaseline harness. Then per-field hot/cold tuning toward the (a) endpoint,
gated on a query-**latency** harness.

---

## Lifetime design — the deferred-unmap mechanism

This is the crux. We need: **a reload may not munmap a mapping while any
in-flight query still aliases its bytes.** Two candidate mechanisms:

### Option A — refcount-drain (borrow / release with deferred close)

Wrap each mapping in a `MapHandle` carrying an `atomic.Int32 refs`. A query
"borrows" the handle at the same instant it borrows the group pointer, and
"releases" it when the tool call returns. Reload, instead of munmapping
in-place, does a **hand-off**: it publishes the new handle, marks the old one
retired, and lets the **last releaser** (the goroutine that drops `refs` to
zero on a retired handle) perform the munmap.

```
type MapHandle struct {
    reader  *fbreader.Reader
    refs    atomic.Int32   // # of in-flight borrows
    retired atomic.Bool    // reload published a successor; unmap when refs hits 0
}

func (h *MapHandle) borrow() bool {           // returns false if already retired+drained
    for {
        n := h.refs.Load()
        if n < 0 { return false }             // sentinel: fully closed
        if h.refs.CompareAndSwap(n, n+1) { return true }
    }
}

func (h *MapHandle) release() {
    if h.refs.Add(-1) == 0 && h.retired.Load() {
        h.closeOnce()                          // last releaser of a retired handle unmaps
    }
}

// reload path, under s.mu:
old := lr.handle
lr.handle = newHandle                          // publish successor FIRST
old.retired.Store(true)
if old.refs.Load() == 0 { old.closeOnce() }    // no in-flight borrows → unmap now
// else: the last in-flight release() will unmap.
```

**How it wraps the existing `State.Group()` borrow.** Today `Group()` copies
the `*LoadedGroup` under `s.mu` and releases the lock (`state.go:1029-1037`).
We extend the borrow so that **the same critical section that copies the group
pointer also `borrow()`s the current `MapHandle` of each repo the call will
touch** (or, more simply, the group snapshot carries the handle set and every
per-repo read goes through `handle.reader`). The handler defers `release()`.
Because the borrow is taken *under `s.mu`*, and reload's publish+retire is
*also* under `s.mu`, there is no window where a handler observes a handle that
is already retired-and-drained: either the handler borrows before retire (refs
≥ 1 → reload defers the unmap to the handler's release) or after the new
handle is published (it borrows the fresh handle). The ordering
"publish successor → mark old retired → conditionally close" under the same
mutex is what closes the race.

**Correctness argument.** A retired handle is unmapped by exactly one
goroutine: whichever observes `refs==0 && retired` last. `borrow()` refuses a
handle already at the closed sentinel. No borrow can begin on a handle after
it is retired *and* observed at zero, because a fresh borrow only ever targets
`lr.handle`, which reload already repointed to the successor before retiring
the old one. Thus no `unsafe.String` alias can outlive its mapping.

### Option B — epoch / generation reclaim

Give the server a monotonically increasing generation counter. Each `MapHandle`
is tagged with the generation at which it was retired. Readers publish the
generation they entered under (a per-P slot, RCU-style). Reload retires the old
handle at generation *g* and enqueues it on a **deferred-unmap list**; a
reclaimer unmaps a retired handle only once the **global minimum active reader
generation** has advanced past *g* (i.e. every reader that could have entered
before the retire has exited). This is the classic RCU / QSBR pattern and is
what the `graph_cache.go` comment gestures at ("swap to an epoch-based
reclaim").

### Decision: **Option A (refcount-drain), with the epoch structure held in reserve.**

Rationale:

1. **The borrow point already exists and is coarse.** grafel's read unit is a
   whole MCP tool call, and there is already exactly one place — `State.Group()`
   under `s.mu` — where a call latches the state it will read. Refcount-drain
   maps one-to-one onto that existing borrow/return boundary; epoch reclaim
   would add a second, orthogonal bookkeeping plane (per-P generation slots, a
   reclaimer poll loop) for no extra safety, given we already have the natural
   latch point.
2. **Reloads are infrequent and coarse.** Reload is content-hash-gated
   (`hashGraphFile`) and swaps a whole repo, so retire events are rare and the
   deferred-unmap set is tiny (usually the single just-superseded mapping).
   The pathological "reader pins an old mapping forever" case is bounded by MCP
   tool-call latency (sub-second), not by an unbounded epoch grace period.
3. **Deterministic reclaim.** The last releaser unmaps *immediately*, so
   retired mappings do not accumulate waiting for a global epoch to advance —
   important because on Windows the mapping also **holds a file lock** (below).

We keep the epoch structure documented as the escape hatch for a future world
where reads become finer-grained than a whole tool call (e.g. streaming
handlers that outlive the `Group()` borrow), where a single coarse refcount
would pin mappings too long.

### Windows file-lock implication

On Windows, `fbreader.Open` maps via `CreateFileMapping`/`MapViewOfFile`, which
**locks the underlying file**: an open mapping blocks deletion/replacement of
`graph.fb` (this is exactly why `Server.Close()` exists). Under
refcount-drain, a retired-but-not-yet-drained mapping keeps the *old*
`graph.fb` bytes locked until the last in-flight reader releases. Because the
engine writes `graph.fb` via **atomic rename** (write temp → rename over),
the successor bytes land at a new inode and the old mapping locks only the
now-unlinked predecessor, which the last releaser promptly unmaps. Deterministic
last-releaser reclaim (Option A) is strictly better than epoch reclaim here:
we do not want an old, file-locking mapping lingering for an epoch grace period
on Windows. F1 must include a Windows CI smoke test that reloads under
concurrent borrows and asserts no `ERROR_SHARING_VIOLATION` on the engine's
rename.

### Reload-during-borrow, restated

With F1 in place, the sequence "handler borrows handle H → reload publishes H'
and retires H → handler keeps reading H's aliases → handler releases H → last
releaser unmaps H" is safe by construction. The lock-free read window is
preserved (handlers still read without holding `s.mu`); only the *unmap* is
deferred out of `reloadLocked` into the borrow-release protocol.

---

## Hot / cold split, and the tunable seam (why (b) → (a) is a dial, not a rewrite)

Not every field wants to live in the mmap on the read path. Two tiers:

- **HOT — keep resident and indexed** (filter/lookup/adjacency/BM25/traversal
  touch these on every query, for *every* candidate entity, so per-entity
  interface dispatch or per-entity `unsafe.String` would show up as latency):
  Entity **ID / Kind / Name / QualifiedName**, Relationship **FromID / ToID**.
  These stay in the resident hot index (id→handle table, the int32 CSR
  adjacency `traversal.go:103-107`, the label index). Filtering runs against
  the **resident index**, not through per-entity `EntityView` dispatch.
- **COLD — zero-copy on payload assembly** (read only when a *result* entity is
  serialized into a tool response, i.e. O(results) not O(graph)):
  SourceFile / Signature / Language / EmbeddingRef / Subtype / Properties
  *values* + the scalar overlays (StartLine / community / pagerank /
  centrality / god / articulation). These are read straight from the mapped
  bytes via the mmap-backed `EntityView`, aliasing where the field is a string.

**FB-absent fields.** EndLine, Tags, Metadata, Confidence are not in the
FlatBuffers schema today and are materialized as zero. Under mmap-backed views
their methods return the zero value (`EndLine() -> 0`, `Tags() -> nil`), which
is **byte-identical to today's behavior** — no fbversion bump needed to
preserve it.

**Tunneled fields.** `module` (tunneled into Properties) and Relationship `id`
(tunneled into Properties) are read via `Property(key)` on the view, which the
mmap impl resolves against the property vector; no special-casing at call
sites.

**The tunable seam — (b) → (a) is a dial.** Because HOT vs COLD is decided
*inside the view implementation and the resident-index builder*, not at the
~3,700 call sites, moving a field between tiers is a localized change. The
conservative first cutover (endpoint (b)) can keep more fields resident; then,
gated on the latency harness, we demote fields to zero-copy-cold one at a time
toward the maximal-memory-win endpoint (a). No consumer code changes when a
field's tier changes — the interface method signature is stable. This is the
property that makes the memory north-star a **measured slider** rather than a
one-shot bet.

### Interaction with the in-flight Properties `[]propKV` refactor

The parallel refactor that replaces `Properties map[string]string` with a
`[]propKV` backing (interned keys) is a prerequisite shape for cold-property
aliasing: at cutover, `EntityView.Property()` / `Properties()` sit **atop the
`[]propKV` backing**, and property **values alias the mmap** while **keys are
already interned** (so no per-value key allocation). The two refactors must
land in a compatible order: the `EntityView.Properties()` method contract is
defined so that either the map-backed or the `[]propKV`-backed impl satisfies
it, and the mmap impl targets the `[]propKV` shape. F2 must not hard-code the
`map[string]string` return in a way that blocks value-aliasing later.

---

## Consequences

**Positive:**

- **~1 GB resident reclaimed per hot repo at cutover**, multiplied by every
  worktree/branch snapshot (the copy simply stops existing; the mmap was
  already open and already paid for).
- **GC pressure drops**: the biggest live pointer-graph the collector scanned
  each cycle (the materialized `Document`) is replaced by off-heap mapped
  bytes the GC never scans.
- **Kernel-managed memory**: cold pages are demand-paged and evictable under
  pressure; resident set tracks working set, not graph size.
- **A stable read interface** (`EntityView`/`RelationshipView`) decouples the
  ~3,700 consumer sites from the physical storage, so future storage changes
  (sharding per ADR-0026 if the corpus ever grows into it, alternative
  encodings) don't ripple through consumers.

**Negative / cost:**

- **~15–20 migration PRs** of mechanical field→method rewrites, plus the F1/F2/
  F3 foundation and the cutover. High total diff, low per-PR risk.
- **`unsafe` on the read path**: `unsafe.String` over ByteVectors. Contained to
  the mmap view impl; justified by the FlatBuffers alignment/length guarantees.
- **Interface dispatch** replaces direct field loads at cold sites — acceptable
  because cold reads are O(results); explicitly *avoided* on hot paths by
  keeping filtering against the resident index.
- **Serve-side only.** The engine still writes and the CLI one-shot still
  materializes (`load.go:269`) — correct, and out of scope.

---

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| **Use-after-unmap (unsafe lifetime)** — a reload munmaps while a query aliases the bytes → SIGSEGV/SIGBUS. | F1 deferred-unmap (refcount-drain): reload never munmaps in-place; the last releaser of a retired handle unmaps. Race closed by taking borrow and publish+retire under the same `s.mu`. Ships before any zero-copy read (F3 flag defaults off until F1 is proven). |
| **Hot-path latency regression** — per-entity interface dispatch or per-entity `unsafe.String` on filter/traversal. | Keep HOT fields (ID/Kind/Name/QN, From/To) in the resident index; filter against the index, not the view. Gate cutover and every field demotion on a query-latency harness (new; see Measurement). |
| **Windows file-lock** — an open/retired mapping blocks `graph.fb` replacement → `ERROR_SHARING_VIOLATION` on the engine rename. | Engine writes via atomic rename to a fresh inode; last-releaser reclaim (Option A, not epoch) unmaps the predecessor promptly. F1 adds a Windows CI reload-under-borrow smoke test. |
| **GC-untracked memory** — resident set no longer visible to Go heap profiles; a leak (handle never released) is invisible to `pprof` heap. | `MapHandle` refcounts and the deferred-unmap set are exported as metrics (open mappings, retired-not-drained count, max borrow age). Alert on retired-not-drained > N or borrow age > tool-call timeout. |
| **Reload-during-borrow correctness** | Covered by F1 (above) + a race-detector stress test: N concurrent borrowers + a tight reload loop under `-race`, asserting zero faults and zero handle leaks. |
| **Migration drift** — a partially-migrated package mixes field reads and method calls, silently reintroducing a copy dependency at cutover. | Each migration PR is behavior-neutral and leaves its package 100% on the interface; a CI lint (vet-style) forbids new direct `.Entities[i].Field` struct-field reads in migrated packages. Cutover cannot land until every consumer is migrated (grep-gate = 0 direct field reads). |
| **`[]propKV` refactor ordering** | `EntityView.Properties()` contract defined storage-agnostic; mmap impl targets `[]propKV`; coordinate merge order so neither refactor hard-codes the other's shape. |

---

## Alternatives considered

1. **Status quo — keep the full heap copy.** Rejected: it is the memory
   north-star's single largest line item, multiplied ×worktree, and it exists
   only as an accident of the lock-free-read safety argument, not by design.
2. **Incremental string-aliasing only** — alias just the big string fields
   (SourceFile/Signature) out of the mmap but keep the `*Document` skeleton and
   the struct scalars. Rejected as the *endpoint* (it leaves most of the copy
   and still needs F1 for the aliased strings, so it pays the hard part —
   lifetime safety — for a fraction of the win). **Retained as a fallback
   cutover posture**: if the latency harness shows the full-cold split
   regresses hot paths, endpoint (b) keeps scalars resident and aliases only
   strings — reachable via the same tunable seam, no code-shape change.
3. **Epoch/QSBR reclaim as the primary lifetime mechanism.** Rejected as
   primary (kept in reserve): adds a second bookkeeping plane and non-
   deterministic reclaim for no extra safety given grafel's coarse,
   already-latched borrow point — and non-deterministic reclaim is actively
   worse on Windows' file-locking mappings. See Lifetime design.
4. **A real embedded graph DB / off-heap store (rocksdb, LMDB, etc.).**
   Rejected: contradicts ADR-0006 (no graph DB) and ADR-0016 (FlatBuffers is
   already the on-disk format and is *already mapped*); we'd be adding a store
   to duplicate bytes we already have mapped.

---

## Measurement plan

Two harnesses; the flag (`GRAFEL_SERVE_FROM_MMAP`, F3) lets both run A/B on one
binary and one corpus.

1. **Resident-memory — extend the existing membaseline harness.** Report RSS
   and Go heap `inuse` for: (baseline) materialized `Document`, (F3 flag on)
   mmap-backed serve, on the corpus and on a repo-plus-3-worktrees layout to
   capture the ×worktree multiplier. **Acceptance at cutover:** resident drops
   by ~the on-disk graph size per hot repo (order ~1 GB on corpus), Go heap
   `inuse` drops by the `Document`'s share, RSS net drop positive after
   accounting for the (already-paid) mapped pages.
2. **Query latency — new harness.** A fixed suite of representative MCP tool
   calls (find / expand / find_callers / traces / cross_links) run against a
   warm server, p50/p95/p99, flag off vs on. **Acceptance:** cutover and each
   subsequent HOT→COLD field demotion must hold hot-path p95 within an agreed
   budget (target: no regression on find/expand/adjacency; cold-heavy payload
   assembly may move within budget). This harness is the gate for the (b)→(a)
   field-tier dial.

Both harnesses run in the plan's F3 PR and gate the cutover PRs.

## No-fbversion-bump note

This is a **read-in-place** change: the mmap-backed views decode the *existing*
`graph.fb` layout. `fbversion.Version` stays **4**
(`internal/graph/fbversion/version.go:19`). FB-absent fields (EndLine, Tags,
Metadata, Confidence) keep returning their materialized-zero values, so read
output is byte-identical. No schema change, no engine write-path change, no
re-index required to adopt.
