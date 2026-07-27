package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/analytics"
	"github.com/cajasmota/grafel/internal/registry"
)

// analyticsFixtureDoc is a small graph carrying every signal the health entry
// reads: orphans, IMPORTS hygiene, coverage in all four crediting phases, an
// import cycle, endpoints and a process flow.
func analyticsFixtureDoc() *graph.Document {
	ents := []graph.Entity{
		{ID: "aaaaaaaaaaaaaaa1", Name: "OrderService", Kind: "Class", SourceFile: "src/order_service.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa2", Name: "PaymentService", Kind: "Class", SourceFile: "src/payment_service.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa3", Name: "ListOrders", Kind: "SCOPE.Operation", SourceFile: "src/handlers.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa4", Name: "GET /orders", Kind: "http_endpoint_definition", SourceFile: "src/handlers.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa5", Name: "GET /admin", Kind: "http_endpoint_definition", SourceFile: "src/admin.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb1", Name: "TestOrderService", Kind: "SCOPE.Operation", SourceFile: "src/order_service_test.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb2", Name: "TestPaymentService", Kind: "SCOPE.Operation", SourceFile: "src/payment_service_test.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb3", Name: "TestListOrders", Kind: "SCOPE.Operation", SourceFile: "src/handlers_test.go", Language: "go"},
		{ID: "ccccccccccccccc1", Name: "modA", Kind: "SCOPE.Module", SourceFile: "src/a.go", Language: "go"},
		{ID: "ccccccccccccccc2", Name: "modB", Kind: "SCOPE.Module", SourceFile: "src/b.go", Language: "go"},
		{ID: "ddddddddddddddd1", Name: "checkout", Kind: "SCOPE.Process.Saga", SourceFile: "src/flow.go", Language: "go"},
		{ID: "ddddddddddddddd2", Name: "Unused", Kind: "Struct", SourceFile: "src/unused.go", Language: "go"},
		{ID: "eeeeeeeeeeeeeee1", Name: "orders-q", Kind: "queue", SourceFile: "src/queue.go", Language: "go"},
		{ID: "eeeeeeeeeeeeeee2", Name: "orders-t", Kind: "topic", SourceFile: "src/topic.go", Language: "go"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "bbbbbbbbbbbbbbb1", ToID: "aaaaaaaaaaaaaaa1", Kind: "TESTS"},
		{ID: "r2", FromID: "bbbbbbbbbbbbbbb3", ToID: "aaaaaaaaaaaaaaa3", Kind: "CALLS"},
		{ID: "r3", FromID: "aaaaaaaaaaaaaaa3", ToID: "aaaaaaaaaaaaaaa4", Kind: "IMPLEMENTS"},
		{ID: "r4", FromID: "aaaaaaaaaaaaaaa5", ToID: "aaaaaaaaaaaaaaa1", Kind: "REQUIRES_AUTH"},
		{ID: "r5", FromID: "ccccccccccccccc1", ToID: "ccccccccccccccc2", Kind: "IMPORTS"},
		{ID: "r6", FromID: "ccccccccccccccc2", ToID: "ccccccccccccccc1", Kind: "IMPORTS"},
		{ID: "r7", FromID: "ccccccccccccccc1", ToID: "ext:fmt:Printf", Kind: "IMPORTS"},
		{ID: "r8", FromID: "ccccccccccccccc2", ToID: "./relative", Kind: "IMPORTS"},
		{ID: "r9", FromID: "ddddddddddddddd1", ToID: "ddddddddddddddd2", Kind: "CONTAINS"},
	}
	return &graph.Document{
		Version:       graph.SchemaVersion,
		Entities:      ents,
		Relationships: rels,
		Stats:         graph.Stats{Entities: len(ents), Relationships: len(rels)},
	}
}

// newAnalyticsFixtureRepo writes the fixture graph into an isolated daemon
// store and returns the repo path.
func newAnalyticsFixtureRepo(t *testing.T, doc *graph.Document) string {
	t.Helper()
	repoPath := t.TempDir()
	if _, err := fbwriter.WriteGraphGen(daemon.StateDirForRepo(repoPath), doc); err != nil {
		t.Fatalf("write fixture graph: %v", err)
	}
	return repoPath
}

