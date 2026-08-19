# C3 — New-feature impact analysis (per-language extractor worklist)

_Epic #5359 (milestone 0.1.4), issue [#5417](https://github.com/cajasmota/grafel/issues/5417).
Triaged 2026-06-23 spec-first per the [C1 process](./new-language-feature-triage.md),
grounded against [`grammars.lock`](../grammars.lock) (bundled snapshot
**smacker @ 2024-08-27**) and the actual extractors in `internal/extractors/`._

This is **analysis only** — no code changes. It is the spec-driven worklist that
the **B1 grammar-bump / B2 smacker-decouple cutover unblocks**: every (b) and (c)
item below carries a **grammar-bump prerequisite** because the 22-month-stale
bundled snapshot may not even produce the new node kind yet (tree-sitter is
error-tolerant, so it emits `ERROR` nodes rather than failing — see B3 audit §1).
The triage is spec-driven, not grammar-driven, exactly as C1 mandates.

## Method

For each grammar-backed language in `grammars.lock` (markdown skipped — its
extractor is pure-stdlib), the "since" window is **2024-08 → 2026-06**. I listed
the real language-version releases in that window from upstream release notes,
extracted the **graph-relevant** syntax/semantic features (new declarations, DI/
attributes, routing, async/concurrency, type-system, module/visibility, data
constructs), and classified each against grafel's **actual** extractor capability
(read from `internal/extractors/<lang>/`), per the C1 buckets:

- **(a) parse-only** — grammar represents it; grafel already models it or it
  carries no graph-relevant structure → **no extractor change**.
- **(b) needs-new-extraction** — a new construct grafel *should* model (new
  entity Kind / relationship / capability) → **new extractor logic + registry cell**.
- **(c) changes-existing-extraction** — existing syntax changed shape; an existing
  extractor's `switch node.Type()` / `ChildByFieldName(...)` must adapt.

Extractor-directory note: grafel's dirs are `golang/`, `javascript/` (TS shares
it), etc. TypeScript has **no** separate extractor — it rides
`internal/extractors/javascript/`.

---

## Per-language table

### High-value languages

