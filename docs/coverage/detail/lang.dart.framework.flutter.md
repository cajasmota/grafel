<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.framework.flutter` — Flutter

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 34

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
| Data fetching | ✅ `full` | — | — | `internal/engine/http_endpoint_dart_client.go`<br>`internal/engine/http_endpoint_dart_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Dio (dio.get/post/put/delete) and package:http (http.get(Uri.parse(...))) outbound calls -> one http_endpoint_call (consumer) per call site + FETCHES from the enclosing function; host stripped, $id/${expr} -> {id}/{param} to match server-route normalisation. Value-asserted in http_endpoint_dart_client_test.go (e.g. dio.post("/auth/login") -> POST /auth/login, http.get(Uri.parse(".../v1/users")) -> GET /v1/users). |
| Prop extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/3578) | `internal/custom/dart/extractors_test.go`<br>`internal/custom/dart/flutter.go`<br>`internal/custom/dart/flutter_edges_resolve_test.go` | Bloc/Cubit/ChangeNotifier/InheritedWidget classes detected as SCOPE.Pattern. #3578: provider/viewmodel binding now emits USES edges from the enclosing widget — context.read|watch|select<T>, BlocBuilder<T>, BlocProvider.of<T>/Provider.of<T>, and Riverpod ref.watch|read(xProvider). Host attributed via brace-span tracking; FromID/ToID resolve to hex IDs via ReferencesEmbedded (value-asserting + resolver tests: ProfileScreen USES ProfileBloc, CartWidget USES CartModel, CounterView USES counterProvider). State-transition/data-flow resolution stays out of scope. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/3578) | `internal/custom/dart/extractors_test.go`<br>`internal/custom/dart/flutter.go`<br>`internal/custom/dart/flutter_edges_resolve_test.go` | #3578: navigation now emits NAVIGATES_TO edges from the enclosing screen widget. Covers Navigator.pushNamed -> route stub, Navigator.push(MaterialPageRoute(builder: => Widget)) -> widget target, go_router context.go/push -> route stub, and GoRoute(path:, builder: => Screen) route->screen wiring. Path params normalized (:id/{id} -> {id}). Host attributed via brace-span tracking; edges resolve to hex IDs via ReferencesEmbedded (value-asserting + resolver tests: HomeScreen NAVIGATES_TO route:/detail/{id}, HomeScreen NAVIGATES_TO DetailScreen, go_route:/profile/{id} NAVIGATES_TO ProfileScreen). PARTIAL only for cross-file sealed-route-class / generated go_router constant indirection (emitted unresolved=true). |

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
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ✅ `full` | — | — | `internal/engine/http_endpoint_dart_client.go`<br>`internal/engine/http_endpoint_dart_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each Dio / package:http call is recorded as an outbound HTTP effect (verb + canonical path) emitted with the SAME http_endpoint_call entity shape as the backend producer side, so the cross-repo linker pairs the Flutter screen with the backend route on reindex. Value-asserted in http_endpoint_dart_client_test.go. |
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
