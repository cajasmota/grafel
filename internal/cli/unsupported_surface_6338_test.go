package cli

// #6338 — end-to-end over the two reporting surfaces: a repo whose sidecar
// records files skipped for having no extractor must say so in `grafel doctor`
// and `grafel status`, and a repo with nothing skipped must say nothing.
//
// These assert on RENDERED output rather than struct fields: the reported
// defect is that the surfaces are silent, and an in-memory assertion cannot
// see silence on screen.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// seed6338 creates a repo with a graph-stats.json carrying unsupported.
func seed6338(t *testing.T, root, slug string, unsupported map[string]int) registry.Repo {
	t.Helper()
	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	side := graph.GraphStatsSidecar{
		Version:               1,
		ComputedAt:            time.Now().Add(-5 * time.Minute),
		TotalFiles:            700,
		TotalEntities:         12,
		TotalRelationships:    3,
		UnsupportedExtensions: unsupported,
	}
	data, err := json.Marshal(side)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}

func renderDoctor6338(t *testing.T, repos []registry.Repo) string {
	t.Helper()
	health := &DoctorGroupHealth{GroupName: "g6338", Status: "HEALTHY"}
	for _, r := range repos {
		rh := computeRepoHealth(r, false)
		health.Repos = append(health.Repos, rh)
	}
	aggregateUnsupported(health)
	var buf bytes.Buffer
	PrintDoctorHealth(&buf, []*DoctorGroupHealth{health})
	return buf.String()
}

func renderStatus6338(t *testing.T, repos []registry.Repo) string {
	t.Helper()
	var buf bytes.Buffer
	PrintStatusSummary(&buf, ComputeStatusSummary("g6338", repos))
	return buf.String()
}

// Requirement 2 on the doctor surface: the extension, its aggregate count and
// the language name all reach the screen.
func TestDoctorReportsUnsupportedExtensions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repo := seed6338(t, tmp, "legacy", map[string]int{".vb": 672, ".pas": 14, ".go": 12045})

	out := renderDoctor6338(t, []registry.Repo{repo})

	if !strings.Contains(out, "Unsupported languages (no extractor):") {
		t.Fatalf("doctor is still silent about skipped files:\n%s", out)
	}
	if !strings.Contains(out, "672 files") {
		t.Fatalf("doctor must report the .vb file count @dcastro-imp had to count by hand:\n%s", out)
	}
	if !strings.Contains(out, "VB.NET") {
		t.Fatalf("doctor must name the language:\n%s", out)
	}
	if !strings.Contains(out, ".pas") {
		t.Fatalf("doctor shows the full table, including the small counts:\n%s", out)
	}
	// The supported extension in the same sidecar must not leak through.
	if strings.Contains(out, "12,045") || strings.Contains(out, "12045") {
		t.Fatalf("a supported extension leaked into the doctor report:\n%s", out)
	}
}

// Requirement 1 on the doctor surface: a clean repo prints nothing at all.
func TestDoctorSilentWhenNothingSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repo := seed6338(t, tmp, "clean", nil)

	out := renderDoctor6338(t, []registry.Repo{repo})
	if strings.Contains(out, "Unsupported languages") {
		t.Fatalf("a clean repo must print no unsupported section:\n%s", out)
	}
}

// Requirement 3 on the doctor surface: once an extractor claims the extension,
// the row disappears even though the on-disk sidecar still counts it.
func TestDoctorDropsNowSupportedExtension(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	// `.go` stands in for `.vb` after #6327 lands: an extension with a large
	// stale count that an extractor now claims.
	repo := seed6338(t, tmp, "stale", map[string]int{".go": 672})

	out := renderDoctor6338(t, []registry.Repo{repo})
	if strings.Contains(out, "Unsupported languages") {
		t.Fatalf("a now-supported extension must vanish from doctor:\n%s", out)
	}
	if strings.Contains(out, "672") {
		t.Fatalf("the stale count must not be printed:\n%s", out)
	}
}

// Counts aggregate ACROSS repos in the group — a monorepo split into five
// repos with 140 .vb files each is one 700-file gap, not five separate ones.
func TestDoctorAggregatesUnsupportedAcrossRepos(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	a := seed6338(t, tmp, "alpha", map[string]int{".vb": 400})
	b := seed6338(t, tmp, "beta", map[string]int{".vb": 272})

	out := renderDoctor6338(t, []registry.Repo{a, b})
	if !strings.Contains(out, "672 files") {
		t.Fatalf("doctor must sum .vb across repos to 672:\n%s", out)
	}
	if strings.Contains(out, "400 files") || strings.Contains(out, "272 files") {
		t.Fatalf("doctor must aggregate, not list per repo:\n%s", out)
	}
}

// The status surface reports too, but only above the floor.
func TestStatusReportsUnsupportedAboveFloorOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repo := seed6338(t, tmp, "legacy", map[string]int{".vb": 672, ".pas": 3})

	out := renderStatus6338(t, []registry.Repo{repo})
	if !strings.Contains(out, "Unsupported languages (no extractor):") {
		t.Fatalf("status is still silent about skipped files:\n%s", out)
	}
	if !strings.Contains(out, "672 files") || !strings.Contains(out, "VB.NET") {
		t.Fatalf("status must name .vb and its count:\n%s", out)
	}
	if strings.Contains(out, ".pas") {
		t.Fatalf("status must suppress the 3-file stray below the floor:\n%s", out)
	}
}

// Requirement 1 on the status surface.
func TestStatusSilentWhenNothingSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repo := seed6338(t, tmp, "clean", nil)

	out := renderStatus6338(t, []registry.Repo{repo})
	if strings.Contains(out, "Unsupported languages") {
		t.Fatalf("a clean repo must print no unsupported section in status:\n%s", out)
	}
}

// Requirement 3 on the status surface.
func TestStatusDropsNowSupportedExtension(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repo := seed6338(t, tmp, "stale", map[string]int{".go": 672})

	out := renderStatus6338(t, []registry.Repo{repo})
	if strings.Contains(out, "Unsupported languages") {
		t.Fatalf("a now-supported extension must vanish from status:\n%s", out)
	}
}
