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

// BUG B (#4532): auto-locate failure on BOTH sides (no semantic counterpart) is
// NOT a fabricated comparison — it returns verdict:"unresolved" with both
// sources null and a note, not a tool error and not a wrong-set diff.
func TestLiteralParity_E2E_UnresolvedBothSides(t *testing.T) {
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("o1", "Unrelated", `[{"key":"A","value":"a"}]`)},
		[]graph.Entity{enumEntity("v1", "Unrelated", `[{"key":"A","value":"a"}]`)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "status_strings",
	})
	if out["verdict"] != "unresolved" {
		t.Fatalf("verdict = %v, want unresolved; out=%+v", out["verdict"], out)
	}
	if out["oracle_source"] != nil || out["v3_source"] != nil {
		t.Errorf("expected null sources, got %v / %v", out["oracle_source"], out["v3_source"])
	}
	if _, ok := out["note"].(string); !ok || out["note"] == "" {
		t.Errorf("expected a non-empty note, got %v", out["note"])
	}
	vm, _ := out["value_mismatches"].([]any)
	if len(vm) != 0 {
		t.Errorf("expected no fabricated comparison, got value_mismatches=%v", vm)
	}
}

// BUG B (#4532): the OLD substring auto-locate compared unrelated "Action" enums
// (SyncActionStatus ↔ OutboxAction). Now: no exact semantic counterpart for the
// action_codenames alias exists on the oracle side (only SyncActionStatus, which
// is NOT the codename set), so the result is unresolved on that side — not a
// silent wrong-set comparison. The v3 side, which DOES have a real ActionCodename
// set, is reported.
func TestLiteralParity_E2E_NoSubstringWrongSet(t *testing.T) {
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("o1", "SyncActionStatus", `[{"key":"PENDING","value":"pending"}]`)},
		[]graph.Entity{enumEntity("v1", "ActionCodename", `[{"key":"LITE","value":"lite"}]`)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
	})
	if out["verdict"] != "unresolved" {
		t.Fatalf("verdict = %v, want unresolved (wrong-set must NOT be compared); out=%+v", out["verdict"], out)
	}
	if out["oracle_source"] != nil {
		t.Errorf("oracle should be unresolved (SyncActionStatus is the wrong set), got %v", out["oracle_source"])
	}
	if out["v3_source"] != "v1" {
		t.Errorf("v3 ActionCodename should resolve, got %v", out["v3_source"])
	}
}

// BUG B (#4532): for an implicit-on-one-side set, an explicit oracle_source
// override pins the value-set and yields a real comparison instead of unresolved.
func TestLiteralParity_E2E_ExplicitOverrideResolvesImplicit(t *testing.T) {
	s := twoGroupServer(t,
		[]graph.Entity{enumEntity("oImplicit", "SyncActionStatus", `[{"key":"LITE","value":"lite"}]`)},
		[]graph.Entity{enumEntity("v1", "ActionCodename", `[{"key":"LITE","value":"lite"}]`)},
	)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
		"oracle_source": "oImplicit",
	})
	if out["verdict"] != "equivalent" {
		t.Fatalf("verdict = %v, want equivalent with explicit oracle_source; out=%+v", out["verdict"], out)
	}
	if out["oracle_source"] != "oImplicit" || out["v3_source"] != "v1" {
		t.Errorf("sources = %v / %v", out["oracle_source"], out["v3_source"])
	}
}

// drfActionMethod builds a DRF @action-decorated method entity, optionally with
// an explicit url_path (empty → codename defaults to the bare method name).
func drfActionMethod(id, name, urlPath string) graph.Entity {
	props := map[string]string{"drf_action": "true"}
	if urlPath != "" {
		props["url_path"] = urlPath
	}
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "SCOPE.Operation",
		Subtype:    "method",
		Properties: props,
	}
}

