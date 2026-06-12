<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.boot` — Boot

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | Boot detected via build.boot/(set-env! …)/(deftask …) manifest signals (build_tools.yaml). Legacy task-based build tool; dependency-graph/target extraction beyond manifest detection is the #3828 build-graph follow-up. |
| Target extraction | 🟢 `partial` | `2026-06-12` | 4910 | `internal/engine/rules/clojure/build_tools.yaml` | (deftask …) task signals detected via manifest content markers; structured target/pipeline extraction tracked in #3828. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
