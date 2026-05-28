package classifier_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/extractors"
)

// newTestClassifier creates a Classifier backed by the in-repo hermetic YAML
// fixture under testdata/yaml. The fixture contains the minimal set of
// skip_patterns.yaml files needed to exercise the classifier's full code path
// without depending on any external path (e.g. a local clone of the Python
// indexer under /tmp). Tests run identically on CI and dev machines.
func newTestClassifier(t *testing.T) *classifier.Classifier {
	t.Helper()

	dir := filepath.Join("testdata", "yaml")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("hermetic YAML fixture missing at %q: %v", dir, err)
	}
	c, err := classifier.New(dir, noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatalf("New(%q): %v", dir, err)
	}
	return c
}

// ---------------------------------------------------------------------------
// New() — constructor error paths
// ---------------------------------------------------------------------------

func TestNew_MissingDir(t *testing.T) {
	_, err := classifier.New("/nonexistent/path/that/does/not/exist", noop.NewTracerProvider().Tracer("test"))
	if err == nil {
		t.Fatal("expected error for missing yamlDataDir, got nil")
	}
}

func TestNew_FileNotDir(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notadir*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = classifier.New(f.Name(), noop.NewTracerProvider().Tracer("test"))
	if err == nil {
		t.Fatalf("expected error when yamlDataDir is a file, got nil")
	}
}

func TestNew_MalformedYAML_DoesNotAbort(t *testing.T) {
	tmp := t.TempDir()
	langDir := filepath.Join(tmp, "go")
	if err := os.MkdirAll(langDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write garbage YAML — should produce a warning but not abort.
	if err := os.WriteFile(filepath.Join(langDir, "skip_patterns.yaml"), []byte(":::bad yaml:::["), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := classifier.New(tmp, noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatalf("New should not fail on malformed YAML, got: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil classifier")
	}
}

// ---------------------------------------------------------------------------
// ClassifyWithSize — size boundary tests
// ---------------------------------------------------------------------------

func TestClassifyWithSize_ExactlyAtLimit_NotSkipped(t *testing.T) {
	c := newTestClassifier(t)
	const oneMiB = int64(1 * 1024 * 1024)
	r := c.ClassifyWithSize(context.Background(), "main.go", oneMiB)
	if r.Skip {
		t.Errorf("file exactly at 1MiB should NOT be skipped, got Skip=true reason=%q", r.SkipReason)
	}
	if r.Language != "go" {
		t.Errorf("expected Language=go, got %q", r.Language)
	}
}

func TestClassifyWithSize_OneBytePastLimit_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	const oneMiBPlusOne = int64(1*1024*1024 + 1)
	r := c.ClassifyWithSize(context.Background(), "main.go", oneMiBPlusOne)
	if !r.Skip {
		t.Fatal("file 1 byte past 1MiB limit should be skipped")
	}
	if r.SkipReason != "too_large" {
		t.Errorf("expected SkipReason=too_large, got %q", r.SkipReason)
	}
}

// ---------------------------------------------------------------------------
// Classify — happy path language detection
// ---------------------------------------------------------------------------

func TestClassify_GoFile(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "internal/foo/main.go")
	if r.Skip {
		t.Errorf("main.go should not be skipped: reason=%q", r.SkipReason)
	}
	if r.Language != "go" {
		t.Errorf("expected Language=go, got %q", r.Language)
	}
	if r.Tier != 1 {
		t.Errorf("expected Tier=1, got %d", r.Tier)
	}
}

func TestClassify_PythonFile(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "src/app.py")
	if r.Skip {
		t.Errorf("app.py should not be skipped: %q", r.SkipReason)
	}
	if r.Language != "python" {
		t.Errorf("expected python, got %q", r.Language)
	}
}

func TestClassify_AssemblyFiles(t *testing.T) {
	c := newTestClassifier(t)
	// Every assembly dialect/extension collapses to the single "assembly"
	// token. .S is preprocessed gas — case-insensitive matching routes it to
	// the same language as .s (#2744).
	for _, path := range []string{
		"arch/x86/boot.s",
		"arch/arm/head.S",
		"crypto/aes.asm",
		"kernel/entry.nasm",
	} {
		r := c.Classify(context.Background(), path)
		if r.Skip {
			t.Errorf("%s should not be skipped: %q", path, r.SkipReason)
		}
		if r.Language != "assembly" {
			t.Errorf("%s: expected assembly, got %q", path, r.Language)
		}
	}
}

func TestClassify_TypeScriptFile(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "src/components/Button.tsx")
	if r.Skip {
		t.Errorf("Button.tsx should not be skipped: %q", r.SkipReason)
	}
	if r.Language != "typescript" {
		t.Errorf("expected typescript, got %q", r.Language)
	}
}

