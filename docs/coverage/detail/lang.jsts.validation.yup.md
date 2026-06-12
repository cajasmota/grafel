<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.validation.yup` — Yup

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Scalar fields only; nested yup.object()/yup.array().of(...) not expanded. |
| Schema extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | custom_js_validation_schema parses 'const X = yup.object().shape({...})' / yup.object({...}) into a SCOPE.Schema with per-field name+type, bound to route via ACCEPTS_INPUT. Proven by TestYupSchema_ShapeFieldsAndAcceptsInput. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Yup chained constraints (.min/.max/.required/.email) not folded into per-field chips. Shares the chain-chip follow-up. |
| Custom validator extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Yup .test() custom validators not modeled. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Yup .cast()/coercion not recognized. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-12` | 4925 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | validation_linkage emits VALIDATES edge from handler to validator:yup on schema.validate(). Same call-site mechanism as joi. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.validation.yup ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
