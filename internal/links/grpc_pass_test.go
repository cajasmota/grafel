package links

import (
	"path/filepath"
	"testing"
)

// TestGRPCPass_ClientServerMatch verifies the happy path:
// a Python client (orders repo) calls Inventory.Reserve via stub,
// and a Go server (inventory repo) implements InventoryServer.Reserve.
// The pass must emit one cross-repo CALLS link with method=grpc,
// channel=grpc, identifier=grpc:Inventory/Reserve.
func TestGRPCPass_ClientServerMatch(t *testing.T) {
	root := fixtureRoot(t)

	// Client side: orders repo — Python stub calls grpc:Inventory/Reserve.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{
				"id": "caller1", "name": "create_order",
				"kind": "SCOPE.Operation", "source_file": "orders/service.py",
			},
			{
				"id": "gm1", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{
					"service":      "Inventory",
					"method":       "Reserve",
					"pattern_type": "grpc_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "caller1", "to_id": "gm1", "kind": "GRPC_HANDLES"},
		},
	})

	// Server side: inventory repo — Go handler implements grpc:Inventory/Reserve.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{
				"id": "handler1", "name": "InventoryServer.Reserve",
				"kind": "SCOPE.Operation", "source_file": "inventory/server.go",
			},
			{
				"id": "gm2", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{
					"service":      "Inventory",
					"method":       "Reserve",
					"pattern_type": "grpc_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "gm2", "kind": "GRPC_IMPLEMENTS"},
		},
	})

	home := filepath.Join(root, "ag-home")
	result, err := RunAllPasses("g1", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g1-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var grpcLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			grpcLinks = append(grpcLinks, l)
		}
	}

	if len(grpcLinks) == 0 {
		t.Fatalf("expected ≥1 grpc link; got 0; total links=%d; pass results=%+v", len(doc.Links), result.Results)
	}

	hit := grpcLinks[0]
	if hit.Source != "orders::caller1" {
		t.Errorf("source: want orders::caller1 (resolved caller), got %s", hit.Source)
	}
	if hit.Target != "inventory::handler1" {
		t.Errorf("target: want inventory::handler1 (resolved handler), got %s", hit.Target)
	}
	if hit.Relation != RelationCalls {
		t.Errorf("relation: want calls, got %s", hit.Relation)
	}
	if hit.Channel == nil || *hit.Channel != "grpc" {
		t.Errorf("channel: want grpc, got %v", hit.Channel)
	}
	if hit.Identifier == nil || *hit.Identifier != "grpc:Inventory/Reserve" {
		t.Errorf("identifier: want grpc:Inventory/Reserve, got %v", hit.Identifier)
	}
}

// TestGRPCPass_NoMatchWithoutBothSides verifies that a GrpcMethod entity
// present in only one repo (server only, no client stub) does NOT produce a link.
func TestGRPCPass_NoMatchWithoutBothSides(t *testing.T) {
	root := fixtureRoot(t)

	// Only a server — no client stub in any other repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{
				"id": "handler1", "name": "InventoryServer.Reserve",
				"kind": "SCOPE.Operation", "source_file": "inventory/server.go",
			},
			{
				"id": "gm2", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
			},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "gm2", "kind": "GRPC_IMPLEMENTS"},
		},
	})
	// A second repo that has nothing gRPC-related.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "c1", "name": "App", "kind": "SCOPE.Component", "source_file": "src/App.tsx"},
		},
		Edges: nil,
	})

	home := filepath.Join(root, "ag-home2")
	if _, err := RunAllPasses("g2", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g2-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			t.Errorf("expected no grpc links, got %+v", l)
		}
	}
}

