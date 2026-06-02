<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.ariadne` — Ariadne GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 48

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | schema-first Ariadne: QueryType()/MutationType()/SubscriptionType()/ObjectType("Query") binders + @<binder>.field("<name>") decorator resolvers -> http:GRAPHQL:/graphql/<Root>/<field>, identical shape to Strawberry. synthesizeAriadne. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | decorated resolver function is the handler; source_handler=SCOPE.Operation:<funcName> rebinds to a HANDLES edge. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Binder var -> root type resolved from QueryType/MutationType/SubscriptionType ctor or ObjectType("<Type>") arg; field name is the literal decorator string. Dynamically-named fields and set_field()/schema-directive bindings not resolved (honest-partial). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3620 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3620 | — | — |
| Request validation | 🔴 `missing` | — | 3620 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3620 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3620 | — | — |
| Interface extraction | 🔴 `missing` | — | 3620 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3620 | — | — |
| Type extraction | 🔴 `missing` | — | 3620 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3620 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3620 | — | — |
| Metric extraction | 🔴 `missing` | — | 3620 | — | — |
| Trace extraction | 🔴 `missing` | — | 3620 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3620 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3620 | — | — |
| Config consumption | 🔴 `missing` | — | 3620 | — | — |
| Constant propagation | 🔴 `missing` | — | 3620 | — | — |
| Dead code detection | 🔴 `missing` | — | 3620 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3620 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3620 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | 3620 | — | — |
| HTTP effect | 🔴 `missing` | — | 3620 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3620 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3620 | — | — |
| Mutation effect | 🔴 `missing` | — | 3620 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3620 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3620 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3620 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3620 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3620 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3620 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3620 | — | — |
| Taint source detection | 🔴 `missing` | — | 3620 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3620 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3620 | — | — |

## Framework-specific

### DataLoader (N+1 batching)

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dataloader extraction | 🟢 `partial` | `2026-06-02` | 3624 | `internal/custom/python/graphql_dataloader.go`<br>`internal/custom/python/graphql_dataloader_test.go`<br>`internal/types/kinds.go` | aiodataloader DataLoader(load_fn=batch_users) / DataLoader(batch_users) -> SCOPE.DataLoader entity named by the assigned var + BATCHES edge to the named batch fn; <loader>.load(id)/.load_many(ids) in a resolver body -> USES edge resolver->loader (resolver = nearest enclosing def), via=graphql_dataloader. Value-asserted: user_loader BATCHES batch_users + author resolver USES user_loader. PARTIAL (honest): regex+enclosing-def heuristic; lambda batch fns get no BATCHES edge; top-level .load() with no enclosing def skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.ariadne ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
