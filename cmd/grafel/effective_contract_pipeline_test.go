package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// effective_contract_pipeline_test.go — LAYER 1 of the deploy-9 item-4 fix
// (grafel_effective_contract returns empty on all ViewSets). These run the
// FULL indexer pipeline (cmd/grafel.Index via runIndexerOn) on real DRF
// fixtures and assert the graph the MCP tool consumes carries the data it
// needs:
//
//  1. A ROUTER-REGISTERED ModelViewSet stamps the per-verb effective contract
//     (effective_kind / effective_source_class / effective_status /
//     effective_error_statuses) onto its router-expanded route entities — the
//     props the tool projects.
//  2. An UNROUTED ModelViewSet emits the ViewSet class declaration + its
//     EXTENDS edge to ModelViewSet but NO router-expanded routes — the exact
//     live-daemon shape where the tool used to return empty and where the
//     class-fallback (LAYER 2) must synthesize the contract from EXTENDS+pack.

// routerExpandedRoutes returns the router-expanded route entities in doc.
func routerExpandedRoutes(doc *graph.Document) []graph.Entity {
	var out []graph.Entity
	for _, e := range doc.Entities {
		if e.PropGet("pattern_type") == "drf_router_expanded" {
			out = append(out, e)
		}
	}
	return out
}

// TestPipeline_RoutedViewSet_StampsEffectiveContract asserts the routed fixture
// carries the stamped per-verb contract the tool projects (the data exists in
// the full-pipeline graph).
func TestPipeline_RoutedViewSet_StampsEffectiveContract(t *testing.T) {
	doc := runIndexerOn(t, "testdata/drf_attribution_fixture", "drf_routed", nil)

	routes := routerExpandedRoutes(doc)
	if len(routes) == 0 {
		t.Fatalf("expected router-expanded routes for ThingViewSet; got none")
	}

	var create *graph.Entity
	for i := range routes {
		if routes[i].PropGet("drf_view_method") == "ThingViewSet.create" {
			create = &routes[i]
			break
		}
	}
	if create == nil {
		t.Fatalf("no router-expanded route for ThingViewSet.create; routes=%v", routes)
	}
	p := create.PropsSnapshot()
	if p["effective_kind"] != "inherited" {
		t.Errorf("create effective_kind=%q; want inherited", p["effective_kind"])
	}
	if p["effective_source_class"] != "CreateModelMixin" {
		t.Errorf("create effective_source_class=%q; want CreateModelMixin", p["effective_source_class"])
	}
	if p["effective_status"] != "201" {
		t.Errorf("create effective_status=%q; want 201", p["effective_status"])
	}
	if p["effective_error_statuses"] != "400" {
		t.Errorf("create effective_error_statuses=%q; want 400 (#278)", p["effective_error_statuses"])
	}
}

// TestPipeline_UnroutedViewSet_NoRoutesButExtendsEdge asserts the unrouted
// fixture is the live-daemon empty shape: a ViewSet class + EXTENDS-to-
// ModelViewSet, but zero router-expanded routes. This is the data the
// class-fallback resolves from.
func TestPipeline_UnroutedViewSet_NoRoutesButExtendsEdge(t *testing.T) {
	doc := runIndexerOn(t, "testdata/drf_unrouted_viewset_fixture", "drf_unrouted", nil)

	if rs := routerExpandedRoutes(doc); len(rs) != 0 {
		t.Fatalf("expected NO router-expanded routes for an unrouted ViewSet; got %d: %+v", len(rs), rs)
	}

	// The ViewSet class declaration must be indexed.
	var cls *graph.Entity
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Name == "WidgetViewSet" {
			cls = e
			break
		}
	}
	if cls == nil {
		t.Fatalf("WidgetViewSet class entity not indexed")
	}

	// And it must carry an EXTENDS edge whose base resolves to ModelViewSet.
	found := false
	for _, r := range doc.Relationships {
		if r.Kind != "EXTENDS" || r.FromID != cls.ID {
			continue
		}
		base := r.PropGet("base_name")
		if strings.Contains(base, "ModelViewSet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WidgetViewSet has no EXTENDS edge to ModelViewSet — class-fallback would have nothing to resolve")
	}
}
