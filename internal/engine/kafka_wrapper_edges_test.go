// Tests for the kafka wrapper + transport idiom detection pass added by #1467.
//
// Each section covers one of the four new idiom families, with a happy-path
// test (static topic literal → entity + edge emitted) and where applicable a
// no-op test (guard fires correctly so unrelated code is not mis-tagged).
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runWrapperDetect is the parallel to runKafkaDetect for the wrapper pass.
func runWrapperDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	return applyKafkaWrapperEdges(lang, path, []byte(src), nil, nil)
}

// ---------------------------------------------------------------------------
// 1. Python KafkaBus wrapper
// ---------------------------------------------------------------------------

// TestKafkaWrapper_PythonBusPublish verifies that bus.publish("topic", ...)
// emits a MessageTopic + PUBLISHES_TO when the file references a KafkaBus
// class name.
func TestKafkaWrapper_PythonBusPublish(t *testing.T) {
	src := `from py_shared import KafkaBus

bus = KafkaBus()

async def place_order(order):
    await bus.publish("orders.placed", order)
`
	ents, rels := runWrapperDetect(t, "python", "orders/service.py", src)

	tp := topicByName(ents, "orders.placed")
	if tp == nil {
		t.Fatalf("expected MessageTopic for orders.placed, ents=%v", ents)
	}
	if tp.Properties["broker"] != "kafka" {
		t.Errorf("broker: want kafka, got %q", tp.Properties["broker"])
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pub[0].ToID, "kafka:orders.placed") {
		t.Errorf("PUBLISHES_TO ToID = %q, want suffix kafka:orders.placed", pub[0].ToID)
	}
}

// TestKafkaWrapper_PythonBusConsumer verifies that bus.consumer("topic") emits
// a MessageTopic + SUBSCRIBES_TO.
func TestKafkaWrapper_PythonBusConsumer(t *testing.T) {
	src := `from py_shared.kafka_bus import KafkaBus

bus = KafkaBus()

async def run():
    async for msg in bus.consumer("payments.settled"):
        await handle(msg)
`
	ents, rels := runWrapperDetect(t, "python", "workers/consumer.py", src)

	tp := topicByName(ents, "payments.settled")
	if tp == nil {
		t.Fatalf("expected MessageTopic for payments.settled, ents=%v", ents)
	}
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(subs[0].ToID, "kafka:payments.settled") {
		t.Errorf("SUBSCRIBES_TO ToID = %q, want suffix kafka:payments.settled", subs[0].ToID)
	}
}

// TestKafkaWrapper_PythonBusSubscribe verifies the .subscribe() form of the
// bus wrapper consumer.
func TestKafkaWrapper_PythonBusSubscribe(t *testing.T) {
	src := `from shared.bus import EventBus

eb = EventBus()

def listen():
    eb.subscribe("inventory.reserved", callback)
`
	ents, rels := runWrapperDetect(t, "python", "listeners/inventory.py", src)

	if topicByName(ents, "inventory.reserved") == nil {
		t.Fatalf("expected MessageTopic for inventory.reserved, ents=%v", ents)
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestKafkaWrapper_PythonBus_NoFire_WithoutGuard verifies that a file that
// calls .publish() without any bus-class reference does NOT produce a topic
// entity (false-positive guard).
func TestKafkaWrapper_PythonBus_NoFire_WithoutGuard(t *testing.T) {
	src := `import requests

def send(topic, msg):
    # Not a Kafka bus — just a regular publish to an HTTP endpoint
    requests.publish(topic, msg)
`
	ents, _ := runWrapperDetect(t, "python", "notabus.py", src)
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			t.Errorf("must not emit MessageTopic without bus-class guard, got %v", e)
		}
	}
}

