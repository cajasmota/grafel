<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.validation.joi` — Joi (@hapi/joi)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Scalar fields only; nested Joi.object()/Joi.array().items(...) not expanded into a nested tree. |
| Schema extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | custom_js_validation_schema parses 'const X = Joi.object({...})' into a SCOPE.Schema with per-field name+type, and binds it to the route via ACCEPTS_INPUT. Proven by TestJoiSchema_FieldsAndAcceptsInput. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-12` | 4976 | `internal/custom/javascript/issue4976_chain_constraints_test.go`<br>`internal/custom/javascript/validation_schema.go` | parseChainConstraints folds Joi field-chain bounds into the per-field Properties["validations"] chip list (class-validator chip format, #4858): .min/.max → MinLength/MaxLength (string) or Min/Max (numeric) with the bound, .required/.email/.uuid/.uri/.pattern/.integer → Required/Email/UUID/Url/Pattern/Int; joi fields default optional unless .required(). Proven by TestJoiChainConstraints_StampedAsChips. |
| Custom validator extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Joi .custom()/.external() validators not modeled as entities. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | Joi convert:true coercion option not recognized. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-12` | 4925 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | validation_linkage emits VALIDATES edge from handler to validator:joi on schema.validate()/Joi.attempt(). Proven by TestValidationLinkage_JoiValidate, TestValidationLinkage_JoiAttempt. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.validation.joi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
