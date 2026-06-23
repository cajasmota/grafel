# tree-sitter cutover plan (B2, #5418, ADR 0023, epic #5359, milestone 0.1.4)

Status: **planned** (this is the cutover spec, not the cutover). Follows
Phase 0 (`docs/treesitter-binding-migration-plan.md`, landed: façade +
`ts/official` + Go on the official binding behind `-tags ts_official`) and ADR
`docs/adrs/0023-*.md`.

> **This is a one-runtime big-bang.** The smacker bundle and the official
> `tree-sitter/go-tree-sitter` module each statically vendor the *same*
> tree-sitter C runtime under identical symbol names, so a single binary that
> links both fails at link time with **247 duplicate C symbols** (macOS `ld`,
> no `--allow-multiple-definition`). The incremental "mixed binary" path is
> therefore **closed**. The cutover removes the collision by removing smacker:
> **every grammar moves to the official runtime in one PR; smacker is dropped
> entirely.** This was confirmed by a bounded co-link test (§8).

---

## 1. Runtime version + ABI range (the bound on everything else)

**Standardize on `github.com/tree-sitter/go-tree-sitter` v0.24.0** (2024-10-14).

- It is the **newest published tag** of the official Go binding. Despite a
  tree-sitter *core* v0.25.x / v0.26.x existing, the **Go binding has no tag
  past v0.24.0** (tags: v0.23.0, v0.23.1, v0.24.0 — verified on the repo's
  `/tags`). There is no "latest stable newer than v0.24.0" to chase; v0.24.0 is
  it, and it is the pin Phase 0 already productized.
- **ABI range it accepts:** v0.24.0 vendors a tree-sitter runtime whose
  `include/tree_sitter/api.h` declares (verified):

  ```
  #define TREE_SITTER_LANGUAGE_VERSION                14
  #define TREE_SITTER_MIN_COMPATIBLE_LANGUAGE_VERSION 13
  ```

  So the runtime accepts grammars emitting **ABI 13 or 14**. A grammar emitting
  **ABI 15** loads but **SIGSEGVs at `RootNode()`** — this is exactly the
  ADR-0023 §6 PoC failure (runtime v0.24.0 + tree-sitter-go v0.25.0 crashed;
  v0.23.4 worked). **ABI 15 is the modern tree-sitter-CLI (the v0.24/v0.25
  grammar generation); ABI 14 is the v0.23.x grammar generation.** This single
  fact drives the entire version matrix: **pin each grammar to its freshest tag
  whose generated `src/parser.c` carries `#define LANGUAGE_VERSION 14` (or 13)**.

> **Why not jump the runtime to a v0.25 core?** No official Go binding ships it.
> Bumping the runtime would mean self-vendoring the core + maintaining the cgo
> binding — out of scope and self-defeating (the whole point of B2 is to get
> *off* a hand-maintained binding). Revisit only if/when `go-tree-sitter`
> publishes a v0.25 tag; then the whole matrix re-floats to ABI 15 in one bump
> (a future Renovate-gated event, not this cutover).

---

## 2. The version matrix (27 grammar-backed languages)

