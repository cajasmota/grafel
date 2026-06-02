// Tests for the deployment / request-flow topology pass — #3633 (epic #3625).
//
// Value-asserting: every test asserts a SPECIFIC graph edge between two
// concrete, canonically-keyed nodes — never `len > 0`.
//
//   - nginx `proxy_pass http://api_backend` + `upstream api_backend`
//     → gateway DEPENDS_ON service:api_backend
//   - docker-compose `web depends_on db` → service:web DEPENDS_ON service:db
//   - Caddy `reverse_proxy backend:9000` → gateway ROUTES_TO service:backend
//   - Kong service with a route → gateway ROUTES_TO the Kong service
//   - Traefik router→service → gateway ROUTES_TO the service
//
// plus negative / no-op guards.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func runDepTopoDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyDeploymentTopologyEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func depTopoHasEdge(rels []types.RelationshipRecord, kind, fromID, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

func depTopoEntity(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id {
			return &ents[i]
		}
	}
	return nil
}

// TestNginx_UpstreamProxyPass_DependsOn is the headline assertion: an nginx
// config whose `proxy_pass` targets a declared `upstream` yields a DEPENDS_ON
// edge from the gateway to the canonical backend service node.
func TestNginx_UpstreamProxyPass_DependsOn(t *testing.T) {
	src := `
upstream api_backend {
    server api1:8080;
    server api2:8080;
}
server {
    listen 80;
    location /api/ {
        proxy_pass http://api_backend;
    }
}
`
	ents, rels := runDepTopoDetect(t, "nginx", "deploy/nginx.conf", src)

	const gw = "service:nginx:nginx.conf"
	const backend = "service:api_backend"

	if depTopoEntity(ents, gw) == nil {
		t.Fatalf("expected gateway node %q to be minted; got %+v", gw, ents)
	}
	be := depTopoEntity(ents, backend)
	if be == nil {
		t.Fatalf("expected backend service node %q; got %+v", backend, ents)
	}
	if be.Kind != depTopoServiceKind {
		t.Fatalf("backend node kind = %q, want %q", be.Kind, depTopoServiceKind)
	}
	if be.Name != "api_backend" {
		t.Fatalf("backend node name = %q, want api_backend", be.Name)
	}

	if !depTopoHasEdge(rels, depTopoDependsOn, gw, backend) {
		t.Fatalf("expected DEPENDS_ON %s -> %s (nginx upstream proxy_pass); got %+v", gw, backend, rels)
	}
}

// TestNginx_ProxyPassBareHost_RoutesTo: a proxy_pass to a host that is NOT a
// declared upstream records a ROUTES_TO to that host's service (not DEPENDS_ON).
func TestNginx_ProxyPassBareHost_RoutesTo(t *testing.T) {
	src := `
server {
    location / {
        proxy_pass http://authservice;
    }
}
`
	_, rels := runDepTopoDetect(t, "nginx", "nginx.conf", src)
	const gw = "service:nginx:nginx.conf"
	const backend = "service:authservice"
	if !depTopoHasEdge(rels, depTopoRoutesTo, gw, backend) {
		t.Fatalf("expected ROUTES_TO %s -> %s; got %+v", gw, backend, rels)
	}
	if depTopoHasEdge(rels, depTopoDependsOn, gw, backend) {
		t.Fatalf("bare-host proxy_pass must NOT emit DEPENDS_ON (no upstream block)")
	}
}

// TestCompose_DependsOn is the canonical compose assertion: `web depends_on db`
// → service:web DEPENDS_ON service:db, with both nodes minted as SCOPE.Service.
func TestCompose_DependsOn(t *testing.T) {
	src := `
version: "3.9"
services:
  web:
    image: nginx
    depends_on:
      - db
      - cache
  db:
    image: postgres
  cache:
    image: redis
`
	ents, rels := runDepTopoDetect(t, "yaml", "docker-compose.yml", src)

	for _, id := range []string{"service:web", "service:db", "service:cache"} {
		e := depTopoEntity(ents, id)
		if e == nil {
			t.Fatalf("expected compose service node %q; got %+v", id, ents)
		}
		if e.Kind != depTopoServiceKind {
			t.Fatalf("node %q kind = %q, want SCOPE.Service", id, e.Kind)
		}
	}
	if !depTopoHasEdge(rels, depTopoDependsOn, "service:web", "service:db") {
		t.Fatalf("expected DEPENDS_ON service:web -> service:db; got %+v", rels)
	}
	if !depTopoHasEdge(rels, depTopoDependsOn, "service:web", "service:cache") {
		t.Fatalf("expected DEPENDS_ON service:web -> service:cache; got %+v", rels)
	}
}

// TestCompose_DependsOn_LongForm: the mapping long form
// `depends_on: {db: {condition: service_healthy}}` is also resolved.
func TestCompose_DependsOn_LongForm(t *testing.T) {
	src := `
services:
  api:
    image: myapp
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres
`
	_, rels := runDepTopoDetect(t, "yaml", "docker-compose.yaml", src)
	if !depTopoHasEdge(rels, depTopoDependsOn, "service:api", "service:db") {
		t.Fatalf("expected DEPENDS_ON service:api -> service:db (long form); got %+v", rels)
	}
}

