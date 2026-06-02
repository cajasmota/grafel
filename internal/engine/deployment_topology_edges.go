// Deployment / request-flow topology synthesis — #3633 (epic #3625).
//
// The proxy / gateway / container-orchestration layer that sits in FRONT of an
// application's services was, until this pass, entirely invisible in the graph.
// A reverse proxy (nginx, Caddy) that fans requests out to a set of upstream
// services, a docker-compose stack whose `web` waits on `db`, an API gateway
// (Kong, Traefik) whose routes target named services — none of those edges were
// recorded. The k8s Ingress→Service and serverless.yml topology are handled by
// their own dedicated passes (kubernetes_edges.go, serverless_framework_edges.go);
// THIS pass covers the remaining, previously-orphaned infra sources whose parser
// lived in internal/enrichers/deployment_topology_extractor.go but was imported
// by zero production code.
//
// It mints first-class graph nodes/edges modelling the request flow:
//
//	gateway / proxy  -- DEPENDS_ON / ROUTES_TO -->  backend SCOPE.Service
//	compose service  -- DEPENDS_ON              -->  compose SCOPE.Service
//
// Canonical IDs make the topology collapse across files: a backend service is
// keyed `service:<name>`, so an nginx `upstream api_backend` target and a
// docker-compose service `api_backend` resolve to the SAME node, and the proxy
// edge lands on the service the rest of the graph already knows.
//
// # Sources
//
//   - nginx (nginx.conf, *.nginx): `upstream <name> { ... }` blocks + a
//     `proxy_pass http://<name>` whose host is an upstream name → the gateway
//     DEPENDS_ON that backend service. A `proxy_pass` to a bare host:port that
//     is NOT a declared upstream is recorded as a ROUTES_TO to a `service:<host>`
//     node (the external/known target).
//   - Caddy (Caddyfile): `reverse_proxy <target>` → gateway ROUTES_TO the target
//     host's service.
//   - docker-compose (docker-compose.yml/.yaml, compose.yml/.yaml,
//     docker-compose.<env>.yml): each top-level `services:` key is a
//     SCOPE.Service; a service's `depends_on:` entries become DEPENDS_ON edges
//     between the two service nodes.
//   - Kong (kong.yml/.yaml declarative config): `services:` with `routes:` →
//     gateway ROUTES_TO the Kong service.
//   - Traefik (traefik dynamic config yaml): http.routers[].service +
//     http.services[] → gateway ROUTES_TO the named service.
//
// # Scope guard
//
// Append-only: this pass never modifies or removes existing entities or edges,
// so it cannot regress the surrounding pipeline's bug-rate. It fires only for a
// file whose basename/extension/content the gate below recognises as one of the
// infra sources above; every other file is a fast no-op. Where the upstream /
// route target is dynamic or templated (a variable, an env interpolation) the
// edge is honestly omitted rather than emitting a garbage node.
//
// Closes #3633. Epic #3625.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

const (
	// depTopoServiceKind is the canonical entity kind for every node this pass
	// mints (both the proxy/gateway layer and the backend services it routes to).
	depTopoServiceKind = string(types.EntityKindService)
	// depTopoDependsOn / depTopoRoutesTo are the two request-flow edge kinds.
	depTopoDependsOn = string(types.RelationshipKindDependsOn)
	depTopoRoutesTo  = string(types.RelationshipKindRoutesTo)
)

// depTopoServiceID returns the canonical, file-independent ID for a backend
// service so the same logical service collapses across infra files: an nginx
// upstream `api` and a docker-compose service `api` both key `service:api`.
func depTopoServiceID(name string) string {
	return "service:" + name
}

// depTopoComposeOverrideRe matches docker-compose.<env>.yml override files.
var depTopoComposeOverrideRe = regexp.MustCompile(`(?i)^docker-compose\.[a-z0-9_-]+\.ya?ml$`)

// nginx parsing.
var (
	nginxUpstreamRe  = regexp.MustCompile(`(?m)^\s*upstream\s+([a-zA-Z0-9_.-]+)\s*\{`)
	nginxProxyPassRe = regexp.MustCompile(`(?m)proxy_pass\s+https?://([a-zA-Z0-9_.-]+)`)
)

// caddy parsing: `reverse_proxy <flags> <target> [<target>...]`. We capture the
// first host:port or host token after the directive.
var caddyReverseProxyRe = regexp.MustCompile(`(?m)^\s*reverse_proxy\s+(.+)$`)

