// Kafka wrapper + transport idiom detection — #1467.
//
// This pass extends the topic-extraction layer with four new detection
// families that the baseline kafka_edges.go / sqs_edges.go missed:
//
//  1. Python KafkaBus wrapper (py_shared.KafkaBus or any class named *Bus /
//     *KafkaBus): `bus.publish(topic, ...)` emits PUBLISHES_TO,
//     `bus.consumer(topic)` / `bus.subscribe(topic)` emits SUBSCRIBES_TO.
//     Used by orders / analytics / semantic-search / workers / order-saga /
//     ledger in the ShipFast fixture.
//
//  2. Java / Kotlin Spring Kafka — array-form @KafkaListener:
//     `@KafkaListener(topics = {"orders.placed", "payments.settled"})`.
//     The existing springKafkaListenerRe already handles the single-literal
//     form; this extends it with the brace-list form already in the regex
//     but exercises a previously untested code path (tests confirmed the
//     regex group 1 fires for brace lists).
//     Also: Spring RedisTemplate `convertAndSend("channel", msg)` emits a
//     `redis:` MessageTopic for the notifications service.
//
//  3. Java Kafka Streams: `builder.stream("topic")` (consumer) and
//     `stream.to("topic")` / `KStream.to("topic")` (producer). Emits
//     MessageTopic entities for the derived topics orders.enriched /
//     orders.high_value produced by the stream-processor service.
//
//  4. AWS SNS `sns.publish(TopicArn=..., ...)` (Python boto3) and
//     `snsClient.publish(new PublishRequest("arn:...", message))` (Java).
//     Extracts the topic name from the ARN suffix and emits a
//     `sns:<topic-name>` MessageTopic so cross-repo P7 links fire against
//     sns:payments.settled / sns:inventory.reserved in the ShipFast fixture.
//     Node/Go SNS publish patterns added for completeness.
//
// All sub-detectors are append-only — they never modify or remove existing
// entities or edges, so they cannot regress the surrounding pipeline's
// bug-rate.
//
// Integration: applyKafkaWrapperEdges is called from detector.go immediately
// after applyKafkaEdges, before applyRabbitMQEdges.
//
// Refs #1467.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// kafkaWrapperSynthesisSupportsLanguage reports whether applyKafkaWrapperEdges
// can emit synthetics for `lang`.
func kafkaWrapperSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "java", "kotlin", "javascript", "typescript", "go":
		return true
	default:
		return false
	}
}

