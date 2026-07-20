package mcp

// di_projection_3870_test.go — #3870: surface NestJS-style dependency-injection
// edges (INJECTED_INTO / BINDS) in the MCP read tools.
//
// Before #3870 these edges were emitted by the per-language DI extractors (e.g.
// internal/custom/javascript/nestjs_di.go) and wired into the graph, but no MCP
// read tool projected them: inspect surfaced only CALLS (calls/called_by) +
// DISCRIMINATES_ON, and expand dropped the connecting edge kind entirely. The
// rewrite agent could therefore see CALLS but not provider→consumer injection.
//
// These tests are VALUE-ASSERTING: they assert the specific DI edge surfaces
// with the right kind + direction + far-side entity where it previously did
// not — not merely that some non-empty output exists.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// makeNestDIDoc builds a graph mirroring the nestjs_di.go emission shape:
//
//	UsersService  --INJECTED_INTO-->  UsersController   (provider → consumer)
//	UsersModule   --BINDS-->          USERS_TOKEN       (module → token)
//	USERS_TOKEN   --BINDS-->          UsersServiceImpl  (token → impl)
//	UsersController --CALLS-->        UsersService      (so we can prove the DI
//	                                                     edge surfaces ALONGSIDE
//	                                                     CALLS, not instead of it)
func makeNestDIDoc() *graph.Document {
	return &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{ID: "svc", Name: "UsersService", Kind: "SCOPE.Component", SourceFile: "users.service.ts", StartLine: 1},
			{ID: "ctrl", Name: "UsersController", Kind: "SCOPE.Component", SourceFile: "users.controller.ts", StartLine: 1},
			{ID: "mod", Name: "UsersModule", Kind: "SCOPE.Component", SourceFile: "users.module.ts", StartLine: 1},
			{ID: "tok", Name: "USERS_TOKEN", Kind: "SCOPE.Component", SourceFile: "users.module.ts", StartLine: 5},
			{ID: "impl", Name: "UsersServiceImpl", Kind: "SCOPE.Component", SourceFile: "users.impl.ts", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			// provider INJECTED_INTO consumer (FromID=provider, ToID=consumer)
			graph.Relationship{ID: "d1", FromID: "svc", ToID: "ctrl", Kind: "INJECTED_INTO"}.WithProperties(map[string]string{"line": "12"}),
			// module BINDS token, token BINDS impl
			graph.Relationship{ID: "d2", FromID: "mod", ToID: "tok", Kind: "BINDS"}.WithProperties(map[string]string{"line": "8"}),
			graph.Relationship{ID: "d3", FromID: "tok", ToID: "impl", Kind: "BINDS"}.WithProperties(map[string]string{"line": "9"}),
			// a plain CALLS so we can assert DI surfaces alongside it
			graph.Relationship{ID: "c1", FromID: "ctrl", ToID: "svc", Kind: "CALLS"}.WithProperties(map[string]string{"line": "20"}),
		},
	}
}

func callNeighbors(t *testing.T, srv *Server, args map[string]any) []any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleGetNeighbors(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNeighbors error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("neighbors tool error: %v", res)
	}
	text := extractResultText(t, res)
	var arr []any
	if err := json.Unmarshal([]byte(text), &arr); err != nil {
		t.Fatalf("neighbors result not a JSON array: %v\nraw: %s", err, text)
	}
	return arr
}

