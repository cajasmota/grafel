<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.fsharp.core` — F#

Auto-generated. Back to [summary](../summary.md).

- **Language:** [F#](../by-language/fsharp.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/resolve/dynamic_patterns_fsharp.go` | #4906: collectCalls scans each let/member body for CALLS edges from three F# call shapes — paren application `name(` / `Module.fn(` (callRE), the pipe operator `|> name` / `|> Module.fn` (pipeCallRE), and the compose operator `>> fn` (composeCallRE) — after stripStringsAndComments scrubs string literals (single / verbatim `@""` / triple-quoted) and `//` line + `(* *)` block comments so call-shaped tokens inside them never leak. An fsharpKeywords denylist drops control-flow / binding keywords (if/match/fun/let/...); single-letter (type-param) targets and self-recursion are filtered; targets de-duplicated per body. CALLS-target QUALITY is sharpened by internal/resolve/dynamic_patterns_fsharp.go, which recognises F#-unique stdlib module-qualified identifiers (List./Seq./Array./Option./Result./Async./Task.* and the Giraffe HTTP combinators route/routef/choose/setStatusCode/text/json) plus an fsharp-gated common-name set (printfn/sprintf/failwith/ignore/id/fst/snd) so the resolver binds them confidently. Partial (honest): calls are body-scoped, not 1-based-line-stamped the way the Nim/Erlang extractors are; SPACE-APPLIED application (`f arg`, the dominant functional idiom) is NOT captured by the paren-anchored callRE — only paren / pipe / compose call sites are. Space-application capture + per-call line stamping are documented follow-ups. |
| Core extraction | ✅ `full` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #4906: regex-based, indentation-aware extractor (no tree-sitter F# grammar in smacker/go-tree-sitter; mirrors the Nim/Crystal precedent). moduleRE (`module [rec] Foo.Bar`) and namespaceRE emit SCOPE.Component (subtype module / namespace). letRE emits `let`/`let rec`/`let mutable` bindings as SCOPE.Operation (subtype let; signature via buildLetSig, generic `<'T>` params tolerated); memberRE emits member/override/abstract member/default definitions as SCOPE.Operation (subtype member), de-duplicated against same-name let bindings to avoid double-counting. Each operation's body is bounded by extractIndentBody (off-side-rule run of lines indented more than the declaration) and its StartLine/EndLine stamped. typeRE emits type declarations as SCOPE.Component, with classifyTypeSubtype distinguishing record / discriminated_union / interface / class / struct / alias (`= {` -> record; `= |` / body-leading `|` -> discriminated_union — the F# enum analog; interface/class/struct keywords). A type CONTAINS its more-indented members (memberRef CONTAINS edges via BuildOperationStructuralRef). All entities/relationships tagged language=fsharp (TagEntitiesLanguage/TagRelationshipsLanguage). Proven by the fsharp_test.go suite. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go`<br>`internal/substrate/fsharp.go` | #4906: collectOpenStatements parses `open Module.Path` statements (inline-comment stripped, de-duplicated) and buildImportEntities emits one IMPORTS edge per opened module to a SCOPE.Component placeholder (importDisplayName normalises `Microsoft.FSharp.Collections` -> `Collections`); every module/type/operation entity also carries the file's open-set on Properties["imports"]. The constant-binding sniffer internal/substrate/fsharp.go additionally recognises module-level `[<Literal>] let X = "..."` and `Environment.GetEnvironmentVariable(...) |> Option.defaultValue` / `defaultArg` env-default shapes plus `open` paths. Partial (honest): IMPORTS edges target the raw opened-module string, not a resolved module entity, and F# `open`-shadowing / auto-open / type-extension resolution is not modelled. Symbol-level resolution is a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.fsharp.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
