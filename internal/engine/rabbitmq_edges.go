// RabbitMQ producer/consumer detection — wave 2 of #726.
//
// For every RabbitMQ publish or consume call site this pass can statically
// recognize, we emit a synthetic `SCOPE.Queue` entity keyed by the queue
// name, plus PUBLISHES_TO or SUBSCRIBES_TO edges from the calling method
// to that queue. The synthetic queue ID is identical across repos
// (`rabbitmq:<queue-name>`), so the existing import-channel linker matches
// producer and consumer sides on shared entity ID without any new cross-repo
// matching code — same trick used by kafka_edges.go (#726 wave 1).
//
// Libraries/frameworks covered:
//   - Python pika: channel.basic_publish / basic_consume / queue_declare
//   - Node amqplib: channel.publish / consume / assertQueue / sendToQueue
//   - Java RabbitMQ client: channel.basicPublish / basicConsume
//   - Go amqp091-go: channel.Publish / Consume / QueueDeclare
//   - Spring AMQP: @RabbitListener / rabbitTemplate.convertAndSend
//   - Quarkus @Incoming/@Outgoing backed by RabbitMQ connector
//   - Celery: @app.task on consumer functions with AMQP broker config
//
// Beyond the minimum:
//   - Exchange→queue binding edges (routing_key recorded as edge property)
//   - Celery @app.task consumer detection via AMQP broker URL
//   - Celery .apply_async / .delay producer edges
//   - Quarkus RabbitMQ connector distinction from Kafka connector
//
// Refs #726.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// queueEntityKind is the entity Kind for synthetic queue entities.
// Reuses the SCOPE.Queue kind already present in kinds.go (line 29).
// Wave 2 uses this for both RabbitMQ and SQS queues.
const queueEntityKind = "SCOPE.Queue"

// rabbitmqSynthesisSupportsLanguage reports whether applyRabbitMQEdges
// can emit synthetics for `lang`.
func rabbitmqSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "javascript", "typescript", "python", "go", "rust":
		return true
	default:
		return false
	}
}

