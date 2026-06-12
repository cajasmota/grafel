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
| Nested model extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/2985) | `internal/custom/python/attrs.go` | attrsClassDecoratorRe detects @attr.s/@attrs.define classes; nested attrs detected via field type references (#3182) |
| Schema extraction | ✅ `full` | `2026-06-11` | 4714 | `internal/custom/python/attrs.go`<br>`internal/custom/python/dto_field_members.go`<br>`internal/custom/python/dto_field_members_test.go`<br>`internal/custom/python/extractors_test.go`<br>`internal/custom/python/testdata/attrs_validators.py` | @attr.s/@attrs.define/@define classes emitted as SCOPE.Pattern attrs_class entities (decorator_form). #4714: each @dataclass / @attr.s / @define class's annotated attribute (name: type = field(...)/attr.ib(...)) is ALSO emitted as a SCOPE.Schema/field member (field_name/field_type/parent_class/optional/validators + parseable Signature) with a CONTAINS edge to the class, the SAME uniform shape as the Pydantic/DRF (#4613) and JS (#4635) DTO field members. extractAttrsDataclassFields + emitPyDTOFieldMembers; optional from any default/Optional[...], field()/attr.ib() with no default+not Optional stays required. Value-asserted by TestDataclass_FieldMembers + TestAttrs_FieldMembers (type, optional, CONTAINS). Nested attrs classes referenced via attrib types only. |

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
