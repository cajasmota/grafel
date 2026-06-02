<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.container.kubernetes` — Kubernetes manifests

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** Containers & Orchestration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-31` | — | `internal/engine/kubernetes_edges.go`<br>`internal/extractors/yaml/extractor.go` | #3551: cross-resource edges are namespace-scoped (selector/ref matching links only within the same metadata.namespace, default-namespaced when omitted). Added NetworkPolicy spec.podSelector, PodDisruptionBudget spec.selector, and Prometheus-Operator ServiceMonitor/PodMonitor selector edges. |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/yaml/extractor.go` | #3551: CustomResourceDefinition captured as a crd_definition entity (spec.names/group/scope in Properties); known CRD instances (ArgoCD Application/AppProject, Argo Rollouts Rollout, cert-manager Certificate/Issuer, Prometheus ServiceMonitor/PodMonitor) typed meaningfully instead of generic Component; metadata.namespace stamped as k8s_namespace Property. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.container.kubernetes ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
