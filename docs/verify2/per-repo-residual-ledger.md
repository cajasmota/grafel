# Per-repo Residual Ledger (Tier-1)

_Seeded 2026-05-19 from wave-1 + wave-2 fix-agent reports and the
`quick-tier1-baseline-refresh-2026-05-19-v2.md` measurement set
(Closes #484, Refs #44)._

This ledger is the single source of truth for **what is still wrong, where, and why**
on every tier-1 repo in `scripts/verify2/run.sh`. It exists so wave-N planning is a
filter+sort against one file, not a re-read of every fix-agent thread.

## How to use this ledger

1. **After every merged fix PR:** the coordinator updates each affected row
   - new `Latest bug-rate` (date + source measurement doc)
   - new `Residual root cause` from the fix-agent's report
   - new `Status` per the enum below
   - `Blocker / next fix` = the next chain-fix PR or issue number to file
2. **After every re-measurement run:** update `Latest bug-rate` for all measured rows
   even if root cause is unchanged.
3. **Picking the next wave:** filter `Status in {at-bar, addressable}`, sort by
   bug-rate desc, take top 3-4. Avoid `structural` and `upstream` unless the
   blocking primitive issue is also in-flight.

## Workflow rule (going forward)

**Every wave's fix-agent PR body MUST include two lines:**

```
Residual root cause: <one sentence — what bug class still dominates the residual>
Status: <at-ship-gate | at-bar | addressable | structural | upstream>
```

The coordinator then updates this ledger as part of the merge step. PRs that
miss these lines should be sent back to the agent before merge.

## Status enum

| Status | Definition |
|---|---|
| `at-ship-gate` | bug-rate <= 1% (#44 target) |
| `at-bar` | 1% < bug-rate <= 8% (per-repo bar passed, ship-gate gap remains) |
| `addressable` | > 8% but next-layer chain-fix is queued (PR# or issue# in Blocker col) |
| `structural` | > 8%, fix requires multi-day work and a dedicated issue (in Blocker col) |
| `upstream` | > 8%, blocked on an extractor/resolver primitive being landed elsewhere |
| `unmeasured` | in `scripts/verify2/run.sh` tier-1 manifest but not yet indexed (not on disk) |

## Post-#560 honest baseline (2026-05-19)

#560 flattened the synthetic `kind: "relationship"` container EntityRecords
emitted by the 4 cross-extractors (imports, httpclient, hierarchy, manifest)
into edges embedded on the existing SCOPE.Component / SCOPE.ExternalAPI
entities. **Bug-rate is unchanged on every repo** because the resolver
disposition logic walks `EntityRecord.Relationships` (which still contains
the same edges) — phantom container entities never contributed to bug
counts. **Entity counts drop** on every repo that uses these extractors,
which is the structural correction (the data-model lie is removed):

| Repo | Pre-#560 ent | Post-#560 ent | Delta | Rel delta |
|---|---:|---:|---:|---:|
| chi | 2,359 | 2,039 | -320 | 0 |
| gin | 6,354 | 5,835 | -519 | 0 |
| express | 2,017 | 1,633 | -384 | 0 |
| spdlog | 1,772 | 1,770 | -2 | 0 |
| nextjs-commerce | 879 | 713 | -166 | 0 |
| nestjs-starter | 71 | 52 | -19 | 0 |
| play-scala-starter | 256 | 251 | -5 | 0 |
| kafka-streams-examples | 2,884 | 2,522 | -362 | 0 |
| vapor-api-template | 60 | 60 | 0 | 0 |
| http.zig | 889 | 889 | 0 | 0 |
| terraform-aws-vpc | 2,403 | 2,403 | 0 | 0 |
| django-realworld | 690 | 563 | -127 | 0 |
| flask-realworld | 917 | 815 | -102 | 0 |
| click | 5,019 | 4,597 | -422 | 0 |
| requests | 22,218 | 21,902 | -316 | 0 |
| client-fixture-a | 10,565 | 9,118 | -1,447 | 0 |
| client-fixture-b | 15,884 | 12,647 | -3,237 | 0 |
| client-fixture-c | 8,980 | 7,361 | -1,619 | 0 |

Zero edges lost on any repo (rel delta = 0 everywhere). The
`Latest bug-rate` columns in the ledger below are valid as-is post-#560
and need no row-by-row rewrite; they are the post-#560 honest baseline
going forward.

## Sources of truth

- Latest aggregate measurement: `docs/verify2/quick-tier1-baseline-refresh-2026-05-19-v3.md` (40 repos, post-determinism #486, includes #474-#483 chain-fixes — **reliable single-shot**)
- Prior aggregate: `docs/verify2/quick-tier1-baseline-refresh-2026-05-19-v2.md` (40 repos, post wave-1+2, pre #474-#483; noisy)
- Prior aggregate: `docs/verify2/quick-tier1-baseline-2026-05-19.md` (40 repos, baseline before any wave)
- Ship-gate v4: `docs/verify2/ship-gate-baseline-refresh-v4.md` (32-repo intersection, pre-quick-tier1)
- Wave-1+2 fix PRs: #466 #467 #468 #469 #470 #471 #472 #473
- Wave-3 chain-fix PRs (merged on `main` but not yet re-measured in v2 doc): #474 #475 #476 #477 #478 #480 #483

## Ledger

(Bug-rate dates: `v3` = 2026-05-19 quick-tier1 refresh v3 (post-determinism #486 — reliable single-shot).
`v2` = 2026-05-19 quick-tier1 refresh v2 (noisy, pre-determinism). `v4` = 2026-05-18 ship-gate v4.
PR# in the Latest column means "post-#NNN re-measurement reported in the PR thread," not yet
folded into an aggregate baseline doc.)

| Repo | Lang | Files | Latest bug-rate (date, source) | Targeting PR | Residual root cause | Status | Blocker / next fix |
|---|---|---:|---|---|---|---|---|
| aspnetcore-docs-samples | razor | 2,674 | 6.18% (2026-05-19, v3) | #473 | clean | at-bar | next razor wave for ship-gate gap |
| tide | fish | 130 | 9.02% (2026-05-19, v3) | — | fish-shell extractor untouched | structural | file fish-extractor issue |
| just | just | 290 | 17.34% (2026-05-19, v3) | — | just-lang extractor untouched | structural | file just-extractor issue |
| http.zig | zig | 36 | 11.53% (2026-05-19, post-wave-3) | wave-3 (zigBareNames) | residual: bug-resolver ambig-bare-hint-fail floor (319/3748 = 8.51% — local-graph dup `init`/`deinit`/`free`/`close` across multiple structs; needs receiver-variable-type-tracking like Go); + 51 IMPORTS dotted-lower-head (file-relative `@import("./foo.zig")` not bound to file entities) | at-bar | file `zig-receiver-variable-type-tracking` + `zig-imports-file-binding` issues |
| kickstart.nvim | lua | 15 | 9.86% (2026-05-19, v3; v2 was 10.14%) | — | lua regression vs v1 baseline (3.45 to 9.86); transitive change from wave-1+2 added endpoints with new bugs | addressable | file lua-regression investigate issue |
| grpc-go-examples | proto | 203 | 7.04% (2026-05-19, v3; v2 was 10.74%) | #472 then #476 then #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | file `receiver-variable-type-tracking` issue; then re-measure |
| apollo-server | graphql | 293 | 4.74% (2026-05-19, v3) | #470 | clean | at-bar | next graphql wave for ship-gate gap |
| jupyter-notebook | notebook | — | — | — | — | unmeasured | clone + index |
| jaffle_shop | sql_dbt | — | — | — | — | unmeasured | clone + index |
| azure-quickstart-templates | bicep | — | — | — | — | unmeasured | clone + index |
| tilt | starlark | — | — | — | — | unmeasured | clone + index |
| camunda-bpm-examples | java_bpmn | — | — | — | — | unmeasured | clone + index |
| asyncapi-spec | asyncapi | — | — | — | — | unmeasured | clone + index |
| smithy | smithy | — | — | — | — | unmeasured | clone + index |
| avro | avro | — | — | — | — | unmeasured | clone + index |
| thrift | thrift | — | — | — | — | unmeasured | clone + index |
| json-schema-spec | json-schema | — | — | — | — | unmeasured | clone + index |
| raml-spec | raml | — | — | — | — | unmeasured | clone + index |
| api-blueprint | api-blueprint | — | — | — | — | unmeasured | clone + index |
| nginx | nginx-conf | — | — | — | — | unmeasured | clone + index |
| apache-httpd | apache-httpd-conf | — | — | — | — | unmeasured | clone + index |
| caddy | caddyfile | — | — | — | — | unmeasured | clone + index |
| traefik | traefik-dynamic | — | — | — | — | unmeasured | clone + index |
| kong | kong-declarative | — | — | — | — | unmeasured | clone + index |
| envoy | envoy-yaml | — | — | — | — | unmeasured | clone + index |
| haproxy | haproxy-cfg | — | — | — | — | unmeasured | clone + index |
| seleniumhq-examples | multi | — | — | — | — | unmeasured | clone + index |
| requests | python | 111 | 1.51% (2026-05-19, python-django-w4; was 1.54%) | python-django-w4 | clean | at-bar | within striking distance of 1% — push for ship-gate |
| flask-realworld | python | 43 | 7.49% (2026-05-19, post-#526; was 7.49% post-#525, 7.60% python-django-w4) | #526 (python class-attr field entities) — measured no-change (92→92 bug-extractor): flask-realworld uses functional view registration, no DRF ViewSet class-attribute pattern. #525 EXTENDS kind-disambiguator landed prior (2 SurrogatePK-family EXTENDS edges bound). Residual: SQLA `Query.first()` collision-blocked, generic verbs, dotted-receiver class member access | at-bar | dotted-receiver class member binding + ruby-collision-free `first` route (chain-fix) |
| click | python | 138 | 6.45% (2026-05-19, post-#525 confirmed flat; was 6.86%) | python-django-w4 + #525 | resolver tier-1 real-vs-SCOPE preference confirmed flat on click (no Component/SCOPE.Component same-file collisions on EXTENDS). Shared cumulative wins | at-bar | next python wave for ship-gate gap |
| django-realworld | python | — | 4.25% (2026-05-19, post-wave-7; was 4.72% post-wave-6, 7.83% python-django-w4) | wave-7 spillover: -0.47pp (Django test/mgmt base classes, `<Class>.paginate_queryset`/`.get_paginated_response` DRF inherited-method classifier, and new stdlib/3p packages classify Django ORM/DRF residual already present in django-realworld). Residual: Django URLConf binder (#527), generic verbs (#529) | at-bar | #527 URLConf — path to ship-gate |
| pandas | python | 197 | 10.11% (2026-05-19, post-#525 confirmed flat; was 13.85%) | python-django-w4 + #525 | resolver tier-1 real-vs-SCOPE preference confirmed flat on pandas. Wave-4 numpy/pandas bare-names (asarray=196/astype=195/is_list_like=145/find_stack_level=109/arange=80/zeros=74/Timestamp/Timedelta/AbstractMethodError/etc.) — top stubs accumulated 80-200 hits each pre-fix | structural | further pandas wave needed: `view`/`func`/`cls`/`equals`/`reindex`/`op` collision-prone, dotted `type._simple_new` receiver-binding |
| client-fixture-a | python (Django backend, user-provided) | 4202 | 8.32% (2026-05-19, post-wave-7 Django test/management/async + DRF inherited methods; was 9.80% post-wave-6, 12.00% post-#526, 13.70% pre-fixes) | wave-7 python django test/management/channels/stdlib + DRF inherited-method classifier — measured delta: bug-extractor 2619 → 2249 (-370), bug-resolver 418 → 331 (-87), bug_rate 9.799% → 8.325% (-1.474pp). Pass-1 (refs.go pythonExternalBaseTypes: TestCase, APITestCase, BaseCommand/Command, TokenObtainPairView/TokenRefreshView/TokenBlacklistView, AsyncConsumer family, MiddlewareMixin, FormParser, *ModelMixin) → 9.80% → 9.64% (-0.16pp). Pass-2 (synth.go knownExternalPackages: asgiref/channels/botocore/csv/contextvars/decimal/email/random/traceback/importlib/django_celery_beat/django_filters/docx/fitz/pdfplumber/pytz + 40 more django ecosystem pkgs; pythonBareNames: Django ORM Q/Case/When/Value, get_object_or_404, atomic, parse_datetime, BaseCommand handlers, unittest assert* family, pandas read_excel/iterrows/fillna, celery delay, boto3 SQS verbs, getenv) → 9.64% → 8.72% (-0.92pp). Pass-3 (refs.go pythonExternalInheritedMethods classifier: routes `<Class>.get_paginated_response` / `.paginate_queryset` / `.get_serializer` / `.get_object` / etc. — 25 DRF GenericAPIView inherited methods + channels lifecycle + BaseCommand lifecycle — to ExternalKnown when leaf method matches) → 8.72% → 8.32% (-0.40pp). Residual: project-internal user methods (`update_or_create_devices`, `sync_users`, `setMessageParams`, `get_safety_filings`, `_with_group_request`, `replace_email_variables`) that require receiver-variable-type tracking; generic verbs (`replace`, `pop`, `items`, `append`, `info`, `warning`, `write`, `read`) explicitly excluded per #94 safer-bias rule. | at-bar (≤8.5%, within striking distance of 8% floor) | Receiver-variable-type tracking (#494 / #543) for user-defined CALLS dominates remaining residual; generic verbs intentionally not allowlisted |
| gin | go | 121 | 6.17% (2026-05-19, v3; v2 was 8.63%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue |
| chi | go | 93 | 4.80% (2026-05-19, v3; v2 was 8.50%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue; ship-gate gap remains |
| etcd | go | 424 | 8.62% (2026-05-19, v3; v2 was 12.40%) | #480 then #483 | bare receiver variable names + dotted Format-B with local-var scope names | upstream | file `receiver-variable-type-tracking` issue (separate, multi-day) — 0.62 pp away from bar |
| express-realworld | javascript | 66 | 9.83% (2026-05-19, v3) | — | javascript extractor not targeted in wave-1+2 | structural | file js-fix-wave issue |
| express | javascript | 145 | 4.03% (2026-05-19, ts-framework-w4; pre-wave-4 13.76%) | ts-framework-w4 | Node stdlib + EventEmitter + assert + fs/path receiver-strip allowlist; `scope:component:import:external:<pkg>` synth folding; node:<mod> direct allowlist. Residual: express HTTP DSL (`app.get`/`post`/`status`/`end`) receiver-stripped to collision-prone bare names (rejected by #104) + `callback`/`request`/`done` test-helper names. Chain-fix: file `js-express-dsl-allowlist` (route-dsl with framework gate) issue. | at-bar | next js wave (express + node http DSL receiver-binding) |
| nestjs-starter | typescript | 16 | 1.75% (2026-05-19, ts-framework-w4; pre-wave-4 16.67%) | ts-framework-w4 | NestJS DI graph residual: 1 `bootstrap.listen` bare name (server.listen receiver-strip not folded). All `scope:component:import:external:@nestjs/*` structural-refs now route via the new synth.go branch. | at-bar | 0.75pp from ship-gate; file `ts-nest-receiver-binding` follow-up |
| client-fixture-c | typescript | 538 | 11.36% (2026-05-19, rn-expo-w4 #508; pre-fix 16.10%) | rn-expo-w4 #508 | RN/Expo runtime allowlist landed: `knownExternalPackages` extended with Expo SDK (`expo-*`, `@expo`, `@expo-google-fonts`), React Native + community packages (`react-native`, `@react-native`, `@react-native-community`, plus 30+ `react-native-*` libs), React Navigation (`@react-navigation`), Reanimated/Gesture-Handler/Safe-Area-Context/Screens/SVG/MMKV/Keychain, NativeWind, Metro, EAS, Gluestack UI (`@gluestack-ui`, `@gluestack-style`), lucide-react-native, @legendapp, aws-amplify. `jsBareNames` extended with TanStack Query hooks (`useQuery`/`useMutation`/`useQueryClient`/`invalidateQueries`/`setQueryData`/`refetch`/`mutate`/`mutateAsync`), React Navigation hooks (`useNavigation`/`useRoute`/`useFocusEffect`/`usePreventRemove`/`useTheme`), Expo Router hooks (`useLocalSearchParams`/`useSegments`/`useRootNavigationState`), RN core hooks (`useColorScheme`/`useWindowDimensions`/`useSafeAreaInsets`/`useBottomTabBarHeight`/`useHeaderHeight`), Reanimated hooks (`useSharedValue`/`useAnimatedStyle`/`runOnUI`/`withTiming`/...), Zustand (`useShallow`/`getState`), navigation API (`setOptions`/`navigate`/`pop`), RN Linking (`openURL`/`canOpenURL`/`openSettings`), chalk color receiver-strip (`gray`/`red`/`green`/...), String/Number proto (`padStart`/`padEnd`/`startsWith`/`endsWith`/`localeCompare`/`lastIndexOf`/`toFixed`/`toPrecision`). 3 diagnostic passes; cumulative -4.74pp. Pass-1 (packages) -2.16pp, pass-2 (RN/Query/Nav/Reanimated hooks) -2.19pp, pass-3 (setOptions/openURL/getState/chalk/String-proto) -0.39pp. Residual: 1147/1154 bug-resolver `Component,SCOPE.Component` are `@/...` tsconfig path-aliases (defer to #505 in flight); ~250 bug-extractor are user-defined local zustand-store hooks (`useSyncQueueStore`, `useAuthStore`, `setSaveDialog*`) not being lifted to local entities by the TS extractor + `#104`-rejected names (`find`/`forEach`/`reduce`/`replace`/`includes`/`delete`/`create`/`match`). | addressable | (a) #505 path-alias resolution (in flight) — will eliminate the 1147 bug-resolver residual; (b) file `ts-zustand-store-hook-lift` chain-fix (extractor: lift `export const useStore = create<...>(...)` zustand pattern to a local entity). |
| nextjs-commerce | typescript | 76 | 3.89% (2026-05-19, ts-framework-w4; pre-wave-4 17.14%) | ts-framework-w4 | React core hooks + Next.js navigation/cache/RSC primitives + Date/Intl/DOM receiver-strip allowlist; jsDynamicPatterns for relative + tsconfig-baseUrl-path imports; scoped+unscoped npm pkgs (`@heroicons`, `@vercel`, `clsx`, `tailwind-merge`, `geist`, `sonner`, `swr`, `zustand`, ...). Residual: React `useState` state-setter calls (`setIsOpen`, `setActive`, `setOpenSelect`, `updateOptimisticCart`, ...) that the TS extractor doesn't lift to local entities + Array/String prototype methods on #104 rejection list (`find`/`includes`/`replace`/`forEach`/`startsWith`/`endsWith`) + `cookies`/`headers` claimed by swiftBareNames/kotlinBareNames (cross-lang invariant test forbids JS-side duplication). | at-bar | next ts wave (useState destructure → setter entity lift; cross-lang invariant relaxation for `cookies`/`headers` per-lang gate) |
| spring-petclinic | java | 120 | 8.34% (2026-05-19, v3; v2 was 8.45%) | — | java extractor not targeted in wave-1+2; just above bar | addressable | first java wave — close to bar |
| kafka-streams-examples | java | 172 | 3.81% (2026-05-19, kafka-fix-w3; pre-fix 22.19%) | kafka-fix-w3 | Apache Kafka / Confluent / Avro / Jetty / Jersey / Guava / RocksDB roots added; Java/Kotlin import-leaf bare-name folding; java.lang receiver-type fold; Kafka-Streams DSL + commons-cli + JAX-RS bare-name allowlists (import-gated). Residual ~85% is user-defined static helpers (`buildPropertiesFromConfigFile`, `getWithRetries`, `sendOrders`, ...) requiring cross-class receiver binding | at-bar | resolved under wave-3 chain-fix; cross-class receiver binding follow-up |
| exposed | kotlin | 115 | 11.00% (2026-05-19, v3; v2 was 8.56% — REGRESSION vs v2 noisy baseline, but v3 single-shot trustworthy) | #471 then #477 | Kotlin DSL receivers beyond Ktor Routing (Exposed SQL DSL) — back above bar | addressable | next Kotlin wave (Exposed/coroutine DSL receivers) |
| ktor-samples | kotlin | 509 | 6.29% (2026-05-19, v3; v2 was 10.40%) | #471 then #477 | residual under bar — wave-3 chain-fix folded in | at-bar | next kotlin wave for ship-gate gap |
| play-scala-starter | scala | 37 | 2.82% (2026-05-19, scala-imports-resolver PR; was 7.75%) | #469 | scala arm added to modulesForFile (sbt + Play `app/` source roots); same-file framework-projection dedup (Play YAML rules emit a `Controller` alias for each SCOPE.Component class) extended from PHP #485 wave-3. 6 of 9 project-local IMPORTS now bind to their declaring SCOPE.Component. Residual: 1 Twirl `.scala.html` template misclassified by the Scala extractor (file as chain-fix), 2 bare-name CALLS (`success` on Promise, `Action.async` Play method) + 1 cross-class receiver call (`counter.nextCount()`) — receiver-typed CALLS binding for scala out of scope | at-bar | next scala wave for ship-gate gap (≤1%) — needs Twirl extractor split + scala bare-CALLS receiver binding |
| usermanager-example | clojure | 17 | 19.74% (2026-05-19, v3) | — | clojure extractor untouched | structural | file clojure-extractor issue |
| rails-realworld | ruby | 105 | 6.65% (2026-05-19, v3) | — | clean | at-bar | next ruby wave for ship-gate gap |
| sidekiq | ruby | 85 | 13.47% (2026-05-19, v3) | — | ruby extractor not targeted in wave-1+2 | structural | file ruby-fix-wave issue |
| laravel-quickstart | php | 83 | 7.33% (2026-05-19, php wave-3 PR; was 24.08%) | #485 wave-3 | residual is 3 bug-extractor edges: 2 internal facade-style dotted CALLS (AuthServiceProvider.registerPolicies, Schema column `index` modifier), 1 inferred-from-class-hierarchy phantom — bare-leaf method-rebind out of scope | at-bar | next php wave for ship-gate gap (≤1%) — needs receiver type inference for `$model->save()` |
| symfony-demo | php | 241 | 8.91% (2026-05-19, php wave-3 PR; was 23.02%) | #485 wave-3 | residual is entity getter / setter chains (`getId`, `getUsername`, `validateUsername` on `$this->validator`) — receiver type inference required; also a handful of leaked twig/JS calls (`querySelector`, `setAttribute`) misclassified as php | at-bar | next php wave for ship-gate gap (≤1%) — needs receiver type inference + twig/JS extractor lang-attribution fix |
| mini-redis | rust | 33 | 14.85% (2026-05-19, v3) | — | rust extractor not targeted in wave-1+2 | structural | file rust-fix-wave issue |
| actix-examples | rust | 460 | 18.75% (2026-05-19, v3) | — | rust extractor not targeted in wave-1+2 | structural | file rust-fix-wave issue |
| vapor-api-template | swift | 21 | 2.13% (2026-05-19, post-wave-4 swift external-known refresh) | chain-fix #491 (looksLikeSourceFilePath basename-only) + #492 (swift import-extractor namespaces SCOPE.Component carrier as `<file>::import::<module>` and tags Subtype="module", eliminating the `App` collision) + wave-4 swift external-known refresh (SwiftNIO sister modules + Apple SSWG packages + Vapor sister kits + swift import-attribute strip in classifyExternal) | flat at 2.13% — the 2 residual bugs are the `App` SwiftPM target-dependency IMPORTS edges (need Package.swift target-extractor). Wave-4 swift external-known additions did not surface any new resolutions here because the residual is structural, not allowlist-driven. | at-bar | ship a SwiftPM target-extractor for `Package.swift` so the `App` target declares a SCOPE.Component the import binds to → drives bug-rate to 0%. |
| sample-food-truck | swift | — | — | — | — | unmeasured | clone + index |
| vapor | swift | ~250 | 8.93% (2026-05-19, post-#499 swift extractor noise filter) — was 9.26% pre-fix | chain-fix #499 (swift extractor): (a) `extractImportPath` now skips `modifiers`/`attribute`/`attributes` subtrees — synthetic dotted import paths like `_documentation.visibility.internal.Foundation` no longer reach `classifyExternal`; the synth-side prefix-strip in `classifyExternal` is now redundant but kept as a belt-and-braces guard. (b) `extractCallRelationships` now filters Swift statement keywords (`defer`, `repeat`, `do`) and bare-receiver `init` from the CALLS emission path; `Type.init(...)` is preserved via the recvRoot != "" gate so explicit initializer calls keep their receiver_type property. Measured delta: bug-extractor 379 → 359 (-20); bug-resolver 85 → 85 (flat); resolved 3070 → 3089 (+19); total bugs 464 → 444 (-20); net bug-rate -0.33pp. Regression check on chi/express/flask/spdlog/vapor-api-template: 0.00pp on all five (perfect non-swift control). Earlier wave-4 swift external-known refresh: (a) extend `knownExternalPackages` with the SwiftNIO sister modules (`NIOPosix`, `NIOConcurrencyHelpers`, `NIOSSL`, `NIOExtras`, `NIOWebSocket`, `NIOTransportServices`, `NIOEmbedded`, `NIOHTTPCompression`, `_NIOFileSystem`, `_NIOFileSystemFoundationCompat`, `CNIOLinux`/`Darwin`/`Posix`/`Atomics`), Apple SSWG packages (`_CryptoExtras`, `AsyncKit`, `AsyncHTTPClient`, `ServiceLifecycle`, `Metrics`, `Atomics`, `Algorithms`, `SystemPackage`, `ArgumentParser`, `ServiceContextModule`, `SwiftASN1`), Vapor sister kits (`RoutingKit`, `ConsoleKit{,Terminal,Commands}`, `MultipartKit`, `WebSocketKit`, `CVaporBcrypt`), and platform shims (`Glibc`, `Musl`, `Android`, `Darwin`, `Dispatch`, `WinSDK`, `X509`); (b) swift-gated attribute-prefix strip in `classifyExternal` for `@_documentation(visibility:...)` / `@_exported` / `@preconcurrency` / `@_implementationOnly` / `@testable` import shapes; (c) extend `swiftBareNames` with NIO Channel API verbs (`fireChannelRead`, `wrapOutboundOut`, `unwrapInboundIn`, `writeAndFlush`, `addHandler`, `runIfActive`, `flatMapErrorThrowing`, `moveReaderIndex`, etc.) + Foundation Codable container types (`UnkeyedContainer`, `SingleValueContainer`) + NIO HTTP codec types. bug-extractor 627 → 379 (-248); bug-resolver 88 → 85 (-3); external-known 291 → 431 (+140); external-unknown 527 → 638 (+111). Net -252 bugs / -5.01pp. Generic verbs (`defer`, `init`, `storage`, `contains`, `read`, `write`, `succeed`, `fail`, `validate`, `serialize`, `closure`, `cache`, `sessions`, etc.) deliberately OMITTED per safer-bias (#94/#105/#106) — they collide with user methods. | addressable | #499 landed (extractor noise filter). Residual 8.93% above ship-gate is structural and lives upstream: (a) #494 receiver-type tracking — local variable type inference (`let svc = MyService(); svc.foo()` cannot resolve `foo` because we only attach receiver_type for declared class fields); (b) bug-resolver floor (~85 edges): ambiguous locally-defined user methods like `validate`/`createSession`/`deleteSession` resolved against multiple same-named candidates — needs cross-file disambiguation pass; (c) remaining bug-extractor edges (~150) are mostly Foundation/NIO generic verbs (`flatMap`, `map`, `then`, etc.) that the safer-bias filter deliberately keeps off the external-known allowlist. |
| aspnetcore-realworld | csharp | 97 | 9.82% (2026-05-19, v3) | #473 | razor/csharp fix improved but residual cs-specific identifier resolution remains | addressable | next csharp wave |
| spdlog | cpp | 175 | 6.95% (2026-05-19, v3) | #468 | clean | at-bar | next cpp wave for ship-gate gap |
| esp-idf | c | — | — | — | — | unmeasured | clone + index |
| flutter-samples | dart | — | — | — | — | unmeasured | clone + index |
| phoenix-todo-list | elixir | 69 | 9.38% (2026-05-19, v3) | — | elixir extractor not targeted in wave-1+2 | addressable | next elixir wave (close to bar) |
| microblog | python | — | — | — | — | unmeasured | clone + index |
| fastapi-realworld | python | — | — | — | — | unmeasured | clone + index |
| golang-gin-realworld | go | — | — | — | — | unmeasured | clone + index |
| actix-diesel-realworld | rust | — | — | — | — | unmeasured | clone + index |
| nestjs-realworld-typeorm | typescript | — | — | — | — | unmeasured | clone + index |
| joal | java | — | — | — | — | unmeasured | clone + index |
| jpetstore-6 | java | — | — | — | — | unmeasured | clone + index |
| ent | go | — | — | — | — | unmeasured | clone + index |
| sqlc-examples | go | — | — | — | — | unmeasured | clone + index |
| netcore-boilerplate | csharp | — | — | — | — | unmeasured | clone + index |
| tokio | rust | 389 | 16.04% (2026-05-19, v3) | — | rust extractor not targeted in wave-1+2 | structural | file rust-fix-wave issue |
| pnpm | javascript | — | — | — | — | unmeasured | clone + index |
| bazel | java | — | — | — | — | unmeasured | clone + index |
| cmake | cpp | — | — | — | — | unmeasured | clone + index |
| mongoose | javascript | — | — | — | — | unmeasured | clone + index |
| mongo-go-driver | go | — | — | — | — | unmeasured | clone + index |
| redis-py | python | — | — | — | — | unmeasured | clone + index |
| cassandra-java-driver | java | — | — | — | — | unmeasured | clone + index |
| aws-sdk-go-v2 | go | — | — | — | — | unmeasured | clone + index |
| rabbitmq-tutorials | python | — | — | — | — | unmeasured | clone + index |
| aws-cdk-examples-typescript | typescript | — | — | — | — | unmeasured | clone + index |
| pulumi-examples-go | go | — | — | — | — | unmeasured | clone + index |
| aws-cloudformation-samples | yaml | — | — | — | — | unmeasured | clone + index |
| aws-sam-cli-app-templates | yaml | — | — | — | — | unmeasured | clone + index |
| serverless-examples | yaml | — | — | — | — | unmeasured | clone + index |
| crossplane | yaml | — | — | — | — | unmeasured | clone + index |
| ansible-for-devops | yaml | — | — | — | — | unmeasured | clone + index |
| nomad-pack | hcl | — | — | — | — | unmeasured | clone + index |
| terraform-aws-vpc | hcl | 105 | 6.34% (2026-05-19, v3) | #466 then #474 | residual: README markdown extraction artifacts (sibling-dir ambiguous basenames) | at-bar | next hcl/markdown wave for ship-gate gap |
| argocd-example-apps | yaml | 91 | 0.00% (2026-05-19, v3; v2 was 16.01%) | #467 then #474 then #478 | clean | at-ship-gate | maintenance |
| prometheus-helm | yaml | 52 | 0.00% (2026-05-19, v3) | — | clean | at-ship-gate | maintenance |
| starter-workflows | yaml | 514 | 0.55% (2026-05-19, v3; v2 was 11.89%) | #467 then #475 then #478 | clean | at-ship-gate | maintenance |
| openapi-stripe | yaml | 5 | 0.00% (2026-05-19, v3) | — | clean | at-ship-gate | maintenance |
| gitlab-runner | yaml | — | — | — | — | unmeasured | clone + index |
| circleci-demo-python-django | yaml | — | — | — | — | unmeasured | clone + index |
| jenkins | groovy | — | — | — | — | unmeasured | clone + index |
| tektoncd-pipeline | yaml | — | — | — | — | unmeasured | clone + index |
| alembic | python | — | — | — | — | unmeasured | clone + index |
| ios-oss | swift | — | — | — | — | unmeasured | clone + index |
| android-architecture | java | — | — | — | — | unmeasured | clone + index |
| compose-samples | kotlin | — | — | — | — | unmeasured | clone + index |
| EntityComponentSystemSamples | csharp | — | — | — | — | unmeasured | clone + index |
| zod | typescript | — | — | — | — | unmeasured | clone + index |
| pydantic | python | — | — | — | — | unmeasured | clone + index |
| aws-lambda-python-runtime-interface-client | python | — | — | — | — | unmeasured | clone + index |
| cloudflare-workers-sdk | typescript | — | — | — | — | unmeasured | clone + index |
| xstate | typescript | — | — | — | — | unmeasured | clone + index |
| hugoDocs | go | — | — | — | — | unmeasured | clone + index |
| sphinx | python | — | — | — | — | unmeasured | clone + index |
| pytest | python | — | — | — | — | unmeasured | clone + index |
| socket.io | typescript | — | — | — | — | unmeasured | clone + index |
| airflow | python | — | — | — | — | unmeasured | clone + index |
| spark | scala | — | — | — | — | unmeasured | clone + index |
| angular-realworld | typescript | — | — | — | — | unmeasured | clone + index |
| sveltekit | typescript | — | — | — | — | unmeasured | clone + index |
| axum | rust | — | — | — | — | unmeasured | clone + index |
| phoenix-live-view | elixir | — | — | — | — | unmeasured | clone + index |
| http4k | kotlin | — | — | — | — | unmeasured | clone + index |

## User-test repos (out-of-corpus snapshots — not part of tier-1)

These are real production codebases the user supplied as private snapshots
under (private fixture, path redacted). They are not in
the verify2 corpus list and are NOT counted in the status roll-up below.
Recorded here so #505 acceptance numbers (and any future
private-codebase chain-fix) have a stable measurement-history anchor.

| Repo | Stack | Files (~) | Latest bug-rate | Last fix PR | Residual root cause | Status | Blocker / next fix |
|---|---|---:|---:|---|---|---|---|
| client-fixture-c | typescript (RN/Expo + Metro + tsconfig paths) | ~538 | **3.99% (2026-05-19, post-#522)** — was 9.73% post-#505 baseline on rebased main, 20.28% pre-#505 | #522 | #522 landed: JS/TS extractor now emits SCOPE.Component / SCOPE.Operation entities for every `export const X = <expr>` shape (objects, instances, hook aliases, reducers, React wrappers via forwardRef/memo/observer/connect/styled). +2701 entities (6279 → 8980). bug-extractor 1622 → 638 (-984). Residual: bug-resolver edges climbed slightly (7 → 52) because alias-resolved IMPORTS now have targets to bind to but the resolver picks the bare leaf when multiple candidate files declare same-named consts — addressable via cross-file disambiguation pass. | at-ship-gate | residual ~3.99% is below 8% bar and approaching ≤1% ship-gate. Remaining work: (a) bug-resolver disambiguation when same-named const is exported from multiple files; (b) Gluestack/Expo deep-path allowlist refresh (still relevant for the external-known column). |
| client-fixture-b | javascript (Vite + React) | ~659 | **12.10% (2026-05-19, post-#522)** — was 13.23% on rebased main, 16.07% on pre-rebase worktree base | #522 | #522 landed: JS extractor now emits entities for `export const X = <expr>` shapes. +6222 entities (9662 → 15884). bug-extractor 4848 → 4213 (-635). Residual is now dominated by orthogonal bug classes, not const exports: (a) `fetchAuthSession` (371 edges) — AWS Amplify named imports not in external-known allowlist; (b) `then`/`includes`/`find`/`forEach` — Promise + Array prototype methods (per #104 safer-bias deliberately omitted); (c) `useSelector` (141 unprefixed) — react-redux named import not externalized; (d) `setFieldsValue`/`validateFields`/`success` — antd form-instance method calls on dynamic-typed instances. None of these are const-export shapes. | addressable | chain-fixes filed: (1) AWS Amplify external-known allowlist refresh; (2) react-redux named-import externalization gap; (3) antd form-instance receiver typing. Note that #522 yields proportionally less here than on client-fixture-c because frontend has many more downstream issues that aren't const-export-related. |

## Status roll-up (v3 refresh 2026-05-19)

| Status | Count |
|---|---:|
| at-ship-gate | 4 |
| at-bar | 16 |
| addressable | 6 |
| structural | 13 |
| upstream | 1 |
| unmeasured | 75 |
| **total tier-1 repos** | **115** |

Notes:
- 4 ship-gate (argocd-example-apps, starter-workflows, prometheus-helm, openapi-stripe) — argocd + starter-workflows now folded into the aggregate baseline.
- 16 at-bar (was 10 at v2): added chi, gin, grpc-go-examples, ktor-samples, terraform-aws-vpc (chain-fixed and folded), play-scala-starter (promoted from addressable).
- 1 upstream (etcd): 0.62 pp from bar but waiting on receiver-variable-type-tracking primitive.
- exposed moved addressable -> addressable but BACK ABOVE bar (8.56 -> 11.00) — v2 number was a noisy underestimate; v3 single-shot trustworthy. Treat as not-yet-at-bar.

## Next-wave candidates (filter: status in {at-bar, addressable}, sorted by bug-rate desc, v3 numbers)

| Rank | Repo | Lang | Bug-rate | Why |
|---:|---|---|---:|---|
| 1 | nextjs-commerce | typescript | 3.89% (was 17.14% pre-wave-4) | useState destructure → state-setter entity lift in TS extractor |
| 2 | nestjs-starter | typescript | 1.75% (was 16.67% pre-wave-4) | 0.75pp from ship-gate — `bootstrap.listen` receiver-strip |
| 3 | exposed | kotlin | 11.00% | Kotlin DSL receivers beyond Ktor Routing (v3 reveals v2 was noisy under-read) |
| 4 | kickstart.nvim | lua | 9.86% | lua regression vs v1 — investigate transitive cause |
| 5 | aspnetcore-realworld | csharp | 9.82% | next csharp wave (one-step from bar) |
| 6 | phoenix-todo-list | elixir | 9.38% | first elixir wave, very close to bar |
| 7 | spring-petclinic | java | 8.34% | first java wave — within striking distance of bar |
| 8 | etcd | go | 8.62% | upstream — receiver-variable-type primitive will unblock |

`structural` rows (rust, php, java, ruby, python, swift, zig, just, fish, clojure)
are higher-bug-rate but each requires a dedicated multi-day extractor wave —
prioritise via the JIRA backlog, not this ledger.

forbidden-term grep: clean
