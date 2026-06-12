<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.base` — Dart (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4912 | `internal/extractors/dart/dart.go`<br>`internal/extractors/dart/relationships_test.go` | CALLS edges are mined from each method/function body by extractCallRelationships: every `[recv.]name(` invocation head (callRE) is resolved to the leaf callee identifier — bare helper() -> "helper"; receiver-prefixed a.b.c.run() -> "run" with the leftmost root recorded under Properties["receiver_root"] (this./super. are normalised to no-receiver). Each unique (target, receiver_root) pair is stamped Properties["line"] (1-based, newline count to the match) and deduped per body (seen map). Self-recursion (target == callerName), Dart control-flow / built-in keyword heads (skipKeywords: if/for/while/return/new/await/yield/var/final/const/...), and return-type-tail false positives (alnum byte immediately before the match) are filtered. Proven by relationships_test.go. |
| Core extraction | ✅ `full` | `2026-06-12` | 4912 | `internal/extractors/dart/dart.go`<br>`internal/extractors/dart/dart_test.go`<br>`internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | Regex-based extractor (no smacker/go-tree-sitter Dart grammar is bundled, so this stays regex like the Python DartParser it ports). Emits: `class` / `abstract class` / `mixin` / `extension` declarations -> SCOPE.Component(subtype=class) (classRE), with brace-matched start/end lines (findBraceEndByte); top-level functions and class methods -> SCOPE.Operation(subtype=method) (methodRE, return-type-optional name+params+`{`); CONTAINS edges from each class to the methods whose byte-range falls inside its body (enclosingClass + BuildOperationStructuralRef, Format-A structural ref). #4912 (types.go): the Dart TYPE SYSTEM — previously dropped (`enum` was in skipKeywords, no typedef/sealed regex) — is now extracted: plain + Dart-2.17 enhanced `enum` -> SCOPE.Enum value-set via extractor.EnumEntity(kind_hint=dart_enum) keeping only the constant identifiers before the first `;` (member ctor args lifted to literal values when single-literal); modern `typedef Name = <type>;` and legacy function-type `typedef Ret Name(params);` -> SCOPE.Schema(subtype=type_alias) with type_body (parity with python/rust/go type_alias); Dart-3 class modifiers `sealed`/`base`/`interface`/`final`/`mixin class` -> SCOPE.Component(class) carrying Properties{class_modifier, dart_sealed, dart_interface} (the base classRE only allows a leading `abstract`, so these were invisible). The type pass is appended post-walk and never double-emits plain/abstract classes (TestDartTypes_PlainClassNotDoubleEmitted). Proven by dart_test.go + types_test.go (PlainEnum/EnhancedEnum/TypedefAlias/TypedefFunc/SealedClass). Honest follow-ups (#4912 tail): enum members with multi-arg/computed ctor values keep only single-literal values; nested/closure scoping is flattened; cross-file receiver-type binding for CALLS is unresolved (receiver_root is recorded for a future resolver pass). |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4912 | `internal/extractors/dart/dart.go`<br>`internal/extractors/dart/relationships_test.go` | IMPORTS edges (#369 PORT-RELS-DART, parity with the java #120 / python #93 contract) are emitted one-per `import '...';` directive (importRE handles single- and double-quoted URIs and the optional `as <prefix>` alias). Each becomes a SCOPE.Component placeholder (named by the URI scheme+top path segment so all `package:flutter/...` imports merge to one Component) carrying file.Path -> URI IMPORTS with Properties{local_name, source_module, imported_name(, alias)}: `import 'foo.dart' as fb;` -> local_name=fb; bare module imports use the leaf path segment (`package:flutter/material.dart` -> leaf=material, source_module=package:flutter; `dart:convert` -> source_module=dart:convert). Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.dart.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
