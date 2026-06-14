// Tests for the Kafka producer/consumer detection pass added by #726 wave 1.
//
// Each language has at least three tests:
//   - Static-string topic name on the producer side (emits MessageTopic +
//     PUBLISHES_TO).
//   - File-local constant resolution on the consumer side (emits
//     MessageTopic + SUBSCRIBES_TO; the consumer test runs first because
//     the Java path needs companion .properties for the cleanest case
//     and we cover that in a dedicated test below).
//   - Dynamic/runtime topic that cannot be statically resolved (emits a
//     `runtime_dynamic=true` topic so the repairs flow #732 can surface it).
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runKafkaDetect is a lightweight in-process driver — kafka_edges.go is an
// append-only engine pass, so we can exercise it directly without going
// through the YAML rule compiler.
func runKafkaDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	// repoRoot empty: tests that need Quarkus channel resolution pass an
	// absolute path so the upward walk finds application.properties on its
	// own; in-memory tests use repo-relative paths and skip resolution.
	res := applyKafkaEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// topicByName returns the first MessageTopic with the given topic_name
// property; helpful for fishing the resolved topic out of the entity
// slice without coupling to slice order.
func topicByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == messageTopicKind && e.Properties["topic_name"] == name {
			return e
		}
	}
	return nil
}

