# Grammar setup audit (B3, epic #5359 — milestone 0.1.4)

_Audit date: 2026-06-23. Source-of-truth manifest: [`grammars.lock`](../grammars.lock)._

This is the foundational deliverable of epic #5359 (Part D step 1): inventory the
real tree-sitter grammar setup so we know what is stale before building the
freshness alarm (A1/A2) and doing the catch-up bump (B1).

## 1. The binding dependency

- **Dep:** `github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82`
  (`go.mod` line 18).
- **Pinned commit `dd81d9e9be82` — date 2024-08-27** (~22 months stale at filing).
- **No `replace` directive, no fork.** The audit explicitly confirmed there is no
  `replace`-to-a-fork already freshening grammars. `go.mod` has zero `replace`
  directives.
- **CRITICAL FINDING — the binding is at upstream HEAD and unmaintained.**
  `gh api compare/dd81d9e9be82...master` on `smacker/go-tree-sitter` returns
  `ahead_by: 0, status: identical`. The pinned commit *is* the current HEAD of
  the upstream binding. There have been **no commits to smacker/go-tree-sitter
  since 2024-08-27** — the binding appears abandoned.

### Consequence for the freshness plan
- **A1 (Renovate/Dependabot on the dep) will find nothing newer** — the dep is
  already at its upstream HEAD. A1 is still worth wiring (cheap, catches the day
  the binding revives) but it is NOT the alarm.
- **A2 (per-grammar upstream tracking via `grammars.lock`) is the real alarm** —
  it tracks each `tree-sitter/tree-sitter-<lang>` independently of the dead binding.
- **B2 (decouple to the official binding) gains urgency.** The official
  `github.com/tree-sitter/go-tree-sitter` is alive: latest release **v0.24.0**,
  latest commit `c9492002f76e` (2025-11-12), with per-language grammar Go
  modules that Renovate can bump independently. This is the only path back to
  automated freshness.

## 2. Grammar-backed vs heuristic-only languages

Authoritative source: the `languageRegistry` in
`internal/treesitter/parser.go` (28 grammars loaded via smacker imports).

**Grammar-backed (28):** bash (alias shell), c, cpp, css, csharp, dockerfile,
elixir, go, groovy, hcl (alias terraform), html, java, javascript, kotlin, lua,
markdown, ocaml, php, proto, python, ruby, rust, scala, sql, swift, toml,
typescript (alias tsx), yaml.

**Heuristic-only (NO grammar dep — out of scope for freshness):** avro, cobol,
bicep, zig, astro, svelte, vue, elm, fish, jcl, jsonschema, just, bazel, lisp,
mage, razor, reasonml, config, task, sresolver. These have their own extractor
drift (noted in the epic for a separate pass). Note: the **markdown** extractor
is pure-stdlib even though a markdown grammar is loaded in the registry.

## 3. Per-grammar staleness (spot-check of the high-value four)

The smacker bundle vendors grammar C sources with **no per-grammar version
provenance** — only ABI `LANGUAGE_VERSION` numbers in each `parser.h`, not the
upstream grammar semver. So the bundled version is recorded as the binding
snapshot date (2024-08-27); upstream-latest is queried live. Full table in
`grammars.lock`.

| Language | Upstream repo | Bundled (smacker snapshot) | Upstream latest release | Upstream last commit |
|---|---|---|---|---|
| Java | tree-sitter/tree-sitter-java | 2024-08-27 | v0.23.5 | 2025-09-15 |
| C# | tree-sitter/tree-sitter-c-sharp | 2024-08-27 | v0.23.5 | 2026-06-02 |
| Python | tree-sitter/tree-sitter-python | 2024-08-27 | v0.25.0 | 2025-09-15 |
| TypeScript | tree-sitter/tree-sitter-typescript | 2024-08-27 | v0.23.2 | 2025-01-30 |

All four (and every grammar-backed language) have moved materially ahead of the
2024-08-27 snapshot. C3 backfill targets flagged in `grammars.lock`:
C# primary constructors + collection expressions, Java sealed types + record
patterns, Python 3.12+ PEP 695 type params, TS const type params.

## 4. A4 prerequisite — does `fidelity` already expose per-language parse errors?

**Partially — the per-parse signal exists but is NOT aggregated per language.**

- The `fidelity` metric (`internal/mcp/tools.go:2947`,
  `internal/mcp/docgen_repair_tools.go`) is an **IMPORTS-resolution** metric:
  `1 − (unresolved IMPORTS / total IMPORTS)`. It is **not** a parse-error-node
  rate. A4 cannot build on it directly.
- However, the parser **already computes a per-parse error-node ratio**:
  `ParseResult.ErrorRatio = error_nodes / total_nodes`
  (`internal/treesitter/parser.go:160-162, 246-250`, via `countNodes`). It is
  used today as a per-file fault-tolerance gate (`maxErrorRatio = 0.10`, files
  above are rejected) and emitted **only as an OTel span attribute**
  (`error_ratio` on the `treesitter.parse` span, `parser.go:256`).
