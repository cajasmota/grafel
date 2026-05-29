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
| Nested model extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/engine/java_annotation_params.go`<br>`internal/engine/java_annotation_params_test.go` | @Valid (cascade/nested validation marker) is recognized and lifts the annotated parameter to required; no recursion into the nested type's own fields. |
| Schema extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/engine/java_annotation_params.go`<br>`internal/engine/java_annotation_params_test.go` | Bean-Validation annotations (@NotNull/@NotBlank/@NotEmpty/@Size/@Min/@Max/@Pattern/@Email) are collected on each handler parameter and drive the Required flag; captured as annotation strings, not structured constraint records (no value bounds parsed). |
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
(or use `go run ./tools/coverage update lang.java.validation.bean-validation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
