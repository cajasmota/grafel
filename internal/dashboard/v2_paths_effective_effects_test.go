package dashboard

// v2_paths_effective_effects_test.go — #4489 live-validation for the endpoint
// EFFECTIVE side-effect aggregation.
//
// The fixture mirrors the acme thin-controller shape that triggered the bug:
//
//	POST /api/v1/widgets  →  WidgetController.create   (handler, NO direct sink)
//	                              └─CALLS→ WidgetService.create  (db_write)
//
// The handler performs NO direct DB write, so the legacy direct-only "Side
// effects" panel reads (0). The new effective-effects aggregation walks the
// handler's downstream CALLS, reads db_write off the canonical links-effects
// sidecar on WidgetService.create, and surfaces it on the endpoint tagged
// source=downstream. This is the before/after the ticket asks for.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// thinControllerFixture: a POST create endpoint whose handler delegates the DB
// write to a downstream service method.
func thinControllerFixture() ([]graph.Entity, []graph.Relationship) {
	entities := []graph.Entity{
		// Synthetic endpoint definition (router-expanded).
		graph.Entity{
			ID:         "ep_post_widgets",
			Name:       "http:POST:/api/v1/widgets",
			Kind:       "http_endpoint_definition",
			SourceFile: "core/routers.py",
			StartLine:  0,
		}.WithProperties(map[string]string{"path": "/api/v1/widgets", "verb": "POST", "framework": "drf"}),
		// Thin controller handler — NO direct side-effect edge.
		{
			ID:         "op_ctrl_create",
			Name:       "WidgetController.create",
			Kind:       "SCOPE.Operation",
			SourceFile: "core/views/widget_controller.py",
			StartLine:  20,
		},
		// Downstream service method that performs the actual DB write.
		{
			ID:         "op_svc_create",
			Name:       "WidgetService.create",
			Kind:       "SCOPE.Operation",
			SourceFile: "core/services/widget_service.py",
			StartLine:  8,
		},
	}
	rels := []graph.Relationship{
		// Handler IMPLEMENTS the definition.
		{FromID: "op_ctrl_create", ToID: "ep_post_widgets", Kind: "IMPLEMENTS"},
		// Handler delegates to the service — the only outbound edge.
		{FromID: "op_ctrl_create", ToID: "op_svc_create", Kind: "CALLS"},
	}
	return entities, rels
}

// writeEffectsSidecar writes a minimal <group>-links-effects.json under $HOME so
// loadDAGEffectsSidecar resolves it. Sets HOME for the test process.
func writeEffectsSidecar(t *testing.T, group string, entries map[string][]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".grafel", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	type entry struct {
		EntityID string   `json:"entity_id"`
		Effects  []string `json:"effects"`
	}
	doc := struct {
		Version int     `json:"version"`
		Method  string  `json:"method"`
		Entries []entry `json:"entries"`
	}{Version: 1, Method: "effect_propagation"}
	for id, effs := range entries {
		doc.Entries = append(doc.Entries, entry{EntityID: id, Effects: effs})
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, group+"-links-effects.json"), buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// TestV2PathDetail_EffectiveEffects_AggregatesDownstreamWrite is the #4489
// before/after: the direct Side-effects panel is empty (the bug), but the new
// effective_effects surfaces db_write tagged source=downstream.
func TestV2PathDetail_EffectiveEffects_AggregatesDownstreamWrite(t *testing.T) {
	entities, rels := thinControllerFixture()

	// db_write lives on the DOWNSTREAM service method, not the handler.
	writeEffectsSidecar(t, "testgrp", map[string][]string{
		"api-backend::op_svc_create": {"db_write"},
	})

	grp := makePathsTestGroup(entities, rels)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	hash := hashStr("/api/v1/widgets")
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200 got %d", resp.StatusCode)
	}
	var body struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := body.Data

	// BEFORE (the bug): the direct-only Side effects panel is empty — the handler
	// has no direct sink edge.
	if len(d.SideEffects) != 0 {
		t.Errorf("direct side_effects: want empty (thin controller), got %+v", d.SideEffects)
	}

	// AFTER (the fix): effective_effects surfaces db_write from the downstream
	// service, tagged source=downstream.
	var gotWrite *v2EffectiveEffect
	for i := range d.EffectiveEffects {
		if d.EffectiveEffects[i].Kind == "db_write" {
			gotWrite = &d.EffectiveEffects[i]
		}
	}
	if gotWrite == nil {
		t.Fatalf("effective_effects: want db_write, got %+v", d.EffectiveEffects)
	}
	if gotWrite.Source != "downstream" {
		t.Errorf("effective_effects db_write source: want downstream, got %q", gotWrite.Source)
	}
}

// TestV2PathDetail_EffectiveEffects_DirectSinkTaggedDirect verifies a sink ON the
// handler itself is tagged source=direct (the non-thin case), so the hint is
// honest about provenance.
func TestV2PathDetail_EffectiveEffects_DirectSinkTaggedDirect(t *testing.T) {
	entities, rels := thinControllerFixture()

	// Put db_write directly on the HANDLER (no delegation needed for the write).
	writeEffectsSidecar(t, "testgrp", map[string][]string{
		"api-backend::op_ctrl_create": {"db_write"},
	})

	grp := makePathsTestGroup(entities, rels)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	hash := hashStr("/api/v1/widgets")
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var gotWrite *v2EffectiveEffect
	for i := range body.Data.EffectiveEffects {
		if body.Data.EffectiveEffects[i].Kind == "db_write" {
			gotWrite = &body.Data.EffectiveEffects[i]
		}
	}
	if gotWrite == nil {
		t.Fatalf("effective_effects: want db_write, got %+v", body.Data.EffectiveEffects)
	}
	if gotWrite.Source != "direct" {
		t.Errorf("effective_effects db_write source: want direct, got %q", gotWrite.Source)
	}
}

// TestV2PathDetail_EffectiveEffects_PureEndpointEmpty verifies an endpoint whose
// reachable functions have no sinks reports no effective_effects (omitempty), so
// the panel is honest rather than fabricating effects.
func TestV2PathDetail_EffectiveEffects_PureEndpointEmpty(t *testing.T) {
	entities, rels := thinControllerFixture()
	// No sidecar entries for either function → pure.
	writeEffectsSidecar(t, "testgrp", map[string][]string{})

	grp := makePathsTestGroup(entities, rels)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	hash := hashStr("/api/v1/widgets")
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.EffectiveEffects) != 0 {
		t.Errorf("effective_effects: want empty for pure endpoint, got %+v", body.Data.EffectiveEffects)
	}
}
