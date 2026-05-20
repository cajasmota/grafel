package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// bytes is already imported above, used in TestPrintDoctorHealthCompleteOutput
var _ = bytes.Contains

// TestDoctorHealthGroupAggregation verifies health computation with empty groups.
func TestDoctorHealthGroupAggregation(t *testing.T) {
	// Test with empty group list
	groups := []registry.GroupRef{}

	health := ComputeDoctorHealth(groups)

	if len(health) != 0 {
		t.Errorf("expected 0 health reports for empty input, got %d", len(health))
	}
}

// TestComputeRepoHealthStale verifies staleness detection.
func TestComputeRepoHealthStale(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "stale-repo")
	os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755)

	// Create state dir with old graph-stats
	stateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(stateDir, 0o755)

	oldTime := time.Now().Add(-48 * time.Hour) // 2 days ago
	side := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         oldTime,
		TotalEntities:      100,
		TotalRelationships: 50,
		RuntimeMS:          1000,
	}
	data, _ := json.Marshal(side)
	os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), data, 0o644)

	repo := registry.Repo{
		Slug:  "stale-repo",
		Path:  repoPath,
		Stack: "go",
	}

	health := computeRepoHealth(repo)

	if health.Status != "STALE" {
		t.Errorf("expected status STALE, got %q", health.Status)
	}
	if health.Entities != 100 {
		t.Errorf("expected 100 entities, got %d", health.Entities)
	}
}

// TestPrintDoctorHealthCompleteOutput verifies comprehensive output generation.
func TestPrintDoctorHealthCompleteOutput(t *testing.T) {
	health := &DoctorGroupHealth{
		GroupName:     "example-group",
		Healthy:       true,
		Status:        "HEALTHY",
		DaemonManaged: true,
		WatcherRepoCount: 3,
		WatcherDirCount: 42,
		WatcherEventsDropped: 0,
		LastWatcherActivity: "30s ago",
		Repos: []*DoctorRepoHealth{
			{
				Slug:           "core-api",
				Path:           "/home/user/repos/core-api",
				Status:         "OK",
				LastIndexed:    time.Now().Add(-30 * time.Minute),
				LastIndexedAge: "30m ago",
				Entities:       2400,
				Relationships:  4900,
				CrossRepoEdges: 3,
			},
			{
				Slug:           "mobile-frontend",
				Path:           "/home/user/repos/mobile-frontend",
				Status:         "OK",
				LastIndexed:    time.Now().Add(-5 * time.Minute),
				LastIndexedAge: "5m ago",
				Entities:       1100,
				Relationships:  2800,
				CrossRepoEdges: 89,
			},
		},
		TotalEntities:       3500,
		TotalRelationships:  7700,
		TotalCrossRepoEdges: 92,
		BugRate:             1.5,
		OrphanEntities:      350,
		OrphanRate:          10.0,
		PendingRepairs:      12,
		PendingEnrichments:  89,
		IssuesFound:         []string{},
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{health})

	output := buf.String()

	// Verify all expected sections are present
	expectedStrings := []string{
		"example-group",
		"HEALTHY",
		"✓",
		"Daemon-managed: true",
		"Watcher: 3 repos",
		"30m ago",
		"5m ago",
		"core-api",
		"mobile-frontend",
		"Quality:",
		"Bug-rate",
		"Orphan entities",
		"Pending repairs",
		"Pending enrichments",
		"Issues found:",
		"[none]",
	}

	for _, expected := range expectedStrings {
		if !bytes.Contains([]byte(output), []byte(expected)) {
			t.Errorf("output missing expected string: %q", expected)
		}
	}
}
