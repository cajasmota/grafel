// Azure Service Bus / Event Hubs producer/consumer detection (#3674,
// #3628 area #2 — completes azure broker topology).
//
// Before this pass azure had NO messaging emitter, so although the
// broker-agnostic topic_pass join (internal/links/topic_pass.go) was ready,
// nothing emitted the PUBLISHES_TO / SUBSCRIBES_TO edges to an azure-keyed
// topic entity — azure producer→consumer topology could never form.
//
// For every Service Bus / Event Hubs send or receive call site this pass can
// statically recognize, we emit a synthetic `SCOPE.MessageTopic` entity keyed
// `azure:<name>` (where <name> is the queue / topic / hub name), plus a
// PUBLISHES_TO or SUBSCRIBES_TO edge from the enclosing function/method. The
// synthetic topic ID is identical across repos, so topic_pass joins producer
// and consumer sides on shared entity Name without any new matching code
// (brokerFromTopicName already maps `azure:<name>` → channel "azure").
//
// Libraries / frameworks covered:
//   - C# Azure.Messaging.ServiceBus:
//     client.CreateSender("orders").SendMessageAsync(...)      → producer
//     client.CreateProcessor("orders") / CreateReceiver("orders") → consumer
//   - C# Azure.Messaging.EventHubs:
//     new EventHubProducerClient(cs, "hub") / SendAsync(...)    → producer
//     new EventHubConsumerClient(grp, cs, "hub")               → consumer
//   - JS/TS @azure/service-bus:
//     client.createSender("orders").sendMessages(...)          → producer
//     client.createReceiver("orders") / .subscribe(...)        → consumer
//   - JS/TS @azure/event-hubs:
//     new EventHubProducerClient(cs, "hub")                     → producer
//     new EventHubConsumerClient(grp, cs, "hub")               → consumer
//   - Python azure-servicebus:
//     client.get_queue_sender(queue_name="orders") / send_messages  → producer
//     client.get_topic_sender(topic_name="orders")             → producer
//     client.get_queue_receiver(queue_name="orders")           → consumer
//     client.get_subscription_receiver(topic_name="orders", ...) → consumer
//   - Python azure-eventhub:
//     EventHubProducerClient(..., eventhub_name="hub") / send_batch → producer
//     EventHubConsumerClient(..., eventhub_name="hub")          → consumer
//
// Honest-partial: dynamic (non-literal) names are skipped — we never fabricate
// a topic entity for a name we cannot read statically. Azure EventGrid is
// intentionally out of scope here; it is already covered by event_bus_edges.go
// under the `event:eventgrid:*` synthetic.
//
// Append-only — never modifies or removes existing entities or edges, so this
// pass cannot regress the surrounding pipeline's bug-rate.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// azureMessagingSupportsLanguage reports whether applyAzureMessagingEdges can
// emit synthetics for `lang`. The dominant Azure SDK languages are C#, JS/TS,
// and Python.
func azureMessagingSupportsLanguage(lang string) bool {
	switch lang {
	case "csharp", "javascript", "typescript", "python":
		return true
	default:
		return false
	}
}

// azureTopicID returns the canonical synthetic ID for an Azure Service Bus /
// Event Hubs entity. The same `azure:<name>` form is emitted by producers and
// consumers across repos so topic_pass joins them on Name.
func azureTopicID(name string) string {
	return "azure:" + name
}

