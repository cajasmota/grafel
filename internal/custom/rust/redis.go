package rust

// redis.go — redis-rs key/channel/stream topology extractor
// (#3625 epic, child of the Python template #3668).
//
// redis-rs offers two call idioms. The typed Commands trait exposes lowercase
// verbs on a connection whose FIRST argument is the key / channel / stream:
//
//	con.get("session:abc")          -> READS_FROM key  "session:abc"
//	con.set("user:xyz", v)          -> WRITES_TO  key  "user:xyz"
//	con.hget("cfg:flags", f)        -> READS_FROM key  "cfg:flags"
//	con.del(k)                      -> op only (dynamic, honest-partial)
//	con.publish("events", p)        -> PUBLISHES_TO channel "events"
//	con.subscribe("events")         -> SUBSCRIBES_TO channel "events"
//	con.xadd("orders", ...)         -> WRITES_TO  stream "orders"
//
// The low-level command builder names the verb in cmd("VERB") and the key in
// the first .arg("key"):
//
//	cmd("GET").arg("session:abc").query(...)   -> READS_FROM key "session:abc"
//	cmd("SET").arg("user:xyz").arg(v)...        -> WRITES_TO  key "user:xyz"
//	cmd("PUBLISH").arg("events").arg(p)...      -> PUBLISHES_TO channel "events"
//
// For each keyed op we emit a SCOPE.Datastore keyspace target node
// (Name == "Datastore:redis:<label>", deduplicated per file) and an access
// edge (READS_FROM / WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO) from the op site
// to that target — mirroring the Python template (#3668).
//
// Key topology (honest-partial for dynamic keys — no fabricated key). Rust has
// no in-literal interpolation; dynamic keys are built with format!(...) or a
// bare variable, both of which yield no static key (op-only):
//
//	"session:abc"   -> key  "session:abc"
//	format!(...)/k  -> dynamic (no static head, no key emitted)

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
	extractor.Register("custom_rust_redis", &rustRedisExtractor{})
}

type rustRedisExtractor struct{}

func (e *rustRedisExtractor) Language() string { return "custom_rust_redis" }

// rustRedisKeyArg matches a double-quoted string literal first argument.
// Group 1 = verb, group 2 = literal body.
const rustRedisKeyArg = `"([^"]*)"`

var (
	// Typed Commands-trait methods (con.get("k"), con.set("k", v), …).
	rustRedisCacheRe = regexp.MustCompile(
		`\.(get|set|set_ex|set_nx|getset|mget|mset|hget|hget_all|hset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*::<[^>]*>\s*\(\s*` + rustRedisKeyArg + `|\.(get|set|set_ex|set_nx|getset|mget|mset|hget|hget_all|hset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*\(\s*` + rustRedisKeyArg)
	rustRedisCacheAllRe = regexp.MustCompile(
		`\.(get|set|set_ex|set_nx|getset|mget|mset|hget|hget_all|hset|hdel|hexists|del|exists|expire|ttl|pttl|persist|incr|decr|append|lpush|rpush|lpop|rpop|lrange|llen|sadd|srem|smembers|sismember|zadd|zrem|zrange|zrangebyscore|zscore)\s*(?:::<[^>]*>)?\s*\(`)

	rustRedisPubSubRe = regexp.MustCompile(
		`\.(publish|subscribe|psubscribe)\s*(?:::<[^>]*>)?\s*\(\s*` + rustRedisKeyArg)
	rustRedisPubSubAllRe = regexp.MustCompile(
		`\.(publish|subscribe|psubscribe)\s*(?:::<[^>]*>)?\s*\(`)

	rustRedisStreamRe = regexp.MustCompile(
		`\.(xadd|xread|xread_options|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*(?:::<[^>]*>)?\s*\(\s*` + rustRedisKeyArg)
	rustRedisStreamAllRe = regexp.MustCompile(
		`\.(xadd|xread|xread_options|xack|xlen|xrange|xrevrange|xtrim|xdel)\s*(?:::<[^>]*>)?\s*\(`)

	// Low-level builder: cmd("VERB").arg("key").
	rustRedisCmdRe = regexp.MustCompile(
		`(?:cmd|Cmd::new)\s*\(\s*"([A-Za-z]+)"\s*\)\s*\.arg\s*\(\s*` + rustRedisKeyArg)
	rustRedisCmdAllRe = regexp.MustCompile(`(?:cmd|Cmd::new)\s*\(\s*"([A-Za-z]+)"\s*\)`)
)

