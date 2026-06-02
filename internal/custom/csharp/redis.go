package csharp

// redis.go — StackExchange.Redis key/channel topology extractor
// (#3625 epic, child of the Python template #3668).
//
// StackExchange.Redis is the dominant C# Redis client. A connected database
// (IDatabase) exposes typed verbs whose FIRST argument is the RedisKey:
//
//	db.StringGet("session:abc")          -> READS_FROM key  "session:abc"
//	db.StringSet("user:" + id, v)        -> WRITES_TO  key-prefix "user:*"
//	db.HashGet("cfg:flags", f)           -> READS_FROM key  "cfg:flags"
//	db.KeyDelete(k)                      -> op only (dynamic, honest-partial)
//
// Pub/Sub goes through the multiplexer's ISubscriber, keyed by a
// RedisChannel first argument:
//
//	sub.Publish("events", payload)       -> PUBLISHES_TO channel "events"
//	sub.Subscribe("events", handler)     -> SUBSCRIBES_TO channel "events"
//
// Streams use the Stream* verb family, keyed by a RedisKey stream name:
//
//	db.StreamAdd("orders", ...)          -> WRITES_TO stream "orders"
//	db.StreamRead("orders", ...)         -> READS_FROM stream "orders"
//
// For each keyed op we emit a SCOPE.Datastore keyspace target node
// (Name == "Datastore:redis:<label>", deduplicated per file) and an access
// edge (READS_FROM / WRITES_TO / PUBLISHES_TO / SUBSCRIBES_TO) from the
// operation site to that target — mirroring the Python template (#3668) so the
// data-access graph is cross-language consistent and traversable.
//
// Key topology (honest-partial for dynamic keys — no fabricated key):
//
//	"session:abc"            -> key  "session:abc"
//	"user:" + id             -> key-prefix "user:*"   (string-concat head)
//	$"session:{sid}"         -> key-prefix "session:*" (interpolated head)
//	$"{tenant}:cfg" / k      -> dynamic (no static head, no key emitted)

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
	extractor.Register("custom_csharp_redis", &redisExtractor{})
}

type redisExtractor struct{}

func (e *redisExtractor) Language() string { return "custom_csharp_redis" }

// csRedisKeyArg matches the first key/channel/stream argument literal. It
// accepts a plain string literal "x" or an interpolated string $"x{...}".
// Group offsets relative to the whole match: group 1 = verb, group 2 = the
// `$` interpolation marker (or empty), group 3 = literal body.
const csRedisKeyArg = `(\$?)"([^"]*)"`

var (
	// csRedisCacheRe — string/hash/list/set/sorted-set ops keyed by a RedisKey.
	csRedisCacheRe = regexp.MustCompile(
		`\.(StringGet|StringSet|StringGetSet|StringIncrement|StringDecrement|StringAppend|HashGet|HashGetAll|HashSet|HashDelete|HashIncrement|HashExists|KeyDelete|KeyExists|KeyExpire|KeyTimeToLive|KeyPersist|ListLeftPush|ListRightPush|ListLeftPop|ListRightPop|ListRange|ListLength|SetAdd|SetRemove|SetMembers|SetContains|SortedSetAdd|SortedSetRemove|SortedSetRangeByRank|SortedSetRangeByScore|SortedSetScore)\s*\(\s*` + csRedisKeyArg)
	csRedisCacheAllRe = regexp.MustCompile(
		`\.(StringGet|StringSet|StringGetSet|StringIncrement|StringDecrement|StringAppend|HashGet|HashGetAll|HashSet|HashDelete|HashIncrement|HashExists|KeyDelete|KeyExists|KeyExpire|KeyTimeToLive|KeyPersist|ListLeftPush|ListRightPush|ListLeftPop|ListRightPop|ListRange|ListLength|SetAdd|SetRemove|SetMembers|SetContains|SortedSetAdd|SortedSetRemove|SortedSetRangeByRank|SortedSetRangeByScore|SortedSetScore)\s*\(`)

	csRedisPubSubRe = regexp.MustCompile(
		`\.(Publish|Subscribe)\s*\(\s*` + csRedisKeyArg)
	csRedisPubSubAllRe = regexp.MustCompile(
		`\.(Publish|Subscribe)\s*\(`)

	csRedisStreamRe = regexp.MustCompile(
		`\.(StreamAdd|StreamRead|StreamReadGroup|StreamRange|StreamLength|StreamTrim|StreamDelete|StreamAcknowledge)\s*\(\s*` + csRedisKeyArg)
	csRedisStreamAllRe = regexp.MustCompile(
		`\.(StreamAdd|StreamRead|StreamReadGroup|StreamRange|StreamLength|StreamTrim|StreamDelete|StreamAcknowledge)\s*\(`)
)

// csRedisReadVerbs is the set of cache verbs that only read.
var csRedisReadVerbs = map[string]bool{
	"StringGet": true, "HashGet": true, "HashGetAll": true, "HashExists": true,
	"KeyExists": true, "KeyTimeToLive": true, "ListRange": true, "ListLength": true,
	"ListLeftPop": true, "ListRightPop": true, "SetMembers": true, "SetContains": true,
	"SortedSetRangeByRank": true, "SortedSetRangeByScore": true, "SortedSetScore": true,
}

