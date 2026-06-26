package extractors

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWalkSourceFiles_HonorsGitignore is the regression test for #5665.
//
// The incremental change-detector's file walk must honor .gitignore exactly
// the way the full indexer (walk.WalkRepo) does. Before the fix, walkSourceFiles
// was a hand-rolled walk with only a small hardcoded denylist and NO .gitignore
// handling, so gitignored build-artifact directories (e.g. ios/Pods,
// android/**/.cxx) were walked and entered the change manifest. Because build
// tooling constantly regenerates/deletes those files, they were counted as
// "changed" on every poll — perpetually tripping the too-many-changed
// full-reindex fallback and pinning the daemon CPU in an endless reindex loop
// even though the repo HEAD never moved.
//
// This test fixes the walk at its source: gitignored paths must not appear in
// the walk result, so they can never drive a reindex.
func TestWalkSourceFiles_HonorsGitignore(t *testing.T) {
	repo := t.TempDir()

	mustWrite := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// .gitignore excludes the churning build-artifact dirs (as real RN/iOS/
	// Android repos do); main.go is a real tracked source file.
	mustWrite(".gitignore", "Pods/\nandroid/app/.cxx/\n")
	mustWrite("main.go", "package main\n\nfunc main() {}\n")
	mustWrite("Pods/Manifest.lock", "PODS: []\n")
	mustWrite("Pods/Target Support Files/Foo/Foo.modulemap", "module Foo {}\n")
	mustWrite("android/app/.cxx/RelWithDebInfo/abc/index.json", "{}\n")

	files, err := walkSourceFiles(repo)
	if err != nil {
		t.Fatalf("walkSourceFiles: %v", err)
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f] = true
	}

	if !got["main.go"] {
		t.Errorf("expected tracked source file main.go to be walked; got %v", files)
	}
	for _, ignored := range []string{
		"Pods/Manifest.lock",
		"Pods/Target Support Files/Foo/Foo.modulemap",
		"android/app/.cxx/RelWithDebInfo/abc/index.json",
	} {
		if got[ignored] {
			t.Errorf("gitignored path %q must NOT be walked — counting it drives the reindex loop (#5665); got %v",
				ignored, files)
		}
	}
}
