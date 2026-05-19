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
	if rel.FromID != "Controller:get_thing" || rel.ToID != "http_endpoint:http:GET:/things/{id}" {
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
