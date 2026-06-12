package mcp

// mro_method_override_4890_test.go — value-asserting coverage for the
// method/Operation-node override path (#4890).
//
// #3973 fixed get_source on the inherited ENDPOINT node (http_endpoint). But
// the rewrite agent ALSO navigates to the METHOD/Operation node itself
// (SCOPE.Operation:ViewSet.create), and there the regression persists: a DRF
// CRUD method that the ViewSet OVERRIDES (its own `def create(self, ...)` body)
// still resolved as INHERITED and get_source returned the synthesized
// inherited-mixin contract — the base class's behaviour — instead of the
// override's own source span. That produced ~14 FALSE rewrite-agent audit
// findings (clients/schedule/groups/inspections/document-templates/jurisdictions).
//
// Root cause: classifyMember reported bodyless=true UNCONDITIONALLY for any
// entity carrying pattern_type=drf_viewset_implicit_method. When that marker
// leaks onto a REAL override node (regex miss on `async def`, an ID collision
// between the engine synthetic and the extractor's real method merging
// properties, etc.), the explicit-body gate in resolveMember was skipped and
// the node was walked up EXTENDS to the external mixin and synthesized.
//
// The fix: a node with a REAL source span is an OVERRIDE and resolves as
// EXPLICIT regardless of the synthetic marker — get_source returns its own
// body. The synthesis is preserved ONLY for a truly inherited, bodyless node.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// overriddenMethodDoc builds a ClientViewSet(ModelViewSet) whose `create`
// method is OVERRIDDEN in the class body (a real SCOPE.Operation node with a
// real span). To model the upstream property-leak that caused #4890, the
// override node ALSO carries pattern_type=drf_viewset_implicit_method — the
// marker that previously forced it down the inherited-synthesis path. The
// external ModelViewSet/CreateModelMixin base is known to the baseknowledge
// pack, so a mis-resolution would synthesize the mixin contract.
func overriddenMethodDoc(t *testing.T, dir string) *graph.Document {
	t.Helper()
	src := "" +
		"class ClientViewSet(ModelViewSet):\n" +
		"    def create(self, request, *args, **kwargs):\n" +
		"        # OVERRIDE_CREATE_MARKER: custom client-creation logic\n" +
		"        return super().create(request, *args, **kwargs)\n"
	if err := os.WriteFile(filepath.Join(dir, "clients.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write clients.py: %v", err)
	}
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "vs", Name: "ClientViewSet", QualifiedName: "ClientViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "clients.py",
				StartLine: 1, EndLine: 4, Language: "python"},
			// The OVERRIDE method node: real body at lines 2-4, but carrying the
			// implicit-method marker (the leak under test).
			{ID: "op_create", Name: "ClientViewSet.create", QualifiedName: "ClientViewSet.create",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "clients.py",
				StartLine: 2, EndLine: 4, Language: "python",
				Signature: "def create(self, request, *args, **kwargs)",
				Properties: map[string]string{
					"pattern_type":      "drf_viewset_implicit_method",
					"viewset_class":     "ClientViewSet",
					"inherited_from":    "rest_framework",
					"drf_method_origin": "create",
				}},
			{ID: "ext_modelviewset", Name: "ModelViewSet", Kind: "SCOPE.External", Language: "python"},
		},
		Relationships: []graph.Relationship{
			{ID: "e1", FromID: "vs", ToID: "ext_modelviewset", Kind: "EXTENDS",
				Properties: map[string]string{
					"language":  "python",
					"base_name": "rest_framework.viewsets.ModelViewSet",
				}},
		},
	}
}

// TestGetSource_OverriddenMethod_ReturnsOwnBody — #4890 primary case.
// get_source on the overridden create method MUST return the override's own
// source span (OVERRIDE_CREATE_MARKER), NOT the synthesized inherited mixin
// contract.
func TestGetSource_OverriddenMethod_ReturnsOwnBody(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, overriddenMethodDoc(t, dir))
	srv.State.groups["test"].Repos["repo1"].Path = dir

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "op_create"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	// Must return the override's own real body.
	if !strings.Contains(text, "OVERRIDE_CREATE_MARKER") {
		t.Errorf("expected the override's own body (OVERRIDE_CREATE_MARKER), got:\n%s", text)
	}
	// Must NOT synthesize an inherited mixin contract.
	if strings.Contains(text, "INHERITED") || strings.Contains(text, "NOT subclass source") ||
		strings.Contains(text, "CreateModelMixin") || strings.Contains(text, "default_status") {
		t.Errorf("overridden method must NOT be redirected to a mixin synthesis:\n%s", text)
	}
}

// TestResolveMember_OverriddenMethod_IsExplicit — unit-level guard: a real-body
// node resolves provExplicit even with the implicit-method marker.
func TestResolveMember_OverriddenMethod_IsExplicit(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, overriddenMethodDoc(t, dir))
	srv.State.groups["test"].Repos["repo1"].Path = dir
	lr := srv.State.groups["test"].Repos["repo1"]

	e := lr.LabelIndex.ByID["op_create"]
	if e == nil {
		t.Fatal("op_create entity not found")
	}
	res := resolveMember(lr, e)
	if res.IsInherited() {
		t.Errorf("overridden method (real body) must resolve as explicit, got provenance=%q", res.Provenance)
	}
}

// trulyInheritedMethodDoc builds a ProductViewSet(ModelViewSet) whose `create`
// is NOT overridden — the engine emits a BODYLESS implicit-method synthetic
// (no StartLine span). This is the genuinely-inherited case the synthesis is
// preserved for.
func trulyInheritedMethodDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "vs", Name: "ProductViewSet", QualifiedName: "ProductViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "products.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			// Bodyless synthetic: NO StartLine/EndLine span — truly inherited.
			{ID: "op_create", Name: "ProductViewSet.create", QualifiedName: "ProductViewSet.create",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "products.py",
				Language: "python",
				Signature: "def create(self, request, *args, **kwargs)",
				Properties: map[string]string{
					"pattern_type":      "drf_viewset_implicit_method",
					"viewset_class":     "ProductViewSet",
					"inherited_from":    "rest_framework",
					"drf_method_origin": "create",
				}},
			{ID: "ext_modelviewset", Name: "ModelViewSet", Kind: "SCOPE.External", Language: "python"},
		},
		Relationships: []graph.Relationship{
			{ID: "e1", FromID: "vs", ToID: "ext_modelviewset", Kind: "EXTENDS",
				Properties: map[string]string{
					"language":  "python",
					"base_name": "rest_framework.viewsets.ModelViewSet",
				}},
		},
	}
}

// TestResolveMember_TrulyInheritedMethod_StaysInherited — NEGATIVE guard: a
// bodyless synthetic (no override) must STILL resolve as inherited so the
// mixin-synthesis path is preserved. The fix must not break the genuine case.
func TestResolveMember_TrulyInheritedMethod_StaysInherited(t *testing.T) {
	srv := newTestServer(t, trulyInheritedMethodDoc())
	lr := srv.State.groups["test"].Repos["repo1"]

	e := lr.LabelIndex.ByID["op_create"]
	if e == nil {
		t.Fatal("op_create entity not found")
	}
	res := resolveMember(lr, e)
	if !res.IsInherited() {
		t.Errorf("truly-inherited bodyless method must resolve as inherited, got provenance=%q", res.Provenance)
	}
}
