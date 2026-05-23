// candidate_confidence_test.go — unit coverage for the per-surface
// confidence floor introduced in #1129.

package dashboard

import (
	"testing"
)

// TestComputeCandidateConfidence_NoisyContainerFlow asserts the strong
// negative signal that drops generic container entry-points (e.g. <module>,
// __main__) well below every surface floor. This is the primary noise case
// the bar was designed to suppress on the Flows surface.
func TestComputeCandidateConfidence_NoisyContainerFlow(t *testing.T) {
	entry := map[string]any{
		"label":       "<module>",
		"entry_name":  "<module>",
		"step_count":  1,
		"source_file": "",
	}
	score, signals := ComputeCandidateConfidence(SurfaceFlows, entry, nil)
	if score >= FloorFor(SurfaceFlows) {
		t.Fatalf("noisy container should score below flows floor, got %.2f signals=%v", score, signals)
	}
}

// TestComputeCandidateConfidence_HighQualityFlow asserts a flow with a real
// source file, cross-stack span, and multi-step chain crosses the floor.
func TestComputeCandidateConfidence_HighQualityFlow(t *testing.T) {
	entry := map[string]any{
		"label":       "ProcessOrderFlow",
		"entry_name":  "process_order",
		"step_count":  6,
		"cross_stack": true,
		"source_file": "checkout/order.py",
	}
	score, signals := ComputeCandidateConfidence(SurfaceFlows, entry, nil)
	if score < FloorFor(SurfaceFlows) {
		t.Fatalf("high-quality flow should clear floor, got %.2f signals=%v", score, signals)
	}
}

// TestComputeCandidateConfidence_TopologyOrphan verifies a topology entry
// with neither producers nor consumers (a synthetic stub) gets penalised
// hard enough to drop below the topology floor (0.45).
func TestComputeCandidateConfidence_TopologyOrphan(t *testing.T) {
	entry := map[string]any{
		"label":     "stub-topic",
		"producers": []any{},
		"consumers": []any{},
	}
	score, _ := ComputeCandidateConfidence(SurfaceTopology, entry, nil)
	if score >= FloorFor(SurfaceTopology) {
		t.Fatalf("topology orphan should score below floor, got %.2f", score)
	}
}

// TestComputeCandidateConfidence_PathsNoHandler verifies a route with no
// resolved handler (handlers_count == 0) is below the paths floor — the
// "synthetic client-side-only fetch" case the floor was designed to catch.
func TestComputeCandidateConfidence_PathsNoHandler(t *testing.T) {
	entry := map[string]any{
		"path":           "/x",
		"handlers_count": 0,
		"controller":     "",
	}
	score, _ := ComputeCandidateConfidence(SurfacePaths, entry, nil)
	if score >= FloorFor(SurfacePaths) {
		t.Fatalf("path with no handler should score below floor, got %.2f", score)
	}
}

// TestComputeCandidateConfidence_BuiltinMethodPenalty verifies that a
// qualified name containing a builtin/stdlib fragment is penalised, even if
// other signals are positive.
func TestComputeCandidateConfidence_BuiltinMethodPenalty(t *testing.T) {
	good := map[string]any{
		"label":          "DescribeFoo",
		"qualified_name": "service.foo.DescribeFoo",
		"source_file":    "service/foo.py",
	}
	bad := map[string]any{
		"label":          "DescribeFoo",
		"qualified_name": "service.foo.DescribeFoo.__str__",
		"source_file":    "service/foo.py",
	}
	good_score, _ := ComputeCandidateConfidence(SurfaceFlows, good, nil)
	bad_score, _ := ComputeCandidateConfidence(SurfaceFlows, bad, nil)
	if !(good_score > bad_score) {
		t.Fatalf("builtin fragment should reduce score: good=%.2f bad=%.2f", good_score, bad_score)
	}
}

// TestFilterByConfidence_Partitions verifies basic partitioning + counts.
func TestFilterByConfidence_Partitions(t *testing.T) {
	entries := []map[string]any{
		{
			"label":       "RealFlow",
			"step_count":  5,
			"cross_stack": true,
			"source_file": "svc/flow.py",
		},
		{
			"label":       "<module>",
			"step_count":  1,
			"source_file": "",
		},
	}
	fr := FilterByConfidence(SurfaceFlows, entries, nil)
	if len(fr.Kept) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(fr.Kept))
	}
	if len(fr.LowConfidence) != 1 {
		t.Fatalf("expected 1 low_confidence, got %d", len(fr.LowConfidence))
	}
	if fr.NoiseRejectedCount != 1 {
		t.Fatalf("noise count mismatch: %d", fr.NoiseRejectedCount)
	}
	if fr.LowConfidence[0]["low_confidence"] != true {
		t.Fatalf("low_confidence flag not set on rejected entry")
	}
	// Confidence value should be set on both partitions.
	if _, ok := fr.Kept[0]["confidence"]; !ok {
		t.Fatalf("confidence not set on kept entry")
	}
}

// TestFilterByConfidence_RankLifts verifies that an explicit ops rank wins
// over a sub-floor confidence score (per #1129 do-step 5).
func TestFilterByConfidence_RankLifts(t *testing.T) {
	entries := []map[string]any{
		{
			"label":      "<module>", // noisy → would score below floor
			"step_count": 1,
			"rank":       float64(0.9),
		},
	}
	fr := FilterByConfidence(SurfaceFlows, entries, nil)
	if len(fr.Kept) != 1 {
		t.Fatalf("rank lift failed: expected 1 kept, got %d (low=%d)",
			len(fr.Kept), len(fr.LowConfidence))
	}
}

