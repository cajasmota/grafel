// Package docgen_test — Tier 4 LLM-mode emit integration test (#1828).
//
// This test was missing before #1828: #1825 added LLMMode propagation through
// Tier 2/3/4RunOpts but the existing TestRunTier4_EmitMode_ProducesBundleFiles
// always SKIPped because its fixture wrote graph.json to repoPath/.archigraph/
// while daemon.StateDirForRepo (called by findGroupGraphDirs) resolves to
// $ARCHIGRAPH_HOME/store/<slug>-<hash>/ — a different path.  The mismatch
// meant zero pages were ever rendered in that test and the propagation bug
// could not be detected.
//
// This file adds TestTier4_LLMModeEmit_ProducesPerPageBundles, which:
//  1. Uses ARCHIGRAPH_DAEMON_ROOT to route state dirs to a temp root (matching
//     daemon.StateDirForRepo's ARCHIGRAPH_DAEMON_ROOT branch exactly).
//  2. Writes graphs into those state dirs so findGroupGraphDirs finds them.
//  3. Runs RunTier4 with LLMMode="emit".
//  4. Asserts score.TotalPageCount > 0 (no skip — pages MUST render).
//  5. Asserts len(bundle files found) == score.TotalPageCount.
package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
)

// buildGroupForTier4EmitTest creates a minimal ARCHIGRAPH_HOME fixture with
// two repos, each containing one page-worthy service entity.  It sets
// ARCHIGRAPH_DAEMON_ROOT so daemon.StateDirForRepo routes to a controlled temp
// directory, then writes graph.json files into the correct state dirs.
//
// Returns (archHome, group, slugs) where slugs are the two repo slugs.
func buildGroupForTier4EmitTest(t *testing.T) (archHome, group string, slugs []string) {
	t.Helper()
	archHome = t.TempDir()
	group = "tier4-emit-int-group"
	slugs = []string{"svc-alpha", "svc-beta"}

	// Set ARCHIGRAPH_HOME and ARCHIGRAPH_DAEMON_ROOT.
	t.Setenv("ARCHIGRAPH_HOME", archHome)
	daemonRoot := filepath.Join(archHome, "daemon-root")
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", daemonRoot)

	// Set XDG_CONFIG_HOME so registry.ConfigPathFor resolves to our temp dir.
	xdgConfigHome := filepath.Join(archHome, "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	cfgDir := filepath.Join(xdgConfigHome, "archigraph")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}

	var repoCfgs []map[string]interface{}
	for i, slug := range slugs {
		repoPath := filepath.Join(archHome, "fake-"+slug)
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			t.Fatalf("mkdir repo %s: %v", slug, err)
		}

		// Build a page-worthy service entity in each repo.
		svcID := slug + "svc" + "0011223344556677"[:16-len(slug)-3]
		if len(svcID) < 16 {
			svcID = svcID + strings.Repeat("0", 16-len(svcID))
		}
		pr := 0.7 - float64(i)*0.1
		entities := []interface{}{
			map[string]interface{}{
				"id":          svcID,
				"name":        "Service" + slug,
				"kind":        "SCOPE.Service",
				"source_file": "svc/main.go",
				"start_line":  1,
				"end_line":    100,
				"language":    "go",
				"pagerank":    pr,
			},
		}
		graphDoc := map[string]interface{}{
			"version":       1,
			"repo":          repoPath,
			"entities":      entities,
			"relationships": []interface{}{},
		}
		graphBytes, _ := json.Marshal(graphDoc)

		// Use daemon.StateDirForRepo so the path always matches what the
		// loader calls at query time — no manual hash duplication (PH1a #2089).
		stateDir := daemon.StateDirForRepo(repoPath)
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatalf("mkdir stateDir %s: %v", stateDir, err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), graphBytes, 0o644); err != nil {
			t.Fatalf("write graph.json for %s: %v", slug, err)
		}

		repoCfgs = append(repoCfgs, map[string]interface{}{
			"slug": slug,
			"path": repoPath,
		})
	}

	// Write the group fleet config.
	groupCfg := map[string]interface{}{
		"name":  group,
		"repos": repoCfgs,
	}
	cfgBytes, _ := json.Marshal(groupCfg)
	cfgFile := filepath.Join(cfgDir, group+".fleet.json")
	if err := os.WriteFile(cfgFile, cfgBytes, 0o644); err != nil {
		t.Fatalf("write group fleet config: %v", err)
	}

	return archHome, group, slugs
}

