<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `api-spec.openapi-ingestion` — OpenAPI 3.x / Swagger 2.0 spec ingestion (endpoint ground-truth)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/openapi_synthesis.go` | Spec endpoints CONVERGE on (do not duplicate) code-extracted routes and HTTP clients: they share the identical canonical synthetic ID http:<VERB>:<path> emitted by httproutes.SyntheticID, so entity-merge collapses a spec endpoint and its code-extracted twin into one node — making the spec a parity oracle. provenance=spec distinguishes spec-only endpoints (declared but not found in code) from code-extracted ones. Test TestOpenAPISynthesis_ConvergesWithCodeRoute asserts the spec GET /users/{id} ID equals httproutes.SyntheticID(GET, Canonicalize(FastAPI, /users/{id})). |
| Service extraction | ✅ `full` | `2026-06-02` | — | `internal/classifier/classifier.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/openapi_synthesis.go`<br>`internal/engine/openapi_synthesis_test.go` | #3628 area #16: OpenAPI 3.x / Swagger 2.0 spec files (openapi.{yaml,json}/swagger.{json,yaml}, sniffed by openapi:/swagger: + paths:) are ingested by synthesizeOpenAPI as a yaml/json case of applyHTTPEndpointSynthesis. Each paths.<path>.<method> becomes a canonical http_endpoint_definition whose synthetic ID http:<VERB>:<canonical-path> is built via httproutes.Canonicalize(FastAPI) — the SAME shape as code-extracted routes — carrying operation_id, summary, request_schema/response_schemas DTO refs, source=openapi_spec, provenance=spec. Value-asserting tests: paths./users/{id}.get{operationId:getUser} -> http:GET:/users/{id}; POST requestBody $ref -> request_schema=CreateUser; Swagger2 definitions form; negative non-spec YAML emits zero endpoints. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update api-spec.openapi-ingestion ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
