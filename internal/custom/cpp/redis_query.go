package cpp

// redis_query.go — redis-plus-plus (sw::redis) C++ query attribution extractor.
//
// Covered DSL surfaces (partial — heuristic regex; no AST):
//
//  query_attribution:   Attributes Redis KV commands (GET, SET, HSET, HGET,
//                       LPUSH, RPUSH, SADD, ZADD, DEL, EXISTS, EXPIRE,
//                       INCR/DECR, MGET/MSET, PIPELINE exec) to call sites.
//
//                       redis-plus-plus API: redis.get("key"), redis.set("key", val),
//                       redis.hset("hash", "field", val), redis.command("CMD", ...),
//                       pipeline.set/get/hset, transaction.set/get.
//
// Status: partial — regex/heuristic; no AST; does not resolve key expressions,
// only literals and method names.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_redis_query", &redisQueryExtractor{})
}

type redisQueryExtractor struct{}

func (e *redisQueryExtractor) Language() string { return "custom_cpp_redis_query" }

var (
	// Gate: must include redis-plus-plus header.
	reRedisPPInclude = regexp.MustCompile(`#\s*include\s+[<"](sw/redis\+\+/[^>"]*|redis\+\+\.h|redis/[^>"]*)[>"]`)

	// Method-call attribution: redis.get("key") / redis->set("key", val) etc.
	// Captures: (1) method name, (2) optional first string argument (the key)
	reRedisPPMethod = regexp.MustCompile(
		`\b(?:\w+)\s*(?:->|\.)\s*(get|set|hset|hget|hmset|hmget|hgetall|hdel|hexists|` +
			`lpush|rpush|lpop|rpop|lrange|llen|` +
			`sadd|srem|smembers|scard|sismember|` +
			`zadd|zrange|zrangebyscore|zrem|zscore|zcard|` +
			`del|exists|expire|ttl|persist|` +
			`incr|decr|incrby|decrby|` +
			`mget|mset|keys|scan|` +
			`publish|subscribe|psubscribe|` +
			`eval|evalsha)\s*\(\s*(?:"([^"]*)")?`,
	)

	// redis.command("CMD", ...) or redis->command("CMD", ...)
	reRedisPPCommand = regexp.MustCompile(
		`\b(?:\w+)\s*(?:->|\.)\s*command\s*\(\s*"([A-Z][A-Z0-9_]*)"`,
	)

	// Pipeline/transaction: pipe.set / pipe.get / tx.set / tx.hset etc.
	reRedisPPPipeline = regexp.MustCompile(
		`\b(?:pipe|pipeline|tx|transaction|multi)\s*(?:->|\.)\s*(set|get|hset|hget|del|incr|lpush|rpush|zadd|sadd)\s*\(`,
	)
)

// redisCommandFromMethod maps redis-plus-plus method names to canonical Redis commands.
var redisCommandFromMethod = map[string]string{
	"get": "GET", "set": "SET",
	"hset": "HSET", "hget": "HGET", "hmset": "HMSET", "hmget": "HMGET",
	"hgetall": "HGETALL", "hdel": "HDEL", "hexists": "HEXISTS",
	"lpush": "LPUSH", "rpush": "RPUSH", "lpop": "LPOP", "rpop": "RPOP",
	"lrange": "LRANGE", "llen": "LLEN",
	"sadd": "SADD", "srem": "SREM", "smembers": "SMEMBERS", "scard": "SCARD",
	"sismember": "SISMEMBER",
	"zadd":      "ZADD", "zrange": "ZRANGE", "zrangebyscore": "ZRANGEBYSCORE",
	"zrem": "ZREM", "zscore": "ZSCORE", "zcard": "ZCARD",
	"del": "DEL", "exists": "EXISTS", "expire": "EXPIRE", "ttl": "TTL",
	"persist": "PERSIST",
	"incr":    "INCR", "decr": "DECR", "incrby": "INCRBY", "decrby": "DECRBY",
	"mget": "MGET", "mset": "MSET", "keys": "KEYS", "scan": "SCAN",
	"publish": "PUBLISH", "subscribe": "SUBSCRIBE", "psubscribe": "PSUBSCRIBE",
	"eval": "EVAL", "evalsha": "EVALSHA",
}

func (e *redisQueryExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.redis_query_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "redis-plus-plus"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}

	src := string(file.Content)
	if !reRedisPPInclude.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Method-call attribution
	for _, m := range reRedisPPMethod.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToLower(strings.TrimSpace(src[m[2]:m[3]]))
		cmd := redisCommandFromMethod[method]
		if cmd == "" {
			cmd = strings.ToUpper(method)
		}
		keyLiteral := ""
		if m[4] >= 0 {
			keyLiteral = src[m[4]:m[5]]
		}
		name := "redis:" + cmd
		if keyLiteral != "" {
			name += ":\"" + keyLiteral + "\""
		}
		name += "@L" + lineStr(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redis-plus-plus", "provenance", "INFERRED_FROM_REDIS_PP_METHOD",
			"redis_command", cmd, "method_name", method)
		if keyLiteral != "" {
			setProps(&ent, "key_literal", keyLiteral)
		}
		add(ent)
	}

	// redis.command("CMD", ...) arbitrary command
	for _, m := range reRedisPPCommand.FindAllStringSubmatchIndex(src, -1) {
		cmd := strings.ToUpper(strings.TrimSpace(src[m[2]:m[3]]))
		name := "redis:command:" + cmd + "@L" + lineStr(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redis-plus-plus", "provenance", "INFERRED_FROM_REDIS_PP_COMMAND",
			"redis_command", cmd)
		add(ent)
	}

	// Pipeline / transaction calls
	for _, m := range reRedisPPPipeline.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToLower(strings.TrimSpace(src[m[2]:m[3]]))
		cmd := redisCommandFromMethod[method]
		if cmd == "" {
			cmd = strings.ToUpper(method)
		}
		name := "redis:pipeline:" + cmd + "@L" + lineStr(src, m[0])
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redis-plus-plus", "provenance", "INFERRED_FROM_REDIS_PP_PIPELINE",
			"redis_command", cmd, "context", "pipeline")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// lineStr returns the 1-based line number as a string for use in entity names.
func lineStr(source string, offset int) string {
	n := lineOf(source, offset)
	s := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
