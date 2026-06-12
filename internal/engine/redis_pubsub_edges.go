// Redis pub/sub + Streams channel discovery — #930.
//
// For every Redis publish/subscribe or XADD/XREADGROUP call site this pass
// can statically recognize, we emit a synthetic SCOPE.Queue entity keyed by
// the channel or stream name, plus PUBLISHES_TO or SUBSCRIBES_TO edges from
// the calling function to that entity. The synthetic ID is identical across
// repos (`channel:redis-pubsub:<name>` or `stream:redis:<name>`), so the
// existing import-channel linker matches producer and consumer sides on
// shared entity ID without any new cross-repo matching code — same technique
// used by kafka_edges.go (#726 wave 1) and nats_edges.go (#726 wave 3).
//
// Redis pub/sub is fire-and-forget; Streams are queue-like with consumer
// groups. Both use SCOPE.Queue as the synthetic entity kind but carry a
// `channel_type` property ("pubsub" | "stream") to let downstream passes
// distinguish them.
//
// Libraries / frameworks covered:
//
//	Python (redis-py / aioredis):
//	  producer: redis.publish('channel', msg) / r.publish('channel', msg)
//	            await r.publish('channel', msg)
//	  consumer: pubsub.subscribe('channel') / r.subscribe('channel')
//	            pubsub.psubscribe('pattern') — wildcard: emits as-is
//	            pubsub.listen() / pubsub.get_message() — no channel arg,
//	              skipped (channel was captured at subscribe time)
//	  streams:  r.xadd('stream', {...}) / await r.xadd('stream', {...})
//	            r.xreadgroup(group, consumer, {'stream': '>'})
//	            r.xread({'stream': last_id})
//
//	Node (ioredis / node-redis):
//	  producer: redis.publish('channel', msg) / client.publish('channel', msg)
//	            redisClient.publish('channel', msg)
//	            publisher.publish('channel', msg)
//	            pubsub.publish('channel', msg)
//	  consumer: redis.subscribe('channel', callback)
//	            subscriber.subscribe('channel', callback)
//	            redis.psubscribe('orders.*', callback) — wildcard
//	            redis.on('message', (ch, msg) => {}) — channel captured earlier
//	  streams:  client.xAdd('stream', '*', {field: val})
//	            client.xReadGroup(group, consumer, [{key:'stream',id:'>'}])
//	            client.xRead([{key:'stream',id:lastId}])
//
//	Go (go-redis / redis/v9):
//	  producer: rdb.Publish(ctx, "channel", message)
//	  consumer: pubsub := rdb.Subscribe(ctx, "channel")
//	            pubsub := rdb.PSubscribe(ctx, "orders.*") — wildcard
//	  streams:  rdb.XAdd(ctx, &redis.XAddArgs{Stream:"stream",...})
//	            rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Streams:[]string{"stream",">"},...})
//	            rdb.XRead(ctx, &redis.XReadArgs{Streams:[]string{"stream",lastId},...})
//
//	Ruby (redis-rb):
//	  producer: redis.publish('channel', message)
//	  consumer: redis.subscribe('channel') { |on| on.message{...} }
//	            redis.psubscribe('orders.*') { |on| ... } — wildcard
//	  streams:  redis.xadd('stream', '*', field: val)
//	            redis.xreadgroup(group, consumer, 'stream', '>') / xread(count:N,'stream',lastid)
//
// Non-pub/sub calls (GET/SET/HSET/LPUSH/etc.) do NOT trigger detection —
// fast-path pre-filter gate guards against false positives.
//
// Emit: SCOPE.Queue + PUBLISHES_TO + SUBSCRIBES_TO.
// ID format:  channel:redis-pubsub:<channel>   (pub/sub)
//
//	stream:redis:<stream>             (Streams)
//
// Refs #930.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// redisPubSubChannelEntityKind is the SCOPE.Queue kind reused for synthetic
// Redis pub/sub channel and stream entities.
const redisPubSubChannelEntityKind = "SCOPE.Queue"

// redisPubSubSynthesisSupportsLanguage reports whether applyRedisPubSubEdges
// can emit synthetics for `lang`.
func redisPubSubSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "javascript", "typescript", "go", "ruby", "java", "kotlin", "elixir", "csharp":
		return true
	default:
		return false
	}
}

