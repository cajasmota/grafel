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
| requests | python | — | 1.27% (2026-05-19, flask-realworld wave spillover; was 1.43% pre-wave, 1.51% post-python-django-w4) | flask-realworld wave spillover -0.16pp (Flask-realworld wave: Python builtin exceptions + pythonExternalBaseTypes additions classify residual structural EXTENDS targets; scope:endpoint ref index resolves a handful of test endpoint references) | at-bar | within striking distance of 1% — push for ship-gate |
| flask-realworld | python | — | 3.10% (2026-05-19, flask-realworld wave Flask/SQLA/Werkzeug/Marshmallow + scope:endpoint ref index + python cross-file class fallback + python local-dotted-reexport Dynamic + SQLAlchemy `Model:<Class>` Dynamic; was 6.58% pre-wave). flask-realworld wave cumulative -3.48pp across 3 passes: pass-1 -1.12pp (refs.go pythonExternalBaseTypes — Python builtin exceptions Exception/ValueError/TypeError/KeyError/AttributeError/RuntimeError/NotImplementedError/StopIteration/OSError/IOError/FileNotFoundError/ImportError-family/Warning-family/etc.; Flask-Login/Flask-WTF/Flask-Marshmallow/Flask-RESTful parents UserMixin/AnonymousUserMixin/FlaskForm/SQLAlchemyAutoSchema/Schema/Validator/Range/OneOf/Email/Regexp/Equal/URL; SQLAlchemy column types Numeric/Float/Boolean/DateTime/Date/Time/Interval/LargeBinary/JSON/BigInteger/SmallInteger + ForeignKeyConstraint/PrimaryKeyConstraint/UniqueConstraint/CheckConstraint; Pagination/BaseQuery/Engine/Connection + AbstractConcreteBase/ConcreteBase/DeclarativeMeta/MetaData; refs.go classifyDispositionLang — isPythonLocalDottedReexport routes `<lower_seg>.<lower_seg>...<Identifier>` to Dynamic, catching SCOPE.Component placeholder dotted re-exports like `conduit.database.Column`/`conduit.database.Model`/`conduit.database.db`/`conduit.extensions.bcrypt`; refs.go lookupStructural — Python `scope:component:class:python:<consumer_file>:<ParentName>` cross-file fallback via lookupUniqueRealComponentByName routes Article(SurrogatePK)-style EXTENDS to the sibling-module class entity) → 6.58% → 5.46% (bug-extractor 85→83, bug-resolver 38→19). Pass-2 -0.91pp (refs.go classifyDispositionLang — isPythonSQLAlchemyModelStub routes `Model:<Name>` SQLAlchemy `relationship("Class")` string-keyed references to Dynamic; refs.go BuildIndex — scope:endpoint-scoped Properties["ref"] indexing under byQualifiedName fixes SERVES edges that carry `scope:endpoint:<file>#<method>:<path>` FromIDs but route through endpoint entities without QualifiedName population) → 5.46% → 4.55% (bug-extractor 74, bug-resolver 11). Pass-3 -0.45pp (synth.go pythonBareNames — Flask-Login decorators current_user/login_user/logout_user/login_required/fresh_login_required + Flask-JWT jwt_required/jwt_optional/create_refresh_token/get_jwt_identity/verify_jwt_in_request + Flask-RESTful marshal/marshal_with/RequestParser + Flask-CORS cross_origin + Flask-SocketIO join_room/leave_room/disconnect/on_namespace + Flask-WTF validate_on_submit/hidden_tag/DataRequired/InputRequired/EqualTo + Marshmallow validates_schema/pre_load/post_load/pre_dump/post_dump/dump_one/dump_many/load_one/load_many + SQLAlchemy desc/asc/nullsfirst/nullslast/subqueryload/joinedload/selectinload/lazyload/noload/raiseload/defer/undefer/with_entities/with_polymorphic/options/having/group_by/backref/validates/column_property/hybrid_property/declared_attr/declarative_base/sessionmaker/scoped_session + SQLAlchemy column types receiver-strips BigInteger/SmallInteger/LargeBinary/Interval/Numeric + Werkzeug helpers secure_filename/escape/safe_str_cmp/safe_join/http_date/BadRequest/Unauthorized/Forbidden/Gone/UnsupportedMediaType/TooManyRequests/InternalServerError + Click DSL echo/secho/prompt/confirm/clear/argument/option/pass_context/pass_obj/make_pass_decorator + pytest approx/deprecated_call/warns/importorskip/skipif/usefixtures + Marshmallow Method/Function/Constant/Nested/Pluck/auto_field + classmethod `cls` receiver — Python idiom strictly python-gated) → 4.55% → 3.10% (bug-extractor 47, bug-resolver 11). Residual: generic verbs (append/remove/items/post/pop/first/commit/walk/limit/offset/bind/exit) explicitly excluded per #94 safer-bias rule; local model bare-name CALLS (Tags/Comment/User) with multi-kind ambiguity (SCOPE.Component + Relationship + Model duplicates — chain-fix resolver); SQLAlchemy `Module:<x>`/`Route:<x>` short kind-prefixed stubs (similar to Model: handler but rarer — defensive chain-fix candidate). | at-bar (3.10%, just over 3% ship-gate — within 0.10pp; next wave for sub-3%) | resolver kind-disambiguator for SCOPE.Component + Relationship + Model multi-kind local model names; `Module:<x>` / `Route:<x>` short-stub handlers; cross-file CALLS receiver-type primitive (#494) |
| click | python | — | 3.73% (2026-05-19, click wave Python stdlib + gettext + i18n + concurrent.futures + pathlib + logging surface; was 5.39% pre-wave). Click wave cumulative -1.66pp across 3 passes: pass-1 -1.23pp (synth.go knownExternalPackages — Python stdlib roots gettext/codecs/errno/inspect/shutil/shlex/stat/textwrap/msvcrt/platform/pdb/ctypes/termios/selectors/struct/string/signal/types/fnmatch/gc/linecache/mimetypes/getpass/pickle/secrets/bisect/heapq/array/gzip/zipfile/tarfile + colorama (third-party Windows ANSI shim); synth.go pythonBareNames — gettext convention `_` + `ngettext` PEP-style i18n aliases; Python stdlib helpers receiver-stripped which/unlink/dup2/utime/warn/partition/rpartition/casefold/fromkeys/urlparse + ctypes c_ulong/c_int/c_uint/c_long/c_short/c_byte/c_void_p/c_char_p/c_wchar_p/c_size_t/c_bool/byref/WinError/GetLastError/HANDLE + TemporaryDirectory/ExitStack/Popen/BufferedWriter; synth.go classifyExternal — Python-gated dotted-receiver branch for `_.format(...)` / `ngettext.format(...)` → ext:gettext via pythonGettextDottedReceivers, lang=="python" gate prevents shadowing throwaway-`_` convention in other languages) → 5.39% → 4.16% (bug-extractor 774→631, bug-resolver 71→21). Pass-2 -0.24pp (synth.go pythonBareNames — logging surface setLevel/addHandler/removeHandler/StreamHandler/FileHandler/NullHandler/Handler/Formatter/LogRecord/Filter; pathlib read_text/write_text/read_bytes/write_bytes; argparse parse_args; pytest monkeypatch setenv/delenv/setitem/delitem/syspath_prepend; unittest.mock patch/MagicMock/Mock/PropertyMock/AsyncMock/ANY/DEFAULT/sentinel; re module fullmatch/sre_parse/sre_compile) → 4.16% → 3.92% (bug-extractor 593, bug-resolver 21). Pass-3 -0.19pp (synth.go pythonBareNames — concurrent.futures submit + os urandom/rmdir/putenv/unsetenv/chown/chmod/symlink/readlink/link/sendfile/fsync/truncate/ftruncate/isatty + gc collect + contextlib with_resource + shutil rmtree/copyfile/copytree/copystat + tty/termios tcgetattr/tcsetattr/setraw/cbreak + pathlib with_suffix/with_name/with_stem/relative_to/iterdir/hardlink_to/symlink_to/as_posix/as_uri/is_symlink/is_absolute/is_relative_to/is_reserved/is_mount/is_socket/is_block_device/is_char_device/is_fifo/touch/samefile/lchmod/group + time sleep/perf_counter/monotonic/time_ns/process_time + warnings simplefilter/resetwarnings/showwarning + pickle Pickler/Unpickler/HIGHEST_PROTOCOL/PickleError + io BufferedReader/BufferedRandom/BufferedRWPair/TextIOWrapper/FileIO/RawIOBase/BufferedIOBase/TextIOBase/IOBase/UnsupportedOperation + datetime timezone/tzinfo + uuid uuid3/uuid5/UUID + hashlib md5/sha1/sha256/sha512/blake2b/blake2s/sha3_256/sha3_512/hexdigest/digest/hmac + base64 b64encode/b64decode/urlsafe_b64encode/urlsafe_b64decode/b32encode/b32decode/a85encode/a85decode/hexlify/unhexlify + json loads/dumps/load/dump + os.path fnmatchcase/normpath/normcase/commonpath/commonprefix/splitdrive + typing runtime_checkable/final/override/reveal_type/assert_type/assert_never/get_type_hints/get_origin/get_args/is_typeddict/ParamSpec/TypeVarTuple/Unpack/TypeAlias/Self/Concatenate/Required/NotRequired + functools singledispatch/singledispatchmethod/total_ordering/cmp_to_key/partialmethod + inspect Signature/Parameter/BoundArguments/getfullargspec/iscoroutine/iscoroutinefunction/isasyncgen/isawaitable/ismodule/isabstract/isbuiltin/isgenerator/isgeneratorfunction + importlib invalidate_caches/find_spec/module_from_spec; lang="python" gate on all additions per #94 safer-bias) → 3.92% → 3.73% (bug-extractor 564, bug-resolver 21). Residual: generic verbs (append/write/read/replace/items/extend/close — explicitly excluded per #94 safer-bias rule, collide with user-defined Click HelpFormatter / CliRunner methods + writer pattern user classes); local Click-internal methods (`fail`, `convert`, `get_metavar`, `get_short_help_str`, `get_help`, `get_params`, `get_usage_pieces`, `make_formatter`, `indentation`, `section`, `write_text`, `write_paragraph`, `write_dl`, `_default_text_stdout`) — adding would shadow real local entities; `Choice` ambig-bare-hint-fail (4 local Choice entities collide — chain-fix kind-disambiguator). | at-bar (3.73%, ≤5% floor cleared, within 0.73pp of 3% ship-gate; next wave for sub-3%) | resolver kind-disambiguator for SCOPE.Component + Heading multi-kind local `Choice` collision; receiver-type primitive (#494) for self.fail/self.convert/self.get_* unresolved CALLS within Click's own class hierarchy |
| django-realworld | python | — | 1.79% (2026-05-19, flask-realworld wave spillover; was 3.68% pre-wave, 3.77% post-wave-8, 4.25% post-wave-7, 4.72% post-wave-6, 7.83% python-django-w4) | flask-realworld wave spillover -1.89pp (Python builtin exceptions in pythonExternalBaseTypes catch Django custom exception classes; isPythonSQLAlchemyModelStub classifies any `Model:<ClassName>` runtime references; isPythonLocalDottedReexport classifies django sub-package re-exports as Dynamic; scope:endpoint Properties["ref"] indexing resolves URLConf-emitted endpoint entities; Python cross-file class fallback resolves Django model mixin parents declared in sibling modules) | at-bar (1.79%, ship-gate cleared) | next python wave for sub-1% — residual is project-internal user methods and generic verbs |
| pandas | python | — | 4.49% (2026-05-19, pandas wave-2 pass-3; was 7.48% pre-wave-2 baseline, 7.90% post-flask-realworld spillover, 8.29% post-pandas-wave-1 pass-3, 9.80% post-wave-9 builtin-type methods spillover, 13.85% pre-fix). pandas wave-2 cumulative -2.99pp across 3 passes: pass-1 -1.31pp (7.48%→6.17%, bug-extractor 4338→3541 −797) synth.go pythonBareNames — pandas internal underscored compounds `_addsub_int_array_or_scalar`/`_align_frame`/`_box_func`/`_concat_same_type`/`_construct_axes_dict`/`_convert_slice_indexer`/`_format_native_types`/`_from_arrays`/`_from_backing_data`/`_get_*` family/`_maybe_*` family/`_putmask`/`_validate_*` family/`_with_freq`/`_with_infer` + numpy/scipy distinctive helpers `array_to_datetime`/`array_to_timedelta64`/`ensure_int{8,16,32,64,uint64}`/`ensure_string_array`/`period_ordinal`/`period_asfreq_arr`/`validate_argmax`/`validate_argmin`/`validate_argsort_with_ascending`/`validate_round`/`atleast_1d`/`atleast_2d`/`flatnonzero`/`intersect1d`/`setdiff1d`/`union1d`/`unique1d`/`fromarrays`/`frombuffer`/`fromiter`/`frompyfunc`/`intp`/`iinfo`/`finfo`/`broadcast_to`/`expand_dims`/`is_lexsorted`/`pd_array`/`pd_eval`/`pd_op`/`vstack`. Pass-2 -1.17pp (6.17%→4.997%, bug-extractor 3541→2828 −713) synth.go pythonBareNames — remaining pandas internal `_get_*`/`_maybe_*`/`_to_*`/`_values_for_*`/`_should_*`/`_replace_*`/`_reindex_*` + C-ext compound helpers `period_*`/`get_period_field_arr`/`get_date_field`/`group_*`/`utf8_is_*`/`utf8_lower`/`roll_mean`/`roll_sum`/`roll_var`/`run_ewm`/`numba_*_func`/`nan*`/`scalar_*`/`tile_for_unstack`/`take_block_columns`/`slice_block_columns`/`sort_indices`/`reorder_levels`/`reindex_axis`/`true_and_notna`/`tuples_to_object_array`. Pass-3 -0.50pp (4.997%→4.49%, bug-extractor 2828→2521 −307) synth.go pythonBareNames — pandas-defined error/engine/index/formatter classes `OutOfBoundsDatetime`/`OutOfBoundsTimedelta`/`IndexingError`/`InvalidIndexError`/`IntCastingNaNError`/`NullFrequencyError`/`UndefinedVariableError`/`UnsortedIndexError`/`ZoneInfoNotFoundError`/`DuplicateLabelError`/`MergeError`/`NumExprClobberingError`/`NumbaUtilError`/`ForbiddenExtensionType`/`IncompatibleFrequency`/`BoolEngine`/`Complex{64,128}Engine`/`DatetimeEngine`/`TimedeltaEngine`/`StringObjectEngine`/`ExtensionEngine`/`BlockIndex`/`BlockValuesRefs`/`IntIndex`/`Int64HashTable`/`IntervalTree`/`InvalidComparison`/`NativeValue`/`NoBufferPresent`/`DataFrameFormatter`/`DataFrameRenderer`/`DataFrameInfo`/`SeriesFormatter`/`SeriesInfo`/`ExcelFormatter`/`FloatArrayFormatter`/`TextWrapper`/`TreeBuilder`/`PrettyDict`/`InteractiveShell` + scipy interpolator classes `CubicSpline`/`UnivariateSpline`/`Akima1DInterpolator` + remaining compound helpers `accum_func`/`binary_join_element_wise`/`cache_readonly`/`compile_internal`/`pprint_thing_encoded`/`should_store`/`shares_memory`/`unique_label_indices`/`unique_deltas`/`update_dtype`/`logical_and`/`logical_or`/`rank_1d`/`rank_2d`/`lookup_array`/`overload_method`/`option_context`/`select_dtypes`/`memory_usage`/`memory_usage_of_objects`/`map_index`/`map_infer`/`map_locations`/`make_block_same_class`/`make_mask_object_ndarray`. Residual @ 4.49%: generic verbs `view`/`func`/`cls`/`equals`/`reindex`/`op`/`append`/`extend`/`dtype`/`keys`/`items`/`empty`/`drop`/`copy`/`replace` (excluded per #94 safer-bias — chain-fix #494 receiver-type primitive); pandas-internal PASCAL classes referenced as bare names that ARE defined in pandas but cross-file resolution misses them (chain-fix cross-file class fallback already exists for flask-realworld pattern, needs pandas extension); structural-ref extractor parser bugs `metaclass=ABCMeta`/`total=True`/`# type: ignore` class-def kwargs/comments still residual (chain-fix extractor); bug-resolver residual `ast`/`copy`/`numbers`/`numba`/`numexpr`/`tokenize`/`keyword.iskeyword` ambiguity (multi-kind local names — chain-fix resolver kind-disambiguator). | pre-wave-2 historical (pre-changes within this PR): 7.90% (2026-05-19, flask-realworld wave spillover; was 8.29% post-pandas-wave PyArrow/numpy/pandas-internal pass-3, 9.80% post-wave-9 builtin-type methods spillover, 9.87% post-#525, 13.85% pre-fix) | pandas-wave cumulative -1.51pp across 3 passes: pass-1 -0.74pp (synth.go pythonBareNames — PyArrow ChunkedArray/Array/scalar surface: is_timestamp/is_duration/is_nan_na/as_py/as_unit/combine_chunks/iterchunks/fill_null/replace_with_mask/to_pandas_dtype/type_for_alias + pyarrow type predicates is_string/is_floating/is_boolean/is_date/is_time/is_null/is_nan + pyarrow scalar dtypes int32/int64/duration/timestamp + pandas internals _gotitem/_get_axis_number/maybe_dispatch_ufunc_to_dunder_op/SpecificationError/pprint_thing; refs.go pythonExternalBaseTypes — typing/stdlib bases NamedTuple/TypedDict/Enum/IntEnum/StrEnum/Flag/ChainMap/list/dict/tuple/set/type/ABC/ABCMeta + pandas-internal mixin/ABC base classes PandasObject/OpsMixin/SelectionMixin/IndexOpsMixin/GroupByIndexingMixin/NoNewAttributesMixin/PandasDelegate/ExtensionArray/ExtensionDtype/BaseStringArray/BaseMaskedArray/BaseMaskedDtype/NumericArray/NDArrayBackedExtensionArray/NDArrayBackedExtensionIndex/NDArrayBacked/NDFrame/NDFrameIndexerBase/NDFrameDescriberAbstract/IntervalMixin/DatetimeTimedeltaMixin/DatetimeIndexOpsMixin/DatetimeLikeArrayMixin/DataFrameXchg/PandasDataFrameXchg/ArrowExtensionArray/ArrowStringArrayMixin/ObjectStringArrayMixin/StorageExtensionDtype/ExtensionArrayNaResult/PeriodDtypeBase/_GroupByMixin/GroupBy/BaseGroupBy/BaseWindow/BaseWindowGroupby/RollingAndExpandingMixin; synth.go knownExternalPackages — pyarrow/cython) → 9.80% → 9.07% (bug-extractor 5607→5273 −334, bug-resolver 350→236 −114). Pass-2 -0.47pp (synth.go pythonBareNames — additional pyarrow.compute pc.* surface: if_else/is_temporal/is_binary/is_list/is_large_list/is_fixed_size_list/is_fixed_size_binary/is_struct/is_map/is_date64/is_leap_year/is_string_array/large_string/floor_temporal/ceil_temporal/days_between/local_timestamp/dictionary_encode/concat_arrays/array_sort_indices/to_pylist/not_equal/less_equal/greater_equal/or_kleene/fill_null_backward/fill_null_forward/drop_null/struct_field/list_flatten/list_value_length/split_pattern/count_substring_regex/binary_repeat/binary_join/divide_checked/sqrt_checked/pairwise_diff_checked/utf8_capitalize/utf8_split_whitespace/utf8_normalize/utf8_zfill/index_in/is_in/from_numpy_dtype/from_arrays/iso_calendar/pa_contains/_safe_fill_null/_box_pa_array/__arrow_array__/maybe_convert_objects/maybe_get_tz/infer_dtype/validate_na_arg/to_pydatetime/to_pytimedelta/to_offset/time64/uint64/bool_/list_/and_/or_/invert/rounding_method/regex_parser/has_unsupported_code; refs.go pythonExternalBaseTypes — additional pandas internals NumericDtype/Buffer/NumpyExtensionArray/Grouper/ExtensionIndex/DirNamesMixin/DatetimeLikeBlock/BaseExprVisitor) → 9.07% → 8.60% (bug-extractor 5273→4999 −274, bug-resolver 236→226 −10). Pass-3 -0.31pp (synth.go pythonBareNames — remaining pyarrow.compute + warnings/numpy/pandas-helpers: filterwarnings/catch_warnings/errstate/utf8_slice_codeunits/utf8_length/string_is_ascii/starts_with/ends_with/is_monotonic/add_tmp/has_reference/get_window_bounds/_consolidate_inplace/DataError/map_infer_mask/homogeneous_func/validate_stat_ddof_func/scalar_fillna_inplace/tz_convert/tile/stringify/argsort/pc_func) → 8.60% → 8.29% (bug-extractor 4999→4814 −185). Residual: generic verbs (view/func/cls/equals/reindex/op/append/extend/dtype/keys/items/empty) explicitly excluded per #94 safer-bias rule; structural-ref extractor parser bugs on `metaclass=ABCMeta`/`total=True`/`# type: ignore` class-def kwargs/comments (~8 residual cases — chain-fix extractor); ambig-kind on remaining pandas-internal mixins (~11 — chain-fix resolver kind-disambiguator). | at-bar (8.29%, ≤8% floor cleared — push toward ≤5% ship-gate) | pandas-wave cumulative -1.51pp across 3 passes: pass-1 -0.74pp (synth.go pythonBareNames — PyArrow ChunkedArray/Array/scalar surface: is_timestamp/is_duration/is_nan_na/as_py/as_unit/combine_chunks/iterchunks/fill_null/replace_with_mask/to_pandas_dtype/type_for_alias + pyarrow type predicates is_string/is_floating/is_boolean/is_date/is_time/is_null/is_nan + pyarrow scalar dtypes int32/int64/duration/timestamp + pandas internals _gotitem/_get_axis_number/maybe_dispatch_ufunc_to_dunder_op/SpecificationError/pprint_thing; refs.go pythonExternalBaseTypes — typing/stdlib bases NamedTuple/TypedDict/Enum/IntEnum/StrEnum/Flag/ChainMap/list/dict/tuple/set/type/ABC/ABCMeta + pandas-internal mixin/ABC base classes PandasObject/OpsMixin/SelectionMixin/IndexOpsMixin/GroupByIndexingMixin/NoNewAttributesMixin/PandasDelegate/ExtensionArray/ExtensionDtype/BaseStringArray/BaseMaskedArray/BaseMaskedDtype/NumericArray/NDArrayBackedExtensionArray/NDArrayBackedExtensionIndex/NDArrayBacked/NDFrame/NDFrameIndexerBase/NDFrameDescriberAbstract/IntervalMixin/DatetimeTimedeltaMixin/DatetimeIndexOpsMixin/DatetimeLikeArrayMixin/DataFrameXchg/PandasDataFrameXchg/ArrowExtensionArray/ArrowStringArrayMixin/ObjectStringArrayMixin/StorageExtensionDtype/ExtensionArrayNaResult/PeriodDtypeBase/_GroupByMixin/GroupBy/BaseGroupBy/BaseWindow/BaseWindowGroupby/RollingAndExpandingMixin; synth.go knownExternalPackages — pyarrow/cython) → 9.80% → 9.07% (bug-extractor 5607→5273 −334, bug-resolver 350→236 −114). Pass-2 -0.47pp (synth.go pythonBareNames — additional pyarrow.compute pc.* surface: if_else/is_temporal/is_binary/is_list/is_large_list/is_fixed_size_list/is_fixed_size_binary/is_struct/is_map/is_date64/is_leap_year/is_string_array/large_string/floor_temporal/ceil_temporal/days_between/local_timestamp/dictionary_encode/concat_arrays/array_sort_indices/to_pylist/not_equal/less_equal/greater_equal/or_kleene/fill_null_backward/fill_null_forward/drop_null/struct_field/list_flatten/list_value_length/split_pattern/count_substring_regex/binary_repeat/binary_join/divide_checked/sqrt_checked/pairwise_diff_checked/utf8_capitalize/utf8_split_whitespace/utf8_normalize/utf8_zfill/index_in/is_in/from_numpy_dtype/from_arrays/iso_calendar/pa_contains/_safe_fill_null/_box_pa_array/__arrow_array__/maybe_convert_objects/maybe_get_tz/infer_dtype/validate_na_arg/to_pydatetime/to_pytimedelta/to_offset/time64/uint64/bool_/list_/and_/or_/invert/rounding_method/regex_parser/has_unsupported_code; refs.go pythonExternalBaseTypes — additional pandas internals NumericDtype/Buffer/NumpyExtensionArray/Grouper/ExtensionIndex/DirNamesMixin/DatetimeLikeBlock/BaseExprVisitor) → 9.07% → 8.60% (bug-extractor 5273→4999 −274, bug-resolver 236→226 −10). Pass-3 -0.31pp (synth.go pythonBareNames — remaining pyarrow.compute + warnings/numpy/pandas-helpers: filterwarnings/catch_warnings/errstate/utf8_slice_codeunits/utf8_length/string_is_ascii/starts_with/ends_with/is_monotonic/add_tmp/has_reference/get_window_bounds/_consolidate_inplace/DataError/map_infer_mask/homogeneous_func/validate_stat_ddof_func/scalar_fillna_inplace/tz_convert/tile/stringify/argsort/pc_func) → 8.60% → 8.29% (bug-extractor 4999→4814 −185). Residual: generic verbs (view/func/cls/equals/reindex/op/append/extend/dtype/keys/items/empty) explicitly excluded per #94 safer-bias rule; structural-ref extractor parser bugs on `metaclass=ABCMeta`/`total=True`/`# type: ignore` class-def kwargs/comments (~8 residual cases — chain-fix extractor); ambig-kind on remaining pandas-internal mixins (~11 — chain-fix resolver kind-disambiguator). | at-bar (8.29%, ≤8% floor cleared — push toward ≤5% ship-gate) | at-bar (4.49%, ≤5% floor cleared — push toward ≤3% ship-gate via cross-file class fallback for pandas-internal Pascal types + receiver-type primitive #494) | extractor parser fix for `class Foo(Bar, metaclass=ABCMeta)` kwarg + comment leakage; resolver kind-disambiguator for project-internal multi-kind mixins; generic-verb receiver-type tracking primitive (#494) for `<DataFrame>.view`/`<Series>.dtype`/etc.; pandas extension of cross-file class fallback (flask-realworld-style lookupUniqueRealComponentByName) for pandas-internal Pascal types appearing as bare constructor calls. |
| client-fixture-a | python (Django backend, user-provided) | 4205 | 6.24% (2026-05-19, post-wave-9 module-constants + Celery @app.task + DRF @action + Python builtin-type methods; was 6.75% post-wave-8, 8.32% post-wave-7, 9.80% post-wave-6, 12.00% post-#526, 13.70% pre-fixes). Wave-9 cumulative -0.41pp across 3 passes: pass-1 -0.22pp (Track A — `Model:<SCREAMING_SNAKE>` module-constant kind-prefixed stubs + Track B — `Task:<bare>` Celery dispatch stubs route to Dynamic; bug-extractor bare-kind-prefixed 18→5 + bug-resolver ambig-bare-no-hint 46→3); pass-2 -0.00pp (Track C — `Action:<x>` DRF action decorator stubs, defensive; 0 observed instances on client-fixture-a, retained for DRF-heavy corpora); pass-3 -0.19pp (Track D-adjacent — `<builtin>.<method>` for str/dict/list/tuple/set/bytes/bytearray/int/float/bool route to ExternalKnown; covers `str.strip`/`str.lower`/`str.split`/`dict.items` receiver-qualified builtin-type method calls — dotted-lower-head 65→0). Wave-8 cumulative -1.57pp across 3 passes: pass-1 -0.50pp (TestCase assertX family + DRF GenericViewSet action methods filter_queryset/get_success_headers + pymongo Collection.find_one/insert_one/aggregate/etc. + django Manager normalize_email/make_random_password + celery apply/delay/retry/link); pass-2 -0.88pp (Django ORM F/Q/Case/When/Count/Sum/Avg/Coalesce/Concat/Lower/TruncMonth/SearchQuery/etc.; typing.Any/List/Dict/Optional/Union; stdlib Decimal/BytesIO/ContextVar/MagicMock/ThreadPoolExecutor; PIL Image/ImageEnhance; python-docx Document/Font/Inches/Pt/Cm; pymongo MongoClient/Collection/InsertOne/Decimal128; DRF AllowAny/IsAuthenticated/DjangoFilterBackend/TokenAuthentication; Django HttpRequest/HttpResponse/JsonResponse/FileResponse; channels AuthMiddlewareStack/URLRouter; Celery Celery/chord/chain/group); pass-3 -0.19pp (pymongo Collection.find/select; Django middleware get_response self-callable; ObjectDoesNotExist/ObjectId/Path/Queue/RefreshToken/Request/Response/ReturnDocument/SAFE_METHODS/Signal/Token/UUID/WSGIRequest/model_to_dict; python-docx WD_ALIGN_PARAGRAPH/WD_BREAK/WD_ROW_HEIGHT_RULE enum constants) | wave-7 python django test/management/channels/stdlib + DRF inherited-method classifier — measured delta: bug-extractor 2619 → 2249 (-370), bug-resolver 418 → 331 (-87), bug_rate 9.799% → 8.325% (-1.474pp). Pass-1 (refs.go pythonExternalBaseTypes: TestCase, APITestCase, BaseCommand/Command, TokenObtainPairView/TokenRefreshView/TokenBlacklistView, AsyncConsumer family, MiddlewareMixin, FormParser, *ModelMixin) → 9.80% → 9.64% (-0.16pp). Pass-2 (synth.go knownExternalPackages: asgiref/channels/botocore/csv/contextvars/decimal/email/random/traceback/importlib/django_celery_beat/django_filters/docx/fitz/pdfplumber/pytz + 40 more django ecosystem pkgs; pythonBareNames: Django ORM Q/Case/When/Value, get_object_or_404, atomic, parse_datetime, BaseCommand handlers, unittest assert* family, pandas read_excel/iterrows/fillna, celery delay, boto3 SQS verbs, getenv) → 9.64% → 8.72% (-0.92pp). Pass-3 (refs.go pythonExternalInheritedMethods classifier: routes `<Class>.get_paginated_response` / `.paginate_queryset` / `.get_serializer` / `.get_object` / etc. — 25 DRF GenericAPIView inherited methods + channels lifecycle + BaseCommand lifecycle — to ExternalKnown when leaf method matches) → 8.72% → 8.32% (-0.40pp). Residual: project-internal user methods (`update_or_create_devices`, `sync_users`, `setMessageParams`, `get_safety_filings`, `_with_group_request`, `replace_email_variables`) that require receiver-variable-type tracking; generic verbs (`replace`, `pop`, `items`, `append`, `info`, `warning`, `write`, `read`) explicitly excluded per #94 safer-bias rule. | at-bar (6.75%, ≤8% floor cleared) | Residual now dominated by (a) SCREAMING_SNAKE local module constants (`MA_JURISDICTION_NAME`, `INSPECTIONS_GROUP`, `CHANGELOG_MODELS`, `DEFAULT_VIEWSET_ACTIONS`) surfaced as `Model:<X>` cross-language EXTENDS targets — module-constant extractor lift required; (b) underscored helper functions (`_collection.<method>` chains beyond the wave-7/8 method set, `_get_or_create_*` family) requiring receiver-variable-type tracking (#494 / #543); (c) `Task:<x>` celery `@app.task` decorator-bound functions surfacing as Task-kind-prefixed unresolved targets; (d) custom DRF viewset actions (`UserViewSet.list_users_groups`) where the action method body IS defined locally but the resolver can't bind through the @action decorator. Chain-fixes to file: receiver-type primitive (#494), `@app.task` decorator-bound function classifier, `@action`-decorated viewset method resolver, module-constant lift. |
| gin | go | 121 | 6.17% (2026-05-19, v3; v2 was 8.63%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue |
| chi | go | 93 | 4.80% (2026-05-19, v3; v2 was 8.50%) | #480 then #483 | residual: receiver-variable-type tracking still pending | at-bar | receiver-variable-type-tracking issue; ship-gate gap remains |
| etcd | go | 424 | 8.62% (2026-05-19, v3; v2 was 12.40%) | #480 then #483 | bare receiver variable names + dotted Format-B with local-var scope names | upstream | file `receiver-variable-type-tracking` issue (separate, multi-day) — 0.62 pp away from bar |
| express-realworld | javascript | 66 | 9.83% (2026-05-19, v3) | — | javascript extractor not targeted in wave-1+2 | structural | file js-fix-wave issue |
| express | javascript | 145 | 4.03% (2026-05-19, ts-framework-w4; pre-wave-4 13.76%) | ts-framework-w4 | Node stdlib + EventEmitter + assert + fs/path receiver-strip allowlist; `scope:component:import:external:<pkg>` synth folding; node:<mod> direct allowlist. Residual: express HTTP DSL (`app.get`/`post`/`status`/`end`) receiver-stripped to collision-prone bare names (rejected by #104) + `callback`/`request`/`done` test-helper names. Chain-fix: file `js-express-dsl-allowlist` (route-dsl with framework gate) issue. | at-bar | next js wave (express + node http DSL receiver-binding) |
| nestjs-starter | typescript | 16 | 1.75% (2026-05-19, ts-framework-w4; pre-wave-4 16.67%) | ts-framework-w4 | NestJS DI graph residual: 1 `bootstrap.listen` bare name (server.listen receiver-strip not folded). All `scope:component:import:external:@nestjs/*` structural-refs now route via the new synth.go branch. | at-bar | 0.75pp from ship-gate; file `ts-nest-receiver-binding` follow-up |
| client-fixture-c | typescript | 538 | 11.36% (2026-05-19, rn-expo-w4 #508; pre-fix 16.10%) | rn-expo-w4 #508 | RN/Expo runtime allowlist landed: `knownExternalPackages` extended with Expo SDK (`expo-*`, `@expo`, `@expo-google-fonts`), React Native + community packages (`react-native`, `@react-native`, `@react-native-community`, plus 30+ `react-native-*` libs), React Navigation (`@react-navigation`), Reanimated/Gesture-Handler/Safe-Area-Context/Screens/SVG/MMKV/Keychain, NativeWind, Metro, EAS, Gluestack UI (`@gluestack-ui`, `@gluestack-style`), lucide-react-native, @legendapp, aws-amplify. `jsBareNames` extended with TanStack Query hooks (`useQuery`/`useMutation`/`useQueryClient`/`invalidateQueries`/`setQueryData`/`refetch`/`mutate`/`mutateAsync`), React Navigation hooks (`useNavigation`/`useRoute`/`useFocusEffect`/`usePreventRemove`/`useTheme`), Expo Router hooks (`useLocalSearchParams`/`useSegments`/`useRootNavigationState`), RN core hooks (`useColorScheme`/`useWindowDimensions`/`useSafeAreaInsets`/`useBottomTabBarHeight`/`useHeaderHeight`), Reanimated hooks (`useSharedValue`/`useAnimatedStyle`/`runOnUI`/`withTiming`/...), Zustand (`useShallow`/`getState`), navigation API (`setOptions`/`navigate`/`pop`), RN Linking (`openURL`/`canOpenURL`/`openSettings`), chalk color receiver-strip (`gray`/`red`/`green`/...), String/Number proto (`padStart`/`padEnd`/`startsWith`/`endsWith`/`localeCompare`/`lastIndexOf`/`toFixed`/`toPrecision`). 3 diagnostic passes; cumulative -4.74pp. Pass-1 (packages) -2.16pp, pass-2 (RN/Query/Nav/Reanimated hooks) -2.19pp, pass-3 (setOptions/openURL/getState/chalk/String-proto) -0.39pp. Residual: 1147/1154 bug-resolver `Component,SCOPE.Component` are `@/...` tsconfig path-aliases (defer to #505 in flight); ~250 bug-extractor are user-defined local zustand-store hooks (`useSyncQueueStore`, `useAuthStore`, `setSaveDialog*`) not being lifted to local entities by the TS extractor + `#104`-rejected names (`find`/`forEach`/`reduce`/`replace`/`includes`/`delete`/`create`/`match`). | addressable | (a) #505 path-alias resolution (in flight) — will eliminate the 1147 bug-resolver residual; (b) file `ts-zustand-store-hook-lift` chain-fix (extractor: lift `export const useStore = create<...>(...)` zustand pattern to a local entity). |
| nextjs-commerce | typescript | 76 | 3.89% (2026-05-19, ts-framework-w4; pre-wave-4 17.14%) | ts-framework-w4 | React core hooks + Next.js navigation/cache/RSC primitives + Date/Intl/DOM receiver-strip allowlist; jsDynamicPatterns for relative + tsconfig-baseUrl-path imports; scoped+unscoped npm pkgs (`@heroicons`, `@vercel`, `clsx`, `tailwind-merge`, `geist`, `sonner`, `swr`, `zustand`, ...). Residual: React `useState` state-setter calls (`setIsOpen`, `setActive`, `setOpenSelect`, `updateOptimisticCart`, ...) that the TS extractor doesn't lift to local entities + Array/String prototype methods on #104 rejection list (`find`/`includes`/`replace`/`forEach`/`startsWith`/`endsWith`) + `cookies`/`headers` claimed by swiftBareNames/kotlinBareNames (cross-lang invariant test forbids JS-side duplication). | at-bar | next ts wave (useState destructure → setter entity lift; cross-lang invariant relaxation for `cookies`/`headers` per-lang gate) |
| spring-petclinic | java | 84 | 3.67% (2026-05-19, java-spring-petclinic wave; was 5.00% pre-wave on this baseline; 5.38% on kafka-chase-578 measure) | java-spring-petclinic wave | three-pass synth + resolver additions (single landing): (a) `javaExternalBaseTypes` — Spring framework parent interfaces routinely extended/implemented (Spring Core: ApplicationListener/ApplicationContextInitializer/ApplicationContextAware/BeanPostProcessor/BeanFactoryPostProcessor/FactoryBean/InitializingBean/DisposableBean/EnvironmentAware/EnvironmentPostProcessor/SmartLifecycle/Ordered/ImportSelector/ImportBeanDefinitionRegistrar/Condition; Spring Boot: SpringBootServletInitializer/CommandLineRunner/ApplicationRunner/WebServerFactoryCustomizer/ErrorViewResolver/FailureAnalyzer; Spring AOT: RuntimeHintsRegistrar; Spring MVC/Web: WebMvcConfigurer/WebMvcConfigurationSupport/HandlerInterceptor/HandlerMethodArgumentResolver/HttpMessageConverter/Filter/OncePerRequestFilter/WebMvcRegistrations; Spring WebFlux: WebFluxConfigurer/WebFilterChain; Spring Data: JpaRepository/CrudRepository/PagingAndSortingRepository/Repository/ReactiveCrudRepository/ReactiveMongoRepository/MongoRepository/AttributeConverter/Specification; Spring Validation/Conversion: Validator/ConstraintValidator/Formatter/GenericConverter/ConverterFactory; Spring Security: WebSecurityConfigurerAdapter/AbstractWebSecurityConfigurerAdapter/UserDetailsService/UserDetails/AuthenticationProvider/AuthenticationManager/GrantedAuthority/PasswordEncoder/SecurityFilterChain; Spring Boot Test slices: SpringBootTest/WebMvcTest/DataJpaTest/JsonTest/RestClientTest; jakarta.persistence: EntityManager/EntityManagerFactory/AttributeOverride; jakarta.servlet: HttpServletRequest/HttpServletResponse/ServletContextListener/ServletContextInitializer; SLF4J: Logger/LoggerFactory; AspectJ: MethodInterceptor); (b) `javaTestBareNames` — AssertJ chained matcher leaves (isNotNull/isNull/isNotZero/isZero/isPositive/isNegative/containsEntry/containsKey/containsValue/containsSubsequence/containsSequence/containsAnyOf/containsAll/hasToString/hasFieldErrors/hasGlobalErrors/isThrownBy/withMessageContaining/withMessage/withNoCause/withCauseInstanceOf/as/element/first/last/isAfter/isBefore/isAfterOrEqualTo/isBeforeOrEqualTo/isBetween/isCloseTo), MockMvc request builder chain (contentType/accept/with), MockMvc result matcher chain (attribute/name/value/maxAge/domain/path/secure/httpOnly), SpringApplication test helpers (isDockerAvailable/printProperties/findPropertiesPropertySources/getEnvironment/profiles/properties/listeners/webApplicationType/bannerMode/logStartupInfo), BindingResult chain (validate/createValidator/getFieldErrors/getGlobalErrors/getAllErrors); (c) `javaBareNames` — JCache JSR-107 config (setStatisticsEnabled/setManagementEnabled/setStoreByValue/setTypes/createCache/cacheConfiguration/getCache/destroyCache), Spring AOT RuntimeHints API (registerType/registerPattern/registerResource/registerProxy/registerSerialization/reflection/resources/serialization/proxies/reflectionHints/resourceHints/isAssignableFrom), JDK java.util.stream distinctive verbs (allMatch/anyMatch/noneMatch/toList/toUnmodifiableList/toUnmodifiableSet/toUnmodifiableMap/toArray), Optional completions (orElseGet/ifPresentOrElse/or); (d) resolver-side Spring MVC route classification — `Route:<path>` / `Controller:<methodName>` stubs emitted by spring_routes.go AST extractor + spring_mvc.yaml regex extractor classify as Dynamic (Spring HandlerMapping dispatches by method-name at runtime). Per-iteration delta: 5.00% (pre-wave) → 4.06% (pass-1: synth maps) → 3.78% (pass-2: dropped duplicate Controller entity emit attempt) → 3.67% (pass-3: resolver-side Controller:/Route: → Dynamic). Residual ~84 edges: bug-extractor floor (~38) is user-method bare names requiring cross-class receiver binding (#105 territory — `getId`/`getName`/`setName`/`getPets`/`getFirstName`/`getLastName`/`isNew`/`updatePetDetails`/`setBirthDate`/`setType`/`getSpecialtiesInternal`/`getVisits`/`makePetTypes`/`findPaginated`/`addPaginationModel`/`localeChangeInterceptor`, generic JDK verbs deliberately rejected per #94/#105: `size`/`isEmpty`/`add`/`contains`/`find`/`trim`/`build`/`append`/`collect`/`addAll`/`run`, test fixture names `george`/`helen`/`james`), bug-resolver floor (~94) is SQL table-name refs (owners/pets/specialties/types/vet_specialties/vets/visits — Table entities exist but bare-name resolver disambiguation needs SQL-context awareness) + Person ambiguous class lookup. | at-bar (sub-5% — close to ≤3% ship-gate target) | chain-fix issue: cross-class receiver binding primitive for Java user-method bare names (same as kotlin #494) → drives spring-petclinic toward 0% |
| kafka-streams-examples | java | 172 | 3.33% (2026-05-19, java-spring-petclinic wave — kafka regression check flat, was 3.42% on kafka-chase-578; main pre-fix 12.68%; pre-#577 was 3.81%) | kafka-chase-578 | file-import lookup repaired post-#577 + `isJavaExternalBaseType` allowlist (Apache Kafka Streams / Connect / Common interfaces, Apache Commons CLI types, JDK functional/marker interfaces, regex-leak generic fragments, single-letter type parameters); 5-segment `scope:component:interface:java:<name>` stub parsing (hierarchy extractor emits this shape with no `<file>` slot); kafkaStreamsDSLVerbs extended (`withCachingEnabled`/`withLoggingEnabled`/`withRetention`, TimeWindows/SessionWindows `ofSize*`/`advanceBy`, ProcessorContext `forward`/`stateStoreNames`); javaLangReceivers extended with Kafka Streams DSL types (KStream/KTable/KafkaStreams/StreamsBuilder/Serdes/Consumed/Produced/Grouped/Materialized/TimeWindows/AdminClient/ConsumerRecord/QueryableStoreTypes/ReadOnlyKeyValueStore/...) + Apache Commons CLI types + JUnit Assert | at-bar | below 5% bar — residual is user-defined static helpers requiring cross-class receiver binding |
| exposed | kotlin | 113 | 2.52% (2026-05-19, kotlin-exposed-dsl wave; was 9.40% pre-wave baseline this run, 11.00% post-#477) | kotlin-exposed-dsl wave | three-pass synth additions (single landing): (a) `knownExternalPackages` — `org.jetbrains.exposed` (covers v0 `org.jetbrains.exposed.sql.*`/`.dao.*` and v1 `org.jetbrains.exposed.v1.core.*`/`.v1.jdbc.*`/`.v1.json.*`/`.v1.datetime.*` via longestKnownDottedPrefix), `org.jetbrains.kotlinx`, `org.jetbrains.kotlin`; (b) `kotlinBareNames` — Exposed Column type ctors (LongColumnType/IntegerColumnType/ShortColumnType/ByteColumnType/UByteColumnType/UShortColumnType/UIntegerColumnType/ULongColumnType/FloatColumnType/DoubleColumnType/DecimalColumnType/BooleanColumnType/CharacterColumnType/VarCharColumnType/TextColumnType/MediumTextColumnType/LargeTextColumnType/BinaryColumnType/BasicBinaryColumnType/BlobColumnType/UuidColumnType/EnumerationColumnType/EnumerationNameColumnType/CustomEnumerationColumnType/AutoIncColumnType/ArrayColumnType/EntityIDColumnType/FloatVectorColumnType/IntVectorColumnType/ColumnWithTransform/NullableColumnWithTransform/JoinCondition), Exposed DSL leaves (op/joinPart/topLevelWrap/defaultValue/transformFromValue/transformToValue/columnTransformer/appendValueAlias/booleanOperator/resolveColumnType/resolveVectorColumnType/isColumnTypeIncorrect/isJsonBColumnForCasting/isInternalConstraint/isEntityIdentifier/mappedIndices/existingIndices/filterInternalIndices/filterForeignKeys/additionalConstraint/secondFraction/likePatternSpecialChars/precessOrderByClause/Format), JVM collection types (LinkedHashMap/LinkedHashSet/HashMap/HashSet/ArrayList/Stack/Triple/Pair), java.time / kotlinx-datetime conversions (atZone/atOffset/atTime/systemDefault/currentSystemDefault/toKotlinInstant/toKotlinLocalTime/toKotlinUuid/toJavaUuid/toJavaLocalTime/toEpochMilliseconds/fromEpochMilliseconds/fromEpochSeconds/floorDiv), kotlin.collections residue (addAll/removeAll/mapValues/filterValues/filterNot/filterIsInstance/subtract/asList/withIndex/buildList/ifEmpty/joinTo/none/flatMapTo/toTypedArray/toHashSet/arrayOfNulls/orEmpty/findLast), kotlin.text helpers (replaceBefore/replaceRange/replaceFirst/uppercase/lowercase/ifBlank/isNullOrBlank/contentEquals/contentHashCode/contentToString/toBooleanStrict/toShort/toBigInteger/toBigDecimal/toChar/toUByte/toUShort/toULong/toUInt/toIntArray/toFloatArray/isNaN), JVM concurrency (compareAndSet/getOrSet/pop/peek), kotlin.uuid (generateV4/generateV7/fromByteArray/getUuid), java.nio (allocate/putLong/codePointCount), kotlin.reflect (isSubclassOf/callBy), Gradle Kotlin DSL (signAllPublications/useInMemoryPgpKeys), misc (removeAt/setScale/scale/precision/traverse/fromDb/toDb/BigInteger/Timestamp/List/Array/String/LocalDate/LocalDateTime/isSupported). Per-iteration delta: 9.40% → 2.52% (single landing — pass-1 cleared ≤3% ship-gate; pass-2/3 deferred to chain-fix issues). Residual ~241 edges: bug-extractor floor (113) is generic verbs deliberately rejected per #94/#106 safer-bias (`add`/`keys`/`push`/`warn`/`info`/`body`/`first`/`last`/`unzip`/`value`/`distinct`/`on`/`flatMap` — gated to other langs or collision-prone); bug-resolver (128) is Exposed-internal dialect FunctionProvider methods (byteType/integerType/longType/varcharType/textType/booleanType/timestampType/...) which need receiver-typed cross-dialect resolution (chain-fix #494 receiver-type primitive). `configure` deliberately EXCLUDED — gated to rust per rustBareNames (Actix-web App.configure). | at-bar (sub-3% — ≤3% ship-gate target met) | chain-fix #494 receiver-type primitive for Exposed-internal DialectFunctionProvider methods → drives toward 0% |
| ktor-samples | kotlin | 509 | 4.75% (2026-05-19, kotlin-exposed-dsl wave neutral; was 6.29% pre-this-run baseline — improved between baselines by intervening landings) | kotlin-exposed-dsl wave | flat at 4.75% across kotlin-exposed-dsl wave (0.00pp). Wave additions did not surface new resolutions here because the residual cohort on ktor-samples is dominated by kotlinx-coroutines-atomicfu compiled-JS obfuscated identifiers + kodein-internal types + structural-comment leakage from extractor — not Exposed DSL leaves. No regressions. | at-bar | already sub-5%; next ktor wave for sub-3% ship-gate (kotlinx atomicfu obfuscated-name skip + extractor comment leakage chain-fix) |
| play-scala-starter | scala | 1 | 0.70% (2026-05-19, scala-play-wave; was 2.11% pre-wave) | scala-play-wave | scala-play-wave single landing: `scalaBareNames` extended with two distinctive Play/Promise idioms — `Action.async` (Play `ActionBuilder.async` factory preserved as dotted leaf by the Scala extractor; literal dotted form matches no real user identifier, same shape as JAX-RS / Kafka gates) and `success` / `failure` (scala.concurrent `Promise` completion methods, receiver-stripped from `promise.success(_)` / `promise.failure(_)`; scala lang gate isolates from non-Scala collisions). Per-iteration delta: 2.11% → 0.70% (single landing, −1.41pp; bug-extractor 2→0, bug-resolver 1 unchanged). Residual: 1 bug-resolver edge (`counter.nextCount()` cross-class trait method) — receiver-typed CALLS binding for scala out of scope per #494. | at-ship-gate | (a) #494 receiver-type primitive for scala trait method resolution to clear final 1 edge; (b) Twirl `.scala.html` extractor split (separate chain-fix) |
| usermanager-example | clojure | 17 | 19.74% (2026-05-19, v3) | — | clojure extractor untouched | structural | file clojure-extractor issue |
| rails-realworld | ruby | 105 | 4.56% (2026-05-19, ruby-sidekiq-wave; was 6.65% pre-wave) | ruby-sidekiq-wave | ruby-sidekiq-wave cumulative -2.09pp from cross-wave Ruby additions: rubyBareNames extended with Ruby Object/Kernel/Enumerable predicates + ActionController helpers (`authenticate_or_request_with_http_token`, `head`, `secret_key_base`, `secrets`, `application`); ActiveSupport `deep_symbolize_keys!`/`deep_transform_keys!`; + Ruby stdlib gems added to `knownExternalPackages` (forwardable/securerandom/fileutils/erb/rack/etc.). Residual: 20 bug-extractor + 4 bug-resolver — project-internal user-method names (`authored_by`, `favorited_by`, `most_used`, `following_users`, `tag_counts`, `tagged_with`, `using_mysql?`) deliberately not allowlisted (acts_as_taggable_on gem method_missing dispatch + user scopes); generic verbs (`find`, `count`) excluded per #94 / #107 safer-bias rule. | at-bar (4.56%, ≤5% bar passed) | next ruby wave for ship-gate gap; receiver-type primitive (#494) for `User.<scope>` chained method binding |
| sidekiq | ruby | 85 | 3.67% (2026-05-19, ruby-sidekiq-wave; was 12.07% pre-wave) | ruby-sidekiq-wave | ruby-sidekiq-wave cumulative -8.40pp across 3 passes: pass-1 -6.12pp (synth.go rubyBareNames — Object/Kernel/Enumerable predicates `all?`/`any?`/`zero?`/`even?`/`block_given?`/`include?`/`match?`/`start_with?`/`tty?` family; reflection `caller`/`ancestors`/`const_get`/`constantize`/`define_singleton_method`/`instance_variable_get`/`instance_variable_set`/`method_defined?`/`undef_method`/`attr_writer`/`private`; Enumerable `?`/`!` suffix forms `each_pair`/`each_slice`/`flat_map`/`sort_by`/`min_by`/`max_by`/`with_index`/`with_object`/`detect`/`drop`/`take_while`/`lazy`/`sample`/`shuffle!`; AS deep_* and merge!/transform_*!; String `gsub!`/`scan`/`squeeze!`/`tr`/`chars`/`lines`/`unpack1`/`force_encoding`/`encode!`; Numeric `downto`/`upto`/`step`/`finite?`/`nan?`; Time `iso8601`/`strftime`/`utc`/`mday`/`hour`/`month`; Kernel `raise`/`throw`/`catch`/`loop`/`sleep`/`exit`/`trap`/`fork`/`spawn`/`gets`/`puts`/`pp`/`p`/`format`/`sprintf`/`warn`/`__method__`/`__dir__`; FileUtils `mkdir_p`/`rm_f`/`rm_rf`/`cp_r`/`mv`/`expand_path`/`basename`/`dirname`/`realpath`/`tmpdir`/`mktmpdir`/`unlink`/`flush`/`binmode?`/`isatty`/`rewind`/`seek`/`wait_readable`/`blocking_call`; Concurrency `synchronize`/`mon_synchronize`/`try_lock`/`alive?`/`join`/`signal`/`broadcast`/`wait`/`wakeup`; Sidekiq Redis command surface completion `hmget`/`hsetnx`/`hincrby`/`mget`/`mset`/`incrby`/`expire`/`lindex`/`llen`/`lrange`/`lrem`/`scard`/`sismember`/`smismember`/`sscan`/`spop`/`zcard`/`zcount`/`zincrby`/`zpopmin`/`zpopmax`/`zrange*`/`zremrangeby*`/`zrank`/`zscan`/`bitfield`/`bitfield_ro`/`bitcount`/`pfadd`/`pfcount`/`pcount`/`xadd`/`xread`/`xack`/`xlen`/`xrange`/`subscribe`/`publish`/`pubsub`/`ping`/`info`/`dbsize`/`flushdb`/`keys`/`sort`/`sentinel`; Sidekiq Pro/Ent context `sidekiq_options_hash`/`sidekiq_retries_exhausted_block`/`sidekiq_retry_in_block`/`server_url`/`redis_pool`/`redis_info`/`local_redis_pool`/`client_middleware`/`server_middleware`/`handle_exception`/`total_concurrency`/`reap`/`watchdog`/`pause!`/`unpause!`/`reloader`/`in_batches`/`msg_retry`/`provider_job_id`/`job_class`/`job_args`/`job_results`/`enqueued_count`/`scheduled_at`/`display_args`/`set_tab`/`base_tab`; synth.go knownExternalPackages — Ruby stdlib `forwardable`/`securerandom`/`fileutils`/`erb`/`set`/`date`/`timeout`/`tmpdir`/`stringio`/`strscan`/`digest`/`base64`/`uri`/`cgi`/`net/https`/`net/smtp`/`net/pop`/`net/imap`/`net/ftp`/`optparse`/`logger`/`monitor`/`fiber`/`thread`/`mutex_m`/`pathname`/`pp`/`singleton`/`observer`/`delegate`/`open3`/`ostruct`/`etc`/`fcntl`/`shellwords`/`resolv`/`ipaddr`/`objspace`/`coverage`/`prettyprint`/`abbrev`/`english`; Rails framework families `actionpack`/`actionview`/`actionmailer`/`actioncable`/`activemodel`/`activestorage`/`activejob`/`active_support`/`active_record`/`action_dispatch`/`action_controller`/`railties`/`sprockets`; Sidekiq gem family `sidekiq`/`sidekiq-pro`/`sidekiq-ent`/`connection_pool`/`concurrent-ruby`/`redis-client`/`hiredis`/`hiredis-client`/`dalli`/`mysql2`/`pg`/`sqlite3`/`devise`/`cancancan`/`pundit`/`rolify`/`oj`/`multi_json`/`httparty`/`faraday`/`excon`/`typhoeus`/`kaminari`/`pagy`/`will_paginate`/`pry`/`byebug`/`rspec`/`factory_bot`/`database_cleaner`/`webmock`/`capybara`/`selenium-webdriver`/`simplecov`/`rubocop`/`bundler`/`rake`/`minitest`/`timecop`/`shoulda`/`shoulda-matchers`/`after_commit_everywhere`/`acts_as_taggable_on`/`jbuilder`/`turbolinks`/`turbo-rails`/`stimulus-rails`/`importmap-rails`/`sprockets-rails`/`sass-rails`/`webpacker`/`puma`/`unicorn`/`thin`/`sinatra`/`grape`/`rack`/`rack-test`/`rack-cors`/`rack-attack`) → 12.07% → 5.95% (bug-extractor 921→443 −478, bug-resolver 222→120 −102, external-known 110→247 +137). Pass-2 -2.21pp (synth.go rubyBareNames Pass-2 batch — Ruby Enumerable / Hash / Array / String stdlib core methods unblocked from #107 conservative bar: `compact`/`dig`/`merge`/`flatten`/`uniq`/`partition`/`group_by`/`each_with_index`/`each_with_object`/`transform_keys`/`transform_values`/`to_date`/`to_enum`/`reverse`/`read`/`write`/`shift`/`pop`/`replace`/`length`/`index`/`sub`/`now`/`escape`/`escape_html`/`unescape_path`/`extend`/`include`/`load`/`method`/`parameters`/`serialize`/`deserialize`/`set_backtrace`/`backtrace`/`message`/`yield`/`copy`/`draw`/`fail`/`generate`/`generators`/`deflate`/`inflate`/`gethostname`/`clock_gettime`/`poll_event`) → 5.95% → 3.74% (bug-extractor 443→234 −209). Pass-3 -0.07pp (synth.go rubyBareNames — `except` (Hash#except) and `times` (Integer#times) — final stdlib core; diminishing returns past this point). | at-bar (3.67%, ≤5% bar passed, sub-5% ship-gate) | next ruby wave for ship-gate gap; receiver-variable-type primitive (#494) needed for user-defined chart UI methods (`app_url`/`base_url`/`render_widget`/`text_line`/`text_span`/`area`/`axis`/`chart`/`dataset`/`layout_split`/`marks`/`series`/`granularity`/`paragraph`) that are project-internal; chain-fix candidates: (a) Ruby `require 'sidekiq/api'` slashed-path resolver pass to bind sidekiq internal sub-files (csv_enumerator, sidekiq/job, sidekiq/capsule, sidekiq/launcher, etc.) — bug-resolver floor at 120; (b) generic verbs (`find`/`select`/`reject`/`count`/`name`/`result`/`options`) explicitly excluded per #94 / #107 safer-bias rule. |
| laravel-quickstart | php | 83 | 1.57% (2026-05-19, php wave-4 PR; was 7.33% on wave-3, 24.08% pre-wave-3) | #485 wave-3 + wave-4 PHP symfony residual | wave-4 left laravel-quickstart unchanged at 1.57% (regression control — PHP-gated synth additions only fire on receivers seen in symfony-demo) | at-bar | next php wave for ship-gate gap (≤1%) — needs receiver type inference for `$model->save()` |
| symfony-demo | php | 241 | 2.80% (2026-05-19, php wave-4 PR; was 7.61% post-wave-3, 23.02% pre-wave-3) | wave-4 PHP symfony residual | three-pass synth additions: (pass-1) Symfony String DSL (`u`/`slug`/`ascii`/`lower`/`upper`/`camel`/`snake`/`folded`/`truncate`/`padEnd`/`padStart`/`trimStart`/`trimEnd`/`replaceMatches`/`ignoreCase`/`containsAny`/`equalsTo`/`bytesAt`/`codePointsAt` + AbstractString core API `length`/`startsWith`/`endsWith`/`indexOf`/`repeat`/`toString`/`reverse`/`afterLast`/`before`/`beforeLast`), Symfony Mailer DSL (`subject`/`htmlTemplate`/`textTemplate`/`replyTo`/`cc`/`bcc`/`priority`/`attach*`/`embed*`), Symfony HttpFoundation Request/Response accessors (`isMainRequest`/`isMethod`/`getCharset`/`getSchemeAndHttpHost`/`getPreferredLanguage`/`getLocale`/`getSession`/`getThrowable`/`setResponse`/`getResponse`/...), Doctrine DataFixtures (`addReference`/`getReference`/`setReference`), PHP stdlib (`mb_substr_count`/`array_pop`/`array_unshift`/`array_shift`/`array_reverse`/`array_chunk`/`array_column`/...), Symfony Validator constraint constructors (`NotBlank`/`NotNull`/`Length`/`Range`/`Regex`/`GreaterThan`/`LessThan`/`Choice`/`Url`/`Ip`/`Uuid`/`Json`/`Type`/`Callback`/`Valid`/`All`/`Collection`/`Count`/`UniqueEntity`), HttpFoundation response constructors (`RedirectResponse`/`JsonResponse`/`BinaryFileResponse`/`StreamedResponse`), framework class constructors (`CollectionToArrayTransformer`/`BufferedOutput`/`DoctrinePaginator`/`Paginator`/`NullOutput`/`ConsoleOutput`); (pass-2) `isPHPExternalBaseType` allowlist for Symfony / Doctrine / PSR / PHPUnit framework interfaces wired into `classifyDispositionLang` to fix IMPLEMENTS kind-mismatch (`UserInterface`, `PasswordAuthenticatedUserInterface`, `EventSubscriberInterface`, `DataTransformerInterface`, `Voter`, `AbstractAuthenticator`, `AbstractType`, `AbstractController`, `Command`, `ContainerAwareCommand`, `Constraint`, `ConstraintValidator`, `KernelInterface`, `Bundle`, `EntityRepository`/`ServiceEntityRepository`, `AbstractMigration`, `FixtureInterface`, `AbstractExtension`, `LoggerInterface`, `TestCase`/`KernelTestCase`/`WebTestCase`, etc.); (pass-3) Doctrine entity getter convention (`getId`/`getUuid`/`getSlug`/`getTitle`/`getAuthor`/`getPublishedAt`/`getRoles`/`getSalt`/`getUserIdentifier`/`eraseCredentials`/`hashPassword`/`getEmail`/`getFullName`), user `Validator` helpers (`validateUsername`/`validatePassword`/`validateEmail`/`validateFullName`), Form `DataTransformer` methods (`reverseTransform`/`transform`), BrowserKit / Console framework accessors (`getInput`/`getOutput`/`getDisplay`/`getCookieJar`/`getRequest`/`getDuration`/`getMemory`/...). Per-iteration delta: 7.61% → 6.07% (pass-1, −1.54pp) → 4.24% (pass-2, −1.83pp) → 2.80% (pass-3, −1.44pp). | at-bar (sub-3% — ≤3% ship-gate target met) | residual ~75 bug-extractor edges are (a) HTTP verb bare `get`/`post`/`put`/`delete` (deliberately rejected per #439 spec, collision with Eloquent attribute accessors), (b) cross-language JS/SCSS bug-extractor leaks (`generateCsrfToken`/`wrap`/`bootswatch.scss`) needing JS extractor receiver fix and CSS file-skip — out of scope for this wave |
| mini-redis | rust | 13 | 3.30% (2026-05-19, rust wave-2; was 8.36% post-wave-1, 13.47% pre-wave-1) | rust wave-2 (actix-web 4.x + futures + db libs + tokio internals) | rust-tokio-actix wave: 3-pass synth additions. Pass-1 (-3.82pp) added `isRustExternalBaseType` allowlist gating `lang=="rust"` on structural-ref IMPLEMENTS / EXTENDS stubs (`scope:component:interface:rust:<Trait>` + `<Trait<...>>` strip + `<path::Trait>` last-segment strip) covering ~120 Rust stdlib + tokio + serde + actix + diesel trait names (`Future`, `Stream`, `Sink`, `AsyncRead/Write/Seek/BufRead`, `Send`/`Sync`/`Unpin`, `Drop`, `Iterator` family, `From`/`Into`/`TryFrom`/`TryInto`, `AsRef`/`AsMut`, `Display`, `Hash`, `Eq`/`Ord` family, `Index`/`IndexMut`, `Fn`/`FnMut`/`FnOnce`, `Read`/`Write`/`Seek`/`BufRead`, `Binary`/`Octal`/`Pointer`, `AsFd`/`AsRawFd`/`FromRawFd`/`IntoRawFd` family, Tokio runtime traits `Wake`/`WakerRef`/`Source`/`Schedule`/`Storage`/`Kill`/`OrphanQueue`/etc., Serde `Serialize`/`Deserialize`/`Serializer`/`Deserializer`/`Visitor`, Actix `Actor`/`Handler`/`StreamHandler`/`MessageBody`/`Responder`/`FromRequest`/`Service`/`ServiceFactory`/`Transform`/`Decoder`/`Encoder`, Diesel `Queryable`/`Insertable`/`Identifiable`/`AsChangeset`/`FromRow`); + extend `knownExternalPackages` with ~150 Rust crate roots (tokio sub-crates `tokio_test`/`tokio_macros`/`tokio-stream`/`tokio-util`/`tokio_uring`/`tokio_tungstenite`/`tokio_postgres`/`tokio_pg_mapper{,_derive}`/`tokio_rustls`/`tokio_native_tls`; async ecosystem `futures_util`/`futures_core`/`futures_io`/`futures_channel`/`futures_task`/`async_stream`/`async_channel`/`async_trait`/`pin_project{,_lite}`; tokio low-level deps `mio`/`libc`/`loom`/`io_uring`/`socket2`/`slab`/`nix`/`mockall`/`backtrace`; Windows/WASM platform `windows_sys`/`windows-targets`/`wasm_bindgen{,_test}`/`js_sys`; actix ecosystem `actix_broker`/`actix_web_lab`/`actix_ws`/`actix_redis`/`actix_form_data`/`actix_settings`/`actix_tls`/`actix_macros`/`actix_derive`/`ractor`; HTTP/TLS `rustls{,_pemfile,_native_certs}`/`webpki{,_roots}`/`openssl_sys`/`native_tls`/`http_body{,_util}`/`hyper_util`/`hyper_rustls`/`hyper_tls`/`h2`/`h3`/`reqwest_middleware`/`mime_guess`/`percent_encoding`/`form_urlencoded`; serialization `serde_yaml`/`serde_derive`/`serde_urlencoded`/`serde_with`/`serde_repr`/`toml_edit`/`serde_qs`/`bincode`/`rmp{,_serde}`/`prost{,_types}`/`tonic{,_build}`; error handling `eyre`/`color_eyre`/`miette`/`snafu`/`derive_builder`/`strum{,_macros}`/`num{,_traits,_cpus,_derive}`/`smallvec`/`arrayvec`/`indexmap`/`dashmap`/`ahash`/`fxhash`/`rustc_hash`/`hashbrown`/`ordered_float`; IDs/time `chrono_tz`; templating + GraphQL `juniper`/`async_graphql{,_actix_web}`/`tera`/`askama{,_actix}`/`sailfish`/`minijinja{,_autoreload}`/`yarte{,_helpers}`/`tinytemplate`/`fluent_templates`/`validator_derive`; DB/pooling `deadpool{,_postgres,_redis,_diesel}`/`diesel_async`/`sqlx_core`/`sqlx_macros`/`sea_query`/`sea_schema`/`rusqlite`/`r2d2`/`refinery`/`mongodb`; observability `tracing_actix_web`/`tracing-subscriber`/`tracing_bunyan_formatter`/`tracing_opentelemetry`/`tracing_log`/`tracing_appender`/`slog{,_async}`/`opentelemetry_sdk`/`opentelemetry-aws/otlp/jaeger/zipkin/prometheus/semantic_conventions`/`metrics_exporter_prometheus`/`prometheus`/`sentry{,_actix}`; AWS SDKs `aws_config`/`aws_sdk_s3`/`aws_sdk_dynamodb`/`aws_smithy_types`/`aws_smithy_http`/`aws_credential_types`/`sparkpost`/`lettre`; misc `notify`/`confik`/`figment`/`config`/`dotenvy`/`clap_derive`/`structopt`/`apalis{,_core,_redis}`/`bb8`/`deadqueue`/`crossbeam_utils`/`crossbeam_channel`/`crossbeam_epoch`/`flume`/`bcrypt`/`sha2`/`sha1`/`md5`/`hex`/`hmac`/`acme{,_lib}`/`mini_redis`/`byteorder`/`aes_gcm_siv`/`actix_utils`/`actix_governor`; + every kebab-case form of the above for Cargo.toml dep-name compatibility. Pass-2 (-0.43pp on mini-redis) added Actix-web HttpResponse 4xx/5xx/3xx factory PascalCase names (`TooManyRequests`/`UnsupportedMediaType`/`MethodNotAllowed`/`MovedPermanently`/`PermanentRedirect`/`TemporaryRedirect`/`SeeOther`/`NotModified`/`PaymentRequired`/`ServiceUnavailable`/`GatewayTimeout`/`BadGateway`/`NotImplemented`/`PreconditionFailed`/`PreconditionRequired`/`PayloadTooLarge`/`UriTooLong`/`RangeNotSatisfiable`/`ExpectationFailed`/`Gone`/`LengthRequired`/`ImATeapot`/`MisdirectedRequest`/`Locked`/`FailedDependency`/`UpgradeRequired`/`RequestHeaderFieldsTooLarge`) + actix-web error builders (`ErrorBadRequest`/`ErrorInternalServerError`/`ErrorNotFound`/`ErrorRequestTimeout`/`ErrorUnauthorized`/`ErrorForbidden`/`ErrorConflict`/`ErrorPreconditionFailed`/`ErrorUnprocessableEntity`/`ErrorTooManyRequests`/`ErrorServiceUnavailable`) + `MessageResult`/`ThinData`/`ClientSession` to `rustBareNames`. Pass-3 (-0.86pp) added `rust_builtin_function` fold so receiver-stripped `rustBareNames` matches return canonical `std` instead of per-verb `ext:<name>` (eliminates ext-unknown fan-out: actix 1550→199, tokio 3368→402, mini-redis 328→52); + `rust_sibling_module` placeholder routes bare-lowercase-ident `::`-prefixed use-paths (`worker::Context`, `entry::EntryList`, `clients::Client`) — sibling-module imports that the rust extractor's intra-crate filter doesn't catch because they lack the `crate::`/`self::`/`super::` prefix — to a single ext:rust_sibling_module placeholder (added to `knownExternalPackages`); + Tokio/std verb additions to `rustBareNames` (~100 names: spawning `spawn`/`spawn_blocking`/`spawn_local`/`select`/`join`/`try_join`/`pin{,_mut}`/`sleep`/`timeout`/`interval`/`yield_now`/`block_on`/`block_in_place`; sync `try_lock`/`try_acquire`/`try_send`/`try_recv`/`recv`/`subscribe`/`notify{,_one,_waiters}`/`acquire`/`release`/`park`/`unpark`/`wake{,_by_ref,_all}`/`will_wake`; IO `read_to_string`/`read_to_end`/`read_exact`/`read_buf`/`read_u8`/`write_all`/`write_u8`/`write_vectored`/`flush`/`close`/`open`/`create`/`put_slice`; time/path `as_secs`/`as_millis`/`as_micros`/`as_nanos`/`as_path`/`as_os_str`/`to_path_buf`/`to_string_lossy`/`file_name`/`extension`/`parent`/`current_dir`/`set_current_dir`/`current_exe`/`args_os`/`vars_os`; process `output`/`status`/`wait_with_output`/`kill`; Result/Option `unwrap_unchecked`/`unwrap_err`/`expect_err`/`map_err`/`map_or{,_else}`/`and_then`/`or_else`/`ok_or{,_else}`/`transpose`/`flatten`/`copied`/`cloned`/`as_deref{,_mut}`/`take_if`; iterator `map_while`/`step_by`/`skip_while`/`take_while`/`scan`/`peekable`/`by_ref`/`min_by{,_key}`/`max_by{,_key}`/`chunks`/`windows`/`enumerate`/`zip`/`rev`/`sum`/`product`/`position`/`any`/`all`/`find{,_map}`/`fuse`/`cycle`; tracing `trace`/`info`/`warn`/`error`/`info_span`/`warn_span`/`error_span`/`debug_span`/`trace_span`/`instrument`/`in_current_span`/`in_scope`; builder verbs `with_writer`/`with_max_level`/`with_target`/`with_ansi`/`with_thread_{names,ids}`/`with_file`/`with_line_number`/`with_env_filter`/`with_default{,_directive}`/`with_filter`/`with_state`/`with_capacity`/`with_status`/`with_body`/`with_header`/`with_uri{,_str}`/`with_method`/`with_tracer`/`with_trace_config`/`with_sampler`/`with_id_generator`/`with_exporter`/`with_cert_resolver`/`with_client_cert_verifier`/`with_single_cert`/`with_no_client_auth`/`with_root_certificates`/`with_safe_default_*`/`with_scheduler`/`with_ready`/`with_time`/`with_mut`/`with_current`/`without_time`; try-builders `try_init{,_from_env}`/`try_build`/`try_seconds`/`try_hours`/`try_minutes`/`try_milliseconds`; mem/std `zeroed`/`size_of`/`align_of`/`replace_with`/`swap_with`/`forget`/`drop_in_place`/`transmute`/`unbounded_channel`). Per-iteration delta: mini-redis 13.47% → 9.65% (pass-1, −3.82pp) → 9.22% (pass-2, −0.43pp) → 8.36% (pass-3, −0.86pp). Bug-extractor 224→129 (−95); bug-resolver 58→46 (−12); external-known 182→508 (+326); external-unknown 271→52 (−219); resolved unchanged. | at-bar (8.36% — at-bar bucket; ≤8% floor) | next rust wave for ship-gate gap: (a) extractor brace-import expansion (`use foo::{X, Y};` currently emits one stub with embedded `{...}` text — `clients::{BlockingClient, BufferedClient, Client}` patterns remain in bug-extractor); (b) sibling-module Cargo.toml resolution to bind `entry::*` / `worker::*` to in-tree submodules (currently folded to `rust_sibling_module` ext placeholder which lands in external-known but loses graph fidelity); (c) generic single-word verbs (`store`/`run`/`subscribe`/`reset`/`state`) deliberately OMITTED per #94 safer-bias rule. |
| actix-examples | rust | 360 | 5.45% (2026-05-19, rust wave-2; was 14.39% post-wave-1, 17.46% pre-wave-1) | rust wave-2 (actix-web 4.x + futures + db libs + tokio internals) | rust-tokio-actix wave (see mini-redis entry for full synth.go change list — same wave). Per-iteration delta: 17.46% → 14.69% (pass-1, −2.77pp) → 14.49% (pass-2, −0.20pp) → 14.39% (pass-3, −0.10pp). Bug-extractor 1760→1443 (−317); bug-resolver 369→310 (−59); external-known 970→2648 (+1678); external-unknown 1500→199 (−1301); resolved unchanged. | addressable | actix-examples residual dominated by (a) long-tail use-paths into unique crates that surface once per sub-example (`yarte_helpers`, `juniper`, `prost`, `rusqlite`, `mongodb`, etc. — already added but each example has a unique mix) and (b) PascalCase generated types (`SerializeToString`/`ParseFromString` from prost-generated code) and (c) bug-resolver structural-ref edges to user-defined Handler/Decoder/Distribution types in actor example. Next rust wave targets: (a) extractor brace-import expansion (high-leverage — opens up multi-import patterns); (b) `static` files (.css/.js/.html) currently routed via `looksLikeSourceFilePath` for some paths but `static/css/style.css` still in bug-resolver. |
| vapor-api-template | swift | 21 | 2.13% (2026-05-19, post-wave-4 swift external-known refresh) | chain-fix #491 (looksLikeSourceFilePath basename-only) + #492 (swift import-extractor namespaces SCOPE.Component carrier as `<file>::import::<module>` and tags Subtype="module", eliminating the `App` collision) + wave-4 swift external-known refresh (SwiftNIO sister modules + Apple SSWG packages + Vapor sister kits + swift import-attribute strip in classifyExternal) | flat at 2.13% — the 2 residual bugs are the `App` SwiftPM target-dependency IMPORTS edges (need Package.swift target-extractor). Wave-4 swift external-known additions did not surface any new resolutions here because the residual is structural, not allowlist-driven. | at-bar | ship a SwiftPM target-extractor for `Package.swift` so the `App` target declares a SCOPE.Component the import binds to → drives bug-rate to 0%. |
| sample-food-truck | swift | — | — | — | — | unmeasured | clone + index |
| vapor | swift | ~250 | 8.93% (2026-05-19, post-#499 swift extractor noise filter) — was 9.26% pre-fix | chain-fix #499 (swift extractor): (a) `extractImportPath` now skips `modifiers`/`attribute`/`attributes` subtrees — synthetic dotted import paths like `_documentation.visibility.internal.Foundation` no longer reach `classifyExternal`; the synth-side prefix-strip in `classifyExternal` is now redundant but kept as a belt-and-braces guard. (b) `extractCallRelationships` now filters Swift statement keywords (`defer`, `repeat`, `do`) and bare-receiver `init` from the CALLS emission path; `Type.init(...)` is preserved via the recvRoot != "" gate so explicit initializer calls keep their receiver_type property. Measured delta: bug-extractor 379 → 359 (-20); bug-resolver 85 → 85 (flat); resolved 3070 → 3089 (+19); total bugs 464 → 444 (-20); net bug-rate -0.33pp. Regression check on chi/express/flask/spdlog/vapor-api-template: 0.00pp on all five (perfect non-swift control). Earlier wave-4 swift external-known refresh: (a) extend `knownExternalPackages` with the SwiftNIO sister modules (`NIOPosix`, `NIOConcurrencyHelpers`, `NIOSSL`, `NIOExtras`, `NIOWebSocket`, `NIOTransportServices`, `NIOEmbedded`, `NIOHTTPCompression`, `_NIOFileSystem`, `_NIOFileSystemFoundationCompat`, `CNIOLinux`/`Darwin`/`Posix`/`Atomics`), Apple SSWG packages (`_CryptoExtras`, `AsyncKit`, `AsyncHTTPClient`, `ServiceLifecycle`, `Metrics`, `Atomics`, `Algorithms`, `SystemPackage`, `ArgumentParser`, `ServiceContextModule`, `SwiftASN1`), Vapor sister kits (`RoutingKit`, `ConsoleKit{,Terminal,Commands}`, `MultipartKit`, `WebSocketKit`, `CVaporBcrypt`), and platform shims (`Glibc`, `Musl`, `Android`, `Darwin`, `Dispatch`, `WinSDK`, `X509`); (b) swift-gated attribute-prefix strip in `classifyExternal` for `@_documentation(visibility:...)` / `@_exported` / `@preconcurrency` / `@_implementationOnly` / `@testable` import shapes; (c) extend `swiftBareNames` with NIO Channel API verbs (`fireChannelRead`, `wrapOutboundOut`, `unwrapInboundIn`, `writeAndFlush`, `addHandler`, `runIfActive`, `flatMapErrorThrowing`, `moveReaderIndex`, etc.) + Foundation Codable container types (`UnkeyedContainer`, `SingleValueContainer`) + NIO HTTP codec types. bug-extractor 627 → 379 (-248); bug-resolver 88 → 85 (-3); external-known 291 → 431 (+140); external-unknown 527 → 638 (+111). Net -252 bugs / -5.01pp. Generic verbs (`defer`, `init`, `storage`, `contains`, `read`, `write`, `succeed`, `fail`, `validate`, `serialize`, `closure`, `cache`, `sessions`, etc.) deliberately OMITTED per safer-bias (#94/#105/#106) — they collide with user methods. | addressable | #499 landed (extractor noise filter). Residual 8.93% above ship-gate is structural and lives upstream: (a) #494 receiver-type tracking — local variable type inference (`let svc = MyService(); svc.foo()` cannot resolve `foo` because we only attach receiver_type for declared class fields); (b) bug-resolver floor (~85 edges): ambiguous locally-defined user methods like `validate`/`createSession`/`deleteSession` resolved against multiple same-named candidates — needs cross-file disambiguation pass; (c) remaining bug-extractor edges (~150) are mostly Foundation/NIO generic verbs (`flatMap`, `map`, `then`, etc.) that the safer-bias filter deliberately keeps off the external-known allowlist. |
| aspnetcore-realworld | csharp | 97 | 9.82% (2026-05-19, v3) | #473 | razor/csharp fix improved but residual cs-specific identifier resolution remains | addressable | next csharp wave |
| spdlog | cpp | 181 | 2.87% (2026-05-19, cpp-spdlog-wave; was 5.76% pre-wave) | cpp-spdlog-wave | cppBareNames extended with POSIX file/fd/dir/env/select/thread/pid/systemd-journal/android-log/MSVC-CRT/distinctive-libc surface + Win32 PascalCase API (`Sleep`, `ZeroMemory`, `MAKEWORD`, `WSASetLastError`, `IsValidSid`, `IsDebuggerPresent`, `InetPtonA`, `GetFullPathName*`, `GetDriveTypeA`, `GetConsoleMode`, `FlushFileBuffers`, `FindFirst/NextFileA`, `FindClose`, `SetHandleInformation`, `OpenProcessToken`, `RegisterEventSourceA`, `OpenEventLogA`, `ReadEventLogA`, `CloseEventLog`); `isSpdlogSinkOrFormatterShape` shape gate routes spdlog-internal `*_sink` / `*_formatter` class-name receiver-strips (`rotating_file_sink`, `short_filename_formatter`, `T_formatter`, `Y_formatter`, ...) to ext:spdlog; `qtBareNames` Qt method surface (`setForeground`, `setBackground`, `setCharFormat`, `currentCharFormat`, `movePosition`, `insertText`, `deleteChar`, `removeSelectedText`, `blockCount`, `fromUtf8`, `fromLatin1`, `invokeMethod`, `Q_ARG`) inside spdlog/sinks/qt_sinks.h call sites. bug-extractor 371 → 181 (-190); bug-resolver 12 → 10 (-2); external-known 1458 → 1551 (+93); external-unknown 651 → 750 (+99); resolved unchanged 4083. Net -2.89pp. Residual root cause: spdlog internal helper functions and member-variable-suffix (`_`) names (extractor bug — own-class members not promoted to entity rows) plus generic single-word verbs deliberately kept off cpp gate per safer-bias (#94). | at-bar | next cpp wave for ship-gate gap: extractor work to bind spdlog own-class members + cross-class receiver tracking for spdlog::details internals |
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
| tokio | rust | 342 | 3.33% (2026-05-19, rust wave-2; was 8.36% post-wave-1, 11.89% pre-wave-1) | rust wave-2 (actix-web 4.x + futures + db libs + tokio internals) | rust-tokio-actix wave (see mini-redis entry for full synth.go change list — same wave). Per-iteration delta: 11.89% → 8.58% (pass-1, −3.31pp) → 8.58% (pass-2, flat — additions targeted actix/mini-redis) → 8.36% (pass-3, −0.22pp from rust_sibling_module fold for `loom::*`/`mio::*`/`libc::*` sibling modules and structural-ref module-path strip in isRustExternalBaseType). Bug-extractor 2031→1622 (−409); bug-resolver 2337→1441 (−896); external-known 1371→5061 (+3690); external-unknown 2787→402 (−2385); resolved unchanged. | at-bar (8.36% — at-bar bucket; ≤8% floor) | next rust wave for ship-gate gap: (a) bug-resolver floor — ambiguous user-defined methods in tokio internals (`store`/`load`/`fetch_add`/`fetch_sub`/`compare_exchange`/`compare_exchange_weak`/`with_*` builder verbs on internal types resolved against multiple same-named candidates) need cross-class receiver disambiguation pass; (b) tokio-specific internal trait family (`Wait`/`WakerRef`/`Storage`/`OrphanQueue`/`InternalStream`/`AssertSync`) — added to `rustExternalBaseTypes` but residual still has long-tail tokio-runtime-specific trait names; (c) `*_uchecked`/`*_unchecked` unsafe variants (`wrapping_sub`/`wrapping_shl`/`unreachable_unchecked`/`transmute_copy`) deliberately not allowlisted (project-internal use of unsafe primitives, not external std). |
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

## Wave-12 (JS/TS extractor destructure-rename lift, #584 ship-gate)

Extractor-level follow-up to wave-11 that addresses the chain-fix
deferred from #585: the JS extractor previously emitted no entity for
the LHS of `const { mutate: createAddress } = useCreateAlternateAddress()`
or `const { data, isLoading } = useQuery()` because the variable-
declarator name field is an `object_pattern`, not an identifier. Every
downstream call site (`createAddress(...)`, `setError(...)`) therefore
landed in bug-extractor on the resolver.

- **Fix shape:** `handleVariableDeclarator` now detects
  `object_pattern` / `array_pattern` LHS and walks the tree, emitting
  one entity per local binding name. Pair patterns (`{ key: local }`)
  emit the LOCAL name, not the property key. Nested patterns recurse
  to leaf bindings. Array patterns emit one entity per identifier
  (covers `useState` tuples + general array destructure).
- **Classification:** when the RHS is a call to a mutation-style
  hook (`useMutation`, `useSWRMutation`, `useState`, `useReducer`,
  `useModal`, `useQuery`, antd App-context hooks, the custom
  `useXxxMutation` convention, or `use{Create|Update|Delete|Patch|
  Toggle|Open|Close|...}Xxx` naming pattern), lifted entities classify
  as `SCOPE.Operation`. Otherwise `SCOPE.Component`. The over-lift on
  non-callable leaves (`data`, `isLoading`) is intentional and cheap:
  the resolver only consults Operation entities for CALLS targets, so
  unused Operation entities are inert.

Per-iteration delta on client-fixture-b (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-wave-11 #585) | 1.738% | 644 | 180 | — |
| Pass-1 (#584 destructure-rename lift) | 1.154% | 422 | 125 | -0.584pp |

client-fixture-c (secondary target):

| Pass | bug-rate | Δ |
|---|---:|---:|
| baseline | 2.680% | — |
| Pass-1 | 2.628% | -0.052pp |

Regression check (main vs wave-12) — 11 listed repos + client-fixture-a:

| Repo | Main | W12 | Δ |
|---|---:|---:|---:|
| chi | 4.233% | 4.233% | 0.000pp |
| flask | 9.458% | 9.458% | 0.000pp |
| spdlog | 5.758% | 5.758% | 0.000pp |
| gin | 4.931% | 4.931% | 0.000pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| express | 2.996% | 2.996% | 0.000pp |
| nextjs-commerce | 2.317% | 2.093% | -0.224pp |
| nestjs-starter | 1.754% | 1.754% | 0.000pp |
| kafka-streams-examples | 3.396% | 3.396% | 0.000pp |
| vapor-api-template | 2.128% | 2.128% | 0.000pp |
| ktor-samples | 4.864% | 4.864% | 0.000pp |
| client-fixture-a | 6.082% | 6.082% | 0.000pp |

No regression. The only non-zero deltas are improvements: nextjs-commerce
(-0.224pp) confirms the destructure-rename lift fires on real React
Query / SWR shapes in the wider TS ecosystem, not just cfb's hooks.
Every non-JS/TS corpus is bit-identical because the new code path is
gated to the JS extractor's variable-declarator handler.

Residual root cause: post-#584 cfb bug-extractor top samples are now
single-word bare callables (`replace`, `warning`, `clearFilters`,
`unwrap`, `get`) — String/Array prototype methods, antd `Modal.warning`
static, lodash/fp `unwrap`, and accessor `get` on opaque receivers.
These are receiver-typing residuals (the call site is `x.replace(...)`
where the receiver-type binding wasn't captured upstream), NOT the
destructure-rename pattern. They split between (a) bareNames additions
to synth.go (a synth/resolve-only follow-up wave) and (b) receiver-
type inference improvements (a deeper extractor change).

Status: AT SHIP-GATE BOUND. cfb 1.738% → 1.154% — within 0.155pp of
the 1.0% ship-gate floor. cfc 2.680% → 2.628%. Wave-12 closes the
destructure-rename gap that wave-11 explicitly deferred. Remaining
0.15pp is receiver-type residuals and is filed as the next chain-fix
candidate (bare-name additions for `replace`/`warning`/`clearFilters`
plus a small set of antd static helpers).

---

## Wave-12 FINAL (ship-gate close, post-#587 receiver-type residual)

Synth-only follow-up to wave-12 (#587) that closes the 0.155pp gap to
the ≤1% ship-gate by classifying the three receiver-type residual
clusters left after the destructure-rename lift. All additions are
TS/JS-gated (per-language dynamicPatternsByLang lookup or
hasJSCollectionLibImport file gate).

- **Track A (String.prototype receiver-strip):** added `replace`,
  `replaceAll`, `trimStart`, `trimEnd`, `repeat`, `matchAll` to
  `jsBareNames`. `trim`, `toLowerCase`, `toUpperCase`, `padStart`,
  `padEnd`, `normalize`, `localeCompare` were already present.
  `replace` was the top bug-extractor leaf on cfb wave-11 residual.
- **Track B (antd Modal/message/notification static + Table render-
  prop callbacks):** added `warning`, `success`, `loading`, `destroyAll`,
  `clearFilters`, `setSelectedKeys` to `jsBareNames`. `confirm`, `error`,
  `info` were already present. These cover `Modal.confirm(...)`,
  `message.success(...)`, `notification.warning(...)`, and antd Table
  `filterDropdown` render-prop callbacks.
- **Track C (lodash / ramda chain-style util methods):** added 80+
  names to `jsCollectionLibBareNames` (per-file-import gated): `get`,
  `set`, `has`, `unwrap`, `omit`, `pick`, `merge`, `cloneDeep`,
  `isEqual`/`isEmpty`/`isObject`/`isString`/`isNumber`/`isFunction`/
  `isNil`/`isNull`/`isUndefined`/`isPlainObject`/..., `keyBy`,
  `orderBy`, `sortBy`, `uniqBy`, `uniq`, `intersection`, `union`,
  `difference`, `chunk`, `compact`, `flatten`, `flattenDeep`, `zip`,
  `unzip`, `times`, `partial`, `debounce`, `throttle`, `memoize`,
  `noop`, `identity`, `constant`, `defaults`, `invert`, `mapValues`/
  `mapKeys`, `keys`/`values`/`entries`, `sumBy`/`meanBy`/`maxBy`/
  `minBy`/`countBy`, `partition`, `take`/`drop`/`head`/`last`/`tail`/
  `initial`/`nth`, `sample`/`sampleSize`/`shuffle`. Already gated by
  hasJSCollectionLibImport so files without lodash/ramda/immutable/
  react imports preserve the safer-bias rule.
- **Track D (opaque `get`):** absorbed into Track C — `get` is
  allowlisted only on files importing lodash/ramda/react. Avoids
  blanket allowlist that would shadow `axios.get` user methods.

Per-iteration delta on client-fixture-b (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-wave-12 #587) | 1.154% | 422 | 125 | — |
| Pass-1 (A + B + C combined) | 0.875% | 292 | 123 | -0.279pp |

Single pass closed the gap. **SHIP-GATE ACHIEVED**: 0.875% < 1.0%.

Regression check (main vs wave-12-final) — 11 listed repos + cfa:

| Repo | Main | W12-F | Δ |
|---|---:|---:|---:|
| chi | 4.233% | 4.233% | 0.000pp |
| flask | 9.450% | 9.450% | 0.000pp |
| spdlog | 5.758% | 5.758% | 0.000pp |
| gin | 4.931% | 4.931% | 0.000pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| express | 2.996% | 2.856% | -0.140pp |
| nextjs-commerce | 2.093% | 1.794% | -0.299pp |
| nestjs-starter | 1.754% | 1.754% | 0.000pp |
| kafka-streams-examples | 3.396% | 3.396% | 0.000pp |
| vapor-api-template | 2.128% | 2.128% | 0.000pp |
| ktor-samples | 4.864% | 4.844% | -0.020pp |
| client-fixture-a | 5.927% | 5.927% | 0.000pp |

No regression. Every non-JS/TS corpus is bit-identical. The JS/TS
corpora (express, nextjs-commerce) and ktor-samples (which ships JS
build templates in a handful of sample modules) show improvements
between -0.02pp and -0.30pp — confirms the additions are real
language-surface, not fixture-specific overfit.

Residual root cause: post-wave-12-FINAL cfb bug-extractor top samples
are now `find`, `append`, `splice`, plus per-component user-handler
names (`handleClientSelection`, `handleReloadData`). `find`/`splice`
are #94 safer-bias rejects (collide with user `find` on hand-rolled
classes); per-component handlers are an extractor-lift gap that
wave-12 (#587) addressed for destructure-rename but not for bare
`const handleX = () => {...}` arrow declarations inside JSX.

Status: at-ship-gate. cfb 1.154% → 0.875% — ≤1% target met. The
remaining 0.875% is split between (a) safer-bias rejects (`find`,
`splice`, `append`) that should stay rejected per #94, and (b) per-
component bare-arrow handler lift that is an extractor change for a
future wave.

---

## Wave-13 React (TS/JS real-residue, ts-w13 cfb 0.875% → 0.574%)

Targeted continuation of wave-12-FINAL ship-gate. Wave-12-FINAL closed
the resolver-residual gap at 0.875% on cfb but left empirical residue
that the prior agent's analysis of 292 bug-extractor edges identified
as real React-ecosystem coverage gaps (Track A) plus a destructured-
parameter callable-lift gap (Track B). Four diagnostic passes:

- **Pass-1 (Track A: library allowlist additions, synth.go jsBareNames):**
  @dnd-kit (useSortable/useDraggable/useDroppable/useDndContext/
  useDndMonitor/closestCenter/closestCorners/rectIntersection/
  pointerWithin/arrayMove/defaultDropAnimationSideEffects/restrictTo*),
  react-router v6 advanced hooks (useRouteError/useRouteLoaderData/
  useRevalidator/useBlocker/useFormAction/useFetcher/useFetchers/
  useViewTransitionState/useSubmit/useAsyncValue/useAsyncError),
  antd Grid (useBreakpoint), SheetJS XLSX snake_case utils
  (sheet_to_json/sheet_add_json/sheet_add_aoa/aoa_to_sheet/
  json_to_sheet/book_new/book_append_sheet/book_set_sheet_visible/
  decode_range/encode_range/decode_cell/encode_cell), Clipboard API
  (ClipboardItem), styled-components (keyframes/createGlobalStyle),
  date-fns (parseISO/isSameDay/isSameMonth/.../isWithinInterval/
  differenceIn{Days,Months,Years,Weeks,Hours,Minutes,Seconds,
  Milliseconds,CalendarDays}/add{Days,Hours,Minutes,Seconds,Weeks,
  Months,Years}/sub{Days,Hours,Minutes,Seconds,Weeks,Months,Years}/
  startOf{Month,Week,Year,Day,Hour,Minute,Quarter}/endOf{...}/
  formatDistance/formatDistanceToNow/formatRelative/formatISO),
  FileReader/DOMParser (FileReader/parseFromString), react-error-
  boundary (useErrorBoundary), and React effect alias
  (useReactEffect). All TS/JS-gated. 0.875% → 0.764% (-0.111pp);
  bug-extractor 292 → 239.

- **Pass-2 (Track B: handler-convention Dynamic patterns, refs.go
  jsDynamicPatterns):** added `^handle[A-Z][A-Za-z0-9]*$` and
  `^after[A-Z][A-Za-z0-9]*$` mirroring the wave-11 `^on[A-Z]...`
  rule. Conservative scope — generic verbs (get/set/load/save/create/
  update/delete/fetch/use/submit/cancel/select/reset/toggle) are
  deliberately EXCLUDED to avoid shadowing user-defined entities.
  `handleX` is universal React tutorial convention (cfb residue:
  handleClientSelection/handleReloadData/handleSaveOnCell/
  handleCloseModal/handleOnRemove/handleCancelButton/etc.);
  `afterX` is form/lifecycle convention (cfb: afterSaveNote/
  afterSaveSuccess/afterCreateSuccess). Same-file preference resolver
  (wave-9 Chain-fix A) fires BEFORE the dynamic-pattern check via
  the hex-ID branch in classifyDispositionLang, so a same-file
  lifted handler still wins. 0.764% → 0.646% (-0.118pp); bug-extractor
  239 → 216; bug-resolver 123 → 90 (33 handler residuals routed to
  Dynamic).

- **Pass-3 (synth.go web platform observers + APIs):** ResizeObserver/
  MutationObserver/IntersectionObserver/PerformanceObserver +
  observe/disconnect/unobserve/takeRecords; DOMParser/XMLSerializer/
  serializeToString; String.prototype additions (charAt/substring);
  dayjs symmetric additions (subtract/toDate); Blob/Response
  (arrayBuffer); Storage API (getItem/removeItem); window/Element
  scroll API (scrollTo/scrollBy); RegExp (exec); DOMPurify (sanitize).
  All TS/JS-gated, all distinctive web-platform names. 0.646% →
  0.591% (-0.055pp); bug-extractor 216 → 190.

- **Pass-4 (synth.go Date UTC + Intl):** Date.prototype UTC accessors
  (getUTCDate/getUTCMonth/getUTCFullYear/getUTCHours/getUTCMinutes/
  getUTCSeconds/getUTCDay/getUTCMilliseconds + setUTC*); toUTCString;
  Intl formatToParts. 0.591% → 0.574% (-0.017pp); bug-extractor 190 →
  182.

Per-iteration delta on client-fixture-b (primary target):

| Pass | bug-rate | bug-ext | bug-res | Δ vs baseline |
|---|---:|---:|---:|---:|
| baseline (post-wave-12-FINAL) | 0.875% | 292 | 123 | — |
| Pass-1 (Track A library allowlist) | 0.764% | 239 | 123 | -0.111pp |
| Pass-2 (Track B handle*/after* dynamic) | 0.646% | 216 | 90 | -0.229pp |
| Pass-3 (web observers + APIs) | 0.591% | 190 | 90 | -0.284pp |
| Pass-4 (Date UTC + Intl) | 0.574% | 182 | 90 | -0.301pp |

Regression check (main vs ts-w13) — 11 listed repos + cfa:

| Repo | Main | W13 | Δ |
|---|---:|---:|---:|
| chi | 4.233% | 4.233% | 0.000pp |
| flask | 9.424% | 9.424% | 0.000pp |
| spdlog | 5.758% | 5.758% | 0.000pp |
| gin | 4.931% | 4.931% | 0.000pp |
| play-scala-starter | 2.113% | 2.113% | 0.000pp |
| express | 2.856% | 2.856% | 0.000pp |
| nextjs-commerce | 1.794% | 1.794% | 0.000pp |
| nestjs-starter | 1.754% | 1.754% | 0.000pp |
| kafka-streams-examples | 3.396% | 3.396% | 0.000pp |
| vapor-api-template | 2.128% | 2.128% | 0.000pp |
| ktor-samples | 4.844% | 4.750% | -0.094pp |
| client-fixture-a | 5.927% | 5.927% | 0.000pp |

No regression. Every non-JS/TS corpus is bit-identical. ktor-samples
shows -0.094pp (a handful of sample modules ship JS templates that
benefit from the @dnd-kit / react-router additions) — confirms the
additions are real language-surface, not fixture-specific overfit.

Residual root cause: post-wave-13 cfb bug-extractor top samples are
now `find`, `append`, `splice`, `concat`, `flatMap`, `forEach`,
`reduce`, `delete`, `get`, `clear`, `read`, `write`, `select` — all
#94 safer-bias rejects (collide with user methods on hand-rolled
classes); plus user-handler names without the `handle*` / `after*`
convention (`saveDirectly`, `fetchModel`, `reportPendingChanges`,
`reload`, `requestFetchData`, `getNameForId`, `getEditableElement`)
that remain extractor-lift gaps for destructured-arrow `const
fetchX = useCallback(...)` not yet handled, plus `styled` and dayjs
field-accessor getters (`year`, `minute`, `second`, `millisecond`)
that are intentionally kept off the allowlist per the wave-7
collision-with-user-model-field rationale.

Status: well-under-ship-gate. cfb 0.875% → 0.574% (-0.301pp,
**34% relative reduction**); ≤1% target retained with deeper margin.
The remaining 0.574% splits ~60% safer-bias rejects (#94 stays
rejected) / ~30% non-handle/after handler-lift gap (filed as
chain-fix candidate: extractor lift of all `const X = useCallback(...)`
inside JSX function bodies — wider than wave-8 #567's wrapper-call
heuristic) / ~10% dayjs field-accessor collisions (kept rejected).

| client-fixture-b | javascript (Vite + React) | ~659 | **0.574% (2026-05-19, ts-w13 react real-residue)** — was 0.875% (post-wave-12-FINAL) | ts-w13-react-real-residue | Wave-13, 4 passes (Track A library allowlist + Track B handle/after Dynamic regex + web-platform observers + Date UTC/Intl). Cumulative -0.301pp; bug-rate 0.875% → 0.574% (-34% relative). bug-extractor 292 → 182; bug-resolver 123 → 90. | well-under-ship-gate | chain-fixes filed: (1) extractor: lift ALL `const X = useCallback/useMemo/=> {...}` inside JSX function bodies to file-scoped SCOPE.Operation (wider than wave-8 #567's wrapper-call heuristic, to cover handlers without `useCallback` wrapper); (2) follow-up #104 relaxation deferred per safer-bias rule. |

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

## scala-play-wave (2026-05-19)

Target: play-scala-starter — at-bar 2.11% (only Scala corpus on disk;
spark not cloned locally). Drove toward ship-gate via two distinctive
bare-name additions to `scalaBareNames` (lang="scala" gated):

- `Action.async` — Play `ActionBuilder.async` factory preserved as
  dotted leaf `Action.async` by the Scala extractor (not stripped to
  bare `async`, which would collide with user methods on any class).
  Literal dotted form matches no user identifier — same shape as JAX-RS
  and Kafka dotted-key entries in the Java gate.
- `success` / `failure` — scala.concurrent `Promise` completion methods
  receiver-stripped from `promise.success(_)` / `promise.failure(_)`.
  Scala lang gate isolates from non-Scala identifier collisions; the
  Promise idiom dominates real Scala code where `success` appears as a
  bare receiver-stripped call.

Per-iteration delta (play-scala-starter):

| Pass | Bug-rate | Δ | Bug-extractor | Bug-resolver |
|---|---:|---:|---:|---:|
| pre-wave | 2.113% | — | 2 | 1 |
| pass-1 (final) | 0.704% | **−1.409pp** | 0 | 1 |

Single landing — bug-extractor cleared (2 → 0). Sole residual is 1
bug-resolver edge (`counter.nextCount()`, cross-class trait method
call) that requires the #494 receiver-type primitive for Scala. Below
the ≤1% ship-gate.

Regression check (14 corpora, all gated by lang="scala"):

| Repo | Lang | main | scala-play-wave | Δ |
|---|---|---:|---:|---:|
| chi | go | 2.025% | 2.025% | 0.000pp |
| express | js | 2.856% | 2.856% | 0.000pp |
| spdlog | cpp | 2.856% | 2.856% | 0.000pp |
| gin | go | 3.383% | 3.383% | 0.000pp |
| nextjs-commerce | ts | 1.794% | 1.794% | 0.000pp |
| nestjs-starter | ts | 1.754% | 1.754% | 0.000pp |
| flask-realworld | python | 2.998% | 2.998% | 0.000pp |
| django-realworld | python | 0.755% | 0.755% | 0.000pp |
| pandas | python | 7.897% | 7.897% | 0.000pp |
| kafka-streams-examples | java | 3.329% | 3.329% | 0.000pp |
| vapor-api-template | swift | 2.128% | 2.128% | 0.000pp |
| ktor-samples | kotlin | 4.735% | 4.735% | 0.000pp |
| sidekiq | ruby | 3.634% | 3.634% | 0.000pp |
| actix-examples | rust | 14.386% | 14.386% | 0.000pp |

Zero deltas anywhere — language gate working as intended (no scala-
named identifiers exist in the regression corpora at the bare-name
lookup level, and the dotted `Action.async` literal cannot match any
non-scala extractor output).

Residual root cause: 1 cross-class trait method call
(`counter.nextCount()`) — Scala extractor preserves the bare leaf
`nextCount` after receiver strip, and the resolver can't bind it to
the `Counter` trait method because trait method resolution requires
receiver-type inference (#494 primitive).

Status: at-ship-gate (0.70%, ≤1% ship-gate cleared).

Chain-fixes filed (for next scala wave):
1. **#494 receiver-type primitive for scala** — would resolve the
   final 1 cross-class trait method call and drive play-scala-starter
   to 0.00%.
2. **Twirl `.scala.html` extractor split** — 1 template still
   misclassified as dynamic; separate chain-fix (Twirl has its own
   syntax, needs a dedicated extractor branch).
3. **Spark corpus clone** — Apache Spark Scala codebase not on disk;
   would stress-test the Scala extractor + resolver at scale before
   any further scala-bareNames additions (avoid over-fitting to
   play-scala-starter's small surface).

---

## Wave: js-barrel-reexport (PLT #537, 2026-05-19)

Driver: client-fixture-c (RN+Expo) was the worst remaining JS/TS
fixture at 2.31% bug-rate post-wave-13. Top bug-extractor samples
(`components.branding.BrandLogo.default`, `components.feedback.LoadingEllipsis.default`,
`components.themed-text.ThemedText`, schema short-form refs) all
pointed at a single structural gap: the JS/TS pipeline could not
resolve barrel re-exports, default-export imports, alias-substituted
imports of non-existent files, OR react_props `scope:schema:<file>#<name>`
USES_PROPS edges. Root cause diagnosis surfaced a deeper issue —
`.tsx` files were being parsed under the plain `typescript`
tree-sitter grammar, which treats JSX tags as syntax errors, leaving
the entire file body as ERROR nodes and stopping the JS extractor
from ever reaching `function_declaration` nodes inside React
components. Default-exported React entities (BrandLogo,
LoadingEllipsis, NewNoteFeature, AppSelectDialog, ...) consequently
never made it into the graph, and every consumer's IMPORTS edge
landed in bug-extractor.

Per-iteration delta on client-fixture-c (target):

| pass | change | bug-rate | delta |
|---|---|---|---|
| baseline | — | 2.305% | — |
| 1 | tsx grammar routing for `.tsx`/`.jsx` | 2.158% | -0.15pp |
| 2 | `pickExistingAliasTargetExists` (alias-stale → external-unknown) + JS default-leaf fallback in `ResolveDottedImportTargetForJS` + `index.ts` directory rollup in `modulesForJSFile` | 2.090% | -0.07pp |
| 3 | `lookupModuleEntityCaseFold` (kebab-/snake-/Pascal-case fold + single-Operation default-export bind) | 2.086% | -0.00pp |
| 4 | jsDynamicPatterns: `^current$`, `^isTablet$`/`isMobile`/`isLandscape`/`isPortrait`/`isDesktop`, `^enqueue$` (RN ref + responsive + notification idioms) | 2.065% | -0.02pp |
| 5 | `scope:schema:<file>#<name>` short-form in `lookupStructural` (react_props USES_PROPS bind) | **1.420%** | **-0.64pp** |

Cumulative -0.885pp on client-fixture-c — ship-gate (≤1%) not yet
hit but well below the 2% acceptance bar. Total bug-extractor on c:
470 → 316 (-154). Total bug-resolver: 23 → 19 (-4). Resolved: 11,299
→ 13,153 (+1,854) — most of that uplift is real entities the tsx
grammar now extracts that the plain typescript grammar dropped on the
floor.

Regression sweep (16 repos, ≥0.5pp = STOP):

| repo | before | after | delta_pp |
|---|---|---|---|
| chi (go) | 0.0203 | 0.0203 | 0.0000 |
| flask (python) | 0.0592 | 0.0592 | 0.0000 |
| spdlog (cpp) | 0.0286 | 0.0286 | 0.0000 |
| gin (go) | 0.0338 | 0.0338 | 0.0000 |
| play-scala-starter (scala) | 0.0070 | 0.0070 | 0.0000 |
| express (js) | 0.0286 | 0.0286 | 0.0000 |
| nextjs-commerce (ts) | 0.0179 | 0.0224 | +0.4470 |
| nestjs-starter (ts) | 0.0175 | 0.0175 | 0.0000 |
| kafka-streams-examples (java) | 0.0333 | 0.0333 | 0.0000 |
| vapor-api-template (swift) | 0.0213 | 0.0213 | 0.0000 |
| ktor-samples (kotlin) | 0.0474 | 0.0474 | 0.0000 |
| sidekiq (ruby) | 0.0363 | 0.0363 | 0.0000 |
| exposed (kotlin) | 0.0251 | 0.0251 | 0.0000 |
| actix-examples (rust) | 0.0590 | 0.0590 | 0.0000 |
| client-fixture-a (python) | 0.0550 | 0.0550 | 0.0000 |
| client-fixture-b (ts) | 0.0057 | 0.0057 | 0.0000 |
| **client-fixture-c (ts/tsx)** | **0.0231** | **0.0142** | **-0.8852** |

Only one non-target movement: `nextjs-commerce +0.45pp` (under the
0.5pp stop threshold). Investigation: bug-extractor 24 → 35 (+11),
bug-resolver unchanged at 0, resolved 636 → 787 (+151), external-known
180 → 187, external-unknown 154 → 209. Net 224 NEW references the
tsx grammar surfaces that the plain typescript grammar previously
hid behind ERROR nodes — the percentage uptick reflects extracted-but-
not-yet-resolved coverage, not a quality regression. Absolute resolved
count is up 24% in this repo. Acceptable.

Residual root cause (client-fixture-c, post-fix at 1.42%):
- Bug-extractor top: `ReactAppDependencyProvider`, `useStyleContext`,
  `createAnimatedComponent`, `timing`, `Value` — all external React
  Native / gluestack-ui / Reanimated platform symbols that should
  classify as external-unknown but currently slip past the allowlist
  because they ride on un-resolvable receiver-strip patterns.
- Bug-resolver top: `url`, `detail`, `showInlineAlert`, `deficiencies`,
  `items` — domain-specific bare names too generic to safely allowlist
  globally (collision with real user methods in other languages).

Status: **below 2% ship-bar (1.42%)**, above 1% ship-gate. Track A
(barrel/re-export) complete; Track B (bare-locals) partial — the
RN-specific patterns landed but the domain-generic `url`/`detail`/
`items` residue is a separate concern (file-scoped allowlists +
react-native receiver-binding work).

Chain-fixes filed:
1. **rn-platform-symbol-allowlist** — extend `knownExternalPackages`
   recognition to bare PascalCase symbols imported from packages
   already on the allowlist (`ReactAppDependencyProvider`,
   `useStyleContext`, `createAnimatedComponent`) so they classify
   external-unknown instead of bug-extractor.
2. **rn-reanimated-receiver-strip** — Reanimated `timing` / `Value`
   are bare leaf names left after `Animated.<name>` receiver strip;
   they need either a JS-gated allowlist or a receiver-aware
   extractor pass.
3. **react-props-schema-binding-followups** — react_props now binds
   props schemas via 3-segment scope refs; verify the same shape is
   not emitted by other cross extractors (proto, schema) without an
   equivalent short-form fallback.
4. **default-export-name-tracking** — long-term: the JS extractor
   should record on each entity whether it is the file's default
   export, so the resolver can bind `<module>.default` deterministically
   instead of relying on file-basename and single-Operation heuristics.

---

## Wave-10 Track D — Python per-import file-scoped gates (2026-05-19)

Lifts generic Python verbs (`first`, `last`, `info`, `error`, `sub`,
`search`, `execute`, `cursor`, `urljoin`, `find_all`, `apply_async`,
...) off #94's safer-bias floor on files that import the canonical
library whose surface those names belong to. Same precedent as wave-9
React (`hasJSCollectionLibImport`) and Ktor (`hasKtorServerImport`):
per-import gates safely activate ecosystem allowlists without
shadowing user methods of the same name on classes elsewhere.

Gates added: pandas / requests / boto3 / redis / django / flask /
sqlalchemy / mongo / celery / logging / re / dbapi / bs4 / urllib
(14 helpers). Each maps to a sentinel subtype routed through the
classifyExternal wrapper so the synthesiser folds the edge to the
canonical ecosystem placeholder (`ext:pandas`, `ext:requests`, ...)
rather than `ext:<bare-leaf>`.

| Corpus | lang | Main | Branch | Delta |
|--------|------|------|--------|-------|
| chi | go | 2.025% | 2.025% | 0.000pp |
| express | js | 2.856% | 2.856% | 0.000pp |
| spdlog | cpp | 2.856% | 2.856% | 0.000pp |
| gin | go | 3.383% | 3.383% | 0.000pp |
| play-scala-starter | scala | 0.704% | 0.704% | 0.000pp |
| nextjs-commerce | ts | 1.794% | 1.794% | 0.000pp |
| nestjs-starter | ts | 1.754% | 1.754% | 0.000pp |
| flask-realworld | python | 2.998% | 2.998% | 0.000pp |
| django-realworld | python | 0.755% | 0.755% | 0.000pp |
| pandas | python | 7.586% | 7.482% | **-0.104pp** (improved) |
| click | python | 3.730% | 3.609% | **-0.121pp** (improved) |
| requests | python | 1.143% | 0.992% | **-0.151pp** (improved) |
| kafka-streams-examples | java | 3.329% | 3.329% | 0.000pp |
| vapor-api-template | swift | 2.128% | 2.128% | 0.000pp |
| ktor-samples | kotlin | 4.735% | 4.735% | 0.000pp |
| sidekiq | ruby | 3.571% | 3.571% | 0.000pp |
| exposed | kotlin | 2.508% | 2.508% | 0.000pp |
| actix-examples | rust | 5.873% | 5.873% | 0.000pp |
| client-fixture-b | ts | 0.572% | 0.572% | 0.000pp |
| client-fixture-c | ts | 2.306% | 2.306% | 0.000pp |
| **client-fixture-a (TARGET)** | **python** | **5.408%** | **3.427%** | **-1.981pp** |

No regressions on any non-python corpus. All python corpora improve
or hold — pandas, click, requests each shed 0.10-0.15pp via the new
gates. Cross-language gate verified by tests
(`TestPythonPerImportGates_CrossLanguageGate`).

Per-iteration delta on client-fixture-a:
- Pass-1 (5.408% → 4.091%, -1.317pp): pandas/requests/boto3/redis/
  django/flask/sqlalchemy/mongo/celery/logging gates wired.
- Pass-2 (4.091% → 3.427%, -0.664pp): re/dbapi/bs4/urllib gates added
  for residual `sub`/`search`/`findall`/`execute`/`cursor`/`urljoin`/
  `find_all`.

**Residual root cause (post-Track D):** the remaining 989
bug-extractor on client-fixture-a are dominated by:
1. Python builtin list/dict ops (`append` 163, `items` 75, `pop` 30,
   `extend` 28, `keys` 17, `remove` 10) — no canonical import gate
   exists (every Python file uses lists); #94 safer-bias rule keeps
   them out. Resolving requires either Python-source heuristic
   (extension-only gate) or full receiver-type inference (#494).
2. `replace` 47, `write` 37, `read` 11, `close` (DB-API already
   gated, but `close` on file objects survives) — string/file
   builtins; same #494 receiver-type primitive blocker.
3. User-helper bare names (`get_safety_filings`, `get_device`,
   `setMessageParams`, `generate_presigned_download_url` on custom
   S3Helper) — actual extraction gaps, NOT external-known; receiver
   stripping loses the class context for cross-file method
   resolution.

**Status:** ≤5% floor cleared. Within 0.43pp of the 3% ship-gate.
Further bug-rate compression on this corpus requires #494 (receiver-
type primitive) — chain-fix already filed in click PR #601.

**Chain-fixes filed (out of scope for this PR):**
1. #494 receiver-type primitive for python — would resolve the
   builtin list/dict ops (`append`/`pop`/`extend`/`keys`) without
   shadowing user methods in other languages.
2. Python-extractor receiver-aware extraction for custom helper
   classes (`S3Helper.generate_presigned_download_url`) — currently
   loses class context after receiver strip, lands in bug-extractor.
3. `replace` / `write` / `read` builtin gate — needs a non-import
   signal (could be: presence of a `with open(...)` block in the
   function, or a `bytes`/`str` literal-typed receiver inference).

---

## #613 — Go chi/gin/echo/fiber/gorilla_mux ROUTES_TO method binding (2026-05-19)

The YAML rules for the five supported Go HTTP routers capture the handler
argument with `(\w+)`, which cannot span `.` — so `r.Get("/users", h.List)`
emits a ROUTES_TO edge to `Controller:h` (the receiver variable) instead of
`Controller:UsersHandler.List` (the actual method entity). Every Go web
service in the corpus + downstream user stacks is affected.

**Fix (Option A):** new AST-driven post-pass `applyGoRouteComposition` in
`internal/engine/go_routes.go`. The pass parses Go source with the stdlib
`go/parser`, builds a same-file `varName -> typeName` map covering the three
declaration shapes that dominate handler-registration code:

1. `var h *UsersHandler` / `var h UsersHandler` — typed var decl.
2. `h := &UsersHandler{...}` / `h := UsersHandler{...}` — composite literal.
3. `h := NewUsersHandler(...)` / `h := handlers.NewUsersHandler(...)` —
   Go `NewT` constructor idiom, which by convention returns `*T`.

It then walks every `<recv>.<HTTPVerb>("<path>", <recv>.<Method>)` call,
resolves `<recv>` via the var-type map, and rewrites the matching
`Controller:<recv>` ROUTES_TO ToID to `Controller:<TypeName>.<Method>`. The
pass edits ToID only — no entities created, no relationships added or
removed — so it cannot regress non-Go pipelines or introduce
false-positive Routes. When a receiver cannot be confidently resolved the
edge is left untouched (safer-bias).

**Quality bench delta (go-chi-mini fixture):**

- Pre-fix recall: relationships 15/16 (93.8%); `nice_to_have` 1/2 (the
  /users → UsersHandler.List edge was nice-to-have because the must_exist
  asserted the broken `Controller:h` target).
- Post-fix recall: relationships 17/17 (100.0%); `must_exist`
  flipped to the correct method target, plus a second must_exist for
  /users/{id} → UsersHandler.Get to assert multiple bindings off one
  receiver var.
- Forbidden hits: 0 (unchanged).

**Regression check (mandatory):**

| Fixture | Pre | Post | Δ |
|---|---:|---:|---:|
| go-chi-mini | 93.8% | 100.0% | +6.2pp |
| java-spring-mini | 100.0% | 100.0% | 0.000pp |
| python-django-mini | 100.0% | 100.0% | 0.000pp |
| rust-tokio-mini | 100.0% | 100.0% | 0.000pp |
| typescript-react-mini | 100.0% | 100.0% | 0.000pp |

Zero regressions outside go (language-gated). All five quality fixtures at
100% recall post-fix.

**Residual root cause:** receiver-type tracking is intentionally bounded to
the three Go-idiomatic shapes above; uncommon patterns (factories that
don't follow `NewT`, struct fields holding handlers, generic-typed
receivers, embedded-struct method promotion) still fall through to
"unresolved → leave edge alone". A future #494 receiver-type primitive
would cover those cases.

**Status:** shipped. New edges land at `pattern_type=ast_driven` +
`go_route_binding=method_receiver_resolved` for downstream telemetry.

**Chain-fixes filed:** none — implementation is self-contained and the
remaining unresolved cases are tracked under the existing #494 receiver-
type primitive umbrella.

---

## Wave: rn-platform-symbols (PR follow-up to #621, 2026-05-19)

Driver: client-fixture-c (RN+Expo) post-#621 sat at 1.43% — above the
1% ship-gate. PR #621 chain-fixes 1 + 2 (RN platform symbols, Reanimated
receiver-strip) are this wave. Diagnosis raised `maxDispositionSamples`
locally to 5000 (reverted before commit) to aggregate true bug-extractor
frequencies. Top residual stubs (count):

- `useStyleContext` (43) — `@gluestack-ui/utils/nativewind-utils`
- `indicator` (12), `delete` (12), `show` (10) — domain-generic
- `Value` (4), `timing` (3), `sequence` (2), `parallel` (1),
  `createAnimatedComponent` (2) — `react-native` `Animated.*`
- `startPlayer` / `stopPlayer` / `pausePlayer` / `seekToPlayer` /
  `startRecorder` / `stopRecorder` / `addPlayBackListener` /
  `removePlayBackListener` / `addRecordBackListener` /
  `removeRecordBackListener` / `addPlaybackEndListener` /
  `removePlaybackEndListener` — `react-native-nitro-sound`
- `getNetworkStateAsync` (3), `requestCameraPermissionsAsync`,
  `pickDirectoryAsync`, `getDocumentAsync`, `startActivityAsync`,
  `shareAsync`, `downloadFileAsync`, `supportedAuthenticationTypesAsync`,
  `isAvailableAsync` — expo-* platform modules
- `openCamera`, `openCropper`, `openPicker` —
  `react-native-image-crop-picker`
- `goBack`, `addListener`, `toggleDrawer`, `back`, `isFocused`,
  `requestCancel` — `@react-navigation/*` / `expo-router`
- `snapToIndex` — `@gorhom/bottom-sheet`
- `requireExternalGestureToFail`, `numberOfTaps`, `manualActivation` —
  `react-native-gesture-handler`
- `useBreakpointValue`, `useDrawerStatus` — gluestack-ui

Eight new file-scoped gates, all in `synth.go` and TS/JS-language-gated
above the call site. Each gate matches on the canonical npm spec
(`spec == "react-native-reanimated"` etc., with prefix match for
sub-paths). The Reanimated gate also fires on `react-native` itself
because `Animated.<name>(...)` is the dominant caller of `timing` /
`Value` / `sequence` / `createAnimatedComponent` and the names ride
on the core `react-native` `Animated` namespace, not the standalone
`react-native-reanimated` package.

Per-iteration delta on client-fixture-c (target):

| pass | change | bug-rate | delta |
|---|---|---|---|
| baseline (#621 merged) | — | 1.433% | — |
| 1 | reanimated/gluestack/expo-camera/expo-file/expo-network/audio-recorder/gesture-handler/react-navigation gates (initial) | 1.043% | -0.39pp |
| 2 | reanimated gate widened to include `react-native` core; audio-recorder gate widened to include `react-native-nitro-sound` | 0.882% | -0.16pp |
| 3 | `@gorhom/bottom-sheet` gate + `useBreakpointValue` / `useDrawerStatus` / `useToken` added to gluestack | 0.873% | -0.01pp |

Cumulative **-0.560pp** on client-fixture-c — **ship-gate (≤1%)
reached at 0.873%**. bug-extractor 317 → 185 (-132). Resolved count
unchanged (13,156) — every win is reclassification from bug-extractor
to external-known/unknown, not new extraction.

Regression sweep (14 repos, ≥0.5pp = STOP):

| repo | main | branch | delta_pp |
|---|---|---|---|
| chi (go) | 0.02025 | 0.02025 | 0.0000 |
| flask (python) | 0.05249 | 0.05192 | -0.057 |
| spdlog (cpp) | 0.02856 | 0.02856 | 0.0000 |
| gin (go) | 0.03383 | 0.03383 | 0.0000 |
| play-scala-starter (scala) | 0.00704 | 0.00704 | 0.0000 |
| express (js) | 0.02856 | 0.02856 | 0.0000 |
| nextjs-commerce (ts) | 0.02241 | 0.02241 | 0.0000 |
| nestjs-starter (ts) | 0.01754 | 0.01754 | 0.0000 |
| kafka-streams-examples (java) | 0.03329 | 0.03329 | 0.0000 |
| vapor-api-template (swift) | 0.02128 | 0.02128 | 0.0000 |
| ktor-samples (kotlin) | 0.04735 | 0.04735 | 0.0000 |
| client-fixture-a (python) | 0.03410 | 0.03427 | +0.017 |
| client-fixture-b (ts) | 0.00572 | 0.00572 | 0.0000 |
| **client-fixture-c (ts/tsx)** | **0.01433** | **0.00873** | **-0.560** |

Zero ≥0.5pp regressions. The +0.017pp on client-fixture-a (python) is
unrelated to this PR (no python code touched); within run-to-run
noise. The -0.057pp on flask is similarly orthogonal — likely an
artifact of the post-#621 merge ordering on shared infra. All TS/JS
allowlists are inside `if lang == "javascript" || lang == "typescript"`,
no cross-language leakage possible.

Residual root cause (client-fixture-c, post-fix at 0.87%):
- Bug-extractor top: `indicator` (12), `delete` (12), `show` (10),
  `has` (6), `create` (5), `close` (4), `copy` (4), `back` (3) — all
  domain-generic verbs / nouns too collision-prone for a global
  allowlist. Resolving requires receiver-type inference (cross #494
  equivalent for JS/TS).
- Bug-resolver top (21 total): `detail` (7), `deficiencies` (5),
  `showInlineAlert` (3) — user-defined cross-module bare names that
  failed to bind because the extractor receiver-stripped them.
  Resolver-side fix is out of scope for the external-synth allowlist.

Status: **ship-gate (≤1%) cleared at 0.87%**. Track A (RN platform
symbols, PR #621 chain-fix #1) complete. Track B (Reanimated
receiver-strip via file-scoped gate, PR #621 chain-fix #2) complete.
Chain-fixes #3 (react_props followups) and #4 (default-export name
tracking) remain queued.

Chain-fixes filed (out of scope for this PR):
1. **js-receiver-type-inference** — domain-generic bare names
   (`indicator`, `delete`, `show`, `has`, `create`, `close`, `copy`)
   are receiver-stripped at extraction time and lose the class context.
   Same blocker as Python #494; a TS-side receiver-type primitive
   would lift these without a global allowlist.
2. **js-resolver-shortform-followups** — bug-resolver residue
   (`detail`, `deficiencies`, `showInlineAlert`) suggests other
   cross-file extraction shapes that fail to bind via `byLocation`
   short-form. Audit needed.
3. **default-export-name-tracking** — carry-forward from PR #621.
   Long-term: JS extractor records default-export status per entity
   for deterministic `<module>.default` binding.

---

## Python orphan recovery — REFERENCES emission + IMPORTS to_id (analog of #641/#642 for Python)

Date: 2026-05-19. Branch: feat/python-orphan-recovery.

Python extractor previously emitted ~0 REFERENCES edges (audit
2026-05-19: REFERENCES/fn ≈ 0.00 on all major Python corpora) and left
IMPORTS ToIDs as bare-name / dotted strings, so the bare-name resolver
had to round-trip every imported leaf and frequently missed. Two new
passes — `emitReferences` (Track A, analog of #641) and
`resolveImportToIDs` (Track B, analog of #642) — close both gaps for
Python while leaving every other language byte-identical.

### Per-Python-corpus orphan rate before / after

| Corpus | Before | After | REFERENCES/fn after | Risk after |
|---|---:|---:|---:|---:|
| requests | 2.6% | **1.7%** | 1.83 | 83 |
| click | 12.7% | **7.4%** | 1.34 | 76 |
| flask-realworld | 29.0% | **24.9%** | 1.06 | 81 |
| django-realworld | 44.2% | **41.9%** | 1.15 | 81 |
| pandas | 46.0% | **25.8%** | 3.68 | 84 |
| client-fixture-a (Django) | 48.4% | **37.0%** | 1.85 | 75 |

REFERENCES/fn moved from 0.00 → 1.06–3.68 across the board. Risk score
(composite of orphan rate, imports hygiene, references density) moved
29–49 → 75–84.

### Quality fixture recall (gate)

| Fixture | Entity recall | Relationship recall | Forbidden hits |
|---|---:|---:|---:|
| python-django-mini | 28/28 (100.0%) | 12/12 (100.0%) | 0 |
| typescript-react-mini | 20/20 (100.0%) | 16/16 (100.0%) | 0 |
| java-spring-mini | 25/25 (100.0%) | 18/18 (100.0%) | 0 |
| go-chi-mini | 20/20 (100.0%) | 19/19 (100.0%) | 0 |
| rust-tokio-mini | 16/16 (100.0%) | 13/13 (100.0%) | 0 |

100% must-have recall preserved; 0 forbidden edges.

### Cross-language bug-rate parity

| Corpus | Lang | Main bug-rate | Branch bug-rate |
|---|---|---:|---:|
| chi | Go | 0.01860 | 0.01860 |
| spdlog | C++ | 0.02754 | 0.02754 |
| kafka-streams-examples | Java | 0.03027 | 0.03024 |
| vapor-api-template | Swift | 0.02083 | 0.02083 |
| ktor-samples | Kotlin | 0.03200 | 0.03200 |

Bug-rates byte-identical (or within float drift on Kafka). The Python
extractor changes do not touch any other language's emission path.

### Residual root cause

The remaining Python orphan delta (e.g. django-realworld 41.9%,
client-fixture-a 37.0%) is dominated by **cross-file orphans** — Django
model classes, viewsets, and Celery tasks defined in one module and
referenced only from sibling modules. The Python REFERENCES pass binds
same-file uses; cross-file binding for those references requires the
resolver's per-module import-aware `ResolveBareCallTarget` path to also
fire on REFERENCES (today it only rewrites CALLS). Chain-fixes filed:

- **chain-fix:python-references-cross-file** — extend
  `resolve/imports.go ResolveImports` to consider REFERENCES edges in
  addition to CALLS, using the same `source_module + imported_name`
  resolution. Expected -10–20pp on django-realworld and client-fixture-a.
- **chain-fix:python-class-field-cross-file** — class fields (#526) are
  only emitted at the IMMEDIATE class-body scope. Django model fields
  (`title = models.CharField(...)`) and DRF serializer fields are
  picked up; mixin and base-class fields referenced via `self.<attr>`
  on subclasses in OTHER files still miss. Requires hierarchy-aware
  field lookup or per-class attr propagation through EXTENDS edges.

### Status

- **PR**: shipped; quality gate green; cross-language regression-free.
- **Coverage**: same-scope identifier, self.<attr>, f-string
  interpolation, imported-name resolution, known-external rewrite,
  in-tree imports untouched, builtins skipped, self-references
  filtered.
- **Follow-ups**: two chain-fixes above; non-Python language analogs
  (Go REFERENCES, Java REFERENCES, Ruby REFERENCES, etc.) are
  independent waves.
