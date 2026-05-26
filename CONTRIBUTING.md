# Contributing to archigraph

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