// TestInspect_SurfacesInjectedInto_OnConsumer asserts that inspecting the
// consumer (controller) surfaces the INJECTED_INTO edge as an INBOUND DI edge
// from the provider — previously inspect projected only CALLS, so this edge was
// invisible.
func TestInspect_SurfacesInjectedInto_OnConsumer(t *testing.T) {
	srv := newTestServer(t, makeNestDIDoc())

	out := callInspectWithArgs(t, srv, map[string]any{
		"group":     "test",
		"entity_id": "ctrl",
	})

	di, ok := out["di_edges"]
	if !ok {
		t.Fatalf("expected di_edges on consumer inspect; keys: %v", mapKeys(out))
	}
	rows, ok := di.([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("di_edges is %T len=%d, want non-empty []any", di, len(rows))
	}
	// Find the INJECTED_INTO inbound edge to the provider "svc".
	found := false
	for _, r := range rows {
		m := r.(map[string]any)
		if m["kind"] == "INJECTED_INTO" && m["direction"] == "inbound" && m["other"] == "svc" {
			found = true
			if line, _ := m["line"].(float64); line != 12 {
				t.Errorf("INJECTED_INTO line = %v, want 12", m["line"])
			}
		}
	}
	if !found {
		t.Fatalf("did not find INJECTED_INTO(inbound, other=svc) in di_edges: %v", rows)
	}
	// Sanity: CALLS projection still works alongside DI (called_by from svc).
	if _, ok := out["called_by"]; !ok {
		t.Errorf("expected called_by to coexist with di_edges")
	}
}

// TestInspect_SurfacesInjectedInto_OnProvider asserts the provider sees the
// same edge as OUTBOUND (it is the FromID).
func TestInspect_SurfacesInjectedInto_OnProvider(t *testing.T) {
	srv := newTestServer(t, makeNestDIDoc())

	out := callInspectWithArgs(t, srv, map[string]any{
		"group":     "test",
		"entity_id": "svc",
	})
	di, ok := out["di_edges"].([]any)
	if !ok {
		t.Fatalf("expected di_edges on provider inspect; keys: %v", mapKeys(out))
	}
	found := false
	for _, r := range di {
		m := r.(map[string]any)
		if m["kind"] == "INJECTED_INTO" && m["direction"] == "outbound" && m["other"] == "ctrl" {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not find INJECTED_INTO(outbound, other=ctrl) on provider: %v", di)
	}
}

// TestInspect_SurfacesBinds_TokenToImpl asserts a BINDS(token→impl) edge
// surfaces on the token as an outbound BINDS to the impl.
func TestInspect_SurfacesBinds_TokenToImpl(t *testing.T) {
	srv := newTestServer(t, makeNestDIDoc())

	out := callInspectWithArgs(t, srv, map[string]any{
		"group":     "test",
		"entity_id": "tok",
	})
	di, ok := out["di_edges"].([]any)
	if !ok {
		t.Fatalf("expected di_edges on token inspect; keys: %v", mapKeys(out))
	}
	// token has: inbound BINDS from module, outbound BINDS to impl.
	sawTokenToImpl := false
	sawModuleToToken := false
	for _, r := range di {
		m := r.(map[string]any)
		if m["kind"] != "BINDS" {
			t.Errorf("unexpected non-BINDS di edge on token: %v", m)
		}
		if m["direction"] == "outbound" && m["other"] == "impl" {
			sawTokenToImpl = true
		}
		if m["direction"] == "inbound" && m["other"] == "mod" {
			sawModuleToToken = true
		}
	}
	if !sawTokenToImpl {
		t.Errorf("did not find BINDS(outbound, other=impl) on token: %v", di)
	}
	if !sawModuleToToken {
		t.Errorf("did not find BINDS(inbound, other=mod) on token: %v", di)
	}
}

// TestInspect_NoDIEdges_OmitsSection asserts the section is omitted entirely for
// an entity with no DI edges (additive / backward compatible).
func TestInspect_NoDIEdges_OmitsSection(t *testing.T) {
	doc := &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{ID: "lonely", Name: "Lonely", Kind: "SCOPE.Operation", SourceFile: "x.ts", StartLine: 1},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": "lonely"})
	if _, ok := out["di_edges"]; ok {
		t.Fatalf("di_edges should be omitted when entity has no DI edges")
	}
}

// TestNeighbors_AnnotatesInjectedIntoEdge asserts that grafel_expand on the
// consumer annotates the provider neighbour with di_kind=INJECTED_INTO and
// di_direction=inbound — distinguishing it from a plain CALLS neighbour, which
// it previously could NOT do (the edge kind was dropped from neighbour rows).
func TestNeighbors_AnnotatesInjectedIntoEdge(t *testing.T) {
	srv := newTestServer(t, makeNestDIDoc())

	rows := callNeighbors(t, srv, map[string]any{
		"group":        "test",
		"entity_id":    "ctrl",
		"depth":        1,
		"token_budget": 5000,
	})

	// The provider "svc" is both a CALLS target and an INJECTED_INTO source for
	// the controller. Assert the neighbour row carries the DI annotation.
	var svcRow map[string]any
	for _, r := range rows {
		m := r.(map[string]any)
		if m["id"] == "api::svc" {
			svcRow = m
		}
	}
	if svcRow == nil {
		t.Fatalf("provider neighbour api::svc not present in expand output: %v", rows)
	}
	if svcRow["di_kind"] != "INJECTED_INTO" {
		t.Errorf("di_kind = %v, want INJECTED_INTO", svcRow["di_kind"])
	}
	if svcRow["di_direction"] != "inbound" {
		t.Errorf("di_direction = %v, want inbound", svcRow["di_direction"])
	}
}

// TestNeighbors_NonDINeighborUnannotated asserts neighbours not connected by a
// DI edge do NOT carry the DI annotation (no false positives).
func TestNeighbors_NonDINeighborUnannotated(t *testing.T) {
	srv := newTestServer(t, makeNestDIDoc())
	// Inspect from the module: it BINDS the token (DI) but reaches impl at
	// depth 2 via token. At depth 1 only "tok" is a direct DI neighbour.
	rows := callNeighbors(t, srv, map[string]any{
		"group":        "test",
		"entity_id":    "mod",
		"depth":        2,
		"token_budget": 5000,
	})
	for _, r := range rows {
		m := r.(map[string]any)
		if m["id"] == "api::tok" {
			if m["di_kind"] != "BINDS" || m["di_direction"] != "outbound" {
				t.Errorf("module's direct BINDS neighbour tok mis-annotated: %v", m)
			}
		}
		// impl is depth-2 from module (not a DIRECT edge of mod) → no annotation.
		if m["id"] == "api::impl" {
			if _, has := m["di_kind"]; has {
				t.Errorf("depth-2 neighbour impl should NOT carry di_kind: %v", m)
			}
		}
	}
}