// applyRedisPubSubEdges runs after applyGRPCEdges and APPENDS SCOPE.Queue
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges for Redis pub/sub and Streams.
// Append-only — never modifies or removes existing entities or edges, so this
// pass cannot regress the surrounding pipeline's bug-rate.
func applyRedisPubSubEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !redisPubSubSynthesisSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Fast pre-filter: skip files with no Redis pub/sub or streams tokens.
	// This guards against false positives on plain redis cache files and
	// keeps this pass cheap for the vast majority of Redis-free files.
	// Uses case-insensitive Contains so that Go's Publish/Subscribe and
	// Node's xAdd/xReadGroup are caught alongside lowercase variants.
	srcLower := strings.ToLower(src)
	hasPublish := strings.Contains(srcLower, "publish")
	hasSubscribe := strings.Contains(srcLower, "subscribe") // covers subscribe, psubscribe, PSubscribe, Subscribe
	// "redis" matches the client/import; "redix" is the Elixir client lib
	// (Redix / Redix.PubSub) — note "redix" does NOT contain "redis", so it
	// must be checked explicitly or Elixir files would be filtered out (#1489).
	hasRedis := strings.Contains(srcLower, "redis") || strings.Contains(srcLower, "redix")
	// A publish or subscribe call is only a pub/sub signal when the file also
	// references redis — UNLESS the call is PSubscribe/psubscribe which is
	// Redis-specific and can appear without an explicit "redis" import when
	// the client variable is declared elsewhere.
	hasPubSubWithRedis := (hasPublish || hasSubscribe) && hasRedis
	hasPSubscribeAlone := strings.Contains(srcLower, "psubscribe")
	hasPubSub := hasPubSubWithRedis || hasPSubscribeAlone
	hasXadd := strings.Contains(srcLower, "xadd")
	hasXread := strings.Contains(srcLower, "xreadgroup") || strings.Contains(srcLower, "xread")
	// Spring RedisTemplate.convertAndSend (Java/Kotlin) is its own pub/sub
	// signal when the file also references redis.
	hasConvertAndSend := strings.Contains(src, "convertAndSend") && hasRedis
	// Spring MessageListenerAdapter / ChannelTopic / PatternTopic consumer side.
	hasChannelTopic := (strings.Contains(src, "ChannelTopic") || strings.Contains(src, "PatternTopic") ||
		strings.Contains(src, "MessageListenerAdapter")) && hasRedis
	if !hasPubSub && !hasXadd && !hasXread && !hasConvertAndSend && !hasChannelTopic {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Dedup-by-ID: one entity per channel/stream per file; one edge per
	// (caller, channel, direction) per file.
	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}

	emitChannel := func(channelID, channelName, channelType string, isWildcard bool, props map[string]string) {
		if seenQueue[channelID] {
			return
		}
		seenQueue[channelID] = true
		merged := map[string]string{
			"broker":       "redis",
			"channel_name": channelName,
			"channel_type": channelType, // "pubsub" or "stream"
			"pattern_type": "redis_pubsub_synthesis",
			"is_wildcard":  boolStr(isWildcard),
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical channels collapse to one entity
		// per repo and match across repos via the import-channel linker.
		entities = append(entities, types.EntityRecord{
			Name:               channelID,
			Kind:               redisPubSubChannelEntityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, channelID, edgeKind string, props map[string]string) {
		if callerName == "" || channelID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + channelID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "redis",
			"pattern_type": "redis_pubsub_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", redisPubSubChannelEntityKind, channelID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePyRedisPubSub(src, emitChannel, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeRedisPubSub(src, emitChannel, emitEdge)
	case "go":
		synthesizeGoRedisPubSub(src, emitChannel, emitEdge)
	case "ruby":
		synthesizeRubyRedisPubSub(src, emitChannel, emitEdge)
	case "java", "kotlin":
		synthesizeSpringRedisPubSub(src, emitChannel, emitEdge)
	case "elixir":
		synthesizeElixirRedisPubSub(src, emitChannel, emitEdge)
	case "csharp":
		synthesizeCSharpRedisPubSub(src, emitChannel, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// redisPubSubChannelID returns the canonical synthetic ID for a Redis pub/sub
// channel.  Identical across repos — the cross-repo linker matches on this.
func redisPubSubChannelID(channel string) string {
	return "channel:redis-pubsub:" + channel
}

// redisStreamID returns the canonical synthetic ID for a Redis stream.
func redisStreamID(stream string) string {
	return "stream:redis:" + stream
}

// looksLikeRedisChannel returns true when `s` plausibly looks like a Redis
// channel or stream name. Belt-and-suspenders guard against matching
// arbitrary string-literal arguments.
func looksLikeRedisChannel(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t{}()\"'") {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '.' || r == '_' || r == '-' || r == ':' || r == '*':
			// Valid Redis channel characters including pub/sub wildcards.
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// Python — redis-py / aioredis
// ---------------------------------------------------------------------------

// pyRedisPublishRe captures r.publish('channel', msg) and
// redis.publish('channel', msg) and await r.publish('channel', msg).
// Group 1 = channel name.
var pyRedisPublishRe = regexp.MustCompile(`\.publish\s*\(\s*["']([^"'\n\r]+)["']`)

// pyRedisSubscribeRe captures pubsub.subscribe('channel') and
// r.subscribe('channel') forms. Group 1 = channel name.
var pyRedisSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// pyRedisPSubscribeRe captures pubsub.psubscribe('orders.*') — pattern
// subscribe. Group 1 = pattern.
var pyRedisPSubscribeRe = regexp.MustCompile(`\.psubscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// pyRedisXAddRe captures r.xadd('stream', {...}) and await r.xadd('stream', ...).
// Group 1 = stream name.
var pyRedisXAddRe = regexp.MustCompile(`(?i)\.xadd\s*\(\s*["']([^"'\n\r]+)["']`)

// pyRedisXReadGroupRe captures r.xreadgroup(group, consumer, {'stream': '>'}).
// We look for a dict-key string literal inside the call. Group 1 = stream name.
var pyRedisXReadGroupRe = regexp.MustCompile(`(?i)\.xreadgroup\s*\([^)]*["']([^"'\n\r]+)["']\s*:`)

// pyRedisXReadRe captures r.xread({'stream': last_id}). Group 1 = stream name.
var pyRedisXReadRe = regexp.MustCompile(`(?i)\.xread\s*\(\s*\{["']([^"'\n\r]+)["']`)

func synthesizePyRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Fast pre-filter: must have redis-related tokens.
	if !strings.Contains(src, "redis") && !strings.Contains(src, "aioredis") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// Pub/Sub publishers
	for _, m := range pyRedisPublishRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "pubsub"})
	}

	// Pub/Sub subscribers (subscribe)
	for _, m := range pyRedisSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "pubsub"})
	}

	// Pub/Sub pattern subscribers (psubscribe)
	for _, m := range pyRedisPSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		if !looksLikeRedisChannel(pattern) {
			continue
		}
		id := redisPubSubChannelID(pattern)
		emitChannel(id, pattern, "pubsub", true, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "pubsub", "is_pattern": "true"})
	}

	// Streams producers (XADD)
	for _, m := range pyRedisXAddRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "stream"})
	}

	// Streams consumers (XREADGROUP)
	for _, m := range pyRedisXReadGroupRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "stream"})
	}

	// Streams read (XREAD)
	for _, m := range pyRedisXReadRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-py"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-py", "channel_type": "stream"})
	}
}

// ---------------------------------------------------------------------------
// Node — ioredis / node-redis
// ---------------------------------------------------------------------------

// nodeRedisPublishRe captures redis.publish('channel', msg),
// client.publish('channel', msg), publisher.publish('channel', msg),
// redisClient.publish('channel', msg), pubsub.publish('channel', msg).
// Group 1 = channel name (single/double/backtick quoted).
var nodeRedisPublishRe = regexp.MustCompile(`\.publish\s*\(\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

// nodeRedisSubscribeRe captures redis.subscribe('channel', ...) and
// subscriber.subscribe('channel', ...). Group 1 = channel name.
var nodeRedisSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

// nodeRedisPSubscribeRe captures redis.psubscribe('orders.*', ...).
// Group 1 = pattern.
var nodeRedisPSubscribeRe = regexp.MustCompile(`\.psubscribe\s*\(\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

// nodeRedisXAddRe captures client.xAdd('stream', '*', {...}) and
// redis.xadd('stream', ...). Group 1 = stream name.
var nodeRedisXAddRe = regexp.MustCompile(`(?i)\.xadd\s*\(\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

// nodeRedisXAddMethodRe captures the ioredis camelCase form xAdd.
var nodeRedisXAddMethodRe = regexp.MustCompile(`\.xAdd\s*\(\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

// nodeRedisXReadGroupRe captures client.xReadGroup(group, consumer,
// [{key:'stream',id:'>'}]). Group 1 = stream name.
var nodeRedisXReadGroupRe = regexp.MustCompile(`(?i)\.xreadgroup\s*\([^)]*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']\s*[,:\}]`)

// nodeRedisXReadRe captures client.xRead([{key:'stream',id:lastId}]).
// Group 1 = stream name.
var nodeRedisXReadRe = regexp.MustCompile(`(?i)\.xread\s*\(\s*\[\s*\{[^}]*?key\s*:\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`)

func synthesizeNodeRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Pre-filter: must reference ioredis / node-redis tokens.
	if !strings.Contains(src, "redis") && !strings.Contains(src, "Redis") && !strings.Contains(src, "ioredis") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	layer := "node-redis"
	if strings.Contains(src, "ioredis") || strings.Contains(src, "IORedis") {
		layer = "ioredis"
	}

	// Pub/Sub publishers
	for _, m := range nodeRedisPublishRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "pubsub"})
	}

	// Pub/Sub subscribers (subscribe)
	for _, m := range nodeRedisSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "pubsub"})
	}

	// Pub/Sub pattern subscribers (psubscribe)
	for _, m := range nodeRedisPSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		if !looksLikeRedisChannel(pattern) {
			continue
		}
		id := redisPubSubChannelID(pattern)
		emitChannel(id, pattern, "pubsub", true, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "pubsub", "is_pattern": "true"})
	}

	// Streams producers (xadd — lowercase ioredis form)
	for _, m := range nodeRedisXAddRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "stream"})
	}

	// Streams producers (xAdd — node-redis camelCase form)
	for _, m := range nodeRedisXAddMethodRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "stream"})
	}

	// Streams consumers (xReadGroup)
	for _, m := range nodeRedisXReadGroupRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "stream"})
	}

	// Streams read (xRead)
	for _, m := range nodeRedisXReadRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": layer})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": layer, "channel_type": "stream"})
	}
}

