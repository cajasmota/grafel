package walk

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// IgnoreFile pattern matching tests
// --------------------------------------------------------------------------

func writeIgnoreFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestIgnoreFile_BasicPatterns(t *testing.T) {
	dir := t.TempDir()
	p := writeIgnoreFile(t, dir, ".gitignore", `
# comment
node_modules
dist/
build
*.egg-info
`)
	ig, err := ParseIgnoreFile("", p, ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile: %v", err)
	}

	cases := []struct {
		relPath string
		want    bool
	}{
		{"node_modules", true},
		{"dist", true},
		{"build", true},
		{"src", false},
		{"app", false},
	}
	for _, tc := range cases {
		skip, line := ig.MatchDir(tc.relPath)
		if skip != tc.want {
			t.Errorf("MatchDir(%q) skip=%v line=%d want skip=%v", tc.relPath, skip, line, tc.want)
		}
	}
}

func TestIgnoreFile_Negation(t *testing.T) {
	dir := t.TempDir()
	p := writeIgnoreFile(t, dir, ".gitignore", `
build
!build/important
`)
	ig, err := ParseIgnoreFile("", p, ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile: %v", err)
	}

	// "build" is matched; "build/important" is un-matched.
	skip, _ := ig.MatchDir("build")
	if !skip {
		t.Error("expected build to be skipped")
	}
	// Negation of a path prefix — the negation rule pattern "build/important"
	// does NOT un-skip "build" itself (it would un-skip "build/important").
	// So "build" stays skipped.
	skip2, _ := ig.MatchDir("build/important")
	// "build/important" should be un-skipped by the negation rule.
	if skip2 {
		t.Error("expected build/important NOT to be skipped (negation rule)")
	}
}

func TestIgnoreFile_AnchoredPattern(t *testing.T) {
	dir := t.TempDir()
	// Anchored pattern: "/android/build" only matches android/build at root.
	p := writeIgnoreFile(t, dir, ".gitignore", "/android/build\n")
	ig, err := ParseIgnoreFile("", p, ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile: %v", err)
	}

	cases := []struct {
		relPath string
		want    bool
	}{
		{"android/build", true},
		{"ios/build", false},
		{"build", false},
	}
	for _, tc := range cases {
		skip, line := ig.MatchDir(tc.relPath)
		if skip != tc.want {
			t.Errorf("MatchDir(%q) skip=%v line=%d want skip=%v", tc.relPath, skip, line, tc.want)
		}
	}
}

func TestIgnoreFile_DoubleStarPattern(t *testing.T) {
	dir := t.TempDir()
	p := writeIgnoreFile(t, dir, ".gitignore", "**/build\n")
	ig, err := ParseIgnoreFile("", p, ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile: %v", err)
	}

	for _, path := range []string{"build", "android/build", "ios/DerivedData/build"} {
		skip, _ := ig.MatchDir(path)
		if !skip {
			t.Errorf("expected %q to be skipped by **/build", path)
		}
	}
}

func TestIgnoreFile_NotExist(t *testing.T) {
	// A missing ignore file should return a no-op IgnoreFile, not an error.
	ig, err := ParseIgnoreFile("", "/nonexistent/.gitignore", ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile on missing file: %v", err)
	}
	if ig == nil {
		t.Fatal("expected non-nil IgnoreFile")
	}
	skip, _ := ig.MatchDir("node_modules")
	if skip {
		t.Error("empty IgnoreFile should not skip anything")
	}
}

// --------------------------------------------------------------------------
// WalkRepo integration tests
// --------------------------------------------------------------------------

func makeRepoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Source files that should be walked.
	mkfile(t, root, "app/index.ts", "export default 42;")
	mkfile(t, root, "src/main.go", "package main")
	mkfile(t, root, "README.md", "# readme")

	// Dirs that should be skipped via .gitignore
	mkfile(t, root, "android/build/output.apk", "binary")
	mkfile(t, root, "ios/Pods/SomePod.h", "header")

	// Dirs that should be skipped via hardcoded list
	mkfile(t, root, "node_modules/lib/index.js", "lib")
	mkfile(t, root, "APK/release.apk", "binary")

	// .gitignore at root
	mkfile(t, root, ".gitignore", "android/build\nios/Pods\n")

	return root
}

