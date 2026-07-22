package install

// reindex_required_test.go — #5907 FIX4: proves `grafel doctor` REPORTS
// (never enqueues) when a registered repo's on-disk graph.fb was written by
// an older grafel build than this binary supports. The enqueue itself is
// already owned by the engine's loop-guarded stale-reindex arm
// (internal/daemon/stale_reindex.go) — this check is purely a visibility
// surface so the auto-reindex isn't silent in doctor output either.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeOldFormatGraphFBAt mirrors internal/graph/reindex_required_test.go's
// writeOldFormatGraphFB: builds a minimal valid graph.fb via fbwriter.Marshal,
// then patches the on-disk Graph.version scalar down to oldVersion so the
// test can fabricate a stale-format graph.fb without an actual old binary.
func writeOldFormatGraphFBAt(t *testing.T, dir string, oldVersion int) {
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

// TestCheckReindexRequired_StaleFormat_Reports proves doctor reports a stale
// repo's format mismatch, naming the repo path and both format versions, and
// that the summary line names the affected repo count.
func TestCheckReindexRequired_StaleFormat_Reports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	stateDir := daemon.StateDirForRepo(repo)
	writeOldFormatGraphFBAt(t, stateDir, 2)

	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "repo", Path: repo}},
	}
	groupsFn, loadFn := fakeGroups(cfg)

	opts := DoctorOptions{groupsFn: groupsFn, loadGroupFn: loadFn}
	cr := checkReindexRequired(opts)
	if cr == nil {
		t.Fatal("expected a non-nil CheckResult for a stale-format repo")
	}
	if cr.OK {
		t.Error("expected OK=false")
	}
	if cr.Severity != SeverityInfo {
		t.Errorf("severity = %v, want %v (report-only, engine already fixes it)", cr.Severity, SeverityInfo)
	}
	if len(cr.Drift) < 2 {
		t.Fatalf("expected at least a summary line + one per-repo line, got %v", cr.Drift)
	}
	if got := cr.Drift[0]; got != "1 repo(s) need reindex after a format upgrade" {
		t.Errorf("summary = %q, want %q", got, "1 repo(s) need reindex after a format upgrade")
	}
	found := false
	for _, d := range cr.Drift[1:] {
		if d == repo+": "+mustReindexReason(t, stateDir) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a drift line naming repo %s with the reindex reason, got %v", repo, cr.Drift)
	}
}

// mustReindexReason recomputes the reason string directly, so the test
// doesn't hardcode wording that belongs to graph.FormatVersionReason.
func mustReindexReason(t *testing.T, stateDir string) string {
	t.Helper()
	_, reason := graph.ReindexRequiredReason(stateDir)
	if reason == "" {
		t.Fatal("expected a non-empty reindex reason for the stale fixture")
	}
	return reason
}

// TestCheckReindexRequired_CurrentFormat_NoReport is the regression guard: a
// repo on the current graph.fb format version must never be reported.
func TestCheckReindexRequired_CurrentFormat_NoReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	repo := filepath.Join(home, "repo")
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

	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "repo", Path: repo}},
	}
	groupsFn, loadFn := fakeGroups(cfg)

	opts := DoctorOptions{groupsFn: groupsFn, loadGroupFn: loadFn}
	if cr := checkReindexRequired(opts); cr != nil {
		t.Errorf("expected nil CheckResult for a current-format repo, got %+v", cr)
	}
}

// TestCheckReindexRequired_NoGroups_NoReport ensures a fresh machine (no
// registered groups) isn't reported as broken.
func TestCheckReindexRequired_NoGroups_NoReport(t *testing.T) {
	opts := DoctorOptions{
		groupsFn:    func() ([]registry.GroupRef, error) { return nil, nil },
		loadGroupFn: func(string) (*registry.GroupConfig, error) { return nil, nil },
	}
	if cr := checkReindexRequired(opts); cr != nil {
		t.Errorf("expected nil CheckResult when no groups are registered, got %+v", cr)
	}
}