// ---------------------------------------------------------------------------
// Go — go-redis / redis/v9
// ---------------------------------------------------------------------------

// goRedisPublishRe captures rdb.Publish(ctx, "channel", message).
// Group 1 = channel name.
var goRedisPublishRe = regexp.MustCompile(`\.Publish\s*\(\s*\w+\s*,\s*"([^"\n\r]+)"`)

// goRedisSubscribeRe captures rdb.Subscribe(ctx, "channel") and
// rdb.Subscribe(ctx, "a", "b"). Group 1 = first channel.
var goRedisSubscribeRe = regexp.MustCompile(`\.Subscribe\s*\(\s*\w+\s*,\s*"([^"\n\r]+)"`)

// goRedisPSubscribeRe captures rdb.PSubscribe(ctx, "orders.*").
// Group 1 = pattern.
var goRedisPSubscribeRe = regexp.MustCompile(`\.PSubscribe\s*\(\s*\w+\s*,\s*"([^"\n\r]+)"`)

// goRedisXAddStreamRe captures Stream: "stream-name" inside an XAddArgs literal.
// Group 1 = stream name.
var goRedisXAddStreamRe = regexp.MustCompile(`XAddArgs\s*\{[^}]*Stream\s*:\s*"([^"\n\r]+)"`)

