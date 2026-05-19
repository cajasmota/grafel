package walk

import (
	"os"
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

func TestWalkRepo_ArchigraphIgnore(t *testing.T) {
	root := t.TempDir()
	// .archigraphignore skips "test-fixtures" even though it's committed.
	mkfile(t, root, ".archigraphignore", "test-fixtures\n")
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
		t.Errorf("expected test-fixtures to be skipped via .archigraphignore; skipped=%v", skipped)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["src/main.go"] {
		t.Errorf("expected src/main.go in files")
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
