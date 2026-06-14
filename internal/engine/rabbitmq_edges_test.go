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

// ---------------------------------------------------------------------------
// C# — RabbitMQ.Client (#4996)
// ---------------------------------------------------------------------------

// TestRabbitMQ_CSharp_BasicPublishNamed covers the named-argument
// channel.BasicPublish(exchange:, routingKey:, body:) producer form.
func TestRabbitMQ_CSharp_BasicPublishNamed(t *testing.T) {
	src := `using RabbitMQ.Client;

public class OrderPublisher
{
    public void Send(IModel channel, byte[] body)
    {
        channel.BasicPublish(exchange: "orders-exchange", routingKey: "orders.created", body: body);
    }
}
`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/OrderPublisher.cs", src)
	qID := rabbitmqQueueID("orders.created")
	q := queueByName(ents, qID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue %q, ents=%v", qID, ents)
	}
	if q.props["broker"] != "rabbitmq" {
		t.Errorf("queue broker = %q, want rabbitmq", q.props["broker"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	found := false
	for _, p := range pubs {
		if strings.Contains(p.to, qID) {
			found = true
			if p.props["exchange"] != "orders-exchange" {
				t.Errorf("exchange prop = %q, want orders-exchange", p.props["exchange"])
			}
			if p.props["messaging_layer"] != "rabbitmq-dotnet" {
				t.Errorf("messaging_layer = %q, want rabbitmq-dotnet", p.props["messaging_layer"])
			}
			if !strings.Contains(p.from, "Send") {
				t.Errorf("PUBLISHES_TO from = %q, want enclosing method Send", p.from)
			}
		}
	}
	if !found {
		t.Fatalf("expected PUBLISHES_TO -> %q, pubs=%v", qID, pubs)
	}
}

// TestRabbitMQ_CSharp_BasicPublishPositional covers the positional
// channel.BasicPublish("ex", "rk", ...) form.
func TestRabbitMQ_CSharp_BasicPublishPositional(t *testing.T) {
	src := `using RabbitMQ.Client;

public class Worker
{
    public void Emit(IModel channel, byte[] body)
    {
        channel.BasicPublish("ex", "tasks.queued", null, body);
    }
}
`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/Worker.cs", src)
	qID := rabbitmqQueueID("tasks.queued")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue %q, ents=%v", qID, ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 || !strings.Contains(pubs[0].to, qID) {
		t.Fatalf("expected PUBLISHES_TO -> %q, pubs=%v", qID, pubs)
	}
}

// TestRabbitMQ_CSharp_BasicConsume covers named + positional consumer forms.
func TestRabbitMQ_CSharp_BasicConsume(t *testing.T) {
	src := `using RabbitMQ.Client;

public class OrderConsumer
{
    public void Start(IModel channel, IBasicConsumer c)
    {
        channel.BasicConsume(queue: "orders.created", autoAck: true, consumer: c);
        channel.BasicConsume("payments.settled", true, c);
    }
}
`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/OrderConsumer.cs", src)
	for _, name := range []string{"orders.created", "payments.settled"} {
		if queueByName(ents, rabbitmqQueueID(name)) == nil {
			t.Fatalf("expected SCOPE.Queue for %q, ents=%v", name, ents)
		}
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) < 2 {
		t.Fatalf("expected >=2 SUBSCRIBES_TO edges, got %d (%v)", len(subs), subs)
	}
	found := false
	for _, s := range subs {
		if strings.Contains(s.to, rabbitmqQueueID("payments.settled")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SUBSCRIBES_TO -> rabbitmq:payments.settled, subs=%v", subs)
	}
}

// TestRabbitMQ_CSharp_QueueDeclare asserts a QueueDeclare emits a queue node
// even with no pub/sub call site.
func TestRabbitMQ_CSharp_QueueDeclare(t *testing.T) {
	src := `using RabbitMQ.Client;

public class Topology
{
    public void Setup(IModel channel)
    {
        channel.QueueDeclare(queue: "audit.events", durable: true, exclusive: false, autoDelete: false, arguments: null);
    }
}
`
	ents, _ := runRabbitMQDetect(t, "csharp", "src/Topology.cs", src)
	q := queueByName(ents, rabbitmqQueueID("audit.events"))
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for audit.events, ents=%v", ents)
	}
	if q.props["declared"] != "true" {
		t.Errorf("declared prop = %q, want true", q.props["declared"])
	}
}

// TestRabbitMQ_CSharp_NoSignal asserts a non-RabbitMQ C# file emits nothing.
func TestRabbitMQ_CSharp_NoSignal(t *testing.T) {
	src := `public class Plain { public void Run() { var x = 1; } }`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/Plain.cs", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected nothing for non-RabbitMQ file, got ents=%d rels=%d", len(ents), len(rels))
	}
}

// --- #5125: exchange topology + event-consumer handler attribution ---------

// exchangeByName finds a SCOPE.Queue node serving as an exchange (entity_role).
func exchangeByName(ents []entityResult, name string) *entityResult {
	for i := range ents {
		if ents[i].kind == queueEntityKind && ents[i].name == "rabbitmq:exchange:"+name &&
			ents[i].props["entity_role"] == "exchange" {
			return &ents[i]
		}
	}
	return nil
}