// applyDeploymentTopologyEdges is the per-file entry point, registered in
// detector.go after the other IaC/topology passes. Append-only.
func applyDeploymentTopologyEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(args.Content)
	base := strings.ToLower(filepath.Base(args.Path))

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}
	emitService := func(name, role, tool string) string {
		if name == "" {
			return ""
		}
		id := depTopoServiceID(name)
		if !seenEnt[id] {
			seenEnt[id] = true
			entities = append(entities, types.EntityRecord{
				ID:            id,
				Name:          name,
				QualifiedName: id,
				Kind:          depTopoServiceKind,
				SourceFile:    args.Path,
				Language:      args.Lang,
				Properties: map[string]string{
					"deployment_role": role, // "gateway" | "backend" | "compose_service"
					"iac_tool":        tool,
					"synthesis":       "deployment_topology",
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}
	emitEdge := func(fromID, toID, kind, flow string) {
		if fromID == "" || toID == "" || fromID == toID {
			return
		}
		key := kind + "|" + fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   kind,
			Properties: map[string]string{
				"flow":      flow,
				"synthesis": "deployment_topology",
			},
		})
	}

	switch {
	case args.Lang == "nginx" || base == "nginx.conf" || strings.HasSuffix(base, ".nginx"):
		applyNginxTopology(src, base, emitService, emitEdge)
	case args.Lang == "caddy" || base == "caddyfile":
		applyCaddyTopology(src, base, emitService, emitEdge)
	case base == "docker-compose.yml" || base == "docker-compose.yaml" ||
		base == "compose.yml" || base == "compose.yaml" ||
		depTopoComposeOverrideRe.MatchString(base):
		applyComposeTopology(src, emitService, emitEdge)
	case base == "kong.yml" || base == "kong.yaml":
		applyKongTopology(src, base, emitService, emitEdge)
	case args.Lang == "yaml" && depTopoIsTraefikDynamic(src):
		applyTraefikTopology(src, base, emitService, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// gatewayName builds a stable, human-readable name for the proxy/gateway node
// of a given tool + file (e.g. "nginx:nginx.conf"). The basename keeps two
// distinct configs in the same repo from collapsing onto one gateway.
func gatewayName(tool, base string) string { return tool + ":" + base }

// applyNginxTopology emits a gateway node and a DEPENDS_ON edge to every backend
// service named by an `upstream` block that a `proxy_pass` targets. A proxy_pass
// to a host that is NOT a declared upstream is recorded as a ROUTES_TO to a
// service node named by that host (an external/leaf backend).
func applyNginxTopology(src, base string, emitService func(name, role, tool string) string, emitEdge func(fromID, toID, kind, flow string)) {
	upstreams := map[string]bool{}
	for _, m := range nginxUpstreamRe.FindAllStringSubmatch(src, -1) {
		upstreams[m[1]] = true
	}
	gw := emitService(gatewayName("nginx", base), "gateway", "nginx")

	for _, m := range nginxProxyPassRe.FindAllStringSubmatch(src, -1) {
		host := m[1]
		if upstreams[host] {
			// proxy_pass http://api_backend + upstream api_backend → the gateway
			// DEPENDS_ON the backend service api_backend.
			to := emitService(host, "backend", "nginx")
			emitEdge(gw, to, depTopoDependsOn, "nginx_proxy_pass_upstream")
		} else {
			// proxy_pass to a bare host (not a local upstream) — a known leaf
			// backend the gateway routes to.
			to := emitService(host, "backend", "nginx")
			emitEdge(gw, to, depTopoRoutesTo, "nginx_proxy_pass")
		}
	}
}

// applyCaddyTopology emits a gateway node and a ROUTES_TO edge to each
// reverse_proxy target host. Dynamic targets (placeholders like {http.…} or
// env interpolations) are skipped.
func applyCaddyTopology(src, base string, emitService func(name, role, tool string) string, emitEdge func(fromID, toID, kind, flow string)) {
	gw := emitService(gatewayName("caddy", base), "gateway", "caddy")
	for _, m := range caddyReverseProxyRe.FindAllStringSubmatch(src, -1) {
		for _, tok := range strings.Fields(m[1]) {
			host := caddyUpstreamHost(tok)
			if host == "" {
				continue
			}
			to := emitService(host, "backend", "caddy")
			emitEdge(gw, to, depTopoRoutesTo, "caddy_reverse_proxy")
		}
	}
}

// caddyUpstreamHost extracts a backend host name from one reverse_proxy token,
// returning "" for non-target tokens (flags, path matchers, placeholders).
func caddyUpstreamHost(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	// Flags / path matchers / block openers are not upstreams.
	if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "/") ||
		strings.HasPrefix(tok, "{") || tok == "{" || tok == "}" {
		return ""
	}
	// Strip scheme.
	if i := strings.Index(tok, "://"); i >= 0 {
		tok = tok[i+3:]
	}
	// Skip dynamic placeholders (Caddy {…} or env ${…}).
	if strings.Contains(tok, "{") || strings.Contains(tok, "$") {
		return ""
	}
	// Strip :port — the service node is host-keyed.
	if i := strings.LastIndex(tok, ":"); i >= 0 {
		// Only treat the trailing segment as a port if it's numeric.
		if isAllDigits(tok[i+1:]) {
			tok = tok[:i]
		}
	}
	if tok == "" || tok == "localhost" {
		return ""
	}
	return tok
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// composeFile is the minimal docker-compose shape this pass decodes.
type composeFile struct {
	Services map[string]struct {
		DependsOn yaml.Node `yaml:"depends_on"`
	} `yaml:"services"`
}

// applyComposeTopology emits a SCOPE.Service per compose service and a
// DEPENDS_ON edge for each `depends_on:` entry (both the list short form and the
// map long form `depends_on: {db: {condition: ...}}`).
func applyComposeTopology(src string, emitService func(name, role, tool string) string, emitEdge func(fromID, toID, kind, flow string)) {
	var cf composeFile
	if err := yaml.Unmarshal([]byte(src), &cf); err != nil || len(cf.Services) == 0 {
		return
	}
	// First mint every service node so depends_on targets always resolve.
	for name := range cf.Services {
		emitService(name, "compose_service", "docker-compose")
	}
	for name, svc := range cf.Services {
		from := depTopoServiceID(name)
		for _, dep := range composeDependsOn(svc.DependsOn) {
			to := emitService(dep, "compose_service", "docker-compose")
			emitEdge(from, to, depTopoDependsOn, "compose_depends_on")
		}
	}
}

// composeDependsOn normalises a depends_on node into a flat list of service
// names, handling both the sequence short form (`depends_on: [db, redis]`) and
// the mapping long form (`depends_on: {db: {condition: service_healthy}}`).
func composeDependsOn(n yaml.Node) []string {
	var out []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				out = append(out, item.Value)
			}
		}
	case yaml.MappingNode:
		// Mapping content is [key, value, key, value, ...]; keys are at even idx.
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; k.Kind == yaml.ScalarNode && k.Value != "" {
				out = append(out, k.Value)
			}
		}
	}
	return out
}

