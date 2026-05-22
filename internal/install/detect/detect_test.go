package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStack(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "go.mod"), "module x\n")
		if got := Stack(dir); got != "go" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("next", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "package.json"), `{"name":"x"}`)
		write(t, filepath.Join(dir, "next.config.js"), "")
		if got := Stack(dir); got != "next" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("python", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "manage.py"), "")
		if got := Stack(dir); got != "python" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestDetectMonorepoPNPM(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - 'packages/*'\n  - 'apps/*'\n")
	write(t, filepath.Join(dir, "packages/a/package.json"), `{"name":"a"}`)
	write(t, filepath.Join(dir, "packages/b/package.json"), `{"name":"b"}`)
	write(t, filepath.Join(dir, "apps/web/package.json"), `{"name":"web"}`)
	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindPNPM {
		t.Fatalf("kind: %q", m.Kind)
	}
	if len(m.Packages) != 3 {
		t.Fatalf("packages: %+v", m.Packages)
	}
}

func TestDetectMonorepoNPMWorkspaces(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "package.json"), `{"name":"root","workspaces":["packages/*"]}`)
	write(t, filepath.Join(dir, "packages/a/package.json"), `{"name":"a"}`)
	write(t, filepath.Join(dir, "packages/b/package.json"), `{"name":"b"}`)
	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindNPM {
		t.Fatalf("kind: %q", m.Kind)
	}
	if len(m.Packages) != 2 {
		t.Fatalf("packages: %+v", m.Packages)
	}
}

func TestDetectMonorepoNx(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "nx.json"), `{}`)
	write(t, filepath.Join(dir, "package.json"), `{"workspaces":["apps/*","libs/*"]}`)
	write(t, filepath.Join(dir, "apps/web/package.json"), `{}`)
	write(t, filepath.Join(dir, "libs/util/package.json"), `{}`)
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindNx || len(m.Packages) != 2 {
		t.Fatalf("nx: %+v", m)
	}
}

