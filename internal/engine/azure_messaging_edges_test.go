// Tests for the Azure Service Bus / Event Hubs producer/consumer detection
// pass (#3674, #3628 area #2 — completes azure broker topology).
//
// Value-asserting (not len>0): each case asserts the exact `azure:<name>`
// MessageTopic ID is emitted AND that the directional edge points at it, so
// that topic_pass would join a producer in one repo to a consumer in another.
// A negative case asserts a dynamic (non-literal) name fabricates no topic.
package engine

import (
	"strings"
	"testing"
)

// runAzureMsgDetect is a lightweight in-process driver for the Azure pass.
func runAzureMsgDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyAzureMessagingEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	ents, rels := res.Entities, res.Relationships
	out := make([]entityResult, 0, len(ents))
	for _, e := range ents {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(rels))
	for _, r := range rels {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
	}
	return out, relOut
}

// azureTopicByID finds the emitted SCOPE.MessageTopic with the given
// canonical azure:<name> ID.
func azureTopicByID(ents []entityResult, topicID string) *entityResult {
	for i := range ents {
		if ents[i].kind == messageTopicKind && ents[i].name == topicID {
			return &ents[i]
		}
	}
	return nil
}

// assertPublishesTo asserts an azure:<name> MessageTopic exists and a
// PUBLISHES_TO edge points at it — i.e. producer side of topic_pass.
func assertPublishesTo(t *testing.T, ents []entityResult, rels []relResult, name string) {
	t.Helper()
	tID := azureTopicID(name)
	te := azureTopicByID(ents, tID)
	if te == nil {
		t.Fatalf("expected SCOPE.MessageTopic %q, ents=%v", tID, ents)
	}
	if te.kind != messageTopicKind {
		t.Fatalf("topic %q kind = %q, want %q", tID, te.kind, messageTopicKind)
	}
	if te.props["broker"] != "azure" {
		t.Fatalf("topic %q broker = %q, want azure", tID, te.props["broker"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	found := false
	for _, p := range pubs {
		if strings.Contains(p.to, tID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PUBLISHES_TO -> %q, pubs=%v", tID, pubs)
	}
}

// assertSubscribesTo asserts an azure:<name> MessageTopic exists and a
// SUBSCRIBES_TO edge points at it — i.e. consumer side of topic_pass.
func assertSubscribesTo(t *testing.T, ents []entityResult, rels []relResult, name string) {
	t.Helper()
	tID := azureTopicID(name)
	te := azureTopicByID(ents, tID)
	if te == nil {
		t.Fatalf("expected SCOPE.MessageTopic %q, ents=%v", tID, ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	found := false
	for _, s := range subs {
		if strings.Contains(s.to, tID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SUBSCRIBES_TO -> %q, subs=%v", tID, subs)
	}
}

// ---------------------------------------------------------------------------
// C# — Azure.Messaging.ServiceBus / EventHubs
// ---------------------------------------------------------------------------

// TestAzure_CSharp_ServiceBusSender: CreateSender("orders")+SendMessageAsync
// → producer PUBLISHES_TO azure:orders.
func TestAzure_CSharp_ServiceBusSender(t *testing.T) {
	src := `using Azure.Messaging.ServiceBus;
public class OrderPublisher {
    public async Task Publish(ServiceBusClient client, string body) {
        ServiceBusSender sender = client.CreateSender("orders");
        await sender.SendMessageAsync(new ServiceBusMessage(body));
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "OrderPublisher.cs", src)
	assertPublishesTo(t, ents, rels, "orders")
}

// TestAzure_CSharp_ServiceBusReceiver: CreateReceiver("orders") → consumer
// SUBSCRIBES_TO azure:orders. Paired with the sender above, topic_pass would
// join them on the shared azure:orders Name.
func TestAzure_CSharp_ServiceBusReceiver(t *testing.T) {
	src := `using Azure.Messaging.ServiceBus;
public class OrderConsumer {
    public async Task Consume(ServiceBusClient client) {
        ServiceBusReceiver receiver = client.CreateReceiver("orders");
        var msg = await receiver.ReceiveMessageAsync();
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "OrderConsumer.cs", src)
	assertSubscribesTo(t, ents, rels, "orders")
}

// TestAzure_CSharp_ServiceBusProcessor: CreateProcessor("orders") → consumer.
func TestAzure_CSharp_ServiceBusProcessor(t *testing.T) {
	src := `using Azure.Messaging.ServiceBus;
public class Worker {
    public void Start(ServiceBusClient client) {
        var processor = client.CreateProcessor("orders");
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "Worker.cs", src)
	assertSubscribesTo(t, ents, rels, "orders")
}

// TestAzure_CSharp_EventHubProducer: EventHubProducerClient(cs,"telemetry")
// → producer PUBLISHES_TO azure:telemetry.
func TestAzure_CSharp_EventHubProducer(t *testing.T) {
	src := `using Azure.Messaging.EventHubs.Producer;
public class TelemetrySink {
    public async Task Send(string cs) {
        var producer = new EventHubProducerClient(cs, "telemetry");
        await producer.SendAsync(new[] { new EventData(new byte[0]) });
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "TelemetrySink.cs", src)
	assertPublishesTo(t, ents, rels, "telemetry")
}

// TestAzure_CSharp_EventHubConsumer: EventHubConsumerClient(grp,cs,"telemetry")
// → consumer SUBSCRIBES_TO azure:telemetry.
func TestAzure_CSharp_EventHubConsumer(t *testing.T) {
	src := `using Azure.Messaging.EventHubs.Consumer;
public class TelemetryReader {
    public void Init(string cs) {
        var consumer = new EventHubConsumerClient("$Default", cs, "telemetry");
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "TelemetryReader.cs", src)
	assertSubscribesTo(t, ents, rels, "telemetry")
}

// ---------------------------------------------------------------------------
// JS / TS — @azure/service-bus + @azure/event-hubs
// ---------------------------------------------------------------------------

// TestAzure_Node_ServiceBusSender: createSender("orders")+sendMessages → producer.
func TestAzure_Node_ServiceBusSender(t *testing.T) {
	src := `const { ServiceBusClient } = require("@azure/service-bus");
async function publishOrder(client, body) {
  const sender = client.createSender("orders");
  await sender.sendMessages({ body });
}
`
	ents, rels := runAzureMsgDetect(t, "javascript", "publish.js", src)
	assertPublishesTo(t, ents, rels, "orders")
}

// TestAzure_Node_ServiceBusReceiver: createReceiver("orders") → consumer.
func TestAzure_TS_ServiceBusReceiver(t *testing.T) {
	src := `import { ServiceBusClient } from "@azure/service-bus";
async function consumeOrders(client: ServiceBusClient) {
  const receiver = client.createReceiver("orders");
  receiver.subscribe({ processMessage: async (m) => {} });
}
`
	ents, rels := runAzureMsgDetect(t, "typescript", "consume.ts", src)
	assertSubscribesTo(t, ents, rels, "orders")
}

// TestAzure_Node_EventHubProducer: new EventHubProducerClient(cs,"telemetry").
func TestAzure_Node_EventHubProducer(t *testing.T) {
	src := `const { EventHubProducerClient } = require("@azure/event-hubs");
async function emit(cs) {
  const producer = new EventHubProducerClient(cs, "telemetry");
  await producer.sendBatch([{ body: "x" }]);
}
`
	ents, rels := runAzureMsgDetect(t, "javascript", "emit.js", src)
	assertPublishesTo(t, ents, rels, "telemetry")
}

// TestAzure_Node_EventHubConsumer: new EventHubConsumerClient(grp,cs,"telemetry").
func TestAzure_Node_EventHubConsumer(t *testing.T) {
	src := `const { EventHubConsumerClient } = require("@azure/event-hubs");
async function read(cs) {
  const consumer = new EventHubConsumerClient("$Default", cs, "telemetry");
}
`
	ents, rels := runAzureMsgDetect(t, "javascript", "read.js", src)
	assertSubscribesTo(t, ents, rels, "telemetry")
}

// ---------------------------------------------------------------------------
// Python — azure-servicebus + azure-eventhub
// ---------------------------------------------------------------------------

// TestAzure_Py_QueueSender: get_queue_sender(queue_name="orders")+send_messages
// → producer PUBLISHES_TO azure:orders.
func TestAzure_Py_QueueSender(t *testing.T) {
	src := `from azure.servicebus import ServiceBusClient

def publish(client, msg):
    sender = client.get_queue_sender(queue_name="orders")
    sender.send_messages(msg)
`
	ents, rels := runAzureMsgDetect(t, "python", "publish.py", src)
	assertPublishesTo(t, ents, rels, "orders")
}

// TestAzure_Py_TopicSender: get_topic_sender(topic_name="orders") → producer.
func TestAzure_Py_TopicSender(t *testing.T) {
	src := `from azure.servicebus import ServiceBusClient

def publish(client, msg):
    sender = client.get_topic_sender(topic_name="orders")
    sender.send_messages(msg)
`
	ents, rels := runAzureMsgDetect(t, "python", "topic.py", src)
	assertPublishesTo(t, ents, rels, "orders")
}

// TestAzure_Py_QueueReceiver: get_queue_receiver(queue_name="orders") → consumer.
func TestAzure_Py_QueueReceiver(t *testing.T) {
	src := `from azure.servicebus import ServiceBusClient

def consume(client):
    receiver = client.get_queue_receiver(queue_name="orders")
    for msg in receiver:
        pass
`
	ents, rels := runAzureMsgDetect(t, "python", "consume.py", src)
	assertSubscribesTo(t, ents, rels, "orders")
}

// TestAzure_Py_SubscriptionReceiver: get_subscription_receiver(topic_name=...)
// → consumer SUBSCRIBES_TO.
func TestAzure_Py_SubscriptionReceiver(t *testing.T) {
	src := `from azure.servicebus import ServiceBusClient

def consume(client):
    receiver = client.get_subscription_receiver(topic_name="orders", subscription_name="sub1")
`
	ents, rels := runAzureMsgDetect(t, "python", "sub.py", src)
	assertSubscribesTo(t, ents, rels, "orders")
}

// TestAzure_Py_EventHubProducer: EventHubProducerClient(eventhub_name="telemetry").
func TestAzure_Py_EventHubProducer(t *testing.T) {
	src := `from azure.eventhub import EventHubProducerClient

def emit(cs):
    producer = EventHubProducerClient.from_connection_string(cs, eventhub_name="telemetry")
    producer.send_batch(producer.create_batch())
`
	ents, rels := runAzureMsgDetect(t, "python", "emit.py", src)
	assertPublishesTo(t, ents, rels, "telemetry")
}

// TestAzure_Py_EventHubConsumer: EventHubConsumerClient(eventhub_name="telemetry").
func TestAzure_Py_EventHubConsumer(t *testing.T) {
	src := `from azure.eventhub import EventHubConsumerClient

def read(cs):
    consumer = EventHubConsumerClient.from_connection_string(cs, consumer_group="$Default", eventhub_name="telemetry")
`
	ents, rels := runAzureMsgDetect(t, "python", "read.py", src)
	assertSubscribesTo(t, ents, rels, "telemetry")
}

// ---------------------------------------------------------------------------
// Cross-side join sanity + negatives
// ---------------------------------------------------------------------------

// TestAzure_TopicPassWouldJoin asserts a C# producer and a Python consumer
// emit the SAME azure:orders Name with opposite-direction edges, which is
// exactly what topic_pass joins across repos.
func TestAzure_TopicPassWouldJoin(t *testing.T) {
	prodEnts, prodRels := runAzureMsgDetect(t, "csharp", "P.cs", `using Azure.Messaging.ServiceBus;
public class P { public async Task Run(ServiceBusClient c){ var s=c.CreateSender("orders"); await s.SendMessageAsync(new ServiceBusMessage("x")); } }`)
	assertPublishesTo(t, prodEnts, prodRels, "orders")

	consEnts, consRels := runAzureMsgDetect(t, "python", "c.py", `from azure.servicebus import ServiceBusClient
def consume(client):
    r = client.get_queue_receiver(queue_name="orders")`)
	assertSubscribesTo(t, consEnts, consRels, "orders")

	// Same canonical ID on both sides — the only thing topic_pass needs.
	if azureTopicID("orders") != "azure:orders" {
		t.Fatalf("canonical id = %q, want azure:orders", azureTopicID("orders"))
	}
}

// TestAzure_DynamicName_NoFabrication: a non-literal sender name must NOT
// fabricate a topic entity or edge (honest-partial).
func TestAzure_DynamicName_NoFabrication(t *testing.T) {
	src := `using Azure.Messaging.ServiceBus;
public class Dyn {
    public async Task Run(ServiceBusClient client, string queueName) {
        var sender = client.CreateSender(queueName);
        await sender.SendMessageAsync(new ServiceBusMessage("x"));
    }
}
`
	ents, rels := runAzureMsgDetect(t, "csharp", "Dyn.cs", src)
	for _, e := range ents {
		if e.kind == messageTopicKind {
			t.Fatalf("dynamic name fabricated topic %q", e.name)
		}
	}
	if len(relsByKind(rels, publishesToEdgeKind)) != 0 {
		t.Fatalf("dynamic name fabricated a PUBLISHES_TO edge: %v", rels)
	}
}

// TestAzure_TemplateLiteralName_NoFabrication: a JS template-literal name with
// interpolation must not fabricate a topic.
func TestAzure_TemplateLiteralName_NoFabrication(t *testing.T) {
	src := "const { ServiceBusClient } = require(\"@azure/service-bus\");\n" +
		"async function pub(client, env) {\n" +
		"  const sender = client.createSender(`orders-${env}`);\n" +
		"  await sender.sendMessages({ body: 'x' });\n" +
		"}\n"
	ents, _ := runAzureMsgDetect(t, "javascript", "dyn.js", src)
	for _, e := range ents {
		if e.kind == messageTopicKind {
			t.Fatalf("template-literal name fabricated topic %q", e.name)
		}
	}
}

// TestAzure_UnsupportedLanguage: a Go file produces nothing (Go is not a
// dominant Azure messaging SDK target here).
func TestAzure_UnsupportedLanguage(t *testing.T) {
	src := `package main
func main() { client.CreateSender("orders") }`
	ents, rels := runAzureMsgDetect(t, "go", "main.go", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("unsupported lang emitted ents=%v rels=%v", ents, rels)
	}
}
