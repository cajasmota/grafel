# Indexing Optimization Roadmap — time, RSS, and worktree-incremental

- **Status**: Draft for review
- **Date**: 2026-07-24
- **Author**: Jorge Cajas
- **Grounded by**: 4 parallel research passes (flows, indexing-time, RSS-structural, worktree-incremental) at HEAD f5e7aa033
- **Refs**: #5890 (write-side streaming epic), #5914 (extractor→SegmentedWriter), #5870/#5912 (read-side mmap/MultiReader), Phase-0 spec `2026-07-24-index-memory-observability-design.md`

## The measured baseline (monorepo-corpus: 427,301 entities / 1,852,622 rels / 31,820 files)

- **Wall time ≈ 1191s (~19m51s):** extraction ~268s (22%), **Tier B link passes ~600s (50%, fully serial)**, flows + graph write + group-algo ~150–250s.
- **Peak RSS ≈ 3.3 GB whole-machine:** ~3277 MB in the `index-internal` subprocess, ~443 MB in the engine link passes. Structural floor ≈ 0.8–1.0 GB; **the other ~2.3 GB is slack.**
- **Multiplier:** each concurrent agent worktree pays the full 3.3 GB / 19 min independently — nothing is shared.

## Cross-cutting foundation (unblocks two of the three goals)

**Swap the previous-graph heap load for an mmap read.** Both incremental paths call
`graph.LoadGraphFromDir` (`internal/extractors/incremental.go:383`, `cmd/grafel/index.go:977`),
which materializes the entire prior graph on the heap. Reading it through the existing
`fbreader.MultiReader` mmap instead is the single pivot that turns incremental from
"saves CPU but not RAM" into "memory-bounded," AND is the substrate under worktree
sharing. This is the highest-leverage single change in the whole roadmap.

## Reducible vs inherent (from research, with confidence)

### Time (Tier B link block is the target)
| Win | Mechanism | Est. | Confidence |
|---|---|--:|---|
| Parallelize independent link passes | 19 passes run serial on one core; most are independent → errgroup bounded by `AlgoCap` | large share of ~600s | high (structure certain; speedup needs cores) |
| Kill redundant source re-reads | ~7 passes each re-read+re-tokenize all 31,820 files → share one content/token cache | I/O-bound share | high |
| Per-repo hash-gate link passes | no change-detection today (group-algo already hash-skips) → skip unchanged repos | full-run cost on incremental | high; ties into worktree work |
| `MarshalIndent` → `Marshal` on big sidecars | pretty-print of 90+60+28 MB; sort already guarantees determinism | modest, free | high |

### RSS (subprocess arena is the target; floor ≈ a third)
| Win | Mechanism | Est. | Confidence |
|---|---|--:|---|
| Streaming write (#5890) | `StreamingWriter` exists but the index path feeds it a whole doc; doc + FB buffer coexist at write | 300–400 MB | high it's real; size est. |
| Free `merged` / scope resolver `idx` | second full entity copy built while `merged` + `idx` still alive (`index.go:4700,4792`) | 150–300 MB | high |
| Drop Bazel full-slice copy | `index.go:4775` copies every EntityRecord by value | 100–150 MB | high |
| Slice-back Properties on extraction records | `types.EntityRecord.Properties` still `map[string]string`; read side already uses `[]propKV` | 80–200 MB | med (populated-fraction NEEDS-PROFILING) |
| Intern low-cardinality strings index-side | `Kind`/`Language`/`Subtype`/`SourceFile` un-interned on write path | 50–150 MB | med (NEEDS-PROFILING) |
| `EntityRecord.Content` | large *if* populated; not found assigned in main extraction path | ? | LOW — profile first; could be #1 or ~0 |

**Irreducible-without-redesign:** the resolver Index (global random-access name→id, ~200–350 MB), one live copy of the records (~500–700 MB), the prev-graph load on the incremental path (needs a segmented queryable store to avoid). These are the #5914 SegmentedWriter direction.

### Worktree-incremental (the user's actual goal: N agents)
- **No sharing today** — each worktree is a path-hashed independent store.
- **Drift enumeration is cheap** — content-hash + commit-range git diff already implemented.
- **Bounded cross-ref re-resolution is feasible** — entity IDs are content-addressed, so re-extracting a file recreates the same IDs; inbound edges from unchanged files stay valid. Scoped resolver already exists (`sresolver/scoped.go`).
- **One honest gap:** "reference-stealing" — a drift file adds a symbol an unchanged file already resolved elsewhere. Narrow; needs a periodic full reconciliation as a safety net.
- **Design staging:** Option C (memory-bound incremental + convert the 50-file bail to a budget) → Option A (shared parent mmap segment + per-worktree drift delta segment + tombstones), the only design that makes N worktrees ≈ 3.3 GB + N × drift.

## Proposed phases

**Phase 0 — Measure (prerequisite for the RSS targets).** Land the memory-observability instrumentation (separate spec, already written). Run one clean index. This converts the LOW/MED-confidence RSS estimates into ranked byte attribution — critically, it settles whether `EntityRecord.Content` is the #1 target or ~0, and the resolver-Index real size. No optimization; no format change.

**Phase 1 — Certain quick wins (no profiling needed, low risk, additive).**
- Parallelize the independent Tier B link passes (time).
- Share a source-content/token cache across the sniffer passes (time).
- `MarshalIndent` → `Marshal` on large sidecars (time).
- Drop the Bazel full-slice copy; free `merged`/scope `idx` (RSS, ~250–450 MB, structurally certain).
Each is an independent PR under the standard TDD + adversarial-review gate.

**Phase 2 — The mmap-prev-graph foundation + memory-bounded incremental (Option C).**
- Swap `LoadGraphFromDir` → `MultiReader` on the prev-graph read (both seams).
- Convert `IncrementalMaxFiles` (50-file bail) into a memory/time budget so drifted worktrees stay incremental.
- Per-repo hash-gate the incremental-friendly link passes.
Result: single-index RSS drops (no whole-prev-graph heap copy) and drifted worktrees stop falling back to full.

**Phase 3 — Structural, format-touching (needs explicit go; gated on Phase 0 numbers).**
- Extraction-record slimming: `[]propKV` + string interning index-side (RSS).
- Streaming write (#5890): wire `buildDocument`→`StreamingWriter` so entities serialize-and-discard (RSS 300–400 MB).
- These change `graph.fb` internals / fbversion → breaking, forced reindex, release-gated.

**Phase 4 — Worktree Option A (the N-agent payoff).** Shared parent mmap segment + per-worktree drift delta segment + tombstone/shadow semantics + drift-scoped re-resolution + periodic reconciliation. Builds entirely on Phases 2–3 infrastructure. Turns N × 3.3 GB into 3.3 GB + N × drift.

## Sequencing rationale

Phase 0 first because the user's directive is "measure before optimizing," and three of the six RSS wins are confidence-gated on it. Phase 1 can run *in parallel* with Phase 0 — those wins are structurally certain and need no measurement. Phase 2's mmap foundation unblocks both the incremental memory win and the worktree sharing, so it precedes the worktree epic. Phases 3–4 are the big structural payoffs and are explicitly release-gated (format changes, forced reindex).

## Open decisions for the user

1. **Ambition:** stop after Phase 1–2 (bounded, low-risk, big time cut + meaningful RSS cut, no format change), or commit to the full program through Phase 4 (the N-agent memory win, but weeks + breaking format).
2. **Phase 0 gating:** land measurement first, or run Phase 1 quick-wins in parallel immediately (recommended — they don't need the numbers).
3. **Flows fix** (separate one-liner) is already in flight and independent of all of this.