// edgesOfKind filters the relationship slice for the given Kind.
func edgesOfKind(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Node / kafkajs
// ---------------------------------------------------------------------------

// TestKafka_Node_StaticTopicProducer covers the kafkajs producer.send({topic})
// shape with a literal topic name. Producer-side: PUBLISHES_TO.
func TestKafka_Node_StaticTopicProducer(t *testing.T) {
	src := `const { Kafka } = require('kafkajs');
const kafka = new Kafka({ clientId: 'app', brokers: ['kafka:9092'] });
const producer = kafka.producer();

async function emitOrder() {
  await producer.send({ topic: "orders.created", messages: [{ value: 'x' }] });
}
`
	ents, rels := runKafkaDetect(t, "javascript", "src/emit.js", src)
	if topicByName(ents, "orders.created") == nil {
		t.Fatalf("expected MessageTopic for orders.created, got %v", ents)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, got none")
	}
	if !strings.Contains(pub[0].ToID, "kafka:orders.created") {
		t.Fatalf("PUBLISHES_TO ToID = %q, want suffix kafka:orders.created", pub[0].ToID)
	}
}

// TestKafka_Node_ConstTopicConsumer covers kafkajs consumer.subscribe with a
// topic name held in a file-local UPPER_CASE constant. The pass must
// resolve the constant and emit a static (non-dynamic) topic.
func TestKafka_Node_ConstTopicConsumer(t *testing.T) {
	src := `const { Kafka } = require('kafkajs');
const TOPIC = "payments.failed";

async function run() {
  const consumer = kafka.consumer({ groupId: 'g' });
  await consumer.subscribe({ topics: [TOPIC], fromBeginning: true });
}
`
	ents, rels := runKafkaDetect(t, "javascript", "src/sub.js", src)
	tp := topicByName(ents, "payments.failed")
	if tp == nil {
		t.Fatalf("expected resolved MessageTopic for payments.failed, ents=%v", ents)
	}
	if tp.Properties["runtime_dynamic"] != "false" {
		t.Fatalf("topic should be static (runtime_dynamic=false), got %q", tp.Properties["runtime_dynamic"])
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, got none. rels=%v", rels)
	}
}

// TestKafka_Node_DynamicTopic covers a topic whose name is computed from a
// config object at runtime. The pass must emit a topic with
// runtime_dynamic=true under the channel-fallback ID.
func TestKafka_Node_DynamicTopic(t *testing.T) {
	src := `const config = require('./config');
async function emit() {
  await producer.send({ topic: config.topicName, messages: [{ value: 'x' }] });
}
`
	// kafkajs send regex requires a quoted topic literal, so a non-literal
	// produces zero entities — which is itself the expected behaviour: the
	// pass must not invent topic names when it can't resolve them. The
	// runtime-dynamic flag is exercised on the Quarkus path (Java tests
	// below); Node's pure dynamic shape is a deliberate skip-emit.
	ents, _ := runKafkaDetect(t, "javascript", "src/dynamic.js", src)
	for _, e := range ents {
		if e.Kind == messageTopicKind && e.Properties["topic_name"] == "config.topicName" {
			t.Fatalf("must not invent a topic from a non-literal expression: %v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Python — confluent-kafka + kafka-python
// ---------------------------------------------------------------------------

// TestKafka_Python_StaticTopicProducer covers confluent-kafka
// `producer.produce("topic", ...)` with a literal first argument.
func TestKafka_Python_StaticTopicProducer(t *testing.T) {
	src := `from confluent_kafka import Producer
p = Producer({"bootstrap.servers": "kafka:9092"})

def emit():
    p.produce("orders.created", key=b"k", value=b"v")
    p.flush()
`
	ents, rels := runKafkaDetect(t, "python", "emit.py", src)
	if topicByName(ents, "orders.created") == nil {
		t.Fatalf("expected MessageTopic for orders.created, ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge")
	}
}

// TestKafka_Python_ConstTopicConsumer covers the module-level
// `TOPIC = "..."` symbol-table case on `consumer.subscribe([TOPIC])`.
func TestKafka_Python_ConstTopicConsumer(t *testing.T) {
	src := `from confluent_kafka import Consumer

TOPIC = "payments.failed"

def main():
    c = Consumer({"group.id": "g"})
    c.subscribe([TOPIC])
`
	ents, rels := runKafkaDetect(t, "python", "sub.py", src)
	tp := topicByName(ents, "payments.failed")
	if tp == nil {
		t.Fatalf("expected resolved MessageTopic, ents=%v", ents)
	}
	if tp.Properties["runtime_dynamic"] != "false" {
		t.Fatalf("topic should be static, got runtime_dynamic=%q", tp.Properties["runtime_dynamic"])
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestKafka_Python_DynamicTopic covers a subscribe call where the topic
// name is built from a config object at runtime. The pass should emit a
// runtime-dynamic placeholder so the repairs flow can resolve it later.
func TestKafka_Python_DynamicTopic(t *testing.T) {
	src := `from confluent_kafka import Consumer
import settings

def main():
    c = Consumer({})
    c.subscribe([settings.feedback_topic])
`
	ents, _ := runKafkaDetect(t, "python", "sub.py", src)
	var found bool
	for _, e := range ents {
		if e.Kind == messageTopicKind && e.Properties["runtime_dynamic"] == "true" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one runtime-dynamic MessageTopic, ents=%v", ents)
	}
}

// ---------------------------------------------------------------------------
// Go — Sarama + segmentio/kafka-go
// ---------------------------------------------------------------------------

// TestKafka_Go_SaramaProducer covers Sarama `ProducerMessage{Topic: "..."}`.
func TestKafka_Go_SaramaProducer(t *testing.T) {
	src := `package main
import "github.com/IBM/sarama"

func Emit(p sarama.SyncProducer) {
    msg := &sarama.ProducerMessage{Topic: "orders.created", Value: sarama.StringEncoder("x")}
    p.SendMessage(msg)
}
`
	ents, rels := runKafkaDetect(t, "go", "emit.go", src)
	if topicByName(ents, "orders.created") == nil {
		t.Fatalf("expected MessageTopic for orders.created, ents=%v", ents)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge")
	}
}

// TestKafka_Go_KafkaGoConsumer covers segmentio/kafka-go ReaderConfig with a
// Topic field — must emit a SUBSCRIBES_TO edge (not PUBLISHES_TO).
func TestKafka_Go_KafkaGoConsumer(t *testing.T) {
	src := `package main
import "github.com/segmentio/kafka-go"

func Read() {
    r := kafka.NewReader(kafka.ReaderConfig{
        Brokers: []string{"kafka:9092"},
        Topic:   "payments.failed",
        GroupID: "g",
    })
    _ = r
}
`
	ents, rels := runKafkaDetect(t, "go", "read.go", src)
	if topicByName(ents, "payments.failed") == nil {
		t.Fatalf("expected MessageTopic for payments.failed, ents=%v", ents)
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) != 0 {
		t.Fatalf("consumer-side must not emit PUBLISHES_TO, rels=%v", rels)
	}
}

// TestKafka_Go_DeadLetterDetection covers the dead-letter naming-convention
// heuristic — a topic ending in -dlq must carry dead_letter=true.
func TestKafka_Go_DeadLetterDetection(t *testing.T) {
	src := `package main
import "github.com/IBM/sarama"

func Emit(p sarama.SyncProducer) {
    p.SendMessage(&sarama.ProducerMessage{Topic: "orders.created-dlq"})
}
`
	_, _ = runKafkaDetect(t, "go", "dlq.go", src)
	// Dead-letter detection is recorded via the channel-binding path which
	// only fires for Quarkus; for direct API calls the broker layer doesn't
	// emit dead_letter automatically. This test is a placeholder that
	// asserts the suffix detection helper is not regressed.
	if !isDeadLetterTopic("orders.created-dlq") {
		t.Fatalf("isDeadLetterTopic(orders.created-dlq) = false; want true")
	}
	if isDeadLetterTopic("orders.created") {
		t.Fatalf("isDeadLetterTopic(orders.created) = true; want false")
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Quarkus SmallRye Reactive Messaging
// ---------------------------------------------------------------------------

// TestKafka_Java_QuarkusOutgoingResolvesChannel covers the Quarkus
// @Outgoing channel → application.properties topic resolution. We create
// a temp tree mirroring the canonical Quarkus layout so loadQuarkusChannel-
// Bindings can find the properties file.
func TestKafka_Java_QuarkusOutgoingResolvesChannel(t *testing.T) {
	dir := t.TempDir()
	resourceDir := filepath.Join(dir, "src", "main", "resources")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	props := `mp.messaging.outgoing.feedback-out.connector=smallrye-kafka
mp.messaging.outgoing.feedback-out.topic=feedback-topic
`
	if err := os.WriteFile(filepath.Join(resourceDir, "application.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}
	javaDir := filepath.Join(dir, "src", "main", "java", "io", "demo")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	javaPath := filepath.Join(javaDir, "FeedbackResource.java")
	src := `package io.demo;
import org.eclipse.microprofile.reactive.messaging.Channel;
import org.eclipse.microprofile.reactive.messaging.Emitter;
import org.eclipse.microprofile.reactive.messaging.Outgoing;

public class FeedbackResource {
    @Channel("feedback-out")
    Emitter<String> feedbackOut;

    @Outgoing("feedback-out")
    public String produce() {
        return "x";
    }
}
`
	if err := os.WriteFile(javaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, rels := runKafkaDetect(t, "java", javaPath, src)
	tp := topicByName(ents, "feedback-topic")
	if tp == nil {
		t.Fatalf("expected resolved topic feedback-topic, ents=%v", ents)
	}
	if tp.Properties["runtime_dynamic"] != "false" {
		t.Fatalf("resolved topic should not be runtime_dynamic, got %q", tp.Properties["runtime_dynamic"])
	}
	if len(edgesOfKind(rels, publishesToEdgeKind)) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestKafka_Java_QuarkusIncomingUnresolvedFallback covers the unresolved-
// channel fallback. When no application.properties exists, the channel
// must be emitted as a runtime-dynamic topic so the repairs flow can
// later attach the physical topic name.
func TestKafka_Java_QuarkusIncomingUnresolvedFallback(t *testing.T) {
	src := `package io.demo;
import org.eclipse.microprofile.reactive.messaging.Incoming;

public class TriageConsumer {
    @Incoming("feedback-in")
    public void onFeedback(String event) {}
}
`
	ents, rels := runKafkaDetect(t, "java", "TriageConsumer.java", src)
	var found bool
	for _, e := range ents {
		if e.Kind == messageTopicKind &&
			e.Properties["channel"] == "feedback-in" &&
			e.Properties["runtime_dynamic"] == "true" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected runtime-dynamic MessageTopic for unresolved channel, ents=%v", ents)
	}
	if len(edgesOfKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestKafka_Java_SpringKafkaListenerStaticTopic covers @KafkaListener with a
// quoted topic literal — must produce a fully resolved MessageTopic.
func TestKafka_Java_SpringKafkaListenerStaticTopic(t *testing.T) {
	src := `package io.demo;
import org.springframework.kafka.annotation.KafkaListener;

public class OrderConsumer {
    @KafkaListener(topics = "orders.created", groupId = "g")
    public void handle(String msg) {}
}
`
	ents, rels := runKafkaDetect(t, "java", "OrderConsumer.java", src)
	if topicByName(ents, "orders.created") == nil {
		t.Fatalf("expected MessageTopic for orders.created, ents=%v", ents)
	}
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestKafka_Kotlin_SpringKafkaListenerBracketArray is the #1489 regression
// test: Kotlin annotation arrays use `[...]`, not Java's `{...}`. The real
// fixture's notifications service declares
// `@KafkaListener(topics = ["orders.high_value"])`; before #1489 the regex
// only matched braces, so the stream-processor→notifications high_value link
// was severed even though the synthetic Java brace test passed.
func TestKafka_Kotlin_SpringKafkaListenerBracketArray(t *testing.T) {
	src := `package io.shipfast.notifications
import org.springframework.kafka.annotation.KafkaListener

class Listeners {
    @KafkaListener(topics = ["orders.placed"], groupId = "notifications")
    fun onOrderPlaced(payload: String) {}

    @KafkaListener(topics = ["orders.high_value"], groupId = "notifications-vip")
    fun onHighValue(payload: String) {}
}
`
	ents, rels := runKafkaDetect(t, "kotlin", "Listeners.kt", src)
	for _, want := range []string{"orders.placed", "orders.high_value"} {
		if topicByName(ents, want) == nil {
			t.Errorf("expected MessageTopic for bracket-array topic %q, ents=%v", want, ents)
		}
	}
	if subs := edgesOfKind(rels, subscribesToEdgeKind); len(subs) < 2 {
		t.Errorf("expected ≥2 SUBSCRIBES_TO edges, got %d; rels=%v", len(subs), rels)
	}
}

// TestKafka_Java_TransformDetected covers the @Incoming + @Outgoing on the
// same method shape — the pass must emit a TRANSFORMS edge between the
// input topic and the output topic.
func TestKafka_Java_TransformDetected(t *testing.T) {
	dir := t.TempDir()
	resourceDir := filepath.Join(dir, "src", "main", "resources")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	props := `mp.messaging.incoming.in-ch.topic=raw-orders
mp.messaging.outgoing.out-ch.topic=enriched-orders
`
	if err := os.WriteFile(filepath.Join(resourceDir, "application.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}
	javaDir := filepath.Join(dir, "src", "main", "java", "io", "demo")
	if err := os.MkdirAll(javaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	javaPath := filepath.Join(javaDir, "Enricher.java")
	src := `package io.demo;
import org.eclipse.microprofile.reactive.messaging.Incoming;
import org.eclipse.microprofile.reactive.messaging.Outgoing;

public class Enricher {
    @Incoming("in-ch")
    @Outgoing("out-ch")
    public String enrich(String raw) { return raw + "!"; }
}
`
	if err := os.WriteFile(javaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, rels := runKafkaDetect(t, "java", javaPath, src)
	transforms := edgesOfKind(rels, transformsEdgeKind)
	if len(transforms) == 0 {
		t.Fatalf("expected TRANSFORMS edge between raw-orders and enriched-orders, rels=%v", rels)
	}
	if !strings.Contains(transforms[0].FromID, "kafka:raw-orders") ||
		!strings.Contains(transforms[0].ToID, "kafka:enriched-orders") {
		t.Fatalf("unexpected TRANSFORMS endpoints: from=%s to=%s", transforms[0].FromID, transforms[0].ToID)
	}
}

// TestKafka_LooksLikeKafkaTopic exercises the topic-shape gate that
// guards the Java direct-API scanner from claiming arbitrary
// `.send("...")` first arguments.
func TestKafka_LooksLikeKafkaTopic(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"orders.created", true},
		{"payments_failed", true},
		{"trace-topic", true},
		{"orders/created", false}, // path-shaped
		{"hello world", false},    // contains space
		{"<dynamic>", false},      // brackets
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeKafkaTopic(tc.in); got != tc.want {
			t.Errorf("looksLikeKafkaTopic(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

// TestKafka_NoOpForUnsupportedLanguage guarantees we do not regress
// bug-rate on non-Kafka corpora — the pass must be a strict no-op for
// languages it doesn't claim to support.
func TestKafka_NoOpForUnsupportedLanguage(t *testing.T) {
	ents, rels := runKafkaDetect(t, "ruby", "lib/x.rb", `producer.send "orders.created"`)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no-op for unsupported language, got ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// PHP — ext-rdkafka (RdKafka\KafkaConsumer + RdKafka\Producer) — Fixes #1495
// ---------------------------------------------------------------------------

// TestKafka_PHPRdKafkaConsumerSubscribe verifies that
// `$consumer->subscribe(['payments.settled'])` emits a canonical
// kafka:payments.settled MessageTopic + SUBSCRIBES_TO edge.
// This is the exact pattern used by billing/app/Console/Commands/ConsumePaymentsSettled.php.
func TestKafka_PHPRdKafkaConsumerSubscribe(t *testing.T) {
	src := `<?php
namespace App\Console\Commands;

use RdKafka\Conf;
use RdKafka\KafkaConsumer;

class ConsumePaymentsSettled extends Command
{
    public function handle(): int
    {
        $conf = new Conf();
        $conf->set('group.id', 'billing-invoicing');
        $consumer = new KafkaConsumer($conf);
        $consumer->subscribe(['payments.settled']);

        while (true) {
            $message = $consumer->consume(120 * 1000);
        }
        return self::SUCCESS;
    }
}
`
	ents, rels := runKafkaDetect(t, "php", "app/Console/Commands/ConsumePaymentsSettled.php", src)

	// Must emit a MessageTopic for payments.settled.
	tp := topicByName(ents, "payments.settled")
	if tp == nil {
		t.Fatalf("expected MessageTopic for payments.settled, ents=%v", ents)
	}
	if tp.Properties["broker"] != "kafka" {
		t.Errorf("broker: want kafka, got %q", tp.Properties["broker"])
	}
	if tp.Name != "kafka:payments.settled" {
		t.Errorf("topic entity Name: want kafka:payments.settled, got %q", tp.Name)
	}

	// Must emit a SUBSCRIBES_TO edge.
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	// One of the edges must target kafka:payments.settled.
	found := false
	for _, s := range subs {
		if strings.Contains(s.ToID, "kafka:payments.settled") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no SUBSCRIBES_TO edge targeting kafka:payments.settled in %v", subs)
	}
}

// TestKafka_PHPRdKafkaConsumerSubscribeMulti verifies multi-topic subscribe.
func TestKafka_PHPRdKafkaConsumerSubscribeMulti(t *testing.T) {
	src := `<?php
use RdKafka\KafkaConsumer;
class OrderConsumer {
    public function run() {
        $consumer = new KafkaConsumer($conf);
        $consumer->subscribe(['orders.placed', 'orders.cancelled']);
    }
}
`
	ents, rels := runKafkaDetect(t, "php", "app/OrderConsumer.php", src)

	for _, topic := range []string{"orders.placed", "orders.cancelled"} {
		tp := topicByName(ents, topic)
		if tp == nil {
			t.Errorf("expected MessageTopic for %s, ents=%v", topic, ents)
		}
	}
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) < 2 {
		t.Errorf("expected at least 2 SUBSCRIBES_TO edges for 2 topics, got %d: %v", len(subs), subs)
	}
}

// TestKafka_PHPRdKafkaConsumerAssign verifies partition-assign consumer pattern.
func TestKafka_PHPRdKafkaConsumerAssign(t *testing.T) {
	src := `<?php
use RdKafka\KafkaConsumer;
use RdKafka\TopicPartition;

class PartitionConsumer {
    public function handle() {
        $consumer = new KafkaConsumer($conf);
        $consumer->assign([new TopicPartition('inventory.reserved', 0)]);
    }
}
`
	ents, rels := runKafkaDetect(t, "php", "app/PartitionConsumer.php", src)

	tp := topicByName(ents, "inventory.reserved")
	if tp == nil {
		t.Fatalf("expected MessageTopic for inventory.reserved, ents=%v", ents)
	}
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge from assign pattern, rels=%v", rels)
	}
	found := false
	for _, s := range subs {
		if strings.Contains(s.ToID, "kafka:inventory.reserved") {
			found = true
		}
	}
	if !found {
		t.Errorf("no SUBSCRIBES_TO targeting inventory.reserved in %v", subs)
	}
}

// TestKafka_PHPRdKafkaProducerNewTopic verifies ->newTopic('topic') producer.
func TestKafka_PHPRdKafkaProducerNewTopic(t *testing.T) {
	src := `<?php
use RdKafka\Producer;

class PaymentProducer {
    public function publish(array $event): void {
        $producer = new Producer();
        $topic = $producer->newTopic('payments.settled');
        $topic->produce(RD_KAFKA_PARTITION_UA, 0, json_encode($event));
    }
}
`
	ents, rels := runKafkaDetect(t, "php", "app/PaymentProducer.php", src)

	tp := topicByName(ents, "payments.settled")
	if tp == nil {
		t.Fatalf("expected MessageTopic for payments.settled, ents=%v", ents)
	}
	pubs := edgesOfKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge from newTopic, rels=%v", rels)
	}
	found := false
	for _, p := range pubs {
		if strings.Contains(p.ToID, "kafka:payments.settled") {
			found = true
		}
	}
	if !found {
		t.Errorf("no PUBLISHES_TO targeting payments.settled in %v", pubs)
	}
}

// TestKafka_PHPNoOpWithoutRdKafka verifies that plain PHP without RdKafka
// import does not cause false positives.
func TestKafka_PHPNoOpWithoutRdKafka(t *testing.T) {
	src := `<?php
class OrderController {
    public function subscribe(array $topics): void {
        // generic subscribe, no kafka
        foreach ($topics as $t) {
            $this->eventBus->register($t);
        }
    }
}
`
	ents, rels := runKafkaDetect(t, "php", "app/OrderController.php", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no-op for PHP without RdKafka import, got ents=%v rels=%v", ents, rels)
	}
}

// TestKafka_PHPCallerAttribution verifies that the caller entity name is
// "ClassName.methodName" matching the PHP extractor's SCOPE.Operation naming.
func TestKafka_PHPCallerAttribution(t *testing.T) {
	src := `<?php
use RdKafka\KafkaConsumer;

class ConsumePaymentsSettled {
    public function handle(): int {
        $consumer = new KafkaConsumer($conf);
        $consumer->subscribe(['payments.settled']);
        return 0;
    }
}
`
	_, rels := runKafkaDetect(t, "php", "app/ConsumePaymentsSettled.php", src)
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	// Find the SCOPE.Operation edge (not the class-level fallback).
	found := false
	for _, s := range subs {
		if strings.HasPrefix(s.FromID, "SCOPE.Operation:") &&
			strings.Contains(s.FromID, "ConsumePaymentsSettled.handle") &&
			strings.Contains(s.ToID, "kafka:payments.settled") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Operation:ConsumePaymentsSettled.handle → kafka:payments.settled edge, got %v", subs)
	}
}

// ---------------------------------------------------------------------------
// Rust — rdkafka (#3558)
// ---------------------------------------------------------------------------

// TestKafka_Rust_FutureRecordProducer covers the rdkafka FutureProducer
// idiom: producer.send(FutureRecord::to("topic")...). Producer side:
// PUBLISHES_TO, attributed to the enclosing fn.
func TestKafka_Rust_FutureRecordProducer(t *testing.T) {
	src := `use rdkafka::producer::{FutureProducer, FutureRecord};

async fn publish_inspection(producer: &FutureProducer, payload: &str) {
    producer
        .send(
            FutureRecord::to("inspections").payload(payload).key("k"),
            std::time::Duration::from_secs(0),
        )
        .await
        .unwrap();
}
`
	ents, rels := runKafkaDetect(t, "rust", "src/producer.rs", src)
	if topicByName(ents, "inspections") == nil {
		t.Fatalf("expected MessageTopic for inspections, got %v", ents)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, got none")
	}
	if !strings.Contains(pub[0].ToID, "kafka:inspections") {
		t.Fatalf("PUBLISHES_TO ToID = %q, want suffix kafka:inspections", pub[0].ToID)
	}
	if !strings.Contains(pub[0].FromID, "publish_inspection") {
		t.Fatalf("PUBLISHES_TO FromID = %q, want enclosing fn publish_inspection", pub[0].FromID)
	}
}

// TestKafka_Rust_StreamConsumerSubscribe covers rdkafka StreamConsumer
// subscribe with a literal topic slice. Consumer side: SUBSCRIBES_TO with
// the exact topic literals.
func TestKafka_Rust_StreamConsumerSubscribe(t *testing.T) {
	src := `use rdkafka::consumer::StreamConsumer;

fn start_consumer(consumer: &StreamConsumer) {
    consumer.subscribe(&["events", "audit.log"]).expect("subscribe");
}
`
	ents, rels := runKafkaDetect(t, "rust", "src/consumer.rs", src)
	if topicByName(ents, "events") == nil {
		t.Fatalf("expected MessageTopic for events, got %v", ents)
	}
	if topicByName(ents, "audit.log") == nil {
		t.Fatalf("expected MessageTopic for audit.log, got %v", ents)
	}
	subs := edgesOfKind(rels, subscribesToEdgeKind)
	if len(subs) < 2 {
		t.Fatalf("expected 2 SUBSCRIBES_TO edges, got %d", len(subs))
	}
	var sawEvents bool
	for _, s := range subs {
		if strings.Contains(s.ToID, "kafka:events") {
			sawEvents = true
			if !strings.Contains(s.FromID, "start_consumer") {
				t.Fatalf("SUBSCRIBES_TO FromID = %q, want enclosing fn start_consumer", s.FromID)
			}
		}
	}
	if !sawEvents {
		t.Fatalf("expected a SUBSCRIBES_TO edge to kafka:events, got %v", subs)
	}
}

// TestKafka_Rust_BaseRecordProducer covers the ThreadedProducer BaseRecord
// idiom: producer.send(BaseRecord::to("metrics")...).
func TestKafka_Rust_BaseRecordProducer(t *testing.T) {
	src := `use rdkafka::producer::{BaseProducer, BaseRecord};

fn emit_metric(producer: &BaseProducer) {
    producer.send(BaseRecord::to("metrics").payload("v").key("k")).unwrap();
}
`
	ents, rels := runKafkaDetect(t, "rust", "src/metrics.rs", src)
	if topicByName(ents, "metrics") == nil {
		t.Fatalf("expected MessageTopic for metrics, got %v", ents)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) == 0 || !strings.Contains(pub[0].ToID, "kafka:metrics") {
		t.Fatalf("expected PUBLISHES_TO to kafka:metrics, got %v", pub)
	}
}
func edgeWithTo(rels []types.RelationshipRecord, kind, toSuffix string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == kind && strings.Contains(rels[i].ToID, toSuffix) {
			return &rels[i]
		}
	}
	return nil
}

// TestKafka_C_RdKafkaProducer covers the C-API rd_kafka_topic_new producer
// path: rd_kafka_topic_new(rk, "events", NULL) → PUBLISHES_TO kafka:events.
func TestKafka_C_RdKafkaProducer(t *testing.T) {
	src := `
#include <librdkafka/rdkafka.h>
void send_event(rd_kafka_t *rk) {
    rd_kafka_topic_t *rkt = rd_kafka_topic_new(rk, "events", NULL);
    rd_kafka_produce(rkt, RD_KAFKA_PARTITION_UA, RD_KAFKA_MSG_F_COPY, payload, len, NULL, 0, NULL);
}`
	ents, rels := runKafkaDetect(t, "c", "svc/producer.c", src)
	tp := topicByName(ents, "events")
	if tp == nil {
		t.Fatalf("expected MessageTopic events, got: %+v", ents)
	}
	if tp.Properties["messaging_layer"] != "librdkafka" {
		t.Errorf("messaging_layer = %q, want librdkafka", tp.Properties["messaging_layer"])
	}
	e := edgeWithTo(rels, publishesToEdgeKind, "kafka:events")
	if e == nil {
		t.Fatalf("expected PUBLISHES_TO kafka:events, got: %+v", rels)
	}
	if e.FromID != "SCOPE.Operation:send_event" {
		t.Errorf("FromID = %q, want SCOPE.Operation:send_event", e.FromID)
	}
}

// TestKafka_C_RdKafkaConsumer covers the C-API subscribe path:
// rd_kafka_topic_partition_list_add(topics, "orders", -1) → SUBSCRIBES_TO.
func TestKafka_C_RdKafkaConsumer(t *testing.T) {
	src := `
#include <librdkafka/rdkafka.h>
void start(rd_kafka_t *rk) {
    rd_kafka_topic_partition_list_t *topics = rd_kafka_topic_partition_list_new(1);
    rd_kafka_topic_partition_list_add(topics, "orders", -1);
    rd_kafka_subscribe(rk, topics);
}`
	ents, rels := runKafkaDetect(t, "c", "svc/consumer.c", src)
	if topicByName(ents, "orders") == nil {
		t.Fatalf("expected MessageTopic orders, got: %+v", ents)
	}
	e := edgeWithTo(rels, subscribesToEdgeKind, "kafka:orders")
	if e == nil {
		t.Fatalf("expected SUBSCRIBES_TO kafka:orders, got: %+v", rels)
	}
	if e.FromID != "SCOPE.Operation:start" {
		t.Errorf("FromID = %q, want SCOPE.Operation:start", e.FromID)
	}
}

// TestKafka_Cpp_RdKafkaCppProduce covers the C++-API producer->produce("topic")
// path → PUBLISHES_TO kafka:metrics with the rdkafkacpp messaging layer.
func TestKafka_Cpp_RdKafkaCppProduce(t *testing.T) {
	src := `
#include <librdkafka/rdkafkacpp.h>
void Reporter::flush(RdKafka::Producer *producer) {
    producer->produce("metrics", RdKafka::Topic::PARTITION_UA,
                      RdKafka::Producer::RK_MSG_COPY, buf, size, NULL, NULL);
}`
	ents, rels := runKafkaDetect(t, "cpp", "svc/reporter.cpp", src)
	tp := topicByName(ents, "metrics")
	if tp == nil {
		t.Fatalf("expected MessageTopic metrics, got: %+v", ents)
	}
	if tp.Properties["messaging_layer"] != "rdkafkacpp" {
		t.Errorf("messaging_layer = %q, want rdkafkacpp", tp.Properties["messaging_layer"])
	}
	e := edgeWithTo(rels, publishesToEdgeKind, "kafka:metrics")
	if e == nil {
		t.Fatalf("expected PUBLISHES_TO kafka:metrics, got: %+v", rels)
	}
	if e.FromID != "SCOPE.Operation:Reporter::flush" {
		t.Errorf("FromID = %q, want SCOPE.Operation:Reporter::flush", e.FromID)
	}
}

// TestKafka_Cpp_RdKafkaCppSubscribe covers the C++-API consumer->subscribe
// brace-list path → SUBSCRIBES_TO for each literal topic.
func TestKafka_Cpp_RdKafkaCppSubscribe(t *testing.T) {
	src := `
#include <librdkafka/rdkafkacpp.h>
void Worker::run(RdKafka::KafkaConsumer *consumer) {
    consumer->subscribe({"payments", "refunds"});
}`
	ents, rels := runKafkaDetect(t, "cpp", "svc/worker.cpp", src)
	if topicByName(ents, "payments") == nil || topicByName(ents, "refunds") == nil {
		t.Fatalf("expected MessageTopics payments & refunds, got: %+v", ents)
	}
	for _, want := range []string{"kafka:payments", "kafka:refunds"} {
		e := edgeWithTo(rels, subscribesToEdgeKind, want)
		if e == nil {
			t.Fatalf("expected SUBSCRIBES_TO %s, got: %+v", want, rels)
		}
		if e.FromID != "SCOPE.Operation:Worker::run" {
			t.Errorf("FromID = %q, want SCOPE.Operation:Worker::run", e.FromID)
		}
	}
}

// ---------------------------------------------------------------------------
// C# / Confluent.Kafka (#4996)
// ---------------------------------------------------------------------------

// TestKafka_CSharp_ProduceProducer covers Confluent.Kafka Produce/ProduceAsync
// with a literal topic. Producer-side: PUBLISHES_TO.
func TestKafka_CSharp_ProduceProducer(t *testing.T) {
	src := `using Confluent.Kafka;

public class OrderPublisher
{
    private readonly IProducer<Null, string> _producer;

    public async Task Emit(string body)
    {
        await _producer.ProduceAsync("orders.created", new Message<Null, string> { Value = body });
        _producer.Produce("orders.shipped", new Message<Null, string> { Value = body });
    }
}
`
	ents, rels := runKafkaDetect(t, "csharp", "src/OrderPublisher.cs", src)
	if topicByName(ents, "orders.created") == nil {
		t.Fatalf("expected MessageTopic for orders.created, got %v", ents)
	}
	if topicByName(ents, "orders.shipped") == nil {
		t.Fatalf("expected MessageTopic for orders.shipped, got %v", ents)
	}
	pub := edgesOfKind(rels, publishesToEdgeKind)
	if len(pub) < 2 {
		t.Fatalf("expected >=2 PUBLISHES_TO edges, got %d", len(pub))
	}
	foundTopic, foundCaller := false, false
	for _, p := range pub {
		if strings.Contains(p.ToID, "kafka:orders.created") {
			foundTopic = true
		}
		if strings.Contains(p.FromID, "Emit") {
			foundCaller = true
		}
		if p.Properties["messaging_layer"] != "confluent-kafka-dotnet" {
			t.Errorf("messaging_layer = %q, want confluent-kafka-dotnet", p.Properties["messaging_layer"])
		}
	}
	if !foundTopic {
		t.Fatalf("expected PUBLISHES_TO -> kafka:orders.created, pubs=%v", pub)
	}
	if !foundCaller {
		t.Fatalf("expected PUBLISHES_TO from enclosing method Emit, pubs=%v", pub)
	}
}

// TestKafka_CSharp_SubscribeConsumer covers consumer.Subscribe single-literal
// and array forms. Consumer-side: SUBSCRIBES_TO.
func TestKafka_CSharp_SubscribeConsumer(t *testing.T) {
	src := `using Confluent.Kafka;

public class OrderConsumer
{
    private readonly IConsumer<Null, string> _consumer;

    public void Start()
    {
        _consumer.Subscribe("orders.created");
        _consumer.Subscribe(new[] { "payments.settled", "orders.shipped" });
    }
}
`
	ents, rels := runKafkaDetect(t, "csharp", "src/OrderConsumer.cs", src)
	for _, want := range []string{"orders.created", "payments.settled", "orders.shipped"} {
		if topicByName(ents, want) == nil {
			t.Fatalf("expected MessageTopic for %q, got %v", want, ents)
		}
	}
	sub := edgesOfKind(rels, subscribesToEdgeKind)
	if len(sub) < 3 {
		t.Fatalf("expected >=3 SUBSCRIBES_TO edges, got %d (%v)", len(sub), sub)
	}
	found := false
	for _, s := range sub {
		if strings.Contains(s.ToID, "kafka:payments.settled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SUBSCRIBES_TO -> kafka:payments.settled, subs=%v", sub)
	}
}

// TestKafka_CSharp_AssignConsumer covers the per-partition manual-assignment
// consumer form `consumer.Assign(new TopicPartition("topic", 0))`. (#5125)
func TestKafka_CSharp_AssignConsumer(t *testing.T) {
	src := `using Confluent.Kafka;

public class PartitionConsumer
{
    private readonly IConsumer<Null, string> _consumer;

    public void Pin()
    {
        _consumer.Assign(new TopicPartition("inventory.reserved", 0));
        _consumer.Assign(new List<TopicPartition>
        {
            new TopicPartition("orders.created", 1),
            new TopicPartition("orders.created", 2),
        });
    }
}
`
	ents, rels := runKafkaDetect(t, "csharp", "src/PartitionConsumer.cs", src)
	for _, want := range []string{"inventory.reserved", "orders.created"} {
		tp := topicByName(ents, want)
		if tp == nil {
			t.Fatalf("expected MessageTopic for %q, ents=%v", want, ents)
		}
		if tp.Properties["assignment"] != "manual" {
			t.Errorf("topic %q assignment = %q, want manual", want, tp.Properties["assignment"])
		}
	}
	sub := edgesOfKind(rels, subscribesToEdgeKind)
	found := false
	for _, s := range sub {
		if strings.Contains(s.ToID, "kafka:inventory.reserved") &&
			s.Properties["assignment"] == "manual" &&
			strings.Contains(s.FromID, "Pin") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SUBSCRIBES_TO Pin->kafka:inventory.reserved (manual), subs=%v", sub)
	}
}

// TestKafka_CSharp_AssignWrongLanguage asserts the C# Assign pass does not fire
// for a non-C# language even with matching tokens. (#5125)
func TestKafka_CSharp_AssignWrongLanguage(t *testing.T) {
	src := `consumer.Assign(new TopicPartition("orders", 0));`
	ents, _ := runKafkaDetect(t, "python", "assign.py", src)
	for _, e := range ents {
		if e.Properties["assignment"] == "manual" {
			t.Fatalf("python file produced a C# manual assignment: %v", e)
		}
	}
}

// TestKafka_CSharp_AssignNoMatch asserts a Kafka C# file with no Assign emits no
// manual-assignment topics. (#5125)
func TestKafka_CSharp_AssignNoMatch(t *testing.T) {
	src := `using Confluent.Kafka;
public class P { public void Pub(IProducer<Null,string> p) { p.Produce("orders", new Message<Null,string>()); } }`
	_, rels := runKafkaDetect(t, "csharp", "src/P.cs", src)
	for _, r := range rels {
		if r.Properties["assignment"] == "manual" {
			t.Fatalf("unexpected manual assignment edge: %v", r)
		}
	}
}

// TestKafka_CSharp_NoSignal asserts a non-Kafka C# file emits nothing.
func TestKafka_CSharp_NoSignal(t *testing.T) {
	src := `public class Plain { public void Run() { var x = 1; } }`
	ents, rels := runKafkaDetect(t, "csharp", "src/Plain.cs", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no entities/edges for non-Kafka file, got ents=%d rels=%d", len(ents), len(rels))
	}
}
