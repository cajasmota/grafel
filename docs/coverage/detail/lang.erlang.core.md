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
| Core extraction | ✅ `full` | `2026-06-12` | 4930 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | Line-oriented regex extractor (no tree-sitter grammar available). Emits -module → SCOPE.Component/module, function clauses → SCOPE.Operation (exported_function when in -export, else function), -record → SCOPE.Component/record, and CONTAINS edges from the module to each exported function. #4930: FUNCTION ARITY IS NOW PART OF IDENTITY — Erlang functions are name/arity, so clauses are grouped on the (name, arity) pair (groupClauses keys "name/arity"), making foo/1 and foo/2 DISTINCT SCOPE.Operation entities; each carries Signature="name/arity" and Properties["arity"]. countArity counts top-level args in the clause head respecting nested () {} [] <<>> and scrubbed strings/comments (empty parens → arity 0). Export classification is arity-precise: only the exported arity gets subtype exported_function (lookup/1 exported, lookup/2 private → distinct subtypes; exportedAr set keyed "name/arity"). Same-name/arity multi-clause functions still collapse to one entity (TestErlangExtractor_MultiClauseFunctions: fib/1). Proven by TestErlangExtractor_ArityIdentity (lookup/1 exported_function + lookup/2 function + start/0) and TestErlangExtractor_ArityNestedArgs (handle/3 with nested tuple/list/binary args). .hrl headers are parsed for records (TestErlangExtractor_HrlFile). Exported-vs-private distinction also via CONTAINS-only-for-exported (TestErlangExtractor_ContainsEdge; bare-name keyed since BuildOperationStructuralRef carries no arity). NOTE: CALLS edges are still resolved by bare function name (Erlang call sites do not always carry a recoverable arity at the regex level) — arity-aware call resolution is deferred to #4989. Other #4930 follow-ups filed: rebar3/erlang.mk build_system+package_manager records (#4987), eunit/common_test extractor-level test frameworks (#4988), -spec/-type/-callback type system + -import resolution + -define macro expansion (#4989). |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 4903 | `internal/extractors/erlang/extractor.go`<br>`internal/extractors/erlang/extractor_test.go` | Every -include("foo.hrl") / -include_lib("app/include/foo.hrl") attribute emits an IMPORTS relationship (import_kind=include) carrying the full path + leaf as local_name (TestErlangExtractor_IncludeImports). Partial: the include is recorded as an IMPORTS edge to the path string but is not resolved to the concrete .hrl SCOPE.Component entity, and -import(mod, [f/1]) function imports are not yet extracted (follow-up #4930). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.erlang.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