ABI column = the `LANGUAGE_VERSION` baked into that tag's `src/parser.c`
(spot-verified by fetching each `parser.c`; all chosen versions are ABI **14**,
inside the v0.24.0 runtime's 13–14 window). "why-not-latest" calls out every
pin-back. **markdown drops** (§6) so it is not in the matrix → **26 grammar
modules** ship.

| language | chosen module (Go binding) | chosen ver | grammar ABI | upstream-latest | why-not-latest |
|---|---|---|---|---|---|
| bash | `tree-sitter/tree-sitter-bash/bindings/go` | **v0.23.3** | 14 | v0.25.1 | latest is ABI 15 (SIGSEGV) → newest v0.23.x |
| c | `tree-sitter/tree-sitter-c/bindings/go` | **v0.23.6** | 14 | v0.24.2 | v0.24.2 is ABI 15 → newest v0.23.x |
| cpp | `tree-sitter/tree-sitter-cpp/bindings/go` | **v0.23.4** | 14 | v0.23.4 | **already latest** (no pin-back) |
| csharp | `tree-sitter/tree-sitter-c-sharp/bindings/go` | **v0.23.1** | 14 | v0.23.5 | v0.23.5 is ABI 15 → newest ABI-14 tag (v0.23.1) |
| css | `tree-sitter/tree-sitter-css/bindings/go` | **v0.23.2** | 14 | v0.25.0 | latest is ABI 15 → newest v0.23.x |
| dockerfile | **vendored** `camdencheek/tree-sitter-dockerfile` C + grafel binding | **v0.2.0** | 14 | v0.2.0 | grammar already latest & ABI 14; binding caveat → §4 |
| elixir | `elixir-lang/tree-sitter-elixir/bindings/go` | **v0.3.4** | 14 | v0.3.5 | v0.3.5 ABI 15 → newest ABI-14 tag |
| go | `tree-sitter/tree-sitter-go/bindings/go` | **v0.23.4** | 14 | v0.25.0 | latest ABI 15 (the PoC crash) → v0.23.4 (Phase-0 proven) |
| groovy | **vendored** `murtaza64/tree-sitter-groovy` C (regen ABI 14) + grafel binding | **regen** | 14 (after regen) | initial | binding caveat + HEAD is ABI 15 → §4 |
| hcl | `tree-sitter-grammars/tree-sitter-hcl/bindings/go` (**source swap**) | **v1.1.x** | 14 | v1.2.0 | v1.2.0 ABI 15; v1.1.x ABI 14. New home has bindings/go on official runtime |
| html | `tree-sitter/tree-sitter-html/bindings/go` | **v0.23.2** | 14 | v0.23.2 | **already latest** |
| java | `tree-sitter/tree-sitter-java/bindings/go` | **v0.23.5** | 14 | v0.23.5 | **already latest** |
| javascript | `tree-sitter/tree-sitter-javascript/bindings/go` | **v0.23.1** | 14 | v0.25.0 | latest ABI 15 → newest ABI-14 tag |
| kotlin | `fwcd/tree-sitter-kotlin/bindings/go` | **0.3.8** | 14 | 0.3.8 | **already latest** & ABI 14 |
| lua | `tree-sitter-grammars/tree-sitter-lua/bindings/go` (**source swap**) | **v0.3.0** | 14 | v0.5.0 | v0.4.0+ ABI 15; v0.3.0 ABI 14. Current src `Azganoth/…` has no go binding |
| ocaml | `tree-sitter/tree-sitter-ocaml/bindings/go` (`binding_ocaml.go`) | **v0.23.2** | 14 | v0.25.0 | latest ABI 15 → newest v0.23.x |
| php | `tree-sitter/tree-sitter-php/bindings/go` (`php.go`) | **v0.23.11** | 14 | v0.24.2 | v0.24.x ABI 15 → newest v0.23.x |
| proto | **vendored** `mitchellh/tree-sitter-proto` C + grafel binding | **master** | 13 | none (2021) | no Go binding anywhere → vendor; §3 |
| python | `tree-sitter/tree-sitter-python/bindings/go` | **v0.23.6** | 14 | v0.25.0 | latest ABI 15 → newest v0.23.x |
| ruby | `tree-sitter/tree-sitter-ruby/bindings/go` | **v0.23.1** | 14 | v0.23.1 | **already latest** |
| rust | `tree-sitter/tree-sitter-rust/bindings/go` | **v0.23.2** | 14 | v0.24.2 | v0.24.x ABI 15 → newest v0.23.x |
| scala | `tree-sitter/tree-sitter-scala/bindings/go` | **v0.23.4** | 14 | v0.26.0 | latest ABI 15 → newest v0.23.x |
| sql | `DerekStride/tree-sitter-sql/bindings/go` | **v0.3.8+** | ≤14 | v0.3.11 | go.mod requires official go-tree-sitter v0.23.1 ⇒ ABI ≤14 by construction; pick newest tag whose generated parser.c is ABI 14 (gate confirms) |
| swift | `alex-pinkus/tree-sitter-swift/bindings/go` | **0.7.3-with-generated-files** | 14 | 0.7.3 | parser.c only checked in on the `-with-generated-files` tags; that tag is ABI 14 (co-link-tested, §8) |
| toml | `tree-sitter-grammars/tree-sitter-toml/bindings/go` (**source swap**) | **v0.7.0** | 14 | v0.7.0 | current src `ikatyang/…` (2021) has no go binding; swap home is ABI 14 & latest |
| typescript (+tsx) | `tree-sitter/tree-sitter-typescript/bindings/go` (`typescript/`, `tsx/`) | **v0.23.2** | 14 | v0.23.2 | **already latest** (ships both `typescript` and `tsx`) |
| yaml | `tree-sitter-grammars/tree-sitter-yaml/bindings/go` (**source swap**) | **v0.7.0** | 14 | v0.7.2 | current src `ikatyang/…` (2021) no go binding; swap home is ABI 14 (pick v0.7.x tag that is ABI 14) |

### Matrix summary

- **26 grammar modules** ship (markdown dropped).
- **Already-latest, no pin-back:** 7 — cpp, html, java, kotlin, ruby, typescript, toml. (Their newest tag is already ABI ≤14.)
- **Pinned back for ABI (latest is ABI 15):** 14 — bash, c, csharp, css, elixir, go, hcl, javascript, lua, ocaml, php, python, rust, scala. Each pins to the **freshest v0.23.x / ABI-14 tag**.
- **Source-swaps (also a freshness win):** 4 — hcl → `tree-sitter-grammars/tree-sitter-hcl`, lua → `tree-sitter-grammars/tree-sitter-lua`, toml → `tree-sitter-grammars/tree-sitter-toml`, yaml → `tree-sitter-grammars/tree-sitter-yaml`. (hcl is new vs the ADR's 3 — the maintained `bindings/go` lives under the `tree-sitter-grammars` org, not `MichaHoffmann`.)
- **Vendored C + grafel binding:** 3 — proto (§3), dockerfile + groovy (§4).
- **community-org bindings on the official runtime (verified go.mod):** sql (`DerekStride`, requires go-tree-sitter v0.23.1), swift (`alex-pinkus`, v0.23.1), kotlin (`fwcd`), elixir (`elixir-lang`, v0.23.1) — all co-link cleanly because they depend on the **official** runtime, not smacker.

> **Renovate guard (A1):** every grammar bump must be routed
> `grammar-bump`+`needs-benchmark` and **gated on ABI ≤14 against runtime
> v0.24.0** + the smoke-parse (§7). A bump that crosses to ABI 15 is a runtime
> crash, not a compile error — the gate is mandatory, not advisory.

---

## 3. proto — the one hard gap (recommendation: **vendor**)

`mitchellh/tree-sitter-proto` has **no Go binding anywhere** and the proto
extractor **parses the CST** (unlike markdown), so it cannot simply drop.
Smacker is being removed entirely, so proto **cannot stay on smacker** (that
would re-introduce the 247-symbol co-link). Two options:

- **(A) Vendor — RECOMMENDED.** Vendor `tree-sitter-proto`'s `src/parser.c` (+
  `scanner.c` if present) into `internal/treesitter/ts/grammars/proto/` and
  hand-write the ~10-line official-style binding
  (`func Language() unsafe.Pointer { return unsafe.Pointer(C.tree_sitter_proto()) }`)
  compiled against the **official** runtime. **Verified feasible:** the grammar's
  `parser.c` is **ABI 13** — squarely inside the v0.24.0 runtime's 13–14 window,
  so it loads and parses without regen. The grammar is frozen (last commit
  2021), so a vendored snapshot needs no churn; Renovate simply has nothing to
  bump. Cost: one ~10-line binding + a vendored C file + a license note (proto
  grammar is MIT).
- **(B) Fallback — heuristic extractor.** Drop proto's CST and replace the
  extractor with a line/brace heuristic (like markdown's stdlib extractor).
  **Tradeoff:** loses nested message / rpc / field-level fidelity that the CST
  gives today — a real fidelity regression for `.proto` files. Only take this if
  vendoring proves problematic in the release-matrix CGO builds.

**Recommendation: (A) vendor.** It is cheap, ABI-safe (13), keeps CST fidelity,
and is the *only* option that lets smacker be fully removed without a fidelity
hit.

---

## 4. caveats — dockerfile & groovy (binding `require`s smacker)

The ADR flagged both as "go.mod still `require`s smacker." **Refined finding
(verified):** each repo's `bindings/go/binding.go` is in fact **official-style**
— it is a pure `import "C"` + `unsafe` file exposing
`func Language() unsafe.Pointer`, with **no smacker import in the binding file
itself**. The smacker coupling is only at the *module* level (an old `go.mod`
require / transitive runtime expectation). So the clean path is identical to
proto: **vendor the C grammar + the (already official-style) binding into
`internal/treesitter/ts/grammars/<lang>/`, compiled against the official
runtime**, bypassing the module's go.mod entirely.

- **dockerfile** — `camdencheek/tree-sitter-dockerfile` @ v0.2.0: grammar
  `parser.c` is **ABI 14** (compatible as-is). **Recommendation: vendor C +
  the official-style binding.** No regen needed. Lowest-risk caveat.
- **groovy** — `murtaza64/tree-sitter-groovy` @ HEAD: grammar `parser.c` is
  **ABI 15** (would SIGSEGV against runtime v0.24.0). So vendoring the current C
  is *not* enough — it must be **regenerated to ABI 14** (run the tree-sitter
  v0.23-line CLI over `grammar.js`, or vendor an earlier ABI-14 generation).
  **Recommendation: vendor + regenerate parser.c to ABI 14** (one-time, then
  frozen), with the official-style binding. If regen proves costly, groovy is
  the one acceptable **heuristic-fallback** candidate (groovy extraction is only
  2 files and lower-value than proto).

Neither needs to keep smacker alive — both fold into the same vendored-grammar
pattern as proto.

---

## 5. source-swaps — lua, toml, yaml (+ hcl)

Move `grammars.lock`'s `source` to the maintained successor under the
`tree-sitter-grammars` org, which ships a `bindings/go` depending on the
**official** runtime (verified each go.mod `require`s
`github.com/tree-sitter/go-tree-sitter`):

- **lua** `Azganoth/…` (no go binding) → `tree-sitter-grammars/tree-sitter-lua`,
  pin **v0.3.0** (ABI 14; v0.4.0+ are ABI 15).
- **toml** `ikatyang/…` (2021, no go binding) →
  `tree-sitter-grammars/tree-sitter-toml` **v0.7.0** (ABI 14, latest).
- **yaml** `ikatyang/…` (2021, no go binding) →
  `tree-sitter-grammars/tree-sitter-yaml` (pick the **v0.7.x tag that is ABI 14**;
  the gate confirms).
- **hcl** (new vs ADR) → `tree-sitter-grammars/tree-sitter-hcl` has `bindings/go`
  on the official runtime, but **v1.2.0 is ABI 15** → pin **v1.1.x (ABI 14)**.

All four are a freshness win (off dead/binding-less repos) *and* satisfy the
one-runtime requirement.

---

## 6. markdown — drop the grammar

The markdown **extractor is pure-stdlib** (no tree-sitter); the loaded grammar
is functionally unused, and `MDeiml/tree-sitter-markdown` has no Go binding.
**Action: remove markdown from `parser.go`'s `languageRegistry`** and from
`grammars.lock`'s grammar list. **Functional impact: none.** This is the only
language that leaves the grammar set; the registry drops 28 → 27 grammar-backed,
26 grammar *modules* (proto/dockerfile/groovy vendored, not modules).

---

## 7. Execution shape — one big-bang PR + the validation gate

Because smacker and official cannot co-link, the cutover is **a single PR** (not
the per-language batches of the superseded incremental plan):

1. Add 23 grammar provider packages under `internal/treesitter/ts/grammars/`
   (siblings of the Phase-0 `golang/`), one per clean/swap/community language,
   each pinning the §2 version; vendor proto + dockerfile + groovy grammars.
2. Replace `adapters_default.go` so **every** language resolves to the official
   adapter; delete `adapters_official.go`'s build-tag split (the `ts_official`
   tag goes away — official becomes the *only* build). Drop markdown from the
   registry.
3. **Remove smacker:** delete `internal/treesitter/ts/smacker/`, drop
   `github.com/smacker/go-tree-sitter` from `go.mod`/`go.sum`, retire
   `ParseResult.Tree` (concrete smacker tree) — only `ParseResult.TSTree`
   (façade) remains.
4. Migrate the remaining ~178 extractor files off `*sitter.Node` → `ts.Node`
   (mechanical, compiler-gated; the Go extractor is the worked example).

### Validation gate (mandatory before merge/tag — ALL must pass)

1. **ABI smoke-parse EVERY grammar.** A `grammars/<lang>/_smoke_test.go` per
   language (the Phase-0 `golang_smoke_test.go` is the template) + the
   `abiGuard` at startup: parse trivial valid source, assert a non-nil,
   non-error root of the expected top kind. This catches the ABI-15-class
   SIGSEGV **at CI**, not in prod, for all 26.
2. **B1 fidelity/coverage re-bench across all 27 languages.** Re-run each
   extractor benchmark; **require no regression** vs the smacker baseline
   (entity counts, edge counts, `error_ratio`). This is the dominant cost and
   the real gate — not a spot check. Where a grammar version changed (most),
   diffs are expected to be *additive* (newer-than-bundled grammar) — review,
   don't auto-fail, but never regress.
3. **#481 determinism re-test.** Confirm whether the official binding still
   needs the `parseMu` global parse serialization (the smacker shared-grammar
   race). If the official runtime is race-free, dropping `parseMu` is a perf
   win; if not, retain it. Re-run the #481 / byte-determinism suite either way.
4. **3-OS release-matrix CGO build.** Run `release.yml`'s full matrix —
   linux amd64/arm64, darwin amd64/arm64, windows amd64 — with
   `CGO_ENABLED=1` (MinGW on Windows), `osusergo` tag intact. Per-module changes
   the number of C compile units, not the toolchain; **must** pass on all five
   legs before tag. (The dashboard-embed pre-step is unaffected.)
5. **License audit of the ~26 new modules.** Run `grafel_license_audit` over the
   new module set (orgs: `tree-sitter`, `tree-sitter-grammars`, `fwcd`,
   `alex-pinkus`, `DerekStride`, `elixir-lang`, plus the vendored
   `mitchellh`/`camdencheek`/`murtaza64` grammars). Pin-by-digest in `go.sum`;
   record licenses (expect MIT/Apache-2.0 throughout). Wider trust surface than
   one smacker dep — this is a ship gate.
6. **A4 canary refresh.** After promotion, refresh
   `docs/grammar-canary-baseline.json` (grammar versions changed ⇒ expected node
   shape shifts); compare `ErrorRatio` to the refreshed baseline.

### C3 (b)/(c) coupling (maintainer rule: not tagged until new features extracted)

Per the epic rule, the cutover is **not tagged** until the C3 new-feature work
(#5417 analysis, #5436 extraction) is folded in **per language**, because the
freshest ABI-14 grammar each language lands on now *parses* features the bundled
2024-08 smacker snapshot could not. As each language flips to its §2 version:

- **(b) needs-new-extraction** — wire the extractor for features the new grammar
  now exposes (the #5417 (b) worklist: C# 14 extension members, Swift actors,
  Kotlin context parameters, Python t-strings, JS/TS `await using`, plus the
  `grammars.lock` `backfill_c3` notes: C#12/13 primary ctors + collection
  expressions, Java sealed types / record patterns, Python PEP 695 type params,
  TS const type params).
- **(c) changes-existing-extraction** — adapt extractors whose node shapes the
  newer grammar changed.

So the merge order is: **cutover (binding swap) → per-language C3 (b)/(c) →
re-bench → tag**. The cutover PR may land first behind the gate, but **0.1.4 is
not tagged** until (b)/(c) are extracted and re-benched for the languages whose
grammar moved.

---

## 8. Bounded co-link verification (executed for this plan)

To prove the one-runtime model removes the 247-symbol blocker, a throwaway
module linked **three grammars on a single official runtime, no smacker**
(`CGO_ENABLED=1`, load < 4):

- runtime `tree-sitter/go-tree-sitter@v0.24.0`
- `tree-sitter-go@v0.23.4` (ABI 14) + `tree-sitter-python@v0.23.6` (ABI 14) +
  `alex-pinkus/tree-sitter-swift@0.7.3-with-generated-files` (ABI 14)

```
go build .   →  exit 0          (NO duplicate-symbol error — smacker absent)
./bin        →  go: root=source_file children=2
                python: root=module children=1
                swift: root=source_file children=1     (no SIGSEGV)
```

This is the headline proof: **dropping smacker dissolves the co-link conflict**,
and **multiple ABI-14 grammars co-resident on the official v0.24.0 runtime build
and parse correctly**. (The §2 ABI column was likewise verified by fetching each
chosen tag's `src/parser.c` and reading its `#define LANGUAGE_VERSION`.)

---

## 9. Rollback

The cutover is all-or-nothing (smacker is removed), so rollback is **revert the
PR** — there is no per-language flip once smacker is gone. Mitigation: keep the
PR behind the gate (§7); only delete `ts/smacker` in the *final* commit once all
26 smoke-parses + the full re-bench + the 5-leg release matrix are green, so a
pre-merge failure reverts cheaply.
