package mcp

// mro_endpoint_3973_test.go — value-asserting coverage for the
// endpoint→mixin MRO bridge (#3973, epic #3968).
//
// T3 (#3915) + T4 (#3946) made get_source / neighbors MRO-aware on the
// inherited-method STUB. But the rewrite agent navigates via the ENDPOINT
// (a router-expanded http_endpoint) and the ViewSet — where the stub path
// never triggered: an http_endpoint is neither a DRF synthetic op nor a
// dotted-name method, so classifyMember rejected it and get_source on the
// endpoint returned the subclass file-top (imports), not the mixin body.
//
// The inherited endpoint already carries the resolution explicitly
// (provenance:inherited + defining_class + drf_view_method), so these tests
// assert the RESOLUTION, not len>0:
//
//  1. get_source on an inherited http_endpoint (defining_class=external mixin)
//     returns the ListModelMixin contract, NOT the subclass file-top.
//  2. inspect on the same endpoint surfaces inheritance{defining_class,
//     resolved, external}.
//  3. neighbors(out) on the inherited endpoint surfaces the INHERITS hop to the
//     defining mixin contract.
//  4. get_source on an inherited endpoint whose defining class IS indexed in the
//     repo returns the BASE's real body.
//  5. NEGATIVE: an explicit (non-inherited) endpoint get_source returns its own
//     real body, unchanged — no mixin redirect.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// inheritedEndpointDoc builds a graph with a UserProfileViewSet and a
// router-expanded GET list endpoint whose `list` verb is INHERITED from the
// external rest_framework.mixins.ListModelMixin — exactly what the DRF engine
// stamps (provenance:inherited + defining_class + drf_view_method, #3831).
func inheritedEndpointDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "vs", Name: "UserProfileViewSet", QualifiedName: "UserProfileViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "user_viewset.py", StartLine: 12, EndLine: 18, Language: "python",
			},
			// Router-expanded inherited endpoint. SourceFile points at the
			// ViewSet file; StartLine is the class def line (the engine's
			// fallback for an inherited verb with no body in this file). If
			// get_source returned THIS span it would emit the subclass file-top
			// (imports), not the mixin body — that is the bug under test.
			{
				ID:         "ep_list",
				Name:       "GET /api/v1/user-profile/",
				Kind:       "http_endpoint",
				SourceFile: "user_viewset.py", StartLine: 1, EndLine: 1,
				Language: "python",
				Properties: map[string]string{
					"verb":            "GET",
					"path":            "/api/v1/user-profile/",
					"framework":       "django",
					"pattern_type":    "drf_router_expanded",
					"provenance":      "inherited",
					"defining_class":  "rest_framework.mixins.ListModelMixin",
					"drf_view_method": "UserProfileViewSet.list",
				},
			},
			{ID: "ext_modelviewset", Name: "ModelViewSet", Kind: "SCOPE.External", Language: "python"},
		},
		Relationships: []graph.Relationship{
			{
				ID: "e1", FromID: "vs", ToID: "ext_modelviewset", Kind: "EXTENDS",
				Properties: map[string]string{
					"language":  "python",
					"base_name": "rest_framework.viewsets.ModelViewSet",
				},
			},
		},
	}
}

