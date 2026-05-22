package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/progress"
)

// writeFile is a small helper for the #1628 end-to-end tests.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// distinctModules returns the set of non-empty Event.Module labels seen across
// extraction progress events.
func distinctProgressModules(events []progress.Event) map[string]bool {
	out := map[string]bool{}
	for _, e := range events {
		if e.Phase == progress.PhaseExtractAST && e.Module != "" {
			out[e.Module] = true
		}
	}
	return out
}

// TestIndex_PlainRepoSingleModule regresses issue #1628: a PLAIN repo (no
// workspace manifest, source split across bare top-level dirs) must index as a
// SINGLE module unit — per-module progress shows one per-repo row, and every
// entity's Properties["module"] is that single label (NOT _root/src/components).
func TestIndex_PlainRepoSingleModule(t *testing.T) {
	dir := t.TempDir()
	// Mirror upvate_core_frontend: one root package.json, source in bare
	// top-level dirs, plus the dot-dir and docs that wrongly surfaced before.
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"upvate-frontend"}`)
	writeFile(t, filepath.Join(dir, "index.ts"), "export const root = 1\n")
	writeFile(t, filepath.Join(dir, "src/app.ts"), "export const app = 1\n")
	writeFile(t, filepath.Join(dir, "src/util/helpers.ts"), "export const h = 1\n")
	writeFile(t, filepath.Join(dir, "components/Button.tsx"), "export const Button = 1\n")
	writeFile(t, filepath.Join(dir, "components/Card.tsx"), "export const Card = 1\n")
	writeFile(t, filepath.Join(dir, "docs/guide.ts"), "export const doc = 1\n")
	writeFile(t, filepath.Join(dir, ".windsurf/skills/skill.ts"), "export const sk = 1\n")

	col := &progress.SliceCollector{}
	idx := newTestIndexer(t, "upvate-frontend", nil)
	idx.publisher = col
	idx.repoSlug = "upvate-frontend"

	doc, err := idx.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// --- per-module progress: a single per-repo label, never per-dir rollups.
	// Tiny fixtures may finish extraction before any tick fires (ticks batch
	// every N files), so an empty set is tolerated; when present it must be the
	// single per-repo label and never a directory rollup.
	progMods := distinctProgressModules(col.Events)
	for m := range progMods {
		if m != "upvate-frontend" {
			t.Errorf("plain repo progress emitted unexpected module %q (want only upvate-frontend)", m)
		}
	}
	for _, bad := range []string{"_root", "src", "components", "docs", ".windsurf/skills", "src/util"} {
		if progMods[bad] {
			t.Errorf("plain repo wrongly split into module %q", bad)
		}
	}

	// --- entity props: every sourced entity carries the single label ---
	entMods := map[string]int{}
	for _, e := range doc.Entities {
		if e.SourceFile == "" {
			continue // synthetic/external nodes use module=_external
		}
		entMods[e.Properties["module"]]++
	}
	if len(entMods) != 1 {
		t.Fatalf("plain repo entity modules: want 1 distinct label, got %d: %v", len(entMods), entMods)
	}
	if _, ok := entMods["upvate-frontend"]; !ok {
		t.Fatalf("plain repo entity module label = %v, want upvate-frontend", entMods)
	}
}

// TestIndex_MonorepoKeepsModules confirms the fix does NOT regress true
// monorepos: a repo with a workspace-style container layout still produces a
// per-package module breakdown in both progress and entity props.
func TestIndex_MonorepoKeepsModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - 'packages/*'\n")
	writeFile(t, filepath.Join(dir, "packages/alpha/package.json"), `{"name":"alpha"}`)
	writeFile(t, filepath.Join(dir, "packages/alpha/index.ts"), "export const a = 1\n")
	writeFile(t, filepath.Join(dir, "packages/alpha/sub/x.ts"), "export const ax = 1\n")
	writeFile(t, filepath.Join(dir, "packages/beta/package.json"), `{"name":"beta"}`)
	writeFile(t, filepath.Join(dir, "packages/beta/index.ts"), "export const b = 1\n")

	col := &progress.SliceCollector{}
	idx := newTestIndexer(t, "polyrepo", nil)
	idx.publisher = col
	idx.repoSlug = "polyrepo"

	doc, err := idx.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Progress ticks may not fire on a tiny fixture; when they do, they must
	// carry per-package labels and never the single per-repo collapse.
	progMods := distinctProgressModules(col.Events)
	if progMods["polyrepo"] {
		t.Errorf("monorepo wrongly collapsed to a single per-repo progress row")
	}

	entMods := map[string]bool{}
	for _, e := range doc.Entities {
		if e.SourceFile == "" {
			continue
		}
		entMods[e.Properties["module"]] = true
	}
	if !entMods["packages/alpha"] || !entMods["packages/beta"] {
		t.Fatalf("monorepo entity modules lost packages: %v", entMods)
	}
}
