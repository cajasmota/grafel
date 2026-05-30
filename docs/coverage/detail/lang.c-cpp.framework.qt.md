<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.qt` — Qt

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 33

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | QObject subclass (Q_OBJECT macro) emits SCOPE.UIComponent; regex/partial |
| Context extraction | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | QApplication/QGuiApplication/QQmlApplicationEngine construction detected as context; regex/partial |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | switch on Qt enums, Q_ASSERT, if(... == Qt::EnumVal) detected; regex/partial |
| Data fetching | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | QNetworkAccessManager get/post/put calls detected as data fetch; regex/partial |
| Prop extraction | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | Q_PROPERTY(type name READ getter) emits property pattern entity; regex/partial |
| State management | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | Qt slots sections (state handlers) detected as SCOPE.Operation/function; regex/partial |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | QStackedWidget setCurrentIndex/setCurrentWidget and QML StackView push/pop detected; regex/partial |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | — | — | `internal/extractors/cpp/extractor.go` | — |
| Interface extraction | — `not_applicable` | — | — | — | C/C++ has no interface keyword; closest construct is pure-virtual abstract class (covered under type_extraction) |
| Type alias extraction | 🟢 `partial` | — | — | `internal/custom/cpp/type_alias.go` | typedef and using-alias declarations extracted by regex; partial = heuristic, no full type resolution |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🟢 `partial` | — | — | `internal/custom/cpp/qt.go` | emit <signal>(...) calls detected; signals sections parsed; regex/partial |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | — | `internal/substrate/entry_points_c_cpp.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_c_cpp.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/c_cpp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_c_cpp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | — |
| Request shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_c_cpp.go` | — |
| Response shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_c_cpp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Schema drift detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/payload_drift.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_c_cpp.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_c_cpp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.qt ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
