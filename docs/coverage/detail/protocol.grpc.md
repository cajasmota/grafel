<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.grpc` — gRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | ✅ `full` | `2026-06-02` | 3686 | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_edges_test.go`<br>`internal/links/grpc_pass.go` | Client stub calls emit SCOPE.GrpcMethod entities keyed grpc:<Service>/<Method> + GRPC_HANDLES edges matching the server identity; P6 (links/grpc_pass.go) joins client↔server across repos. Node/TS client coverage now includes the modern factory-function stubs nice-grpc (createClient(<Service>Definition, ch)) and Connect/connectrpc (createPromiseClient(<Service>Service, transport)) alongside classic @grpc/grpc-js constructor stubs. |
| Method attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_edges_test.go` | — |
| Service extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/grpc_edges.go` | — |

## Related extraction records

This hub record tracks the technology at a high level. The deep, code-level
coverage for this technology lives in the per-language records below — each
one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.c-cpp.framework.grpc`](./lang.c-cpp.framework.grpc.md) | C/C++ | framework | 7 full, 12 partial, 21 missing, 14 n/a |
| [`lang.csharp.framework.grpc-net`](./lang.csharp.framework.grpc-net.md) | C# | framework | 11 full, 26 partial, 2 missing, 15 n/a |
| [`lang.elixir.framework.grpc`](./lang.elixir.framework.grpc.md) | elixir | framework | 2 full, 20 partial, 18 missing, 14 n/a |
| [`lang.go.framework.grpc`](./lang.go.framework.grpc.md) | go | framework | 1 full, 3 partial, 37 missing, 13 n/a |
| [`lang.rust.framework.tonic`](./lang.rust.framework.tonic.md) | rust | framework | 9 full, 4 partial, 35 missing, 1 n/a |
| [`lang.scala.framework.scalapb-grpc`](./lang.scala.framework.scalapb-grpc.md) | scala | framework | 9 full, 18 partial, 10 missing, 17 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
