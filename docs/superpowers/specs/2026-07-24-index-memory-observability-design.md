# Index Memory Observability (Phase 0)

- **Status**: Draft for review
- **Date**: 2026-07-24
- **Author**: Jorge Cajas
- **Refs**: #5890 (write-side bounded-memory epic), #5822, #5726, #5931
- **Supersedes nothing.** This is the prerequisite phase for any indexing-memory work.

## Problem

A single index of a 427k-entity / 1.85M-relationship monorepo peaks at **3352 MB**
whole-machine, of which **3277 MB** is the `grafel index-internal` extraction
subprocess. The primary consumer of grafel is concurrent AI agents working on
separate git worktrees, each of which can trigger its own index. The peak
therefore multiplies by the number of concurrent agents.

grafel already has RSS-budget admission control that is supposed to prevent this
(`internal/daemon/sched/scheduler.go:17-24, 197-200`): a job is admitted only when
`sum(running predicted) + new <= BudgetMB`. **It is calibrated on a broken
signal.** The predictor prefers per-repo history from
`~/.grafel/repo-rss-history.json`, which is written from `observedPeakMB`
(`scheduler.go:1253, 1299-1316, 1492-1493`) — sampled via
`currentProcessRSSMB()` = `process.RSSBytes(os.Getpid())`, i.e. **the engine's own
RSS only**, on a 5 s ticker, as a *delta above baseline*.

Measured discrepancy on one run:

| source | value |
|---|--:|
| `repo-rss-history.json` `peak_rss_mb` | **443 MB** |
| 1 s external sampler, all processes | **3352 MB** |

A **7.6x understatement**. Against the observed `mem_limit: 2560MB`, the scheduler
believes ~5 concurrent indexes of this repo fit (5 × 443 = 2215). The true cost is
~16.5 GB.

## Why we cannot act yet

The subprocess holding the memory is the one process with **no instrumentation**:

- `net/http/pprof` is mounted only on the dashboard, i.e. the **serve** process,
  gated by `GRAFEL_DEBUG_PPROF` (`internal/dashboard/server.go:14, 421-476`).
- There is **no** `runtime.ReadMemStats` and **no** `runtime/pprof` anywhere in
  `cmd/grafel/index.go` or `internal/extractors/`.

We cannot say whether the 3277 MB is dominated by entity rows, relationship rows,
the global resolver tables, the fold passes, or the flow walkers. Any optimization
target chosen now would be a guess, and any "success" unverifiable.

Note also that both the full and incremental paths funnel through the same
fully-materialized `Document`: `internal/extractors/incremental.go:383`
(`graph.LoadGraphFromDir` — whole previous graph) and `:669`
(`fbwriter.WriteGraphGen(stateDir, doc)` — whole doc). Incremental indexing is a
CPU optimization, not a memory one. So one instrument covers all paths.

## Goals

1. Attribute the extraction subprocess's peak RSS to specific phases and specific
   allocation sites.
2. Produce a whole-machine memory curve across `index-internal`, `engine`, and
   `serve` for a single index, from one trace.
3. Make the data reproducible across repo sizes so we can see how each structure
   scales.

## Non-goals (explicit)

- **No optimization.** This phase changes no data structure, no algorithm, no
  format, and no scheduling behaviour.
- **No change to admission control.** Correcting `repo-rss-history.json` to record
  the true peak is the obvious follow-on, but it is a *behaviour* change and lands
  separately so this phase is provably inert.
- No change to `graph.fb` format or `fbversion`.
- Not enabled by default. All collection is opt-in via environment variable.

## Design

Three components. M1 and M2 answer different questions and compose; M3 is the
whole-machine frame around them.

### M1 — Phase-tagged memstats sampler (the *where*)

A goroutine in the `index-internal` child sampling `runtime.ReadMemStats` on a
ticker (default 250 ms), tagging each sample with the currently-active phase, and
appending to an NDJSON trace file.

Phases already exist as constants and require no new taxonomy
(`internal/progress/event.go:29-77`): `scanning`, `extracting_ast`,
`resolving_refs`, `running_algorithms`, `building_communities`,
`computing_centrality`, `materializing`, `detecting_links`, `computing_flows`,
`writing_graph`.

Per sample: `ts`, `phase`, `heap_alloc`, `heap_inuse`, `heap_sys`, `heap_objects`,
`stack_inuse`, `next_gc`, `num_gc`, plus process RSS from the existing
`internal/process` helper (`process.RSSBytes`) so heap and RSS can be compared —
the gap between them is itself a finding.

