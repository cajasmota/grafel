<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.framework.jester` — Jester / Prologue (Nim HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

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
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
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
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/nim/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_jester.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks_nim.go`<br>`internal/extractors/cross/testmap/resolver.go` | Nim slice of the test->endpoint coverage-linkage tail epic (#4749/#4615 — the LAST language in the tail; mirrors Crystal/Kemal + Lua/Lapis). PRODUCER: internal/engine/http_endpoint_jester.go synthesizes canonical http_endpoint_definition entities from Nim web route registrations — Jester routes:-block verb entries (get "/users/@id": with @id->{id} via FrameworkJester) and Prologue app.get("/users/{id}", h) / addRoute("/x", h, HttpGet) (curly-brace {id}, FrameworkPrologue) — in the same shape axum/Rocket/Express/Kemal/Vapor emit; the base nim extractor stays structural-only. ROUTE-HIT linkage: custom/nim/tests_route_e2e.go emits one test_suite per Nim test file (tFoo.nim / *_test.nim / tests/) carrying e2e_route_calls from std/httpclient helpers (client.get/post/put/delete/request against a test server, host stripped to path); the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass emits the TESTS edge to the exercised Jester/Prologue endpoint (proven by TestIssue4749_NimUnittestE2ERouteTestsLinkToEndpoints + TestJester_*/TestPrologue_* producer tests + TestNimRouteE2E_*). NAMED-SYMBOL test->SUT linkage: std/unittest suite/test blocks registered in the shared cross/testmap extractor (frameworks_nim.go) — each test "...": leaf emits a TESTS edge to the production symbol via direct-call resolution (high) and suite-subject fallback (medium), using a Nim indentation-aware block-body extractor; pytest skipped for .nim files to avoid the shared 'unittest' import-token collision. SCOPE-OWNER: std/unittest test "...": descriptions are prose (not code symbols) with no callable entity name, so — like JS/Ruby/Crystal anonymous closures — the suite-level test_suite is the scope-owner carrying the route hits (#4684/#4680 analog). Local-variable/receiver typing (#4749 part a) is N/A: the nim base extractor names procs/methods bare (CONTAINS edge from an attached type, not Type.method) with no class-qualified receiver resolver to consume a receiver_type stamp; route-string linkage is the coverage mechanism (mirrors functional Elixir #4688 / Crystal). Honest exclusions/follow-ups: Jester re"..." regex routes and multi-verb route headers, Prologue cross-call group/prefix mounts, and fully dynamic/concatenated routes (no static literal). |

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
