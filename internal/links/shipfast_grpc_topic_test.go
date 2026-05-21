package links

import (
	"path/filepath"
	"testing"
)

// TestShipFastManifest_GRPCAndTopicLinks is the #1447 acceptance test.
//
// It replicates the ShipFast polyglot fixture at the link-pass level:
//
//	§2 gRPC:
//	  orders (Python client) → inventory (Go server)
//	  via contracts/proto/inventory.proto — Inventory/Reserve
//
//	§3 Topics:
//	  orders.placed   : orders → inventory, notifications, analytics  (3 links)
//	  payments.settled: payments → orders, billing, ledger             (3 links)
//	  inventory.reserved (SNS): inventory → shipping                   (1 link)
//
// Expected after: ≥1 gRPC link (orders→inventory) + ≥7 topic links.
// Before running this test both counts would be 0 (issue #1447).
func TestShipFastManifest_GRPCAndTopicLinks(t *testing.T) {
	root := fixtureRoot(t)

	// ---- orders service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			// gRPC client stub: calls Inventory.Reserve
			{"id": "create_order_fn", "name": "create_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "gm_inv_reserve_client", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			// Topic publisher: orders.placed
			{"id": "place_order_fn", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "orders/producer.py"},
			{"id": "topic_orders_placed_pub", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			// Topic subscriber: payments.settled
			{"id": "on_payment_fn", "name": "on_payment_settled", "kind": "SCOPE.Operation", "source_file": "orders/consumer.py"},
			{"id": "topic_payments_settled_orders_sub", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "create_order_fn", "to_id": "gm_inv_reserve_client", "kind": "GRPC_HANDLES"},
			{"from_id": "place_order_fn", "to_id": "topic_orders_placed_pub", "kind": "PUBLISHES_TO"},
			{"from_id": "on_payment_fn", "to_id": "topic_payments_settled_orders_sub", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- inventory service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			// gRPC server: implements Inventory.Reserve
			{"id": "inventory_handler", "name": "InventoryServer.Reserve", "kind": "SCOPE.Operation", "source_file": "inventory/server.go"},
			{"id": "gm_inv_reserve_server", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			// Topic subscriber: orders.placed
			{"id": "inv_on_order_fn", "name": "on_order_placed", "kind": "SCOPE.Operation", "source_file": "inventory/consumer.go"},
			{"id": "topic_orders_placed_inv", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			// Topic publisher: inventory.reserved (SNS)
			{"id": "inv_reserve_fn", "name": "reserve_item", "kind": "SCOPE.Operation", "source_file": "inventory/service.go"},
			{"id": "topic_inv_reserved_pub", "name": "sns:inventory.reserved", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "inventory_handler", "to_id": "gm_inv_reserve_server", "kind": "GRPC_IMPLEMENTS"},
			{"from_id": "inv_on_order_fn", "to_id": "topic_orders_placed_inv", "kind": "SUBSCRIBES_TO"},
			{"from_id": "inv_reserve_fn", "to_id": "topic_inv_reserved_pub", "kind": "PUBLISHES_TO"},
		},
	})

	// ---- notifications service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "notifications",
		Entities: []map[string]any{
			{"id": "notif_fn", "name": "send_confirmation", "kind": "SCOPE.Operation", "source_file": "notifications/handler.js"},
			{"id": "topic_orders_placed_notif", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "notif_fn", "to_id": "topic_orders_placed_notif", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- analytics service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "analytics",
		Entities: []map[string]any{
			{"id": "analytics_fn", "name": "track_order", "kind": "SCOPE.Operation", "source_file": "analytics/handler.go"},
			{"id": "topic_orders_placed_analytics", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "analytics_fn", "to_id": "topic_orders_placed_analytics", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- payments service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "payments",
		Entities: []map[string]any{
			{"id": "settle_fn", "name": "settle_payment", "kind": "SCOPE.Operation", "source_file": "payments/service.py"},
			{"id": "topic_payments_settled_pub", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "settle_fn", "to_id": "topic_payments_settled_pub", "kind": "PUBLISHES_TO"},
		},
	})

	// ---- billing service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "billing",
		Entities: []map[string]any{
			{"id": "billing_fn", "name": "record_payment", "kind": "SCOPE.Operation", "source_file": "billing/consumer.go"},
			{"id": "topic_payments_settled_billing", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "billing_fn", "to_id": "topic_payments_settled_billing", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- ledger service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "ledger",
		Entities: []map[string]any{
			{"id": "ledger_fn", "name": "post_entry", "kind": "SCOPE.Operation", "source_file": "ledger/consumer.go"},
			{"id": "topic_payments_settled_ledger", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "ledger_fn", "to_id": "topic_payments_settled_ledger", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- shipping service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "shipping",
		Entities: []map[string]any{
			{"id": "shipping_fn", "name": "create_shipment", "kind": "SCOPE.Operation", "source_file": "shipping/consumer.go"},
			{"id": "topic_inv_reserved_sub", "name": "sns:inventory.reserved", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "shipping_fn", "to_id": "topic_inv_reserved_sub", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-shipfast")
	result, err := RunAllPasses("shipfast", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "shipfast-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	grpcCount := 0
	topicCount := 0
	for _, l := range doc.Links {
		switch l.Method {
		case MethodGRPC:
			grpcCount++
		case MethodTopic:
			topicCount++
		}
	}

	// §2: At least orders→inventory gRPC link.
	if grpcCount < 1 {
		t.Errorf("§2 gRPC: expected ≥1 cross-repo gRPC link, got 0; pass results=%+v", result.Results)
	}

	// Verify the specific orders→inventory Reserve link exists.
	grpcFound := false
	for _, l := range doc.Links {
		if l.Method == MethodGRPC &&
			l.Source == "orders::create_order_fn" &&
			l.Target == "inventory::inventory_handler" {
			grpcFound = true
			if l.Identifier == nil || *l.Identifier != "grpc:Inventory/Reserve" {
				t.Errorf("§2 gRPC: identifier: want grpc:Inventory/Reserve, got %v", l.Identifier)
			}
			if l.Channel == nil || *l.Channel != "grpc" {
				t.Errorf("§2 gRPC: channel: want grpc, got %v", l.Channel)
			}
			break
		}
	}
	if !grpcFound {
		t.Errorf("§2 gRPC: missing orders::create_order_fn → inventory::inventory_handler link")
		for _, l := range doc.Links {
			if l.Method == MethodGRPC {
				t.Logf("  found grpc link: %s → %s", l.Source, l.Target)
			}
		}
	}

	// §3: Expect 7 topic links total:
	//   orders.placed (kafka): orders→inventory (1), orders→notifications (2), orders→analytics (3)
	//   payments.settled (sns): payments→orders (4), payments→billing (5), payments→ledger (6)
	//   inventory.reserved (sns): inventory→shipping (7)
	if topicCount < 7 {
		t.Errorf("§3 topics: expected ≥7 cross-repo topic links, got %d; pass results=%+v", topicCount, result.Results)
		for _, l := range doc.Links {
			if l.Method == MethodTopic {
				t.Logf("  found topic link: %s → %s (identifier=%v, channel=%v)", l.Source, l.Target, l.Identifier, l.Channel)
			}
		}
	}

	t.Logf("Before: 0 gRPC links, 0 topic links (issue #1447)")
	t.Logf("After:  %d gRPC links, %d topic links", grpcCount, topicCount)
	t.Logf("Total cross-repo links: %d", len(doc.Links))
}