// TestGRPCPass_NoEdgesNoLink verifies that when the Name appears in two
// repos but neither repo has GRPC_HANDLES nor GRPC_IMPLEMENTS edges, no
// link is emitted (both sides look like neither client nor server).
func TestGRPCPass_NoEdgesNoLink(t *testing.T) {
	root := fixtureRoot(t)

	// Two repos both have the GrpcMethod entity, but no edges at all.
	for _, repo := range []string{"orders", "inventory"} {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{
					"id": "gm1", "name": "grpc:Inventory/Reserve",
					"kind": "SCOPE.GrpcMethod", "source_file": "",
					"properties": map[string]any{"pattern_type": "grpc_synthesis"},
				},
			},
			Edges: []map[string]string{}, // no GRPC_HANDLES or GRPC_IMPLEMENTS edges
		})
	}

	home := filepath.Join(root, "ag-home3")
	if _, err := RunAllPasses("g3", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g3-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			t.Errorf("expected no grpc links (no edges present), got %+v", l)
		}
	}
}

// TestGRPCPass_MultipleMethodsSameService verifies that multiple RPC methods
// on the same service each produce their own independent cross-repo link.
func TestGRPCPass_MultipleMethodsSameService(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "c1", "name": "create_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "c2", "name": "cancel_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "gm1", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			{"id": "gm2", "name": "grpc:Inventory/Release", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "c1", "to_id": "gm1", "kind": "GRPC_HANDLES"},
			{"from_id": "c2", "to_id": "gm2", "kind": "GRPC_HANDLES"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "h1", "name": "Server.Reserve", "kind": "SCOPE.Operation", "source_file": "server.go"},
			{"id": "h2", "name": "Server.Release", "kind": "SCOPE.Operation", "source_file": "server.go"},
			{"id": "gm3", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			{"id": "gm4", "name": "grpc:Inventory/Release", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "gm3", "kind": "GRPC_IMPLEMENTS"},
			{"from_id": "h2", "to_id": "gm4", "kind": "GRPC_IMPLEMENTS"},
		},
	})

	home := filepath.Join(root, "ag-home4")
	if _, err := RunAllPasses("g4", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g4-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var grpcLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			grpcLinks = append(grpcLinks, l)
		}
	}

	if len(grpcLinks) != 2 {
		t.Fatalf("expected 2 grpc links (one per method), got %d: %+v", len(grpcLinks), grpcLinks)
	}

	// Both links should go from orders→inventory.
	for _, l := range grpcLinks {
		if l.Relation != RelationCalls {
			t.Errorf("relation: want calls, got %s", l.Relation)
		}
		if l.Channel == nil || *l.Channel != "grpc" {
			t.Errorf("channel: want grpc, got %v", l.Channel)
		}
	}
}

// TestGRPCPass_Idempotent verifies that running P6 twice does not duplicate
// gRPC links (method-segregated overwrite).
func TestGRPCPass_Idempotent(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "caller1", "name": "create_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "gm1", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "caller1", "to_id": "gm1", "kind": "GRPC_HANDLES"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "handler1", "name": "Server.Reserve", "kind": "SCOPE.Operation", "source_file": "server.go"},
			{"id": "gm2", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "gm2", "kind": "GRPC_IMPLEMENTS"},
		},
	})

	home := filepath.Join(root, "ag-home5")

	run1, err := RunAllPasses("g5", root, home)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := RunAllPasses("g5", root, home)
	if err != nil {
		t.Fatal(err)
	}

	var grpcCount1, grpcCount2 int
	for _, r := range run1.Results {
		if r.Pass == "grpc" {
			grpcCount1 = r.LinksAdded
		}
	}
	for _, r := range run2.Results {
		if r.Pass == "grpc" {
			grpcCount2 = r.LinksAdded
		}
	}

	if grpcCount1 != 1 {
		t.Errorf("run1: expected 1 grpc link added, got %d", grpcCount1)
	}
	if grpcCount2 != 1 {
		t.Errorf("run2: expected 1 grpc link added (idempotent replace), got %d", grpcCount2)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g5-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var grpcLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			grpcLinks = append(grpcLinks, l)
		}
	}
	if len(grpcLinks) != 1 {
		t.Errorf("expected exactly 1 grpc link after 2 runs, got %d", len(grpcLinks))
	}
}
