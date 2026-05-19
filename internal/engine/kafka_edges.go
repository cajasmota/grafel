// Kafka producer/consumer detection — wave 1 of #726.
//
// For every Kafka producer- or consumer-shaped call site this pass can
// statically recognize, we emit a synthetic `MessageTopic` entity keyed by
// the broker + topic name, plus PUBLISHES_TO or SUBSCRIBES_TO edges from
// the calling method to that topic. The synthetic topic ID is identical
// across repos (`kafka:<topic-name>`), so the existing import-channel
// linker matches producer and consumer sides on shared entity ID without
// any new cross-repo matching code — the same trick used by
// http_endpoint_synthesis (#534).
//
// Wave 1 covers Kafka only. RabbitMQ, SQS, NATS, and Pub/Sub are wave 2.
//
// Channel→topic resolution (Quarkus / SmallRye Reactive Messaging):
//
//	`@Outgoing("feedback-out")` references a *channel name*, not a Kafka
//	topic. The physical topic is bound in `application.properties`:
//	  mp.messaging.outgoing.feedback-out.topic=feedback-topic
//	We walk up from the .java file to find the sibling
//	`src/main/resources/application.properties` and resolve channel →
//	topic. When the resolution fails (no properties file, no binding) we
//	fall back to `kafka:channel:<channel>` and mark the topic with
//	`runtime_dynamic=true` so the repairs flow (#732) can surface it.
//
// Refs #726.
package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// messageTopicKind is the Kind used for synthetic Kafka topic entities.
// Wave 2 will reuse the same kind for other brokers, distinguished by the
// `broker` property and the entity ID prefix.
const messageTopicKind = "SCOPE.MessageTopic"

// publishesToEdgeKind is the relationship from a producer caller to its
// MessageTopic. Direction: `<caller> -> MessageTopic`.
const publishesToEdgeKind = "PUBLISHES_TO"

// subscribesToEdgeKind is the inverse for consumer-side handlers.
const subscribesToEdgeKind = "SUBSCRIBES_TO"

// transformsEdgeKind is emitted when a single method is BOTH @Incoming
// and @Outgoing — a Kafka stream-transform operator. Direction:
// `<input-topic> -> <output-topic>`.
const transformsEdgeKind = "TRANSFORMS"

// kafkaSynthesisSupportsLanguage reports whether applyKafkaEdges can
// emit synthetics for `lang`. Wave 1 covers Java/Kotlin, JS/TS, Python,
// and Go — the languages with non-trivial Kafka adoption in the corpora.
func kafkaSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "javascript", "typescript", "python", "go":
		return true
	default:
		return false
	}
}

