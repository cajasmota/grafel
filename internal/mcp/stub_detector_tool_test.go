package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// writeEffectsSidecar writes a <group>-links-effects.json under $HOME (set via
// t.Setenv) exactly as the effect-propagation pass does, so the stub detector
// resolves effects through its canonical on-disk path. entries maps a PREFIXED
// entity id ("<repo>::<local>") to its effect kinds; a pure handler still gets
// an entry (empty Effects, source "pure") so the closure reads as resolved.
func writeEffectsSidecar(t *testing.T, group string, entries map[string][]string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".archigraph", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	doc := effectsSidecarDoc{Version: 1, Method: "test"}
	for id, effs := range entries {
		src := "pure"
		if len(effs) > 0 {
			src = "direct"
		}
		doc.Entries = append(doc.Entries, effectsSidecarEntry{EntityID: id, Effects: effs, Source: src})
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	path := filepath.Join(dir, group+"-links-effects.json")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// endpointDef builds an http_endpoint_definition entity with verb/path props.
func endpointDef(id, verb, path string) graph.Entity {
	return graph.Entity{
		ID:   id,
		Name: verb + " " + path,
		Kind: string(types.EntityKindHTTPEndpointDefinition),
		Properties: map[string]string{
			"verb": verb,
			"path": path,
		},
	}
}

// handlerFn builds a handler entity (an Operation/function) carrying an
// effect-property list (the in-process effects fallback the stub detector
// reads when no sidecar is present). effects may be "" for a pure handler.
func handlerFn(id, name, effects string) graph.Entity {
	props := map[string]string{}
	if effects != "" {
		props["effects"] = effects
	}
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       string(types.EntityKindOperation),
		Properties: props,
	}
}

// implementsEdge links a handler to the endpoint definition it implements.
func implementsEdge(handlerID, defID string) graph.Relationship {
	return graph.Relationship{Kind: "IMPLEMENTS", FromID: handlerID, ToID: defID}
}

// callsEdge links a caller to a callee (downstream CALLS for the effects walk).
func callsEdge(from, to string) graph.Relationship {
	return graph.Relationship{Kind: "CALLS", FromID: from, ToID: to}
}

// stubTwoGroupServer builds a Server with a v3 group and an oracle group, each
// a single repo with the given entities + relationships.
func stubTwoGroupServer(t *testing.T, v3 *graph.Document, oracle *graph.Document) *Server {
	t.Helper()
	// Isolate the effects-sidecar lookup directory per test.
	t.Setenv("HOME", t.TempDir())
	reg := &Registry{Groups: map[string]RegistryGroup{
		"v3":     {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
		"oracle": {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["v3"] = &LoadedGroup{
		Name:  "v3",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: v3}},
	}
	st.groups["oracle"] = &LoadedGroup{
		Name:  "oracle",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: oracle}},
	}
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func callStubDetector(t *testing.T, s *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_stub_detector"
	req.Params.Arguments = args
	res, err := s.handleStubDetector(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return out
}

// resultFor returns the per-endpoint result map for the given endpoint label.
func resultFor(t *testing.T, out map[string]any, label string) map[string]any {
	t.Helper()
	results, _ := out["results"].([]any)
	for _, r := range results {
		m := r.(map[string]any)
		if m["endpoint"] == label {
			return m
		}
	}
	t.Fatalf("no result for endpoint %q in %+v", label, out["results"])
	return nil
}

// The marquee case: a v3 endpoint whose handler is pure (returns canned data)
// while the linked oracle counterpart's handler computes (db_write) → likely_stub.
func TestStubDetector_E2E_LikelyStub(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "GET", "/api/orders/{id}"),
			handlerFn("h1", "OrderView.retrieve", ""), // pure
		},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("od1", "GET", "/orders/{pk}"),
			handlerFn("oh1", "OrderViewSet.retrieve", "db_read,db_write"),
		},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubTwoGroupServer(t, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": nil})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_read", "db_write"}})
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})

	if out["likely_stubs"].(float64) != 1 {
		t.Fatalf("likely_stubs = %v, want 1; out=%+v", out["likely_stubs"], out)
	}
	r := resultFor(t, out, "GET /api/orders/{id}")
	if r["verdict"] != "likely_stub" {
		t.Fatalf("verdict = %v, want likely_stub", r["verdict"])
	}
	sigs := r["signals"].(map[string]any)
	if sigs["no_effects_v3"] != "yes" || sigs["oracle_has_effects"] != "yes" {
		t.Errorf("signals wrong: %+v", sigs)
	}
}

// Both sides compute → implemented, no stub flagged.
func TestStubDetector_E2E_Implemented(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "POST", "/api/orders"),
			handlerFn("h1", "OrderView.create", "db_write"),
		},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("od1", "POST", "/orders"),
			handlerFn("oh1", "OrderViewSet.create", "db_write"),
		},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubTwoGroupServer(t, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": {"db_write"}})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_write"}})
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})

	if out["likely_stubs"].(float64) != 0 {
		t.Fatalf("likely_stubs = %v, want 0", out["likely_stubs"])
	}
	r := resultFor(t, out, "POST /api/orders")
	if r["verdict"] != "implemented" {
		t.Fatalf("verdict = %v, want implemented", r["verdict"])
	}
}

