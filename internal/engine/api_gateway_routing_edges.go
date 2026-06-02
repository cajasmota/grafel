// API-gateway route topology synthesis — #3723 (epic #3628, roadmap area #21).
//
// The deployment_topology pass (#3633, deployment_topology_edges.go) modelled the
// reverse-proxy / infra-gateway layer that sits in FRONT of an application:
// nginx, Caddy, Kong, Traefik. This pass covers the complementary, previously-
// invisible layer — the APPLICATION-FRAMEWORK API gateways whose configuration
// declares a route and the upstream service it forwards to:
//
//   - Spring Cloud Gateway (Spring Boot): `application.yml`
//     `spring.cloud.gateway.routes` and the Java `RouteLocatorBuilder` DSL.
//   - Ocelot (.NET): `ocelot.json` `Routes[].DownstreamHostAndPorts`.
//   - Express Gateway / http-proxy-middleware (Node): `gateway.config.yml`
//     apiEndpoints+serviceEndpoints, and `createProxyMiddleware({target})`.
//
// It mints a first-class gateway-route node and a ROUTES_TO edge to the upstream
// backend service, mirroring the edge shape #3633 established:
//
//	gateway route (SCOPE.Route)  -- ROUTES_TO -->  backend SCOPE.Service
//
// The backend service is keyed `service:<name>` — the SAME canonical key the
// deployment-topology pass uses — so a Spring Cloud Gateway route to
// `lb://user-service` and a docker-compose service `user-service` collapse onto
// one SCOPE.Service node, and the route edge lands on the service the rest of the
// graph already knows.
//
// # Scope guard
//
// Append-only: this pass never modifies or removes existing entities or edges,
// so it cannot regress the surrounding pipeline. It fires only for a file whose
// basename / language / content the gate recognises as one of the gateway
// sources above; every other file is a fast no-op. Where the upstream URI is
// dynamic / templated (a `${...}` placeholder, a variable) the edge is honestly
// omitted rather than emitting a garbage node (honest-partial).
//
// Closes #3723. Epic #3628.
package engine

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

const (
	apiGwRouteKind   = string(types.EntityKindRoute)
	apiGwServiceKind = string(types.EntityKindService)
	apiGwRoutesTo    = string(types.RelationshipKindRoutesTo)
)

// apiGwServiceID returns the canonical, file-independent ID for a backend
// service. It MUST match deployment_topology_edges.go's depTopoServiceID so the
// gateway route and any compose/proxy node for the same logical service collapse
// onto a single SCOPE.Service node.
func apiGwServiceID(name string) string { return "service:" + name }

// apiGwRouteID keys a gateway route node by gateway-tool + file + route
// identity, keeping two routes in the same file (and the same route across two
// gateways) distinct.
func apiGwRouteID(tool, base, routeKey string) string {
	return "route:" + tool + ":" + base + ":" + routeKey
}