// applyKafkaEdges runs after the existing HTTP synthesis pass and APPENDS
// MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO / TRANSFORMS edges.
// It never modifies or removes existing entities or edges, so it cannot
// regress the bug-rate of the surrounding pipeline.
func applyKafkaEdges(
	lang string,
	path string,
	repoRoot string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !kafkaSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Dedup-by-ID: one MessageTopic entity per (broker, topic) per file,
	// one PUBLISHES_TO / SUBSCRIBES_TO per (caller, topic, direction).
	seenTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(topicID, topicName, broker string, dynamic bool, props map[string]string) {
		if seenTopic[topicID] {
			return
		}
		seenTopic[topicID] = true
		merged := map[string]string{
			"broker":         broker,
			"topic_name":     topicName,
			"pattern_type":   "kafka_synthesis",
			"runtime_dynamic": boolStr(dynamic),
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile is left empty so every emission of the same (Kind, Name)
		// across files within a repo collapses to the SAME stamped entity
		// ID (see graph.EntityID — source_file participates in the hash).
		// This gives MessageTopic the "one node per topic" property the
		// cross-repo linker needs.
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
			"broker":       "kafka",
			"pattern_type": "kafka_synthesis",
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

	emitTransform := func(fromTopicID, toTopicID string) {
		if fromTopicID == "" || toTopicID == "" || fromTopicID == toTopicID {
			return
		}
		key := transformsEdgeKind + "|" + fromTopicID + "|" + toTopicID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", messageTopicKind, fromTopicID),
			ToID:   fmt.Sprintf("%s:%s", messageTopicKind, toTopicID),
			Kind:   transformsEdgeKind,
			Properties: map[string]string{
				"broker":       "kafka",
				"pattern_type": "kafka_synthesis",
			},
		})
	}

	switch lang {
	case "java", "kotlin":
		synthesizeJavaKafka(src, path, repoRoot, emitTopic, emitEdge, emitTransform)
	case "javascript", "typescript":
		synthesizeNodeKafka(src, emitTopic, emitEdge)
	case "python":
		synthesizePyKafka(src, emitTopic, emitEdge)
	case "go":
		synthesizeGoKafka(src, emitTopic, emitEdge)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Quarkus SmallRye + Spring Kafka + direct API
// ---------------------------------------------------------------------------

// quarkusOutgoingRe captures `@Outgoing("channel")` annotations. The
// channel name is in capture group 1.
var quarkusOutgoingRe = regexp.MustCompile(`@Outgoing\s*\(\s*"([^"\n\r]+)"\s*\)`)

// quarkusIncomingRe captures `@Incoming("channel")` annotations.
var quarkusIncomingRe = regexp.MustCompile(`@Incoming\s*\(\s*"([^"\n\r]+)"\s*\)`)

// quarkusChannelRe captures `@Channel("channel")` field/parameter
// annotations. These typically decorate `Emitter<T>` producer fields.
var quarkusChannelRe = regexp.MustCompile(`@Channel\s*\(\s*"([^"\n\r]+)"\s*\)`)

// springKafkaListenerRe captures `@KafkaListener(topics = "topic")` and
// `@KafkaListener(topics = {"a", "b"})`. The topic list is in group 1.
var springKafkaListenerRe = regexp.MustCompile(`@KafkaListener\s*\(\s*[^)]*?topics\s*=\s*(?:\{([^}]+)\}|"([^"\n\r]+)")`)

// directKafkaSendRe captures `KafkaTemplate.send("topic", ...)` and
// `producer.send(new ProducerRecord<>("topic", ...))` style calls.
var directKafkaSendRe = regexp.MustCompile(`\.send\s*\(\s*(?:new\s+ProducerRecord\s*<[^>]*>\s*\(\s*)?"([^"\n\r]+)"`)

// methodNameRe finds the next method declaration after an annotation
// block. Group 1 = method name.
var methodNameRe = regexp.MustCompile(`(?m)^\s*(?:@[\w.()"\s,=]+\s*)*(?:public|protected|private|static|final|abstract|void|\s)*\s+\w[\w<>\[\],\s.?]*\s+(\w+)\s*\(`)

// fieldNameRe finds the variable name of an `Emitter<T> name;` field
// declaration that follows a @Channel annotation. Group 1 = field name.
var fieldNameRe = regexp.MustCompile(`(?:Emitter|MutinyEmitter|KafkaProducer|KafkaTemplate)\s*<[^>]*>\s+(\w+)\s*;`)

// classNameRe captures the first `class Foo` declaration in a Java/Kotlin
// file. Used as a fallback caller kind when we can't pinpoint the method.
var classNameRe = regexp.MustCompile(`(?m)\b(?:public\s+|abstract\s+|final\s+)*class\s+(\w+)`)

func synthesizeJavaKafka(
	src, path, repoRoot string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
	emitTransform func(fromTopicID, toTopicID string),
) {
	// Fast pre-filter: skip files with no Kafka-shaped tokens.
	if !strings.Contains(src, "@Outgoing") && !strings.Contains(src, "@Incoming") &&
		!strings.Contains(src, "@Channel") && !strings.Contains(src, "@KafkaListener") &&
		!strings.Contains(src, "KafkaTemplate") && !strings.Contains(src, "KafkaProducer") &&
		!strings.Contains(src, "ProducerRecord") {
		return
	}

	// Build the channel→topic table from the companion application.properties
	// (Quarkus convention). The walk needs an absolute path — when the
	// detector passes a repo-relative `path`, prepend `repoRoot`.
	absPath := path
	if repoRoot != "" && !filepath.IsAbs(absPath) {
		absPath = filepath.Join(repoRoot, path)
	}
	bindings := loadQuarkusChannelBindings(absPath)

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// Per-annotation collectors keyed by line offset so we can attribute
	// each @Incoming/@Outgoing to the following method declaration.
	type ann struct {
		offset  int
		kind    string // "incoming" | "outgoing"
		channel string
	}
	var anns []ann
	for _, m := range quarkusOutgoingRe.FindAllStringSubmatchIndex(src, -1) {
		anns = append(anns, ann{offset: m[0], kind: "outgoing", channel: src[m[2]:m[3]]})
	}
	for _, m := range quarkusIncomingRe.FindAllStringSubmatchIndex(src, -1) {
		anns = append(anns, ann{offset: m[0], kind: "incoming", channel: src[m[2]:m[3]]})
	}

	// Group annotations sharing the same following method declaration —
	// a method that has BOTH @Incoming and @Outgoing is a transform.
	type methodGroup struct {
		methodName string
		incoming   []string
		outgoing   []string
	}
	groups := map[string]*methodGroup{}
	groupOrder := []string{}

	for _, a := range anns {
		methodName := findFollowingMethod(src, a.offset)
		if methodName == "" {
			methodName = "<anon>"
		}
		g, ok := groups[methodName]
		if !ok {
			g = &methodGroup{methodName: methodName}
			groups[methodName] = g
			groupOrder = append(groupOrder, methodName)
		}
		if a.kind == "outgoing" {
			g.outgoing = append(g.outgoing, a.channel)
		} else {
			g.incoming = append(g.incoming, a.channel)
		}
	}

	for _, name := range groupOrder {
		g := groups[name]
		// Resolve channels → topic IDs (may emit MessageTopic entities).
		incomingIDs := make([]string, 0, len(g.incoming))
		outgoingIDs := make([]string, 0, len(g.outgoing))
		// Method-level edge uses SCOPE.Operation:<Class>.<method> so it
		// resolves against the Java extractor's operation entities. Class-
		// level fallback uses Service:<Class> which is the kind the Java
		// extractor emits for Quarkus consumer/producer classes (the same
		// kind that was orphan before this pass landed).
		methodEntityName := g.methodName
		if className != "" && g.methodName != "<anon>" {
			methodEntityName = className + "." + g.methodName
		}
		for _, ch := range g.incoming {
			id := resolveAndEmitKafkaChannel(ch, "incoming", bindings, emitTopic)
			incomingIDs = append(incomingIDs, id)
			emitEdge("SCOPE.Operation", methodEntityName, id, subscribesToEdgeKind, map[string]string{
				"channel":         ch,
				"messaging_layer": "smallrye_reactive",
			})
			// Class-level fallback so the Quarkus consumer class itself is
			// no longer orphan (it was a Service:<Class> orphan in baseline).
			if className != "" {
				emitEdge("Service", className, id, subscribesToEdgeKind, map[string]string{
					"channel":         ch,
					"messaging_layer": "smallrye_reactive",
				})
			}
		}
		for _, ch := range g.outgoing {
			id := resolveAndEmitKafkaChannel(ch, "outgoing", bindings, emitTopic)
			outgoingIDs = append(outgoingIDs, id)
			emitEdge("SCOPE.Operation", methodEntityName, id, publishesToEdgeKind, map[string]string{
				"channel":         ch,
				"messaging_layer": "smallrye_reactive",
			})
			if className != "" {
				emitEdge("Service", className, id, publishesToEdgeKind, map[string]string{
					"channel":         ch,
					"messaging_layer": "smallrye_reactive",
				})
			}
		}
		// Transform: method has both. Emit TRANSFORMS topic→topic edges.
		if len(incomingIDs) > 0 && len(outgoingIDs) > 0 {
			for _, in := range incomingIDs {
				for _, out := range outgoingIDs {
					emitTransform(in, out)
				}
			}
		}
	}

	// @Channel("name") on Emitter fields → the producer is the enclosing
	// class. Attribute the PUBLISHES_TO edge to the class itself.
	for _, m := range quarkusChannelRe.FindAllStringSubmatchIndex(src, -1) {
		channel := src[m[2]:m[3]]
		// Try to find the field name that follows this annotation.
		fieldName := ""
		tail := src[m[1]:]
		if fm := fieldNameRe.FindStringSubmatch(tail); len(fm) >= 2 {
			fieldName = fm[1]
		}
		id := resolveAndEmitKafkaChannel(channel, "outgoing", bindings, emitTopic)
		caller := className
		if caller == "" {
			caller = fieldName
		}
		if caller == "" {
			continue
		}
		// Service:<Class> matches the Java extractor's class entity kind.
		emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
			"channel":         channel,
			"messaging_layer": "smallrye_reactive",
			"emitter_field":   fieldName,
		})
	}

	// Spring Kafka — @KafkaListener(topics = "...") / topics = {"a","b"}.
	for _, m := range springKafkaListenerRe.FindAllStringSubmatchIndex(src, -1) {
		methodName := findFollowingMethod(src, m[0])
		if methodName == "" {
			methodName = className
		}
		var topics []string
		if m[2] != -1 {
			// Brace list — split on commas, trim quotes/whitespace.
			for _, tok := range strings.Split(src[m[2]:m[3]], ",") {
				tok = strings.TrimSpace(tok)
				tok = strings.Trim(tok, `"'`)
				if tok != "" {
					topics = append(topics, tok)
				}
			}
		} else if m[4] != -1 {
			topics = append(topics, src[m[4]:m[5]])
		}
		for _, t := range topics {
			id := kafkaTopicID(t)
			emitTopic(id, t, "kafka", false, map[string]string{
				"messaging_layer": "spring_kafka",
			})
			operationName := methodName
			if className != "" && methodName != className {
				operationName = className + "." + methodName
			}
			emitEdge("SCOPE.Operation", operationName, id, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "spring_kafka",
			})
			if className != "" {
				emitEdge("Service", className, id, subscribesToEdgeKind, map[string]string{
					"messaging_layer": "spring_kafka",
				})
			}
		}
	}

	// Direct send() API — KafkaTemplate / KafkaProducer with a literal
	// topic string. Caller is the enclosing class (we don't try to pinpoint
	// the method, which would require AST scope tracking).
	if strings.Contains(src, "KafkaTemplate") || strings.Contains(src, "KafkaProducer") || strings.Contains(src, "ProducerRecord") {
		for _, m := range directKafkaSendRe.FindAllStringSubmatch(src, -1) {
			if len(m) < 2 {
				continue
			}
			topic := m[1]
			// Heuristic: topic-shaped strings only — must not contain spaces
			// or slashes (avoids matching arbitrary string-literal first
			// arguments that happen to follow `.send(`).
			if !looksLikeKafkaTopic(topic) {
				continue
			}
			id := kafkaTopicID(topic)
			emitTopic(id, topic, "kafka", false, map[string]string{
				"messaging_layer": "direct_api",
			})
			caller := className
			if caller == "" {
				continue
			}
			emitEdge("Service", caller, id, publishesToEdgeKind, map[string]string{
				"messaging_layer": "direct_api",
			})
		}
	}
}

