<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.plugin-system` — Plugin / extension-system registration (SCOPE.Plugin + REGISTERS_PLUGIN)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/plugin_system_edges.go`<br>`internal/engine/plugin_system_edges_test.go` | Emits one REGISTERS_PLUGIN edge (RelationshipKindRegistersPlugin) from the declaring config/build file (File:<path>) to each SCOPE.Plugin entity — the composition of a project's build/runtime behaviour. Append-only; files registering no plugins are untouched. |
| Resource extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/detector.go`<br>`internal/engine/plugin_system_edges.go`<br>`internal/engine/plugin_system_edges_test.go` | #3628 area #25: append-only file-keyed detector pass applyPluginSystemEdges (detector.go) emits one synthetic SCOPE.Plugin entity (EntityKindPlugin, ID plugin:<ecosystem>:<name>, Subtype=system) per distinct (ecosystem, plugin name) across: Webpack/Vite/Rollup plugins:[new X()/factory()], Babel/ESLint plugins/extends string arrays, pytest pytest_plugins, setuptools entry_points, Maven <plugin><artifactId>, Gradle plugins{ id }. Ecosystem-scoped ID keeps webpack 'html' distinct from babel 'html'. Honest-partial: literal plugin names only (spreads/variables/non-literal artifactIds yield no node). 14 value-asserting tests. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.plugin-system ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
