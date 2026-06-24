<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.fsharp.tool.paket` — Paket (.NET/F# package manager)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [F#](../by-language/fsharp.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | ✅ `full` | `2026-06-24` | 5368 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/paket.go`<br>`internal/extractors/cross/manifest/paket_test.go` | #5368 (epic #5360): paket.lock is the fully-resolved transitive closure Paket writes after `paket install` — the resolved tree the manifest never fully names (#2865 lockfile_parsing contract). parsePaketLock walks the `NUGET` resolver sections, recording each indented `<PackageId> (<version>)` resolved entry as kind=locked. `GROUP Test`/`GROUP Build` headers flag is_dev=true. Honest: transitive CONSTRAINT sub-lines (`Name (>= x)` nested under a package) carry a constraint operator, not a resolved pin, so a name with no own resolved top-level entry is NOT recorded, and a name already recorded at its resolved entry de-dupes its constraint sub-lines; non-NUGET sections (GITHUB/GIT/HTTP file references) are skipped. Proven by TestPaket_LockResolvedDeps / _LockGroupTestIsDev / _LockTransitiveConstraintDeduped. |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5368 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/paket.go`<br>`internal/extractors/cross/manifest/paket_test.go` | #5368 (epic #5360): paket.dependencies is Paket's declared-dependency manifest (the F#/.NET alternative to bare NuGet PackageReference). parsePaketDependencies mines each `nuget <PackageId> [<constraint>]` line, keeping the Paket constraint (`~> 5.0`, `>= 7.0.0`, exact `13.0.3`, or bare) verbatim as the version and stripping trailing per-package options (prerelease/content:/redirects:/...). `group Test`/`group Build`/`group Fake` blocks flag is_dev=true (the Paket test/build-tooling idiom, mirroring conanfile test_requires + nimble taskRequires); the implicit Main group is runtime, and a Main declaration wins runtime over a later same-name group re-declaration. paket.dependencies is suffix/exact-dispatched in IsManifest/detectPackageManager/dispatchParser to package_manager `paket`; emits DEPENDS_ON + DEPENDS_ON_PACKAGE edges + SBOM package nodes like every other ecosystem. Honest-partial: `github user/repo`/`git https://…` source dependencies are not NuGet packages (no paket NuGet coordinate) and are skipped; `source`/`framework`/`storage`/`strategy`/... config directives are ignored. Proven by TestPaket_DependenciesRuntimeDeps / _DependenciesGroupsAreDev / _DependenciesRuntimeWinsOverDev / _DependsOnEdges / _NoDependencies. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.fsharp.tool.paket ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
