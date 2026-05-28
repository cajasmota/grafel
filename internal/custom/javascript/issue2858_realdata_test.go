package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Blank imports register the base SFC extractors used by the meta-framework
	// corpus (.astro) alongside the custom_js_* set.
	_ "github.com/cajasmota/archigraph/internal/extractors/astro"
)

// issue2858_realdata_test.go — real-data verification for the meta-framework
// Server / Data Flow / Build / Lifecycle cells (#2858). Runs the registered
// extractors over a realistic, multi-file, manifest-free meta-framework corpus
// under testdata/fixtures/real-world/meta-framework/ — the same dispatch the
// indexer uses (base SFC extractor + the custom_js_* framework set) — and
// asserts the new capability markers fire on real-shaped source.

// runFrameworkExtractor reads a corpus file and runs the named extractor over
// it, returning the emitted entities. Skips the test if the corpus is absent.
func runFrameworkExtractor(t *testing.T, extractorName, rel, lang string) []types.EntityRecord {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "meta-framework", rel)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("corpus file %s not present: %v", rel, err)
	}
	e, ok := extreg.Get(extractorName)
	if !ok {
		t.Fatalf("extractor %q not registered", extractorName)
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: path, Language: lang, Content: content})
	if err != nil {
		t.Fatalf("extract %s: %v", rel, err)
	}
	return ents
}

func realHasSubtype(ents []types.EntityRecord, subtype string) bool {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return true
		}
	}
	return false
}

// assertServerBuildCells asserts the four shared meta-fw cells are proved by the
// emitted entities. want maps capability → whether it must be present.
func assertServerBuildCells(t *testing.T, framework string, ents []types.EntityRecord, server, hydration, loader, staticGen bool) {
	t.Helper()
	if server && !(realHasSubtype(ents, "server_component") || realHasSubtype(ents, "server_boundary")) {
		t.Errorf("%s: missing server_components marker", framework)
	}
	if hydration && !realHasSubtype(ents, "client_boundary") {
		t.Errorf("%s: missing hydration_boundaries marker", framework)
	}
	if loader && !realHasSubtype(ents, "data_loader") {
		t.Errorf("%s: missing data_loaders marker", framework)
	}
	if staticGen && !realHasSubtype(ents, "static_generation") {
		t.Errorf("%s: missing static_generation marker", framework)
	}
}

func TestIssue2858_RealData_Next(t *testing.T) {
	page := runFrameworkExtractor(t, "custom_js_nextjs",
		"next-app/app/blog/[slug]/page.tsx", "typescript")
	// Server component (RSC default), data loaders (generateStaticParams/
	// generateMetadata), static generation (revalidate + generateStaticParams).
	assertServerBuildCells(t, "next(page)", page, true, false, true, true)

	client := runFrameworkExtractor(t, "custom_js_nextjs",
		"next-app/app/blog/[slug]/CommentBox.tsx", "typescript")
	if !realHasSubtype(client, "client_boundary") {
		t.Error("next(client): missing 'use client' hydration boundary")
	}
}

func TestIssue2858_RealData_Remix(t *testing.T) {
	route := runFrameworkExtractor(t, "custom_js_remix",
		"remix-app/app/routes/posts.$id.tsx", "typescript")
	assertServerBuildCells(t, "remix(route)", route, true, false, true, false)

	srv := runFrameworkExtractor(t, "custom_js_remix",
		"remix-app/app/utils/session.server.ts", "typescript")
	if !realHasSubtype(srv, "server_boundary") {
		t.Error("remix(.server): missing server boundary")
	}
	cfg := runFrameworkExtractor(t, "custom_js_remix", "remix-app/vite.config.ts", "typescript")
	if !realHasSubtype(cfg, "static_generation") {
		t.Error("remix(vite): missing static_generation (ssr:false)")
	}
}

func TestIssue2858_RealData_Gatsby(t *testing.T) {
	page := runFrameworkExtractor(t, "custom_js_gatsby",
		"gatsby-app/src/pages/blog.tsx", "typescript")
	assertServerBuildCells(t, "gatsby", page, true, true, true, true)
}

func TestIssue2858_RealData_Nuxt(t *testing.T) {
	page := runFrameworkExtractor(t, "custom_js_nuxt", "nuxt-app/pages/users.vue", "typescript")
	assertServerBuildCells(t, "nuxt(page)", page, false, true, true, false)

	srv := runFrameworkExtractor(t, "custom_js_nuxt", "nuxt-app/server/api/users.get.ts", "typescript")
	if !realHasSubtype(srv, "server_boundary") {
		t.Error("nuxt(server): missing server boundary")
	}
	cfg := runFrameworkExtractor(t, "custom_js_nuxt", "nuxt-app/nuxt.config.ts", "typescript")
	if !realHasSubtype(cfg, "static_generation") {
		t.Error("nuxt(config): missing static_generation (prerender route rules)")
	}
}

func TestIssue2858_RealData_SvelteKit(t *testing.T) {
	srv := runFrameworkExtractor(t, "custom_js_svelte",
		"sveltekit-app/src/routes/post/[id]/+page.server.ts", "typescript")
	assertServerBuildCells(t, "sveltekit(server)", srv, true, false, true, false)

	page := runFrameworkExtractor(t, "custom_js_svelte",
		"sveltekit-app/src/routes/post/[id]/+page.svelte", "typescript")
	assertServerBuildCells(t, "sveltekit(page)", page, false, true, false, true)
}

func TestIssue2858_RealData_Astro(t *testing.T) {
	ents := runFrameworkExtractor(t, "astro",
		"astro-app/src/pages/blog/[slug].astro", "astro")
	assertServerBuildCells(t, "astro", ents, true, true, true, true)
}
