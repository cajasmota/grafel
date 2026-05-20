package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

func TestComputeStatusSummary(t *testing.T) {
	// Create temporary directory structure for testing.
	tmpDir := t.TempDir()

	// When ARCHIGRAPH_DAEMON_ROOT is set, StateDirForRepo uses a hashed state directory.
	// So we need to create files in the right places.
	t.Setenv(daemon.EnvRoot, tmpDir)

	// Create a mock repo 1.
	repo1Path := filepath.Join(tmpDir, "repo1")
	os.MkdirAll(repo1Path, 0o755)

	// Get the actual state dir that daemon.StateDirForRepo will use.
	stateDir1 := daemon.StateDirForRepo(repo1Path)
	os.MkdirAll(stateDir1, 0o755)

	// Write graph-stats.json for repo1.
	side1 := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-5 * time.Minute),
		TotalEntities:      1135,
		TotalRelationships: 2400,
		Communities:        12,
		Modularity:         0.5,
		GodNodes:           3,
		ArticulationPoints: 8,
		RuntimeMS:          5000,
	}
	data1, _ := json.Marshal(side1)
	os.WriteFile(filepath.Join(stateDir1, "graph-stats.json"), data1, 0o644)

	// Create a mock repo 2.
	repo2Path := filepath.Join(tmpDir, "repo2")
	os.MkdirAll(repo2Path, 0o755)

	// Get the actual state dir that daemon.StateDirForRepo will use.
	stateDir2 := daemon.StateDirForRepo(repo2Path)
	os.MkdirAll(stateDir2, 0o755)

	// Write graph-stats.json for repo2.
	side2 := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-10 * time.Minute),
		TotalEntities:      3200,
		TotalRelationships: 6100,
		Communities:        18,
		Modularity:         0.6,
		GodNodes:           5,
		ArticulationPoints: 12,
		RuntimeMS:          8000,
	}
	data2, _ := json.Marshal(side2)
	os.WriteFile(filepath.Join(stateDir2, "graph-stats.json"), data2, 0o644)

	// Create repo list.
	repos := []registry.Repo{
		{
			Slug: "repo-1",
			Path: repo1Path,
		},
		{
			Slug: "repo-2",
			Path: repo2Path,
		},
	}

	summary := ComputeStatusSummary("test-group", repos)

	// Verify aggregation.
	if summary.GroupName != "test-group" {
		t.Errorf("expected group name 'test-group', got %q", summary.GroupName)
	}
	if summary.TotalEntities != 4335 {
		t.Errorf("expected 4335 total entities, got %d", summary.TotalEntities)
	}
	if summary.TotalRelationships != 8500 {
		t.Errorf("expected 8500 total relationships, got %d", summary.TotalRelationships)
	}

	// Verify per-repo stats.
	if rs, ok := summary.RepoStats["repo-1"]; !ok {
		t.Error("repo-1 not found in RepoStats")
	} else {
		if rs.Entities != 1135 {
			t.Errorf("repo-1: expected 1135 entities, got %d", rs.Entities)
		}
		if rs.Relationships != 2400 {
			t.Errorf("repo-1: expected 2400 relationships, got %d", rs.Relationships)
		}
		if rs.Path != repo1Path {
			t.Errorf("repo-1: expected path %q, got %q", repo1Path, rs.Path)
		}
	}

	if rs, ok := summary.RepoStats["repo-2"]; !ok {
		t.Error("repo-2 not found in RepoStats")
	} else {
		if rs.Entities != 3200 {
			t.Errorf("repo-2: expected 3200 entities, got %d", rs.Entities)
		}
		if rs.Relationships != 6100 {
			t.Errorf("repo-2: expected 6100 relationships, got %d", rs.Relationships)
		}
	}
}

func TestFormatTimeSince(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds ago",
			duration: 30 * time.Second,
			expected: "30s ago",
		},
		{
			name:     "minutes ago",
			duration: 5 * time.Minute,
			expected: "5m ago",
		},
		{
			name:     "hours ago",
			duration: 3 * time.Hour,
			expected: "3h ago",
		},
		{
			name:     "hours and minutes ago",
			duration: 2*time.Hour + 30*time.Minute,
			expected: "2h30m ago",
		},
		{
			name:     "days ago",
			duration: 2 * 24 * time.Hour,
			expected: "2d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTime := time.Now().Add(-tt.duration)
			result := formatTimeSince(testTime)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFormatTimeSinceZero(t *testing.T) {
	result := formatTimeSince(time.Time{})
	if result != "(never)" {
		t.Errorf("expected '(never)' for zero time, got %q", result)
	}
}

func TestRepoStatusWithoutGraphStats(t *testing.T) {
	// Test that RepoStatus can be created even when graph-stats.json doesn't exist.
	tmpDir := t.TempDir()

	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "test-repo")
	os.MkdirAll(repoPath, 0o755)

	repos := []registry.Repo{
		{
			Slug: "test-repo",
			Path: repoPath,
		},
	}

	summary := ComputeStatusSummary("test-group", repos)

	rs, ok := summary.RepoStats["test-repo"]
	if !ok {
		t.Fatal("test-repo not found in RepoStats")
	}

	// Should have zero values but still be present.
	if rs.Entities != 0 {
		t.Errorf("expected 0 entities, got %d", rs.Entities)
	}
	if rs.Relationships != 0 {
		t.Errorf("expected 0 relationships, got %d", rs.Relationships)
	}
	if rs.LastIndexedAge != "(never)" {
		t.Errorf("expected '(never)', got %q", rs.LastIndexedAge)
	}
}

func TestFmtIntWithCommas(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{100, "100"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1000000, "1,000,000"},
		{1234567890, "1,234,567,890"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := fmtInt(tt.input)
			if result != tt.expected {
				t.Errorf("fmtInt(%d): expected %q, got %q", tt.input, tt.expected, result)
			}
		})
	}
}

func TestLoadCandidateCountsArray(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".archigraph")
	os.MkdirAll(stateDir, 0o755)

	// Write enrichment-candidates.json as a bare array with distinct subject IDs.
	candidates := []map[string]interface{}{
		{"kind": "enrichment_edge", "subject_id": "e1"},
		{"kind": "enrichment_edge", "subject_id": "e2"},
		{"kind": "repair_edge", "subject_id": "r1"},
		{"kind": "enrichment_edge", "subject_id": "e3"},
		{"kind": "repair_edge", "subject_id": "r2"},
	}
	data, _ := json.Marshal(candidates)
	os.WriteFile(filepath.Join(stateDir, "enrichment-candidates.json"), data, 0o644)

	// loadCandidateCounts now returns (uniqueSubjects, totalActions, repairCount).
	subjects, actions, repair := loadCandidateCounts(stateDir)
	if subjects != 3 {
		t.Errorf("expected 3 unique enrichment subjects, got %d", subjects)
	}
	if actions != 3 {
		t.Errorf("expected 3 total enrichment actions, got %d", actions)
	}
	if repair != 2 {
		t.Errorf("expected 2 repair candidates, got %d", repair)
	}
}

func TestLoadCandidateCountsMissing(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".archigraph")
	os.MkdirAll(stateDir, 0o755)

	// No enrichment-candidates.json file.
	subjects, actions, repair := loadCandidateCounts(stateDir)
	if subjects != 0 {
		t.Errorf("expected 0 enrichment subjects when file missing, got %d", subjects)
	}
	if actions != 0 {
		t.Errorf("expected 0 enrichment actions when file missing, got %d", actions)
	}
	if repair != 0 {
		t.Errorf("expected 0 repair candidates when file missing, got %d", repair)
	}
}
