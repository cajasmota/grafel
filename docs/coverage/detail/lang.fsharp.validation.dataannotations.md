<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.fsharp.validation.dataannotations` — DataAnnotations (F# records)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [F#](../by-language/fsharp.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | ✅ `full` | `2026-06-14` | 5130 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go`<br>`internal/extractors/fsharp/validators.go` | #5130 (follow-up #5049): a record field whose declared type is another record defined in the SAME file now materialises an owner->nested VALIDATES edge (Properties via=nested_model, field, line) on the owning SCOPE.Component type — the F# analog of recursive [<ValidateComplexType>] DataAnnotations validation. collectRecordTypeNames pre-scans the file for record type names (reusing typeRE+classifyTypeSubtype so it agrees with the main type pass); fsNestedRecordType strips F# type modifiers (option/list/seq/array/[]) so `ShipTo: Address` / `BillTo: Address option` both resolve to the Address record TYPE entity; the edge is de-duplicated per distinct nested type and self-references excluded. Honest residual (regex heuristic, no type resolution): only same-file record types are resolved — a nested record imported from another module is not followed, and a generic application Result<Address> is intentionally not treated as a nested validated object. Proven by TestFSharp_NestedModelValidates. |
| Schema extraction | ✅ `full` | `2026-06-13` | 5049 | `internal/extractors/fsharp/du_record_members.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #4942: F# record types emit each field as a SCOPE.Schema/field sub-entity (extractTypeMembers/parseRecordFields), with a type->field CONTAINS edge. Proven by TestFSharp_RecordFields. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-13` | 5049 | `internal/extractors/fsharp/du_record_members.go`<br>`internal/extractors/fsharp/field_validations.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #5049: System.ComponentModel.DataAnnotations attributes on record fields ([<Required>]/[<EmailAddress>]/[<StringLength(n)>]/[<MinLength(n)>]/[<MaxLength(n)>]/[<Range(lo,hi)>]/[<RegularExpression(..)>] and markers Phone/Url/CreditCard/Compare/...) are parsed off the preceding (and inline) [<...>] attribute lines and folded into terse comma-free chips on Properties["validations"] (Required, Email, MaxLength:120, Range:1..5, Pattern), which the dashboard ShapeTree renders. Mirrors the Java #4872 / TS #4858 / Python #4871 field-validation chips. Non-validation attributes ([<CLIMutable>]) are ignored. Proven by TestFSharp_DataAnnotationsValidations, TestFSharp_InlineValidationAttribute, TestFSharp_NonValidationAttributesIgnored. |
| Custom validator extraction | ✅ `full` | `2026-06-14` | 5130 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go`<br>`internal/extractors/fsharp/validators.go` | #5130 (follow-up #5049): THREE custom-validator surfaces are now linked. (1) Validus pipelines — a `validate { ... }`/`validator { ... }` computation expression and/or Check.String.*/Check.Int.*/validateField/ValidatorGroup combinators in a let/member body emit a VALIDATES edge from the enclosing SCOPE.Operation to the validator:validus stub (Properties library=validus, via=validator_pipeline, combinators, computation_expression, line). (2) FsToolkit.ErrorHandling — a `validation { ... }` CE and/or Validation.* applicative-accumulation combinators emit a VALIDATES edge to validator:fstoolkit (a bare Result.* WITHOUT the validation CE is ordinary error handling and is NOT over-claimed). (3) DataAnnotations custom validators — a [<CustomValidation(typeof<T>, "M")>] field attribute emits an owner->validator:dataannotations VALIDATES edge (via=custom_validation, validator_type, method, field) and a type implementing IValidatableObject emits one (via=ivalidatableobject), the type's Validate member being the custom validator. All map onto the existing VALIDATES relationship shape (JS class-validator #2904 / Java Bean-Validation #4872). Honest residual (regex head-symbol heuristics, no F# type/CE resolution, consistent with the rest of the fsharp extractor): pipeline recognition is by CE head + combinator presence (the validator:<lib> target is a synthetic stub, no stub entity emitted, mirroring the raw-string CALLS-target convention); a custom validator method referenced only by string is not resolved to its implementation. Proven by TestFSharp_ValidusPipeline, TestFSharp_ValidatorGroup, TestFSharp_FsToolkitValidation, TestFSharp_CustomValidationAttr, TestFSharp_IValidatableObject, TestFSharp_Validator_WrongLanguageNoOp, TestFSharp_Validator_NoMatchNoOp. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | — `not_applicable` | `2026-06-13` | — | — | DataAnnotations validate, they do not coerce types; model binding/coercion is the HTTP framework's responsibility. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-14` | 5130 | `internal/extractors/fsharp/fsharp_test.go` | #5130: validation-specific extraction fixtures now exist (happy-path Validus/FsToolkit/nested/custom + wrong-language no-op + no-match no-op in fsharp_test.go), and the general F# Giraffe/Saturn test->endpoint linkage substrate (#4749) supplies TESTS edges. Honest-partial: no validator-specific TESTS edge from a test asserting a validator is materialised yet (a test calling validateUser links to the operation, not to the validator:<lib> stub). Deferred deepening. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.fsharp.validation.dataannotations ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
