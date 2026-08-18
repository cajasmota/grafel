# Generated-source patterns — unvalidated input for #6329

> **This is not configuration. Nothing reads this file.**
>
> It is a research note: raw, *unvalidated* material salvaged from the 31
> `internal/engine/rules/*/skip_patterns.yaml` files that were deleted in #6330.
> Treat every line below as a claim to be checked against real repositories, not
> as a rule to be switched on.
>
> Do **not** turn this into a YAML file and do **not** wire it into the
> classifier. That is exactly the trap #6330 removed. Any pattern that survives
> review must arrive with a consumer and a test that fails when the pattern is
> wrong.

## Why the old files were deleted

`classifier.New` only ever populated its glob-skip list when it was handed a
non-empty `yamlDataDir`, and all four production call sites passed `""`. The 31
YAML files therefore never executed — not once — and so were never validated:

- 6 files did not unmarshal at all into the loader's struct (`cicd`,
  `html_templates`, `javascript_typescript`, `protobuf`, `scala`, `swift`);
  a `slog.Warn` nobody read discarded them whole.
- 2 more (`java`, `ruby`) parsed to zero patterns because their top-level key
  did not match.
- Of the 247 patterns that did parse, 155 (63%) could never match: the loader
  used `filepath.Match`, which has no `**` and does not cross `/`.
- Several entries were prose, not globs — `"kustomize build output"`,
  `"*.dev.yml / *.local.yml"`, `"build.rs output (OUT_DIR)"`,
  `"*.proto with zero service/message blocks"`. Python's 7 entries were
  symbolic names (`protobuf_generated`, `type_stub_file`), not patterns.
- Of the 92 that would have matched, switching them on would have immediately
  dropped **`*_test.go`** (every Go test file in the graph), plus `install.sh`,
  `bootstrap.sh`, `*.env`, `AssemblyInfo.cs`, `GlobalUsings.cs` and `sqlite3.c`.

See #6330 for the full accounting.

## What was worth keeping

The intent behind the files — treat machine-generated source differently from
authored source — is sound, and #6329 is rebuilding it properly with a consumer
and tests. What follows is the subset of the old content that plausibly
identifies **generated source that still declares real symbols**: files a graph
may want to mark as generated (or parse for declarations while skipping bodies)
rather than drop outright.

Build-output directories, binary artifacts, IDE metadata and vendor trees are
deliberately **not** reproduced here — those are already handled by
`universalPathSkip` / binary-extension detection in `internal/classifier`, or
they are not source at all.

| Language | Pattern | Generator |
|---|---|---|
| go | `*.pb.go` | protoc-gen-go |
| go | `*_grpc.pb.go` | protoc-gen-go-grpc |
| go | `*.pb.gw.go` | grpc-gateway |
| go | `*.pb.validate.go` | protoc-gen-validate |
| go | `*.connect.go` | connect-go |
| go | `*.twirp.go` | twirp |
| go | `wire_gen.go` | google/wire |
| go | `zz_generated*.go` | controller-gen (Kubernetes operators) |
| go | `*_string.go` | `stringer` |
| go | `bindata.go` | go-bindata / go-rice |
| go | `mock_*.go`, `*_mock.go` | gomock / mockery |
| csharp | `*.Designer.cs` | WinForms designer |
| csharp | `*.g.cs`, `*.g.i.cs` | Roslyn source generators |
| dart | `*.g.dart` | build_runner (json_serializable, drift, floor, isar, hive, retrofit) |
| dart | `*.freezed.dart` | freezed |
| dart | `*.config.dart` | injectable |
| dart | `*.mocks.dart` | mockito `@GenerateMocks` |
| dart | `generated_plugin_registrant.dart` | Flutter toolchain |
| kotlin | `BuildConfig.kt` | Android Gradle Plugin |
| kotlin | `R.kt` | Android resource compiler / KSP |
| kotlin | `*_Generated.kt` | Room / Hilt annotation processors |
| java | `*MapperImpl.java` | MapStruct |
| java | `*_.java` | JPA static metamodel |
| rust | `*.pb.rs`, `*_pb.rs` | prost / tonic |
| rust | `*.generated.rs` | assorted codegen |
| graphql/ts | `*.generated.ts`, `*.generated.tsx` | graphql-codegen |
| graphql/ts | `__generated__/` | Relay |
| cpp | `*.pb.cc`, `*.pb.h` | protoc |
| cpp | `*.grpc.pb.cc`, `*.grpc.pb.h` | grpc_cpp_plugin |
| cpp | `moc_*.cpp`, `ui_*.h`, `qrc_*.cpp` | Qt moc / uic / rcc |
| cpp | `*.odb.cxx`, `*.odb.hxx` | ODB ORM compiler |
| python | `*_pb2.py`, `*_pb2_grpc.py`, `*_pb2.pyi` | protoc |
| js/ts | `*_pb.js`, `*_pb.d.ts`, `*_grpc_web_pb.js`, `*.pb.ts`, `*.connect.ts` | protoc / connect-web |
| ruby | `*_pb.rb`, `*_services_pb.rb` | protoc |

## Known hazards, carried forward deliberately

Anyone acting on the table above should know what bit the old config:

1. **`*_test.go` is not generated source.** The old go file listed it with
   `action: scope=test`, i.e. "tag it", but the loader collapsed every entry to
   an unconditional skip. Any tri-state design in #6329 must not lose that
   distinction at the loader boundary.
2. **`build.rs` is authored source**, not output. The rust file listed both
   `build.rs` and the prose entry `build.rs output (OUT_DIR)`; only the second
   was meant.
3. **`*.g.cs` collides with hand-written files** in some codebases; suffix
   matching alone is not evidence of generation.
4. **A "generated" marker is better evidence than a filename.** Go's
   `^// Code generated .* DO NOT EDIT\.$` header, the equivalent
   `@generated` marker, and `.gitattributes linguist-generated` are checkable
   facts about a file's content. Filename globs are a heuristic; prefer content
   evidence where it exists.
5. **Match semantics must be chosen, not inherited.** The deleted loader used
   `filepath.Match` against the basename and then the whole path, which quietly
   made every `/`-containing and `**` pattern inert. Whatever #6329 uses must
   state its matcher and have a test per pattern class.