// TestKafkaWrapper_PythonBus_MultipleTopics verifies that multiple
// publish/consumer calls in one file each emit independent topic entities.
func TestKafkaWrapper_PythonBus_MultipleTopics(t *testing.T) {
	src := `from py_shared import KafkaBus

bus = KafkaBus()

async def on_order(order):
    await bus.publish("orders.placed", order)
    await bus.publish("order-saga.started", order)

async def run():
    async for msg in bus.consumer("payments.settled"):
        pass
`
	ents, rels := runWrapperDetect(t, "python", "orders/saga.py", src)

	topics := map[string]bool{}
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			topics[e.Properties["topic_name"]] = true
		}
	}
	for _, want := range []string{"orders.placed", "order-saga.started", "payments.settled"} {
		if !topics[want] {
			t.Errorf("expected MessageTopic for %q, got %v", want, topics)
		}
	}

	if len(edgesOfKind(rels, publishesToEdgeKind)) < 2 {
		t.Errorf("expected ≥2 PUBLISHES_TO edges")
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) < 1 {
		t.Errorf("expected ≥1 SUBSCRIBES_TO edge")
	}
}

// ---------------------------------------------------------------------------
// 2. Java Kafka Streams
// ---------------------------------------------------------------------------

// TestKafkaWrapper_JavaKafkaStreams_SourceAndSink verifies that
// builder.stream("source-topic") + kStream.to("sink-topic") each produce a
// MessageTopic entity with the correct edge direction.
func TestKafkaWrapper_JavaKafkaStreams_SourceAndSink(t *testing.T) {
	src := `package io.demo;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;

public class OrderEnricher {
    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();
        KStream<String, Order> orders = builder.stream("orders.placed");
        orders.filter((k, v) -> v != null)
              .to("orders.enriched");
        return builder.build();
    }
}
`
	ents, rels := runWrapperDetect(t, "java", "stream-processor/OrderEnricher.java", src)

	if topicByName(ents, "orders.placed") == nil {
		t.Fatalf("expected MessageTopic for orders.placed (source), ents=%v", ents)
	}
	if topicByName(ents, "orders.enriched") == nil {
		t.Fatalf("expected MessageTopic for orders.enriched (sink), ents=%v", ents)
	}

	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge for source topic, rels=%v", rels)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge for sink topic, rels=%v", rels)
	}

	// Verify source has stream_role=source and sink has stream_role=sink.
	for _, r := range subs {
		if r.Properties["stream_role"] != "source" {
			t.Errorf("SUBSCRIBES_TO stream_role: want source, got %q", r.Properties["stream_role"])
		}
	}
	for _, r := range pub {
		if r.Properties["stream_role"] != "sink" {
			t.Errorf("PUBLISHES_TO stream_role: want sink, got %q", r.Properties["stream_role"])
		}
	}
}

// TestKafkaWrapper_JavaKafkaStreams_HighValueBranch verifies that a branched
// topology writing to orders.high_value also emits a MessageTopic.
func TestKafkaWrapper_JavaKafkaStreams_HighValueBranch(t *testing.T) {
	src := `package io.demo;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;

public class HighValueRouter {
    public void buildTopology(StreamsBuilder builder) {
        KStream<String, Order> stream = builder.stream("orders.enriched");
        stream.filter((k, v) -> v.total > 1000)
              .to("orders.high_value");
        stream.filter((k, v) -> v.total <= 1000)
              .to("orders.standard");
    }
}
`
	ents, rels := runWrapperDetect(t, "java", "stream-processor/HighValueRouter.java", src)

	for _, want := range []string{"orders.enriched", "orders.high_value", "orders.standard"} {
		if topicByName(ents, want) == nil {
			t.Errorf("expected MessageTopic for %q, ents=%v", want, ents)
		}
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) < 2 {
		t.Errorf("expected ≥2 PUBLISHES_TO edges for sink topics, rels=%v", rels)
	}
}

// TestKafkaWrapper_JavaKafkaStreams_NoFire_WithoutGuard verifies that a
// plain Java file calling .stream() unrelated to Kafka Streams does NOT
// produce a MessageTopic (e.g. a file with no KafkaStreams/KStream/
// StreamsBuilder token).
func TestKafkaWrapper_JavaKafkaStreams_NoFire_WithoutGuard(t *testing.T) {
	src := `package io.demo;
import java.util.stream.Stream;

public class Utils {
    public void process(Stream<String> data) {
        data.filter(s -> s.length() > 0)
            .forEach(System.out::println);
    }
}
`
	ents, _ := runWrapperDetect(t, "java", "Utils.java", src)
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			t.Errorf("must not emit MessageTopic from plain Java Streams, got %v", e)
		}
	}
}

