package ruby

// redis.go — redis-rb key/channel/stream topology extractor
// (#3625 epic, child of the Python template #3668).
//
// redis-rb exposes lowercase command verbs on a Redis client whose FIRST
// argument is the key / channel / stream:
//
//	redis.get("session:abc")        -> READS_FROM key  "session:abc"
//	redis.set("user:" + id, v)      -> WRITES_TO  key-prefix "user:*"
//	redis.hget("cfg:flags", f)      -> READS_FROM key  "cfg:flags"
//	redis.del(k)                    -> op only (dynamic, honest-partial)
//	redis.publish("events", p)      -> PUBLISHES_TO channel "events"
//	redis.subscribe("events")       -> SUBSCRIBES_TO channel "events"
//	redis.xadd("orders", ...)       -> WRITES_TO  stream "orders"
//	redis.xread("orders", ...)      -> READS_FROM stream "orders"
//
// For each keyed op we emit a SCOPE.Datastore keyspace target node
// (Name == "Datastore:redis:<label>", deduplicated per file) and an access
// edge (READS_FROM / WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO) from the op site
// to that target — mirroring the Python template (#3668).
//
// Key topology (honest-partial for dynamic keys — no fabricated key):
//
//	"session:abc" / 'session:abc'  -> key  "session:abc"
//	"user:" + id                   -> key-prefix "user:*"  (`+`-concat head)
//	"user:#{id}"                   -> key-prefix "user:*"  (interp head)
//	"#{tenant}:cfg" / k            -> dynamic (no static head, no key emitted)

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
	extractor.Register("custom_ruby_redis", &rubyRedisExtractor{})
}

type rubyRedisExtractor struct{}

func (e *rubyRedisExtractor) Language() string { return "custom_ruby_redis" }

// rubyRedisKeyArg matches the first key argument literal. Group 1 = verb,
// group 2 = quote char, group 3 = literal body (a `#{` interpolation marker may
// appear inside a double-quoted body and is captured up to the closing quote).
const rubyRedisKeyArg = `(["'])((?:[^"'\\]|\\.)*?)["']`

var (
	rubyRedisCacheRe = regexp.MustCompile(
		`\.(get|set|setex|setnx|getset|mget|mset|hget|hgetall|hset|hmget|hmset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|incrby|decrby|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*\(\s*` + rubyRedisKeyArg)
	rubyRedisCacheAllRe = regexp.MustCompile(
		`\.(get|set|setex|setnx|getset|mget|mset|hget|hgetall|hset|hmget|hmset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|incrby|decrby|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*\(`)

	rubyRedisPubSubRe = regexp.MustCompile(
		`\.(publish|subscribe|psubscribe)\s*\(\s*` + rubyRedisKeyArg)
	rubyRedisPubSubAllRe = regexp.MustCompile(
		`\.(publish|subscribe|psubscribe)\s*\(`)

	rubyRedisStreamRe = regexp.MustCompile(
		`\.(xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*\(\s*` + rubyRedisKeyArg)
	rubyRedisStreamAllRe = regexp.MustCompile(
		`\.(xadd|xread|xreadgroup|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*\(`)
)

var rubyRedisReadVerbs = map[string]bool{
	"get": true, "hget": true, "hgetall": true, "hmget": true, "hexists": true,
	"getset": true, "mget": true, "ttl": true, "pttl": true, "exists": true,
	"lrange": true, "llen": true, "smembers": true, "sismember": true,
	"zrange": true, "zrangebyscore": true, "zscore": true, "lpop": true, "rpop": true,
}

func rubyRedisCacheEdge(verb string) string {
	if rubyRedisReadVerbs[verb] {
		return string(types.RelationshipKindReadsFrom)
	}
	return string(types.RelationshipKindWritesTo)
}

func rubyRedisPubSubEdge(verb string) string {
	if strings.HasPrefix(verb, "sub") || strings.HasPrefix(verb, "psub") {
		return string(types.RelationshipKindSubscribesTo)
	}
	return string(types.RelationshipKindPublishesTo)
}

func rubyRedisStreamEdge(verb string) string {
	switch verb {
	case "xadd", "xtrim", "xdel", "xack":
		return string(types.RelationshipKindWritesTo)
	default:
		return string(types.RelationshipKindReadsFrom)
	}
}

func rubyRedisKeyspaceRef(label string) string {
	return fmt.Sprintf("Datastore:redis:%s", label)
}

type rubyRedisAccess struct {
	key       string
	keyPrefix string
	dynamic   bool
}

// rubyNormaliseRedisKey resolves a captured (quote, literal, after) into a
// rubyRedisAccess. Single-quoted strings do NOT interpolate in Ruby, so only
// double-quoted literals are scanned for a `#{` marker; `after` detects a
// `+`-concatenation prefix.
func rubyNormaliseRedisKey(quote, literal, after string) rubyRedisAccess {
	if quote == `"` {
		if i := strings.Index(literal, "#{"); i >= 0 {
			head := literal[:i]
			if head == "" {
				return rubyRedisAccess{dynamic: true}
			}
			return rubyRedisAccess{keyPrefix: head + "*"}
		}
	}
	if literal == "" {
		return rubyRedisAccess{dynamic: true}
	}
	if strings.HasPrefix(strings.TrimLeft(after, " \t"), "+") {
		return rubyRedisAccess{keyPrefix: literal + "*"}
	}
	return rubyRedisAccess{key: literal}
}

type rubyKeyedRedisOp struct {
	verb   string
	access rubyRedisAccess
}

func rubyKeyedRedisOps(src string, re *regexp.Regexp) map[int]rubyKeyedRedisOp {
	res := make(map[int]rubyKeyedRedisOp)
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
		res[line] = rubyKeyedRedisOp{verb: verb, access: rubyNormaliseRedisKey(quote, literal, after)}
	}
	return res
}

func rubyRedisLabel(e *types.EntityRecord, a rubyRedisAccess, keyProp, prefixProp string) string {
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

func (e *rubyRedisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.ruby_redis")
	_, span := tracer.Start(ctx, "custom.ruby_redis")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "redis") && !strings.Contains(src, "Redis") {
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
		ref := rubyRedisKeyspaceRef(label)
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
		keyed := rubyKeyedRedisOps(src, keyRe)
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
				label := rubyRedisLabel(&ent, op.access, keyProp, prefixProp)
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
							"framework": "redis", "op": op.verb, edgeLabel: label,
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

	scan(rubyRedisCacheAllRe, rubyRedisCacheRe, "cache", "cache_op", "redis_cache",
		rubyRedisCacheEdge, "key", "key_prefix", "keyspace")
	scan(rubyRedisPubSubAllRe, rubyRedisPubSubRe, "pubsub", "pubsub", "redis_pubsub",
		rubyRedisPubSubEdge, "channel", "channel_prefix", "channel")
	scan(rubyRedisStreamAllRe, rubyRedisStreamRe, "stream", "stream_op", "redis_stream",
		rubyRedisStreamEdge, "stream", "stream_prefix", "stream")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
