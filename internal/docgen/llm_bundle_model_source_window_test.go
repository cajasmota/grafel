package docgen_test

// llm_bundle_model_source_window_test.go — tests for the Model whole-body
// source_window strategy introduced in #1876.
//
// For Model entities the source_window must cover the ENTIRE class body
// (start_line..end_line) so that field declarations, associations, and inner
// Meta classes are all visible to the LLM.  The default ±20-line strategy
// clips mid-class and loses all field definitions.
//
// Test surface:
//   - Model entity: source_window contains lines beyond start+20 (whole body).
//   - Non-Model entity (Function): source_window uses the default ±20 strategy.
//   - Oversized Model (> SourceWindowWholeBodyMaxLines): clipped + annotation.
//   - Model with end_line=0 (sentinel bug, #1868 not yet fixed): falls back
//     gracefully to default window; must not crash or produce empty window.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// modelSourceWindowHarness creates an isolated environment with a single
// entity of the given kind and a source file whose body spans bodyLines lines
// (starting at srcStartLine within the file).
//
// Returns the group name, entity ID, and the source file content so the
// caller can assert specific lines appear in source_window.
func modelSourceWindowHarness(
	t *testing.T,
	kind string,
	srcStartLine, srcEndLine int,
	bodyLines int,
) (groupName, entityID string, srcContent string) {
	t.Helper()

	tmp := t.TempDir()

	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "testrepo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = fmt.Sprintf("model-sw-test-%s", kind)

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "testrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Build a Python-like source file with padding before the class start and
	// bodyLines field declarations inside the class body.
	var sb strings.Builder
	// Write padding lines before the class to fill lines 1..(srcStartLine-1).
	for i := 1; i < srcStartLine; i++ {
		sb.WriteString(fmt.Sprintf("# padding line %d\n", i))
	}
	// Class declaration at srcStartLine.
	sb.WriteString(fmt.Sprintf("class %sModel(models.Model):\n", kind))
	// Field declarations filling the class body.
	for f := 1; f <= bodyLines; f++ {
		sb.WriteString(fmt.Sprintf("    field_%d = models.CharField(max_length=%d)\n", f, f*10))
	}
	// Ensure the file has at least srcEndLine lines.
	currentLine := srcStartLine + 1 + bodyLines
	for currentLine <= srcEndLine {
		sb.WriteString(fmt.Sprintf("# tail line %d\n", currentLine))
		currentLine++
	}
	srcContent = sb.String()

	srcRelPath := "models/mymodel.py"
	srcAbsPath := filepath.Join(repoPath, srcRelPath)
	if err := os.MkdirAll(filepath.Dir(srcAbsPath), 0o755); err != nil {
		t.Fatalf("mkdir src dir: %v", err)
	}
	if err := os.WriteFile(srcAbsPath, []byte(srcContent), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	entityID = "cafebabe00001876"
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 1, Entities: 1, Relationships: 0},
		Entities: []graph.Entity{
			{
				ID:         entityID,
				Name:       kind + "Model",
				Kind:       kind,
				SourceFile: srcRelPath,
				StartLine:  srcStartLine,
				EndLine:    srcEndLine,
				Language:   "python",
			},
		},
		Relationships: []graph.Relationship{},
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, entityID, srcContent
}

// buildBundleForHarness is a thin helper that calls BuildBundle with sane
// defaults and returns the source_window string.
func buildBundleForHarness(t *testing.T, groupName, entityID string) string {
	t.Helper()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}
	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	return bundle.GraphContext.SourceWindow
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestModelSourceWindow_WholeBody verifies that for a Model entity the
// source_window covers the entire class body and includes field declarations
// beyond the first ±20 lines.
func TestModelSourceWindow_WholeBody(t *testing.T) {
	// Class starts at line 5 and has 60 field declarations, so end_line = 5+60 = 65.
	// The default ±20 strategy would emit lines 1..45 — missing fields 41-60.
	// The whole-body strategy must emit lines 5..65, covering all 60 fields.
	const startLine = 5
	const bodyLines = 60
	const endLine = startLine + bodyLines // 65

	groupName, entityID, _ := modelSourceWindowHarness(t, "Model", startLine, endLine, bodyLines)
	sw := buildBundleForHarness(t, groupName, entityID)

	if sw == "" {
		t.Fatal("source_window is empty for Model entity")
	}

	// The last field (field_60) must appear in the window.
	lastField := fmt.Sprintf("field_%d", bodyLines)
	if !strings.Contains(sw, lastField) {
		t.Errorf("source_window missing %q — whole-body strategy must include all fields\nwindow:\n%s", lastField, sw)
	}

	// The class declaration line must appear.
	if !strings.Contains(sw, "class ModelModel") {
		t.Errorf("source_window missing class declaration\nwindow:\n%s", sw)
	}

	t.Logf("Model whole-body source_window (%d chars, %d lines):\n%s",
		len(sw), strings.Count(sw, "\n"), sw)
}

