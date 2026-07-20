package dashboard

// v2_modules_test.go — coverage for the GET /api/v2/groups/:group/modules/
// analysis endpoint, focused on the FULL modules + edges arrays added in
// #1386 to power the webui-v2 "Module overview" surface (closing epic
// #1380 alongside #1384).
//
// The shape under test is the per-repo entry inside data.repos[]:
//
//   - modules:    ONE entry per module (NOT top-N) so the overview can
//                 lay out every node — sized + tinted from the centrality
//                 data.
//   - edges:      ONE entry per directed module→module aggregated edge so
//                 the overview can render every inter-module arrow with a
//                 weight + scc_internal flag for cycle highlighting.
//
// The endpoint also keeps its pre-existing top_pagerank / top_betweenness /
// sccs fields, so the test asserts those survived too.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildModuleCycleFixture wires three modules into a directed dependency
// cycle (a → b → c → a) plus one singleton "solo" so the SCC pass yields a
// real, non-trivial SCC of size 3 and we can also exercise the non-cycle
// edge tinting code path.
func buildModuleCycleFixture() *DashGroup {
	mkMod := func(name string) graph.Entity {
		return graph.Entity{
			ID:   "mod-" + name,
			Kind: "Module",
			Name: name,
		}.WithProperties(map[string]string{
			"module": name,
			"repo":   "testrepo",
		},
		)
	}
	mkEnt := func(id, mod string) graph.Entity {
		return graph.Entity{
			ID:   id,
			Kind: "function",
			Name: id,
		}.WithProperties(map[string]string{
			"module": mod,
			"repo":   "testrepo",
		},
		)
	}

	entities := []graph.Entity{
		mkMod("a"), mkMod("b"), mkMod("c"), mkMod("solo"),
		mkEnt("a1", "a"), mkEnt("a2", "a"),
		mkEnt("b1", "b"), mkEnt("b2", "b"),
		mkEnt("c1", "c"), mkEnt("c2", "c"),
		mkEnt("s1", "solo"),
	}
	rels := []graph.Relationship{
		{ID: "a1->b1", FromID: "a1", ToID: "b1", Kind: "CALLS"},
		{ID: "a2->b2", FromID: "a2", ToID: "b2", Kind: "CALLS"},
		{ID: "b1->c1", FromID: "b1", ToID: "c1", Kind: "CALLS"},
		{ID: "c1->a1", FromID: "c1", ToID: "a1", Kind: "CALLS"},
		{ID: "a1->s1", FromID: "a1", ToID: "s1", Kind: "CALLS"},
	}
	return makeGraphTestGroup(entities, rels)
}

