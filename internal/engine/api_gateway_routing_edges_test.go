// Tests for the API-gateway route topology pass — #3723 (epic #3628 area #21).
//
// Value-asserting: every test asserts a SPECIFIC ROUTES_TO edge from a concrete
// gateway-route node to a canonically-keyed `service:<name>` SCOPE.Service node
// — never `len > 0`.
//
//   - Spring Cloud Gateway YAML  Path=/users/** uri lb://USER-SERVICE
//     → route ROUTES_TO service:USER-SERVICE
//   - Spring Cloud Gateway Java  .path("/orders/**").uri("lb://order-service")
//     → route ROUTES_TO service:order-service
//   - Ocelot UpstreamPathTemplate /api/orders → ROUTES_TO service:order-service
//   - Express Gateway pipeline serviceEndpoint url → ROUTES_TO service host
//   - http-proxy-middleware target → ROUTES_TO service host
//
// plus negative / no-op guards (a non-gateway yaml emits no route).
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func runAPIGwDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyAPIGatewayRoutingEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// apiGwEdgeTo returns the ROUTES_TO edge landing on serviceID, or nil.
func apiGwEdgeTo(rels []types.RelationshipRecord, serviceID string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == apiGwRoutesTo && rels[i].ToID == serviceID {
			return &rels[i]
		}
	}
	return nil
}

func apiGwEntity(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id {
			return &ents[i]
		}
	}
	return nil
}

// apiGwHasRoutesTo asserts a ROUTES_TO edge from any route to serviceID and that
// the FROM node is a SCOPE.Route. Returns the edge for further property checks.
func apiGwHasRoutesTo(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord, serviceID string) *types.RelationshipRecord {
	t.Helper()
	e := apiGwEdgeTo(rels, serviceID)
	if e == nil {
		t.Fatalf("expected a ROUTES_TO edge to %q; got %+v", serviceID, rels)
	}
	from := apiGwEntity(ents, e.FromID)
	if from == nil {
		t.Fatalf("ROUTES_TO source node %q not minted", e.FromID)
	}
	if from.Kind != apiGwRouteKind {
		t.Fatalf("route node %q kind = %q, want %q", e.FromID, from.Kind, apiGwRouteKind)
	}
	svc := apiGwEntity(ents, serviceID)
	if svc == nil {
		t.Fatalf("backend service node %q not minted", serviceID)
	}
	if svc.Kind != apiGwServiceKind {
		t.Fatalf("service node %q kind = %q, want %q", serviceID, svc.Kind, apiGwServiceKind)
	}
	return e
}

// TestSpringCloudGatewayYAML_RoutesTo is the headline assertion: a
// spring.cloud.gateway.routes entry with uri lb://USER-SERVICE and a
// Path=/users/** predicate yields route ROUTES_TO service:USER-SERVICE, with the
// path predicate carried on the edge.
func TestSpringCloudGatewayYAML_RoutesTo(t *testing.T) {
	src := `
spring:
  cloud:
    gateway:
      routes:
        - id: users-route
          uri: lb://USER-SERVICE
          predicates:
            - Path=/users/**
        - id: orders-route
          uri: lb://ORDER-SERVICE
          predicates:
            - Path=/orders/**
`
	ents, rels := runAPIGwDetect(t, "yaml", "application.yml", src)

	const userSvc = "service:USER-SERVICE"
	e := apiGwHasRoutesTo(t, ents, rels, userSvc)
	if e.Properties["predicate"] != "/users/**" {
		t.Fatalf("edge predicate = %q, want /users/**", e.Properties["predicate"])
	}
	// The route node carries the path + uri.
	route := apiGwEntity(ents, e.FromID)
	if route.Properties["path"] != "/users/**" {
		t.Fatalf("route path prop = %q, want /users/**", route.Properties["path"])
	}
	if route.Properties["uri"] != "lb://USER-SERVICE" {
		t.Fatalf("route uri prop = %q", route.Properties["uri"])
	}
	// Second route resolves independently.
	apiGwHasRoutesTo(t, ents, rels, "service:ORDER-SERVICE")
}

