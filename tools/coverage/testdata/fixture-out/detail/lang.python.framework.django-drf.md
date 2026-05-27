<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django-drf` — Django REST Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ⚠️ `partial` | `2026-05-27` | — | [link](https://github.com/cajasmota/archigraph/issues/1942) | `internal/engine/django_drf_actions.go` |
| `endpoint_synthesis` | ✅ `full` | `2026-05-27` | — | — | `internal/engine/django_drf_actions.go`<br>`internal/engine/http_endpoint_synthesis.go` |
| `handler_attribution` | ✅ `full` | `2026-05-27` | — | — | `internal/engine/django_drf_actions.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django-drf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