func newV2ModulesTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name: "testgrp", ConfigPath: "/tmp/testgrp.json", Repos: []string{"testrepo"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

type v2ModulesEdge struct {
	FromModule  string `json:"from_module"`
	ToModule    string `json:"to_module"`
	Weight      int    `json:"weight"`
	SCCInternal bool   `json:"scc_internal"`
	SCCID       int    `json:"scc_id"`
}

type v2ModulesCentrality struct {
	ModuleID    string  `json:"module_id"`
	ModuleName  string  `json:"module_name"`
	PageRank    float64 `json:"pagerank"`
	Betweenness float64 `json:"betweenness"`
	InDegree    int     `json:"in_degree"`
	OutDegree   int     `json:"out_degree"`
	InCycle     bool    `json:"in_cycle"`
}

type v2ModulesRepo struct {
	Repo           string                `json:"repo"`
	NumModules     int                   `json:"num_modules"`
	NumModuleEdges int                   `json:"num_module_edges"`
	NumSCCs        int                   `json:"num_sccs"`
	LargestSCCSize int                   `json:"largest_scc_size"`
	ModulesInCycle int                   `json:"modules_in_cycle"`
	TopPageRank    []v2ModulesCentrality `json:"top_pagerank"`
	TopBetweenness []v2ModulesCentrality `json:"top_betweenness"`
	SCCs           []struct {
		ID      int      `json:"id"`
		Size    int      `json:"size"`
		Members []string `json:"members"`
	} `json:"sccs"`
	Modules []v2ModulesCentrality `json:"modules"`
	Edges   []v2ModulesEdge       `json:"edges"`
}

type v2ModulesEnvelope struct {
	Data struct {
		Repos []v2ModulesRepo `json:"repos"`
		Count int             `json:"count"`
	} `json:"data"`
}

func fetchModuleAnalysis(t *testing.T, ts *httptest.Server) v2ModulesEnvelope {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/modules/analysis")
	if err != nil {
		t.Fatalf("GET modules/analysis: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var env v2ModulesEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

// TestV2ModulesAnalysis_FullModulesAndEdges asserts that the overview-
// powering fields land on the wire: every module gets a `modules` entry
// (not just the top-N) and every aggregated edge gets an `edges` entry
// with weight + scc_internal correctly set.
func TestV2ModulesAnalysis_FullModulesAndEdges(t *testing.T) {
	ts := newV2ModulesTestServer(t, buildModuleCycleFixture())
	env := fetchModuleAnalysis(t, ts)

	if len(env.Data.Repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(env.Data.Repos))
	}
	r := env.Data.Repos[0]

	if r.NumModules < 3 {
		t.Fatalf("want >=3 modules, got %d", r.NumModules)
	}
	// All four modules (a, b, c, solo) must appear in `modules`.
	if got := len(r.Modules); got != r.NumModules {
		t.Fatalf("modules array size %d != num_modules %d (overview needs every module)", got, r.NumModules)
	}
	names := map[string]v2ModulesCentrality{}
	for _, m := range r.Modules {
		names[m.ModuleName] = m
		if !strings.HasPrefix(m.ModuleID, "testrepo::") {
			t.Errorf("module ID %q lacks repo prefix", m.ModuleID)
		}
	}
	for _, want := range []string{"a", "b", "c", "solo"} {
		if _, ok := names[want]; !ok {
			t.Errorf("module %q missing from `modules` array", want)
		}
	}

	// in_cycle must be true for a/b/c (the SCC) and false for solo.
	for _, n := range []string{"a", "b", "c"} {
		if !names[n].InCycle {
			t.Errorf("module %q expected in_cycle=true, got false", n)
		}
	}
	if names["solo"].InCycle {
		t.Errorf("solo module unexpectedly flagged in_cycle=true")
	}

	// `edges` must contain the cycle edges, with scc_internal=true on at least
	// the cycle members, and the a→solo edge with scc_internal=false.
	if len(r.Edges) == 0 {
		t.Fatalf("edges array empty — overview cannot draw")
	}
	var sawCycle, sawNonCycle bool
	for _, e := range r.Edges {
		if !strings.HasPrefix(e.FromModule, "testrepo::") || !strings.HasPrefix(e.ToModule, "testrepo::") {
			t.Errorf("edge %v lacks repo prefix on endpoints", e)
		}
		if e.Weight <= 0 {
			t.Errorf("edge %v has non-positive weight %d", e, e.Weight)
		}
		if e.SCCInternal {
			sawCycle = true
			if e.SCCID < 0 {
				t.Errorf("scc_internal=true but scc_id=%d on edge %v", e.SCCID, e)
			}
		} else {
			sawNonCycle = true
			if e.SCCID >= 0 {
				t.Errorf("scc_internal=false but scc_id=%d on edge %v", e.SCCID, e)
			}
		}
	}
	if !sawCycle {
		t.Errorf("expected at least one scc_internal=true edge from the a→b→c→a cycle")
	}
	if !sawNonCycle {
		t.Errorf("expected at least one scc_internal=false edge (a→solo)")
	}

	// The pre-existing top-N and SCC fields must still be populated.
	if len(r.TopPageRank) == 0 {
		t.Errorf("top_pagerank unexpectedly empty")
	}
	if len(r.SCCs) == 0 || r.SCCs[0].Size < 3 {
		t.Errorf("expected an SCC of size >=3, got %+v", r.SCCs)
	}
}
