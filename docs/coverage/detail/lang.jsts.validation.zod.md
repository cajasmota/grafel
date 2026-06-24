<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.validation.zod` — Zod

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | ✅ `full` | `2026-06-24` | 5496 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | expandNestedZodObjects recurses into nested z.object({...}) sub-schemas — descending through z.array(...), z.record(...), z.union([...]) branches and .optional()/.nullable()/.default()/.describe() chain wrappers — emitting each nested object as a child SCOPE.Schema (subtype=nested_schema) named with the dotted field path (e.g. Profile.address, Profile.address.city) carrying its own scalar fields + field members, linked to its parent via a CONTAINS(member=nested_schema) edge. Recursion capped at depth 8 to bound pathological nesting. Honest-partial: a field with no statically-locatable z.object() (dynamic/computed/$ref to a named schema) yields no nested child. Proven by TestZodSchema_NestedObjects (nested object, array-of-objects, optional nested object, union object branch), TestZodSchema_NestedObjects_Recursive, TestZodSchema_NoNesting_NoNestedEntities. |
| Schema extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | custom_js_validation_schema parses 'const X = z.object({...})' into a SCOPE.Schema entity carrying each scalar field name+type (field_<name> props, fields summary), binds it to the consuming route via ACCEPTS_INPUT/RETURNS, and #4845 expands each field into a SCOPE.Schema/field member. Proven by TestZodSchema_FieldsAndAcceptsInput, TestZodSchema_FieldMembers, TestZodSchema_ReturnsEdge, TestZodSchema_MiddlewareBinding. |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | ✅ `full` | `2026-06-12` | 4976 | `internal/custom/javascript/issue4976_chain_constraints_test.go`<br>`internal/custom/javascript/validation_schema.go` | parseChainConstraints folds zod field-chain bounds into the per-field Properties["validations"] chip list (same format as class-validator #4858): .min/.max → MinLength/MaxLength (string/array) or Min/Max (numeric) with the scalar bound, .int/.email/.uuid/.url/.regex/.positive/.negative → Int/Email/UUID/Url/Pattern/... and .optional()/.nullish() set the field optional. Proven by TestZodChainConstraints_StampedAsChips, TestSchemaField_NoChainConstraints_NoValidationsProp. |
| Custom validator extraction | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | zod .refine()/.superRefine()/.transform() custom validators are not modeled as entities. |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | 🔴 `missing` | — | 4925 | `internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | z.coerce.* coercion wrappers are not recognized as coercion flags. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-12` | 4925 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | validation_linkage emits a VALIDATES edge from the enclosing operation to validator:zod / dto:<schemaVar> when a handler body calls Schema.parse()/safeParse(); this links request validation to the route, not validator-to-test. Proven by TestValidationLinkage_ZodParse. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.validation.zod ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
