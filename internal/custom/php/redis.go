package php

// redis.go — predis / phpredis key/channel/stream topology extractor
// (#3625 epic, child of the Python template #3668).
//
// Both predis and the phpredis C-extension expose lowercase command verbs on a
// client object whose FIRST argument is the key / channel / stream:
//
//	$redis->get("session:abc")        -> READS_FROM key  "session:abc"
//	$redis->set("user:".$id, $v)      -> WRITES_TO  key-prefix "user:*"
//	$redis->hget("cfg:flags", $f)     -> READS_FROM key  "cfg:flags"
//	$redis->del($k)                   -> op only (dynamic, honest-partial)
//	$redis->publish("events", $p)     -> PUBLISHES_TO channel "events"
//	$redis->subscribe(["events"], $h) -> SUBSCRIBES_TO channel "events"
//	$redis->xadd("orders", ...)       -> WRITES_TO  stream "orders"
//	$redis->xread(...)                -> READS_FROM stream "orders"
//
// For each keyed op we emit a SCOPE.Datastore keyspace target node
// (Name == "Datastore:redis:<label>", deduplicated per file) and an access
// edge (READS_FROM / WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO) from the op site
// to that target — mirroring the Python template (#3668).
//
// Key topology (honest-partial for dynamic keys — no fabricated key):
//
//	"session:abc" / 'session:abc'  -> key  "session:abc"
//	"user:".$id                    -> key-prefix "user:*"  (`.`-concat head)
//	"user:$id" / "user:{$id}"      -> key-prefix "user:*"  (interp head)
//	"$tenant:cfg" / $k             -> dynamic (no static head, no key emitted)

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_redis", &phpRedisExtractor{})
}

type phpRedisExtractor struct{}

func (e *phpRedisExtractor) Language() string { return "custom_php_redis" }

// phpRedisKeyArg matches the first key argument literal: a single- or
// double-quoted string (optionally inside a `[` for subscribe array arg).
// Group 1 = verb, group 2 = quote char, group 3 = literal body.
const phpRedisKeyArg = `\[?\s*(["'])([^"']*)["']`

var (
	phpRedisCacheRe = regexp.MustCompile(
		`->(get|set|setex|setnx|getset|mget|mset|hget|hgetall|hset|hmget|hmset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|incrby|decrby|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*\(\s*` + phpRedisKeyArg)
	phpRedisCacheAllRe = regexp.MustCompile(
		`->(get|set|setex|setnx|getset|mget|mset|hget|hgetall|hset|hmget|hmset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|incrby|decrby|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*\(`)

	phpRedisPubSubRe = regexp.MustCompile(
		`->(publish|subscribe|psubscribe)\s*\(\s*` + phpRedisKeyArg)
	phpRedisPubSubAllRe = regexp.MustCompile(
		`->(publish|subscribe|psubscribe)\s*\(`)

	phpRedisStreamRe = regexp.MustCompile(
		`(?i)->(xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*\(\s*` + phpRedisKeyArg)
	phpRedisStreamAllRe = regexp.MustCompile(
		`(?i)->(xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*\(`)
)

var phpRedisReadVerbs = map[string]bool{
	"get": true, "hget": true, "hgetall": true, "hmget": true, "hexists": true,
	"getset": true, "mget": true, "ttl": true, "pttl": true, "exists": true,
	"lrange": true, "llen": true, "smembers": true, "sismember": true,
	"zrange": true, "zrangebyscore": true, "zscore": true, "lpop": true, "rpop": true,
}

func phpRedisCacheEdge(verb string) string {
	if phpRedisReadVerbs[strings.ToLower(verb)] {
		return string(types.RelationshipKindReadsFrom)
	}
	return string(types.RelationshipKindWritesTo)
}

func phpRedisPubSubEdge(verb string) string {
	v := strings.ToLower(verb)
	if strings.HasPrefix(v, "sub") || strings.HasPrefix(v, "psub") {
		return string(types.RelationshipKindSubscribesTo)
	}
	return string(types.RelationshipKindPublishesTo)
}

func phpRedisStreamEdge(verb string) string {
	switch strings.ToLower(verb) {
	case "xadd", "xtrim", "xdel", "xack":
		return string(types.RelationshipKindWritesTo)
	default:
		return string(types.RelationshipKindReadsFrom)
	}
}

func phpRedisKeyspaceRef(label string) string {
	return fmt.Sprintf("Datastore:redis:%s", label)
}

type phpRedisAccess struct {
	key       string
	keyPrefix string
	dynamic   bool
}

// phpNormaliseRedisKey resolves a captured (quote, literal, after) into a
// phpRedisAccess. Single-quoted strings do NOT interpolate in PHP, so only
// double-quoted literals are checked for `$` interpolation; `after` detects a
// `.`-concatenation prefix.
func phpNormaliseRedisKey(quote, literal, after string) phpRedisAccess {
	if quote == `"` {
		// Double-quoted: `$var` or `{$var}` interpolates. Take static head.
		if i := phpInterpIndex(literal); i >= 0 {
			head := literal[:i]
			if head == "" {
				return phpRedisAccess{dynamic: true}
			}
			return phpRedisAccess{keyPrefix: head + "*"}
		}
	}
	if literal == "" {
		return phpRedisAccess{dynamic: true}
	}
	if strings.HasPrefix(strings.TrimLeft(after, " \t"), ".") {
		return phpRedisAccess{keyPrefix: literal + "*"}
	}
	return phpRedisAccess{key: literal}
}

