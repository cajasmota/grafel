// stepadj_bench_2417_test.go — microbenchmarks for the StepAdj cache (#2417).
//
// Parallel to PR #2285's BenchmarkGrafelAgentResolvedEdges / baseline pair.
// Two benches:
//
//   - BenchmarkBuildProcessSteps_Baseline: inline copy of the pre-#2417
//     O(R) scan over Doc.Relationships (the pattern being retired).
//   - BenchmarkBuildProcessSteps_Adj: the production path using StepAdj
//     (O(deg(proc)) lookup).
//
// Run with: go test ./internal/mcp/... -run=^$ -bench=BenchmarkBuildProcessSteps -benchmem
package mcp

import (
	"sort"
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildStepBenchDoc returns a Document with nProcs process entities, each
// owning nSteps STEP_IN_PROCESS edges, plus nNoise filler edges of other kinds
// so the linear scan baseline must walk the full relationship slice.
func buildStepBenchDoc(nProcs, nSteps, nNoise int) (*graph.Document, string) {
	var ents []graph.Entity
	var rels []graph.Relationship

	// One process entity that we will query.
	targetProcID := "proc0"
	ents = append(ents, graph.Entity{
		ID: targetProcID, Name: "Proc0", Kind: "process_flow", SourceFile: "f.go", StartLine: 1,
	})

	// Additional process entities (background; not queried).
	for p := 1; p < nProcs; p++ {
		pid := "proc" + strconv.Itoa(p)
		ents = append(ents, graph.Entity{
			ID: pid, Name: pid, Kind: "process_flow", SourceFile: "f.go", StartLine: p + 1,
		})
		// Each background process also has steps so RelCount is realistic.
		for s := 0; s < nSteps; s++ {
			sid := pid + "_s" + strconv.Itoa(s)
			ents = append(ents, graph.Entity{
				ID: sid, Name: sid, Kind: "Function", SourceFile: "f.go", StartLine: 1,
			})
			rels = append(rels, graph.Relationship{
				FromID: pid, ToID: sid, Kind: stepInProcessEdge,
			}.WithProperties(map[string]string{"step_index": strconv.Itoa(s)}))
		}
	}

	// Steps for the target process entity (proc0).
	for s := 0; s < nSteps; s++ {
		sid := targetProcID + "_s" + strconv.Itoa(s)
		ents = append(ents, graph.Entity{
			ID: sid, Name: sid, Kind: "Function", SourceFile: "f.go", StartLine: 1,
		})
		rels = append(rels, graph.Relationship{
			FromID: targetProcID, ToID: sid, Kind: stepInProcessEdge,
		}.WithProperties(map[string]string{"step_index": strconv.Itoa(s)}))
	}

	// Noise edges of other kinds to inflate R.
	for i := 0; i < nNoise; i++ {
		nid := "n" + strconv.Itoa(i)
		ents = append(ents, graph.Entity{
			ID: nid, Name: nid, Kind: "Function", SourceFile: "f.go", StartLine: 1,
		})
		rels = append(rels, graph.Relationship{
			FromID: nid, ToID: targetProcID, Kind: "CALLS",
		})
	}

	return &graph.Document{Entities: ents, Relationships: rels}, targetProcID
}

// buildProcessSteps_baseline is the PRE-#2417 O(R) linear scan, inlined for
// benchmark comparison.  Not part of the production code path.
func buildProcessSteps_baseline(doc *graph.Document, procID string) []struct {
	idx int
	id  string
} {
	type indexed struct {
		idx int
		id  string
	}
	var ordered []indexed
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if rel.Kind != stepInProcessEdge || rel.FromID != procID {
			continue
		}
		idxStr := ""
		if rel.PropLen() > 0 {
			idxStr = rel.PropGet("step_index")
		}
		n, _ := strconv.Atoi(idxStr)
		ordered = append(ordered, indexed{n, rel.ToID})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].idx < ordered[j].idx })
	out := make([]struct {
		idx int
		id  string
	}, len(ordered))
	for i, o := range ordered {
		out[i] = struct {
			idx int
			id  string
		}{o.idx, o.id}
	}
	return out
}

// BenchmarkBuildProcessSteps_Baseline benchmarks the pre-#2417 O(R) scan.
// Relationship count: 10 procs × 8 steps + 10 000 noise = 10 080.
func BenchmarkBuildProcessSteps_Baseline(b *testing.B) {
	doc, procID := buildStepBenchDoc(10, 8, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildProcessSteps_baseline(doc, procID)
	}
}

// BenchmarkBuildProcessSteps_Adj benchmarks the post-#2417 O(deg(proc)) path.
func BenchmarkBuildProcessSteps_Adj(b *testing.B) {
	doc, procID := buildStepBenchDoc(10, 8, 10000)
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	procEnt := byID[procID]
	lr := &LoadedRepo{
		Repo:       "bench",
		Doc:        doc,
		LabelIndex: BuildLabelIndex(doc),
	}
	// Warm the lazy indexes so the loop measures the query, not the build (#3367).
	lr.getStepAdj()
	lr.getAdjacency()
	lr.getByID()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildProcessStepsWithCrossRepo(lr, procEnt, nil)
	}
}