// kongDeclarative is the minimal Kong declarative-config shape.
type kongDeclarative struct {
	Services []struct {
		Name   string `yaml:"name"`
		Routes []struct {
			Name string `yaml:"name"`
		} `yaml:"routes"`
	} `yaml:"services"`
}

// applyKongTopology emits a gateway node and a ROUTES_TO edge to every declared
// Kong service that carries at least one route (the request entry points).
func applyKongTopology(src, base string, emitService func(name, role, tool string) string, emitEdge func(fromID, toID, kind, flow string)) {
	var kc kongDeclarative
	if err := yaml.Unmarshal([]byte(src), &kc); err != nil || len(kc.Services) == 0 {
		return
	}
	gw := emitService(gatewayName("kong", base), "gateway", "kong")
	for _, svc := range kc.Services {
		if svc.Name == "" || len(svc.Routes) == 0 {
			continue
		}
		to := emitService(svc.Name, "backend", "kong")
		emitEdge(gw, to, depTopoRoutesTo, "kong_route")
	}
}

// depTopoIsTraefikDynamic content-sniffs a Traefik dynamic-config file: it has a
// top-level `http:` mapping with `routers:` and/or `services:` underneath.
func depTopoIsTraefikDynamic(src string) bool {
	return strings.Contains(src, "http:") &&
		strings.Contains(src, "routers:") &&
		strings.Contains(src, "service:")
}

// traefikDynamic is the minimal Traefik dynamic-config shape.
type traefikDynamic struct {
	HTTP struct {
		Routers map[string]struct {
			Service string `yaml:"service"`
		} `yaml:"routers"`
		Services map[string]yaml.Node `yaml:"services"`
	} `yaml:"http"`
}

// applyTraefikTopology emits a gateway node and a ROUTES_TO edge from the gateway
// to each service a router targets.
func applyTraefikTopology(src, base string, emitService func(name, role, tool string) string, emitEdge func(fromID, toID, kind, flow string)) {
	var td traefikDynamic
	if err := yaml.Unmarshal([]byte(src), &td); err != nil {
		return
	}
	if len(td.HTTP.Routers) == 0 {
		return
	}
	gw := emitService(gatewayName("traefik", base), "gateway", "traefik")
	for _, r := range td.HTTP.Routers {
		if r.Service == "" {
			continue
		}
		to := emitService(r.Service, "backend", "traefik")
		emitEdge(gw, to, depTopoRoutesTo, "traefik_router")
	}
}
