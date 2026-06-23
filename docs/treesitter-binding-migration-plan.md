# tree-sitter binding migration plan (B2, #5418, ADR 0023)

Status: **Phase 0 landed** (abstraction + Go migrated end-to-end on the official
binding, behind `-tags ts_official`). This document is the executable plan for
Phases 1–4 — exact API mapping, per-package site counts, batch order, and the
per-language validation gate. It is the follow-up artifact to
`docs/adrs/0023-migrate-to-official-tree-sitter-binding-per-language-modules.md`.

---

## 0. What Phase 0 delivered

- **`internal/treesitter/ts`** — the binding-agnostic façade. Minimal interface
  modelled on grafel's *real* CST usage (no query engine, no cursors):
  `Node` (`Type`, `Child`, `NamedChild`, `ChildCount`, `NamedChildCount`,
  `ChildByFieldName`, `Parent`, `StartByte`/`EndByte`, `StartPoint`/`EndPoint`,
  `IsNamed`, `IsError`, `String`), `Tree` (`RootNode`, `Close`), `Parser`,
  `Language` (carries an `Adapter()` tag), `Adapter`. Int widths kept as the
  extractors expect (`StartByte`/`ChildCount` as `uint32`); the official adapter
  narrows from `uint` at the boundary. **Nil contract:** a missing node is an
  untyped-nil `ts.Node`, so `if n == nil` keeps working.
- **`ts/smacker`** — adapter over `smacker/go-tree-sitter`, no behaviour change.
  Every grammar runs on it by default. `WrapLanguage`/`WrapTree` let the factory
  keep producing smacker trees while extractors consume the façade.
- **`ts/official`** — adapter over `tree-sitter/go-tree-sitter` v0.24.0.
- **`ts/grammars/golang`** — the Go grammar via `tree-sitter-go/bindings/go`,
  ABI-pinned to **v0.23.4**, wrapped as a `ts.Language`.
- **Go extractor migrated** (`internal/extractors/golang/*`): all 16 non-test
  files traverse `ts.Node` instead of `*sitter.Node`. By default it runs on the
  smacker adapter (façade-level no-op); under `-tags ts_official` it parses Go via
  the official binding.
- **Factory routing** (`internal/treesitter/parser.go` +
  `adapters_default.go` / `adapters_official.go`): per-language adapter selection.
  `ParseResult.TSTree` (façade tree, always set) added alongside `ParseResult.Tree`
  (concrete smacker tree, set for non-migrated languages). `FileInput.TSTree`
  added; the pipeline (`cmd/grafel/index.go`, `internal/daemon/extract/subproc.go`)
  stamps it.
- **ABI guard + smoke test**: `abiGuard()` runs once per migrated language before
  the first real parse (asserts a sane, non-error root → catches an ABI mismatch
  that compiles but would SIGSEGV at `RootNode`). `golang_smoke_test.go` is the
  per-grammar smoke parse.

### Validation done in Phase 0
- `go build ./...`, `go vet`, `gofmt -l` clean (default build).
- Go extractor suite passes `-count=1` through the façade (smacker adapter) — **no
  extraction delta** from introducing the abstraction.
- Python extractor suite (still on smacker) passes — no pipeline regression.
- Official Go smoke-parse passes (`-run TestGoSmokeParse`) on v0.24.0 + grammar
  v0.23.4.

---

## 1. ⚠️ Co-link blocker (the gating Phase-1 problem)

**Finding (new — the ADR PoC built ONE grammar in a throwaway module and never
co-linked smacker).** The `smacker/go-tree-sitter` bundle and the official
`tree-sitter/go-tree-sitter` module each **statically vendor the same tree-sitter
C runtime** (`lib/src/*.c`) under **identical symbol names** (`_ts_stack_*`,
`_ts_parser_*`, …). A single Go binary that links **both** fails at link time:

```
ld: 247 duplicate symbols   (e.g. _ts_stack_node_count_since_error, _ts_stack_clear)
clang++: error: linker command failed with exit code 1
```

macOS `ld` has no `--allow-multiple-definition` equivalent, so this is a hard
error. Consequence: **you cannot have smacker-backed and official-backed grammars
co-resident in one binary** as written. Phase 0 therefore gates the official path
behind `-tags ts_official`; default builds link only smacker.

**This must be resolved before Phase 1 ships a mixed binary.** Options (decide in
Phase 1):

