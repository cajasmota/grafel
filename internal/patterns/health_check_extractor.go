package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// healthCheckExtractor detects health check and readiness probe endpoints.
// Matches Python health_check_extractor.py.
type healthCheckExtractor struct{}

var (
	healthExpressTriggerRE  = regexp.MustCompile(`(?i)(?:app|router)\s*\.\s*(?:get|post|all|use)\s*\(\s*['` + "`" + `""]/?(?:health|healthz|ready|readyz|ping|status)['` + "`" + `""]`)
	healthSpringTriggerRE   = regexp.MustCompile(`(?:@ConditionalOnEnabledHealthIndicator|implements\s+HealthIndicator|management\.endpoints\.web\.exposure\.include\s*=)`)
	healthQuarkusTriggerRE  = regexp.MustCompile(`@(?:Liveness|Readiness)\b`)
	healthFastAPITriggerRE  = regexp.MustCompile(`(?i)@\s*\w+\s*\.\s*(?:get|post|route)\s*\(\s*["']/?(?:health|healthz|ready|readyz|ping)["']`)
	healthExpressRouteRE    = regexp.MustCompile(`(?i)(?:app|router)\s*\.\s*(?:get|post|all|use)\s*\(\s*['` + "`" + `""](?P<path>/?(?:health|healthz|ready|readyz|ping|status))['` + "`" + `""]`)
	healthSpringIndicatorRE = regexp.MustCompile(`(?:implements\s+HealthIndicator|extends\s+AbstractHealthIndicator)`)
	healthSpringCondRE      = regexp.MustCompile(`@ConditionalOnEnabledHealthIndicator\s*\(\s*["']([^"']+)["']`)
	healthQuarkusAnnotRE    = regexp.MustCompile(`@(Liveness|Readiness)\b`)
	healthFastAPIRouteRE    = regexp.MustCompile(`(?i)@\s*\w+\s*\.\s*(?:get|post|route)\s*\(\s*["'](?P<path>/?(?:health|healthz|ready|readyz|ping))["']`)
)

var readinessPaths = map[string]bool{"/ready": true, "/readyz": true}

func probeKind(path string) string {
	path = strings.ToLower(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if readinessPaths[path] {
		return "readiness_probe"
	}
	return "health_check"
}

func (h *healthCheckExtractor) Category() string { return "health_check" }

func (h *healthCheckExtractor) AppliesTo(src string) bool {
	return healthExpressTriggerRE.MatchString(src) ||
		healthSpringTriggerRE.MatchString(src) ||
		healthQuarkusTriggerRE.MatchString(src) ||
		healthFastAPITriggerRE.MatchString(src)
}

func (h *healthCheckExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Express/NestJS routes
	pathIdx := healthExpressRouteRE.SubexpIndex("path")
	for _, m := range healthExpressRouteRE.FindAllStringSubmatchIndex(src, -1) {
		if pathIdx < 0 || m[pathIdx*2] < 0 {
			continue
		}
		path := strings.ToLower(src[m[pathIdx*2]:m[pathIdx*2+1]])
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		kind := probeKind(path)
		key := fmt.Sprintf("express:%s", path)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("health_endpoint%s", path), "SCOPE.Operation", kind, language,
			lineOf(src, m[0]),
			map[string]string{"kind": kind, "path": path, "framework": "express"}))
	}

	// Spring Boot Actuator
	if healthSpringIndicatorRE.MatchString(src) {
		key := "spring:health_indicator"
		if !seen[key] {
			seen[key] = true
			name := "health_indicator"
			for _, m := range healthSpringCondRE.FindAllStringSubmatchIndex(src, -1) {
				name = src[m[2]:m[3]]
			}
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Operation", "health_check", language, 1,
				map[string]string{"kind": "health_check", "framework": "spring_actuator"}))
		}
	}

	// Quarkus/Micronaut @Liveness / @Readiness
	for _, m := range healthQuarkusAnnotRE.FindAllStringSubmatchIndex(src, -1) {
		ann := src[m[2]:m[3]]
		key := "quarkus:" + ann
		if seen[key] {
			continue
		}
		seen[key] = true
		kind := "health_check"
		if ann == "Readiness" {
			kind = "readiness_probe"
		}
		results = append(results, makeEntity(filePath,
			"quarkus_"+strings.ToLower(ann), "SCOPE.Operation", kind, language,
			lineOf(src, m[0]),
			map[string]string{"kind": kind, "annotation": "@" + ann, "framework": "quarkus"}))
	}

	// FastAPI/Flask routes
	fastPathIdx := healthFastAPIRouteRE.SubexpIndex("path")
	for _, m := range healthFastAPIRouteRE.FindAllStringSubmatchIndex(src, -1) {
		if fastPathIdx < 0 || m[fastPathIdx*2] < 0 {
			continue
		}
		path := strings.ToLower(src[m[fastPathIdx*2]:m[fastPathIdx*2+1]])
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		kind := probeKind(path)
		key := fmt.Sprintf("fastapi:%s", path)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("health_endpoint%s", path), "SCOPE.Operation", kind, language,
			lineOf(src, m[0]),
			map[string]string{"kind": kind, "path": path, "framework": "fastapi"}))
	}

	return results
}

func init() {
	Register(&healthCheckExtractor{})
}