// applyKafkaWrapperEdges runs after applyKafkaEdges and APPENDS MessageTopic
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges for the idioms listed above.
// It never modifies or removes existing entities or edges.
func applyKafkaWrapperEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !kafkaWrapperSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	seenTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(topicID, topicName, broker string, props map[string]string) {
		if seenTopic[topicID] {
			return
		}
		seenTopic[topicID] = true
		merged := map[string]string{
			"broker":          broker,
			"topic_name":      topicName,
			"pattern_type":    "kafka_wrapper_synthesis",
			"runtime_dynamic": "false",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               topicID,
			Kind:               messageTopicKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, topicID, edgeKind string, props map[string]string) {
		if callerName == "" || topicID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + topicID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"pattern_type": "kafka_wrapper_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", messageTopicKind, topicID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePyKafkaBusWrapper(src, emitTopic, emitEdge)
		synthesizePySNSPublish(src, emitTopic, emitEdge)
	case "java", "kotlin":
		synthesizeJavaKafkaStreams(src, emitTopic, emitEdge)
		synthesizeJavaRedisConvertAndSend(src, emitTopic, emitEdge)
		synthesizeJavaSNSPublish(src, emitTopic, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeSNSPublish(src, emitTopic, emitEdge)
	case "go":
		synthesizeGoSNSPublish(src, emitTopic, emitEdge)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// 1. Python KafkaBus / generic bus wrapper
// ---------------------------------------------------------------------------

// pyBusPublishRe captures `bus.publish("topic", ...)` where `bus` is any
// variable name. This covers py_shared.KafkaBus and similar wrapper classes
// that wrap confluent-kafka behind a higher-level API.
// Group 1 = topic name (string literal).
var pyBusPublishRe = regexp.MustCompile(`\bpublish\s*\(\s*["']([^"'\n\r]+)["']`)

// pyBusConsumerRe captures `bus.consumer("topic", ...)` and
// `bus.subscribe("topic", ...)` wrapper consumer shapes.
// Group 1 = topic name (string literal).
var pyBusConsumerRe = regexp.MustCompile(`\b(?:consumer|subscribe)\s*\(\s*["']([^"'\n\r]+)["']`)

// pyKafkaBusClassRe detects that the file either imports or defines a
// KafkaBus-style wrapper so the generic .publish() / .consumer() regexes
// above don't fire on unrelated code (e.g. a Flask route's .publish() call).
var pyKafkaBusClassRe = regexp.MustCompile(
	`(?:KafkaBus|kafka_bus|EventBus|MessageBus|BusAdapter|AsyncBus|from\s+\S*bus\S*\s+import|import\s+\S*bus\S*)`,
)

func synthesizePyKafkaBusWrapper(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	// Only fire when the file contains a bus-wrapper class reference.
	if !pyKafkaBusClassRe.MatchString(src) {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// Producer: publish("topic", ...)
	for _, m := range pyBusPublishRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(topic) {
			continue
		}
		id := kafkaTopicID(topic)
		emitTopic(id, topic, "kafka", map[string]string{"messaging_layer": "kafka_bus_wrapper"})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_bus_wrapper",
		})
	}

	// Consumer: consumer("topic", ...) or subscribe("topic", ...)
	for _, m := range pyBusConsumerRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(topic) {
			continue
		}
		id := kafkaTopicID(topic)
		emitTopic(id, topic, "kafka", map[string]string{"messaging_layer": "kafka_bus_wrapper"})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_bus_wrapper",
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Java / Kotlin — Spring RedisTemplate.convertAndSend
// ---------------------------------------------------------------------------

// javaRedisConvertAndSendRe captures
// `redisTemplate.convertAndSend("channel", payload)` which is the Spring
// Redis pub/sub publisher. Group 1 = channel name.
var javaRedisConvertAndSendRe = regexp.MustCompile(
	`\.convertAndSend\s*\(\s*["']([^"'\n\r]+)["']`,
)

// javaRedisListenerRe captures Spring `@RedisListener(topics = "channel")` or
// `@RedisListener(channels = "channel")` — Group 1 = channel name.
var javaRedisListenerRe = regexp.MustCompile(
	`@RedisListener\s*\([^)]*?(?:topics|channels)\s*=\s*["']([^"'\n\r]+)["']`,
)

func synthesizeJavaRedisConvertAndSend(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "convertAndSend") && !strings.Contains(src, "RedisListener") {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// Publisher: redisTemplate.convertAndSend("channel", payload)
	for _, m := range javaRedisConvertAndSendRe.FindAllStringSubmatchIndex(src, -1) {
		channel := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(channel) {
			continue
		}
		// Emit as `redis:<channel>` so it matches redis pub/sub topic IDs used
		// by the P7 cross-repo linker.
		id := "redis:" + channel
		emitTopic(id, channel, "redis", map[string]string{"messaging_layer": "spring_redis"})
		caller := className
		if caller == "" {
			continue
		}
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"broker":          "redis",
			"messaging_layer": "spring_redis",
		})
	}

	// Consumer: @RedisListener(topics="channel")
	for _, m := range javaRedisListenerRe.FindAllStringSubmatchIndex(src, -1) {
		channel := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(channel) {
			continue
		}
		id := "redis:" + channel
		emitTopic(id, channel, "redis", map[string]string{"messaging_layer": "spring_redis"})
		methodName := findFollowingMethod(src, m[0])
		if methodName == "" {
			methodName = className
		}
		callerName := methodName
		if className != "" && methodName != className {
			callerName = className + "." + methodName
		}
		if callerName == "" {
			continue
		}
		emitEdge("SCOPE.Operation", callerName, id, subscribesToEdgeKind, map[string]string{
			"broker":          "redis",
			"messaging_layer": "spring_redis",
		})
	}
}

// ---------------------------------------------------------------------------
// 3. Java Kafka Streams: builder.stream("topic") / kStream.to("topic") /
//    kStream.through("topic") / chained .filter().to() / .branch()[n].to()
// ---------------------------------------------------------------------------