func mkfile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestWalkRepo_SkipsGitignoreDirs(t *testing.T) {
	root := makeRepoFixture(t)
	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	// android/build and ios/Pods should be in skipped list.
	skippedAbsPaths := make(map[string]string)
	for _, s := range skipped {
		skippedAbsPaths[s.AbsPath] = s.Rule
	}

	androidBuild := filepath.Join(root, "android", "build")
	iosPods := filepath.Join(root, "ios", "Pods")

	if _, ok := skippedAbsPaths[androidBuild]; !ok {
		t.Errorf("expected android/build to be skipped; skipped=%v", skippedAbsPaths)
	}
	if _, ok := skippedAbsPaths[iosPods]; !ok {
		t.Errorf("expected ios/Pods to be skipped; skipped=%v", skippedAbsPaths)
	}

	// Source files should be present.
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for _, want := range []string{"app/index.ts", "src/main.go", "README.md"} {
		if !fileSet[want] {
			t.Errorf("expected %q in files; files=%v", want, files)
		}
	}

	// No android/build or ios/Pods file should have leaked.
	for _, f := range files {
		if strings.HasPrefix(f, "android/build/") || strings.HasPrefix(f, "ios/Pods/") {
			t.Errorf("file from skipped dir leaked into results: %q", f)
		}
	}
}

func TestWalkRepo_HardcodedSkip(t *testing.T) {
	root := t.TempDir()
	// No .gitignore — rely on hardcoded list.
	mkfile(t, root, "node_modules/foo/bar.js", "lib")
	mkfile(t, root, "APK/release.apk", "binary")
	mkfile(t, root, "Pods/SomePod.h", "header")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}

	for _, want := range []string{"node_modules", "APK", "Pods"} {
		if !skippedNames[want] {
			t.Errorf("expected %q to be hardcoded-skipped; skipped=%v", want, skipped)
		}
	}

	// Source should be present.
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in files")
	}
}

func TestWalkRepo_GrafelIgnore(t *testing.T) {
	root := t.TempDir()
	// .grafelignore skips "test-fixtures" even though it's committed.
	mkfile(t, root, ".grafelignore", "test-fixtures\n")
	mkfile(t, root, "test-fixtures/big_fixture.json", "big json")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}
	if !skippedNames["test-fixtures"] {
		t.Errorf("expected test-fixtures to be skipped via .grafelignore; skipped=%v", skipped)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in files")
	}
}

func TestWalkRepo_InheritsParentGrafelIgnoreForMonorepoChild(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")

	mkfile(t, root, ".grafelignore", "**/wwwroot/libs/\nsrc/generated/\n")
	mkfile(t, root, "src/Assessment.HttpApi.Host/wwwroot/libs/datatables.net/js/dataTables.min.js", "generated")
	mkfile(t, root, "src/generated/out.go", "generated")
	mkfile(t, root, "src/Assessment.HttpApi.Host/Program.cs", "class Program {}")

	files, skipped, err := WalkRepo(filepath.Join(root, "src"), nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if fileSet["Assessment.HttpApi.Host/wwwroot/libs/datatables.net/js/dataTables.min.js"] {
		t.Fatalf("parent .grafelignore did not skip wwwroot libs; files=%v skipped=%v", files, skipped)
	}
	if fileSet["generated/out.go"] {
		t.Fatalf("parent .grafelignore did not rewrite src/generated for child root; files=%v skipped=%v", files, skipped)
	}
	if !fileSet["Assessment.HttpApi.Host/Program.cs"] {
		t.Fatalf("source file missing after inherited ignore filtering; files=%v skipped=%v", files, skipped)
	}
}

func TestWalkRepo_InheritsNestedGrafelIgnoreRelativeToItsDirectory(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")

	mkfile(t, root, ".grafelignore", "apps/**/dist/\n")
	mkfile(t, root, "apps/.grafelignore", "admin/generated/\n")
	mkfile(t, root, "apps/admin/dist/bundle.js", "generated")
	mkfile(t, root, "apps/admin/generated/client.ts", "generated")
	mkfile(t, root, "apps/admin/src/main.ts", "source")

	files, skipped, err := WalkRepo(filepath.Join(root, "apps", "admin"), nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if fileSet["dist/bundle.js"] || fileSet["generated/client.ts"] {
		t.Fatalf("nested inherited ignores were not applied; files=%v skipped=%v", files, skipped)
	}
	if !fileSet["src/main.ts"] {
		t.Fatalf("source file missing; files=%v skipped=%v", files, skipped)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWalkRepo_PrintSkipped(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, ".gitignore", "android/build\n")
	mkfile(t, root, "android/build/out.apk", "binary")
	mkfile(t, root, "node_modules/foo.js", "lib")
	mkfile(t, root, "app/main.ts", "code")

	var buf strings.Builder
	_, _, err := WalkRepo(root, &Options{PrintSkipped: &buf})
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[skip]") {
		t.Errorf("expected [skip] lines in output; got: %q", out)
	}
	if !strings.Contains(out, "(rule:") {
		t.Errorf("expected rule label in output; got: %q", out)
	}
}

func TestWalkRepo_AdditionalSkipDirs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "custom-cache/data.bin", "data")
	mkfile(t, root, "src/main.go", "package main")

	opts := &Options{AdditionalSkipDirs: []string{"custom-cache"}}
	files, skipped, err := WalkRepo(root, opts)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}
	if !skippedNames["custom-cache"] {
		t.Errorf("expected custom-cache to be skipped; skipped=%v", skipped)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in files")
	}
}

