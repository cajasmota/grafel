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

## 5. Sequencing (Part D)

1. **B3 (this audit + `grammars.lock`)** ✓ + A1/A2 freshness alarm.
2. **B1** catch-up bump behind the fidelity/coverage benchmark.
3. **A3** calendar + **A4** parse-error canary.
4. **C1/C2** process; **C3** backfill for the catch-up window.
5. **B2** decoupling — assessment, may slip past 0.1.4.
