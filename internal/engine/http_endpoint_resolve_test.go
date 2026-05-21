package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestResolveHandlers_EmitsImplementsEdgeAndClearsProperty verifies the
// happy path: a synthetic http_endpoint whose `source_handler` resolves
// to a real same-file entity produces an IMPLEMENTS edge on the handler
// and removes the property from the synthetic.
func TestResolveHandlers_EmitsImplementsEdgeAndClearsProperty(t *testing.T) {
	handler := types.EntityRecord{
		Kind:       "Controller",
		Name:       "get_thing",
		SourceFile: "app.py",
		Language:   "python",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/things/{id}",
		SourceFile: "app.py",
		Language:   "python",
		Properties: map[string]string{
			"source_handler": "Controller:get_thing",
			"framework":      "flask",
		},
	}
	merged := []types.EntityRecord{handler, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.Synthetics != 1 || stats.HandlerResolved != 1 || stats.HandlerDropped != 0 {
		t.Errorf("stats unexpected: %+v", stats)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entities (no drop), got %d", len(out))
	}
	// Handler gets the IMPLEMENTS edge.
	if len(out[0].Relationships) != 1 {
		t.Fatalf("expected 1 relationship on handler, got %d", len(out[0].Relationships))
	}
	rel := out[0].Relationships[0]
	if rel.Kind != implementsEdgeKind {
		t.Errorf("expected kind=%s, got %s", implementsEdgeKind, rel.Kind)
	}
	// #1217: the legacy http_endpoint kind is migrated to http_endpoint_definition
	// by ResolveHTTPEndpointHandlers, so the edge ToID uses the new kind.
	if rel.FromID != "Controller:get_thing" || rel.ToID != "http_endpoint_definition:http:GET:/things/{id}" {
		t.Errorf("edge ids wrong: from=%s to=%s", rel.FromID, rel.ToID)
	}
	// Synthetic's source_handler property cleared.
	if _, ok := out[1].Properties["source_handler"]; ok {
		t.Errorf("source_handler property should be cleared, got %+v", out[1].Properties)
	}
}

// TestResolveHandlers_DropsUnresolvedSynthetic verifies that a synthetic
// pointing at a non-existent handler is removed from the merged set —
// keeping it would leave an orphan http_endpoint node that inflates
// resolver bug-rate.
func TestResolveHandlers_DropsUnresolvedSynthetic(t *testing.T) {
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/missing",
		SourceFile: "app.py",
		Language:   "python",
		Properties: map[string]string{
			"source_handler": "Controller:does_not_exist",
		},
	}
	merged := []types.EntityRecord{synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.HandlerDropped != 1 {
		t.Errorf("expected 1 drop, got %d", stats.HandlerDropped)
	}
	if len(out) != 0 {
		t.Errorf("expected unresolved synthetic dropped, out=%+v", out)
	}
}

// TestResolveHandlers_KeepsSyntheticWithNoHandlerProp verifies that
// synthetics without `source_handler` (e.g. Express inline handlers) are
// retained as-is — they're valid unbound endpoints, not orphans.
func TestResolveHandlers_KeepsSyntheticWithNoHandlerProp(t *testing.T) {
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/inline",
		SourceFile: "server.js",
		Language:   "javascript",
		Properties: map[string]string{
			"framework": "express",
		},
	}
	merged := []types.EntityRecord{synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.NoHandlerProp != 1 {
		t.Errorf("expected 1 no_handler_prop, got %d", stats.NoHandlerProp)
	}
	if len(out) != 1 {
		t.Errorf("expected synthetic preserved, got %d", len(out))
	}
}

// TestResolveHandlers_FBVCrossFileKindFallback verifies that a Django FBV
// endpoint (pattern_type=urlconf_nested_include) whose source_handler uses
// the "Controller:<name>" kind can be resolved cross-file via the
// resolverKindEquivalents fallback — i.e. the entity lives in views.py as
// SCOPE.Operation but the handler ref says Controller (issue #527).
func TestResolveHandlers_FBVCrossFileKindFallback(t *testing.T) {
	// SCOPE.Operation entity for health_check in views.py
	handler := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "health_check",
		SourceFile: "users/views.py",
		Language:   "python",
	}
	// http_endpoint entity in urls.py (different SourceFile from handler)
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:ANY:/users/health",
		SourceFile: "myproject/urls.py",
		Language:   "python",
		Properties: map[string]string{
			"source_handler": "Controller:health_check",
			"framework":      "django",
			"pattern_type":   "urlconf_nested_include",
		},
	}
	merged := []types.EntityRecord{handler, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.Synthetics != 1 {
		t.Errorf("expected 1 synthetic, got %d", stats.Synthetics)
	}
	if stats.HandlerResolved != 1 {
		t.Errorf("expected handler_resolved=1, got %d (handler_dropped=%d no_handler_prop=%d)",
			stats.HandlerResolved, stats.HandlerDropped, stats.NoHandlerProp)
	}
	if stats.HandlerDropped != 0 {
		t.Errorf("expected handler_dropped=0, got %d", stats.HandlerDropped)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entities (no drop), got %d", len(out))
	}
	// Handler (health_check) gets the IMPLEMENTS edge appended.
	if len(out[0].Relationships) != 1 {
		t.Fatalf("expected 1 relationship on handler (health_check), got %d: %+v",
			len(out[0].Relationships), out[0].Relationships)
	}
	rel := out[0].Relationships[0]
	if rel.Kind != implementsEdgeKind {
		t.Errorf("expected kind=%s, got %s", implementsEdgeKind, rel.Kind)
	}
	if rel.FromID != "SCOPE.Operation:health_check" {
		t.Errorf("edge FromID wrong: got %q, want SCOPE.Operation:health_check", rel.FromID)
	}
	// #1217: legacy kind migrated to http_endpoint_definition.
	if rel.ToID != "http_endpoint_definition:http:ANY:/users/health" {
		t.Errorf("edge ToID wrong: got %q, want http_endpoint_definition:http:ANY:/users/health", rel.ToID)
	}
	// source_handler cleared from synthetic.
	if _, ok := out[1].Properties["source_handler"]; ok {
		t.Errorf("source_handler should be cleared on synthetic, got %+v", out[1].Properties)
	}
}

