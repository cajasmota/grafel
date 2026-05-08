package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// cacheEvictionDetector detects cache eviction and TTL configuration.
// Matches Python cache_eviction_detector.py.
type cacheEvictionDetector struct{}

var (
	cacheJavaTriggerRE   = regexp.MustCompile(`@(?:Cacheable|CacheEvict|CachePut|CacheConfig)\b`)
	cacheRedisTriggerRE  = regexp.MustCompile(`(?:redis\.expire|setex|EXPIRE\s+\w+\s+\d+)`)
	cacheNodeTriggerRE   = regexp.MustCompile(`(?:node-cache|lru-cache|cache-manager)`)
	cachePythonTriggerRE = regexp.MustCompile(`(?:cache\.set|cache\.delete|cache\.clear|@cache)`)
	cacheGoTriggerRE     = regexp.MustCompile(`(?:ristretto|bigcache|go-cache|gcache)`)
	cacheDotNetTriggerRE = regexp.MustCompile(`(?:IMemoryCache|IDistributedCache|SetSlidingExpiration|SetAbsoluteExpiration)`)
	cacheRubyTriggerRE   = regexp.MustCompile(`(?:Rails\.cache|cache_store|expires_in)`)
	cacheSpringRE        = regexp.MustCompile(`@(?:Cacheable|CacheEvict|CachePut)\s*\(([^)]*)\)`)
	cacheSpringNameRE    = regexp.MustCompile(`(?:cacheNames|value)\s*=\s*["']([^"']+)["']`)
	cacheRedisTTLRE      = regexp.MustCompile(`(?:expire|setex|EXPIRE)\s*\(?.*?(\d+)`)
)

func (c *cacheEvictionDetector) Category() string { return "cache_eviction" }

func (c *cacheEvictionDetector) AppliesTo(src string) bool {
	return cacheJavaTriggerRE.MatchString(src) ||
		cacheRedisTriggerRE.MatchString(src) ||
		cacheNodeTriggerRE.MatchString(src) ||
		cachePythonTriggerRE.MatchString(src) ||
		cacheGoTriggerRE.MatchString(src) ||
		cacheDotNetTriggerRE.MatchString(src) ||
		cacheRubyTriggerRE.MatchString(src)
}

func (c *cacheEvictionDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Spring @Cacheable / @CacheEvict / @CachePut
	for idx, m := range cacheSpringRE.FindAllStringSubmatchIndex(src, -1) {
		ann := src[m[0]:m[1]]
		cacheName := "default"
		if nm := cacheSpringNameRE.FindStringSubmatch(ann); nm != nil {
			cacheName = nm[1]
		}
		key := fmt.Sprintf("spring:%s:%d", cacheName, idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cache_eviction_spring_%s", cacheName),
			"SCOPE.Pattern", "cache_eviction", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "cache_eviction", "framework": "spring", "cache_name": cacheName}))
	}

	// Redis TTL
	for idx, m := range cacheRedisTTLRE.FindAllStringSubmatchIndex(src, -1) {
		ttl := ""
		if m[2] >= 0 {
			ttl = src[m[2]:m[3]]
		}
		key := fmt.Sprintf("redis:ttl:%s:%d", ttl, idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("cache_eviction_redis_%d", idx),
			"SCOPE.Pattern", "cache_eviction", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "cache_eviction", "framework": "redis", "ttl_seconds": ttl}))
	}

	// .NET IMemoryCache / IDistributedCache
	if cacheDotNetTriggerRE.MatchString(src) {
		key := "dotnet:cache:0"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"cache_eviction_dotnet", "SCOPE.Pattern", "cache_eviction", language, 1,
				map[string]string{"kind": "cache_eviction", "framework": "aspnet"}))
		}
	}

	return results
}

func init() {
	Register(&cacheEvictionDetector{})
}
