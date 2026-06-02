package mcp

import (
	"reflect"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// effective_contract_test.go — value-asserting tests for projectEffectiveContract
// (#3835, T5). They assert the structured per-verb contract lifted from the
// stamped `effective_*` properties — the inverse of
// engine.stampDRFEffectiveContract — for the inherited / explicit / action
// cases plus the honest-partial negative case.

// routeEntity builds a router-expanded http_endpoint entity carrying the given
// stamped properties (pattern_type is forced so isRouterExpandedRoute matches).
func routeEntity(props map[string]string) *graph.Entity {
	p := map[string]string{"pattern_type": "drf_router_expanded"}
	for k, v := range props {
		p[k] = v
	}
	return &graph.Entity{ID: "http:x", Kind: "http_endpoint", Properties: p}
}

// TestProjectEffectiveContract_InheritedCreate verifies the #278 case: the
// stamped inherited-create properties project into a contract with
// kind=inherited, source_class=CreateModelMixin, default_status=201,
// error_statuses=[400], serializer + permissions resolved.
func TestProjectEffectiveContract_InheritedCreate(t *testing.T) {
	e := routeEntity(map[string]string{
		"verb":                            "POST",
		"path":                            "/api/v1/roles",
		"drf_view_method":                 "RoleViewSet.create",
		"provenance":                      "inherited",
		"effective_kind":                  "inherited",
		"effective_source_class":          "CreateModelMixin",
		"effective_status":                "201",
		"effective_error_statuses":        "400",
		"effective_permission_applicable": "true",
		"serializer_class":                "RoleSerializer",
		"middleware_names":                "IsAuthenticated",
		"auth_required":                   "true",
	})

	c, ok := projectEffectiveContract(e)
	if !ok {
		t.Fatal("expected projection to succeed for a router-expanded route")
	}
	if c.Verb != "POST" || c.Path != "/api/v1/roles" {
		t.Errorf("verb/path = %q %q; want POST /api/v1/roles", c.Verb, c.Path)
	}
	if c.Handler != "RoleViewSet.create" {
		t.Errorf("handler = %q; want RoleViewSet.create", c.Handler)
	}
	if c.Kind != "inherited" {
		t.Errorf("kind = %q; want inherited", c.Kind)
	}
	if c.SourceClass != "CreateModelMixin" {
		t.Errorf("source_class = %q; want CreateModelMixin", c.SourceClass)
	}
	if c.DefaultStatus != 201 {
		t.Errorf("default_status = %d; want 201", c.DefaultStatus)
	}
	if !reflect.DeepEqual(c.ErrorStatuses, []int{400}) {
		t.Errorf("error_statuses = %v; want [400] (#278)", c.ErrorStatuses)
	}
	if c.Serializer != "RoleSerializer" {
		t.Errorf("serializer = %q; want RoleSerializer", c.Serializer)
	}
	if !reflect.DeepEqual(c.Permissions, []string{"IsAuthenticated"}) {
		t.Errorf("permissions = %v; want [IsAuthenticated]", c.Permissions)
	}
	if !c.AuthRequired {
		t.Error("auth_required = false; want true")
	}
}

// TestProjectEffectiveContract_ExplicitList verifies an explicit overridden
// list: kind=explicit, source_class=the ViewSet, status from the pack default
// (200), no error statuses.
func TestProjectEffectiveContract_ExplicitList(t *testing.T) {
	e := routeEntity(map[string]string{
		"verb":                   "GET",
		"path":                   "/widgets",
		"effective_kind":         "explicit",
		"effective_source_class": "WidgetViewSet",
		"effective_status":       "200",
		"effective_pagination":   "true",
		"serializer_class":       "WidgetSerializer",
	})

	c, _ := projectEffectiveContract(e)
	if c.Kind != "explicit" {
		t.Errorf("kind = %q; want explicit", c.Kind)
	}
	if c.SourceClass != "WidgetViewSet" {
		t.Errorf("source_class = %q; want WidgetViewSet", c.SourceClass)
	}
	if c.DefaultStatus != 200 {
		t.Errorf("default_status = %d; want 200", c.DefaultStatus)
	}
	if len(c.ErrorStatuses) != 0 {
		t.Errorf("error_statuses = %v; want empty", c.ErrorStatuses)
	}
	if !c.Pagination {
		t.Error("pagination = false; want true")
	}
}

// TestProjectEffectiveContract_Action verifies an @action route: kind=action,
// source_class=the ViewSet, NO default status (honest-partial — status lives in
// the decorated body).
func TestProjectEffectiveContract_Action(t *testing.T) {
	e := routeEntity(map[string]string{
		"verb":                   "POST",
		"path":                   "/orders/{pk}/approve",
		"drf_view_method":        "OrderViewSet.approve",
		"effective_kind":         "action",
		"effective_source_class": "OrderViewSet",
		"serializer_class":       "OrderSerializer",
	})

	c, _ := projectEffectiveContract(e)
	if c.Kind != "action" {
		t.Errorf("kind = %q; want action", c.Kind)
	}
	if c.SourceClass != "OrderViewSet" {
		t.Errorf("source_class = %q; want OrderViewSet", c.SourceClass)
	}
	if c.DefaultStatus != 0 {
		t.Errorf("default_status = %d; want 0 (no fabricated status for @action)", c.DefaultStatus)
	}
	if c.Serializer != "OrderSerializer" {
		t.Errorf("serializer = %q; want OrderSerializer", c.Serializer)
	}
}

// TestProjectEffectiveContract_HonestPartial verifies the negative case: a
// route with no pack-derived fields (unknown base) projects with the kind/
// source present but status/error_statuses ZERO — never fabricated.
func TestProjectEffectiveContract_HonestPartial(t *testing.T) {
	e := routeEntity(map[string]string{
		"verb":                   "GET",
		"path":                   "/things",
		"effective_kind":         "explicit",
		"effective_source_class": "CustomViewSet",
		// no effective_status / effective_error_statuses (unknown base).
	})

	c, _ := projectEffectiveContract(e)
	if c.DefaultStatus != 0 {
		t.Errorf("default_status = %d; want 0 (honest-partial omit)", c.DefaultStatus)
	}
	if len(c.ErrorStatuses) != 0 {
		t.Errorf("error_statuses = %v; want empty (honest-partial omit)", c.ErrorStatuses)
	}
	if c.Kind != "explicit" {
		t.Errorf("kind = %q; want explicit (resolvable field still present)", c.Kind)
	}
}

// TestProjectEffectiveContract_NonRouteRejected verifies a non-router-expanded
// entity is rejected (the projection is route-specific).
func TestProjectEffectiveContract_NonRouteRejected(t *testing.T) {
	e := &graph.Entity{ID: "x", Kind: "http_endpoint", Properties: map[string]string{"verb": "GET"}}
	if _, ok := projectEffectiveContract(e); ok {
		t.Error("expected projection to reject a non-router-expanded entity")
	}
}
