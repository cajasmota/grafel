<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django-drf` — Django REST Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 38

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/django_drf_actions.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/drf_serializer_fields.go` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/python_relational_bundle_test.go`<br>`internal/extractors/python/router_register.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | 2816 | `internal/extractors/python/config_module.go`<br>`internal/extractors/python/django_drf_permissions.go`<br>`internal/mcp/auth_coverage.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/extractors/python/drf_serializer_fields.go`<br>`internal/extractors/python/drf_serializer_fields_test.go` | — |
| Request validation | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/extractors/python/django_drf_permissions.go`<br>`internal/extractors/python/django_drf_permissions_test.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ⚠️ `partial` | `2026-05-29` | — | `internal/engine/django_imports_rewrite.go` | Django middleware import rewriting provides partial coverage; DRF-specific middleware detection not yet implemented |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/pytest.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/substrate/def_use_python.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | — | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | — | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | — | `internal/substrate/template_pattern_python.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Framework-specific

### Django Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Admin detection | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| Signal handler attribution | ⚠️ `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2739) | `internal/engine/django_signal_pubsub_edges.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django-drf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
