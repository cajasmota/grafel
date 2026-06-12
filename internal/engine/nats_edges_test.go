// Tests for the NATS producer/consumer detection pass added by #726 wave 3.
//
// Coverage per language:
//   - Go: Publish, Subscribe, QueueSubscribe (with queue group), Request (request/reply),
//     JetStream Publish/Subscribe, NATS Streaming Publish/Subscribe
//   - Node: nc.publish, nc.subscribe, nc.request (request/reply),
//     js.publish/subscribe (JetStream)
//   - Python: nc.publish, nc.subscribe, nc.request, js.publish/subscribe
//   - Java: connection.publish, connection.subscribe, dispatcher.subscribe,
//     connection.request, js.publish/subscribe
//   - Wildcard subjects (orders.*, inventory.>) are valid
//   - No-signal guard (non-NATS file produces no output)
package engine

import (
	"strings"
	"testing"
)

// runNATSDetect is a lightweight in-process driver for the NATS pass.
func runNATSDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyNATSEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
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

// natsSubjectByID returns the first SCOPE.Queue entity with the given ID.
func natsSubjectByID(ents []entityResult, subjectID string) *entityResult {
	for i := range ents {
		if ents[i].kind == queueEntityKind && ents[i].name == subjectID {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Go — nats.go
// ---------------------------------------------------------------------------

// TestNATS_Go_Publish covers nc.Publish(subject, data).
func TestNATS_Go_Publish(t *testing.T) {
	src := `package orders

import "github.com/nats-io/nats.go"

func PublishOrder(nc *nats.Conn, data []byte) error {
	return nc.Publish("orders.created", data)
}
`
	ents, rels := runNATSDetect(t, "go", "orders.go", src)
	subID := natsSubjectID("orders.created")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pubs[0].to, subID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, subID)
	}
	if pubs[0].props["broker"] != "nats" {
		t.Fatalf("broker = %q, want nats", pubs[0].props["broker"])
	}
}

// TestNATS_Go_Subscribe covers nc.Subscribe(subject, handler).
func TestNATS_Go_Subscribe(t *testing.T) {
	src := `package consumer

import "github.com/nats-io/nats.go"

func StartConsumer(nc *nats.Conn) {
	nc.Subscribe("orders.created", func(msg *nats.Msg) {
		processOrder(msg.Data)
	})
}
`
	ents, rels := runNATSDetect(t, "go", "consumer.go", src)
	subID := natsSubjectID("orders.created")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Go_QueueSubscribe covers nc.QueueSubscribe with queue group.
func TestNATS_Go_QueueSubscribe(t *testing.T) {
	src := `package worker

import "github.com/nats-io/nats.go"

func StartWorker(nc *nats.Conn) {
	nc.QueueSubscribe("jobs.process", "worker-pool", func(msg *nats.Msg) {
		handleJob(msg.Data)
	})
}
`
	ents, rels := runNATSDetect(t, "go", "worker.go", src)
	subID := natsSubjectID("jobs.process")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for jobs.process, ents=%v", ents)
	}
	if q.props["queue_group"] != "worker-pool" {
		t.Fatalf("queue_group = %q, want worker-pool", q.props["queue_group"])
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if subs[0].props["queue_group"] != "worker-pool" {
		t.Fatalf("edge queue_group = %q, want worker-pool", subs[0].props["queue_group"])
	}
}

// TestNATS_Go_Request covers nc.Request — request/reply pattern.
func TestNATS_Go_Request(t *testing.T) {
	src := `package rpc

import (
	"time"
	"github.com/nats-io/nats.go"
)

func GetInventory(nc *nats.Conn, item string) ([]byte, error) {
	msg, err := nc.Request("inventory.query", []byte(item), 5*time.Second)
	return msg.Data, err
}
`
	ents, rels := runNATSDetect(t, "go", "rpc.go", src)
	subID := natsSubjectID("inventory.query")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for inventory.query, ents=%v", ents)
	}
	if q.props["pattern"] != "request_reply" {
		t.Fatalf("pattern = %q, want request_reply", q.props["pattern"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge for Request, rels=%v", rels)
	}
	if pubs[0].props["pattern"] != "request_reply" {
		t.Fatalf("edge pattern = %q, want request_reply", pubs[0].props["pattern"])
	}
}

// TestNATS_Go_JetStreamPublish covers js.Publish with jetstream=true property.
func TestNATS_Go_JetStreamPublish(t *testing.T) {
	src := `package jetstream

import "github.com/nats-io/nats.go"

func PublishEvent(nc *nats.Conn, data []byte) {
	js, _ := nc.JetStream()
	js.Publish("events.user.created", data)
}
`
	ents, rels := runNATSDetect(t, "go", "js_pub.go", src)
	subID := natsSubjectID("events.user.created")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for events.user.created, ents=%v", ents)
	}
	if q.props["jetstream"] != "true" {
		t.Fatalf("jetstream = %q, want true", q.props["jetstream"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["jetstream"] != "true" {
		t.Fatalf("edge jetstream = %q, want true", pubs[0].props["jetstream"])
	}
}

// TestNATS_Go_JetStreamSubscribe covers js.Subscribe with jetstream=true property.
func TestNATS_Go_JetStreamSubscribe(t *testing.T) {
	src := `package jetstream

import "github.com/nats-io/nats.go"

func ConsumeEvents(nc *nats.Conn) {
	js, _ := nc.JetStream()
	js.Subscribe("events.user.*", func(msg *nats.Msg) {
		handleEvent(msg.Data)
		msg.Ack()
	})
}
`
	ents, rels := runNATSDetect(t, "go", "js_sub.go", src)
	subID := natsSubjectID("events.user.*")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for events.user.*, ents=%v", ents)
	}
	if q.props["jetstream"] != "true" {
		t.Fatalf("jetstream = %q, want true", q.props["jetstream"])
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Go_NATSStreaming covers NATS Streaming (STAN) with nats_streaming property.
func TestNATS_Go_NATSStreaming(t *testing.T) {
	src := `package legacy

import stan "github.com/nats-io/stan.go"

func PublishLegacy(sc stan.Conn, data []byte) {
	stan.Publish("legacy-orders", data)
}

func SubscribeLegacy(sc stan.Conn) {
	sc.Subscribe("legacy-orders", func(msg *stan.Msg) {
		process(msg.Data)
	})
}
`
	ents, rels := runNATSDetect(t, "go", "legacy.go", src)
	subID := natsSubjectID("legacy-orders")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for legacy-orders, ents=%v", ents)
	}
	if q.props["nats_streaming"] != "true" {
		t.Fatalf("nats_streaming = %q, want true", q.props["nats_streaming"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Node — nats.js
// ---------------------------------------------------------------------------

// TestNATS_Node_Publish covers nc.publish(subject, data).
func TestNATS_Node_Publish(t *testing.T) {
	src := `import { connect, StringCodec } from 'nats';

async function publishOrder(data) {
  const nc = await connect({ servers: 'nats://localhost:4222' });
  const sc = StringCodec();
  nc.publish('orders.new', sc.encode(data));
  await nc.drain();
}
`
	ents, rels := runNATSDetect(t, "typescript", "pub.ts", src)
	subID := natsSubjectID("orders.new")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.new, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Node_Subscribe covers nc.subscribe(subject) for-await pattern.
func TestNATS_Node_Subscribe(t *testing.T) {
	src := `import { connect } from 'nats';

async function listenForOrders() {
  const nc = await connect({ servers: 'nats://localhost:4222' });
  const sub = nc.subscribe('orders.new');
  for await (const msg of sub) {
    handleOrder(msg.data);
  }
}
`
	ents, rels := runNATSDetect(t, "javascript", "sub.js", src)
	subID := natsSubjectID("orders.new")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.new, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Node_Request covers nc.request — request/reply.
func TestNATS_Node_Request(t *testing.T) {
	src := `import { connect, StringCodec } from 'nats';

async function queryService(subject, payload) {
  const nc = await connect({ servers: 'nats://localhost:4222' });
  const sc = StringCodec();
  const msg = await nc.request('service.query', sc.encode(payload), { timeout: 1000 });
  return sc.decode(msg.data);
}
`
	ents, _ := runNATSDetect(t, "typescript", "rpc.ts", src)
	subID := natsSubjectID("service.query")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for service.query, ents=%v", ents)
	}
	if q.props["pattern"] != "request_reply" {
		t.Fatalf("pattern = %q, want request_reply", q.props["pattern"])
	}
}

// TestNATS_Node_JetStreamPublish covers js.publish(subject, data).
func TestNATS_Node_JetStreamPublish(t *testing.T) {
	src := `import { connect } from 'nats';

async function publishEvent() {
  const nc = await connect({ servers: 'nats://localhost:4222' });
  const js = nc.jetstream();
  await js.publish('events.payment.processed', payload);
}
`
	ents, _ := runNATSDetect(t, "javascript", "js_pub.js", src)
	subID := natsSubjectID("events.payment.processed")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for events.payment.processed, ents=%v", ents)
	}
	if q.props["jetstream"] != "true" {
		t.Fatalf("jetstream = %q, want true", q.props["jetstream"])
	}
}

// ---------------------------------------------------------------------------
// Python — nats.py
// ---------------------------------------------------------------------------

// TestNATS_Python_Publish covers await nc.publish(subject, payload).
func TestNATS_Python_Publish(t *testing.T) {
	src := `import asyncio
import nats

async def publish_message():
    nc = await nats.connect("nats://localhost:4222")
    await nc.publish("notifications.email", b"Hello!")
    await nc.close()
`
	ents, rels := runNATSDetect(t, "python", "pub.py", src)
	subID := natsSubjectID("notifications.email")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for notifications.email, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Python_Subscribe covers await nc.subscribe(subject, cb=handler).
func TestNATS_Python_Subscribe(t *testing.T) {
	src := `import asyncio
import nats

async def subscribe():
    nc = await nats.connect("nats://localhost:4222")
    await nc.subscribe("notifications.email", cb=handle_message)
    await asyncio.sleep(10)
`
	ents, rels := runNATSDetect(t, "python", "sub.py", src)
	subID := natsSubjectID("notifications.email")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for notifications.email, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Python_Request covers await nc.request — request/reply.
func TestNATS_Python_Request(t *testing.T) {
	src := `import asyncio
import nats

async def query_service():
    nc = await nats.connect("nats://localhost:4222")
    response = await nc.request("pricing.query", b"item-42", timeout=1)
    return response.data
`
	ents, rels := runNATSDetect(t, "python", "rpc.py", src)
	subID := natsSubjectID("pricing.query")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for pricing.query, ents=%v", ents)
	}
	if q.props["pattern"] != "request_reply" {
		t.Fatalf("pattern = %q, want request_reply", q.props["pattern"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge for request, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Java — nats.java
// ---------------------------------------------------------------------------

// TestNATS_Java_Publish covers connection.publish(subject, data).
func TestNATS_Java_Publish(t *testing.T) {
	src := `import io.nats.client.Connection;
import io.nats.client.Nats;

public class OrderPublisher {
    private Connection nc;

    public void publishOrder(byte[] data) throws Exception {
        nc.publish("orders.placed", data);
    }
}
`
	ents, rels := runNATSDetect(t, "java", "OrderPublisher.java", src)
	subID := natsSubjectID("orders.placed")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.placed, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Java_Subscribe covers connection.subscribe(subject).
func TestNATS_Java_Subscribe(t *testing.T) {
	src := `import io.nats.client.Connection;
import io.nats.client.Subscription;

public class OrderConsumer {
    public void startConsuming(Connection nc) throws Exception {
        Subscription sub = nc.subscribe("orders.placed");
        Message msg = sub.nextMessage(Duration.ofSeconds(5));
        processOrder(msg.getData());
    }
}
`
	ents, rels := runNATSDetect(t, "java", "OrderConsumer.java", src)
	subID := natsSubjectID("orders.placed")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.placed, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestNATS_Java_DispatcherSubscribe covers dispatcher.subscribe(subject, handler).
func TestNATS_Java_DispatcherSubscribe(t *testing.T) {
	src := `import io.nats.client.Dispatcher;

public class AsyncConsumer {
    public void setup(Connection nc) {
        Dispatcher d = nc.createDispatcher((msg) -> processMessage(msg));
        d.subscribe("inventory.>", (msg) -> handleInventory(msg));
    }
}
`
	ents, rels := runNATSDetect(t, "java", "AsyncConsumer.java", src)
	subID := natsSubjectID("inventory.>")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for inventory.>, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if subs[0].props["dispatcher"] != "true" {
		t.Fatalf("dispatcher = %q, want true", subs[0].props["dispatcher"])
	}
}

// TestNATS_Java_Request covers connection.request — request/reply.
func TestNATS_Java_Request(t *testing.T) {
	src := `import io.nats.client.Connection;

public class PricingClient {
    public byte[] queryPrice(Connection nc, byte[] item) throws Exception {
        Message reply = nc.request("pricing.lookup", item, Duration.ofSeconds(2));
        return reply.getData();
    }
}
`
	ents, _ := runNATSDetect(t, "java", "PricingClient.java", src)
	subID := natsSubjectID("pricing.lookup")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for pricing.lookup, ents=%v", ents)
	}
	if q.props["pattern"] != "request_reply" {
		t.Fatalf("pattern = %q, want request_reply", q.props["pattern"])
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

// TestNATS_WildcardSubject verifies that wildcard subjects like orders.*
// and inventory.> are accepted and emitted as-is.
func TestNATS_WildcardSubject(t *testing.T) {
	src := `package subscriber

import "github.com/nats-io/nats.go"

func SubscribeAll(nc *nats.Conn) {
	nc.Subscribe("orders.*", func(msg *nats.Msg) { handleOrder(msg.Data) })
	nc.Subscribe("inventory.>", func(msg *nats.Msg) { handleInventory(msg.Data) })
}
`
	ents, rels := runNATSDetect(t, "go", "wildcard.go", src)
	idOrders := natsSubjectID("orders.*")
	idInventory := natsSubjectID("inventory.>")
	if natsSubjectByID(ents, idOrders) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.*, ents=%v", ents)
	}
	if natsSubjectByID(ents, idInventory) == nil {
		t.Fatalf("expected SCOPE.Queue for inventory.>, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) < 2 {
		t.Fatalf("expected 2 SUBSCRIBES_TO edges, got %d: %v", len(subs), rels)
	}
}

// TestNATS_NoBrokerSignal verifies non-NATS files produce no output.
func TestNATS_NoBrokerSignal(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`
	ents, rels := runNATSDetect(t, "go", "hello.go", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("no nats signal: expected empty, got ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// Rust — async-nats
// ---------------------------------------------------------------------------

// TestNATS_Rust_Publish covers async-nats client.publish("subject", payload).await.
func TestNATS_Rust_Publish(t *testing.T) {
	src := `use async_nats;

async fn publish_order(client: &async_nats::Client, data: Vec<u8>) -> Result<(), async_nats::Error> {
    client.publish("orders.created", data.into()).await?;
    Ok(())
}
`
	ents, rels := runNATSDetect(t, "rust", "orders.rs", src)
	subID := natsSubjectID("orders.created")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pubs[0].to, subID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, subID)
	}
	if pubs[0].props["broker"] != "nats" {
		t.Fatalf("broker = %q, want nats", pubs[0].props["broker"])
	}
	if pubs[0].props["messaging_layer"] != "async-nats" {
		t.Fatalf("messaging_layer = %q, want async-nats", pubs[0].props["messaging_layer"])
	}
	if !strings.Contains(pubs[0].from, "publish_order") {
		t.Fatalf("caller = %q, want to contain publish_order", pubs[0].from)
	}
}

// TestNATS_Rust_Subscribe covers async-nats client.subscribe("subject").await.
func TestNATS_Rust_Subscribe(t *testing.T) {
	src := `use async_nats;

async fn start_consumer(client: &async_nats::Client) -> Result<(), async_nats::Error> {
    let mut subscriber = client.subscribe("orders.created").await?;
    while let Some(message) = subscriber.next().await {
        handle(message);
    }
    Ok(())
}
`
	ents, rels := runNATSDetect(t, "rust", "consumer.rs", src)
	subID := natsSubjectID("orders.created")
	if natsSubjectByID(ents, subID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders.created, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if subs[0].props["messaging_layer"] != "async-nats" {
		t.Fatalf("messaging_layer = %q, want async-nats", subs[0].props["messaging_layer"])
	}
}

// TestNATS_Rust_QueueSubscribe covers client.queue_subscribe("subject", "queue").await.
func TestNATS_Rust_QueueSubscribe(t *testing.T) {
	src := `use async_nats;

async fn start_worker(client: &async_nats::Client) -> Result<(), async_nats::Error> {
    let mut sub = client.queue_subscribe("jobs.process", "worker-pool").await?;
    while let Some(msg) = sub.next().await {
        handle_job(msg);
    }
    Ok(())
}
`
	ents, rels := runNATSDetect(t, "rust", "worker.rs", src)
	subID := natsSubjectID("jobs.process")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for jobs.process, ents=%v", ents)
	}
	if q.props["queue_group"] != "worker-pool" {
		t.Fatalf("queue_group = %q, want worker-pool", q.props["queue_group"])
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
	if subs[0].props["queue_group"] != "worker-pool" {
		t.Fatalf("edge queue_group = %q, want worker-pool", subs[0].props["queue_group"])
	}
}

// TestNATS_Rust_Request covers client.request("subject", payload).await (request/reply).
func TestNATS_Rust_Request(t *testing.T) {
	src := `use async_nats;

async fn ask(client: &async_nats::Client) -> Result<(), async_nats::Error> {
    let response = client.request("rpc.echo", "ping".into()).await?;
    Ok(())
}
`
	_, rels := runNATSDetect(t, "rust", "rpc.rs", src)
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge for request, rels=%v", rels)
	}
	if pubs[0].props["pattern"] != "request_reply" {
		t.Fatalf("pattern = %q, want request_reply", pubs[0].props["pattern"])
	}
}

// TestNATS_Rust_JetStream covers a JetStream context flagging jetstream=true.
func TestNATS_Rust_JetStream(t *testing.T) {
	src := `use async_nats::jetstream;

async fn publish_event(jetstream: jetstream::Context, data: Vec<u8>) -> Result<(), async_nats::Error> {
    jetstream.publish("events.stream", data.into()).await?;
    Ok(())
}
`
	ents, rels := runNATSDetect(t, "rust", "stream.rs", src)
	subID := natsSubjectID("events.stream")
	q := natsSubjectByID(ents, subID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for events.stream, ents=%v", ents)
	}
	if q.props["jetstream"] != "true" {
		t.Fatalf("jetstream prop = %q, want true", q.props["jetstream"])
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["jetstream"] != "true" {
		t.Fatalf("edge jetstream = %q, want true", pubs[0].props["jetstream"])
	}
	if pubs[0].props["messaging_layer"] != "nats-jetstream-rust" {
		t.Fatalf("messaging_layer = %q, want nats-jetstream-rust", pubs[0].props["messaging_layer"])
	}
}

// TestNATS_Rust_NoSignal verifies a Rust file without async-nats markers is ignored.
func TestNATS_Rust_NoSignal(t *testing.T) {
	src := `fn main() {
    let client = redis::Client::open("redis://localhost").unwrap();
    client.publish("some.channel", "hi");
}
`
	ents, rels := runNATSDetect(t, "rust", "noise.rs", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("no async-nats signal: expected empty, got ents=%v rels=%v", ents, rels)
	}
}