// applyRabbitMQEdges runs after applyKafkaEdges and APPENDS SCOPE.Queue
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges. Append-only — never
// modifies or removes existing entities or edges, so this pass cannot
// regress the surrounding pipeline's bug-rate.
func applyRabbitMQEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !rabbitmqSynthesisSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Dedup-by-ID: one SCOPE.Queue entity per queue name per file.
	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}

	emitQueue := func(queueID, queueName, exchange, routingKey string, props map[string]string) {
		if seenQueue[queueID] {
			return
		}
		seenQueue[queueID] = true
		merged := map[string]string{
			"broker":       "rabbitmq",
			"queue_name":   queueName,
			"pattern_type": "rabbitmq_synthesis",
		}
		if exchange != "" {
			merged["exchange"] = exchange
		}
		if routingKey != "" {
			merged["routing_key"] = routingKey
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical queue names collapse to ONE
		// entity per repo and match across repos via the import-channel
		// linker (same technique as kafka_edges.go MessageTopic).
		entities = append(entities, types.EntityRecord{
			Name:               queueID,
			Kind:               queueEntityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, queueID, edgeKind string, props map[string]string) {
		if callerName == "" || queueID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + queueID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "rabbitmq",
			"pattern_type": "rabbitmq_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", queueEntityKind, queueID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePyRabbitMQ(src, emitQueue, emitEdge)
		synthesizePyAioPika(src, emitQueue, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeRabbitMQ(src, emitQueue, emitEdge)
	case "java", "kotlin":
		synthesizeJavaRabbitMQ(src, emitQueue, emitEdge)
	case "go":
		synthesizeGoRabbitMQ(src, emitQueue, emitEdge)
	case "rust":
		synthesizeRustLapin(src, emitQueue, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// rabbitmqQueueID returns the canonical synthetic ID for a RabbitMQ queue.
// Identical across repos so the cross-repo linker matches producer and
// consumer sides without any new linker code.
func rabbitmqQueueID(queue string) string {
	return "rabbitmq:" + queue
}

// looksLikeQueueName returns true when `s` plausibly looks like a
// RabbitMQ queue or routing key name. More permissive than Kafka topic
// names since RabbitMQ allows slashes and colons in routing keys.
func looksLikeQueueName(s string) bool {
	if s == "" || len(s) > 250 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<>{}") {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '.' || r == '_' || r == '-' || r == '/' || r == ':':
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// Python — pika + Celery
// ---------------------------------------------------------------------------

// pikaBasicPublishRe captures pika channel.basic_publish(exchange=X, routing_key=Y).
// Groups: 1=exchange, 2=routing_key.
var pikaBasicPublishRe = regexp.MustCompile(`\.basic_publish\s*\(\s*exchange\s*=\s*["']([^"'\n\r]*?)["']\s*,\s*routing_key\s*=\s*["']([^"'\n\r]+)["']`)

// pikaBasicPublishPosRe captures positional pika basic_publish(exchange, routing_key, body).
// Groups: 1=exchange, 2=routing_key.
var pikaBasicPublishPosRe = regexp.MustCompile(`\.basic_publish\s*\(\s*["']([^"'\n\r]*?)["']\s*,\s*["']([^"'\n\r]+)["']`)

// pikaBasicConsumeKwRe captures channel.basic_consume(queue=name, ...).
// Group 1 = queue name.
var pikaBasicConsumeKwRe = regexp.MustCompile(`\.basic_consume\s*\(\s*queue\s*=\s*["']([^"'\n\r]+)["']`)

// pikaBasicConsumePosRe captures channel.basic_consume(name, callback).
// Group 1 = queue name.
var pikaBasicConsumePosRe = regexp.MustCompile(`\.basic_consume\s*\(\s*["']([^"'\n\r]+)["']`)

// pikaQueueDeclareKwRe captures channel.queue_declare(queue=name).
// Group 1 = queue name.
var pikaQueueDeclareKwRe = regexp.MustCompile(`\.queue_declare\s*\(\s*queue\s*=\s*["']([^"'\n\r]+)["']`)

// pikaQueueDeclarePosRe captures channel.queue_declare(name).
// Group 1 = queue name.
var pikaQueueDeclarePosRe = regexp.MustCompile(`\.queue_declare\s*\(\s*["']([^"'\n\r]+)["']`)

// ---------------------------------------------------------------------------
// Python — aio-pika (async RabbitMQ) — #1638
//
// aio-pika is the de-facto async RabbitMQ client. Unlike pika it routes
// through an exchange object, so publishes look like:
//
//	await channel.default_exchange.publish(Message(...), routing_key="q")
//	await exchange.publish(msg, routing_key=QUEUE_CONST)
//
// and consumers / declarations look like:
//
//	queue = await channel.declare_queue("q", durable=True)
//	await queue.consume(handler)
//
// We resolve the queue name from the publish routing_key and from
// declare_queue, then attach consume() edges to the most recently declared
// queue in the same file (single-queue ETL workers are the common case).
// ---------------------------------------------------------------------------

// aioPikaPublishLitRe captures a routing_key="q" kwarg. aio-pika publishes
// pass a Message(...) positional arg whose own parens defeat a `.publish(...`
// anchored match, so we key on the routing_key kwarg directly. Presence of a
// `.publish(` somewhere in the file is verified by the caller guard.
// Group 1 = routing key (literal queue name).
var aioPikaPublishLitRe = regexp.MustCompile(`routing_key\s*=\s*["']([^"'\n\r]+)["']`)

// aioPikaPublishVarRe captures routing_key=QUEUE_CONST (resolved via consts).
// Group 1 = constant name.
var aioPikaPublishVarRe = regexp.MustCompile(`routing_key\s*=\s*([A-Za-z_][A-Za-z0-9_]*)`)

// aioPikaDeclareQueueLitRe captures await channel.declare_queue("q", ...).
// Group 1 = queue name (literal).
var aioPikaDeclareQueueLitRe = regexp.MustCompile(`\.declare_queue\s*\(\s*["']([^"'\n\r]+)["']`)

// aioPikaDeclareQueueVarRe captures await channel.declare_queue(QUEUE_CONST, ...).
// Group 1 = constant name.
var aioPikaDeclareQueueVarRe = regexp.MustCompile(`\.declare_queue\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

// aioPikaConsumeRe captures queue.consume(handler) — async consumer side.
var aioPikaConsumeRe = regexp.MustCompile(`\.consume\s*\(`)

// pyConstAssignRe captures module-level NAME = "value" string constants used
// to resolve queue-name references in aio-pika calls.
var pyConstAssignRe = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*["']([^"'\n\r]+)["']`)

// synthesizePyAioPika emits RabbitMQ producer/consumer edges for aio-pika.
func synthesizePyAioPika(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	isAioPika := strings.Contains(src, "aio_pika") || strings.Contains(src, "aio-pika")
	// declare_queue + consume without an aio_pika import is still aio-pika
	// (re-exported names), but plain pika never calls declare_queue/consume —
	// it uses queue_declare/basic_consume — so this is safe.
	if !isAioPika && !strings.Contains(src, "declare_queue") {
		return
	}

	// Build a module-level string-constant table for routing_key/queue refs.
	consts := map[string]string{}
	for _, m := range pyConstAssignRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 3 {
			consts[m[1]] = m[2]
		}
	}
	resolve := func(tok string) string {
		if v, ok := consts[tok]; ok {
			return v
		}
		return tok
	}

	enclosing := func(offset int) string { return findEnclosingPyName(src, offset) }

	// Track the last declared queue name so consume() can bind to it when the
	// consume call does not name a queue (aio-pika consumes on a queue object).
	lastDeclared := ""

	// declare_queue (literal + const) — emit the queue entity.
	declareNames := map[int]string{}
	for _, m := range aioPikaDeclareQueueLitRe.FindAllStringSubmatchIndex(src, -1) {
		declareNames[m[0]] = src[m[2]:m[3]]
	}
	for _, m := range aioPikaDeclareQueueVarRe.FindAllStringSubmatchIndex(src, -1) {
		if _, seen := declareNames[m[0]]; seen {
			continue
		}
		declareNames[m[0]] = resolve(src[m[2]:m[3]])
	}
	for off, name := range declareNames {
		if !looksLikeQueueName(name) {
			continue
		}
		_ = off
		lastDeclared = name
		emitQueue(rabbitmqQueueID(name), name, "", "", map[string]string{
			"declared":        "true",
			"messaging_layer": "aio-pika",
		})
	}

	// publish via routing_key (literal + const) — producer side. Only for
	// genuine aio-pika files: pika also uses routing_key= on basic_publish,
	// which is already handled by synthesizePyRabbitMQ — avoid double-counting.
	pubOffsets := map[int]string{}
	if isAioPika && strings.Contains(src, ".publish(") {
		for _, m := range aioPikaPublishLitRe.FindAllStringSubmatchIndex(src, -1) {
			pubOffsets[m[0]] = src[m[2]:m[3]]
		}
		for _, m := range aioPikaPublishVarRe.FindAllStringSubmatchIndex(src, -1) {
			if _, seen := pubOffsets[m[0]]; seen {
				continue
			}
			pubOffsets[m[0]] = resolve(src[m[2]:m[3]])
		}
	}
	for off, name := range pubOffsets {
		if !looksLikeQueueName(name) {
			continue
		}
		qID := rabbitmqQueueID(name)
		emitQueue(qID, name, "", name, map[string]string{"messaging_layer": "aio-pika"})
		emitEdge("Function", enclosing(off), qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "aio-pika",
			"routing_key":     name,
		})
	}

	// consume — consumer side, bound to the last declared queue in the file.
	if lastDeclared != "" {
		for _, m := range aioPikaConsumeRe.FindAllStringSubmatchIndex(src, -1) {
			// Skip the declare_queue/publish offsets already handled.
			qID := rabbitmqQueueID(lastDeclared)
			emitEdge("Function", enclosing(m[0]), qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "aio-pika",
			})
		}
	}
}

// celeryTaskRe captures @app.task or @celery.task decorated functions.
// Group 1 = function name.
var celeryTaskRe = regexp.MustCompile(`@(?:app|celery)\.task[^\n]*\n(?:@[^\n]+\n)*\s*def\s+(\w+)\s*\(`)

// celeryBrokerRe captures broker URL with rabbitmq/amqp scheme.
var celeryBrokerRe = regexp.MustCompile(`(?i)(?:BROKER_URL|broker_url|broker)\s*[=:]\s*["'](?:amqp|amqps)://`)

// celeryTaskSendRe captures .apply_async or .delay calls on task objects.
// Group 1 = task name.
var celeryTaskSendRe = regexp.MustCompile(`(\w+)\.(?:apply_async|delay)\s*\(`)

func synthesizePyRabbitMQ(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasPika := strings.Contains(src, "pika") || strings.Contains(src, "basic_publish") ||
		strings.Contains(src, "basic_consume") || strings.Contains(src, "queue_declare")
	hasCelery := strings.Contains(src, "celery") || strings.Contains(src, "@app.task")
	if !hasPika && !hasCelery {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	if hasPika {
		// basic_publish keyword form
		for _, m := range pikaBasicPublishRe.FindAllStringSubmatchIndex(src, -1) {
			exchange := src[m[2]:m[3]]
			routingKey := src[m[4]:m[5]]
			queueName := routingKey
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, exchange, routingKey, nil)
			caller := enclosing(m[0])
			emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "pika",
				"exchange":        exchange,
				"routing_key":     routingKey,
			})
		}
		// basic_publish positional form
		for _, m := range pikaBasicPublishPosRe.FindAllStringSubmatchIndex(src, -1) {
			exchange := src[m[2]:m[3]]
			routingKey := src[m[4]:m[5]]
			queueName := routingKey
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, exchange, routingKey, nil)
			caller := enclosing(m[0])
			emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "pika",
				"exchange":        exchange,
				"routing_key":     routingKey,
			})
		}
		// basic_consume keyword form
		for _, m := range pikaBasicConsumeKwRe.FindAllStringSubmatchIndex(src, -1) {
			queueName := src[m[2]:m[3]]
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, "", "", nil)
			caller := enclosing(m[0])
			emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "pika",
			})
		}
		// basic_consume positional form
		for _, m := range pikaBasicConsumePosRe.FindAllStringSubmatchIndex(src, -1) {
			queueName := src[m[2]:m[3]]
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, "", "", nil)
			caller := enclosing(m[0])
			emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "pika",
			})
		}
		// queue_declare keyword form
		for _, m := range pikaQueueDeclareKwRe.FindAllStringSubmatchIndex(src, -1) {
			queueName := src[m[2]:m[3]]
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, "", "", map[string]string{"declared": "true"})
		}
		// queue_declare positional form
		for _, m := range pikaQueueDeclarePosRe.FindAllStringSubmatchIndex(src, -1) {
			queueName := src[m[2]:m[3]]
			if !looksLikeQueueName(queueName) {
				continue
			}
			qID := rabbitmqQueueID(queueName)
			emitQueue(qID, queueName, "", "", map[string]string{"declared": "true"})
		}
	}

	if hasCelery {
		// Only treat tasks as RabbitMQ consumers when the file contains
		// an AMQP broker URL (otherwise could be Redis or SQS Celery).
		if celeryBrokerRe.MatchString(src) {
			for _, m := range celeryTaskRe.FindAllStringSubmatchIndex(src, -1) {
				taskName := src[m[2]:m[3]]
				// Celery task queue defaults to the task name.
				qID := rabbitmqQueueID(taskName)
				emitQueue(qID, taskName, "", "", map[string]string{"celery_task": "true"})
				emitEdge("Function", taskName, qID, subscribesToEdgeKind, map[string]string{
					"messaging_layer": "celery",
					"celery_task":     "true",
				})
			}
			// Celery .apply_async / .delay → producer side.
			for _, m := range celeryTaskSendRe.FindAllStringSubmatchIndex(src, -1) {
				taskName := src[m[2]:m[3]]
				qID := rabbitmqQueueID(taskName)
				caller := findEnclosingPyName(src, m[0])
				emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
					"messaging_layer": "celery",
				})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Node — amqplib
// ---------------------------------------------------------------------------

// nodeAmqpPublishRe captures channel.publish(exchange, routingKey, content).
// Groups: 1=exchange, 2=routingKey.
var nodeAmqpPublishRe = regexp.MustCompile("" +
	`\.publish\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]*?)[\"'`]" +
	`\s*,\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeAmqpConsumeRe captures channel.consume(queue, handler).
// Group 1 = queue name.
var nodeAmqpConsumeRe = regexp.MustCompile("" +
	`\.consume\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeAmqpAssertQueueRe captures channel.assertQueue(name).
// Group 1 = queue name.
var nodeAmqpAssertQueueRe = regexp.MustCompile("" +
	`\.assertQueue\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeAmqpSendToQueueRe captures channel.sendToQueue(queue, content).
// Group 1 = queue name.
var nodeAmqpSendToQueueRe = regexp.MustCompile("" +
	`\.sendToQueue\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

func synthesizeNodeRabbitMQ(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "amqplib") && !strings.Contains(src, "amqp") &&
		!strings.Contains(src, "assertQueue") && !strings.Contains(src, "sendToQueue") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	// channel.publish(exchange, routingKey, content)
	for _, m := range nodeAmqpPublishRe.FindAllStringSubmatchIndex(src, -1) {
		exchange := src[m[2]:m[3]]
		routingKey := src[m[4]:m[5]]
		queueName := routingKey
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, exchange, routingKey, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "amqplib",
			"exchange":        exchange,
			"routing_key":     routingKey,
		})
	}

	// channel.sendToQueue(queue, content) — direct queue publish
	for _, m := range nodeAmqpSendToQueueRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "amqplib",
		})
	}

	// channel.consume(queue, handler)
	for _, m := range nodeAmqpConsumeRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "amqplib",
		})
	}

	// channel.assertQueue(name) — queue declaration
	for _, m := range nodeAmqpAssertQueueRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", map[string]string{"declared": "true"})
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring AMQP + Quarkus + direct RabbitMQ client
// ---------------------------------------------------------------------------

