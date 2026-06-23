# 0023 — Migrate off the single `smacker/go-tree-sitter` binding to the official `tree-sitter/go-tree-sitter` + per-language grammar modules

- **Status:** Proposed (assessment — B2, #5418, epic #5359, milestone 0.1.4)
- **Date:** 2026-06-23
- **Deciders:** coordinator / maintainer (this ADR is the go/no-go input)
- **Supersedes / relates to:** B3 audit (`docs/grammar-freshness-audit.md`), `grammars.lock`,
  A1 Renovate (#5410), A2 freshness cron (#5411), A4 canary (#5414)

> This is an **assessment + phased plan**, not the migration. It determines
> feasibility, effort, risk, and a phased rollout for the coordinator to decide.

---

## Context

grafel parses 28 grammar-backed languages through **one** pinned dependency,
`github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82`
(2024-08-27), wired in `internal/treesitter/parser.go`. The B3 audit established
the decisive fact: **that pinned commit IS smacker's upstream HEAD and the binding
is unmaintained** (`compare dd81d9e9be82...master` ⇒ `ahead_by: 0`). There is no
newer smacker commit to ride, so:

- **B1 (bump smacker) is a no-op** — already at upstream HEAD.
- **A1 (Renovate on the dep) finds nothing** — already at HEAD.
- The **only** path back to fresh grammars + automated freshness is migrating to
  the **official `github.com/tree-sitter/go-tree-sitter`** (alive: v0.24.0,
  2025-11-12), where **each grammar is its own maintained Go module**
  (`github.com/tree-sitter/tree-sitter-<lang>/bindings/go`) that Renovate can bump
  independently.

This ADR assesses that migration (B2).

---

## 1. Current usage surface (what must be ported)

The dependency is **not** localized to `parser.go`. It is woven through the whole
extractor layer:

| Measure | Count |
|---|---|
| `.go` files importing `smacker/go-tree-sitter` (any path) | **245** |
| Files importing the smacker **root** package `sitter "…/go-tree-sitter"` (the `*sitter.Node` consumers) | **219** |
| Files importing a grammar **subpackage** (`…/golang`, `…/python`, …) | 69 |
| `sitter.Node` references (manual CST traversal) | **1758** |
| `GetLanguage()` grammar-acquisition call sites | 102 |

grafel uses a **narrow, deep** slice of the API: it constructs a parser, sets a
language, parses bytes, and then does **manual depth-first `Node` traversal**.
It does **not** use tree-sitter's native query engine (`Query` / `QueryCursor`)
anywhere — verified zero usages. The Node/Parser/Tree methods actually called:

| Method (smacker) | Call sites | Notes |
|---|---|---|
| `Node.Type()` | **1628** | renamed in official → `Kind()` |
| `Node.String()` | 1643 | debug; present in both |
| `Node.ChildByFieldName()` | 1168 | same name |
| `Node.Child(int)` | 938 | official takes `uint`, returns `*Node` |
| `Node.ChildCount()` | 907 | official returns `uint` (was `uint32`) |
| `Node.StartByte() / EndByte()` | 284 / 275 | official returns `uint` |
| `Node.StartPoint() / EndPoint()` | 224 / 131 | renamed → `StartPosition()` / `EndPosition()`; type `Point` |
| `Node.NamedChild() / NamedChildCount()` | 186 / 172 | same names |
| `Node.RootNode()` (Tree) | 42 | same |
| `Node.Parent()` | 34 | same |
| `Node.IsNamed() / IsError() / IsNull()` | 31 / 1 / 1 | `IsNull` not in official (use `nil` check) |
| `Node.FieldNameForChild()` | 1 | official takes `uint32`, same |
| `Node.Content(src)` | 1 | renamed → `Utf8Text(src []byte)` |
| `sitter.NewParser()` / `SetLanguage()` / `ParseCtx()` | ~75 files | official: `NewParser()`, `SetLanguage(*Language)`, `Parse([]byte, *Tree) *Tree` |

The factory (`internal/treesitter/parser.go`) owns: the `languageRegistry`
(`map[string]*sitter.Language` populated from 28 `GetLanguage()` imports), the
`ParserFactory`, the 10 % `ErrorRatio` gate, `countNodes`, and the `parseMu`
serialization workaround for issue #481 (shared-grammar-state race in smacker —
worth re-testing whether the official binding still needs it).

---

## 2. Official binding API delta

Researched against `tree-sitter/go-tree-sitter` **v0.24.0** (the live runtime) and
several per-language modules; confirmed with a working PoC (§6).

```go
import (
    ts "github.com/tree-sitter/go-tree-sitter"
    tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
)
p := ts.NewParser(); defer p.Close()
p.SetLanguage(ts.NewLanguage(tsgo.Language()))   // was: p.SetLanguage(golang.GetLanguage())
tree := p.Parse(src, nil); defer tree.Close()     // was: p.ParseCtx(ctx, nil, src) -> (*Tree, error)
root := tree.RootNode()
```

**Breaking differences grafel must absorb:**

1. **Language acquisition.** smacker `pkg.GetLanguage() *sitter.Language` →
   official `pkg.Language() unsafe.Pointer` wrapped by
   `ts.NewLanguage(ptr) *ts.Language`. (102 acquisition sites, but concentrated;
   most extractors funnel through small `language.go` helpers like
   `internal/extractors/golang/language.go`.)
2. **`Node.Type()` → `Node.Kind()`.** 1628 call sites. Pure rename.
3. **`Node.StartPoint()/EndPoint()` → `StartPosition()/EndPosition()`.** 355 sites.
4. **`Node.Content(src)` → `Node.Utf8Text(src)`.** 1 site.
5. **Unsigned ints.** `Child(uint)`, `ChildCount() uint`, `StartByte() uint`
   (smacker used `int`/`uint32`). Affects index-loop typing (`for i := 0; i <
   int(n.ChildCount()); i++` patterns) — compiler-caught, mechanical.
6. **`Parse` signature.** Official `Parse([]byte, *Tree) *Tree` (no `ctx`, no
   `error`) vs smacker `ParseCtx(ctx, *Tree, []byte) (*Tree, error)`. Localized to
   the factory.
7. **Resource model.** Official `Parser`/`Tree`/`QueryCursor` expose `Close()`
   tied to a finalizer; grafel should `defer tree.Close()` to avoid relying on GC
   for C memory. Localized to the factory.
8. **`IsNull()`** has no official equivalent — replace with a `nil` check (1 site).

**Verdict:** no semantic gaps. Every smacker call has a 1:1 official equivalent.
The delta is **mechanical and compiler-enforced** (renames + int widening), which
is exactly the kind of change an abstraction shim (§5) can absorb without touching
all 219 files.

---

## 3. Per-language module inventory (the feasibility crux)

For each of grafel's 28 grammar-backed languages: does a maintained Go module with
an **official-style** binding (`Language() unsafe.Pointer`, depending on
`tree-sitter/go-tree-sitter`) exist?

| Lang | Source module path (Go binding) | Official-style Go binding? | Notes |
|---|---|---|---|
| bash | `tree-sitter/tree-sitter-bash/bindings/go` | ✅ | tree-sitter org |
| c | `tree-sitter/tree-sitter-c/bindings/go` | ✅ | |
| cpp | `tree-sitter/tree-sitter-cpp/bindings/go` | ✅ | |
| css | `tree-sitter/tree-sitter-css/bindings/go` | ✅ | |
| csharp | `tree-sitter/tree-sitter-c-sharp/bindings/go` | ✅ | high-value |
| dockerfile | `camdencheek/tree-sitter-dockerfile/bindings/go` | ⚠️ | binding present but go.mod still `require smacker` — needs a newer tag or a fork that targets the official runtime |
| elixir | `elixir-lang/tree-sitter-elixir/bindings/go` | ✅ | |
| go | `tree-sitter/tree-sitter-go/bindings/go` | ✅ | high-value; PoC-verified |
| groovy | `murtaza64/tree-sitter-groovy/bindings/go` | ⚠️ | binding present but go.mod still `require smacker` — same caveat as dockerfile |
| hcl | `MichaHoffmann/tree-sitter-hcl/bindings/go` | ✅ | official-style binding |
| html | `tree-sitter/tree-sitter-html/bindings/go` | ✅ | |
| java | `tree-sitter/tree-sitter-java/bindings/go` | ✅ | high-value |
| javascript | `tree-sitter/tree-sitter-javascript/bindings/go` | ✅ | |
| kotlin | `fwcd/tree-sitter-kotlin/bindings/go` | ✅ | community, active (v0.3.x) |
| lua | `tree-sitter-grammars/tree-sitter-lua/bindings/go` | ✅ (**source swap**) | current source `Azganoth/...` has NO go binding; `tree-sitter-grammars/...` (v0.5.0, 2026-06) does |
| markdown | — | ❌ **GAP** | `MDeiml/tree-sitter-markdown` has releases but **no Go binding**; no official-style alt found. *But the markdown EXTRACTOR is pure-stdlib (audit §2) — the grammar is loaded yet functionally unused.* Functional impact ≈ none. |
| ocaml | `tree-sitter/tree-sitter-ocaml/bindings/go` | ✅ | |
| php | `tree-sitter/tree-sitter-php/bindings/go` | ✅ | |
| proto | — | ❌ **GAP** | `mitchellh/tree-sitter-proto` has no Go binding; no official-style alt found. **Real functional gap** (proto extractor parses CST). |
| python | `tree-sitter/tree-sitter-python/bindings/go` | ✅ | high-value |
| ruby | `tree-sitter/tree-sitter-ruby/bindings/go` | ✅ | |
| rust | `tree-sitter/tree-sitter-rust/bindings/go` | ✅ | high-value |
| scala | `tree-sitter/tree-sitter-scala/bindings/go` | ✅ | |
| sql | `DerekStride/tree-sitter-sql/bindings/go` | ✅ | community, active |
| swift | `alex-pinkus/tree-sitter-swift/bindings/go` | ✅ | community, active |
| toml | `tree-sitter-grammars/tree-sitter-toml/bindings/go` | ✅ (**source swap**) | current source `ikatyang/...` unmaintained (2021, no go binding); `tree-sitter-grammars/...` (v0.7.0) is the maintained successor |
| typescript (+tsx) | `tree-sitter/tree-sitter-typescript/bindings/go` | ✅ | high-value; ships both `typescript` and `tsx` |
| yaml | `tree-sitter-grammars/tree-sitter-yaml/bindings/go` | ✅ (**source swap**) | current source `ikatyang/...` (2021) → `tree-sitter-grammars/...` (v0.7.2) maintained successor |

**Summary:**
- **21 / 28 — clean** (official-style Go binding at the current source).
- **3 / 28 — source swap** (lua, toml, yaml): a maintained binding exists under a
  *different* repo (`tree-sitter-grammars/*`). Free freshness win; update
  `grammars.lock`'s `source`.
- **2 / 28 — caveat** (dockerfile, groovy): a Go binding exists but its module
  still `require`s smacker as its runtime. Options: (a) wait for / request an
  upstream tag targeting the official runtime, (b) maintain a thin grafel fork of
  just the binding file (the C grammar is unchanged; only the 10-line `binding.go`
  runtime import differs), or (c) keep these two on smacker behind the abstraction
  during transition.
- **2 / 28 — true gap** (markdown, proto): **no** official-style Go binding
  anywhere. **markdown's functional impact ≈ zero** (extractor is pure-stdlib;
  the loaded grammar is unused), so it can simply drop from the registry. **proto
  is the one real gap** — keep on smacker behind the abstraction, or vendor the C
  source + write a 10-line binding.