// TestRabbitMQ_CSharp_ExchangeDeclareAndBind covers ExchangeDeclare + QueueBind
// emitting an exchange node (with type) and a ROUTES_TO topology edge. (#5125)
func TestRabbitMQ_CSharp_ExchangeDeclareAndBind(t *testing.T) {
	src := `using RabbitMQ.Client;

public class Topology
{
    public void Setup(IModel channel)
    {
        channel.ExchangeDeclare(exchange: "events", type: "fanout");
        channel.QueueDeclare(queue: "audit", durable: true);
        channel.QueueBind(queue: "audit", exchange: "events", routingKey: "");
    }
}
`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/Topology.cs", src)
	ex := exchangeByName(ents, "events")
	if ex == nil {
		t.Fatalf("expected exchange node for 'events', ents=%v", ents)
	}
	if ex.props["exchange_type"] != "fanout" {
		t.Errorf("exchange_type = %q, want fanout", ex.props["exchange_type"])
	}
	routes := relsByKind(rels, "ROUTES_TO")
	found := false
	for _, r := range routes {
		if strings.Contains(r.from, "rabbitmq:exchange:events") &&
			strings.Contains(r.to, rabbitmqQueueID("audit")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ROUTES_TO events->audit, routes=%v", routes)
	}
}

// TestRabbitMQ_CSharp_ExchangeDeclarePositionalTopic covers the positional
// ExchangeDeclare("ex", ExchangeType.Topic, ...) form. (#5125)
func TestRabbitMQ_CSharp_ExchangeDeclarePositionalTopic(t *testing.T) {
	src := `using RabbitMQ.Client;
public class T {
    public void S(IModel ch) {
        ch.ExchangeDeclare("logs", ExchangeType.Topic, durable: true);
        ch.QueueBind("errors", "logs", "log.error");
    }
}`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/T.cs", src)
	ex := exchangeByName(ents, "logs")
	if ex == nil || ex.props["exchange_type"] != "topic" {
		t.Fatalf("expected topic exchange 'logs', ents=%v", ents)
	}
	routes := relsByKind(rels, "ROUTES_TO")
	found := false
	for _, r := range routes {
		if strings.Contains(r.to, rabbitmqQueueID("errors")) && r.props["routing_key"] == "log.error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ROUTES_TO logs->errors rk=log.error, routes=%v", routes)
	}
}

// TestRabbitMQ_CSharp_EventingReceivedMethodGroup covers handler attribution
// for the method-group form `consumer.Received += OnMessage;`. (#5125)
func TestRabbitMQ_CSharp_EventingReceivedMethodGroup(t *testing.T) {
	src := `using RabbitMQ.Client;
using RabbitMQ.Client.Events;

public class OrderConsumer
{
    public void Start(IModel channel)
    {
        var consumer = new EventingBasicConsumer(channel);
        consumer.Received += OnMessage;
        channel.BasicConsume(queue: "orders.created", autoAck: true, consumer: consumer);
    }

    private void OnMessage(object sender, BasicDeliverEventArgs ea) { }
}
`
	_, rels := runRabbitMQDetect(t, "csharp", "src/OrderConsumer.cs", src)
	qID := rabbitmqQueueID("orders.created")
	subs := relsByKind(rels, subscribesToEdgeKind)
	found := false
	for _, s := range subs {
		if strings.Contains(s.from, "OnMessage") && strings.Contains(s.to, qID) &&
			s.props["handler"] == "method_group" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SUBSCRIBES_TO OnMessage->%s (method_group), subs=%v", qID, subs)
	}
}

// TestRabbitMQ_CSharp_EventingReceivedInlineLambdaNoExtraEdge asserts the
// inline-lambda handler does NOT produce a duplicate handler edge — its body
// is already covered by the BasicConsume edge on the enclosing method. (#5125)
func TestRabbitMQ_CSharp_EventingReceivedInlineLambdaNoExtraEdge(t *testing.T) {
	src := `using RabbitMQ.Client;
public class C {
    public void Listen(IModel channel) {
        var consumer = new EventingBasicConsumer(channel);
        consumer.Received += (model, ea) => { Handle(ea); };
        channel.BasicConsume("jobs.pending", true, consumer);
    }
}`
	_, rels := runRabbitMQDetect(t, "csharp", "src/C.cs", src)
	subs := relsByKind(rels, subscribesToEdgeKind)
	// Exactly one SUBSCRIBES_TO (the BasicConsume edge on Listen); no method_group.
	for _, s := range subs {
		if s.props["handler"] == "method_group" {
			t.Fatalf("inline lambda wrongly produced a method_group edge: %v", s)
		}
	}
	if len(subs) != 1 || !strings.Contains(subs[0].from, "Listen") {
		t.Fatalf("expected exactly one SUBSCRIBES_TO from Listen, subs=%v", subs)
	}
}

// TestRabbitMQ_CSharp_Topology_WrongLanguage asserts the C# topology pass does
// not fire for a non-C# language even with matching tokens. (#5125)
func TestRabbitMQ_CSharp_Topology_WrongLanguage(t *testing.T) {
	src := `ch.ExchangeDeclare("events", "fanout"); ch.QueueBind("a", "events", "");`
	ents, rels := runRabbitMQDetect(t, "go", "topology.go", src)
	for _, e := range ents {
		if e.props["entity_role"] == "exchange" {
			t.Fatalf("go file produced a C# exchange node: %v", e)
		}
	}
	_ = rels
}

// TestRabbitMQ_CSharp_Topology_NoMatch asserts a RabbitMQ C# file with no
// exchange/bind/handler emits no topology nodes/edges. (#5125)
func TestRabbitMQ_CSharp_Topology_NoMatch(t *testing.T) {
	src := `using RabbitMQ.Client;
public class P { public void Pub(IModel ch, byte[] b) { ch.BasicPublish("", "q", null, b); } }`
	ents, rels := runRabbitMQDetect(t, "csharp", "src/P.cs", src)
	for _, e := range ents {
		if e.props["entity_role"] == "exchange" {
			t.Fatalf("unexpected exchange node: %v", e)
		}
	}
	for _, r := range rels {
		if r.kind == "ROUTES_TO" {
			t.Fatalf("unexpected ROUTES_TO edge: %v", r)
		}
	}
}
