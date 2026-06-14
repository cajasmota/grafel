<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.deployment.request-topology` — Reverse-proxy / gateway request topology

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-14` | — | `internal/classifier/classifier.go`<br>`internal/engine/deployment_code_edges.go`<br>`internal/engine/deployment_code_edges_test.go`<br>`internal/engine/deployment_topology_edges.go`<br>`internal/engine/deployment_topology_edges_test.go`<br>`internal/types/kinds.go` | #3633 (epic #3625): restored the previously-orphaned deployment_topology enricher as a LIVE engine pass (applyDeploymentTopologyEdges, registered in detector.go after the IaC/serverless passes). Models the proxy/gateway request-flow that was invisible in the graph: nginx `proxy_pass http://<name>` matched to an `upstream <name>` block -> gateway DEPENDS_ON service:<name> (a bare-host proxy_pass -> ROUTES_TO); Caddy `reverse_proxy <host:port>` -> gateway ROUTES_TO service:<host>; docker-compose `<svc> depends_on <dep>` (both short list + long mapping form) -> service:<svc> DEPENDS_ON service:<dep>; Kong declarative service-with-routes -> gateway ROUTES_TO service:<name>; Traefik dynamic router.service -> gateway ROUTES_TO service:<name>. Backend services keyed `service:<name>` so a proxy upstream and a compose service collapse onto one SCOPE.Service node. nginx.conf/Caddyfile/.nginx classified (no extractor needed; classified files still reach the Pass 2.5 detector). K8s Ingress->Service and serverless.yml topology are owned by infra.container.kubernetes / the serverless cell. Value-asserting tests assert each specific edge; honest-partial: dynamic/templated targets ({...}/${...}) are omitted, not guessed. #4983 (Topology Model 2/3 epic #4810): EXTENDED with the explicit infra-to-code DEPLOYS edge (new RelationshipKindDeploys) — the deploy-time mapping the infra-infra (applyKubernetesEdges) and request-flow passes never drew. New LIVE pass applyDeploymentCodeEdges (registered after applyDeploymentTopologyEdges): a Kubernetes workload (Deployment/StatefulSet/DaemonSet/ReplicaSet/Job/CronJob/Pod/Rollout) container image -> DEPLOYS from k8s/<file>#resource/<kind>/<name> to service:<imageRepo>; a docker-compose service first-party image (or a rename over build) -> DEPLOYS from service:<svc> to service:<imageRepo>; a serverless.yml function with a static handler -> DEPLOYS from aws-lambda:<name> to service:<name>. Target keyed service:<name> (the SAME canonical key) so the edge collapses onto the real code/compose service node; every edge carries inferred=true + a match provenance so Model 2/3 styles the deploy-time mapping distinctly from identity/usage cross-links. Value-asserting tests assert each specific edge. Honest-partial: public base/sidecar images (postgres/redis/nginx/node/...) and templated/variable refs (${...}) are filtered, not guessed; a build-only same-name compose service is left to the identity cross-link (no self-loop); ECS/App Service/CloudRun image-code joins are deploy-deferred follow-ups. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/deployment_topology_edges.go`<br>`internal/engine/deployment_topology_edges_test.go` | #3633: mints SCOPE.Service nodes for the proxy/gateway layer (one per config, role=gateway) and for every backend it routes to (role=backend) plus docker-compose services (role=compose_service). Partial because only the request-flow-relevant fields are extracted (service identity + dependency targets), not full per-resource config (ports/volumes/networks remain with the docker ruleset / yaml extractor). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.deployment.request-topology ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
