<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.grpc` — elixir-grpc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc :Method, Req, Resp declarations in use GRPC.Service modules emitted as SCOPE.GrpcMethod (grpc:<service>/<method>) with method + request/response message names. |
| Schema extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | Request/response protobuf message names captured per rpc; stream() wrappers stripped and classified into streaming mode. |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | use GRPC.Server / GRPC.Service / GRPC.Stub modules emitted as SCOPE.GrpcService with grpc_role server|definition|client; service name resolved from name:/service: option. |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; gRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; gRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to gRPC, which signals outcome via GRPC.RPCError on the trailer, not HTTP 2xx/4xx codes. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis (path+verb producer endpoints) does not apply to gRPC; service registration is captured as transport_binding. gRPC method addressing is package.Service/Method, not an HTTP path+verb. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for gRPC. RPC-method->service binding is modelled by procedure_extraction (rpc/service) + the service-impl in schema_extraction, not by an HTTP route table. |
| Route extraction | — `not_applicable` | — | — | — | gRPC has no HTTP route paths; the HTTP route extractor emits 0 entities on a gRPC service impl. RPC method addressing is package.Service/Method, surfaced via procedure_extraction + transport_binding. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | gRPC services render no server-side views/templates; responses are protobuf messages (tRPC returns plain typed values), not rendered views. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4038:auth-rpc-gap | — | The elixir auth_coverage sniffer is HTTP-route/endpoint-keyed (it scans synthesized HTTP endpoints / router pipelines / framework guards), not framework-agnostic like csharp's [Authorize] attribute. VERIFIED: it emits 0 auth entities on a gRPC service idiom (GRPC.Server callbacks / protobuf structs / GRPC.RPCError / GRPC.Server.Stream). gRPC auth (call-credentials / metadata interceptors) is a real, untriaged gap — honest missing, not n/a. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | gRPC request/response payloads are protobuf messages (tRPC: zod-typed inputs), surfaced under schema_extraction; MVC DTO inference does not model them. HTTP MVC DTO extractor yields 0 entities on a gRPC service. |
| Request validation | — `not_applicable` | — | — | — | gRPC has no MVC request-validation pipeline; message-field constraints live in proto (tRPC validates via the zod input schema, surfaced as schema, not an MVC validator). HTTP MVC validation extractor yields 0 entities here. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | gRPC cross-cutting concerns use interceptors (tRPC: middleware links), not the HTTP framework's Use*/plug/filter request-pipeline. The HTTP middleware extractor yields 0 entities on a gRPC service; interceptor cataloguing is potential future work. |
| Rate limit stamping | — `not_applicable` | — | — | — | HTTP endpoint rate-limit/throttle stamping is an HTTP-middleware concept; gRPC throttling is done via interceptors/server options, not HTTP rate limiters. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/elixir/typespec.go` | language-level type-system sniffer is framework-agnostic; fires on enums in gRPC service/contract code. VERIFIED on the gRPC idiom. |
| Interface extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/elixir/typespec.go` | framework-agnostic; fires on the gRPC service impl/contract interface. VERIFIED on the gRPC idiom. |
| Type alias extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/elixir/typespec.go` | framework-agnostic; fires on type aliases in gRPC code. VERIFIED on the gRPC idiom. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/elixir/typespec.go` | framework-agnostic; fires on every gRPC service-impl class / payload struct / case class. VERIFIED on the gRPC idiom. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3963 | — | — |
| DI injection point | 🔴 `missing` | — | 3963 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3963 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks.go` | test-framework sniffer keys on the standard test macros, not the framework-under-test; links gRPC service/router tests like any other test. VERIFIED a named test case is extracted from a gRPC test idiom. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | — | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | logging sniffer is library/call-keyed (framework-agnostic); fires on a gRPC service that logs. VERIFIED on the gRPC idiom. PARTIAL: heuristic. |
| Metric extraction | 🟢 `partial` | — | — | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | metric sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument metrics. VERIFIED on the gRPC idiom. PARTIAL: heuristic. |
| Trace extraction | 🟢 `partial` | — | — | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | trace sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument traces. PARTIAL: heuristic. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_elixir.go` | Elixir def-use sniffer registered; intra-procedural def-use chains over .ex/.exs |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | Cross-repo identity grpc:<service>/<method> matches the shared #725 linker convention so client stub and server impl join across repos. |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc request message type recorded as request_message on each SCOPE.GrpcMethod. |
| Request sink dataflow | — `not_applicable` | — | — | — | The request->sink dataflow sniffer roots taint at HTTP MVC binders / request accessors. gRPC methods take a strongly-typed protobuf request (tRPC: a zod-typed input) + call context, with no HTTP MVC request-root to seed; not applicable to this transport. |
| Response shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc response message type recorded as response_message on each SCOPE.GrpcMethod. |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_elixir.go` | Elixir template-pattern sniffer registered: i18n (gettext/dgettext), log_format (Logger.*), SQL literals via Ecto.Adapters.SQL |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
