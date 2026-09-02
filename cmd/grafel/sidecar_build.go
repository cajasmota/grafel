package main

import (
	"encoding/json"
	"time"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
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
	renameStats algorithms.RenameStats,
	unsupportedExt map[string]int,
	undeclared fbwriter.UndeclaredKindReport,
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

		// #6087 — rename-detection completeness, always taken from THIS run
		// and never carried forward from prior: it describes the pass that
		// produced the graph now being written, so a stale true would be as
		// wrong as a stale false. A run that skipped the pass or had no prior
		// graph writes false, which is correct — nothing was dropped.
		RenameDetectTruncated:    renameStats.Truncated,
		RenameDetectAddedSkipped: renameStats.AddedSkipped,
		RenameDetectPairsSkipped: renameStats.PairsSkipped,
	}

	// #6338 — always from THIS run, never carried forward from prior: it
	// describes the files this walk saw. Assigned unconditionally; an empty
	// tally is dropped from the JSON by the field's omitempty tag, which is
	// what makes a fully-supported repo carry no key at all. (A len()>0 guard
	// here was measured to be dead — omitempty already elides an empty map —
	// and removed.)
	side.UnsupportedExtensions = unsupportedExt

	// #6757 arm C — always from THIS run, never carried forward: it describes
	// the relationships this write path actually serialized, including the
	// runtime-valued kinds no static scan can see. A clean run leaves all three
	// fields zero and omitempty drops them, so a repo with a fully declared
	// vocabulary carries no key at all.
	side.UndeclaredRelationshipEdges = undeclared.Edges
	side.UndeclaredRelationshipKindCount = undeclared.DistinctKinds
	if len(undeclared.Kinds) > 0 {
		kinds := make(map[string]int, len(undeclared.Kinds))
		for _, k := range undeclared.Kinds {
			kinds[k.Kind] = k.Edges
		}
		side.UndeclaredRelationshipKinds = kinds
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
