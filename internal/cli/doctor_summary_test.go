package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// TestComputeRepoHealth verifies per-repo health snapshot computation.
func TestComputeRepoHealth(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a mock repo with .git directory
	repoPath := filepath.Join(tmpDir, "test-repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	repo := registry.Repo{
		Slug:  "test-repo",
		Path:  repoPath,
		Stack: "go",
	}

	health := computeRepoHealth(repo)

	// Verify basic fields
	if health.Slug != "test-repo" {
		t.Errorf("health.Slug = %q, want %q", health.Slug, "test-repo")
	}
	if health.Path != repoPath {
		t.Errorf("health.Path = %q, want %q", health.Path, repoPath)
	}
	if health.Status != "OK" {
		t.Errorf("health.Status = %q, want %q", health.Status, "OK")
	}
	if health.LastIndexedAge != "(never)" {
		t.Errorf("health.LastIndexedAge = %q, want %q", health.LastIndexedAge, "(never)")
	}
}

// TestComputeRepoHealthMissing verifies handling of missing repos.
func TestComputeRepoHealthMissing(t *testing.T) {
	repo := registry.Repo{
		Slug:  "missing-repo",
		Path:  "/nonexistent/path",
		Stack: "go",
	}

	health := computeRepoHealth(repo)

	if health.Status != "MISSING" {
		t.Errorf("health.Status = %q, want %q", health.Status, "MISSING")
	}
}

// TestComputeDoctorHealthEmpty verifies handling of empty group list.
func TestComputeDoctorHealthEmpty(t *testing.T) {
	result := ComputeDoctorHealth([]registry.GroupRef{})

	if len(result) != 0 {
		t.Errorf("ComputeDoctorHealth([]) returned %d groups, want 0", len(result))
	}
}

// TestPrintDoctorHealth verifies output formatting without a live daemon.
func TestPrintDoctorHealth(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	health := &DoctorGroupHealth{
		GroupName:     "test-group",
		Healthy:       true,
		Status:        "HEALTHY",
		DaemonManaged: false,
		Repos: []*DoctorRepoHealth{
			{
				Slug:           "test-repo",
				Path:           repoPath,
				Status:         "OK",
				LastIndexed:    time.Now().Add(-1 * time.Hour),
				LastIndexedAge: "1h ago",
				Entities:       100,
				Relationships:  50,
				CrossRepoEdges: 5,
			},
		},
		TotalEntities:       100,
		TotalRelationships:  50,
		TotalCrossRepoEdges: 5,
		BugRate:             0.0,
		OrphanEntities:      10,
		OrphanRate:          10.0,
		PendingRepairs:      2,
		PendingEnrichments:  3,
		IssuesFound:         []string{},
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{health})

	// Verify output contains expected elements
	if buf.Len() == 0 {
		t.Error("PrintDoctorHealth produced empty output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test-group")) {
		t.Error("output does not contain group name")
	}
	if !bytes.Contains(buf.Bytes(), []byte("HEALTHY")) {
		t.Error("output does not contain HEALTHY status")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test-repo")) {
		t.Error("output does not contain repo slug")
	}
}

// TestPrintDoctorHealthWithIssues verifies issue reporting in output.
func TestPrintDoctorHealthWithIssues(t *testing.T) {
	health := &DoctorGroupHealth{
		GroupName:     "problem-group",
		Healthy:       false,
		Status:        "DEGRADED",
		DaemonManaged: false,
		Repos:         []*DoctorRepoHealth{},
		IssuesFound: []string{
			"repo foo hasn't been indexed in >24h (last: 28h ago)",
		},
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{health})

	if !bytes.Contains(buf.Bytes(), []byte("DEGRADED")) {
		t.Error("output does not contain DEGRADED status")
	}
	if !bytes.Contains(buf.Bytes(), []byte("hasn't been indexed")) {
		t.Error("output does not report indexing issue")
	}
}

// TestComputeQualityMetrics verifies aggregation of quality metrics.
func TestComputeQualityMetrics(t *testing.T) {
	health := &DoctorGroupHealth{
		GroupName:           "test-group",
		Repos:               []*DoctorRepoHealth{},
		TotalEntities:       100,
		TotalRelationships:  50,
		OrphanEntities:      10,
		PendingRepairs:      5,
		PendingEnrichments:  8,
	}

	computeQualityMetrics(health)

	expectedOrphanRate := 10.0
	if health.OrphanRate != expectedOrphanRate {
		t.Errorf("health.OrphanRate = %.1f, want %.1f", health.OrphanRate, expectedOrphanRate)
	}
}

// BenchmarkFormatTimeSince measures the performance of time formatting.
func BenchmarkFormatTimeSince(b *testing.B) {
	t := time.Now().Add(-5 * time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = formatTimeSince(t)
	}
}

// BenchmarkComputeRepoHealth measures per-repo health computation.
func BenchmarkComputeRepoHealth(b *testing.B) {
	tmpDir := b.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		b.Fatalf("failed to create test repo: %v", err)
	}

	repo := registry.Repo{
		Slug:  "test-repo",
		Path:  repoPath,
		Stack: "go",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = computeRepoHealth(repo)
	}
}