// TestSpringCloudGatewayYAML_DynamicURI_Omitted: a templated uri (${...}) is
// honestly dropped, not guessed.
func TestSpringCloudGatewayYAML_DynamicURI_Omitted(t *testing.T) {
	src := `
spring:
  cloud:
    gateway:
      routes:
        - id: dyn
          uri: ${UPSTREAM_URI}
          predicates:
            - Path=/x/**
`
	_, rels := runAPIGwDetect(t, "yaml", "application.yml", src)
	for _, r := range rels {
		if r.Kind == apiGwRoutesTo {
			t.Fatalf("dynamic ${...} uri must NOT emit a ROUTES_TO edge; got %+v", r)
		}
	}
}

// TestSpringCloudGatewayJava_RoutesTo: the RouteLocatorBuilder DSL form.
func TestSpringCloudGatewayJava_RoutesTo(t *testing.T) {
	src := `
package com.example.gw;
import org.springframework.cloud.gateway.route.RouteLocator;
import org.springframework.cloud.gateway.route.builder.RouteLocatorBuilder;

public class GatewayConfig {
    public RouteLocator routes(RouteLocatorBuilder builder) {
        return builder.routes()
            .route("orders", r -> r.path("/orders/**").uri("lb://order-service"))
            .route(r -> r.path("/users/**").uri("lb://user-service"))
            .build();
    }
}
`
	ents, rels := runAPIGwDetect(t, "java", "GatewayConfig.java", src)
	e := apiGwHasRoutesTo(t, ents, rels, "service:order-service")
	if e.Properties["predicate"] != "/orders/**" {
		t.Fatalf("java route predicate = %q, want /orders/**", e.Properties["predicate"])
	}
	apiGwHasRoutesTo(t, ents, rels, "service:user-service")
}

// TestOcelot_UpstreamPathTemplate_RoutesTo: Ocelot ocelot.json
// UpstreamPathTemplate /api/orders → downstream order-service.
func TestOcelot_UpstreamPathTemplate_RoutesTo(t *testing.T) {
	src := `
{
  "Routes": [
    {
      "UpstreamPathTemplate": "/api/orders",
      "DownstreamPathTemplate": "/orders",
      "DownstreamHostAndPorts": [ { "Host": "order-service", "Port": 80 } ]
    },
    {
      "UpstreamPathTemplate": "/api/users",
      "DownstreamPathTemplate": "/users",
      "ServiceName": "user-service"
    }
  ]
}
`
	ents, rels := runAPIGwDetect(t, "json", "ocelot.json", src)
	e := apiGwHasRoutesTo(t, ents, rels, "service:order-service")
	route := apiGwEntity(ents, e.FromID)
	if route.Properties["upstream_path_template"] != "/api/orders" {
		t.Fatalf("ocelot route upstream = %q, want /api/orders", route.Properties["upstream_path_template"])
	}
	if route.Properties["downstream_path_template"] != "/orders" {
		t.Fatalf("ocelot downstream = %q, want /orders", route.Properties["downstream_path_template"])
	}
	// ServiceName form also resolves.
	apiGwHasRoutesTo(t, ents, rels, "service:user-service")
}

// TestOcelot_ReRoutes_LegacyKey: pre-v16 Ocelot used "ReRoutes".
func TestOcelot_ReRoutes_LegacyKey(t *testing.T) {
	src := `
{
  "ReRoutes": [
    { "UpstreamPathTemplate": "/legacy", "ServiceName": "legacy-svc" }
  ]
}
`
	ents, rels := runAPIGwDetect(t, "json", "ocelot.json", src)
	apiGwHasRoutesTo(t, ents, rels, "service:legacy-svc")
}

