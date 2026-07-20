package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// callFlowTool invokes a handler directly (expects JSON result).
func callFlowTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	text := extractResultText(t, res)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		// May be markdown (summarize_subgraph) — return nil.
		return nil
	}
	return out
}

// callFlowToolText returns the raw text result (for summarize_subgraph markdown).
func callFlowToolText(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	return extractResultText(t, res)
}

// callFlowToolError expects the handler to return a tool error; returns the error text.
func callFlowToolError(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		return err.Error()
	}
	if res != nil && res.IsError {
		return extractResultText(t, res)
	}
	t.Fatal("expected error result but got success")
	return ""
}

// buildChainDoc builds: A --CALLS--> B --CALLS--> C
func buildChainDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go", StartLine: 10},
			{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go", StartLine: 20},
			{ID: "ent-c", Name: "FuncC", Kind: "Function", SourceFile: "c.go", StartLine: 30},
		},
		[]graph.Relationship{
			{FromID: "ent-a", ToID: "ent-b", Kind: "CALLS"},
			{FromID: "ent-b", ToID: "ent-c", Kind: "CALLS"},
		},
	)
}

// buildDeadCodeDoc builds: A --CALLS--> B, and C (isolated).
func buildDeadCodeDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go"},
			{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go"},
			{ID: "ent-c", Name: "DeadFunc", Kind: "Function", SourceFile: "dead.go"},
			{ID: "ent-ext", Name: "fmt.Println", Kind: "stdlib.Function", SourceFile: ""},
		},
		[]graph.Relationship{
			{FromID: "ent-a", ToID: "ent-b", Kind: "CALLS"},
		},
	)
}

// ---------------------------------------------------------------------------
// TestFindCallers
// ---------------------------------------------------------------------------

// TestFindCallersStructured_NoWireBytes verifies #2325: the structured
// variant returns the typed map directly — internal cross-handler dispatch
// (mergeNeighbors) consumes this without ever parsing wire bytes.
func TestFindCallersStructured_NoWireBytes(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"entity_id": "ent-b"}
	val, errRes := srv.findCallersStructured(context.Background(), req)
	if errRes != nil {
		t.Fatalf("unexpected error from structured seam: %v", errRes.Content)
	}
	if val == nil {
		t.Fatal("expected structured map, got nil")
	}
	// Typed shape: callers []caller (typed slice), not []any. The whole
	// point of the structured seam is no JSON round-trip.
	if val["callers"] == nil {
		t.Fatalf("expected callers key in structured result: %+v", val)
	}
	// entity_name surfaces in the merge step, must be present.
	if val["entity_name"] != "FuncB" {
		t.Errorf("expected entity_name=FuncB, got %v", val["entity_name"])
	}
}

// TestMergeNeighbors_NoParse verifies that mergeNeighbors composes its
// output from structured maps directly — passing typed maps yields a
// merged result with both callers and callees lists, no parse step.
func TestMergeNeighbors_NoParse(t *testing.T) {
	in := map[string]any{
		"entity_id":   "r1::a",
		"entity_name": "A",
		"repo":        "r1",
		"depth":       1,
		"callers":     []any{map[string]any{"id": "r1::b"}},
		"count":       1,
	}
	out := map[string]any{
		"entity_id":   "r1::a",
		"entity_name": "A",
		"repo":        "r1",
		"depth":       1,
		"callees":     []any{map[string]any{"id": "r1::c"}, map[string]any{"id": "r1::d"}},
		"count":       2,
	}
	res := mergeNeighbors(in, nil, out, nil)
	if res == nil || res.IsError {
		t.Fatalf("mergeNeighbors returned error: %v", res)
	}
	// res must carry a deferred value (StructuredContent) — proves jsonResult
	// is the only marshal point.
	if res.StructuredContent == nil {
		t.Error("expected mergeNeighbors result to carry deferred StructuredContent")
	}
	merged, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected merged map[string]any, got %T", res.StructuredContent)
	}
	if merged["direction"] != "both" {
		t.Errorf("expected direction=both, got %v", merged["direction"])
	}
	if merged["callers"] == nil || merged["callees"] == nil {
		t.Errorf("expected callers+callees merged: %+v", merged)
	}
}

func TestFindCallers_DirectCaller(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncB has 1 direct caller: FuncA.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller, got %d", len(callers))
	}
	first := callers[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected caller=FuncA, got %v", first["name"])
	}
	if first["hop_count"].(float64) != 1 {
		t.Errorf("expected hop_count=1, got %v", first["hop_count"])
	}
}

func TestFindCallers_Transitive(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncC has 1 direct caller (FuncB) and 1 transitive caller (FuncA at depth 2).
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(2),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 2 {
		t.Fatalf("expected 2 callers (direct + transitive), got %d", len(callers))
	}
}

func TestFindCallers_NotFound(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)
	errMsg := callFlowToolError(t, srv.handleFindCallers, map[string]any{
		"entity_id": "no-such-entity",
	})
	if errMsg == "" {
		t.Fatal("expected error for unknown entity")
	}
}

func TestFindCallers_NoCallers(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA has no callers.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	callers := out["callers"].([]any)
	if len(callers) != 0 {
		t.Errorf("expected 0 callers for root, got %d", len(callers))
	}
}

// TestFindCallers_NoEdgeSignal verifies that when an entity is found but has no
// callers, the response carries the explicit no-edge signal (#1618).
func TestFindCallers_NoEdgeSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	// "result" field must be present and set to "no_incoming_edges".
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-caller response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_incoming_edges" {
		t.Errorf("expected result=no_incoming_edges, got %q", res)
	}
	// "note" field must be present and non-empty.
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-caller response")
	}
}

// TestFindCallers_WithCallersNoSignal verifies that the no-edge signal is NOT
// present when callers are found (#1618 regression guard).
func TestFindCallers_WithCallersNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncB has 1 caller (FuncA) — result field must NOT be set.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
	})
	if _, hasResult := out["result"]; hasResult {
		t.Errorf("expected no 'result' field when callers are present, got %v", out["result"])
	}
}

// TestFindCallers_ExcludesContainsEdges verifies that CONTAINS edges (module/file
// CONTAINS entity) are not treated as callers — only reference kinds (CALLS,
// REFERENCES, TESTS, …) should appear in find_callers results (#1915/#1965).
func TestFindCallers_ExcludesContainsEdges(t *testing.T) {
	// Topology:
	//   fileNode --CONTAINS--> hookFn   (structural: file owns the function)
	//   callerFn --CALLS-->    hookFn   (real caller)
	doc := minDoc(
		[]graph.Entity{
			{ID: "file-node", Name: "hooks.js", Kind: "SCOPE.Component", SourceFile: "hooks.js"},
			{ID: "hook-fn", Name: "useProposalCounts", Kind: "SCOPE.Operation", SourceFile: "hooks.js", StartLine: 10},
			{ID: "caller-fn", Name: "ContractProposals", Kind: "SCOPE.Operation", SourceFile: "proposals.jsx", StartLine: 5},
		},
		[]graph.Relationship{
			// Structural containment — must NOT appear in find_callers.
			{FromID: "file-node", ToID: "hook-fn", Kind: "CONTAINS"},
			// Real caller — MUST appear in find_callers.
			{FromID: "caller-fn", ToID: "hook-fn", Kind: "CALLS"},
		},
	)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "hook-fn",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		names := make([]string, 0, len(callers))
		for _, c := range callers {
			if cm, ok := c.(map[string]any); ok {
				names = append(names, fmt.Sprintf("%v", cm["name"]))
			}
		}
		t.Fatalf("expected exactly 1 caller (ContractProposals), got %d: %v", len(callers), names)
	}
	first := callers[0].(map[string]any)
	if first["name"] != "ContractProposals" {
		t.Errorf("expected caller=ContractProposals, got %v", first["name"])
	}
}

// TestFindCallers_FileEntityReferencesSource verifies that a file/module
// CONTAINER entity is surfaced as a caller when the inbound edge is REFERENCES
// — post-#2020 file entities legitimately own REFERENCES edges to the targets
// they reference (e.g. `core/admin.py` REFERENCES Models / ModelAdmin classes
// via admin.site.register(...)). Before #2039 the noiseContainer filter
// silently dropped these, so find_callers returned 0. (#2039 / closes #2015)
func TestFindCallers_FileEntityReferencesSource(t *testing.T) {
	// Topology:
	//   adminFile (Component, subtype=file) --REFERENCES--> ContractModel
	doc := minDoc(
		[]graph.Entity{
			graph.Entity{
				ID: "admin-file", Name: "core/admin.py",
				Kind: "SCOPE.Component", SourceFile: "core/admin.py",
			}.WithProperties(map[string]string{"subtype": "file"}),
			{
				ID: "contract-model", Name: "ContractModel",
				Kind: "SCOPE.Schema", SourceFile: "core/models.py", StartLine: 42,
			},
		},
		[]graph.Relationship{
			{FromID: "admin-file", ToID: "contract-model", Kind: "REFERENCES"},
		},
	)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "contract-model",
		"depth":     float64(1),
	})
	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller (core/admin.py via REFERENCES), got %d: %v", len(callers), callers)
	}
	first := callers[0].(map[string]any)
	if first["name"] != "core/admin.py" {
		t.Errorf("expected caller=core/admin.py, got %v", first["name"])
	}
}

