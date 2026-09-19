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
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/cli"
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

// TestGroupDiagnostics_UnmeasuredBugRateIsExplicitNull asserts the serialised
// diagnostics payload. Same reason as the webhook body: the pointer is only
// worth anything if the null survives to the wire, and `,omitempty` on the tag
// would silently delete it.
func TestGroupDiagnostics_UnmeasuredBugRateIsExplicitNull(t *testing.T) {
	gd := convertGroupHealth(&cli.DoctorGroupHealth{
		GroupName: "g",
		Status:    "HEALTHY",
		Repos:     []*cli.DoctorRepoHealth{},
	})
	body, err := json.Marshal(gd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"bug_rate":null`) {
		t.Errorf("diagnostics payload does not carry an explicit null bug_rate:\n%s", got)
	}

	// Positive control: a measured rate reaches the wire as a number.
	gd = convertGroupHealth(&cli.DoctorGroupHealth{
		GroupName:      "g",
		Status:         "HEALTHY",
		Repos:          []*cli.DoctorRepoHealth{},
		BugRate:        audit.BugRate{TotalImports: 4, ResolvedImports: 3},
		ReposGraphRead: 1,
	})
	body, err = json.Marshal(gd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"bug_rate":25`) {
		t.Errorf("measured bug_rate missing from the diagnostics payload:\n%s", got)
	}
}

// TestBugRateHealthyBand_MatchesDashboardFidelity ties the terminal's healthy
// band to the dashboard's. `doctor` prints "✓" at or below
// audit.BugRateHealthyMaxPct; the dashboard calls a group healthy at fidelity
// >= 0.97. The comment on the constant claims these agree — this is the
// assertion that makes the claim checkable, in the only package that can see
// both.
func TestBugRateHealthyBand_MatchesDashboardFidelity(t *testing.T) {
	const healthyFidelity = 0.97

	if fid := fidelityFromBugRate(audit.BugRateHealthyMaxPct); fid != healthyFidelity {
		t.Errorf("fidelityFromBugRate(%.2f) = %v, want exactly %v — the terminal's band and the dashboard's have drifted apart",
			audit.BugRateHealthyMaxPct, fid, healthyFidelity)
	}
	if fid := fidelityFromBugRate(audit.BugRateHealthyMaxPct + 0.1); fid >= healthyFidelity {
		t.Errorf("a rate just past the band still reads as healthy fidelity (%v)", fid)
	}
	if _, health := deriveHealthFromFidelity(fidelityFromBugRate(audit.BugRateHealthyMaxPct)); health != healthHealthy {
		t.Errorf("the band's own rate is not %q on the dashboard: %q", healthHealthy, health)
	}
}

// TestBuildOrphanAuditReply_ReferencesUseTheSameTally closes the last seam in
// this reply. The unresolved-references panel used to be fed by a second pair
// of counters accumulated beside the bug-rate tally; they are gone, and this
// pins that the panel now reads the same pooled numbers the rate does. Without
// it the argument could be replaced by anything (including zeros) and nothing
// would notice — which is how a third derivation gets wired back in.
func TestBuildOrphanAuditReply_ReferencesUseTheSameTally(t *testing.T) {
	reply := buildOrphanAuditReply("g", []*audit.RepoReport{
		reportWithImports("/tmp/a", 4, 1),
		reportWithImports("/tmp/b", 6, 3),
	})

	if reply.References.Total != 10 {
		t.Errorf("References.Total = %d, want the pooled 10", reply.References.Total)
	}
	if reply.References.Resolved != 6 {
		t.Errorf("References.Resolved = %d, want the pooled 6", reply.References.Resolved)
	}
	if reply.References.Unresolved != 4 {
		t.Errorf("References.Unresolved = %d, want 4", reply.References.Unresolved)
	}
	if reply.References.ResolvedRate != 0.6 {
		t.Errorf("References.ResolvedRate = %v, want 0.6", reply.References.ResolvedRate)
	}
	// The panel and the headline rate describe the same population: 40%
	// unresolved is the complement of a 0.6 resolved rate.
	if reply.BugRatePct != 40.0 {
		t.Errorf("bug_rate_pct = %v, want 40 — the panel and the rate have drifted apart", reply.BugRatePct)
	}
}
