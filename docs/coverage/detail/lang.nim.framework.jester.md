<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.framework.jester` — Jester / Prologue (Nim HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint response codes | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_jester.go`<br>`internal/engine/httproutes/canonicalize.go` | Jester routes:-block verb entries and Prologue app.get/addRoute registrations synthesized to canonical http_endpoint_definition via synthesizeJester/synthesizePrologue (#4749) with @id->{id} (FrameworkJester) and {id} (FrameworkPrologue) canonicalisation; value-asserting tests in http_endpoint_jester_test.go. Honest follow-ups: Jester re"..." regex routes, multi-verb headers, Prologue cross-call group/prefix mounts. |
| Handler attribution | 🟢 `partial` | `2026-06-11` | 4749 | `internal/engine/http_endpoint_jester.go` | Endpoints emitted with handlerKind=Controller; Jester routes: blocks and Prologue verb methods use inline/anonymous handler bodies, so a same-named handler IMPLEMENTS edge is bound only when the resolver finds one — full named-handler attribution is a follow-up. |
| Route extraction | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_jester.go` | Static Jester/Prologue verb+path routes recovered by synthesizeJester/synthesizePrologue (#4749); interpolated/concatenated paths dropped (honest). |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-06-24` | 5367 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/jester_middleware.go` | Jester before:/after: filter blocks (incl. path-scoped 'before re"/admin/.*":') and Prologue app.use(...) / newApp(middlewares = @[...]) registrations are recognised by custom_nim_jester_middleware, each emitting a SCOPE.Pattern/middleware (framework, provenance, middleware_type, phase, path_scope) mirroring the Lua/Rust middleware shape. Honest partial: regex-based; Prologue middleware attribution is *Middleware-name gated; dynamically-built middleware lists and conditionally-registered middleware are not recovered. |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-24` | — | `internal/custom/nim/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_jester.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks_nim.go`<br>`internal/extractors/cross/testmap/frameworks_nim_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | std/unittest suite/test linkage (frameworks_nim.go, indentation-aware block-body extractor) plus #5367 testament fallback: a testament-style Nim test file (a 'discard """…"""' spec header, asserting via top-level doAssert rather than test "…" cases) yields ONE file-level test case (qname from the spec description: key) whose post-spec body links its production calls — detectNimTestament, value-asserted in TestNimTestament_*. Route-hit linkage via custom/nim/tests_route_e2e.go (std/httpclient helpers) + the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass binds Nim route tests to Jester/Prologue http_endpoint_definition producers (#4749). Honest exclusions: Jester re"…" regex routes, multi-verb headers, Prologue cross-call group/prefix mounts, fully dynamic routes. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.framework.jester ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
