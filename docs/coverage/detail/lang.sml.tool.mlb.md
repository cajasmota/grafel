<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.sml.tool.mlb` — MLB (MLton ML Basis build file)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [Standard ML](../by-language/sml.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | `2026-06-24` | — | `internal/extractors/cross/manifest/sml.go` | MLB has no lockfile format — MLton resolves the basis/source closure at compile time from the ordered .mlb include graph (the MLB semantics ARE the resolution), not a separate lockfile. There is no version-constraint syntax in .mlb, so there is nothing to lock at the manifest level. |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5383 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/sml.go`<br>`internal/extractors/cross/manifest/sml_test.go` | parseMLB mines the MLton *.mlb ML Basis path list (basis/local/in/end/open/ann declaration keywords and ann "..." annotation strings skipped; (* ... *) comments stripped). The .mlb includes — the anchored MLton stdlib basis $(SML_LIB)/basis/basis.mlb plus nested .mlb includes (basename-converged) — become DEPENDS_ON + DEPENDS_ON_PACKAGE + SBOM package nodes (package_manager=mlton_mlb) with EMPTY version (MLB has no version-constraint syntax; honest). The stdlib basis floor (basis.mlb) is KEPT as a real edge (mirrors cabal base / nimble nim / idris base, #5373/#5367/#5382). Source-file members (.sml/.sig/.fun/.ml) are surfaced on the project anchor as a compact mlb_config property (no new entity kind; same model as cm_config / ipkg_config). Suffix-dispatched in IsManifest/detectPackageManager/dispatchParser. Compiler toolchain: MLton; Poly/ML uses its own use-file model. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.sml.tool.mlb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