// TestFindCallers_FileEntityImportsSource verifies that a file/module CONTAINER
// entity is surfaced as a caller when the inbound edge is IMPORTS — file-level
// import edges live on the file entity post-#2020. (#2039 / closes #1985)
func TestFindCallers_FileEntityImportsSource(t *testing.T) {
	// Topology:
	//   viewsetFile --IMPORTS--> HasPermission
	doc := minDoc(
		[]graph.Entity{
			graph.Entity{
				ID: "viewset-file", Name: "building_alternate_address_viewset.py",
				Kind: "SCOPE.Component", SourceFile: "viewsets/building_alternate_address_viewset.py",
			}.WithProperties(map[string]string{"subtype": "file"}),
			{
				ID: "has-permission", Name: "HasPermission",
				Kind: "SCOPE.Operation", SourceFile: "permissions.py", StartLine: 10,
			},
		},
		[]graph.Relationship{
			{FromID: "viewset-file", ToID: "has-permission", Kind: "IMPORTS"},
		},
	)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "has-permission",
		"depth":     float64(1),
	})
	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller (viewset file via IMPORTS), got %d: %v", len(callers), callers)
	}
	first := callers[0].(map[string]any)
	if first["name"] != "building_alternate_address_viewset.py" {
		t.Errorf("expected caller=building_alternate_address_viewset.py, got %v", first["name"])
	}
}

// TestFindCallers_ModuleInitReExports verifies that an __init__.py module
// entity surfaces as a caller of a re-exported module via the IMPORTS edge
// emitted by #2026. Before #2039 the noiseContainer filter dropped it.
// (#2039 / closes #1991)
func TestFindCallers_ModuleInitReExports(t *testing.T) {
	// Topology:
	//   acme_core/__init__.py (Component, subtype=module) --IMPORTS--> acme_core.celery
	doc := minDoc(
		[]graph.Entity{
			graph.Entity{
				ID: "init-module", Name: "acme_core/__init__.py",
				Kind: "SCOPE.Component", SourceFile: "acme_core/__init__.py",
			}.WithProperties(map[string]string{"subtype": "module"}),
			graph.Entity{
				ID:   "celery-module",
				Name: "acme_core.celery",
				Kind: "SCOPE.Component", SourceFile: "acme_core/celery.py",
			}.WithProperties(map[string]string{"subtype": "module"}),
			// Target needs a real referencable entity; the test entity itself is
			// a module container — find_callers operates on the target as a
			// pure id (no noise filter on the target). Walk from celery-module.
		},
		[]graph.Relationship{
			{FromID: "init-module", ToID: "celery-module", Kind: "IMPORTS"},
		},
	)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "celery-module",
		"depth":     float64(1),
	})
	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller (__init__.py via IMPORTS), got %d: %v", len(callers), callers)
	}
	first := callers[0].(map[string]any)
	if first["name"] != "acme_core/__init__.py" {
		t.Errorf("expected caller=acme_core/__init__.py, got %v", first["name"])
	}
}

// TestFindCallers_ChecklistAdminPyPostFinalize reproduces the verification-round
// failure for #2015: post-#1964 the python extractor's finalize sweep stamps
// StartLine=1 onto previously-zero file containers, so the noiseContainer gate
// in classifyNoise that required StartLine==0 stopped matching. The container
// then fell through to either noiseShadow (and got dropped) or noiseNone (and
// passed through). Without #2015 the production scenario — Checklist model with
// 17 REFERENCES + IMPORTS edges from core/admin.py — was still missing the
// admin.py caller. This test exercises that exact shape: a file Component with
// StartLine>0 and only the top-level Subtype set (no Properties["subtype"]),
// connected via REFERENCES + IMPORTS edges in addition to a CALLS-via-method
// noise neighbour. After the fix admin.py is the file-level caller. (#2015)
func TestFindCallers_ChecklistAdminPyPostFinalize(t *testing.T) {
	doc := minDoc(
		[]graph.Entity{
			// Target: Checklist model.
			{
				ID: "checklist-model", Name: "Checklist",
				Kind: "SCOPE.Schema", SourceFile: "core/models/checklist.py", StartLine: 12,
			},
			// File container WITH StartLine=1 (post-#1964 finalize) and only
			// the top-level Subtype field set — Properties["subtype"] is
			// intentionally absent here to simulate fb-load paths that lose
			// the redundant Properties copy. Pre-#2015 this entity would slip
			// through the noiseContainer gate, get re-classified as
			// noiseShadow by the StartLine==0 fallback (which also wouldn't
			// match here, so it became noiseNone) — and in some real graphs
			// failed the byID lookup entirely.
			{
				ID: "admin-file", Name: "core/admin.py",
				Kind: "SCOPE.Component", SourceFile: "core/admin.py",
				StartLine: 1,
				Subtype:   "file",
			},
			// A real method caller so the result count is non-empty
			// independent of the file caller.
			{
				ID: "register-call", Name: "register_models",
				Kind: "SCOPE.Operation", SourceFile: "core/setup.py", StartLine: 8,
			},
		},
		[]graph.Relationship{
			// 17 REFERENCES (simulated as one — BFS de-dupes anyway).
			{FromID: "admin-file", ToID: "checklist-model", Kind: "REFERENCES"},
			// IMPORTS edge from the same file.
			{FromID: "admin-file", ToID: "checklist-model", Kind: "IMPORTS"},
			// A plain CALLS caller — must still appear.
			{FromID: "register-call", ToID: "checklist-model", Kind: "CALLS"},
		},
	)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "checklist-model",
		"depth":     float64(1),
	})
	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	names := map[string]bool{}
	for _, c := range callers {
		if cm, ok := c.(map[string]any); ok {
			names[fmt.Sprintf("%v", cm["name"])] = true
		}
	}
	if !names["core/admin.py"] {
		t.Errorf("expected core/admin.py to be a caller of Checklist; got names=%v", names)
	}
	if !names["register_models"] {
		t.Errorf("expected register_models to be a caller of Checklist; got names=%v", names)
	}
}

// TestFindCallers_NilByIDSyntheticFileCaller verifies that when an inbound
// REFERENCES edge points to a FromID that isn't in the byID index (a path
// string that the resolver's IMPORTS→FileEntity-hex rewrite missed), the
// handler now synthesises a caller entry rather than silently dropping the
// signal. (#2015 — pragmatic recovery for unresolved file-level callers.)
func TestFindCallers_NilByIDSyntheticFileCaller(t *testing.T) {
	doc := minDoc(
		[]graph.Entity{
			{
				ID: "target-model", Name: "Building",
				Kind: "SCOPE.Schema", SourceFile: "core/models.py", StartLine: 1,
			},
			// Note: NO entity with ID "raw/path/admin.py" — the inbound
			// edge below points to a path string that the resolver never
			// rewrote.
		},
		[]graph.Relationship{
			{FromID: "raw/path/admin.py", ToID: "target-model", Kind: "REFERENCES"},
		},
	)
	srv := newTestServer(t, doc)
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "target-model",
		"depth":     float64(1),
	})
	callers, _ := out["callers"].([]any)
	if len(callers) != 1 {
		t.Fatalf("expected 1 synthetic caller, got %d: %v", len(callers), callers)
	}
	first := callers[0].(map[string]any)
	// Name should be the leaf of the path.
	if first["name"] != "admin.py" {
		t.Errorf("expected synthetic caller name=admin.py, got %v", first["name"])
	}
}

// ---------------------------------------------------------------------------
// TestFindCallees
// ---------------------------------------------------------------------------

func TestFindCallees_Direct(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA calls FuncB directly.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	callees, ok := out["callees"].([]any)
	if !ok {
		t.Fatalf("expected callees array, got %T", out["callees"])
	}
	if len(callees) != 1 {
		t.Fatalf("expected 1 callee, got %d", len(callees))
	}
	first := callees[0].(map[string]any)
	if first["name"] != "FuncB" {
		t.Errorf("expected callee=FuncB, got %v", first["name"])
	}
}

func TestFindCallees_Transitive(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA calls FuncB (hop 1) and transitively FuncC (hop 2).
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(2),
	})
	callees := out["callees"].([]any)
	if len(callees) != 2 {
		t.Fatalf("expected 2 callees, got %d", len(callees))
	}
}

