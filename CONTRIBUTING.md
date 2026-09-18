# Contributing to grafel

## CI overview

Most PR CI is automatic; the expensive platform coverage is opt-in via the
**`ci:full`** label.

> **Maintenance note.** The tables below are hand-maintained and have rotted
> before — they described the pre-#6291 world (no automatic per-PR CI) long
> after `test.yml` gained a plain `pull_request:` trigger, and they named a
> `linux-smoke` workflow that does not exist in `.github/workflows/`. Re-derive
> from the `on:` blocks before trusting any row.

### What runs on every PR

| Workflow | Cost | Always? |
|---|---|---|
| `board-hygiene` (closure-keyword check) | ~5 s | Yes — all PRs |
| `cross-platform compile` (3-platform `go vet` / `go build`, no test bodies) | mins | Yes — all PRs |
| `module hygiene`, `node-type gate`, `quality`, `windows installers` | mins | Yes — all PRs |
| `coverage-docs` | mins | Yes, when the PR touches its `paths:` |
| `test` (ubuntu + windows, no `-race`) | tens of min | Yes — all PRs (#6291) |
| `test` (+ macos, and `-race` everywhere) | much longer — `-race` is ~5-10x | No — `ci:full` label or `workflow_dispatch` |
| `windows-cgo-smoke` (daemon healthz smoke, graduated from experiment in #2230) | ~5 min | No — `ci:full` label or `workflow_dispatch` |

---

### When does the full `test` matrix run on a PR?

The two-platform `test` matrix runs on every PR push automatically. To get the
third platform (macOS) and `-race`:

1. Apply the **`ci:full`** label (see below), OR
2. Use `workflow_dispatch` from the Actions tab

---

### Opt-in: `ci:full` label

Apply the **`ci:full`** label to trigger full 3-platform CI (`test` with macOS
and `-race`, plus `windows-cgo-smoke`) on any PR. Both follow the label on
**every subsequent push**, not only at the moment it is applied (#7219, #7244) —
and both carry a presence gate that turns RED if the coverage the label promises
silently stops being scheduled.

**When to use it:**

- You want to validate code changes across all platforms before merge.
- You're about to merge and want extra confidence.
- You changed something in `cmd/`, `internal/`, or `go.mod`/`go.sum` and want to test it before marking ready.

**How to apply:**

In the GitHub PR sidebar → Labels → select `ci:full`. The
`pull_request_target: labeled` trigger starts the labelled jobs immediately;
every later push re-runs them through the plain `pull_request` trigger, which
reads the same label.

---

### Opt-out: `board:exempt` label

Apply **`board:exempt`** to skip the closure-keyword check on chore PRs that legitimately don't map to an open issue:

- Typo fixes in docs
- `.gitignore` / `.editorconfig` tweaks
- CI formatting cleanups
- Emergency hotfixes that predate issue tracking

The label is checked in the `board-hygiene` workflow. Applying `board:exempt` (or removing it) re-triggers the workflow immediately via the `labeled`/`unlabeled` events, so the check resolves without requiring a re-push.

---

### Manual run: `workflow_dispatch`

Any workflow that supports `workflow_dispatch` can be triggered from the **Actions tab** in GitHub:

1. Go to `Actions` → select the workflow (e.g. `test`, `Linux Smoke Test`, `quality`).
2. Click **Run workflow** → choose a branch → click the green button.

Use this when you want to run CI on a branch that wouldn't otherwise trigger it automatically (e.g. a WIP branch, a branch that only touches docs).

---

### Where smoke runs

There is currently **no `linux-smoke` workflow** in `.github/workflows/` — this
section described one that no longer exists. Post-merge sanity on `main` comes
from the workflows with a `push: branches: [main]` trigger (`cross-platform
compile`, `module hygiene`, `node-type gate`, `quality`, `windows installers`,
`coverage-docs`). Note that `test` is **not** among them: it has no `push`
trigger at all, by design (#6056/#6064) — on a tag it is invoked by `release.yml`
through `workflow_call`.

---

### Release pipeline

Pushing a tag matching `v*` triggers the full release pipeline (`release.yml`):

- 5-platform binary builds (linux amd64/arm64, macos amd64/arm64, windows amd64)
- Checksums + GitHub Release creation
- Smoke also fires on the tag push

Tags should only be pushed from `main` after all CI is green.

---

### Scenario reference

| Scenario | Workflows triggered |
|---|---|
| Any PR (default) | `board-hygiene` + `cross-platform compile` + `module hygiene` + `node-type gate` + `quality` + `windows installers` + `test` (ubuntu + windows, no `-race`) |
| PR with `board:exempt` label | as above; `board-hygiene` passes via the label |
| PR with `ci:full` label | as above, plus macOS and `-race` in `test`, plus `windows-cgo-smoke` — on **every push** while the label is applied |
| Push to `main` | the `push: branches: [main]` workflows (`cross-platform compile`, `module hygiene`, `node-type gate`, `quality`, `windows installers`, `coverage-docs`). **Not** `test`. |
| Tag push (`v1.2.3`) | `release` pipeline, which calls `test` (3 platforms, `-race`) via `workflow_call` and blocks the publish on it |
| `workflow_dispatch` from Actions UI | Whichever workflow(s) you trigger |

## Performance tests

Performance and scaling assertions live behind the **`perf` build tag** and are
excluded from `test` — that is, from the release gate.

```bash
make test-perf                                  # everything, on a quiet machine
go test -tags perf ./internal/graph/ -run Scaling -v   # one of them
```

They also run weekly (and on demand) via the `perf` workflow. That job is
**advisory**: never make it a required check and never gate a release on it.

Separately, `go vet -tags perf ./...` runs as a step in the `test` workflow — so
the tagged files are compile-checked on every release tag, every `ci:full` PR
and every manual dispatch, i.e. everywhere the release gate itself runs. It is
also in `pre-merge`, which is dispatch-only. Since #6291 `test` has a plain
`pull_request` trigger, so this compile check does run on every PR push — but
only on the Linux leg, and it is still worth running `go vet -tags perf ./...`
locally while iterating on a perf-tagged file.

### Which side of the line is a test on?

The tag is not "slow tests" — it is **"assertions a shared runner cannot make"**.
Both of these are timing tests, and they belong in different places:

| | Belongs in the gate | Belongs behind `perf` |
|---|---|---|
| What it asserts | Something **completes** / does not deadlock, hang, or block | Something is **fast**, or scales at exponent X, or allocates under N bytes |
| Failure mode it catches | Unbounded: hangs forever, or costs 5s / 113s instead of 1ms | Bounded: costs 2x what it should |
| Budget sizing | As generous as possible while still separating from the failure mode | As tight as the measurement supports |
| Example | `readSourceWindow` under an fsnotify watcher — pre-fix it took exactly 5.000s, so a 1s bound catches it with a 1000x margin over the healthy path | `TestLouvainScalingExponent` — a fitted `N^1.45` bound that went red at `N^1.72` on a loaded Windows runner with no algorithmic change |

Rules of thumb when writing or reviewing one:

- **A hang guard is not a latency budget.** If the failure you are guarding
  against is "blocks forever" or "takes 5s instead of 1ms", pick a bound that
  sits comfortably between the two and leave it there. Do not tighten it later
  because "it only takes 3ms in practice" — that converts a correctness test
  into a flake generator without adding coverage.
- **Headroom is not immunity.** `TestIncremental_Performance_SingleFileEdit`
  ran in 116-249ms against a 1s budget — 4-8x headroom — and still failed at
  1.073s inside a loaded full-suite run. If a bound can be crossed by
  contention alone, it does not belong in the gate at any headroom.
- **Keep the correctness half in the gate.** When a test mixes both (e.g. "the
  incremental path is taken AND it is fast"), move the timing assertion behind
  the tag and make sure the correctness assertion still runs by default —
  duplicate it into an untagged test if nothing else covers it.
- **Log, don't assert, on numbers you cannot bound.** Several tests here log
  heap/alloc figures and assert only on structure. That is the right shape.

### What moving a test behind the tag costs

Gating is not free. Three losses were accepted knowingly when the tag was
introduced; if you are touching this area, these are the ones to weigh.

1. **The only leak guard on the Pass-4 pipeline.** `TestSingleRunPeak`'s
   "retained heap must be <50% of transient peak" is the sole assertion anywhere
   that the algorithm pipeline releases what it allocates. It is GC-timing
   dependent, which is why it is gated — but calling it "just a heuristic"
   undersells it. Nothing in the release gate now catches a pipeline leak.
2. **Concurrent `RunAlgorithms` is no longer race-detected in CI.**
   `TestOverlapPeak` is the only test that runs two `graph.RunAlgorithms`
   pipelines at once. The release gate runs `-race`; `perf.yml` runs with
   `-race` specifically to keep this one covered (see the note in that
   workflow), but on a weekly cadence rather than per-tag.
3. **Both asymptotic assertions are now weekly-only.** Louvain's O(E)-per-sweep
   property (`TestLouvainScalingExponent`) and
   `TestBuildIndexFromModules_SubQuadratic` are the only tests in the tree that
   would catch an algorithmic-complexity regression as opposed to a
   correctness one. A quadratic reintroduction can now land and sit unnoticed
   for up to a week.

If you add to the tag, add to this list.

### Do not add `-short` to CI

`testing.Short()` is **not** the mechanism for this. 22 files hold 53
`testing.Short()` guards and CI has never passed `-short`, so not one of them
has ever fired. Auditing what they actually guard: alongside genuinely slow
benches they cover the entire `internal/daemon/watch` watcher suite (debounce,
gitignore/skip-dir handling, worktree exclusion, extension acceptance), the
`internal/docgen` LLM-mode integration tests, `internal/daemon/extract`'s
end-to-end entity-equivalence tests, and `internal/extractors/golang`'s
large-file extraction — all correctness. Adding `-short` would silently narrow
the release gate, which is the exact defect class this mechanism exists to
prevent. Use the build tag.

Nor is there a middle path of "keep the guard, skip only the slow part": all 53
sit at the top of their function (max offset 12 lines), so every one of them is
all-or-nothing without restructuring the test first.
