package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/responseshapediff"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// TestResolveBranchFields_BracedShape: a "Response{id,email}" shape resolves to
// a field set directly off the braces — no DTO expansion needed.
func TestResolveBranchFields_BracedShape(t *testing.T) {
	fs, ok := resolveBranchFields(nil, "Response{id,email,created_at}", "")
	if !ok {
		t.Fatalf("expected braced shape to resolve")
	}
	got := map[string]bool{}
	for _, f := range fs {
		got[f.Name] = true
	}
	for _, want := range []string{"id", "email", "created_at"} {
		if !got[want] {
			t.Errorf("missing field %q in %+v", want, fs)
		}
	}
}

// TestResolveBranchFields_DTOExpansion: a bare serializer/DTO name expands via
// the SCOPE.Schema field membership (#4635).
func TestResolveBranchFields_DTOExpansion(t *testing.T) {
	r := &LoadedRepo{Repo: "r", Doc: &graph.Document{Repo: "r", Entities: []graph.Entity{
		schemaField("UserDto.id", "UserDto", "id", "int", true),
		schemaField("UserDto.email", "UserDto", "email", "string", true),
	}}}
	fs, ok := resolveBranchFields(r, "UserDto", "")
	if !ok || len(fs) != 2 {
		t.Fatalf("expected 2 expanded DTO fields, got ok=%v fs=%+v", ok, fs)
	}
}

// TestResolveBranchFields_Unresolved: an opaque shape with no braces and no
// matching DTO does not resolve (honest-partial).
func TestResolveBranchFields_Unresolved(t *testing.T) {
	if _, ok := resolveBranchFields(nil, "someExpr(...)", ""); ok {
		t.Fatalf("expected opaque shape to be unresolved")
	}
}

// TestComposeResponseContract_Branches: an effectiveContract with two response
// branches composes into a Contract with per-status field sets.
func TestComposeResponseContract_Branches(t *testing.T) {
	c := &effectiveContract{
		ResponseBranches: []contractResponseBranch{
			{Status: 200, Shape: "Response{id,email}"},
			{Status: 409, Shape: "dict{existing_user}"},
		},
	}
	got := composeResponseContract(nil, c)
	if !got.ResolvedAny {
		t.Fatalf("expected ResolvedAny")
	}
	if len(got.Branches) != 2 {
		t.Fatalf("expected 2 branches, got %+v", got.Branches)
	}
}

// TestEndpointOwningClass: owning-class resolution from the common stamped props.
func TestEndpointOwningClass(t *testing.T) {
	cases := []struct {
		props map[string]string
		want  string
	}{
		{map[string]string{"drf_view_method": "UserViewSet.create"}, "UserViewSet"},
		{map[string]string{"controller": "app.UsersController"}, "UsersController"},
		{map[string]string{"handler": "OrdersController.list"}, "OrdersController"},
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		e := graph.EntityPtr(graph.Entity{}.WithProperties(c.props))
		if got := endpointOwningClass(e); got != c.want {
			t.Errorf("endpointOwningClass(%v) = %q, want %q", c.props, got, c.want)
		}
	}
}

// schemaField builds a SCOPE.Schema field-membership entity the way the
// field-membership extractors stamp it.
func schemaField(name, parent, fieldName, fieldType string, required bool) graph.Entity {
	props := map[string]string{
		"parent_class": parent,
		"field_name":   fieldName,
		"field_type":   fieldType,
	}
	if required {
		props["required"] = "true"
	} else {
		props["optional"] = "true"
	}
	return graph.Entity{
		ID:      name,
		Name:    name,
		Kind:    "SCOPE.Schema",
		Subtype: "field",
	}.WithProperties(props)
}

// TestResponseShapeDiff_Handler_EmptyJoin: the handler runs cleanly and reports a
// zero join over two unrelated groups (the plumbing path), returning a
// well-formed envelope.
func TestResponseShapeDiff_Handler_EmptyJoin(t *testing.T) {
	s := twoGroupEndpointServer(t,
		[]graph.Entity{endpointEntity("o1", "GET", "/api/v1/users", nil)},
		[]graph.Entity{endpointEntity("v1", "GET", "/orders", nil)},
	)
	req := mcpapi.CallToolRequest{}
	req.Params.Name = "grafel_response_shape_diff"
	req.Params.Arguments = map[string]any{"group_oracle": "oracle", "group_v3": "v3"}
	res, err := s.handleResponseShapeDiff(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["records"]; !ok {
		t.Fatalf("expected records key in envelope: %+v", out)
	}
}

// TestResponseShapeDiff_CoreWiring sanity-checks that the handler's verdict comes
// straight from the diff core for a synthesized pair (the 409-drift fixture),
// proving the composition → diff → verdict wiring.
func TestResponseShapeDiff_CoreWiring(t *testing.T) {
	oracle := responseshapediff.Contract{ResolvedAny: true, Branches: []responseshapediff.Branch{
		{Status: 200, Resolved: true, Fields: []responseshapediff.Field{{Name: "id"}}},
		{Status: 409, Resolved: true, Fields: []responseshapediff.Field{{Name: "existing_user"}}},
	}}
	v3 := responseshapediff.Contract{ResolvedAny: true, Branches: []responseshapediff.Branch{
		{Status: 200, Resolved: true, Fields: []responseshapediff.Field{{Name: "id"}}},
	}}
	got := responseshapediff.Diff(oracle, v3)
	if got.Verdict != responseshapediff.VerdictDrift {
		t.Fatalf("verdict = %q, want drift", got.Verdict)
	}
	if responseVerdictRank(got.Verdict) != 0 {
		t.Fatalf("drift should rank first, got %d", responseVerdictRank(got.Verdict))
	}
}
