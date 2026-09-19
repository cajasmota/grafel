package dashboard

// bugrate_pool_7271_test.go — the group bug rate must POOL every repo (#7271).
//
// `doctor` and these endpoints all report per GROUP, and a group is plural. The
// reported scenario is two repos with markedly different rates sitting in one
// group: a figure that silently reflects one of them is a confident wrong
// number, which is the same defect class the issue is about, one level up.
//
// Each fixture below is chosen so every wrong aggregation is distinguishable
// from the right one:
//
//	repo A   1 of 4 unresolved → 25.0%
//	repo B   3 of 6 unresolved → 50.0%
//	pooled   4 of 10           → 40.0%   ← the only correct answer
//	mean-of-means              → 37.5%

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/quality/audit"
	"github.com/cajasmota/grafel/internal/registry"
)

// reportWithImports builds the audit report shape the reply builder consumes:
// total IMPORTS edges, of which unresolved are raw path strings.
func reportWithImports(path string, total, unresolved int) *audit.RepoReport {
	return &audit.RepoReport{
		Path:         path,
		Entities:     10,
		Orphans:      1,
		ImportsTotal: total,
		ImportsToIDFormat: map[audit.ImportFormat]int{
			audit.ImportFormatHex:        total - unresolved,
			audit.ImportFormatPathString: unresolved,
		},
	}
}

// TestBuildOrphanAuditReply_BugRatePoolsEveryRepo pins the aggregation seam in
// the reply the quality panel reads.
func TestBuildOrphanAuditReply_BugRatePoolsEveryRepo(t *testing.T) {
	reply := buildOrphanAuditReply("g", []*audit.RepoReport{
		reportWithImports("/tmp/a", 4, 1),
		reportWithImports("/tmp/b", 6, 3),
	})

	if reply.BugRatePct != 40.0 {
		t.Errorf("bug_rate_pct = %v, want 40 (pooled 4 of 10). 25 = first repo only, 50 = last repo only, 37.5 = mean of the per-repo rates", reply.BugRatePct)
	}
}

// writePoolRepo creates a repo with a real graph.fb carrying exactly total
// IMPORTS edges, of which unresolved are unbound path strings.
func writePoolRepo(t *testing.T, home, slug string, total, unresolved int) string {
	t.Helper()

	repoPath := filepath.Join(home, "repos", slug)
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now(),
		Stats:       graph.Stats{Entities: 2, Relationships: total, Files: 2},
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
		},
	}
	for i := 0; i < total; i++ {
		to := "aaaaaaaaaaaaaaaa"
		if i < unresolved {
			to = fmt.Sprintf("./unresolved/%d", i)
		}
		doc.Relationships = append(doc.Relationships,
			graph.Relationship{FromID: "bbbbbbbbbbbbbbbb", ToID: to, Kind: "IMPORTS"})
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	side := &graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         doc.GeneratedAt,
		TotalEntities:      len(doc.Entities),
		TotalRelationships: len(doc.Relationships),
	}
	if err := graph.WriteSidecar(filepath.Join(stateDir, "graph.fb"), side, true); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return repoPath
}

// TestQualityComposite_BugRatePoolsEveryRepo drives the composite-score
// endpoint over a real two-repo group. This is the second aggregation seam and
// it is graded separately on purpose: a DEAD verdict on the reply builder above
// says nothing about this one.
func TestQualityComposite_BugRatePoolsEveryRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, home)

	repoA := writePoolRepo(t, home, "repo-a", 4, 1)
	repoB := writePoolRepo(t, home, "repo-b", 6, 3)

	cfgPath := filepath.Join(home, "pool.fleet.json")
	cfg := &registry.GroupConfig{
		Name: "pool",
		Repos: []registry.Repo{
			{Slug: "repo-a", Path: repoA, Stack: registry.StackList{"go"}},
			{Slug: "repo-b", Path: repoB, Stack: registry.StackList{"go"}},
		},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("pool", cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/quality/composite/pool", nil)
	req.SetPathValue("group", "pool")
	rec := httptest.NewRecorder()
	srv.handleQualityComposite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var reply CompositeScoreReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if reply.Repos != 2 {
		t.Fatalf("audited %d repos, want 2 — the fixture cannot pin pooling", reply.Repos)
	}
	if reply.BugRatePct != 40.0 {
		t.Errorf("bug_rate_pct = %v, want 40 (pooled 4 of 10). 25 = first repo only, 50 = last repo only, 37.5 = mean of the per-repo rates", reply.BugRatePct)
	}
}