// TestKafkaWrapper_JavaKafkaStreams_Through verifies that kStream.through("topic")
// emits a MessageTopic with BOTH PUBLISHES_TO and SUBSCRIBES_TO edges, since
// Kafka Streams writes to the through-topic and reads it back internally
// (repartition / intermediate routing).
func TestKafkaWrapper_JavaKafkaStreams_Through(t *testing.T) {
	src := `package io.demo;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;

public class PaymentRouter {
    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();
        KStream<String, Payment> payments = builder.stream("payments.settled");
        KStream<String, Payment> repartitioned = payments.through("payments.normalised");
        repartitioned.filter((k, v) -> v != null)
                     .to("orders.enriched");
        return builder.build();
    }
}
`
	ents, rels := runWrapperDetect(t, "java", "stream-processor/PaymentRouter.java", src)

	// through-topic must be present as a MessageTopic entity.
	throughTopic := topicByName(ents, "payments.normalised")
	if throughTopic == nil {
		t.Fatalf("expected MessageTopic for payments.normalised (through), ents=%v", ents)
	}
	if throughTopic.Properties["stream_role"] != "through" {
		t.Errorf("stream_role: want through, got %q", throughTopic.Properties["stream_role"])
	}

	// through-topic must have BOTH PUBLISHES_TO and SUBSCRIBES_TO edges.
	var pubToThrough, subToThrough bool
	for _, r := range rels {
		if strings.Contains(r.ToID, "kafka:payments.normalised") {
			switch r.Kind {
			case publishesToEdgeKind:
				pubToThrough = true
			case subscribesToEdgeKind:
				subToThrough = true
			}
		}
	}
	if !pubToThrough {
		t.Errorf("expected PUBLISHES_TO edge for through-topic payments.normalised, rels=%v", rels)
	}
	if !subToThrough {
		t.Errorf("expected SUBSCRIBES_TO edge for through-topic payments.normalised, rels=%v", rels)
	}

	// Source + final sink must also be present.
	if topicByName(ents, "payments.settled") == nil {
		t.Fatalf("expected MessageTopic for payments.settled (source), ents=%v", ents)
	}
	if topicByName(ents, "orders.enriched") == nil {
		t.Fatalf("expected MessageTopic for orders.enriched (sink), ents=%v", ents)
	}
}