// twoGroupServerDoc is like twoGroupServer but takes whole Documents so a side
// can carry relationships (CONTAINS) for viewset-scoped derivation.
func twoGroupServerDoc(t *testing.T, oracleDoc, v3Doc *graph.Document) *Server {
	t.Helper()
	reg := &Registry{Groups: map[string]RegistryGroup{
		"oracle": {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
		"v3":     {Repos: map[string]RegistryRepo{"r": {Path: t.TempDir()}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["oracle"] = &LoadedGroup{
		Name:  "oracle",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: oracleDoc}},
	}
	st.groups["v3"] = &LoadedGroup{
		Name:  "v3",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: v3Doc}},
	}
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

// BUG B (#4665 part b): the DERIVATION RESOLVER yields the implicit oracle set
// (DRF action codenames = @action method names / url_paths) and diffs it against
// a declared v3 set — turning an otherwise-unresolved comparison into a real one.
// Here the oracle has NO declared enum, only @action methods; v3 has a declared
// ActionCodename enum that drifts (missing 'cancel'). Expect a real drift verdict.
func TestLiteralParity_E2E_DerivationResolver(t *testing.T) {
	oracleDoc := &graph.Document{Repo: "r", Entities: []graph.Entity{
		drfActionMethod("m1", "ProposalViewSet.lite", ""),
		drfActionMethod("m2", "ProposalViewSet.send_proposals", "send_proposals"),
		drfActionMethod("m3", "ProposalViewSet.cancel", ""),
	}}
	v3Doc := &graph.Document{Repo: "r", Entities: []graph.Entity{
		// v3 declared the codenames but DROPPED 'cancel' (membership drift).
		enumEntity("v1", "PermissionAction",
			`[{"key":"LITE","value":"lite"},{"key":"SEND_PROPOSALS","value":"send_proposals"}]`),
	}}
	s := twoGroupServerDoc(t, oracleDoc, v3Doc)

	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
		"oracle_derive": "drf_action_codenames", "v3_source": "v1",
	})
	if out["verdict"] != "drift" {
		t.Fatalf("verdict = %v, want drift; out=%+v", out["verdict"], out)
	}
	// The derived oracle set has 'cancel' which v3 lacks.
	oo, _ := out["only_in_oracle"].([]any)
	foundCancel := false
	for _, v := range oo {
		if v == "cancel" {
			foundCancel = true
		}
	}
	if !foundCancel {
		t.Errorf("expected derived oracle codename 'cancel' in only_in_oracle, got %v", out["only_in_oracle"])
	}
	if out["v3_source"] != "v1" {
		t.Errorf("v3_source = %v, want v1", out["v3_source"])
	}
}

// BUG B (#4665 part b): when BOTH sides are derived and reconciled, the verdict
// is equivalent. Also exercises the explicit url_path override (DRF non-default).
func TestLiteralParity_E2E_DerivationBothSidesEquivalent(t *testing.T) {
	mk := func() *graph.Document {
		return &graph.Document{Repo: "r", Entities: []graph.Entity{
			drfActionMethod("a1", "VS.lite", ""),
			drfActionMethod("a2", "VS.send", "send_proposals"),
		}}
	}
	s := twoGroupServerDoc(t, mk(), mk())
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
		"oracle_derive": "drf_action_codenames", "v3_derive": "drf_action_codenames",
	})
	if out["verdict"] != "equivalent" {
		t.Fatalf("verdict = %v, want equivalent; out=%+v", out["verdict"], out)
	}
}

// BUG B (#4665 part b): a `viewset` scope restricts the derived set to ONE
// ViewSet's @action methods, so an unrelated ViewSet's actions don't leak in.
func TestLiteralParity_E2E_DerivationViewsetScope(t *testing.T) {
	oracleDoc := &graph.Document{Repo: "r",
		Entities: []graph.Entity{
			{ID: "vsA", Name: "ProposalViewSet", Kind: "SCOPE.Component", Subtype: "class"},
			{ID: "vsB", Name: "DeviceViewSet", Kind: "SCOPE.Component", Subtype: "class"},
			drfActionMethod("mA", "ProposalViewSet.lite", ""),
			drfActionMethod("mB", "DeviceViewSet.reboot", ""), // unrelated — must be excluded
		},
		Relationships: []graph.Relationship{
			{ID: "c1", FromID: "vsA", ToID: "mA", Kind: "CONTAINS"},
			{ID: "c2", FromID: "vsB", ToID: "mB", Kind: "CONTAINS"},
		},
	}
	v3Doc := &graph.Document{Repo: "r", Entities: []graph.Entity{
		enumEntity("v1", "PermissionAction", `[{"key":"LITE","value":"lite"}]`),
	}}
	s := twoGroupServerDoc(t, oracleDoc, v3Doc)
	out := callLiteralParity(t, s, map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
		"oracle_derive": "drf_action_codenames", "viewset": "ProposalViewSet",
		"v3_source": "v1",
	})
	if out["verdict"] != "equivalent" {
		t.Fatalf("verdict = %v, want equivalent (reboot must be scoped out); out=%+v", out["verdict"], out)
	}
}

// BUG B (#4665 part b): an unknown derivation kind is a hard error (the caller
// explicitly asked to derive), not a silent unresolved.
func TestLiteralParity_E2E_DerivationUnknownKind(t *testing.T) {
	s := twoGroupServerDoc(t,
		&graph.Document{Repo: "r"},
		&graph.Document{Repo: "r", Entities: []graph.Entity{
			enumEntity("v1", "PermissionAction", `[{"key":"LITE","value":"lite"}]`)}},
	)
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_literal_parity"
	req.Params.Arguments = map[string]any{
		"group_oracle": "oracle", "group_v3": "v3", "set": "action_codenames",
		"oracle_derive": "bogus_kind", "v3_source": "v1",
	}
	res, err := s.handleLiteralParity(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for unknown derivation kind")
	}
}
