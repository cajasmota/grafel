package mcp

// effective_contract_mro_3964_test.go — LAYER 2 of the deploy-9 item-4 fix:
// grafel_effective_contract returned EMPTY on every DRF ViewSet because it
// read ONLY the engine-stamped effective_* props on router-expanded routes, and
// on a real index those routes either (a) lacked the stamped contract fields, or
// (b) were not emitted at all for a ViewSet whose routing the expansion pass did
// not materialise — even though the SAME contract is MRO-resolvable from the
// ViewSet's EXTENDS edges + the baseknowledge pack (the path get_source uses and
// that the report confirmed WORKS).
//
// These tests load graphs THROUGH graph.fb (the daemon's real load path) and
// call handleEffectiveContract directly. The class-fallback test replicates the
// EXACT full-pipeline shape captured from cmd/grafel (Kind="View",
// subtype="", an EXTENDS edge base_name="viewsets.ModelViewSet", and ZERO
// router-expanded routes) — the live-daemon empty case. It MUST fail before the
// fix and pass after.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callEffContractFB writes doc to graph.fb, loads it back via the production
// loader, builds a server, and calls the handler — exercising the full daemon
// serving path (FB round-trip + handler).
func callEffContractFB(t *testing.T, doc *graph.Document, entityID string) effectiveContractResult {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write fb: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("load fb: %v", err)
	}
	srv := newTestServer(t, loaded)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": entityID}
	res, err := srv.handleEffectiveContract(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler IsError: %s", resultText(res))
	}
	var out effectiveContractResult
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, resultText(res))
	}
	return out
}

// unroutedViewSetDoc replicates the full-pipeline shape of the unrouted
// WidgetViewSet fixture (verified in cmd/grafel Layer 1):
//   - the ViewSet declaration is Kind="View", subtype="" (NOT isClassEntity).
//   - it has an EXTENDS edge to an external ModelViewSet base (base_name carries
//     "viewsets.ModelViewSet"); the pack resolves the CRUD contract from it.
//   - there are ZERO router-expanded route entities.
func unroutedViewSetDoc() *graph.Document {
	return &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "widgetvs", Name: "WidgetViewSet", QualifiedName: "myapp.views.WidgetViewSet",
				Kind: "View", Subtype: "", SourceFile: "myapp/views.py", StartLine: 4, EndLine: 16,
				Language: "python"},
			{ID: "ext:viewsets", Name: "viewsets", Kind: "ExternalSymbol", Language: "python"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{FromID: "widgetvs", ToID: "ext:viewsets", Kind: "EXTENDS"}.WithProperties(map[string]string{"base_name": "viewsets.ModelViewSet"}),
		},
	}
}

// TestEffectiveContract_ClassFallback_UnroutedViewSet_ThroughFB is the
// regression for the deploy-9 empty bug: a ModelViewSet with NO router-expanded
// routes must still yield its inherited CRUD contract, resolved from EXTENDS +
// the baseknowledge pack — the same data get_source reads. FAILS before the
// class-fallback, PASSES after.
func TestEffectiveContract_ClassFallback_UnroutedViewSet_ThroughFB(t *testing.T) {
	out := callEffContractFB(t, unroutedViewSetDoc(), "WidgetViewSet")

	if len(out.Groups) != 1 {
		t.Fatalf("expected 1 group synthesized from EXTENDS+pack, got %d (note=%q)", len(out.Groups), out.Note)
	}
	g := out.Groups[0]
	if g.Class != "WidgetViewSet" {
		t.Errorf("group class=%q; want WidgetViewSet", g.Class)
	}
	// ModelViewSet contributes the six CRUD verbs.
	byMember := map[string]effectiveContract{}
	for _, h := range g.Handlers {
		byMember[leafAfterDot(h.Handler)] = h
	}
	for _, want := range []string{"create", "retrieve", "update", "partial_update", "destroy", "list"} {
		if _, ok := byMember[want]; !ok {
			t.Errorf("missing inherited verb %q in synthesized contract; got %v", want, g.Handlers)
		}
	}
	// The #278 fact must be present on create — resolved purely from the pack.
	create := byMember["create"]
	if create.Kind != "inherited" {
		t.Errorf("create kind=%q; want inherited", create.Kind)
	}
	if create.SourceClass == "" || leafAfterDot(create.SourceClass) != "CreateModelMixin" {
		t.Errorf("create source_class=%q; want ...CreateModelMixin", create.SourceClass)
	}
	if create.DefaultStatus != 201 {
		t.Errorf("create default_status=%d; want 201", create.DefaultStatus)
	}
	if len(create.ErrorStatuses) != 1 || create.ErrorStatuses[0] != 400 {
		t.Errorf("create error_statuses=%v; want [400] (#278)", create.ErrorStatuses)
	}
	// list is paginated per the pack.
	if !byMember["list"].Pagination {
		t.Errorf("list should be marked paginated by the pack")
	}
}