func TestFindCallees_LeafReturnsEmpty(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncC is a leaf — no outbound edges.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(1),
	})
	callees := out["callees"].([]any)
	if len(callees) != 0 {
		t.Errorf("expected 0 callees for leaf, got %d", len(callees))
	}
}

// TestFindCallees_NoEdgeSignal verifies that when an entity is found but has no
// callees, the response carries the explicit no-edge signal (#1618).
func TestFindCallees_NoEdgeSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncC is a leaf — no outbound edges.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(1),
	})
	// "result" field must be present and set to "no_outgoing_edges".
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-callee response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_outgoing_edges" {
		t.Errorf("expected result=no_outgoing_edges, got %q", res)
	}
	// "note" field must be present and non-empty.
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-callee response")
	}
}

// TestFindCallees_WithCalleesNoSignal verifies that the no-edge signal is NOT
// present when callees are found (#1618 regression guard).
func TestFindCallees_WithCalleesNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA calls FuncB — result field must NOT be set.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	if _, hasResult := out["result"]; hasResult {
		t.Errorf("expected no 'result' field when callees are present, got %v", out["result"])
	}
}

// ---------------------------------------------------------------------------
// TestImpactRadius
// ---------------------------------------------------------------------------

func TestImpactRadius_RootChanges(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// Changing FuncB affects FuncA (its caller).
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-b",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok {
		t.Fatalf("expected affected array, got %T", out["affected"])
	}
	if len(affected) == 0 {
		t.Fatal("expected at least 1 affected entity")
	}
	// First result is highest-risk (FuncA is the only caller).
	first := affected[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected FuncA in impact, got %v", first["name"])
	}
	// risk_score must be in [0, 1].
	rs, ok := first["risk_score"].(float64)
	if !ok {
		t.Fatalf("expected numeric risk_score, got %T", first["risk_score"])
	}
	if rs < 0 || rs > 1 {
		t.Errorf("risk_score out of [0,1]: %v", rs)
	}
}

func TestImpactRadius_RootHasNoUpstreamImpact(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA is the root of the chain (no inbound callers), so changing it
	// affects nobody above it. impact_radius walks inbound, so count = 0.
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-a",
		"hops":      float64(1),
	})
	affected := out["affected"].([]any)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected for root (no callers), got %d", len(affected))
	}
}

// TestImpactRadius_InboundTestsSuppressesNoCoverageLabel verifies #3974
// end-to-end through the handler: two sibling callers of the root entity are
// identical except one has an inbound TESTS edge (from a test entity) and the
// other has none. The TESTS-covered caller must NOT carry "no test coverage" in
// its risk_reason, while the uncovered caller still does. This asserts the
// AuthService-class bug (genuinely-tested entity mislabelled uncovered) is fixed.
func TestImpactRadius_InboundTestsSuppressesNoCoverageLabel(t *testing.T) {
	entities := []graph.Entity{
		// Root whose impact we query (changing it affects its callers).
		{ID: "ent-core", Name: "CoreService", Kind: "Class", SourceFile: "core.go"},
		// Covered caller: production class WITH an inbound TESTS edge.
		{ID: "ent-auth", Name: "AuthService", Kind: "Class", SourceFile: "auth.go"},
		// Uncovered caller: identical production class, NO TESTS edge.
		{ID: "ent-billing", Name: "BillingService", Kind: "Class", SourceFile: "billing.go"},
		// A test/spec entity that exercises AuthService.
		{ID: "ent-auth-test", Name: "TestAuthService", Kind: "Function", SourceFile: "auth_test.go"},
	}
	rels := []graph.Relationship{
		// Both services depend on CoreService → both are in its impact radius.
		{FromID: "ent-auth", ToID: "ent-core", Kind: "CALLS"},
		{FromID: "ent-billing", ToID: "ent-core", Kind: "CALLS"},
		// AuthService has real test linkage via an inbound TESTS edge.
		{FromID: "ent-auth-test", ToID: "ent-auth", Kind: "TESTS"},
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-core",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected affected entities, got %v", out["affected"])
	}
	reasons := map[string]string{}
	scores := map[string]float64{}
	for _, a := range affected {
		m := a.(map[string]any)
		name, _ := m["name"].(string)
		reasons[name], _ = m["risk_reason"].(string)
		scores[name], _ = m["risk_score"].(float64)
	}

	authReason, ok := reasons["AuthService"]
	if !ok {
		t.Fatal("AuthService not found in affected list")
	}
	billingReason, ok := reasons["BillingService"]
	if !ok {
		t.Fatal("BillingService not found in affected list")
	}

	// The covered (TESTS-linked) entity must NOT be flagged uncovered.
	if strings.Contains(authReason, "no test coverage") {
		t.Errorf("AuthService has an inbound TESTS edge; risk_reason must NOT say 'no test coverage', got %q", authReason)
	}
	// The uncovered sibling must STILL be flagged (honest — no false suppression).
	if !strings.Contains(billingReason, "no test coverage") {
		t.Errorf("BillingService has zero TESTS edges; risk_reason MUST say 'no test coverage', got %q", billingReason)
	}
	// The label change must move the score: covered scores strictly lower.
	if scores["AuthService"] >= scores["BillingService"] {
		t.Errorf("covered AuthService (%v) must score lower than uncovered BillingService (%v)", scores["AuthService"], scores["BillingService"])
	}
}

// TestImpactRadius_TestSpecEntityNotMislabelled verifies #3974: when a test-spec
// entity itself lands in an impact radius (e.g. it depends on the root), it must
// not be mislabelled "no test coverage" — it is not production code under test.
func TestImpactRadius_TestSpecEntityNotMislabelled(t *testing.T) {
	entities := []graph.Entity{
		{ID: "ent-helper", Name: "sharedHelper", Kind: "Function", SourceFile: "helper.go"},
		// A test/spec entity that calls the helper → lands in helper's impact radius.
		{ID: "ent-spec", Name: "helperSpec", Kind: "Function", SourceFile: "helper.spec.ts"},
	}
	rels := []graph.Relationship{
		{FromID: "ent-spec", ToID: "ent-helper", Kind: "CALLS"},
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-helper",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected affected entities, got %v", out["affected"])
	}
	var specEntry map[string]any
	for _, a := range affected {
		m := a.(map[string]any)
		if m["name"] == "helperSpec" {
			specEntry = m
			break
		}
	}
	if specEntry == nil {
		t.Fatal("helperSpec not found in affected list")
	}
	reason, _ := specEntry["risk_reason"].(string)
	if strings.Contains(reason, "no test coverage") {
		t.Errorf("a test-spec entity must not be labelled 'no test coverage'; got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// TestImpactRadius reliability (#3925): input-driven errors → graceful results
// ---------------------------------------------------------------------------

// TestImpactRadius_MissingEntity_GracefulNotError verifies #3925: a missing
// entity_id returns a well-formed empty result with a reason, NOT a tool error.
func TestImpactRadius_MissingEntity_GracefulNotError(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"entity_id": "ent-does-not-exist",
		"hops":      float64(2),
	}
	res, err := srv.handleImpactRadius(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// The whole point of the fix: this must NOT be a tool error.
	if res.IsError {
		t.Fatalf("missing entity must NOT be a tool error; got error result: %v", res.Content)
	}
	out := decodeFlowResult(t, res)
	if out["resolved"] != false {
		t.Errorf("expected resolved=false, got %v", out["resolved"])
	}
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) != 0 {
		t.Errorf("expected empty affected array, got %v", out["affected"])
	}
	if c, _ := out["count"].(float64); c != 0 {
		t.Errorf("expected count=0, got %v", out["count"])
	}
	reason, _ := out["reason"].(string)
	if reason == "" {
		t.Error("expected a non-empty reason explaining why nothing was found")
	}
}

// TestImpactRadius_NameInsteadOfID_Resolves verifies #3925: when the caller
// passes an entity *name* (not its graph ID), and that name is unique, the
// handler resolves it to the real entity and returns the correct impact set —
// no error.
func TestImpactRadius_NameInsteadOfID_Resolves(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// "FuncB" is a Name, not an ID (the ID is "ent-b"). Changing FuncB affects
	// FuncA (its caller). Resolution by name must find ent-b and return FuncA.
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "FuncB",
		"hops":      float64(1),
	})
	if out["resolved"] != true {
		t.Errorf("expected resolved=true after name resolution, got %v", out["resolved"])
	}
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected at least 1 affected entity, got %v", out["affected"])
	}
	first := affected[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected FuncA as affected caller of FuncB, got %v", first["name"])
	}
}

