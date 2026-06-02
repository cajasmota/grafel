package links

import (
	"path/filepath"
	"testing"
)

// TestTopicPass_KafkaPublisherSubscriber verifies the happy path:
// orders repo publishes to kafka:orders.placed; inventory and notifications
// repos subscribe. Two cross-repo topic links expected.
func TestTopicPass_KafkaPublisherSubscriber(t *testing.T) {
	root := fixtureRoot(t)

	// Publisher: orders repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "pub1", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "orders/handler.py"},
			{
				"id": "topic1", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})

	// Subscriber: inventory repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "sub1", "name": "on_order_placed", "kind": "SCOPE.Operation", "source_file": "inventory/consumer.go"},
			{
				"id": "topic2", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	// Subscriber: notifications repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "notifications",
		Entities: []map[string]any{
			{"id": "sub2", "name": "send_confirmation", "kind": "SCOPE.Operation", "source_file": "notifications/handler.js"},
			{
				"id": "topic3", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub2", "to_id": "topic3", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic1")
	result, err := RunAllPasses("tg1", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg1-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}

	// Expect 2 links: orders→inventory and orders→notifications.
	if len(topicLinks) != 2 {
		t.Fatalf("expected 2 topic links, got %d; results=%+v; links=%+v", len(topicLinks), result.Results, topicLinks)
	}

	for _, l := range topicLinks {
		if l.Source != "orders::pub1" {
			t.Errorf("source: want orders::pub1, got %s", l.Source)
		}
		if l.Channel == nil || *l.Channel != "kafka" {
			t.Errorf("channel: want kafka, got %v", l.Channel)
		}
		if l.Identifier == nil || *l.Identifier != "kafka:orders.placed" {
			t.Errorf("identifier: want kafka:orders.placed, got %v", l.Identifier)
		}
		if l.Relation != RelationPublishesTo {
			t.Errorf("relation: want publishes_to, got %s", l.Relation)
		}
	}

	// Verify the correct subscriber repos are targeted.
	targets := map[string]bool{}
	for _, l := range topicLinks {
		targets[l.Target] = true
	}
	if !targets["inventory::sub1"] {
		t.Error("expected target inventory::sub1 among topic links")
	}
	if !targets["notifications::sub2"] {
		t.Error("expected target notifications::sub2 among topic links")
	}
}

// TestTopicPass_SNStoSQS verifies that an SNS publisher → subscriber
// cross-repo pair produces a link when the canonical topic Name is shared.
// Simulates ShipFast §3: payments.settled (payments→billing).
func TestTopicPass_SNStoSQS(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "payments",
		Entities: []map[string]any{
			{"id": "pub1", "name": "settle_payment", "kind": "SCOPE.Operation", "source_file": "payments/service.py"},
			{
				"id": "topic1", "name": "sns:payments.settled",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "sns", "topic_name": "payments.settled"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})

	writeFixture(t, root, fixtureGraph{
		Repo: "billing",
		Entities: []map[string]any{
			{"id": "sub1", "name": "record_payment", "kind": "SCOPE.Operation", "source_file": "billing/consumer.go"},
			{
				"id": "topic2", "name": "sns:payments.settled",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "sns", "topic_name": "payments.settled"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic2")
	if _, err := RunAllPasses("tg2", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg2-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}

	if len(topicLinks) != 1 {
		t.Fatalf("expected 1 topic link, got %d: %+v", len(topicLinks), topicLinks)
	}
	if topicLinks[0].Channel == nil || *topicLinks[0].Channel != "sns" {
		t.Errorf("channel: want sns, got %v", topicLinks[0].Channel)
	}
	if topicLinks[0].Source != "payments::pub1" {
		t.Errorf("source: want payments::pub1, got %s", topicLinks[0].Source)
	}
	if topicLinks[0].Target != "billing::sub1" {
		t.Errorf("target: want billing::sub1, got %s", topicLinks[0].Target)
	}
}

// TestTopicPass_NoPublisher verifies that a topic present in two repos but
// only with subscribers (no publishers) does NOT produce a link.
func TestTopicPass_NoPublisher(t *testing.T) {
	root := fixtureRoot(t)

	for _, repo := range []string{"svc-a", "svc-b"} {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{"id": "sub1", "name": "handler", "kind": "SCOPE.Operation", "source_file": "handler.go"},
				{
					"id": "topic1", "name": "kafka:shared.event",
					"kind": "SCOPE.MessageTopic", "source_file": "",
				},
			},
			Edges: []map[string]string{
				{"from_id": "sub1", "to_id": "topic1", "kind": "SUBSCRIBES_TO"},
			},
		})
	}

	home := filepath.Join(root, "ag-home-topic3")
	if _, err := RunAllPasses("tg3", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg3-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			t.Errorf("expected no topic links, got %+v", l)
		}
	}
}

// TestTopicPass_BrokerFromTopicName checks that channel extraction works
// for all broker prefixes used by ShipFast §3 and beyond.
func TestTopicPass_BrokerFromTopicName(t *testing.T) {
	cases := []struct {
		name   string
		expect string
	}{
		{"kafka:orders.placed", "kafka"},
		{"sns:payments.settled", "sns"},
		{"sqs:inventory-reserved-queue", "sqs"},
		{"event:eventbridge:orders:orders.placed", "eventbridge"},
		{"event:eventgrid:topic:event-type", "eventgrid"},
		{"event:cloudevents:source:type", "cloudevents"},
		{"redis:orders.placed", "redis"},
		{"nats:orders.placed", "nats"},
		{"pubsub:orders.placed", "pubsub"},
		// #3628 area #2: the five brokers whose producer→consumer topology join
		// was audited. pulsar/rabbitmq/mqtt/azure already carried correct
		// channels; kinesis was mislabelled "redis" by the stream: collision.
		{"topic:pulsar:persistent://public/default/orders", "topic"},
		{"rabbitmq:orders", "rabbitmq"},
		{"mqtt:sensors/temperature", "mqtt"},
		{"azure:orders-topic", "azure"},
		{"servicebus:orders", "servicebus"},
		// Kinesis (serverless) 2-segment stream synthetic — now "kinesis",
		// previously fell through to the redis fallback.
		{"stream:orders", "kinesis"},
		// Redis Streams 3-segment synthetic stays "redis" (regression guard
		// for the stream: disambiguation).
		{"stream:redis:events.log", "redis"},
	}
	for _, tc := range cases {
		got := brokerFromTopicName(tc.name)
		if got != tc.expect {
			t.Errorf("brokerFromTopicName(%q): want %q, got %q", tc.name, tc.expect, got)
		}
	}
}

// TestTopicPass_Idempotent verifies that running P7 twice does not duplicate
// topic links (method-segregated overwrite guarantees exactly-once semantics).
func TestTopicPass_Idempotent(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "pub1", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "o.py"},
			{"id": "topic1", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "sub1", "name": "on_order", "kind": "SCOPE.Operation", "source_file": "i.go"},
			{"id": "topic2", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic4")

	run1, err := RunAllPasses("tg4", root, home)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := RunAllPasses("tg4", root, home)
	if err != nil {
		t.Fatal(err)
	}

	var topicCount1, topicCount2 int
	for _, r := range run1.Results {
		if r.Pass == "topic" {
			topicCount1 = r.LinksAdded
		}
	}
	for _, r := range run2.Results {
		if r.Pass == "topic" {
			topicCount2 = r.LinksAdded
		}
	}

	if topicCount1 != 1 {
		t.Errorf("run1: expected 1 topic link added, got %d", topicCount1)
	}
	if topicCount2 != 1 {
		t.Errorf("run2: expected 1 topic link added (idempotent replace), got %d", topicCount2)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg4-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}
	if len(topicLinks) != 1 {
		t.Errorf("expected exactly 1 topic link after 2 runs, got %d", len(topicLinks))
	}
}

// TestTopicPass_EventBridgeChannel verifies that eventbridge-prefixed topic
// names produce channel="eventbridge" (not "event").
func TestTopicPass_EventBridgeChannel(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "pub1", "name": "dispatch_event", "kind": "SCOPE.Operation", "source_file": "orders/events.py"},
			{"id": "topic1", "name": "event:eventbridge:orders:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "analytics",
		Entities: []map[string]any{
			{"id": "sub1", "name": "track_order", "kind": "SCOPE.Operation", "source_file": "analytics/handler.go"},
			{"id": "topic2", "name": "event:eventbridge:orders:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic5")
	if _, err := RunAllPasses("tg5", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg5-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}
	if len(topicLinks) != 1 {
		t.Fatalf("expected 1 topic link, got %d: %+v", len(topicLinks), topicLinks)
	}
	if topicLinks[0].Channel == nil || *topicLinks[0].Channel != "eventbridge" {
		t.Errorf("channel: want eventbridge, got %v", topicLinks[0].Channel)
	}
}

// TestTopicPass_TwoDistinctTopicsSameRepoPair is the #1474 regression guard.
//
// When two DIFFERENT topic names both flow from the same publisher repo to the
// same subscriber repo, the pre-#1474 code collapsed them into a single edge
// because the dedup key was (source-entity, target-entity, method). If the same
// representative entity (lexicographic minimum) was chosen for BOTH topics on
// each side, MakeID produced an identical hash and the second edge was dropped.
//
// After the fix the dedup key is (topicName, source-entity, target-entity), so
// each distinct topic between a given repo-pair emits its own edge.
func TestTopicPass_TwoDistinctTopicsSameRepoPair(t *testing.T) {
	root := fixtureRoot(t)

	// orders repo: one function publishes BOTH topics.
	// Using the SAME entity ID ("shared_pub") as the publisher for both ensures
	// the pre-fix code would choose the same representative and produce a
	// colliding MakeID → dropping the second topic edge.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			// shared_pub is the lex-minimum publisher entity for both topics.
			{"id": "shared_pub", "name": "publish_events", "kind": "SCOPE.Operation", "source_file": "orders/producer.py"},
			{"id": "topic_placed", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			{"id": "topic_shipped", "name": "kafka:orders.shipped", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "shared_pub", "to_id": "topic_placed", "kind": "PUBLISHES_TO"},
			{"from_id": "shared_pub", "to_id": "topic_shipped", "kind": "PUBLISHES_TO"},
		},
	})

	// notifications repo: one function subscribes to BOTH topics.
	// Same pattern: shared_sub is the lex-minimum subscriber for both topics.
	writeFixture(t, root, fixtureGraph{
		Repo: "notifications",
		Entities: []map[string]any{
			{"id": "shared_sub", "name": "handle_order_event", "kind": "SCOPE.Operation", "source_file": "notifications/handler.js"},
			{"id": "topic_placed_n", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			{"id": "topic_shipped_n", "name": "kafka:orders.shipped", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "shared_sub", "to_id": "topic_placed_n", "kind": "SUBSCRIBES_TO"},
			{"from_id": "shared_sub", "to_id": "topic_shipped_n", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic-1474")
	result, err := RunAllPasses("tg1474", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg1474-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}

	// Must emit 2 edges: one per distinct topic, not 1 (the pre-#1474 collapse).
	if len(topicLinks) != 2 {
		t.Fatalf("#1474 regression: expected 2 topic links (one per distinct topic), got %d; "+
			"pass results=%+v; links=%+v", len(topicLinks), result.Results, topicLinks)
	}

	// Both edges must share the same source and target (same rep entities).
	for _, l := range topicLinks {
		if l.Source != "orders::shared_pub" {
			t.Errorf("source: want orders::shared_pub, got %s", l.Source)
		}
		if l.Target != "notifications::shared_sub" {
			t.Errorf("target: want notifications::shared_sub, got %s", l.Target)
		}
	}

	// Verify both topic identifiers are present.
	identifiers := map[string]bool{}
	for _, l := range topicLinks {
		if l.Identifier != nil {
			identifiers[*l.Identifier] = true
		}
	}
	if !identifiers["kafka:orders.placed"] {
		t.Error("expected kafka:orders.placed identifier among topic links")
	}
	if !identifiers["kafka:orders.shipped"] {
		t.Error("expected kafka:orders.shipped identifier among topic links")
	}

	// Both link IDs must be distinct.
	if topicLinks[0].ID == topicLinks[1].ID {
		t.Errorf("link IDs must be distinct per topic; both got %s", topicLinks[0].ID)
	}
}

// TestTopicPass_RedisPubSubQueueJoin is the #1489 regression test: the Redis
// pub/sub engine pass emits SCOPE.Queue entities (Name
// `channel:redis-pubsub:<name>`), NOT SCOPE.MessageTopic. Before #1489 P7
// only scanned SCOPE.MessageTopic, so a redis publisher and subscriber in
// different repos sharing the identical channel Name were never paired. This
// mirrors the real fixture: notifications (Kotlin) publishes
// notifications.push; tracking-ws (Node) and realtime-dashboard (Elixir)
// subscribe.
func TestTopicPass_RedisPubSubQueueJoin(t *testing.T) {
	root := fixtureRoot(t)

	q := func(repo, entID, opName, file, edge string) {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{"id": opName, "name": opName, "kind": "SCOPE.Operation", "source_file": file},
				{
					"id": entID, "name": "channel:redis-pubsub:notifications.push",
					"kind": "SCOPE.Queue", "source_file": "",
					"properties": map[string]any{"broker": "redis", "channel_type": "pubsub"},
				},
			},
			Edges: []map[string]string{
				{"from_id": opName, "to_id": entID, "kind": edge},
			},
		})
	}
	q("notifications", "q_pub", "publishPush", "Listeners.kt", "PUBLISHES_TO")
	q("tracking-ws", "q_sub1", "module", "index.ts", "SUBSCRIBES_TO")
	q("realtime-dashboard", "q_sub2", "Subscriber", "subscriber.ex", "SUBSCRIBES_TO")

	home := filepath.Join(root, "ag-home-redisq")
	if _, err := RunAllPasses("tgredis", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "tgredis-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	targets := map[string]bool{}
	for _, l := range doc.Links {
		if l.Method != MethodTopic {
			continue
		}
		// P7 links from the publisher OPERATION (edge from_id), not the queue
		// entity id.
		if l.Source != "notifications::publishPush" {
			t.Errorf("source: want notifications::publishPush, got %s", l.Source)
		}
		if l.Channel == nil || *l.Channel != "redis" {
			t.Errorf("channel: want redis, got %v", l.Channel)
		}
		targets[l.Target] = true
	}
	if !targets["tracking-ws::module"] {
		t.Error("expected notifications→tracking-ws redis link")
	}
	if !targets["realtime-dashboard::Subscriber"] {
		t.Error("expected notifications→realtime-dashboard redis link")
	}
}

// TestTopicPass_BullMQQueueJoin proves BullMQ topic_attribution (#2865): the
// engine bullmq pass emits SCOPE.Queue entities Named `bullmq:<name>` on both
// the producer (`new Queue('emails')` + `queue.add`) and consumer
// (`new Worker('emails')`) sides. Because the canonical name is identical, P7
// joins a producer service to its worker service across repos with no
// BullMQ-specific matching code — exactly like the redis-queue join above.
func TestTopicPass_BullMQQueueJoin(t *testing.T) {
	root := fixtureRoot(t)

	q := func(repo, entID, opName, file, edge string) {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{"id": opName, "name": opName, "kind": "SCOPE.Operation", "source_file": file},
				{
					"id": entID, "name": "bullmq:emails",
					"kind": "SCOPE.Queue", "source_file": "",
					"properties": map[string]any{"broker": "bullmq", "queue_name": "emails"},
				},
			},
			Edges: []map[string]string{
				{"from_id": opName, "to_id": entID, "kind": edge},
			},
		})
	}
	q("api-gateway", "q_pub", "enqueueWelcome", "producer.ts", "PUBLISHES_TO")
	q("email-worker", "q_sub", "worker", "worker.ts", "SUBSCRIBES_TO")

	home := filepath.Join(root, "ag-home-bullmq")
	if _, err := RunAllPasses("tgbullmq", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "tgbullmq-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var linked bool
	for _, l := range doc.Links {
		if l.Method != MethodTopic {
			continue
		}
		if l.Source != "api-gateway::enqueueWelcome" {
			t.Errorf("source: want api-gateway::enqueueWelcome, got %s", l.Source)
		}
		if l.Channel == nil || *l.Channel != "bullmq" {
			t.Errorf("channel: want bullmq, got %v", l.Channel)
		}
		if l.Target == "email-worker::worker" {
			linked = true
		}
	}
	if !linked {
		t.Error("expected api-gateway→email-worker bullmq topic link")
	}
}

// ---------------------------------------------------------------------------
// #3628 area #2 — broker topology completeness.
//
// The producer→consumer topology join in topic_pass is keyed on entity *kind*
// (SCOPE.MessageTopic / SCOPE.Queue), not on the broker prefix, so any broker
// that emits one of those kinds with PUBLISHES_TO / SUBSCRIBES_TO edges and a
// canonical cross-repo Name is joined. The meta-audit called out pulsar,
// rabbitmq, mqtt, kinesis and azure (Service Bus / Event Hubs) as never being
// joined; these tests assert the SPECIFIC producer→topic→consumer edge that
// the join now (and, for the four already-correct brokers, already) produces,
// and lock the kinesis channel-label fix (stream: collision with Redis
// Streams) in place.
// ---------------------------------------------------------------------------

// brokerJoinFixture wires a single producer repo (PUBLISHES_TO topicName) and a
// single consumer repo (SUBSCRIBES_TO topicName) sharing the canonical entity
// Name, runs the passes, and returns the topic links produced.
func brokerJoinFixture(t *testing.T, topicName, kind string) []Link {
	t.Helper()
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "producer-svc",
		Entities: []map[string]any{
			{"id": "prod_fn", "name": "publish_event", "kind": "SCOPE.Operation", "source_file": "producer-svc/pub.go"},
			{"id": "topic_p", "name": topicName, "kind": kind, "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "prod_fn", "to_id": "topic_p", "kind": "PUBLISHES_TO"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "consumer-svc",
		Entities: []map[string]any{
			{"id": "cons_fn", "name": "on_event", "kind": "SCOPE.Operation", "source_file": "consumer-svc/sub.go"},
			{"id": "topic_c", "name": topicName, "kind": kind, "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "cons_fn", "to_id": "topic_c", "kind": "SUBSCRIBES_TO"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("bg", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "bg-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}
	return topicLinks
}

// TestTopicPass_BrokerTopologyCompleteness asserts the exact producer→consumer
// join through the shared broker node for each of the five audited brokers.
func TestTopicPass_BrokerTopologyCompleteness(t *testing.T) {
	cases := []struct {
		broker      string
		topicName   string
		kind        string
		wantChannel string
	}{
		// Pulsar: pulsar_edges.go emits SCOPE.MessageTopic Name
		// "topic:pulsar:<canonical-uri>".
		{"pulsar", "topic:pulsar:persistent://public/default/orders.placed", "SCOPE.MessageTopic", "topic"},
		// RabbitMQ: rabbitmq_edges.go emits SCOPE.Queue Name "rabbitmq:<queue>".
		// Producer publishes to exchange "orders", consumer subscribes the same
		// queue/exchange "orders" → join through the shared rabbitmq:orders node.
		{"rabbitmq", "rabbitmq:orders", "SCOPE.Queue", "rabbitmq"},
		// MQTT: cpp_messaging_edges.go emits SCOPE.MessageTopic Name
		// "mqtt:<topic>".
		{"mqtt", "mqtt:sensors/temperature", "SCOPE.MessageTopic", "mqtt"},
		// Kinesis: serverless_framework_edges.go emits SCOPE.Queue Name
		// "stream:<name>". Channel must read "kinesis", NOT "redis" (#3628).
		{"kinesis", "stream:orders", "SCOPE.Queue", "kinesis"},
		// Azure Service Bus / Event Hubs: azure-prefixed MessageTopic synthetic.
		{"azure", "azure:orders-topic", "SCOPE.MessageTopic", "azure"},
	}
	for _, tc := range cases {
		t.Run(tc.broker, func(t *testing.T) {
			links := brokerJoinFixture(t, tc.topicName, tc.kind)
			if len(links) != 1 {
				t.Fatalf("%s: want exactly 1 producer→consumer topology link, got %d: %+v", tc.broker, len(links), links)
			}
			l := links[0]
			if l.Source != "producer-svc::prod_fn" {
				t.Errorf("%s: source: want producer-svc::prod_fn, got %s", tc.broker, l.Source)
			}
			if l.Target != "consumer-svc::cons_fn" {
				t.Errorf("%s: target: want consumer-svc::cons_fn, got %s", tc.broker, l.Target)
			}
			if l.Relation != RelationPublishesTo {
				t.Errorf("%s: relation: want %s, got %s", tc.broker, RelationPublishesTo, l.Relation)
			}
			if l.Identifier == nil || *l.Identifier != tc.topicName {
				t.Errorf("%s: identifier: want %q, got %v", tc.broker, tc.topicName, l.Identifier)
			}
			if l.Channel == nil || *l.Channel != tc.wantChannel {
				t.Errorf("%s: channel: want %q, got %v", tc.broker, tc.wantChannel, l.Channel)
			}
		})
	}
}

// TestTopicPass_BrokerNoFalseTopology asserts the honest-join contract for the
// audited brokers: a producer whose topic has NO matching consumer (no
// SUBSCRIBES_TO anywhere) yields no topology edge. Guards against fabricating
// a cross-component link from a one-sided publisher.
func TestTopicPass_BrokerNoFalseTopology(t *testing.T) {
	root := fixtureRoot(t)
	// producer-only repo for a rabbitmq exchange "orphan".
	writeFixture(t, root, fixtureGraph{
		Repo: "producer-only",
		Entities: []map[string]any{
			{"id": "prod_fn", "name": "publish_orphan", "kind": "SCOPE.Operation", "source_file": "producer-only/pub.go"},
			{"id": "topic_p", "name": "rabbitmq:orphan", "kind": "SCOPE.Queue", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "prod_fn", "to_id": "topic_p", "kind": "PUBLISHES_TO"},
		},
	})
	// A second repo that merely *mentions* a DIFFERENT kinesis stream as a
	// consumer — no shared Name with the rabbitmq producer.
	writeFixture(t, root, fixtureGraph{
		Repo: "unrelated-consumer",
		Entities: []map[string]any{
			{"id": "cons_fn", "name": "on_other", "kind": "SCOPE.Operation", "source_file": "unrelated-consumer/sub.go"},
			{"id": "topic_c", "name": "stream:something-else", "kind": "SCOPE.Queue", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "cons_fn", "to_id": "topic_c", "kind": "SUBSCRIBES_TO"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("ng", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "ng-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			t.Errorf("expected NO topic link (no shared producer↔consumer topic), got %+v", l)
		}
	}
}