// findFollowingMethod locates the method-declaration name that comes
// immediately after the byte offset `from` in `src`. Returns "" when no
// method is found in the next ~500 chars.
func findFollowingMethod(src string, from int) string {
	end := from + 800
	if end > len(src) {
		end = len(src)
	}
	window := src[from:end]
	if m := methodNameRe.FindStringSubmatch(window); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// loadQuarkusChannelBindings reads the companion application.properties
// for a Java/Kotlin source file and returns a map of channel → topic
// (plus delivery-guarantee hints). Resolution walks upward from the file
// path to find the nearest `src/main/resources/application.properties`.
//
// Returns an empty (non-nil) map if no properties file is found.
func loadQuarkusChannelBindings(srcPath string) map[string]channelBinding {
	out := map[string]channelBinding{}
	// Walk up to find the service root containing src/main/resources.
	dir := filepath.Dir(srcPath)
	for i := 0; i < 12 && dir != "" && dir != "/" && dir != "."; i++ {
		propsPath := filepath.Join(dir, "src", "main", "resources", "application.properties")
		if _, err := os.Stat(propsPath); err == nil {
			parseQuarkusProperties(propsPath, out)
			return out
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// channelBinding captures the resolved topic name plus optional
// delivery-guarantee metadata for a single Quarkus messaging channel.
type channelBinding struct {
	Topic             string
	DeliveryGuarantee string // e.g. "acks=all"
	Direction         string // "incoming" | "outgoing"
}

// quarkusPropChannelRe captures
// `mp.messaging.<dir>.<channel>.<setting>=<value>` lines.
var quarkusPropChannelRe = regexp.MustCompile(`^\s*mp\.messaging\.(incoming|outgoing)\.([\w.-]+?)\.([\w.-]+)\s*=\s*(.+?)\s*$`)

func parseQuarkusProperties(path string, out map[string]channelBinding) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		m := quarkusPropChannelRe.FindStringSubmatch(line)
		if len(m) < 5 {
			continue
		}
		dir, channel, setting, value := m[1], m[2], m[3], m[4]
		b := out[channel]
		b.Direction = dir
		switch setting {
		case "topic":
			b.Topic = value
		case "acks":
			b.DeliveryGuarantee = "acks=" + value
		}
		out[channel] = b
	}
}

// resolveAndEmitKafkaChannel resolves a Quarkus channel name to its
// physical topic, emits the MessageTopic entity, and returns the
// canonical topic ID for edge wiring.
//
// When the channel cannot be resolved (no companion properties file, or
// no `topic=` binding for the channel), the topic is emitted under the
// fallback ID `kafka:channel:<channel>` with `runtime_dynamic=true` so
// the repairs flow (#732) can surface it.
func resolveAndEmitKafkaChannel(
	channel, direction string,
	bindings map[string]channelBinding,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
) string {
	b, ok := bindings[channel]
	if ok && b.Topic != "" {
		id := kafkaTopicID(b.Topic)
		props := map[string]string{
			"messaging_layer": "smallrye_reactive",
			"channel":         channel,
			"direction":       direction,
		}
		if b.DeliveryGuarantee != "" {
			props["delivery_guarantee"] = b.DeliveryGuarantee
		}
		if isDeadLetterTopic(b.Topic) {
			props["dead_letter"] = "true"
		}
		emitTopic(id, b.Topic, "kafka", false, props)
		return id
	}
	// Unresolved — emit a runtime-dynamic placeholder so the repairs flow
	// can later attach the real topic name.
	id := "kafka:channel:" + channel
	emitTopic(id, channel, "kafka", true, map[string]string{
		"messaging_layer": "smallrye_reactive",
		"channel":         channel,
		"direction":       direction,
	})
	return id
}

// kafkaTopicID returns the canonical synthetic ID for a Kafka topic.
// Identical across repos so the cross-repo linker matches producer and
// consumer sides without any new linker code.
func kafkaTopicID(topic string) string {
	return "kafka:" + topic
}

// looksLikeKafkaTopic returns true when `s` plausibly looks like a Kafka
// topic name. Belt-and-suspenders gate for the direct `.send("...")`
// scanner so we don't claim arbitrary string-literal first arguments.
func looksLikeKafkaTopic(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	if strings.ContainsAny(s, " /\\\n\r\t<>{}") {
		return false
	}
	// Topic names typically contain only [a-zA-Z0-9._-]. Require at least
	// one alphanumeric character.
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return hasAlnum
}

// isDeadLetterTopic returns true when the topic name follows a known
// dead-letter naming convention.
func isDeadLetterTopic(topic string) bool {
	low := strings.ToLower(topic)
	return strings.HasSuffix(low, "-dlq") || strings.HasSuffix(low, ".dlq") ||
		strings.HasSuffix(low, "_dlq") || strings.HasSuffix(low, "-deadletter") ||
		strings.HasSuffix(low, ".deadletter") || strings.Contains(low, ".dead-letter")
}


// ---------------------------------------------------------------------------
// Node — kafkajs producer + consumer
// ---------------------------------------------------------------------------

// nodeKafkaSendRe captures `producer.send({ topic: "name", ... })`.
// Group 1 = topic literal.
var nodeKafkaSendRe = regexp.MustCompile(`\.send\s*\(\s*\{[^}]*?topic\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeKafkaProduceRe captures node-rdkafka style `producer.produce("topic", ...)`.
var nodeKafkaProduceRe = regexp.MustCompile(`\.produce\s*\(\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeKafkaSubscribeRe captures kafkajs `consumer.subscribe({ topics: ["a","b"] })`
// and the legacy `topic: "name"` shape. Group 1 = topic list body, group 2 = single topic.
var nodeKafkaSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*\{[^}]*?(?:topics\s*:\s*\[([^\]]+)\]|topic\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `])`)

// nodeConstStringRe captures `const NAME = "literal"` file-local constants
// used as topic-name aliases.
var nodeConstStringRe = regexp.MustCompile(`(?m)^\s*(?:const|let|var)\s+([A-Z_][A-Z0-9_]*)\s*=\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeFunctionRe captures the enclosing function/method name for a given
// offset. Group 1 = name.
var nodeFunctionRe = regexp.MustCompile(`(?m)(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>)|(\w+)\s*[:=]\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>))`)

func synthesizeNodeKafka(
	src string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "kafka") && !strings.Contains(src, "Kafka") {
		return
	}
	consts := collectNodeConstStrings(src)
	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	// Producer .send({ topic: "..." })
	for _, m := range nodeKafkaSendRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		caller := enclosing(m[0])
		emitProducerTopic(topic, "kafkajs", caller, emitTopic, emitEdge)
	}
	// Producer .produce("topic", ...)
	for _, m := range nodeKafkaProduceRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		caller := enclosing(m[0])
		emitProducerTopic(topic, "node-rdkafka", caller, emitTopic, emitEdge)
	}
	// Consumer .subscribe({ topics: [...] | topic: "..." })
	for _, m := range nodeKafkaSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		caller := enclosing(m[0])
		var topics []string
		if m[2] != -1 {
			for _, tok := range strings.Split(src[m[2]:m[3]], ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				// Resolve const reference vs string literal.
				if unq, ok := unquote(tok); ok {
					topics = append(topics, unq)
				} else if v, ok := consts[tok]; ok {
					topics = append(topics, v)
				} else {
					// Dynamic / non-literal — keep as channel-style fallback.
					topics = append(topics, tok)
				}
			}
		} else if m[4] != -1 {
			topics = append(topics, src[m[4]:m[5]])
		}
		for _, t := range topics {
			emitConsumerTopic(t, "kafkajs", caller, emitTopic, emitEdge)
		}
	}
}

// emitProducerTopic emits a MessageTopic + PUBLISHES_TO for a Node-side
// producer, resolving file-local constant aliases when present.
func emitProducerTopic(
	rawTopic, layer, caller string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	topic, dynamic := rawTopic, false
	id := kafkaTopicID(topic)
	if !looksLikeKafkaTopic(topic) {
		id = "kafka:channel:" + topic
		dynamic = true
	}
	emitTopic(id, topic, "kafka", dynamic, map[string]string{
		"messaging_layer": layer,
	})
	emitEdge("Function", caller, id, publishesToEdgeKind, map[string]string{
		"messaging_layer": layer,
	})
}

// emitConsumerTopic emits a MessageTopic + SUBSCRIBES_TO for a Node-side
// consumer.
func emitConsumerTopic(
	rawTopic, layer, caller string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	topic, dynamic := rawTopic, false
	id := kafkaTopicID(topic)
	if !looksLikeKafkaTopic(topic) {
		id = "kafka:channel:" + topic
		dynamic = true
	}
	emitTopic(id, topic, "kafka", dynamic, map[string]string{
		"messaging_layer": layer,
	})
	emitEdge("Function", caller, id, subscribesToEdgeKind, map[string]string{
		"messaging_layer": layer,
	})
}

// unquote tries to strip surrounding single/double/backtick quotes from a
// token. Returns the inner string + true on success.
func unquote(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", false
	}
	c := s[0]
	if (c == '"' || c == '\'' || c == '`') && s[len(s)-1] == c {
		return s[1 : len(s)-1], true
	}
	return "", false
}

