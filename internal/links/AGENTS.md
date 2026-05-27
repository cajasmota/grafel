# Cross-repo + cross-language linkers — agent guide

The linker layer turns synthesised endpoints, topics, and gRPC services into cross-repo / cross-language edges. Each strategy is an independent pass over candidate pairs with a confidence score.

## Conventions

- **One pass per transport.** `http_pass.go`, `grpc_pass.go`, `topic_pass.go`, `openapi_pass.go`, `import_pass.go`, `sameas_pass.go`, `label_pass.go`, `string_pass.go`. Keep new strategies in their own file with a focused `_test.go`.
- **Candidates → confidence → resolution.** `candidates.go` builds pair sets; `confidence.go` scores them; the pass emits edges only above the confidence threshold. Don't bypass the scoring layer.
- **Phantom edges** (`phantom_edges.go`) flag dangling references when no producer/consumer match is found. Use them rather than silently dropping unresolved links.
- **Telemetry.** Passes record per-strategy hit counts; the `live_fixture_test.go` + `scale_stress_test.go` gates assert no regression. New strategies should add their counter to telemetry.

## When to add a new resolve strategy

- A new transport / protocol that doesn't fit `http`, `grpc`, `topic`, `openapi`, `import`, `sameas`, `label`, `string`.
- A materially different matching heuristic (e.g. URL templates, OpenAPI `operationId`, content-type negotiation). Prefer extending an existing pass via a sub-strategy if the transport is the same.

## Coverage matrix update

A new resolve strategy often expands what the matrix can claim about a framework. After landing, check whether any `lang.<lang>.framework.<name>` or `runtime.<...>` records gain cites — if so, update `docs/coverage/registry.json` per the root `AGENTS.md` "Coverage matrix update" workflow.

Examples:
- A new gRPC bidi-stream resolver may upgrade `runtime.realtime.grpc` from partial → full.
- An OpenAPI-driven cross-repo linker may add cites to many framework records simultaneously.

## Related

- Producers of the entities linked here: `internal/engine/AGENTS.md`
- Per-language candidate emitters: `internal/extractors/AGENTS.md`
