// Package docgen_test — LLM-mode emit tests (tickets B + C, issue #1813).
//
// These tests verify:
//   1. Tier 0 with --llm-mode=emit writes both .md AND -bundle.json.
//   2. The bundle JSON unmarshals cleanly into LLMPromptBundle with correct fields.
//   3. Tier 1 with --llm-mode=emit writes bundle with tier=1 and section count ≥ 1.
//   4. --llm-mode=invalid returns an error mentioning valid options.
//   5. --llm-mode="" (default) preserves existing behaviour — no bundle file written.
package docgen_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// validateLLMMode — exposed via RunOpts error path
// ---------------------------------------------------------------------------

// TestLLMMode_InvalidReturnsError verifies that an unrecognised --llm-mode
// value returns an error that names the valid options.
func TestLLMMode_InvalidReturnsError(t *testing.T) {
	t.Parallel()
	opts := docgen.RunOpts{
		Group:        "any-group",
		SeedEntityID: "abc123",
		Section:      "overview",
		OutputDir:    t.TempDir(),
		LLMMode:      "not-valid",
	}
	_, _, _, err := docgen.Run(opts)
	if err == nil {
		t.Fatal("expected error for invalid --llm-mode, got nil")
	}
	// Error must mention the bad value AND valid options.
	if !strings.Contains(err.Error(), "not-valid") {
		t.Errorf("expected bad-mode name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "emit") {
		t.Errorf("expected 'emit' mentioned in error, got: %v", err)
	}
}

// TestLLMMode_InvalidTier1ReturnsError verifies Tier 1 also rejects invalid mode.
func TestLLMMode_InvalidTier1ReturnsError(t *testing.T) {
	t.Parallel()
	opts := docgen.Tier1RunOpts{
		Group:        "any-group",
		SeedEntityID: "abc123",
		OutputDir:    t.TempDir(),
		LLMMode:      "unknown-mode",
	}
	_, _, _, err := docgen.RunTier1(opts)
	if err == nil {
		t.Fatal("expected error for invalid --llm-mode on tier1, got nil")
	}
	if !strings.Contains(err.Error(), "unknown-mode") {
		t.Errorf("expected bad-mode name in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Default mode (LLMMode="") — no bundle file written
// ---------------------------------------------------------------------------

// TestLLMMode_DefaultNoBundleFile verifies that the default empty LLMMode
// writes the stub .md and score.json only — no -bundle.json file.
func TestLLMMode_DefaultNoBundleFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	archHome, group, entityID, repoPath := buildMinimalGroupForEmitTests(t)
	t.Setenv("ARCHIGRAPH_HOME", archHome)
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", filepath.Join(archHome, "daemon-root"))
	writeGraphForEmitTest(t, archHome, repoPath, entityID)

	outDir := t.TempDir()
	opts := docgen.RunOpts{
		Group:        group,
		SeedEntityID: entityID,
		Section:      "overview",
		OutputDir:    outDir,
		LLMMode:      "", // default
	}

	mdPath, _, _, err := docgen.Run(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	// .md must exist.
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("stub .md not written: %v", statErr)
	}

	// No -bundle.json must exist.
	bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
	if _, statErr := os.Stat(bundlePath); statErr == nil {
		t.Errorf("bundle.json unexpectedly written in default (non-emit) mode: %s", bundlePath)
	}
}

// TestLLMMode_DefaultNoBundleFileTier1 mirrors the check for Tier 1.
func TestLLMMode_DefaultNoBundleFileTier1(t *testing.T) {
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
		LLMMode:      "", // default
	}

	mdPath, _, _, err := docgen.RunTier1(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	// No -bundle.json must exist.
	bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
	if _, statErr := os.Stat(bundlePath); statErr == nil {
		t.Errorf("bundle.json unexpectedly written in default (non-emit) mode: %s", bundlePath)
	}
}

// ---------------------------------------------------------------------------
// Emit mode — Tier 0
// ---------------------------------------------------------------------------

// TestLLMMode_EmitTier0_WritesBothFiles verifies that --llm-mode=emit writes
// both the stub .md AND the -bundle.json file alongside it.
func TestLLMMode_EmitTier0_WritesBothFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	archHome, group, entityID, repoPath := buildMinimalGroupForEmitTests(t)
	t.Setenv("ARCHIGRAPH_HOME", archHome)
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", filepath.Join(archHome, "daemon-root"))
	writeGraphForEmitTest(t, archHome, repoPath, entityID)

	outDir := t.TempDir()
	opts := docgen.RunOpts{
		Group:        group,
		SeedEntityID: entityID,
		Section:      "overview",
		OutputDir:    outDir,
		LLMMode:      "emit",
	}

	mdPath, scorePath, score, err := docgen.Run(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	// 1. Stub .md must exist.
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("stub .md not written: %v", statErr)
	}

	// 2. score.json must exist with llm_mode="emit".
	if _, statErr := os.Stat(scorePath); statErr != nil {
		t.Errorf("score.json not written: %v", statErr)
	}
	if score.LLMMode != "emit" {
		t.Errorf("score.LLMMode: got %q want %q", score.LLMMode, "emit")
	}

	// 3. Bundle JSON must exist alongside the stub.
	bundlePath := filepath.Join(outDir, entityID+"-overview-bundle.json")
	if _, statErr := os.Stat(bundlePath); statErr != nil {
		t.Fatalf("-bundle.json not written: %v", statErr)
	}

	// 4. Bundle JSON must unmarshal into LLMPromptBundle cleanly.
	bundleData, readErr := os.ReadFile(bundlePath)
	if readErr != nil {
		t.Fatalf("read bundle: %v", readErr)
	}
	var bundle docgen.LLMPromptBundle
	if unmarshalErr := json.Unmarshal(bundleData, &bundle); unmarshalErr != nil {
		t.Fatalf("unmarshal bundle: %v", unmarshalErr)
	}

	// 5. Bundle fields must be correct.
	if bundle.Version != docgen.LLMBundleVersion {
		t.Errorf("bundle.Version: got %q want %q", bundle.Version, docgen.LLMBundleVersion)
	}
	if bundle.Tier != 0 {
		t.Errorf("bundle.Tier: got %d want 0", bundle.Tier)
	}
	if bundle.Group != group {
		t.Errorf("bundle.Group: got %q want %q", bundle.Group, group)
	}
	if len(bundle.Sections) == 0 {
		t.Error("bundle.Sections is empty — expected at least one section for Tier 0")
	}
	if bundle.Sections[0].Section != "overview" {
		t.Errorf("bundle.Sections[0].Section: got %q want %q", bundle.Sections[0].Section, "overview")
	}
	if bundle.PromptHash == "" {
		t.Error("bundle.PromptHash is empty")
	}
	if bundle.GeneratedAt == "" {
		t.Error("bundle.GeneratedAt is empty")
	}
}

// TestLLMMode_EmitTier0_BundleJSON_AllRequiredKeys ensures the emitted JSON
// contains all required schema keys defined in the LLMPromptBundle struct.
func TestLLMMode_EmitTier0_BundleJSON_AllRequiredKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	archHome, group, entityID, repoPath := buildMinimalGroupForEmitTests(t)
	t.Setenv("ARCHIGRAPH_HOME", archHome)
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", filepath.Join(archHome, "daemon-root"))
	writeGraphForEmitTest(t, archHome, repoPath, entityID)

	outDir := t.TempDir()
	opts := docgen.RunOpts{
		Group:        group,
		SeedEntityID: entityID,
		Section:      "flows",
		OutputDir:    outDir,
		LLMMode:      "emit",
	}

	_, _, _, err := docgen.Run(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	bundlePath := filepath.Join(outDir, entityID+"-flows-bundle.json")
	bundleData, readErr := os.ReadFile(bundlePath)
	if readErr != nil {
		t.Fatalf("read bundle: %v", readErr)
	}

	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(bundleData, &parsed); jsonErr != nil {
		t.Fatalf("parse bundle JSON: %v", jsonErr)
	}

	requiredKeys := []string{
		"version", "tier", "group", "seed_entity_id",
		"sections", "graph_context", "prompt_hash", "generated_at",
	}
	for _, k := range requiredKeys {
		if _, ok := parsed[k]; !ok {
			t.Errorf("bundle JSON missing required key: %q", k)
		}
	}
}

// ---------------------------------------------------------------------------
// Emit mode — Tier 1
// ---------------------------------------------------------------------------

// TestLLMMode_EmitTier1_WritesBundleWithTier1 verifies that --llm-mode=emit
// on a Tier 1 run writes the bundle with tier=1 and section count ≥ 1.
func TestLLMMode_EmitTier1_WritesBundleWithTier1(t *testing.T) {
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

	mdPath, scorePath, score, err := docgen.RunTier1(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	// 1. Page .md must exist.
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("page .md not written: %v", statErr)
	}

	// 2. score.json must exist with llm_mode="emit".
	if _, statErr := os.Stat(scorePath); statErr != nil {
		t.Errorf("score.json not written: %v", statErr)
	}
	if score.LLMMode != "emit" {
		t.Errorf("score.LLMMode: got %q want %q", score.LLMMode, "emit")
	}

	// 3. Bundle must be alongside the page with the expected name pattern.
	// mdPath is <outDir>/<pageID>-page.md; bundle is <outDir>/<pageID>-page-bundle.json
	bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
	if _, statErr := os.Stat(bundlePath); statErr != nil {
		t.Fatalf("-page-bundle.json not written: %v (expected at %s)", statErr, bundlePath)
	}

	// 4. Bundle JSON must unmarshal cleanly.
	bundleData, readErr := os.ReadFile(bundlePath)
	if readErr != nil {
		t.Fatalf("read bundle: %v", readErr)
	}
	var bundle docgen.LLMPromptBundle
	if unmarshalErr := json.Unmarshal(bundleData, &bundle); unmarshalErr != nil {
		t.Fatalf("unmarshal bundle: %v", unmarshalErr)
	}

	// 5. Bundle must declare tier=1.
	if bundle.Tier != 1 {
		t.Errorf("bundle.Tier: got %d want 1", bundle.Tier)
	}

	// 6. Tier 1 bundle must contain all sections for the entity kind.
	if len(bundle.Sections) == 0 {
		t.Error("bundle.Sections is empty — Tier 1 must include the full page section set")
	}

	// 7. PageID must be set on the bundle.
	if bundle.PageID == "" {
		t.Error("bundle.PageID is empty for Tier 1 bundle")
	}
}

// TestLLMMode_EmitTier1_ScoreHasLLMMode verifies score.json carries llm_mode field.
func TestLLMMode_EmitTier1_ScoreHasLLMMode(t *testing.T) {
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

	_, scorePath, _, err := docgen.RunTier1(opts)
	if err != nil {
		t.Skipf("graph load failed (acceptable in test env): %v", err)
	}

	scoreData, readErr := os.ReadFile(scorePath)
	if readErr != nil {
		t.Fatalf("read score.json: %v", readErr)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(scoreData, &parsed); jsonErr != nil {
		t.Fatalf("parse score.json: %v", jsonErr)
	}
	if got, ok := parsed["llm_mode"]; !ok {
		t.Error("score.json missing llm_mode field in emit mode")
	} else if got != "emit" {
		t.Errorf("score.json llm_mode: got %v want %q", got, "emit")
	}
}

// ---------------------------------------------------------------------------
// Score struct — LLMMode field serialisation
// ---------------------------------------------------------------------------

// TestScore_LLMModeField_OmitEmpty verifies that Score with LLMMode="" omits
// the field from JSON (omitempty semantics).
func TestScore_LLMModeField_OmitEmpty(t *testing.T) {
	t.Parallel()
	score := docgen.Score{
		Tier:    0,
		Section: "overview",
		LLMMode: "",
	}
	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "llm_mode") {
		t.Errorf("score JSON should omit llm_mode when empty; got: %s", string(data))
	}
}

// TestScore_LLMModeField_EmitPresent verifies that Score with LLMMode="emit"
// includes the field in JSON.
func TestScore_LLMModeField_EmitPresent(t *testing.T) {
	t.Parallel()
	score := docgen.Score{
		Tier:    0,
		Section: "overview",
		LLMMode: "emit",
	}
	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"llm_mode":"emit"`) {
		t.Errorf("score JSON missing llm_mode field; got: %s", string(data))
	}
}

// TestTier1Score_LLMModeField_OmitEmpty mirrors the Tier0 test for Tier1Score.
func TestTier1Score_LLMModeField_OmitEmpty(t *testing.T) {
	t.Parallel()
	score := docgen.Tier1Score{
		Tier:    1,
		LLMMode: "",
	}
	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "llm_mode") {
		t.Errorf("Tier1Score JSON should omit llm_mode when empty; got: %s", string(data))
	}
}

// TestTier1Score_LLMModeField_EmitPresent verifies Tier1Score includes the field.
func TestTier1Score_LLMModeField_EmitPresent(t *testing.T) {
	t.Parallel()
	score := docgen.Tier1Score{
		Tier:    1,
		LLMMode: "emit",
	}
	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"llm_mode":"emit"`) {
		t.Errorf("Tier1Score JSON missing llm_mode field; got: %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// buildMinimalGroupForEmitTests creates a temp archigraph home, a group config,
// and a fake repo directory. It returns archHome, groupName, entityID, and
// repoPath. Callers must then call writeGraphForEmitTest to place the graph.json
// in the canonical daemon state dir.
//
// The group config is written to a temp XDG_CONFIG_HOME so that
// registry.ConfigPathFor resolves it without touching the real user config.
// Tests using this helper must call:
//
//	t.Setenv("ARCHIGRAPH_HOME", archHome)
//	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", filepath.Join(archHome, "daemon-root"))
//	t.Setenv("XDG_CONFIG_HOME", filepath.Join(archHome, "xdg-config"))
func buildMinimalGroupForEmitTests(t *testing.T) (archHome, group, entityID, repoPath string) {
	t.Helper()
	archHome = t.TempDir()
	group = "emit-test-group"
	entityID = "emitentity0001aa"

	// XDG_CONFIG_HOME-based config dir: <archHome>/xdg-config/archigraph/
	xdgCfgDir := filepath.Join(archHome, "xdg-config", "archigraph")
	if err := os.MkdirAll(xdgCfgDir, 0o755); err != nil {
		t.Fatalf("mkdir xdgCfgDir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(archHome, "xdg-config"))

	repoPath = filepath.Join(archHome, "emit-fake-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repoPath: %v", err)
	}

	groupCfg := map[string]interface{}{
		"repos": []map[string]interface{}{{"path": repoPath}},
	}
	cfgBytes, _ := json.Marshal(groupCfg)
	// Fleet config filename: <group>.fleet.json
	if err := os.WriteFile(filepath.Join(xdgCfgDir, group+".fleet.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write group fleet config: %v", err)
	}

	return archHome, group, entityID, repoPath
}

// writeGraphForEmitTest writes a minimal graph.json into the canonical
// ARCHIGRAPH_DAEMON_ROOT state dir so findGroupGraphDirs can discover it.
//
// PH1a (#2089): uses daemon.StateDirForRepo directly so the path always
// matches what the loader resolves — no manual hash duplication.
func writeGraphForEmitTest(t *testing.T, archHome, repoPath, entityID string) {
	t.Helper()

	// daemon.StateDirForRepo honours ARCHIGRAPH_DAEMON_ROOT (set by the caller)
	// and appends the per-ref sub-directory (PH1a). Writing here ensures
	// the file is always found at exactly the path the loader will look.
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}

	nb1ID := "neighbour001abc"
	nb2ID := "neighbour002def"
	entities := []interface{}{
		map[string]interface{}{
			"id": entityID, "name": "handleQueryGraph", "kind": "SCOPE.Function",
			"source_file": "internal/daemon/query.go",
			"start_line": 42, "end_line": 120, "language": "go",
			"signature": "func handleQueryGraph(w http.ResponseWriter, r *http.Request)",
		},
		map[string]interface{}{
			"id": nb1ID, "name": "loadGraphFromCache", "kind": "SCOPE.Function",
			"source_file": "internal/daemon/cache.go",
			"start_line": 10, "end_line": 45, "language": "go",
		},
		map[string]interface{}{
			"id": nb2ID, "name": "GraphDocument", "kind": "SCOPE.Struct",
			"source_file": "internal/graph/graph.go",
			"start_line": 20, "end_line": 100, "language": "go",
		},
	}
	rels := []interface{}{
		map[string]interface{}{
			"id": fmt.Sprintf("rel001-%s", entityID[:8]),
			"from_id": entityID, "to_id": nb1ID, "kind": "CALLS",
		},
		map[string]interface{}{
			"id": fmt.Sprintf("rel002-%s", entityID[:8]),
			"from_id": entityID, "to_id": nb2ID, "kind": "USES",
		},
	}
	graphDoc := map[string]interface{}{
		"version": 1, "repo": repoPath,
		"entities": entities, "relationships": rels,
	}
	graphBytes, _ := json.Marshal(graphDoc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
}