1. **One runtime for all grammars (preferred).** Move *every* grammar onto the
   official runtime in one cut, compiling grammar C against a single
   `tree-sitter/go-tree-sitter`. Removes the dup by removing smacker entirely —
   but requires every language to have an official-style binding at once (proto is
   the blocker; see §4), so it can't be incremental.
2. **Symbol prefixing.** Build one binding's C with a `-D`-renamed symbol prefix
   (or `objcopy --redefine-syms`) so the two runtimes don't collide. Lets smacker
   and official co-exist during the incremental migration. Needs a build-system
   hook (CGO `#cgo` flags or a vendored, prefixed copy) and ABI care — the two
   runtimes are different versions.
3. **Out-of-process grammar host.** Run official-backed grammars in the existing
   subprocess extractor (`internal/daemon/extract`) and smacker-backed ones in the
   parent (or vice-versa). Heaviest; reuses infra already present.

Recommendation: prototype **(2) symbol prefixing** first (keeps incrementalism);
fall back to **(1) one-shot cutover** if/when proto is resolved.

---

## 2. API rename mapping (smacker → official), absorbed by the façade

The façade hides all of these; this table is for anyone porting a call site
directly (or auditing the adapter).

| smacker | official | façade exposes | Notes |
|---|---|---|---|
| `Node.Type() string` | `Node.Kind() string` | `Type()` | pure rename |
| `Node.Child(int) *Node` | `Node.Child(uint) *Node` | `Child(int)` | adapter casts index |
| `Node.ChildCount() uint32` | `Node.ChildCount() uint` | `ChildCount() uint32` | adapter narrows |
| `Node.NamedChild(int)` | `Node.NamedChild(uint)` | `NamedChild(int)` | index cast |
| `Node.NamedChildCount() uint32` | `… uint` | `uint32` | narrow |
| `Node.ChildByFieldName(string)` | same | same | — |
| `Node.StartByte()/EndByte() uint32` | `… uint` | `uint32` | narrow |
| `Node.StartPoint()/EndPoint() Point` | `StartPosition()/EndPosition() Point` | `StartPoint()/EndPoint()` | rename; `Point.Row/Column` `uint32`←`uint` |
| `Node.Parent()/IsNamed()/IsError()` | same | same | — |
| `Node.IsNull()` | *(none)* | — | use `== nil` (1 site repo-wide; 0 in Go) |
| `Node.Content(src)` | `Node.Utf8Text(src)` | *(not in façade)* | grafel byte-slices via Start/EndByte |
| `Node.String()` | `Node.ToSexp()` | `String()` | debug S-expr |
| `Tree.RootNode()` | same | `RootNode()` | — |
| `sitter.NewParser()` | `ts.NewParser()` | `Adapter.NewParser(lang)` | — |
| `Parser.SetLanguage(*Language)` (no err) | `… error` | inside `NewParser` | ABI mismatch surfaces here |
| `Parser.ParseCtx(ctx, old, src) (*Tree, err)` | `Parser.Parse(src, old) *Tree` | `Parser.Parse(src) (Tree, err)` | no ctx |
| `pkg.GetLanguage() *Language` | `ts.NewLanguage(pkg.Language()) *Language` | grammar provider pkg | per-language |

---

## 3. Per-package migration surface (the work to port)

Files importing the smacker root package (the `*sitter.Node` consumers),
non-test: **150 files**; any smacker import (incl. grammar subpkgs): **178**;
`sitter.Node` references: **~1503**. The mechanical port per file is exactly the
Go-extractor port already done: swap the import to `…/internal/treesitter/ts`,
`*sitter.Node → ts.Node`, switch `file.Tree → file.TSTree`, and provide a
grammar provider package + a registry line.

Per-extractor-language non-test file counts (the natural migration units):

| Language | files | Language | files | Language | files |
|---|---|---|---|---|---|
| javascript | 34 | hcl | 6 | scala | 3 |
| python | 29 | csharp | 6 | html | 2 |
| java | 12 | cpp | 4 | groovy | 2 |
| ruby | 10 | yaml | 3 | elixir | 2 |
| php | 9 | swift | 3 | shell(bash) | 1 |
| rust | 6 | | | proto, lua, dockerfile, css | 1 each |
| kotlin | 6 | | | | |