| Language | Versions since ~2024-08 | Notable NEW graph-relevant syntax/features | Class. | Extractor work needed | Effort |
|---|---|---|---|---|---|
| **python** | 3.13 (2024-10), 3.14 (2025-10) | (1) **t-strings / template strings PEP 750** (`t"..."` → `Template`/`Interpolation` objects); (2) `except*`/comma-grouped `except` (PEP 758); (3) deferred annotations PEP 649/749; (4) free-threading (runtime, no syntax) | mostly **a**, **(b)** for t-strings | t-strings are a genuinely new literal kind that flows into `render_template`/raw-SQL sinks (`template_render.go`, `raw_sql_db_calls.go` already special-case `f-string` interpolation). A `template_string` node should be walked the same way → **(b)** new literal handling on the RENDERS / raw-SQL passes (no new Kind, a new node-type branch + capability cell). `except*`, deferred annotations, comma-except = **(a)** (exception_flow already walks `except_clause`; annotation strings unchanged in the graph). | **S** |
| **java** | 23 (2024-09), 24 (2025-03), 25 (2025-09) | (1) record patterns / pattern-matching in `switch` (final); (2) **flexible constructor bodies** (statements before `super()`); (3) **module import declarations** (`import module M`); (4) sealed types (final); (5) structured concurrency / scoped values (API) | mostly **a**, **(c)** for module-import | Records (`record_declaration`) + sealed already emitted (`java.go` walks them, subtype `record`). Record patterns in switch = `(a)` (no entity; tx/exception walks tolerate). **Module import declarations** `import module java.base;` are a **new import node shape** the `imports.go` IMPORTS pass must recognise or it silently drops the dep → **(c)**. Flexible constructor bodies = `(a)` (body is still a `constructor_body` grafel walks). | **S** |
| **javascript / typescript** | TS 5.6 (2024-09), 5.7 (2024-11), 5.8 (2025-03), 5.9; JS = ES2024/2025 (decorators stage-3, `using`/`await using`) | (1) **stage-3 decorators** (new `@dec accessor x` semantics); (2) **`using`/`await using`** explicit resource management; (3) **TS const type params** `<const T>`; (4) `--erasableSyntaxOnly`; (5) import attributes `with {…}` | mostly **a**, **(c)** decorators, **(b)** `using` | grafel leans heavily on decorators for DI/routing (`angular*.go`, `react*.go`, NestJS `#4503`). Stage-3 `accessor` fields + the new decorator-on-`accessor` node shape may rename/re-nest the `decorator` node → **(c)** adapt the decorator-walk so DI/route detection doesn't silently stop. **`await using`** is a new resource/disposal construct grafel doesn't model (effects/cleanup edges) → **(b)** *low value* (defer). const type params = `(a)` (type-param walk unaffected). | **M** (c) / S (b) |
| **csharp** | C# 13 / .NET 9 (2024-11), C# 14 / .NET 10 (2025-11) | (1) **extension members / extension blocks** (extension properties, operators, static); (2) `field` keyword (final in 14); (3) partial events & constructors; (4) `params` collections; (5) null-conditional assignment; (6) user-defined compound-assign operators | **a** + **b** + **c** | The **C# 13 worked example** in the C1 doc already fixed `field`=(b), `allows ref struct`=(c), partial properties=(c). **C# 14 extension blocks** are a **new member-container node** (`extension` block holding properties/methods bound to another type) — grafel emits members from class/struct/record/interface bodies (`csharp.go`, `field_members.go`) but not from an `extension` block; the extended-type relationship is graph-relevant → **(b)** new container walk + EXTENDS-like edge. **Partial constructors/events** extend the existing partial-member merge path → **(c)**. `params`/null-cond/compound-op = `(a)`. | **M** |
| **golang** | 1.23 (2024-08), 1.24 (2025-02), 1.25 (2025-08) | (1) **range-over-func iterators** (`for x := range fn`); (2) **generic type aliases** (`type Set[T] = …`); (3) `go.mod` `tool` directive; (4) weak pointers / `os.Root` (API) | mostly **a**, **(c)** generic type alias | range-over-func is a new `for`-range RHS shape but produces CALLS to the iterator fn grafel already walks → `(a)`. **Generic type aliases** change the `type_alias`/`type_spec` node to carry a `type_parameter_list`; `type_table.go`/`type_table_test.go` resolve type aliases for receiver/field typing — the alias walk must tolerate the param list or mis-resolve the alias target → **(c)**. `tool` directive in `gomod.go` (already parses `require`/`replace`, `#4705`) → **(c)** *small* (new go.mod line kind). | **S** |
| **rust** | 2024 edition / 1.85 (2025-02) + 1.8x | (1) **async closures** (`async \|\| {}`, `AsyncFn*` traits); (2) **`gen` blocks** (generators, reserved); (3) `impl Trait` lifetime-capture changes; (4) `unsafe extern`, `unsafe` attributes; (5) RPITIT / precise capturing | **a** mostly | `rust.go` walks `struct_item`/`enum_item`/`trait_item`/`impl_item`/`function_item`/`use_declaration`/`call_expression`. Async closures + gen blocks are expression-level — they produce calls/closures grafel already traverses, no new entity a consumer queries → `(a)`. `unsafe extern` could rename the `extern` block node → `(c)` *only if* grafel modeled FFI blocks (it doesn't) → `(a)`. **No (b).** | — |
| **kotlin** | 2.0 (2024-05, just before window), 2.1 (2024-11), 2.2 (2025-06) | (1) **context parameters** (`context(x) fun …`, replaces context receivers); (2) guard conditions in `when` (stable); (3) non-local break/continue; (4) multi-dollar interpolation; (5) context-sensitive resolution (preview) | mostly **a**, **(b)** context params | `kotlin.go` walks `class_declaration`/`object_declaration`/`function_declaration`/`property_declaration`/`type_alias`. **Context parameters** are a new function/property **declaration clause** introducing implicit dependencies — conceptually a DI/dependency edge (analogous to constructor injection grafel models elsewhere). The new `context(...)` node maps to a REFERENCES/INJECTED-style edge grafel doesn't emit → **(b)** *medium value* (DI surfacing). Guard conditions/interpolation/non-local jumps = `(a)`. | **M** |
| **swift** | 6.0 (2024-09), 6.1 (2025-03), 6.2 (2025-09) | (1) **typed throws** (`throws(MyError)`); (2) **macros** (`@freestanding`/`@attached`, `#macro`); (3) `@MainActor`-by-default / `@concurrent`; (4) `actor`/`distributed actor` (6.0 strict); (5) noncopyable `~Copyable` | **a** + **b** | `swift.go` walks `class_declaration` (incl. struct/enum via `value`), `protocol_declaration`, `function_declaration` — but **no `actor`, no macro, no typed-throws**. **`actor` / `distributed actor` declarations** are a new top-level concurrency component grafel should emit as a `SCOPE.Component` (subtype `actor`) like class/struct → **(b)**. **Macros** (`macro` decls + `@attached` expansion) are a new code-generating construct, but their expansion is invisible to the CST → triage as `(a)` for now (can't model expansion without semantic info). Typed throws = `(c)` *iff* exception_flow walks `throws` (swift has no exception_flow.go) → effectively `(a)`. | **S** ((b) actor) |
| **php** | 8.4 (2024-11) | (1) **property hooks** (`get`/`set` bodies on properties); (2) **asymmetric visibility** (`public private(set)`); (3) `new` without parens in chains; (4) lazy objects (API); (5) `#[\Deprecated]` attribute | mostly **a**, **(c)** hooks | `php.go` + `field_members.go` emit fields from `property_declaration` + `property_promotion_parameter`. **Property hooks** add a `property_hook`/hook-body child to `property_declaration`; the property is still emitted (membership intact = `(a)`), but the hook bodies contain **calls grafel will miss** unless the walk descends into them → **(c)** adapt field/call walk to recurse hook bodies. **Asymmetric visibility** is a modifier on the same property node → `(a)` (membership unaffected). | **S** |
| **ruby** | 3.4 (2024-12) | (1) **`it` implicit block param**; (2) `Data.define` (3.2, already modeled); (3) frozen-string-literal default warning; (4) pattern matching (stable) | **a** | `ruby.go` already handles `Data.define`/`Struct.new` for field membership (`#4854`). `it` block param is sugar for `_1` → produces the same call nodes grafel walks → `(a)`. **No (b)/(c).** | — |

### Config / smaller languages (briefer — mostly parse-only)

| Language | Versions / grammar movement since 2024-08 | Notable new syntax | Class. | Work | Effort |
|---|---|---|---|---|---|
| **scala** | 3.5 (2024-08), 3.6, 3.7 | named tuples, `into`/`@experimental` capture-checking, better-fors | **a** | grafel's scala support is shallow; new features are type-system level, no new entity a consumer queries. | — |
| **c** | C23 (`constexpr`, `_BitInt`, `nullptr`, `#embed`) | typed enums, `constexpr` | **a** | C extractor models functions/structs; `constexpr`/`_BitInt` are type-level → no new entity. | — |
| **cpp** | C++23/26 grammar churn (`if consteval`, deducing `this`, modules) | **C++ modules** (`export module`) | **a**, watch **(b)** | C++ modules *could* be a (b) module-boundary entity, but grafel's cpp support is shallow and modules adoption is low → record as deferred (a). | — |
| **php (config)** | — | covered above | — | — | — |
| **css** | CSS Nesting, `@layer`, container queries, `:has()` | nesting, `@layer` | **a** | CSS extractor models selectors/rules; nesting/layers are structural sugar, no graph edge a consumer queries. | — |
| **html** | grammar v0.23 | custom elements (no syntax change) | **a** | — | — |
| **sql** | grammar v0.3.11 | dialect additions (`MERGE`, JSON ops) | **a** | SQL raw-call extraction is sink-based; new clauses don't add a new entity. | — |
| **hcl / terraform** | grammar v1.2 | `for_each`/dynamic blocks (existing), provider-defined functions | **a** | HCL resource/edge extraction unchanged by new fn syntax. | — |
| **toml** | grammar frozen 2021 | none | **a** | No grammar movement; no work. | — |
| **yaml** | grammar frozen 2021 | none | **a** | No grammar movement; no work. | — |
| **dockerfile** | grammar v0.2 | heredocs (already common), `COPY --chmod` | **a** | Stage/instruction extraction unchanged. | — |
| **proto** | grammar frozen 2021 | proto editions (2023), **and proto2** | **c** (measured, #6357) | The "edition-agnostic" reasoning previously recorded here is wrong. The bundled grammar accepts **proto3 only**: `edition = "2023"` (error_ratio 0.2500), `syntax = "proto2"` (0.2222) and the proto2 keywords `optional` / `required` / `group` (0.21-0.27) each collapse the whole file body into one ERROR node, so the file never clears the 10% syntax-error gate and yields zero entities. The ratio does not dilute with file size. Measured over the local 14-file .proto population: 4 dropped whole — etcd `record.proto` + `snap.proto` (plain proto2), grpc-go `route_guide.proto` (edition 2023), and gin `protoexample/test.proto` (proto2; classifier-skipped as vendor_testdata in production, so 3 of the 10 classifier-eligible files are affected). Custom options (`option (gogoproto.marshaler_all) = true;`) are NOT the cause and parse at 0.0000. | grammar upgrade |
| **lua** | grammar frozen 2022 | none | **a** | — | — |
| **ocaml** | grammar v0.25 | OCaml 5.x effects, `let*` | **a** | grafel ocaml support shallow; effects are runtime. | — |
| **elixir** | grammar v0.3.5 | `defguard`, set-theoretic types (preview) | **a** | Module/function extraction unchanged; types are preview/runtime. | — |
| **groovy** | grammar initial | n/a | **a** | Grammar just initial; baseline only. | — |
| **bash** | grammar v0.25 | minor | **a** | Script extraction unchanged. | — |
| **rust (covered above)** | — | — | — | — | — |

---

## Summary — the (b) needs-new-extraction backfill worklist (ranked by value)

These are the genuinely **new constructs grafel should model** that the cutover
(B1 grammar bump / B2 decouple) unblocks. Each needs new extractor logic **and**
a coverage-registry cell in the same PR (`coverage fmt --check`), built via the
[C2 recipe](./extractor-recipe.md). All carry the **grammar-bump prerequisite**.

| Rank | Language | (b) feature | Proposed entity Kind / relationship | Why it matters (graph consumer) | Effort |
|---|---|---|---|---|---|
| 1 | **csharp** | C# 14 **extension blocks / extension members** | walk the `extension` block as a member container; emit its properties/methods as `SCOPE.Operation`/`SCOPE.Schema` with an `EXTENDS_TYPE`/REFERENCES edge to the extended type | extension members are the modern API surface; missing them under-reports a type's operations + call targets | M |
| 2 | **swift** | **`actor` / `distributed actor`** declarations | `SCOPE.Component` subtype `actor` (mirror class/struct/enum already in `swift.go`) | concurrency components are first-class architecture nodes; today they vanish from the graph | S |
| 3 | **kotlin** | **context parameters** (`context(x) fun …`) | a dependency/INJECTED-style edge from the function/property to the context type | context params are an implicit-DI mechanism; surfacing them matches constructor-injection modeling grafel already does elsewhere | M |
| 4 | **python** | **t-strings (PEP 750 template strings)** | new `template_string` literal branch on the RENDERS / raw-SQL sink passes (no new Kind) | t-strings are the sanctioned templating/SQL-building primitive; missing them loses RENDERS + raw-SQL edges that the `f-string` path already captures | S |
| 5 | **javascript/typescript** | **`await using` / explicit resource management** | an effects/cleanup edge (DISPOSES) on the resource binding | low value today (sparse adoption); defer behind 1-4 | S |

**(c) changes-existing-extraction** (adaptation so existing emission doesn't
silently stop): java module-import nodes (`imports.go`), JS stage-3 decorator
`accessor` shape (DI/route walks), C# partial constructors/events + C# 13
`allows ref struct`/partial-property merge (already in the C1 worked example), Go
generic type-alias param list (`type_table.go`) + `tool` go.mod directive
(`gomod.go`), php property-hook bodies (descend for missed calls).

**Net:** of the 27 grammar-backed languages, the real **(b) backfill** is
concentrated in **5 high-value languages** — csharp (extension members), swift
(actors), kotlin (context params), python (t-strings), and a deferrable js/ts
(`await using`). Everything else is **(a) parse-only** or **(c) small
adaptation**. The config/smaller languages are uniformly **(a)**.

### Added

- **C3 new-feature impact analysis (#5417):** per-language triage of language
  features released during the ~22-month grammar catch-up window (2024-08 →
  2026-06), classified (a) parse-only / (b) needs-new-extraction / (c)
  changes-existing-extraction against grafel's actual extractors. Identifies the
  (b) backfill worklist — C# 14 extension members, Swift actors, Kotlin context
  parameters, Python t-strings, JS/TS `await using` — and the (c) adaptations,
  all gated on the B1 grammar-bump / B2 smacker-decouple cutover.
  ([`docs/c3-feature-impact-analysis.md`](docs/c3-feature-impact-analysis.md))
