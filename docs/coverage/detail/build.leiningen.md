<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.leiningen` — Leiningen

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | Leiningen — the dominant pre-2018 Clojure build tool — detected via project.clj/(defproject …)/:dependencies [ …Maven coords… ]/:profiles signals (build_tools.yaml). Manifest-level dependency detection works; full dependency-graph edge extraction is the #3828 build-graph follow-up. (cf. Elixir build.hex partial.) |
| Target extraction | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | :uberjar-name / :main / :ring {:handler …} application signals detected; structured lein-task/target extraction tracked in #3828. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.leiningen ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
