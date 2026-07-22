package install_test

// update_reindex_test.go — #5907 FIX4: proves `grafel update` surfaces
// (report-only) the count of registered repos that need a reindex after a
// format upgrade, without itself enqueueing anything (the engine's own
// loop-guarded stale-reindex arm, internal/daemon/stale_reindex.go, owns
// that).

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeOldFormatGraphFBForUpdate mirrors internal/graph/reindex_required_test.go's
// writeOldFormatGraphFB: builds a minimal valid graph.fb via fbwriter.Marshal,
// then patches the on-disk Graph.version scalar down to oldVersion.
func writeOldFormatGraphFBForUpdate(t *testing.T, dir string, oldVersion int) {
	t.Helper()
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-old-version",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
		},
	}
	doc.Stats.Entities = 1
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root := fb.GetRootAsGraph(buf, 0)
	if !root.MutateVersion(int32(oldVersion)) {
		t.Fatalf("MutateVersion(%d) returned false — slot missing?", oldVersion)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), buf, 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// registerFakeGroupWithRepo writes a minimal registry.json + group config so
// registry.Groups()/registry.LoadGroupConfig resolve one repo, using the SAME
// GRAFEL_HOME/XDG_CONFIG_HOME env vars newTestEnv already set.
func registerFakeGroupWithRepo(t *testing.T, group, repoPath string) {
	t.Helper()
	home, err := registry.HomeDir()
	if err != nil {
		t.Fatalf("registry.HomeDir: %v", err)
	}
	cfgDir, err := registry.ConfigDir()
	if err != nil {
		t.Fatalf("registry.ConfigDir: %v", err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: "repo", Path: repoPath}}}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := registry.Registry{Version: 1, Groups: []registry.GroupRef{{Name: group, ConfigPath: cfgPath}}}
	regData, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regData, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunUpdate_ReportsReposNeedingReindex proves RunUpdate's
// ReposNeedingReindex counts a registered repo whose on-disk graph.fb is
// stale-format, without enqueueing a reindex itself (there is no reindex
// request plumbing wired into RunUpdate — a passing test here that doesn't
// hang/error already demonstrates no side channel was invoked).
func TestRunUpdate_ReportsReposNeedingReindex(t *testing.T) {
	env := newTestEnv(t)

	repo := filepath.Join(t.TempDir(), "stale-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repo)
	writeOldFormatGraphFBForUpdate(t, stateDir, 2)
	registerFakeGroupWithRepo(t, "demo", repo)

	newContent := []byte("#!/bin/sh\necho new-grafel")
	newBinPath := filepath.Join(t.TempDir(), "new-grafel")
	if err := os.WriteFile(newBinPath, newContent, 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.2-test",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			return copyTestFile(newBinPath, destPath)
		},
	}

	result, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if result.ReposNeedingReindex != 1 {
		t.Errorf("ReposNeedingReindex = %d, want 1", result.ReposNeedingReindex)
	}
}

// TestRunUpdate_NoReposNeedingReindex_WhenCurrent is the regression guard: a
// registered repo on the current graph.fb format is never counted.
func TestRunUpdate_NoReposNeedingReindex_WhenCurrent(t *testing.T) {
	env := newTestEnv(t)

	repo := filepath.Join(t.TempDir(), "current-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repo)
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Repo:        "fixture-current-version",
		Entities: []graph.Entity{
			{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"},
		},
	}
	doc.Stats.Entities = 1
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	registerFakeGroupWithRepo(t, "demo", repo)

	newContent := []byte("#!/bin/sh\necho new-grafel")
	newBinPath := filepath.Join(t.TempDir(), "new-grafel")
	if err := os.WriteFile(newBinPath, newContent, 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.3-test",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			return copyTestFile(newBinPath, destPath)
		},
	}

	result, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if result.ReposNeedingReindex != 0 {
		t.Errorf("ReposNeedingReindex = %d, want 0 for a current-format repo", result.ReposNeedingReindex)
	}
}