func TestIsHardcodedSkip(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"node_modules", true},
		{"Pods", true},
		{"DerivedData", true},
		{"APK", true},
		{"__pycache__", true},
		{".gradle", true},
		{"myapp.egg-info", true},
		// generated dirs (MANIFEST §25 D24)
		{"_generated", true},
		{"src", false},
		{"app", false},
		{"internal", false},
	}
	for _, tc := range cases {
		if got := IsHardcodedSkip(tc.name); got != tc.want {
			t.Errorf("IsHardcodedSkip(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestWalkRepo_SkipsGeneratedDir verifies that a directory named _generated
// (MANIFEST §25 / D24 fixture) is excluded from the walk via the hardcoded
// skip list, and that sibling source files are still returned.
func TestWalkRepo_SkipsGeneratedDir(t *testing.T) {
	root := t.TempDir()
	// Generated proto/OpenAPI stubs — must be excluded.
	mkfile(t, root, "services/orders/app/_generated/orderspb/orders_pb2.py", "# generated")
	mkfile(t, root, "services/orders/app/_generated/openapi_client/payments_api.py", "# generated")
	// Legitimate source alongside the generated dir.
	mkfile(t, root, "services/orders/app/handlers.py", "# real source")
	mkfile(t, root, "services/orders/app/models.py", "# real source")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	// _generated must appear in the skipped list.
	generatedSkipped := false
	for _, s := range skipped {
		if filepath.Base(s.AbsPath) == "_generated" {
			generatedSkipped = true
			break
		}
	}
	if !generatedSkipped {
		t.Errorf("expected _generated dir to be skipped; skipped=%v", skipped)
	}

	// No generated file should leak into results.
	for _, f := range files {
		if strings.Contains(f, "_generated/") {
			t.Errorf("generated file leaked into walk results: %q", f)
		}
	}

	// Legitimate source files must still be present.
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for _, want := range []string{
		"services/orders/app/handlers.py",
		"services/orders/app/models.py",
	} {
		if !fileSet[want] {
			t.Errorf("expected source file %q in results; files=%v", want, files)
		}
	}
}

// TestWalkRepo_SkipsVendorDir verifies that vendor/ directories (D24/D25 vendored
// dirs) are excluded, while legitimate top-level source is preserved.
func TestWalkRepo_SkipsVendorDir(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "vendor/carrier-sdk/client.go", "// vendored sdk")
	mkfile(t, root, "vendor/grpc_health/health.go", "// vendored grpc")
	mkfile(t, root, "internal/service/service.go", "package service")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	vendorSkipped := false
	for _, s := range skipped {
		if filepath.Base(s.AbsPath) == "vendor" {
			vendorSkipped = true
			break
		}
	}
	if !vendorSkipped {
		t.Errorf("expected vendor/ to be skipped; skipped=%v", skipped)
	}

	for _, f := range files {
		if strings.HasPrefix(f, "vendor/") {
			t.Errorf("vendored file leaked into walk results: %q", f)
		}
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["internal/service/service.go"] {
		t.Errorf("expected internal/service/service.go in results")
	}
}

// TestWalkRepo_SkipsLinguistGeneratedDir verifies Layer 4 (P3): a directory
// containing a .gitattributes file with "* linguist-generated=true" is skipped
// even if its name is not in the hardcoded list.
func TestWalkRepo_SkipsLinguistGeneratedDir(t *testing.T) {
	root := t.TempDir()
	// Simulate a dir with a linguist-generated marker (non-standard name).
	mkfile(t, root, "proto_out/.gitattributes", "# auto-generated\n* linguist-generated=true\n")
	mkfile(t, root, "proto_out/foo_pb2.py", "# generated code")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	linguistSkipped := false
	for _, s := range skipped {
		if filepath.Base(s.AbsPath) == "proto_out" && s.Rule == "linguist-generated" {
			linguistSkipped = true
			break
		}
	}
	if !linguistSkipped {
		t.Errorf("expected proto_out to be skipped via linguist-generated rule; skipped=%v", skipped)
	}

	for _, f := range files {
		if strings.HasPrefix(f, "proto_out/") {
			t.Errorf("linguist-generated file leaked into walk results: %q", f)
		}
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in results")
	}
}

// TestWalkRepo_SkipsToolAgentDirs verifies issue #1629: AI / pair-programmer
// and CI metadata dirs are filtered at the walker so they never appear in
// the index. These dirs hold .md / .json config — not source — and were
// previously inflating per-module file counts (e.g. .windsurf/skills/*.md).
func TestWalkRepo_SkipsToolAgentDirs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, ".windsurf/skills/coding.md", "skill")
	mkfile(t, root, ".cursor/rules/foo.json", "{}")
	mkfile(t, root, ".claude/settings.json", "{}")
	mkfile(t, root, ".github/workflows/ci.yml", "name: ci")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}
	for _, want := range []string{".windsurf", ".cursor", ".claude", ".github"} {
		if !skippedNames[want] {
			t.Errorf("expected %q to be skipped (issue #1629); skipped=%v", want, skipped)
		}
	}

	for _, f := range files {
		for _, prefix := range []string{".windsurf/", ".cursor/", ".claude/", ".github/"} {
			if strings.HasPrefix(f, prefix) {
				t.Errorf("tool-agent file leaked into walk results: %q", f)
			}
		}
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in results; files=%v", files)
	}
}

