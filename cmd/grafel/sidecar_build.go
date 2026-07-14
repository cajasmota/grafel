package main

import (
	"encoding/json"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildStatsSidecar constructs the graph-stats.json payload for the index
// pass that just produced doc. It is a pure function (no I/O) so the
// count-drift regression (#task-31: graph.fb rewritten with fresh counts
// while graph-stats.json kept an arbitrarily stale count because the whole
// sidecar write was gated behind doc.AlgorithmStats != nil) can be covered
// by unit tests without touching the filesystem.
//
// Counts (TotalFiles/TotalEntities/TotalRelationships), ExtractMS, and the
// parse-error canary are ALWAYS taken from the fresh build — they must never
// be allowed to diverge from graph.fb.
//
// The algorithm-derived fields (Communities/Modularity/GodNodes/
// ArticulationPoints/RuntimeMS) come from doc.AlgorithmStats when Pass 4
// (graph-algo) ran. When it was skipped (doc.AlgorithmStats == nil — the
// daemon's reactive/rebuild reindex path always skips it), those fields are
// carried forward from prior (the previously-written sidecar, or nil if
// none exists yet) rather than zeroed, so a skip-graph-algo reindex never
// wipes out real algorithm data computed by an earlier full build.
func buildStatsSidecar(
	doc *graph.Document,
	extractMS int64,
	canaryRaw json.RawMessage,
	canarySpiked bool,
	prior *graph.GraphStatsSidecar,
	computedAt time.Time,
) *graph.GraphStatsSidecar {
	side := &graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         computedAt,
		TotalFiles:         doc.Stats.Files,
		TotalEntities:      doc.Stats.Entities,
		TotalRelationships: doc.Stats.Relationships,
		ExtractMS:          extractMS,
		ParseErrorCanary:   canaryRaw,
		ParseErrorSpike:    canarySpiked,
	}

	if doc.AlgorithmStats != nil {
		side.Communities = doc.AlgorithmStats.NumCommunities
		side.Modularity = doc.AlgorithmStats.LouvainModularity
		side.GodNodes = doc.AlgorithmStats.NumGodNodes
		side.ArticulationPoints = doc.AlgorithmStats.NumArticulationPts
		side.RuntimeMS = doc.AlgorithmStats.RuntimeMS
	} else if prior != nil {
		side.Communities = prior.Communities
		side.Modularity = prior.Modularity
		side.GodNodes = prior.GodNodes
		side.ArticulationPoints = prior.ArticulationPoints
		side.RuntimeMS = prior.RuntimeMS
	}

	return side
}