func TestClassify_RustFile(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "src/main.rs")
	if r.Skip {
		t.Errorf("main.rs should not be skipped: %q", r.SkipReason)
	}
	if r.Language != "rust" {
		t.Errorf("expected rust, got %q", r.Language)
	}
}

func TestClassify_JavaFile(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "src/main/java/App.java")
	if r.Skip {
		t.Errorf("App.java should not be skipped: %q", r.SkipReason)
	}
	if r.Language != "java" {
		t.Errorf("expected java, got %q", r.Language)
	}
}

// ---------------------------------------------------------------------------
// Classify — vendor / dependency directory skips
// ---------------------------------------------------------------------------

func TestClassify_NodeModules_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "node_modules/lodash/lodash.js")
	if !r.Skip {
		t.Fatal("node_modules file should be skipped")
	}
	if r.SkipReason == "" {
		t.Error("SkipReason should not be empty for node_modules")
	}
}

func TestClassify_VendorDir_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "vendor/github.com/foo/bar/bar.go")
	if !r.Skip {
		t.Fatal("vendor/ file should be skipped")
	}
}

func TestClassify_GitDir_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), ".git/config")
	if !r.Skip {
		t.Fatal(".git/ file should be skipped")
	}
}

func TestClassify_Pycache_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "some_project/__pycache__/foo.cpython-311.pyc")
	if !r.Skip {
		t.Fatal("__pycache__ file should be skipped")
	}
}

// ---------------------------------------------------------------------------
// Classify — binary extension skips
// ---------------------------------------------------------------------------

func TestClassify_SoBinary_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "lib/libfoo.so")
	if !r.Skip {
		t.Fatal(".so file should be skipped as binary")
	}
	if r.SkipReason != "binary" {
		t.Errorf("expected SkipReason=binary, got %q", r.SkipReason)
	}
}

func TestClassify_DllBinary_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "bin/foo.dll")
	if !r.Skip {
		t.Fatal(".dll should be skipped as binary")
	}
	if r.SkipReason != "binary" {
		t.Errorf("expected SkipReason=binary, got %q", r.SkipReason)
	}
}

func TestClassify_ExeBinary_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "bin/server.exe")
	if !r.Skip {
		t.Fatal(".exe should be skipped as binary")
	}
	if r.SkipReason != "binary" {
		t.Errorf("expected SkipReason=binary, got %q", r.SkipReason)
	}
}

// ---------------------------------------------------------------------------
// Classify — unsupported extension
// ---------------------------------------------------------------------------

func TestClassify_UnknownExtension_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "file.unknown")
	if !r.Skip {
		t.Fatal("file with unknown extension should be skipped")
	}
	if r.SkipReason != "unsupported_extension" {
		t.Errorf("expected SkipReason=unsupported_extension, got %q", r.SkipReason)
	}
	if r.Language != "" {
		t.Errorf("expected empty Language, got %q", r.Language)
	}
}

func TestClassify_NoExtension_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "Makefile")
	if !r.Skip {
		t.Fatalf("file with no extension and no recognised language should be skipped, got Skip=false language=%q", r.Language)
	}
}

// ---------------------------------------------------------------------------
// Classify — empty filename
// ---------------------------------------------------------------------------

func TestClassify_EmptyFilename_HandledGracefully(t *testing.T) {
	c := newTestClassifier(t)
	r := c.Classify(context.Background(), "")
	// Must not panic. Skip should be true.
	if !r.Skip {
		t.Error("empty filename should be skipped")
	}
}

// ---------------------------------------------------------------------------
// YAML-sourced glob skip patterns (requires reference clone)
// ---------------------------------------------------------------------------

// hermeticYAMLDir returns the path to the in-repo YAML fixture shipped under
// testdata/yaml. It is the hermetic replacement for the Python reference clone
// at /tmp/archigraph-ref and contains the minimal skip_patterns.yaml
// files needed to exercise glob-skip behaviour.
func hermeticYAMLDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "yaml")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("hermetic YAML fixture missing at %q: %v", dir, err)
	}
	return dir
}

