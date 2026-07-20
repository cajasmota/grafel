package mcp

// mro_3833_test.go — value-asserting coverage for MRO-aware get_source /
// inspect (#3833). These assert the DEFINING-class resolution, not len>0:
//
//  1. get_source on a DRF inherited `retrieve` synthetic resolves to the
//     RetrieveModelMixin contract (defining_class + default_status 200) via the
//     baseknowledge pack, NOT the subclass file.
//  2. inspect on the same synthetic surfaces inheritance{defining_class,
//     resolved, external} so the consumer knows it is inherited and from where.
//  3. get_source on an in-repo inherited member (base class indexed in the
//     repo) returns the BASE's real body via the EXTENDS walk.
//  4. An unresolvable inherited member returns the honest-unresolved section
//     (no fabricated body / status).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// drfViewSetDoc builds a graph for `class RoleViewSet(ModelViewSet)` with an
// EXTENDS edge to an external ModelViewSet and a bodyless DRF synthetic
// `retrieve` operation (exactly what the engine emits).
func drfViewSetDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "vs", Name: "RoleViewSet", QualifiedName: "RoleViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "views.py", StartLine: 10, EndLine: 20, Language: "python",
			},
			// Bodyless DRF synthetic for the inherited retrieve verb.
			graph.Entity{
				ID: "op_retrieve", Name: "RoleViewSet.retrieve", QualifiedName: "RoleViewSet.retrieve",
				Kind: "SCOPE.Operation", Subtype: "method", Language: "python",
				SourceFile: "views.py", StartLine: 0, EndLine: 0,
				Signature: "def retrieve(self, request, *args, **kwargs)",
			}.WithProperties(map[string]string{
				"pattern_type":      "drf_viewset_implicit_method",
				"viewset_class":     "RoleViewSet",
				"inherited_from":    "rest_framework",
				"drf_method_origin": "retrieve",
			},
			),
			// External base stub (SCOPE.External "ext:" placeholder).
			{
				ID: "ext_modelviewset", Name: "ModelViewSet", Kind: "SCOPE.External",
				SourceFile: "", Language: "python",
			},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{
				ID: "e1", FromID: "vs", ToID: "ext_modelviewset", Kind: "EXTENDS",
			}.WithProperties(map[string]string{
				"language":  "python",
				"base_name": "rest_framework.viewsets.ModelViewSet",
			},
			),
		},
	}
}

// TestGetSource_DRFInheritedRetrieve_ResolvesToMixinContract — #3833 case 1.
func TestGetSource_DRFInheritedRetrieve_ResolvesToMixinContract(t *testing.T) {
	srv := newTestServer(t, drfViewSetDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "op_retrieve"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	// MUST identify the defining mixin, not the subclass file.
	if !strings.Contains(text, "RetrieveModelMixin") {
		t.Errorf("expected defining class RetrieveModelMixin in output, got:\n%s", text)
	}
	// MUST carry the pack's default status 200 for retrieve.
	if !strings.Contains(text, "default_status: 200") {
		t.Errorf("expected default_status: 200 from pack, got:\n%s", text)
	}
	// MUST be explicit that the body is NOT the subclass source.
	if !strings.Contains(text, "NOT subclass source") {
		t.Errorf("expected explicit not-subclass marker, got:\n%s", text)
	}
	// MUST NOT pretend to be reading views.py (the subclass file).
	if strings.Contains(text, "views.py") {
		t.Errorf("output should not reference the subclass file views.py:\n%s", text)
	}
}

// TestInspect_DRFInheritedRetrieve_SurfacesDefiningClass — #3833 case 2.
func TestInspect_DRFInheritedRetrieve_SurfacesDefiningClass(t *testing.T) {
	srv := newTestServer(t, drfViewSetDoc())
	out := callInspect(t, srv, "op_retrieve")
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
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "RetrieveModelMixin") {
		t.Errorf("expected defining_class RetrieveModelMixin, got %q", dc)
	}
	if inh["external"] != true {
		t.Errorf("expected external=true, got %v", inh["external"])
	}
	// default_status round-trips as float64 through JSON.
	if ds, _ := inh["default_status"].(float64); int(ds) != 200 {
		t.Errorf("expected default_status 200, got %v", inh["default_status"])
	}
}

