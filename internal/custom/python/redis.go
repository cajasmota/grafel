package python

import (
	"context"
	"fmt"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_redis", &RedisExtractor{})
}

// RedisExtractor extracts Redis usage patterns: client connections, cache
// operations, pub/sub, streams, Lua scripts, queue patterns, and rate limiting.
type RedisExtractor struct{}

func (e *RedisExtractor) Language() string { return "python_redis" }

var (
	rdPyClientRe = regexp.MustCompile(
		`(?:redis\.Redis|redis\.StrictRedis|Redis\.from_url|aioredis\.from_url|aioredis\.create_redis_pool|Redis)\s*\(`)
	rdCacheOpRe = regexp.MustCompile(
		`(?:\.|->)(?:get|set|hget|hset|hgetall|hmset|hmget|setex|setnx|getset|mget|mset|expire|ttl|pttl|persist|delete|exists|incr|decr|incrby|decrby|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zrank)\s*\(`)
	rdPubSubRe = regexp.MustCompile(
		`(?:\.|->)(?:publish|subscribe|psubscribe|pubsub)\s*\(`)
	rdStreamRe = regexp.MustCompile(
		`(?i)(?:\.|->)(?:xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xinfo|xtrim|xdel|xclaim|xpending)\s*\(`)
	rdLuaRe = regexp.MustCompile(
		`(?:\.|->)(?:eval|evalsha|script_load|register_script)\s*\(`)
	rdRateLimitRe = regexp.MustCompile(
		`(?:incr|INCR)\s*\([^)]*\).*(?:expire|EXPIRE)\s*\(`)
)

func (e *RedisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_redis")
	_, span := tracer.Start(ctx, "custom.python_redis")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord
	seenLines := make(map[string]map[int]bool)

	getSeen := func(cat string) map[int]bool {
		if seenLines[cat] == nil {
			seenLines[cat] = make(map[int]bool)
		}
		return seenLines[cat]
	}

	// 1. Client connections
	for _, idx := range allMatchesIndex(rdPyClientRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("client")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_client:%s:%d", file.Path, line),
			"SCOPE.Service", "", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "client", "language": "python"}))
	}

	// 2. Cache operations
	for _, idx := range allMatchesIndex(rdCacheOpRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("cache")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_cache:%s:%d", file.Path, line),
			"SCOPE.Operation", "cache_op", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "cache_op", "language": "python"}))
	}

	// 3. Pub/Sub
	for _, idx := range allMatchesIndex(rdPubSubRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("pubsub")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_pubsub:%s:%d", file.Path, line),
			"SCOPE.Operation", "pubsub", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "pubsub", "language": "python"}))
	}

	// 4. Streams
	for _, idx := range allMatchesIndex(rdStreamRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("stream")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_stream:%s:%d", file.Path, line),
			"SCOPE.Operation", "stream_op", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "stream_op", "language": "python"}))
	}

	// 5. Lua scripts
	for _, idx := range allMatchesIndex(rdLuaRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("lua")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_lua:%s:%d", file.Path, line),
			"SCOPE.Operation", "lua_script", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "lua_script", "language": "python"}))
	}

	// 6. Rate limiting patterns
	for _, idx := range allMatchesIndex(rdRateLimitRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("rate_limit")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("redis_rate_limit:%s:%d", file.Path, line),
			"SCOPE.Pattern", "rate_limit", file.Path, line,
			map[string]string{"framework": "redis", "pattern_type": "rate_limit", "language": "python"}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
