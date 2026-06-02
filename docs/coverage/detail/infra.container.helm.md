<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.container.helm` — Helm charts

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Containers & Orchestration
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/extractors/yaml/extractor.go`<br>`internal/extractors/yaml/extractor_test.go`<br>`internal/extractors/yaml/helm.go` | — |
| Env resolution | ✅ `full` | `2026-05-31` | — | `internal/extractors/yaml/extractor_test.go`<br>`internal/extractors/yaml/helm.go`<br>`internal/types/kinds.go` | #3552: values data-flow deepened. Template .Values binds re-root through {{- with .Values.x }}/{{- range .Values.y }} scopes (bare .field -> x.field) and capture both operands of | default pipelines. _helpers.tpl named templates source their own BINDS (.Values reads in the define body) and INCLUDES edges, with the passed include arg classified (root_context/dict/list/scoped_context) for define->include arg flow. Parent values.yaml subchart blocks (matched against sibling Chart.yaml dependency name/alias) emit OVERRIDES edges to helm_subchart_values:<sub>:<path> (cross-chart values flow); cross-chart stub->subchart-values linking is DEPLOY-DEFERRED. |
| Resource extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/yaml/extractor.go`<br>`internal/extractors/yaml/extractor_test.go`<br>`internal/extractors/yaml/helm.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.container.helm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