// TestKafkaWrapper_JavaKafkaStreams_MapValuesThenTo verifies that a chained
// .mapValues(...).to("topic") form produces a PUBLISHES_TO edge for the sink.
func TestKafkaWrapper_JavaKafkaStreams_MapValuesThenTo(t *testing.T) {
	src := `package io.demo;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;

public class OrderEnricher {
    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();
        KStream<String, Order> orders = builder.stream("orders.placed");
        orders.mapValues(order -> enrich(order))
              .to("orders.enriched");
        return builder.build();
    }
}
`
	ents, rels := runWrapperDetect(t, "java", "stream-processor/OrderEnricher.java", src)

	if topicByName(ents, "orders.placed") == nil {
		t.Fatalf("expected MessageTopic for orders.placed, ents=%v", ents)
	}
	if topicByName(ents, "orders.enriched") == nil {
		t.Fatalf("expected MessageTopic for orders.enriched, ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Errorf("expected PUBLISHES_TO edge for orders.enriched, rels=%v", rels)
	}
}

// TestKafkaWrapper_JavaKafkaStreams_StreamProcessorFixture verifies the full
// stream-processor fixture: consumes orders.placed + payments.settled,
// produces orders.enriched + orders.high_value.
// This is the fixture-level recall test for #1480 — equivalent of
// TestKafkaWrapper_ShipFastTopicRecall but for the Kafka Streams DSL.
func TestKafkaWrapper_JavaKafkaStreams_StreamProcessorFixture(t *testing.T) {
	// OrderEnrichmentTopology: source=orders.placed + payments.settled,
	// sink=orders.enriched + orders.high_value.
	enrichmentSrc := `package io.streamprocessor;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;

public class OrderEnrichmentTopology {
    public Topology buildTopology() {
        StreamsBuilder builder = new StreamsBuilder();
        KStream<String, Order> ordersStream = builder.stream("orders.placed");
        KStream<String, Payment> paymentsStream = builder.stream("payments.settled");
        KStream<String, EnrichedOrder> enriched = ordersStream.mapValues(order -> enrich(order));
        enriched.to("orders.enriched");
        enriched.filter((key, value) -> value != null && value.total > 1000)
                .to("orders.high_value");
        return builder.build();
    }
}
`
	ents, rels := runWrapperDetect(t, "java",
		"stream-processor/src/main/java/io/streamprocessor/OrderEnrichmentTopology.java",
		enrichmentSrc)

	// All four expected topics must be present.
	for _, want := range []string{"orders.placed", "payments.settled", "orders.enriched", "orders.high_value"} {
		if topicByName(ents, want) == nil {
			t.Errorf("expected MessageTopic for %q, ents=%v", want, ents)
		}
	}

	// Two SUBSCRIBES_TO edges (source topics).
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) < 2 {
		t.Errorf("expected ≥2 SUBSCRIBES_TO edges (source topics), got %d; rels=%v", len(subs), rels)
	}

	// Two PUBLISHES_TO edges (sink topics).
	pubs := edgesOfKind(rels, publishesToEdgeKind)
	if len(pubs) < 2 {
		t.Errorf("expected ≥2 PUBLISHES_TO edges (sink topics), got %d; rels=%v", len(pubs), rels)
	}

	// Canonical IDs must match what P7 will join on.
	wantIDs := map[string]bool{
		"kafka:orders.placed":     false,
		"kafka:payments.settled":  false,
		"kafka:orders.enriched":   false,
		"kafka:orders.high_value": false,
	}
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			wantIDs[e.Name] = true
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("canonical MessageTopic %q not emitted — P7 cross-repo link will miss it", id)
		}
	}

	t.Logf("stream-processor fixture: %d topics, %d SUBSCRIBES_TO, %d PUBLISHES_TO",
		len(ents), len(subs), len(pubs))
}

// TestKafkaWrapper_JavaKafkaStreams_ConstantTopicArgs is the #1489 regression
// test: the REAL polyglot fixture's OrderEnrichmentTopology uses named
// `static final String` constants for every topic, not inline literals:
//
//	public static final String OUT_ENRICHED = "orders.enriched";
//	builder.stream(SRC_ORDERS, ...); enriched.to(OUT_ENRICHED, ...);
//
// Before #1489 the `.stream(...)` / `.to(...)` regexes only matched string
// literals, so stream-processor emitted ZERO topic entities on the real graph
// (verified) and the stream-processor→{analytics,search-os,notifications}
// cross-repo links never fired despite the synthetic literal test passing.
func TestKafkaWrapper_JavaKafkaStreams_ConstantTopicArgs(t *testing.T) {
	src := `package io.shipfast.streams;
import org.apache.kafka.streams.StreamsBuilder;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;

public class OrderEnrichmentTopology {
    public static final String SRC_ORDERS = "orders.placed";
    public static final String SRC_PAYMENTS = "payments.settled";
    public static final String OUT_ENRICHED = "orders.enriched";
    public static final String OUT_HIGH_VALUE = "orders.high_value";

    public Topology build() {
        StreamsBuilder builder = new StreamsBuilder();
        KStream<String, String> orders = builder.stream(SRC_ORDERS, Consumed.with());
        KStream<String, String> payments = builder.stream(SRC_PAYMENTS, Consumed.with());
        KStream<String, String> enriched = orders.join(payments, OrderEnricher::merge);
        enriched.to(OUT_ENRICHED, Produced.with());
        enriched.filter((k, v) -> total(v) >= 100000L).to(OUT_HIGH_VALUE, Produced.with());
        return builder.build();
    }
}
`
	ents, rels := runWrapperDetect(t, "java",
		"stream-processor/src/main/java/io/shipfast/streams/OrderEnrichmentTopology.java", src)

	wantIDs := map[string]bool{
		"kafka:orders.placed":     false,
		"kafka:payments.settled":  false,
		"kafka:orders.enriched":   false,
		"kafka:orders.high_value": false,
	}
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			if _, ok := wantIDs[e.Name]; ok {
				wantIDs[e.Name] = true
			}
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("constant-arg topology: MessageTopic %q not emitted; ents=%v", id, ents)
		}
	}

	if subs := edgesOfKind(rels, subscribesToEdgeKind); len(subs) < 2 {
		t.Errorf("expected ≥2 SUBSCRIBES_TO (constant source topics), got %d", len(subs))
	}
	if pubs := edgesOfKind(rels, publishesToEdgeKind); len(pubs) < 2 {
		t.Errorf("expected ≥2 PUBLISHES_TO (constant sink topics), got %d", len(pubs))
	}
}