func csRedisCacheEdge(verb string) string {
	if csRedisReadVerbs[verb] {
		return string(types.RelationshipKindReadsFrom)
	}
	return string(types.RelationshipKindWritesTo)
}

func csRedisPubSubEdge(verb string) string {
	if verb == "Subscribe" {
		return string(types.RelationshipKindSubscribesTo)
	}
	return string(types.RelationshipKindPublishesTo)
}

func csRedisStreamEdge(verb string) string {
	switch verb {
	case "StreamAdd", "StreamTrim", "StreamDelete", "StreamAcknowledge":
		return string(types.RelationshipKindWritesTo)
	default:
		return string(types.RelationshipKindReadsFrom)
	}
}

func csRedisKeyspaceRef(label string) string {
	return fmt.Sprintf("Datastore:redis:%s", label)
}

// csRedisAccess is the resolved key topology for one keyed op.
type csRedisAccess struct {
	key       string
	keyPrefix string
	dynamic   bool
}

// csNormaliseRedisKey resolves a captured (interp-marker, literal, after) into
// a csRedisAccess. `after` is the source window immediately following the
// literal, used to detect a `+`-concatenation prefix.
func csNormaliseRedisKey(interp, literal, after string) csRedisAccess {
	if interp == "$" {
		if i := strings.IndexByte(literal, '{'); i >= 0 {
			head := literal[:i]
			if head == "" {
				return csRedisAccess{dynamic: true}
			}
			return csRedisAccess{keyPrefix: head + "*"}
		}
		if literal == "" {
			return csRedisAccess{dynamic: true}
		}
		return csRedisAccess{key: literal}
	}
	if literal == "" {
		return csRedisAccess{dynamic: true}
	}
	if strings.HasPrefix(strings.TrimLeft(after, " \t"), "+") {
		return csRedisAccess{keyPrefix: literal + "*"}
	}
	return csRedisAccess{key: literal}
}

// csKeyedRedisOp pairs a verb with its resolved key, keyed by line.
type csKeyedRedisOp struct {
	verb   string
	access csRedisAccess
}

func csKeyedRedisOps(src string, re *regexp.Regexp) map[int]csKeyedRedisOp {
	res := make(map[int]csKeyedRedisOp)
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		// groups: 0-1 full, 2-3 verb, 4-5 interp, 6-7 literal
		if len(m) < 8 {
			continue
		}
		line := lineOf(src, m[0])
		if _, ok := res[line]; ok {
			continue
		}
		verb := src[m[2]:m[3]]
		interp := ""
		if m[4] >= 0 {
			interp = src[m[4]:m[5]]
		}
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
		res[line] = csKeyedRedisOp{verb: verb, access: csNormaliseRedisKey(interp, literal, after)}
	}
	return res
}

// csRedisLabel writes the key/channel/stream prop and returns the keyspace
// label, or "" when fully dynamic (honest-partial — no fabricated key).
func csRedisLabel(e *types.EntityRecord, a csRedisAccess, keyProp, prefixProp string) string {
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

func (e *redisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.csharp_redis")
	_, span := tracer.Start(ctx, "custom.csharp_redis")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	// Cheap pre-filter: only run the regexes on files that look Redis-ish.
	// StackExchange.Redis verbs are PascalCase and namespaced (String*/Hash*/
	// Stream*/Publish/Subscribe), distinctive enough to gate on.
	if !strings.Contains(src, "Redis") && !strings.Contains(src, ".String") &&
		!strings.Contains(src, ".Hash") && !strings.Contains(src, ".Stream") &&
		!strings.Contains(src, ".Publish") && !strings.Contains(src, ".Subscribe") &&
		!strings.Contains(src, ".Key") && !strings.Contains(src, ".List") &&
		!strings.Contains(src, ".Set") && !strings.Contains(src, ".Sorted") {
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
		ref := csRedisKeyspaceRef(label)
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
		keyed := csKeyedRedisOps(src, keyRe)
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
				label := csRedisLabel(&ent, op.access, keyProp, prefixProp)
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
				// Op matched the keyed-verb form but no static key literal could
				// be resolved (fully-dynamic key) — honest-partial: emit the op
				// site flagged dynamic, with NO fabricated keyspace/edge.
				setProps(&ent, keyProp, "<dynamic>", "dynamic", "true")
			}
			out = append(out, ent)
		}
	}

	scan(csRedisCacheAllRe, csRedisCacheRe, "cache", "cache_op", "redis_cache",
		csRedisCacheEdge, "key", "key_prefix", "keyspace")
	scan(csRedisPubSubAllRe, csRedisPubSubRe, "pubsub", "pubsub", "redis_pubsub",
		csRedisPubSubEdge, "channel", "channel_prefix", "channel")
	scan(csRedisStreamAllRe, csRedisStreamRe, "stream", "stream_op", "redis_stream",
		csRedisStreamEdge, "stream", "stream_prefix", "stream")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
