<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.scalapb-grpc` — ScalaPB / zio-grpc / fs2-grpc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 26

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Procedure extraction | ✅ `full` | `2026-05-31` | — | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: each def <rpc>(request: ReqT): Eff[RespT] of a ScalaPB AbstractService / zio-grpc ZGeneratedService / fs2-grpc *Fs2Grpc service trait -> SCOPE.Operation endpoint at /<Service>/<rpc>, verb=RPC, rpc_protocol=grpc, grpc_service+grpc_method+handler_name stamped. Value-asserting tests pin sayHello/listUsers + service Greeter (Z/Fs2Grpc decorations stripped). File-local. |
| Schema extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: request/response message types recovered from the method signature (Request<T> param + last effect type-arg of ZIO/Future/F[_]) and emitted as SCOPE.Schema DTO refs with grpc_message_role request/response. PARTIAL: message FIELD shapes live in ScalaPB-generated case-class companions; names only. Value-asserted (HelloRequest/HelloReply/UserList). |

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
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Request message type NAME recovered from the RPC param type; field shapes live in ScalaPB-generated message companions (not statically present). Value-asserted (HelloRequest). |
| Response shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Response message type NAME recovered as the last effect type-argument (Future/ZIO/F[_]); generated message field shapes not statically resolvable. Value-asserted (HelloReply). |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.scalapb-grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
