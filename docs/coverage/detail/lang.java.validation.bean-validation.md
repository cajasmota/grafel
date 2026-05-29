<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.validation.bean-validation` — Bean Validation (Jakarta/javax)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | ✅ `full` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go`<br>`internal/custom/java/bean_validation_test.go` | @Valid (cascade/nested validation marker) is recognized and lifts the annotated parameter to required; no recursion into the nested type's own fields. |
| Schema extraction | ✅ `full` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go`<br>`internal/custom/java/bean_validation_test.go`<br>`testdata/fixtures/sources/java/bean_validation/ValidatedDtoFixture.java` | Parameter-level Bean Validation constraints (@NotNull, @NotBlank, @Size, @Min, @Max, @Email, @Pattern) are captured in the Annotations slice on each handler parameter and drive the Required flag. Field-level recursion into nested DTO classes is not implemented (partial scope). Proven by TestBeanValidation_SchemaExtraction_Issue3002 and TestBeanValidation_MultipleConstraints_Issue3002. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go`<br>`internal/custom/java/bean_validation_test.go`<br>`internal/engine/java_annotation_params.go` | Bean-Validation annotations (@NotNull/@NotBlank/@NotEmpty/@Size/@Min/@Max/@Pattern/@Email) are collected on each handler parameter and drive the Required flag; captured as annotation strings, not structured constraint records (no value bounds parsed). |
| Custom validator extraction | ⚠️ `partial` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go`<br>`internal/custom/java/bean_validation_test.go` | Extracting classes that implement ConstraintValidator<A,T> requires scanning for the interface-implementation pattern. No current extractor does this. Leave red — out of scope for #3002. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | — `not_applicable` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go` | Java Bean Validation does not coerce types; type coercion is handled by JAX-RS ParamConverter / Spring Converter<S,T>. No extractor covers this pattern. Leave red — out of scope for #3002. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/junit5.go` | Bean Validation integration tests use JUnit 5 with jakarta.validation.Validator. The junit5 extractor (internal/custom/java/junit5.go) captures @Test methods for the junit5 framework tag. Tests for bean-validation handlers are linked via the same JUnit 5 test-method extraction path used by other Java frameworks (e.g. Jakarta EE, Spring Boot — see #2996, #2991). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.validation.bean-validation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
