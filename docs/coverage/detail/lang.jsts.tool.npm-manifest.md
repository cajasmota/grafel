<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.tool.npm-manifest` — npm / yarn / pnpm (package.json + lockfile)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency usage status | ✅ `full` | `2026-06-24` | — | `cmd/grafel/index.go`<br>`internal/external/manifest_xref.go`<br>`internal/external/manifest_xref_test.go` | #5526: whole-graph pass external.CrossReferenceManifests, run after external.Synthesize, joins declared package.json deps + lockfile-resolved versions + the JS/TS import graph (ext:<pkg> nodes / IMPORTS source_module roots, subpath-normalised e.g. lodash/fp→lodash). Stamps each declared npm dep with imported=true|false; a declared-but-unimported dep is flagged dead_dependency_candidate=true (the dead-dependency view). The matching import-derived ext:<pkg> External node is enriched with the resolved version + dep_section so a version surfaces on the navigable node. JS/TS-scoped; other ecosystems (requirements.txt/pyproject, go.mod, Cargo.toml, Gemfile, pom.xml) are follow-ups. Complements the multi-language analysis.dependency-hygiene deplinker pass (usage_status used/unused/phantom). |
| Lockfile parsing | ✅ `full` | `2026-06-24` | — | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/extractor_test.go` | #5526 / #2865: package-lock.json (npm v1/v2/v3), pnpm-lock.yaml (v5/v6/v9) and yarn.lock (classic v1 + berry v2+) parsed to the resolved top-level package→exact-version map (dependency_kind=locked). The declared-dependency cross-reference pass (internal/external/manifest_xref.go) prefers the lockfile-resolved EXACT version for each declared dep, falling back to the manifest range when no lockfile resolution exists. |
| Manifest parsing | ✅ `full` | `2026-06-24` | — | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/extractor_test.go` | #5526: package.json parsed into one external_dependency record per declared package across ALL four sections — dependencies (dep_section=prod), devDependencies (dev), peerDependencies (peer), optionalDependencies (optional). Each carries the declared version RANGE (version_range), is_dev, dependency_kind and declared=true. First-section-wins on duplicate names. SCOPE.Component(external_dependency) + DEPENDS_ON + converged SCOPE.Package nodes emitted as before. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.tool.npm-manifest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
