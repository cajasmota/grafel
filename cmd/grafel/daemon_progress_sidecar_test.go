package main

// daemon_progress_sidecar_test.go — split-mode progress bridge, WRITE side
// (ADR-0024 / epic #5729). Asserts daemonRebuildFuncCore tees the progress
// broker into a per-group NDJSON sidecar ONLY in split mode, and leaves the
// monolith path byte-for-byte unchanged (no progress/ files written).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/progress"
)

// progressDir returns GRAFEL_HOME/progress and whether it exists with any
// .ndjson sidecar inside.
func progressSidecarFiles(t *testing.T) []string {
	t.Helper()
	home := os.Getenv("GRAFEL_HOME")
	dir := filepath.Join(home, "progress")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".ndjson" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// TestRebuild_MonolithWritesNoSidecar asserts that with split mode DISABLED the
// rebuild path creates NO progress sidecar files — monolith behavior is
// byte-for-byte unchanged.
func TestRebuild_MonolithWritesNoSidecar(t *testing.T) {
	t.Setenv(daemon.SplitModeEnvVar, "0") // escape hatch: monolith
	group := setupTestGroup(t, "mono-group", []string{"r1", "r2"})

	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error { return nil }

	if _, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if files := progressSidecarFiles(t); len(files) != 0 {
		t.Errorf("monolith rebuild wrote %d progress sidecar file(s), want 0: %v", len(files), files)
	}
}

// TestRebuild_SplitModeWritesSidecar asserts that with split mode ENABLED the
// rebuild tees the broker into a per-group sidecar carrying the group-scoped
// terminal (the link tracker's Done), so serve's tailer can bridge it.
func TestRebuild_SplitModeWritesSidecar(t *testing.T) {
	t.Setenv(daemon.SplitModeEnvVar, "1") // split ON
	group := setupTestGroup(t, "split-group", []string{"r1", "r2"})

	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error { return nil }

	if _, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// Exactly one sidecar (this group) must exist at the deterministic path.
	path, err := progress.SidecarPath(group)
	if err != nil {
		t.Fatalf("SidecarPath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sidecar at %s: %v", path, err)
	}

	// Read it back: the group-scoped terminal (RepoSlug == group, done) from the
	// link tracker must be present, proving the group link tracker flows through
	// the tee.
	r, err := progress.NewSidecarReader(group)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	events, err := r.ReadAll()
	if err != nil {
		t.Fatalf("readall: %v", err)
	}
	var sawGroupTerminal bool
	for _, e := range events {
		if e.GroupSlug == group && e.RepoSlug == group && e.Phase == progress.PhaseDone {
			sawGroupTerminal = true
		}
	}
	if !sawGroupTerminal {
		t.Errorf("group-scoped terminal not found in sidecar; events=%+v", events)
	}
}
