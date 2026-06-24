<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elm.framework.tea` — The Elm Architecture (Elm frontend)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elm](../by-language/elm.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🟢 `partial` | `2026-06-24` | 5375 | `internal/extractors/elm/extractor.go`<br>`internal/extractors/elm/tea.go`<br>`internal/extractors/elm/tea_test.go` | The TEA view operation is tagged Properties[tea_role]=view (the render half of the MVU triad); the Model/Msg data types are re-kinded SCOPE.Model/SCOPE.Event. Honest-partial: unlike the F# Feliz pass, Elm view functions are not re-kinded SCOPE.UIComponent and no RENDERS edges between nested view helpers are emitted (the Html DSL is heuristic-only) — deferred. |
| Context extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Data fetching | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Prop extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | ✅ `full` | `2026-06-24` | 5375 | `internal/extractors/elm/extractor.go`<br>`internal/extractors/elm/tea.go`<br>`internal/extractors/elm/tea_test.go` | TEA MVU: the Model type alias re-kinded SCOPE.Model (tea_model, tea_role=model), the Msg custom type re-kinded SCOPE.Event (tea_msg) with its '|'-separated constructor variants recorded on Properties[tea_variants] as the event set, and init/update/view tagged Properties[tea_role]. The Browser.sandbox/element/document/application program entry (idiomatically main) is flagged Properties[tea_program]=true + tea_program_kind. Import-gated on Browser/Html (no-op for a plain Elm helper module). Proven by TestTEA_ModelRekind / _MsgRekindWithVariants / _TriadRoles / _ProgramFlagged / _NonFrontendNoop. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-24` | 5375 | `internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_elm.go`<br>`internal/extractors/cross/testmap/frameworks_elm_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | elm-test linkage via the cross-language testmap extractor. detectElmTest (frameworks_elm.go) detects describe "Subject" [ test "..." <| \_ -> ... ] leaves plus fuzz/fuzzN/fuzzWith property cases; the description is the LAST string literal (fuzz leaves carry a fuzzer arg first). Each leaf's list/off-side-rule-bounded body (extractElmTestBody, ends at the next ,/]/column-0 dedent) is scanned by the resolver: an Elm SPACE-APPLIED production call (add 2 2 — Elm is curried, invisible to the paren directCallRE) resolves to a high-confidence TESTS edge via elmSpaceAppRE (gated tf.lang==elm, keyword+stopword filtered), and the describe subject seeds a medium-confidence subject edge. The elm-test/Expect/Fuzz DSL (describe/test/fuzz*/Expect.*/Fuzz.*) is denylisted in resolver.go so it never surfaces as the SUT. FILENAME/PATH gated (*Test.elm / *Tests.elm / /tests/) — NOT import gated (the bare test/expect tokens would over-match the substring import matcher). Proven by TestElmTest_DirectCallHighConfidence / _BodyScoped / _FuzzCase / _AssertionDSLNotSubject. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elm.framework.tea ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
