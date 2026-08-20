package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6429 / #6374 (Spring's share) — Java Spring MVC's route→handler hop must
// BIND, not dangle.
//
// Before this fix, spring_routes.go emitted `Route:<path> -> Controller:<method>`
// and threw the method name away, and synthesizeSpringFromComposed stamped
// `source_handler = "Route:<path>"` — the endpoint pointing at its own route
// path rather than at any handler. Net effect: the ROUTES_TO edge dangled and
// Spring got no IMPLEMENTS edge at all, while refs.go accounted the dangle as
// DispositionDynamic so the resolver-bug rate never showed it.
//
// Kotlin (spring_routes_kotlin.go) already stamped
// `source_handler = "Controller:<methodName>"`; this is the Java side of that
// asymmetry.

const springHandlerBinding6429Src = `package com.example.api;

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
}
`

const springHandlerBinding6429Path = "src/main/java/com/example/api/OrderController.java"

// TestSpring6429_RoutesToTargetsResolvedOperation proves the AST-composed
// ROUTES_TO edge points at the QUALIFIED handler Operation the Java extractor
// actually lands (`SCOPE.Operation:OrderController.listOrders`) rather than at
// the `Controller:<bareMethod>` stub that never resolved.
func TestSpring6429_RoutesToTargetsResolvedOperation(t *testing.T) {
	_, rels := detectInline(t, "java", springHandlerBinding6429Path, springHandlerBinding6429Src)

	want := map[string]string{
		"Route:/api/orders": "SCOPE.Operation:OrderController.listOrders",
	}
	seen := map[string][]string{}
	for _, r := range rels {
		if r.Kind != "ROUTES_TO" || !strings.HasPrefix(r.FromID, "Route:/api") {
			continue
		}
		if r.Properties.Get("pattern_type") != "ast_driven" {
			continue
		}
		seen[r.FromID] = append(seen[r.FromID], r.ToID)
		if strings.HasPrefix(r.ToID, "Controller:") {
			t.Errorf("#6429: AST-composed ROUTES_TO %s still targets the dangling stub %q", r.FromID, r.ToID)
		}
	}
	for from, wantTo := range want {
		found := false
		for _, got := range seen[from] {
			if got == wantTo {
				found = true
			}
		}
		if !found {
			t.Errorf("#6429: ROUTES_TO from %s: want ToID %q, got %v", from, wantTo, seen[from])
		}
	}
	if len(seen) == 0 {
		t.Fatalf("#6429: no ast_driven ROUTES_TO edges emitted at all")
	}
}

// TestSpring6429_EndpointSourceHandlerIsController proves the synthesized
// http_endpoint_definition names the HANDLER, not its own route path.
func TestSpring6429_EndpointSourceHandlerIsController(t *testing.T) {
	ents, _ := detectInline(t, "java", springHandlerBinding6429Path, springHandlerBinding6429Src)

	for _, verbPath := range [][2]string{{"GET", "/api/orders"}, {"POST", "/api/orders"}} {
		ep := endpointByVerbPath(ents, verbPath[0], verbPath[1])
		if ep == nil {
			t.Fatalf("#6429: endpoint %s %s not emitted", verbPath[0], verbPath[1])
		}
		got := ep.Properties["source_handler"]
		if strings.HasPrefix(got, "Route:") {
			t.Errorf("#6429: endpoint %s %s stamps source_handler=%q — the endpoint points at its own route path, not a handler",
				verbPath[0], verbPath[1], got)
		}
	}
	if got := endpointByVerbPath(ents, "GET", "/api/orders").Properties["source_handler"]; got != "Controller:listOrders" {
		t.Errorf("#6429: GET /api/orders source_handler = %q, want %q", got, "Controller:listOrders")
	}
	if got := endpointByVerbPath(ents, "POST", "/api/orders").Properties["source_handler"]; got != "Controller:createOrder" {
		t.Errorf("#6429: POST /api/orders source_handler = %q, want %q", got, "Controller:createOrder")
	}
}

// TestSpring6429_ImplementsEdgeReachesHandler is the #6374 gate: with the
// handler Operation present in the same file (as the Java symbol extractor
// lands it, qualified), the existing http_endpoint_resolve binder must produce
// a resolved IMPLEMENTS edge handler → endpoint.
func TestSpring6429_ImplementsEdgeReachesHandler(t *testing.T) {
	ents, rels := detectInline(t, "java", springHandlerBinding6429Path, springHandlerBinding6429Src)

	// Inject the same-file handler Operations the base Java extractor produces.
	for _, m := range []string{"OrderController.listOrders", "OrderController.createOrder"} {
		ents = append(ents, types.EntityRecord{
			Kind:       "SCOPE.Operation",
			Name:       m,
			Subtype:    "method",
			SourceFile: springHandlerBinding6429Path,
			Language:   "java",
		})
	}

	merged, stats := ResolveHTTPEndpointHandlers(ents)
	if stats.HandlerResolved == 0 {
		t.Fatalf("#6374(spring): no Spring handler resolved (resolved=%d dropped=%d nohandler=%d)",
			stats.HandlerResolved, stats.HandlerDropped, stats.NoHandlerProp)
	}
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}

	idOf := func(kind, name string) string {
		for i := range merged {
			if merged[i].Kind == kind && merged[i].Name == name {
				return merged[i].ID
			}
		}
		return ""
	}

	for _, tc := range []struct{ verb, path, handler string }{
		{"GET", "/api/orders", "OrderController.listOrders"},
		{"POST", "/api/orders", "OrderController.createOrder"},
	} {
		ep := endpointByVerbPath(merged, tc.verb, tc.path)
		if ep == nil {
			t.Fatalf("#6374(spring): endpoint %s %s lost after resolve", tc.verb, tc.path)
		}
		handlerID := idOf("SCOPE.Operation", tc.handler)
		if handlerID == "" {
			t.Fatalf("#6374(spring): handler %q missing from merged set", tc.handler)
		}
		// Embedded IMPLEMENTS edges live on the handler; resolve them.
		var all []types.RelationshipRecord
		all = append(all, rels...)
		for i := range merged {
			all = append(all, merged[i].Relationships...)
		}
		idx := resolve.BuildIndex(merged)
		resolve.References(all, idx)

		found := false
		for _, r := range all {
			if r.Kind == implementsEdgeKind && r.FromID == handlerID && r.ToID == ep.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("#6374(spring): no resolved IMPLEMENTS edge %s -> %s %s", tc.handler, tc.verb, tc.path)
		}
	}
}