// springRabbitListenerRe captures @RabbitListener(queues = "name") and
// @RabbitListener(queues = {"a","b"}) forms.
// Group 1 = brace-list body, Group 2 = single queue name.
var springRabbitListenerRe = regexp.MustCompile(`@RabbitListener\s*\([^)]*?queues\s*=\s*(?:\{([^}]+)\}|"([^"\n\r]+)")`)

// springRabbitTemplateSendRe captures rabbitTemplate.convertAndSend(exchange, routingKey, msg).
// Groups: 1=exchange, 2=routingKey.
var springRabbitTemplateSendRe = regexp.MustCompile(`rabbitTemplate\.convertAndSend\s*\(\s*"([^"\n\r]*?)"\s*,\s*"([^"\n\r]+)"`)

// javaRabbitBasicPublishRe captures channel.basicPublish(exchange, routingKey, props, body).
// Groups: 1=exchange, 2=routingKey.
var javaRabbitBasicPublishRe = regexp.MustCompile(`\.basicPublish\s*\(\s*"([^"\n\r]*?)"\s*,\s*"([^"\n\r]+)"`)

// javaRabbitBasicConsumeRe captures channel.basicConsume(queue, ...).
// Group 1 = queue name.
var javaRabbitBasicConsumeRe = regexp.MustCompile(`\.basicConsume\s*\(\s*"([^"\n\r]+)"`)

