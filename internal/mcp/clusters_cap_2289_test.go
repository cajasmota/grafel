package mcp

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// Test_handleListCommunities_DefaultCaps_2289 verifies the new default caps:
//   - top_entities_limit=3 truncates the top_entities slice
//   - min_size=20 filters out small communities
//
// Issue #2289: clusters response was returning full top_entities arrays for
// every community by default, blowing the session token budget.
func Test_handleListCommunities_DefaultCaps_2289(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			// Big community (>= 20) with 5 top entities — should be kept,
			// truncated to first 3.
			{ID: 1, Size: 50, Modularity: 0.5, TopEntities: []string{"a", "b", "c", "d", "e"}},
			// Small community (< 20) — filtered out entirely.
			{ID: 2, Size: 5, Modularity: 0.3, TopEntities: []string{"x", "y"}},
			// Big community whose top_entities slice is already shorter than
			// the cap — should pass through unchanged.
			{ID: 3, Size: 25, Modularity: 0.4, TopEntities: []string{"m", "n"}},
			// Right at the threshold — kept.
			{ID: 4, Size: 20, Modularity: 0.6, TopEntities: []string{"p", "q", "r", "s"}},
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleListCommunities, map[string]any{
		"group": "test",
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 communities (min_size=20 default), got %d: %+v", len(got), got)
	}
	byID := map[float64]map[string]any{}
	for _, c := range got {
		byID[c["id"].(float64)] = c
	}
	if _, ok := byID[2]; ok {
		t.Errorf("community id=2 (size=5) should have been filtered by min_size=20")
	}
	if top := byID[1]["top_entities"].([]any); len(top) != 3 {
		t.Errorf("community 1 top_entities not capped at 3: %v", top)
	} else if top[0] != "a" || top[2] != "c" {
		t.Errorf("community 1 top_entities slice changed unexpectedly: %v", top)
	}
	if top := byID[3]["top_entities"].([]any); len(top) != 2 {
		t.Errorf("community 3 had only 2 top_entities; cap must not pad: %v", top)
	}
	if top := byID[4]["top_entities"].([]any); len(top) != 3 {
		t.Errorf("community 4 (size==20, the threshold) should be kept and capped: %v", top)
	}
}

// Test_handleListCommunities_OverrideCaps_2289 verifies callers can opt back
// into the full pre-#2289 behaviour by passing top_entities_limit=-1 and
// min_size=0.
func Test_handleListCommunities_OverrideCaps_2289(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			{ID: 1, Size: 50, Modularity: 0.5, TopEntities: []string{"a", "b", "c", "d", "e"}},
			{ID: 2, Size: 5, Modularity: 0.3, TopEntities: []string{"x", "y"}},
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleListCommunities, map[string]any{
		"group":              "test",
		"top_entities_limit": -1,
		"min_size":           0,
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	if len(got) != 2 {
		t.Fatalf("override min_size=0 should keep both communities, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c["id"].(float64) == 1 {
			if top := c["top_entities"].([]any); len(top) != 5 {
				t.Errorf("override top_entities_limit=-1 should return all 5 entities, got %v", top)
			}
		}
	}
}

// Test_handleListCommunities_ExplicitLimitHigherThanLen_2289 confirms a custom
// top_entities_limit larger than the slice does not panic or pad.
func Test_handleListCommunities_ExplicitLimitHigherThanLen_2289(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			{ID: 1, Size: 100, Modularity: 0.5, TopEntities: []string{"a", "b"}},
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleListCommunities, map[string]any{
		"group":              "test",
		"top_entities_limit": 10,
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 community, got %d", len(got))
	}
	if top := got[0]["top_entities"].([]any); len(top) != 2 {
		t.Errorf("limit=10 with 2 entities should return 2, got %v", top)
	}
}

// Test_handleListCommunities_SortBySize_2319 verifies that communities are
// sorted by size in descending order before applying min_size and top_entities_limit.
// This ensures the cap applies to the largest communities, not an arbitrary subset.
// Issue #2319: without sorting, top_entities_limit=3 would pick 3 communities in
// iteration order rather than the 3 largest.
func Test_handleListCommunities_SortBySize_2319(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			// Provided in unsorted order (sizes 10, 50, 30)
			{ID: 1, Size: 10, Modularity: 0.5, TopEntities: []string{"a"}},
			{ID: 2, Size: 50, Modularity: 0.6, TopEntities: []string{"b", "c"}},
			{ID: 3, Size: 30, Modularity: 0.4, TopEntities: []string{"d", "e", "f"}},
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleListCommunities, map[string]any{
		"group":              "test",
		"min_size":           20, // filters out id=1 (size 10)
		"top_entities_limit": 3,  // default; not a limit on count but per-community
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	// Should keep ids 2 (50) and 3 (30), filtered in size descending order.
	if len(got) != 2 {
		t.Fatalf("want 2 communities (sizes 50 and 30 >= min_size 20), got %d: %+v", len(got), got)
	}
	// First result should be the largest (id=2, size 50)
	if got[0]["id"].(float64) != 2 {
		t.Errorf("first community should be id=2 (size 50), got id=%v", got[0]["id"])
	}
	if got[0]["size"].(float64) != 50 {
		t.Errorf("first community size should be 50, got %v", got[0]["size"])
	}
	// Second result should be id=3 (size 30)
	if got[1]["id"].(float64) != 3 {
		t.Errorf("second community should be id=3 (size 30), got id=%v", got[1]["id"])
	}
	if got[1]["size"].(float64) != 30 {
		t.Errorf("second community size should be 30, got %v", got[1]["size"])
	}
}
