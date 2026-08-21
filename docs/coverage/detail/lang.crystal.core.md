<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.core` — Crystal

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | 🟢 `partial` | `2026-06-12` | 4905 | `internal/extractors/crystal/extractor.go`<br>`internal/extractors/crystal/extractor_test.go` | extractCallRelationships scans each def/macro body for `[receiver.]name(` invocations (callRE), emitting a CALLS edge per call; a skipKeywords denylist drops Crystal control-flow/keyword tokens (if/unless/while/case/return/yield/raise/new/super/...) the call pattern would otherwise match, and callKeywordReceivers strips `self.`/`super.` receiver roots. Partial (honest): calls do not yet carry a 1-based line Property the way the Nim/Erlang extractors do (call edges are body-scoped, not line-stamped), and string-literal/comment scrubbing is keyword-denylist-based rather than a full lexer — interpolated call sites inside strings can leak. Line-precision + literal scrubbing deepening is follow-up #4937. |
| Core extraction | 🟢 `partial` | `2026-06-12` | 4905 | `internal/extractors/crystal/extractor.go`<br>`internal/extractors/crystal/extractor_test.go` | Line/indent-oriented regex extractor (no tree-sitter Crystal grammar — smacker/go-tree-sitter has no Crystal grammar; mirrors the Dart precedent). classRE emits class/abstract class/struct as SCOPE.Component (subtype class); moduleRE emits module; libRE emits lib (C-binding block) — each carrying StartLine/EndLine spanned by findEndKeyword's nesting-aware block scanner and a first-line Signature. defRE emits def/abstract def/`def self.x` as SCOPE.Operation (subtype method) and macroRE emits macro (subtype macro), each attached to its innermost enclosing scope via a CONTAINS edge (enclosingScope byte-range nesting). isScopeDeclaration prevents a def from double-consuming a scope header. All entities/relationships tagged language=crystal. Proven by the TestCrystal_* suite in extractor_test.go. WEAK (follow-up #4937): enum/`alias`/generic type-params are not emitted, defs are named bare with no Type.method receiver, and macro-generated methods are invisible to the regex scanner. STATUS RETAGGED full -> partial (#6432). The published meaning of the ✅/`full` tier is *fixture-proven, resolves the general case* — that is the legend printed on every by-language page — and nothing in this tree measures this extractor's recall: internal/quality/golden/ carries no fixture for it, and internal/quality/golden/baseline.json records no entity_found/entity_expected figures for it. The unit tests cited above prove the shapes they name; they do not measure recall over real sources. #6424 established what that difference is worth: solidity carried core_extraction=full on exactly this class of evidence, and the first graded fixture written for it scored 19/40 entities, because a contract declared with base-constructor arguments silently dropped its entire file — every function, event, modifier and state variable in it. This extractor is line/regex-oriented rather than AST-resolved, which is `partial`'s own published meaning, so the tier is wrong on both halves of its definition. Retag to full only when a graded fixture exists for this extractor and its measured recall is recorded in baseline.json, the way lang.solidity.core now is. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 4905 | `internal/extractors/crystal/extractor.go`<br>`internal/extractors/crystal/extractor_test.go` | requireRE parses `require "path"` and `require_relative "path"`, emitting one IMPORTS edge per required path to a SCOPE.Component placeholder keyed by the path string. Partial (honest): the edge targets the raw path literal, not a resolved module entity, and shard (dependency) paths vs project-relative paths are not distinguished; symbol-level resolution is a follow-up. Proven by the require-edge assertions in extractor_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
