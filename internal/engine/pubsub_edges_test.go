// Tests for the Google Cloud Pub/Sub producer/consumer detection pass
// added by #726 wave 3.
//
// Coverage per language:
//   - Python: publish (resource path), subscribe (subscription path),
//     create_subscription, topic_path, Eventarc trigger, Pub/Sub Lite
//   - Node: topic.publish + pubsub.topic, subscription.on('message') + pubsub.subscription
//   - Go: client.Topic + Publish, client.Subscription + Receive
//   - Java: TopicName.of + publisher.publish, SubscriptionName.of + subscriber.startAsync,
//     MessageReceiver implementation
package engine

import (
	"strings"
	"testing"
)

// runPubSubDetect is a lightweight in-process driver for the Pub/Sub pass.
func runPubSubDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	ents, rels := applyPubSubEdges(lang, path, []byte(src), nil, nil)
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

// ---------------------------------------------------------------------------
// Python — google-cloud-pubsub
// ---------------------------------------------------------------------------

// TestPubSub_Python_PublishResourcePath covers publisher.publish with a full
// resource path projects/my-project/topics/my-topic.
func TestPubSub_Python_PublishResourcePath(t *testing.T) {
	src := `from google.cloud import pubsub_v1

def send_event(data):
    publisher = pubsub_v1.PublisherClient()
    topic_path = "projects/my-project/topics/order-events"
    publisher.publish(topic_path, data.encode())
`
	ents, rels := runPubSubDetect(t, "python", "publisher.py", src)
	topicID := pubsubTopicID("my-project", "order-events")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for %s, ents=%v", topicID, ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pubs[0].to, topicID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, topicID)
	}
	if pubs[0].props["broker"] != "pubsub" {
		t.Fatalf("broker = %q, want pubsub", pubs[0].props["broker"])
	}
}

// TestPubSub_Python_TopicPath covers publisher.topic_path(project, topic)
// emitting a topic entity without requiring a publish call.
func TestPubSub_Python_TopicPath(t *testing.T) {
	src := `from google.cloud import pubsub_v1

publisher = pubsub_v1.PublisherClient()
topic_path = publisher.topic_path('acme-project', 'payments-topic')
`
	ents, _ := runPubSubDetect(t, "python", "setup.py", src)
	topicID := pubsubTopicID("acme-project", "payments-topic")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for %s, ents=%v", topicID, ents)
	}
	q := queueByName(ents, topicID)
	if q.props["project"] != "acme-project" {
		t.Fatalf("project = %q, want acme-project", q.props["project"])
	}
}

