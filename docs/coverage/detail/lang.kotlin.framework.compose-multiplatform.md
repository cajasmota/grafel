<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.compose-multiplatform` — Compose Multiplatform

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 14

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | — `not_applicable` | — | — | — | Compose Multiplatform is a UI framework targeting Android, iOS, Desktop, and Web. It has no IPC tier — there is no message-passing boundary between processes in the CMP architecture. Platform-native IPC (Android Binder, iOS XPC) is outside the CMP scope. N/A is accurate. |
| Main renderer split | — `not_applicable` | — | — | — | Compose Multiplatform has no Electron/Tauri main/renderer process split. All targets (Android, iOS, Desktop, Web) run the UI in-process. N/A is the accurate classification. |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinNativeImportsExtractor covers Compose Multiplatform native bridge — cinterop platform.*/cnames.structs.* imports for iOS/native targets, @CName exported symbols, and actual fun delegating to native. Android JNI (System.loadLibrary) also covered. Partial because .def cinterop file parsing is outside scope. |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | — | — | `internal/substrate/kotlin.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Env fallback recognition | ✅ `full` | — | — | `internal/substrate/kotlin.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/kotlin/imports.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.compose-multiplatform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
