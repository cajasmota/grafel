package astro

import "testing"

// issue2878_idioms_test.go — proving fixtures for the Astro framework_specific
// *idiom* cells closed by issue #2878:
//
//   astro_island_directive  — client:* hydration directives on a child component
//                             (the partial-hydration island idiom).
//   astro_frontmatter_fetch — a server-side `fetch(...)` in the --- frontmatter,
//                             Astro's idiomatic data-fetch baked into the render.
//
// Hand-written, dependency-manifest-free .astro snippets exercised through the
// extractor.

func TestAstro2878IslandDirective(t *testing.T) {
	src := `---
import Counter from '../components/Counter.jsx'
import Chart from '../components/Chart.jsx'
---
<main>
  <Counter client:load />
  <Chart client:visible />
</main>
`
	ents := astroExtract(t, "src/pages/index.astro", src)

	// IMPLEMENTS island edges on the host component entity.
	var islandEdges int
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" && r.Properties["island_directive"] != "" {
				islandEdges++
			}
		}
	}
	if islandEdges < 2 {
		t.Errorf("expected >=2 island IMPLEMENTS edges, got %d (astro_island_directive)", islandEdges)
	}

	// First-class island marker entities.
	if !hasSubtype(ents, "client_boundary") {
		t.Error("expected client_boundary island marker (astro_island_directive)")
	}
	if !hasName(ents, "island:Counter") {
		t.Error("expected island:Counter marker (astro_island_directive)")
	}
}

func TestAstro2878FrontmatterFetch(t *testing.T) {
	src := `---
const res = await fetch('https://api.example.com/posts')
const posts = await res.json()
---
<ul>{posts.map((p) => <li>{p.title}</li>)}</ul>
`
	ents := astroExtract(t, "src/pages/posts.astro", src)
	if !hasName(ents, "frontmatter_fetch") {
		t.Error("expected frontmatter_fetch data_loader (astro_frontmatter_fetch)")
	}
	var found bool
	for _, e := range ents {
		if e.Name == "frontmatter_fetch" && e.Subtype == "data_loader" &&
			e.Properties["loader_kind"] == "frontmatter_fetch" && e.Properties["rendering"] == "server" {
			found = true
		}
	}
	if !found {
		t.Error("frontmatter_fetch must be a server-rendering data_loader (astro_frontmatter_fetch)")
	}

	// A frontmatter with no fetch must NOT emit a frontmatter_fetch marker.
	noFetch := `---
const title = 'Static'
---
<h1>{title}</h1>
`
	nf := astroExtract(t, "src/pages/static.astro", noFetch)
	if hasName(nf, "frontmatter_fetch") {
		t.Error("a frontmatter with no fetch should not emit frontmatter_fetch")
	}
}
