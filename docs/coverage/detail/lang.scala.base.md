<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.base` — Scala (base language)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4915 | `internal/extractors/scala/issue2114_self_recursion_dotted_target_test.go`<br>`internal/extractors/scala/localvar_receiver_4749_test.go`<br>`internal/extractors/scala/relationships_test.go`<br>`internal/extractors/scala/scala.go` | #4915 documents the previously-undocumented base record. CALLS edges (#379) are mined per function body from every call_expression descendant (extractCallRelationships): a bare helper() -> ToID="helper"; a field_expression receiver `obj.m()` -> ToID="m", and when the receiver resolves to a known type via the enclosing class's val/var/class-parameter members (collectContainerFieldTypes), the function's own parameters (collectParamTypes) or a local-variable `val x = new T()` initialiser (#4749, collectScalaLocalVarTypes — DIRECT new-ctor only, a factory/builder RHS leaves the local untyped so no class edge is forged), the target is emitted as the dotted "<Type>.<method>" form with Properties["receiver_type"]=<Type>. Each edge is stamped Properties["line"] (1-based, call.StartPoint().Row+1). Dotted cross-type targets that share the caller's leaf name are NOT dropped (#2114 self-recursion guard applies only to bare targets). Proven by relationships_test.go + localvar_receiver_4749_test.go + issue2114_self_recursion_dotted_target_test.go. |
| Core extraction | ✅ `full` | `2026-06-12` | 4915 | `internal/extractors/scala/constantset.go`<br>`internal/extractors/scala/exception_flow.go`<br>`internal/extractors/scala/issue4432_constantset_test.go`<br>`internal/extractors/scala/scala.go`<br>`internal/extractors/scala/scala_test.go` | Tree-sitter (smacker/go-tree-sitter/scala) extractor. Emits: class_definition -> SCOPE.Component(subtype=class), and case class (case_class_definition, or class_definition whose raw text is prefixed `case class `) -> subtype=case_class; trait_definition -> SCOPE.Component(subtype=trait); object_definition -> SCOPE.Component(subtype=object); function_definition/function_declaration -> SCOPE.Operation(subtype=function), with CONTAINS edges from each class/object/trait to every Operation declared in its template_body via the canonical Format-A structural ref (scope:operation:method:scala:<file>:<name>, #379). Twirl templates (*.scala.html, #501) get a SCOPE.Component(subtype=twirl) file entity and are still CST-walked. #4432 (constantset.go) indexes constant collections / enumerations as searchable SCOPE.Enum value-sets via the shared extractor.EnumEntity helper: object const groups, `val X = Map(...)/Seq(...)`, Scala 3 `enum X { case A, B }`, and sealed-trait + case-object enumerations — members lifted with StripLiteralQuotes, non-literal values dropped honest-partial. #3628 (exception_flow.go) adds THROWS/CATCHES edges to shared SCOPE.ExceptionType convergence nodes. Proven by scala_test.go + issue4432_constantset_test.go + exception_flow_test.go. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4915 | `internal/extractors/scala/relationships_test.go`<br>`internal/extractors/scala/scala.go` | IMPORTS edges (#379, parity with the python #93 / java #120 contract) are emitted per `import` directive (buildImports) carrying the Properties contract: `import a.b.c` -> local_name=c/source_module=a.b/imported_name=c; `import a.b.{C, D}` selector groups emit one edge per selector with each leaf's local_name/imported_name; `import scala.collection.mutable._` -> wildcard=1/source_module=scala.collection.mutable (the ToID drops the wildcard leaf). namespace_wildcard and selector CST shapes are both handled. Proven by relationships_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.base ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
