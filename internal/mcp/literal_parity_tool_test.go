package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// enumEntity builds a SCOPE.Enum value-set entity with the given members_json,
// mirroring what the shared enum/value-set extractor emits.
func enumEntity(id, name, membersJSON string) graph.Entity {
	return graph.Entity{
		ID:            id,
		Name:          name,
		QualifiedName: "scope:enum:fixture.py:" + name,
		Kind:          string(types.EntityKindEnum),
		Properties: map[string]string{
			"enum_name":    name,
			"members_json": membersJSON,
		},
	}
}

// twoGroupServer builds a Server with two loaded groups, each holding one repo
// with the supplied entities.
func twoGroupServer(t *testing.T, oracleEnts, v3Ents []graph.Entity) *Server {
	t.Helper()
	reg := &Registry{Groups: map[string]RegistryGroup{
		"oracle": {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
		"v3":     {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["oracle"] = &LoadedGroup{
		Name:  "oracle",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: &graph.Document{Repo: "r", Entities: oracleEnts}}},
	}
	st.groups["v3"] = &LoadedGroup{
		Name:  "v3",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: &graph.Document{Repo: "r", Entities: v3Ents}}},
	}
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func callLiteralParity(t *testing.T, s *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_literal_parity"
	req.Params.Arguments = args
	res, err := s.handleLiteralParity(context.Background(), req)
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

// End-to-end: auto-locate the value-set by alias in both groups and report an
// equivalent verdict for a clean rewrite.
func TestLiteralParity_E2E_Equivalent(t *testing.T) {
	mj := `[{"key":"DASHBOARD","value":"dashboard"},{"key":"SETTINGS","value":"settings"}]`
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("o1", "PERMISSION_PAGES", mj)},
		[]graph.Entity{enumEntity("v1", "PermissionPage", mj)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "page_slugs",
	})
	if out["verdict"] != "equivalent" {
		t.Fatalf("verdict = %v, want equivalent; out=%+v", out["verdict"], out)
	}
	if out["oracle_source"] != "o1" || out["v3_source"] != "v1" {
		t.Errorf("auto-locate resolved wrong entities: %v / %v", out["oracle_source"], out["v3_source"])
	}
}

// End-to-end: a value mismatch (_ vs -) yields a drift verdict.
func TestLiteralParity_E2E_ValueMismatchDrift(t *testing.T) {
	oracleMJ := `[{"key":"ADMIN","value":"core_admin"}]`
	v3MJ := `[{"key":"ADMIN","value":"core-admin"}]`
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("o1", "ACTION_CODENAMES", oracleMJ)},
		[]graph.Entity{enumEntity("v1", "ActionCodename", v3MJ)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
	})
	if out["verdict"] != "drift" {
		t.Fatalf("verdict = %v, want drift", out["verdict"])
	}
	vm, _ := out["value_mismatches"].([]any)
	if len(vm) != 1 {
		t.Fatalf("value_mismatches = %v, want 1", out["value_mismatches"])
	}
	m := vm[0].(map[string]any)
	if m["oracle"] != "core_admin" || m["v3"] != "core-admin" {
		t.Errorf("mismatch payload = %+v", m)
	}
}

// End-to-end: explicit *_source entity ids pin the value-sets directly.
func TestLiteralParity_E2E_ExplicitSource(t *testing.T) {
	mj := `[{"key":"A","value":"a"}]`
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("oX", "SomethingObscure", mj)},
		[]graph.Entity{enumEntity("vX", "AlsoObscure", mj)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "enum:Whatever",
		"oracle_source": "oX", "v3_source": "vX",
	})
	if out["verdict"] != "equivalent" {
		t.Fatalf("verdict = %v, want equivalent", out["verdict"])
	}
	if out["oracle_source"] != "oX" || out["v3_source"] != "vX" {
		t.Errorf("explicit source not honoured: %+v", out)
	}
}

// End-to-end: missing required arg returns a tool error.
func TestLiteralParity_E2E_MissingArgs(t *testing.T) {
	s := twoGroupServer(t, nil, nil)
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_literal_parity"
	req.Params.Arguments = map[string]any{"group_oracle": "oracle"}
	res, err := s.handleLiteralParity(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing args")
	}
}

// End-to-end: auto-locate failure (no matching value-set) is a clean error,
// not a panic.
func TestLiteralParity_E2E_AutoLocateMiss(t *testing.T) {
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("o1", "Unrelated", `[{"key":"A","value":"a"}]`)},
		[]graph.Entity{enumEntity("v1", "Unrelated", `[{"key":"A","value":"a"}]`)},
	)
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_literal_parity"
	req.Params.Arguments = map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "status_strings",
	}
	res, err := s.handleLiteralParity(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected auto-locate miss error")
	}
}
