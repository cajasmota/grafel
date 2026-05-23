// Apache Pulsar producer/consumer detection — extends #515.
//
// For every Pulsar producer or consumer call site this pass can statically
// recognize, we emit a synthetic `SCOPE.MessageTopic` entity keyed by the
// canonical Pulsar topic URI, plus PUBLISHES_TO or SUBSCRIBES_TO edges.
//
// Pulsar topic naming:
//
//	Full URI:  persistent://tenant/namespace/topic
//	           non-persistent://tenant/namespace/topic
//	Short form: "orders"  →  canonical: "persistent://public/default/orders"
//
// The cross-repo matching key is ALWAYS the full canonical URI so that
// a producer in repo A and a consumer in repo B sharing the same topic name
// will match via identical entity IDs — the same trick used by the Kafka
// and NATS passes (#726).
//
// SDKs covered:
//
//	Python  (pulsar-client):    client.create_producer(topic='…') + producer.send(…)
//	                            client.subscribe(topic, subscription_name)
//	Java    (pulsar-client):    client.newProducer().topic("…").create()
//	                            client.newConsumer().topic("…").subscriptionName("…").subscribe()
//	Go      (pulsar-client-go): client.CreateProducer(pulsar.ProducerOptions{Topic: "…"})
//	                            client.Subscribe(pulsar.ConsumerOptions{Topic: "…", SubscriptionName: "…"})
//	Node    (pulsar-client):    client.createProducer({ topic: "…" })
//	                            client.subscribe({ topic: "…", subscription: "…" })
//
// False-positive guard:
//
//	Python: create_producer is matched only when the file imports pulsar
//	        (import pulsar / from pulsar …). boto3 SQS uses a different
//	        signature and is never confused because it never imports pulsar.
//	Java:   newProducer/newConsumer matches only after a PulsarClient or
//	        PulsarClient.builder() token in the same file.
//	Go:     CreateProducer/Subscribe matches only when the file imports the
//	        pulsar-client-go SDK (github.com/apache/pulsar-client-go/pulsar).
//	Node:   createProducer matches only when the file imports/requires
//	        pulsar-client.
//
// Refs #936.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pulsarTopicEntityKind is the Kind used for synthetic Pulsar topic entities.
// We reuse the existing MessageTopic kind so the dashboard topology surface
// picks up Pulsar topics in the same "topics" bucket, distinguished by
// broker=pulsar in entity properties.
const pulsarTopicEntityKind = messageTopicKind // "SCOPE.MessageTopic"

// pulsarProducesEdge / pulsarConsumesEdge — reuse existing edge vocabulary.
const pulsarProducesEdge = publishesToEdgeKind  // "PUBLISHES_TO"
const pulsarConsumesEdge = subscribesToEdgeKind // "SUBSCRIBES_TO"

// pulsarDefaultScheme is the scheme used when normalising a bare topic name.
const pulsarDefaultScheme = "persistent"

// pulsarDefaultTenant / pulsarDefaultNamespace are the Pulsar defaults.
const pulsarDefaultTenant = "public"
const pulsarDefaultNamespace = "default"

// ---------------------------------------------------------------------------
// Topic canonicalisation
// ---------------------------------------------------------------------------

// normalisePulsarTopic returns the canonical full-URI form of a Pulsar topic.
//
//	"orders"                       → "persistent://public/default/orders"
//	"public/default/orders"        → "persistent://public/default/orders"
//	"persistent://t/ns/orders"     → "persistent://t/ns/orders"
//	"non-persistent://t/ns/orders" → "non-persistent://t/ns/orders"
func normalisePulsarTopic(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Already a full URI.
	if strings.Contains(raw, "://") {
		return raw
	}
	parts := strings.SplitN(raw, "/", 3)
	switch len(parts) {
	case 3:
		// tenant/namespace/topic
		return pulsarDefaultScheme + "://" + raw
	case 1:
		// bare topic name
		return fmt.Sprintf("%s://%s/%s/%s",
			pulsarDefaultScheme, pulsarDefaultTenant, pulsarDefaultNamespace, raw)
	default:
		// Ambiguous — return as-is with default scheme.
		return pulsarDefaultScheme + "://" + raw
	}
}

// pulsarTopicID returns the synthetic entity ID for a Pulsar topic.
func pulsarTopicID(canonical string) string {
	return "topic:pulsar:" + canonical
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// pulsarSynthesisSupportsLanguage reports whether applyPulsarEdges can emit
// synthetics for lang.
func pulsarSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "java", "kotlin", "go", "javascript", "typescript":
		return true
	default:
		return false
	}
}

