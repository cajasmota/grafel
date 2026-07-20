# ADR-0027 companion — mmap + zero-copy resident graph: PR-by-PR implementation plan

Ordered, dependency-annotated PR list for [ADR-0027](0027-mmap-zerocopy-resident-graph.md).
Epic #5850. Serve-side only. No fbversion bump.

**Legend.** Every PR's baseline acceptance is **build + `go vet` + `go test ./...`
green**. Migration PRs additionally require **behavior-neutral** (no output diff;
concrete `graph.Entity`/`Relationship` still satisfies the interface, so runtime
is unchanged). Cutover PRs additionally require a **measured memory/latency delta**
from the harnesses in ADR-0027 §Measurement.

Rough scale: **3 foundation + ~16 migration + 3–4 cutover ≈ 22–23 PRs.**

---

## Phase F — Foundation (STRICTLY SEQUENTIAL: F1 → F2 → F3)

### F1 — deferred-unmap lifetime (`MapHandle` refcount-drain)
- **Depends on:** nothing. **This is the gate for all zero-copy work.**
- **Scope:** `internal/mcp/state.go` (+ a new `internal/mcp/maphandle.go`).
  Introduce `MapHandle{ reader, refs atomic.Int32, retired atomic.Bool, closed
  sync.Once }` with `borrow()`/`release()`/`closeOnce()`; `closeOnce` MUST be a
  genuine idempotent guard (`sync.Once` / CAS-on-flag). Route the existing
  `lr.Reader` through a `MapHandle`. Rework `reloadLocked` (`state.go:784`) so
  the reader swap at `state.go:910`/`945` becomes **publish-successor →
  retire-old → conditional-close** instead of an in-place `Close()`. **Convert
  the two other munmap sites the same way — not just reload:** repo eviction
  (`state.go:972`) and server `Close()` (`state.go:1107`) must route through
  `retired.Store(true)` + conditional `closeOnce()`, never a bare
  `lr.Reader.Close()` (same munmap-while-borrowed hazard). Extend the
  `State.Group()` borrow (`state.go:1029-1037`) so a call latches the repo
  `MapHandle`s under `s.mu`, returns them as an **immutable per-call snapshot**,
  and the handler `release()`s on return. Serve's mmap handles come ONLY from
  `reloadLocked`'s own `fbreader.Open` — this PR must NOT wire serve to the
  unsafe `internal/daemon/mcp` LRU (`graph_cache.go`, closes without draining).
- **Acceptance:** build/vet/test green; **BLOCKING — `closeOnce` is genuinely
  idempotent** (`sync.Once`/CAS): exactly-once munmap rests entirely here, since
  both reload and the last releaser can reach the close (a double-munmap unit
  test must pass — force the two-observer interleaving and assert one unmap);
  **BLOCKING — read-through-captured-handle**: `Group()` returns an immutable
  per-call snapshot and reads bind to the borrowed handle, never a live re-deref
  of `lr.handle`/`lr.Reader`/`lr.Doc` (a race test reloads in a tight loop while
  borrowers read, asserting no fault and no handle leak); **new race-detector
  stress test** (N borrowers + tight reload loop under `-race`, zero faults,
  zero handle leaks); **Windows CI reload-under-borrow smoke test** asserting no
  `ERROR_SHARING_VIOLATION` on the engine rename; metrics exported
  (open mappings / retired-not-drained / max borrow age). **No query yet reads
  through the mmap** — this PR only makes the unmap safe to defer. Behavior-neutral.

### F2 — view interfaces + resident hot index without a `Document`
- **Depends on:** F1 (borrow protocol exists).
- **Scope:** `internal/graph` — add `EntityView` / `RelationshipView`
  interfaces (§Decision) and make `graph.Entity`/`graph.Relationship` implement
  them (mechanical: field→method, or wrapper methods). `internal/mcp` — prove
  the **hot resident index** (id→handle `getByID`/`LabelIndex` at
  `state.go:473`; int32 CSR adjacency `traversal.go:103-107`;
  `getAdjacency` at `state.go:513`) can build from **handles**, not from
  `*Entity` pointers into a materialized `Document`. Define `Properties()`
  storage-agnostic (compatible with both `map[string]string` and the in-flight
  `[]propKV`).
- **Acceptance:** build/vet/test green; interfaces satisfied by concrete types;
  hot-index builders have a handle-fed code path (still fed by `Document` for
  now) and are **keyed off the per-call captured handle**, not a live `lr.*`
  field (the read-through-captured-handle invariant), proven by a race test.
  Behavior-neutral. **No consumer migrated yet.**

### F3 — mmap-backed view impl behind a flag
- **Depends on:** F1 + F2.
- **Scope:** `internal/graph` (or `internal/mcp`) — `mmapEntityView` /
  `mmapRelationshipView` backed by `fbgraph.Entity`/`Relationship` accessors,
  cold string fields via `unsafe.String` over `ByteVector`
  (`fbgraph/Entity.go:48,88...`). **Impl note:** `unsafe.String(&bv[0],
  len(bv))` PANICS on an empty ByteVector (`&bv[0]` indexes a zero-length
  slice) — the view impl needs a `len(bv)==0 → ""` guard; it WILL hit real data
  (empty Signature/Subtype are common). Add `GRAFEL_SERVE_FROM_MMAP` flag (env +
  fleet config) selecting mmap-backed vs materialized views **per repo load**.
  Land **both harnesses**: extend membaseline (RSS + heap inuse, incl.
  repo+3-worktrees) and add the new query-latency harness (find/expand/
  find_callers/traces/cross_links, p50/p95/p99).
