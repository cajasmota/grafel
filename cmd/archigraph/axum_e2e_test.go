package main

import (
	"fmt"
	"os"
	"testing"
)

// TestAxumE2E_PricingService indexes the ShipFast pricing service and verifies
// that axum route extraction emits http_endpoint_definition for POST /quote.
// This is the before→after proof for issue #1420.
//
// Note: entities in a graph.Document use stamped (hashed) IDs; the Name field
// retains the canonical synthetic ID (e.g. "http:POST:/quote").
func TestAxumE2E_PricingService(t *testing.T) {
	const pricingPath = "/Users/jorgecajas/Documents/Projects/polyglot-platform/services/pricing"
	if _, err := os.Stat(pricingPath); err != nil {
		t.Skipf("requires local Axum fixture at %s: %v", pricingPath, err)
	}
	doc := runIndexerOn(t, pricingPath, "pricing", nil)

	// Collect by Name (canonical ID) not by stamped hash ID.
	var defs, calls []string
	for _, e := range doc.Entities {
		switch e.Kind {
		case "http_endpoint_definition":
			defs = append(defs, e.Name)
		case "http_endpoint_call":
			calls = append(calls, e.Name)
		}
	}

	t.Logf("http_endpoint_definition count: %d", len(defs))
	for _, name := range defs {
		t.Logf("  definition name: %s", name)
	}
	t.Logf("http_endpoint_call count: %d", len(calls))

	wantDef := "http:POST:/quote"
	found := false
	for _, name := range defs {
		if name == wantDef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pricing service: missing http_endpoint_definition %q (got names: %v)", wantDef, defs)
	}

	// Verify framework=axum property on the definition entity.
	for _, e := range doc.Entities {
		if e.Kind == "http_endpoint_definition" && e.Name == wantDef {
			fw := e.Properties["framework"]
			t.Logf("  %s: framework=%s", e.Name, fw)
			if fw != "axum" {
				t.Errorf("expected framework=axum, got %q", fw)
			}
		}
	}

	fmt.Printf("\n=== axum E2E before→after ===\n")
	fmt.Printf("Before (#1420): http_endpoint_definition for services/pricing = 0 (axum not supported)\n")
	fmt.Printf("After  (#1420): http_endpoint_definition for services/pricing = %d\n", len(defs))
	for _, name := range defs {
		fmt.Printf("  %s\n", name)
	}
	fmt.Printf("=== end axum E2E ===\n\n")
}

// TestAxumE2E_OrdersCaller indexes the orders service (Python/httpx) and
// verifies the POST /quote consumer-side call entity is present.
func TestAxumE2E_OrdersCaller(t *testing.T) {
	const ordersPath = "/Users/jorgecajas/Documents/Projects/polyglot-platform/services/orders"
	if _, err := os.Stat(ordersPath); err != nil {
		t.Skipf("requires local Axum fixture at %s: %v", ordersPath, err)
	}
	doc := runIndexerOn(t, ordersPath, "orders", nil)

	// Collect by Name (canonical ID) not by stamped hash ID.
	var calls []string
	for _, e := range doc.Entities {
		if e.Kind == "http_endpoint_call" || e.Kind == "http_endpoint" {
			calls = append(calls, e.Name)
		}
	}

	t.Logf("http_endpoint_call names from orders: %d", len(calls))
	for _, name := range calls {
		t.Logf("  call: %s", name)
	}

	wantCall := "http:POST:/quote"
	found := false
	for _, name := range calls {
		if name == wantCall {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("orders service: missing http_endpoint_call %q (got: %v)", wantCall, calls)
	}
}
