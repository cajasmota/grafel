<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.framework.pedestal` — Pedestal

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 4910 | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | 4910 | — | — |
| Endpoint response codes | 🔴 `missing` | — | 4910 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-06-24` | 5362 | `internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_clojure_test.go`<br>`internal/engine/httproutes/canonicalize.go` | #5362: synthesizeClojureRoutes (http_endpoint_clojure.go) now emits canonical http_endpoint_definition entities for Pedestal table-route vectors — a route is a vector whose head is a string-literal path and whose second element is the HTTP-verb keyword (["/users/:id" :get get-user :route-name :get-user] / #{["/u" :get h]}), gated on an io.pedestal marker so a verb keyword never double-fires on a Reitit {…}-map route. Colon path params (:id) canonicalised to {id} via FrameworkClojure. Proven by TestClojure_PedestalRoutes. Partial: interpolated/variable paths are honest-skipped, and handler-symbol → defn binding is left to the same-name resolver (#4910 tail). |
| Handler attribution | 🔴 `missing` | — | 4910 | — | — |
| Route extraction | 🟢 `partial` | — | 5362 | `internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_clojure_test.go` | #5362: Pedestal table-route vectors (["/path" :verb handler …]) are now parsed by synthesizeClojureRoutes (cljPedestalRouteRe) into per-(verb,path) endpoints. Proven by TestClojure_PedestalRoutes. Partial: only literal-path table routes; interpolated paths honest-skipped. |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | 4910 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4910 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 4910 | — | — |
| Request validation | 🔴 `missing` | — | 4910 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 4910 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 4910 | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 4910 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 4910 | — | — |
| Interface extraction | 🔴 `missing` | — | 4910 | — | — |
| Type alias extraction | 🔴 `missing` | — | 4910 | — | — |
| Type extraction | 🔴 `missing` | — | 4910 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4910 | — | — |
| DI injection point | 🔴 `missing` | — | 4910 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4910 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 4910 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4910 | — | — |
| Metric extraction | 🔴 `missing` | — | 4910 | — | — |
| Trace extraction | 🔴 `missing` | — | 4910 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 4910 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4910 | — | — |
| Config consumption | 🔴 `missing` | — | 4910 | — | — |
| Constant propagation | 🔴 `missing` | — | 4910 | — | — |
| Dead code detection | 🔴 `missing` | — | 4910 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4910 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4910 | — | — |
| Error flow | 🔴 `missing` | — | 4910 | — | — |
| Feature flag gating | 🔴 `missing` | — | 4910 | — | — |
| Fs effect | 🔴 `missing` | — | 4910 | — | — |
| HTTP effect | 🔴 `missing` | — | 4910 | — | — |
| Import resolution quality | 🔴 `missing` | — | 4910 | — | — |
| Module cycle detection | 🔴 `missing` | — | 4910 | — | — |
| Mutation effect | 🔴 `missing` | — | 4910 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4910 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4910 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4910 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4910 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4910 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4910 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4910 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4910 | — | — |
| Taint source detection | 🔴 `missing` | — | 4910 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4910 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4910 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.clojure.framework.pedestal ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