// TestWalkRepo_SkipsAssetDirs verifies issue #1629: asset / image / media
// dirs are filtered at the walker. assets/images/icon-dark.png and similar
// were polluting the per-module file count for mobile repos.
func TestWalkRepo_SkipsAssetDirs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "assets/images/icon-dark.png", "png")
	mkfile(t, root, "assets/fonts/Inter.ttf", "font")
	mkfile(t, root, "images/hero.jpg", "jpg")
	mkfile(t, root, "fonts/regular.woff2", "font")
	mkfile(t, root, "media/intro.mp4", "video")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}
	for _, want := range []string{"assets", "images", "fonts", "media"} {
		if !skippedNames[want] {
			t.Errorf("expected %q to be skipped (issue #1629); skipped=%v", want, skipped)
		}
	}

	for _, f := range files {
		if strings.HasPrefix(f, "assets/") || strings.HasPrefix(f, "images/") ||
			strings.HasPrefix(f, "fonts/") || strings.HasPrefix(f, "media/") {
			t.Errorf("asset file leaked into walk results: %q", f)
		}
	}
}

// TestWalkRepo_SkipsDocsDir verifies issue #1629: top-level docs/ dir
// (often generated or hand-authored markdown) is filtered. Note that
// per #1658, generated docs live in the daemon store, not the repo,
// so this filters legacy/hand-authored markdown trees. Source under
// docs/ must use additional_skip_dirs override.
func TestWalkRepo_SkipsDocsDir(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "docs/architecture.md", "# arch")
	mkfile(t, root, "docs/guide.md", "# guide")
	mkfile(t, root, "src/main.go", "package main")

	files, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	docsSkipped := false
	for _, s := range skipped {
		if filepath.Base(s.AbsPath) == "docs" {
			docsSkipped = true
			break
		}
	}
	if !docsSkipped {
		t.Errorf("expected docs/ to be skipped (issue #1629); skipped=%v", skipped)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "docs/") {
			t.Errorf("docs file leaked into walk results: %q", f)
		}
	}
}

// TestWalkRepo_SkipsBinaryExtensions verifies issue #1629: binary / image /
// media / archive / compiled file extensions are filtered at file-level,
// even when sitting next to real source.
func TestWalkRepo_SkipsBinaryExtensions(t *testing.T) {
	root := t.TempDir()
	// Real source.
	mkfile(t, root, "src/main.go", "package main")
	mkfile(t, root, "src/index.ts", "export {}")
	// Binary noise alongside source — should be filtered.
	mkfile(t, root, "src/logo.png", "png")
	mkfile(t, root, "src/screenshot.jpg", "jpg")
	mkfile(t, root, "src/diagram.svg", "<svg/>")
	mkfile(t, root, "src/intro.mp4", "video")
	mkfile(t, root, "src/manual.pdf", "pdf")
	mkfile(t, root, "src/bundle.zip", "zip")
	mkfile(t, root, "src/native.dylib", "binary")
	mkfile(t, root, "src/font.woff2", "font")

	files, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	// Real source kept.
	for _, want := range []string{"src/main.go", "src/index.ts"} {
		if !fileSet[want] {
			t.Errorf("expected %q in results; files=%v", want, files)
		}
	}
	// Binary extensions filtered.
	for _, bad := range []string{
		"src/logo.png", "src/screenshot.jpg", "src/diagram.svg",
		"src/intro.mp4", "src/manual.pdf", "src/bundle.zip",
		"src/native.dylib", "src/font.woff2",
	} {
		if fileSet[bad] {
			t.Errorf("binary file leaked into walk results: %q", bad)
		}
	}
}