var rustRedisReadVerbs = map[string]bool{
	"get": true, "hget": true, "hget_all": true, "hexists": true, "getset": true,
	"mget": true, "ttl": true, "pttl": true, "exists": true, "lrange": true,
	"llen": true, "smembers": true, "sismember": true, "zrange": true,
	"zrangebyscore": true, "zscore": true, "lpop": true, "rpop": true,
}

func rustRedisCacheEdge(verb string) string {
	if rustRedisReadVerbs[verb] {
		return string(types.RelationshipKindReadsFrom)
	}
	return string(types.RelationshipKindWritesTo)
}

func rustRedisPubSubEdge(verb string) string {
	if strings.HasPrefix(verb, "sub") || strings.HasPrefix(verb, "psub") {
		return string(types.RelationshipKindSubscribesTo)
	}
	return string(types.RelationshipKindPublishesTo)
}

func rustRedisStreamEdge(verb string) string {
	switch verb {
	case "xadd", "xtrim", "xdel", "xack":
		return string(types.RelationshipKindWritesTo)
	default:
		return string(types.RelationshipKindReadsFrom)
	}
}

// rustRedisCmdEdge classifies an UPPERCASE raw-command verb (GET/SET/PUBLISH/…)
// into READS_FROM / WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO.
func rustRedisCmdEdge(rawVerb string) (string, string, string) {
	v := strings.ToUpper(rawVerb)
	switch v {
	case "GET", "MGET", "HGET", "HGETALL", "HEXISTS", "EXISTS", "TTL", "PTTL",
		"LRANGE", "LLEN", "SMEMBERS", "SISMEMBER", "ZRANGE", "ZSCORE",
		"XRANGE", "XREVRANGE", "XLEN", "XREAD":
		return string(types.RelationshipKindReadsFrom), "key", "keyspace"
	case "PUBLISH":
		return string(types.RelationshipKindPublishesTo), "channel", "channel"
	case "SUBSCRIBE", "PSUBSCRIBE":
		return string(types.RelationshipKindSubscribesTo), "channel", "channel"
	default:
		return string(types.RelationshipKindWritesTo), "key", "keyspace"
	}
}

func rustRedisKeyspaceRef(label string) string {
	return fmt.Sprintf("Datastore:redis:%s", label)
}

type rustRedisAccess struct {
	key     string
	dynamic bool
}

func rustNormaliseRedisKey(literal string) rustRedisAccess {
	if literal == "" {
		return rustRedisAccess{dynamic: true}
	}
	return rustRedisAccess{key: literal}
}

type rustKeyedRedisOp struct {
	verb   string
	access rustRedisAccess
}

// rustKeyedRedisOps handles the typed-method regex whose two alternations each
// capture (verb, literal): turbofish form groups 2-3/4-5, plain form 6-7/8-9.
func rustKeyedRedisOps(src string, re *regexp.Regexp) map[int]rustKeyedRedisOp {
	res := make(map[int]rustKeyedRedisOp)
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		if _, ok := res[line]; ok {
			continue
		}
		verb, literal := rustPickVerbLiteral(src, m)
		if verb == "" {
			continue
		}
		res[line] = rustKeyedRedisOp{verb: verb, access: rustNormaliseRedisKey(literal)}
	}
	return res
}

// rustPickVerbLiteral walks the submatch pairs and returns the first matched
// (verb, literal) couple. The cache regex has two alternations; pub/sub and
// stream regexes have one. We scan every (start,end) pair: the first non-(-1)
// captured group is the verb, the next is the literal.
func rustPickVerbLiteral(src string, m []int) (string, string) {
	var got []string
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 {
			got = append(got, src[m[i]:m[i+1]])
		}
	}
	switch len(got) {
	case 0:
		return "", ""
	case 1:
		return got[0], ""
	default:
		return got[0], got[1]
	}
}