// applyPulsarEdges runs after applyNATSEdges and APPENDS synthetic
// SCOPE.MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges for
// Apache Pulsar. Append-only — never touches existing entities or edges.
func applyPulsarEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !pulsarSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	seenTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(canonical string) {
		id := pulsarTopicID(canonical)
		if seenTopic[id] {
			return
		}
		seenTopic[id] = true
		entities = append(entities, types.EntityRecord{
			Name:       id,
			Kind:       pulsarTopicEntityKind,
			SourceFile: "",
			Language:   lang,
			Properties: map[string]string{
				"broker":       "pulsar",
				"topic_name":   canonical,
				"pattern_type": "pulsar_synthesis",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, topicCanonical, edgeKind string) {
		if callerName == "" || topicCanonical == "" {
			return
		}
		topicID := pulsarTopicID(topicCanonical)
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + topicID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:   fmt.Sprintf("%s:%s", pulsarTopicEntityKind, topicID),
			Kind:   edgeKind,
			Properties: map[string]string{
				"broker":       "pulsar",
				"pattern_type": "pulsar_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		synthesizePyPulsar(src, emitTopic, emitEdge)
	case "java", "kotlin":
		synthesizeJavaPulsar(src, emitTopic, emitEdge)
	case "go":
		synthesizeGoPulsar(src, emitTopic, emitEdge)
	case "javascript", "typescript":
		synthesizeNodePulsar(src, emitTopic, emitEdge)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Python — pulsar-client
// ---------------------------------------------------------------------------

// pyPulsarImportRe detects `import pulsar` or `from pulsar import …`.
var pyPulsarImportRe = regexp.MustCompile(`(?m)^\s*(?:import pulsar|from pulsar\b)`)

// pyPulsarCreateProducerRe captures `client.create_producer(topic='…')` or
// `client.create_producer(topic="…")`.
var pyPulsarCreateProducerRe = regexp.MustCompile(`\.create_producer\s*\(\s*(?:topic\s*=\s*)?["']([^"'\n\r]+)["']`)

// pyPulsarSubscribeRe captures `client.subscribe(topic, subscription_name)`.
// Group 1 = topic (string literal only).
var pyPulsarSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*["']([^"'\n\r]+)["']`)

func synthesizePyPulsar(
	src string,
	emitTopic func(canonical string),
	emitEdge func(callerKind, callerName, canonical, edgeKind string),
) {
	// Guard: file must import pulsar SDK.
	if !pyPulsarImportRe.MatchString(src) {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	for _, m := range pyPulsarCreateProducerRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarProducesEdge)
	}

	for _, m := range pyPulsarSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarConsumesEdge)
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — pulsar-client
// ---------------------------------------------------------------------------

// jPulsarClientRe detects PulsarClient (import or usage) as a file-level
// guard so we don't match newProducer/newConsumer on other frameworks.
var jPulsarClientRe = regexp.MustCompile(`\bPulsarClient\b`)

// jPulsarProducerTopicRe captures `.topic("…")` immediately following a
// `.newProducer()` chain. Group 1 = topic literal.
var jPulsarProducerTopicRe = regexp.MustCompile(`\.newProducer\s*\([^)]*\)\s*(?:\.[^.()]+\([^)]*\)\s*)*\.topic\s*\(\s*"([^"\n\r]+)"\s*\)`)

// jPulsarConsumerTopicRe captures `.topic("…")` immediately following a
// `.newConsumer()` chain.
var jPulsarConsumerTopicRe = regexp.MustCompile(`\.newConsumer\s*\([^)]*\)\s*(?:\.[^.()]+\([^)]*\)\s*)*\.topic\s*\(\s*"([^"\n\r]+)"\s*\)`)

// jPulsarReaderTopicRe captures `.newReader().topic("…")` (Pulsar Reader API).
var jPulsarReaderTopicRe = regexp.MustCompile(`\.newReader\s*\([^)]*\)\s*(?:\.[^.()]+\([^)]*\)\s*)*\.topic\s*\(\s*"([^"\n\r]+)"\s*\)`)

func synthesizeJavaPulsar(
	src string,
	emitTopic func(canonical string),
	emitEdge func(callerKind, callerName, canonical, edgeKind string),
) {
	if !jPulsarClientRe.MatchString(src) {
		return
	}

	// Class name for caller attribution — same heuristic as Java Kafka pass.
	caller := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		caller = m[1]
	}
	if caller == "" {
		caller = "module"
	}

	for _, m := range jPulsarProducerTopicRe.FindAllStringSubmatch(src, -1) {
		canonical := normalisePulsarTopic(m[1])
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Service", caller, canonical, pulsarProducesEdge)
	}

	for _, m := range jPulsarConsumerTopicRe.FindAllStringSubmatch(src, -1) {
		canonical := normalisePulsarTopic(m[1])
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Service", caller, canonical, pulsarConsumesEdge)
	}

	for _, m := range jPulsarReaderTopicRe.FindAllStringSubmatch(src, -1) {
		canonical := normalisePulsarTopic(m[1])
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Service", caller, canonical, pulsarConsumesEdge)
	}
}

// ---------------------------------------------------------------------------
// Go — pulsar-client-go
// ---------------------------------------------------------------------------

// goPulsarImportRe detects import of the Apache Pulsar Go client SDK.
var goPulsarImportRe = regexp.MustCompile(`"github\.com/apache/pulsar-client-go/pulsar"`)

// goPulsarProducerTopicRe captures `Topic: "…"` inside a
// pulsar.ProducerOptions{…} literal.
var goPulsarProducerTopicRe = regexp.MustCompile(`pulsar\.ProducerOptions\s*\{[^}]*?Topic\s*:\s*"([^"\n\r]+)"`)

// goPulsarConsumerTopicRe captures `Topic: "…"` inside a
// pulsar.ConsumerOptions{…} literal.
var goPulsarConsumerTopicRe = regexp.MustCompile(`pulsar\.ConsumerOptions\s*\{[^}]*?Topic\s*:\s*"([^"\n\r]+)"`)

// goPulsarConsumerTopicsRe captures `Topics: []string{"a","b"}` inside a
// pulsar.ConsumerOptions literal.
var goPulsarConsumerTopicsRe = regexp.MustCompile(`pulsar\.ConsumerOptions\s*\{[^}]*?Topics\s*:\s*\[\]string\s*\{([^}]+)\}`)

// goPulsarReaderTopicRe captures `Topic: "…"` inside a
// pulsar.ReaderOptions{…} literal.
var goPulsarReaderTopicRe = regexp.MustCompile(`pulsar\.ReaderOptions\s*\{[^}]*?Topic\s*:\s*"([^"\n\r]+)"`)

func synthesizeGoPulsar(
	src string,
	emitTopic func(canonical string),
	emitEdge func(callerKind, callerName, canonical, edgeKind string),
) {
	if !goPulsarImportRe.MatchString(src) {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	for _, m := range goPulsarProducerTopicRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarProducesEdge)
	}

	for _, m := range goPulsarConsumerTopicRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarConsumesEdge)
	}

	for _, m := range goPulsarConsumerTopicsRe.FindAllStringSubmatchIndex(src, -1) {
		body := src[m[2]:m[3]]
		offset := m[0]
		caller := enclosing(offset)
		for _, tok := range strings.Split(body, ",") {
			if raw, ok := unquote(tok); ok {
				canonical := normalisePulsarTopic(raw)
				if canonical == "" {
					continue
				}
				emitTopic(canonical)
				emitEdge("Function", caller, canonical, pulsarConsumesEdge)
			}
		}
	}

	for _, m := range goPulsarReaderTopicRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarConsumesEdge)
	}
}

