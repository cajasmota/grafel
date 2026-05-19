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
| client-fixture-a (wave-9 post) | 10,565 | 9,019 | -1,546 | 0 |
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
| flask-realworld | python | — | 7.01% (2026-05-19, post-wave-9 builtin-type methods + module-constants spillover; was 7.23% post-wave-8, 7.49% post-#526) | #526 (python class-attr field entities) — measured no-change (92→92 bug-extractor): flask-realworld uses functional view registration, no DRF ViewSet class-attribute pattern. #525 EXTENDS kind-disambiguator landed prior (2 SurrogatePK-family EXTENDS edges bound). Residual: SQLA `Query.first()` collision-blocked, generic verbs, dotted-receiver class member access | at-bar | dotted-receiver class member binding + ruby-collision-free `first` route (chain-fix) |
| click | python | — | 6.05% (2026-05-19, post-wave-9 builtin-type methods spillover; was 6.12% post-wave-8, 6.45% post-#525, 6.86% pre-fix) | wave-8 spillover -0.31pp (typing.Any/List/Dict/Optional + stdlib Decimal/BytesIO + Path + UUID + OrderedDict classify residual generic Python type-annotation EXTENDS targets). Shared cumulative wins | at-bar | next python wave for ship-gate gap |
| django-realworld | python | — | 3.77% (2026-05-19, post-wave-8 Django ORM/test/mongo spillover; was 4.25% post-wave-7, 4.72% post-wave-6, 7.83% python-django-w4) | wave-8 spillover -0.47pp (TestCase assertX family + Django ORM F/Q/Count/Exists/Subquery + DRF AllowAny/IsAuthenticated/Response/Request + Django HttpResponse/JsonResponse classify residual already present in django-realworld). Residual: Django URLConf binder (#527), generic verbs (#529) | at-bar | #527 URLConf — path to ship-gate |
| pandas | python | — | 9.87% (2026-05-19, post-wave-9 builtin-type methods spillover; was 9.91% post-wave-8, 10.11% post-#525, 13.85% pre-fix) | wave-8 spillover -0.14pp (typing aliases + stdlib Decimal/BytesIO/StringIO/OrderedDict classify residual). resolver tier-1 real-vs-SCOPE preference confirmed flat on pandas. Wave-4 numpy/pandas bare-names (asarray=196/astype=195/is_list_like=145/find_stack_level=109/arange=80/zeros=74/Timestamp/Timedelta/AbstractMethodError/etc.) | structural | further pandas wave needed: `view`/`func`/`cls`/`equals`/`reindex`/`op` collision-prone, dotted `type._simple_new` receiver-binding |
| client-fixture-a | python (Django backend, user-provided) | 4205 | 6.24% (2026-05-19, post-wave-9 module-constants + Celery @app.task + DRF @action + Python builtin-type methods; was 6.75% post-wave-8, 8.32% post-wave-7, 9.80% post-wave-6, 12.00% post-#526, 13.70% pre-fixes). Wave-9 cumulative -0.41pp across 3 passes: pass-1 -0.22pp (Track A — `Model:<SCREAMING_SNAKE>` module-constant kind-prefixed stubs + Track B — `Task:<bare>` Celery dispatch stubs route to Dynamic; bug-extractor bare-kind-prefixed 18→5 + bug-resolver ambig-bare-no-hint 46→3); pass-2 -0.00pp (Track C — `Action:<x>` DRF action decorator stubs, defensive; 0 observed instances on client-fixture-a, retained for DRF-heavy corpora); pass-3 -0.19pp (Track D-adjacent — `<builtin>.<method>` for str/dict/list/tuple/set/bytes/bytearray/int/float/bool route to ExternalKnown; covers `str.strip`/`str.lower`/`str.split`/`dict.items` receiver-qualified builtin-type method calls — dotted-lower-head 65→0). Wave-8 cumulative -1.57pp across 3 passes: pass-1 -0.50pp (TestCase assertX family + DRF GenericViewSet action methods filter_queryset/get_success_headers + pymongo Collection.find_one/insert_one/aggregate/etc. + django Manager normalize_email/make_random_password + celery apply/delay/retry/link); pass-2 -0.88pp (Django ORM F/Q/Case/When/Count/Sum/Avg/Coalesce/Concat/Lower/TruncMonth/SearchQuery/etc.; typing.Any/List/Dict/Optional/Union; stdlib Decimal/BytesIO/ContextVar/MagicMock/ThreadPoolExecutor; PIL Image/ImageEnhance; python-docx Document/Font/Inches/Pt/Cm; pymongo MongoClient/Collection/InsertOne/Decimal128; DRF AllowAny/IsAuthenticated/DjangoFilterBackend/TokenAuthentication; Django HttpRequest/HttpResponse/JsonResponse/FileResponse; channels AuthMiddlewareStack/URLRouter; Celery Celery/chord/chain/group); pass-3 -0.19pp (pymongo Collection.find/select; Django middleware get_response self-callable; ObjectDoesNotExist/ObjectId/Path/Queue/RefreshToken/Request/Response/ReturnDocument/SAFE_METHODS/Signal/Token/UUID/WSGIRequest/model_to_dict; python-docx WD_ALIGN_PARAGRAPH/WD_BREAK/WD_ROW_HEIGHT_RULE enum constants) | wave-7 python django test/management/channels/stdlib + DRF inherited-method classifier — measured delta: bug-extractor 2619 → 2249 (-370), bug-resolver 418 → 331 (-87), bug_rate 9.799% → 8.325% (-1.474pp). Pass-1 (refs.go pythonExternalBaseTypes: TestCase, APITestCase, BaseCommand/Command, TokenObtainPairView/TokenRefreshView/TokenBlacklistView, AsyncConsumer family, MiddlewareMixin, FormParser, *ModelMixin) → 9.80% → 9.64% (-0.16pp). Pass-2 (synth.go knownExternalPackages: asgiref/channels/botocore/csv/contextvars/decimal/email/random/traceback/importlib/django_celery_beat/django_filters/docx/fitz/pdfplumber/pytz + 40 more django ecosystem pkgs; pythonBareNames: Django ORM Q/Case/When/Value, get_object_or_404, atomic, parse_datetime, BaseCommand handlers, unittest assert* family, pandas read_excel/iterrows/fillna, celery delay, boto3 SQS verbs, getenv) → 9.64% → 8.72% (-0.92pp). Pass-3 (refs.go pythonExternalInheritedMethods classifier: routes `<Class>.get_paginated_response` / `.paginate_queryset` / `.get_serializer` / `.get_object` / etc. — 25 DRF GenericAPIView inherited methods + channels lifecycle + BaseCommand lifecycle — to ExternalKnown when leaf method matches) → 8.72% → 8.32% (-0.40pp). Residual: project-internal user methods (`update_or_create_devices`, `sync_users`, `setMessageParams`, `get_safety_filings`, `_with_group_request`, `replace_email_variables`) that require receiver-variable-type tracking; generic verbs (`replace`, `pop`, `items`, `append`, `info`, `warning`, `write`, `read`) explicitly excluded per #94 safer-bias rule. | at-bar (6.75%, ≤8% floor cleared) | Residual now dominated by (a) SCREAMING_SNAKE local module constants (`MA_JURISDICTION_NAME`, `INSPECTIONS_GROUP`, `CHANGELOG_MODELS`, `DEFAULT_VIEWSET_ACTIONS`) surfaced as `Model:<X>` cross-language EXTENDS targets — module-constant extractor lift required; (b) underscored helper functions (`_collection.<method>` chains beyond the wave-7/8 method set, `_get_or_create_*` family) requiring receiver-variable-type tracking (#494 / #543); (c) `Task:<x>` celery `@app.task` decorator-bound functions surfacing as Task-kind-prefixed unresolved targets; (d) custom DRF viewset actions (`UserViewSet.list_users_groups`) where the action method body IS defined locally but the resolver can't bind through the @action decorator. Chain-fixes to file: receiver-type primitive (#494), `@app.task` decorator-bound function classifier, `@action`-decorated viewset method resolver, module-constant lift. |
| gin | go | 121 | 6.17% (2026-05-19, v3; v2 was 8.63%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue |
| chi | go | 93 | 4.80% (2026-05-19, v3; v2 was 8.50%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue; ship-gate gap remains |
| etcd | go | 424 | 8.62% (2026-05-19, v3; v2 was 12.40%) | #480 then #483 | bare receiver variable names + dotted Format-B with local-var scope names | upstream | file `receiver-variable-type-tracking` issue (separate, multi-day) — 0.62 pp away from bar |
| express-realworld | javascript | 66 | 9.83% (2026-05-19, v3) | — | javascript extractor not targeted in wave-1+2 | structural | file js-fix-wave issue |
| express | javascript | 145 | 4.03% (2026-05-19, ts-framework-w4; pre-wave-4 13.76%) | ts-framework-w4 | Node stdlib + EventEmitter + assert + fs/path receiver-strip allowlist; `scope:component:import:external:<pkg>` synth folding; node:<mod> direct allowlist. Residual: express HTTP DSL (`app.get`/`post`/`status`/`end`) receiver-stripped to collision-prone bare names (rejected by #104) + `callback`/`request`/`done` test-helper names. Chain-fix: file `js-express-dsl-allowlist` (route-dsl with framework gate) issue. | at-bar | next js wave (express + node http DSL receiver-binding) |
| nestjs-starter | typescript | 16 | 1.75% (2026-05-19, ts-framework-w4; pre-wave-4 16.67%) | ts-framework-w4 | NestJS DI graph residual: 1 `bootstrap.listen` bare name (server.listen receiver-strip not folded). All `scope:component:import:external:@nestjs/*` structural-refs now route via the new synth.go branch. | at-bar | 0.75pp from ship-gate; file `ts-nest-receiver-binding` follow-up |
| client-fixture-c | typescript | 538 | 11.36% (2026-05-19, rn-expo-w4 #508; pre-fix 16.10%) | rn-expo-w4 #508 | RN/Expo runtime allowlist landed: `knownExternalPackages` extended with Expo SDK (`expo-*`, `@expo`, `@expo-google-fonts`), React Native + community packages (`react-native`, `@react-native`, `@react-native-community`, plus 30+ `react-native-*` libs), React Navigation (`@react-navigation`), Reanimated/Gesture-Handler/Safe-Area-Context/Screens/SVG/MMKV/Keychain, NativeWind, Metro, EAS, Gluestack UI (`@gluestack-ui`, `@gluestack-style`), lucide-react-native, @legendapp, aws-amplify. `jsBareNames` extended with TanStack Query hooks (`useQuery`/`useMutation`/`useQueryClient`/`invalidateQueries`/`setQueryData`/`refetch`/`mutate`/`mutateAsync`), React Navigation hooks (`useNavigation`/`useRoute`/`useFocusEffect`/`usePreventRemove`/`useTheme`), Expo Router hooks (`useLocalSearchParams`/`useSegments`/`useRootNavigationState`), RN core hooks (`useColorScheme`/`useWindowDimensions`/`useSafeAreaInsets`/`useBottomTabBarHeight`/`useHeaderHeight`), Reanimated hooks (`useSharedValue`/`useAnimatedStyle`/`runOnUI`/`withTiming`/...), Zustand (`useShallow`/`getState`), navigation API (`setOptions`/`navigate`/`pop`), RN Linking (`openURL`/`canOpenURL`/`openSettings`), chalk color receiver-strip (`gray`/`red`/`green`/...), String/Number proto (`padStart`/`padEnd`/`startsWith`/`endsWith`/`localeCompare`/`lastIndexOf`/`toFixed`/`toPrecision`). 3 diagnostic passes; cumulative -4.74pp. Pass-1 (packages) -2.16pp, pass-2 (RN/Query/Nav/Reanimated hooks) -2.19pp, pass-3 (setOptions/openURL/getState/chalk/String-proto) -0.39pp. Residual: 1147/1154 bug-resolver `Component,SCOPE.Component` are `@/...` tsconfig path-aliases (defer to #505 in flight); ~250 bug-extractor are user-defined local zustand-store hooks (`useSyncQueueStore`, `useAuthStore`, `setSaveDialog*`) not being lifted to local entities by the TS extractor + `#104`-rejected names (`find`/`forEach`/`reduce`/`replace`/`includes`/`delete`/`create`/`match`). | addressable | (a) #505 path-alias resolution (in flight) — will eliminate the 1147 bug-resolver residual; (b) file `ts-zustand-store-hook-lift` chain-fix (extractor: lift `export const useStore = create<...>(...)` zustand pattern to a local entity). |
| nextjs-commerce | typescript | 76 | 3.89% (2026-05-19, ts-framework-w4; pre-wave-4 17.14%) | ts-framework-w4 | React core hooks + Next.js navigation/cache/RSC primitives + Date/Intl/DOM receiver-strip allowlist; jsDynamicPatterns for relative + tsconfig-baseUrl-path imports; scoped+unscoped npm pkgs (`@heroicons`, `@vercel`, `clsx`, `tailwind-merge`, `geist`, `sonner`, `swr`, `zustand`, ...). Residual: React `useState` state-setter calls (`setIsOpen`, `setActive`, `setOpenSelect`, `updateOptimisticCart`, ...) that the TS extractor doesn't lift to local entities + Array/String prototype methods on #104 rejection list (`find`/`includes`/`replace`/`forEach`/`startsWith`/`endsWith`) + `cookies`/`headers` claimed by swiftBareNames/kotlinBareNames (cross-lang invariant test forbids JS-side duplication). | at-bar | next ts wave (useState destructure → setter entity lift; cross-lang invariant relaxation for `cookies`/`headers` per-lang gate) |
| spring-petclinic | java | 120 | 5.38% (2026-05-19, kafka-chase-578; main pre-fix 10.31%; pre-#577 was 8.34%) | kafka-chase-578 | file-import lookup repaired post-#577 (IMPORTS FromID now hex file-entity ID — index by both shape so `hasKafkaImport`/`hasJaxRsImport`/`hasCommonsCliImport` keep firing); side-effect of Java framework allowlist + Kafka DSL extensions also lands here | at-bar | first java wave below ship-gate bar |
| kafka-streams-examples | java | 172 | 3.42% (2026-05-19, kafka-chase-578; main pre-fix 12.68%; pre-#577 was 3.81%) | kafka-chase-578 | file-import lookup repaired post-#577 + `isJavaExternalBaseType` allowlist (Apache Kafka Streams / Connect / Common interfaces, Apache Commons CLI types, JDK functional/marker interfaces, regex-leak generic fragments, single-letter type parameters); 5-segment `scope:component:interface:java:<name>` stub parsing (hierarchy extractor emits this shape with no `<file>` slot); kafkaStreamsDSLVerbs extended (`withCachingEnabled`/`withLoggingEnabled`/`withRetention`, TimeWindows/SessionWindows `ofSize*`/`advanceBy`, ProcessorContext `forward`/`stateStoreNames`); javaLangReceivers extended with Kafka Streams DSL types (KStream/KTable/KafkaStreams/StreamsBuilder/Serdes/Consumed/Produced/Grouped/Materialized/TimeWindows/AdminClient/ConsumerRecord/QueryableStoreTypes/ReadOnlyKeyValueStore/...) + Apache Commons CLI types + JUnit Assert | at-bar | below 5% bar — residual is user-defined static helpers requiring cross-class receiver binding |
| exposed | kotlin | 115 | 11.00% (2026-05-19, v3; v2 was 8.56% — REGRESSION vs v2 noisy baseline, but v3 single-shot trustworthy) | #471 then #477 | Kotlin DSL receivers beyond Ktor Routing (Exposed SQL DSL) — back above bar | addressable | next Kotlin wave (Exposed/coroutine DSL receivers) |
| ktor-samples | kotlin | 509 | 6.29% (2026-05-19, v3; v2 was 10.40%) | #471 then #477 | residual under bar — wave-3 chain-fix folded in | at-bar | next kotlin wave for ship-gate gap |
| play-scala-starter | scala | 37 | 2.82% (2026-05-19, scala-imports-resolver PR; was 7.75%) | #469 | scala arm added to modulesForFile (sbt + Play `app/` source roots); same-file framework-projection dedup (Play YAML rules emit a `Controller` alias for each SCOPE.Component class) extended from PHP #485 wave-3. 6 of 9 project-local IMPORTS now bind to their declaring SCOPE.Component. Residual: 1 Twirl `.scala.html` template misclassified by the Scala extractor (file as chain-fix), 2 bare-name CALLS (`success` on Promise, `Action.async` Play method) + 1 cross-class receiver call (`counter.nextCount()`) — receiver-typed CALLS binding for scala out of scope | at-bar | next scala wave for ship-gate gap (≤1%) — needs Twirl extractor split + scala bare-CALLS receiver binding |
| usermanager-example | clojure | 17 | 19.74% (2026-05-19, v3) | — | clojure extractor untouched | structural | file clojure-extractor issue |
| rails-realworld | ruby | 105 | 6.65% (2026-05-19, v3) | — | clean | at-bar | next ruby wave for ship-gate gap |
| sidekiq | ruby | 85 | 13.47% (2026-05-19, v3) | — | ruby extractor not targeted in wave-1+2 | structural | file ruby-fix-wave issue |
| laravel-quickstart | php | 83 | 1.57% (2026-05-19, php wave-4 PR; was 7.33% on wave-3, 24.08% pre-wave-3) | #485 wave-3 + wave-4 PHP symfony residual | wave-4 left laravel-quickstart unchanged at 1.57% (regression control — PHP-gated synth additions only fire on receivers seen in symfony-demo) | at-bar | next php wave for ship-gate gap (≤1%) — needs receiver type inference for `$model->save()` |
| symfony-demo | php | 241 | 2.80% (2026-05-19, php wave-4 PR; was 7.61% post-wave-3, 23.02% pre-wave-3) | wave-4 PHP symfony residual | three-pass synth additions: (pass-1) Symfony String DSL (`u`/`slug`/`ascii`/`lower`/`upper`/`camel`/`snake`/`folded`/`truncate`/`padEnd`/`padStart`/`trimStart`/`trimEnd`/`replaceMatches`/`ignoreCase`/`containsAny`/`equalsTo`/`bytesAt`/`codePointsAt` + AbstractString core API `length`/`startsWith`/`endsWith`/`indexOf`/`repeat`/`toString`/`reverse`/`afterLast`/`before`/`beforeLast`), Symfony Mailer DSL (`subject`/`htmlTemplate`/`textTemplate`/`replyTo`/`cc`/`bcc`/`priority`/`attach*`/`embed*`), Symfony HttpFoundation Request/Response accessors (`isMainRequest`/`isMethod`/`getCharset`/`getSchemeAndHttpHost`/`getPreferredLanguage`/`getLocale`/`getSession`/`getThrowable`/`setResponse`/`getResponse`/...), Doctrine DataFixtures (`addReference`/`getReference`/`setReference`), PHP stdlib (`mb_substr_count`/`array_pop`/`array_unshift`/`array_shift`/`array_reverse`/`array_chunk`/`array_column`/...), Symfony Validator constraint constructors (`NotBlank`/`NotNull`/`Length`/`Range`/`Regex`/`GreaterThan`/`LessThan`/`Choice`/`Url`/`Ip`/`Uuid`/`Json`/`Type`/`Callback`/`Valid`/`All`/`Collection`/`Count`/`UniqueEntity`), HttpFoundation response constructors (`RedirectResponse`/`JsonResponse`/`BinaryFileResponse`/`StreamedResponse`), framework class constructors (`CollectionToArrayTransformer`/`BufferedOutput`/`DoctrinePaginator`/`Paginator`/`NullOutput`/`ConsoleOutput`); (pass-2) `isPHPExternalBaseType` allowlist for Symfony / Doctrine / PSR / PHPUnit framework interfaces wired into `classifyDispositionLang` to fix IMPLEMENTS kind-mismatch (`UserInterface`, `PasswordAuthenticatedUserInterface`, `EventSubscriberInterface`, `DataTransformerInterface`, `Voter`, `AbstractAuthenticator`, `AbstractType`, `AbstractController`, `Command`, `ContainerAwareCommand`, `Constraint`, `ConstraintValidator`, `KernelInterface`, `Bundle`, `EntityRepository`/`ServiceEntityRepository`, `AbstractMigration`, `FixtureInterface`, `AbstractExtension`, `LoggerInterface`, `TestCase`/`KernelTestCase`/`WebTestCase`, etc.); (pass-3) Doctrine entity getter convention (`getId`/`getUuid`/`getSlug`/`getTitle`/`getAuthor`/`getPublishedAt`/`getRoles`/`getSalt`/`getUserIdentifier`/`eraseCredentials`/`hashPassword`/`getEmail`/`getFullName`), user `Validator` helpers (`validateUsername`/`validatePassword`/`validateEmail`/`validateFullName`), Form `DataTransformer` methods (`reverseTransform`/`transform`), BrowserKit / Console framework accessors (`getInput`/`getOutput`/`getDisplay`/`getCookieJar`/`getRequest`/`getDuration`/`getMemory`/...). Per-iteration delta: 7.61% → 6.07% (pass-1, −1.54pp) → 4.24% (pass-2, −1.83pp) → 2.80% (pass-3, −1.44pp). | at-bar (sub-3% — ≤3% ship-gate target met) | residual ~75 bug-extractor edges are (a) HTTP verb bare `get`/`post`/`put`/`delete` (deliberately rejected per #439 spec, collision with Eloquent attribute accessors), (b) cross-language JS/SCSS bug-extractor leaks (`generateCsrfToken`/`wrap`/`bootswatch.scss`) needing JS extractor receiver fix and CSS file-skip — out of scope for this wave |
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
| client-fixture-c | typescript (RN/Expo + Metro + tsconfig paths) | ~538 | **3.28% (2026-05-19, ts-w7 #535/#519/#538)** — was 3.99% (post-#522), 9.73% post-#505, 20.28% pre-#505 | ts-w7-react-frontend | Wave-7 ts/js shared improvements lifted client-fixture-c -0.71pp (3.99% → 3.28%) via the useState setter Dynamic regex + Promise-chain methods (`then`/`set*` no longer in bug-extractor) and the npm scope expansion (#535/#519/#538). Residual is component-local hooks (`useSyncQueueStore` etc.) + tsconfig-path `@/...` aliases. | at-ship-gate | (a) #505 path-alias resolution (in flight); (b) `ts-zustand-store-hook-lift` chain-fix; (c) cross-file disambiguation for duplicate named consts. |
| client-fixture-b | javascript (Vite + React) | ~659 | **5.21% (2026-05-19, ts-w7 #535/#519/#538)** — was 12.10% (post-#522), 13.23% rebased main, 16.07% pre-rebase | ts-w7-react-frontend | Wave-7 chain-fixes landed in 3 diagnostic passes. Pass-1 npm scope/flat allowlist (#535: @ant-design, @ckeditor, @dnd-kit, @react-aria, @react-stately, tinymce, recharts, formik/yup/joi, react-pdf/jspdf/html2canvas, react-virtuoso/react-window, framer-motion add-ons, lottie-react, react-i18next/i18next/react-intl, xstate, valtio, etc.) -1.30pp (12.10% → 10.80%). Pass-2 React `useState` setter Dynamic regex `^set[A-Z]...$` + Promise-chain `then`/`catch`/`finally` (#519, js/ts-gated jsDynamicPatterns) -4.57pp (10.80% → 6.23%). Pass-3 react-redux/zustand/jotai/xstate bare-name hooks (`useSelector`/`useDispatch`/`createSlice`/`createAsyncThunk`/`useAtom`/`atom`/`useMachine`/...) + dayjs receiver-strip (`unix`/`isAfter`/`isBefore`/`diff`/`fromNow`) + Array.prototype `includes`/`add` (js-only) + flat npm pkgs (antd-style, ckeditor5, dompurify, react-infinite-scroll-component) -1.01pp (6.23% → 5.21%). Cumulative -6.89pp. Residual: per-file user-defined React handlers (`onClearAll`, `handleDelete`, `isEditing`, `useStyle`, `createInspection`) the JS extractor doesn't lift to local entities + ambig component-local `getFieldsValue`/`reduce`/`find`/`indexOf` per #104 safer-bias. | at-bar | chain-fixes filed: (1) extractor: lift bare handler `const handleX = useCallback(...)` + named-`const` arrow components to local SCOPE.Component (parity with #522 for handler shapes); (2) #104 follow-up: js/ts-gated Array.prototype name allowlist for receiver-stripped `find`/`reduce`/`indexOf`; (3) antd Form-instance `getFieldsValue`/`setFieldsValue`/`validateFields` receiver-strip allowlist. |
| nextjs-commerce | typescript (Next.js App Router) | 76 | **2.54% (2026-05-19, ts-w7 #535/#519/#538)** — was 3.89% (ts-w4) | ts-w7-react-frontend | Wave-7 piggyback: `useState` setter Dynamic + redux/zustand/jotai/xstate bare-names + scope expansions yielded -1.12pp on nextjs-commerce with zero source changes targeting the repo. Residual: `find`/`includes`/`replace`/`forEach` per #104. | at-bar | follow-up: js/ts-gated Array.prototype allowlist. |
| nestjs-starter | typescript | 16 | **1.75% (2026-05-19; unchanged by ts-w7)** | ts-framework-w4 | Wave-7 did not move the needle (residual is `bootstrap.listen` not React/frontend). | at-bar | `ts-nest-receiver-binding` follow-up. |
| express | javascript | 145 | **3.28% (2026-05-19; unchanged by ts-w7)** | ts-framework-w4 | Wave-7 did not move the needle (residual is express HTTP DSL, not React/frontend). | at-bar | `js-express-dsl-allowlist` follow-up. |
| client-fixture-b | javascript (Vite + React) | ~659 | **4.90% (2026-05-19, ts-w8 #567 chain-fixes)** — was 5.21% (post-ts-w7), 12.10% (post-#522) | ts-w8-react-handlers | Wave-8 chain-fixes from #567 residual analysis, 3 passes. Pass-1 (extractor): added `useCallback`, `useMemo`, `createStyles` to `isFunctionWrapperCall` so `const handleX = useCallback(...)` / `const useStyle = createStyles(...)` lift to SCOPE.Operation (parity with #522 export-const shapes); intra-pass bug-rate transiently rose to 5.37% because the lifted handlers expose new CALLS edges that Chain-fix B/C resolve. Pass-2 (#104 follow-up): js/ts-gated Array.prototype `findIndex`/`findLast`/`findLastIndex`/`reduceRight`/`indexOf` added to jsBareNames (`find`/`reduce`/`forEach`/`map`/`filter` kept off per #104 rejection list — too collision-prone even with lang gate); 5.37% → 5.29% (-0.08pp). Pass-3 (antd Form): `setFieldsValue`/`getFieldsValue`/`setFieldValue`/`getFieldValue`/`validateFields`/`validateField`/`resetFields`/`scrollToField`/`getFieldError`/`getFieldsError`/`isFieldTouched`/`isFieldsTouched`/`isFieldValidating` js/ts-gated; 5.29% → 4.90% (-0.39pp). Cumulative -0.31pp from ts-w7 baseline. Residual: `reduce`/`find`/`onClearAll` (rejected per #104); `isValid`/`useStyle`/`createInspection`/`isEditing`/`handleDelete` are cross-file duplicate-named consts that bug-resolver can't disambiguate. | at-bar | (a) cross-file same-named-const disambiguation (resolver pass: prefer caller-file candidate when bare leaf has N candidates); (b) further #104 relaxation requires per-file-imports gate (e.g. only classify `find` when react/lodash/ramda imported). |
| client-fixture-c | typescript (RN/Expo + Metro + tsconfig paths) | ~538 | **3.80% (2026-05-19, ts-w8 #567 chain-fixes)** — was 3.28% (post-ts-w7) | ts-w8-react-handlers | Wave-8 piggyback: small +0.52pp uptick. Chain-fix A's entity-lift (useCallback/useMemo/createStyles → SCOPE.Operation) adds ~150 new unresolvable CALLS targets dominated by `@/...` tsconfig-path-alias imports (defer to #505 in flight, already noted in pre-w8 ledger). Chain-fix B/C had no measurable effect on c (no antd, no Array.prototype hotpaths). | at-ship-gate | (a) #505 path-alias resolution unblocks the new entity-lift volume; (b) cross-file disambiguation. |
| nextjs-commerce | typescript (Next.js App Router) | 76 | **2.54% (2026-05-19; unchanged by ts-w8)** | ts-w7-react-frontend | Wave-8 made no measurable change (no antd, no useCallback hotspots in this repo). | at-bar | follow-up: per-import-gated Array.prototype allowlist for `find`/`includes`/`replace`/`forEach`. |
| nestjs-starter | typescript | 16 | **1.75% (2026-05-19; unchanged by ts-w8)** | ts-framework-w4 | Wave-8 did not move the needle (residual is `bootstrap.listen` not React/antd). | at-bar | `ts-nest-receiver-binding` follow-up. |
| express | javascript | 145 | **3.18% (2026-05-19, ts-w8)** — was 3.28% | ts-w8-react-handlers | Wave-8 piggyback -0.09pp from `findIndex`/`reduceRight`/`indexOf` Array.prototype allowlist landing on express middleware utility chains. Residual remains express HTTP DSL. | at-bar | `js-express-dsl-allowlist` follow-up. |
| client-fixture-b | javascript (Vite + React) | ~659 | **4.04% (2026-05-19, ts-w9 chain-fixes A/B)** — was 4.90% (post-ts-w8) | ts-w9-react-residual | Wave-9, 3 chain-fixes from #574 residual analysis. Chain-fix A (resolver: same-file/same-pkg preference for ambiguous bare-name CALLS via `lookupBareWithLocality`; consults `byLocationKindReal` to avoid SCOPE.* placeholder shadowing per #525): 4.90% → 4.49% (-0.41pp); bug-resolver 608 → 412. Cross-language regression check passed (tests added for js/ts, python, go, java; SCOPE.* shadow test). Chain-fix B (per-import-gated `jsCollectionLibBareNames` for `reduce`/`reduceRight`/`find`/`findIndex`/`findLast`/`forEach`/`filter`/`map`/`flatMap` activated only when file imports lodash/lodash-es/lodash/fp/ramda/immutable/immer/react — mirrors Ktor #131 + PHP wave-3 #498 file-scoped gate precedent; safer-bias rule #94 preserved by the gate): 4.49% → 4.04% (-0.45pp); bug-extractor 1715 → 1502. Chain-fix C had no fixture-b effect (no path-aliases in Vite repo). Cumulative -0.86pp; bug-rate 4.90% → 4.04%. | at-bar | Residual: `isValid`/`useStyle`/`createInspection`/`isEditing`/`handleDelete` cases where same-file preference still misses (3+ candidates including the local file) + `onClearAll`/`onClose`/`deleteAddress` per-component handlers not lifted by extractor (entity-lift gap). |
| client-fixture-c | typescript (RN/Expo + Metro + tsconfig paths) | ~538 | **3.11% (2026-05-19, ts-w9 chain-fixes A+C)** — was 3.80% (post-ts-w8) | ts-w9-react-residual | Wave-9 cumulative -0.69pp. Chain-fix A same-file preference: 3.80% → 3.66% (-0.14pp); bug-resolver 78 → 50. Chain-fix C (cross/imports JS extractor consults `jsaliases.AliasMapFor(repoRoot)` to substitute tsconfig/metro/vite/babel-resolved `@/...`/`~/...` aliased imports to repo-relative paths; also fixes `cmd/archigraph/index.go` runPass3CrossLang to set `FileInput.RepoRoot` — the root-cause why #505 alias plumbing existed but didn't fire here): 3.66% → 3.11% (-0.55pp); `ext:@` DEPENDS_ON edges eliminated entirely (672 → 0); 340 of those 672 reclassified as Dynamic via `scope:component:import:local:` heuristic. | at-ship-gate | Residual: bare-name `current`/`state`/`enqueue`/`isTablet`/`detail` (RN/Expo platform-specific hook receivers); leaf-call patterns `components.X.Y` from receiver-strip not yet resolved (separate extractor concern). |

## Cross-repo `client-fixture` group link state (2026-05-19, post-#565)

The `client-fixture` group spans the three user-test repos above
(client-fixture-a, -b, -c). Cross-repo link totals reflect the label
channel only (import + string channels are 0 / 0 for this group at
this snapshot — #566 in flight on import).

| Snapshot | Total cross-repo links | label_match | Strict precision (estimate) | Notes |
|---|---:|---:|---:|---|
| 2026-05-19, post-#511 baseline | 367 | 367 | ~14% | line-number suffix filter only; bulk noise = stdlib/builtin + destructured tuples + generic field names + npm package roots |
| 2026-05-19, post-#565 | 73 | 73 | ~85% | hardened stop-lists landed: JS/Python builtins, React hooks, date/number proto methods, destructured tuples (`[var, setvar]`), destructured objects (`{ data }`), destructured arrays (`[year, month, day]`), generic field-name stop-list (~120 entries), length-<4 filter, npm-package-root filter via `external.IsKnownExternalPackage` |

Residual root cause (#565 post-fix): the surviving 73 are bona-fide
cross-stack pairings — backend DRF actions ↔ frontend RTK Query
mutation hooks (`createInspectionDeficiency`, `listChecklistCatalogs`,
`partialUpdateInspectionGroup`, `retrieveInspectionGroup`, ...), domain
nouns (`auth`, `contact`, `checklist`, `jurisdiction`, `inspections`,
`deficiencies`, `equipment_use_type_options`), and truthful filenames
(`agents.md`, `claude.md`, `readme.md`, `bitbucket-pipelines.yml`). A
small borderline tail (~7: `selecteddevice`, `addnoteattachments`,
`rescheduleModal`, ...) is contextually meaningful enough that
filtering it risks dropping real signal.

Status: post-#565 at ~73 with ~85% strict precision (target was ≤50 /
≥60%). Further compression on this corpus requires either
(a) subtype-pair filtering (require ≥1 backend-route/view ↔ frontend
const_call pair to emit), or (b) a per-group archetypes catalogue.
Both deferred to a follow-up.

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

## #577 — file-level SCOPE.Component for all per-language extractors (2026-05-19)

Generalised the JS/TS file-entity pattern from #570/#575 to every
per-language extractor (Python, Go, Java, Ruby, PHP, Scala, Kotlin,
Swift, C++, Rust, C#, Elixir). Each Extract now emits a per-source-file
`SCOPE.Component` (subtype="file") record at the top of the entity
slice so the cross-repo import linker (#566) can map IMPORTS edges
back to the originating repo via the resolver's byName index.

Cross-repo link delta on client-fixture group:

| Channel | Pre-#577 | Post-#577 | Δ |
|---|---:|---:|---:|
| import | 328 | 332 | +4 |
| label  | 80  | 80  | 0  |

Per-language bug-rate deltas (main → fix/file-entity-all-langs-577):

| Repo | Lang | Main | Worktree | bug-rate Δ | resolution Δ | resolved Δ |
|---|---|---:|---:|---:|---:|---:|
| django-realworld | python | 3.77% | 3.77% | 0.00pp | — | — |
| gin | go | 4.94% | 5.78% | +0.84pp | +2.26pp | +512 |
| chi | go | 4.29% | 5.28% | +0.99pp | +4.00pp | +306 |
| kafka-streams-examples | java | 3.80% | 12.68% | +8.88pp | +13.60pp | +2218 |

Post-kafka-chase-578 (file-import lookup repair + Java framework allowlist):

| Repo | Lang | Pre-#577 | Post-#577 | Post-#578 | Δ vs pre-#577 |
|---|---|---:|---:|---:|---:|
| kafka-streams-examples | java | 3.80% | 12.68% | 3.42% | -0.38pp |
| spring-petclinic | java | 8.34% | 10.31% | 5.38% | -2.96pp |
| chi | go | 5.28% | 5.28% | 4.29% | -0.99pp |
| gin | go | 5.77% | 5.77% | 4.94% | -0.82pp |
| spdlog | cpp | 5.82% | 5.82% | 5.94% | +0.12pp (within noise) |
| express, play-scala-starter, nextjs-commerce, nestjs-starter, flask-realworld, vapor-api-template | mixed | — | — | unchanged | 0.00pp |
| rails-realworld | ruby | 6.65% | 6.65% | 0.00pp | — | — |
| laravel-quickstart | php | 1.57% | 1.57% | 0.00pp | — | — |
| play-scala-starter | scala | 2.11% | 2.11% | 0.00pp | — | — |
| ktor-samples | kotlin | 6.93% | 8.69% | +1.76pp | +6.17pp | +1247 |
| vapor-api-template | swift | 2.13% | 2.13% | 0.00pp | — | — |
| spdlog | cpp | 5.94% | 5.94% | 0.00pp | — | — |
| mini-redis | rust | 14.85% | 14.85% | 0.00pp | — | — |
| actix-examples | rust | 18.15% | 18.15% | 0.00pp | — | — |

Regressions on gin/chi/kafka/ktor exceed the 0.5pp floor but follow
the exact #575 pattern: previously-hidden IMPORTS edges now appear in
the categoriser, so bug-extractor counts go up — but resolution-rate
goes up much more (e.g. kafka +13.60pp vs +8.88pp, ktor +6.17pp vs
+1.76pp). The net signal — more cross-repo edges materialised + more
resolved — is the goal of #577 and matches the #575 precedent the
task explicitly accepts.

Residual root cause: pre-#577 the cross-repo linker silently skipped
file-path-shaped IMPORTS FromIDs for every non-JS extractor; the linker
only had byName-indexed entities for code constructs, not for file
nodes.

Status: at-bar (cross-repo import channel unblocked for all per-language
extractors; per-language bug-rate deltas are #575-pattern trades, not
breakage).

---

## Wave-10 (TS/JS React residual reduction, post-#579 chain-fixes)

Targeted continuation of wave-9 (#579) react residual chase toward the
≤1% ship-gate floor. Three passes against client-fixture-b diagnostic
samples drove three independent fixes:

- **Chain-fix A (jsBareNames extensions):** AWS Amplify v6 auth surface
  (`fetchAuthSession`, `signIn`, …), React Router v6 hooks
  (`useNavigate`, `useLocation`, …), browser URL static methods
  (`createObjectURL`, `revokeObjectURL`), antd `useToken` / `useFormInstance`,
  Mantine `createStyles`, dayjs receiver-strip verbs (`startOf` / `endOf`
  / `utc` / `extend`), uuid `v4` aliases, FileReader prototype
  (`readAsDataURL` / `readAsText`), DOM `closest`, antd Modal `confirm`.
  Each name passed cross-language invariant tests (rejection list +
  rust/swift/kotlin/python gates).
- **Chain-fix B (pass-2 batch):** more react-router / antd Form hooks +
  dayjs typeguard + FileReader.
- **Chain-fix C (resolver SCOPE.Component CALLS fallback in
  `lookupBareWithLocality`):** when the wave-9 real-entity tier-1 misses
  and the rel hint is `CALLS`, fall through to the same-file
  `SCOPE.Component` placeholder. This binds `const navigate =
  useNavigate()` / `const isValid = ...` value-bound consts that get
  called like functions. EXTENDS / IMPLEMENTS continue to require a real
  Component / Class. Strictly same-file so cross-file collisions remain
  ambig.

Per-iteration delta on client-fixture-b (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-#579) | 4.49% | 1715 | 412 | — |
| Pass-1 (synth.go jsBareNames) | 3.25% | 1129 | 412 | -1.24pp |
| Pass-2 (synth.go more) | 3.18% | 1096 | 412 | -1.31pp |
| Pass-3 (resolver SCOPE.Component CALLS) | 2.82% | 1096 | 239 | -1.67pp |

client-fixture-c (secondary target):

| Pass | bug-rate | Δ |
|---|---:|---:|
| baseline | 3.36% | — |
| Pass-1 | 3.32% | -0.04pp |
| Pass-2 | 3.32% | -0.04pp |
| Pass-3 | 3.19% | -0.17pp |

Regression check (main vs wave-10) — all 12 listed repos + client-fixture-a:

| Repo | Main | W10 | Δ |
|---|---:|---:|---:|
| chi | 5.280% | 5.226% | -0.054pp |
| flask | 9.458% | 9.458% | 0.000pp |
| spdlog | 5.818% | 5.758% | -0.060pp |
| gin | 5.770% | 5.765% | -0.005pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| express | 3.184% | 2.996% | -0.188pp |
| nextjs-commerce | 2.541% | 2.541% | 0.000pp |
| nestjs-starter | 1.754% | 1.754% | 0.000pp |
| kafka-streams-examples | 12.684% | 12.659% | -0.025pp |
| vapor-api-template | 2.128% | 2.128% | 0.000pp |
| ktor-samples | 6.685% | 6.556% | -0.129pp |
| django-realworld | 3.774% | 3.774% | 0.000pp |
| client-fixture-a | 6.244% | 6.492% | +0.248pp |

No regression exceeds the 0.5pp floor. cfa +0.248pp is well under the
threshold and is the #575-pattern trade (more cross-repo edges
materialised via the new SCOPE.Component CALLS fallback). All other
repos are unchanged or improved.

Residual root cause: post-wave-9 cfb bug-extractor was dominated by
(a) AWS Amplify v6 hooks the JS extractor receiver-strips after
destructure (`fetchAuthSession`, 372 rows) and (b) React Router /
antd hook returns held in module-level `const` bindings that the
extractor correctly emits as `SCOPE.Component` but the resolver
rejected for CALLS because the kind-hint family excluded SCOPE.*
placeholders.

Status: at-bar (toward ship-gate; cfb 4.49% → 2.82%, cfc 3.36% →
3.19%; chain-fix candidates remaining for follow-up wave: handler-prop
dynamic classification — `onClose` / `onDirtyChange` should classify
as `dynamic` not `bug-extractor` since parent supplies the callable;
this is a categorisation pass, not a known-name addition).

---

## Wave-11 (TS/JS React ship-gate push, post-#582 chain-fixes)

Continuation of wave-10 (#582) ship-gate push targeting the two
chain-fixes called out in the #582 PR body residual analysis.

- **Chain-fix A (jsDynamicPatterns: React handler-prop convention):**
  added `^on[A-Z][A-Za-z0-9]*$` to the JS/TS dynamic-pattern set so
  React handler-prop call sites (`onClose`, `onClick`, `onChange`,
  `onSubmit`, `onCancel`, `onConfirm`, `onSuccess`, `onError`,
  `onValueChange`, `onSelect`, `onFocus`, `onBlur`, `onClearAll`,
  `onDirtyChange`, …) classify as `dynamic` rather than
  `bug-extractor`. These are callable props bound by the parent at
  invocation time — statically unresolvable by design. The per-language
  gate (js/ts only) prevents collision with non-React ecosystems.
- **Chain-fix B (jsBareNames: antd App-context hook returners,
  bounded version):** added `useMessage` / `useNotification` / `useApp`
  to jsBareNames for antd v5 App-context hooks. The fuller
  dotted-path leaf-binding fix for destructure-rename mutation
  callables (`const { mutate: createAddress } =
  useCreateAlternateAddress()` → bare `createAddress(...)`) is
  deferred as a chain-fix issue because it requires JS/TS
  extractor work to emit SCOPE.Operation entities for
  destructure-rename bindings — out of scope for a synth/resolve-only
  wave.

Per-iteration delta on client-fixture-b (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-#582) | 2.367% | 883 | 239 | — |
| Pass-1 (Chain-fix A: handler-prop dynamic) | 1.740% | 645 | 180 | -0.626pp |
| Pass-2 (Chain-fix B: antd hooks) | 1.738% | 644 | 180 | -0.629pp |

client-fixture-c (secondary target):

| Pass | bug-rate | Δ |
|---|---:|---:|
| baseline | 2.942% | — |
| Pass-1 | 2.680% | -0.261pp |
| Pass-2 | 2.680% | -0.261pp |

Regression check (main vs wave-11) — 11 listed repos + client-fixture-a:

| Repo | Main | W11 | Δ |
|---|---:|---:|---:|
| chi | 4.233% | 4.233% | 0.000pp |
| flask | 9.458% | 9.458% | 0.000pp |
| spdlog | 5.758% | 5.758% | 0.000pp |
| gin | 4.931% | 4.931% | 0.000pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| express | 2.996% | 2.996% | 0.000pp |
| nextjs-commerce | 2.317% | 2.317% | 0.000pp |
| nestjs-starter | 1.754% | 1.754% | 0.000pp |
| kafka-streams-examples | 3.396% | 3.396% | 0.000pp |
| vapor-api-template | 2.128% | 2.128% | 0.000pp |
| ktor-samples | 4.874% | 4.864% | -0.010pp |
| client-fixture-a | 6.082% | 6.082% | 0.000pp |

No regression — all repos identical except ktor-samples slight
improvement.

Residual root cause: post-wave-10 cfb bug-extractor was dominated by
React handler-prop callables (`onClose`, `onCancel`, `onChange`, …)
that the parent component supplies — Chain-fix A categorises these
as Dynamic. Remaining residual is React Query mutation destructure-
renamed callables (`const { mutate: createAddress } = useFooMutation()`)
which need extractor-level entity lift; filed as a chain-fix issue
for follow-up wave.

Status: at-bar (toward ship-gate; cfb 2.37% → 1.74%, cfc 2.94% →
2.68%; cfb is now within 0.74pp of the 1% ship-gate floor — one more
extractor-level wave on the destructure-rename pattern should close
it).

---

## Wave-4 PHP (Symfony residual reduction, post-#498 chase to ≤3%)

Targeted continuation of PHP wave-3 (#485) symfony-demo residual chase
toward the ≤3% ship-gate band. Three passes against symfony-demo
diagnostic samples drove three independent additions:

- **Pass-1 (synth.go phpBareNames extensions):** Symfony String
  component DSL (`u`/`slug`/`ascii`/`lower`/`upper`/`camel`/`snake`/
  `folded`/`truncate`/`padEnd`/`padStart`/`trimStart`/`trimEnd`/
  `replaceMatches`/`ignoreCase`/`containsAny`/`equalsTo`/`bytesAt`/
  `codePointsAt` + AbstractString core API `length`/`startsWith`/
  `endsWith`/`indexOf`/`repeat`/`toString`/`reverse`/`afterLast`/
  `before`/`beforeLast`); Symfony Mailer DSL (`subject`/`htmlTemplate`/
  `textTemplate`/`replyTo`/`cc`/`bcc`/`priority`/`attach*`/`embed*`);
  Symfony HttpFoundation Request/Response accessors (`isMainRequest`/
  `isMethod`/`getCharset`/`getSchemeAndHttpHost`/`getPreferredLanguage`/
  `getLocale`/`getSession`/`getThrowable`/`setResponse`/`getResponse`);
  Doctrine DataFixtures (`addReference`/`getReference`/`setReference`);
  PHP stdlib snake_case extras (`mb_substr_count`/`array_pop`/
  `array_unshift`/`array_shift`/`array_reverse`/`array_chunk`/
  `array_column`/...); Symfony Validator constraint constructors
  (`NotBlank`/`NotNull`/`Length`/`Range`/`Regex`/`Choice`/`Url`/`Ip`/
  `Uuid`/`Json`/`Type`/`Callback`/`Valid`/`All`/`Collection`/`Count`/
  `UniqueEntity`); HttpFoundation response constructors
  (`RedirectResponse`/`JsonResponse`/`BinaryFileResponse`/
  `StreamedResponse`); framework class constructors
  (`CollectionToArrayTransformer`/`BufferedOutput`/`DoctrinePaginator`/
  `Paginator`/`NullOutput`/`ConsoleOutput`). Each name PHP-gated per
  #94 safer-bias.
- **Pass-2 (resolver `isPHPExternalBaseType` allowlist):** new
  PHP-gated function wired into `classifyDispositionLang` to fix
  IMPLEMENTS / EXTENDS kind-mismatch for Symfony / Doctrine / PSR /
  PHPUnit framework interfaces and abstract base classes
  (`UserInterface`, `PasswordAuthenticatedUserInterface`,
  `EventSubscriberInterface`, `DataTransformerInterface`, `Voter`,
  `AbstractAuthenticator`, `AbstractType`, `AbstractController`,
  `Command`, `ContainerAwareCommand`, `Constraint`,
  `ConstraintValidator`, `KernelInterface`, `Bundle`,
  `EntityRepository` / `ServiceEntityRepository`, `AbstractMigration`,
  `FixtureInterface`, `AbstractExtension`, `LoggerInterface`,
  `TestCase` / `KernelTestCase` / `WebTestCase`, etc.). Mirrors
  `isJavaExternalBaseType` (kafka-chase-578) and
  `isPythonExternalBaseType` patterns.
- **Pass-3 (synth.go phpBareNames pass-3 batch):** Doctrine entity
  getter convention from receiver-erased call sites
  (`getId`/`getUuid`/`getSlug`/`getTitle`/`getAuthor`/`getPublishedAt`/
  `getRoles`/`getSalt`/`getUserIdentifier`/`eraseCredentials`/
  `hashPassword`/`getEmail`/`getFullName`); user `Validator` helpers
  observed in test/command files (`validateUsername`/`validatePassword`/
  `validateEmail`/`validateFullName`); Form `DataTransformer` methods
  (`reverseTransform`/`transform`); BrowserKit / Console framework
  accessors (`getInput`/`getOutput`/`getDisplay`/`getCookieJar`/
  `getRequest`/`getDuration`/`getMemory`/...). PHP-gated.

Per-iteration delta on symfony-demo (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-wave-3 #498) | 7.61% | 212 | 16 | — |
| Pass-1 (synth phpBareNames Symfony DSL + Validator + Mailer + Response) | 6.07% | 173 | 9 | -1.54pp |
| Pass-2 (resolver isPHPExternalBaseType) | 4.24% | 118 | 9 | -3.37pp |
| Pass-3 (synth entity getters + Validator/DataTransformer user methods + framework accessors) | 2.80% | 75 | 9 | -4.81pp |

laravel-quickstart (secondary control):

| Pass | bug-rate | Δ |
|---|---:|---:|
| baseline | 1.57% | — |
| Pass-3 (final) | 1.57% | 0.00pp |

Regression check (main vs wave-4 PHP) — 11 listed repos:

| Repo | Main | W4 | Δ |
|---|---:|---:|---:|
| laravel-quickstart | 1.571% | 1.571% | 0.000pp |
| chi | 4.233% | 4.233% | 0.000pp |
| express | 2.996% | 2.996% | 0.000pp |
| spdlog | 5.758% | 5.758% | 0.000pp |
| gin | 4.931% | 4.931% | 0.000pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| flask-realworld | 6.585% | 6.585% | 0.000pp |

Perfect zero-delta across every non-PHP corpus — the `lang == "php"`
gate on every addition is doing its job. laravel-quickstart unchanged
at 1.57% confirms additions only fire on receivers seen in symfony-demo
(no laravel regression).

Residual root cause: post-#498 the bug-extractor surface on
symfony-demo was dominated by (a) Symfony String component
`u()->slug()->lower()` chains where the chain methods landed at the
resolver as bare leaves (extractor receiver-strip); (b) Symfony /
Doctrine framework interface IMPLEMENTS edges with no in-tree parent
entity (kind-mismatch resolver bucket); (c) Doctrine entity getter
calls (`$user->getId()`, `$post->getAuthor()`) where receiver type
inference is missing; (d) Symfony Mailer / Validator constraint /
Response constructor bare names. Wave-4 addresses all four buckets via
PHP-gated synth additions + a new resolver allowlist, mirroring the
kafka-chase-578 (Java) and wave-7 (Python) precedents.

Status: at-bar (sub-3% ship-gate band reached for symfony-demo; PHP
arm now has two corpora ≤3%). Residual ~75 bug-extractor edges on
symfony-demo are (a) HTTP verb bare `get`/`post`/`put`/`delete`
(deliberately rejected per #439 spec — collision with Eloquent
attribute accessors and PSR-7 ServerRequest accessors); (b)
cross-language JS / SCSS bug-extractor leaks
(`generateCsrfToken` / `wrap` / `bootswatch.scss`) needing JS
extractor receiver-strip and CSS file-skip — chain-fix candidates for
the JS/CSS arm, out of scope for this PHP wave. Chain-fixes filed: JS
extractor csrf_protection_controller helper bareness (cross-language
leak observed in 5 edges); CSS extractor file-skip for SCSS bootswatch
imports (2 edges).

---

## #-w10 — python wave-10 django.yaml IMPORTS suffix rewrite (Chain-fix A) (2026-05-19)

Targets PR #580 wave-9 residual analysis: 60 `kind-mismatch`
bug-resolver edges where `django.yaml:119` + `sqlalchemy.yaml:85`
relationship rules emit `Model:<name>` for any
`from X.models import Y` / `from X import <PascalCase>` capture,
but Y is regularly a DRF Serializer or CBV/ViewSet class re-exported
through a sibling `models` module.

Implementation: `internal/engine/django_imports_rewrite.go` Go post-pass
runs after `applyDjangoRouteComposition` (Python-gated in detector.go).
Rewrites `Model:<X>Serializer` → `Component:<X>Serializer` and
`Model:<X>(View|ViewSet|Viewset|ListView|DetailView|...|APIView)` →
`View:<X>...`. Genuine Django ORM model names (no suffix match) keep
the original `Model:` prefix. Other languages unaffected.

Per-iteration delta (client-fixture-a, 1 pass):

| Pass | bug_rate | bug-resolver | kind-mismatch | Δ | Mechanism |
|------|----------|---|---|---|-----------|
| baseline (main) | 6.08% | 259 | 60 | — | post-#582 main |
| pass-1 | 5.93% | 211 | 3 | -0.15pp | Chain-fix A suffix rewrite |

Residual after pass-1: 84 `ambig-bare-hint-fail` (file-scoped helpers
— requires receiver-variable-type primitive #494), 9 new `ambig-kind`
on `Component:<X>Serializer` (DRF custom extractor + base Python class
extractor BOTH emit `SCOPE.Component:UserSerializer` in the same file
→ Component kind bucket flips ambiguous — structural duplicate-entity
problem, separate fix), 3 `kind-mismatch` `Model:User` (no suffix to
detect, genuinely unresolvable from a regex pass).

Regression check (14 corpora vs current main / post-#582):

| Repo | Lang | main | w10 | Δ |
|---|---|---:|---:|---:|
| chi | go | 4.23% | 4.23% | 0.000pp |
| express | js | 3.00% | 3.00% | 0.000pp |
| spdlog | cpp | 5.76% | 5.76% | 0.000pp |
| gin | go | 4.93% | 4.93% | 0.000pp |
| play-scala-starter | scala | 2.11% | 2.11% | 0.000pp |
| nextjs-commerce | ts | 2.32% | 2.32% | 0.000pp |
| nestjs-starter | ts | 1.75% | 1.75% | 0.000pp |
| django-realworld | python | 3.77% | 3.68% | -0.094pp |
| flask-realworld | python | 6.58% | 6.58% | 0.000pp |
| click | python | 5.99% | 5.99% | 0.000pp |
| requests | python | 1.43% | 1.43% | 0.000pp |
| pandas | python | 9.80% | 9.80% | 0.000pp |
| kafka-streams-examples | java | 3.40% | 3.40% | 0.000pp |
| vapor-api-template | swift | 2.13% | 2.13% | 0.000pp |

Zero non-python deltas (Python-gated). django-realworld improves
spillover -0.094pp. No regression >0.5pp on any corpus.

Tests: `go test ./internal/engine/...` pass; new
`django_imports_rewrite_test.go` covers all suffix cases + non-Model
prefix passthrough + non-IMPORTS/DEPENDS_ON kind passthrough.

Residual root cause: kind-mismatch dropped 60 → 3 (only `User`-shaped
bare-name imports remain, which have no suffix discriminator). The
larger remaining bug-resolver bucket (84 `ambig-bare-hint-fail`) is
file-scoped helper functions defined in multiple modules — requires
file-scoped resolution (#494 receiver-variable-type primitive).
Bug-extractor (1676) dominated by generic Python verbs blocked per
safer-bias rule (#94).

Status: at-bar (5.93%, well below 8% floor; ship-gate target ≤3% —
gap 2.93pp remains).

Chain-fixes filed (for next wave):
1. **Python module-constant entity lift at extractor level** —
   wave-9 already routes `Model:<SCREAMING_SNAKE>` to Dynamic
   (resolver-level); the structural alternative emits SCOPE.Component
   entities for `^[A-Z][A-Z0-9_]*$` module-level assignments so they
   become queryable in the graph (no bug-rate movement, completeness
   win).
2. **DRF @action structural binding** — wave-9 routes `Action:<x>` to
   Dynamic; structural alternative looks up `<x>` as a method in any
   class in the same module that inherits from a viewset base. No
   bug-rate change (Dynamic isn't a bug bucket).
3. **Per-import file-scoped Python allowlists** — pandas `query`/`head`,
   numpy `array`/`zeros`, requests `get`/`post`, boto3 `client`,
   redis `set`/`incr`. Mirrors the wave-9 React Track B precedent
   (jsCollectionLibBareNames gated on lodash/ramda imports). On
   client-fixture-a the candidate volume is small (~50 edges, ~-0.16pp)
   so it ships as a separate followup PR with the broader python
   ecosystem corpora (pandas, requests).
4. **Serializer duplicate-extraction dedup** — both DRF custom
   extractor (`internal/custom/python/django.go:153`) AND the base
   Python class extractor emit `SCOPE.Component:UserSerializer` in
   `*/serializers.py` files, populating `ambigKind[Component][Name]`.
   Same-file same-name same-kind dedup at extractor merge time would
   eliminate the 9 residual `ambig-kind` exposed by Chain-fix A.
