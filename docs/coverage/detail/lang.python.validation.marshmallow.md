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
| Nested model extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | — |
| Schema extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py`<br>`internal/patterns/schema_detector.go` | marshmallow Schemas surface only as generic Python classes: class + class-attribute fields (e.g. name = fields.Str()) are emitted as SCOPE.Schema/field by extractClassFields. No marshmallow-specific field-type or validate= recognition. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2985) | — | — |
| Custom validator extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | — |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/marshmallow.go`<br>`internal/custom/python/testdata/marshmallow_nested.py` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.marshmallow ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
