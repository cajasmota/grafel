<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.validation.bean-validation` — Bean Validation / konform / Valiktor (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🟢 `partial` | `2026-06-12` | 4972 | `internal/custom/kotlin/validation.go`<br>`internal/custom/kotlin/validation_test.go` | Data-class fields are indexed (indexDataClasses/emitDataClasses) and bean constraints bound per-field, but recursion into nested DTO field types to emit a VALIDATES owner->nested edge (the Java #3605 path) is not implemented for Kotlin. Field shapes captured; nested @Valid edge not yet materialised. Tracked by #4972. |
| Schema extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/kotlin/validation.go`<br>`internal/custom/kotlin/validation_test.go` | kotlinValidationExtractor emits request_validation rules from @Valid/@Validated handler params, per-field bean-constraint rules (@NotNull/@NotBlank/@Size/@Min/@Max/@Pattern/@Email) on data classes, konform Validation<T>{} DSL rules, and Valiktor/Arrow validate(foo){} contract blocks; each validated type also emits a dto entity. Proven by TestValidationAtValid, TestValidationFieldAnnotations, TestValidationKonformDSL, TestValidationContractBlock, TestValidationDTOPropertyShape. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/kotlin/validation.go`<br>`internal/custom/kotlin/validation_test.go` | beanConstraintBounds folds Jakarta/javax bean constraints into Size/Min/Max/Pattern bounds; konform constraints (minLength/maxLength/pattern/minimum/maximum) parsed via reKonformConstraint into per-field request_validation rules. Proven by TestValidationBeanRulesFieldConstraintBounds and TestValidationKonformDSL. |
| Custom validator extraction | 🟢 `partial` | `2026-06-12` | 4924 | `internal/custom/kotlin/validation.go`<br>`internal/custom/kotlin/validation_test.go` | Valiktor / Arrow-style validate(foo){} and Validator{} contract blocks are recognised as request_validation owners (emitContractBlocks), capturing custom validation entrypoints. Classes implementing a dedicated Validator interface are not scanned. Proven by TestValidationContractBlock / TestValidationContractWithTypeEmitsDTO. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | — `not_applicable` | `2026-06-12` | — | `internal/custom/kotlin/validation.go` | Kotlin bean/konform/Valiktor validation does not coerce types; coercion is a serializer concern (kotlinx.serialization). Out of scope. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-12` | 3437 | `internal/extractors/kotlin/tests.go` | Kotlin tests (Kotest specs + JUnit5 @Test fun) link to validation handlers via the shared kotlin tests.go path (emitKotestTestScopeOwner + walk() @Test mining). Validation-specific test->SUT linkage depth tracked under #3437. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.validation.bean-validation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
