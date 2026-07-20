package mcp

// effective_contract_tool_test.go — value-asserting coverage for the
// grafel_effective_contract MCP tool (#3836, epic #3829 MRO T6).
//
// These assert the STRUCTURED per-verb contract grouped under the ViewSet, not
// len>0:
//
//  1. effective_contract on a ModelViewSet groups its router-expanded routes
//     into per-verb contracts: inherited create {kind:inherited,
//     source_class:CreateModelMixin, default_status:201, error_statuses:[400]},
//     explicit list {kind:explicit, status 200, pagination}, @action approve
//     {kind:action, no fabricated status}.
//  2. Targeting a single route resolves to the SAME ViewSet group.
//  3. Honest-partial: a verb whose route omitted the pack fields surfaces the
//     resolvable fields and omits the unknown ones (status 0, no error_statuses).
//  4. NEGATIVE: a ViewSet with no router-expanded routes returns an empty group
//     set with the reindex note — not an error, not fabricated handlers.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// roleViewSetDoc builds a DRF ModelViewSet (RoleViewSet) plus the four
// router-expanded route entities the engine would stamp for it: an INHERITED
// create (POST, 201 + [400], CreateModelMixin), an EXPLICIT list (GET, 200,
// paginated), an @action approve (POST, no curated status), and an
// honest-partial verb (GET retrieve on an unknown base — no pack fields).
func roleViewSetDoc() *graph.Document {
	route := func(id string, props map[string]string) graph.Entity {
		p := map[string]string{"pattern_type": "drf_router_expanded", "framework": "django"}
		for k, v := range props {
			p[k] = v
		}
		return graph.Entity{ID: id, Name: id, Kind: "http_endpoint", Language: "python"}.WithProperties(p)
	}
	return &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "roleviewset", Name: "RoleViewSet", QualifiedName: "core.views.RoleViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/views.py",
				StartLine: 1, EndLine: 3, Language: "python"},
			// INHERITED create — the #278 case.
			route("http:create", map[string]string{
				"verb": "POST", "path": "/api/v1/roles",
				"drf_view_method":          "RoleViewSet.create",
				"provenance":               "inherited",
				"effective_kind":           "inherited",
				"effective_source_class":   "CreateModelMixin",
				"effective_status":         "201",
				"effective_error_statuses": "400",
				"serializer_class":         "RoleSerializer",
				"middleware_names":         "IsAuthenticated",
				"auth_required":            "true",
			}),
			// EXPLICIT list, paginated.
			route("http:list", map[string]string{
				"verb": "GET", "path": "/api/v1/roles",
				"drf_view_method":        "RoleViewSet.list",
				"provenance":             "explicit",
				"effective_kind":         "explicit",
				"effective_source_class": "RoleViewSet",
				"effective_status":       "200",
				"effective_pagination":   "true",
				"serializer_class":       "RoleSerializer",
			}),
			// @action approve — no fabricated status.
			route("http:approve", map[string]string{
				"verb": "POST", "path": "/api/v1/roles/{pk}/approve",
				"drf_view_method":        "RoleViewSet.approve",
				"provenance":             "action",
				"effective_kind":         "action",
				"effective_source_class": "RoleViewSet",
				"serializer_class":       "RoleSerializer",
			}),
			// Honest-partial retrieve — unknown base, no pack fields.
			route("http:retrieve", map[string]string{
				"verb": "GET", "path": "/api/v1/roles/{pk}",
				"drf_view_method":        "RoleViewSet.retrieve",
				"effective_kind":         "explicit",
				"effective_source_class": "RoleViewSet",
			}),
			// A DIFFERENT ViewSet's route — must NOT leak into the RoleViewSet group.
			route("http:other", map[string]string{
				"verb": "GET", "path": "/api/v1/widgets",
				"drf_view_method":        "WidgetViewSet.list",
				"effective_kind":         "explicit",
				"effective_source_class": "WidgetViewSet",
			}),
		},
	}
}

// callEffectiveContract invokes the tool and returns the decoded result.
func callEffectiveContract(t *testing.T, srv *Server, entityID string) effectiveContractResult {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": entityID}
	res, err := srv.handleEffectiveContract(context.Background(), req)
	if err != nil {
		t.Fatalf("handleEffectiveContract error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleEffectiveContract returned IsError: %s", resultText(res))
	}
	var out effectiveContractResult
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, resultText(res))
	}
	return out
}

// handlerByVerbPath finds the per-verb contract for the given verb+path in a
// group, or fails the test.
func handlerByVerbPath(t *testing.T, g effectiveContractGroup, verb, path string) effectiveContract {
	t.Helper()
	for _, h := range g.Handlers {
		if h.Verb == verb && h.Path == path {
			return h
		}
	}
	t.Fatalf("no handler %s %s in group %s; handlers=%+v", verb, path, g.Class, g.Handlers)
	return effectiveContract{}
}

