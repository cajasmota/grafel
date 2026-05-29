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
| Nested model extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/extractors/python/extractor.go`<br>`internal/extractors/python/extractor_test.go` | @attr.s/@define classes surface only as generic Python classes: PEP 526 annotated attributes are emitted as SCOPE.Schema/field by extractClassFields. No attrs-specific validator=/converter= recognition. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Custom validator extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.validation.attrs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
