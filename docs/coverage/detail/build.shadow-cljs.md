<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.shadow-cljs` — shadow-cljs

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | shadow-cljs — the standard ClojureScript build tool — detected via shadow-cljs.edn/{:builds {…}}/:target signals plus the high-confidence shadow-cljs.edn + package.json co-presence (build_tools.yaml). Bridges the npm ecosystem; npm dependency-graph extraction is handled by the JS manifest path, cljs build-graph extraction is the #3828 follow-up. |
| Target extraction | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | :builds map with :target (:browser/:node-script/:npm-module/:react-native) detected via content markers; structured build-target extraction tracked in #3828. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.shadow-cljs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
