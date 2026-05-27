<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.wpf` — WPF

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 6

## Capabilities


### Process

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `ipc_extraction` | ❌ `missing` | — | — | — | — | — |
| `main_renderer_split` | ❌ `missing` | — | — | — | — | — |

### Native

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `native_module_imports` | ❌ `missing` | — | — | — | — | — |

### Updates

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `confidence_overlay` | ✅ `full` | `2026-05-28` | — | — | `internal/types/confidence.go`<br>`internal/graph/graph.go`<br>`internal/mcp/tools.go` | — |
| `dead_code_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_csharp.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go`<br>`internal/mcp/dead_code.go` | — |
| `reachability_analysis` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_csharp.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.wpf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
