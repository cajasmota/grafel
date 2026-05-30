<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.validation.validator` — validator

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/validator.go`<br>`internal/custom/rust/validator_test.go` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/validator.go`<br>`internal/custom/rust/validator_test.go` | — |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/validator.go`<br>`internal/custom/rust/validator_test.go` | — |
| Custom validator extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/validator.go`<br>`internal/custom/rust/validator_test.go` | — |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | — `not_applicable` | `2026-05-30` | — | — | validator does not coerce types; type coercion is serde's responsibility, covered by dto_extraction in fw_validation.go |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3545) | `internal/custom/rust/validator_test.go` | validator-specific fixtures assert exact constraint values; general rust test-linkage substrate provides edges |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.validation.validator ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
