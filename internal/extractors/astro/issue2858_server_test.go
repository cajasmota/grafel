package astro

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// issue2858_server_test.go — proving fixtures for the Astro meta-framework
// Server (server_components, hydration_boundaries) + Data Flow (data_loaders) +
// Build (static_generation) coverage cells closed by issue #2858. Hand-written,
// dependency-manifest-free .astro snippets exercised through the extractor.

func astroExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "astro", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func hasSubtype(ents []types.EntityRecord, subtype string) bool {
	for _, e := range ents {
		if e.Subtype == subtype {
			return true
		}
	}
	return false
}

func hasName(ents []types.EntityRecord, name string) bool {
	for _, e := range ents {
		if e.Name == name {
			return true
		}
	}
	return false
}

func TestAstro2858ServerComponentAndDataLoaderAndStaticGen(t *testing.T) {
	src := `---
import Layout from '../layouts/Layout.astro'
export async function getStaticPaths() {
  const posts = await getCollection('blog')
  return posts.map((p) => ({ params: { slug: p.slug } }))
}
const { slug } = Astro.params
export const prerender = true
---
<Layout>
  <h1>{slug}</h1>
  <Counter client:load />
</Layout>
`
	ents := astroExtract(t, "src/pages/blog/[slug].astro", src)

	if !hasSubtype(ents, "server_component") {
		t.Error("expected Astro frontmatter server_component (server_components)")
	}
	if !hasName(ents, "getStaticPaths") {
		t.Error("expected getStaticPaths data_loader (data_loaders)")
	}
	if !hasName(ents, "getCollection") {
		t.Error("expected getCollection content-collection data_loader (data_loaders)")
	}
	if !hasSubtype(ents, "static_generation") {
		t.Error("expected getStaticPaths + prerender static_generation (static_generation)")
	}
	if !hasSubtype(ents, "client_boundary") {
		t.Error("expected client:load island hydration boundary (hydration_boundaries)")
	}
	// The island marker should name the hydrated component.
	if !hasName(ents, "island:Counter") {
		t.Error("expected island:Counter hydration-boundary marker")
	}
}

func TestAstro2858StaticPageNoIslandsIsServerOnly(t *testing.T) {
	// A plain content page: server-rendered, no client islands.
	src := `---
const title = 'About'
---
<html><body><h1>{title}</h1></body></html>
`
	ents := astroExtract(t, "src/pages/about.astro", src)
	if !hasSubtype(ents, "server_component") {
		t.Error("expected server_component for islandless page (server_components)")
	}
	if hasSubtype(ents, "client_boundary") {
		t.Error("islandless page should have no hydration boundary")
	}
}
