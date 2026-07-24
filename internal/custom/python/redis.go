package python

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("python_redis", &RedisExtractor{})
}

// RedisExtractor extracts Redis usage patterns: client connections, cache
// operations, pub/sub, streams, Lua scripts, queue patterns, and rate limiting.
//
// Beyond detecting the operation *category* (get/set/publish/xadd/…) it also
// extracts the KEY / channel / stream the call touches — the crown-jewel
// data-access detail (#3643, epic #3625). For each keyed op it emits a
// SCOPE.Datastore keyspace target entity and an access edge (READS_FROM /
// WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO) from the operation to that target,
// mirroring the JS driver-layer shape (scanJSRedis in
// internal/engine/orm_queries_jsts_drivers.go) so the data-access graph is
// traversable and cross-language consistent.
//
// Key topology:
//   - literal key          r.get("session:abc")    -> key "session:abc"
//   - string-concat prefix  r.set("user:" + id, v)  -> key_prefix "user:*"
//   - f-string prefix       r.get(f"session:{sid}") -> key_prefix "session:*"
//   - fully-dynamic key     r.get(k)                -> op only, no key (honest partial)
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

	// rdCacheKeyRe captures the command verb (group 1) and its first string-
	// literal key argument (group 2). The verb drives the read/write edge
	// direction; the key is the data-access target.
	rdCacheKeyRe = regexp.MustCompile(
		`(?:\.|->)(get|set|hget|hset|hgetall|hmset|hmget|setex|setnx|getset|mget|mset|expire|ttl|pttl|persist|delete|exists|incr|decr|incrby|decrby|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zrank)\s*\(\s*` + rdKeyArg)

	// rdPubSubKeyRe captures the pub/sub verb (group 1) and the channel
	// literal (group 2).
	rdPubSubKeyRe = regexp.MustCompile(
		`(?:\.|->)(publish|subscribe|psubscribe)\s*\(\s*` + rdKeyArg)

	// rdStreamKeyRe captures the stream verb (group 1) and the stream key
	// literal (group 2).
	rdStreamKeyRe = regexp.MustCompile(
		`(?i)(?:\.|->)(xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xtrim|xdel|xclaim|xpending)\s*\(\s*` + rdKeyArg)
)

// rdKeyArg matches a Redis key/channel/stream first-argument literal and
// captures the literal body in group 2 (relative to the verb capture in
// group 1). It accepts:
//   - a plain string literal          "session:abc"   /  'events'
//   - an f-string                     f"user:{uid}"
//
// The surrounding quote style is normalised away; prefix/dynamic handling is
// done by normaliseRedisKey.
const rdKeyArg = `(f?["'])([^"']*)["']`

// redisKeyAccess captures the resolved key topology for one keyed op.
type redisKeyAccess struct {
	key       string // concrete key when fully literal, else ""
	keyPrefix string // "user:*" style namespace when prefixed/dynamic-suffix
	dynamic   bool   // true when no static key/prefix could be resolved
}

// normaliseRedisKey turns a captured first-argument literal (and the raw
// source window after it) into a redisKeyAccess.
//
//	"session:abc"            -> key "session:abc"
//	"user:" (followed by +x) -> key_prefix "user:*"
//	f"session:{sid}"         -> key_prefix "session:*"  (literal head before {)
//	f"{tenant}:cfg"          -> dynamic (no static head)
//	""                       -> dynamic
func normaliseRedisKey(quote, literal, after string) redisKeyAccess {
	isFString := strings.HasPrefix(quote, "f")

	if isFString {
		// f-string: take the static head before the first interpolation `{`.
		// A literal with no `{` is effectively a constant key.
		if i := strings.IndexByte(literal, '{'); i >= 0 {
			head := literal[:i]
			if head == "" {
				return redisKeyAccess{dynamic: true}
			}
			return redisKeyAccess{keyPrefix: keyspaceGlob(head)}
		}
		if literal == "" {
			return redisKeyAccess{dynamic: true}
		}
		return redisKeyAccess{key: literal}
	}

	if literal == "" {
		return redisKeyAccess{dynamic: true}
	}

	// Plain string literal. If the very next non-space source token is a `+`
	// concatenation, this literal is a key *prefix* (`"user:" + id`).
	rest := strings.TrimLeft(after, " \t")
	if strings.HasPrefix(rest, "+") {
		return redisKeyAccess{keyPrefix: keyspaceGlob(literal)}
	}
	return redisKeyAccess{key: literal}
}

