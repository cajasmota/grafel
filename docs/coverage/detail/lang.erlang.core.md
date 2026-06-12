<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.erlang.core` — Erlang

Auto-generated. Back to [summary](../summary.md).

- **Language:** [erlang](../by-language/erlang.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4903 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | Every CALLS edge carries Properties["line"] (1-based line within the clause body) — both qualified Module:Function and bare Function calls. Comments, strings and quoted atoms are scrubbed before scanning (stripCommentsAndStrings) so tokens inside literals are not mistaken for calls. Proven by TestErlangExtractor_CallsEdge + TestErlangExtractor_QualifiedCallsEdge. |
| Core extraction | ✅ `full` | `2026-06-12` | 4903 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | Line-oriented regex extractor (no tree-sitter grammar available). Emits -module → SCOPE.Component/module, function clauses grouped across all clauses by name → SCOPE.Operation (exported_function when in -export, else function), -record → SCOPE.Component/record, and CONTAINS edges from the module to each exported function. Multi-clause functions collapse to one entity (TestErlangExtractor_MultiClauseFunctions); .hrl headers are parsed for records (TestErlangExtractor_HrlFile). Exported-vs-private distinction recorded via the exported_function subtype + CONTAINS-only-for-exported (TestErlangExtractor_ContainsEdge). NOTE: function arity is dropped from identity (foo/1 and foo/2 collapse) — tracked in follow-up #4930. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 4903 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | Every -include("foo.hrl") / -include_lib("app/include/foo.hrl") attribute emits an IMPORTS relationship (import_kind=include) carrying the full path + leaf as local_name (TestErlangExtractor_IncludeImports). Partial: the include is recorded as an IMPORTS edge to the path string but is not resolved to the concrete .hrl SCOPE.Component entity, and -import(mod, [f/1]) function imports are not yet extracted (follow-up #4930). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.erlang.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
