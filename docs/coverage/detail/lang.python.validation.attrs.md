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
| Nested model extraction | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2985) | — | — |
| Schema extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/python/attrs.go`<br>`internal/custom/python/testdata/attrs_validators.py` | @attr.s/@define classes surface only as generic Python classes: PEP 526 annotated attributes are emitted as SCOPE.Schema/field by extractClassFields. No attrs-specific validator=/converter= recognition. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2985) | — | — |
| Custom validator extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/attrs.go`<br>`internal/custom/python/testdata/attrs_validators.py` | — |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/attrs.go`<br>`internal/custom/python/testdata/attrs_validators.py` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.attrs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
