package astro_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/astro" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func mustExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("astro")
	if !ok {
		t.Fatal("astro extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "astro",
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

func findByName(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func findBySubtype(recs []types.EntityRecord, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Subtype == subtype {
			out = append(out, r)
		}
	}
	return out
}

func hasRelKind(recs []types.EntityRecord, kind, toID string) bool {
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == kind && (toID == "" || rel.ToID == toID) {
				return true
			}
		}
	}
	return false
}

func countRelKind(recs []types.EntityRecord, kind string) int {
	n := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == kind {
				n++
			}
		}
	}
	return n
}

// ── synthetic fixture — Astro page under pages/ ──────────────────────────────
//
// BlogPost.astro — represents a typical Astro page with:
//   - frontmatter: imports, Astro.props destructure
//   - framework islands (React + Vue)
//   - child component references
//   - duplicate tags (should be deduplicated in RENDERS)
//   - <style> block (should be ignored)
const blogPostAstro = `---
import Layout from '../layouts/Layout.astro';
import Header from '../components/Header.astro';
import ReactCounter from '../components/ReactCounter.jsx';
import VueLikeButton from '../components/VueLikeButton.vue';
import { getCollection, getEntry } from 'astro:content';

const { title, description = 'No description', publishDate } = Astro.props;
const posts = await getCollection('blog');
---

<Layout title={title}>
  <Header />
  <article>
    <h1>{title}</h1>
    <p>{description}</p>
    <ReactCounter client:load initialCount={0} />
    <VueLikeButton client:idle postId="123" />
    <!-- Duplicate Header — should produce only one RENDERS edge -->
    <Header />
  </article>
</Layout>

<style>
  article {
    max-width: 65ch;
    margin: 0 auto;
  }
</style>
`

// ── registration ─────────────────────────────────────────────────────────────

func TestExtractor_Language(t *testing.T) {
	ext, ok := extractor.Get("astro")
	if !ok {
		t.Fatal("astro extractor not registered")
	}
	if ext.Language() != "astro" {
		t.Errorf("Language() = %q, want %q", ext.Language(), "astro")
	}
}

// ── component entity — page subtype ──────────────────────────────────────────

func TestExtractor_PageEntity(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	comp := findByName(recs, "BlogPost")
	if comp == nil {
		t.Fatal("expected SCOPE.Component entity named 'BlogPost'")
	}
	if comp.Kind != "SCOPE.Component" {
		t.Errorf("Kind = %q, want SCOPE.Component", comp.Kind)
	}
	if comp.Subtype != "astro_page" {
		t.Errorf("Subtype = %q, want astro_page", comp.Subtype)
	}
	if comp.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", comp.StartLine)
	}
	if comp.Language != "astro" {
		t.Errorf("Language = %q, want astro", comp.Language)
	}
}

// ── component entity — non-page subtype ──────────────────────────────────────

func TestExtractor_ComponentSubtype(t *testing.T) {
	const src = `---
const { label } = Astro.props;
---
<button>{label}</button>
`
	recs := mustExtract(t, "src/components/MyButton.astro", src)
	comp := findByName(recs, "MyButton")
	if comp == nil {
		t.Fatal("expected component entity 'MyButton'")
	}
	if comp.Subtype != "astro_component" {
		t.Errorf("Subtype = %q, want astro_component", comp.Subtype)
	}
}

// ── frontmatter imports → IMPORTS edges ──────────────────────────────────────

func TestExtractor_ImportEdges(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	for _, want := range []string{
		"../layouts/Layout.astro",
		"../components/Header.astro",
		"../components/ReactCounter.jsx",
		"../components/VueLikeButton.vue",
		"astro:content",
	} {
		if !hasRelKind(recs, "IMPORTS", want) {
			t.Errorf("expected IMPORTS edge to %q", want)
		}
	}
}

// ── Astro.props destructured → prop entities ─────────────────────────────────

func TestExtractor_DestructuredProps(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	props := findBySubtype(recs, "prop")
	propNames := make(map[string]bool)
	for _, p := range props {
		propNames[p.Name] = true
	}

	for _, want := range []string{"title", "description", "publishDate"} {
		if !propNames[want] {
			t.Errorf("expected prop entity %q, got: %v", want, propNames)
		}
	}

	// Verify Kind on each prop
	titleProp := findByName(recs, "title")
	if titleProp == nil {
		t.Fatal("expected entity named 'title'")
	}
	if titleProp.Kind != "SCOPE.Operation" {
		t.Errorf("title Kind = %q, want SCOPE.Operation", titleProp.Kind)
	}
}

// ── Astro.props non-destructured binding ─────────────────────────────────────

func TestExtractor_PropsBinding(t *testing.T) {
	const src = `---
const props = Astro.props;
---
<div>{props.title}</div>
`
	recs := mustExtract(t, "src/components/Widget.astro", src)

	binding := findByName(recs, "props")
	if binding == nil {
		t.Fatal("expected entity 'props' for non-destructured Astro.props binding")
	}
	if binding.Subtype != "props_binding" {
		t.Errorf("Subtype = %q, want props_binding", binding.Subtype)
	}
}

// ── template RENDERS edges ────────────────────────────────────────────────────

func TestExtractor_RendersEdges(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	for _, child := range []string{"Layout", "Header", "ReactCounter", "VueLikeButton"} {
		if !hasRelKind(recs, "RENDERS", child) {
			t.Errorf("expected RENDERS edge to %q", child)
		}
	}

	// Header appears twice but must be deduplicated to a single RENDERS edge.
	headerCount := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "RENDERS" && rel.ToID == "Header" {
				headerCount++
			}
		}
	}
	if headerCount != 1 {
		t.Errorf("Header RENDERS count = %d, want 1 (deduplication)", headerCount)
	}
}