// keyspaceGlob renders a key prefix as a `prefix*` glob, collapsing a trailing
// separator so `"user:"` -> `user:*` and `"user"` -> `user*`.
func keyspaceGlob(prefix string) string {
	return prefix + "*"
}

// rdReadVerbs is the set of cache/list/set/hash verbs that only read.
var rdReadVerbs = map[string]bool{
	"get": true, "hget": true, "hgetall": true, "hmget": true, "getset": true,
	"mget": true, "ttl": true, "pttl": true, "exists": true, "lrange": true,
	"llen": true, "smembers": true, "sismember": true, "zrange": true,
	"zrangebyscore": true, "zrank": true, "lpop": true, "rpop": true,
}

// cacheEdgeKind returns READS_FROM for pure-read verbs, WRITES_TO otherwise.
func cacheEdgeKind(verb string) string {
	if rdReadVerbs[strings.ToLower(verb)] {
		return string(types.RelationshipKindReadsFrom)
	}
	return string(types.RelationshipKindWritesTo)
}

// pubsubEdgeKind maps publish -> PUBLISHES_TO, (p)subscribe -> SUBSCRIBES_TO.
func pubsubEdgeKind(verb string) string {
	if strings.HasPrefix(strings.ToLower(verb), "sub") || strings.HasPrefix(strings.ToLower(verb), "psub") {
		return string(types.RelationshipKindSubscribesTo)
	}
	return string(types.RelationshipKindPublishesTo)
}

// streamEdgeKind maps producing stream verbs to WRITES_TO and consuming/
// inspecting verbs to READS_FROM.
func streamEdgeKind(verb string) string {
	switch strings.ToLower(verb) {
	case "xadd":
		return string(types.RelationshipKindWritesTo)
	case "xtrim", "xdel", "xack", "xclaim":
		return string(types.RelationshipKindWritesTo)
	default:
		// xread, xreadgroup, xlen, xrange, xrevrange, xinfo, xpending
		return string(types.RelationshipKindReadsFrom)
	}
}

// redisKeyspaceRef builds the stable ToID for a keyspace/channel/stream target
// node so multiple call-sites that touch the same key converge on one node.
func redisKeyspaceRef(keyspace string) string {
	return fmt.Sprintf("Datastore:redis:%s", keyspace)
}

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
	seenKeyspace := make(map[string]bool)

	getSeen := func(cat string) map[int]bool {
		if seenLines[cat] == nil {
			seenLines[cat] = make(map[int]bool)
		}
		return seenLines[cat]
	}

	// emitKeyspace adds one SCOPE.Datastore target node per distinct keyspace
	// (deduplicated across the file). Returns the ToID for the access edge.
	emitKeyspace := func(label, keyType string, line int) string {
		ref := redisKeyspaceRef(label)
		if !seenKeyspace[ref] {
			seenKeyspace[ref] = true
			out = append(out, entity(ref,
				"SCOPE.Datastore", keyType, file.Path, line,
				map[string]string{
					"framework":    "redis",
					"pattern_type": "keyspace",
					"keyspace":     label,
					"key_type":     keyType,
					"language":     "python",
				}))
		}
		return ref
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

	// 2. Cache operations (with key topology)
	keyedCache := keyedRedisOps(source, rdCacheKeyRe)
	for _, idx := range allMatchesIndex(rdCacheOpRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("cache")
		if seen[line] {
			continue
		}
		seen[line] = true
		props := map[string]string{"framework": "redis", "pattern_type": "cache_op", "language": "python"}
		ent := entity(fmt.Sprintf("redis_cache:%s:%d", file.Path, line),
			"SCOPE.Operation", "cache_op", file.Path, line, props)
		if op, ok := keyedCache[line]; ok {
			label := applyKeyProps(props, op.access)
			if label != "" {
				keyType := "key"
				if op.access.keyPrefix != "" {
					keyType = "key_prefix"
				}
				ref := emitKeyspace(label, keyType, line)
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: ref,
					Kind: cacheEdgeKind(op.verb),
					Properties: types.Props{
						{K: "framework", V: "redis"},
						{K: "keyspace", V: label},
						{K: "op", V: strings.ToLower(op.verb)},
					},
				})
			}
		}
		out = append(out, ent)
	}

	// 3. Pub/Sub (with channel topology)
	keyedPubSub := keyedRedisOps(source, rdPubSubKeyRe)
	for _, idx := range allMatchesIndex(rdPubSubRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("pubsub")
		if seen[line] {
			continue
		}
		seen[line] = true
		props := map[string]string{"framework": "redis", "pattern_type": "pubsub", "language": "python"}
		ent := entity(fmt.Sprintf("redis_pubsub:%s:%d", file.Path, line),
			"SCOPE.Operation", "pubsub", file.Path, line, props)
		if op, ok := keyedPubSub[line]; ok {
			label := applyChannelProps(props, op.access)
			if label != "" {
				keyType := "channel"
				if op.access.keyPrefix != "" {
					keyType = "channel_prefix"
				}
				ref := emitKeyspace(label, keyType, line)
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: ref,
					Kind: pubsubEdgeKind(op.verb),
					Properties: types.Props{
						{K: "channel", V: label},
						{K: "framework", V: "redis"},
						{K: "op", V: strings.ToLower(op.verb)},
					},
				})
			}
		}
		out = append(out, ent)
	}

	// 4. Streams (with stream topology)
	keyedStream := keyedRedisOps(source, rdStreamKeyRe)
	for _, idx := range allMatchesIndex(rdStreamRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("stream")
		if seen[line] {
			continue
		}
		seen[line] = true
		props := map[string]string{"framework": "redis", "pattern_type": "stream_op", "language": "python"}
		ent := entity(fmt.Sprintf("redis_stream:%s:%d", file.Path, line),
			"SCOPE.Operation", "stream_op", file.Path, line, props)
		if op, ok := keyedStream[line]; ok {
			label := applyStreamProps(props, op.access)
			if label != "" {
				keyType := "stream"
				if op.access.keyPrefix != "" {
					keyType = "stream_prefix"
				}
				ref := emitKeyspace(label, keyType, line)
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: ref,
					Kind: streamEdgeKind(op.verb),
					Properties: types.Props{
						{K: "framework", V: "redis"},
						{K: "op", V: strings.ToLower(op.verb)},
						{K: "stream", V: label},
					},
				})
			}
		}
		out = append(out, ent)
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

