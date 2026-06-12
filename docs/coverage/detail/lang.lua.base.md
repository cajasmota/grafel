<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lua.base` — Lua (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4911 | `internal/extractors/lua/lua.go`<br>`internal/extractors/lua/relationships_test.go` | CALLS edges are mined from each function body (findFunctionBody) by extractCallRelationships: every function_call descendant is resolved to its callee via luaCallTarget — bare helper() -> "helper"; dotted foo.run(x) and method obj:bar() -> the trailing identifier ("run"/"bar"), i.e. the last identifier before the function_call_paren/function_arguments/string_argument child. Each unique target is stamped Properties["line"] (1-based, call.StartPoint().Row+1) and deduped per body (seen map). Self-recursion (target == callerName) and require (luaCallStop — it produces IMPORTS, not CALLS) are filtered. Proven by relationships_test.go. |
| Core extraction | ✅ `full` | `2026-06-12` | 4911 | `internal/extractors/lua/lua.go`<br>`internal/extractors/lua/lua_test.go`<br>`internal/extractors/lua/oop.go` | tree-sitter (smacker/go-tree-sitter lua grammar) extractor. Emits: function_statement / function_declaration (global function foo, dotted function M.foo, colon function M:bar) -> SCOPE.Operation(subtype=function|method — colon notation => method), with the receiver segment used to wire CONTAINS from the owning table (buildFunctionStatement/buildFunctionDecl); local function name -> SCOPE.Operation(subtype=function) (buildLocalFunction); top-level `local M = {}` empty-table declarations -> SCOPE.Component(subtype=module_table) (collectModuleTables), with one CONTAINS edge per `function M.x`/`function M:x` (BuildOperationStructuralRef, #375). #4911: the Lua metatable OOP idiom is now recognised (oop.go, applyOOP) — a `local T = {}` table that does `T.__index = T` (self-index) or `setmetatable(T, {__index = Parent})` is promoted from module_table to SCOPE.Component(subtype=class), and metatable inheritance (`local Child = setmetatable({}, {__index = Parent})` / `setmetatable(Child, {__index = Parent})` / `Child.__index = Parent`) emits an EXTENDS edge child -> parent (BuildComponentStructuralRef, Properties{base_name, inheritance=metatable, child_name}). Precision-first: only tables actually declared as top-level empty tables are promoted — inline setmetatable-declared children are never invented as class entities. Proven by TestLuaExtractor_GlobalFunctions / _LocalFunction / _OOPClassInheritance / _OOPInheritanceTableForm. Honest follow-ups (#4911 tail): module-level constants (`local X = 5`) are not emitted as SCOPE.Constant; nested-function / closure scoping is flattened; multi-level metatable chains beyond the direct parent are not walked; `Class = Class or {}` guard-tables and OO-library forms (middleclass `class()`, 30log) are not yet detected. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4911 | `internal/extractors/lua/lua.go`<br>`internal/extractors/lua/relationships_test.go` | IMPORTS edges (#375 PORT-RELS-LUA, parity with the java #120 / python #93 contract) are emitted one-per require: every `require("foo.bar")` and bare `require "foo"` call (walkForImportEdges + requireArgPath, handling function_arguments / string_argument / string node shapes and string_content extraction) becomes a SCOPE.Component carrying file.Path -> required-path IMPORTS with Properties{local_name, source_module, imported_name, import_kind="require"}. When bound to a local (`local foo = require(...)`, analyzeRequireDecl) the LHS identifier is local_name/imported_name; otherwise it falls back to the trailing dotted path segment. Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.lua.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