// TestExpressGateway_RoutesTo: gateway.config.yml pipeline whose proxy policy
// targets a serviceEndpoint resolves the apiEndpoint route → service host.
func TestExpressGateway_RoutesTo(t *testing.T) {
	src := `
apiEndpoints:
  users:
    host: '*'
    paths: '/users/*'
serviceEndpoints:
  usersService:
    url: 'http://user-service:8080'
pipelines:
  usersPipeline:
    apiEndpoints:
      - users
    policies:
      - proxy:
          action:
            serviceEndpoint: usersService
`
	ents, rels := runAPIGwDetect(t, "yaml", "gateway.config.yml", src)
	e := apiGwHasRoutesTo(t, ents, rels, "service:user-service")
	route := apiGwEntity(ents, e.FromID)
	if route.Properties["api_endpoint"] != "users" {
		t.Fatalf("express route api_endpoint = %q, want users", route.Properties["api_endpoint"])
	}
}

// TestHTTPProxyMiddleware_RoutesTo: createProxyMiddleware({ target }) → service.
func TestHTTPProxyMiddleware_RoutesTo(t *testing.T) {
	src := `
const { createProxyMiddleware } = require('http-proxy-middleware');
app.use('/api', createProxyMiddleware({
  target: 'http://catalog-service:3000',
  changeOrigin: true,
  pathRewrite: { '^/api': '' },
}));
`
	ents, rels := runAPIGwDetect(t, "javascript", "proxy.js", src)
	e := apiGwHasRoutesTo(t, ents, rels, "service:catalog-service")
	route := apiGwEntity(ents, e.FromID)
	if route.Properties["path_rewrite"] != "true" {
		t.Fatalf("expected path_rewrite=true prop; got %q", route.Properties["path_rewrite"])
	}
	if route.Properties["target"] != "http://catalog-service:3000" {
		t.Fatalf("target prop = %q", route.Properties["target"])
	}
}

// TestHTTPProxyMiddleware_DynamicTarget_Omitted: a `${...}` template-literal
// target is honestly dropped.
func TestHTTPProxyMiddleware_DynamicTarget_Omitted(t *testing.T) {
	src := "const m = createProxyMiddleware({ target: `http://${HOST}:3000` });"
	_, rels := runAPIGwDetect(t, "javascript", "proxy.js", src)
	for _, r := range rels {
		if r.Kind == apiGwRoutesTo {
			t.Fatalf("dynamic ${...} target must NOT emit a ROUTES_TO edge; got %+v", r)
		}
	}
}

// TestNegative_PlainYAML_NoRoute: an ordinary application.yml that is NOT a
// gateway config emits no route entities or edges.
func TestNegative_PlainYAML_NoRoute(t *testing.T) {
	src := `
server:
  port: 8080
spring:
  application:
    name: my-service
  datasource:
    url: jdbc:postgresql://db:5432/app
`
	ents, rels := runAPIGwDetect(t, "yaml", "application.yml", src)
	for _, e := range ents {
		if e.Kind == apiGwRouteKind {
			t.Fatalf("non-gateway yaml must NOT mint a route node; got %+v", e)
		}
	}
	for _, r := range rels {
		if r.Kind == apiGwRoutesTo {
			t.Fatalf("non-gateway yaml must NOT emit ROUTES_TO; got %+v", r)
		}
	}
}

// TestNegative_EmptyContent_NoOp: nil content is a fast no-op.
func TestNegative_EmptyContent_NoOp(t *testing.T) {
	res := applyAPIGatewayRoutingEdges(DetectorPassArgs{Lang: "yaml", Path: "ocelot.json", Content: nil})
	if len(res.Entities) != 0 || len(res.Relationships) != 0 {
		t.Fatalf("empty content should be a no-op; got %d ents %d rels", len(res.Entities), len(res.Relationships))
	}
}

// TestServiceID_CollapsesWithDeploymentTopology proves the canonical key match:
// a gateway route to lb://user-service and the deployment-topology service node
// for the same name share one ID, so the edge lands on the known node.
func TestServiceID_CollapsesWithDeploymentTopology(t *testing.T) {
	if apiGwServiceID("user-service") != depTopoServiceID("user-service") {
		t.Fatalf("api-gateway service key %q must equal deployment-topology key %q",
			apiGwServiceID("user-service"), depTopoServiceID("user-service"))
	}
}
