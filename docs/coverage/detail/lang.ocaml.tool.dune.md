<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ocaml.tool.dune` — Dune / opam (OCaml build)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [OCaml](../by-language/ocaml.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Manifest parsing | ✅ `full` | `2026-06-24` | 5374 | `internal/extractors/cross/manifest/extractor.go` | Two OCaml manifest shapes parsed (#5374): *.opam (parseOpam mines the depends: list of quoted package names with {>= "x"} version formulas; a {with-test}/{with-doc} filter flags is_dev; depopts: entries are kind=optional; first version literal of the formula is recorded) and dune-project (parseDuneProject reads the inline (package (depends ...)) generate_opam_files stanza: bare atoms, (caqti (>= 1.9.0)) constrained sub-lists, :with-test dev filters). The ocaml compiler-version floor is kept as a real edge (mirrors nimble nim / luarocks lua). Suffix/exact-name dispatched in IsManifest/detectPackageManager/dispatchParser to package managers opam/dune; emits DEPENDS_ON + DEPENDS_ON_PACKAGE + SBOM package nodes like every other ecosystem. Honest scope: opam version formula conjunctions/filters reduced to the first version literal; pin-depends:/conflicts:/available: not modelled; plain dune (libraries ...) lists are NOT mined (internal+external indistinguishable; that surface is the source-graph IMPORTS). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ocaml.tool.dune ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