// TestImpactRadius_AmbiguousName_ReturnsCandidates verifies #3925: when an
// entity_id matches multiple entities by name, the handler returns a
// disambiguation candidates list (no error).
func TestImpactRadius_AmbiguousName_ReturnsCandidates(t *testing.T) {
	// Two distinct entities share the name "process".
	doc := minDoc(
		[]graph.Entity{
			{ID: "ent-1", Name: "process", Kind: "Function", SourceFile: "a.go"},
			{ID: "ent-2", Name: "process", Kind: "Method", SourceFile: "b.go"},
			{ID: "ent-caller", Name: "main", Kind: "Function", SourceFile: "main.go"},
		},
		[]graph.Relationship{
			{FromID: "ent-caller", ToID: "ent-1", Kind: "CALLS"},
		},
	)
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"entity_id": "process", "hops": float64(1)}
	res, err := srv.handleImpactRadius(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("ambiguous name must NOT be a tool error; got: %v", res.Content)
	}
	out := decodeFlowResult(t, res)
	if out["ambiguous"] != true {
		t.Errorf("expected ambiguous=true, got %v", out["ambiguous"])
	}
	cands, ok := out["candidates"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %v", out["candidates"])
	}
	// Each candidate must carry a precise entity_id the caller can re-issue.
	for _, c := range cands {
		m := c.(map[string]any)
		if m["entity_id"] == "" || m["entity_id"] == nil {
			t.Errorf("candidate missing entity_id: %v", m)
		}
	}
}

// TestImpactRadius_HighDegree_TruncatesHonestly verifies #3925/#5793: a node
// with more dependents than the 500-cap returns a bounded result with an honest
// truncation marker (no error, no unbounded payload). Post-#5793 the full
// per-entity list is opt-in (detail=full) and the 500-cap is only the binding
// constraint under a budget large enough not to shrink further, so this test
// exercises that verbose path explicitly.
func TestImpactRadius_HighDegree_TruncatesHonestly(t *testing.T) {
	entities := []graph.Entity{
		{ID: "ent-hub", Name: "hub", Kind: "Function", SourceFile: "hub.go"},
	}
	var rels []graph.Relationship
	// Far more callers than impactRadiusMaxResults.
	n := impactRadiusMaxResults + 50
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("caller-%d", i)
		entities = append(entities, graph.Entity{
			ID: id, Name: fmt.Sprintf("caller%d", i), Kind: "Function", SourceFile: "callers.go",
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "ent-hub", Kind: "CALLS"})
	}
	srv := newTestServer(t, minDoc(entities, rels))

	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id":    "ent-hub",
		"hops":         float64(1),
		"detail":       "full",
		"token_budget": float64(400000), // large enough that the 500-cap binds, not bytes
	})
	if out["resolved"] != true {
		t.Errorf("expected resolved=true, got %v", out["resolved"])
	}
	affected, ok := out["affected"].([]any)
	if !ok {
		t.Fatalf("expected affected array, got %T", out["affected"])
	}
	if len(affected) != impactRadiusMaxResults {
		t.Errorf("expected affected capped at %d, got %d", impactRadiusMaxResults, len(affected))
	}
	if out["truncated"] != true {
		t.Errorf("expected truncated=true for high-degree node, got %v", out["truncated"])
	}
	total, _ := out["total_affected"].(float64)
	if int(total) != n {
		t.Errorf("expected total_affected=%d, got %v", n, out["total_affected"])
	}
	if note, _ := out["truncation_note"].(string); note == "" {
		t.Error("expected a non-empty truncation_note")
	}
}

// decodeFlowResult unmarshals a (possibly non-error) tool result into a map.
func decodeFlowResult(t *testing.T, res *mcpapi.CallToolResult) map[string]any {
	t.Helper()
	text := extractResultText(t, res)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// buildMixedCallerDoc builds a doc for #2644 testing.
//
// Graph shape (impact_radius walks INBOUND edges from the queried entity):
//
//	ent-hub (Function)    → CALLS →    ent-leaf (Function)
//	ent-named-{1..4} (Function)     → CALLS     → ent-hub  (4 named callers)
//	ent-mod-{1..9}   (SCOPE.Component) → CONTAINS → ent-hub  (9 module/file nodes)
//
// impact_radius(ent-leaf): finds ent-hub as a 1-hop inbound caller.
// ent-hub total in-degree = 4 CALLS (named) + 9 CONTAINS (module) = 13.
func buildMixedCallerDoc() *graph.Document {
	entities := []graph.Entity{
		{ID: "ent-leaf", Name: "lowLevelHelper", Kind: "Function", SourceFile: "helper.ts"},
		{ID: "ent-hub", Name: "useInspectorHomeData", Kind: "Function", SourceFile: "hooks.ts", StartLine: 10},
	}
	rels := []graph.Relationship{
		// hub calls leaf — hub appears in impact_radius(leaf) results.
		{FromID: "ent-hub", ToID: "ent-leaf", Kind: "CALLS"},
	}
	// 4 named callers of hub.
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("ent-named-%d", i)
		entities = append(entities, graph.Entity{
			ID: id, Name: fmt.Sprintf("CallerFunc%d", i), Kind: "Function", SourceFile: "callers.ts",
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "ent-hub", Kind: "CALLS"})
	}
	// 9 module/file nodes pointing to hub.
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("ent-mod-%d", i)
		entities = append(entities, graph.Entity{
			ID: id, Name: fmt.Sprintf("module%d.ts", i), Kind: "SCOPE.Component", SourceFile: fmt.Sprintf("module%d.ts", i),
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "ent-hub", Kind: "CONTAINS"})
	}
	return minDoc(entities, rels)
}

// TestImpactRadius_NamedVsModuleCallerBreakdown verifies #2644:
// when an affected entity has both named callers and module/file nodes in its
// inbound edges, risk_reason must emit the qualified breakdown form.
func TestImpactRadius_NamedVsModuleCallerBreakdown(t *testing.T) {
	doc := buildMixedCallerDoc()
	srv := newTestServer(t, doc)

	// Query impact of ent-leaf: ent-hub is the 1-hop inbound caller.
	// ent-hub has in-degree 13 (4 named CALLS + 9 CONTAINS module nodes).
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-leaf",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected at least 1 affected entity, got %v", out["affected"])
	}
	// Find ent-hub in results.
	var hubEntry map[string]any
	for _, a := range affected {
		m := a.(map[string]any)
		if m["name"] == "useInspectorHomeData" {
			hubEntry = m
			break
		}
	}
	if hubEntry == nil {
		t.Fatal("useInspectorHomeData not found in affected list")
	}
	reason, _ := hubEntry["risk_reason"].(string)
	// Must mention named callers and module/file node breakdown.
	if !strings.Contains(reason, "named callers") {
		t.Errorf("risk_reason should contain 'named callers'; got: %q", reason)
	}
	if !strings.Contains(reason, "module/file nodes") {
		t.Errorf("risk_reason should contain 'module/file nodes'; got: %q", reason)
	}
	if !strings.Contains(reason, "inbound edges") {
		t.Errorf("risk_reason should contain 'inbound edges'; got: %q", reason)
	}
}

// TestImpactRadius_NamedOnly verifies that when all inbound edges come from
// named operation entities (no module/file nodes), risk_reason uses the
// simplified "N named callers" form without a module breakdown.
func TestImpactRadius_NamedOnly(t *testing.T) {
	// Graph: ent-hub → ent-leaf; 6 named callers → ent-hub; 0 module nodes.
	// impact_radius(ent-leaf) → ent-hub in affected with in-degree 6 (named only).
	entities := []graph.Entity{
		{ID: "ent-leaf", Name: "leafFunc", Kind: "Function", SourceFile: "leaf.go"},
		{ID: "ent-hub", Name: "coreHelper", Kind: "Function", SourceFile: "core.go"},
	}
	rels := []graph.Relationship{
		{FromID: "ent-hub", ToID: "ent-leaf", Kind: "CALLS"},
	}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("ent-named-%d", i)
		entities = append(entities, graph.Entity{
			ID: id, Name: fmt.Sprintf("Caller%d", i), Kind: "Function", SourceFile: "callers.go",
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "ent-hub", Kind: "CALLS"})
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	// impact_radius(ent-leaf): ent-hub appears in affected with 6 named callers.
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-leaf",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected at least 1 affected entity, got %v", out["affected"])
	}
	var hubEntry map[string]any
	for _, a := range affected {
		m := a.(map[string]any)
		if m["name"] == "coreHelper" {
			hubEntry = m
			break
		}
	}
	if hubEntry == nil {
		t.Fatal("coreHelper not found in affected list")
	}
	reason, _ := hubEntry["risk_reason"].(string)
	// Should use simple "N named callers" without module breakdown.
	if !strings.Contains(reason, "named callers") {
		t.Errorf("risk_reason should contain 'named callers'; got: %q", reason)
	}
	if strings.Contains(reason, "module/file nodes") {
		t.Errorf("risk_reason should NOT mention module/file nodes when there are none; got: %q", reason)
	}
}

