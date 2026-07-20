package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"
)

// TestBuildEffectsPayload_SidecarWins is the #2804 regression at the MCP
// boundary: effect data is persisted only to the on-disk sidecar (entity
// .Properties are empty in the daemon-loaded graph). The payload builder
// must read the sidecar, not report `pure`.
func TestBuildEffectsPayload_SidecarWins(t *testing.T) {
	e := &graph.Entity{ID: "9986b358cfb5dacd", Name: "ProposalViewSet.send_proposals", Kind: "SCOPE.Operation"}
	sidecar := map[string]effectsSidecarEntry{
		"acme-core::9986b358cfb5dacd": {
			EntityID:    "acme-core::9986b358cfb5dacd",
			Effects:     []string{"db_read", "db_write", "fs_read", "fs_write"},
			Confidences: map[string]float64{"db_read": 0.85, "db_write": 0.85, "fs_read": 0.9, "fs_write": 1.0},
			Sinks:       map[string][]string{"db_write": {"orm.write"}},
			Source:      "direct",
		},
	}

	out := buildEffectsPayload("acme-core", e, sidecar)

	if got := out["effect_source"]; got != "direct" {
		t.Fatalf("effect_source=%v; want direct (sidecar must override empty properties)", got)
	}
	effs, ok := out["effects"].([]string)
	if !ok || len(effs) != 4 {
		t.Fatalf("effects=%v; want the 4 sidecar effects", out["effects"])
	}
	if got := out["confidence"]; got != 1.0 {
		t.Errorf("headline confidence=%v; want 1.0 (max per-effect), not the 0.3 pure default", got)
	}
}

// TestBuildEffectsPayload_PureWhenAbsent verifies the fallback contract:
// an entity in neither the sidecar nor entity.Properties is reported pure
// with the documented low confidence.
func TestBuildEffectsPayload_PureWhenAbsent(t *testing.T) {
	e := &graph.Entity{ID: "deadbeef", Name: "add", Kind: "SCOPE.Function"}
	out := buildEffectsPayload("acme-core", e, map[string]effectsSidecarEntry{})
	if got := out["effect_source"]; got != "pure" {
		t.Fatalf("effect_source=%v; want pure", got)
	}
	if got := out["confidence"]; got != 0.3 {
		t.Errorf("confidence=%v; want 0.3", got)
	}
}

// TestBuildEffectsPayload_PropertiesFallback verifies the in-process path
// (live link run) still works when no sidecar entry exists but properties
// are stamped.
func TestBuildEffectsPayload_PropertiesFallback(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{
			ID:   "abc",
			Name: "svc",
			Kind: "SCOPE.Function",
		}.WithProperties(map[string]string{
			links.EffectPropertyKeyList:       "http_out",
			links.EffectPropertyKeyConfidence: "http_out=0.95",
			links.EffectPropertyKeySource:     "transitive",
		},
		))
	out := buildEffectsPayload("acme-core", e, map[string]effectsSidecarEntry{})
	if got := out["effect_source"]; got != "transitive" {
		t.Fatalf("effect_source=%v; want transitive", got)
	}
	if got := out["confidence"]; got != 0.95 {
		t.Errorf("confidence=%v; want 0.95", got)
	}
}