func TestClassify_PbGoGenerated_Skipped(t *testing.T) {
	c, err := classifier.New(hermeticYAMLDir(t), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Classify(context.Background(), "internal/proto/service.pb.go")
	if !r.Skip {
		t.Fatal("*.pb.go should be skipped as generated")
	}
}

func TestClassify_WireGenGo_Skipped(t *testing.T) {
	c, err := classifier.New(hermeticYAMLDir(t), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Classify(context.Background(), "internal/di/wire_gen.go")
	if !r.Skip {
		t.Fatal("wire_gen.go should be skipped as generated")
	}
}

// ---------------------------------------------------------------------------
// IsBinaryContent
// ---------------------------------------------------------------------------

func TestIsBinaryContent_WithNullByte(t *testing.T) {
	data := []byte("hello\x00world")
	if !classifier.IsBinaryContent(data) {
		t.Error("expected binary=true for content with null byte")
	}
}

func TestIsBinaryContent_PlainText(t *testing.T) {
	data := []byte("package main\n\nfunc main() {}\n")
	if classifier.IsBinaryContent(data) {
		t.Error("expected binary=false for plain text")
	}
}

func TestIsBinaryContent_EmptySlice(t *testing.T) {
	if classifier.IsBinaryContent([]byte{}) {
		t.Error("expected binary=false for empty slice")
	}
}

// ---------------------------------------------------------------------------
// Extension coverage — spot-check 20+ extensions
// ---------------------------------------------------------------------------

func TestExtensionCoverage(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []struct {
		file string
		lang string
	}{
		{"foo.py", "python"},
		{"foo.go", "go"},
		{"foo.js", "javascript"},
		{"foo.jsx", "javascript"},
		{"foo.ts", "typescript"},
		{"foo.tsx", "typescript"},
		{"foo.java", "java"},
		{"foo.kt", "kotlin"},
		{"foo.rb", "ruby"},
		{"foo.php", "php"},
		{"foo.rs", "rust"},
		{"foo.cs", "csharp"},
		{"foo.swift", "swift"},
		{"foo.dart", "dart"},
		{"foo.scala", "scala"},
		{"foo.c", "c"},
		{"foo.cpp", "cpp"},
		{"foo.sh", "shell"},
		{"foo.ex", "elixir"},
		{"foo.zig", "zig"},
		{"foo.lua", "lua"},
		{"foo.sql", "sql"},
		{"foo.tf", "terraform"},
		{"foo.hcl", "hcl"},
		{"foo.proto", "protobuf"},
		{"foo.graphql", "graphql"},
		{"foo.prisma", "prisma"},
		{"foo.hs", "haskell"},
		{"foo.pony", "pony"},
		{"foo.idr", "idris"},
		{"foo.pl", "perl"},
		{"foo.res", "rescript"},
		{"foo.resi", "rescript"},
		{"foo.sol", "solidity"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := c.Classify(ctx, tc.file)
			if r.Language != tc.lang {
				t.Errorf("file=%q: expected Language=%q, got %q", tc.file, tc.lang, r.Language)
			}
			if r.Skip {
				t.Errorf("file=%q: expected Skip=false, got true reason=%q", tc.file, r.SkipReason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// YAML / TOML / Dockerfile / HTML extension-map fixes
// ---------------------------------------------------------------------------

// TestMX1100_YAML verifies that .yaml and .yml route to the "yaml" language
// token (matching extractor.Register("yaml", …)) and are not silently dropped.
func TestMX1100_YAML(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []struct {
		file string
	}{
		{"config.yaml"},
		{"config.yml"},
		{"k8s/deployment.yaml"},
		{"docker-compose.yml"},
		{"path/to/.github/workflows/ci.yaml"},
		{"path/to/.github/workflows/ci.yml"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := c.Classify(ctx, tc.file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, got Skip=true reason=%q", tc.file, r.SkipReason)
			}
			if r.Language != "yaml" {
				t.Errorf("file=%q: expected Language=yaml, got %q", tc.file, r.Language)
			}
			if r.Tier != 1 {
				t.Errorf("file=%q: expected Tier=1, got %d", tc.file, r.Tier)
			}
		})
	}
}

// TestMX1100_YAML_ClassifyWithSize verifies .yaml / .yml with size path.
func TestMX1100_YAML_ClassifyWithSize(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	const smallFile = int64(1024)

	for _, file := range []string{"config.yaml", "config.yml"} {
		t.Run(file, func(t *testing.T) {
			r := c.ClassifyWithSize(ctx, file, smallFile)
			if r.Skip {
				t.Errorf("%s: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "yaml" {
				t.Errorf("%s: expected Language=yaml, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_TOML verifies that .toml routes to the "toml" language token and
// is not silently dropped by the classifier.
func TestMX1100_TOML(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []string{
		"Cargo.toml",
		"pyproject.toml",
		"config.toml",
		"path/to/Cargo.toml",
	}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("%s: should NOT be skipped, got Skip=true reason=%q", file, r.SkipReason)
			}
			if r.Language != "toml" {
				t.Errorf("%s: expected Language=toml, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_Dockerfile verifies that bare "Dockerfile" and "Containerfile"
// basenames route to "dockerfile" via the basename map.
func TestMX1100_Dockerfile(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []struct {
		file string
	}{
		{"Dockerfile"},
		{"Containerfile"},
		{"services/api/Dockerfile"},
		{"infra/Containerfile"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := c.Classify(ctx, tc.file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, got Skip=true reason=%q", tc.file, r.SkipReason)
			}
			if r.Language != "dockerfile" {
				t.Errorf("file=%q: expected Language=dockerfile, got %q", tc.file, r.Language)
			}
			if r.Tier != 1 {
				t.Errorf("file=%q: expected Tier=1, got %d", tc.file, r.Tier)
			}
		})
	}
}

// TestMX1100_Dockerfile_ClassifyWithSize verifies Dockerfile with size path.
func TestMX1100_Dockerfile_ClassifyWithSize(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	const smallFile = int64(512)

	for _, file := range []string{"Dockerfile", "services/api/Dockerfile", "Containerfile"} {
		t.Run(file, func(t *testing.T) {
			r := c.ClassifyWithSize(ctx, file, smallFile)
			if r.Skip {
				t.Errorf("%s: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "dockerfile" {
				t.Errorf("%s: expected Language=dockerfile, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_Vue_LanguageToken verifies that .vue maps to the "vue" runtime
// extractor-dispatch token (the dedicated Vue SFC extractor, not the generic
// "html" extractor). Note: "vue" is a JS/TS framework, not a coverage language
// — on the coverage by-language axis it collapses into jsts (#2821). This token
// is purely the dispatch key that routes .vue files to the SFC extractor.
func TestMX1100_Vue_LanguageToken(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	for _, file := range []string{"src/App.vue", "components/MyButton.vue"} {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, got Skip=true reason=%q", file, r.SkipReason)
			}
			if r.Language != "vue" {
				t.Errorf("file=%q: expected Language=vue, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_HTML_LanguageToken verifies that .html and .htm map to "html"
// (matching extractor.Register("html", …)) — not the old "html_templates" token.
func TestMX1100_HTML_LanguageToken(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []struct {
		file string
	}{
		{"index.html"},
		{"page.htm"},
		{"templates/base.html"},
		{"view.erb"},
		{"template.ejs"},
		{"layout.hbs"},
		{"layout.handlebars"},
		{"template.j2"},
		{"template.jinja"},
		{"template.jinja2"},
		{"view.pug"},
		{"template.njk"},
		{"template.mustache"},
		{"template.twig"},
		{"view.haml"},
		{"view.slim"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := c.Classify(ctx, tc.file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, got Skip=true reason=%q", tc.file, r.SkipReason)
			}
			if r.Language != "html" {
				t.Errorf("file=%q: expected Language=html (not html_templates), got %q", tc.file, r.Language)
			}
		})
	}
}

// TestAstroLanguageToken verifies that .astro files map to the "astro" runtime
// extractor-dispatch token (matching extractor.Register("astro", …)). Note:
// "astro" is a JS/TS framework, not a coverage language — on the coverage
// by-language axis it collapses into jsts (#2821). This token only routes
// .astro files to the dedicated SFC extractor.
func TestAstroLanguageToken(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	for _, file := range []string{
		"page.astro",
		"src/pages/index.astro",
		"src/components/Header.astro",
	} {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "astro" {
				t.Errorf("file=%q: expected Language=astro, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_HTML_ClassifyWithSize verifies HTML token via the size path.
func TestMX1100_HTML_ClassifyWithSize(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	const smallFile = int64(2048)

	for _, file := range []string{"index.html", "page.htm"} {
		t.Run(file, func(t *testing.T) {
			r := c.ClassifyWithSize(ctx, file, smallFile)
			if r.Skip {
				t.Errorf("%s: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "html" {
				t.Errorf("%s: expected Language=html, got %q", file, r.Language)
			}
		})
	}
}

// TestSvelte_LanguageToken verifies that .svelte files are classified with the
// "svelte" runtime extractor-dispatch token (not "html") so the dedicated
// svelte SFC extractor is invoked. Note: "svelte" is a JS/TS framework, not a
// coverage language — on the coverage by-language axis it collapses into jsts
// (#2821). This token only routes .svelte files to the SFC extractor.
func TestSvelte_LanguageToken(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	for _, file := range []string{"src/App.svelte", "lib/Button.svelte", "routes/+page.svelte"} {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, got Skip=true reason=%q", file, r.SkipReason)
			}
			if r.Language != "svelte" {
				t.Errorf("file=%q: expected Language=svelte, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1100_Vue_ClassifyWithSize verifies that .vue maps to "vue" via the size path.
func TestMX1100_Vue_ClassifyWithSize(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	const smallFile = int64(2048)
	r := c.ClassifyWithSize(ctx, "App.vue", smallFile)
	if r.Skip {
		t.Errorf("App.vue: should NOT be skipped, reason=%q", r.SkipReason)
	}
	if r.Language != "vue" {
		t.Errorf("App.vue: expected Language=vue, got %q", r.Language)
	}
}

// TestMX1100_DockerfileVariants_NotConfusedWithExtension verifies that files
// that merely have "Dockerfile" in their name but also have an extension are
// NOT routed as dockerfile (e.g. Dockerfile.bak → unknown, not dockerfile).
func TestMX1100_DockerfileWithExtension_NotDockerfile(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	// Dockerfile.bak has extension ".bak" — not in any map — should be skipped
	// as unsupported_extension, NOT classified as dockerfile.
	r := c.Classify(ctx, "Dockerfile.bak")
	if r.Language == "dockerfile" {
		t.Error("Dockerfile.bak should NOT be classified as dockerfile — it has an extension")
	}
}

// ---------------------------------------------------------------------------
// ClassifyWithSize — additional branch coverage
// ---------------------------------------------------------------------------

func TestClassifyWithSize_EmptyFilename(t *testing.T) {
	c := newTestClassifier(t)
	r := c.ClassifyWithSize(context.Background(), "", 100)
	if !r.Skip {
		t.Error("empty filename should be skipped")
	}
	if r.SkipReason != "empty_path" {
		t.Errorf("expected SkipReason=empty_path, got %q", r.SkipReason)
	}
}

func TestClassifyWithSize_VendorDir_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.ClassifyWithSize(context.Background(), "vendor/github.com/foo/bar.go", 512)
	if !r.Skip {
		t.Fatal("vendor/ file should be skipped via ClassifyWithSize")
	}
}

func TestClassifyWithSize_BinaryExtension_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.ClassifyWithSize(context.Background(), "lib/foo.so", 1024)
	if !r.Skip {
		t.Fatal(".so file should be skipped as binary via ClassifyWithSize")
	}
	if r.SkipReason != "binary" {
		t.Errorf("expected SkipReason=binary, got %q", r.SkipReason)
	}
}

func TestClassifyWithSize_UnknownExtension_Skipped(t *testing.T) {
	c := newTestClassifier(t)
	r := c.ClassifyWithSize(context.Background(), "data.unknown", 256)
	if !r.Skip {
		t.Fatal("unknown extension should be skipped via ClassifyWithSize")
	}
	if r.SkipReason != "unsupported_extension" {
		t.Errorf("expected SkipReason=unsupported_extension, got %q", r.SkipReason)
	}
}

// TestIsBinaryContent_LargeContent verifies the probe truncation at 512 bytes.
func TestIsBinaryContent_LargeContent(t *testing.T) {
	// Build a 600-byte slice: first 512 bytes are clean text, null byte at 513.
	data := make([]byte, 600)
	for i := range data {
		data[i] = 'a'
	}
	data[512] = 0x00 // null byte beyond the probe window

	if classifier.IsBinaryContent(data) {
		t.Error("null byte beyond 512-byte probe window should NOT trigger binary detection")
	}
}

// ---------------------------------------------------------------------------
// Classifier contract tests: extractor registry ↔ classifier routing
// ---------------------------------------------------------------------------

// classifierRepresentativeInputs is the single source of truth mapping each
// classifier language token (that the pipeline must route end-to-end) to a
// representative file path. Every entry encodes a bidirectional contract:
//
//  1. (forward) the classifier must produce that token for the given path.
//  2. (reverse) a registered extractor must exist for the token the classifier
//     produces.
//
// Languages that the classifier recognises but for which no extractor exists
// yet (objective_c, haskell, perl, r, markdown, prisma, toml) are intentionally
// absent from this table — they are tracked as known gaps in knownMismatches
// below. Add an entry here ONLY when both a classifier rule AND a registered
// extractor exist for the language.
var classifierRepresentativeInputs = map[string]string{
	// Core compiled / scripted languages — all fully wired.
	"python":     "src/app.py",
	"go":         "internal/server.go",
	"javascript": "src/index.js",
	"typescript": "src/types.ts",
	"java":       "src/Main.java",
	"kotlin":     "src/Main.kt",
	"ruby":       "lib/app.rb",
	"php":        "src/index.php",
	"rust":       "src/main.rs",
	"csharp":     "src/Program.cs",
	"razor":      "Pages/Index.razor",
	"swift":      "Sources/App.swift",
	"dart":       "lib/main.dart",
	"scala":      "src/Main.scala",
	"shell":      "scripts/deploy.sh",
	"elixir":     "lib/app.ex",
	"groovy":     "src/Main.groovy",
	"clojure":    "src/core.clj",
	"zig":        "src/main.zig",
	"lua":        "src/main.lua",
	"sql":        "migrations/001_init.sql",
	"hcl":        "infra/backend.hcl",
	"terraform":  "infra/main.tf",
	"css":        "styles/main.css",
	"html":       "templates/index.html",
	"yaml":       "config/app.yaml",
	"graphql":    "schema/schema.graphql",
	"dockerfile": "Dockerfile",
	"fish":       "config.fish",
	"just":       "Justfile",
	// C / C++ — fully wired via the cpp extractor which registers both tokens
	// under "c" and "cpp" (added the extractor; wired it into
	// registry_gen.go so the init() fires at import time).
	"c":        "src/main.c",
	"cpp":      "src/main.cpp",
	"solidity": "contracts/TokenMint.sol",
	// Niche research languages — fully wired extractor + classifier.
	"pony":  "src/main.pony",
	"idris": "src/Main.idr",
}

// knownMismatches documents classifier ↔ extractor gaps that exist on the
// current main branch and require a follow-up fix before they can be added to
// classifierRepresentativeInputs. Each entry describes the gap so failures
// produce actionable output.
//
// When a gap is fixed: remove it from knownMismatches, add a row to
// classifierRepresentativeInputs, and verify both contract tests still pass.
var knownMismatches = map[string]string{
	// Classifier produces "protobuf" for .proto files, but the extractor
	// registers as "proto". Fix: align classifier token to "proto" or rename
	// the extractor to "protobuf".
	"protobuf": "proto/service.proto",
	// Languages recognised by the classifier but with no extractor yet:
	"objective_c": "src/AppDelegate.m",
	"haskell":     "src/Main.hs",
	"perl":        "scripts/build.pl",
	"r":           "analysis/model.r",
	"markdown":    "docs/README.md",
	"prisma":      "prisma/schema.prisma",
	"toml":        "Cargo.toml",
}

// isCoreLanguageToken returns true when lang is a fully-wired core classifier
// token (present in classifierRepresentativeInputs). Custom/framework extractor
// tokens (custom_go_gin, python_django, etc.) and known-gap tokens are excluded.
func isCoreLanguageToken(lang string) bool {
	_, ok := classifierRepresentativeInputs[lang]
	return ok
}

// TestClassifier_EveryRegisteredExtractorHasRoutedExtension verifies that
// every fully-wired core language extractor in the global registry has at
// least one classifier routing rule (extension or basename) that produces its
// token.
//
// Failure means a registered extractor is unreachable from the classifier
// pipeline — files of that language will receive ErrNoExtractorForLanguage at
// runtime. Failures name the broken extractor and the input that was tried.
//
// Known gaps (proto/terraform/c/etc.) are tracked in knownMismatches and do
// not cause failures here — they are reported as t.Log entries until fixed.
func TestClassifier_EveryRegisteredExtractorHasRoutedExtension(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	allLangs := extractors.List()

	var newMismatches []string
	var checked int

	for _, lang := range allLangs {
		if !isCoreLanguageToken(lang) {
			// Known gap or custom/framework extractor — log, don't fail.
			if _, isKnown := knownMismatches[lang]; isKnown {
				t.Logf("KNOWN GAP (forward): extractor %q has no classifier routing rule — see knownMismatches", lang)
			}
			// else: custom_* / python_* / etc. — silently skip (not classifier-routed)
			continue
		}
		checked++

		repInput := classifierRepresentativeInputs[lang]
		result := c.Classify(ctx, repInput)
		if result.Language != lang {
			newMismatches = append(newMismatches, lang+
				": classifier returned \""+result.Language+
				"\" for representative input \""+repInput+"\""+
				" — add a classifier rule that produces \""+lang+"\"")
		}
	}

	if len(newMismatches) > 0 {
		t.Errorf("%s FAILED — %d/%d registered core extractor(s) have no working classifier route:\n  - %s",
			t.Name(), len(newMismatches), checked,
			strings.Join(newMismatches, "\n  - "))
	} else {
		t.Logf("PASS: %d registered core extractor(s) each have ≥1 classifier routing rule", checked)
	}
}

// TestClassifier_EveryRoutedExtensionHasRegisteredExtractor verifies that
// every language token produced by the classifier for a known input has a
// registered extractor. This is the forward-direction of the bug
// class: classifier produces token X → extractors.Get(X) must succeed.
//
// The table classifierRepresentativeInputs defines the verified set. Any new
// classifier token added without a corresponding registered extractor will
// surface here on the next test run.
//
// Known gaps (protobuf, c, etc.) are tracked in knownMismatches and logged
// without causing a test failure until a fix is committed.
func TestClassifier_EveryRoutedExtensionHasRegisteredExtractor(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	var newMismatches []string

	// Check fully-wired entries — these must all pass.
	for lang, repInput := range classifierRepresentativeInputs {
		result := c.Classify(ctx, repInput)
		if result.Skip {
			t.Errorf("representative input %q for language %q was unexpectedly skipped (reason: %q) — fix classifierRepresentativeInputs",
				repInput, lang, result.SkipReason)
			continue
		}
		if result.Language == "" {
			t.Errorf("representative input %q for language %q produced empty Language — fix classifierRepresentativeInputs",
				repInput, lang)
			continue
		}
		_, ok := extractors.Get(result.Language)
		if !ok {
			newMismatches = append(newMismatches,
				result.Language+": classifier produces \""+result.Language+
					"\" for \""+repInput+
					"\" but no extractor is registered — add to knownMismatches or register an extractor")
		}
	}

	// Log known gaps without failing.
	for lang, repInput := range knownMismatches {
		result := c.Classify(ctx, repInput)
		if result.Skip || result.Language == "" {
			// Path was skipped or produced no token — gap is a missing classifier
			// rule rather than a missing extractor.
			t.Logf("KNOWN GAP (reverse): %q has no classifier rule producing it (input %q skipped/empty)", lang, repInput)
			continue
		}
		_, ok := extractors.Get(result.Language)
		if !ok {
			t.Logf("KNOWN GAP (reverse): classifier produces %q for %q but no extractor registered — tracked in knownMismatches",
				result.Language, repInput)
		}
	}

	if len(newMismatches) > 0 {
		t.Errorf("%s FAILED — %d language token(s) produced by the classifier have no registered extractor:\n  - %s",
			t.Name(), len(newMismatches),
			strings.Join(newMismatches, "\n  - "))
	} else {
		t.Logf("PASS: every classifier language token in the contract table has a registered extractor (%d entries verified)",
			len(classifierRepresentativeInputs))
	}
}

// ---------------------------------------------------------------------------
// Fish shell and Justfile routing
// ---------------------------------------------------------------------------

// TestMX1058_Fish verifies that .fish files route to the new "fish" language
// (previously grouped with "shell") so the fish-specific extractor can run.
func TestMX1058_Fish(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []string{
		"config.fish",
		"functions/fish_prompt.fish",
		"completions/kubectl.fish",
	}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "fish" {
				t.Errorf("file=%q: expected Language=fish, got %q", file, r.Language)
			}
			if r.Tier != 1 {
				t.Errorf("file=%q: expected Tier=1, got %d", file, r.Tier)
			}
		})
	}
}

// TestMX1058_Just_Basename verifies that bare "Justfile" / "justfile" route
// to the "just" language via the basename map.
func TestMX1058_Just_Basename(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []string{
		"Justfile",
		"justfile",
		".justfile",
		"services/api/Justfile",
		"services/api/justfile",
	}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "just" {
				t.Errorf("file=%q: expected Language=just, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1058_Just_Extension verifies that *.just files route to the "just"
// language via the extension map (used by companion files like
// included.just for `import` / `!include` directives).
func TestMX1058_Just_Extension(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []string{"shared.just", "include/common.just"}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q: should NOT be skipped, reason=%q", file, r.SkipReason)
			}
			if r.Language != "just" {
				t.Errorf("file=%q: expected Language=just, got %q", file, r.Language)
			}
		})
	}
}

// TestMX1058_ShellStillRoutes verifies that non-fish shell files still
// route to "shell" after we peeled .fish off.
func TestMX1058_ShellStillRoutes(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	for _, file := range []string{"deploy.sh", "lib.bash", "setup.zsh", "profile.ksh"} {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Language != "shell" {
				t.Errorf("file=%q: expected Language=shell, got %q", file, r.Language)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// C / C++ routing + extractor registration
// ---------------------------------------------------------------------------

// TestMX1122_CppRoutesToCppExtractor verifies that every C/C++ source and
// header extension handled by the classifier routes to a registered extractor
// (the cpp extractor, which registers itself under both "c" and "cpp").
//
// Before the cpp extractor package existed on disk but was missing
// from internal/extractors/registry_gen.go, so its init() never fired and
// these files fell through to the no-op path. This test locks that fix in:
// if somebody drops the cpp import again, the extractors.Get check below
// fails and the regression is caught at CI time.
func TestMX1122_CppRoutesToCppExtractor(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []struct {
		file     string
		wantLang string
	}{
		// C — header and implementation.
		{"src/main.c", "c"},
		{"include/api.h", "c"},
		// C++ — all canonical extensions.
		{"src/main.cpp", "cpp"},
		{"src/widget.cc", "cpp"},
		{"src/widget.cxx", "cpp"},
		{"include/widget.hpp", "cpp"},
		{"include/widget.hxx", "cpp"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := c.Classify(ctx, tc.file)
			if r.Skip {
				t.Fatalf("file=%q: should NOT be skipped, reason=%q", tc.file, r.SkipReason)
			}
			if r.Language != tc.wantLang {
				t.Fatalf("file=%q: expected Language=%q, got %q", tc.file, tc.wantLang, r.Language)
			}
			if r.Tier != 1 {
				t.Errorf("file=%q: expected Tier=1, got %d", tc.file, r.Tier)
			}

			// Contract: the classifier token must resolve to a registered
			// extractor — i.e. the cpp package's init() actually fired.
			// This is the regression guard.
			ex, ok := extractors.Get(r.Language)
			if !ok {
				t.Fatalf("file=%q: classifier produced Language=%q but no extractor is registered "+
					"— cpp extractor likely missing from registry_gen.go",
					tc.file, r.Language)
			}
			if ex.Language() != tc.wantLang {
				t.Errorf("file=%q: extractor.Language()=%q, want %q",
					tc.file, ex.Language(), tc.wantLang)
			}
		})
	}
}

// TestClassify_PackageSwift_IsSwiftPackage verifies that "Package.swift" is
// classified as "swift_package" (not the generic "swift"), and that the
// registered swift_package extractor is reachable via the classifier's output
// token. Issue #497.
func TestClassify_PackageSwift_IsSwiftPackage(t *testing.T) {
	c := newTestClassifier(t)
	ctx := context.Background()

	cases := []string{
		"Package.swift",
		"Sources/MyApp/Package.swift",
	}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			r := c.Classify(ctx, file)
			if r.Skip {
				t.Errorf("file=%q should NOT be skipped", file)
			}
			if r.Language != "swift_package" {
				t.Errorf("file=%q: Language=%q, want swift_package", file, r.Language)
			}
			ex, ok := extractors.Get(r.Language)
			if !ok {
				t.Fatalf("file=%q: no extractor registered for %q", file, r.Language)
			}
			if ex.Language() != "swift_package" {
				t.Errorf("extractor.Language()=%q want swift_package", ex.Language())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Issue #1708 — narrow JSON routing for Debezium / Kafka-Connect connectors
// ---------------------------------------------------------------------------
// The classifier must route ONLY path-narrow JSON files (cdc/, debezium/,
// kafka-connect/, *-connector.json, etc.) to language="json" so the
// Debezium CDC engine pass sees them. Indexing all .json files would
// balloon scope across package.json / tsconfig.json / lockfiles.

func TestClassify_DebeziumConnectorJSON(t *testing.T) {
	c := newTestClassifier(t)

	cdcPaths := []string{
		"services/cdc/orders-connector.json",
		"services/cdc/users.json",
		"infra/debezium/billing.json",
		"infra/kafka-connect/inventory.json",
		"infra/connectors/payments.json",
		"connectors/orders-connector.json",
		"connectors/orders.connector.json",
		"deploy/orders-debezium.json",
	}
	for _, p := range cdcPaths {
		t.Run(p, func(t *testing.T) {
			r := c.Classify(context.Background(), p)
			if r.Skip {
				t.Errorf("%s should not be skipped: %q", p, r.SkipReason)
			}
			if r.Language != "json" {
				t.Errorf("expected Language=json, got %q", r.Language)
			}
		})
	}
}

func TestClassify_GenericJSONNotIndexed(t *testing.T) {
	c := newTestClassifier(t)
	// These must NOT be picked up — they would balloon indexing scope and
	// the Debezium pass would no-op on them anyway via the content sniff.
	noisyPaths := []string{
		"package.json",
		"tsconfig.json",
		"jest.config.json",
		"src/data/users.json",
		"frontend/public/manifest.json",
	}
	for _, p := range noisyPaths {
		t.Run(p, func(t *testing.T) {
			r := c.Classify(context.Background(), p)
			if r.Language != "" {
				t.Errorf("%s should have empty Language to skip Pass1, got %q", p, r.Language)
			}
		})
	}
}