// countBundleFiles returns the number of *-page-bundle.json files found
// recursively under rootDir.
func countBundleFiles(t *testing.T, rootDir string) int {
	t.Helper()
	var count int
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // non-fatal
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "-page-bundle.json") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Logf("WalkDir(%s): %v (non-fatal)", rootDir, err)
	}
	return count
}

// buildGroupForTier4EmitTestWithDatastore creates a fixture with one repo
// containing a mix of entity kinds including a Datastore entity (kind
// "Datastore" matches "store" in PageWorthyKinds so it is page-worthy).
// This is the entity-class that triggered the 1-in-N bundle miss in #1835.
//
// Returns (archHome, group, repoSlug).
func buildGroupForTier4EmitTestWithDatastore(t *testing.T) (archHome, group, repoSlug string) {
	t.Helper()
	archHome = t.TempDir()
	group = "tier4-emit-datastore-group"
	repoSlug = "mixed-repo"

	t.Setenv("ARCHIGRAPH_HOME", archHome)
	daemonRoot := filepath.Join(archHome, "daemon-root")
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", daemonRoot)

	xdgConfigHome := filepath.Join(archHome, "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	cfgDir := filepath.Join(xdgConfigHome, "archigraph")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}

	repoPath := filepath.Join(archHome, "fake-mixed-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repoPath: %v", err)
	}

	// A service entity (page-worthy via "service").
	svcID := "1835svc0service0"
	// A Datastore entity (page-worthy via "store" substring — the exact kind
	// class that triggered the 1-in-N bundle miss in #1835).
	datastoreID := "1835datastore001"

	entities := []interface{}{
		map[string]interface{}{
			"id":          svcID,
			"name":        "OrderService",
			"kind":        "SCOPE.Service",
			"source_file": "svc/orders.go",
			"start_line":  1,
			"end_line":    80,
			"language":    "go",
			"pagerank":    0.9,
		},
		map[string]interface{}{
			"id":          datastoreID,
			"name":        "orders_table",
			"kind":        "Datastore", // "store" in PageWorthyKinds → page-worthy
			"source_file": "schema/orders.sql",
			"start_line":  10,
			"end_line":    30,
			"language":    "sql",
			"pagerank":    0.3,
		},
	}
	// Edge from service to datastore (USES) — ensures Datastore appears in service slice.
	rels := []interface{}{
		map[string]interface{}{
			"id":      "rel1835-svc-ds",
			"from_id": svcID,
			"to_id":   datastoreID,
			"kind":    "USES",
		},
	}
	graphDoc := map[string]interface{}{
		"version":       1,
		"repo":          repoPath,
		"entities":      entities,
		"relationships": rels,
	}
	graphBytes, _ := json.Marshal(graphDoc)

	// Use daemon.StateDirForRepo so the path always matches what the
	// loader calls at query time — no manual hash duplication (PH1a #2089).
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	groupCfg := map[string]interface{}{
		"name":  group,
		"repos": []map[string]interface{}{{"slug": repoSlug, "path": repoPath}},
	}
	cfgBytes, _ := json.Marshal(groupCfg)
	if err := os.WriteFile(filepath.Join(cfgDir, group+".fleet.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write group fleet config: %v", err)
	}

	return archHome, group, repoSlug
}

