<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.erlang.framework.cowboy` — Cowboy

Auto-generated. Back to [summary](../summary.md).

- **Language:** [erlang](../by-language/erlang.md)
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
| Endpoint synthesis | ✅ `full` | `2026-06-11` | — | `internal/engine/elixir_routes.go`<br>`internal/engine/http_endpoint_erlang_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go` | #4749 (epic #4615 tail): Erlang Cowboy dispatch tables (cowboy_router:compile([{'_', [{"/users/:id", user_handler, []}]}])) in .erl files synthesise canonical http_endpoint_definition entities. The shared synthesizeCowboy producer (internal/engine/elixir_routes.go), previously reached only for case "elixir", is now wired for case "erlang" in applyHTTPEndpointSynthesis and erlang is allowed through synthesisSupportsLanguage. Colon path params (:id) canonicalised to {id} via FrameworkCowboy. Cowboy encodes the verb in the handler's init/2 (not the dispatch table) so an ANY endpoint is emitted per route. Proven by TestErlang_CowboyDispatch. |
| Handler attribution | 🟢 `partial` | — | 4749 | `internal/engine/elixir_routes.go`<br>`internal/engine/http_endpoint_erlang_test.go` | Cowboy dispatch route carries the handler module atom (user_handler), stamped as source_handler, but Cowboy verb dispatch happens inside the handler's init/2 + cowboy_req:method — no per-verb handler function is named in the dispatch table, so no endpoint->handler IMPLEMENTS bridge to a specific operation is emitted (ANY endpoint attributed to the module). Honest partial. |
| Route extraction | ✅ `full` | `2026-06-11` | — | `internal/engine/elixir_routes.go`<br>`internal/engine/http_endpoint_erlang_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go` | #4749 (epic #4615 tail): synthesizeCowboy extracts literal {"/path", Handler, _} dispatch-table triples from Erlang cowboy_router:compile(...) lists (gated on a cowboy_router signal). Host wildcard '_' and non-/ strings skipped; :id folded to {id}. Proven by TestErlang_CowboyDispatch + TestErlang_NonCowboyTupleIgnored (negative guard). |

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
| Tests linkage | 🟢 `partial` | — | 4749 | `internal/custom/erlang/tests_route_e2e.go`<br>`internal/engine/elixir_routes.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_e2e_testmap_4749_erlang_test.go` | Test->endpoint route-hit linkage (#4749, slice of all-framework #4615). Erlang is FUNCTIONAL / process-based (no OO receiver objects) so local-variable/receiver typing (#4680/#4681) is N/A — a Cowboy handler is dispatched by the literal route path on the request, not by an obj.method() receiver; route-string linkage is the coverage mechanism (mirrors functional Elixir #4688 / Clojure #4749). custom_erlang_tests_route_e2e (internal/custom/erlang/tests_route_e2e.go) emits one test_suite per eunit/common_test file (*_tests.erl, *_SUITE.erl, or /test/ dir) carrying e2e_route_calls (VERB+route) for httpc:request(get, {"http://host/path", []}, ...), the bare httpc:request("url") GET form, gun:get(Conn, "/path"), and hackney:get(<<"url">>, ...) / hackney:request(verb, ...) route hits; the language-agnostic engine.linkE2ERouteTestsToEndpoints pass (#4351/#4369) matches each pair to the http_endpoint_definition synthesised by synthesizeCowboy and emits the TESTS edge. Proven RED->GREEN in http_endpoint_e2e_testmap_4749_erlang_test.go (httpc GET+POST -> ANY /todos + gun path-param). Test scope: name_test()/name_test_() (eunit) and case(Config) (common_test) are named fns already mined; route hits live inside their bodies so the suite is keyed per-file (one suite/file, like Jest/ExUnit/clojure.test) — Erlang test blocks are named function clauses not closures, so no synthetic anonymous-block scope-owner is needed. PARTIAL (honest): eunit/CT commonly spin a test server on an ephemeral port and BUILD the URL by ++ concatenation (httpc:request(get, {"http://localhost:" ++ integer_to_list(Port) ++ "/users", []}, ...)) — these non-literal paths are NOT statically recoverable and are dropped (proven by TestErlang_BuiltURLExcluded); only fully-literal-path hits link. ++-built-URL recovery is the documented follow-up. |

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
(or use `go run ./tools/coverage update lang.erlang.framework.cowboy ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