Plus shared, non-language files to migrate last (they thread the tree type):
`internal/extractor/extractor.go` (`FileInput`), `internal/extractors/incremental.go`,
`internal/treesitter/*`, and 3 files in `internal/engine` + 1 in `internal/custom`.

> **Do not** migrate these one tiny file at a time across the boundary — migrate a
> whole **language** (all its files) in a batch so its tree never crosses adapters
> mid-extraction, then re-bench that language.

---

## 4. Per-language batch order & source status (from `grammars.lock` + ADR §3)

**21 clean** (official-style Go binding at the current source — drop-in):
bash, c, cpp, css, csharp, elixir, **go ✅(done)**, hcl, html, java, javascript,
kotlin, ocaml, php, python, ruby, rust, scala, sql, swift, typescript(+tsx).

**3 source-swap** (maintained binding under a *different* repo — also a freshness
win; update `grammars.lock` `source`):
- lua → `tree-sitter-grammars/tree-sitter-lua` (v0.5.0)
- toml → `tree-sitter-grammars/tree-sitter-toml` (v0.7.0)
- yaml → `tree-sitter-grammars/tree-sitter-yaml` (v0.7.2)

**2 caveat** (Go binding exists but its module still `require`s smacker as
runtime): dockerfile (`camdencheek/…`), groovy (`murtaza64/…`). Options: wait for
an upstream tag on the official runtime, maintain a thin grafel fork of the
~10-line `binding.go`, or keep on smacker behind the façade.

**2 true gap:**
- **markdown** — no Go binding anywhere, **but the markdown extractor is pure
  stdlib** (the loaded grammar is functionally unused). Action: **drop markdown
  from the registry** (no functional impact).
- **proto** — no official-style Go binding; the proto extractor parses the CST.
  **Real blocker.** Keep on smacker behind the façade, or vendor the C source +
  write a ~10-line binding. **This is the one language that keeps smacker alive**
  and therefore the gating item for fully removing smacker (and, with the
  symbol-prefix approach abandoned, for the one-runtime cutover).

### Recommended batch sequence

- **Phase 1 — high-value clean batch** (all `tree-sitter` org, official-style):
  python, java, typescript(+tsx), csharp, rust. *(Go already done.)* Resolve the
  **co-link blocker (§1) before this batch links a mixed binary.**
- **Phase 2 — remaining clean + source-swaps:** bash, c, cpp, css, elixir, hcl,
  html, kotlin, ocaml, php, ruby, scala, sql, swift + lua, toml, yaml.
- **Phase 3 — caveats + gaps:** dockerfile, groovy (fork-or-wait); **markdown
  dropped**; **proto stays on smacker** (or vendored).
- **Phase 4 — cut over + remove smacker** once proto is resolved.

---

## 5. Per-language validation gate (mandatory, before promoting off smacker)

For each language, before flipping its registry entry to the official adapter:

1. **ABI smoke-parse** (the `abiGuard` + a `grammars/<lang>/_smoke_test.go`): parse
   trivial valid source, assert non-nil non-error root and expected top kind. This
   catches the v0.25-vs-v0.24 SIGSEGV class **at CI/startup**, not in prod.
2. **Pin to an ABI-compatible grammar version** against runtime v0.24.0 (Go: the
   v0.23.4 pin is the proven pair). Renovate's `grammar-bump`+`needs-benchmark`
   routing (A1) must gate any bump on this.
3. **B1 fidelity/coverage benchmark**: re-run the language's extractor benchmark;
   **require no regression** vs the smacker baseline (entity counts, edge counts,
   `error_ratio`) before promotion.
4. **A4 canary** (`docs/grammar-canary-baseline.json`): compare `ErrorRatio` /
   node-shape to baseline; refresh baseline after promotion (grammar version
   changed ⇒ expected shape may shift).
5. **#481 race re-test**: confirm whether the official binding still needs the
   `parseMu` global parse serialization (a perf freebie if it doesn't). Currently
   retained conservatively in `parseOfficial`.
6. **Release matrix**: re-run linux/darwin/windows × arch with `CGO_ENABLED=1`
   before shipping a batch (per-module changes the C compile units, not the
   toolchain; `osusergo` unaffected).
7. **License + supply-chain**: `grafel_license_audit` over the ~26 new modules
   (several orgs) and pin-by-digest before merge.

### Rollback
Per-language: flip the registry entry back to the smacker adapter — no code
revert (the façade makes both interchangeable). Whole-migration: smacker stays in
`go.mod` until Phase 4.
