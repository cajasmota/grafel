package links

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolverInFile checks that an in-file string literal resolves directly.
func TestResolverInFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/config.ts", `export const API_URL = "https://api.example.com";
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{{
			ID:         "e1",
			Name:       "API_URL",
			Kind:       "SCOPE.Variable",
			SourceFile: "src/config.ts",
		}},
	}}
	r := buildResolver(graphs)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	got := r.Resolve("repo-a", "src/config.ts", "API_URL")
	if got.Value != "https://api.example.com" {
		t.Errorf("Value = %q, want https://api.example.com", got.Value)
	}
	if got.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", got.Confidence)
	}
}

// TestResolverCrossFileJSTS verifies a cross-file IMPORTS walk through a
// re-exported binding (file A imports it from file B).
func TestResolverCrossFileJSTS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/shared.ts", `export const API_URL = "https://api.example.com";
`)
	writeFile(t, root, "src/app.ts", `import { API_URL } from "./shared";
fetch(`+"`"+`${API_URL}/things`+"`"+`);
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "e1", Name: "API_URL", Kind: "SCOPE.Variable", SourceFile: "src/shared.ts"},
			{ID: "e2", Name: "fetch", Kind: "SCOPE.Function", SourceFile: "src/app.ts"},
		},
	}}
	r := buildResolver(graphs)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	got := r.Resolve("repo-a", "src/app.ts", "API_URL")
	if got.Value != "https://api.example.com" {
		t.Errorf("cross-file value = %q, want https://api.example.com (steps=%v)", got.Value, got.Steps)
	}
	if got.Confidence > 0.6 {
		t.Errorf("cross-file confidence = %v, want ≤0.6 (min over chain)", got.Confidence)
	}
}

// TestApplyResolverDynamicBaseURL exercises the consumer-side http rewriter:
// a dynamic_baseurl consumer with a /{apiUrl}/things path should be rewritten
// to /things after substrate substitutes apiUrl → "https://api.example.com".
func TestApplyResolverDynamicBaseURL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/app.ts", `const apiUrl = process.env.VITE_API_URL ?? "https://api.example.com";
fetch(`+"`"+`${apiUrl}/things`+"`"+`);
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{
				ID:         "h1",
				Name:       "GET /{apiUrl}/things",
				Kind:       "http_endpoint_call",
				SourceFile: "src/app.ts",
				Properties: map[string]string{
					"verb":        "GET",
					"path":        "/{apiUrl}/things",
					"url_kind":    "dynamic_baseurl",
					"caller_file": "src/app.ts",
				},
			},
		},
	}}
	r := buildResolver(graphs)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	mutated := applyResolverToConsumerHTTP(graphs, r)
	if mutated != 1 {
		t.Fatalf("mutated = %d, want 1", mutated)
	}
	e := graphs[0].Entities[0]
	if e.Properties["path"] != "/things" {
		t.Errorf("rewritten path = %q, want /things", e.Properties["path"])
	}
	if e.Properties["url_kind"] != "literal" {
		t.Errorf("url_kind = %q, want literal", e.Properties["url_kind"])
	}
	if e.Properties["substrate_resolved_value"] != "https://api.example.com" {
		t.Errorf("substrate_resolved_value missing or wrong: %+v", e.Properties)
	}
}

