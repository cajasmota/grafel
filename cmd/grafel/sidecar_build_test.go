package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestBuildStatsSidecar_AlgoNil_CountsAlwaysWritten is the regression guard
// for the bug where the daemon's reactive/rebuild reindex runs with
// --skip-pass=graph-algo (AlgorithmStats == nil), which previously skipped
// the sidecar write entirely and left graph-stats.json holding a stale
// count while graph.fb held the fresh one. The sidecar must now always be
// built (and thus written) with the fresh counts, regardless of whether
// Pass 4 ran.
func TestBuildStatsSidecar_AlgoNil_CountsAlwaysWritten(t *testing.T) {
	doc := &graph.Document{
		Stats: graph.Stats{Files: 10, Entities: 200, Relationships: 66000},
	}
	side := buildStatsSidecar(doc, 1500, nil, false, nil, time.Unix(1000, 0).UTC())

	if side == nil {
		t.Fatalf("buildStatsSidecar returned nil; sidecar must always be built so it can be written")
	}
	if side.TotalFiles != 10 || side.TotalEntities != 200 || side.TotalRelationships != 66000 {
		t.Errorf("counts not carried from fresh doc.Stats: %+v", side)
	}
	if side.ExtractMS != 1500 {
		t.Errorf("extract_ms: got %d, want 1500", side.ExtractMS)
	}
	if side.Version != 1 {
		t.Errorf("version: got %d, want 1", side.Version)
	}
}

// TestBuildStatsSidecar_AlgoNil_PreservesPriorAlgoFields verifies that when
// Pass 4 (graph-algo) was skipped, any pre-existing algorithm-derived fields
// (communities, modularity, god nodes, articulation points, runtime) are
// carried forward from the prior sidecar rather than zeroed out, while the
// counts are refreshed to the new build's values.
func TestBuildStatsSidecar_AlgoNil_PreservesPriorAlgoFields(t *testing.T) {
	doc := &graph.Document{
		Stats: graph.Stats{Files: 12, Entities: 300, Relationships: 500},
	}
	prior := &graph.GraphStatsSidecar{
		Version:            1,
		TotalEntities:      999999, // stale — must NOT leak through
		TotalRelationships: 3561346,
		Communities:        7,
		Modularity:         0.42,
		GodNodes:           3,
		ArticulationPoints: 5,
		RuntimeMS:          8800,
	}

	side := buildStatsSidecar(doc, 2000, nil, false, prior, time.Unix(2000, 0).UTC())

	if side.TotalEntities != 300 || side.TotalRelationships != 500 || side.TotalFiles != 12 {
		t.Errorf("counts must be refreshed from fresh doc.Stats, not carried from prior: %+v", side)
	}
	if side.Communities != 7 || side.Modularity != 0.42 || side.GodNodes != 3 ||
		side.ArticulationPoints != 5 || side.RuntimeMS != 8800 {
		t.Errorf("algorithm fields must be carried forward from prior sidecar: %+v", side)
	}
}

// TestBuildStatsSidecar_AlgoNil_NoPriorSidecar verifies that when Pass 4 was
// skipped AND there is no pre-existing sidecar (first-ever index, or one
// deleted), the algorithm fields simply default to zero rather than erroring.
func TestBuildStatsSidecar_AlgoNil_NoPriorSidecar(t *testing.T) {
	doc := &graph.Document{
		Stats: graph.Stats{Files: 1, Entities: 5, Relationships: 4},
	}
	side := buildStatsSidecar(doc, 10, nil, false, nil, time.Unix(3000, 0).UTC())

	if side.TotalEntities != 5 || side.TotalRelationships != 4 {
		t.Errorf("counts: %+v", side)
	}
	if side.Communities != 0 || side.Modularity != 0 || side.GodNodes != 0 ||
		side.ArticulationPoints != 0 || side.RuntimeMS != 0 {
		t.Errorf("algorithm fields must default to zero with no prior sidecar: %+v", side)
	}
}

// TestBuildStatsSidecar_AlgoPresent_AllFieldsFromFreshBuild verifies that
// when Pass 4 DID run, all fields (counts + algorithm) come from the fresh
// build, exactly as before this fix — even if a prior sidecar with
// different algorithm values is passed in (it must be ignored in favor of
// the fresh AlgorithmStats).
func TestBuildStatsSidecar_AlgoPresent_AllFieldsFromFreshBuild(t *testing.T) {
	doc := &graph.Document{
		Stats: graph.Stats{Files: 20, Entities: 400, Relationships: 900},
		AlgorithmStats: &graph.AlgorithmStats{
			NumCommunities:     11,
			LouvainModularity:  0.55,
			NumGodNodes:        2,
			NumArticulationPts: 6,
			RuntimeMS:          4321,
		},
	}
	prior := &graph.GraphStatsSidecar{
		Communities:        999,
		Modularity:         0.01,
		GodNodes:           999,
		ArticulationPoints: 999,
		RuntimeMS:          999,
	}
	canary := json.RawMessage(`{"spiked":false}`)

	side := buildStatsSidecar(doc, 777, canary, true, prior, time.Unix(4000, 0).UTC())

	if side.TotalFiles != 20 || side.TotalEntities != 400 || side.TotalRelationships != 900 {
		t.Errorf("counts: %+v", side)
	}
	if side.Communities != 11 || side.Modularity != 0.55 || side.GodNodes != 2 ||
		side.ArticulationPoints != 6 || side.RuntimeMS != 4321 {
		t.Errorf("algorithm fields must come from fresh AlgorithmStats, not prior: %+v", side)
	}
	if side.ExtractMS != 777 {
		t.Errorf("extract_ms: got %d, want 777", side.ExtractMS)
	}
	if !side.ParseErrorSpike {
		t.Errorf("parse_error_spike: got false, want true")
	}
	if string(side.ParseErrorCanary) != string(canary) {
		t.Errorf("parse_error_canary not carried through: %s", side.ParseErrorCanary)
	}
}