// looksLikeAzureName returns true when `s` is a plausible Service Bus / Event
// Hubs entity name. Names are alphanumeric plus `-`, `_`, `.`, `/` (the slash
// allows topic/subscription paths), 1-260 chars. Rejects anything with
// interpolation/template markers so dynamic names are not fabricated.
func looksLikeAzureName(s string) bool {
	if s == "" || len(s) > 260 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<>{}$`") {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return hasAlnum
}

// applyAzureMessagingEdges runs after the other broker passes and APPENDS
// SCOPE.MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges.
func applyAzureMessagingEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !azureMessagingSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Dedup-by-ID: one MessageTopic entity per topic per file, one
	// PUBLISHES_TO / SUBSCRIBES_TO per (caller, topic, direction).
	seenTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(topicID, topicName string, props map[string]string) {
		if seenTopic[topicID] {
			return
		}
		seenTopic[topicID] = true
		merged := map[string]string{
			"broker":          "azure",
			"topic_name":      topicName,
			"pattern_type":    "azure_messaging_synthesis",
			"runtime_dynamic": "false",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical topic names collapse to ONE
		// entity per repo and match across repos via topic_pass.
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
			"broker":       "azure",
			"pattern_type": "azure_messaging_synthesis",
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
	case "csharp":
		synthesizeCSharpAzureMessaging(src, emitTopic, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeAzureMessaging(src, emitTopic, emitEdge)
	case "python":
		synthesizePyAzureMessaging(src, emitTopic, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// emitAzure is a small helper shared by the per-language synthesizers: it
// validates the name, emits the topic, and emits the directional edge from the
// enclosing caller.
func emitAzure(
	name, callerKind, caller, edgeKind, layer, kind string,
	emitTopic func(topicID, topicName string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !looksLikeAzureName(name) {
		return
	}
	tID := azureTopicID(name)
	emitTopic(tID, name, map[string]string{"messaging_layer": layer, "entity_kind": kind})
	emitEdge(callerKind, caller, tID, edgeKind, map[string]string{
		"messaging_layer": layer,
		"entity_kind":     kind,
	})
}

// ---------------------------------------------------------------------------
// C# — Azure.Messaging.ServiceBus / Azure.Messaging.EventHubs
// ---------------------------------------------------------------------------

// csServiceBusSenderRe captures client.CreateSender("orders").
var csServiceBusSenderRe = regexp.MustCompile(`\.CreateSender\s*\(\s*"([^"\n\r]+)"`)

// csServiceBusReceiverRe captures client.CreateReceiver("orders") and the
// session/processor variants CreateProcessor / CreateSessionReceiver /
// CreateSessionProcessor.
var csServiceBusReceiverRe = regexp.MustCompile(`\.Create(?:Processor|Receiver|SessionReceiver|SessionProcessor)\s*\(\s*"([^"\n\r]+)"`)

// csEventHubProducerRe captures new EventHubProducerClient(<cs>, "hub").
// The hub name is the LAST string literal argument; group 1 = hub.
var csEventHubProducerRe = regexp.MustCompile(`new\s+EventHubProducerClient\s*\([^)]*?,\s*"([^"\n\r]+)"`)

// csEventHubConsumerRe captures new EventHubConsumerClient(group, <cs>, "hub").
var csEventHubConsumerRe = regexp.MustCompile(`new\s+EventHubConsumerClient\s*\([^)]*?,\s*"([^"\n\r]+)"\s*\)`)

func synthesizeCSharpAzureMessaging(
	src string,
	emitTopic func(topicID, topicName string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "ServiceBus") && !strings.Contains(src, "EventHub") &&
		!strings.Contains(src, "CreateSender") && !strings.Contains(src, "CreateReceiver") &&
		!strings.Contains(src, "CreateProcessor") {
		return
	}
	enclosing := func(offset int) string {
		return findEnclosingCSharpMethod(src, offset)
	}

	for _, m := range csServiceBusSenderRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-servicebus-dotnet", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range csServiceBusReceiverRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-servicebus-dotnet", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range csEventHubProducerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-eventhubs-dotnet", "eventhub", emitTopic, emitEdge)
	}
	for _, m := range csEventHubConsumerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-eventhubs-dotnet", "eventhub", emitTopic, emitEdge)
	}
}

// ---------------------------------------------------------------------------
// JS / TS — @azure/service-bus + @azure/event-hubs
// ---------------------------------------------------------------------------