// javaKafkaStreamBuilderRe captures `builder.stream("topic")` and
// `streams.builder().stream("topic")` — the source topic read by a topology.
// Group 1 = topic name.
var javaKafkaStreamBuilderRe = regexp.MustCompile(
	`\.stream\s*\(\s*"([^"\n\r]+)"`,
)

// javaKafkaStreamToRe captures `kStream.to("topic")` — the sink topic written
// by a Kafka Streams topology. Also fires for chained forms like
// `.filter(...).to("topic")`, `.mapValues(...).to("topic")`, and
// `.branch(...)[n].to("topic")`. Group 1 = topic name.
var javaKafkaStreamToRe = regexp.MustCompile(
	`\.to\s*\(\s*"([^"\n\r]+)"`,
)

// javaKafkaStreamThroughRe captures `kStream.through("topic")` — the
// rerouting operator that both consumes from and produces to an intermediate
// topic, acting as both source and sink in the topology.
// Group 1 = topic name.
var javaKafkaStreamThroughRe = regexp.MustCompile(
	`\.through\s*\(\s*"([^"\n\r]+)"`,
)

func synthesizeJavaKafkaStreams(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	// Guard: must reference Kafka Streams tokens.
	if !strings.Contains(src, "KafkaStreams") &&
		!strings.Contains(src, "StreamsBuilder") &&
		!strings.Contains(src, "KStream") &&
		!strings.Contains(src, "kafka_streams") {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}
	caller := className
	if caller == "" {
		caller = "streams"
	}

	// Source topics: builder.stream("topic") — consumer side.
	for _, m := range javaKafkaStreamBuilderRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(topic) {
			continue
		}
		id := kafkaTopicID(topic)
		emitTopic(id, topic, "kafka", map[string]string{"messaging_layer": "kafka_streams", "stream_role": "source"})
		emitEdge("Service", caller, id, subscribesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_streams",
			"stream_role":     "source",
		})
	}

	// Sink topics: kStream.to("topic") — producer side.
	// Also fires for chained DSL forms: .filter(...).to("x"),
	// .mapValues(...).to("x"), .branch(...)[n].to("x").
	for _, m := range javaKafkaStreamToRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(topic) {
			continue
		}
		// Avoid matching Java method calls that happen to look like .to("…")
		// but are not Kafka Streams topology calls. Heuristic: if the
		// surrounding ~300 chars contain KStream / KTable / topology tokens,
		// it's safe to claim this.
		ctx := surroundingText(src, m[0], 300)
		if !strings.Contains(ctx, "KStream") &&
			!strings.Contains(ctx, "KTable") &&
			!strings.Contains(ctx, "StreamsBuilder") &&
			!strings.Contains(ctx, ".through(") &&
			!strings.Contains(ctx, ".branch(") &&
			!strings.Contains(ctx, ".filter(") &&
			!strings.Contains(ctx, ".mapValues(") &&
			!strings.Contains(ctx, "topology") &&
			!strings.Contains(src, "KafkaStreams") {
			continue
		}
		id := kafkaTopicID(topic)
		emitTopic(id, topic, "kafka", map[string]string{"messaging_layer": "kafka_streams", "stream_role": "sink"})
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_streams",
			"stream_role":     "sink",
		})
	}

	// Through topics: kStream.through("topic") — intermediate rerouting.
	// The topology PUBLISHES_TO the through-topic AND SUBSCRIBES_TO it
	// (Kafka Streams writes records to the through-topic and then reads them
	// back internally). Emit both edges so cross-repo linkers can match either
	// side of the through-topic connection.
	for _, m := range javaKafkaStreamThroughRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		if !looksLikeKafkaTopic(topic) {
			continue
		}
		id := kafkaTopicID(topic)
		emitTopic(id, topic, "kafka", map[string]string{"messaging_layer": "kafka_streams", "stream_role": "through"})
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_streams",
			"stream_role":     "through",
		})
		emitEdge("Service", caller, id, subscribesToEdgeKind, map[string]string{
			"broker":          "kafka",
			"messaging_layer": "kafka_streams",
			"stream_role":     "through",
		})
	}
}