// quarkusRabbitIncomingRe / quarkusRabbitOutgoingRe match Quarkus
// @Incoming/@Outgoing annotations when the file references rabbitmq.
var quarkusRabbitIncomingRe = regexp.MustCompile(`@Incoming\s*\(\s*"([^"\n\r]+)"\s*\)`)
var quarkusRabbitOutgoingRe = regexp.MustCompile(`@Outgoing\s*\(\s*"([^"\n\r]+)"\s*\)`)

func synthesizeJavaRabbitMQ(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasRabbit := strings.Contains(src, "RabbitListener") ||
		strings.Contains(src, "rabbitTemplate") ||
		strings.Contains(src, "basicPublish") ||
		strings.Contains(src, "basicConsume") ||
		strings.Contains(src, "rabbitmq")
	if !hasRabbit {
		return
	}

	// Attempt to extract class name for entity attribution.
	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// @RabbitListener(queues = ...)
	for _, m := range springRabbitListenerRe.FindAllStringSubmatchIndex(src, -1) {
		methodName := findFollowingMethod(src, m[0])
		if methodName == "" {
			methodName = className
		}
		var queues []string
		if m[2] != -1 {
			for _, tok := range strings.Split(src[m[2]:m[3]], ",") {
				tok = strings.TrimSpace(tok)
				tok = strings.Trim(tok, `"'`)
				if tok != "" {
					queues = append(queues, tok)
				}
			}
		} else if m[4] != -1 {
			queues = append(queues, src[m[4]:m[5]])
		}
		for _, q := range queues {
			if !looksLikeQueueName(q) {
				continue
			}
			qID := rabbitmqQueueID(q)
			emitQueue(qID, q, "", "", map[string]string{"messaging_layer": "spring_amqp"})
			operationName := methodName
			if className != "" && methodName != className {
				operationName = className + "." + methodName
			}
			emitEdge("SCOPE.Operation", operationName, qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "spring_amqp",
			})
			if className != "" {
				emitEdge("Service", className, qID, subscribesToEdgeKind, map[string]string{
					"messaging_layer": "spring_amqp",
				})
			}
		}
	}

	// rabbitTemplate.convertAndSend(exchange, routingKey, msg)
	for _, m := range springRabbitTemplateSendRe.FindAllStringSubmatchIndex(src, -1) {
		exchange := src[m[2]:m[3]]
		routingKey := src[m[4]:m[5]]
		queueName := routingKey
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, exchange, routingKey, map[string]string{"messaging_layer": "spring_amqp"})
		if className != "" {
			emitEdge("Service", className, qID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "spring_amqp",
				"exchange":        exchange,
				"routing_key":     routingKey,
			})
		}
	}

	// channel.basicPublish(exchange, routingKey, props, body)
	for _, m := range javaRabbitBasicPublishRe.FindAllStringSubmatchIndex(src, -1) {
		exchange := src[m[2]:m[3]]
		routingKey := src[m[4]:m[5]]
		queueName := routingKey
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, exchange, routingKey, map[string]string{"messaging_layer": "java_rabbitmq_client"})
		if className != "" {
			emitEdge("Service", className, qID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "java_rabbitmq_client",
				"exchange":        exchange,
				"routing_key":     routingKey,
			})
		}
	}

	// channel.basicConsume(queue, ...)
	for _, m := range javaRabbitBasicConsumeRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", map[string]string{"messaging_layer": "java_rabbitmq_client"})
		if className != "" {
			emitEdge("Service", className, qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "java_rabbitmq_client",
			})
		}
	}

	// Quarkus @Incoming/@Outgoing backed by RabbitMQ connector.
	// Distinguish from Kafka by checking for "rabbitmq" in the file.
	isQuarkusRabbit := strings.Contains(src, "rabbitmq") &&
		(strings.Contains(src, "@Incoming") || strings.Contains(src, "@Outgoing"))
	if isQuarkusRabbit {
		for _, m := range quarkusRabbitIncomingRe.FindAllStringSubmatchIndex(src, -1) {
			channel := src[m[2]:m[3]]
			if !looksLikeQueueName(channel) {
				continue
			}
			qID := rabbitmqQueueID(channel)
			emitQueue(qID, channel, "", "", map[string]string{"messaging_layer": "quarkus_rabbitmq"})
			methodName := findFollowingMethod(src, m[0])
			if methodName == "" {
				methodName = className
			}
			operationName := methodName
			if className != "" && methodName != className {
				operationName = className + "." + methodName
			}
			emitEdge("SCOPE.Operation", operationName, qID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "quarkus_rabbitmq",
			})
		}
		for _, m := range quarkusRabbitOutgoingRe.FindAllStringSubmatchIndex(src, -1) {
			channel := src[m[2]:m[3]]
			if !looksLikeQueueName(channel) {
				continue
			}
			qID := rabbitmqQueueID(channel)
			emitQueue(qID, channel, "", "", map[string]string{"messaging_layer": "quarkus_rabbitmq"})
			methodName := findFollowingMethod(src, m[0])
			if methodName == "" {
				methodName = className
			}
			operationName := methodName
			if className != "" && methodName != className {
				operationName = className + "." + methodName
			}
			emitEdge("SCOPE.Operation", operationName, qID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "quarkus_rabbitmq",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Go — amqp091-go
// ---------------------------------------------------------------------------

// goAmqpPublishRe captures channel.Publish(exchange, routingKey, ...).
// Groups: 1=exchange, 2=routingKey.
var goAmqpPublishRe = regexp.MustCompile(`\.Publish\s*\(\s*"([^"\n\r]*?)"\s*,\s*"([^"\n\r]+)"`)

// goAmqpConsumeRe captures channel.Consume(queue, ...).
// Group 1 = queue name.
var goAmqpConsumeRe = regexp.MustCompile(`\.Consume\s*\(\s*"([^"\n\r]+)"`)

// goAmqpQueueDeclareRe captures ch.QueueDeclare(name, ...).
// Group 1 = queue name.
var goAmqpQueueDeclareRe = regexp.MustCompile(`\.QueueDeclare\s*\(\s*"([^"\n\r]+)"`)

func synthesizeGoRabbitMQ(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasAmqp := strings.Contains(src, "amqp") || strings.Contains(src, "rabbitmq")
	if !hasAmqp {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// channel.Publish(exchange, routingKey, ...)
	for _, m := range goAmqpPublishRe.FindAllStringSubmatchIndex(src, -1) {
		exchange := src[m[2]:m[3]]
		routingKey := src[m[4]:m[5]]
		queueName := routingKey
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, exchange, routingKey, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "amqp091-go",
			"exchange":        exchange,
			"routing_key":     routingKey,
		})
	}

	// channel.Consume(queue, ...)
	for _, m := range goAmqpConsumeRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "amqp091-go",
		})
	}

	// ch.QueueDeclare(name, ...)
	for _, m := range goAmqpQueueDeclareRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", map[string]string{"declared": "true"})
	}
}