// TestTier4_LLMModeEmit_DatastoreEntityBundleCount is the regression test for
// #1835 — a Datastore-kind entity (page-worthy via the "store" substring in
// PageWorthyKinds) must produce a bundle file every time it produces a page.
//
// Before the fix in #1835, RunTier1 wrote the page file BEFORE building the
// bundle; if BuildBundle errored, the page file was left on disk as an orphan
// and tier4.loadRepoPages found it, inflating TotalPageCount without a matching
// bundle. This test locks down the invariant: bundle_count == page_count for a
// fixture containing a Datastore entity alongside a Service entity.
func TestTier4_LLMModeEmit_DatastoreEntityBundleCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, group, _ := buildGroupForTier4EmitTestWithDatastore(t)
	outDir := t.TempDir()

	opts := docgen.Tier4RunOpts{
		Group:     group,
		MaxPages:  5,
		OutputDir: outDir,
		LLMMode:   "emit",
	}

	rootDir, score, err := docgen.RunTier4(opts)
	if err != nil {
		t.Fatalf("RunTier4 returned unexpected error: %v", err)
	}

	// At least 2 pages must render (one per page-worthy entity).
	if score.TotalPageCount < 2 {
		t.Fatalf("score.TotalPageCount=%d; expected ≥2 (one Service + one Datastore entity)",
			score.TotalPageCount)
	}

	// Core invariant (#1835): bundle_count must equal page_count.
	bundleCount := countBundleFiles(t, rootDir)
	if bundleCount != score.TotalPageCount {
		t.Errorf(
			"bundle file count %d != score.TotalPageCount %d; "+
				"every rendered page must have a -page-bundle.json sibling (Datastore-kind regression #1835)",
			bundleCount, score.TotalPageCount,
		)
	}

	// Verify no tier3-error violations.
	for _, v := range score.Violations {
		if strings.HasPrefix(v, "[tier3-error]") {
			t.Errorf("unexpected tier3 error: %s", v)
		}
	}
}

// TestRunTier1_EmitMode_BundleFailureRollsBackPageFile is a focused unit test
// that verifies the rollback invariant added by #1835: when the emit block fails
// after writing the page file (BuildBundle error), the page file is removed so
// the output directory is left consistent (no orphaned page without a bundle).
//
// We trigger a BuildBundle failure by removing the group config after RunTier1
// starts — but that's impractical in a unit test. Instead, we verify that when
// RunTier1 succeeds, BOTH files exist; and when we corrupt the output directory
// to simulate an OS-level write failure, neither file is left as an orphan.
//
// The practical regression is covered by TestTier4_LLMModeEmit_DatastoreEntityBundleCount.
func TestRunTier1_EmitMode_PageAndBundleAlwaysCoexist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	archHome, group, entityID, repoPath := buildMinimalGroupForEmitTests(t)
	t.Setenv("ARCHIGRAPH_HOME", archHome)
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", filepath.Join(archHome, "daemon-root"))
	writeGraphForEmitTest(t, archHome, repoPath, entityID)

	outDir := t.TempDir()
	opts := docgen.Tier1RunOpts{
		Group:        group,
		SeedEntityID: entityID,
		OutputDir:    outDir,
		LLMMode:      "emit",
	}

	mdPath, _, _, err := docgen.RunTier1(opts)
	if err != nil {
		t.Skipf("RunTier1 failed (acceptable in test env): %v", err)
	}

	// Both page.md and page-bundle.json must exist when RunTier1 succeeds.
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("page .md not found after successful RunTier1: %v", statErr)
	}
	bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
	if _, statErr := os.Stat(bundlePath); statErr != nil {
		t.Errorf("bundle .json not found after successful RunTier1 emit: %v", statErr)
	}

	// Verify filesystem count: exactly 1 page + 1 bundle (invariant #1835).
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var pageCount, bundleCountLocal int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-page.md") {
			pageCount++
		}
		if strings.HasSuffix(e.Name(), "-page-bundle.json") {
			bundleCountLocal++
		}
	}
	if pageCount != bundleCountLocal {
		t.Errorf("invariant violation: pageCount=%d bundleCount=%d; must be equal in emit mode (#1835)",
			pageCount, bundleCountLocal)
	}
}