// goRedisXReadGroupStreamsRe captures Streams: []string{"stream-name", ">"} inside
// XReadGroupArgs. Group 1 = stream name (first element).
var goRedisXReadGroupStreamsRe = regexp.MustCompile(`XReadGroupArgs\s*\{[^}]*Streams\s*:\s*\[\]string\s*\{\s*"([^"\n\r]+)"`)

// goRedisXReadStreamsRe captures Streams: []string{"stream-name", lastID} inside
// XReadArgs. Group 1 = stream name (first element).
var goRedisXReadStreamsRe = regexp.MustCompile(`XReadArgs\s*\{[^}]*Streams\s*:\s*\[\]string\s*\{\s*"([^"\n\r]+)"`)

func synthesizeGoRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Pre-filter: must reference go-redis or pub/sub tokens. We intentionally
	// do NOT require "redis" here because the outer applyRedisPubSubEdges
	// pre-filter already guards the call site; the inner filter must not
	// double-gate on the redis token (e.g. when the client var is declared
	// in another file and only PSubscribe/Publish/Subscribe appears here).
	srcLower := strings.ToLower(src)
	if !strings.Contains(srcLower, "redis") && !strings.Contains(srcLower, "publish") &&
		!strings.Contains(srcLower, "subscribe") && !strings.Contains(srcLower, "xadd") &&
		!strings.Contains(srcLower, "xread") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// Pub/Sub publishers
	for _, m := range goRedisPublishRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "pubsub"})
	}

	// Pub/Sub subscribers (Subscribe)
	for _, m := range goRedisSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "pubsub"})
	}

	// Pub/Sub pattern subscribers (PSubscribe)
	for _, m := range goRedisPSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		if !looksLikeRedisChannel(pattern) {
			continue
		}
		id := redisPubSubChannelID(pattern)
		emitChannel(id, pattern, "pubsub", true, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "pubsub", "is_pattern": "true"})
	}

	// Streams producers (XAdd)
	for _, m := range goRedisXAddStreamRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "stream"})
	}

	// Streams consumers (XReadGroup)
	for _, m := range goRedisXReadGroupStreamsRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "stream"})
	}

	// Streams read (XRead)
	for _, m := range goRedisXReadStreamsRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "go-redis"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "go-redis", "channel_type": "stream"})
	}
}

