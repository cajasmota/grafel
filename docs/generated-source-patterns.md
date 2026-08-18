# Generated-source patterns — unvalidated input for #6329

> **This is not configuration. Nothing reads this file.**
>
> It is a research note about the 31 `internal/engine/rules/*/skip_patterns.yaml`
> files deleted in #6330. Treat every line below as a claim to be checked against
> real repositories, not as a rule to be switched on.
>
> Do **not** turn this into a YAML file and do **not** wire it into the
> classifier. That is exactly the trap #6330 removed. Any pattern that survives
> review must arrive with a consumer and a test that fails when the pattern is
> wrong.

## Read the originals, don't trust this summary

The 31 deleted files are recoverable in full. **Do this before acting on the
table below** — the table is a lossy digest, and the digest is biased (see
[Provenance](#provenance-and-known-gaps)):

```sh
# One language:
git show 753771635:internal/engine/rules/swift/skip_patterns.yaml

# All 31, into a scratch directory:
for f in $(git ls-tree -r --name-only 753771635 internal/engine/rules \
             | grep '/skip_patterns\.yaml$'); do
  echo "===== $f"; git show "753771635:$f"
done
```

`753771635` is the commit immediately preceding the deletion. The files were
never renamed, so `git log --diff-filter=D -- '*/skip_patterns.yaml'` will also
find them if that SHA ages out of easy reach.

## Why the old files were deleted

`classifier.New` only ever populated its glob-skip list when it was handed a
non-empty `yamlDataDir`, and all four production call sites passed `""`. The 31
YAML files therefore never executed — not once — and so were never validated:

- 6 files did not unmarshal at all into the loader's struct (`cicd`,
  `html_templates`, `javascript_typescript`, `protobuf`, `scala`, `swift`);
  a `slog.Warn` nobody read discarded them whole.
- 2 more (`java`, `ruby`) unmarshalled but yielded nothing usable. Both *do*
  have a top-level `skip_patterns:` list; the divergence is per-entry — they
  carry `file_patterns:` / `paths:` where the loader's struct read `pattern:`,
  so every entry hit the `if sp.Pattern == ""` continue.
- Of the 247 patterns that did parse, 155 (63%) could never match: the loader
  used `filepath.Match`, which has no `**` and does not cross `/`.
- Several entries were prose, not globs — `"kustomize build output"`,
  `"*.dev.yml / *.local.yml"`, `"build.rs output (OUT_DIR)"`,
  `"*.proto with zero service/message blocks"`,
  `"V{timestamp}__{description}.sql (Flyway auto-generated)"`. Python's 7
  entries were symbolic names (`protobuf_generated`, `type_stub_file`) whose
  real content lived in a `match_conditions:` list the loader never read.
- Of the 92 that would have matched, switching them on would have immediately
  dropped **`*_test.go`** (every Go test file in the graph), plus `install.sh`,
  `bootstrap.sh`, `*.env`, `AssemblyInfo.cs`, `GlobalUsings.cs` and `sqlite3.c`.

See #6330 for the full accounting.

## What was worth keeping

The intent behind the files — treat machine-generated source differently from
authored source — is sound, and #6329 is rebuilding it properly with a consumer
and tests. What follows is the subset that plausibly identifies **generated
source that still declares real symbols**: files a graph may want to mark as
generated (or parse for declarations while skipping bodies) rather than drop.

Build-output directories, binary artifacts, IDE metadata and vendor trees are
deliberately **not** reproduced here — those are already handled by
`universalPathSkip` / binary-extension detection in `internal/classifier`, or
they are not source at all.

| Language | Pattern | Generator |
|---|---|---|
| go | `*.pb.go` | protoc-gen-go |
| go | `*.pb.gw.go` | grpc-gateway |
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
| dart | `*.mocks.dart` | mockito `@GenerateMocks` — **`scope=test`, not skip** |
| dart | `generated_plugin_registrant.dart` | Flutter toolchain |
| kotlin | `BuildConfig.kt` | Android Gradle Plugin |
| kotlin | `R.kt` | Android resource compiler / KSP |
| kotlin | `*_Generated.kt`, `**/*GeneratedBy*.kt` | Room / Hilt / Dagger annotation processors |
| kotlin | `**/*.pb.kt` | protoc Kotlin DSL |
| kotlin | `**/*_Impl.kt` | Room DAO/database implementations |
| java | `*MapperImpl.java` | MapStruct |
| java | `*_.java` | JPA static metamodel |
| java | `javax.annotation.processing.Generated`, `@Generated` | **content marker**, not a filename glob |
| scala | `*_generated.scala`, `*_managed.scala` | sbt-managed sources (protobuf, thrift, avro) |
| scala | `*.html.scala`, `*.xml.scala`, `*.js.scala`, `*.txt.scala` | Twirl templates (Play) — source of truth is the `.html` |
| swift | `*.pb.swift` | SwiftProtobuf |
| swift | `*.grpc.swift` | grpc-swift |
| swift | `*.generated.swift` | Sourcery / SwiftGen / Mockolo |
| swift | `R.generated.swift` | R.swift resource accessor |
| swift | `*Mock.swift` | Mockolo / Sourcery |
| swift | `DI/GeneratedDI.swift` | Needle DI |
| rust | `*.pb.rs`, `*_pb.rs` | prost / tonic |
| rust | `*.generated.rs` | assorted codegen |
| graphql/ts | `*.generated.ts`, `*.generated.tsx`, `**/*.generated.js` | graphql-codegen and friends |
| graphql/ts | `__generated__/` | Relay |
| ts | `**/*.d.ts` | declaration output — **has authored exceptions**, see hazards |
| cpp | `*.pb.cc`, `*.pb.h` | protoc |
| cpp | `*.grpc.pb.cc`, `*.grpc.pb.h` | grpc_cpp_plugin |
| cpp | `moc_*.cpp`, `ui_*.h`, `qrc_*.cpp` | Qt moc / uic / rcc |
| cpp | `*.odb.cxx`, `*.odb.hxx` | ODB ORM compiler |
| python | `*_pb2.py`, `*_pb2_grpc.py`, `*_pb2.pyi` | protoc |
| js/ts | `*_pb.js`, `*_pb.d.ts`, `*_grpc_web_pb.js`, `*.pb.ts`, `*.connect.ts` | protoc / connect-web |
| csharp | `*.pb.cs` | protoc — original keyed on the `// <auto-generated>` header, not the suffix |
| ruby | `*_pb.rb`, `*_services_pb.rb` | protoc |
| ruby | `db/schema.rb` | Rails, regenerated from `db/migrate/` on `db:schema:load` |

### Content-conditional entries — do not flatten these into globs

Several entries were never `(language, glob)` pairs at all. They were
predicates over file *content*, and reducing them to a filename match inverts
their meaning. They are listed separately because hazard 5 below is exactly
about this.

| Source | Condition | Intended disposition |
|---|---|---|
| python / sql | Alembic migration in `alembic/versions/` **containing only `op.*` calls** | `scope=migration`. The original is explicit that "manually authored migrations (with custom `upgrade()` logic beyond simple `op.*` calls) should be indexed" — a filename match would eat those too. |
| sql | `schema.sql` / `structure.sql` / `dump.sql` **whose header is `-- PostgreSQL database dump` / `-- MySQL dump`** | skip |
| sql | `seed*.sql`, `fixture*.sql` **containing only `INSERT INTO`, no DDL** | skip |
| sql | Flyway `V[0-9]{8,}__*.sql` **with no stored procedures/views** | `scope=migration` |
| python | `__init__.py` **whose top-level statements are all imports plus optional `__all__`** | skip |
| python | `conftest.py` **whose functions are all `@pytest.fixture`, no `test_` functions** | skip |
| python | `.pyi` stubs | skip **unless** the project has no corresponding `.py` (stub-only packages) |
| protobuf | `.proto` **with zero service/message blocks** | skip |
| javascript | barrel `index.ts` **whose top-level statements are all re-exports** | skip |

## Provenance and known gaps

Be precise about what this digest is:

- **It is not the complete set.** Directory, build-output, IDE-metadata and
  vendor entries were dropped on purpose; some judgement calls were made about
  what counts as "declares symbols". Recover the originals with the commands
  above rather than assuming absence here means absence there.
- **The go rows are a superset of go's own file.** `*_grpc.pb.go`,
  `*.pb.validate.go`, `*.connect.go` and `*.twirp.go` came from
  `protobuf/skip_patterns.yaml`, not from `go/skip_patterns.yaml`.
- **The six files that never unmarshalled are the least-reviewed content here**
  — `cicd`, `html_templates`, `javascript_typescript`, `protobuf`, `scala`,
  `swift`. Nobody has ever seen these execute, and they were also the hardest
  to digest because their schemas were the most irregular. Weight them as the
  *least* trustworthy rows in the table, not the most, and read the originals.

## Known hazards, carried forward deliberately

Anyone acting on the table above should know what bit the old config:

1. **The config was tri-state — arguably four-state — and the loader was
   binary.** This was a repeated, deliberate design intent, not a one-off:
   `go` (`*_test.go`), `dart` (`*.mocks.dart`) and `sql` (three entries) all
   used `action: scope=…, skip LLM enrichment` to mean "index it, tag it, don't
   enrich it". `sql` adds a fourth state on top of `scope=test`:
   `action: scope=migration, skip LLM enrichment`. The loader collapsed every
   one of these to an unconditional skip, which is how "tag Go test files"
   would have become "delete every Go test file". #6329 must not lose the
   distinction at its own loader boundary.
2. **`build.rs` is authored source, and the original knew that.** The rust file
   listed `build.rs` with `match_type: exact_filename` and an explicit
   `exception:` — *"May be indexed as a low-priority metadata file to detect
   generated code paths and FFI linkage."* So the author meant "treat as build
   metadata", a fourth disposition again, not "generated output". The separate
   prose entry `build.rs output (OUT_DIR)` was the one aimed at generated code.
   The substantive point stands — `build.rs` is hand-written and must not be
   dropped — but the original was not confused about it.
3. **Suffix matching alone is not evidence of generation.** `*.g.cs` collides
   with hand-written files in some codebases, and the `javascript_typescript`
   file's own `**/*.d.ts` entry carried an `exceptions:` list for authored
   declaration files. The `protobuf` file made the better choice for C#: it
   keyed on the `// <auto-generated>` header rather than on `.pb.cs`.
4. **Prefer a content marker to a filename.** Go's
   `^// Code generated .* DO NOT EDIT\.$` header, the `@generated` marker,
   Java's `javax.annotation.processing.Generated` / `@Generated` annotations,
   C#'s `// <auto-generated>`, Python's
   `# Generated by the protocol buffer compiler` and
   `.gitattributes linguist-generated` are checkable facts about a file's
   contents. Filename globs are a heuristic.
5. **Match semantics must be chosen, not inherited.** The deleted loader used
   `filepath.Match` against the basename and then the whole path, which quietly
   made every `/`-containing and `**` pattern inert — 63% of the corpus.
   Whatever #6329 uses must state its matcher and have a test per pattern class.
   The `**`-prefixed patterns above (`**/*.pb.kt`, `**/*.generated.js`) are
   written in the originals' notation and require a matcher that supports it.
