package dashboard

// v2_source_test.go — unit tests for the source-peek helpers (#4499).
//
// Covers the path-traversal guard and multi-repo resolution in
// resolveSourcePath, the small-file-whole / large-file-window logic in
// sourceWindow, and the extension → language hint mapping.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepoFile creates rel under root (making parent dirs) with content.
func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestResolveSourcePath_FindsFileAcrossRepos(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeRepoFile(t, rootB, "src/app.ts", "const x = 1;\n")

	grp := &DashGroup{Repos: map[string]*DashRepo{
		"a": {Slug: "a", Path: rootA},
		"b": {Slug: "b", Path: rootB},
	}}

	abs, slug, rel, ok := resolveSourcePath(grp, "src/app.ts", "")
	if !ok {
		t.Fatalf("expected to resolve file in repo b")
	}
	if slug != "b" {
		t.Errorf("slug = %q; want b", slug)
	}
	if rel != "src/app.ts" {
		t.Errorf("rel = %q; want src/app.ts", rel)
	}
	if !strings.HasPrefix(abs, rootB) {
		t.Errorf("abs %q not under rootB %q", abs, rootB)
	}
}

func TestResolveSourcePath_PinnedRepo(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	// Same rel path exists in BOTH repos; ?repo must pin selection.
	writeRepoFile(t, rootA, "x.go", "package a\n")
	writeRepoFile(t, rootB, "x.go", "package b\n")

	grp := &DashGroup{Repos: map[string]*DashRepo{
		"a": {Slug: "a", Path: rootA},
		"b": {Slug: "b", Path: rootB},
	}}

	_, slug, _, ok := resolveSourcePath(grp, "x.go", "a")
	if !ok || slug != "a" {
		t.Fatalf("pinned resolve = (%q,%v); want (a,true)", slug, ok)
	}
}

// TestResolveSourcePath_StaleRepoHintFallsBackToGroupScan reproduces the
// upvate-v3 / core-backend-v3 source-peek failure (#4551). The frontend passed
// the GROUP name ("upvate-v3") as the repo hint instead of the entity's owning
// repo slug ("core-backend-v3"), and the file lives in a repo that is NOT the
// first one in the group. Before the fix the pinned-but-missing hint failed
// hard ("file not found in any repo"); now it falls back to scanning every
// repo root and resolves the file in its real repo.
func TestResolveSourcePath_StaleRepoHintFallsBackToGroupScan(t *testing.T) {
	rootEmpty := t.TempDir()       // an unrelated repo, first in iteration sometimes
	rootBackend := t.TempDir()     // the repo that actually holds the file
	writeRepoFile(t, rootBackend, "src/app.controller.ts", "export class AppController {}\n")

	grp := &DashGroup{Repos: map[string]*DashRepo{
		"frontend":        {Slug: "frontend", Path: rootEmpty},
		"core-backend-v3": {Slug: "core-backend-v3", Path: rootBackend},
	}}

	// Hint is the GROUP name, which is not a repo slug — must not fail hard.
	abs, slug, rel, ok := resolveSourcePath(grp, "src/app.controller.ts", "upvate-v3")
	if !ok {
		t.Fatalf("expected fallback scan to resolve file despite stale repo hint")
	}
	if slug != "core-backend-v3" {
		t.Errorf("slug = %q; want core-backend-v3", slug)
	}
	if rel != "src/app.controller.ts" {
		t.Errorf("rel = %q; want src/app.controller.ts", rel)
	}
	if !strings.HasPrefix(abs, rootBackend) {
		t.Errorf("abs %q not under rootBackend %q", abs, rootBackend)
	}
}

func TestResolveSourcePath_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	// Secret lives OUTSIDE the repo root.
	parent := filepath.Dir(root)
	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	grp := &DashGroup{Repos: map[string]*DashRepo{
		"a": {Slug: "a", Path: root},
	}}

	for _, attack := range []string{
		"../secret.txt",
		"../../etc/passwd",
		"foo/../../secret.txt",
		"/etc/passwd",
	} {
		if _, _, _, ok := resolveSourcePath(grp, attack, ""); ok {
			t.Errorf("traversal %q resolved; want rejected", attack)
		}
	}
}

func TestResolveSourcePath_MissingFile(t *testing.T) {
	root := t.TempDir()
	grp := &DashGroup{Repos: map[string]*DashRepo{"a": {Slug: "a", Path: root}}}
	if _, _, _, ok := resolveSourcePath(grp, "nope.ts", ""); ok {
		t.Errorf("missing file resolved; want false")
	}
}

func TestSourceWindow(t *testing.T) {
	cases := []struct {
		name                 string
		total, line, context int
		wantStart, wantEnd   int
	}{
		{"small file returned whole", 50, 25, 10, 1, 50},
		{"large file windowed around line", 5000, 1000, 40, 960, 1040},
		{"window clamped at top", 5000, 5, 40, 1, 45},
		{"window clamped at bottom", 5000, 4990, 40, 4950, 5000},
		{"large file no line anchors head", 5000, 0, 40, 1, 81},
		{"empty file", 0, 10, 10, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := sourceWindow(c.total, c.line, c.context)
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("sourceWindow(%d,%d,%d) = (%d,%d); want (%d,%d)",
					c.total, c.line, c.context, start, end, c.wantStart, c.wantEnd)
			}
		})
	}
}

func TestLanguageFromExt(t *testing.T) {
	cases := map[string]string{
		"src/app.ts":      "typescript",
		"src/App.tsx":     "tsx",
		"main.go":         "go",
		"a/b/c.py":        "python",
		"infra/main.tf":   "hcl",
		"Dockerfile":      "docker",
		"Makefile":        "makefile",
		"data.unknownext": "text",
		"noext":           "text",
	}
	for path, want := range cases {
		if got := languageFromExt(path); got != want {
			t.Errorf("languageFromExt(%q) = %q; want %q", path, got, want)
		}
	}
}

func TestReadAllLines(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lines, err := readAllLines(p)
	if err != nil {
		t.Fatalf("readAllLines: %v", err)
	}
	if len(lines) != 3 || lines[0] != "a" || lines[2] != "c" {
		t.Errorf("lines = %v; want [a b c]", lines)
	}
}