// ---------------------------------------------------------------------------
// Ruby — redis-rb
// ---------------------------------------------------------------------------

// rubyRedisPublishRe captures redis.publish('channel', message).
// Group 1 = channel name.
var rubyRedisPublishRe = regexp.MustCompile(`\.publish\s*\(\s*["']([^"'\n\r]+)["']`)

// rubyRedisSubscribeRe captures redis.subscribe('channel') { ... }.
// Group 1 = channel name.
var rubyRedisSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// rubyRedisPSubscribeRe captures redis.psubscribe('orders.*') { ... }.
// Group 1 = pattern.
var rubyRedisPSubscribeRe = regexp.MustCompile(`\.psubscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// rubyRedisXAddRe captures redis.xadd('stream', '*', field: val).
// Group 1 = stream name.
var rubyRedisXAddRe = regexp.MustCompile(`(?i)\.xadd\s*\(\s*["']([^"'\n\r]+)["']`)

// rubyRedisXReadGroupRe captures redis.xreadgroup(group, consumer, 'stream', '>').
// Group 1 = stream name (third positional argument).
var rubyRedisXReadGroupRe = regexp.MustCompile(`(?i)\.xreadgroup\s*\([^)]*,\s*[^,)]+,\s*["']([^"'\n\r]+)["']`)

// rubyRedisXReadRe captures redis.xread('stream', lastid) and
// redis.xread(count: N, streams: ['stream', lastid]). Group 1 = stream name.
var rubyRedisXReadRe = regexp.MustCompile(`(?i)\.xread\s*\([^)]*["']([^"'\n\r]+)["']`)

// rubyMethodRe captures Ruby method definitions: `def method_name` or
// `def self.method_name`. Group 1 = method name.
var rubyMethodRe = regexp.MustCompile(`(?m)^\s*def\s+(?:self\.)?(\w+)`)

// findEnclosingRubyName walks backward from `offset` looking for a method def.
func findEnclosingRubyName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := rubyMethodRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	return matches[len(matches)-1][1]
}

func synthesizeRubyRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Pre-filter: must reference redis tokens.
	if !strings.Contains(src, "redis") && !strings.Contains(src, "Redis") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingRubyName(src, offset)
	}

	// Pub/Sub publishers
	for _, m := range rubyRedisPublishRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "pubsub"})
	}

	// Pub/Sub subscribers (subscribe)
	for _, m := range rubyRedisSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "pubsub"})
	}

	// Pub/Sub pattern subscribers (psubscribe)
	for _, m := range rubyRedisPSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		if !looksLikeRedisChannel(pattern) {
			continue
		}
		id := redisPubSubChannelID(pattern)
		emitChannel(id, pattern, "pubsub", true, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "pubsub", "is_pattern": "true"})
	}

	// Streams producers (xadd)
	for _, m := range rubyRedisXAddRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "stream"})
	}

	// Streams consumers (xreadgroup)
	for _, m := range rubyRedisXReadGroupRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "stream"})
	}

	// Streams read (xread)
	for _, m := range rubyRedisXReadRe.FindAllStringSubmatchIndex(src, -1) {
		stream := src[m[2]:m[3]]
		if !looksLikeRedisChannel(stream) {
			continue
		}
		id := redisStreamID(stream)
		emitChannel(id, stream, "stream", false, map[string]string{"messaging_layer": "redis-rb"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{"messaging_layer": "redis-rb", "channel_type": "stream"})
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring Data Redis (RedisTemplate / StringRedisTemplate)
// ---------------------------------------------------------------------------
//
// Publisher: `redisTemplate.convertAndSend("channel", payload)` and the
//   StringRedisTemplate variant.  This is the standard Spring pub/sub
//   publisher idiom; Kotlin uses the exact same API.
//
// Consumer: Spring registers listeners via
//   `container.addMessageListener(adapter, new ChannelTopic("channel"))` or
//   `container.addMessageListener(adapter, new PatternTopic("pattern"))`.
//   A plain `MessageListenerAdapter` constructor reference is also detected
//   when the channel is supplied in the same statement.
//
// Entity ID: `channel:redis-pubsub:<channel>` — identical to the ID emitted
//   by the Python / Node / Go / Ruby pass so P7 cross-repo links fire
//   automatically between the Kotlin publisher and any language's subscriber
//   (and vice-versa).

// springRedisConvertAndSendRe captures
//
//	`redisTemplate.convertAndSend("channel", payload)` and the
//	`stringRedisTemplate.convertAndSend("channel", msg)` variant.
//
// Group 1 = channel name (string literal, single or double quoted).
var springRedisConvertAndSendRe = regexp.MustCompile(
	`\.convertAndSend\s*\(\s*["']([^"'\n\r]+)["']`,
)

// springRedisChannelTopicRe captures
//
//	`new ChannelTopic("channel")` and `ChannelTopic("channel")` in
//	Kotlin/Java.  Group 1 = channel name.
var springRedisChannelTopicRe = regexp.MustCompile(
	`\bChannelTopic\s*\(\s*["']([^"'\n\r]+)["']`,
)

// springRedisPatternTopicRe captures
//
//	`new PatternTopic("pattern")` — wildcard subscription.
//
// Group 1 = pattern.
var springRedisPatternTopicRe = regexp.MustCompile(
	`\bPatternTopic\s*\(\s*["']([^"'\n\r]+)["']`,
)

func synthesizeSpringRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Guard: must reference Spring Redis or Redis pub/sub tokens.
	if !strings.Contains(src, "convertAndSend") &&
		!strings.Contains(src, "ChannelTopic") &&
		!strings.Contains(src, "PatternTopic") &&
		!strings.Contains(src, "MessageListenerAdapter") {
		return
	}

	// Extract the enclosing class name for the caller entity.
	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// Publisher: redisTemplate.convertAndSend("channel", payload)
	for _, m := range springRedisConvertAndSendRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "spring-data-redis"})
		caller := className
		if caller == "" {
			// Fallback: use enclosing method or a generic sentinel.
			caller = findFollowingMethod(src, m[0])
		}
		if caller == "" {
			continue
		}
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"messaging_layer": "spring-data-redis",
			"channel_type":    "pubsub",
		})
	}

	// Consumer: container.addMessageListener(adapter, new ChannelTopic("channel"))
	for _, m := range springRedisChannelTopicRe.FindAllStringSubmatchIndex(src, -1) {
		ch := src[m[2]:m[3]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "spring-data-redis"})
		caller := className
		if caller == "" {
			caller = "listener"
		}
		emitEdge("Service", caller, id, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "spring-data-redis",
			"channel_type":    "pubsub",
		})
	}

	// Consumer: container.addMessageListener(adapter, new PatternTopic("pattern"))
	for _, m := range springRedisPatternTopicRe.FindAllStringSubmatchIndex(src, -1) {
		pattern := src[m[2]:m[3]]
		if !looksLikeRedisChannel(pattern) {
			continue
		}
		id := redisPubSubChannelID(pattern)
		emitChannel(id, pattern, "pubsub", true, map[string]string{"messaging_layer": "spring-data-redis"})
		caller := className
		if caller == "" {
			caller = "listener"
		}
		emitEdge("Service", caller, id, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "spring-data-redis",
			"channel_type":    "pubsub",
			"is_pattern":      "true",
		})
	}
}

// ---------------------------------------------------------------------------
// Elixir — Redix.PubSub (#1489)
// ---------------------------------------------------------------------------
//
// The real polyglot fixture's realtime-dashboard (Phoenix/Elixir) consumes the
// `notifications.push` Redis channel via:
//
//	@redis_channel "notifications.push"
//	{:ok, pubsub} = Redix.PubSub.start_link(...)
//	{:ok, _ref} = Redix.PubSub.subscribe(pubsub, @redis_channel, self())
//
// and may also publish via Redix.command(conn, ["PUBLISH", "chan", msg]).
// Before #1489 Elixir was unsupported, so realtime-dashboard emitted no
// SCOPE.Queue entity and never paired with the Kotlin notifications publisher.
// This synthesizer resolves module-attribute channel constants
// (`@name "value"`) and string literals on Redix.PubSub.subscribe /
// .psubscribe and Redix PUBLISH commands, emitting the SAME canonical
// `channel:redis-pubsub:<name>` SCOPE.Queue ID so P7 joins it cross-repo.

// Note: module-attribute constants (`@name "value"`) are parsed with
// elixirModuleAttrRe, defined in http_endpoint_jsts_client_1483.go.

// elixirRedixSubscribeRe captures Redix.PubSub.subscribe / .psubscribe with a
// string-literal or @module-attribute channel argument (after the pubsub conn
// arg). Group 1 = "" or method suffix, Group 2 = literal, Group 3 = @attr.
var elixirRedixSubscribeRe = regexp.MustCompile(
	`Redix\.PubSub\.(p?subscribe)\s*\(\s*[^,]+,\s*(?:"([^"\n\r]+)"|@([a-z_][a-zA-Z0-9_]*))`,
)

// elixirRedixPublishRe captures Redix.command/Redix.noreply_command with a
// PUBLISH verb: `Redix.command(conn, ["PUBLISH", "chan", msg])` where the
// channel is a literal (group 1) or @attr (group 2).
var elixirRedixPublishRe = regexp.MustCompile(
	`(?i)Redix\.[a-z_]*command[a-z_!]*\s*\([^,]+,\s*\[\s*"PUBLISH"\s*,\s*(?:"([^"\n\r]+)"|@([a-z_][a-zA-Z0-9_]*))`,
)

// elixirModuleNameRe captures the enclosing `defmodule Foo.Bar do` so subscribe
// edges carry a stable per-file caller name.
var elixirModuleNameRe = regexp.MustCompile(`defmodule\s+([A-Za-z0-9_.]+)\s+do`)

func synthesizeElixirRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "Redix") {
		return
	}

	// Build the module-attribute constant table (@redis_channel "x").
	attrs := map[string]string{}
	for _, m := range elixirModuleAttrRe.FindAllStringSubmatch(src, -1) {
		attrs[m[1]] = m[2]
	}

	caller := "module"
	if m := elixirModuleNameRe.FindStringSubmatch(src); len(m) >= 2 {
		caller = m[1]
	}

	resolve := func(lit, attr string) (string, bool) {
		if lit != "" {
			return lit, true
		}
		if attr == "" {
			return "", false
		}
		v, ok := attrs[attr]
		return v, ok
	}

	// Subscribers: Redix.PubSub.subscribe / psubscribe.
	for _, m := range elixirRedixSubscribeRe.FindAllStringSubmatch(src, -1) {
		isPattern := m[1] == "psubscribe"
		ch, ok := resolve(m[2], m[3])
		if !ok || !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", isPattern, map[string]string{"messaging_layer": "redix"})
		emitEdge("Service", caller, id, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "redix",
			"channel_type":    "pubsub",
			"is_pattern":      boolStr(isPattern),
		})
	}

	// Publishers: Redix.command(conn, ["PUBLISH", chan, msg]).
	for _, m := range elixirRedixPublishRe.FindAllStringSubmatch(src, -1) {
		ch, ok := resolve(m[1], m[2])
		if !ok || !looksLikeRedisChannel(ch) {
			continue
		}
		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", false, map[string]string{"messaging_layer": "redix"})
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"messaging_layer": "redix",
			"channel_type":    "pubsub",
		})
	}
}

