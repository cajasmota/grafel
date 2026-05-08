package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// redisKeyExtractor extracts Redis cache key patterns.
// Matches Python redis_key_extractor.py.
type redisKeyExtractor struct{}

var (
	redisPythonRE = regexp.MustCompile(`(?:[a-zA-Z_]\w*)\.(get|set|delete|expire|setex)\(f?["']([^"']+)["']`)
	redisNodeRE   = regexp.MustCompile("(?:client|redis)\\.(get|set|del|expire|setex)\\s*\\(\\s*(?:[\"'`]([^\"'`]+)[\"'`])")
	redisJavaRE   = regexp.MustCompile(`(?:jedis|redis)\.(get|set|del|expire|setex)\s*\(\s*["']([^"']+)["']`)
	redisGoRE     = regexp.MustCompile(`rdb\.(Get|Set|Del|Expire|SetEX)\s*\(\s*ctx\s*,\s*["']([^"']+)["']`)
)

var redisPythonSignals = []string{"import redis", "from redis"}
var redisNodeSignals = []string{"require('ioredis')", `require("ioredis")`}
var redisJavaSignals = []string{"import redis.clients.jedis"}
var redisGoSignals = []string{`"github.com/redis/go-redis`}

func detectRedisClient(src string) (clientLib, lang string) {
	// Java before Python (to avoid false positive on "import redis" substring)
	for _, sig := range redisJavaSignals {
		if strings.Contains(src, sig) {
			return "jedis", "java"
		}
	}
	for _, sig := range redisGoSignals {
		if strings.Contains(src, sig) {
			return "go-redis", "go"
		}
	}
	for _, sig := range redisPythonSignals {
		if strings.Contains(src, sig) {
			return "redis-py", "python"
		}
	}
	for _, sig := range redisNodeSignals {
		if strings.Contains(src, sig) {
			return "ioredis", "javascript"
		}
	}
	return "", ""
}

func (r *redisKeyExtractor) Category() string { return "cache_key" }

func (r *redisKeyExtractor) AppliesTo(src string) bool {
	lib, _ := detectRedisClient(src)
	return lib != ""
}

func (r *redisKeyExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	clientLib, _ := detectRedisClient(src)
	if clientLib == "" {
		return nil
	}

	var results []types.EntityRecord
	seen := map[string]bool{}

	var re *regexp.Regexp
	switch clientLib {
	case "redis-py":
		re = redisPythonRE
	case "ioredis":
		re = redisNodeRE
	case "jedis":
		re = redisJavaRE
	case "go-redis":
		re = redisGoRE
	default:
		return nil
	}

	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		op := strings.ToLower(src[m[2]:m[3]])
		keyPattern := ""
		if m[4] >= 0 {
			keyPattern = src[m[4]:m[5]]
		} else if m[6] >= 0 {
			keyPattern = src[m[6]:m[7]]
		}
		if keyPattern == "" {
			continue
		}
		dedupeKey := keyPattern + ":" + op
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true
		results = append(results, makeEntity(filePath,
			"cache_key_"+keyPattern+"_"+op, "SCOPE.Pattern", "cache_key", language,
			lineOf(src, m[0]),
			map[string]string{
				"kind":           "cache_key",
				"key_pattern":    keyPattern,
				"operation":      op,
				"client_library": clientLib,
			}))
	}

	return results
}

func init() {
	Register(&redisKeyExtractor{})
}
