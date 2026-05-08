package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// serviceDetector detects microservice definitions and integrations.
// Matches Python service_detector.py.
type serviceDetector struct{}

var serviceImportPrefixes = []string{
	"grpc", "thrift", "protobuf", "google.golang.org/grpc",
	"consul", "eureka", "service-discovery",
	"@microservice", "@GrpcService",
}

var (
	svcGRPCServerRE    = regexp.MustCompile(`grpc\.NewServer\s*\(`)
	svcGRPCClientRE    = regexp.MustCompile(`grpc\.Dial(?:Context)?\s*\(`)
	svcHTTPHandlerRE   = regexp.MustCompile(`http\.Handle(?:Func)?\s*\(\s*["']([^"']+)["']`)
	svcSpringServiceRE = regexp.MustCompile(`@(?:Service|Component|RestController|Controller)\b`)
	svcNestServiceRE   = regexp.MustCompile(`@Injectable\b`)
	svcFastAPIRouterRE = regexp.MustCompile(`(?:APIRouter|FastAPI)\s*\(`)
	svcConsulRE        = regexp.MustCompile(`(?:consul\.NewClient|consul\.DefaultConfig)`)
	svcEurekaRE        = regexp.MustCompile(`@EnableEurekaClient\b`)
)

func (s *serviceDetector) Category() string { return "service" }

func (s *serviceDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, p := range serviceImportPrefixes {
		if strings.Contains(srcLower, strings.ToLower(p)) {
			return true
		}
	}
	return svcGRPCServerRE.MatchString(src) ||
		svcHTTPHandlerRE.MatchString(src) ||
		svcSpringServiceRE.MatchString(src) ||
		svcNestServiceRE.MatchString(src) ||
		svcFastAPIRouterRE.MatchString(src)
}

func (s *serviceDetector) Detect(filePath, language, src string) []types.EntityRecord {
	// MX-1081: The kotlin extractor owns Spring stereotype → SCOPE.Service
	// conversion locally (see internal/extractors/kotlin/kotlin.go). Emitting
	// the generic "spring_service" ghost here alongside the proper named
	// service entity causes duplicate SCOPE.Service nodes and fails Python
	// golden parity. Skip kotlin entirely.
	if language == "kotlin" {
		return nil
	}
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, svcKind string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Service", "service", language, line,
			map[string]string{"kind": "service", "service_kind": svcKind}))
	}

	if m := svcGRPCServerRE.FindStringIndex(src); m != nil {
		emit("grpc:server", "grpc_server", "grpc_server", lineOf(src, m[0]))
	}
	if m := svcGRPCClientRE.FindStringIndex(src); m != nil {
		emit("grpc:client", "grpc_client", "grpc_client", lineOf(src, m[0]))
	}
	for _, m := range svcHTTPHandlerRE.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		emit("http:"+path, "http_handler_"+strings.ReplaceAll(path, "/", "_"), "http_handler", lineOf(src, m[0]))
	}
	if m := svcSpringServiceRE.FindStringIndex(src); m != nil {
		emit("spring:service", "spring_service", "spring_bean", lineOf(src, m[0]))
	}
	if m := svcNestServiceRE.FindStringIndex(src); m != nil {
		emit("nestjs:injectable", "nestjs_injectable", "nestjs_service", lineOf(src, m[0]))
	}
	if m := svcFastAPIRouterRE.FindStringIndex(src); m != nil {
		emit("fastapi:router", "fastapi_router", "fastapi_service", lineOf(src, m[0]))
	}
	if m := svcConsulRE.FindStringIndex(src); m != nil {
		emit("consul:client", "consul_client", "service_discovery", lineOf(src, m[0]))
	}
	if m := svcEurekaRE.FindStringIndex(src); m != nil {
		emit("eureka:client", "eureka_client", "service_discovery", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&serviceDetector{})
}
