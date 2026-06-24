<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.sml.tool.cm` — CM (SML/NJ Compilation Manager build file)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [Standard ML](../by-language/sml.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | `2026-06-24` | — | `internal/extractors/cross/manifest/sml.go` | CM has no lockfile format — the SML/NJ Compilation Manager resolves and pins the dependency closure at build time from the anchored .cm group graph (CM/anchors path config), not a per-unit lockfile. There is no version-constraint syntax in .cm, so there is nothing to lock at the manifest level. |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5383 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/sml.go`<br>`internal/extractors/cross/manifest/sml_test.go` | parseCM mines the SML/NJ *.cm group/library member list (between the `is` keyword and the closing `end`, after an optional `( <exports> )` clause; (* ... *) comments stripped). Library references — anchored SML/NJ stdlib refs ($/basis.cm, $smlnj/...) plus relative .cm includes (util/util.cm, basename-converged) — become DEPENDS_ON + DEPENDS_ON_PACKAGE + SBOM package nodes (package_manager=smlnj_cm) with EMPTY version (CM has no version-constraint syntax; honest). The toolchain stdlib floor ($/basis.cm) is KEPT as a real edge, mirroring cabal base / nimble nim / idris base (#5373/#5367/#5382). Source-file members (.sml/.sig/.fun/.ml) and the group/library export kind are surfaced on the project anchor as a compact cm_config property (no new entity kind; same model as ipkg_config / Zig build_targets). Suffix-dispatched in IsManifest/detectPackageManager/dispatchParser. Compiler toolchain: SML/NJ (CM), also consumable by MLton via cm2mlb. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.sml.tool.cm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