// TestWalkRepo_SkipsBuildOutputDirs verifies issue #1629: build / out /
// dist / .cache / bin / obj / .terraform / .ruff_cache are filtered.
func TestWalkRepo_SkipsBuildOutputDirs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, ".cache/foo.txt", "x")
	mkfile(t, root, "bin/server", "x")
	mkfile(t, root, "obj/Debug/app.o", "x")
	mkfile(t, root, ".terraform/providers/aws", "x")
	mkfile(t, root, ".ruff_cache/foo", "x")
	mkfile(t, root, "src/main.go", "package main")

	_, skipped, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	skippedNames := make(map[string]bool)
	for _, s := range skipped {
		skippedNames[filepath.Base(s.AbsPath)] = true
	}
	for _, want := range []string{".cache", "bin", "obj", ".terraform", ".ruff_cache"} {
		if !skippedNames[want] {
			t.Errorf("expected %q to be skipped (issue #1629); skipped=%v", want, skipped)
		}
	}
}

// TestDefaultWalkerHelpers verifies the read-only accessors stay in sync
// with the canonical maps and return safe (independent) copies.
func TestDefaultWalkerHelpers(t *testing.T) {
	dirs := defaultWalkerSkipDirs()
	if len(dirs) != len(hardcodedSkipDirs) {
		t.Errorf("defaultWalkerSkipDirs len=%d want %d", len(dirs), len(hardcodedSkipDirs))
	}
	// Mutating the returned map must not leak.
	dirs["bogus-test-entry"] = struct{}{}
	if _, ok := hardcodedSkipDirs["bogus-test-entry"]; ok {
		t.Error("defaultWalkerSkipDirs returned a live reference; mutation leaked")
	}

	exts := defaultWalkerSkipExtensions()
	if len(exts) != len(hardcodedSkipExtensions) {
		t.Errorf("defaultWalkerSkipExtensions len=%d want %d", len(exts), len(hardcodedSkipExtensions))
	}
	for _, want := range []string{".png", ".jpg", ".svg", ".mp4", ".pdf", ".zip", ".woff2"} {
		if _, ok := exts[want]; !ok {
			t.Errorf("default skip extensions missing %q", want)
		}
	}
}

// TestIsHardcodedSkip_NewEntries covers the #1629 additions for the
// watcher and scheduler (which use IsHardcodedSkip to short-circuit).
func TestIsHardcodedSkip_NewEntries(t *testing.T) {
	for _, name := range []string{
		".windsurf", ".cursor", ".claude", ".github",
		"assets", "images", "fonts", "media", "docs",
		".cache", "bin", "obj", ".terraform", ".ruff_cache",
	} {
		if !IsHardcodedSkip(name) {
			t.Errorf("IsHardcodedSkip(%q) = false, want true (issue #1629)", name)
		}
	}
}

// TestIsLinguistGeneratedDir tests the helper directly.
func TestIsLinguistGeneratedDir(t *testing.T) {
	t.Run("marks_all_generated", func(t *testing.T) {
		dir := t.TempDir()
		writeIgnoreFile(t, dir, ".gitattributes", "# generated\n* linguist-generated=true\n")
		if !isLinguistGeneratedDir(dir) {
			t.Error("expected true for '* linguist-generated=true'")
		}
	})

	t.Run("partial_pattern_not_wildcard", func(t *testing.T) {
		dir := t.TempDir()
		writeIgnoreFile(t, dir, ".gitattributes", "*.pb.go linguist-generated=true\n")
		if isLinguistGeneratedDir(dir) {
			t.Error("expected false: partial pattern should not trigger directory skip")
		}
	})

	t.Run("no_gitattributes", func(t *testing.T) {
		dir := t.TempDir()
		if isLinguistGeneratedDir(dir) {
			t.Error("expected false: no .gitattributes present")
		}
	})
}
