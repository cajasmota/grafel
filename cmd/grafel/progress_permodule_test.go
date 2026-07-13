package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/progress"
)

// writeJSMonorepo builds a synthetic npm-workspaces monorepo under dir with the
// given packages, each populated with filesPerPkg trivial .js files. It returns
// the repo-relative package roots (e.g. "packages/alpha"). The layout is a real
// TRUE monorepo: a package.json workspaces manifest AND a packages/ container,
// so detect.DetectMonorepo classifies it with >=2 package roots.
func writeJSMonorepo(t *testing.T, dir string, pkgs []string, filesPerPkg int) []string {
	t.Helper()
	root := `{"name":"root","private":true,"workspaces":["packages/*"]}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(root), 0o644); err != nil {
		t.Fatalf("write root package.json: %v", err)
	}
	roots := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		pkgDir := filepath.Join(dir, "packages", p)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", pkgDir, err)
		}
		manifest := fmt.Sprintf(`{"name":"@scope/%s","version":"1.0.0"}`, p)
		if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(manifest), 0o644); err != nil {
			t.Fatalf("write %s package.json: %v", p, err)
		}
		for f := 0; f < filesPerPkg; f++ {
			src := fmt.Sprintf("export function %s_fn%d(a, b) {\n  return a + b + %d;\n}\n", p, f, f)
			name := fmt.Sprintf("mod%d.js", f)
			if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(src), 0o644); err != nil {
				t.Fatalf("write %s/%s: %v", p, name, err)
			}
		}
		roots = append(roots, "packages/"+p)
	}
	return roots
}

// TestPerModuleExtractTicks_SmallMonorepo is the STEP-1 experiment turned
// regression test. It runs a REAL index over a synthetic JS-workspaces monorepo
// through a RECORDING publisher (not the coalescing sidecar) and asserts that
// per-file extracting_ast Ticks with a NON-EMPTY, correct Module are published
// for EVERY package — even when each package has only a handful of files (well
// under progress.TickEveryNFiles).
//
// Before the fix, extraction ticks were gated solely on the GLOBAL file counter
// hitting a multiple of TickEveryNFiles (20) with no final flush, so a monorepo
// whose total file count is < 20 (or whose per-module files never coincide with
// a global multiple of 20) emitted ZERO per-module ticks. This reproduces the
// deployed-daemon evidence: only post-extraction Phase() events reached the
// sidecar, no per-module rows.
func TestPerModuleExtractTicks_SmallMonorepo(t *testing.T) {
	dir := t.TempDir()
	// 3 files/pkg * 2 pkgs = 6 project .js files — deliberately < TickEveryNFiles
	// so the old global-counter gate emits nothing.
	roots := writeJSMonorepo(t, dir, []string{"alpha", "beta"}, 3)

	// Sanity: the fixture must be a TRUE monorepo with >=2 packages, else the
	// resolver installs a single per-repo label and the test proves nothing.
	mono, err := detect.DetectMonorepo(dir)
	if err != nil {
		t.Fatalf("DetectMonorepo: %v", err)
	}
	if mono.Kind == detect.KindNone {
		t.Fatalf("fixture not detected as a monorepo (Kind=%q); packages=%v", mono.Kind, mono.Packages)
	}
	if len(mono.Packages) < 2 {
		t.Fatalf("expected >=2 packages, got %d: %v", len(mono.Packages), mono.Packages)
	}

	col := &progress.SliceCollector{}
	idx := newTestIndexer(t, "jsmono", nil, "")
	idx.publisher = col
	idx.repoSlug = "jsmono"

	if _, err := idx.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Collect the set of modules that received an extracting_ast tick.
	modTicks := map[string]int{}
	for _, e := range col.Events {
		if e.Phase == progress.PhaseExtractAST && e.Module != "" {
			modTicks[e.Module]++
		}
	}

	for _, want := range roots {
		if modTicks[want] == 0 {
			t.Errorf("no per-module extracting_ast tick for module %q (got module ticks: %v)", want, modTicks)
		}
	}
}

// TestPerModuleExtractTicks_LargerMonorepo asserts full per-module coverage even
// when total file count exceeds TickEveryNFiles: EVERY package must get at least
// one tick, not just whichever package happened to own a file landing on a
// global multiple of 20. Three packages of 8 files each = 24 total; under the
// old global gate only the single file at global index 20 would tick, covering
// at most one package.
func TestPerModuleExtractTicks_LargerMonorepo(t *testing.T) {
	dir := t.TempDir()
	roots := writeJSMonorepo(t, dir, []string{"alpha", "beta", "gamma"}, 8)

	col := &progress.SliceCollector{}
	idx := newTestIndexer(t, "jsmono3", nil, "")
	idx.publisher = col
	idx.repoSlug = "jsmono3"

	if _, err := idx.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	modTicks := map[string]int{}
	for _, e := range col.Events {
		if e.Phase == progress.PhaseExtractAST && e.Module != "" {
			modTicks[e.Module]++
		}
	}
	for _, want := range roots {
		if modTicks[want] == 0 {
			t.Errorf("no per-module extracting_ast tick for module %q (got: %v)", want, modTicks)
		}
	}
}
