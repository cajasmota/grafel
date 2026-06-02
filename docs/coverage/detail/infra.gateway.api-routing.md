<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.gateway.api-routing` — API-gateway route topology (application frameworks)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-02` | — | `internal/classifier/classifier.go`<br>`internal/engine/api_gateway_routing_edges.go`<br>`internal/engine/api_gateway_routing_edges_test.go` | #3723 (epic #3628 area #21): application-framework API-gateway route→upstream-service topology, complementing the #3633 deployment_topology cell which owns the reverse-proxy / infra gateways (nginx/Caddy/Kong/Traefik). New LIVE engine pass applyAPIGatewayRoutingEdges (registered in detector.go after applyDeploymentTopologyEdges). Mints a SCOPE.Route node per gateway route and a ROUTES_TO edge to the upstream `service:<name>` SCOPE.Service node — the SAME canonical key the deployment-topology pass uses, so a gateway route to `lb://user-service` and a docker-compose service `user-service` collapse onto one node. Gateways: Spring Cloud Gateway YAML (`spring.cloud.gateway.routes[].uri` lb://USER-SERVICE / http(s):// with Path predicate carried as edge prop) + Java RouteLocatorBuilder DSL (`.route(r -> r.path("/users/**").uri("lb://user-service"))`); Ocelot .NET (`ocelot.json`/`ocelot.<env>.json` Routes[]/ReRoutes[] — ServiceName or DownstreamHostAndPorts[0].Host, UpstreamPathTemplate carried); Express Gateway (`gateway.config.yml` pipelines→proxy policy serviceEndpoint→url host, per bound apiEndpoint); http-proxy-middleware (`createProxyMiddleware({target})` host). ocelot.json basename routed to language=json by the classifier so it reaches the Pass 2.5 detector. Value-asserting tests assert each specific route→service edge (Path=/users/** uri lb://USER-SERVICE → ROUTES_TO service:USER-SERVICE; Ocelot UpstreamPathTemplate /api/orders → service:order-service). Honest-partial: dynamic/templated upstreams (${...}) are omitted, not guessed. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3723 | `internal/engine/api_gateway_routing_edges.go`<br>`internal/engine/api_gateway_routing_edges_test.go` | #3723: mints SCOPE.Route nodes for each gateway route (props: gateway_tool, path/predicate, uri/target/upstream_path_template, variant) and SCOPE.Service nodes (role=backend) for each upstream. Partial because only the request-flow-relevant route fields are extracted (route identity, path predicate, upstream identity), not the full gateway config (filters/policies/rate-limits/CORS/auth-per-route remain unmodelled). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.gateway.api-routing ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
