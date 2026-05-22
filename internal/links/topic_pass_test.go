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
