<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.compose` — Jetpack Compose (Android UI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: composeContextExtractor emits context_provider/context_consumer entities from CompositionLocal definitions (compositionLocalOf, staticCompositionLocalOf), CompositionLocalProvider usages, and LocalXxx.current access patterns. Partial because complex dynamic provision patterns may not be captured. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: composeDeepLinkExtractor emits deep_link entities from navDeepLink { uriPattern = ... } blocks and standalone uriPattern = ... declarations in Compose Navigation composable() blocks. Partial because activity-level intent-filter deep links in AndroidManifest.xml require separate XML parsing. |
| Navigation extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/kotlin/compose.go` | compose.go (#3576) emits screen->route NAVIGATES_TO edges: navController.navigate("detail/42") inside an enclosing @Composable produces HomeScreen -NAVIGATES_TO-> route:detail/{id}, with concrete path-arg segments (numbers/$var/${expr}) normalized back to the declared {id} template; constant routes ("home") pass through unchanged. Value-asserted in compose_edges_test.go (TestComposeNavigatesToEdge asserts HomeScreen NAVIGATES_TO route:detail/{id} and DetailScreen NAVIGATES_TO route:home). Edges leave FromID empty so the resolver substitutes the enclosing composable's host ID at assembly. PARTIAL: sealed-class Screen.X.route indirection emits a NAVIGATES_TO edge marked unresolved=true (TestComposeNavigatesToRouteConstPartial) because the literal route string lives in another file and cannot be resolved in-file. |
| Screen detection | 🟢 `partial` | `2026-05-30` | — | `internal/custom/kotlin/compose.go` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | 🟢 `partial` | `2026-05-30` | — | `internal/custom/kotlin/compose.go` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinNativeImportsExtractor emits native_library and native_function entities from System.loadLibrary(), Runtime.getRuntime().load(), external fun declarations, and companion object JNI init patterns. Partial because ProGuard-renamed JNI entry points may not be resolved. |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinBranchCondExtractor emits branch_condition entities from if(), when() (with is/enum branches), inline if ternary, and filter{}/takeIf{} lambdas. Covers Data Flow branch condition extraction for Compose Kotlin code. Partial because deeply nested or lambda-captured conditions may be missed. |
| State management | 🟢 `partial` | `2026-06-02` | — | `internal/custom/kotlin/compose.go` | compose.go emits StateFlow/MutableStateFlow/collectAsState state entities, plus (#3576) view->viewmodel USES edges: val vm: HomeViewModel = viewModel() / hiltViewModel() / koinViewModel<T>() inside an enclosing @Composable produces HomeScreen -USES-> HomeViewModel (the ViewModel type is already an entity via kotlin extraction; the edge wires the screen to it). Value-asserted in compose_edges_test.go (TestComposeUsesViewModelEdge asserts HomeScreen USES HomeViewModel, ProfileScreen USES ProfileViewModel, SettingsScreen USES SettingsViewModel, and the negative no-cross-screen-leak case). Partial: cross-file StateFlow ownership and dynamic state holders remain unmodeled. |

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
| Request shape extraction | — `not_applicable` | — | — | — | Compose is a declarative UI framework with no HTTP handler surface. No request shapes are extracted. HTTP calls in Compose apps go through a separate network layer (ktor-client, Retrofit) handled in non-Compose files. |
| Response shape extraction | — `not_applicable` | — | — | — | Compose is a pure UI framework; there are no HTTP response shapes to extract from @Composable functions. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Schema drift detection | — `not_applicable` | — | — | — | Compose UI layer has no HTTP payload schema. Schema drift is not applicable for @Composable functions. |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.compose ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