The phase is read from the existing `progress.Tracker` (`cmd/grafel/index.go:849`)
rather than introducing parallel state, so the trace cannot drift from what the
UI reports.

**Answers:** which phase peaks, the shape of the curve, and how much of RSS is Go
heap versus everything else.

### M2 — Heap profiles at phase boundaries (the *what*)

`runtime/pprof.WriteHeapProfile` written to `heap-<phase>-<seq>.pprof` at each
phase transition, plus one at the observed high-water mark. Analyzed offline with
`go tool pprof`.

Because Go's heap profile is allocation-site attributed, this yields a ranked list
of the structures actually holding bytes at the peak, which is exactly the input
needed to decide what is reducible versus irreducible.

**Answers:** *what* is holding the memory inside the peak phase.

### M3 — Whole-machine rollup (the *total*)

The child's trace covers only the child. `engine` and `serve` sample themselves
into the same trace directory using the same record shape, so one run produces one
directory that can be merged on `ts` into a single whole-machine curve.

This subsumes the external sampler used to find the original discrepancy and makes
the measurement reproducible by anyone, not just by someone running an ad-hoc
script.

### Gating and output

- Single env var `GRAFEL_MEMTRACE_DIR`. Unset (default) = fully inert: no
  goroutine, no ticker, no files.
- The engine propagates it to the `index-internal` child through the existing
  env-passing path (`cmd/grafel/daemon.go:1060` fork-exec).
- Optional `GRAFEL_MEMTRACE_INTERVAL` (default `250ms`).
- Output: `$GRAFEL_MEMTRACE_DIR/<run-id>/{child,engine,serve}.ndjson` plus
  `heap-*.pprof` from the child.
- Naming follows the existing `GRAFEL_DEBUG_PPROF` / `GRAFEL_BUG_*_OUT` precedent
  for opt-in diagnostics.

**Privacy:** memstats and RSS are numeric only. Heap profiles carry type and
allocation-site names, never file contents — but they can reveal internal symbol
names, so profiles are written only under the operator-specified directory and
never emitted to logs or telemetry. This matters because the corpus in use is
confidential; the trace must never carry repository content.

## Data flow

```
index-internal child
  progress.Tracker ──current phase──┐
  runtime.ReadMemStats ─────────────┼──> memtrace writer ──> child.ndjson
  process.RSSBytes ─────────────────┘                   └──> heap-<phase>.pprof
engine  ── self-sample ──> engine.ndjson
serve   ── self-sample ──> serve.ndjson
                                merged offline on ts ──> whole-machine curve
```

## Error handling

Instrumentation must never affect an index. Every failure path is best-effort and
silent-but-logged-once: if the directory is not writable, if a profile write
fails, or if the ticker cannot start, the sampler disables itself and the index
proceeds unchanged. No error from this subsystem may propagate into the index
result. This mirrors the existing precedent for the watcher PID registry
("best-effort: a registry write failure must never stop the watcher from doing its
job", `internal/cli/watch.go:222-223`).

## Testing

- Unit: sampler emits well-formed NDJSON with the expected fields; phase tagging
  follows the tracker; a non-writable directory disables the sampler without
  error.
- Unit: with `GRAFEL_MEMTRACE_DIR` unset, no goroutine is started and no file is
  created — the inert-by-default guarantee.
- Integration: a small fixture repo index under `GRAFEL_MEMTRACE_DIR` produces a
  trace whose phase sequence matches the progress sidecar's phase sequence for the
  same run. This ties the two observability channels together and would catch
  drift.
- Portability: no POSIX-only assumptions; the repo runs a 3-OS matrix on release
  tags. `process.RSSBytes` already has per-platform implementations.

## Success criteria

This phase succeeds when we can answer, with evidence rather than inference:

1. Which phase holds the peak, and what fraction of total RSS it represents.
2. The ranked top allocation sites at that peak, with byte counts.
3. For each of the top structures: does it scale with entities, with
   relationships, with file count, or is it fixed overhead?
4. How much of the child's RSS is Go heap versus non-heap (mmap, stacks, runtime).

Only then do we set a memory target, because only then is the split between
*irreducible* and *reducible* known.

## Follow-on (not in this phase)

- Feed the true peak (child included) into `repo-rss-history.json` so admission
  control stops under-counting by 7.6x. This alone bounds the concurrent-agent
  multiplier without reducing any single index's peak.
- Whatever structural reduction the data justifies, under #5890.