// collectNodeConstStrings scans for top-level string constants and returns
// the const-name → value table for file-local symbol resolution.
func collectNodeConstStrings(src string) map[string]string {
	out := map[string]string{}
	for _, m := range nodeConstStringRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 3 {
			out[m[1]] = m[2]
		}
	}
	return out
}

// findEnclosingNodeName walks backward from `offset` looking for a
// function/arrow declaration. Returns "module" when no enclosing function
// can be found within ~3KB so we still emit *something* attributable.
func findEnclosingNodeName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := nodeFunctionRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	last := matches[len(matches)-1]
	for _, g := range last[1:] {
		if g != "" {
			return g
		}
	}
	return "module"
}

// ---------------------------------------------------------------------------
// Python — confluent-kafka + kafka-python
// ---------------------------------------------------------------------------

// pyProduceRe captures confluent-kafka `producer.produce("topic", ...)`.
var pyProduceRe = regexp.MustCompile(`\.produce\s*\(\s*["']([^"'\n\r]+)["']`)

// pySendRe captures kafka-python `producer.send("topic", ...)`.
var pySendRe = regexp.MustCompile(`\.send\s*\(\s*["']([^"'\n\r]+)["']`)

// pySubscribeRe captures `consumer.subscribe(["topic-a", "topic-b"])`.
var pySubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*\[([^\]]+)\]`)

// pyConsumerCtorRe captures `KafkaConsumer("topic", ...)` (kafka-python).
var pyConsumerCtorRe = regexp.MustCompile(`KafkaConsumer\s*\(\s*["']([^"'\n\r]+)["']`)

// pyConstStringRe captures module-level `NAME = "value"` constants.
var pyConstStringRe = regexp.MustCompile(`(?m)^([A-Z_][A-Z0-9_]*)\s*=\s*["']([^"'\n\r]+)["']`)

// pyFunctionRe matches `def name(` declarations.
var pyFunctionRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

func synthesizePyKafka(
	src string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "kafka") && !strings.Contains(src, "Kafka") {
		return
	}
	consts := map[string]string{}
	for _, m := range pyConstStringRe.FindAllStringSubmatch(src, -1) {
		if len(m) >= 3 {
			consts[m[1]] = m[2]
		}
	}
	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// confluent-kafka .produce("topic", ...)
	for _, m := range pyProduceRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		emitProducerTopic(topic, "confluent-kafka", enclosing(m[0]), emitTopic, emitEdge)
	}
	// kafka-python .send("topic", ...) — but ONLY when there's a KafkaProducer
	// reference somewhere (otherwise we'd match generic .send calls).
	if strings.Contains(src, "KafkaProducer") {
		for _, m := range pySendRe.FindAllStringSubmatchIndex(src, -1) {
			topic := src[m[2]:m[3]]
			emitProducerTopic(topic, "kafka-python", enclosing(m[0]), emitTopic, emitEdge)
		}
	}
	// consumer.subscribe(["topic", ...])
	for _, m := range pySubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		caller := enclosing(m[0])
		for _, tok := range strings.Split(src[m[2]:m[3]], ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			topic := tok
			dynamic := false
			if unq, ok := unquote(tok); ok {
				topic = unq
			} else if v, ok := consts[tok]; ok {
				topic = v
			} else {
				dynamic = true
			}
			id := kafkaTopicID(topic)
			if dynamic || !looksLikeKafkaTopic(topic) {
				id = "kafka:channel:" + topic
				dynamic = true
			}
			emitTopic(id, topic, "kafka", dynamic, map[string]string{
				"messaging_layer": "confluent-kafka",
			})
			emitEdge("Function", caller, id, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "confluent-kafka",
			})
		}
	}
	// KafkaConsumer("topic", ...) — kafka-python constructor form.
	for _, m := range pyConsumerCtorRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		emitConsumerTopic(topic, "kafka-python", enclosing(m[0]), emitTopic, emitEdge)
	}
}

func findEnclosingPyName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := pyFunctionRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	return matches[len(matches)-1][1]
}

// ---------------------------------------------------------------------------
// Go — Sarama + segmentio/kafka-go
// ---------------------------------------------------------------------------

// goSaramaTopicFieldRe captures `Topic: "name"` inside a Sarama
// `ProducerMessage{...}` literal.
var goSaramaTopicFieldRe = regexp.MustCompile(`Topic\s*:\s*"([^"\n\r]+)"`)