// TestGetSource_InheritedEndpoint_ResolvesToMixinContract — #3973 case 1.
func TestGetSource_InheritedEndpoint_ResolvesToMixinContract(t *testing.T) {
	srv := newTestServer(t, inheritedEndpointDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "ep_list"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	// MUST identify the defining mixin, not the subclass file.
	if !strings.Contains(text, "ListModelMixin") {
		t.Errorf("expected defining class ListModelMixin in output, got:\n%s", text)
	}
	// MUST carry the pack's default status 200 for list.
	if !strings.Contains(text, "default_status: 200") {
		t.Errorf("expected default_status: 200 from pack, got:\n%s", text)
	}
	// MUST be explicit that the body is NOT the subclass source.
	if !strings.Contains(text, "NOT subclass source") {
		t.Errorf("expected explicit not-subclass marker, got:\n%s", text)
	}
	// MUST NOT pretend to read the subclass file (the file-top imports bug).
	if strings.Contains(text, "user_viewset.py") {
		t.Errorf("output must not reference the subclass file user_viewset.py:\n%s", text)
	}
}

// TestInspect_InheritedEndpoint_SurfacesDefiningClass — #3973 case 2.
func TestInspect_InheritedEndpoint_SurfacesDefiningClass(t *testing.T) {
	srv := newTestServer(t, inheritedEndpointDoc())
	out := callInspect(t, srv, "ep_list")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section, got keys: %v", mapKeys(out))
	}
	if inh["inherited"] != true {
		t.Errorf("expected inherited=true, got %v", inh["inherited"])
	}
	if inh["resolved"] != true {
		t.Errorf("expected resolved=true, got %v", inh["resolved"])
	}
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "ListModelMixin") {
		t.Errorf("expected defining_class ListModelMixin, got %q", dc)
	}
	if inh["external"] != true {
		t.Errorf("expected external=true, got %v", inh["external"])
	}
	if mem, _ := inh["member"].(string); mem != "list" {
		t.Errorf("expected member=list, got %q", mem)
	}
}

// TestNeighbors_InheritedEndpoint_SurfacesInheritsToMixin — #3973 case 3. The
// inherited endpoint must expose the INHERITS hop to the defining mixin
// contract via neighbors(out), so the rewrite agent navigating from the
// ENDPOINT sees the INHERITS relationship (not just the method stub).
func TestNeighbors_InheritedEndpoint_SurfacesInheritsToMixin(t *testing.T) {
	srv := newTestServer(t, inheritedEndpointDoc())
	out := callNeighbors3834(t, srv, "ep_list", "out")
	names := calleeNames3834(t, out)

	found := false
	for _, n := range names {
		if strings.Contains(n, "ListModelMixin") && strings.Contains(n, "list") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected neighbors(out) on inherited endpoint to surface the ListModelMixin.list INHERITS hop, got: %v", names)
	}
	// The hop must be flagged via_inherits and be a LEAF (external contract, no
	// fabricated callees).
	if !calleeFlaggedInherits(out, "rest_framework.mixins.ListModelMixin.list") {
		t.Errorf("expected the mixin contract flagged via_inherits=true, got: %v", out["callees"])
	}
	if len(names) != 1 {
		t.Errorf("external contract must be a leaf with no fabricated callees; got %d: %v", len(names), names)
	}
}

// inRepoInheritedEndpointDoc builds a graph whose inherited endpoint's defining
// class is declared IN the indexed repo (a project-local base ViewSet) with a
// real `list` body — get_source on the endpoint must return that base body.
func inRepoInheritedEndpointDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "base", Name: "BaseListViewSet", QualifiedName: "BaseListViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "base.py",
				StartLine: 1, EndLine: 4, Language: "python"},
			{ID: "base_list", Name: "BaseListViewSet.list", QualifiedName: "BaseListViewSet.list",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.py",
				StartLine: 2, EndLine: 4, Language: "python",
				Signature: "def list(self, request)"},
			{ID: "vs", Name: "ReportViewSet", QualifiedName: "ReportViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "views.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			{ID: "ep_list", Name: "GET /reports/", Kind: "http_endpoint",
				SourceFile: "views.py", StartLine: 1, EndLine: 1, Language: "python",
				Properties: map[string]string{
					"verb":            "GET",
					"path":            "/reports/",
					"framework":       "django",
					"pattern_type":    "drf_router_expanded",
					"provenance":      "inherited",
					"defining_class":  "BaseListViewSet",
					"drf_view_method": "ReportViewSet.list",
				}},
		},
		Relationships: []graph.Relationship{
			{ID: "e1", FromID: "vs", ToID: "base", Kind: "EXTENDS",
				Properties: map[string]string{"language": "python", "base_name": "BaseListViewSet"}},
		},
	}
}

