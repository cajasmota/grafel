package mcp

// mro_traverse_3834_test.go — value-asserting coverage for MRO-aware
// neighbors / def_use / trace (#3834, epic #3829 MRO T4).
//
// These assert the TRAVERSAL REACHES the defining implementation, not len>0:
//
//  1. neighbors(out) on an in-repo inherited stub (ChildService.handle) reaches
//     the BASE method's callees (BaseService.handle -> process) — the stub's own
//     CALLS edges are empty, so without the INHERITS hop this dead-ends.
//  2. trace from the inherited stub reaches the BASE method body via the
//     INHERITS edge.
//  3. neighbors(out) on a DRF inherited `retrieve` synthetic surfaces the
//     external RetrieveModelMixin contract endpoint (external=true) and does NOT
//     fabricate call edges from it.
//  4. trace from the DRF stub resolves to the RetrieveModelMixin contract node.
//  5. neighbors(in) on the BASE method surfaces the inheriting subclass stub
//     (reverse-INHERITS).
//  6. def_use on the inherited stub returns the BASE member's reaching-def
//     chains, tagged resolved_via_inherits.
//  7. NEGATIVE: an unresolvable inherited member produces NO synthetic edge —
//     honest dead-end, no fabricated callees.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// inRepoBaseCallDoc extends inRepoBaseDoc's shape with a real CALLS edge from
// the base method to a `process` helper, so neighbors(out)/trace on the
// inherited child stub must reach `process` THROUGH the base body.
func inRepoBaseCallDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "base", Name: "BaseService", QualifiedName: "BaseService",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "base.py",
				StartLine: 1, EndLine: 4, Language: "python"},
			{ID: "base_handle", Name: "BaseService.handle", QualifiedName: "BaseService.handle",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.py",
				StartLine: 2, EndLine: 4, Language: "python"},
			{ID: "process", Name: "BaseService.process", QualifiedName: "BaseService.process",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.py",
				StartLine: 5, EndLine: 6, Language: "python"},
			{ID: "child", Name: "ChildService", QualifiedName: "ChildService",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "child.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			// Bodyless inherited member on the child (no CALLS edges of its own).
			{ID: "child_handle", Name: "ChildService.handle", QualifiedName: "ChildService.handle",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "child.py",
				StartLine: 0, EndLine: 0, Language: "python",
				Signature: "def handle(self, request)"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "e1", FromID: "child", ToID: "base", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "BaseService"}),
			// The BASE method calls process — the real implementation edge.
			graph.Relationship{ID: "e2", FromID: "base_handle", ToID: "process", Kind: "CALLS"}.WithProperties(map[string]string{"language": "python"}),
		},
	}
}

func callNeighbors3834(t *testing.T, srv *Server, entityID, direction string) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group": "test", "entity_id": entityID, "direction": direction, "depth": 3,
	}
	res, err := srv.handleNeighbors(context.Background(), req)
	if err != nil {
		t.Fatalf("neighbors error: %v", err)
	}
	if res.IsError {
		t.Fatalf("neighbors tool error: %v", res.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("neighbors decode: %v", err)
	}
	return out
}