// ---------------------------------------------------------------------------
// TestSubgraph_FormatMarkdown (format=markdown path of the unified tool)
// ---------------------------------------------------------------------------

func TestSubgraph_FormatMarkdown_ContainsStructure(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	text := callFlowToolText(t, srv.handleSubgraph, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
		"format":    "markdown",
	})

	if !strings.Contains(text, "FuncB") {
		t.Errorf("summary should contain entity name FuncB; got:\n%s", text)
	}
	if !strings.Contains(text, "Called by") {
		t.Errorf("summary should have 'Called by' section; got:\n%s", text)
	}
	if !strings.Contains(text, "Calls") {
		t.Errorf("summary should have 'Calls' section; got:\n%s", text)
	}
}

func TestSubgraph_FormatMarkdown_RootNoCallers(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	text := callFlowToolText(t, srv.handleSubgraph, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
		"format":    "markdown",
	})
	if !strings.Contains(text, "No callers") {
		t.Errorf("FuncA has no callers; summary should say so:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// TestSubgraph — unified grafel_subgraph tool (#1754)
// ---------------------------------------------------------------------------

// TestSubgraph_FormatRaw_DefaultsToRaw verifies that omitting format= returns
// JSON (raw) output identical to the old get_subgraph path.
func TestSubgraph_FormatRaw_DefaultsToRaw(t *testing.T) {
	entities := []graph.Entity{
		{ID: "root", Name: "Root", Kind: "Function"},
		{ID: "child", Name: "Child", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "root", ToID: "child", Kind: "CALLS"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	// No format= → default is "raw".
	out := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(1),
	})
	if out == nil {
		t.Fatal("expected JSON output for format=raw, got nil (markdown path?)")
	}
	nc, ok := out["node_count"].(float64)
	if !ok {
		t.Fatalf("node_count missing or wrong type in raw output: %v", out)
	}
	if int(nc) != 2 {
		t.Errorf("expected node_count=2, got %d", int(nc))
	}
}

// TestSubgraph_FormatRaw_GraphCounts verifies node_count and edge_count for
// a known fixture using format="raw" on the canonical grafel_subgraph tool.
func TestSubgraph_FormatRaw_GraphCounts(t *testing.T) {
	entities := []graph.Entity{
		{ID: "root", Name: "Root", Kind: "Function"},
		{ID: "child", Name: "Child", Kind: "Function"},
		{ID: "grandchild", Name: "GrandChild", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "root", ToID: "child", Kind: "CALLS"},
		{ID: "r2", FromID: "child", ToID: "grandchild", Kind: "CALLS"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	// depth=2 from root should reach root+child+grandchild (3 nodes, 2 edges).
	unified := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(2),
		"format":    "raw",
	})
	if unified == nil {
		t.Fatal("expected JSON output for format=raw")
	}
	if nc := int(unified["node_count"].(float64)); nc != 3 {
		t.Errorf("expected node_count=3, got %d", nc)
	}
	if ec := int(unified["edge_count"].(float64)); ec != 2 {
		t.Errorf("expected edge_count=2, got %d", ec)
	}
}

// TestSubgraph_FormatMarkdown_ContainsEntityName verifies that format="markdown"
// returns a summary containing the target entity name.
func TestSubgraph_FormatMarkdown_ContainsEntityName(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	text := callFlowToolText(t, srv.handleSubgraph, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
		"format":    "markdown",
	})
	if !strings.Contains(text, "FuncB") {
		t.Errorf("format=markdown should contain FuncB in output:\n%s", text)
	}
}

// TestSubgraph_InvalidFormat verifies a helpful error is returned for unknown format.
func TestSubgraph_InvalidFormat(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
		"format":    "xml",
	}
	res, err := srv.handleSubgraph(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for invalid format")
	}
}

// TestSubgraph_FormatRaw_SmallGraphNotTruncated verifies the common case is
// returned complete: a small subgraph carries truncated=false and emits no
// truncation_note. (#3924)
func TestSubgraph_FormatRaw_SmallGraphNotTruncated(t *testing.T) {
	entities := []graph.Entity{
		{ID: "root", Name: "Root", Kind: "Function"},
		{ID: "child", Name: "Child", Kind: "Function"},
		{ID: "grandchild", Name: "GrandChild", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "root", ToID: "child", Kind: "CALLS"},
		{ID: "r2", FromID: "child", ToID: "grandchild", Kind: "CALLS"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	out := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(2),
		"format":    "raw",
	})
	if out == nil {
		t.Fatal("expected JSON output for format=raw")
	}
	if trunc, _ := out["truncated"].(bool); trunc {
		t.Errorf("small subgraph should not be truncated: %v", out)
	}
	if _, ok := out["truncation_note"]; ok {
		t.Errorf("small subgraph should carry no truncation_note: %v", out["truncation_note"])
	}
	if nc := int(out["node_count"].(float64)); nc != 3 {
		t.Errorf("expected node_count=3, got %d", nc)
	}
	if ec := int(out["edge_count"].(float64)); ec != 2 {
		t.Errorf("expected edge_count=2, got %d", ec)
	}
}

// TestSubgraph_FormatRaw_HighDegreeBounded builds a hub with many neighbours
// reached at depth 2 and verifies the response is bounded by max_nodes AND
// honestly marked truncated (not silently dropped). (#3924)
func TestSubgraph_FormatRaw_HighDegreeBounded(t *testing.T) {
	// root -> hub, hub -> leaf_i for many i. With depth=2 from root, the leaves
	// are reached at hop 2 and would explode the node set unbounded.
	const fanout = 500
	entities := []graph.Entity{
		{ID: "root", Name: "Root", Kind: "Function"},
		{ID: "hub", Name: "Hub", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r-root-hub", FromID: "root", ToID: "hub", Kind: "CALLS"},
	}
	for i := 0; i < fanout; i++ {
		id := fmt.Sprintf("leaf%d", i)
		entities = append(entities, graph.Entity{ID: id, Name: id, Kind: "Function"})
		rels = append(rels, graph.Relationship{
			ID: "r-hub-" + id, FromID: "hub", ToID: id, Kind: "CALLS",
		})
	}
	srv := newTestServer(t, minDoc(entities, rels))

	const cap = 50
	out := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(2),
		"format":    "raw",
		"max_nodes": float64(cap),
	})
	if out == nil {
		t.Fatal("expected JSON output for format=raw")
	}
	trunc, _ := out["truncated"].(bool)
	if !trunc {
		t.Fatalf("high-degree subgraph should be truncated=true, got: %v", out["truncated"])
	}
	if _, ok := out["truncation_note"]; !ok {
		t.Error("truncated response must carry an explicit truncation_note (honest, not silent)")
	}
	nc := int(out["node_count"].(float64))
	if nc > cap {
		t.Errorf("node_count %d must not exceed max_nodes cap %d", nc, cap)
	}
	if nc < 2 {
		t.Errorf("expected at least root+hub in bounded result, got node_count=%d", nc)
	}
}

// TestSubgraph_FormatRaw_EdgeTuplesPreserved verifies the from/to/kind edge
// tuples the rewrite agent's format=raw consumer relies on are emitted with
// correct direction after switching edge collection to the adjacency index. (#3924)
func TestSubgraph_FormatRaw_EdgeTuplesPreserved(t *testing.T) {
	entities := []graph.Entity{
		{ID: "a", Name: "A", Kind: "Function"},
		{ID: "b", Name: "B", Kind: "Function"},
		{ID: "c", Name: "C", Kind: "Class"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r2", FromID: "a", ToID: "c", Kind: "REFERENCES"},
	}
	srv := newTestServer(t, minDoc(entities, rels))

	out := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "a",
		"depth":     float64(1),
		"format":    "raw",
	})
	if out == nil {
		t.Fatal("expected JSON output for format=raw")
	}
	rawEdges, ok := out["edges"].([]any)
	if !ok {
		t.Fatalf("edges missing or wrong type: %v", out["edges"])
	}
	got := map[string]string{} // "from>to" -> kind
	for _, re := range rawEdges {
		em := re.(map[string]any)
		from := em["from_id"].(string)
		to := em["to_id"].(string)
		kind := em["kind"].(string)
		got[from+">"+to] = kind
	}
	// Direction preserved: a->b CALLS, a->c REFERENCES (prefixed with repo1::).
	if got["repo1::a>repo1::b"] != "CALLS" {
		t.Errorf("expected a->b CALLS edge tuple, got map: %v", got)
	}
	if got["repo1::a>repo1::c"] != "REFERENCES" {
		t.Errorf("expected a->c REFERENCES edge tuple, got map: %v", got)
	}
	if ec := int(out["edge_count"].(float64)); ec != 2 {
		t.Errorf("expected edge_count=2, got %d", ec)
	}
}

