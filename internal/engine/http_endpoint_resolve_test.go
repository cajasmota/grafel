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

// findImplementsTo returns the index of the entity in out that owns an
// IMPLEMENTS edge pointing at toID, or -1.
func findImplementsTo(out []types.EntityRecord, toID string) int {
	for i := range out {
		for _, r := range out[i].Relationships {
			if r.Kind == implementsEdgeKind && r.ToID == toID {
				return i
			}
		}
	}
	return -1
}

// TestResolveHandlers_NestHealthNotBoundToToolingScript (#3426) is the
// parity-oracle bug: a NestJS `GET /health` definition emitted from
// health.controller.ts with source_handler="Controller:check" must resolve
// to the SAME-FILE `check` method (indexed as SCOPE.Operation), NOT to the
// repo-wide `function check(...)` in scripts/docs-check.mjs. Before the fix
// the exact-kind same-file lookup missed (real method is SCOPE.Operation,
// not Controller) and the global bare-name fallback bound `check` to the
// build script, mis-sourcing the route to scripts/docs-check.mjs:28.
func TestResolveHandlers_NestHealthNotBoundToToolingScript(t *testing.T) {
	const ctrlFile = "src/modules/health/health.controller.ts"
	// Real handler method `check()` in the controller file, indexed as a
	// method kind (SCOPE.Operation), NOT kind Controller.
	realHandler := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "check",
		SourceFile: ctrlFile,
		StartLine:  17,
		EndLine:    20,
		Language:   "typescript",
	}
	// Collision: a same-named function in a build/tooling script.
	toolingHandler := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "check",
		SourceFile: "scripts/docs-check.mjs",
		StartLine:  28,
		EndLine:    35,
		Language:   "javascript",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/health",
		SourceFile: ctrlFile,
		StartLine:  12,
		Language:   "typescript",
		Properties: map[string]string{
			"source_handler": "Controller:check",
			"framework":      "nestjs",
		},
	}
	// Order toolingHandler FIRST so the old globalIdx (first-writer-wins)
	// would have bound to it — proving the fix, not index ordering.
	merged := []types.EntityRecord{toolingHandler, realHandler, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.HandlerResolved != 1 {
		t.Fatalf("expected handler_resolved=1, got %+v", stats)
	}
	// Locate the synthetic in the output.
	var endpoint *types.EntityRecord
	for i := range out {
		if out[i].Kind == httpEndpointDefinitionKind {
			endpoint = &out[i]
		}
	}
	if endpoint == nil {
		t.Fatalf("endpoint definition not found in out")
	}
	if endpoint.SourceFile != ctrlFile {
		t.Errorf("endpoint mis-sourced: got SourceFile=%q, want %q (must NOT be the tooling script)",
			endpoint.SourceFile, ctrlFile)
	}
	if endpoint.StartLine != realHandler.StartLine {
		t.Errorf("endpoint StartLine=%d, want %d (real handler body, not docs-check.mjs:28)",
			endpoint.StartLine, realHandler.StartLine)
	}
	// IMPLEMENTS edge must point at the real same-file handler.
	owner := findImplementsTo(out, httpEndpointDefinitionKind+":http:GET:/health")
	if owner < 0 {
		t.Fatalf("no IMPLEMENTS edge to the endpoint found")
	}
	if out[owner].SourceFile != ctrlFile {
		t.Errorf("IMPLEMENTS edge owned by %q (line %d), want the real handler in %q",
			out[owner].SourceFile, out[owner].StartLine, ctrlFile)
	}
}

// TestResolveHandlers_ToolingOnlyCollisionNotRebound (#3426) verifies that
// when there is NO same-file handler and the ONLY global match for the
// bare name lives in a tooling script, the resolver does NOT rebind the
// endpoint to that script. The synthetic keeps its own (controller) source.
func TestResolveHandlers_ToolingOnlyCollisionNotRebound(t *testing.T) {
	const ctrlFile = "src/a.controller.ts"
	// Only the tooling script has a `check` symbol; the controller file has
	// none.
	toolingHandler := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "check",
		SourceFile: "scripts/docs-check.mjs",
		StartLine:  28,
		Language:   "javascript",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/health",
		SourceFile: ctrlFile,
		StartLine:  9,
		Language:   "typescript",
		Properties: map[string]string{
			"source_handler": "Controller:check",
			"framework":      "nestjs",
		},
	}
	merged := []types.EntityRecord{toolingHandler, synth}
	out, _ := ResolveHTTPEndpointHandlers(merged)

	var endpoint *types.EntityRecord
	for i := range out {
		if out[i].Kind == httpEndpointDefinitionKind {
			endpoint = &out[i]
		}
	}
	if endpoint == nil {
		t.Fatalf("endpoint definition not found (should be kept, not dropped)")
	}
	if endpoint.SourceFile != ctrlFile {
		t.Errorf("endpoint rebound to a tooling script: SourceFile=%q, want %q",
			endpoint.SourceFile, ctrlFile)
	}
	if endpoint.SourceFile == "scripts/docs-check.mjs" || endpoint.StartLine == 28 {
		t.Errorf("endpoint must never resolve to the build script (got %q:%d)",
			endpoint.SourceFile, endpoint.StartLine)
	}
}

// TestResolveHandlers_ExpressCrossFileStillResolves (#3426 regression
// guard) verifies the legitimate cross-file Express case (#753) is NOT
// regressed: a handler `index` in controllers/users.js, route synthetic in
// routes.js, with NO tooling-file collision, still resolves cross-file via
// the global fallback.
func TestResolveHandlers_ExpressCrossFileStillResolves(t *testing.T) {
	handler := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "index",
		SourceFile: "controllers/users.js",
		StartLine:  4,
		Language:   "javascript",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:GET:/users",
		SourceFile: "routes.js",
		StartLine:  11,
		Language:   "javascript",
		Properties: map[string]string{
			"source_handler": "Controller:index",
			"framework":      "express",
		},
	}
	merged := []types.EntityRecord{handler, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.HandlerResolved != 1 {
		t.Fatalf("expected handler_resolved=1 (cross-file Express), got %+v", stats)
	}
	var endpoint *types.EntityRecord
	for i := range out {
		if out[i].Kind == httpEndpointDefinitionKind {
			endpoint = &out[i]
		}
	}
	if endpoint == nil {
		t.Fatalf("endpoint definition not found")
	}
	if endpoint.SourceFile != "controllers/users.js" {
		t.Errorf("Express cross-file resolution regressed: SourceFile=%q, want controllers/users.js",
			endpoint.SourceFile)
	}
	owner := findImplementsTo(out, httpEndpointDefinitionKind+":http:GET:/users")
	if owner < 0 || out[owner].SourceFile != "controllers/users.js" {
		t.Errorf("IMPLEMENTS edge not on the real cross-file handler")
	}
}
