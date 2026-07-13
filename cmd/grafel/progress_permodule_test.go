package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/progress"
)

// pkgSpec describes one synthetic monorepo package.
type pkgSpec struct {
	name         string
	files        int  // number of extractable .js source files
	withManifest bool // whether to write a package.json (a classifier-SKIPPED file)
}

// writeJSMonorepo builds a synthetic npm-workspaces monorepo under dir. The root
// carries a package.json `workspaces` manifest AND a packages/ container, so
// detect.DetectMonorepo classifies it as a TRUE monorepo. Each package gets
// spec.files trivial .js files; a manifest is written only when requested — a
// manifest-less package has NO classifier-skipped file, so its per-module bar
// can only be driven by extracted SOURCE (the B1 regression guard). Returns the
// repo-relative package roots (e.g. "packages/alpha").
func writeJSMonorepo(t *testing.T, dir string, specs []pkgSpec) []string {
	t.Helper()
	root := `{"name":"root","private":true,"workspaces":["packages/*"]}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(root), 0o644); err != nil {
		t.Fatalf("write root package.json: %v", err)
	}
	roots := make([]string, 0, len(specs))
	for _, s := range specs {
		pkgDir := filepath.Join(dir, "packages", s.name)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", pkgDir, err)
		}
		if s.withManifest {
			manifest := fmt.Sprintf(`{"name":"@scope/%s","version":"1.0.0"}`, s.name)
			if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(manifest), 0o644); err != nil {
				t.Fatalf("write %s package.json: %v", s.name, err)
			}
		}
		for f := 0; f < s.files; f++ {
			src := fmt.Sprintf("export function %s_fn%d(a, b) {\n  return a + b + %d;\n}\n", s.name, f, f)
			name := fmt.Sprintf("mod%d.js", f)
			if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(src), 0o644); err != nil {
				t.Fatalf("write %s/%s: %v", s.name, name, err)
			}
		}
		roots = append(roots, "packages/"+s.name)
	}
	return roots
}

// runMonorepoIndex indexes dir through a recording publisher and returns the
// captured events, asserting the fixture is a real monorepo first.
func runMonorepoIndex(t *testing.T, dir, repoSlug string) []progress.Event {
	t.Helper()
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
	idx := newTestIndexer(t, repoSlug, nil, "")
	idx.publisher = col
	idx.repoSlug = repoSlug
	if _, err := idx.Run(context.Background(), dir); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return col.Events
}

// TestPerModuleExtractTicks_SmallMonorepo is the STEP-1 experiment turned
// regression test. It runs a REAL index over a synthetic JS-workspaces monorepo
// through a RECORDING publisher (not the coalescing sidecar) and asserts that
// per-file extracting_ast Ticks with a NON-EMPTY, correct Module are published
// for EVERY package — even when each package has only a handful of files (well
// under progress.TickEveryNFiles) and even for a package with NO manifest
// (source files must drive the tick — the B1 guard).
func TestPerModuleExtractTicks_SmallMonorepo(t *testing.T) {
	dir := t.TempDir()
	// alpha/beta carry manifests; gamma is manifest-less (source-only), so its
	// per-module tick can ONLY come from extracted .js — this fails pre-fix,
	// when only classifier-skipped files populated the per-module maps.
	roots := writeJSMonorepo(t, dir, []pkgSpec{
		{name: "alpha", files: 3, withManifest: true},
		{name: "beta", files: 3, withManifest: true},
		{name: "gamma", files: 4, withManifest: false},
	})

	events := runMonorepoIndex(t, dir, "jsmono")

	// Every module must receive an extracting_ast tick whose CurrentFile is a
	// .js SOURCE file (not a skipped package.json) — proving source drives it.
	srcTickSeen := map[string]bool{}
	for _, e := range events {
		if e.Phase != progress.PhaseExtractAST || e.Module == "" {
			continue
		}
		if strings.HasSuffix(e.CurrentFile, ".js") {
			srcTickSeen[e.Module] = true
		}
	}
	for _, want := range roots {
		if !srcTickSeen[want] {
			t.Errorf("module %q: no extracting_ast tick with a .js source CurrentFile (source did not drive the per-module tick)", want)
		}
	}
}

// TestPerModuleExtractTicks_IndependentProgress is the B2 guard: two packages of
// DIFFERENT sizes must produce per-module ticks with DIFFERENT FilesDone/
// FilesTotal (distinct denominators / percentages), i.e. the bars advance
// independently rather than in lockstep on the shared aggregate. It also asserts
// each module's final tick reaches its OWN 100% (FilesDone == FilesTotal) and
// that no bogus "_root" row is emitted.
func TestPerModuleExtractTicks_IndependentProgress(t *testing.T) {
	dir := t.TempDir()
	// Distinct source-file counts → distinct per-module totals. Manifest-less so
	// FilesTotal reflects source files exactly (no +1 for a skipped manifest).
	writeJSMonorepo(t, dir, []pkgSpec{
		{name: "small", files: 3, withManifest: false},
		{name: "large", files: 9, withManifest: false},
	})

	events := runMonorepoIndex(t, dir, "jsmono2")

	// Track, per module, the FilesTotal seen and the max FilesDone.
	totals := map[string]int{}
	maxDone := map[string]int{}
	for _, e := range events {
		if e.Phase != progress.PhaseExtractAST || e.Module == "" {
			continue
		}
		if e.Module == "_root" {
			t.Errorf("bogus _root per-module row emitted: %+v", e)
		}
		totals[e.Module] = e.FilesTotal
		if e.FilesDone > maxDone[e.Module] {
			maxDone[e.Module] = e.FilesDone
		}
	}

	small := "packages/small"
	large := "packages/large"
	if totals[small] == 0 || totals[large] == 0 {
		t.Fatalf("missing per-module ticks: small total=%d large total=%d (all: %v)", totals[small], totals[large], totals)
	}
	// Independent denominators — NOT the shared aggregate.
	if totals[small] == totals[large] {
		t.Errorf("per-module bars are in lockstep: small FilesTotal=%d == large FilesTotal=%d (expected distinct per-module totals)", totals[small], totals[large])
	}
	if totals[small] != 3 || totals[large] != 9 {
		t.Errorf("per-module FilesTotal not the module's own file count: small=%d (want 3) large=%d (want 9)", totals[small], totals[large])
	}
	// Each module reaches its OWN 100%.
	if maxDone[small] != totals[small] {
		t.Errorf("small never reached 100%%: maxDone=%d total=%d", maxDone[small], totals[small])
	}
	if maxDone[large] != totals[large] {
		t.Errorf("large never reached 100%%: maxDone=%d total=%d", maxDone[large], totals[large])
	}
}

// TestPerModuleExtractTicks_LargerMonorepo asserts full per-module coverage even
// when total file count exceeds TickEveryNFiles: EVERY package gets at least one
// tick, not just whichever package owned a file landing on a global multiple of
// 20. Three packages of 8 files each = 24 total; under the old global gate only
// the file at global index 20 would tick, covering at most one package.
func TestPerModuleExtractTicks_LargerMonorepo(t *testing.T) {
	dir := t.TempDir()
	roots := writeJSMonorepo(t, dir, []pkgSpec{
		{name: "alpha", files: 8, withManifest: true},
		{name: "beta", files: 8, withManifest: true},
		{name: "gamma", files: 8, withManifest: true},
	})

	events := runMonorepoIndex(t, dir, "jsmono3")

	modTicks := map[string]int{}
	for _, e := range events {
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
