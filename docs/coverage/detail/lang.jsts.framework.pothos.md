<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.pothos` — Pothos (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 46

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | — |
| Handler attribution | — `not_applicable` | — | — | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | Pothos root fields are inline-arrow resolvers (builder.queryField('users', t => ...)) with no addressable handler symbol; the operation endpoint is emitted with NO source_handler (NoHandlerProp keep-path), matching the Apollo resolver-map convention. There is no method symbol to bind a HANDLES edge to. |
| Route extraction | 🟢 `partial` | — | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | builder.queryField/mutationField/subscriptionField + the builder.queryType/mutationType/subscriptionType fields:(t)=>({...}) maps -> http:GRAPHQL:/graphql/<Root>/<field> (EXACT canonical shape as gqlgen/Apollo/Strawberry so client links #3667 join). Honest-partial: a non-literal (variable/computed) field name is not recovered. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3619 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3619 | — | — |
| Request validation | 🔴 `missing` | — | 3619 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3619 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3619 | — | — |
| Interface extraction | 🔴 `missing` | — | 3619 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3619 | — | — |
| Type extraction | 🔴 `missing` | — | 3619 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3619 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3619 | — | — |
| Metric extraction | 🔴 `missing` | — | 3619 | — | — |
| Trace extraction | 🔴 `missing` | — | 3619 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3619 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3619 | — | — |
| Config consumption | 🔴 `missing` | — | 3619 | — | — |
| Constant propagation | 🔴 `missing` | — | 3619 | — | — |
| Dead code detection | 🔴 `missing` | — | 3619 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3619 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3619 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | 3619 | — | — |
| HTTP effect | 🔴 `missing` | — | 3619 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3619 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3619 | — | — |
| Mutation effect | 🔴 `missing` | — | 3619 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3619 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3619 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3619 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3619 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3619 | — | — |
| Taint source detection | 🔴 `missing` | — | 3619 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3619 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3619 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.pothos ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
