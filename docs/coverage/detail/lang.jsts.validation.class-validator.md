<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.validation.class-validator` — class-validator (NestJS DTOs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🟢 `partial` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | @ValidateNested()/@Type(() => Child) nested DTO references are recognized at the field level but not expanded into a structured nested-schema tree (validation_schema.go:458 notes @Type targets are scalar-coercion, not nesting). |
| Schema extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/javascript/reqresp_dto_test.go`<br>`internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go`<br>`internal/extractors/javascript/issue4845_dto_field_membership_test.go` | A class decorated with class-validator decorators is recognized as a validation schema (library=class-validator) and emitted as a SCOPE.Schema; #4845 expands each property into a SCOPE.Schema/field member. Proven by TestClassValidator_FieldsFromDecorators, TestClassValidatorDTO_FieldMembers; TestClassValidator_PlainClassSkipped guards against treating an undecorated class as a schema. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-12` | — | `internal/extractors/javascript/class_validator_fields.go`<br>`internal/extractors/javascript/issue4858_field_validations_test.go` | #4858: each DTO SCOPE.Schema/field carries its class-validator decorators as a terse Properties["validations"] chip list (e.g. "IsString,MinLength:2,MaxLength:120,IsOptional"); single-scalar-bound decorators (MinLength/MaxLength/Min/Max/...) fold their value into the chip. ~60 recognised decorators across common/type/string/number/date families; unrelated decorators (@ApiProperty/@Transform) ignored. Dashboard ShapeTree renders these as per-field chips. Proven by TestDTO_ClassValidatorConstraintsStampedOnField. |
| Custom validator extraction | 🔴 `missing` | — | 4925 | `internal/extractors/javascript/class_validator_fields.go`<br>`internal/extractors/javascript/issue4858_field_validations_test.go` | @Validate(CustomConstraint) / @ValidatorConstraint custom validator classes are not modeled as distinct entities; only the membership-recognised decorator chips are captured. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | @Type(() => Number)/class-transformer coercion and ValidationPipe({transform:true}) are not modeled as coercion flags. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-12` | 4925 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | validation_linkage emits a VALIDATES edge from a NestJS controller method to dto:<TypeName> on @Body()/@Query()/@Param() DTO params, and to validator:class-validator on validate()/validateOrReject() call sites. Proven by TestValidationLinkage_ClassValidator, TestValidationLinkage_NestDTO. This is route-to-DTO linkage, not validator-to-test. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.validation.class-validator ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