// TestEffectiveContract_ModelViewSet_PerVerbShape asserts the grouped per-verb
// contract for RoleViewSet: inherited create, explicit list, @action approve,
// honest-partial retrieve — and that WidgetViewSet's route does not leak in.
func TestEffectiveContract_ModelViewSet_PerVerbShape(t *testing.T) {
	srv := newTestServer(t, roleViewSetDoc())
	out := callEffectiveContract(t, srv, "RoleViewSet")

	if len(out.Groups) != 1 {
		t.Fatalf("expected exactly 1 group (RoleViewSet), got %d: %+v", len(out.Groups), out.Groups)
	}
	g := out.Groups[0]
	if g.Class != "RoleViewSet" {
		t.Errorf("group class = %q; want RoleViewSet", g.Class)
	}
	if g.Framework != "django" {
		t.Errorf("framework = %q; want django", g.Framework)
	}
	if len(g.Handlers) != 4 {
		t.Fatalf("expected 4 handlers (no WidgetViewSet leak), got %d: %+v", len(g.Handlers), g.Handlers)
	}

	// INHERITED create — the #278 contract.
	create := handlerByVerbPath(t, g, "POST", "/api/v1/roles")
	if create.Kind != "inherited" {
		t.Errorf("create kind = %q; want inherited", create.Kind)
	}
	if create.SourceClass != "CreateModelMixin" {
		t.Errorf("create source_class = %q; want CreateModelMixin", create.SourceClass)
	}
	if create.DefaultStatus != 201 {
		t.Errorf("create default_status = %d; want 201", create.DefaultStatus)
	}
	if len(create.ErrorStatuses) != 1 || create.ErrorStatuses[0] != 400 {
		t.Errorf("create error_statuses = %v; want [400] (#278)", create.ErrorStatuses)
	}
	if create.Serializer != "RoleSerializer" {
		t.Errorf("create serializer = %q; want RoleSerializer", create.Serializer)
	}
	if len(create.Permissions) != 1 || create.Permissions[0] != "IsAuthenticated" {
		t.Errorf("create permissions = %v; want [IsAuthenticated]", create.Permissions)
	}
	if !create.AuthRequired {
		t.Error("create auth_required = false; want true")
	}

	// EXPLICIT list — status from body, paginated.
	list := handlerByVerbPath(t, g, "GET", "/api/v1/roles")
	if list.Kind != "explicit" {
		t.Errorf("list kind = %q; want explicit", list.Kind)
	}
	if list.DefaultStatus != 200 {
		t.Errorf("list default_status = %d; want 200", list.DefaultStatus)
	}
	if !list.Pagination {
		t.Error("list pagination = false; want true")
	}

	// @action approve — no fabricated status.
	approve := handlerByVerbPath(t, g, "POST", "/api/v1/roles/{pk}/approve")
	if approve.Kind != "action" {
		t.Errorf("approve kind = %q; want action", approve.Kind)
	}
	if approve.DefaultStatus != 0 {
		t.Errorf("approve default_status = %d; want 0 (no fabricated status for @action)", approve.DefaultStatus)
	}

	// HONEST-PARTIAL retrieve — resolvable kind present, status omitted.
	retrieve := handlerByVerbPath(t, g, "GET", "/api/v1/roles/{pk}")
	if retrieve.Kind != "explicit" {
		t.Errorf("retrieve kind = %q; want explicit", retrieve.Kind)
	}
	if retrieve.DefaultStatus != 0 {
		t.Errorf("retrieve default_status = %d; want 0 (honest-partial omit)", retrieve.DefaultStatus)
	}
	if len(retrieve.ErrorStatuses) != 0 {
		t.Errorf("retrieve error_statuses = %v; want empty (honest-partial omit)", retrieve.ErrorStatuses)
	}
}

// TestEffectiveContract_TargetByRoute resolves a SINGLE route entity to its
// owning ViewSet group — the same RoleViewSet contract.
func TestEffectiveContract_TargetByRoute(t *testing.T) {
	srv := newTestServer(t, roleViewSetDoc())
	out := callEffectiveContract(t, srv, "http:create")

	if len(out.Groups) != 1 {
		t.Fatalf("expected 1 group resolved from a route, got %d", len(out.Groups))
	}
	if out.Groups[0].Class != "RoleViewSet" {
		t.Errorf("route resolved to class %q; want RoleViewSet", out.Groups[0].Class)
	}
	if len(out.Groups[0].Handlers) != 4 {
		t.Errorf("expected the full 4-verb RoleViewSet contract from a route target, got %d", len(out.Groups[0].Handlers))
	}
}

// TestEffectiveContract_NoRoutes returns an empty group set with the reindex
// note — not an error, not fabricated handlers.
func TestEffectiveContract_NoRoutes(t *testing.T) {
	doc := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "lonely", Name: "LonelyViewSet", QualifiedName: "core.LonelyViewSet",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "core/views.py",
				StartLine: 1, EndLine: 2, Language: "python"},
		},
	}
	srv := newTestServer(t, doc)
	out := callEffectiveContract(t, srv, "LonelyViewSet")

	if len(out.Groups) != 0 {
		t.Fatalf("expected no groups for a ViewSet with no routes, got %d", len(out.Groups))
	}
	if out.Note == "" {
		t.Error("expected an honest reindex note when no router-expanded routes are found")
	}
}
