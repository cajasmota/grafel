<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.soap` — SOAP / WSDL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | 🟢 `partial` | `2026-06-02` | 3750 | `internal/engine/http_endpoint_soap.go`<br>`internal/links/http_pass.go` | #3628 — client/server synthetics keyed http:SOAP:/soap/<Service>/<Op> (+ service-less alias) join via the Name-based HTTP linker; round-trip covered in TestHTTPPass_SOAPCrossRepoMatch. Python zeep + JS node-soap + Java JAX-WS-port clients; JAX-WS @WebService/@WebMethod producers. Partial: client-side service binding is service-less honest-partial; dynamic op names skipped |
| Method attribution | 🟢 `partial` | `2026-06-02` | 3750 | `internal/engine/http_endpoint_soap.go` | #3628 — operation names from zeep client.service.<Op>(), node-soap client.<Op>Async(), JAX-WS port.<op>(), and @WebMethod(operationName=) producers; dynamic op names skipped |
| Service extraction | 🟢 `partial` | `2026-06-02` | 3750 | `internal/engine/http_endpoint_soap.go` | #3628 — JAX-WS @WebService serviceName/name attribute (else class name) on the producer side; client side is service-less honest-partial |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.soap ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