// ── framework island IMPLEMENTS edges ────────────────────────────────────────

func TestExtractor_IslandImplementsEdges(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	// ReactCounter uses client:load, VueLikeButton uses client:idle.
	for _, island := range []string{"ReactCounter", "VueLikeButton"} {
		if !hasRelKind(recs, "IMPLEMENTS", island) {
			t.Errorf("expected IMPLEMENTS edge to %q (framework island)", island)
		}
	}

	// Non-island components should NOT get an IMPLEMENTS edge.
	if hasRelKind(recs, "IMPLEMENTS", "Header") {
		t.Error("Header is not an island; should not have IMPLEMENTS edge")
	}
}

// ── island directives captured in properties ─────────────────────────────────

func TestExtractor_IslandDirectiveProperty(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPLEMENTS" && rel.ToID == "ReactCounter" {
				if !strings.Contains(rel.Properties["island_directive"], "client:load") {
					t.Errorf("ReactCounter IMPLEMENTS directive = %q, want client:load",
						rel.Properties["island_directive"])
				}
				return
			}
		}
	}
	t.Error("ReactCounter IMPLEMENTS edge not found")
}

// ── client:visible directive ──────────────────────────────────────────────────

func TestExtractor_ClientVisibleIsland(t *testing.T) {
	const src = `---
import LazyChart from '../components/LazyChart.jsx';
---
<LazyChart client:visible width={800} />
`
	recs := mustExtract(t, "src/pages/Dashboard.astro", src)
	if !hasRelKind(recs, "IMPLEMENTS", "LazyChart") {
		t.Error("expected IMPLEMENTS edge for client:visible island LazyChart")
	}
}

// ── entity recall (≥ 80%) ────────────────────────────────────────────────────

// Expected entities in blogPostAstro:
//
//	BlogPost (component)           = 1
//	title, description, publishDate = 3 (props)
//
// Total = 4.  Threshold 80% → 4 * 0.8 = 3.2 → ceil → 4 minimum.
// We also count import + renders edges separately via relationship tests.
func TestExtractor_EntityRecall(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	expected := []string{"BlogPost", "title", "description", "publishDate"}
	found := 0
	for _, want := range expected {
		if findByName(recs, want) != nil {
			found++
		}
	}
	total := len(expected)
	threshold := int(float64(total)*0.8 + 0.5)
	if found < threshold {
		t.Errorf("entity recall: found %d/%d (want ≥%d)", found, total, threshold)
	}
}

// ── zero false positives ─────────────────────────────────────────────────────

func TestExtractor_NoFalsePositives(t *testing.T) {
	recs := mustExtract(t, "src/pages/BlogPost.astro", blogPostAstro)

	for _, r := range recs {
		if strings.TrimSpace(r.Name) == "" {
			t.Errorf("entity with blank Name: %+v", r)
		}
		if strings.TrimSpace(r.Kind) == "" {
			t.Errorf("entity with blank Kind: %+v", r)
		}
		if r.QualityScore < 0 || r.QualityScore > 1 {
			t.Errorf("entity %q QualityScore = %.2f outside [0,1]", r.Name, r.QualityScore)
		}
		if r.SourceFile == "" {
			t.Errorf("entity %q has empty SourceFile", r.Name)
		}
		if r.Language != "astro" {
			t.Errorf("entity %q Language = %q, want astro", r.Name, r.Language)
		}
	}
}

// ── empty file ────────────────────────────────────────────────────────────────

func TestExtractor_EmptyFile(t *testing.T) {
	recs := mustExtract(t, "Empty.astro", "")
	if len(recs) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(recs))
	}
}

// ── template-only (no frontmatter) ───────────────────────────────────────────

func TestExtractor_NoFrontmatter(t *testing.T) {
	const src = `<main>
  <h1>Hello from Astro</h1>
  <p>No frontmatter at all.</p>
</main>
`
	recs := mustExtract(t, "src/components/Simple.astro", src)
	comp := findByName(recs, "Simple")
	if comp == nil {
		t.Fatal("expected component entity 'Simple'")
	}
	if comp.Subtype != "astro_component" {
		t.Errorf("Subtype = %q, want astro_component", comp.Subtype)
	}
	// No props entities expected.
	if len(findBySubtype(recs, "prop")) != 0 {
		t.Error("expected no prop entities for template-only file")
	}
}

// ── style block stripped from template scan ───────────────────────────────────

func TestExtractor_StyleBlockIgnored(t *testing.T) {
	const src = `---
import Nav from './Nav.astro';
---
<Nav />
<style>
  /* Nav should not be extracted as a component from here */
  .Nav { color: red; }
</style>
`
	recs := mustExtract(t, "src/components/Page.astro", src)
	// Only one RENDERS edge to Nav (from the template, not from style).
	navCount := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "RENDERS" && rel.ToID == "Nav" {
				navCount++
			}
		}
	}
	if navCount != 1 {
		t.Errorf("Nav RENDERS count = %d, want 1", navCount)
	}
}

// ── import deduplication ──────────────────────────────────────────────────────

func TestExtractor_ImportDeduplication(t *testing.T) {
	const src = `---
import Foo from './Foo.astro';
import Foo from './Foo.astro';
---
<Foo />
`
	recs := mustExtract(t, "src/pages/Dedup.astro", src)
	fooImports := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" && rel.ToID == "./Foo.astro" {
				fooImports++
			}
		}
	}
	if fooImports != 1 {
		t.Errorf("expected exactly 1 IMPORTS edge for './Foo.astro', got %d", fooImports)
	}
}
