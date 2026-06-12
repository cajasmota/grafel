<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.framework.flutter` — Flutter

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/3505) | `internal/custom/dart/extractors_test.go`<br>`internal/custom/dart/flutter.go` | StatelessWidget/StatefulWidget class declarations -> SCOPE.UIComponent (widget_type stateless/stateful). Regex/heuristic but catches the canonical Flutter widget idiom; value-asserting tests (TestFlutterStatelessWidget/StatefulWidget). |
| Context extraction | ✅ `full` | — | — | `internal/custom/dart/extractors_test.go`<br>`internal/custom/dart/flutter.go`<br>`internal/custom/dart/flutter_edges_resolve_test.go` | DI / state-binding (Provider, Riverpod, BLoC) emits USES edges from the enclosing widget. flutter.go steps 10-13: context.read|watch|select<T>, BlocBuilder<T,S>, BlocProvider.of<T>/Provider.of<T>/RepositoryProvider.of<T>, and Riverpod ref.watch|read|listen(xProvider). Host attributed via brace-span tracking; edges resolve to hex IDs via ReferencesEmbedded. Value-asserting + resolver tests: ProfileScreen USES ProfileBloc (context.read), CartWidget USES CartModel (Provider.of), CounterView USES counterProvider (ref.watch). This is the DI home cell for the ui_frontend subcategory. |

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
| Enum extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: the base Dart extractor (types.go, dartEnums) now emits a SCOPE.Enum value-set node per `enum` declaration via extractor.EnumEntity(kind_hint=dart_enum) — plain enums and Dart-2.17 enhanced enums (constants-before-`;` kept as members, single-literal ctor args lifted to values). Value-asserted: TestDartTypes_PlainEnum (Color -> red,green,blue), TestDartTypes_EnhancedEnum (Planet -> mercury,earth, post-`;` fields/methods dropped). PARTIAL: regex over canonical declarations; computed/multi-arg ctor values are not resolved. |
| Interface extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: Dart-3 class modifiers (types.go, dartModifiedClasses) — `sealed`/`base`/`interface`/`final`/`mixin class` — are now extracted as SCOPE.Component(class) carrying Properties{class_modifier, dart_sealed, dart_interface}; the base classRE only matched plain/`abstract` classes so these (incl. `interface class`, Dart's nominal-interface form) were invisible. Value-asserted: TestDartTypes_SealedClass (sealed Shape -> dart_sealed=true; interface Drawable -> dart_interface=true). PARTIAL: subtype permits-clause / exhaustiveness graph wiring not modelled; Dart has no standalone `interface` keyword (interfaces are implicit via `implements`). |
| Type alias extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: `typedef` (types.go, dartTypedefs) now emits SCOPE.Schema(subtype=type_alias) with type_body — both the modern `typedef Name = <type>;` (Dart 2.13+) and the legacy function-type `typedef Ret Name(params);` spellings, parity with the python/rust/go type_alias shape. Value-asserted: TestDartTypes_TypedefAlias (JsonMap -> Map<String, dynamic>), TestDartTypes_TypedefFunc (Comparator). PARTIAL: regex over canonical one-line forms. |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/engine/loader.go`<br>`internal/engine/rules/dart/test_patterns.yaml` | flutter_test / integration_test / Dart test-package classification via rules/dart/test_patterns.yaml (testWidgets/WidgetTester/pumpWidget/find.byType + group/test/expect), embedded and loaded by engine/loader.go (go:embed all:rules). PARTIAL: classifies test files/frameworks but no testmap framework detector exists for Flutter yet, so no graph TESTS edges are synthesised from testWidgets() to the widget under test (cf. lang.kotlin.*/php pest+phpunit which have testmap detectors -> full). Deepening (a flutter_test detector in internal/extractors/cross/testmap) is the path to full. test->ENDPOINT route-hit linkage for the Dart SERVER frameworks (shelf_router/dart_frog/conduit) is now FULL and tracked under lang.dart.framework.shelf (#4758, closing the #4757 N/A): a Dart server-side http_endpoint_definition producer (internal/engine/http_endpoint_dart.go) now exists, so custom/dart/tests_route_e2e.go emits package:test route-hit suites that the shared engine.linkE2ERouteTestsToEndpoints pass binds to the exercised endpoint. This Flutter (UI) cell stays PARTIAL only for the testWidgets()->widget-under-test graph TESTS edges (a flutter_test testmap detector is the remaining work); the route-hit half of the #4749 tail is no longer N/A. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/effect_sinks_dart.go`<br>`internal/substrate/effect_sinks_t3_test.go` | sqflite db.query/rawQuery (db_read) and db.insert/update/delete/execute (db_write), plus drift generated methods, recognised by the framework-blind Dart effect sniffer (gated on .dart via LanguageForPath). Value-asserted: TestSniffEffectsDart_PrimitiveCoverage maps fetchUsers->db_read, saveUser->db_write. PARTIAL: regex heuristic on canonical primitives, not a full ORM corpus. |
| Dead code detection | 🟢 `partial` | — | 4035 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_dart.go` | reachability.go BFS seeds from the Dart entry-point sniffer (entry_points_dart.go: top-level main, runApp, dart_frog onRequest, shelf Request handlers, Isolate.spawn) so Flutter/server entities reached only via those roots stop being over-reported as dead. Value-asserted: TestDartEntryPoints_MainAndRunApp/DartFrogHandler/IsolateSpawn. PARTIAL: full Dart module/pubspec resolution not modelled. |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/dart_flutter_substrate_credit_4034_test.go`<br>`internal/substrate/def_use_dart.go` | var/final/const/late and typed local declarations -> defs; bare identifier reads -> uses, both attributed to the nearest enclosing Dart function/method header. Value-asserted: TestFlutterSubstrate_DefUseChainExtraction (final name=userId; final greeting=name; -> name/greeting defs + name use, attributed to loadUser). PARTIAL: regex heuristic, function-local scope. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/effect_sinks_dart.go`<br>`internal/substrate/effect_sinks_t3_test.go` | dart:io File(...).readAsString/openRead (fs_read) and writeAsString/openWrite/create (fs_write) recognised by the Dart effect sniffer. Value-asserted: TestSniffEffectsDart_PrimitiveCoverage maps loadConfig->fs_read, writeLog->fs_write. PARTIAL: regex heuristic on canonical primitives. |
| HTTP effect | ✅ `full` | — | — | `internal/engine/http_endpoint_dart_client.go`<br>`internal/engine/http_endpoint_dart_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each Dio / package:http call is recorded as an outbound HTTP effect (verb + canonical path) emitted with the SAME http_endpoint_call entity shape as the backend producer side, so the cross-repo linker pairs the Flutter screen with the backend route on reindex. Value-asserted in http_endpoint_dart_client_test.go. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/effect_sinks_dart.go`<br>`internal/substrate/effect_sinks_t3_test.go` | this.field = ... assignments inside a method body recognised as mutation effects by the Dart effect sniffer. Value-asserted: TestSniffEffectsDart_PrimitiveCoverage maps saveUser->mutation. PARTIAL: regex heuristic. |
| Pure function tagging | 🟢 `partial` | — | 4035 | `internal/links/pure_function_pass.go`<br>`internal/substrate/entry_points_dart.go` | pure_function_pass.go is language-agnostic and tags effect-free functions as pure over the reachable set; the Dart entry-point sniffer (entry_points_dart.go, #4035) now seeds that reachable set so non-entry effect-free Dart functions are eligible. PARTIAL: regex heuristic; async/Future purity not fully modelled. |
| Reachability analysis | 🟢 `partial` | — | 4035 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_dart.go` | reachability.go BFS seeds from the new Dart entry-point sniffer (entry_points_dart.go): top-level main (cli_main), runApp / dart_frog onRequest / shelf Request handlers / Isolate.spawn (framework_lifecycle), and test/testWidgets/group (test_entry). Value-asserted: TestDartEntryPoints_* (main+runApp, onRequest, Isolate.spawn(heavyTask)) + negatives (nested class main, plain fn). PARTIAL: full Dart module-boundary traversal requires pubspec integration. |
| Request shape extraction | 🟢 `partial` | — | 4035 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_dart.go` | payload_shapes_dart.go (#4035) lifts consumer request shapes from Dio/http post/put/patch call bodies: inline map literals (data: {'name': n, 'email': e}) and <var>.toJson() of a same-file @JsonSerializable/@freezed DTO. Value-asserted: TestDartPayload_DioPostBody_RequestShape ({name,email}, VerbHint POST) + ToJsonBody + negative GetNoBody. PARTIAL: consumer-side only (Dart is the client), same-file DTO resolution, single-line call bodies. |
| Response shape extraction | 🟢 `partial` | — | 4035 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_dart.go` | payload_shapes_dart.go (#4035) lifts consumer response shapes from X.fromJson(...) where X is a same-file @JsonSerializable/@freezed DTO; field names+types+nullability come from the class's final fields. Value-asserted: TestDartPayload_JsonSerializableDTO_ResponseShape (User -> {id:int, name:String, email:String? optional}) + negative PlainClassNoDTO. PARTIAL: same-file DTO resolution; cross-file/generated *.g.dart shapes not resolved. |
| Sanitizer recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/dart_flutter_substrate_credit_4034_test.go`<br>`internal/substrate/taint_sites_dart.go`<br>`internal/substrate/taint_sites_test.go` | Parameterised sqflite (db.query(..., whereArgs: [...]) / substitutionValues) recognised as a sanitizer by the Dart taint sniffer. Value-asserted: TestTaintSniffer_Dart_RawQueryIsSink (db.query(whereArgs:) -> sanitizer). PARTIAL: regex heuristic. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/dart_flutter_substrate_credit_4034_test.go`<br>`internal/substrate/taint_sites_dart.go`<br>`internal/substrate/taint_sites_test.go` | sqflite db.rawQuery/rawInsert/rawUpdate/rawDelete with non-literal args (SQL injection), Process.run/start (command injection), File.write* with non-literal path (path traversal) flagged as sinks by the Dart taint sniffer. Value-asserted: TestTaintSniffer_Dart_RawQueryIsSink (db.rawQuery(concat) -> SQL sink). PARTIAL: regex heuristic. |
| Taint source detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/dart_flutter_substrate_credit_4034_test.go`<br>`internal/substrate/taint_sites_dart.go`<br>`internal/substrate/taint_sites_test.go` | dart:io HttpRequest.uri.queryParameters/requestedUri/read and shelf Request inputs flagged as taint sources by the Dart taint sniffer. Value-asserted: TestFlutterSubstrate_TaintSourceDetection (request.uri.queryParameters -> source). PARTIAL: regex heuristic, server-side Dart idioms. |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/dart_flutter_substrate_credit_4034_test.go`<br>`internal/substrate/template_pattern_dart.go` | i18n (AppLocalizations.of(context).x / tr / Intl.message / easy_localization), log_format (print/debugPrint/Logger), and SQL literals (sqflite/drift raw queries) catalogued by the Dart template-pattern sniffer. Value-asserted: TestFlutterSubstrate_TemplatePatternCatalog (debugPrint->log_format, AppLocalizations.translate->i18n, SELECT...->sql). PARTIAL: regex heuristic, conservative i18n package set. |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.dart.framework.flutter ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