// applyAPIGatewayRoutingEdges is the per-file entry point, registered in
// detector.go after applyDeploymentTopologyEdges. Append-only.
func applyAPIGatewayRoutingEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(args.Content)
	base := strings.ToLower(filepath.Base(args.Path))

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	emitService := func(name string) string {
		if name == "" {
			return ""
		}
		id := apiGwServiceID(name)
		if !seenEnt[id] {
			seenEnt[id] = true
			entities = append(entities, types.EntityRecord{
				ID:            id,
				Name:          name,
				QualifiedName: id,
				Kind:          apiGwServiceKind,
				SourceFile:    args.Path,
				Language:      args.Lang,
				Properties: map[string]string{
					"deployment_role": "backend",
					"synthesis":       "api_gateway_routing",
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}

	emitRoute := func(tool, base, routeKey, name string, props map[string]string) string {
		id := apiGwRouteID(tool, base, routeKey)
		if !seenEnt[id] {
			seenEnt[id] = true
			p := map[string]string{
				"gateway_tool": tool,
				"synthesis":    "api_gateway_routing",
			}
			for k, v := range props {
				if v != "" {
					p[k] = v
				}
			}
			entities = append(entities, types.EntityRecord{
				ID:               id,
				Name:             name,
				QualifiedName:    id,
				Kind:             apiGwRouteKind,
				SourceFile:       args.Path,
				Language:         args.Lang,
				Properties:       p,
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}

	emitRoutesTo := func(routeID, serviceID, flow, predicate string) {
		if routeID == "" || serviceID == "" || routeID == serviceID {
			return
		}
		key := routeID + "|" + serviceID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"flow":      flow,
			"synthesis": "api_gateway_routing",
		}
		if predicate != "" {
			props["predicate"] = predicate
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     routeID,
			ToID:       serviceID,
			Kind:       apiGwRoutesTo,
			Properties: props,
		})
	}

	switch {
	case base == "ocelot.json" || apiGwIsOcelotName(base):
		applyOcelotRouting(src, base, emitService, emitRoute, emitRoutesTo)
	case args.Lang == "yaml" && apiGwIsSpringGatewayYAML(src):
		applySpringCloudGatewayYAML(src, base, emitService, emitRoute, emitRoutesTo)
	case args.Lang == "yaml" && apiGwIsExpressGatewayYAML(src):
		applyExpressGatewayYAML(src, base, emitService, emitRoute, emitRoutesTo)
	case args.Lang == "java" && strings.Contains(src, "RouteLocatorBuilder"):
		applySpringCloudGatewayJava(src, base, emitService, emitRoute, emitRoutesTo)
	case (args.Lang == "javascript" || args.Lang == "typescript") &&
		strings.Contains(src, "createProxyMiddleware"):
		applyHTTPProxyMiddleware(src, base, emitService, emitRoute, emitRoutesTo)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// apiGwIsOcelotName matches Ocelot config files following the conventional
// `ocelot.<env>.json` naming (ocelot.json is checked explicitly).
var apiGwOcelotNameRe = regexp.MustCompile(`(?i)^ocelot\.[a-z0-9_-]+\.json$`)

func apiGwIsOcelotName(base string) bool { return apiGwOcelotNameRe.MatchString(base) }

// apiGwUpstreamFromURI extracts the upstream service name from a Spring Cloud
// Gateway `uri`. Forms handled: `lb://USER-SERVICE` (service-discovery load-
// balanced — the authority IS the service id), and `http(s)://host[:port]`
// (the host is the service). A `${...}` placeholder or empty value yields "".
func apiGwUpstreamFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" || strings.Contains(uri, "${") {
		return ""
	}
	i := strings.Index(uri, "://")
	if i < 0 {
		return ""
	}
	authority := uri[i+3:]
	// Strip path / query.
	if j := strings.IndexAny(authority, "/?"); j >= 0 {
		authority = authority[:j]
	}
	// Strip :port.
	if j := strings.LastIndex(authority, ":"); j >= 0 && isAllDigits(authority[j+1:]) {
		authority = authority[:j]
	}
	authority = strings.TrimSpace(authority)
	if authority == "" || authority == "localhost" || strings.Contains(authority, "$") {
		return ""
	}
	return authority
}

// ---------------------------------------------------------------------------
// Spring Cloud Gateway — YAML (application.yml spring.cloud.gateway.routes)
// ---------------------------------------------------------------------------

func apiGwIsSpringGatewayYAML(src string) bool {
	return strings.Contains(src, "spring:") &&
		strings.Contains(src, "gateway:") &&
		strings.Contains(src, "routes:")
}

type springGatewayYAML struct {
	Spring struct {
		Cloud struct {
			Gateway struct {
				Routes []struct {
					ID         string   `yaml:"id"`
					URI        string   `yaml:"uri"`
					Predicates []string `yaml:"predicates"`
				} `yaml:"routes"`
			} `yaml:"gateway"`
		} `yaml:"cloud"`
	} `yaml:"spring"`
}

// springGatewayPathPredicate returns the first `Path=...` predicate value, the
// most common gateway match. Other predicate kinds are kept verbatim if no Path
// is present.
func springGatewayPathPredicate(predicates []string) string {
	for _, p := range predicates {
		if strings.HasPrefix(p, "Path=") {
			return strings.TrimPrefix(p, "Path=")
		}
	}
	if len(predicates) > 0 {
		return predicates[0]
	}
	return ""
}

func applySpringCloudGatewayYAML(src, base string, emitService func(string) string, emitRoute func(tool, base, routeKey, name string, props map[string]string) string, emitRoutesTo func(routeID, serviceID, flow, predicate string)) {
	var doc springGatewayYAML
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return
	}
	for idx, r := range doc.Spring.Cloud.Gateway.Routes {
		svcName := apiGwUpstreamFromURI(r.URI)
		if svcName == "" {
			continue
		}
		predicate := springGatewayPathPredicate(r.Predicates)
		routeKey := r.ID
		if routeKey == "" {
			routeKey = "route" + itoa(idx)
		}
		routeID := emitRoute("spring-cloud-gateway", base, routeKey, routeKey, map[string]string{
			"path":    predicate,
			"uri":     r.URI,
			"variant": "yaml",
		})
		svcID := emitService(svcName)
		emitRoutesTo(routeID, svcID, "spring_cloud_gateway_route", predicate)
	}
}

// ---------------------------------------------------------------------------
// Spring Cloud Gateway — Java RouteLocatorBuilder DSL
// ---------------------------------------------------------------------------

// apiGwJavaRouteRe matches one `.route(...)` builder chain (lazily, up to the
// closing of the lambda's `.uri(...)`), capturing the whole body so the path()
// and uri() sub-matches can be pulled out. A `.route("id", r -> ...)` form is
// also covered: the optional first string arg is ignored for upstream purposes.
var (
	apiGwJavaRouteChainRe = regexp.MustCompile(`(?s)\.route\s*\((.*?)\.uri\s*\(\s*"([^"]*)"\s*\)`)
	apiGwJavaPathRe       = regexp.MustCompile(`\.path\s*\(\s*"([^"]*)"`)
	apiGwJavaRouteIDRe    = regexp.MustCompile(`\.route\s*\(\s*"([^"]*)"`)
)

func applySpringCloudGatewayJava(src, base string, emitService func(string) string, emitRoute func(tool, base, routeKey, name string, props map[string]string) string, emitRoutesTo func(routeID, serviceID, flow, predicate string)) {
	idx := 0
	for _, m := range apiGwJavaRouteChainRe.FindAllStringSubmatch(src, -1) {
		body, uri := m[1], m[2]
		svcName := apiGwUpstreamFromURI(uri)
		if svcName == "" {
			continue
		}
		var path string
		if pm := apiGwJavaPathRe.FindStringSubmatch(body); pm != nil {
			path = pm[1]
		}
		routeKey := ""
		if rid := apiGwJavaRouteIDRe.FindStringSubmatch(m[0]); rid != nil {
			routeKey = rid[1]
		}
		if routeKey == "" {
			routeKey = "route" + itoa(idx)
		}
		idx++
		routeID := emitRoute("spring-cloud-gateway", base, routeKey, routeKey, map[string]string{
			"path":    path,
			"uri":     uri,
			"variant": "java",
		})
		svcID := emitService(svcName)
		emitRoutesTo(routeID, svcID, "spring_cloud_gateway_route", path)
	}
}

// ---------------------------------------------------------------------------
// Ocelot (.NET) — ocelot.json Routes[]
// ---------------------------------------------------------------------------

type ocelotConfig struct {
	Routes []ocelotRoute `json:"Routes"`
	// Legacy Ocelot (pre-v16) used "ReRoutes".
	ReRoutes []ocelotRoute `json:"ReRoutes"`
}

type ocelotRoute struct {
	UpstreamPathTemplate   string `json:"UpstreamPathTemplate"`
	DownstreamPathTemplate string `json:"DownstreamPathTemplate"`
	ServiceName            string `json:"ServiceName"`
	DownstreamHostAndPorts []struct {
		Host string `json:"Host"`
		Port int    `json:"Port"`
	} `json:"DownstreamHostAndPorts"`
}

func applyOcelotRouting(src, base string, emitService func(string) string, emitRoute func(tool, base, routeKey, name string, props map[string]string) string, emitRoutesTo func(routeID, serviceID, flow, predicate string)) {
	var cfg ocelotConfig
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		return
	}
	routes := cfg.Routes
	routes = append(routes, cfg.ReRoutes...)
	for idx, r := range routes {
		// Prefer the service-discovery name; fall back to the first
		// DownstreamHostAndPorts host (a static upstream).
		svcName := strings.TrimSpace(r.ServiceName)
		if svcName == "" && len(r.DownstreamHostAndPorts) > 0 {
			svcName = strings.TrimSpace(r.DownstreamHostAndPorts[0].Host)
		}
		if svcName == "" || strings.Contains(svcName, "{") || svcName == "localhost" {
			continue
		}
		upstream := r.UpstreamPathTemplate
		routeKey := upstream
		if routeKey == "" {
			routeKey = "route" + itoa(idx)
		}
		routeID := emitRoute("ocelot", base, routeKey, upstream, map[string]string{
			"upstream_path_template":   upstream,
			"downstream_path_template": r.DownstreamPathTemplate,
		})
		svcID := emitService(svcName)
		emitRoutesTo(routeID, svcID, "ocelot_route", upstream)
	}
}

// ---------------------------------------------------------------------------
// Express Gateway — gateway.config.yml (apiEndpoints + serviceEndpoints + pipelines)
// ---------------------------------------------------------------------------

func apiGwIsExpressGatewayYAML(src string) bool {
	return strings.Contains(src, "apiEndpoints:") &&
		strings.Contains(src, "serviceEndpoints:")
}

type expressGatewayYAML struct {
	APIEndpoints     map[string]yaml.Node `yaml:"apiEndpoints"`
	ServiceEndpoints map[string]struct {
		URL string `yaml:"url"`
	} `yaml:"serviceEndpoints"`
	Pipelines map[string]struct {
		APIEndpoints []string `yaml:"apiEndpoints"`
		// Each policy is a single-key map; for the proxy policy the value is the
		// action struct `{action: {serviceEndpoint: <name>}}`.
		Policies []map[string]struct {
			Action struct {
				ServiceEndpoint string `yaml:"serviceEndpoint"`
			} `yaml:"action"`
		} `yaml:"policies"`
	} `yaml:"pipelines"`
}

// expressURLHost extracts the host (service name) from a serviceEndpoint url.
func expressURLHost(url string) string {
	url = strings.TrimSpace(url)
	if url == "" || strings.Contains(url, "${") {
		return ""
	}
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
	}
	if j := strings.IndexAny(url, "/?"); j >= 0 {
		url = url[:j]
	}
	if j := strings.LastIndex(url, ":"); j >= 0 && isAllDigits(url[j+1:]) {
		url = url[:j]
	}
	url = strings.TrimSpace(url)
	if url == "" || url == "localhost" || strings.Contains(url, "$") {
		return ""
	}
	return url
}

func applyExpressGatewayYAML(src, base string, emitService func(string) string, emitRoute func(tool, base, routeKey, name string, props map[string]string) string, emitRoutesTo func(routeID, serviceID, flow, predicate string)) {
	var doc expressGatewayYAML
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return
	}
	for pipeName, pipe := range doc.Pipelines {
		// Resolve the proxy policy's target serviceEndpoint → its url host.
		var svcName string
		for _, pol := range pipe.Policies {
			for name, body := range pol {
				if name != "proxy" {
					continue
				}
				ep := body.Action.ServiceEndpoint
				if ep == "" {
					continue
				}
				if se, ok := doc.ServiceEndpoints[ep]; ok {
					if h := expressURLHost(se.URL); h != "" {
						svcName = h
					}
				}
			}
		}
		if svcName == "" {
			continue
		}
		// Each apiEndpoint bound to the pipeline is a route entry point.
		for _, apiName := range pipe.APIEndpoints {
			routeID := emitRoute("express-gateway", base, pipeName+":"+apiName, apiName, map[string]string{
				"pipeline":     pipeName,
				"api_endpoint": apiName,
			})
			svcID := emitService(svcName)
			emitRoutesTo(routeID, svcID, "express_gateway_route", apiName)
		}
	}
}

// ---------------------------------------------------------------------------
// http-proxy-middleware (Node) — createProxyMiddleware({ target, ... })
// ---------------------------------------------------------------------------

// apiGwProxyMiddlewareRe captures one createProxyMiddleware({...}) options
// object so the target can be pulled out.
var (
	apiGwProxyMiddlewareRe  = regexp.MustCompile(`(?s)createProxyMiddleware\s*\(\s*\{(.*?)\}\s*\)`)
	apiGwProxyTargetRe      = regexp.MustCompile(`target\s*:\s*['"` + "`" + `]([^'"` + "`" + `]*)['"` + "`" + `]`)
	apiGwProxyPathRewriteRe = regexp.MustCompile(`pathRewrite`)
)

// apiGwHostFromTarget extracts the service host from an http-proxy-middleware
// target. Template literals containing `${...}` are dynamic → "".
func apiGwHostFromTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "${") {
		return ""
	}
	if i := strings.Index(target, "://"); i >= 0 {
		target = target[i+3:]
	}
	if j := strings.IndexAny(target, "/?"); j >= 0 {
		target = target[:j]
	}
	if j := strings.LastIndex(target, ":"); j >= 0 && isAllDigits(target[j+1:]) {
		target = target[:j]
	}
	target = strings.TrimSpace(target)
	if target == "" || target == "localhost" || strings.Contains(target, "$") {
		return ""
	}
	return target
}

func applyHTTPProxyMiddleware(src, base string, emitService func(string) string, emitRoute func(tool, base, routeKey, name string, props map[string]string) string, emitRoutesTo func(routeID, serviceID, flow, predicate string)) {
	idx := 0
	for _, m := range apiGwProxyMiddlewareRe.FindAllStringSubmatch(src, -1) {
		body := m[1]
		tm := apiGwProxyTargetRe.FindStringSubmatch(body)
		if tm == nil {
			continue
		}
		svcName := apiGwHostFromTarget(tm[1])
		if svcName == "" {
			continue
		}
		hasRewrite := ""
		if apiGwProxyPathRewriteRe.MatchString(body) {
			hasRewrite = "true"
		}
		routeKey := "proxy" + itoa(idx)
		idx++
		routeID := emitRoute("http-proxy-middleware", base, routeKey, svcName, map[string]string{
			"target":       tm[1],
			"path_rewrite": hasRewrite,
		})
		svcID := emitService(svcName)
		emitRoutesTo(routeID, svcID, "http_proxy_middleware", "")
	}
}