// ---------------------------------------------------------------------------
// C# — StackExchange.Redis ISubscriber (#5016)
// ---------------------------------------------------------------------------
//
// StackExchange.Redis exposes pub/sub through the multiplexer's ISubscriber.
// Before #5016 the C# Redis extractor (internal/custom/csharp/redis.go)
// recorded Publish/Subscribe only as a per-file ORM keyspace access
// (SCOPE.Datastore + PUBLISHES_TO/SUBSCRIBES_TO to a Datastore node) — useful
// for data-access topology but invisible to the cross-repo message_broker
// view. This synthesizer adds the broker view: it emits the SAME canonical
// `channel:redis-pubsub:<name>` SCOPE.Queue entity the other languages use, so
// a C# publisher and a (e.g. Node/Python/Kotlin) subscriber on the same
// channel pair up cross-repo via the import-channel linker (P7) — no new
// matching code.
//
// Producer:  sub.Publish("events", payload)
//            sub.PublishAsync("events", payload)
//            sub.Publish(RedisChannel.Literal("events"), payload)
//            sub.Publish(new RedisChannel("events", RedisChannel.PatternMode.Literal), p)
// Consumer:  sub.Subscribe("events", handler)
//            sub.SubscribeAsync("events", handler)
//            sub.Subscribe(RedisChannel.Pattern("orders.*"), handler)  // wildcard
//
// The first argument is the RedisChannel. We capture the first string literal
// after the verb (covering both a bare "events" and the literal nested inside
// RedisChannel.Literal(...) / RedisChannel.Pattern(...) / new RedisChannel(...)).
// Pattern subscriptions are flagged via a trailing/embedded `*` wildcard or an
// explicit RedisChannel.Pattern(...) / PatternMode.Pattern marker.

