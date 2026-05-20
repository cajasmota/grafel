// Tests for the Apache Pulsar producer/consumer detection pass (#936).
//
// Acceptance matrix:
//  1. Python client.create_producer(topic='persistent://public/default/orders') → PUBLISHES_TO
//  2. Java short-name consumer canonicalised to persistent://public/default/orders → SUBSCRIBES_TO
//  3. Cross-repo matching: producer entity ID == consumer entity ID for same topic
//  4. No false match against non-Pulsar create_producer (boto3 SQS)
//  5. No false match against Kafka .subscribe() calls in the same file
package engine

import (
	"strings"
	"testing"
)

// runPulsarDetect is a lightweight driver for the Pulsar pass.
func runPulsarDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	ents, rels := applyPulsarEdges(lang, path, []byte(src), nil, nil)
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

// pulsarTopicByCanonical returns the first SCOPE.MessageTopic entity whose
// Name matches pulsarTopicID(canonical).
func pulsarTopicByCanonical(ents []entityResult, canonical string) *entityResult {
	id := pulsarTopicID(canonical)
	for i := range ents {
		if ents[i].kind == pulsarTopicEntityKind && ents[i].name == id {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Topic normalisation unit tests
// ---------------------------------------------------------------------------

func TestNormalisePulsarTopic(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"persistent://acme/payments/orders", "persistent://acme/payments/orders"},
		{"non-persistent://acme/payments/orders", "non-persistent://acme/payments/orders"},
		{"public/default/orders", "persistent://public/default/orders"},
		{"orders", "persistent://public/default/orders"},
		{"", ""},
		{"  persistent://t/ns/q  ", "persistent://t/ns/q"},
	}
	for _, c := range cases {
		got := normalisePulsarTopic(c.raw)
		if got != c.want {
			t.Errorf("normalisePulsarTopic(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Python — pulsar-client
// ---------------------------------------------------------------------------

func TestPulsar_Py_CreateProducerFullURI(t *testing.T) {
	src := `
import pulsar

client = pulsar.Client('pulsar://localhost:6650')

def send_order(payload: bytes):
    producer = client.create_producer(topic='persistent://public/default/orders')
    producer.send(payload)
`
	ents, rels := runPulsarDetect(t, "python", "producer.py", src)

	canonical := "persistent://public/default/orders"
	e := pulsarTopicByCanonical(ents, canonical)
	if e == nil {
		t.Fatalf("expected SCOPE.MessageTopic for %q, got none", canonical)
	}
	if e.props["broker"] != "pulsar" {
		t.Errorf("broker = %q, want pulsar", e.props["broker"])
	}

	found := false
	for _, r := range rels {
		if r.kind == pulsarProducesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected PUBLISHES_TO edge to %q, got %+v", canonical, rels)
	}
}

func TestPulsar_Py_SubscribeFullURI(t *testing.T) {
	src := `
import pulsar

client = pulsar.Client('pulsar://localhost:6650')

def consume_orders():
    consumer = client.subscribe('persistent://public/default/orders', subscription_name='my-sub')
    while True:
        msg = consumer.receive()
        consumer.acknowledge(msg)
`
	ents, rels := runPulsarDetect(t, "python", "consumer.py", src)

	canonical := "persistent://public/default/orders"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Fatalf("expected topic entity for %q", canonical)
	}

	found := false
	for _, r := range rels {
		if r.kind == pulsarConsumesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SUBSCRIBES_TO edge to %q, got %+v", canonical, rels)
	}
}

func TestPulsar_Py_ShortNameNormalised(t *testing.T) {
	src := `
import pulsar
client = pulsar.Client('pulsar://localhost:6650')
producer = client.create_producer(topic='orders')
`
	ents, _ := runPulsarDetect(t, "python", "short.py", src)
	canonical := "persistent://public/default/orders"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Errorf("expected canonicalised topic %q, got none", canonical)
	}
}

// TestPulsar_Py_NoFalsePositive_Boto3 ensures boto3 SQS create_queue / send_message
// does NOT trigger a Pulsar match even though the call chain is superficially
// similar.
func TestPulsar_Py_NoFalsePositive_Boto3(t *testing.T) {
	src := `
import boto3

sqs = boto3.client('sqs', region_name='us-east-1')

def send_sqs(msg):
    url = sqs.create_queue(QueueName='orders')['QueueUrl']
    sqs.send_message(QueueUrl=url, MessageBody=msg)
`
	ents, rels := runPulsarDetect(t, "python", "sqs_producer.py", src)
	if len(ents) > 0 || len(rels) > 0 {
		t.Errorf("expected 0 entities/rels for non-Pulsar file, got %d/%d", len(ents), len(rels))
	}
}

// TestPulsar_Py_NoFalsePositive_KafkaSubscribe ensures a Kafka .subscribe()
// call in a Python file that does NOT import pulsar is not matched.
func TestPulsar_Py_NoFalsePositive_KafkaSubscribe(t *testing.T) {
	src := `
from confluent_kafka import Consumer

c = Consumer({'bootstrap.servers': 'localhost:9092', 'group.id': 'grp'})
c.subscribe(['orders'])
`
	ents, rels := runPulsarDetect(t, "python", "kafka_consumer.py", src)
	if len(ents) > 0 || len(rels) > 0 {
		t.Errorf("expected 0 entities/rels for non-Pulsar file, got %d/%d", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// Java — pulsar-client
// ---------------------------------------------------------------------------

func TestPulsar_Java_ProducerConsumerCrossRepoMatch(t *testing.T) {
	producerSrc := `
import org.apache.pulsar.client.api.PulsarClient;
import org.apache.pulsar.client.api.Producer;

public class OrderProducer {
    public void send(byte[] payload) throws Exception {
        PulsarClient client = PulsarClient.builder().serviceUrl("pulsar://localhost:6650").build();
        Producer<byte[]> producer = client.newProducer()
            .topic("persistent://public/default/orders")
            .create();
        producer.send(payload);
    }
}
`
	consumerSrc := `
import org.apache.pulsar.client.api.PulsarClient;
import org.apache.pulsar.client.api.Consumer;

public class OrderConsumer {
    public void consume() throws Exception {
        PulsarClient client = PulsarClient.builder().serviceUrl("pulsar://localhost:6650").build();
        Consumer<byte[]> consumer = client.newConsumer()
            .topic("orders")
            .subscriptionName("my-sub")
            .subscribe();
    }
}
`
	entsP, relsP := runPulsarDetect(t, "java", "OrderProducer.java", producerSrc)
	entsC, relsC := runPulsarDetect(t, "java", "OrderConsumer.java", consumerSrc)

	canonical := "persistent://public/default/orders"
	epID := pulsarTopicID(canonical)

	eP := pulsarTopicByCanonical(entsP, canonical)
	eC := pulsarTopicByCanonical(entsC, canonical)

	if eP == nil {
		t.Fatalf("producer: expected topic entity %q, got none", canonical)
	}
	if eC == nil {
		t.Fatalf("consumer: expected topic entity %q, got none (short-name canonicalisation failed)", canonical)
	}

	// Entity IDs must match (cross-repo join key).
	if eP.name != eC.name {
		t.Errorf("cross-repo mismatch: producer entity %q != consumer entity %q", eP.name, eC.name)
	}
	if eP.name != epID {
		t.Errorf("entity name = %q, want %q", eP.name, epID)
	}

	foundPub := false
	for _, r := range relsP {
		if r.kind == pulsarProducesEdge && strings.Contains(r.to, epID) {
			foundPub = true
		}
	}
	if !foundPub {
		t.Errorf("producer: expected PUBLISHES_TO edge, got %+v", relsP)
	}

	foundSub := false
	for _, r := range relsC {
		if r.kind == pulsarConsumesEdge && strings.Contains(r.to, epID) {
			foundSub = true
		}
	}
	if !foundSub {
		t.Errorf("consumer: expected SUBSCRIBES_TO edge, got %+v", relsC)
	}
}

// ---------------------------------------------------------------------------
// Go — pulsar-client-go
// ---------------------------------------------------------------------------

func TestPulsar_Go_ProducerOptions(t *testing.T) {
	src := `
package main

import (
	"github.com/apache/pulsar-client-go/pulsar"
)

func publishOrder(client pulsar.Client, payload []byte) error {
	producer, err := client.CreateProducer(pulsar.ProducerOptions{
		Topic: "persistent://acme/billing/invoices",
	})
	if err != nil {
		return err
	}
	_, err = producer.Send(ctx, &pulsar.ProducerMessage{Payload: payload})
	return err
}
`
	ents, rels := runPulsarDetect(t, "go", "producer.go", src)
	canonical := "persistent://acme/billing/invoices"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Fatalf("expected topic %q, got none", canonical)
	}
	found := false
	for _, r := range rels {
		if r.kind == pulsarProducesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PUBLISHES_TO, got %+v", rels)
	}
}

func TestPulsar_Go_ConsumerOptions(t *testing.T) {
	src := `
package main

import (
	"github.com/apache/pulsar-client-go/pulsar"
)

func consumeInvoices(client pulsar.Client) {
	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            "persistent://acme/billing/invoices",
		SubscriptionName: "invoice-consumer",
	})
	_ = consumer
	_ = err
}
`
	ents, rels := runPulsarDetect(t, "go", "consumer.go", src)
	canonical := "persistent://acme/billing/invoices"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Fatalf("expected topic %q, got none", canonical)
	}
	found := false
	for _, r := range rels {
		if r.kind == pulsarConsumesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SUBSCRIBES_TO, got %+v", rels)
	}
}

func TestPulsar_Go_NoFalsePositive_NonPulsarImport(t *testing.T) {
	src := `
package main

import "fmt"

func main() {
	opts := struct{ Topic string }{Topic: "orders"}
	fmt.Println(opts.Topic)
}
`
	ents, rels := runPulsarDetect(t, "go", "no_pulsar.go", src)
	if len(ents) > 0 || len(rels) > 0 {
		t.Errorf("expected 0 entities/rels, got %d/%d", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// Node / TypeScript — pulsar-client
// ---------------------------------------------------------------------------

func TestPulsar_Node_CreateProducer(t *testing.T) {
	src := `
const Pulsar = require('pulsar-client')

async function publishOrders() {
  const client = new Pulsar.Client({ serviceUrl: 'pulsar://localhost:6650' })
  const producer = await client.createProducer({ topic: 'persistent://public/default/orders' })
  await producer.send({ data: Buffer.from('hello') })
}
`
	ents, rels := runPulsarDetect(t, "javascript", "producer.js", src)
	canonical := "persistent://public/default/orders"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Fatalf("expected topic %q, got none", canonical)
	}
	found := false
	for _, r := range rels {
		if r.kind == pulsarProducesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PUBLISHES_TO, got %+v", rels)
	}
}

func TestPulsar_Node_Subscribe(t *testing.T) {
	src := `
import Pulsar from 'pulsar-client'

async function consumeOrders() {
  const client = new Pulsar.Client({ serviceUrl: 'pulsar://localhost:6650' })
  const consumer = await client.subscribe({
    topic: 'persistent://public/default/orders',
    subscription: 'my-sub',
  })
  const msg = await consumer.receive()
  await consumer.acknowledge(msg)
}
`
	ents, rels := runPulsarDetect(t, "typescript", "consumer.ts", src)
	canonical := "persistent://public/default/orders"
	if pulsarTopicByCanonical(ents, canonical) == nil {
		t.Fatalf("expected topic %q, got none", canonical)
	}
	found := false
	for _, r := range rels {
		if r.kind == pulsarConsumesEdge && strings.Contains(r.to, pulsarTopicID(canonical)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SUBSCRIBES_TO, got %+v", rels)
	}
}

func TestPulsar_Node_NoFalsePositive_NonPulsarImport(t *testing.T) {
	src := `
const kafka = require('kafkajs')
const producer = kafka.producer()
await producer.send({ topic: 'orders', messages: [{ value: 'hello' }] })
`
	ents, rels := runPulsarDetect(t, "javascript", "kafka_producer.js", src)
	if len(ents) > 0 || len(rels) > 0 {
		t.Errorf("expected 0 entities/rels for non-Pulsar file, got %d/%d", len(ents), len(rels))
	}
}
