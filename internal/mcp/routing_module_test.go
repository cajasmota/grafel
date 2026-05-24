// routing_module_test.go — M3 (#2180) module-slug resolution tests.
//
// Verifies that CWDResolution.ModuleSlug is populated when cwd is inside
// a declared module sub-path of a registered monorepo repo.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeRegistryAndFleet writes a minimal registry.json + fleet config in tmp.
// Returns the registry path and a ready *State.
// The fleet config has one group with one repo that has nModules modules.
func writeRegistryAndFleet(t *testing.T, tmp, groupName, repoSlug, repoPath string, moduleSubPaths []string) (*State, string) {
	t.Helper()

	// Write fleet config.
	type repoEntry struct {
		Slug    string   `json:"slug"`
		Path    string   `json:"path"`
		Modules []string `json:"modules,omitempty"`
	}
	type fleetCfg struct {
		Name  string      `json:"name"`
		Repos []repoEntry `json:"repos"`
	}
	cfg := fleetCfg{
		Name:  groupName,
		Repos: []repoEntry{{Slug: repoSlug, Path: repoPath, Modules: moduleSubPaths}},
	}
	fleetData, _ := json.Marshal(cfg)
	fleetPath := filepath.Join(tmp, groupName+".fleet.json")
	if err := os.WriteFile(fleetPath, fleetData, 0o644); err != nil {
		t.Fatalf("write fleet: %v", err)
	}

	// Write registry.json (CLI array format).
	type groupRef struct {
		Name       string `json:"name"`
		ConfigPath string `json:"config_path"`
	}
	type regShape struct {
		Version int        `json:"version"`
		Groups  []groupRef `json:"groups"`
	}
	reg := regShape{Version: 1, Groups: []groupRef{{Name: groupName, ConfigPath: fleetPath}}}
	regData, _ := json.Marshal(reg)
	regPath := filepath.Join(tmp, "registry.json")
	if err := os.WriteFile(regPath, regData, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	// Build in-memory Registry (mcp.Registry) that points to the repo path.
	inMemReg := &Registry{
		Path: regPath,
		Groups: map[string]RegistryGroup{
			groupName: {
				Repos: map[string]RegistryRepo{
					repoSlug: {Path: repoPath},
				},
			},
		},
	}
	return NewState(inMemReg), regPath
}

// TestModuleSlugForCWD_InsideModule verifies that moduleSlugForCWD returns
// the correct module sub-path when cwd is inside a module directory.
func TestModuleSlugForCWD_InsideModule(t *testing.T) {
	tmp := t.TempDir()

	repoPath := filepath.Join(tmp, "platform")
	moduleSubPaths := []string{"services/payments", "services/orders", "services/auth"}
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	st, _ := writeRegistryAndFleet(t, tmp, "client-fixture-x", "platform", repoPath, moduleSubPaths)

	// CWD inside services/payments/api/handlers
	cwdInsidePayments := filepath.Join(repoPath, "services", "payments", "api", "handlers")
	if err := os.MkdirAll(cwdInsidePayments, 0o755); err != nil {
		t.Fatal(err)
	}

	got := moduleSlugForCWD(st, "client-fixture-x", "platform", repoPath, cwdInsidePayments)
	if got != "services/payments" {
		t.Errorf("moduleSlugForCWD: want %q, got %q", "services/payments", got)
	}
}

// TestModuleSlugForCWD_AtModuleRoot verifies that being at the exact module
// root path also resolves correctly.
func TestModuleSlugForCWD_AtModuleRoot(t *testing.T) {
	tmp := t.TempDir()

	repoPath := filepath.Join(tmp, "platform")
	moduleSubPaths := []string{"orders", "payments"}
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	st, _ := writeRegistryAndFleet(t, tmp, "client-fixture-x", "platform", repoPath, moduleSubPaths)

	// CWD exactly at the orders module root.
	ordersRoot := filepath.Join(repoPath, "orders")
	if err := os.MkdirAll(ordersRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	got := moduleSlugForCWD(st, "client-fixture-x", "platform", repoPath, ordersRoot)
	if got != "orders" {
		t.Errorf("moduleSlugForCWD: want %q, got %q", "orders", got)
	}
}

// TestModuleSlugForCWD_OutsideModule verifies that being at the repo root
// (but not inside any module) returns "".
func TestModuleSlugForCWD_OutsideModule(t *testing.T) {
	tmp := t.TempDir()

	repoPath := filepath.Join(tmp, "platform")
	moduleSubPaths := []string{"services/payments", "services/orders"}
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	st, _ := writeRegistryAndFleet(t, tmp, "client-fixture-x", "platform", repoPath, moduleSubPaths)

	// CWD at the repo root — not inside any module.
	got := moduleSlugForCWD(st, "client-fixture-x", "platform", repoPath, repoPath)
	if got != "" {
		t.Errorf("moduleSlugForCWD at repo root: want %q, got %q", "", got)
	}
}

// TestModuleSlugForCWD_NoModules verifies that a repo with no declared
// modules returns "" without panicking.
func TestModuleSlugForCWD_NoModules(t *testing.T) {
	tmp := t.TempDir()

	repoPath := filepath.Join(tmp, "standalone")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write fleet with no modules.
	st, _ := writeRegistryAndFleet(t, tmp, "client-fixture-y", "standalone", repoPath, nil)

	cwdInside := filepath.Join(repoPath, "src")
	if err := os.MkdirAll(cwdInside, 0o755); err != nil {
		t.Fatal(err)
	}

	got := moduleSlugForCWD(st, "client-fixture-y", "standalone", repoPath, cwdInside)
	if got != "" {
		t.Errorf("moduleSlugForCWD (no modules): want %q, got %q", "", got)
	}
}

// TestModuleSlugForCWD_LongestPrefixWins verifies that when two module paths
// are nested (e.g. "a" and "a/b"), the more specific one wins.
func TestModuleSlugForCWD_LongestPrefixWins(t *testing.T) {
	tmp := t.TempDir()

	repoPath := filepath.Join(tmp, "platform")
	// "shared" is a prefix of "shared/core" — we want the longer match.
	moduleSubPaths := []string{"shared", "shared/core"}
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	st, _ := writeRegistryAndFleet(t, tmp, "client-fixture-x", "platform", repoPath, moduleSubPaths)

	cwdInsideCore := filepath.Join(repoPath, "shared", "core", "utils")
	if err := os.MkdirAll(cwdInsideCore, 0o755); err != nil {
		t.Fatal(err)
	}

	got := moduleSlugForCWD(st, "client-fixture-x", "platform", repoPath, cwdInsideCore)
	if got != "shared/core" {
		t.Errorf("moduleSlugForCWD (longest prefix): want %q, got %q", "shared/core", got)
	}
}
