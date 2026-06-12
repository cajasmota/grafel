<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.groovy.base` — Groovy (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [groovy](../by-language/groovy.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4914 | `internal/extractors/groovy/groovy.go`<br>`internal/extractors/groovy/localvar_receiver_4749_test.go`<br>`internal/extractors/groovy/relationships_test.go` | CALLS edges (#372) are mined per method/function body from every function_call descendant (extractCallRelationships). Bare helper() -> ToID="helper"; a dotted_identifier receiver obj.method() -> ToID="method", and when the receiver's first segment is PascalCase the target is emitted as the dotted "<Type>.<method>" form with Properties["receiver_type"]=<Type>. #4749 local-variable receiver typing: `def c = new FooController(); c.index()` -> CALLS FooController.index (collectGroovyLocalVarTypes builds varName->ClassName ONLY from a DIRECT `new ClassName(...)` initialiser; a factory/builder RHS leaves the local untyped so no class edge is forged — the Java #4682 negative-case guard), with constructor-call (`new X()`) phantom suppression (groovyConstructorCalls). Each edge is stamped Properties["line"] (1-based, call.StartPoint().Row+1). Self-recursion on bare (non-dotted) targets and the keyword heads this/super/new are filtered; dotted cross-type targets sharing the caller's leaf name are NOT dropped (#2114). Proven by relationships_test.go + localvar_receiver_4749_test.go. |
| Core extraction | ✅ `full` | `2026-06-12` | 4914 | `internal/extractors/groovy/groovy.go`<br>`internal/extractors/groovy/groovy_grails_gradle_test.go`<br>`internal/extractors/groovy/groovy_test.go`<br>`internal/extractors/groovy/types.go`<br>`internal/extractors/groovy/types_test.go` | Tree-sitter (smacker/go-tree-sitter/groovy) extractor. Emits: `class` (class_declaration/class_definition) -> SCOPE.Component(subtype=class); `interface X {}` (the grammar parses interface as a class_definition) is also captured as a SCOPE.Component; class methods (method_declaration, and `def`-style function_definition inside a class body) -> SCOPE.Operation(subtype=method); top-level `def name()` -> SCOPE.Operation(subtype=function), with CONTAINS edges from each class to every Operation declared in its body (canonical Format-A structural ref scope:operation:method:groovy:<file>:<name>, #372). Gradle build DSL: `apply plugin: '<id>'` -> SCOPE.Component(subtype=plugin_id, gradle_dsl=apply_plugin) and `task <name> {…}` / `task <name>(type: X)` -> SCOPE.Operation(subtype=task, gradle_dsl=task). #4914 (types.go) adds the Groovy enum value-set previously unmodelled: the smacker grammar has no dedicated enum node — `enum X {…}` parses as a `declaration[enum, X]` header whose body is the following `closure` sibling — so buildGroovyEnumValueSet pairs the header with that closure and emits a SCOPE.Enum value-set via extractor.EnumEntity(kind_hint=groovy_enum). Bare constants (`RED, GREEN, BLUE`, surfaced inside an ERROR>parameter_list) yield one member each; valued constants (`ACTIVE(1)`, `HEARTS('red')`, parsed as function_call+argument_list) lift the single leading literal (int/float/string/bool, quote-stripped) to the member value. Member collection stops at the first enum-body `declaration` so trailing fields/constructors (`double mass; Planet(double m){…}`) are never mis-counted. Proven by groovy_test.go + types_test.go (PlainEnumValueSet/ValuedEnum/StringValuedEnum/EnumWithBodyExcludesFieldsAndCtor/NoEnumNoEmit). Honest follow-ups (#4914 tail): `trait`/`@interface` (annotation) decls and multi-arg enum constant values are not yet modelled; interface members are emitted but the Component subtype is not distinguished from class. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4914 | `internal/extractors/groovy/groovy.go`<br>`internal/extractors/groovy/relationships_test.go` | IMPORTS edges (#372, parity with the python #93 / java #120 / scala #379 contract) are emitted one-per `import` directive (buildImportRecord) as a SCOPE.Component placeholder carrying file.Path -> module IMPORTS with the full Properties contract: `import foo.Bar` -> local_name=Bar/source_module=foo/imported_name=Bar; `import foo.Bar as Baz` -> local_name=Baz/imported_name=Bar (alias preserved); `import foo.something.*` -> wildcard=1/source_module=foo.something; `import static foo.Util.helper` -> import_kind=static; `import static foo.Util.*` -> wildcard=1+import_kind=static. A raw-text fallback parses the same shapes for grammars that don't expose qualified_name directly. Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.groovy.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