// Thin on BOTH sides → thin, NOT a stub (false-positive guard).
func TestStubDetector_E2E_ThinBothSides(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "GET", "/api/health"),
			handlerFn("h1", "health", ""),
		},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("od1", "GET", "/health"),
			handlerFn("oh1", "health_check", ""),
		},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubTwoGroupServer(t, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": nil})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": nil})
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})

	if out["likely_stubs"].(float64) != 0 {
		t.Fatalf("likely_stubs = %v, want 0 (both thin)", out["likely_stubs"])
	}
	r := resultFor(t, out, "GET /api/health")
	if r["verdict"] != "thin" {
		t.Fatalf("verdict = %v, want thin", r["verdict"])
	}
}

// The effects walk follows downstream CALLS: a thin v3 controller that
// delegates to a pure service is still pure; the oracle's controller delegates
// to a db_write service → contrast → likely_stub. Exercises the transitive walk.
func TestStubDetector_E2E_TransitiveDelegation(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "GET", "/api/report"),
			handlerFn("h1", "ReportView.get", ""),        // thin controller
			handlerFn("svc1", "ReportService.build", ""), // pure service (canned)
		},
		Relationships: []graph.Relationship{
			implementsEdge("h1", "d1"),
			callsEdge("h1", "svc1"),
		},
	}
	oracle := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("od1", "GET", "/report"),
			handlerFn("oh1", "ReportViewSet.get", ""),            // thin controller
			handlerFn("osvc1", "ReportService.build", "db_read"), // real query
		},
		Relationships: []graph.Relationship{
			implementsEdge("oh1", "od1"),
			callsEdge("oh1", "osvc1"),
		},
	}
	s := stubTwoGroupServer(t, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": nil, "r::svc1": nil})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": nil, "r::osvc1": {"db_read"}})
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})

	r := resultFor(t, out, "GET /api/report")
	if r["verdict"] != "likely_stub" {
		t.Fatalf("verdict = %v, want likely_stub (transitive contrast)", r["verdict"])
	}
	oe, _ := r["oracle_effects"].([]any)
	if len(oe) != 1 || oe[0] != "db_read" {
		t.Errorf("oracle_effects = %v, want [db_read] (transitive)", r["oracle_effects"])
	}
}

// A v3 endpoint with no oracle counterpart is reported under unlinked_v3, not
// scored.
func TestStubDetector_E2E_Unlinked(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "GET", "/api/new-feature"),
			handlerFn("h1", "NewView.get", ""),
		},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r"} // no endpoints
	s := stubTwoGroupServer(t, v3, oracle)
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})

	if out["linked_count"].(float64) != 0 {
		t.Fatalf("linked_count = %v, want 0", out["linked_count"])
	}
	unlinked, _ := out["unlinked_v3"].([]any)
	if len(unlinked) != 1 || unlinked[0] != "GET /api/new-feature" {
		t.Fatalf("unlinked_v3 = %v, want [GET /api/new-feature]", out["unlinked_v3"])
	}
}

// The optional endpoint filter narrows scoring to one endpoint.
func TestStubDetector_E2E_EndpointFilter(t *testing.T) {
	v3 := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("d1", "GET", "/api/orders/{id}"),
			handlerFn("h1", "OrderView.retrieve", ""),
			endpointDef("d2", "POST", "/api/orders"),
			handlerFn("h2", "OrderView.create", "db_write"),
		},
		Relationships: []graph.Relationship{
			implementsEdge("h1", "d1"),
			implementsEdge("h2", "d2"),
		},
	}
	oracle := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			endpointDef("od1", "GET", "/orders/{pk}"),
			handlerFn("oh1", "OrderViewSet.retrieve", "db_read"),
			endpointDef("od2", "POST", "/orders"),
			handlerFn("oh2", "OrderViewSet.create", "db_write"),
		},
		Relationships: []graph.Relationship{
			implementsEdge("oh1", "od1"),
			implementsEdge("oh2", "od2"),
		},
	}
	s := stubTwoGroupServer(t, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": nil, "r::h2": {"db_write"}})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_read"}, "r::oh2": {"db_write"}})
	out := callStubDetector(t, s, map[string]any{
		"group_v3": "v3", "group_oracle": "oracle",
		"endpoint": "GET /api/orders/{id}",
	})
	if out["linked_count"].(float64) != 1 {
		t.Fatalf("linked_count = %v, want 1 (filtered)", out["linked_count"])
	}
	r := resultFor(t, out, "GET /api/orders/{id}")
	if r["verdict"] != "likely_stub" {
		t.Errorf("verdict = %v, want likely_stub", r["verdict"])
	}
}

// Missing required args returns a tool error.
func TestStubDetector_E2E_MissingArgs(t *testing.T) {
	s := stubTwoGroupServer(t, &graph.Document{Repo: "r"}, &graph.Document{Repo: "r"})
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_stub_detector"
	req.Params.Arguments = map[string]any{"group_v3": "v3"}
	res, err := s.handleStubDetector(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing group_oracle")
	}
}