// ---------------------------------------------------------------------------
// 4. AWS SNS publish — emit sns:<topic-name> MessageTopic
// ---------------------------------------------------------------------------

// snsTopicNameFromARN extracts the human-readable topic name from an SNS ARN.
// `arn:aws:sns:us-east-1:123456789012:payments-settled` → `payments-settled`.
// Returns the input unchanged when it is not an ARN (already a bare name).
func snsTopicNameFromARN(arn string) string {
	// ARN form: arn:aws:sns:<region>:<account>:<topic-name>
	if !strings.HasPrefix(arn, "arn:") {
		return arn
	}
	parts := strings.Split(arn, ":")
	if len(parts) >= 6 {
		return parts[len(parts)-1]
	}
	return arn
}

// snsTopicID returns the canonical `sns:<topic-name>` entity ID.
func snsTopicID(topicName string) string {
	return "sns:" + topicName
}

// pySNSPublishRe captures Python boto3 with a literal TopicArn:
//
//	sns.publish(TopicArn="arn:...", Message="...")
//	client.publish(TopicArn='payments-settled', ...)
//
// Group 1 = TopicArn value (ARN or bare name).
var pySNSPublishLiteralRe = regexp.MustCompile(
	`\.publish\s*\([^)]*?TopicArn\s*=\s*["']([^"'\n\r]+)["']`,
)

// pySNSPublishVarRe captures TopicArn=VARIABLE_NAME (constant reference).
// Group 1 = variable name.
var pySNSPublishVarRe = regexp.MustCompile(
	`\.publish\s*\([^)]*?TopicArn\s*=\s*([A-Z_][A-Z0-9_]*)`,
)

// pySNSSubscribeTopicArnLiteralRe captures Python boto3 subscriber with a
// literal TopicArn. Group 1 = TopicArn value.
var pySNSSubscribeTopicArnLiteralRe = regexp.MustCompile(
	`\.subscribe\s*\([^)]*?TopicArn\s*=\s*["']([^"'\n\r]+)["']`,
)

// pySNSSubscribeTopicArnVarRe captures TopicArn=VARIABLE_NAME on subscribe.
// Group 1 = variable name.
var pySNSSubscribeTopicArnVarRe = regexp.MustCompile(
	`\.subscribe\s*\([^)]*?TopicArn\s*=\s*([A-Z_][A-Z0-9_]*)`,
)

func synthesizePySNSPublish(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "TopicArn") {
		return
	}

	// Build module-level constant table (NAME = 'value') for ARN resolution.
	consts := map[string]string{}
	for _, m := range pyConstStringRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 3 {
			consts[m[1]] = m[2]
		}
	}

	// resolveARN tries to get a literal ARN/name from the matched value,
	// falling back to the constant table.
	resolveARN := func(literal, varName string) string {
		if literal != "" {
			return literal
		}
		if v, ok := consts[varName]; ok {
			return v
		}
		return ""
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// Publisher: sns.publish(TopicArn=... ) — literal form.
	for _, m := range pySNSPublishLiteralRe.FindAllStringSubmatchIndex(src, -1) {
		arn := resolveARN(src[m[2]:m[3]], "")
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{"messaging_layer": "boto3_sns", "arn": arn})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{
			"broker": "sns", "messaging_layer": "boto3_sns",
		})
	}

	// Publisher: sns.publish(TopicArn=VARIABLE) — constant-resolved form.
	for _, m := range pySNSPublishVarRe.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		arn := resolveARN("", varName)
		if arn == "" {
			continue
		}
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{"messaging_layer": "boto3_sns", "arn": arn})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{
			"broker": "sns", "messaging_layer": "boto3_sns",
		})
	}

	// Subscriber: sns.subscribe(TopicArn=...) — literal form.
	for _, m := range pySNSSubscribeTopicArnLiteralRe.FindAllStringSubmatchIndex(src, -1) {
		arn := resolveARN(src[m[2]:m[3]], "")
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{"messaging_layer": "boto3_sns", "arn": arn})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{
			"broker": "sns", "messaging_layer": "boto3_sns",
		})
	}

	// Subscriber: sns.subscribe(TopicArn=VARIABLE) — constant-resolved form.
	for _, m := range pySNSSubscribeTopicArnVarRe.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		arn := resolveARN("", varName)
		if arn == "" {
			continue
		}
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{"messaging_layer": "boto3_sns", "arn": arn})
		emitEdge("Function", enclosing(m[0]), id, subscribesToEdgeKind, map[string]string{
			"broker": "sns", "messaging_layer": "boto3_sns",
		})
	}
}