// csRedisPubSubRe captures the StackExchange.Redis ISubscriber verb plus the
// first string literal argument. Group 1 = verb (Publish/PublishAsync/
// Subscribe/SubscribeAsync), group 2 = the channel literal. The optional
// `RedisChannel.Literal(` / `RedisChannel.Pattern(` / `new RedisChannel(`
// wrapper before the literal is matched non-capturing so both bare and
// wrapped channel forms resolve to the same name.
var csRedisPubSubRe = regexp.MustCompile(
	`\.(Publish|PublishAsync|Subscribe|SubscribeAsync)\s*\(\s*(?:(?:new\s+)?RedisChannel\s*(?:\.\s*(?:Literal|Pattern))?\s*\(\s*)?"([^"\n\r]+)"`,
)

func synthesizeCSharpRedisPubSub(
	src string,
	emitChannel func(channelID, channelName, channelType string, isWildcard bool, props map[string]string),
	emitEdge func(callerKind, callerName, channelID, edgeKind string, props map[string]string),
) {
	// Guard: must reference StackExchange.Redis ISubscriber pub/sub tokens.
	if !strings.Contains(src, ".Publish") && !strings.Contains(src, ".Subscribe") {
		return
	}

	// Enclosing class name for a stable per-file caller entity.
	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	for _, m := range csRedisPubSubRe.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ch := src[m[4]:m[5]]
		if !looksLikeRedisChannel(ch) {
			continue
		}
		isPublish := strings.HasPrefix(verb, "Publish")
		// A wildcard `*` makes this a pattern subscription (psubscribe-style).
		// RedisChannel.Pattern(...) also implies a pattern; both surface as a
		// `*`-bearing literal in practice, so the glob check covers it.
		isWildcard := strings.Contains(ch, "*")

		caller := className
		if caller == "" {
			caller = findFollowingMethod(src, m[0])
		}
		if caller == "" {
			continue
		}

		id := redisPubSubChannelID(ch)
		emitChannel(id, ch, "pubsub", isWildcard, map[string]string{
			"messaging_layer": "stackexchange-redis",
		})

		edge := subscribesToEdgeKind
		if isPublish {
			edge = publishesToEdgeKind
		}
		props := map[string]string{
			"messaging_layer": "stackexchange-redis",
			"channel_type":    "pubsub",
		}
		if isWildcard {
			props["is_pattern"] = "true"
		}
		emitEdge("Service", caller, id, edge, props)
	}
}
