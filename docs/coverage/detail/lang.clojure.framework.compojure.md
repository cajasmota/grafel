<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.framework.compojure` — Compojure

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 4749 | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | 4749 | — | — |
| Endpoint response codes | 🔴 `missing` | — | 4749 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_clojure_test.go`<br>`internal/engine/httproutes/canonicalize.go` | #4749 (epic #4615 tail): synthesizeClojureRoutes (http_endpoint_clojure.go) emits canonical http_endpoint_definition entities from Compojure verb macros ((GET "/users/:id" [] handler), (POST ...), inside (defroutes app ...)); colon path params (:id) canonicalised to {id} via FrameworkClojure (canonicalizeColonParams). Proven by TestClojure_CompojureRoutes. Handler attribution is partial — trailing handler symbol not yet bound to its defn entity (route emitted with empty handler ref, resolver binds by same-name when present). |
| Handler attribution | 🟢 `partial` | — | 4749 | `internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_clojure_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Route emitted with empty handler ref; resolver binds by same-name handler when present. Trailing-form handler symbol not yet bound to its defn entity — honest partial. |
| Route extraction | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_clojure_test.go`<br>`internal/engine/httproutes/canonicalize.go` | #4749 (epic #4615 tail): synthesizeClojureRoutes (http_endpoint_clojure.go) emits canonical http_endpoint_definition entities from Compojure verb macros ((GET "/users/:id" [] handler), (POST ...), inside (defroutes app ...)); colon path params (:id) canonicalised to {id} via FrameworkClojure (canonicalizeColonParams). Proven by TestClojure_CompojureRoutes. Handler attribution is partial — trailing handler symbol not yet bound to its defn entity (route emitted with empty handler ref, resolver binds by same-name when present). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | 4749 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4749 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 4749 | — | — |
| Request validation | 🔴 `missing` | — | 4749 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 4749 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 4749 | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 4749 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 4749 | — | — |
| Interface extraction | 🔴 `missing` | — | 4749 | — | — |
| Type alias extraction | 🔴 `missing` | — | 4749 | — | — |
| Type extraction | 🔴 `missing` | — | 4749 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4749 | — | — |
| DI injection point | 🔴 `missing` | — | 4749 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4749 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/clojure/tests_route_e2e.go`<br>`internal/engine/http_endpoint_clojure.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_e2e_testmap_4749_clojure_test.go` | Test->endpoint route-hit linkage (#4749, slice of all-framework #4615). Clojure is FUNCTIONAL (no OO receiver objects) so local-variable/receiver typing (#4680/#4681) is N/A — a Ring handler is dispatched by the literal route string on the mock request map, not by an obj.method() receiver; route-string linkage is the coverage mechanism (mirrors functional Elixir #4688). custom_clojure_tests_route_e2e (internal/custom/clojure/tests_route_e2e.go) emits one test_suite per clojure.test file (*_test.clj/cljc/cljs or /test/ dir) carrying e2e_route_calls (VERB+route) for ring-mock (app (mock/request :get "/path")) and peridot/kerodon (request app "/path" :request-method :get) route hits; the language-agnostic engine.linkE2ERouteTestsToEndpoints pass (#4351/#4369) matches each pair to the http_endpoint_definition synthesised by synthesizeClojureRoutes and emits the TESTS edge. Proven RED->GREEN in http_endpoint_e2e_testmap_4749_clojure_test.go (GET+POST + path-param). Test scope: (deftest name ...) are named fns already mined; route hits live inside the deftest body so the suite is keyed per-file (one suite/file, like Jest/ExUnit) — no synthetic anonymous-block scope-owner needed. Honest exclusion: interpolated/built routes ((mock/request :get (str "/x/" id))) and variable routes are dropped (non-literal). |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4749 | — | — |
| Metric extraction | 🔴 `missing` | — | 4749 | — | — |
| Trace extraction | 🔴 `missing` | — | 4749 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 4749 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4749 | — | — |
| Config consumption | 🔴 `missing` | — | 4749 | — | — |
| Constant propagation | 🔴 `missing` | — | 4749 | — | — |
| Dead code detection | 🔴 `missing` | — | 4749 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4749 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4749 | — | — |
| Error flow | 🔴 `missing` | — | 4749 | — | — |
| Feature flag gating | 🔴 `missing` | — | 4749 | — | — |
| Fs effect | 🔴 `missing` | — | 4749 | — | — |
| HTTP effect | 🔴 `missing` | — | 4749 | — | — |
| Import resolution quality | 🔴 `missing` | — | 4749 | — | — |
| Module cycle detection | 🔴 `missing` | — | 4749 | — | — |
| Mutation effect | 🔴 `missing` | — | 4749 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4749 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4749 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4749 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4749 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4749 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4749 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4749 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4749 | — | — |
| Taint source detection | 🔴 `missing` | — | 4749 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4749 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4749 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.clojure.framework.compojure ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
