<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.jsonrpc` — JSON-RPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | 🟢 `partial` | `2026-06-02` | 3750 | `internal/engine/http_endpoint_jsonrpc.go`<br>`internal/links/http_pass.go` | #3628 — client/server synthetics keyed http:JSONRPC:/jsonrpc/<method> join via the Name-based HTTP linker; round-trip covered in TestHTTPPass_JSONRPCCrossRepoMatch. JS jayson + Python ServerProxy clients; jayson method-map + register_function producers. Partial: dynamic method names honest-partial skipped |
| Method attribution | 🟢 `partial` | `2026-06-02` | 3750 | `internal/engine/http_endpoint_jsonrpc.go` | #3628 — literal method names from client.request('m') / ServerProxy.<m>() and jayson method-map / register_function keys; dynamic names skipped |
| Service extraction | 🔴 `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.jsonrpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