// TestCaddy_ReverseProxy_RoutesTo: `reverse_proxy backend:9000` → gateway
// ROUTES_TO service:backend (port stripped, host-keyed).
func TestCaddy_ReverseProxy_RoutesTo(t *testing.T) {
	src := `
example.com {
    reverse_proxy backend:9000
}
`
	ents, rels := runDepTopoDetect(t, "caddy", "Caddyfile", src)
	const gw = "service:caddy:caddyfile"
	const backend = "service:backend"
	if depTopoEntity(ents, backend) == nil {
		t.Fatalf("expected backend node %q; got %+v", backend, ents)
	}
	if !depTopoHasEdge(rels, depTopoRoutesTo, gw, backend) {
		t.Fatalf("expected ROUTES_TO %s -> %s; got %+v", gw, backend, rels)
	}
}

// TestCaddy_DynamicTarget_Skipped: a reverse_proxy to a placeholder target
// emits NO backend node / edge (honest-partial on dynamic config).
func TestCaddy_DynamicTarget_Skipped(t *testing.T) {
	src := `
example.com {
    reverse_proxy {http.reverse_proxy.upstream.hostport}
}
`
	ents, rels := runDepTopoDetect(t, "caddy", "Caddyfile", src)
	for _, e := range ents {
		if e.Properties["deployment_role"] == "backend" {
			t.Fatalf("dynamic reverse_proxy target must not mint a backend node, got %+v", e)
		}
	}
	for _, r := range rels {
		if r.Kind == depTopoRoutesTo && r.Properties["flow"] == "caddy_reverse_proxy" {
			t.Fatalf("dynamic reverse_proxy target must not emit a ROUTES_TO edge, got %+v", r)
		}
	}
}

// TestKong_Route_RoutesTo: a declarative Kong service with a route → gateway
// ROUTES_TO that service; a service with no routes is not surfaced.
func TestKong_Route_RoutesTo(t *testing.T) {
	src := `
_format_version: "3.0"
services:
  - name: orders-service
    url: http://orders:8080
    routes:
      - name: orders-route
        paths:
          - /orders
  - name: dangling-service
    url: http://nowhere:8080
`
	_, rels := runDepTopoDetect(t, "yaml", "kong.yml", src)
	const gw = "service:kong:kong.yml"
	if !depTopoHasEdge(rels, depTopoRoutesTo, gw, "service:orders-service") {
		t.Fatalf("expected ROUTES_TO %s -> service:orders-service; got %+v", gw, rels)
	}
	if depTopoHasEdge(rels, depTopoRoutesTo, gw, "service:dangling-service") {
		t.Fatalf("a Kong service with no routes must NOT be routed to")
	}
}

// TestTraefik_Router_RoutesTo: a Traefik dynamic-config router→service →
// gateway ROUTES_TO the service.
func TestTraefik_Router_RoutesTo(t *testing.T) {
	src := `
http:
  routers:
    web-router:
      rule: "Host(` + "`example.com`" + `)"
      service: web-service
  services:
    web-service:
      loadBalancer:
        servers:
          - url: "http://10.0.0.1:80"
`
	_, rels := runDepTopoDetect(t, "yaml", "traefik-dynamic.yml", src)
	const gw = "service:traefik:traefik-dynamic.yml"
	if !depTopoHasEdge(rels, depTopoRoutesTo, gw, "service:web-service") {
		t.Fatalf("expected ROUTES_TO %s -> service:web-service; got %+v", gw, rels)
	}
}

// TestNoOp_UnrelatedYAML: a plain application YAML produces no deployment-topology
// entities/edges (negative guard against false firing).
func TestNoOp_UnrelatedYAML(t *testing.T) {
	src := `
name: my-app
config:
  debug: true
  retries: 3
`
	ents, rels := runDepTopoDetect(t, "yaml", "app-config.yml", src)
	for _, e := range ents {
		if e.Properties["synthesis"] == "deployment_topology" {
			t.Fatalf("unrelated YAML must not mint deployment_topology nodes, got %+v", e)
		}
	}
	for _, r := range rels {
		if r.Properties["synthesis"] == "deployment_topology" {
			t.Fatalf("unrelated YAML must not emit deployment_topology edges, got %+v", r)
		}
	}
}

// TestNoOp_EmptyContent: empty content is a clean no-op.
func TestNoOp_EmptyContent(t *testing.T) {
	res := applyDeploymentTopologyEdges(DetectorPassArgs{Lang: "nginx", Path: "nginx.conf", Content: nil})
	if len(res.Entities) != 0 || len(res.Relationships) != 0 {
		t.Fatalf("empty content must be a no-op; got %d ents / %d rels", len(res.Entities), len(res.Relationships))
	}
}