// TestLeadingTemplateIdent covers the path-parsing helper boundary cases.
func TestLeadingTemplateIdent(t *testing.T) {
	cases := map[string]string{
		"/{apiUrl}/things": "apiUrl",
		"/{apiUrl}":        "apiUrl",
		"/things":          "",
		"/{}/foo":          "",
		"":                 "",
		"/{api-url}/x":     "", // hyphen not an ident char
		"/{a.b}/x":         "", // dot not allowed in leading ident
	}
	for in, want := range cases {
		if got := leadingTemplateIdent(in); got != want {
			t.Errorf("leadingTemplateIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStripURLPrefix covers the URL prefix trimmer.
func TestStripURLPrefix(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/v1": "/v1",
		"http://x.com":               "",
		"/already/path":              "/already/path",
		"nothost":                    "nothost",
	}
	for in, want := range cases {
		if got := stripURLPrefix(in); got != want {
			t.Errorf("stripURLPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBackendSubstrateImportResolutionFixture loads the hand-written
// Express/NestJS-flavoured multi-file fixture from
// internal/extractors/javascript/testdata/substrate_import_resolution/ and
// verifies that the constant-propagation resolver correctly walks the
// cross-file IMPORTS edge for all three named constants:
//   - API_BASE_URL (ProvenanceLiteral in config.ts → resolved in app.ts)
//   - DB_URL (ProvenanceEnvFallback in config.ts → resolved in app.ts)
//   - SERVER_PORT (ProvenanceLiteral in config.ts → resolved in nest_app.ts)
//
// This is the proving fixture for import_resolution_quality across all 12
// backend-HTTP framework records (adonisjs, express, fastify, feathers, hapi,
// hono, koa, marblejs, nestjs, polka, restify, sails) — all share the same
// JS/TS substrate sniffer and constant-propagation pass (#2848).
func TestBackendSubstrateImportResolutionFixture(t *testing.T) {
	// Locate the fixture directory relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is .../internal/links/constant_propagation_test.go
	// fixture lives at .../internal/extractors/javascript/testdata/substrate_import_resolution/
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	fixturePath := filepath.Join(repoRoot, "internal", "extractors", "javascript",
		"testdata", "substrate_import_resolution")
	if _, err := os.Stat(filepath.Join(fixturePath, "config.ts")); err != nil {
		t.Fatalf("fixture not found at %s: %v", fixturePath, err)
	}

	graphs := []repoGraph{{
		Repo:     "repo-backend",
		FileRoot: fixturePath,
		Entities: []entityNode{
			{ID: "c1", Name: "API_BASE_URL", Kind: "SCOPE.Variable", SourceFile: "config.ts"},
			{ID: "c2", Name: "DB_URL", Kind: "SCOPE.Variable", SourceFile: "config.ts"},
			{ID: "c3", Name: "SERVER_PORT", Kind: "SCOPE.Variable", SourceFile: "config.ts"},
			{ID: "a1", Name: "app", Kind: "SCOPE.Variable", SourceFile: "app.ts"},
			{ID: "n1", Name: "bootstrap", Kind: "SCOPE.Function", SourceFile: "nest_app.ts"},
		},
	}}

	r := buildResolver(graphs)
	if r == nil {
		t.Fatal("expected non-nil resolver; substrate found no JS/TS bindings")
	}

	// app.ts imports API_BASE_URL and DB_URL from ./config — both must resolve.
	gotAPIBase := r.Resolve("repo-backend", "app.ts", "API_BASE_URL")
	if gotAPIBase.Value != "https://api.example.com" {
		t.Errorf("app.ts API_BASE_URL resolved to %q, want https://api.example.com (steps=%v)",
			gotAPIBase.Value, gotAPIBase.Steps)
	}
	if gotAPIBase.Confidence <= 0 || gotAPIBase.Confidence > 0.6 {
		t.Errorf("app.ts API_BASE_URL confidence = %v, want >0 and ≤0.6 (cross-file cap)", gotAPIBase.Confidence)
	}
	if len(gotAPIBase.Steps) < 2 {
		t.Errorf("app.ts API_BASE_URL steps = %v, want at least 2 (import hop + literal)", gotAPIBase.Steps)
	}

	gotDBURL := r.Resolve("repo-backend", "app.ts", "DB_URL")
	if gotDBURL.Value != "postgres://localhost:5432/mydb" {
		t.Errorf("app.ts DB_URL resolved to %q, want postgres://localhost:5432/mydb (steps=%v)",
			gotDBURL.Value, gotDBURL.Steps)
	}

	// nest_app.ts imports SERVER_PORT and API_BASE_URL from ./config.
	gotPort := r.Resolve("repo-backend", "nest_app.ts", "SERVER_PORT")
	if gotPort.Value != "3000" {
		t.Errorf("nest_app.ts SERVER_PORT resolved to %q, want 3000 (steps=%v)",
			gotPort.Value, gotPort.Steps)
	}

	gotNestAPIBase := r.Resolve("repo-backend", "nest_app.ts", "API_BASE_URL")
	if gotNestAPIBase.Value != "https://api.example.com" {
		t.Errorf("nest_app.ts API_BASE_URL resolved to %q, want https://api.example.com (steps=%v)",
			gotNestAPIBase.Value, gotNestAPIBase.Steps)
	}
}

// TestPythonCrossFileResolve exercises the dotted-module import path
// (`from package.module import X`) using the dotted-form fileLookup index.
func TestPythonCrossFileResolve(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/settings.py", `API_URL = "https://api.example.com"
`)
	writeFile(t, root, "pkg/client.py", `from pkg.settings import API_URL
def go(): return API_URL
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "e1", Name: "API_URL", Kind: "SCOPE.Variable", SourceFile: "pkg/settings.py"},
			{ID: "e2", Name: "go", Kind: "SCOPE.Function", SourceFile: "pkg/client.py"},
		},
	}}
	r := buildResolver(graphs)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	got := r.Resolve("repo-a", "pkg/client.py", "API_URL")
	if got.Value != "https://api.example.com" {
		t.Errorf("python cross-file value = %q, want https://api.example.com (steps=%v)", got.Value, got.Steps)
	}
}