// nodeServiceBusSenderRe captures client.createSender("orders").
var nodeServiceBusSenderRe = regexp.MustCompile("" +
	`\.createSender\s*\(\s*` + "[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeServiceBusReceiverRe captures client.createReceiver("orders", ...).
// The first argument (queue/topic name) is group 1.
var nodeServiceBusReceiverRe = regexp.MustCompile("" +
	`\.createReceiver\s*\(\s*` + "[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeEventHubProducerRe captures new EventHubProducerClient(cs, "hub").
var nodeEventHubProducerRe = regexp.MustCompile("" +
	`new\s+EventHubProducerClient\s*\([^)]*?,\s*` + "[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeEventHubConsumerRe captures new EventHubConsumerClient(group, cs, "hub").
var nodeEventHubConsumerRe = regexp.MustCompile("" +
	`new\s+EventHubConsumerClient\s*\([^)]*?,\s*` + "[\"'`]([^\"'`\\n\\r]+)[\"'`]\\s*\\)")

func synthesizeNodeAzureMessaging(
	src string,
	emitTopic func(topicID, topicName string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "service-bus") && !strings.Contains(src, "event-hubs") &&
		!strings.Contains(src, "createSender") && !strings.Contains(src, "createReceiver") &&
		!strings.Contains(src, "EventHubProducerClient") && !strings.Contains(src, "EventHubConsumerClient") {
		return
	}
	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	for _, m := range nodeServiceBusSenderRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-servicebus-js", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range nodeServiceBusReceiverRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-servicebus-js", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range nodeEventHubProducerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-eventhubs-js", "eventhub", emitTopic, emitEdge)
	}
	for _, m := range nodeEventHubConsumerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-eventhubs-js", "eventhub", emitTopic, emitEdge)
	}
}

// ---------------------------------------------------------------------------
// Python — azure-servicebus + azure-eventhub
// ---------------------------------------------------------------------------

// pySBSenderRe captures get_queue_sender(queue_name="orders") and
// get_topic_sender(topic_name="orders"). Group 1 = name.
var pySBSenderRe = regexp.MustCompile(`\.get_(?:queue|topic)_sender\s*\(\s*(?:[^)]*?)?(?:queue_name|topic_name)\s*=\s*["']([^"'\n\r]+)["']`)

// pySBReceiverRe captures get_queue_receiver(queue_name="orders") and
// get_subscription_receiver(topic_name="orders", subscription_name=...).
var pySBReceiverRe = regexp.MustCompile(`\.get_(?:queue|subscription)_receiver\s*\(\s*(?:[^)]*?)?(?:queue_name|topic_name)\s*=\s*["']([^"'\n\r]+)["']`)

// pyEHProducerRe captures EventHubProducerClient(..., eventhub_name="hub") and
// the EventHubProducerClient.from_connection_string(..., eventhub_name="hub")
// factory form.
var pyEHProducerRe = regexp.MustCompile(`EventHubProducerClient(?:\.\w+)?\s*\([^)]*?eventhub_name\s*=\s*["']([^"'\n\r]+)["']`)

// pyEHConsumerRe captures EventHubConsumerClient(..., eventhub_name="hub") and
// the .from_connection_string(...) factory form.
var pyEHConsumerRe = regexp.MustCompile(`EventHubConsumerClient(?:\.\w+)?\s*\([^)]*?eventhub_name\s*=\s*["']([^"'\n\r]+)["']`)

func synthesizePyAzureMessaging(
	src string,
	emitTopic func(topicID, topicName string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	if !strings.Contains(src, "azure") && !strings.Contains(src, "ServiceBus") &&
		!strings.Contains(src, "EventHub") && !strings.Contains(src, "get_queue_sender") &&
		!strings.Contains(src, "get_topic_sender") && !strings.Contains(src, "get_queue_receiver") &&
		!strings.Contains(src, "get_subscription_receiver") {
		return
	}
	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	for _, m := range pySBSenderRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-servicebus-python", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range pySBReceiverRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-servicebus-python", "servicebus", emitTopic, emitEdge)
	}
	for _, m := range pyEHProducerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), publishesToEdgeKind,
			"azure-eventhub-python", "eventhub", emitTopic, emitEdge)
	}
	for _, m := range pyEHConsumerRe.FindAllStringSubmatchIndex(src, -1) {
		emitAzure(src[m[2]:m[3]], "Function", enclosing(m[0]), subscribesToEdgeKind,
			"azure-eventhub-python", "eventhub", emitTopic, emitEdge)
	}
}
