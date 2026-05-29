<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.validation.attrs` — attrs

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2985) | — | — |
| Schema extraction | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/attrs.go`<br>`internal/custom/python/extractors_test.go`<br>`internal/custom/python/testdata/attrs_validators.py` | @attr.s/@attrs.define/@define decorated classes emitted as SCOPE.Pattern attrs_class entities with decorator_form; tested in TestAttrs_ClassDecorator_AttrS, TestAttrs_ClassDecorator_Define, and TestAttrs_FullFixture. Nested attrs classes are referenced as attrib field types but no structural tree is emitted. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | 🟢 `partial` | `2026-05-29` | 3077 | `internal/custom/python/attrs.go`<br>`internal/custom/python/extractors_test.go`<br>`internal/custom/python/testdata/attrs_validators.py` | validators.instance_of(), in_(), and and_() on attrib()/field() validator= kwargs emit constraint_<field> SCOPE.Pattern entities; tested in TestAttrs_Constraint_InstanceOf, TestAttrs_Constraint_In, TestAttrs_Constraint_And, TestAttrs_Constraint_FullFixture. |
| Custom validator extraction | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/attrs.go`<br>`internal/custom/python/extractors_test.go`<br>`internal/custom/python/testdata/attrs_validators.py` | @<field>.validator decorator-style validators (TestAttrs_FieldValidator) and validator= kwarg (TestAttrs_ValidatorKwarg) both fully extracted as SCOPE.Pattern field_validator entities; TestAttrs_FullFixture exercises the fixture end-to-end. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ✅ `full` | `2026-05-29` | 3061 | `internal/custom/python/attrs.go`<br>`internal/custom/python/extractors_test.go`<br>`internal/custom/python/testdata/attrs_validators.py` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.attrs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