- `ErrorRatio` is **not aggregated per language, not persisted to the graph, and
  not exposed in any metric/stats surface** (confirmed: no `ErrorRatio` reads
  outside `parser.go`).

**A4 verdict:** the raw per-parse signal is already there; A4's work is to
**aggregate `ErrorRatio` by language during indexing, persist a baseline, and
alert on per-language spikes** — not to compute error nodes from scratch, and
not to extend `fidelity` (different axis).

## 4a. A2 — the monthly freshness alarm (built)

The freshness alarm is live as a scheduled GitHub Action plus a small Go tool.

- **Checker:** `tools/grammar-freshness` (standalone, zero `internal/` imports).
  It reads `grammars.lock`, and for each grammar-backed language queries the
  upstream `source` repo's latest **release/tag** via the GitHub API, falling
  back to the **default-branch latest commit date** when a repo has no releases.
  It compares the upstream commit date to the bundled smacker snapshot
  (`2024-08-27`) and reports each grammar as `STALE`, `CURRENT`, or `UNKNOWN`
  (unreachable). Run it locally:

  ```sh
  GITHUB_TOKEN=$(gh auth token) go run ./tools/grammar-freshness            # human table
  GITHUB_TOKEN=$(gh auth token) go run ./tools/grammar-freshness -format markdown  # issue body
  ```

  It exits non-zero **only** on a hard error (unreadable manifest, or *every*
  upstream lookup failing). Finding stale grammars is reported, not a failure.
  It is rate-limit-aware (honours the reset header once) and resilient to
  individual repos being unreachable.

- **Workflow:** `.github/workflows/grammar-freshness.yml` runs on a **monthly
  cron** (06:00 UTC on the 1st) plus manual `workflow_dispatch`. It is *not*
  wired to push/PR to stay inside free-tier minutes (CI policy). With minimal
  permissions (`issues: write`, `contents: read`) it runs the checker and, if
  any grammar is stale, **creates or updates a single tracking issue** —
  identified idempotently by the stable label **`grammar-freshness`** (title
  fallback) — whose body is the checker's markdown table of stale grammars and
  the last-checked date. Re-runs edit the same issue rather than spamming new
  ones.

- **How to read the tracking issue:** the table lists every grammar whose
  upstream has moved ahead of the bundled snapshot, with the upstream latest
  release/commit and an approximate months-behind figure. Because the smacker
  binding is unmaintained, expect **most/all 28 grammars to show stale** — that
  is the intended signal motivating the B1 catch-up bump and the B2 decoupling.
  A dry run at audit time flagged **24 of 28** stale (the 4 current — lua,
  proto, toml, yaml — have upstreams that genuinely predate the snapshot).

- **`last_verified` refresh:** the manifest's `last_verified` / upstream-latest
  columns are refreshed manually when a maintainer reconciles the tracking issue
  (e.g. after a catch-up bump). The cron itself reports against the committed
  manifest and does not auto-commit, keeping the Action read-only on the repo.

## 4b. A4 — runtime parse-error-node canary (#5414)

The gold-standard, **version-agnostic** freshness alarm. A2 tells you a grammar's
upstream has moved; A4 tells you that the bundled grammar is *actually failing to
parse* the code you index — the direct symptom of unhandled new syntax —
regardless of any version number.

### What it tracks

tree-sitter is error-tolerant: when it hits syntax it does not recognise it emits
`ERROR` nodes instead of failing. A per-parse `ErrorRatio = error_nodes /
total_nodes` already exists on `ParseResult` (`internal/treesitter/parser.go`),
used today only as a per-file gate (`maxErrorRatio = 0.10`) and an OTel span
attribute. A4 **aggregates that ratio per language across an index run**,
node-weighted, and compares it to a baseline:

- For every parse, both the in-process path (`cmd/grafel/index.go`) and the
  subprocess path (`internal/daemon/extract/subproc.go`) fold the parse's
  `ErrorRatio` + `NodeCount` into a per-language accumulator
  (`treesitter.ParseErrorCanary`). `.tsx`/`.jsx` files roll up under
  `typescript`/`javascript` (keyed by the classifier language, not the tsx parse
  override).
- The subprocess path reports its per-language stats in `BatchStats.ParseErrors`;
  the coordinator (`internal/daemon/extract/coordinator.go`) merges them across
  batches into `Result.ParseErrors`.
- Per language the canary records **files parsed, total nodes, error nodes**, and
  the node-weighted **aggregate error rate** (`error_nodes / total_nodes`).

### Where to read it (the stats surface)

The report is written into the `graph-stats.json` sidecar next to `graph.json`
(`internal/graph/graph.go` `GraphStatsSidecar`):

- `parse_error_spike` — top-level boolean alarm. `true` ⇒ at least one language
  spiked vs baseline. Dashboards / a future cron can read this without decoding
  the full report.
- `parse_error_canary` — the full `treesitter.CanaryReport`: the thresholds used,
  the overall `spiked` flag, and a per-language array with `current_rate`,
  `baseline_rate`, `delta`, `files`, `total_nodes`, `error_nodes`, `spiked`, and a
  `reason` (`"abs"` or `"rel"`).

