<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.tonic` — Tonic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | RPC endpoints synthesized per async method; .add_service(<Svc>Server::new) captured as SCOPE.Service registration |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | handler_name=<ImplType>.<method> attributed per RPC method |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | #[tonic::async_trait] impl <Service> for <Type> RPC methods become RPC endpoints at /<Service>/<Method>; verb=RPC, rpc_protocol=grpc |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | Request<T>/Response<T> message types emitted as SCOPE.Schema DTOs with grpc_message_role request/response |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (internal/extractors/rust/rust.go, registered unconditionally as "rust" with no framework gating) emits enum_item -> SCOPE.Component subtype="enum" with variants/generics/derives props for every .rs file. Value-asserting probe TestRustExtractor_TypeSystem_PerFramework drives a tonic-style file through the extractor and asserts the enum entity fires. |
| Interface extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Service trait NAME recovered from impl <Service> for <Type>; the trait itself is tonic-build-generated and not statically present |
| Type alias extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (rust.go, unconditional per-language) emits type_item -> SCOPE.Component subtype="type_alias" with aliased_type/generics props for every .rs file. Probe TestRustExtractor_TypeSystem_PerFramework asserts the type_alias entity + its aliased_type prop on a tonic-style file. |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | gRPC message type names recovered from Request<T>/Response<T> wrappers |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic Rust observability scanner (internal/custom/rust/observability.go) recognises tracing/log/slog macros + #[instrument] on any .rs file; the #3981 import marker now attributes tonic files to this cell. Probe TestRustObs_FrameworkAttribution_TonicAsyncGraphql asserts a tonic file emits a tracing log entity with framework="tonic". Stays partial-equivalent for message binding per the scanner's documented log honesty note, but detection + attribution fire. |
| Metric extraction | ✅ `full` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic observability scanner (observability.go) captures metric NAMEs (metrics!/prometheus/otel meter) at the call site on any .rs file; the #3981 tonic import marker attributes them to this cell. The same value-asserting metric-name machinery proven for axum applies — tonic services that emit these metric macros are now credited. |
| Trace extraction | ✅ `full` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic observability scanner (observability.go) captures span NAMEs (span!/info_span!/otel tracer + #[instrument]) at the call site on any .rs file; the #3981 tonic import marker attributes them to this cell. Probe TestRustObs_FrameworkAttribution_TonicAsyncGraphql asserts a tonic file emits a span entity with framework="tonic". |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Request<T> message type NAME recovered; field shapes live in tonic-build-generated structs (build.rs OUT_DIR), not statically present in source |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Response<T> message type NAME recovered; generated message field shapes not statically resolvable |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.tonic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
