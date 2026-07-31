# Contributing to grafel

## CI overview

CI is **fast by default**: PRs run zero CI except board-hygiene. Full 3-platform tests are manual or opt-in. Post-merge always validates.

### What runs on every PR

| Workflow | Cost | Always? |
|---|---|---|
| `board-hygiene` (closure-keyword check) | ~5 s | Yes — all PRs |
| `test` (3-platform: ubuntu / macos / windows with MinGW) | ~30 min | No — manual or `ci:full` label only |
| `windows-cgo-smoke` (daemon healthz smoke, graduated from experiment in #2230) | ~5 min | No — manual or `ci:full` label only |
| `linux-smoke` | ~3 min | Post-merge + tag only |

---

### When does `test` run on a PR?

`test` does **not** run automatically on any PR by default. To trigger it:

1. Apply the **`ci:full`** label (see below), OR
2. Use `workflow_dispatch` from the Actions tab

---

### Opt-in: `ci:full` label

Apply the **`ci:full`** label to trigger full 3-platform CI (`test` + `windows-cgo-experiment`) on any PR.

**When to use it:**

- You want to validate code changes across all platforms before merge.
- You're about to merge and want extra confidence.
- You changed something in `cmd/`, `internal/`, or `go.mod`/`go.sum` and want to test it before marking ready.

**How to apply:**

In the GitHub PR sidebar → Labels → select `ci:full`. The `pull_request_target: labeled` trigger will start CI jobs immediately.

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

`linux-smoke` runs **only on push to `main`** and on **tag pushes (`v*`)**. It is not a PR gate — its job is post-merge sanity: confirm the binary builds and indexes a golden fixture before the commit is considered stable.

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
| Any PR (default) | `board-hygiene` only |
| PR with `board:exempt` label | `board-hygiene` (passes via label) |
| PR with `ci:full` label | `board-hygiene` + `test` (3 platforms) + `windows-cgo-smoke` |
| Push to `main` | `board-hygiene` (passes) + `test` + `linux-smoke` |
| Tag push (`v1.2.3`) | `board-hygiene` (passes) + `test` + `linux-smoke` + `release` pipeline |
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

### Do not add `-short` to CI

`testing.Short()` is **not** the mechanism for this. 29 files call it and CI has
never passed `-short`, so none of those guards has ever fired. Auditing what
they actually guard: alongside genuinely slow benches they cover the entire
`internal/daemon/watch` watcher suite (debounce, gitignore/skip-dir handling,
worktree exclusion, extension acceptance), the `internal/docgen` LLM-mode
integration tests, `internal/daemon/extract`'s end-to-end entity-equivalence
tests, and `internal/extractors/golang`'s large-file extraction — all
correctness. Adding `-short` would silently narrow the release gate, which is
the exact defect class this mechanism exists to prevent. Use the build tag.
