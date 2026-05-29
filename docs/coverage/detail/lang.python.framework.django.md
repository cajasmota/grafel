<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django` — Django

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/django_routes.go`<br>`internal/engine/django_urlconf_nested.go`<br>`internal/engine/rules/python/frameworks/django.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/django_admin_routes.go`<br>`internal/engine/django_routes.go` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/django_routes.go`<br>`internal/engine/django_urlconf_nested.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/python/django.go`<br>`internal/mcp/auth_coverage.go` | login_required and permission_required decorators extracted by django.go; auth_coverage.go MCP tool analyses coverage graph-wide |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/django.go` | Django admin registrations (admin.site.register, @admin.register) and form patterns; full form field introspection not yet implemented |
| Request validation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/python/django.go` | Middleware class and process_request/process_response/process_view hooks extracted; MIDDLEWARE setting list analysis not yet implemented |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Interface extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Type alias extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Type extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go`<br>`internal/engine/tests_edges.go`<br>`internal/engine/tests_edges_test.go` | pytest.go extracts test funcs; multi-hop TESTS pass (#2987) links test-client calls through ROUTES_TO to handlers; framework fixture tests in tests_edges_test.go |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/def_use_python.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/template_pattern_python.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
