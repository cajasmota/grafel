# Kubernetes manifests convention

Required reading: `_graph-searchability.md`.

Applies to repos primarily containing Kubernetes manifests (raw YAML, Kustomize, or Helm charts). For Terraform-defined infra, see `infra-terraform.md`.

## Public surface

1. **Workloads** — `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob` resources. Each is a unit of public behavior.
2. **Services** — `Service`, `Ingress`, `Gateway`/`HTTPRoute` (Gateway API). These define how the workload is reachable.
3. **ConfigMaps and Secrets** — declared values, often consumed by reference.
4. **CustomResourceDefinitions** if the repo defines or consumes operators.
5. **Helm chart values schema** if the repo is a chart.

## Module shape

For Kustomize:

```
base/
  <component>/
    deployment.yaml
    service.yaml
    kustomization.yaml
overlays/
  prod/
    kustomization.yaml
    patches/
  staging/
```

For Helm:

```
charts/<name>/
  Chart.yaml
  values.yaml
  templates/
    deployment.yaml
    ...
```

A community typically maps to a base component or a chart.

## Entry points (Pass 3)

- Each `overlays/<env>/kustomization.yaml`, or each chart's release config.
- `argocd` / `flux` Application/Kustomization manifests if GitOps is in use.

## Dynamic edges (Pass 4)

- **Service selectors** — `Service.spec.selector` matches on labels; the matched workloads are silent. Encode `Service` → `Deployment` matches in `flows.md`.
- **ConfigMap / Secret mounts** — a workload references a config by name. Document the binding.
- **Service-account → role bindings** — `RoleBinding` / `ClusterRoleBinding` couple a workload identity to permissions.
- **NetworkPolicy** — declares which workloads can talk to which. Belongs in `cross-cutting/auth.md`.
- **Ingress / HTTPRoute** — host/path → service mappings; same role as routes in a web framework.

## Deployment signals (Pass 5)

- CI: the command that produces the manifests (`kustomize build`, `helm template`) and what applies them (kubectl, GitOps controller).
- Image tags: where they come from (a sibling repo's CI usually).
- Environments: which overlay/values file targets which cluster.

## Manifest files

For Helm: `Chart.yaml`, `Chart.lock`, `values.yaml`. For Kustomize: `kustomization.yaml`. For raw manifests, none — list the YAML files in `reference/scripts.md`.

## Cross-cutting pitfalls

- **Resource limits** — missing `resources.requests`/`limits` is a common source of OOMKills and noisy-neighbor issues.
- **Probes** — `livenessProbe` vs `readinessProbe` semantics. Misconfigured liveness causes restart storms.
- **`imagePullPolicy: Always` with mutable tags** vs immutable tags — describe the policy.

## Cross-repo signals

The image tag in a `Deployment` is the join key to whichever code repo built it. When `grafel_cross_links(action=list)` proposes an edge from a `Deployment` to a `cmd/<name>/main.go` in a sibling repo, accept if the image name matches the build artifact name. Helm subchart dependencies (`Chart.yaml` `dependencies`) are also cross-repo edges, usually to a separate chart repo.