// ---------------------------------------------------------------------------
// Rust — lapin (async AMQP / RabbitMQ client)  #3558
// ---------------------------------------------------------------------------
//
// lapin's Channel API is positional:
//
//   Publish:
//     channel.basic_publish(
//         "exchange",        // exchange
//         "routing_key",     // routing key
//         BasicPublishOptions::default(),
//         payload,
//         BasicProperties::default(),
//     )
//
//   Consume:
//     channel.basic_consume(
//         "queue",           // queue name
//         "consumer_tag",
//         BasicConsumeOptions::default(),
//         FieldTable::default(),
//     )
//
//   Declare (records the queue node even without pub/sub at this site):
//     channel.queue_declare("queue", QueueDeclareOptions::default(), FieldTable::default())
//
// Caller attribution mirrors the rdkafka Rust pass: SCOPE.Operation:<fn-name>
// for the nearest enclosing `fn`. findEnclosingRustFnName lives in kafka_edges.go.

// rustLapinPublishRe captures lapin `basic_publish("exchange", "routing_key", ...)`.
// Group 1 = exchange (may be empty for the default exchange), group 2 = routing key.
var rustLapinPublishRe = regexp.MustCompile(
	`\.basic_publish\s*\(\s*"([^"\n\r]*)"\s*,\s*"([^"\n\r]+)"`,
)

