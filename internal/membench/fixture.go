// Package membench builds a large SYNTHETIC graph.Document (no corpus access
// required) and measures the heap cost of the in-process post-extraction
// pipeline — external.Synthesize + the group-scope graph-algorithm pass
// (BuildGraph -> ComputeCommunities -> ComputeCentrality via
// graph.RunAlgorithms) — that the split-mode ENGINE process runs per repo.
//
// It exists to reproduce and bound the RSS/CPU blow-up reported in #5681 on a
// LARGE monorepo (287k entities / 1.3M relationships) WITHOUT touching the
// off-limits corpus. The fixture is deterministic (fixed PRNG seed) so peak
// numbers are comparable run-to-run.
package membench

import (
	"fmt"
	"math/rand"

	"github.com/cajasmota/grafel/internal/graph"
)

// FixtureSpec controls the shape of the synthetic document.
type FixtureSpec struct {
	// Entities is the number of code entities (functions/methods/classes).
	Entities int
	// Files is the number of distinct SourceFile paths the entities spread
	// across (each file also gets a SCOPE.Component file node that owns the
	// IMPORTS edges, matching the post-#577 extractor shape).
	Files int
	// CallEdges is the number of CALLS relationships wired between entities
	// (both endpoints are real entities so BuildGraph retains them).
	CallEdges int
	// ImportsPerFile is the average number of IMPORTS edges emitted per file
	// component to bare external package names (Synthesize turns each unique
	// name into an ext:<name> placeholder entity).
	ImportsPerFile int
	// ExternalPackages is the size of the external-package name pool the
	// IMPORTS edges draw from (bounds how many ext: entities Synthesize adds).
	ExternalPackages int
	// Seed makes the fixture deterministic.
	Seed int64
}

// DefaultLargeSpec approximates the #5681 monorepo scale: ~220k entities across
// ~18k files with ~1.2M CALLS edges + ~360k IMPORTS edges. Well above the
// 8000-node sampled-betweenness threshold (algorithms.go:605), so the sampled
// path is exercised — guarding against an accidental exact-Brandes regression.
func DefaultLargeSpec() FixtureSpec {
	return FixtureSpec{
		Entities:         220_000,
		Files:            18_000,
		CallEdges:        1_200_000,
		ImportsPerFile:   20,
		ExternalPackages: 4_000,
		Seed:             0x5681,
	}
}

// langs cycles a handful of realistic language tags so the Synthesize
// classifier's per-language allowlists have something to chew on.
var langs = []string{"go", "python", "typescript", "java", "kotlin"}

// BuildSyntheticDocument assembles a graph.Document to the given spec. Every
// entity carries populated Properties + Metadata maps (the live-object weight
// that dominates the per-run heap on entity-dense repos). IMPORTS edges point
// at bare external names so external.Synthesize has real work to do.
func BuildSyntheticDocument(spec FixtureSpec) *graph.Document {
	rng := rand.New(rand.NewSource(spec.Seed))

	nFiles := spec.Files
	if nFiles < 1 {
		nFiles = 1
	}

	// File component entities (post-#577 IMPORTS FromID shape). One per file.
	fileIDs := make([]string, nFiles)
	entities := make([]graph.Entity, 0, spec.Entities+nFiles)
	for f := 0; f < nFiles; f++ {
		path := fmt.Sprintf("src/pkg%03d/module%05d.go", f%512, f)
		id := graph.EntityID("mono", "SCOPE.Component", path, path)
		fileIDs[f] = id
		entities = append(entities, graph.Entity{
			ID:         id,
			Name:       fmt.Sprintf("module%05d", f),
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: path,
			Language:   langs[f%len(langs)],

			Metadata: map[string]interface{}{"synthetic": true, "file_index": f},
		}.WithProperties(map[string]string{"path": path, "loc": fmt.Sprintf("%d", 100+rng.Intn(900))}))
	}

	// Code entities, each anchored to a file.
	entIDs := make([]string, spec.Entities)
	for i := 0; i < spec.Entities; i++ {
		f := rng.Intn(nFiles)
		path := entities[f].SourceFile
		name := fmt.Sprintf("Func_%06d", i)
		id := graph.EntityID("mono", "SCOPE.Function", name, path)
		entIDs[i] = id
		entities = append(entities, graph.Entity{
			ID:            id,
			Name:          name,
			QualifiedName: fmt.Sprintf("pkg%03d.%s", f%512, name),
			Kind:          "SCOPE.Function",
			Subtype:       "function",
			SourceFile:    path,
			StartLine:     1 + rng.Intn(2000),
			EndLine:       1 + rng.Intn(2000),
			Language:      langs[f%len(langs)],
			Signature:     fmt.Sprintf("func %s(ctx context.Context, id int) error", name),

			Metadata: map[string]interface{}{
				"complexity": rng.Intn(30),
				"synthetic":  true,
			},
		}.WithProperties(map[string]string{
			"visibility":     "public",
			"receiver":       fmt.Sprintf("T%03d", i%256),
			"callsite_count": fmt.Sprintf("%d", 1+rng.Intn(5)),
		},
		))
	}

	// External package name pool for IMPORTS targets.
	extPkgs := make([]string, spec.ExternalPackages)
	for p := range extPkgs {
		extPkgs[p] = fmt.Sprintf("com.example.dep%04d", p)
	}

	nImports := nFiles * spec.ImportsPerFile
	rels := make([]graph.Relationship, 0, spec.CallEdges+nImports)

	// CALLS edges between real entities (retained by BuildGraph).
	for e := 0; e < spec.CallEdges; e++ {
		from := entIDs[rng.Intn(len(entIDs))]
		to := entIDs[rng.Intn(len(entIDs))]
		rels = append(rels, graph.Relationship{
			ID:     graph.RelationshipID(from, to, "CALLS"),
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		}.WithProperties(map[string]string{
			"callsite_count": fmt.Sprintf("%d", 1+rng.Intn(4)),
		},
		))
	}

	// IMPORTS edges from file components to bare external names (Synthesize fuel).
	for f := 0; f < nFiles; f++ {
		for k := 0; k < spec.ImportsPerFile; k++ {
			pkg := extPkgs[rng.Intn(len(extPkgs))]
			rels = append(rels, graph.Relationship{
				ID:     graph.RelationshipID(fileIDs[f], pkg, "IMPORTS"),
				FromID: fileIDs[f],
				ToID:   pkg, // bare, unresolved → Synthesize makes ext:<pkg>
				Kind:   "IMPORTS",
			}.WithProperties(map[string]string{
				"source_module": pkg,
				"imported_name": fmt.Sprintf("Sym%d", k),
			},
			))
		}
	}

	return &graph.Document{
		Version:       1,
		Repo:          "mono",
		Entities:      entities,
		Relationships: rels,
		Stats: graph.Stats{
			Files:         nFiles,
			Entities:      len(entities),
			Relationships: len(rels),
		},
	}
}
