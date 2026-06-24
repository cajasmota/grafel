<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lisp.tool.asdf` — ASDF / Quicklisp (Common Lisp system definition)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lisp](../by-language/lisp.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | `2026-06-24` | 5385 | `internal/extractors/cross/manifest/lisp.go` | ASDF / Quicklisp has no per-project in-repo lockfile. A Quicklisp dist pins exact system versions REMOTELY (by dated dist release, e.g. quicklisp 2024-xx-xx), resolved at install time against the Quicklisp dist index — there is no committed lock manifest in the source tree to parse. The *.asd :depends-on list is the declared dependency surface; manifest_parsing covers it in full. |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5385 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/lisp.go`<br>`internal/extractors/cross/manifest/lisp_test.go` | parseAsd mines the Common Lisp ASDF *.asd system definition: every `(defsystem ...)` form (optionally package-qualified — asdf:defsystem / cl:defsystem — and with a string, keyword, or bare-symbol system name) is scanned for its `:depends-on (...)` list, which is balance-parsed (matchParen) so nested dependency-spec lists are read correctly. All three ASDF dependency shapes are recovered: bare string ("alexandria"), bare/keyword symbol (bordeaux-threads / :alexandria), and a dependency-spec list whose dep name is the contained string/symbol — (:version "cl-ppcre" "2.0.0") (the constraint IS captured as version), (:feature :sbcl "sb-posix") (the :feature guard keyword is skipped), and (:require "uiop"). Reader-conditional #+/#- guards are flattened (the guard expression is skipped, the dep is kept). `;`/`;;` line comments and #| |# block comments are stripped (stripLispComments) so commented-out deps are not mined. The system name itself is never emitted as a dependency. A multi-system .asd (system + system/tests) merges all forms' deps into the per-file project anchor. The system name + top-level :components member names (:file/:module/:static-file/...) are surfaced on the project anchor as a compact deterministic `asd_config` property — no new entity kind, the same model as the Idris ipkg_config / SML cm_config / Zig build_targets props (#5382/#5383/#5377). Suffix-dispatched in IsManifest/detectPackageManager/dispatchParser to package_manager=asdf; emits DEPENDS_ON + DEPENDS_ON_PACKAGE + SBOM package nodes like every other ecosystem. Quicklisp installs exactly these :depends-on systems, so the .asd is the canonical Quicklisp dependency manifest. Proven by TestAsd_Dependencies / _VersionSpec / _DependsOnEdges / _ConfigAnchor / _PackageQualifiedDefsystem / _MultipleSystems / _NoDependencies / _IsManifest. Honest scope: only the declared :depends-on manifest surface is recovered; ASDF :defsystem-depends-on (build-time system-class deps) and #+/#- conditional guards are flattened (deps kept, condition dropped); Lisp dialect fragmentation (Scheme/Racket/Clojure) is out of scope. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.lisp.tool.asdf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