// TestFilterByConfidence_ToggleOff verifies the master env toggle disables
// floor filtering — everything ends up in Kept regardless of score.
func TestFilterByConfidence_ToggleOff(t *testing.T) {
	t.Setenv("ARCHIGRAPH_CONFIDENCE_FLOOR", "off")
	entries := []map[string]any{
		{"label": "<module>", "step_count": 1},
		{"label": "ProperFlow", "step_count": 4, "source_file": "x/y.py"},
	}
	fr := FilterByConfidence(SurfaceFlows, entries, nil)
	if len(fr.Kept) != 2 {
		t.Fatalf("toggle off: expected all 2 kept, got %d", len(fr.Kept))
	}
	if len(fr.LowConfidence) != 0 {
		t.Fatalf("toggle off: expected 0 low_confidence, got %d", len(fr.LowConfidence))
	}
	if fr.FloorApplied != 0 {
		t.Fatalf("toggle off: floor should be 0, got %.2f", fr.FloorApplied)
	}
}

// TestFloorFor_EnvOverride verifies that a per-surface env override beats the
// built-in default.
func TestFloorFor_EnvOverride(t *testing.T) {
	t.Setenv("ARCHIGRAPH_CONFIDENCE_FLOOR_FLOWS", "0.8")
	if got := FloorFor(SurfaceFlows); got != 0.8 {
		t.Fatalf("FloorFor(flows) override: got %.2f want 0.80", got)
	}
}

// TestFloorFor_InvalidEnvFallsBack verifies an unparseable env var falls back
// to the built-in default rather than crashing or scoring everything as 0.
func TestFloorFor_InvalidEnvFallsBack(t *testing.T) {
	t.Setenv("ARCHIGRAPH_CONFIDENCE_FLOOR_TOPOLOGY", "not-a-float")
	if got := FloorFor(SurfaceTopology); got != defaultFloors[SurfaceTopology] {
		t.Fatalf("invalid env should fall back to default; got %.2f", got)
	}
}

// TestComputeCandidateConfidence_FlowReactNoise verifies the trivial-terminal
// discriminator catches the dominant noise class on a React-heavy frontend:
// flows whose terminal is a React hook (useEffect / useCallback / useQuery)
// or a setState setter (setLoading, setIsOpen).
func TestComputeCandidateConfidence_FlowReactNoise(t *testing.T) {
	cases := []struct {
		label string
		desc  string
	}{
		{"AnimatedLoader → useCallback", "react_hook_terminal"},
		{"BuildingDetails → useQuery", "react_query_terminal"},
		{"NotesViewer → setIsLoading", "react_setter_terminal"},
		{"Component → useMemo", "react_memo_terminal"},
		{"Container → map", "lodash_map_terminal"},
	}
	floor := FloorFor(SurfaceFlows)
	for _, c := range cases {
		entry := map[string]any{
			"label":       c.label,
			"step_count":  4,
			"source_file": "src/components/Foo.tsx",
		}
		score, signals := ComputeCandidateConfidence(SurfaceFlows, entry, nil)
		if score >= floor {
			t.Errorf("%s: expected score < %.2f, got %.2f signals=%v",
				c.desc, floor, score, signals)
		}
	}
}

// TestExtractFlowTerminal exercises the label parser.
func TestExtractFlowTerminal(t *testing.T) {
	if extractFlowTerminal("A → B") != "B" {
		t.Fatalf("basic terminal extraction failed")
	}
	if extractFlowTerminal("A → B → C") != "C" {
		t.Fatalf("last-segment terminal extraction failed")
	}
	if extractFlowTerminal("no arrow here") != "" {
		t.Fatalf("absent-arrow should return empty")
	}
}

// TestIsSetterName covers the React state-setter heuristic.
func TestIsSetterName(t *testing.T) {
	if !isSetterName("setLoading") {
		t.Fatalf("setLoading should be a setter")
	}
	if !isSetterName("setIsOpen") {
		t.Fatalf("setIsOpen should be a setter")
	}
	if isSetterName("settle") {
		t.Fatalf("settle should NOT be a setter (lowercase after set)")
	}
	if isSetterName("set") {
		t.Fatalf("plain set should NOT match (too short)")
	}
}

// TestComputeCandidateConfidence_HintsBoostScore verifies that providing
// optional graph-level hints can lift a borderline entry across the floor.
func TestComputeCandidateConfidence_HintsBoostScore(t *testing.T) {
	entry := map[string]any{
		"label":      "edge",
		"step_count": 2,
	}
	noHint, _ := ComputeCandidateConfidence(SurfaceFlows, entry, nil)

	entry2 := map[string]any{
		"label":      "edge",
		"step_count": 2,
	}
	hints := &ConfidenceHints{
		OutboundEdges:    8,
		HasCrossRepoEdge: true,
		ResolvedRefs:     2,
	}
	withHints, _ := ComputeCandidateConfidence(SurfaceFlows, entry2, hints)

	if !(withHints > noHint) {
		t.Fatalf("hints should boost score: no_hint=%.2f with_hints=%.2f", noHint, withHints)
	}
}