// TestKafkaWrapper_JavaKafkaStreams_UnresolvedConstantSkipped verifies that a
// `.to(SOME_VAR)` whose identifier is NOT a known string constant is skipped
// rather than emitting a topic named after the variable.
func TestKafkaWrapper_JavaKafkaStreams_UnresolvedConstantSkipped(t *testing.T) {
	src := `package io.demo;
import org.apache.kafka.streams.kstream.KStream;
import org.apache.kafka.streams.KafkaStreams;
public class T {
    void build(KStream<String,String> enriched, String runtimeTopic) {
        enriched.to(runtimeTopic);
    }
}
`
	ents, _ := runWrapperDetect(t, "java", "demo/T.java", src)
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			t.Errorf("unresolved identifier should emit no topic, got %q", e.Name)
		}
	}
}

// TestKafkaWrapper_JavaKafkaStreams_ThroughTopicNoFireOnPlainJava verifies
// that .through() on a plain Java non-Streams file does not emit any topic.
func TestKafkaWrapper_JavaKafkaStreams_ThroughTopicNoFireOnPlainJava(t *testing.T) {
	src := `package io.demo;

public class UrlBuilder {
    // through() here is a path-component helper, not Kafka Streams.
    public String buildUrl(String base) {
        return base.through("normalised");
    }
}
`
	ents, _ := runWrapperDetect(t, "java", "UrlBuilder.java", src)
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			t.Errorf("must not emit MessageTopic from plain through() call, got %v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Java / Kotlin Spring RedisTemplate.convertAndSend
//
// As of #1482 this detection is handled by applyRedisPubSubEdges
// (redis_pubsub_edges.go) which emits the canonical
// channel:redis-pubsub:<channel> SCOPE.Queue entity IDs so the P7 cross-repo
// linker correctly joins the Kotlin publisher with its consumer.  The wrapper
// pass no longer emits a separate (wrong-ID) entity for this idiom.
// ---------------------------------------------------------------------------

// TestKafkaWrapper_JavaRedisConvertAndSend_RedisPubSubPass verifies that the
// correct pass (applyRedisPubSubEdges) now handles Spring
// redisTemplate.convertAndSend and emits the canonical
// channel:redis-pubsub:<channel> SCOPE.Queue entity so P7 links fire.
func TestKafkaWrapper_JavaRedisConvertAndSend_RedisPubSubPass(t *testing.T) {
	src := `package io.demo;
import org.springframework.data.redis.core.RedisTemplate;

public class NotificationService {
    private final RedisTemplate<String, Object> redisTemplate;

    public void sendNotification(Notification n) {
        redisTemplate.convertAndSend("notifications.channel", n);
    }
}
`
	ents, rels := applyRedisPubSubEdges("java", "notifications/NotificationService.java", []byte(src), nil, nil)

	wantID := "channel:redis-pubsub:notifications.channel"
	var found *types.EntityRecord
	for i := range ents {
		if ents[i].Name == wantID {
			found = &ents[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected SCOPE.Queue entity %q, ents=%v", wantID, ents)
	}
	if found.Kind != redisPubSubChannelEntityKind {
		t.Errorf("Kind: want %q, got %q", redisPubSubChannelEntityKind, found.Kind)
	}
	if found.Properties["broker"] != "redis" {
		t.Errorf("broker: want redis, got %q", found.Properties["broker"])
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	// Verify the ToID uses the canonical SCOPE.Queue prefix.
	if !strings.Contains(pub[0].ToID, "SCOPE.Queue:channel:redis-pubsub:notifications.channel") {
		t.Errorf("PUBLISHES_TO ToID = %q, want SCOPE.Queue:channel:redis-pubsub:notifications.channel", pub[0].ToID)
	}

	// Also verify the kafka_wrapper pass no longer emits a competing wrong-ID entity.
	wrapperEnts, _ := runWrapperDetect(t, "java", "notifications/NotificationService.java", src)
	for _, e := range wrapperEnts {
		if strings.HasPrefix(e.Name, "redis:") {
			t.Errorf("kafka_wrapper_edges must not emit redis:<channel> entity after #1482 fix, got %v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. AWS SNS publish
// ---------------------------------------------------------------------------

// TestKafkaWrapper_PythonSNSPublish verifies that
// sns.publish(TopicArn='arn:...:payments-settled', ...) emits
// sns:payments-settled MessageTopic + PUBLISHES_TO.
func TestKafkaWrapper_PythonSNSPublish(t *testing.T) {
	src := `import boto3
sns = boto3.client('sns', region_name='us-east-1')

TOPIC_ARN = 'arn:aws:sns:us-east-1:123456789012:payments-settled'

def settle_payment(payment_id: str):
    sns.publish(
        TopicArn=TOPIC_ARN,
        Message=str(payment_id),
    )
`
	ents, rels := runWrapperDetect(t, "python", "payments/service.py", src)

	var found *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == messageTopicKind && ents[i].Properties["topic_name"] == "payments-settled" {
			found = &ents[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected MessageTopic for payments-settled, ents=%v", ents)
	}
	if found.Properties["broker"] != "sns" {
		t.Errorf("broker: want sns, got %q", found.Properties["broker"])
	}
	if !strings.HasPrefix(found.Name, "sns:") {
		t.Errorf("Name: want sns: prefix, got %q", found.Name)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pub[0].ToID, "sns:payments-settled") {
		t.Errorf("PUBLISHES_TO ToID = %q, want suffix sns:payments-settled", pub[0].ToID)
	}
}

// TestKafkaWrapper_PythonSNSPublish_BareName verifies that when the TopicArn
// is already a bare name (not an ARN), the topic name is preserved as-is.
func TestKafkaWrapper_PythonSNSPublish_BareName(t *testing.T) {
	src := `import boto3
sns = boto3.client('sns')

def notify():
    sns.publish(TopicArn='inventory.reserved', Message='ok')
`
	ents, _ := runWrapperDetect(t, "python", "inventory/notify.py", src)

	if topicByName(ents, "inventory.reserved") == nil {
		t.Fatalf("expected MessageTopic for inventory.reserved (bare name), ents=%v", ents)
	}
}

// TestKafkaWrapper_NodeSNSPublish verifies Node/TS AWS SDK v3:
// new PublishCommand({ TopicArn: "arn:...:inventory-reserved", ... }).
func TestKafkaWrapper_NodeSNSPublish(t *testing.T) {
	src := `import { SNSClient, PublishCommand } from "@aws-sdk/client-sns";
const sns = new SNSClient({});

async function publishInventoryReserved(item) {
  await sns.send(new PublishCommand({
    TopicArn: "arn:aws:sns:us-east-1:123456789012:inventory-reserved",
    Message: JSON.stringify(item),
  }));
}
`
	ents, rels := runWrapperDetect(t, "typescript", "inventory/publish.ts", src)

	if topicByName(ents, "inventory-reserved") == nil {
		t.Fatalf("expected MessageTopic for inventory-reserved, ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestKafkaWrapper_GoSNSPublish verifies Go AWS SDK v2:
// snsClient.Publish(ctx, &sns.PublishInput{TopicArn: aws.String("arn:..."), ...}).
func TestKafkaWrapper_GoSNSPublish(t *testing.T) {
	src := `package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/sns"
)

func PublishOrdersPlaced(ctx context.Context, client *sns.Client, msg string) error {
    _, err := client.Publish(ctx, &sns.PublishInput{
        TopicArn: aws.String("arn:aws:sns:us-east-1:123456789012:orders-placed"),
        Message:  aws.String(msg),
    })
    return err
}
`
	ents, rels := runWrapperDetect(t, "go", "orders/publish.go", src)

	if topicByName(ents, "orders-placed") == nil {
		t.Fatalf("expected MessageTopic for orders-placed, ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestKafkaWrapper_GoSNSPublish_ConstResolved verifies that a TopicArn that
// references a package-level string const (rather than an inline literal) is
// resolved via the Go const table. Regression for #1553 — the ShipFast
// inventory service publishes inventory.reserved with a named ARN const.
func TestKafkaWrapper_GoSNSPublish_ConstResolved(t *testing.T) {
	src := `package internal

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/sns"
)

const inventoryReservedTopicARN = "arn:aws:sns:us-east-1:000000000000:inventory.reserved"

func publishReserved(ctx context.Context, c *sns.Client, msg string) {
    c.Publish(ctx, &sns.PublishInput{
        TopicArn: aws.String(inventoryReservedTopicARN),
        Message:  aws.String(msg),
    })
}
`
	ents, rels := runWrapperDetect(t, "go", "inventory/consumer.go", src)

	if topicByName(ents, "inventory.reserved") == nil {
		t.Fatalf("expected MessageTopic for inventory.reserved (const-resolved), ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestKafkaWrapper_SNSTopicNameFromARN exercises the ARN-to-name extractor.
func TestKafkaWrapper_SNSTopicNameFromARN(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"arn:aws:sns:us-east-1:123456789012:payments-settled", "payments-settled"},
		{"arn:aws:sns:eu-west-1:999999999999:inventory.reserved", "inventory.reserved"},
		{"payments-settled", "payments-settled"}, // bare name passthrough
		{"", ""},
	}
	for _, tc := range cases {
		got := snsTopicNameFromARN(tc.in)
		if got != tc.want {
			t.Errorf("snsTopicNameFromARN(%q): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

// TestKafkaWrapper_NoOpForUnsupportedLanguage verifies the pass is a strict
// no-op for unsupported languages (defensive regression guard).
func TestKafkaWrapper_NoOpForUnsupportedLanguage(t *testing.T) {
	ents, rels := runWrapperDetect(t, "ruby", "lib/x.rb", `bus.publish("topic", msg)`)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no-op for ruby, got ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// Fixture-level cross-repo topic link recall test
// ---------------------------------------------------------------------------

// TestKafkaWrapper_ShipFastTopicRecall verifies that after indexing ShipFast-
// style Python services that use the KafkaBus wrapper, the shared topic names
// (orders.placed, payments.settled) appear as MessageTopic entities with
// matching names that would be joined by the P7 cross-repo linker.
//
// This is an extraction-only test: it confirms that the canonical IDs emitted
// by the publisher service match those emitted by the subscriber service so
// that P7 can link them — it does not invoke P7 directly (the full P7 test
// lives in links/shipfast_grpc_topic_test.go).
func TestKafkaWrapper_ShipFastTopicRecall(t *testing.T) {
	// Producer: orders service uses KafkaBus wrapper to publish orders.placed.
	producerSrc := `from py_shared import KafkaBus

bus = KafkaBus()

async def place_order(order):
    await bus.publish("orders.placed", order)
    await bus.publish("order-saga.started", order)
`

	// Consumer: inventory service uses KafkaBus wrapper to consume orders.placed.
	consumerSrc := `from py_shared import KafkaBus

bus = KafkaBus()

async def run():
    async for msg in bus.consumer("orders.placed"):
        await reserve_inventory(msg)
`

	pubEnts, _ := runWrapperDetect(t, "python", "orders/service.py", producerSrc)
	subEnts, _ := runWrapperDetect(t, "python", "inventory/consumer.py", consumerSrc)

	// Both sides must emit a MessageTopic with Name = "kafka:orders.placed".
	findTopic := func(ents []types.EntityRecord, name string) bool {
		for _, e := range ents {
			if e.Kind == messageTopicKind && e.Name == "kafka:orders.placed" {
				_ = name
				return true
			}
		}
		return false
	}

	if !findTopic(pubEnts, "orders.placed") {
		t.Fatalf("publisher side did not emit kafka:orders.placed entity, ents=%v", pubEnts)
	}
	if !findTopic(subEnts, "orders.placed") {
		t.Fatalf("subscriber side did not emit kafka:orders.placed entity, ents=%v", subEnts)
	}

	t.Logf("Both sides emit kafka:orders.placed — P7 cross-repo link will fire")
}
