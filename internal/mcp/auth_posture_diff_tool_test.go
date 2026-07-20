package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// endpointEntity builds an http_endpoint_definition entity with the given verb,
// path, and auth-posture properties — mirroring what the engine stamps.
func endpointEntity(id, verb, path string, props map[string]string) graph.Entity {
	p := map[string]string{"verb": verb, "path": path}
	for k, v := range props {
		p[k] = v
	}
	return graph.Entity{
		ID:   id,
		Name: verb + " " + path,
		Kind: "http_endpoint_definition",
	}.WithProperties(p)
}

// twoGroupEndpointServer builds a Server with an oracle and a v3 group, each
// holding one repo with the supplied endpoint entities.
func twoGroupEndpointServer(t *testing.T, oracleEnts, v3Ents []graph.Entity) *Server {
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

func callAuthPostureDiff(t *testing.T, s *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "grafel_auth_posture_diff"
	req.Params.Arguments = args
	res, err := s.handleAuthPostureDiff(context.Background(), req)
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

// firstRecord returns the single diff record's verdict from a tool result.
func firstRecordVerdict(t *testing.T, out map[string]any) string {
	t.Helper()
	recs, _ := out["records"].([]any)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d; out=%+v", len(recs), out)
	}
	m := recs[0].(map[string]any)
	return m["verdict"].(string)
}

const e2eGetPerms = `
def get_permissions(self):
    if self.action == "approve":
        return [CustomPagePermissionCheck(PERMISSION_PAGES["client_admin"])]
    else:
        return [CustomActionPermissionCheck()]
`

// E2E: oracle page grant, v3 @RequirePage with the SAME slug (hyphen variant) →
// equivalent.
func TestAuthPostureDiff_E2E_Equivalent(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "POST", "/clients/{id}/approve", map[string]string{
			"has_get_permissions": "true", "get_permissions_source": e2eGetPerms, "effective_action": "approve",
		})},
		[]graph.Entity{endpointEntity("v1", "POST", "/clients/:id/approve", map[string]string{
			"require_page": "client-admin", "effective_action": "approve",
		})},
	)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3", "format": "full"})
	if v := firstRecordVerdict(t, out); v != "equivalent" {
		t.Fatalf("verdict=%s, want equivalent; out=%+v", v, out)
	}
}

// E2E: oracle page grant, v3 only authenticated → looser (the RBAC regression).
func TestAuthPostureDiff_E2E_Looser(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "POST", "/clients/{id}/approve", map[string]string{
			"has_get_permissions": "true", "get_permissions_source": e2eGetPerms, "effective_action": "approve",
		})},
		[]graph.Entity{endpointEntity("v1", "POST", "/clients/:id/approve", map[string]string{
			"auth_required": "true", "auth_guard": "JwtAuthGuard", "effective_action": "approve",
		})},
	)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3"})
	if v := firstRecordVerdict(t, out); v != "looser" {
		t.Fatalf("verdict=%s, want looser; out=%+v", v, out)
	}
}

// E2E: oracle page client_admin vs v3 page billing_admin (genuinely different
// slug) → slug_mismatch.
func TestAuthPostureDiff_E2E_SlugMismatch(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "POST", "/clients/{id}/approve", map[string]string{
			"has_get_permissions": "true", "get_permissions_source": e2eGetPerms, "effective_action": "approve",
		})},
		[]graph.Entity{endpointEntity("v1", "POST", "/clients/:id/approve", map[string]string{
			"require_page": "billing_admin", "effective_action": "approve",
		})},
	)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3"})
	if v := firstRecordVerdict(t, out); v != "slug_mismatch" {
		t.Fatalf("verdict=%s, want slug_mismatch; out=%+v", v, out)
	}
}

// E2E: the §10 else default arm is an ACTION grant; v3 @RequirePage on the same
// endpoint → kind_mismatch (page vs action, same strength). This is the test
// that would FAIL if the decoder mis-treated else as authenticated.
func TestAuthPostureDiff_E2E_KindMismatch_ElseIsAction(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "GET", "/clients", map[string]string{
			"has_get_permissions": "true", "get_permissions_source": e2eGetPerms, "effective_action": "list",
		})},
		[]graph.Entity{endpointEntity("v1", "GET", "/clients", map[string]string{
			"require_page": "client_admin", "effective_action": "list",
		})},
	)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3"})
	if v := firstRecordVerdict(t, out); v != "kind_mismatch" {
		t.Fatalf("verdict=%s, want kind_mismatch (else=action vs v3 page); out=%+v", v, out)
	}
}

// E2E: missing required arg returns a tool error.
func TestAuthPostureDiff_E2E_MissingArgs(t *testing.T) {
	s := twoGroupEndpointServer(t, nil, nil)
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "grafel_auth_posture_diff"
	req.Params.Arguments = map[string]any{"group_oracle": "oracle"}
	res, err := s.handleAuthPostureDiff(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing args")
	}
}

