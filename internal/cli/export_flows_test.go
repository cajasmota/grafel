package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/registry"
)

// setupExportFlowGroup writes a group whose repo has a BAKED intra-repo
// SCOPE.Process flow in graph.json, and returns the group name + the repo state
// dir (so the caller can drop a flows.json sidecar).
func setupExportFlowGroup(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	xdg := t.TempDir()
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(daemon.EnvRoot, root)

	const group = "expflow"
	repoPath := filepath.Join(root, "repoA")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := graph.Document{
		Version: graph.SchemaVersion, Repo: "repoA",
		Entities: []graph.Entity{
			{ID: "fn1", Name: "handleSubmit", Kind: "Function", SourceFile: "a.go", StartLine: 1},
			{ID: "fn2", Name: "callService", Kind: "Function", SourceFile: "a.go", StartLine: 6},
			graph.Entity{ID: "baked-proc", Name: "BakedIntraFlow", Kind: "SCOPE.Process", SourceFile: "a.go", StartLine: 1}.
				WithProperties(map[string]string{"step_count": "2", "cross_stack": "false"}),
		},
		Relationships: []graph.Relationship{
			{ID: "c1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
			graph.Relationship{ID: "s1", FromID: "baked-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
			graph.Relationship{ID: "s2", FromID: "baked-proc", ToID: "fn2", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "1"}),
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: "repoA", Path: repoPath, Stack: registry.StackList{"go"}}}}
	if err := registry.SaveGroupConfig(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	return group, stateDir
}

// TestExport_FlowSidecarReplacesBakedFlow: the offline export (a primary
// debugging surface) must carry the CROSS-REPO-AWARE flow from the flow
// side-table, REPLACING the baked intra flow — not doubling it (#5904 PR-b).
func TestExport_FlowSidecarReplacesBakedFlow(t *testing.T) {
	group, stateDir := setupExportFlowGroup(t)

	// Drop a fresh flow sidecar: a cross-repo SCOPE.Process replacing the baked one.
	ents := []graph.Entity{
		graph.Entity{ID: "xrepo-proc", Name: "CrossRepoFlow", Kind: "SCOPE.Process", SourceFile: "a.go", StartLine: 1}.
			WithProperties(map[string]string{"step_count": "3", "cross_stack": "true"}),
	}
	rels := []graph.Relationship{
		graph.Relationship{ID: "xs0", FromID: "xrepo-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
		graph.Relationship{ID: "ph1", FromID: "fn2", ToID: "remote::ep", Kind: "CALLS"}.WithProperties(map[string]string{"cross_repo": "true", "target_repo": "remote"}),
	}
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert flows sidecar: %v", err)
	}

	out, err := runExport(t, group, "cypher")
	if err != nil {
		t.Fatalf("export cypher: %v", err)
	}
	if !strings.Contains(out, "CrossRepoFlow") {
		t.Errorf("export missing the cross-repo-aware flow: %s", out)
	}
	if strings.Contains(out, "BakedIntraFlow") {
		t.Errorf("export leaked the baked intra flow (REPLACE violated / double-count): %s", out)
	}
}

// TestExport_NoSidecar_KeepsBakedFlow: with no sidecar the export shows the
// baked intra flow exactly as today (single-repo parity / graceful degradation).
func TestExport_NoSidecar_KeepsBakedFlow(t *testing.T) {
	group, _ := setupExportFlowGroup(t)
	out, err := runExport(t, group, "cypher")
	if err != nil {
		t.Fatalf("export cypher: %v", err)
	}
	if !strings.Contains(out, "BakedIntraFlow") {
		t.Errorf("no-sidecar export must keep the baked intra flow: %s", out)
	}
}
