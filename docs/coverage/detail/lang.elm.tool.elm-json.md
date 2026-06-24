<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elm.tool.elm-json` — elm.json (Elm package manager)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elm](../by-language/elm.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | `2026-06-24` | 5375 | `internal/classifier/classifier.go`<br>`internal/extractors/cross/manifest/elm.go`<br>`internal/extractors/cross/manifest/elm_test.go`<br>`internal/extractors/cross/manifest/extractor.go` | elm.json IS the lockfile — an application's direct/indirect dependency versions are EXACTLY pinned (the indirect block is the resolved transitive closure), so there is no separate lockfile format. The resolved tree is recovered directly from manifest_parsing (indirect deps flagged indirect=true). |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5375 | `internal/classifier/classifier.go`<br>`internal/extractors/cross/manifest/elm.go`<br>`internal/extractors/cross/manifest/elm_test.go`<br>`internal/extractors/cross/manifest/extractor.go` | elm.json is JSON with two shapes distinguished by the type field. parseElmJSON handles both: an application's dependencies block ({direct:{...},indirect:{...}} of exactly-pinned versions, indirect deps flagged indirect=true) and a package's flat {author/project: version-range} map; both carry a test-dependencies block flagged is_dev=true. First-declaration-wins on duplicates (direct>indirect>test). elm.json is classified elm (classifier.go exactBasenameLanguageMap) so it reaches the _cross_manifest pass, and is wired into IsManifest/detectPackageManager/dispatchParser/parsers by exact basename to package_manager=elm; emits DEPENDS_ON + DEPENDS_ON_PACKAGE edges + SBOM package nodes like every other ecosystem. Proven by TestElmJSON_ApplicationDeps / _PackageDeps / _DependsOnEdges / _IsManifest. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elm.tool.elm-json ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