// TestPubSub_Python_Subscribe covers subscriber.subscribe(subscription_path, callback).
func TestPubSub_Python_Subscribe(t *testing.T) {
	src := `from google.cloud import pubsub_v1

def listen():
    subscriber = pubsub_v1.SubscriberClient()
    subscription_path = "projects/my-project/subscriptions/order-sub"
    streaming_pull_future = subscriber.subscribe(subscription_path, callback=process_msg)
`
	ents, rels := runPubSubDetect(t, "python", "subscriber.py", src)
	// The subscription path "projects/my-project/subscriptions/order-sub"
	// yields a topic entity with project="my-project" and name="order-sub".
	topicID := pubsubTopicID("my-project", "order-sub")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-sub (project=my-project), ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestPubSub_Python_CreateSubscription covers subscriber.create_subscription
// emitting a topic entity without a direction edge.
func TestPubSub_Python_CreateSubscription(t *testing.T) {
	src := `from google.cloud import pubsub_v1

subscriber = pubsub_v1.SubscriberClient()
subscriber.create_subscription(
    name="projects/my-proj/subscriptions/my-sub",
    topic="projects/my-proj/topics/inventory-updates",
)
`
	ents, rels := runPubSubDetect(t, "python", "create_sub.py", src)
	topicID := pubsubTopicID("my-proj", "inventory-updates")
	q := queueByName(ents, topicID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for inventory-updates, ents=%v", ents)
	}
	if q.props["declared"] != "true" {
		t.Fatalf("declared = %q, want true", q.props["declared"])
	}
	if len(relsByKind(rels, publishesToEdgeKind))+len(relsByKind(rels, subscribesToEdgeKind)) != 0 {
		t.Fatalf("create_subscription should not emit direction edges, rels=%v", rels)
	}
}

// TestPubSub_Python_EventarcTrigger covers Cloud Run / Eventarc trigger
// handler detection as a Pub/Sub consumer.
func TestPubSub_Python_EventarcTrigger(t *testing.T) {
	src := `import functions_framework

@functions_framework.cloud_event
def hello_pubsub(cloud_event):
    """Cloud Run function triggered by Pub/Sub via Eventarc."""
    # event type: google.cloud.pubsub.topic.v1.messagePublished
    data = cloud_event.data
    process(data)
`
	ents, rels := runPubSubDetect(t, "python", "trigger.py", src)
	if len(ents) == 0 {
		t.Fatalf("expected at least one SCOPE.Queue entity, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge for Eventarc trigger, rels=%v", rels)
	}
	if subs[0].props["eventarc"] != "true" {
		t.Fatalf("eventarc property = %q, want true", subs[0].props["eventarc"])
	}
}

// TestPubSub_Python_PubSubLite covers PublisherClient usage (Pub/Sub Lite).
func TestPubSub_Python_PubSubLite(t *testing.T) {
	src := `from google.cloud.pubsublite.cloudpubsub import PublisherClient

def publish_lite(topic_path):
    with PublisherClient() as publisher:
        publisher.publish("projects/my-proj/topics/lite-topic", b"data")
`
	ents, rels := runPubSubDetect(t, "python", "lite_pub.py", src)
	if len(ents) == 0 {
		t.Fatalf("expected SCOPE.Queue entity, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Node — @google-cloud/pubsub
// ---------------------------------------------------------------------------

// TestPubSub_Node_TopicPublish covers pubsub.topic('name').publish(data).
func TestPubSub_Node_TopicPublish(t *testing.T) {
	src := `const { PubSub } = require('@google-cloud/pubsub');
const pubsub = new PubSub();

async function publishMessage(data) {
  const topic = pubsub.topic('user-events');
  await topic.publish(Buffer.from(data));
}
`
	ents, rels := runPubSubDetect(t, "javascript", "pub.js", src)
	topicID := pubsubTopicID("", "user-events")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for user-events, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestPubSub_Node_SubscriptionOnMessage covers pubsub.subscription + .on('message').
func TestPubSub_Node_SubscriptionOnMessage(t *testing.T) {
	src := `const { PubSub } = require('@google-cloud/pubsub');
const pubsub = new PubSub();

function listenForMessages() {
  const subscription = pubsub.subscription('order-sub');
  subscription.on('message', message => {
    console.log(message.data);
    message.ack();
  });
}
`
	ents, rels := runPubSubDetect(t, "javascript", "sub.js", src)
	topicID := pubsubTopicID("", "order-sub")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-sub, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Go — cloud.google.com/go/pubsub
// ---------------------------------------------------------------------------

// TestPubSub_Go_TopicPublish covers client.Topic("name") + topic.Publish(ctx, ...).
func TestPubSub_Go_TopicPublish(t *testing.T) {
	src := `package publisher

import (
	"context"
	"cloud.google.com/go/pubsub"
)

func PublishOrder(ctx context.Context, client *pubsub.Client, data []byte) {
	t := client.Topic("order-created")
	result := t.Publish(ctx, &pubsub.Message{Data: data})
	_, _ = result.Get(ctx)
}
`
	ents, rels := runPubSubDetect(t, "go", "publisher.go", src)
	topicID := pubsubTopicID("", "order-created")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["messaging_layer"] != "go-pubsub" {
		t.Fatalf("messaging_layer = %q, want go-pubsub", pubs[0].props["messaging_layer"])
	}
}

// TestPubSub_Go_SubscriptionReceive covers client.Subscription("name") + sub.Receive(ctx, ...).
func TestPubSub_Go_SubscriptionReceive(t *testing.T) {
	src := `package subscriber

import (
	"context"
	"cloud.google.com/go/pubsub"
)

func ConsumeOrders(ctx context.Context, client *pubsub.Client) {
	sub := client.Subscription("order-sub")
	err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		process(msg.Data)
		msg.Ack()
	})
	_ = err
}
`
	ents, rels := runPubSubDetect(t, "go", "subscriber.go", src)
	topicID := pubsubTopicID("", "order-sub")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-sub, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Java — google-cloud-pubsub
// ---------------------------------------------------------------------------

// TestPubSub_Java_PublisherPublish covers TopicName.of + publisher.publish.
func TestPubSub_Java_PublisherPublish(t *testing.T) {
	src := `import com.google.cloud.pubsub.v1.Publisher;
import com.google.pubsub.v1.TopicName;

public class OrderPublisher {
    public void publish(String message) throws Exception {
        TopicName topicName = TopicName.of("my-project", "order-topic");
        Publisher publisher = Publisher.newBuilder(topicName).build();
        PubsubMessage msg = PubsubMessage.newBuilder()
            .setData(ByteString.copyFromUtf8(message))
            .build();
        publisher.publish(msg);
    }
}
`
	ents, rels := runPubSubDetect(t, "java", "OrderPublisher.java", src)
	topicID := pubsubTopicID("my-project", "order-topic")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-topic, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestPubSub_Java_SubscriberStartAsync covers SubscriptionName.of + subscriber.startAsync.
func TestPubSub_Java_SubscriberStartAsync(t *testing.T) {
	src := `import com.google.cloud.pubsub.v1.Subscriber;
import com.google.pubsub.v1.SubscriptionName;

public class OrderSubscriber {
    public void subscribe() {
        SubscriptionName subscriptionName = SubscriptionName.of("my-project", "order-sub");
        Subscriber subscriber = Subscriber.newBuilder(subscriptionName, receiver).build();
        subscriber.startAsync().awaitRunning();
    }
}
`
	ents, rels := runPubSubDetect(t, "java", "OrderSubscriber.java", src)
	topicID := pubsubTopicID("my-project", "order-sub")
	if queueByName(ents, topicID) == nil {
		t.Fatalf("expected SCOPE.Queue for order-sub, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestPubSub_Java_MessageReceiver covers MessageReceiver interface implementation.
func TestPubSub_Java_MessageReceiver(t *testing.T) {
	src := `import com.google.cloud.pubsub.v1.MessageReceiver;

public class PaymentProcessor implements MessageReceiver {
    @Override
    public void receiveMessage(PubsubMessage message, AckReplyConsumer consumer) {
        processPayment(message.getData());
        consumer.ack();
    }
}
`
	ents, rels := runPubSubDetect(t, "java", "PaymentProcessor.java", src)
	if len(ents) == 0 {
		t.Fatalf("expected at least one SCOPE.Queue entity, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge for MessageReceiver, rels=%v", rels)
	}
	if subs[0].props["message_receiver"] != "true" {
		t.Fatalf("message_receiver = %q, want true", subs[0].props["message_receiver"])
	}
}

// TestPubSub_NoBrokerSignal verifies that files without Pub/Sub imports
// produce no output.
func TestPubSub_NoBrokerSignal(t *testing.T) {
	src := `def some_function():
    result = requests.get("https://example.com/api/data")
    return result.json()
`
	ents, rels := runPubSubDetect(t, "python", "http.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("no pubsub signal: expected empty, got ents=%v rels=%v", ents, rels)
	}
}

// TestPubSub_UnsupportedLanguage verifies Rust is skipped gracefully.
func TestPubSub_UnsupportedLanguage(t *testing.T) {
	src := `fn publish(topic: &str) { /* pubsub publish */ }`
	ents, rels := runPubSubDetect(t, "rust", "pub.rs", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("rust should be skipped, got ents=%v rels=%v", ents, rels)
	}
}
