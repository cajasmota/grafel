<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.core` — Nim

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4904 | `internal/extractors/nim/nim.go`<br>`internal/extractors/nim/nim_test.go` | collectCalls emits a CALLS edge per proc invocation inside a body, each carrying Properties["line"] (1-based line within the scanned body). String literals (single/double/triple-quoted) and #-comments are scrubbed by stripStringsAndComments before scanning, and a nimKeywords denylist drops control-flow/keyword tokens the call regex would otherwise pick up; self-recursion is filtered and duplicate targets de-duplicated. Proven by TestNim_CallsEdges, TestNim_SelfRecursionExcluded, TestNim_CallsDeduped. |
| Core extraction | 🟢 `partial` | `2026-06-12` | 4904 | `internal/extractors/nim/nim.go`<br>`internal/extractors/nim/nim_test.go` | Line/indent-oriented regex extractor (no tree-sitter Nim grammar). procRE emits proc/func/method/template/macro/iterator/converter declarations as SCOPE.Operation (subtype distinguishes method/template/macro/iterator from the default proc; signatures via buildSig; export `*` marker stripped) — TestNim_ProcDiscovery + TestNim_ExportMarkerStripped. typeRE emits type declarations as SCOPE.Component with correct subtype for object/ref object/enum/tuple/distinct (distinct is the Nim type-alias analog) — TestNim_TypeDiscovery. CONTAINS edges from a type to procs/methods taking that type as a parameter (containsTypeName param-substring receiver heuristic — WEAK: matches on any param-list substring of the type name) — TestNim_ContainsEdges. All entities/relationships tagged language=nim — TestNim_LanguageTagged. STATUS RETAGGED full -> partial (#6432). The published meaning of the ✅/`full` tier is *fixture-proven, resolves the general case* — that is the legend printed on every by-language page — and nothing in this tree measures this extractor's recall: internal/quality/golden/ carries no fixture for it, and internal/quality/golden/baseline.json records no entity_found/entity_expected figures for it. The unit tests cited above prove the shapes they name; they do not measure recall over real sources. #6424 established what that difference is worth: solidity carried core_extraction=full on exactly this class of evidence, and the first graded fixture written for it scored 19/40 entities, because a contract declared with base-constructor arguments silently dropped its entire file — every function, event, modifier and state variable in it. This extractor is line/regex-oriented rather than AST-resolved, which is `partial`'s own published meaning, so the tier is wrong on both halves of its definition. Retag to full only when a graded fixture exists for this extractor and its measured recall is recorded in baseline.json, the way lang.solidity.core now is. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 4904 | `internal/extractors/nim/nim.go`<br>`internal/extractors/nim/nim_test.go` | collectImports parses `import a, b`, `import std/strutils`, `include module`, and `from X import Y`, emitting one IMPORTS edge per module with importDisplayName normalisation (std/strutils -> strutils) — proven by TestNim_ImportEdges. Partial (honest): a `from X import sym1, sym2` records only the module X, not the imported symbols; imports are edges to the module path string, not resolved to the concrete module SCOPE.Component entity. Symbol-level from-import and module resolution are follow-up #4932. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
