<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.libevent` — libevent

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 43

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint synthesis | — `not_applicable` | — | — | — | libevent is a low-level async I/O library; no HTTP route-registration DSL exists — routing is app-level, not framework-provided |
| Handler attribution | — `not_applicable` | — | — | — | libevent is a low-level async I/O library; no HTTP route-registration DSL exists — routing is app-level, not framework-provided |
| Route extraction | — `not_applicable` | — | — | — | libevent is a low-level async I/O library; no HTTP route-registration DSL exists — routing is app-level, not framework-provided |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | — `not_applicable` | — | — | — | low-level network/event library; no authentication subsystem |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | Raw async I/O library; no HTTP request DTO framework or field declaration DSL provided |
| Request validation | — `not_applicable` | — | — | — | Raw async I/O library; HTTP request validation is app-level, not framework-provided |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | low-level network/event library; no HTTP middleware pipeline |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/cpp/extractor.go` | Scoped/unscoped enums with enumerator names, explicit values, and fixed underlying type |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/cpp/extractor.go` | Abstract class (pure-virtual methods) and C++20 concept extraction |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/type_alias.go` | typedef (incl. function-pointer), using-alias, and alias templates |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/cpp/extractor.go` | class/struct/union with data members (name/type/access), base-class inheritance, abstract detection |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_cpp.go`<br>`internal/extractors/cross/testmap/resolver.go` | gtest (TEST/TEST_F/TEST_P), catch2 + doctest (TEST_CASE/SECTION), boost.test (BOOST_AUTO_TEST_CASE/BOOST_FIXTURE_TEST_CASE), cppunit (CPPUNIT_TEST + void Class::testX bodies, inline and out-of-line), and cpputest (TEST(group,name)) registered in the shared testmap extractor: each case emits a TESTS edge to the production symbol via direct-call resolution (high), suite/fixture/group subject fallback (medium, Test/Fixture affix stripped), and *_test/test_*/FooTest naming convention (low). #include <...> headers feed framework selection; EXPECT_*/ASSERT_*/CHECK/REQUIRE/BOOST_CHECK/CPPUNIT_ASSERT/STRCMP_EQUAL assertion macros stop-worded. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/observability.go`<br>`internal/substrate/template_pattern_c_cpp.go` | Heuristic regex: spdlog/glog/Boost.Log/printf/std stream detected; log_level severity token captured at call site (spdlog::info, LOG(INFO)). Message text and runtime format args NOT pinned (dataflow); logger-> receiver type assumed not resolved -> stays partial. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/observability.go` | Metric name captured as literal at call site: prometheus .Name("name"), otel meter->CreateCounter("name"), statsd .increment("name") -> metric_name prop; value-asserting tests pin specific names. Runtime-bound names stay unpinned (honest). No cross-file resolution. |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/observability.go` | Span name captured as literal at call site (tracer->StartSpan("name")/StartActiveSpan/jaeger StartSpan) -> span_name prop; value-asserting tests pin specific names. No cross-file resolution needed. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_c_cpp.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_c_cpp.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_c_cpp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_c_cpp.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_c_cpp.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.libevent ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
