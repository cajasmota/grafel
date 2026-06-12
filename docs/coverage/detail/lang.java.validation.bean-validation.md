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
| Nested model extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/bean_validation.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/java_edge_carrier_reverify_test.go` | #3605 (epic #3584): nested @Valid field schema entities emit live AND the VALIDATES edge (owner DTO -> nested type) now materialises — patternResultToRecords synthesises the owning-DTO carrier (SCOPE.Class) for its SourceRef (scope:class:bean_validation:...) instead of dropping it. TestReverifyBeanValidationNestedValidCarrier asserts the VALIDATES OrderRequest->Address (field=shippingAddress, via=valid_annotation) edge live. |
| Schema extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/bean_validation.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Parameter-level Bean Validation constraints (@NotNull, @NotBlank, @Size, @Min, @Max, @Email, @Pattern) are captured in the Annotations slice on each handler parameter and drive the Required flag. Field-level recursion into nested DTO classes is not implemented (partial scope). Proven by TestBeanValidation_SchemaExtraction_Issue3002 and TestBeanValidation_MultipleConstraints_Issue3002. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-12` | 4872 | `internal/engine/java_annotation_params.go`<br>`internal/extractors/java/field_validations.go`<br>`internal/extractors/java/field_validations_test.go` | Bean-Validation annotations (@NotNull/@NotBlank/@NotEmpty/@Size/@Min/@Max/@Pattern/@Email) are collected on each handler parameter and drive the Required flag. #4872 also routes FIELD-level constraints on DTO/record/class fields into the unified Properties["validations"] chip list that the dashboard ShapeTree renders (parity with TS class-validator #4858 and Python #4871): javax.validation.constraints.* AND jakarta.validation.constraints.* are handled. @NotNull->Required, @NotEmpty/@NotBlank, @Size(min,max)->Size:min..max / MaxLength: / MinLength:, @Min/@Max/@DecimalMin/@DecimalMax->Min:/Max: (scalar bounds folded), @Pattern->Pattern, @Email, @Positive/@Negative(+OrZero), @Digits, @Past/@Future(+OrPresent), @AssertTrue/@AssertFalse, @Valid. Value-asserted by TestJava_BeanValidation_ClassDTO_jakarta, TestJava_BeanValidation_javaxAndDecimal, TestJava_BeanValidation_RecordComponents. |
| Custom validator extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/bean_validation.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Extracting classes that implement ConstraintValidator<A,T> requires scanning for the interface-implementation pattern. No current extractor does this. Leave red — out of scope for #3002. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | — `not_applicable` | `2026-05-29` | 3100 | `internal/custom/java/bean_validation.go` | Java Bean Validation does not coerce types; type coercion is handled by JAX-RS ParamConverter / Spring Converter<S,T>. No extractor covers this pattern. Leave red — out of scope for #3002. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/junit5.go` | Bean Validation integration tests use JUnit 5 with jakarta.validation.Validator. The junit5 extractor (internal/custom/java/junit5.go) captures @Test methods for the junit5 framework tag. Tests for bean-validation handlers are linked via the same JUnit 5 test-method extraction path used by other Java frameworks (e.g. Jakarta EE, Spring Boot — see #2996, #2991). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.validation.bean-validation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
