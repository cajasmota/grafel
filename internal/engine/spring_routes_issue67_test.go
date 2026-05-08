package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// TestDetect_SpringRouteComposition_Issue67 is the regression test for
// issue #67: a controller with class-level `@RequestMapping("/api")` and
// three method-level `@GetMapping` annotations must produce three composed
// Route entities (`/api/orders`, `/api/users`, `/api/items`) — not three
// orphan method paths plus a stray class-level Route.
func TestDetect_SpringRouteComposition_Issue67(t *testing.T) {
	fixturePath := filepath.Join("testdata", "issue67_api_controller.java.fixture")
	src, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "src/main/java/com/example/api/ApiController.java",
		Content:  src,
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	wantComposed := []string{"/api/orders", "/api/users", "/api/items"}
	forbidden := []string{"/api", "/orders", "/users", "/items"}

	got := make(map[string]bool)
	for _, e := range result.Entities {
		if e.Kind == "Route" {
			got[e.Name] = true
		}
	}

	for _, p := range wantComposed {
		if !got[p] {
			t.Errorf("missing composed Route %q", p)
		}
	}
	for _, p := range forbidden {
		if got[p] {
			t.Errorf("orphan Route %q must not be present after AST composition", p)
		}
	}

	// Count Route entities: must be exactly 3 (one per handler) — no
	// orphan class-level Route, no method-only duplicates.
	routeCount := 0
	for _, e := range result.Entities {
		if e.Kind == "Route" {
			routeCount++
		}
	}
	if routeCount != 3 {
		t.Errorf("Route count = %d, want 3", routeCount)
	}

	// And exactly 3 ROUTES_TO edges, all composed and ast_driven.
	type rel struct{ from, to string }
	wantRels := map[rel]bool{
		{"Route:/api/orders", "Controller:listOrders"}: false,
		{"Route:/api/users", "Controller:listUsers"}:   false,
		{"Route:/api/items", "Controller:listItems"}:   false,
	}
	routesToCount := 0
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		routesToCount++
		k := rel{r.FromID, r.ToID}
		if _, ok := wantRels[k]; ok {
			wantRels[k] = true
		}
		if r.Properties["pattern_type"] != "ast_driven" {
			t.Errorf("ROUTES_TO %s->%s pattern_type = %q, want ast_driven",
				r.FromID, r.ToID, r.Properties["pattern_type"])
		}
	}
	if routesToCount != 3 {
		t.Errorf("ROUTES_TO count = %d, want 3", routesToCount)
	}
	for k, seen := range wantRels {
		if !seen {
			t.Errorf("missing ROUTES_TO %s -> %s", k.from, k.to)
		}
	}
}

// TestDetect_SpringRoute_NoClassPrefix verifies that a controller WITHOUT
// a class-level @RequestMapping is left alone — the YAML rules continue
// to emit method-only Routes, and the AST pass does not interfere.
func TestDetect_SpringRoute_NoClassPrefix(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
public class FlatController {

    @GetMapping("/health")
    public String health() {
        return "ok";
    }
}
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "FlatController.java",
		Content:  []byte(src),
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	var seenHealth bool
	for _, e := range result.Entities {
		if e.Kind == "Route" && e.Name == "/health" {
			seenHealth = true
		}
	}
	if !seenHealth {
		t.Error("expected Route:/health from YAML rules when no class-level prefix is present")
	}
}

// TestDetect_SpringRoute_NonControllerWithRequestMapping verifies that a
// class with @RequestMapping but WITHOUT @RestController/@Controller
// (e.g. a plain @Component or unrelated class) is not subjected to AST
// composition — its methods stay as the YAML rules emit them.
func TestDetect_SpringRoute_NonControllerWithRequestMapping(t *testing.T) {
	src := `package com.example.api;

import org.springframework.stereotype.Component;
import org.springframework.web.bind.annotation.*;

@Component
@RequestMapping("/internal")
public class NotAController {

    @GetMapping("/probe")
    public String probe() {
        return "ok";
    }
}
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "NotAController.java",
		Content:  []byte(src),
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// Composition should not trigger — no @RestController/@Controller.
	for _, e := range result.Entities {
		if e.Kind == "Route" && e.Name == "/internal/probe" {
			t.Errorf("AST composition should not run on non-controller classes; got composed Route %q", e.Name)
		}
	}
}