// javaAWSSNSPublishRe captures Java/Kotlin AWS SDK:
//
//	snsClient.publish(new PublishRequest("arn:...", message))
//	snsClient.publish(PublishRequest.builder().topicArn("arn:...").build())
//	snsClient.publish(r -> r.topicArn("payments-settled").message("..."))
//
// Group 1 = topicArn value (from either `topicArn(` or `new PublishRequest(` first arg).
var javaAWSSNSPublishRe = regexp.MustCompile(
	`(?:topicArn\s*\(\s*["']([^"'\n\r]+)["']|new\s+PublishRequest\s*\(\s*["']([^"'\n\r]+)["'])`,
)

func synthesizeJavaSNSPublish(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "SnsClient") &&
		!strings.Contains(src, "AmazonSNS") &&
		!strings.Contains(src, "PublishRequest") &&
		!strings.Contains(src, "topicArn") {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	for _, m := range javaAWSSNSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		// Group 1 or group 2 depending on which alternative matched.
		arn := ""
		if m[2] != -1 {
			arn = src[m[2]:m[3]]
		} else if m[4] != -1 {
			arn = src[m[4]:m[5]]
		}
		if arn == "" {
			continue
		}
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{
			"messaging_layer": "aws-sdk-java",
			"arn":             arn,
		})
		caller := className
		if caller == "" {
			caller = "unknown"
		}
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"broker":          "sns",
			"messaging_layer": "aws-sdk-java",
		})
	}
}

// nodeAWSSNSPublishRe captures Node/TS AWS SDK v2/v3:
//
//	sns.publish({ TopicArn: "arn:...", Message: "..." }).promise()
//	new PublishCommand({ TopicArn: "arn:...", Message: "..." })
//
// Group 1 = TopicArn value.
var nodeAWSSNSPublishRe = regexp.MustCompile(
	`TopicArn\s*:\s*["` + "`" + `']([^"` + "`" + `'\n\r]+)["` + "`" + `']`,
)

func synthesizeNodeSNSPublish(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "TopicArn") {
		return
	}
	// Guard: must reference SNS publish tokens to avoid matching SQS code
	// that happens to use TopicArn as a subscription attribute.
	if !strings.Contains(src, "SNS") && !strings.Contains(src, "sns") &&
		!strings.Contains(src, "PublishCommand") && !strings.Contains(src, "PublishRequest") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	for _, m := range nodeAWSSNSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		arn := src[m[2]:m[3]]
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{
			"messaging_layer": "aws-sdk-js",
			"arn":             arn,
		})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{
			"broker":          "sns",
			"messaging_layer": "aws-sdk-js",
		})
	}
}

// goAWSSNSPublishRe captures Go AWS SDK v2:
//
//	snsClient.Publish(ctx, &sns.PublishInput{TopicArn: aws.String("arn:..."), ...})
//
// Group 1 = TopicArn value.
var goAWSSNSPublishRe = regexp.MustCompile(
	`TopicArn\s*:\s*(?:aws\.String\s*\(\s*)?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`,
)

func synthesizeGoSNSPublish(
	src string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "TopicArn") {
		return
	}
	if !strings.Contains(src, "sns.") && !strings.Contains(src, "SNS") &&
		!strings.Contains(src, "PublishInput") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	for _, m := range goAWSSNSPublishRe.FindAllStringSubmatchIndex(src, -1) {
		arn := src[m[2]:m[3]]
		topicName := snsTopicNameFromARN(arn)
		if !looksLikeKafkaTopic(topicName) {
			continue
		}
		id := snsTopicID(topicName)
		emitTopic(id, topicName, "sns", map[string]string{
			"messaging_layer": "aws-sdk-go-v2",
			"arn":             arn,
		})
		emitEdge("Function", enclosing(m[0]), id, publishesToEdgeKind, map[string]string{
			"broker":          "sns",
			"messaging_layer": "aws-sdk-go-v2",
		})
	}
}
