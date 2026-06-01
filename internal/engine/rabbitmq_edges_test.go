// Tests for the RabbitMQ producer/consumer detection pass added by #726 wave 2.
//
// Each language has at minimum:
//   - Static queue name on the producer side (emits SCOPE.Queue + PUBLISHES_TO).
//   - Static queue name on the consumer side (emits SCOPE.Queue + SUBSCRIBES_TO).
//   - Queue declare / assertQueue emits entity without a direction edge.
//   - Beyond-minimum: exchange→routing_key binding recorded as edge property.
package engine

import (
	"strings"
	"testing"
)

// runRabbitMQDetect is a lightweight in-process driver for the RabbitMQ pass.
func runRabbitMQDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyRabbitMQEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
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

type entityResult struct {
	kind  string
	name  string
	props map[string]string
}

type relResult struct {
	from  string
	to    string
	kind  string
	props map[string]string
}

func queueByName(ents []entityResult, queueID string) *entityResult {
	for i := range ents {
		if ents[i].kind == queueEntityKind && ents[i].name == queueID {
			return &ents[i]
		}
	}
	return nil
}

func relsByKind(rels []relResult, kind string) []relResult {
	var out []relResult
	for _, r := range rels {
		if r.kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — pika
// ---------------------------------------------------------------------------

// TestRabbitMQ_Python_PikaPublishKeyword covers the pika
// channel.basic_publish(exchange=X, routing_key=Y, body=Z) keyword form.
func TestRabbitMQ_Python_PikaPublishKeyword(t *testing.T) {
	src := `import pika

def send_order(channel):
    channel.basic_publish(
        exchange='orders-exchange',
        routing_key='orders.created',
        body=b'hello',
    )
`
	ents, rels := runRabbitMQDetect(t, "python", "send.py", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, got %v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, got none")
	}
	if !strings.Contains(pubs[0].to, qID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, qID)
	}
	if pubs[0].props["exchange"] != "orders-exchange" {
		t.Fatalf("exchange property = %q, want orders-exchange", pubs[0].props["exchange"])
	}
}

// TestRabbitMQ_Python_PikaConsume covers channel.basic_consume(queue=name, ...).
func TestRabbitMQ_Python_PikaConsume(t *testing.T) {
	src := `import pika

def start_consumer(channel):
    channel.basic_consume(queue='orders.created', on_message_callback=callback, auto_ack=True)
    channel.start_consuming()
`
	ents, rels := runRabbitMQDetect(t, "python", "consume.py", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestRabbitMQ_Python_PikaQueueDeclare covers channel.queue_declare(queue=name).
// Queue declare should emit a SCOPE.Queue entity but NO direction edge.
func TestRabbitMQ_Python_PikaQueueDeclare(t *testing.T) {
	src := `import pika

def setup(channel):
    channel.queue_declare(queue='task-queue', durable=True)
`
	ents, rels := runRabbitMQDetect(t, "python", "setup.py", src)
	qID := rabbitmqQueueID("task-queue")
	q := queueByName(ents, qID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for task-queue, ents=%v", ents)
	}
	if q.props["declared"] != "true" {
		t.Fatalf("declared property = %q, want true", q.props["declared"])
	}
	// queue_declare alone should not emit direction edges
	if len(relsByKind(rels, publishesToEdgeKind))+len(relsByKind(rels, subscribesToEdgeKind)) != 0 {
		t.Fatalf("queue_declare should not emit PUBLISHES_TO/SUBSCRIBES_TO, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Node — amqplib
// ---------------------------------------------------------------------------

// TestRabbitMQ_Node_AmqplibPublish covers channel.publish(exchange, routingKey, content).
func TestRabbitMQ_Node_AmqplibPublish(t *testing.T) {
	src := `const amqp = require('amqplib');

async function send() {
  const conn = await amqp.connect('amqp://localhost');
  const ch = await conn.createChannel();
  ch.publish('logs', 'orders.created', Buffer.from('hello'));
}
`
	ents, rels := runRabbitMQDetect(t, "javascript", "send.js", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestRabbitMQ_Node_AmqplibConsume covers channel.consume(queue, handler).
func TestRabbitMQ_Node_AmqplibConsume(t *testing.T) {
	src := `const amqp = require('amqplib');

async function listen() {
  const conn = await amqp.connect('amqp://localhost');
  const ch = await conn.createChannel();
  await ch.assertQueue('task-queue');
  ch.consume('task-queue', (msg) => { console.log(msg.content.toString()); });
}
`
	ents, rels := runRabbitMQDetect(t, "javascript", "listen.js", src)
	qID := rabbitmqQueueID("task-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for task-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestRabbitMQ_Node_AssertQueue covers channel.assertQueue(name) emitting
// a SCOPE.Queue entity without a direction edge.
func TestRabbitMQ_Node_AssertQueue(t *testing.T) {
	src := `const amqp = require('amqplib');
async function setup(ch) {
  await ch.assertQueue('work-queue', { durable: true });
}
`
	ents, rels := runRabbitMQDetect(t, "javascript", "setup.js", src)
	qID := rabbitmqQueueID("work-queue")
	q := queueByName(ents, qID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for work-queue, ents=%v", ents)
	}
	if q.props["declared"] != "true" {
		t.Fatalf("declared property = %q, want true", q.props["declared"])
	}
	// assertQueue alone should not emit direction edges
	if len(relsByKind(rels, publishesToEdgeKind))+len(relsByKind(rels, subscribesToEdgeKind)) != 0 {
		t.Fatalf("assertQueue should not emit direction edges, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Java — Spring AMQP + direct RabbitMQ client
// ---------------------------------------------------------------------------

// TestRabbitMQ_Java_SpringRabbitListener covers @RabbitListener(queues = "name").
func TestRabbitMQ_Java_SpringRabbitListener(t *testing.T) {
	src := `package io.demo;
import org.springframework.amqp.rabbit.annotation.RabbitListener;

public class OrderConsumer {
    @RabbitListener(queues = "orders.created")
    public void handleOrder(String msg) {}
}
`
	ents, rels := runRabbitMQDetect(t, "java", "OrderConsumer.java", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestRabbitMQ_Java_RabbitTemplateSend covers rabbitTemplate.convertAndSend(exchange, routingKey, msg).
func TestRabbitMQ_Java_RabbitTemplateSend(t *testing.T) {
	src := `package io.demo;
import org.springframework.amqp.rabbit.core.RabbitTemplate;

public class OrderPublisher {
    private final RabbitTemplate rabbitTemplate;

    public void publishOrder(String order) {
        rabbitTemplate.convertAndSend("orders-exchange", "orders.created", order);
    }
}
`
	ents, rels := runRabbitMQDetect(t, "java", "OrderPublisher.java", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["exchange"] != "orders-exchange" {
		t.Fatalf("exchange property = %q, want orders-exchange", pubs[0].props["exchange"])
	}
}

// ---------------------------------------------------------------------------
// Go — amqp091-go
// ---------------------------------------------------------------------------

// TestRabbitMQ_Go_Publish covers channel.Publish(exchange, routingKey, ...).
func TestRabbitMQ_Go_Publish(t *testing.T) {
	src := `package main

import amqp "github.com/rabbitmq/amqp091-go"

func sendOrder(ch *amqp.Channel) error {
    return ch.Publish("orders-exchange", "orders.created", false, false, amqp.Publishing{
        Body: []byte("hello"),
    })
}
`
	ents, rels := runRabbitMQDetect(t, "go", "send.go", src)
	qID := rabbitmqQueueID("orders.created")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["routing_key"] != "orders.created" {
		t.Fatalf("routing_key = %q, want orders.created", pubs[0].props["routing_key"])
	}
}

// TestRabbitMQ_Go_Consume covers channel.Consume(queue, ...).
func TestRabbitMQ_Go_Consume(t *testing.T) {
	src := `package main

import amqp "github.com/rabbitmq/amqp091-go"

func startConsumer(ch *amqp.Channel) {
    msgs, _ := ch.Consume("task-queue", "", true, false, false, false, nil)
    for d := range msgs {
        _ = d
    }
}
`
	ents, rels := runRabbitMQDetect(t, "go", "consumer.go", src)
	qID := rabbitmqQueueID("task-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for task-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

// TestRabbitMQ_NoOpForUnsupportedLanguage ensures the pass is a strict
// no-op for languages it doesn't claim to support.
func TestRabbitMQ_NoOpForUnsupportedLanguage(t *testing.T) {
	ents, rels := runRabbitMQDetect(t, "ruby", "lib/x.rb", `channel.basic_publish("q", "x")`)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no-op for unsupported language, got ents=%v rels=%v", ents, rels)
	}
}

// TestRabbitMQ_LooksLikeQueueName exercises the queue-name gate.
func TestRabbitMQ_LooksLikeQueueName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"orders.created", true},
		{"task-queue", true},
		{"orders/created", true}, // RabbitMQ allows slashes
		{"hello world", false},   // space
		{"<dynamic>", false},     // angle brackets
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeQueueName(tc.in); got != tc.want {
			t.Errorf("looksLikeQueueName(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

// #1638 — aio-pika async RabbitMQ producer/consumer detection.
func TestRabbitMQ_Python_AioPikaPublishConstRoutingKey(t *testing.T) {
	src := `import aio_pika

LOAD_QUEUE = "etl-load-queue"

async def publish_aggregate(channel, aggregate):
    await channel.default_exchange.publish(
        aio_pika.Message(body=b"x"),
        routing_key=LOAD_QUEUE,
    )
`
	ents, rels := runRabbitMQDetect(t, "python", "stage5.py", src)
	qID := rabbitmqQueueID("etl-load-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for etl-load-queue, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 || !strings.Contains(pubs[0].to, qID) {
		t.Fatalf("expected aio-pika PUBLISHES_TO to %q, got %v", qID, pubs)
	}
	if pubs[0].props["messaging_layer"] != "aio-pika" {
		t.Fatalf("messaging_layer = %q, want aio-pika", pubs[0].props["messaging_layer"])
	}
}

func TestRabbitMQ_Python_AioPikaDeclareAndConsume(t *testing.T) {
	src := `import aio_pika

LOAD_QUEUE = "etl-load-queue"

async def consume(connection):
    channel = await connection.channel()
    queue = await channel.declare_queue(LOAD_QUEUE, durable=True)
    await queue.consume(on_message)
`
	ents, rels := runRabbitMQDetect(t, "python", "stage6.py", src)
	qID := rabbitmqQueueID("etl-load-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for etl-load-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 || !strings.Contains(subs[0].to, qID) {
		t.Fatalf("expected aio-pika SUBSCRIBES_TO to %q, got %v", qID, subs)
	}
}

// ---------------------------------------------------------------------------
// Rust — lapin (#3558)
// ---------------------------------------------------------------------------

// TestRabbitMQ_Rust_LapinPublish covers lapin
// channel.basic_publish("exchange", "routing_key", ...). Producer side:
// PUBLISHES_TO keyed on the routing key, attributed to the enclosing fn.
func TestRabbitMQ_Rust_LapinPublish(t *testing.T) {
	src := `use lapin::{Channel, BasicProperties, options::BasicPublishOptions};

async fn send_event(channel: &Channel, payload: &[u8]) {
    channel
        .basic_publish(
            "events-exchange",
            "events.created",
            BasicPublishOptions::default(),
            payload,
            BasicProperties::default(),
        )
        .await
        .unwrap();
}
`
	ents, rels := runRabbitMQDetect(t, "rust", "src/publisher.rs", src)
	qID := rabbitmqQueueID("events.created")
	q := queueByName(ents, qID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for events.created, got %v", ents)
	}
	if q.props["exchange"] != "events-exchange" {
		t.Errorf("expected exchange=events-exchange, got %q", q.props["exchange"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, got none")
	}
	if !strings.Contains(pubs[0].to, qID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, qID)
	}
	if pubs[0].props["routing_key"] != "events.created" {
		t.Errorf("expected routing_key=events.created, got %q", pubs[0].props["routing_key"])
	}
	if !strings.Contains(pubs[0].from, "send_event") {
		t.Errorf("PUBLISHES_TO FromID = %q, want enclosing fn send_event", pubs[0].from)
	}
}

// TestRabbitMQ_Rust_LapinConsume covers lapin
// channel.basic_consume("queue", ...). Consumer side: SUBSCRIBES_TO.
func TestRabbitMQ_Rust_LapinConsume(t *testing.T) {
	src := `use lapin::{Channel, options::BasicConsumeOptions, types::FieldTable};

async fn consume_jobs(channel: &Channel) {
    let _consumer = channel
        .basic_consume(
            "jobs.pending",
            "worker-1",
            BasicConsumeOptions::default(),
            FieldTable::default(),
        )
        .await
        .unwrap();
}
`
	ents, rels := runRabbitMQDetect(t, "rust", "src/worker.rs", src)
	qID := rabbitmqQueueID("jobs.pending")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for jobs.pending, got %v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, got none")
	}
	if !strings.Contains(subs[0].to, qID) {
		t.Fatalf("SUBSCRIBES_TO ToID = %q, want to contain %q", subs[0].to, qID)
	}
	if !strings.Contains(subs[0].from, "consume_jobs") {
		t.Errorf("SUBSCRIBES_TO FromID = %q, want enclosing fn consume_jobs", subs[0].from)
	}
}

// TestRabbitMQ_Rust_LapinQueueDeclare covers lapin queue_declare("queue", ...)
// — records the queue node even without a pub/sub call in the same file.
func TestRabbitMQ_Rust_LapinQueueDeclare(t *testing.T) {
	src := `use lapin::{Channel, options::QueueDeclareOptions, types::FieldTable};

async fn setup(channel: &Channel) {
    channel
        .queue_declare("notifications", QueueDeclareOptions::default(), FieldTable::default())
        .await
        .unwrap();
}
`
	ents, _ := runRabbitMQDetect(t, "rust", "src/setup.rs", src)
	q := queueByName(ents, rabbitmqQueueID("notifications"))
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for notifications, got %v", ents)
	}
	if q.props["declared"] != "true" {
		t.Errorf("expected declared=true, got %q", q.props["declared"])
	}
}