// TestGetSource_InRepoInheritedMember_ReturnsBaseBody — #3833 case 3. The base
// class is declared in the indexed repo; get_source on the inherited verb must
// return the BASE's real body via the EXTENDS walk.
func TestGetSource_InRepoInheritedMember_ReturnsBaseBody(t *testing.T) {
	dir := t.TempDir()
	baseSrc := "" +
		"class BaseService:\n" +
		"    def handle(self, request):\n" +
		"        return self.process(request)  # BASE_BODY_MARKER\n"
	if err := os.WriteFile(filepath.Join(dir, "base.py"), []byte(baseSrc), 0o644); err != nil {
		t.Fatalf("write base.py: %v", err)
	}
	srv := newTestServer(t, inRepoBaseDoc())
	// Repoint the test repo at our temp dir so readSourceWindow finds base.py.
	srv.State.groups["test"].Repos["repo1"].Path = dir

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "child_handle"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %v", res.Content)
	}
	text := extractResultText(t, res)

	if !strings.Contains(text, "BASE_BODY_MARKER") {
		t.Errorf("expected base body (BASE_BODY_MARKER) via EXTENDS walk, got:\n%s", text)
	}
	if !strings.Contains(text, "INHERITED") || !strings.Contains(text, "BaseService") {
		t.Errorf("expected inherited-from-BaseService header, got:\n%s", text)
	}
}

// inRepoBaseDoc: ChildService(BaseService) where BaseService.handle is a real
// in-repo method body and ChildService.handle is a bodyless inherited member.
func inRepoBaseDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "base", Name: "BaseService", QualifiedName: "BaseService",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "base.py",
				StartLine: 1, EndLine: 3, Language: "python"},
			{ID: "base_handle", Name: "BaseService.handle", QualifiedName: "BaseService.handle",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.py",
				StartLine: 2, EndLine: 3, Language: "python"},
			{ID: "child", Name: "ChildService", QualifiedName: "ChildService",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "child.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			// Bodyless inherited member on the child.
			{ID: "child_handle", Name: "ChildService.handle", QualifiedName: "ChildService.handle",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "child.py",
				StartLine: 0, EndLine: 0, Language: "python",
				Signature: "def handle(self, request)"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "e1", FromID: "child", ToID: "base", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "BaseService"}),
		},
	}
}

// TestInspect_UnresolvableInheritedMember_HonestUnresolved — #3833 case 4. A
// bodyless member whose base is neither indexed nor in the pack must surface as
// resolved=false with a note, and get_source must NOT fabricate a body.
func TestInspect_UnresolvableInheritedMember_HonestUnresolved(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "cls", Name: "MysteryView", QualifiedName: "MysteryView",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "m.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			graph.Entity{ID: "op", Name: "MysteryView.frobnicate", QualifiedName: "MysteryView.frobnicate",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "m.py",
				StartLine: 0, EndLine: 0, Language: "python",
				Signature: "def frobnicate(self)",
			}.WithProperties(map[string]string{
				"pattern_type":      "drf_viewset_implicit_method",
				"viewset_class":     "MysteryView",
				"drf_method_origin": "frobnicate",
			}),
			{ID: "ext", Name: "WeirdBase", Kind: "SCOPE.External", Language: "python"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "e1", FromID: "cls", ToID: "ext", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "some.unknown.WeirdBase"}),
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "op")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for unresolved member, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != false {
		t.Errorf("expected resolved=false, got %v", inh["resolved"])
	}
	if _, hasDC := inh["defining_class"]; hasDC {
		t.Errorf("unresolved member must NOT carry a defining_class, got %v", inh["defining_class"])
	}
	if note, _ := inh["note"].(string); note == "" {
		t.Errorf("expected an honest-unresolved note, got empty")
	}

	// get_source must not fabricate an external body for the unresolved member.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "op"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		return // an error (e.g. file not found) is acceptable — no fabrication
	}
	text := extractResultText(t, res)
	if strings.Contains(text, "synthesized inherited-member contract") {
		t.Errorf("unresolved member must not get a fabricated external body:\n%s", text)
	}
}
