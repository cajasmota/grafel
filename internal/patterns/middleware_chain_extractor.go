package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// middlewareChainExtractor detects middleware chain definitions.
// Matches Python middleware_chain_extractor.py.
type middlewareChainExtractor struct{}

var mcSourceTokens = []string{
	"app.use(", "router.use(", "UseInterceptors", "UseGuards",
	"AddMiddleware", "app.UseMiddleware",
}

var (
	mcExpressRouteRE        = regexp.MustCompile(`(?:app|router)\s*\.\s*(get|post|put|patch|delete|all)\s*\(\s*["']([^"']+)["']`)
	mcExpressAppUseRE       = regexp.MustCompile(`app\s*\.\s*use\s*\(([^)]+)\)`)
	mcNestInterceptorsRE  = regexp.MustCompile(`@UseInterceptors\s*\(\s*([^)]+)\)`)
	mcASPNetUseRE           = regexp.MustCompile(`app\.Use(\w+)\s*\(`)
	mcFastAPIMiddlewareRE   = regexp.MustCompile(`add_middleware\s*\(\s*(\w+)`)
)

func (m *middlewareChainExtractor) Category() string { return "middleware_chain" }

func (m *middlewareChainExtractor) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range mcSourceTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

func (m *middlewareChainExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Express routes
	for idx, match := range mcExpressRouteRE.FindAllStringSubmatchIndex(src, -1) {
		method := src[match[2]:match[3]]
		path := src[match[4]:match[5]]
		key := fmt.Sprintf("express:%s:%s", method, path)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("middleware_express_%s_%d", method, idx),
			"SCOPE.Operation", "middleware_route", language,
			lineOf(src, match[0]),
			map[string]string{"kind": "middleware_chain", "framework": "express", "method": method, "path": path}))
	}

	// Express app.use()
	for idx, match := range mcExpressAppUseRE.FindAllStringSubmatchIndex(src, -1) {
		mw := strings.TrimSpace(src[match[2]:match[3]])
		key := "express:use:" + mw
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("middleware_app_use_%d", idx),
			"SCOPE.Component", "middleware", language,
			lineOf(src, match[0]),
			map[string]string{"kind": "middleware_chain", "framework": "express", "middleware": mw}))
	}

	// NestJS @UseInterceptors
	for _, match := range mcNestInterceptorsRE.FindAllStringSubmatchIndex(src, -1) {
		mw := strings.TrimSpace(src[match[2]:match[3]])
		key := "nestjs:interceptor:" + mw
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"middleware_nestjs_interceptor_"+mw,
			"SCOPE.Component", "middleware", language,
			lineOf(src, match[0]),
			map[string]string{"kind": "middleware_chain", "framework": "nestjs", "middleware": mw, "type": "interceptor"}))
	}

	// FastAPI add_middleware
	for _, match := range mcFastAPIMiddlewareRE.FindAllStringSubmatchIndex(src, -1) {
		mw := src[match[2]:match[3]]
		key := "fastapi:middleware:" + mw
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"middleware_fastapi_"+mw,
			"SCOPE.Component", "middleware", language,
			lineOf(src, match[0]),
			map[string]string{"kind": "middleware_chain", "framework": "fastapi", "middleware": mw}))
	}

	// ASP.NET app.Use*
	for _, match := range mcASPNetUseRE.FindAllStringSubmatchIndex(src, -1) {
		mw := src[match[2]:match[3]]
		key := "aspnet:use:" + mw
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"middleware_aspnet_"+mw,
			"SCOPE.Component", "middleware", language,
			lineOf(src, match[0]),
			map[string]string{"kind": "middleware_chain", "framework": "aspnet", "middleware": mw}))
	}

	return results
}

func init() {
	Register(&middlewareChainExtractor{})
}