// readOnlyHistoryEntry reads the single health entry the batch appended.
func readOnlyHistoryEntry(t *testing.T, root string) quality.HealthEntry {
	t.Helper()
	var path string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, "health-history.jsonl") {
			path = p
		}
		return nil
	})
	if err != nil || path == "" {
		t.Fatalf("health-history.jsonl not written under %s", root)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open history: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var last quality.HealthEntry
	n := 0
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		if err := json.Unmarshal(sc.Bytes(), &last); err != nil {
			t.Fatalf("decode history line: %v", err)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("history lines = %d, want 1", n)
	}
	return last
}

// TestAppendRebuildHistory_ScansEachRepoOnce is the hazard assertion. The
// pre-change batch materialised each repo's graph FOUR times (audit, coverage+
// cycles, auth-uncovered, agents-map). One scan per repo, and never through the
// full-materialise Document loader.
func TestAppendRebuildHistory_ScansEachRepoOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repoA := newAnalyticsFixtureRepo(t, analyticsFixtureDoc())
	repoB := newAnalyticsFixtureRepo(t, analyticsFixtureDoc())

	scans := map[string]int{}
	origScan := scanRepoAnalytics
	scanRepoAnalytics = func(stateDir string) (analytics.RepoScan, error) {
		scans[stateDir]++
		return origScan(stateDir)
	}
	t.Cleanup(func() { scanRepoAnalytics = origScan })

	loads := 0
	origLoad := loadGraphFromStateDir
	loadGraphFromStateDir = func(stateDir string) (*graph.Document, error) {
		loads++
		return origLoad(stateDir)
	}
	t.Cleanup(func() { loadGraphFromStateDir = origLoad })

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, []string{repoA, repoB}); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}

	if len(scans) != 2 {
		t.Fatalf("scanned %d distinct repos, want 2: %v", len(scans), scans)
	}
	for dir, n := range scans {
		if n != 1 {
			t.Errorf("repo %s scanned %d times, want exactly 1 — the whole analytics "+
				"batch must share one pass over each repo's graph", dir, n)
		}
	}
	if loads != 0 {
		t.Errorf("the batch materialised %d full graph.Documents; with a readable graph.fb "+
			"present it must stream the mmap and never materialise", loads)
	}
}

