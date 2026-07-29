<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.solidity.core` — Solidity

Auto-generated. Back to [summary](../summary.md).

- **Language:** [solidity](../by-language/solidity.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | 🟢 `partial` | `2026-06-24` | 5371 | `internal/extractors/solidity/extractor.go`<br>`internal/extractors/solidity/extractor_test.go` | collectCallsFromBody emits CALLS edges per dotted (Type.method()) and bare (fn()) invocation in a function/modifier body, with a solidityKeywords denylist dropping control-flow/builtins/type/visibility tokens and self-recursion filtering on undotted leaves (dotted cross-contract calls preserved, #2114). Partial (honest): edges do NOT yet carry a per-call line property; targets are unresolved name strings, not bound to the callee SCOPE.Operation. |
| Core extraction | ✅ `full` | `2026-07-29` | 5371 | `internal/extractors/solidity/extractor.go`<br>`internal/extractors/solidity/extractor_test.go`<br>`internal/extractors/solidity/line_attribution_test.go` | Regex extractor (no bundled tree-sitter Solidity grammar). contractRE emits contract/library/interface/abstract-contract as SCOPE.Component (subtype contract/library/interface); functionRE/eventRE/modifierRE emit members as SCOPE.Operation (subtype function/event/modifier) with CONTAINS edges from the owning contract. Comments and string literals are scrubbed (stripCommentsAndStrings) and brace-matched bodies extracted (extractBracedBody) before member/call scanning; the scrub preserves byte length and newline positions, so lines counted over the scrubbed text still match the source. The member regexes end at the opening paren, so declSignature scans on from the match with paren depth to the terminating { or ; and Signature carries the full parameter list, mutability/visibility and returns clause; before that it stopped at the paren. StartLine is the declaration's own line: leading whitespace in the declaration regexes can no longer cross a line boundary, members are anchored on the contract's opening brace rather than its match start, and buildImportEntities sets a line on import entities, which previously reported 0 (#6040). Proven by the extractor_test.go and line_attribution_test.go suites (contract/library/interface discovery, member CONTAINS, signature capture, reported lines). |
| Import resolution quality | 🟢 `partial` | `2026-06-24` | 5371 | `internal/extractors/solidity/extractor.go`<br>`internal/extractors/solidity/extractor_test.go` | buildImportEntities parses both import styles (import "./Foo.sol"; and import {Sym} from "pkg/...";), emitting one IMPORTS edge per target path with source_module/imported_name/local_name properties and a SCOPE.Component for the imported unit. Partial (honest): named-symbol imports record only the module path (not per-symbol bindings) and edges point at the raw path string, not a resolved file/contract entity. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.solidity.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