// routedViewSetThroughFBDoc replicates the routed ThingViewSet full-pipeline
// shape (Kind="http_endpoint_definition", stamped effective_* props).
func routedViewSetThroughFBDoc() *graph.Document {
	route := func(id, verb, path, dvm, kind, src, status, errs string) graph.Entity {
		p := map[string]string{
			"pattern_type":           "drf_router_expanded",
			"framework":              "django",
			"verb":                   verb,
			"path":                   path,
			"drf_view_method":        dvm,
			"effective_kind":         kind,
			"effective_source_class": src,
		}
		if status != "" {
			p["effective_status"] = status
		}
		if errs != "" {
			p["effective_error_statuses"] = errs
		}
		return graph.Entity{ID: id, Name: id, Kind: "http_endpoint_definition", Language: "python"}.WithProperties(p)
	}
	return &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			route("e1", "POST", "/api/v1/things", "ThingViewSet.create", "inherited", "CreateModelMixin", "201", "400"),
			route("e2", "GET", "/api/v1/things", "ThingViewSet.list", "explicit", "ThingViewSet", "200", ""),
		},
	}
}

// TestEffectiveContract_RoutedViewSet_ThroughFB confirms the router-expanded
// path still works end-to-end through FB (no regression of the stamped path).
func TestEffectiveContract_RoutedViewSet_ThroughFB(t *testing.T) {
	out := callEffContractFB(t, routedViewSetThroughFBDoc(), "ThingViewSet")
	if len(out.Groups) != 1 {
		t.Fatalf("expected 1 group from router-expanded routes, got %d (note=%q)", len(out.Groups), out.Note)
	}
	if len(out.Groups[0].Handlers) != 2 {
		t.Fatalf("expected 2 handlers (create,list), got %d", len(out.Groups[0].Handlers))
	}
}

// unstampedRouteDoc is a router-expanded create route MISSING the effective_*
// stamp (older index) but carrying provenance=inherited + defining_class — the
// shape get_source resolves. The tool must backfill the contract from the pack.
func unstampedRouteDoc() *graph.Document {
	return &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			graph.Entity{ID: "r1", Name: "r1", Kind: "http_endpoint_definition", Language: "python"}. // No effective_* — but the inherited provenance + defining
				// mixin ARE present (what resolveInheritedEndpoint needs).

				WithProperties(map[string]string{
					"pattern_type":    "drf_router_expanded",
					"framework":       "django",
					"verb":            "POST",
					"path":            "/api/v1/things",
					"drf_view_method": "ThingViewSet.create",

					"provenance":     "inherited",
					"defining_class": "rest_framework.mixins.CreateModelMixin",
				}),
		},
	}
}

// TestEffectiveContract_BackfillUnstampedRoute_ThroughFB asserts a router-
// expanded route with NO stamped effective_* still surfaces the full per-verb
// contract, backfilled from the MRO/pack resolution get_source uses.
func TestEffectiveContract_BackfillUnstampedRoute_ThroughFB(t *testing.T) {
	out := callEffContractFB(t, unstampedRouteDoc(), "ThingViewSet")
	if len(out.Groups) != 1 || len(out.Groups[0].Handlers) != 1 {
		t.Fatalf("expected 1 group/1 handler, got %d groups (note=%q)", len(out.Groups), out.Note)
	}
	h := out.Groups[0].Handlers[0]
	if h.Kind != "inherited" {
		t.Errorf("kind=%q; want inherited (backfilled)", h.Kind)
	}
	if h.DefaultStatus != 201 {
		t.Errorf("default_status=%d; want 201 (backfilled from pack)", h.DefaultStatus)
	}
	if len(h.ErrorStatuses) != 1 || h.ErrorStatuses[0] != 400 {
		t.Errorf("error_statuses=%v; want [400] (backfilled #278)", h.ErrorStatuses)
	}
}