// TestDetectMonorepoPolyglot builds a synthetic polyglot monorepo mirroring the
// polyglot-platform fixture: a pnpm-workspace.yaml that lists ONLY the Node
// services, plus many non-Node services (Python/Go/Java/Kotlin/Rust/PHP/Elixir)
// the manifest does not list. Detection must surface ALL of them, not just the
// pnpm packages (regression for #1559).
func TestDetectMonorepoPolyglot(t *testing.T) {
	dir := t.TempDir()
	// pnpm workspace listing ONLY the Node services + frontends.
	write(t, filepath.Join(dir, "pnpm-workspace.yaml"),
		"packages:\n  - 'services/gateway'\n  - 'services/catalog'\n  - 'frontend/*'\n")
	write(t, filepath.Join(dir, "services/gateway/package.json"), `{"name":"gateway"}`)
	write(t, filepath.Join(dir, "services/gateway/src/index.ts"), "export const x = 1\n")
	write(t, filepath.Join(dir, "services/catalog/package.json"), `{"name":"catalog"}`)
	write(t, filepath.Join(dir, "services/catalog/src/index.ts"), "export const y = 1\n")
	write(t, filepath.Join(dir, "frontend/web/package.json"), `{"name":"web"}`)
	write(t, filepath.Join(dir, "frontend/web/src/app.tsx"), "export const A = 1\n")

	// Non-Node services NOT in the workspace manifest.
	write(t, filepath.Join(dir, "services/orders/requirements.txt"), "fastapi\n")
	write(t, filepath.Join(dir, "services/orders/app/db.py"), "x = 1\n")
	write(t, filepath.Join(dir, "services/analytics/analytics/main.py"), "print(1)\n") // no manifest, .py only
	write(t, filepath.Join(dir, "services/inventory/go.mod"), "module inventory\n")
	write(t, filepath.Join(dir, "services/inventory/main.go"), "package main\n")
	write(t, filepath.Join(dir, "services/notifications/build.gradle.kts"), "// kt\n")
	write(t, filepath.Join(dir, "services/notifications/src/Main.kt"), "fun main(){}\n")
	write(t, filepath.Join(dir, "services/legacy-erp/pom.xml"), "<project/>\n")
	write(t, filepath.Join(dir, "services/legacy-erp/src/Main.java"), "class M{}\n")
	write(t, filepath.Join(dir, "services/pricing/Cargo.toml"), "[package]\n")
	write(t, filepath.Join(dir, "services/pricing/src/main.rs"), "fn main(){}\n")
	write(t, filepath.Join(dir, "services/billing/composer.json"), `{}`)
	write(t, filepath.Join(dir, "services/billing/routes/web.php"), "<?php\n")
	write(t, filepath.Join(dir, "services/realtime-dashboard/mix.exs"), "defmodule M do end\n")
	// Top-level non-container module dir.
	write(t, filepath.Join(dir, "data-pipeline/dags/etl.py"), "x = 1\n")
	// libs/
	write(t, filepath.Join(dir, "libs/go-shared/go.mod"), "module shared\n")
	write(t, filepath.Join(dir, "libs/py-shared/pyproject.toml"), "[project]\n")
	// Noise that must be ignored.
	write(t, filepath.Join(dir, "node_modules/junk/index.js"), "//\n")
	write(t, filepath.Join(dir, "infra/k8s/deploy.yaml"), "kind: x\n")
	write(t, filepath.Join(dir, "vendor/carrier-sdk/main.go"), "package x\n")

	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, p := range m.Packages {
		got[p] = true
	}
	// Every polyglot service must be present, not just the Node ones.
	wantPresent := []string{
		"services/gateway", "services/catalog", "frontend/web", // Node
		"services/orders", "services/analytics", "data-pipeline", "libs/py-shared", // Python
		"services/inventory", "libs/go-shared", // Go
		"services/notifications",      // Kotlin
		"services/legacy-erp",         // Java
		"services/pricing",            // Rust
		"services/billing",            // PHP
		"services/realtime-dashboard", // Elixir
	}
	for _, w := range wantPresent {
		if !got[w] {
			t.Errorf("missing expected module %q; got %v", w, m.Packages)
		}
	}
	// Noise must NOT appear.
	for _, bad := range []string{"node_modules", "infra", "vendor", "node_modules/junk"} {
		if got[bad] {
			t.Errorf("ignored dir leaked as module: %q", bad)
		}
	}
	if m.Kind != KindPNPM {
		t.Errorf("kind: want pnpm (workspace manifest wins), got %q", m.Kind)
	}
	if len(m.Packages) < 14 {
		t.Errorf("expected >=14 modules across languages, got %d: %v", len(m.Packages), m.Packages)
	}
}

// TestDetectMonorepoPolyglotNoManifest covers a polyglot monorepo with NO
// workspace manifest at all — detection should still find all services via the
// code-dir scan and report KindPolyglot.
func TestDetectMonorepoPolyglotNoManifest(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "services/orders/app/main.py"), "x=1\n")
	write(t, filepath.Join(dir, "services/inventory/main.go"), "package main\n")
	write(t, filepath.Join(dir, "services/billing/index.php"), "<?php\n")
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindPolyglot {
		t.Fatalf("kind: want polyglot, got %q (%v)", m.Kind, m.Packages)
	}
	if len(m.Packages) != 3 {
		t.Fatalf("want 3 modules, got %d: %v", len(m.Packages), m.Packages)
	}
}

// TestDetectMonorepoRealPolyglotPlatform asserts against the REAL fixture on
// disk when present. Skipped in CI where the fixture is absent.
func TestDetectMonorepoRealPolyglotPlatform(t *testing.T) {
	const fixture = "/Users/jorgecajas/Documents/Projects/polyglot-platform"
	if _, err := os.Stat(fixture); err != nil {
		t.Skip("polyglot-platform fixture not present")
	}
	m, err := DetectMonorepo(fixture)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range m.Packages {
		got[p] = true
	}
	// Non-Node services that the pnpm-workspace.yaml does NOT list.
	mustHave := []string{
		"services/orders", "services/analytics", "services/workers", "services/order-saga",
		"services/semantic-search", "services/ledger", // Python
		"services/inventory", "services/shipping", "libs/go-shared", // Go
		"services/notifications",                           // Kotlin
		"services/legacy-erp", "services/stream-processor", // Java
		"services/pricing", "services/rate-limiter", // Rust
		"services/billing",            // PHP
		"services/realtime-dashboard", // Elixir
	}
	missing := 0
	for _, w := range mustHave {
		if !got[w] {
			t.Errorf("real fixture missing polyglot module %q", w)
			missing++
		}
	}
	if len(m.Packages) < 20 {
		t.Errorf("expected >=20 modules from polyglot-platform, got %d: %v", len(m.Packages), m.Packages)
	}
	t.Logf("polyglot-platform detected %d modules (kind=%s)", len(m.Packages), m.Kind)
}

