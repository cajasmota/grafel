// Regression tests for issue #6178 round 3: loadDAGEffectsSidecar
// (v2_paths_downstream_dag.go), dataFlowSidecarPath and taintSidecarPath
// (handlers_dataflow.go), groupMemoryDir (handlers_repairs.go), and
// groupPatternsDir (handlers_patterns.go) each hand-rolled their own
// os.UserHomeDir()/os.Getenv("HOME") join and never consulted GRAFEL_HOME.
// They now delegate to links.PassSidecarPath/MemoryDir/PatternsDir, the
// shared derivation; these tests pin that delegation at each call site
// independently.
package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withDivergentHome6178 sets HOME/USERPROFILE and GRAFEL_HOME to two
// DIFFERENT temp dirs — the shape a real isolated run uses (only
// GRAFEL_HOME set) — and returns (sandboxHome, grafelHome). Suffixed
// "6178" to avoid colliding with any package-local withSandboxHome-style
// helper this package may already define for other purposes.
func withDivergentHome6178(t *testing.T) (string, string) {
	t.Helper()
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	return sandboxHome, grafelHome
}

func TestDataFlowSidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome6178(t)
	got := dataFlowSidecarPath("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links-data-flow.json")
	if got != want {
		t.Fatalf("dataFlowSidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-data-flow.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestTaintSidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome6178(t)
	got := taintSidecarPath("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links-taint.json")
	if got != want {
		t.Fatalf("taintSidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-taint.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestLoadDAGEffectsSidecar_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome6178(t)

	// Correct location: under GRAFEL_HOME.
	correctDir := filepath.Join(grafelHome, "groups")
	if err := os.MkdirAll(correctDir, 0o755); err != nil {
		t.Fatal(err)
	}
	correctDoc := `{"version":1,"method":"effect_propagation","entries":[{"entity_id":"repo::a","effects":["db_write"]}]}`
	if err := os.WriteFile(filepath.Join(correctDir, "g6178-links-effects.json"), []byte(correctDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Decoy at the HOME-derived fallback, different content.
	decoyDir := filepath.Join(sandboxHome, ".grafel", "groups")
	if err := os.MkdirAll(decoyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decoyDoc := `{"version":1,"method":"effect_propagation","entries":[{"entity_id":"decoy::x","effects":["decoy"]}]}`
	if err := os.WriteFile(filepath.Join(decoyDir, "g6178-links-effects.json"), []byte(decoyDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadDAGEffectsSidecar("g6178")
	if got == nil {
		t.Fatal("loadDAGEffectsSidecar returned nil, want the GRAFEL_HOME entries")
	}
	effs, ok := got["repo::a"]
	if !ok {
		t.Fatalf("regression: got %+v, want entry for repo::a (the GRAFEL_HOME doc) — looks like the HOME-derived decoy was read instead", got)
	}
	if len(effs) != 1 || effs[0] != "db_write" {
		t.Fatalf("repo::a effects = %v, want [db_write]", effs)
	}
	if _, decoyPresent := got["decoy::x"]; decoyPresent {
		t.Fatal("regression: decoy entry present — read the HOME-derived fallback file")
	}
}

func TestGroupMemoryDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome6178(t)
	got := groupMemoryDir("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-memory")
	if got != want {
		t.Fatalf("groupMemoryDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestGroupPatternsDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome6178(t)
	got := groupPatternsDir("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-patterns")
	if got != want {
		t.Fatalf("groupPatternsDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-patterns")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

// TestHandleListFindings_WarnsOnRealHomeOrphan_6178 exercises the migration
// warning added in handleListFindings: when GRAFEL_HOME diverges from the
// legacy HOME-derived path, findings live at the legacy path, and the
// GRAFEL_HOME path has none, the request must still succeed (empty
// findings, not an error) — the warning is a log line, not a behavior
// change to the response.
func TestHandleListFindings_EmptyWhenOrphaned_6178(t *testing.T) {
	sandboxHome, _ := withDivergentHome6178(t)
	legacyDir := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	finding := map[string]any{"id": "f1", "text": "orphaned finding"}
	buf, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "f1.json"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
	// groupMemoryDir now resolves under GRAFEL_HOME, which has nothing —
	// readFindingFiles must return an empty (non-nil-panicking) result.
	got := readFindingFiles(groupMemoryDir("g6178"))
	if len(got) != 0 {
		t.Fatalf("expected 0 findings under the (empty) GRAFEL_HOME memory dir, got %d", len(got))
	}
}
