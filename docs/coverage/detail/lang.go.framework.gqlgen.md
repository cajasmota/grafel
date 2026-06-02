<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.gqlgen` — gqlgen (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 48

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml` | — |
| Handler attribution | ✅ `full` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml` | Resolver method on generated *queryResolver/*mutationResolver/*subscriptionResolver -> http:GRAPHQL:/graphql/<Root>/<field>; source_handler=SCOPE.Operation:<receiver>.<Method> rebinds to a HANDLES edge. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml`<br>`internal/extractors/graphql/graphql.go` | Operation endpoints synthesised from Go resolver receivers; SDL schema types parsed by the shared graphql extractor. Field-name mapping is gqlgen default lowerCamel and does not yet read gqlgen.yml overrides or @goField directives. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3613 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3613 | — | — |
| Request validation | 🔴 `missing` | — | 3613 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3613 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | ✅ `full` | `2026-06-02` | — | `internal/extractors/graphql/graphql.go`<br>`internal/extractors/graphql/type_graph.go` | gqlgen is SDL-driven: the schema (object types + object-typed fields) is declared in *.graphql files, so the SDL type→type graph pass (internal/extractors/graphql/type_graph.go, #3805) already emits the GRAPH_RELATES object-type→type edges with list/nullable cardinality between the SCOPE.Schema type nodes. gqlgen's generated Go resolvers carry no additional type refs (operation glue only), so no code-first Go extractor is required; the SDL pass is the source of truth for the gqlgen schema relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3613 | — | — |
| Interface extraction | 🔴 `missing` | — | 3613 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3613 | — | — |
| Type extraction | 🔴 `missing` | — | 3613 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3613 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3613 | — | — |
| Metric extraction | 🔴 `missing` | — | 3613 | — | — |
| Trace extraction | 🔴 `missing` | — | 3613 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3613 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3613 | — | — |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | 3613 | — | — |
| Dead code detection | 🔴 `missing` | — | 3613 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3613 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3613 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/golang/exception_flow.go`<br>`internal/extractors/golang/exception_flow_test.go` | return ErrX / fmt.Errorf %w -> THROWS; errors.Is/As -> CATCHES; named sentinels only (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | 3613 | — | — |
| HTTP effect | 🔴 `missing` | — | 3613 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3613 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3613 | — | — |
| Mutation effect | 🔴 `missing` | — | 3613 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3613 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3613 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3613 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3613 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3613 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3613 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3613 | — | — |
| Taint source detection | 🔴 `missing` | — | 3613 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3613 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3613 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.gqlgen ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