func TestDetectMonorepoNone(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module x\n")
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindNone {
		t.Fatalf("expected KindNone, got %q", m.Kind)
	}
}

// TestDetectMonorepoPlainFrontend regresses issue #1628: a PLAIN frontend repo
// (a single package.json at the root, source split across bare top-level dirs
// like src/, components/, docs/, assets/ — NO workspace manifest, NO container
// layout) must be treated as a SINGLE unit, not split per-top-level-dir.
func TestDetectMonorepoPlainFrontend(t *testing.T) {
	dir := t.TempDir()
	// One root package.json with NO "workspaces" field.
	write(t, filepath.Join(dir, "package.json"), `{"name":"core-frontend","version":"1.0.0"}`)
	write(t, filepath.Join(dir, "src/index.tsx"), "export const A = 1\n")
	write(t, filepath.Join(dir, "src/components/Button.tsx"), "export const B = 1\n")
	write(t, filepath.Join(dir, "components/Card.tsx"), "export const C = 1\n")
	write(t, filepath.Join(dir, "docs/readme.md"), "# docs\n")
	write(t, filepath.Join(dir, "assets/images/logo.svg"), "<svg/>\n")
	write(t, filepath.Join(dir, ".windsurf/skills/x.ts"), "export const D = 1\n")

	m, err := DetectMonorepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindNone {
		t.Fatalf("plain frontend repo must be KindNone (single unit), got %q with packages %v", m.Kind, m.Packages)
	}
	if len(m.Packages) != 0 {
		t.Fatalf("plain repo must report 0 modules, got %d: %v", len(m.Packages), m.Packages)
	}
}

// TestDetectMonorepoPlainMobile regresses #1628 for a plain Go/mobile-style repo
// with bare top-level source dirs and no manifests below the root.
func TestDetectMonorepoPlainMobile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module core-mobile\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	write(t, filepath.Join(dir, "src/app.go"), "package src\n")
	write(t, filepath.Join(dir, "internal/store/store.go"), "package store\n")
	write(t, filepath.Join(dir, "ui/screen.go"), "package ui\n")
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindNone || len(m.Packages) != 0 {
		t.Fatalf("plain mobile repo must be single unit (KindNone, 0 pkgs), got %q %v", m.Kind, m.Packages)
	}
}

// TestDetectMonorepoSingleWorkspaceManifest confirms an explicit workspace
// manifest still flags a monorepo even with a single listed package — the
// author has DECLARED a workspace, so we honour it.
func TestDetectMonorepoSingleWorkspaceManifest(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - 'packages/only'\n")
	write(t, filepath.Join(dir, "packages/only/package.json"), `{"name":"only"}`)
	m, _ := DetectMonorepo(dir)
	if m.Kind != KindPNPM {
		t.Fatalf("declared workspace must be a monorepo, got %q", m.Kind)
	}
}

// TestDetectMonorepoMultipleTopLevelManifests confirms that two+ independently
// manifested top-level dirs (e.g. two go.mod packages side by side, no
// container dir) DO constitute a monorepo.
func TestDetectMonorepoMultipleTopLevelManifests(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "auth/go.mod"), "module auth\n")
	write(t, filepath.Join(dir, "auth/main.go"), "package main\n")
	write(t, filepath.Join(dir, "billing/go.mod"), "module billing\n")
	write(t, filepath.Join(dir, "billing/main.go"), "package main\n")
	m, _ := DetectMonorepo(dir)
	if m.Kind == KindNone {
		t.Fatalf("two top-level go.mod packages must be a monorepo, got KindNone")
	}
	got := map[string]bool{}
	for _, p := range m.Packages {
		got[p] = true
	}
	if !got["auth"] || !got["billing"] {
		t.Fatalf("expected auth+billing modules, got %v", m.Packages)
	}
}