// ---------------------------------------------------------------------------
// TestFindDeadCode
// ---------------------------------------------------------------------------

func TestFindDeadCode_IsolatedEntity(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead, ok := out["dead_code"].([]any)
	if !ok {
		t.Fatalf("expected dead_code array, got %T", out["dead_code"])
	}
	// DeadFunc (ent-c) should appear; FuncA→FuncB are connected; ent-ext is stdlib.
	found := false
	for _, item := range dead {
		m := item.(map[string]any)
		if m["name"] == "DeadFunc" {
			found = true
		}
		// FuncA and FuncB must NOT appear (they have edges between them).
		if m["name"] == "FuncA" || m["name"] == "FuncB" {
			t.Errorf("FuncA/FuncB should not be dead code, but appeared: %v", m["name"])
		}
		// stdlib entities must not appear.
		if m["name"] == "fmt.Println" {
			t.Errorf("stdlib entity should not appear in dead code results")
		}
	}
	if !found {
		t.Error("DeadFunc should be listed as dead code")
	}
}

func TestFindDeadCode_KindFilter(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	// Filter to "Class" — no entities match, expect empty.
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{
		"kind_filter": "Class",
	})
	dead := out["dead_code"].([]any)
	if len(dead) != 0 {
		t.Errorf("expected 0 dead Class entities, got %d", len(dead))
	}
}

func TestFindDeadCode_StdlibExcluded(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead := out["dead_code"].([]any)
	for _, item := range dead {
		m := item.(map[string]any)
		if name, _ := m["name"].(string); name == "fmt.Println" {
			t.Error("stdlib entity fmt.Println must not appear in dead code")
		}
	}
}

// buildPublicAPIDeadCodeDoc models the polyglot-platform fixture's dead-code
// scenario: a shared module exporting several public operations where most are
// either called in-repo, imported by another repo, or legitimate API, and only
// the marker-tagged unreferenced ones are genuinely dead.
//
//	verifyToken      — exported, imported cross-repo (IMPORTS imported_name)   → LIVE
//	hasRole          — exported, unreferenced, NO dead marker (legit API)      → LIVE
//	legacySignToken  — exported, unreferenced, "legacy" marker                 → DEAD
//	postHandler      — route handler (GET /x), unreferenced                    → LIVE
//	DeadReindexAll   — exported, unreferenced, "Dead" marker                   → DEAD
func buildPublicAPIDeadCodeDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "mod", Name: "src", Kind: "Module", SourceFile: "src/auth.ts"},
			{ID: "verify", Name: "verifyToken", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "role", Name: "hasRole", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "legacy", Name: "legacySignToken", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "route", Name: "GET /products", Kind: "SCOPE.Operation", SourceFile: "src/index.ts", Language: "typescript"},
			{ID: "dead2", Name: "DeadReindexAll", Kind: "SCOPE.Operation", SourceFile: "internal/server.go", Language: "go"},
			// External import marker carrying imported_name=verifyToken.
			{ID: "ext", Name: "@shared/js", Kind: "SCOPE.External", SourceFile: ""},
			// A shared helper each operation calls — gives them an outbound
			// edge so they are not "fully isolated" (Class 1), exercising the
			// Class-2 unreferenced-public-operation path instead.
			{ID: "helper", Name: "jwtSign", Kind: "SCOPE.Operation", SourceFile: "src/jwt.ts", Language: "typescript"},
		},
		[]graph.Relationship{
			{FromID: "mod", ToID: "verify", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "role", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "legacy", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "route", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "dead2", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "helper", Kind: "CONTAINS"},
			// Each public op calls the shared helper (outbound edge).
			{FromID: "verify", ToID: "helper", Kind: "CALLS"},
			{FromID: "role", ToID: "helper", Kind: "CALLS"},
			{FromID: "legacy", ToID: "helper", Kind: "CALLS"},
			{FromID: "route", ToID: "helper", Kind: "CALLS"},
			{FromID: "dead2", ToID: "helper", Kind: "CALLS"},
			// verifyToken is imported (consumed) cross-repo.
			graph.Relationship{FromID: "mod", ToID: "ext", Kind: "IMPORTS"}.WithProperties(map[string]string{"imported_name": "verifyToken"}),
		},
	)
}

func TestFindDeadCode_PrecisionOnPublicAPI(t *testing.T) {
	srv := newTestServer(t, buildPublicAPIDeadCodeDoc())
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead := out["dead_code"].([]any)

	flagged := map[string]bool{}
	for _, item := range dead {
		flagged[item.(map[string]any)["name"].(string)] = true
	}

	wantDead := []string{"legacySignToken", "DeadReindexAll"}
	wantLive := []string{"verifyToken", "hasRole", "GET /products"}

	for _, n := range wantDead {
		if !flagged[n] {
			t.Errorf("expected %q to be flagged as dead code", n)
		}
	}
	for _, n := range wantLive {
		if flagged[n] {
			t.Errorf("false positive: %q should NOT be flagged (live/legit API)", n)
		}
	}
	if len(flagged) != len(wantDead) {
		t.Errorf("expected exactly %d dead entities, got %d: %v", len(wantDead), len(flagged), flagged)
	}
}

func TestFindDeadCode_ImportedNotFlagged(t *testing.T) {
	srv := newTestServer(t, buildPublicAPIDeadCodeDoc())
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	for _, item := range out["dead_code"].([]any) {
		if item.(map[string]any)["name"] == "verifyToken" {
			t.Fatal("verifyToken is imported cross-repo and must not be flagged")
		}
	}
}

// ---------------------------------------------------------------------------
// TestImpactRiskScore unit tests
// ---------------------------------------------------------------------------

func TestImpactRiskScore_HighInDegree(t *testing.T) {
	e := graph.EntityPtr(graph.Entity{Kind: "Function"}.WithProperties(map[string]string{}))
	score := impactRiskScore(e, 50, false, false)
	if score <= 0 {
		t.Errorf("high in-degree should produce score > 0, got %v", score)
	}
}

func TestImpactRiskScore_APIBoundary(t *testing.T) {
	e := graph.EntityPtr(graph.Entity{Kind: "http_endpoint_definition"}.WithProperties(map[string]string{}))
	score := impactRiskScore(e, 0, false, false)
	if score < 0.25 {
		t.Errorf("API boundary entity should score >= 0.25, got %v", score)
	}
}

func TestImpactRiskScore_WithCoverage(t *testing.T) {
	eCovered := graph.EntityPtr(graph.Entity{Kind: "Function"}.WithProperties(map[string]string{"test_coverage": "85"}))
	eUncovered := graph.EntityPtr(graph.Entity{Kind: "Function"}.WithProperties(map[string]string{}))
	scoreCovered := impactRiskScore(eCovered, 0, false, false)
	scoreUncovered := impactRiskScore(eUncovered, 0, false, false)
	if scoreCovered >= scoreUncovered {
		t.Errorf("covered entity (%v) should score lower than uncovered (%v)", scoreCovered, scoreUncovered)
	}
}

// TestImpactRiskScore_InboundTestsSuppressesNoCoverage verifies #3974: an entity
// with an inbound TESTS edge (hasInboundTests=true) but no test_coverage property
// must score LOWER than an identical entity with neither signal, because the
// TESTS edge is genuine test linkage and the no-coverage penalty must not apply.
func TestImpactRiskScore_InboundTestsSuppressesNoCoverage(t *testing.T) {
	e := graph.EntityPtr(graph.Entity{Kind: "Class", Name: "AuthService"}.WithProperties(map[string]string{}))
	withTests := impactRiskScore(e, 0, true /*hasInboundTests*/, false)
	withoutTests := impactRiskScore(e, 0, false, false)
	if withTests >= withoutTests {
		t.Errorf("entity with inbound TESTS edge (%v) must score lower than one with zero TESTS (%v)", withTests, withoutTests)
	}
	// Concretely: no other risk factors → TESTS-covered entity scores 0.0.
	if withTests != 0.0 {
		t.Errorf("AuthService with inbound TESTS and no other risk should score 0.0, got %v", withTests)
	}
}

// TestImpactRiskScore_TestSpecEntityNotFlagged verifies #3974: a test-spec entity
// itself is not production code, so the no-coverage penalty must never apply.
func TestImpactRiskScore_TestSpecEntityNotFlagged(t *testing.T) {
	spec := graph.EntityPtr(graph.Entity{Kind: "Function", Name: "TestLogin", SourceFile: "auth_test.go"}.WithProperties(map[string]string{}))
	score := impactRiskScore(spec, 0, false /*no inbound TESTS*/, true /*isTestEntity*/)
	if score != 0.0 {
		t.Errorf("test-spec entity must not be penalised for no coverage; got %v", score)
	}
}

