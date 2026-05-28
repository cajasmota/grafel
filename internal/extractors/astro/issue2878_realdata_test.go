package astro

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// issue2878_realdata_test.go — real-data verification for the Astro idiom cells
// (#2878) over the shared meta-framework corpus.

func TestAstro2878RealDataIslandDirective(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world",
		"meta-framework", "astro-app", "src", "pages", "blog", "[slug].astro")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("corpus file not present: %v", err)
	}
	e := &Extractor{}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "astro", Content: content,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// The corpus page renders `<Counter client:visible />` — an island.
	var islandEdges int
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == "IMPLEMENTS" && r.Properties["island_directive"] != "" {
				islandEdges++
			}
		}
	}
	if islandEdges == 0 {
		t.Error("real-data: expected >=1 island IMPLEMENTS edge (astro_island_directive)")
	}
	if !hasSubtype(ents, "client_boundary") {
		t.Error("real-data: expected client_boundary island marker (astro_island_directive)")
	}
}