// rustLapinConsumeRe captures lapin `basic_consume("queue", ...)`.
// Group 1 = queue name.
var rustLapinConsumeRe = regexp.MustCompile(
	`\.basic_consume\s*\(\s*"([^"\n\r]+)"`,
)

// rustLapinQueueDeclareRe captures lapin `queue_declare("queue", ...)`.
// Group 1 = queue name.
var rustLapinQueueDeclareRe = regexp.MustCompile(
	`\.queue_declare\s*\(\s*"([^"\n\r]+)"`,
)

func synthesizeRustLapin(
	src string,
	emitQueue func(queueID, queueName, exchange, routingKey string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "lapin") && !strings.Contains(src, "basic_publish") &&
		!strings.Contains(src, "basic_consume") {
		return
	}
	enclosing := func(offset int) string {
		return findEnclosingRustFnName(src, offset)
	}

	// Publish: basic_publish("exchange", "routing_key", ...).
	// The routing key is the queue-routing identity used by the cross-repo
	// linker (matches the amqp091-go convention in synthesizeGoRabbitMQ).
	for _, m := range rustLapinPublishRe.FindAllStringSubmatchIndex(src, -1) {
		exchange := src[m[2]:m[3]]
		routingKey := src[m[4]:m[5]]
		if !looksLikeQueueName(routingKey) {
			continue
		}
		qID := rabbitmqQueueID(routingKey)
		emitQueue(qID, routingKey, exchange, routingKey, nil)
		emitEdge("SCOPE.Operation", enclosing(m[0]), qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "lapin",
			"exchange":        exchange,
			"routing_key":     routingKey,
		})
	}

	// Consume: basic_consume("queue", ...).
	for _, m := range rustLapinConsumeRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", nil)
		emitEdge("SCOPE.Operation", enclosing(m[0]), qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "lapin",
		})
	}

	// Declare: queue_declare("queue", ...) — record the node even when no
	// pub/sub call site is present in this file.
	for _, m := range rustLapinQueueDeclareRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeQueueName(queueName) {
			continue
		}
		qID := rabbitmqQueueID(queueName)
		emitQueue(qID, queueName, "", "", map[string]string{"declared": "true"})
	}
}
