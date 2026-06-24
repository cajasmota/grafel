<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.framework.lucky` — Lucky (Crystal web framework)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 5366 | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | 5366 | — | — |
| Endpoint response codes | 🔴 `missing` | — | 5366 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-06-24` | 5366 | `internal/engine/http_endpoint_kemal.go`<br>`internal/engine/http_endpoint_kemal_test.go` | Each statically-known Lucky inline-path route synthesises one canonical http_endpoint_definition (verb+canonical path, framework=kemal handler kind Controller) in the same shape axum/Vapor/Express emit, so the shared resolver + e2e route-test linker light up for Lucky. Partial: name-derived Action routes excluded (see route_extraction). |
| Handler attribution | 🔴 `missing` | — | 5366 | — | — |
| Route extraction | 🟢 `partial` | `2026-06-24` | 5366 | `internal/engine/http_endpoint_kemal.go`<br>`internal/engine/http_endpoint_kemal_test.go` | Lucky Action classes declare routes with an inline-path get/post/... "/path" macro inside the class body; kemalRouteRe (synthesizeKemalRoutes) captures these at a statement boundary and canonicalises the :name path-param convention via FrameworkKemal. Partial (honest): the class-NAME-derived Action route form (Users::Index -> /users, no inline path literal) is not statically recoverable here and is a documented exclusion. |
| Websocket route extraction | 🔴 `missing` | — | 5366 | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | 5366 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 5366 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 5366 | — | — |
| Request validation | 🔴 `missing` | — | 5366 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 5366 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 5366 | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 5366 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 5366 | — | — |
| Interface extraction | 🔴 `missing` | — | 5366 | — | — |
| Type alias extraction | 🔴 `missing` | — | 5366 | — | — |
| Type extraction | 🔴 `missing` | — | 5366 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 5366 | — | — |
| DI injection point | 🔴 `missing` | — | 5366 | — | — |
| DI scope resolution | 🔴 `missing` | — | 5366 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/tests_route_e2e.go`<br>`internal/extractors/crystal/depth.go` | A Lucky/Kemal/spec request-driving spec (.cr spec hitting get/post "/path" via the spec-kemal helpers) emits a test_suite carrying e2e_route_calls; the language-agnostic linkE2ERouteTestsToEndpoints pass binds a TESTS edge to the exact synthesised endpoint. extractSpecSuite additionally links describe<Const> spec subjects to their class. Anonymous-closure spec blocks carry the route-hit on the suite owner. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 5366 | — | — |
| Metric extraction | 🔴 `missing` | — | 5366 | — | — |
| Trace extraction | 🔴 `missing` | — | 5366 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 5366 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 5366 | — | — |
| Config consumption | 🔴 `missing` | — | 5366 | — | — |
| Constant propagation | 🔴 `missing` | — | 5366 | — | — |
| Dead code detection | 🔴 `missing` | — | 5366 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 5366 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 5366 | — | — |
| Error flow | 🔴 `missing` | — | 5366 | — | — |
| Feature flag gating | 🔴 `missing` | — | 5366 | — | — |
| Fs effect | 🔴 `missing` | — | 5366 | — | — |
| HTTP effect | 🔴 `missing` | — | 5366 | — | — |
| Import resolution quality | 🔴 `missing` | — | 5366 | — | — |
| Module cycle detection | 🔴 `missing` | — | 5366 | — | — |
| Mutation effect | 🔴 `missing` | — | 5366 | — | — |
| Pure function tagging | 🔴 `missing` | — | 5366 | — | — |
| Reachability analysis | 🔴 `missing` | — | 5366 | — | — |
| Request shape extraction | 🔴 `missing` | — | 5366 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 5366 | — | — |
| Response shape extraction | 🔴 `missing` | — | 5366 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 5366 | — | — |
| Schema drift detection | 🔴 `missing` | — | 5366 | — | — |
| Taint sink detection | 🔴 `missing` | — | 5366 | — | — |
| Taint source detection | 🔴 `missing` | — | 5366 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 5366 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 5366 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.framework.lucky ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