// goSegmentioWriterRe matches a kafka.Writer{...} construction with a
// Topic field set to a literal string. Same regex as the Sarama case —
// the field name is what matters.
var goKafkaGoReaderTopicRe = regexp.MustCompile(`kafka\.ReaderConfig\s*\{[^}]*?Topic\s*:\s*"([^"\n\r]+)"`)

// goFunctionRe matches Go function/method declarations:
// `func (r *Foo) Bar(` or `func Bar(`. Group 1 = method receiver type,
// group 2 = function/method name.
var goFunctionRe = regexp.MustCompile(`(?m)^func\s+(?:\(\s*\w+\s+\*?(\w+)\s*\)\s*)?(\w+)\s*\(`)

func synthesizeGoKafka(
	src string,
	emitTopic func(topicID, topicName, broker string, dynamic bool, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "kafka") && !strings.Contains(src, "sarama") && !strings.Contains(src, "Sarama") {
		return
	}
	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// Sarama or kafka-go Writer: `Topic: "name"` field. We do not know the
	// direction from the literal alone — Sarama ProducerMessage is always
	// producer-side; kafka.ReaderConfig is always consumer-side. We use
	// surrounding-text heuristics: if the surrounding ~120 chars contain
	// `ReaderConfig` or `NewReader`, it's a consumer.
	for _, m := range goSaramaTopicFieldRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		ctx := surroundingText(src, m[0], 200)
		isConsumer := strings.Contains(ctx, "ReaderConfig") || strings.Contains(ctx, "NewReader") ||
			strings.Contains(ctx, "ConsumerGroup") || strings.Contains(ctx, "Consumer{")
		caller := enclosing(m[0])
		if isConsumer {
			emitConsumerTopic(topic, "kafka-go", caller, emitTopic, emitEdge)
		} else {
			emitProducerTopic(topic, "sarama", caller, emitTopic, emitEdge)
		}
	}
	// Belt-and-suspenders: explicit kafka.ReaderConfig matches even if the
	// generic field regex above missed it (e.g. due to multi-line layout).
	for _, m := range goKafkaGoReaderTopicRe.FindAllStringSubmatchIndex(src, -1) {
		topic := src[m[2]:m[3]]
		emitConsumerTopic(topic, "kafka-go", enclosing(m[0]), emitTopic, emitEdge)
	}
}

func findEnclosingGoName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := goFunctionRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "package"
	}
	last := matches[len(matches)-1]
	name := last[2]
	if last[1] != "" {
		name = last[1] + "." + name
	}
	return name
}

// surroundingText returns up to `radius` characters of `src` centered on
// `offset`. Used for direction heuristics in the Go Kafka pass.
func surroundingText(src string, offset, radius int) string {
	start := offset - radius
	if start < 0 {
		start = 0
	}
	end := offset + radius
	if end > len(src) {
		end = len(src)
	}
	return src[start:end]
}