// Regression for #4550: the live acme (DRF) ↔ acme-v3 (NestJS) endpoint key
// sets joined ZERO under the old auth_posture_diff key, while stub_detector
// joined 420/446 on the SAME data. Two divergences made the old key never match:
//
//  1. api-prefix: DRF stamps /api/v1/... while NestJS stamps /v1/... — the old
//     normalizeEndpointPath did NOT strip the /api[/vN] prefix.
//  2. #action fold: the old key appended "#<action>" from the oracle's
//     effective_action, but NestJS endpoints carry no action — so even when the
//     paths lined up the suffix split the buckets.
//
// This fixture reproduces both: every oracle row is /api/v1/... + an
// effective_action; every v3 row is /v1/... with NO action and a param-name
// drift ({id} vs :id, dup handler names across resources). The shared join key
// (endpoint_join.go) must produce a NON-TRIVIAL join. Under the old key this
// asserted 0 (RED).
func TestAuthPostureDiff_Regression4550_AcmeV3Join(t *testing.T) {
	// DRF oracle: /api/v1 prefix, brace params, an effective_action per row.
	oracle := []graph.Entity{
		endpointEntity("o-list", "GET", "/api/v1/clients", map[string]string{
			"auth_required": "true", "effective_action": "list",
		}),
		endpointEntity("o-retrieve", "GET", "/api/v1/clients/{id}", map[string]string{
			"auth_required": "true", "effective_action": "retrieve",
		}),
		endpointEntity("o-approve", "POST", "/api/v1/clients/{id}/approve", map[string]string{
			"auth_required": "true", "effective_action": "approve",
		}),
		// dup handler-name shape: a second resource with the same trailing verb.
		endpointEntity("o-dev-list", "GET", "/api/v1/devices", map[string]string{
			"auth_required": "true", "effective_action": "list",
		}),
		endpointEntity("o-dev-retrieve", "GET", "/api/v1/devices/{pk}", map[string]string{
			"auth_required": "true", "effective_action": "retrieve",
		}),
	}
	// NestJS v3: /v1 prefix (no /api), colon params, NO action stamped.
	v3 := []graph.Entity{
		endpointEntity("v-list", "GET", "/v1/clients", map[string]string{"auth_required": "true"}),
		endpointEntity("v-retrieve", "GET", "/v1/clients/:id", map[string]string{"auth_required": "true"}),
		endpointEntity("v-approve", "POST", "/v1/clients/:id/approve", map[string]string{"auth_required": "true"}),
		endpointEntity("v-dev-list", "GET", "/v1/devices", map[string]string{"auth_required": "true"}),
		endpointEntity("v-dev-retrieve", "GET", "/v1/devices/:id", map[string]string{"auth_required": "true"}),
	}
	s := twoGroupEndpointServer(t, oracle, v3)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3"})

	joined, _ := out["joined"].(float64)
	if joined < 5 {
		t.Fatalf("joined=%v, want all 5 oracle↔v3 pairs to join "+
			"(api-prefix strip + no #action fold); RED=0 under the old key; out=%+v", joined, out)
	}
}

// Regression for #4550: the shared endpoint join key must fold the DRF
// /api/v1/... ↔ NestJS /v1/... prefix divergence and param-name drift to the
// SAME bucket, and must NOT depend on an action suffix.
func TestEndpointJoinKey_PrefixAndParamFold(t *testing.T) {
	cases := []struct{ verb, oracle, v3 string }{
		{"GET", "/api/v1/clients", "/v1/clients"},
		{"GET", "/api/v1/clients/{id}", "/v1/clients/:id"},
		{"POST", "/api/v1/clients/{id}/approve", "/v1/clients/:pk/approve"},
		{"GET", "/api/v2/devices/<int:pk>", "/devices/{id}"},
		{"GET", "/api/v1/clients/", "/v1/clients"}, // trailing slash fold
	}
	for _, c := range cases {
		ok := newEndpointJoinKey(c.verb, c.oracle)
		vk := newEndpointJoinKey(c.verb, c.v3)
		if ok != vk {
			t.Errorf("join keys differ: oracle %q→%+v vs v3 %q→%+v", c.oracle, ok, c.v3, vk)
		}
	}
}

// E2E: an unlinked oracle endpoint (no v3 counterpart) is simply omitted, not an
// error — the join is inner.
func TestAuthPostureDiff_E2E_UnlinkedOmitted(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "GET", "/only-oracle", map[string]string{"auth_required": "true"})},
		[]graph.Entity{endpointEntity("v1", "GET", "/only-v3", map[string]string{"auth_required": "true"})},
	)
	out := callAuthPostureDiff(t, s, map[string]any{"group_oracle": "oracle", "group_v3": "v3"})
	if j, _ := out["joined"].(float64); j != 0 {
		t.Fatalf("joined=%v, want 0 (no shared endpoint)", out["joined"])
	}
}
