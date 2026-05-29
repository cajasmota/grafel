<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.validation.marshmallow` — marshmallow

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | fields.Nested(OtherSchema) and fields.Nested("OtherSchema", many=True) emitted as SCOPE.Pattern nested_field entities with nested_schema name; tested in TestMarshmallow_NestedField (2 variants) and TestMarshmallow_FullFixture (nested_address + nested_orders). |
| Schema extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py`<br>`internal/patterns/schema_detector.go` | marshmallow Schemas surface only as generic Python classes: class + class-attribute fields (e.g. name = fields.Str()) are emitted as SCOPE.Schema/field by extractClassFields. No marshmallow-specific field-type or validate= recognition. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | 🟢 `partial` | `2026-05-29` | 3077 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | validate.Range(min,max), validate.Length(min,max), validate.OneOf([...]) on field declarations emit constraint_<field> SCOPE.Pattern entities with constraint_validator/constraint_min/constraint_max/constraint_choices props; tested in TestMarshmallow_Constraint_Range, TestMarshmallow_Constraint_Length, TestMarshmallow_Constraint_OneOf, TestMarshmallow_Constraint_FullFixture. |
| Custom validator extraction | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | @validates('field') emits SCOPE.Pattern field_validator entities; @validates_schema emits schema_validator entities. Both decorator forms tested in TestMarshmallow_ValidatesDecorator, TestMarshmallow_ValidatesSchema, and TestMarshmallow_FullFixture. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | @post_load / @pre_load hooks emitted as SCOPE.Pattern coercion_hook entities; per-field load_default/missing/data_key kwarg detection included. Both forms tested in TestMarshmallow_PostLoad and TestMarshmallow_FullFixture. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.marshmallow ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
