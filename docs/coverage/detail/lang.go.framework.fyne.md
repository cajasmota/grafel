<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.fyne` — Fyne (desktop GUI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 14

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | — `not_applicable` | — | — | — | — |
| Main renderer split | — `not_applicable` | — | — | — | — |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3218) | `internal/custom/golang/fyne.go`<br>`internal/custom/golang/fyne_gomobile_test.go` | — |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-29` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/golang/config_consumer.go`<br>`internal/extractors/golang/config_consumer_test.go` | os.Getenv/viper.GetString -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-29` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.fyne ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