So the **hard blocker count is 1 language (proto)** + 2 "needs a fork-or-wait"
(dockerfile, groovy). Everything else is available today.

---

## 4. Build / CGO implications

**Both** bindings are CGO (`import "C"`). CGO is non-negotiable for tree-sitter in
Go regardless of this decision — so migrating does **not** introduce CGO, it
**redistributes** it.

| Dimension | smacker (today) | Official per-module |
|---|---|---|
| Dependency shape | 1 module bundling 28 grammars' C sources | 1 runtime module + ~26 grammar modules, each shipping its own C source |
| `go.mod` lines | 1 | ~27 (Renovate-managed; this is the *point* — independent bumps) |
| CGO compile units | 28 grammars in one package tree | 28 grammars across modules; **same total C compiled** |
| Cross-compile model | release uses **native runners per OS/arch** (`release.yml` matrix: linux amd64/arm64, darwin amd64/arm64, windows amd64; `CGO_ENABLED=1`, MinGW on Windows) — **not** zig/cross | **unchanged** — per-module CGO compiles identically on native runners |
| `osusergo` tag | applied to every build (#5222 launchd fix) | **unaffected** — orthogonal to grammar bindings |
| Binary size | ~all 28 grammars linked | ~same (same grammars); dead-grammar elision possible per-module |
| Build time | one big package | parallelizable per-module; first build downloads more modules (cache amortizes) |

**Key build risks:**
1. **ABI pinning (confirmed empirically, §6).** Each grammar module embeds a
   tree-sitter `LANGUAGE_VERSION`; the runtime supports a **range**. Pairing
   runtime `v0.24.0` with grammar `tree-sitter-go v0.25.0` **built fine but
   SIGSEGV'd at `RootNode()`** — an ABI mismatch. Pinning the grammar to `v0.23.4`
   fixed it. **Consequence:** Renovate "bump each grammar independently" needs a
   guard — a grammar bump that outruns the runtime ABI is a runtime crash, not a
   compile error. The B1 benchmark gate (re-bench every bumped grammar) **must**
   include a smoke-parse, and Renovate's `grammar-bump`+`needs-benchmark` routing
   (already wired in A1) is the right control. Consider pinning the runtime and
   only accepting grammar versions within its ABI window.
2. **Windows CI.** Already proven with smacker (MinGW + `CGO_ENABLED=1`,
   `windows.yml` / `windows-cgo-experiment.yml`). Per-module changes the number of
   C compile units, not the toolchain. Low risk, but **must re-run the full
   release matrix** before shipping.
3. **`go.sum` / supply chain.** ~26 new modules from several orgs
   (`tree-sitter`, `tree-sitter-grammars`, `fwcd`, `alex-pinkus`, `DerekStride`,
   `camdencheek`, `MichaHoffmann`, `murtaza64`, `elixir-lang`). Wider trust
   surface than one smacker dep — vendor or pin-by-digest + a license audit
   (`grafel_license_audit`) before merge.

---

## 5. Effort, risk, and phased plan

### Effort

| Phase | Work | Rough size |
|---|---|---|
| Abstraction layer | Introduce a grafel-owned façade so the rename/int-widening delta lives in ONE place | **M** |
| Per-grammar wiring | 21 clean + 3 source-swap modules behind a build-tag/registry | **M** |
| Caveat/gap handling | proto + dockerfile + groovy (fork-binding or keep-on-smacker) | **S–M** |
| Call-site migration | `.Type()→.Kind()`, points, int widening across 219 files — mostly `gofmt`-able codemod | **M** (mechanical, compiler-gated) |
| Re-benchmark | every migrated language through the B1 fidelity/coverage benchmark | **L** (the real cost — must be thorough, not spot-check) |
| Release matrix | re-run linux/darwin/windows × arch with `CGO_ENABLED=1` | **S** |

**Total: a multi-PR effort dominated by re-benchmarking, not by code.** The code
change is large-surface but low-depth (mechanical, compiler-enforced).

### The abstraction (de-risks everything)

Introduce a thin grafel-owned node/parser façade (e.g. extend
`internal/treesitter` with a `tsnode` type and a `Grammar` interface) that wraps
*either* a smacker or an official `*Node`. Extractors traverse the façade, not the
vendor type. This:
- collapses 1758 `sitter.Node` references to one wrapped type;
- lets grammars move **one language at a time** (smacker fallback for not-yet-migrated langs);
- isolates the `Type()→Kind()` / point / int-width delta to the façade;
- makes rollback trivial (flip a grammar back to the smacker provider).

(Trade-off: a façade adds an indirection per node access on a hot path. Given the
1758 sites and the existing `parseMu` global serialization, validate that the
wrapper is allocation-free / inlined, or generate per-method shims, before
committing the whole codebase to it.)

### Phasing

1. **Phase 0 — abstraction + PoC landed.** Add the façade + the official runtime
   as a *parallel* provider behind a build tag/flag. Migrate **one** high-value,
   official-supported language (go or python) end-to-end. Gate on its benchmark
   matching smacker. *(The PoC in §6 already proves the build + API; this phase
   productizes it.)*
2. **Phase 1 — high-value batch.** go, python, java, typescript/tsx, csharp, rust
   — all `tree-sitter` org, all official-style. Re-bench each; keep smacker as
   fallback.
3. **Phase 2 — remaining clean + source-swaps.** The other 15 clean langs + lua,
   toml, yaml (swap `grammars.lock` source). Re-bench each.
4. **Phase 3 — caveats + gap.** dockerfile, groovy (fork-binding-or-wait);
   **proto stays on smacker** behind the abstraction (or vendor); **markdown
   drops** from the registry (extractor is stdlib). At this point smacker remains
   *only* for proto until a binding appears.
5. **Phase 4 — cut over + remove smacker** once proto is resolved (binding
   appears, is vendored, or proto demoted to heuristic).

### Validation gate (mandatory, per migrated language)

- The **B1 fidelity/coverage benchmark** must show **no regression** vs the
  smacker baseline for each language *before* its grammar is promoted off the
  fallback.
- A **smoke-parse + `ErrorRatio` check** against the A4 canary baseline
  (`docs/grammar-canary-baseline.json`) per grammar — catches ABI mismatch (§6)
  that compiles but crashes/garbles at runtime. Refresh the canary baseline after
  each promotion (grammar version changed ⇒ expected node shape may shift).
- Re-test the **#481 race**: confirm whether the official binding still needs the
  `parseMu` global serialization (a freebie if it doesn't).

### Rollback

Per-language: the abstraction's `Grammar` provider flips back to smacker for that
language (config/build-tag), no code revert. Whole-migration: smacker stays in
`go.mod` until Phase 4, so revert = stop promoting + pin back.

---

## 6. PoC (executed, load-permitting)

Built a throwaway module (`CGO_ENABLED=1`, ONE grammar, load ≈ 5.8 < 10):

```go
p := ts.NewParser()
p.SetLanguage(ts.NewLanguage(tsgo.Language()))
tree := p.Parse([]byte("package p\nfunc F() int { return 1 }\n"), nil)
root := tree.RootNode()
// root.Kind()="source_file"  ChildCount()=2
// child0.Kind()="package_clause"  StartByte/EndByte=0/9  IsError=false IsNamed=true
```

- `go get github.com/tree-sitter/go-tree-sitter@v0.24.0` + the tree-sitter-go
  binding, then `go build` (CGO) of one package: **exit 0** — the official runtime
  + a grammar compile cleanly on this toolchain.
- **ABI finding:** the binding's default `@latest` resolved grammar **v0.25.0**,
  which **SIGSEGV'd at `RootNode()`** against runtime v0.24.0 (ABI mismatch).
  Pinning the grammar to **v0.23.4** produced correct output. This is the headline
  build risk (§4) — captured here so the plan's gate enforces ABI-compatible
  pairing.

---

## Decision (recommendation)

**GO — migrate, phased, behind the abstraction. Do NOT attempt the full
migration inside 0.1.4.**

- **Feasible:** 21/28 languages have official-style Go bindings today; 3 more via a
  trivial source swap (a *freshness win*); only **proto** is a hard blocker and it
  has a clean fallback (keep on smacker / vendor). markdown is a non-issue (stdlib
  extractor). No semantic API gaps — the delta is mechanical and compiler-enforced.
- **Worth it:** it is the *only* route back to fresh grammars + Renovate-driven
  per-grammar freshness (A1 becomes live), and removes the single-point-of-failure
  on a dead dependency.
- **But invasive:** 219 files / 1758 traversal sites + a full re-benchmark + a
  release-matrix re-run, plus a real ABI-pinning hazard (§6). The cost is the
  benchmark, not the code.

**Recommendation on milestone:** land **Phase 0 (abstraction + 1 language +
benchmark gate)** as the 0.1.4 deliverable of B2; **slip Phases 1–4 past 0.1.4**
as a phased program gated by the B1 benchmark, exactly as the epic's risk note
anticipated. First concrete step: build the abstraction layer and productize the
§6 PoC behind a build tag.

---

## Consequences

- **Positive:** automated, independent grammar freshness (Renovate A1 goes live);
  off a dead dependency; clean fallback story; source-swaps fix 3 stale grammars
  for free.
- **Negative / risk:** large mechanical diff; wider supply-chain surface (~26
  modules across orgs → license audit + pin); ABI-pin discipline required;
  benchmark cost is the gating expense; façade hot-path overhead must be measured.
- **Out of scope:** heuristic-only languages (avro, cobol, bicep, zig, …) — no
  grammar dep, unaffected. New-feature *modeling* (epic Part C) is separate from
  this *binding* migration.
