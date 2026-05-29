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
| Nested model extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/extractors/python/discriminator.go`<br>`internal/extractors/python/nested_ctor_refs.go` | Discriminated-union tags captured via DISCRIMINATES_ON (discriminator.go); nested model construction referenced via nested_ctor_refs. No structured nested-schema tree. |
| Schema extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/extractors/python/extractor.go`<br>`internal/extractors/python/extractor_test.go` | Base Python extractor emits the model class (SCOPE.Component) and its annotated fields (SCOPE.Schema/field) via extractClassFields; no Pydantic-specific Field()/constraint parsing. |

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
(or use `go run ./tools/coverage update lang.python.validation.pydantic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