// TestGetSource_InheritedEndpoint_InRepoBase_ReturnsBaseBody — #3973 case 4.
func TestGetSource_InheritedEndpoint_InRepoBase_ReturnsBaseBody(t *testing.T) {
	dir := t.TempDir()
	baseSrc := "" +
		"class BaseListViewSet:\n" +
		"    def list(self, request):\n" +
		"        return Response(self.get_queryset())  # BASE_LIST_MARKER\n"
	if err := os.WriteFile(filepath.Join(dir, "base.py"), []byte(baseSrc), 0o644); err != nil {
		t.Fatalf("write base.py: %v", err)
	}
	srv := newTestServer(t, inRepoInheritedEndpointDoc())
	srv.State.groups["test"].Repos["repo1"].Path = dir

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "ep_list"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	if !strings.Contains(text, "BASE_LIST_MARKER") {
		t.Errorf("expected base body (BASE_LIST_MARKER) via endpoint→base resolution, got:\n%s", text)
	}
	if !strings.Contains(text, "INHERITED") || !strings.Contains(text, "BaseListViewSet") {
		t.Errorf("expected inherited-from-BaseListViewSet header, got:\n%s", text)
	}
}

// explicitEndpointDoc builds a router-expanded endpoint whose `list` verb is
// OVERRIDDEN in the ViewSet body (provenance:explicit). get_source must return
// its own real body — the bridge must NOT redirect it.
func explicitEndpointDoc(t *testing.T, dir string) *graph.Document {
	t.Helper()
	src := "" +
		"class AuditViewSet(ModelViewSet):\n" +
		"    def list(self, request):\n" +
		"        return Response([])  # EXPLICIT_LIST_MARKER\n"
	if err := os.WriteFile(filepath.Join(dir, "audit.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write audit.py: %v", err)
	}
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "vs", Name: "AuditViewSet", QualifiedName: "AuditViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "audit.py",
				StartLine: 1, EndLine: 3, Language: "python"},
			// Explicit endpoint: real body at lines 2-3, provenance=explicit.
			{ID: "ep_list", Name: "GET /audit/", Kind: "http_endpoint",
				SourceFile: "audit.py", StartLine: 2, EndLine: 3, Language: "python",
				Properties: map[string]string{
					"verb":            "GET",
					"path":            "/audit/",
					"framework":       "django",
					"pattern_type":    "drf_router_expanded",
					"provenance":      "explicit",
					"defining_class":  "AuditViewSet",
					"drf_view_method": "AuditViewSet.list",
				}},
		},
	}
}

// TestGetSource_ExplicitEndpoint_ReturnsOwnBody — #3973 case 5 (NEGATIVE).
func TestGetSource_ExplicitEndpoint_ReturnsOwnBody(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, explicitEndpointDoc(t, dir))
	srv.State.groups["test"].Repos["repo1"].Path = dir

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "ep_list"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	// Must return the endpoint's own real body, NOT a mixin contract.
	if !strings.Contains(text, "EXPLICIT_LIST_MARKER") {
		t.Errorf("expected the explicit endpoint's own body (EXPLICIT_LIST_MARKER), got:\n%s", text)
	}
	if strings.Contains(text, "NOT subclass source") || strings.Contains(text, "ListModelMixin") {
		t.Errorf("explicit endpoint must NOT be redirected to a mixin contract:\n%s", text)
	}
}

// TestNeighbors_ExplicitEndpoint_NoInheritsHop — #3973 case 5b (NEGATIVE). An
// explicit endpoint must NOT project a synthetic INHERITS edge.
func TestNeighbors_ExplicitEndpoint_NoInheritsHop(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, explicitEndpointDoc(t, dir))
	srv.State.groups["test"].Repos["repo1"].Path = dir

	r := srv.State.groups["test"].Repos["repo1"]
	if edges := mroOutboundEdges(r, "ep_list"); len(edges) != 0 {
		t.Errorf("explicit endpoint must project NO INHERITS edge, got: %v", edges)
	}
}
