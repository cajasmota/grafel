<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.swift.base` — Swift (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4913 | `internal/extractors/swift/relationships_test.go`<br>`internal/extractors/swift/swift.go` | CALLS edges are mined per function body from tree-sitter call_expression nodes (extractCallRelationships). Bare `foo()` -> ToID="foo"; navigation `obj.method()` -> ToID="method" with the leftmost receiver root captured, and when the receiver is a known same-class stored property of declared type T the edge carries Properties["receiver_type"]=T (mirrors Java's T.method goal in property form, via collectFieldTypes). Each edge is stamped Properties["line"] (1-based, call.StartPoint().Row+1). Self-recursion (target==callerName) and the keyword heads self/super/Self are filtered. Proven by relationships_test.go. |
| Core extraction | ✅ `full` | `2026-06-12` | 4913 | `internal/extractors/swift/field_members.go`<br>`internal/extractors/swift/swift.go`<br>`internal/extractors/swift/swift_test.go`<br>`internal/extractors/swift/types.go`<br>`internal/extractors/swift/types_test.go` | Tree-sitter (smacker/go-tree-sitter/swift) extractor. Emits: `class`/`struct`/`enum` declarations (the grammar reuses node type class_declaration, distinguished by the first keyword child) -> SCOPE.Component(subtype=class|struct|enum); `protocol` -> SCOPE.Component(subtype=protocol); top-level and member `func` declarations -> SCOPE.Operation(subtype=function); each type attaches one CONTAINS edge per function declared in its body (Format-A structural ref scope:operation:method:swift:<file>:<name>, #381). #4854 field membership (field_members.go): each stored let/var property -> SCOPE.Schema/field with a type->field CONTAINS edge, plus in-file base-class EXTENDS (attachSwiftExtends). #4913 (types.go) adds the Swift TYPE SYSTEM previously unmodelled: an `enum` is additionally emitted as a SCOPE.Enum value-set via extractor.EnumEntity(kind_hint=swift_enum) — one member per `case` identifier (comma-grouped `case a, b` -> two members), with `case x = <literal>` raw values (int/string/bool) lifted to the member value (StripLiteralQuotes); and `typealias Name = <type>` -> SCOPE.Schema(subtype=type_alias) with type_body (function-type and generic RHS captured), superseding the vapor-only reSwiftTypealias->Component v1. The enum value-set is emitted ALONGSIDE the nominal Component (no replacement, TestSwiftTypes_PlainEnumValueSet asserts both survive). Proven by swift_test.go + types_test.go (PlainEnumValueSet/RawValueEnum/StringRawValueEnum/TypeAlias/NoTypeAliasNoEmit). Honest follow-ups (#4913 tail): associated-value enum cases keep only the case name (no payload type modelling); computed/expression raw values keep only single-literal forms; extension-declared members and generic where-clauses are not yet attributed. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4913 | `internal/extractors/swift/relationships_test.go`<br>`internal/extractors/swift/swift.go` | IMPORTS edges (#381 PORT-RELS-SWIFT, parity with java #120 / python #93) are emitted one-per `import Module` directive (buildImport). For `import Module.Submodule.Symbol` the leaf becomes local_name/imported_name and the prefix is the source_module. A file-level SCOPE.Component(subtype=file) is also emitted (#577) so the cross-repo import linker (#566) can map IMPORTS back to the originating repo via the resolver byName index. Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.swift.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
