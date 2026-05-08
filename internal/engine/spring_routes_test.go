package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// sampleSpringController exercises @GetMapping / @PostMapping / @PutMapping /
// @DeleteMapping / @PatchMapping / @RequestMapping. Each annotation has a
// handler method on the following line.
const sampleSpringController = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import java.util.List;

@RestController
@RequestMapping("/api")
public class OrderController {

    @GetMapping("/orders")
    public List<Order> listOrders() {
        return null;
    }

    @PostMapping("/orders")
    public Order createOrder(@RequestBody Order o) {
        return o;
    }

    @PutMapping("/orders/{id}")
    public Order updateOrder(@PathVariable Long id, @RequestBody Order o) {
        return o;
    }

    @DeleteMapping("/orders/{id}")
    public void deleteOrder(@PathVariable Long id) {
    }

    @PatchMapping("/orders/{id}")
    public Order patchOrder(@PathVariable Long id) {
        return null;
    }

    @RequestMapping(value = "/legacy", method = RequestMethod.GET)
    public String legacy() {
        return "ok";
    }
}
`

// TestDetect_SpringRoutes verifies that Spring MVC route composition
// produces fully-qualified paths by combining the class-level
// @RequestMapping prefix with each method-level verb annotation.
//
// Issue #67: previously the YAML regex rules emitted orphan flat Routes
// (`Route:/api` + `Route:/orders`); the AST pass now composes them into
// `Route:/api/orders` and drops the orphans.
func TestDetect_SpringRoutes(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "src/main/java/com/example/api/OrderController.java",
		Content:  []byte(sampleSpringController),
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Expected composed route paths. The class is annotated with
	// @RequestMapping("/api") so every handler path is prefixed.
	expectedPaths := map[string]bool{
		"/api/orders":      false, // @GetMapping + @PostMapping
		"/api/orders/{id}": false, // @PutMapping + @DeleteMapping + @PatchMapping
		"/api/legacy":      false, // @RequestMapping(value = "/legacy", ...)
	}
	// Forbidden orphan routes — must be replaced by composed versions.
	forbidden := map[string]bool{
		"/api":         true,
		"/orders":      true,
		"/orders/{id}": true,
		"/legacy":      true,
	}
	for _, e := range result.Entities {
		if e.Kind != "Route" {
			continue
		}
		if _, ok := expectedPaths[e.Name]; ok {
			expectedPaths[e.Name] = true
		}
		if forbidden[e.Name] {
			t.Errorf("orphan Route %q should have been replaced by composed form", e.Name)
		}
	}
	for path, seen := range expectedPaths {
		if !seen {
			t.Errorf("expected composed Route %q, not found", path)
		}
	}

	// Expected ROUTES_TO relationships: one per @*Mapping handler.
	type rel struct{ from, to string }
	expectedRels := map[rel]bool{
		{"Route:/api/orders", "Controller:listOrders"}:       false,
		{"Route:/api/orders", "Controller:createOrder"}:      false,
		{"Route:/api/orders/{id}", "Controller:updateOrder"}: false,
		{"Route:/api/orders/{id}", "Controller:deleteOrder"}: false,
		{"Route:/api/orders/{id}", "Controller:patchOrder"}:  false,
		{"Route:/api/legacy", "Controller:legacy"}:           false,
	}
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		key := rel{r.FromID, r.ToID}
		if _, ok := expectedRels[key]; ok {
			expectedRels[key] = true
		}
	}
	for k, seen := range expectedRels {
		if !seen {
			t.Errorf("expected ROUTES_TO relationship %s -> %s, not found", k.from, k.to)
		}
	}

	// Sanity: every ROUTES_TO emitted for this file should be ast_driven
	// (the AST pass replaced the YAML edges).
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		if r.Properties["pattern_type"] != "ast_driven" {
			t.Errorf("ROUTES_TO %s -> %s: pattern_type = %q, want ast_driven",
				r.FromID, r.ToID, r.Properties["pattern_type"])
		}
	}

	// Property checks on composed Route entities.
	for _, e := range result.Entities {
		if e.Kind != "Route" {
			continue
		}
		if e.Language != "java" {
			t.Errorf("route %q: Language = %q, want java", e.Name, e.Language)
		}
		if e.Properties["pattern_type"] != "ast_driven" {
			t.Errorf("route %q: pattern_type = %q, want ast_driven", e.Name, e.Properties["pattern_type"])
		}
	}
}
