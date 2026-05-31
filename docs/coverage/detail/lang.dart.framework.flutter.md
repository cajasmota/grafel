<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.framework.flutter` — Flutter

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 33

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/3505) | `internal/custom/dart/extractors_test.go`<br>`internal/custom/dart/flutter.go` | StatelessWidget/StatefulWidget class declarations -> SCOPE.UIComponent (widget_type stateless/stateful). Regex/heuristic but catches the canonical Flutter widget idiom; value-asserting tests (TestFlutterStatelessWidget/StatefulWidget). |
| Context extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Data fetching | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Prop extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/dart/flutter.go` | Bloc/Cubit/ChangeNotifier/InheritedWidget classes + StreamBuilder/BlocBuilder/context.read|watch|select<T> detected as SCOPE.Pattern/Component (state_kind). PARTIAL: class/usage detection only — no state-transition or data-flow resolution, no provider->consumer edges. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/dart/flutter.go` | Navigator.pushNamed/pushReplacementNamed + GoRoute(path:) detected as SCOPE.Operation/endpoint with route_path. PARTIAL: routes emitted as standalone entities — no screen->screen NAVIGATES_TO edges, no route->widget/builder resolution. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.dart.framework.flutter ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
