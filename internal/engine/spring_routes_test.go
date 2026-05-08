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

// TestDetect_SpringRoutes verifies that the spring_mvc.yaml rules emit Route
// entities for @GetMapping / @PostMapping / @PutMapping / @DeleteMapping /
// @PatchMapping / @RequestMapping annotations and ROUTES_TO relationships
// pointing to the handler methods.
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

	// Expected route paths from the six annotated handlers.
	expectedPaths := map[string]bool{
		"/orders":      false, // @GetMapping + @PostMapping (deduped to one Route entity)
		"/orders/{id}": false, // @PutMapping + @DeleteMapping + @PatchMapping (deduped)
		"/legacy":      false, // @RequestMapping
		"/api":         false, // class-level @RequestMapping
	}
	for _, e := range result.Entities {
		if e.Kind != "Route" {
			continue
		}
		if _, ok := expectedPaths[e.Name]; ok {
			expectedPaths[e.Name] = true
		}
	}
	for path, seen := range expectedPaths {
		if !seen {
			t.Errorf("expected Route entity with name %q, not found", path)
		}
	}

	// Expected ROUTES_TO relationships: one per @*Mapping handler annotation.
	type rel struct{ from, to string }
	expectedRels := map[rel]bool{
		{"Route:/orders", "Controller:listOrders"}:       false,
		{"Route:/orders", "Controller:createOrder"}:      false,
		{"Route:/orders/{id}", "Controller:updateOrder"}: false,
		{"Route:/orders/{id}", "Controller:deleteOrder"}: false,
		{"Route:/orders/{id}", "Controller:patchOrder"}:  false,
		{"Route:/legacy", "Controller:legacy"}:           false,
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

	// Sanity: at least one ROUTES_TO carries the yaml_driven pattern_type.
	var found bool
	for _, r := range result.Relationships {
		if r.Kind == "ROUTES_TO" && r.Properties["pattern_type"] == "yaml_driven" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one yaml_driven ROUTES_TO relationship")
	}

	// Property checks on Route entities.
	for _, e := range result.Entities {
		if e.Kind != "Route" {
			continue
		}
		if e.Language != "java" {
			t.Errorf("route %q: Language = %q, want java", e.Name, e.Language)
		}
		if e.Properties["pattern_type"] != "yaml_driven" {
			t.Errorf("route %q: pattern_type = %q, want yaml_driven", e.Name, e.Properties["pattern_type"])
		}
	}

}
