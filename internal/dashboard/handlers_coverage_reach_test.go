package dashboard

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/coverage"
	"github.com/cajasmota/archigraph/internal/graph"
)

func ep(id, name string, reachable string) graph.Entity {
	props := map[string]string{}
	if reachable != "" {
		props[coverage.PropTestReachable] = reachable
	}
	return graph.Entity{ID: id, Name: name, Kind: "SCOPE.Endpoint", Properties: props}
}

func TestReachAccumulator_TestedUntestedRollup(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		ep("e1", "GET /a", "true"),
		ep("e2", "POST /b", "false"),
		ep("e3", "GET /c", "false"),
		// A non-endpoint with the prop must be ignored.
		{ID: "f1", Name: "helper", Kind: "SCOPE.Function",
			Properties: map[string]string{coverage.PropTestReachable: "false"}},
	}}

	var a reachAccumulator
	a.accumulate(doc, "repoA")
	s := a.summarize()

	if !s.Computed {
		t.Fatal("expected Computed=true when endpoints carry the prop")
	}
	if s.TotalEndpoints != 3 || s.TestedEndpoints != 1 || s.OrphanEndpoints != 2 {
		t.Fatalf("rollup mismatch: total=%d tested=%d orphan=%d",
			s.TotalEndpoints, s.TestedEndpoints, s.OrphanEndpoints)
	}
	if len(s.Orphans) != 2 {
		t.Fatalf("expected 2 orphans, got %d", len(s.Orphans))
	}
	for _, o := range s.Orphans {
		if o.Repo != "repoA" {
			t.Errorf("orphan %s missing repo slug, got %q", o.ID, o.Repo)
		}
	}
	if got := s.ReachablePct; got < 33.0 || got > 34.0 {
		t.Errorf("ReachablePct = %v, want ~33.3", got)
	}
}

func TestReachAccumulator_NotComputedDegrades(t *testing.T) {
	// Endpoints exist but none carry the reachability prop (pre-#5061 index).
	doc := &graph.Document{Entities: []graph.Entity{
		ep("e1", "GET /a", ""),
		ep("e2", "POST /b", ""),
	}}

	var a reachAccumulator
	a.accumulate(doc, "repoA")
	s := a.summarize()

	if s.Computed {
		t.Fatal("expected Computed=false when no endpoint carries the prop")
	}
	if s.TotalEndpoints != 0 || s.OrphanEndpoints != 0 {
		t.Fatalf("unstamped endpoints must not be counted: total=%d orphan=%d",
			s.TotalEndpoints, s.OrphanEndpoints)
	}
	if len(s.Orphans) != 0 {
		t.Fatalf("expected no orphans when not computed, got %d", len(s.Orphans))
	}
}

func TestReachAccumulator_AllTestedNoOrphans(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		ep("e1", "GET /a", "true"),
		ep("e2", "POST /b", "true"),
	}}

	var a reachAccumulator
	a.accumulate(doc, "repoA")
	s := a.summarize()

	if !s.Computed {
		t.Fatal("expected Computed=true")
	}
	if s.OrphanEndpoints != 0 || len(s.Orphans) != 0 {
		t.Fatalf("expected zero orphans, got %d / %d", s.OrphanEndpoints, len(s.Orphans))
	}
	if s.ReachablePct != 100.0 {
		t.Errorf("ReachablePct = %v, want 100", s.ReachablePct)
	}
}

func TestReachAccumulator_OrphanCap(t *testing.T) {
	ents := make([]graph.Entity, 0, reachOrphanCap+5)
	for i := 0; i < reachOrphanCap+5; i++ {
		ents = append(ents, ep("e"+string(rune('a'+i%26))+string(rune(i)), "GET /x", "false"))
	}
	doc := &graph.Document{Entities: ents}

	var a reachAccumulator
	a.accumulate(doc, "r")
	s := a.summarize()

	if len(s.Orphans) != reachOrphanCap {
		t.Fatalf("orphan list not capped: got %d want %d", len(s.Orphans), reachOrphanCap)
	}
	if s.OrphansMore != 5 {
		t.Errorf("OrphansMore = %d, want 5", s.OrphansMore)
	}
	if s.OrphanEndpoints != reachOrphanCap+5 {
		t.Errorf("OrphanEndpoints count must reflect full total, got %d", s.OrphanEndpoints)
	}
}

func TestReachAccumulator_MultiRepo(t *testing.T) {
	var a reachAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{ep("a1", "GET /a", "true")}}, "repoA")
	a.accumulate(&graph.Document{Entities: []graph.Entity{ep("b1", "GET /b", "false")}}, "repoB")
	s := a.summarize()

	if s.TotalEndpoints != 2 || s.TestedEndpoints != 1 || s.OrphanEndpoints != 1 {
		t.Fatalf("multi-repo rollup mismatch: %+v", s)
	}
	if len(s.Orphans) != 1 || s.Orphans[0].Repo != "repoB" {
		t.Fatalf("orphan repo slug not propagated: %+v", s.Orphans)
	}
}