// ---------------------------------------------------------------------------
// Node / TypeScript — pulsar-client
// ---------------------------------------------------------------------------

// nodePulsarImportRe detects `require('pulsar-client')` or
// `import … from 'pulsar-client'`.
var nodePulsarImportRe = regexp.MustCompile(`(?:require|from)\s*\(\s*["']pulsar-client["']\s*\)|from\s+["']pulsar-client["']`)

// nodePulsarCreateProducerRe captures `client.createProducer({ topic: "…" })`.
var nodePulsarCreateProducerRe = regexp.MustCompile(`\.createProducer\s*\(\s*\{[^}]*?topic\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodePulsarSubscribeRe captures `client.subscribe({ topic: "…", … })`.
var nodePulsarSubscribeRe = regexp.MustCompile(`\.subscribe\s*\(\s*\{[^}]*?topic\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

func synthesizeNodePulsar(
	src string,
	emitTopic func(canonical string),
	emitEdge func(callerKind, callerName, canonical, edgeKind string),
) {
	if !nodePulsarImportRe.MatchString(src) {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	for _, m := range nodePulsarCreateProducerRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarProducesEdge)
	}

	for _, m := range nodePulsarSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		raw := src[m[2]:m[3]]
		canonical := normalisePulsarTopic(raw)
		if canonical == "" {
			continue
		}
		emitTopic(canonical)
		emitEdge("Function", enclosing(m[0]), canonical, pulsarConsumesEdge)
	}
}