// rustKeyedRedisCmds handles the cmd("VERB").arg("key") builder form.
func rustKeyedRedisCmds(src string) map[int]rustKeyedRedisOp {
	res := make(map[int]rustKeyedRedisOp)
	for _, m := range rustRedisCmdRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOf(src, m[0])
		if _, ok := res[line]; ok {
			continue
		}
		verb := src[m[2]:m[3]]
		literal := ""
		if m[4] >= 0 {
			literal = src[m[4]:m[5]]
		}
		res[line] = rustKeyedRedisOp{verb: verb, access: rustNormaliseRedisKey(literal)}
	}
	return res
}

func rustRedisLabel(e *types.EntityRecord, a rustRedisAccess, keyProp string) string {
	if a.key != "" {
		setProps(e, keyProp, a.key)
		return a.key
	}
	setProps(e, keyProp, "<dynamic>", "dynamic", "true")
	return ""
}

func (e *rustRedisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.rust_redis")
	_, span := tracer.Start(ctx, "custom.rust_redis")
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
		ref := rustRedisKeyspaceRef(label)
		if !seenKeyspace[ref] {
			seenKeyspace[ref] = true
			ent := makeEntity(ref, "SCOPE.Datastore", keyType, file.Path, file.Language, line)
			setProps(&ent, "framework", "redis", "pattern_type", "keyspace",
				"keyspace", label, "key_type", keyType)
			out = append(out, ent)
		}
		return ref
	}

	scan := func(allRe *regexp.Regexp, keyed map[int]rustKeyedRedisOp, cat, subtype, idPrefix string,
		edgeFor func(string) string, keyProp, edgeLabel string) {
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
				label := rustRedisLabel(&ent, op.access, keyProp)
				if label != "" {
					ref := emitKeyspace(label, keyProp, line)
					ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
						ToID: ref,
						Kind: edgeFor(op.verb),
						Properties: map[string]string{
							"framework": "redis", "op": op.verb, edgeLabel: label,
						},
					})
				}
			} else {
				// Fully-dynamic key (format!(...) / bare var) — honest-partial:
				// op site flagged dynamic, no fabricated keyspace/edge.
				setProps(&ent, keyProp, "<dynamic>", "dynamic", "true")
			}
			out = append(out, ent)
		}
	}

	scan(rustRedisCacheAllRe, rustKeyedRedisOps(src, rustRedisCacheRe),
		"cache", "cache_op", "redis_cache", rustRedisCacheEdge, "key", "keyspace")
	scan(rustRedisPubSubAllRe, rustKeyedRedisOps(src, rustRedisPubSubRe),
		"pubsub", "pubsub", "redis_pubsub", rustRedisPubSubEdge, "channel", "channel")
	scan(rustRedisStreamAllRe, rustKeyedRedisOps(src, rustRedisStreamRe),
		"stream", "stream_op", "redis_stream", rustRedisStreamEdge, "stream", "stream")

	// Low-level cmd("VERB").arg("key") builder form.
	cmdKeyed := rustKeyedRedisCmds(src)
	for _, m := range rustRedisCmdAllRe.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		seen := getSeen("cmd")
		if seen[line] {
			continue
		}
		seen[line] = true
		rawVerb := src[m[2]:m[3]]
		ent := makeEntity(fmt.Sprintf("redis_cmd:%s:%d", file.Path, line),
			"SCOPE.Operation", "cache_op", file.Path, file.Language, line)
		setProps(&ent, "framework", "redis", "pattern_type", "cache_op", "raw_verb", strings.ToUpper(rawVerb))
		if op, ok := cmdKeyed[line]; ok {
			edge, keyProp, edgeLabel := rustRedisCmdEdge(op.verb)
			label := rustRedisLabel(&ent, op.access, keyProp)
			if label != "" {
				ref := emitKeyspace(label, keyProp, line)
				ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
					ToID: ref,
					Kind: edge,
					Properties: map[string]string{
						"framework": "redis", "op": strings.ToUpper(op.verb), edgeLabel: label,
					},
				})
			}
		}
		out = append(out, ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