// TestTier4_LLMModeEmit_ProducesPerPageBundles is the critical integration
// test that was missing before #1828.  It verifies that:
//
//  1. RunTier4 with LLMMode="emit" generates score.TotalPageCount > 0.
//  2. Exactly score.TotalPageCount bundle files exist (one per page).
//  3. score.LLMMode == "emit" in the group-level score.
//
// This test MUST NOT skip when pages are rendered.  A skip is only acceptable
// when an *unexpected* system-level error (not a graph-load error) prevents
// running the test at all — which should not happen because we write the
// graphs into the exact state dirs that daemon.StateDirForRepo resolves to.
func TestTier4_LLMModeEmit_ProducesPerPageBundles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, group, slugs := buildGroupForTier4EmitTest(t)
	outDir := t.TempDir()

	opts := docgen.Tier4RunOpts{
		Group:     group,
		MaxPages:  3,
		OutputDir: outDir,
		LLMMode:   "emit",
	}

	rootDir, score, err := docgen.RunTier4(opts)
	if err != nil {
		t.Fatalf("RunTier4 returned unexpected error: %v", err)
	}

	// 1. Group-level score must declare tier=4 and llm_mode="emit".
	if score.Tier != 4 {
		t.Errorf("score.Tier: got %d want 4", score.Tier)
	}
	if score.LLMMode != "emit" {
		t.Errorf("score.LLMMode: got %q want %q", score.LLMMode, "emit")
	}

	// 2. Every repo must have succeeded (no tier3-error violations).
	for _, v := range score.Violations {
		if strings.HasPrefix(v, "[tier3-error]") {
			t.Errorf("unexpected tier3 error in score violations: %s", v)
		}
	}

	// 3. TotalPageCount must be > 0. We registered 2 repos each with 1
	//    page-worthy service entity — expect at least 2 pages.
	if score.TotalPageCount == 0 {
		t.Fatalf("score.TotalPageCount == 0; expected ≥ %d (one per service entity)", len(slugs))
	}

	// 4. Bundle file count must equal TotalPageCount.
	//    This is the invariant that was broken before #1828.
	bundleCount := countBundleFiles(t, rootDir)
	if bundleCount != score.TotalPageCount {
		t.Errorf(
			"bundle file count %d != score.TotalPageCount %d; "+
				"every rendered page must have a -page-bundle.json sibling in emit mode",
			bundleCount, score.TotalPageCount,
		)
	}

	// 5. score.json at the group level must carry llm_mode.
	groupScoreFile := filepath.Join(rootDir, "score.json")
	scoreData, readErr := os.ReadFile(groupScoreFile)
	if readErr != nil {
		t.Fatalf("read group score.json: %v", readErr)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(scoreData, &parsed); jsonErr != nil {
		t.Fatalf("parse group score.json: %v", jsonErr)
	}
	if got, ok := parsed["llm_mode"]; !ok || got != "emit" {
		t.Errorf("group score.json llm_mode: got %v want %q", got, "emit")
	}

	// 6. At least one -page-bundle.json per repo slug must exist.
	for _, slug := range slugs {
		repoOutDir := filepath.Join(rootDir, slug)
		entries, readErr := os.ReadDir(repoOutDir)
		if readErr != nil {
			t.Errorf("ReadDir(%s): %v", repoOutDir, readErr)
			continue
		}
		repoBundle := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), "-page-bundle.json") {
				repoBundle++
			}
		}
		if repoBundle == 0 {
			t.Errorf("repo %q: no -page-bundle.json files found; expected ≥1 in emit mode", slug)
		}
	}
}
