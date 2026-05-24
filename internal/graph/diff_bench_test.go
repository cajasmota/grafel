// diff_bench_test.go — benchmark the DiffDocs algorithm at realistic scale.
package graph

import (
	"fmt"
	"testing"
)

// buildSyntheticDoc creates a *Document with n entities and ~n*3 relationships
// for benchmarking. IDs are deterministic 8-hex strings.
func buildSyntheticDoc(n int) *Document {
	entities := make([]Entity, n)
	for i := 0; i < n; i++ {
		entities[i] = Entity{
			ID:         fmt.Sprintf("%08x", i),
			Kind:       []string{"Function", "Class", "Interface", "Module"}[i%4],
			Name:       fmt.Sprintf("entity_%d", i),
			SourceFile: fmt.Sprintf("pkg/file%d.go", i/50),
			StartLine:  i * 10,
			EndLine:    i*10 + 9,
		}
	}
	// ~3 relationships per entity.
	rels := make([]Relationship, 0, n*3)
	for i := 0; i < n-1; i++ {
		fromID := fmt.Sprintf("%08x", i)
		toID := fmt.Sprintf("%08x", i+1)
		rels = append(rels, Relationship{
			ID:     RelationshipID(fromID, toID, "calls"),
			FromID: fromID,
			ToID:   toID,
			Kind:   "calls",
		})
		if i+5 < n {
			toID2 := fmt.Sprintf("%08x", i+5)
			rels = append(rels, Relationship{
				ID:     RelationshipID(fromID, toID2, "uses"),
				FromID: fromID,
				ToID:   toID2,
				Kind:   "uses",
			})
		}
	}
	return &Document{Entities: entities, Relationships: rels}
}

// BenchmarkDiffDocs_6k benchmarks the diff algorithm at the 6k-entity
// scale mentioned in the spec as the "typical" size.
func BenchmarkDiffDocs_6k(b *testing.B) {
	docA := buildSyntheticDoc(6000)
	// docB: 80% shared, 10% added, 10% modified.
	docBEntities := make([]Entity, 0, 6200)
	for i := 0; i < 5400; i++ { // 90% of A → kept
		docBEntities = append(docBEntities, docA.Entities[i])
	}
	// Modify 600 entities (simulate source_window changes).
	for i := 5400; i < 6000; i++ {
		e := docA.Entities[i]
		e.StartLine += 100 // changes the source_window hash
		docBEntities = append(docBEntities, e)
	}
	// Add 200 new entities.
	for i := 0; i < 200; i++ {
		docBEntities = append(docBEntities, Entity{
			ID:         fmt.Sprintf("new%06x", i),
			Kind:       "Function",
			Name:       fmt.Sprintf("newFn_%d", i),
			SourceFile: fmt.Sprintf("pkg/new%d.go", i),
		})
	}
	docB := &Document{Entities: docBEntities, Relationships: docA.Relationships}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffDocs(docA, docB)
	}
}

// BenchmarkDiffDocs_1k benchmarks at smaller scale.
func BenchmarkDiffDocs_1k(b *testing.B) {
	docA := buildSyntheticDoc(1000)
	docB := buildSyntheticDoc(1100) // mostly different IDs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffDocs(docA, docB)
	}
}