// phpInterpIndex returns the index of the first interpolation marker (`$` or
// `{$`) in a double-quoted literal body, or -1 if none.
func phpInterpIndex(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '$' {
			return i
		}
		if s[i] == '{' && i+1 < len(s) && s[i+1] == '$' {
			return i
		}
	}
	return -1
}

type phpKeyedRedisOp struct {
	verb   string
	access phpRedisAccess
}

func phpKeyedRedisOps(src string, re *regexp.Regexp) map[int]phpKeyedRedisOp {
	res := make(map[int]phpKeyedRedisOp)
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		// groups: 0-1 full, 2-3 verb, 4-5 quote, 6-7 literal
		if len(m) < 8 {
			continue
		}
		line := lineOf(src, m[0])
		if _, ok := res[line]; ok {
			continue
		}
		verb := src[m[2]:m[3]]
		quote := src[m[4]:m[5]]
		literal := ""
		if m[6] >= 0 {
			literal = src[m[6]:m[7]]
		}
		after := ""
		if m[1] < len(src) {
			end := m[1] + 8
			if end > len(src) {
				end = len(src)
			}
			after = src[m[1]:end]
		}
		res[line] = phpKeyedRedisOp{verb: verb, access: phpNormaliseRedisKey(quote, literal, after)}
	}
	return res
}

func phpRedisLabel(e *types.EntityRecord, a phpRedisAccess, keyProp, prefixProp string) string {
	switch {
	case a.key != "":
		setProps(e, keyProp, a.key)
		return a.key
	case a.keyPrefix != "":
		setProps(e, prefixProp, a.keyPrefix)
		return a.keyPrefix
	default:
		setProps(e, keyProp, "<dynamic>", "dynamic", "true")
		return ""
	}
}

func (e *phpRedisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.php_redis")
	_, span := tracer.Start(ctx, "custom.php_redis")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "Redis") && !strings.Contains(src, "redis") &&
		!strings.Contains(src, "predis") && !strings.Contains(src, "Predis") {
		return nil, nil
	}

	var out []types.EntityRecord
	seenKeyspace := make(map[string]bool)
	seenLine := map[string]map[int]bool{}
	getSeen := func(cat string) map[int]bool {
		if seenLine[cat] == nil {
			seenLine[cat] = map[int]bool{}
		}
		return seenLine[cat]
	}

	emitKeyspace := func(label, keyType string, line int) string {
		ref := phpRedisKeyspaceRef(label)
		if !seenKeyspace[ref] {
			seenKeyspace[ref] = true
			ent := makeEntity(ref, "SCOPE.Datastore", keyType, file.Path, file.Language, line)
			setProps(&ent, "framework", "redis", "pattern_type", "keyspace",
				"keyspace", label, "key_type", keyType)
			out = append(out, ent)
		}
		return ref
	}

	scan := func(allRe, keyRe *regexp.Regexp, cat, subtype, idPrefix string,
		edgeFor func(string) string, keyProp, prefixProp, edgeLabel string) {
		keyed := phpKeyedRedisOps(src, keyRe)
		for _, m := range allRe.FindAllStringSubmatchIndex(src, -1) {
			line := lineOf(src, m[0])
			seen := getSeen(cat)
			if seen[line] {
				continue
			}
			seen[line] = true
			ent := makeEntity(fmt.Sprintf("%s:%s:%d", idPrefix, file.Path, line),
				"SCOPE.Operation", subtype, file.Path, file.Language, line)
			setProps(&ent, "framework", "redis", "pattern_type", subtype)
			if op, ok := keyed[line]; ok {
				label := phpRedisLabel(&ent, op.access, keyProp, prefixProp)
				if label != "" {
					keyType := keyProp
					if op.access.keyPrefix != "" {
						keyType = prefixProp
					}
					ref := emitKeyspace(label, keyType, line)
					ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
						ToID: ref,
						Kind: edgeFor(op.verb),
						Properties: map[string]string{
							"framework": "redis", "op": strings.ToLower(op.verb), edgeLabel: label,
						},
					})
				}
			} else {
				// Fully-dynamic key — honest-partial: op site flagged dynamic,
				// no fabricated keyspace/edge.
				setProps(&ent, keyProp, "<dynamic>", "dynamic", "true")
			}
			out = append(out, ent)
		}
	}

	scan(phpRedisCacheAllRe, phpRedisCacheRe, "cache", "cache_op", "redis_cache",
		phpRedisCacheEdge, "key", "key_prefix", "keyspace")
	scan(phpRedisPubSubAllRe, phpRedisPubSubRe, "pubsub", "pubsub", "redis_pubsub",
		phpRedisPubSubEdge, "channel", "channel_prefix", "channel")
	scan(phpRedisStreamAllRe, phpRedisStreamRe, "stream", "stream_op", "redis_stream",
		phpRedisStreamEdge, "stream", "stream_prefix", "stream")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
