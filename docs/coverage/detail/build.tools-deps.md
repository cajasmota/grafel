<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.tools-deps` — tools.deps / deps.edn

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | tools.deps — the official Clojure dependency system (1.9+) — detected via deps.edn/{:deps {…}}/:mvn/version/:git/url/:aliases signals (build_tools.yaml). Maven and git coordinate deps detected at the manifest level; full dependency-graph edge extraction is the #3828 build-graph follow-up. |
| Target extraction | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | :aliases task variants and the tools.build build.clj pattern (io.github.clojure/tools.build) detected via content markers; structured alias/target extraction tracked in #3828. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.tools-deps ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