// TestResolveCallers_EmitsFetchesEdge (#754) verifies that a consumer
// synthetic with a resolvable source_caller produces a FETCHES edge on
// the caller's embedded relationships and clears the property.
func TestResolveCallers_EmitsFetchesEdge(t *testing.T) {
	caller := types.EntityRecord{
		Kind:       "Function",
		Name:       "fetchUsers",
		SourceFile: "client.ts",
		Language:   "typescript",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/api/users",
		SourceFile: "client.ts",
		Language:   "typescript",
		Properties: map[string]string{
			"framework":     "fetch",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:fetchUsers",
		},
	}
	merged := []types.EntityRecord{caller, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.CallerResolved != 1 {
		t.Errorf("expected 1 caller_resolved, got %d", stats.CallerResolved)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entities preserved, got %d", len(out))
	}
	// caller should now own a FETCHES relationship to the synthetic.
	var found bool
	for _, r := range out[0].Relationships {
		if r.Kind == "FETCHES" {
			found = true
			if r.FromID != "Function:fetchUsers" {
				t.Errorf("FETCHES from = %q, want Function:fetchUsers", r.FromID)
			}
			// #1217: legacy kind migrated to http_endpoint_call for consumer synthetics.
			if r.ToID != "http_endpoint_call:http:GET:/api/users" {
				t.Errorf("FETCHES to = %q, want http_endpoint_call:http:GET:/api/users", r.ToID)
			}
		}
	}
	if !found {
		t.Errorf("expected FETCHES edge on caller, got %+v", out[0].Relationships)
	}
	// source_caller should be cleared.
	if _, has := out[1].Properties["source_caller"]; has {
		t.Errorf("source_caller should be cleared after resolution; got %v", out[1].Properties)
	}
}

// TestResolveCallers_FallbackToFileContainer (#754) verifies that when
// the precise caller-name lookup fails, the resolver falls back to
// any same-file container entity (class/module/file-component) and
// still emits a FETCHES edge — necessary because real-world JS/TS
// class-field arrow methods aren't surfaced as discrete entities.
func TestResolveCallers_FallbackToFileContainer(t *testing.T) {
	fileComponent := types.EntityRecord{
		Kind:       "SCOPE.Component",
		Name:       "src/svc.js",
		SourceFile: "src/svc.js",
	}
	classComponent := types.EntityRecord{
		Kind:       "SCOPE.Component",
		Name:       "BranchesService",
		SourceFile: "src/svc.js",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/api/x",
		SourceFile: "src/svc.js",
		Properties: map[string]string{
			"framework":     "axios",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:byId", // not a real entity name
		},
	}
	merged := []types.EntityRecord{fileComponent, classComponent, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.CallerResolved != 1 {
		t.Errorf("expected 1 caller_resolved via fallback, got %d", stats.CallerResolved)
	}
	// Both same-file Components should now own a FETCHES edge to the synthetic.
	gotEdges := 0
	for i := range out {
		for _, r := range out[i].Relationships {
			if r.Kind == "FETCHES" {
				gotEdges++
			}
		}
	}
	if gotEdges < 2 {
		t.Errorf("expected at least 2 FETCHES edges from same-file containers, got %d", gotEdges)
	}
}

// TestResolveCallers_NoMatchKeepsSynthetic (#754) verifies that when
// nothing in the same file matches as a fallback, the synthetic stays
// alive (cross-repo bridges are valuable even when intra-repo
// reachability is missing) and no edge is emitted.
func TestResolveCallers_NoMatchKeepsSynthetic(t *testing.T) {
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/orphan",
		SourceFile: "lonely.js",
		Properties: map[string]string{
			"framework":     "fetch",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:nobody",
		},
	}
	merged := []types.EntityRecord{synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.CallerResolved != 0 {
		t.Errorf("expected 0 caller_resolved, got %d", stats.CallerResolved)
	}
	if stats.CallerUnresolved != 1 {
		t.Errorf("expected 1 caller_unresolved, got %d", stats.CallerUnresolved)
	}
	if len(out) != 1 {
		t.Errorf("expected synthetic preserved despite unresolved caller, got %d", len(out))
	}
}