// keyedOp pairs a resolved key access with its command verb, keyed by line.
type keyedOp struct {
	verb   string
	access redisKeyAccess
}

// keyedRedisOps runs a key-capturing regex over the source and returns a
// line -> keyedOp map (first keyed op per line wins, matching the per-line
// dedup of the category scan).
func keyedRedisOps(source string, re *regexp.Regexp) map[int]keyedOp {
	res := make(map[int]keyedOp)
	for _, m := range re.FindAllStringSubmatchIndex(source, -1) {
		// groups: 0-1 full, 2-3 verb, 4-5 quote, 6-7 literal
		if len(m) < 8 {
			continue
		}
		line := lineOf(source, m[0])
		if _, ok := res[line]; ok {
			continue
		}
		verb := source[m[2]:m[3]]
		quote := source[m[4]:m[5]]
		literal := ""
		if m[6] >= 0 {
			literal = source[m[6]:m[7]]
		}
		after := ""
		if m[1] < len(source) {
			end := m[1] + 8
			if end > len(source) {
				end = len(source)
			}
			after = source[m[1]:end]
		}
		access := normaliseRedisKey(quote, literal, after)
		res[line] = keyedOp{verb: verb, access: access}
	}
	return res
}

// applyKeyProps writes key / key_prefix props for cache ops and returns the
// keyspace label (concrete key or glob), or "" when fully dynamic.
func applyKeyProps(props map[string]string, a redisKeyAccess) string {
	switch {
	case a.key != "":
		props["key"] = a.key
		return a.key
	case a.keyPrefix != "":
		props["key_prefix"] = a.keyPrefix
		return a.keyPrefix
	default:
		props["key"] = "<dynamic>"
		return ""
	}
}

// applyChannelProps writes channel / channel_prefix props for pub/sub ops.
func applyChannelProps(props map[string]string, a redisKeyAccess) string {
	switch {
	case a.key != "":
		props["channel"] = a.key
		return a.key
	case a.keyPrefix != "":
		props["channel_prefix"] = a.keyPrefix
		return a.keyPrefix
	default:
		props["channel"] = "<dynamic>"
		return ""
	}
}

// applyStreamProps writes stream / stream_prefix props for stream ops.
func applyStreamProps(props map[string]string, a redisKeyAccess) string {
	switch {
	case a.key != "":
		props["stream"] = a.key
		return a.key
	case a.keyPrefix != "":
		props["stream_prefix"] = a.keyPrefix
		return a.keyPrefix
	default:
		props["stream"] = "<dynamic>"
		return ""
	}
}