// ---------------------------------------------------------------------------
// TestExpand_NoEdgeSignal (#1618)
// ---------------------------------------------------------------------------

// buildIsolatedDoc builds a single entity with no edges.
func buildIsolatedDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "iso-1", Name: "IsolatedFunc", Kind: "Function", SourceFile: "iso.go", StartLine: 1},
		},
		[]graph.Relationship{},
	)
}

// TestExpand_NoEdgeSignal verifies that grafel_expand returns the explicit
// no-edge signal when the entity is found but has zero neighbours (#1618).
func TestExpand_NoEdgeSignal(t *testing.T) {
	doc := buildIsolatedDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleGetNeighbors, map[string]any{
		"node": "iso-1",
	})
	if out == nil {
		t.Fatal("expected JSON response, got nil")
	}
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-edge response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_edges" {
		t.Errorf("expected result=no_edges, got %q", res)
	}
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-edge response")
	}
	count, ok := out["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("expected count=0, got %v", out["count"])
	}
}

// TestExpand_WithEdgesNoSignal verifies that the no-edge signal is NOT present
// when the entity has neighbours (#1618 regression guard).
func TestExpand_WithEdgesNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServer(t, doc)

	// FuncA has an outbound edge to FuncB — no no-edge signal expected.
	// handleGetNeighbors returns a flat array for the non-empty case, which
	// does not decode to map[string]any. We just verify it's not an error and
	// does not contain result=no_edges.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"node": "ent-a"}
	res, err := srv.handleGetNeighbors(nil, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	text := extractResultText(t, res)
	if strings.Contains(text, `"result"`) && strings.Contains(text, `"no_edges"`) {
		t.Errorf("no_edges signal must not appear when edges exist: %s", text)
	}
}

// ---------------------------------------------------------------------------
// #1738: token_budget enforcement tests for find_callers / find_callees / expand
// ---------------------------------------------------------------------------

// build25CallerDoc builds a doc with 25 entities all calling "target".
func build25CallerDoc() *graph.Document {
	entities := []graph.Entity{{ID: "target", Name: "Target", Kind: "Function", SourceFile: "t.go", StartLine: 1}}
	rels := []graph.Relationship{}
	for i := 0; i < 25; i++ {
		cid := fmt.Sprintf("caller%02d", i)
		entities = append(entities, graph.Entity{
			ID:         cid,
			Name:       fmt.Sprintf("Caller%02d", i),
			Kind:       "Function",
			SourceFile: fmt.Sprintf("c%02d.go", i),
			StartLine:  i + 1,
		})
		rels = append(rels, graph.Relationship{FromID: cid, ToID: "target", Kind: "CALLS"})
	}
	return minDoc(entities, rels)
}

// build25CalleeDoc builds a doc where "root" calls 25 callees.
func build25CalleeDoc() *graph.Document {
	entities := []graph.Entity{{ID: "root", Name: "Root", Kind: "Function", SourceFile: "r.go", StartLine: 1}}
	rels := []graph.Relationship{}
	for i := 0; i < 25; i++ {
		cid := fmt.Sprintf("callee%02d", i)
		entities = append(entities, graph.Entity{
			ID:         cid,
			Name:       fmt.Sprintf("Callee%02d", i),
			Kind:       "Function",
			SourceFile: fmt.Sprintf("c%02d.go", i),
			StartLine:  i + 1,
		})
		rels = append(rels, graph.Relationship{FromID: "root", ToID: cid, Kind: "CALLS"})
	}
	return minDoc(entities, rels)
}

// TestFindCallers_TokenBudgetEnforced verifies that a very tight token_budget
// caps the callers slice and produces a truncation_note (#1738).
func TestFindCallers_TokenBudgetEnforced(t *testing.T) {
	srv := newTestServer(t, build25CallerDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id":    "target",
		"depth":        float64(1),
		"token_budget": float64(50), // tiny — forces truncation
		"group":        "test",
	})
	count, _ := out["count"].(float64)
	if int(count) >= 25 {
		t.Errorf("expected callers capped by token_budget, got count=%v", count)
	}
	truncNote, _ := out["truncation_note"].(string)
	if truncNote == "" {
		t.Errorf("expected truncation_note when token_budget is exceeded")
	}
}

// TestFindCallees_TokenBudgetEnforced verifies the same for callees.
func TestFindCallees_TokenBudgetEnforced(t *testing.T) {
	srv := newTestServer(t, build25CalleeDoc())
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id":    "root",
		"depth":        float64(1),
		"token_budget": float64(50),
		"group":        "test",
	})
	count, _ := out["count"].(float64)
	if int(count) >= 25 {
		t.Errorf("expected callees capped by token_budget, got count=%v", count)
	}
	truncNote, _ := out["truncation_note"].(string)
	if truncNote == "" {
		t.Errorf("expected truncation_note when token_budget is exceeded")
	}
}