// TestAppendRebuildHistory_OutputUnchanged proves the persisted metrics are
// identical to what the pre-change four-load batch produced. The expected
// values are recomputed here from the ORIGINAL reference functions
// (audit-equivalent counters via analytics.ScanDocument, which itself is pinned
// to audit.AuditPath / graph.ComputeCoverage / graph.FindImportCycles).
func TestAppendRebuildHistory_OutputUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	doc := analyticsFixtureDoc()
	repoA := newAnalyticsFixtureRepo(t, doc)
	repoB := newAnalyticsFixtureRepo(t, doc)

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, []string{repoA, repoB}); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}
	got := readOnlyHistoryEntry(t, root)

	// Reference: two identical repos, folded exactly as the old loops folded.
	ref := analytics.ScanDocument(doc)
	wantEntities := 2 * ref.Entities
	wantOrphanRate := 100.0 * float64(2*ref.Orphans) / float64(wantEntities)
	wantBugRate := 100.0 * float64(2*(ref.ImportsTotal-ref.ImportsResolved)) / float64(2*ref.ImportsTotal)
	wantCoverage := 100.0 * float64(2*ref.CoveredProduction) / float64(2*ref.TotalProduction)

	if got.TotalEntities != wantEntities {
		t.Errorf("TotalEntities = %d, want %d", got.TotalEntities, wantEntities)
	}
	if got.OrphanRate != wantOrphanRate {
		t.Errorf("OrphanRate = %v, want %v", got.OrphanRate, wantOrphanRate)
	}
	if got.BugRate != wantBugRate {
		t.Errorf("BugRate = %v, want %v", got.BugRate, wantBugRate)
	}
	if got.HealthScore != quality.ComputeHealthScore(wantOrphanRate, wantBugRate) {
		t.Errorf("HealthScore = %v, want %v", got.HealthScore,
			quality.ComputeHealthScore(wantOrphanRate, wantBugRate))
	}
	if got.CoveragePct == nil || *got.CoveragePct != wantCoverage {
		t.Errorf("CoveragePct = %v, want %v", got.CoveragePct, wantCoverage)
	}
	if got.Cycles == nil || *got.Cycles != 2*ref.Cycles {
		t.Errorf("Cycles = %v, want %d", got.Cycles, 2*ref.Cycles)
	}
	if got.AuthUncovered == nil || *got.AuthUncovered != 2*ref.AuthUncovered {
		t.Errorf("AuthUncovered = %v, want %d", got.AuthUncovered, 2*ref.AuthUncovered)
	}
	if got.TotalEndpoints != 2*ref.Endpoints {
		t.Errorf("TotalEndpoints = %d, want %d", got.TotalEndpoints, 2*ref.Endpoints)
	}
	if got.TotalFlows != 2*ref.Flows {
		t.Errorf("TotalFlows = %d, want %d", got.TotalFlows, 2*ref.Flows)
	}
	// Non-vacuity: a fixture that produced zeros everywhere would pass the
	// equalities above without proving anything.
	if ref.Orphans == 0 || ref.ImportsTotal == 0 || ref.CoveredProduction == 0 ||
		ref.Cycles == 0 || ref.AuthUncovered == 0 || ref.Endpoints == 0 || ref.Flows == 0 {
		t.Fatalf("fixture is vacuous: %+v", ref)
	}
}

// TestAppendRebuildHistory_IsVisibleToTheStageGate: the batch is a heavy,
// unbounded read of the whole corpus graph. Before #5954 it ran in a bare
// goroutine that took no gate token and was invisible to the scheduler's busy
// accounting, so it could co-reside with the forked link and group-algo
// children and it distorted the FreeOSMemory release timing. It must now
// register with the gate for its whole window and unwind afterwards.
func TestAppendRebuildHistory_IsVisibleToTheStageGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	repo := newAnalyticsFixtureRepo(t, analyticsFixtureDoc())

	liveHolds := startBargeObserverScheduler(t)

	holdsDuringScan := 0
	origScan := scanRepoAnalytics
	scanRepoAnalytics = func(stateDir string) (analytics.RepoScan, error) {
		holdsDuringScan = liveHolds()
		return origScan(stateDir)
	}
	t.Cleanup(func() { scanRepoAnalytics = origScan })

	cfg := &registry.GroupConfig{Name: "acme"}
	if err := appendRebuildHistory(root, "acme", cfg, []string{repo}); err != nil {
		t.Fatalf("appendRebuildHistory: %v", err)
	}

	if holdsDuringScan == 0 {
		t.Error("no gate registration was live WHILE the analytics batch was walking the corpus " +
			"graph; the batch must register so background heavy stages defer around it and the " +
			"scheduler's busy accounting (which gates the FreeOSMemory release loop) sees it")
	}
	if n := liveHolds(); n != 0 {
		t.Errorf("the analytics batch left %d gate registration(s) held after returning", n)
	}
}

// TestAppendRebuildHistory_ReleasesGateRegistrationOnError: no wedge. A batch
// that fails partway must still unwind its registration.
func TestAppendRebuildHistory_ReleasesGateRegistrationOnError(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	liveHolds := startBargeObserverScheduler(t)

	// An unwritable root makes quality.AppendEntry fail at the end of the batch.
	root := filepath.Join(t.TempDir(), "missing", "\x00bad")
	cfg := &registry.GroupConfig{Name: "acme"}
	_ = appendRebuildHistory(root, "acme", cfg, nil)

	if n := liveHolds(); n != 0 {
		t.Errorf("a failed analytics batch left %d gate registration(s) held", n)
	}
}