// calleeNames extracts the "name" of every callee record in a neighbors result.
func calleeNames3834(t *testing.T, out map[string]any) []string {
	t.Helper()
	raw, _ := out["callees"].([]any)
	var names []string
	for _, r := range raw {
		m, _ := r.(map[string]any)
		if n, _ := m["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// TestNeighbors_InRepoInheritedStub_ReachesBaseCallees — #3834 case 1. The
// inherited stub owns NO CALLS edges; neighbors(out) must hop via INHERITS to
// the base body and surface BaseService.process (the base's real callee).
func TestNeighbors_InRepoInheritedStub_ReachesBaseCallees(t *testing.T) {
	srv := newTestServer(t, inRepoBaseCallDoc())
	out := callNeighbors3834(t, srv, "child_handle", "out")
	names := calleeNames3834(t, out)

	if !containsString(names, "BaseService.process") {
		t.Fatalf("expected neighbors(out) on inherited stub to reach BaseService.process via INHERITS, got callees: %v", names)
	}
	// The defining base method itself must also be surfaced (the INHERITS hop).
	if !containsString(names, "BaseService.handle") {
		t.Errorf("expected the defining base member BaseService.handle in callees, got: %v", names)
	}
	// The hop must be flagged as via_inherits on the defining member.
	if !calleeFlaggedInherits(out, "BaseService.handle") {
		t.Errorf("expected BaseService.handle flagged via_inherits=true, got: %v", out["callees"])
	}
}

// TestTrace_InRepoInheritedStub_ReachesBaseBody — #3834 case 2.
func TestTrace_InRepoInheritedStub_ReachesBaseBody(t *testing.T) {
	srv := newTestServer(t, inRepoBaseCallDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "source": "child_handle", "target": "process"}
	res, err := srv.handleShortestPath(context.Background(), req)
	if err != nil {
		t.Fatalf("trace error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("trace decode: %v", err)
	}
	if out["found"] != true {
		t.Fatalf("expected a path from the inherited stub to process via INHERITS+CALLS, got: %v", out)
	}
	// The path must traverse the defining base method (the INHERITS hop) — not
	// teleport. Assert base_handle is on the path.
	path, _ := out["path"].([]any)
	if !pathItemsContain3834(path, "base_handle") {
		t.Errorf("expected path to traverse base_handle (the defining member), got: %v", path)
	}
	// And the INHERITS edge kind must appear in the edge list.
	edges, _ := out["edges"].([]any)
	if !pathItemsContain3834(edges, inheritsEdgeKind) {
		t.Errorf("expected an %s edge on the resolved path, got edges: %v", inheritsEdgeKind, edges)
	}
}

// TestNeighbors_DRFInheritedStub_ReachesExternalContract — #3834 case 3.
func TestNeighbors_DRFInheritedStub_ReachesExternalContract(t *testing.T) {
	srv := newTestServer(t, drfViewSetDoc())
	out := callNeighbors3834(t, srv, "op_retrieve", "out")
	names := calleeNames3834(t, out)

	// Must surface the external RetrieveModelMixin contract endpoint.
	found := false
	for _, n := range names {
		if strings.Contains(n, "RetrieveModelMixin") && strings.Contains(n, "retrieve") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected DRF inherited stub neighbors(out) to surface the RetrieveModelMixin.retrieve contract endpoint, got: %v", names)
	}
	// HONEST-PARTIAL: the external contract is a LEAF — it must not bring along
	// fabricated callees. Only the single contract endpoint should appear.
	if len(names) != 1 {
		t.Errorf("external contract must be a leaf with no fabricated callees; got %d callees: %v", len(names), names)
	}
}

// TestTrace_DRFInheritedStub_ResolvesToContract — #3834 case 4.
func TestTrace_DRFInheritedStub_ResolvesToContract(t *testing.T) {
	srv := newTestServer(t, drfViewSetDoc())
	contractID := externalContractID("rest_framework.mixins.RetrieveModelMixin", "retrieve")
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "source": "op_retrieve", "target": contractID}
	res, err := srv.handleShortestPath(context.Background(), req)
	if err != nil {
		t.Fatalf("trace error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("trace decode: %v", err)
	}
	if out["found"] != true {
		t.Fatalf("expected trace from DRF stub to resolve to the RetrieveModelMixin contract endpoint, got: %v", out)
	}
	edges, _ := out["edges"].([]any)
	if !pathItemsContain3834(edges, inheritsEdgeKind) {
		t.Errorf("expected the resolution edge to be %s, got: %v", inheritsEdgeKind, edges)
	}
}

// TestNeighbors_BaseMethod_SurfacesInheritingStub — #3834 case 5 (reverse).
func TestNeighbors_BaseMethod_SurfacesInheritingStub(t *testing.T) {
	srv := newTestServer(t, inRepoBaseCallDoc())
	out := callNeighbors3834(t, srv, "base_handle", "in")
	raw, _ := out["callers"].([]any)
	found := false
	for _, r := range raw {
		m, _ := r.(map[string]any)
		if n, _ := m["name"].(string); n == "ChildService.handle" {
			found = true
			if m["via_inherits"] != true {
				t.Errorf("expected ChildService.handle caller flagged via_inherits, got: %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("expected neighbors(in) on the base method to surface the inheriting ChildService.handle stub, got callers: %v", raw)
	}
}

// TestDefUse_InheritedStub_ReturnsBaseChains — #3834 case 6.
func TestDefUse_InheritedStub_ReturnsBaseChains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Write a def-use sidecar with a chain for the BASE member only (the stub
	// has none — it is bodyless).
	doc := defUseSidecarDoc{
		Version: 1, Method: "test", Total: 1,
		Entries: []defUseSidecarEntry{{
			Repo: "repo1", EntityID: "base_handle", Name: "BaseService.handle",
			SourceFile: "base.py",
			Chains:     []defUseChainSidecar{{Var: "result", DefLine: 2, UseLine: 3}},
		}},
	}
	writeSidecar(t, home, "test", "def-use", doc)

	srv := newTestServer(t, inRepoBaseCallDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "child_handle"}
	res, err := srv.handleDefUse(context.Background(), req)
	if err != nil {
		t.Fatalf("def_use error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("def_use decode: %v", err)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected def_use on inherited stub to return the BASE member's 1 chain entry, got %d: %v", len(entries), out)
	}
	e0, _ := entries[0].(map[string]any)
	if e0["resolved_via_inherits"] != true {
		t.Errorf("expected resolved_via_inherits=true on the inherited def_use entry, got: %v", e0)
	}
	if name, _ := e0["name"].(string); name != "BaseService.handle" {
		t.Errorf("expected the BASE member's chains, got name %q", name)
	}
	// And the chain content must be the base's (var "result").
	if !strings.Contains(extractResultText(t, res), `"result"`) {
		t.Errorf("expected the base member's reaching-def chain (var result), got: %s", extractResultText(t, res))
	}
}

// TestNeighbors_UnresolvableInheritedStub_NoFabrication — #3834 case 7
// (negative). A bodyless member whose base is neither indexed nor in the pack
// must NOT grow a synthetic INHERITS edge — honest dead-end.
func TestNeighbors_UnresolvableInheritedStub_NoFabrication(t *testing.T) {
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

	// Direct unit assertion: no synthetic outbound edge.
	lr := srv.State.groups["test"].Repos["repo1"]
	if edges := mroOutboundEdges(lr, "op"); len(edges) != 0 {
		t.Fatalf("unresolvable inherited member must produce NO synthetic INHERITS edge, got: %+v", edges)
	}

	// And neighbors(out) must report no outgoing edges (no fabricated callees).
	out := callNeighbors3834(t, srv, "op", "out")
	if names := calleeNames3834(t, out); len(names) != 0 {
		t.Errorf("unresolvable inherited stub must have no fabricated callees, got: %v", names)
	}
}

// --- test helpers ----------------------------------------------------------

func pathItemsContain3834(items []any, want string) bool {
	for _, it := range items {
		if s, _ := it.(string); strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func calleeFlaggedInherits(out map[string]any, name string) bool {
	raw, _ := out["callees"].([]any)
	for _, r := range raw {
		m, _ := r.(map[string]any)
		if n, _ := m["name"].(string); n == name {
			return m["via_inherits"] == true
		}
	}
	return false
}

func writeSidecar(t *testing.T, home, group, suffix string, doc any) {
	t.Helper()
	dir := filepath.Join(home, ".grafel", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	path := filepath.Join(dir, group+"-links-"+suffix+".json")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}