// TestExpand_TokenBudgetEnforced verifies that grafel_expand caps its
// output when token_budget is tight (#1738).
func TestExpand_TokenBudgetEnforced(t *testing.T) {
	// Build a star graph: root connected to 30 leaf nodes via CALLS.
	entities := []graph.Entity{{ID: "root", Name: "Root", Kind: "Function", SourceFile: "r.go", StartLine: 1}}
	rels := []graph.Relationship{}
	for i := 0; i < 30; i++ {
		lid := fmt.Sprintf("leaf%02d", i)
		entities = append(entities, graph.Entity{
			ID:         lid,
			Name:       fmt.Sprintf("Leaf%02d", i),
			Kind:       "Function",
			SourceFile: fmt.Sprintf("l%02d.go", i),
			StartLine:  i + 1,
		})
		rels = append(rels, graph.Relationship{FromID: "root", ToID: lid, Kind: "CALLS"})
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"node":         "root",
		"depth":        float64(1),
		"token_budget": float64(50), // very tight
		"group":        "test",
	}
	res, err := srv.handleGetNeighbors(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	var rawResult any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &rawResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Result is either a raw array (no truncation path) or a map (truncated path).
	switch v := rawResult.(type) {
	case []any:
		if len(v) >= 30 {
			t.Errorf("expected neighbors capped by token_budget, got %d items", len(v))
		}
	case map[string]any:
		count, _ := v["count"].(float64)
		if int(count) >= 30 {
			t.Errorf("expected neighbors capped by token_budget, got count=%v", count)
		}
		truncNote, _ := v["truncation_note"].(string)
		if truncNote == "" {
			t.Errorf("expected truncation_note when token_budget is exceeded")
		}
	default:
		t.Fatalf("unexpected result type %T", rawResult)
	}
}

// TestFindCallers_RankedByFrequency verifies #2577: callers are sorted by call
// frequency (descending), not alphabetically. Fixture: A→X 3 times, B→X 1 time,
// C→X 5 times => expected order [C (5), A (3), B (1)].
func TestFindCallers_RankedByFrequency(t *testing.T) {
	// Build a doc where A, B, C each call X, but with different frequencies:
	// A calls X 3 times, B calls X 1 time, C calls X 5 times.
	entities := []graph.Entity{
		{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go", StartLine: 10},
		{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go", StartLine: 20},
		{ID: "ent-c", Name: "FuncC", Kind: "Function", SourceFile: "c.go", StartLine: 30},
		{ID: "ent-x", Name: "FuncX", Kind: "Function", SourceFile: "x.go", StartLine: 40},
	}
	rels := []graph.Relationship{
		// A calls X 3 times
		{FromID: "ent-a", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-a", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-a", ToID: "ent-x", Kind: "CALLS"},
		// B calls X 1 time
		{FromID: "ent-b", ToID: "ent-x", Kind: "CALLS"},
		// C calls X 5 times
		{FromID: "ent-c", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-c", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-c", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-c", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-c", ToID: "ent-x", Kind: "CALLS"},
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-x",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 3 {
		t.Fatalf("expected 3 callers, got %d", len(callers))
	}

	// Expected order: C (5 calls), A (3 calls), B (1 call).
	first := callers[0].(map[string]any)
	if first["name"] != "FuncC" {
		t.Errorf("expected first caller=FuncC, got %v", first["name"])
	}

	second := callers[1].(map[string]any)
	if second["name"] != "FuncA" {
		t.Errorf("expected second caller=FuncA, got %v", second["name"])
	}

	third := callers[2].(map[string]any)
	if third["name"] != "FuncB" {
		t.Errorf("expected third caller=FuncB, got %v", third["name"])
	}
}

// TestFindCallers_TieBreakAlphabetical verifies that when two callers have the
// same frequency, they are sorted alphabetically. Fixture: A→X 2 times, B→X 2
// times => expected order [A, B].
func TestFindCallers_TieBreakAlphabetical(t *testing.T) {
	entities := []graph.Entity{
		{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go", StartLine: 10},
		{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go", StartLine: 20},
		{ID: "ent-x", Name: "FuncX", Kind: "Function", SourceFile: "x.go", StartLine: 40},
	}
	rels := []graph.Relationship{
		// A calls X 2 times
		{FromID: "ent-a", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-a", ToID: "ent-x", Kind: "CALLS"},
		// B calls X 2 times
		{FromID: "ent-b", ToID: "ent-x", Kind: "CALLS"},
		{FromID: "ent-b", ToID: "ent-x", Kind: "CALLS"},
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-x",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 2 {
		t.Fatalf("expected 2 callers, got %d", len(callers))
	}

	// Both have the same frequency (2), so should be sorted alphabetically: A, B.
	first := callers[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected first caller=FuncA, got %v", first["name"])
	}

	second := callers[1].(map[string]any)
	if second["name"] != "FuncB" {
		t.Errorf("expected second caller=FuncB, got %v", second["name"])
	}
}

// TestFindCallersIntegration_FrequencyRankedOnRealHandler exercises the full
// MCP handler end-to-end (#2591). It verifies that when extractors emit ONE
// CALLS edge per (caller, callee) pair with a Properties["count"] encoding the
// call-site frequency (the real-world pattern), the MCP response honours that
// frequency rather than falling back to alphabetical order.
//
// Fixture: A×1 call, B×3 calls, C×2 calls → expected order [B, C, A].
// Each caller has exactly ONE relationship record but with "count" in Properties.
// This matches how JS/Python extractors represent multi-site callers on acme data.
func TestFindCallersIntegration_FrequencyRankedOnRealHandler(t *testing.T) {
	// One CALLS edge per caller, with Properties["count"] encoding frequency.
	entities := []graph.Entity{
		{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.js", StartLine: 1},
		{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.js", StartLine: 1},
		{ID: "ent-c", Name: "FuncC", Kind: "Function", SourceFile: "c.js", StartLine: 1},
		{ID: "ent-t", Name: "callApi", Kind: "Function", SourceFile: "api.js", StartLine: 1},
	}
	rels := []graph.Relationship{
		// A calls callApi 1 time (single edge, count=1)
		graph.Relationship{FromID: "ent-a", ToID: "ent-t", Kind: "CALLS"}.WithProperties(map[string]string{"count": "1"}),
		// B calls callApi 3 times (single edge, count=3) — should rank first
		graph.Relationship{FromID: "ent-b", ToID: "ent-t", Kind: "CALLS"}.WithProperties(map[string]string{"count": "3"}),
		// C calls callApi 2 times (single edge, count=2) — should rank second
		graph.Relationship{FromID: "ent-c", ToID: "ent-t", Kind: "CALLS"}.WithProperties(map[string]string{"count": "2"}),
	}
	doc := minDoc(entities, rels)
	srv := newTestServer(t, doc)

	// Call THROUGH handleFindCallers (the real MCP entry point), not the
	// structured seam directly — this is what #2591 identified as broken.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-t",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 3 {
		t.Fatalf("expected 3 callers, got %d: %v", len(callers), callers)
	}

	// Expected order by count desc: B(3), C(2), A(1).
	names := make([]string, len(callers))
	for i, c := range callers {
		names[i] = c.(map[string]any)["name"].(string)
	}
	if names[0] != "FuncB" {
		t.Errorf("rank 1: expected FuncB (count=3), got %q; full order=%v", names[0], names)
	}
	if names[1] != "FuncC" {
		t.Errorf("rank 2: expected FuncC (count=2), got %q; full order=%v", names[1], names)
	}
	if names[2] != "FuncA" {
		t.Errorf("rank 3: expected FuncA (count=1), got %q; full order=%v", names[2], names)
	}
}

// ---------------------------------------------------------------------------
// #3648 — high-degree truncation: production callers must survive the cap.
// ---------------------------------------------------------------------------

// buildHighDegreeCallersDoc builds a target with many callers: `prod` production
// callers (in src/*.go) and `test` test callers (in *_test.go), all calling the
// target directly (hop 1). Used to verify production-first ranking and the
// dropped-breakdown truncation note.
func buildHighDegreeCallersDoc(prod, test int) *graph.Document {
	ents := []graph.Entity{
		{ID: "target", Name: "Target", Kind: "Function", SourceFile: "src/target.go", StartLine: 1},
	}
	var rels []graph.Relationship
	for i := 0; i < prod; i++ {
		id := fmt.Sprintf("prod-%02d", i)
		ents = append(ents, graph.Entity{
			ID: id, Name: fmt.Sprintf("ProdCaller%02d", i), Kind: "Function",
			SourceFile: fmt.Sprintf("src/prod_%02d.go", i), StartLine: 10,
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "target", Kind: "CALLS"})
	}
	for i := 0; i < test; i++ {
		id := fmt.Sprintf("test-%02d", i)
		ents = append(ents, graph.Entity{
			ID: id, Name: fmt.Sprintf("TestCaller%02d", i), Kind: "Function",
			SourceFile: fmt.Sprintf("src/feature_%02d_test.go", i), StartLine: 20,
		})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "target", Kind: "CALLS"})
	}
	return minDoc(ents, rels)
}

// TestFindCallers_HighDegreeProductionSurvivesTruncation asserts that under a
// tight token_budget the production callers are retained and only test callers
// are shed, and that the truncation note reports the dropped breakdown. This is
// the #3648 regression: previously the tail was shed in insertion order, so
// production callers could be dropped while test callers were kept.
func TestFindCallers_HighDegreeProductionSurvivesTruncation(t *testing.T) {
	const prodN, testN = 5, 25
	doc := buildHighDegreeCallersDoc(prodN, testN)
	srv := newTestServer(t, doc)

	// Tight budget so most callers are shed, but enough to fit all production.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id":    "target",
		"token_budget": 300,
	})
	if out == nil {
		t.Fatal("nil result map")
	}

	callers, _ := out["callers"].([]any)
	if len(callers) == 0 {
		t.Fatal("no callers returned")
	}
	if len(callers) >= prodN+testN {
		t.Fatalf("budget did not truncate: got %d callers (total %d)", len(callers), prodN+testN)
	}

	// Every production caller must be present; count survivors by kind.
	survivedProd, survivedTest := 0, 0
	for _, c := range callers {
		m := c.(map[string]any)
		name := m["name"].(string)
		if strings.HasPrefix(name, "ProdCaller") {
			survivedProd++
		} else if strings.HasPrefix(name, "TestCaller") {
			survivedTest++
		} else {
			t.Errorf("unexpected caller name %q", name)
		}
	}
	if survivedProd != prodN {
		t.Errorf("production callers dropped: %d/%d survived (test callers should be dropped first)", survivedProd, prodN)
	}
	// The whole point: production retained AHEAD of test even though test
	// callers are far more numerous and were inserted in between.
	if survivedTest != len(callers)-prodN {
		t.Errorf("survivor accounting off: prod=%d test=%d total=%d", survivedProd, survivedTest, len(callers))
	}

	// Truncation note + omitted breakdown.
	note, ok := out["truncation_note"].(string)
	if !ok || note == "" {
		t.Fatal("expected truncation_note on a truncated response")
	}
	if !strings.Contains(note, "test") || !strings.Contains(note, "production") {
		t.Errorf("note lacks kind breakdown: %q", note)
	}
	omitted, ok := out["omitted"].(map[string]any)
	if !ok {
		t.Fatal("expected omitted breakdown map")
	}
	droppedProd := int(omitted["production"].(float64))
	droppedTest := int(omitted["test"].(float64))
	if droppedProd != 0 {
		t.Errorf("no production callers should be dropped, got %d (note=%q)", droppedProd, note)
	}
	if droppedTest != testN-survivedTest {
		t.Errorf("dropped-test count wrong: omitted=%d, expected %d", droppedTest, testN-survivedTest)
	}
	if droppedTest == 0 {
		t.Error("expected some test callers to be dropped under tight budget")
	}
}

// TestFindCallers_ProductionDroppedWarnsExplicitly asserts that when the budget
// is SO tight that even production callers must be shed, the note flags it
// (different hint than the test-only case).
func TestFindCallers_ProductionDroppedWarnsExplicitly(t *testing.T) {
	doc := buildHighDegreeCallersDoc(20, 5)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id":    "target",
		"token_budget": 100, // floor; forces production to be dropped too
	})
	note, _ := out["truncation_note"].(string)
	omitted, _ := out["omitted"].(map[string]any)
	if omitted == nil {
		t.Fatal("expected omitted breakdown")
	}
	if int(omitted["production"].(float64)) == 0 {
		t.Skip("budget fit all production; not exercising the prod-dropped path")
	}
	if !strings.Contains(note, "production "+"callers were dropped") {
		t.Errorf("note should warn that production callers were dropped: %q", note)
	}
}
