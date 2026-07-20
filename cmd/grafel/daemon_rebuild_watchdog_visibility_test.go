package main

// daemon_rebuild_watchdog_visibility_test.go — #5822 sub-ask 3 regression
// guard: a rebuild-watchdog SIGKILL (#5143's per-repo timeout) must be
// VISIBLE, not silent. Before this change, the timeout result was
// discarded entirely: nothing was persisted, so `grafel status` kept
// showing the previous (stale) index with no error/warning — the only
// trace was daemon.err.
//
// These tests assert against the REAL status-plane read/write
// (internal/statusfile) and the REAL status renderer (internal/cli), not
// mocks-only.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/cli"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// repoPathForSlug looks up the on-disk path registered for slug within group
// — the counterpart to setupTestGroup, which builds paths internally but
// does not return them.
func repoPathForSlug(t *testing.T, group, slug string) string {
	t.Helper()
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	for _, g := range groups {
		if g.Name != group {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			t.Fatalf("LoadGroupConfig: %v", err)
		}
		for _, r := range cfg.Repos {
			if r.Slug == slug {
				return r.Path
			}
		}
	}
	t.Fatalf("repo slug %q not found in group %q", slug, group)
	return ""
}

// TestRebuildWatchdogFailure_PersistedAndSurfacedInStatus is the RED test for
// #5822 sub-ask 3: simulate a rebuild that hits the per-repo watchdog timeout
// and assert (a) the failure marker is persisted to the status-plane sidecar,
// and (b) `grafel status`'s rendering (internal/cli.PrintStatusSummary)
// includes the FAILED line.
func TestRebuildWatchdogFailure_PersistedAndSurfacedInStatus(t *testing.T) {
	group := setupTestGroup(t, "watchdog-visibility-group", []string{"fast", "stuck"})
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "300ms")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	mockIndexFn := func(repoPath, _, slug string, _ []string, _, _ bool, _ ...IndexOption) error {
		if slug == "stuck" {
			<-release // block far longer than the watchdog
			return nil
		}
		return nil
	}

	// concurrency=2 so "fast" and "stuck" run in parallel: with concurrency=1
	// "fast" would starve behind "stuck" on the single worker slot and get
	// spuriously marked STALLED too, even though its indexFn returns instantly.
	_, _, err := daemonRebuildFuncCore(2, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err == nil || !strings.Contains(err.Error(), "stuck") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error naming the stuck repo, got: %v", err)
	}

	stuckPath := repoPathForSlug(t, group, "stuck")

	// (a) the failure marker is persisted to the status-plane sidecar.
	sf, sfErr := statusfile.Read(stuckPath)
	if sfErr != nil {
		t.Fatalf("statusfile.Read(stuck): %v", sfErr)
	}
	if sf.LastRebuildFailure == nil {
		t.Fatal("statusfile: LastRebuildFailure is nil, want a persisted watchdog-timeout marker")
	}
	if !strings.Contains(sf.LastRebuildFailure.Reason, "timed out") {
		t.Errorf("LastRebuildFailure.Reason = %q, want it to mention the timeout", sf.LastRebuildFailure.Reason)
	}
	if sf.LastRebuildFailure.At.IsZero() {
		t.Error("LastRebuildFailure.At is zero, want a recorded timestamp")
	}

	// The successful "fast" repo must NOT carry a failure marker.
	fastPath := repoPathForSlug(t, group, "fast")
	if fsf, ferr := statusfile.Read(fastPath); ferr == nil && fsf.LastRebuildFailure != nil {
		t.Errorf("fast repo unexpectedly has a LastRebuildFailure marker: %+v", fsf.LastRebuildFailure)
	}

	// (b) `grafel status`'s rendering includes the FAILED line.
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	var cfg *registry.GroupConfig
	for _, g := range groups {
		if g.Name == group {
			cfg, err = registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				t.Fatalf("LoadGroupConfig: %v", err)
			}
		}
	}
	if cfg == nil {
		t.Fatalf("group config for %q not found", group)
	}

	summary := cli.ComputeStatusSummary(group, cfg.Repos)
	var buf bytes.Buffer
	cli.PrintStatusSummary(&buf, summary)
	out := buf.String()
	if !strings.Contains(out, "last rebuild FAILED") {
		t.Errorf("PrintStatusSummary output missing 'last rebuild FAILED' line:\n%s", out)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("PrintStatusSummary output missing the timeout reason:\n%s", out)
	}
}

// TestRebuildSuccess_ClearsPriorFailureMarker guards against a stale FAILED
// line lingering forever: a subsequent SUCCESSFUL rebuild of the same repo
// must clear the marker set by an earlier failed attempt.
func TestRebuildSuccess_ClearsPriorFailureMarker(t *testing.T) {
	group := setupTestGroup(t, "watchdog-clear-group", []string{"flaky"})
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "300ms")

	release := make(chan struct{})
	failFn := func(repoPath, _, slug string, _ []string, _, _ bool, _ ...IndexOption) error {
		<-release // never returns before the watchdog fires
		return nil
	}

	_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, failFn, noopLinksFn)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error on the first (failing) rebuild, got: %v", err)
	}
	close(release)

	flakyPath := repoPathForSlug(t, group, "flaky")
	sf, sfErr := statusfile.Read(flakyPath)
	if sfErr != nil || sf.LastRebuildFailure == nil {
		t.Fatalf("expected a persisted failure marker after the first rebuild, got err=%v marker=%v", sfErr, sf)
	}

	// Second rebuild: succeeds immediately.
	okFn := func(repoPath, _, slug string, _ []string, _, _ bool, _ ...IndexOption) error {
		return nil
	}
	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, okFn, noopLinksFn)
	if err != nil {
		t.Fatalf("second (successful) rebuild failed: %v", err)
	}
	if len(rebuilt) != 1 {
		t.Fatalf("expected 1 rebuilt repo, got %d", len(rebuilt))
	}

	sf2, sfErr2 := statusfile.Read(flakyPath)
	if sfErr2 != nil {
		t.Fatalf("statusfile.Read after successful rebuild: %v", sfErr2)
	}
	if sf2.LastRebuildFailure != nil {
		t.Errorf("LastRebuildFailure = %+v, want nil after a successful rebuild (must not linger)", sf2.LastRebuildFailure)
	}

	// Status rendering must no longer show the FAILED line either.
	groups, gerr := registry.Groups()
	if gerr != nil {
		t.Fatalf("registry.Groups: %v", gerr)
	}
	var cfg *registry.GroupConfig
	for _, g := range groups {
		if g.Name == group {
			cfg, err = registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				t.Fatalf("LoadGroupConfig: %v", err)
			}
		}
	}
	summary := cli.ComputeStatusSummary(group, cfg.Repos)
	var buf bytes.Buffer
	cli.PrintStatusSummary(&buf, summary)
	if strings.Contains(buf.String(), "last rebuild FAILED") {
		t.Errorf("PrintStatusSummary still shows a FAILED line after a successful rebuild:\n%s", buf.String())
	}
}
