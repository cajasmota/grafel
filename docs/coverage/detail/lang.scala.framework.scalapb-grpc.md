<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.scalapb-grpc` — ScalaPB / zio-grpc / fs2-grpc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 30

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | ✅ `full` | `2026-05-31` | — | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: each def <rpc>(request: ReqT): Eff[RespT] of a ScalaPB AbstractService / zio-grpc ZGeneratedService / fs2-grpc *Fs2Grpc service trait -> SCOPE.Operation endpoint at /<Service>/<rpc>, verb=RPC, rpc_protocol=grpc, grpc_service+grpc_method+handler_name stamped. Value-asserting tests pin sayHello/listUsers + service Greeter (Z/Fs2Grpc decorations stripped). File-local. |
| Schema extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: request/response message types recovered from the method signature (Request<T> param + last effect type-arg of ZIO/Future/F[_]) and emitted as SCOPE.Schema DTO refs with grpc_message_role request/response. PARTIAL: message FIELD shapes live in ScalaPB-generated case-class companions; names only. Value-asserted (HelloRequest/HelloReply/UserList). |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: <Service>Grpc.stub/blockingStub/bindService(Resource) site detected -> SCOPE.Component grpc_stub with companion+accessor. PARTIAL: generated stub/companion code itself is scalapbc-emitted, not statically present; we record the use site. Value-asserted (GreeterGrpc.stub). |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-31` | — | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: RPC endpoint synthesis /<Service>/<rpc> from the service-trait method set; service trait emitted as SCOPE.Service grpc_service. Value-asserting tests pin the path + grpc_service. Regex, file-local; no .proto/AST. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go, RegisterEffectSniffer('scala')) recognises Slick/Doobie/Quill/JPA read+write primitives; framework-agnostic, fires on any .scala file. Fixture: TestScalaTrailing_Grpc_Effects. |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability/dead-code pass over Scala entry points (entry_points_scala.go) + IMPORTS/CALLS edges; framework-agnostic, fires on any .scala file. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala'), def_use_scala.go) fires on any .scala file via LanguageForPath; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator def->use pairs. Fixture: TestScalaTrailing_Grpc_DefUse. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises Source.fromFile/Files/os-lib read+write primitives; framework-agnostic. |
| HTTP effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises outbound HTTP primitives (sttp/akka-pekko/http4s/requests); framework-agnostic. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by the Scala extractor pipeline; framework-agnostic. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises this.<field>= mutation; framework-agnostic. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | — | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags Scala functions with no effect properties; framework-agnostic (esp. apt for effectful/functional Scala idioms). |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability pass seeded from Scala entry points (entry_points_scala.go); framework-agnostic. |
| Request shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Request message type NAME recovered from the RPC param type; field shapes live in ScalaPB-generated message companions (not statically present). Value-asserted (HelloRequest). |
| Response shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Response message type NAME recovered as the last effect type-argument (Future/ZIO/F[_]); generated message field shapes not statically resolvable. Value-asserted (HelloReply). |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises parameterised-SQL/HTML-escape/Form-mapping sanitizers; framework-agnostic. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises SQL-splice/command/path/XSS/ReDoS sinks; framework-agnostic. Fixture: TestScalaTrailing_Grpc_Taint. |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go, RegisterTaintSniffer('scala')) recognises request/param/sys.env/decode sources; framework-agnostic, fires on any .scala file. |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint flow (taint_flow.go over taint_sites_scala.go) reports source->sink findings; framework-agnostic. Fixture: TestScalaTrailing_Grpc_Taint. |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.scalapb-grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