// TestModelSourceWindow_NonModelUsesDefault verifies that a Function entity
// still uses the default ±20-line strategy and does NOT include lines far
// beyond its start.
func TestModelSourceWindow_NonModelUsesDefault(t *testing.T) {
	// Function starts at line 5 and has 60 lines, end_line=65.
	// Default strategy emits start-20..end+20 = 1..85.
	// But only bodyLines field lines exist — this is just a sanity check that
	// the window is non-empty and the default strategy is selected (no truncation
	// annotation should appear for a non-oversized function).
	const startLine = 5
	const bodyLines = 10
	const endLine = startLine + bodyLines

	groupName, entityID, _ := modelSourceWindowHarness(t, "Function", startLine, endLine, bodyLines)
	sw := buildBundleForHarness(t, groupName, entityID)

	if sw == "" {
		t.Fatal("source_window is empty for Function entity")
	}

	// The truncation annotation must NOT appear for a non-Model that fits.
	if strings.Contains(sw, "truncated_at_line") {
		t.Errorf("truncated_at_line annotation present in non-Model window — only Model whole-body should emit it\nwindow:\n%s", sw)
	}

	t.Logf("Function (default strategy) source_window (%d chars):\n%s", len(sw), sw)
}

// TestModelSourceWindow_OversizedModelClipped verifies that when a Model body
// exceeds SourceWindowWholeBodyMaxLines the window is clipped at the cap and
// the truncated_at_line annotation is appended.
func TestModelSourceWindow_OversizedModelClipped(t *testing.T) {
	// Build a model with more than SourceWindowWholeBodyMaxLines fields.
	cap := docgen.SourceWindowWholeBodyMaxLines
	const startLine = 1
	bodyLines := cap + 50 // well over the cap
	endLine := startLine + bodyLines

	groupName, entityID, _ := modelSourceWindowHarness(t, "Model", startLine, endLine, bodyLines)
	sw := buildBundleForHarness(t, groupName, entityID)

	if sw == "" {
		t.Fatal("source_window is empty for oversized Model entity")
	}

	// Truncation annotation must be present.
	if !strings.Contains(sw, "truncated_at_line") {
		t.Errorf("expected truncated_at_line annotation for oversized Model (body=%d lines, cap=%d)\nwindow:\n%s",
			bodyLines, cap, sw)
	}

	// The field just beyond the cap must NOT appear (it was clipped).
	beyondCapField := fmt.Sprintf("field_%d", cap+10)
	if strings.Contains(sw, beyondCapField) {
		t.Errorf("source_window contains %q which is beyond the %d-line cap — clipping did not work\nwindow:\n%s",
			beyondCapField, cap, sw)
	}

	t.Logf("Oversized Model clipped source_window (%d chars):\n%s", len(sw), sw)
}

// TestModelSourceWindow_EndLineZeroFallback verifies that when a Model entity
// has end_line=0 (the #1868 sentinel bug) the whole-body strategy falls back
// to the default window and does not crash or return empty.
func TestModelSourceWindow_EndLineZeroFallback(t *testing.T) {
	// Use end_line=0 to simulate the sentinel.
	const startLine = 5
	const endLine = 0 // sentinel — end_line not populated
	const bodyLines = 5

	groupName, entityID, _ := modelSourceWindowHarness(t, "Model", startLine, endLine, bodyLines)
	sw := buildBundleForHarness(t, groupName, entityID)

	// Must be non-empty — the fallback ±20-line window applies.
	if sw == "" {
		t.Error("source_window is empty for Model with end_line=0 — fallback must produce a non-empty window")
	}

	// No crash, no truncation annotation expected in fallback path.
	if strings.Contains(sw, "truncated_at_line") {
		t.Errorf("unexpected truncated_at_line annotation in end_line=0 fallback window\nwindow:\n%s", sw)
	}

	t.Logf("Model end_line=0 fallback source_window (%d chars):\n%s", len(sw), sw)
}