On a spike the indexer also logs a `WARN parse-error canary SPIKE language=… …`
line to stderr at index time, pointing back to this document.

### Baseline + spike detection

- The baseline lives at **`docs/grammar-canary-baseline.json`** (committed
  source-of-truth) — overridable with `GRAFEL_CANARY_BASELINE`. Its JSON shape is
  exactly a `Snapshot()` (`{version, by_lang: {lang: {files, total_nodes,
  error_nodes}}}`), so a known-good run's snapshot can be persisted back as the
  next baseline. A **missing** baseline file is not an error: a first-ever run
  records without spiking.
- A language **spikes** when either test trips (zero-tolerant — a language with no
  parsed nodes never spikes):
  - **absolute:** `current_rate − baseline_rate ≥ GRAFEL_CANARY_ABS_DELTA`
    (default **0.02**, i.e. +2 percentage points). Always applies, including for a
    first-seen language above the threshold.
  - **relative:** `current_rate ≥ baseline_rate × GRAFEL_CANARY_REL_FACTOR`
    (default **2.0**, i.e. doubled). Only applies once the baseline carries
    ≥ 200 nodes and a non-zero rate, so noise on tiny baselines does not raise
    false alarms.

### Refreshing the baseline

Because the baseline captures what the **bundled grammars** produce (not any one
repo), it travels with the grammar, not the indexed code. After a deliberate
grammar change (e.g. the B1 catch-up bump) or once a spike is confirmed-benign,
refresh it: take a clean index run's `parse_error_canary.languages[].{total_nodes,
error_nodes,files}` (or call `treesitter.SaveBaseline`) and commit the updated
`docs/grammar-canary-baseline.json`. A rising baseline that you accept is how you
acknowledge "this is the new normal for this grammar."

## 4c. A1 — Renovate dependency-bump automation (#5410)

General Go-module bump automation, wired and **grammar-ready**.

- **Config:** [`renovate.json`](../renovate.json) at the repo root. Extends
  `config:recommended` + dependency dashboard, runs on a **monthly** schedule to
  limit PR noise, groups routine Go modules into one PR, and uses
  `separateMajorMinor` / `separateMultipleMajor`. No auto-merge anywhere. There
  is **no** `.github/dependabot.yml` — Renovate is the single dependency-bump
  tool (don't double up).
- **Grammar routing:** a dedicated `packageRules` entry matches the grammar
  binding (`smacker/go-tree-sitter`), the official decouple target
  (`tree-sitter/go-tree-sitter`), and any future per-language
  `tree-sitter/tree-sitter-<lang>` modules. Those bumps get a distinct
  `grammar-bump` + `needs-benchmark` label so a grammar bump routes to the B1
  benchmark gate, **never** auto-merge.
- **Honest framing — A1 is blind today.** As §1 establishes, the smacker binding
  is pinned at its own upstream HEAD and unmaintained, so Renovate finds nothing
  newer on it. A1 still earns its place for (a) the repo's *other* Go deps, and
  (b) the day the binding revives or **B2 (#5418)** splits grammars into
  per-language modules — at which point Renovate auto-PRs each grammar
  independently. Until then, **A2 (#5411) is the real grammar alarm.**

## 4d. A3 — language-release calendar (#5413)

The **proactive** leg: fire ahead of known release dates rather than waiting for
an alarm to catch up.

- **Doc:** [`docs/language-release-calendar.md`](./language-release-calendar.md)
  — a cadence table for the predictable-cadence languages (Java Mar/Sep, C#/.NET
  Nov, Python Oct, Go Feb/Aug, TS ~quarterly, Rust ~6wk, Swift ~Sep, PHP Nov,
  Ruby Dec, …; irregular ones marked as such), plus a per-release checklist that
  feeds the **C1 triage process (#5415)** — for each version N, verify the new
  syntax parses (via the A4 canary / A2 cron) and that the extractors model the
  new constructs (modeling gap → C2 recipe #5416, C3 backfill #5417).
- **Cron:** `.github/workflows/language-release-calendar.yml` — monthly cron +
  `workflow_dispatch`, no push/PR (free-tier policy), minimal perms
  (`issues: write`, `contents: read`). It computes the predictable release
  windows landing in the next ~8 weeks and opens/updates a **single idempotent
  reminder issue** (stable label `grammar-release-watch`) pointing back at the
  calendar.
- **How it complements the alarms:** A2 tells you a grammar's upstream moved; A4
  tells you parsing is *actually* failing; **A3 nudges you before either gap
  opens** for a scheduled release. All three reconcile against
  [`grammars.lock`](../grammars.lock).

## 5. Sequencing (Part D)

1. **B3 (this audit + `grammars.lock`)** ✓ + **A1** Renovate ✓ + **A2** cron ✓.
2. **B1** catch-up bump behind the fidelity/coverage benchmark.
3. **A3** calendar ✓ + **A4** parse-error canary ✓.
4. **C1/C2** process; **C3** backfill for the catch-up window.
5. **B2** decoupling — assessment, may slip past 0.1.4.
