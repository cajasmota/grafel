package golang

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

// redis_driver.go: coarse key-schema + command-DSL extractor for the go-redis
// client (github.com/redis/go-redis, formerly github.com/go-redis/redis).
//
// Redis is a schema-less key/value store, so the honest coverage shape is:
//
//   - Models / Schema — partial. Redis has no declared schema, but key naming
//                    conventions encode a coarse one: a string literal that
//                    looks like a namespaced key pattern ("user:%d",
//                    "session:{id}", "cache:user:profile") is surfaced as a
//                    SCOPE.Schema keyspace whose prefix is the logical "type".
//                    This is purely heuristic (a key pattern is a convention,
//                    not an enforced schema), hence partial.
//   - Queries      — partial. Command call sites (Get/Set/HSet/HGet/LPush/
//                    SAdd/Incr/Expire/Del/...) are captured with the command
//                    verb. Binding a command to a concrete key from a regex is
//                    not reliable, so this stays partial.
//   - Relationships— honesty-NA. Redis has no relational layer. Recorded as
//                    not_applicable in the registry (no code claim).
//   - Migrations   — honesty-NA. Redis has no schema and no migration concept.
//
// The extractor gates on the go-redis import actually being present, so a
// file that merely mentions "redis" without the client is not poached.

func init() {
	extractor.Register("custom_go_redis_driver", &redisDriverExtractor{})
}

type redisDriverExtractor struct{}

func (e *redisDriverExtractor) Language() string { return "custom_go_redis_driver" }

var (
	// Import marker for go-redis (current and legacy import paths).
	reImportRedis = regexp.MustCompile(`"github\.com/(?:redis/go-redis|go-redis/redis)(?:/v\d+)?(?:/redis)?"`)

	// A namespaced key pattern string literal: one or more ":"-separated
	// segments where the first segment is a plain identifier. The capture is
	// the full literal; the logical type is its leading segment.
	//   "user:%d"  "session:{id}"  "cache:user:profile"
	reRedisKeyPattern = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*:[^"]+)"`)

	// go-redis command call sites. The verb is captured so query_type can be
	// stamped. Covers the common string/hash/list/set/key-mgmt surface.
	reRedisCommand = regexp.MustCompile(
		`(?m)\.(GetSet|GetEx|GetDel|Get|SetNX|SetEX|SetXX|Set|MGet|MSet|Append|Incr|IncrBy|Decr|DecrBy|HGetAll|HMGet|HMSet|HGet|HSet|HSetNX|HDel|HExists|LPush|RPush|LPop|RPop|LRange|LLen|SAdd|SRem|SMembers|SIsMember|ZAdd|ZRange|ZScore|ZRem|Expire|ExpireAt|TTL|Persist|Exists|Del|Unlink|Type|Keys|Scan)\s*\(`,
	)
)

func (e *redisDriverExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.redis_driver_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "redis"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportRedis.MatchString(src) {
		// No go-redis import: not our file.
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

	// 1. Coarse schema: namespaced key-pattern literals => keyspaces, keyed by
	//    the leading prefix segment (the logical "type"). Dedup by prefix so a
	//    keyspace appears once even when many patterns share it.
	for _, m := range reRedisKeyPattern.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		prefix := pattern[:strings.IndexByte(pattern, ':')]
		ent := makeEntity("keyspace:"+prefix, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redis", "provenance", "INFERRED_FROM_REDIS_KEY_PATTERN",
			"keyspace_prefix", prefix, "key_pattern", pattern)
		add(ent)
	}

	// 2. Queries: command call sites. Heuristic — captures the command verb
	//    but cannot bind to a concrete key from a regex.
	for _, m := range reRedisCommand.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("redis:"+verb+":"+lineToken(src, m[0]), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "redis", "provenance", "INFERRED_FROM_REDIS_COMMAND",
			"query_type", redisVerbKind(verb), "call_verb", verb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// redisVerbKind classifies a go-redis command into a coarse read/write verb so
// the query_type property is comparable across the data-access extractors.
func redisVerbKind(verb string) string {
	switch {
	case strings.HasPrefix(verb, "Get"), strings.HasPrefix(verb, "MGet"),
		strings.HasPrefix(verb, "HGet"), strings.HasPrefix(verb, "HMGet"),
		strings.HasPrefix(verb, "LRange"), strings.HasPrefix(verb, "LLen"),
		strings.HasPrefix(verb, "LPop"), strings.HasPrefix(verb, "RPop"),
		strings.HasPrefix(verb, "SMembers"), strings.HasPrefix(verb, "SIsMember"),
		strings.HasPrefix(verb, "ZRange"), strings.HasPrefix(verb, "ZScore"),
		strings.HasPrefix(verb, "HExists"), verb == "Exists", verb == "TTL",
		verb == "Type", verb == "Keys", verb == "Scan", verb == "HGetAll":
		return "read"
	case verb == "Del", verb == "Unlink", verb == "HDel", verb == "SRem", verb == "ZRem":
		return "delete"
	default:
		// Set/HSet/LPush/SAdd/ZAdd/Incr/Expire/... are writes.
		return "write"
	}
}
