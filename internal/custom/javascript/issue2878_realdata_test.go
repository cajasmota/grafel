package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the base .astro SFC extractor used for the Astro corpus file.
	_ "github.com/cajasmota/archigraph/internal/extractors/astro"
)

// issue2878_realdata_test.go — real-data verification for the meta-framework
// framework_specific *idiom* cells (#2878). Runs the registered extractors over
// the realistic, multi-file, manifest-free corpus under
// testdata/fixtures/real-world/meta-framework/ — the same dispatch the indexer
// uses — and asserts the new idiom markers fire on real-shaped source.

func runIdiomExtractor(t *testing.T, extractorName, rel, lang string) []types.EntityRecord {
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

func idiomHasSubtype(ents []types.EntityRecord, subtype string) bool {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return true
		}
	}
	return false
}

func idiomHasName(ents []types.EntityRecord, name string) bool {
	for i := range ents {
		if ents[i].Name == name {
			return true
		}
	}
	return false
}

func TestIssue2878RealData_Next(t *testing.T) {
	// 'use client' directive boundary on the App-Router client component.
	client := runIdiomExtractor(t, "custom_js_nextjs", "next-app/app/blog/[slug]/CommentBox.tsx", "typescript")
	if !idiomHasName(client, "use client") {
		t.Error("real-data: expected 'use client' directive (use_client_server_directive)")
	}
	// 'use server' module with server actions.
	actions := runIdiomExtractor(t, "custom_js_nextjs", "next-app/app/blog/[slug]/actions.ts", "typescript")
	if !idiomHasName(actions, "use server") {
		t.Error("real-data: expected 'use server' directive (use_client_server_directive)")
	}
	if !idiomHasSubtype(actions, "server_action") {
		t.Error("real-data: expected server_action (server_actions)")
	}
	// middleware runtime detection.
	mw := runIdiomExtractor(t, "custom_js_nextjs", "next-app/middleware.ts", "typescript")
	if !idiomHasSubtype(mw, "middleware") {
		t.Error("real-data: expected middleware marker (middleware_runtime_detection)")
	}
	// next.config detection.
	cfg := runIdiomExtractor(t, "custom_js_nextjs", "next-app/next.config.ts", "typescript")
	if !idiomHasSubtype(cfg, "framework_config") {
		t.Error("real-data: expected next.config framework_config (next_config_detection)")
	}
}

func TestIssue2878RealData_Nuxt(t *testing.T) {
	page := runIdiomExtractor(t, "custom_js_nuxt", "nuxt-app/pages/users.vue", "typescript")
	if !idiomHasSubtype(page, "auto_import") {
		t.Error("real-data: expected auto_import marker for useRoute/useState (nuxt_auto_import)")
	}
	srv := runIdiomExtractor(t, "custom_js_nuxt", "nuxt-app/server/api/users.get.ts", "typescript")
	if !idiomHasSubtype(srv, "server_route") {
		t.Error("real-data: expected server_route marker (nuxt_server_routes)")
	}
}

func TestIssue2878RealData_Remix(t *testing.T) {
	route := runIdiomExtractor(t, "custom_js_remix", "remix-app/app/routes/posts.$id.tsx", "typescript")
	if !idiomHasSubtype(route, "loader_action_pair") {
		t.Error("real-data: expected loader_action_pair marker (remix_loader_action_pair)")
	}
}

func TestIssue2878RealData_SvelteKit(t *testing.T) {
	load := runIdiomExtractor(t, "custom_js_svelte", "sveltekit-app/src/routes/post/[id]/+page.server.ts", "typescript")
	if !idiomHasSubtype(load, "data_loader") {
		t.Error("real-data: expected SvelteKit load() data_loader (sveltekit_load_function)")
	}
}

func TestIssue2878RealData_Gatsby(t *testing.T) {
	page := runIdiomExtractor(t, "custom_js_gatsby", "gatsby-app/src/pages/blog.tsx", "typescript")
	if !idiomHasName(page, "pageQuery") {
		t.Error("real-data: expected pageQuery data_loader (gatsby_graphql_pagequery)")
	}
}
