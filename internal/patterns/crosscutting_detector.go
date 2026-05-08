package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// crosscuttingDetector detects cross-cutting concerns: caching, transactions, middleware.
// Matches Python crosscutting_detector.py.
type crosscuttingDetector struct{}

var cacheImportPrefixes = []string{
	"cache", "redis", "memcached", "caffeine", "ehcache", "spring-cache",
}
var txImportPrefixes = []string{
	"@Transactional", "transaction", "atomic", "begin_nested", "savepoint",
}
var mwImportPrefixes = []string{
	"middleware", "interceptor", "filter", "guard", "UseGuards", "UseInterceptors",
}

var (
	ccGoMiddlewareRE    = regexp.MustCompile(`func\s+\w*\s*\([^)]*http\.Handler[^)]*\)\s*http\.Handler`)
	ccNestGuardRE       = regexp.MustCompile(`@UseGuards\s*\(`)
	ccNestInterceptorRE = regexp.MustCompile(`@UseInterceptors\s*\(`)
	ccTransactionalRE   = regexp.MustCompile(`@Transactional\b`)
	ccCacheableRE       = regexp.MustCompile(`@(?:Cacheable|CacheEvict|CachePut)\b`)
)

func (c *crosscuttingDetector) Category() string { return "cross_cutting" }

func (c *crosscuttingDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, p := range append(append(cacheImportPrefixes, txImportPrefixes...), mwImportPrefixes...) {
		if strings.Contains(srcLower, strings.ToLower(p)) {
			return true
		}
	}
	return ccGoMiddlewareRE.MatchString(src) ||
		ccNestGuardRE.MatchString(src) ||
		ccNestInterceptorRE.MatchString(src) ||
		ccTransactionalRE.MatchString(src) ||
		ccCacheableRE.MatchString(src)
}

func (c *crosscuttingDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	if ccGoMiddlewareRE.MatchString(src) {
		key := "go:middleware"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"crosscutting_middleware_go", "SCOPE.Component", "cross_cutting", language, 1,
				map[string]string{"kind": "cross_cutting", "concern": "CROSSCUTTING:MIDDLEWARE_GO"}))
		}
	}

	for _, m := range ccNestGuardRE.FindAllStringIndex(src, -1) {
		key := "nestjs:guard"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"crosscutting_guard_nestjs", "SCOPE.Component", "cross_cutting", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "cross_cutting", "concern": "CROSSCUTTING:MIDDLEWARE_NESTJS_GUARD"}))
		}
	}

	for _, m := range ccNestInterceptorRE.FindAllStringIndex(src, -1) {
		key := "nestjs:interceptor"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"crosscutting_interceptor_nestjs", "SCOPE.Component", "cross_cutting", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "cross_cutting", "concern": "CROSSCUTTING:MIDDLEWARE_NESTJS_INTERCEPTOR"}))
		}
	}

	if ccTransactionalRE.MatchString(src) {
		key := "transactional"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"crosscutting_transactional", "SCOPE.Component", "cross_cutting", language, 1,
				map[string]string{"kind": "cross_cutting", "concern": "CROSSCUTTING:TRANSACTION"}))
		}
	}

	if ccCacheableRE.MatchString(src) {
		key := "cacheable"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"crosscutting_cacheable", "SCOPE.Component", "cross_cutting", language, 1,
				map[string]string{"kind": "cross_cutting", "concern": "CROSSCUTTING:CACHE"}))
		}
	}

	return results
}

func init() {
	Register(&crosscuttingDetector{})
}
