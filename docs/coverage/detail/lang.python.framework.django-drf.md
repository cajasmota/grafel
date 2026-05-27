<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django-drf` — Django REST Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/django_drf_actions.go` | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/django_drf_actions.go` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/drf_serializer_fields.go` | — |
| `middleware_coverage` | ❌ `missing` | — | — | — | — | — |

## Framework-specific

### Django Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `admin_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| `signal_handler_attribution` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | `internal/engine/django_signal_pubsub_edges.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django-drf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
