<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.validation.pydantic` — Pydantic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🟢 `partial` | `2026-05-29` | — | `internal/extractors/python/discriminator.go`<br>`internal/extractors/python/nested_ctor_refs.go` | Discriminated-union tags captured via DISCRIMINATES_ON (discriminator.go); nested model construction referenced via nested_ctor_refs. No structured nested-schema tree. |
| Schema extraction | ✅ `full` | `2026-08-23` | — | `internal/custom/python/dto_field_members.go`<br>`internal/extractors/python/extractor.go`<br>`internal/patterns/patterns_test.go`<br>`internal/patterns/schema_detector.go` | Base Python extractor emits the model class (SCOPE.Component) and its annotated fields (SCOPE.Schema/field) via extractClassFields; schema_detector.go classifies the BaseModel subclass as a pydantic schema_validation entity, exercised by TestSchemaDetector_PydanticBaseModel. #6561: each field member also carries default_value, the default expression verbatim, when that expression is a single literal (a quoted string with no escapes, a plain int or float, True/False/None) — either as the whole right-hand side, or as the default= argument of one Field(...) call named bare or dotted. Everything else publishes NO property at all rather than a partial read: a call, an f-string, a container display, arithmetic, Field(...) Ellipsis, a Field() whose default= is only default_factory=, two Field() calls in one expression, and numeric spellings outside the literal pattern (1_000, 1.5e3). A POSITIONAL default, Field("x"), is likewise absent rather than read. Comments and string arguments are both blanked before the scan (pyStripComments, pyBlankStringLiterals), so prose that names a default — a trailing # default=5, or a description=/alias= string saying "default=5, historical" — never becomes a published value. Tests: TestPydantic_FieldDefaultValue_6561, TestDRF_FieldMembersCarryNoDefaultValue_6561. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-11` | — | `internal/custom/python/pydantic.go`<br>`internal/dashboard/shape_tree.go`<br>`internal/extractors/python/field_validations.go`<br>`internal/extractors/python/field_validations_test.go` | python_pydantic extractor parses Field(gt=, ge=, lt=, le=, min_length=, max_length=, min_items=, max_items=, multiple_of=, max_digits=, decimal_places=, pattern=, regex=) into a SCOPE.Pattern entity per field with constraint_* properties; constraint-free Field(default=...) is skipped (#2984). #4871: each per-field SCOPE.Schema/field entity now ALSO carries its constraints as a terse Properties["validations"] list (e.g. "Required,MaxLength:120,MinLength:2"; scalar bounds gt/ge/lt/le/max_length/... fold the value into the chip, pattern/regex render a bare Pattern marker), covering Field(), Annotated[T, Field(...)], conint/constr/confloat/condecimal constrained types, Optional[X]/X|None (Optional marker) and @field_validator/@validator presence (Validated marker) — which the dashboard ShapeTree renders as per-field constraint chips. Tests: TestPythonFieldValidations_Pydantic, TestShape_FieldValidationsChips. |
| Custom validator extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/pydantic.go`<br>`internal/custom/python/testdata/pydantic_validators.py` | python_pydantic extractor emits SCOPE.Pattern entities for @field_validator / @validator (v1) and @model_validator / @root_validator (v1), capturing target fields, mode (before/after, pre=True), validator fn, and dialect. Issue #2984. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/pydantic.go`<br>`internal/custom/python/testdata/pydantic_validators.py` | model_config = ConfigDict(...) (v2) and inner class Config (v1) coercion flags recognized as SCOPE.Pattern model_config entities with coercion_flags property; tested in TestPydantic_ModelConfig, TestPydantic_V1ConfigClass, and TestPydantic_Fixture. Per-field annotation-driven coercion (int/str/datetime) is not modeled — structural model-level coercion is fully extracted. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/python/pytest.go` | python_pytest extracts pytest test functions/classes/fixtures; tests exercising Pydantic models are captured as test entities but no validator-to-test linkage edge is emitted. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.pydantic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