- **Acceptance:** build/vet/test green; flag **defaults OFF** (materialized path
  unchanged); with flag ON a smoke query returns byte-identical results on a
  small fixture; harnesses runnable in CI and produce the A/B report. Behavior-
  neutral with flag off.

---

## Phase M — Migration (PARALLELIZABLE after F2; ~16 PRs)

Each PR migrates one package (or file-group slice) from **direct struct-field
reads** (`e.Name`, `doc.Relationships[i].Kind`) to **interface method calls**
(`e.Name()`). Behavior- and memory-neutral (concrete type still used at
runtime). Big packages are sub-split into ~150–300-site chunks. A CI lint
forbids *new* direct field reads in a package once migrated.

Ordering heuristic: smaller/leaf packages first (fast wins, exercise the
interface), then the big three. Site counts are approximate.

**mcp (~1023 sites) — 4 PRs:**
- **M1** mcp query/find/expand handlers (~280)
- **M2** mcp traversal + adjacency consumers (`traversal.go`, paths/traces) (~260)
- **M3** mcp scoring/BM25 + search fusion (`scoring.go`) (~250)
- **M4** mcp remaining (whoami/stats/cross_links/labels/misc) (~230)

**dashboard (~1097 sites) — 5 PRs:**
- **M5** dashboard graph/topology panel data assembly (~230)
- **M6** dashboard paths/flows panels (~230)
- **M7** dashboard entity/detail + inspect serialization (~230)
- **M8** dashboard search/filter surfaces (~220)
- **M9** dashboard remaining (metrics, overview, misc) (~187)

**engine (~741 sites) — 3 PRs** *(read sites only; engine write path untouched):*
- **M10** engine post-index read/verification passes (~250)
- **M11** engine graph-algo attribute readers (community/pagerank/centrality) (~250)
- **M12** engine remaining read consumers (~241)

**resolve (~368 sites) — 1–2 PRs:**
- **M13** resolve residual-repair + binding readers (~368; split to 2× ~184 if diff is large)

**links (~347 sites) — 1 PR:**
- **M14** links cross-repo edge readers (~347)

**docgen (~171 sites) — 1 PR:**
- **M15** docgen entity/relationship readers (~171)

**tail — 1 PR:**
- **M16** everything else that iterates `.Entities`/`.Relationships` across the
  remaining files (sweep to drive the direct-field-read grep-gate to 0).

> The ~468 files that iterate `.Entities`/`.Relationships` are covered by the
> above by package ownership; M16 mops up stragglers so the cutover gate
> (`0` direct field reads) can go green.

**Acceptance (every M-PR):** build/vet/test green; **behavior-neutral** (no
golden/output diff); migrated package passes the direct-field-read lint. No
memory change expected (still materialized).

---

## Phase C — Cutover (SEQUENTIAL; after ALL M-PRs + F3)

### C1 — flip loader to mmap-backed views, stop materializing, drop struct fields
- **Depends on:** all M-PRs (grep-gate: 0 direct field reads) + F3.
- **Scope:** loader stops building the heap `*graph.Document`
  (`internal/graph/load.go` resident path / `reloadLocked` `state.go:945`);
  `LoadedRepo` serves `EntityView`/`RelationshipView` from the mmap; **drop the
  now-unread struct fields** from `graph.Entity`/`graph.Relationship`
  (`graph.go:60-95,98+`). Default the serve path to mmap-backed (retire the F3
  flag's OFF branch for serve, or flip default ON).
- **Acceptance:** build/vet/test green; **membaseline shows the resident drop**
  (~on-disk graph size per hot repo, ×worktree on the multi-worktree layout;
  Go heap `inuse` drops by the `Document`'s share); query-latency harness holds
  hot-path p95 within budget. The memory north-star win lands here.

### C2 — Properties value-aliasing atop `[]propKV`
- **Depends on:** C1 + the in-flight `[]propKV` refactor merged.
- **Scope:** `EntityView.Property()/Properties()` alias property **values** out
  of the mmap; keys already interned. Coordinate merge order per ADR-0027
  §Interaction.
- **Acceptance:** build/vet/test green; no per-value key allocation on the read
  path (alloc benchmark); membaseline shows the property-vector share moving
  off-heap.

### C3..Cn — per-field HOT/COLD tuning toward endpoint (a)
- **Depends on:** C1 (+ C2). One small PR per field (or small batch) demoted
  from resident to zero-copy-cold via the tunable seam.
- **Scope:** move a field's tier inside the view impl / resident-index builder
  only — **no consumer changes** (interface signatures stable).
- **Acceptance:** build/vet/test green; membaseline shows incremental resident
  drop; query-latency harness confirms hot-path p95 stays within budget
  (**this harness is the gate** — a field that regresses hot paths stays
  resident, i.e. we stop at endpoint (b) for that field). Iterate until the
  measured (a) endpoint or the latency budget binds.

---

## Dependency graph (summary)

```
F1 ─▶ F2 ─▶ F3 ─┐
        │        ├─▶ C1 ─▶ C2 ─▶ C3..Cn
        └▶ M1..M16 (parallel) ─┘        (each Cx gated on membaseline + latency harness)
```

- F1 → F2 → F3 strictly sequential (F3 needs the flag+harness before any C).
- M1..M16 may proceed in parallel once F2 lands (they only need the interface).
- C1 needs **both** all M-PRs (grep-gate 0) **and** F3.
- C2 additionally needs the `[]propKV` refactor.
