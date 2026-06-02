<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.kmp` — Kotlin Multiplatform (KMP / KMM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: composeContextExtractor handles KMP expect/actual context patterns — expect class AppContext, actual class AppContext(val context: android.content.Context), and context factory functions. Partial because iOS-side CoreData/NSManagedObjectContext patterns may not be fully captured. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | — `not_applicable` | — | — | — | KMP (Kotlin Multiplatform) is a shared code framework, not a navigation host. Deep links are a platform-specific concern handled by Compose Navigation (Android) or native iOS routing — not by the KMP shared module itself. N/A is the honest classification. |
| Navigation extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/kotlin/compose.go` | compose.go (#3576) runs on all Kotlin including KMP shared modules and emits screen->route NAVIGATES_TO edges from navController.navigate("route") call sites attributed to the enclosing @Composable, with concrete path-arg segments normalized to {id}. Value-asserted in compose_edges_test.go (TestComposeNavigatesToEdge). PARTIAL: sealed-class Screen.X.route route-constant indirection emits an edge marked unresolved=true (cross-file constant, not resolvable in-file). |
| Screen detection | 🟢 `partial` | `2026-05-30` | — | `internal/custom/kotlin/compose.go` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | 🟢 `partial` | `2026-05-30` | — | `internal/custom/kotlin/compose.go` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinNativeImportsExtractor emits native_import entities from cinterop platform.* and cnames.structs.* imports, KMP actual fun delegating to native calls, and @CName exported symbols. These are the primary KMP native bridge patterns. Partial because C struct typedef resolution requires .def file parsing. |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | Recording win via new extractor: kotlinBranchCondExtractor is the same agnostic Kotlin if/when/filter pass that runs on all Kotlin source including KMP shared modules. Branch conditions in expect/actual code are captured identically. |
| State management | 🟢 `partial` | `2026-06-02` | — | `internal/custom/kotlin/compose.go` | compose.go emits StateFlow/collectAsState state entities plus (#3576) view->viewmodel USES edges (HomeScreen -USES-> HomeViewModel) from viewModel()/hiltViewModel()/koinViewModel<T>() injection inside an enclosing @Composable; runs on KMP shared Kotlin identically. Value-asserted in compose_edges_test.go (TestComposeUsesViewModelEdge). Partial: cross-file state ownership unmodeled. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🟢 `partial` | `2026-05-30` | — | `internal/custom/kotlin/compose.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/engine/rules/kotlin/test_patterns.yaml`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Deep Kotlin TESTING linkage (#3437): junit5 @Test/@ParameterizedTest/@RepeatedTest + class-name subject; kotest StringSpec/FunSpec/DescribeSpec/BehaviorSpec/ShouldSpec/Spek DSL leaf cases with body call-scan; MockK mockk<T>() subject association with every{}/verify{} blocks blanked so the mocked call never leaks; Kotlin assertion/mockk stopwords (shouldBe/assertThrows/every/verify/any). Value-asserted in extractor_test.go (TestKotlin_JUnit5_*/Kotest_*/Mockk_*/Spek_*). |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Request shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_kotlin.go` | KMP shared code can use ktor-client; client.get/post with mapOf and data class @Serializable shapes are extracted. Server-side shapes require ktor server in shared module (less common). File-local. |
| Response shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_kotlin.go` | KMP ktor-client response data classes extracted via kotlinx.serialization @Serializable data class primary constructor parameters. File-local. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Schema drift detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_kotlin.go` | KMP payload shapes are compared across producer/consumer for drift detection. Limited to ktor-client usage in KMP shared modules. File-local. |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.kmp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
