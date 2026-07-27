package analytics

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/quality/audit"
)

// fixtureDoc builds a small but structurally rich graph that exercises every
// number the post-rebuild analytics batch reads: orphans (audit semantics),
// IMPORTS hygiene, coverage (all four crediting phases), an import cycle,
// endpoint/flow kind tallies and the auth-uncovered count.
func fixtureDoc() *graph.Document {
	ents := []graph.Entity{
		// Production, covered directly by a TESTS edge.
		{ID: "aaaaaaaaaaaaaaa1", Name: "OrderService", Kind: "Class", SourceFile: "src/order_service.go", Language: "go"},
		// Production, covered only via name affinity (phase 3).
		{ID: "aaaaaaaaaaaaaaa2", Name: "PaymentService", Kind: "Class", SourceFile: "src/payment_service.go", Language: "go"},
		// Handler + the endpoint definition it implements (phase 4 hop).
		{ID: "aaaaaaaaaaaaaaa3", Name: "ListOrders", Kind: "SCOPE.Operation", SourceFile: "src/handlers.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa4", Name: "GET /orders", Kind: "http_endpoint_definition", SourceFile: "src/handlers.go", Language: "go"},
		// Endpoint with an auth edge, and one without.
		{ID: "aaaaaaaaaaaaaaa5", Name: "GET /admin", Kind: "http_endpoint_definition", SourceFile: "src/admin.go", Language: "go"},
		// Auth-rule discriminators. /health is an endpoint for the TALLY but is a
		// client-side call site, so it is outside the auth-scoped set. /login
		// carries the lowercase "auth" edge kind; /profile carries a lowercase
		// "requires_auth" that must NOT count (the auth match is case-sensitive).
		{ID: "aaaaaaaaaaaaaaa7", Name: "GET /health", Kind: "http_endpoint_call", SourceFile: "src/client.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa8", Name: "POST /login", Kind: "http_endpoint", SourceFile: "src/login.go", Language: "go"},
		{ID: "aaaaaaaaaaaaaaa9", Name: "PUT /profile", Kind: "http_endpoint", SourceFile: "src/profile.go", Language: "go"},
		// Production subject whose only same-token test lives in a DIFFERENT
		// directory subtree, so phase-3 name affinity must decline to credit it.
		// See TestCoverageTotals_NameAffinityIsDirectoryScoped.
		{ID: "aaaaaaaaaaaaaaa6", Name: "ShippingService", Kind: "Class", SourceFile: "src/shipping_service.go", Language: "go"},
		// Test entities.
		{ID: "bbbbbbbbbbbbbbb1", Name: "TestOrderService", Kind: "SCOPE.Operation", SourceFile: "src/order_service_test.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb2", Name: "TestPaymentService", Kind: "SCOPE.Operation", SourceFile: "src/payment_service_test.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb3", Name: "TestListOrders", Kind: "SCOPE.Operation", SourceFile: "src/handlers_test.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb4", Name: "TestShippingService", Kind: "SCOPE.Operation", SourceFile: "tools/qa/shipping_service_test.go", Language: "go"},
		// Two modules that import each other -> one cycle.
		{ID: "ccccccccccccccc1", Name: "modA", Kind: "SCOPE.Module", SourceFile: "src/a.go", Language: "go"},
		{ID: "ccccccccccccccc2", Name: "modB", Kind: "SCOPE.Module", SourceFile: "src/b.go", Language: "go"},
		// A process flow and a plain orphan (only a CONTAINS edge -> still an orphan).
		{ID: "ddddddddddddddd1", Name: "checkout", Kind: "SCOPE.Process.Saga", SourceFile: "src/flow.go", Language: "go"},
		{ID: "ddddddddddddddd2", Name: "Unused", Kind: "Struct", SourceFile: "src/unused.go", Language: "go"},
		// Flow-rule discriminators: the bare "SCOPE.Process" kind IS a flow;
		// "SCOPE.Processor" is a 15-char near-miss that must NOT be (its first 14
		// bytes are "SCOPE.Processo", not "SCOPE.Process.").
		{ID: "ddddddddddddddd3", Name: "signup", Kind: "SCOPE.Process", SourceFile: "src/flow2.go", Language: "go"},
		{ID: "ddddddddddddddd4", Name: "Processor", Kind: "SCOPE.Processor", SourceFile: "src/proc.go", Language: "go"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "bbbbbbbbbbbbbbb1", ToID: "aaaaaaaaaaaaaaa1", Kind: "TESTS"},
		{ID: "r2", FromID: "bbbbbbbbbbbbbbb3", ToID: "aaaaaaaaaaaaaaa3", Kind: "CALLS"},
		{ID: "r3", FromID: "aaaaaaaaaaaaaaa3", ToID: "aaaaaaaaaaaaaaa4", Kind: "IMPLEMENTS"},
		{ID: "r4", FromID: "aaaaaaaaaaaaaaa5", ToID: "aaaaaaaaaaaaaaa1", Kind: "REQUIRES_AUTH"},
		// IMPORTS hygiene: one resolved hex, one ext-qualified, one path string.
		{ID: "r5", FromID: "ccccccccccccccc1", ToID: "ccccccccccccccc2", Kind: "IMPORTS"},
		{ID: "r6", FromID: "ccccccccccccccc2", ToID: "ccccccccccccccc1", Kind: "IMPORTS"},
		{ID: "r7", FromID: "ccccccccccccccc1", ToID: "ext:fmt:Printf", Kind: "IMPORTS"},
		{ID: "r8", FromID: "ccccccccccccccc2", ToID: "./relative", Kind: "IMPORTS"},
		// Structural only: does NOT rescue ddddddddddddddd2 from orphan status.
		{ID: "r9", FromID: "ddddddddddddddd1", ToID: "ddddddddddddddd2", Kind: "CONTAINS"},
		// Auth edges: "auth" counts, "requires_auth" (wrong case) must not.
		{ID: "r10", FromID: "aaaaaaaaaaaaaaa8", ToID: "aaaaaaaaaaaaaaa1", Kind: "auth"},
		{ID: "r11", FromID: "aaaaaaaaaaaaaaa9", ToID: "aaaaaaaaaaaaaaa1", Kind: "requires_auth"},
	}
	return &graph.Document{
		Version:       graph.SchemaVersion,
		Entities:      ents,
		Relationships: rels,
		Stats:         graph.Stats{Entities: len(ents), Relationships: len(rels)},
	}
}

// writeFixture persists doc as a graph.fb under a fresh state dir and returns
// both the repo root and the state dir.
func writeFixture(t *testing.T, doc *graph.Document) (repoPath, stateDir string) {
	t.Helper()
	// Isolate the daemon store so StateDirForRepo (which audit.AuditPath uses
	// to discover a repo's graph) resolves inside the test's temp tree.
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	repoPath = t.TempDir()
	stateDir = daemon.StateDirForRepo(repoPath)
	if _, err := fbwriter.WriteGraphGen(stateDir, doc); err != nil {
		t.Fatalf("write fixture graph: %v", err)
	}
	return repoPath, stateDir
}

// TestScanRepo_MatchesDocumentReference is the byte-identity proof for the
// streaming scan: every number the mmap-backed single-pass scanner produces
// must equal the number the pre-change, fully-materialised Document code path
// produces for the same graph.
func TestScanRepo_MatchesDocumentReference(t *testing.T) {
	doc := fixtureDoc()
	_, stateDir := writeFixture(t, doc)

	got, err := ScanRepo(stateDir)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	want := ScanDocument(doc)

	if got != want {
		t.Fatalf("streaming scan diverged from the materialised reference:\n got  %+v\n want %+v", got, want)
	}
}

// TestScanDocument_MatchesAuditReference pins the audit-derived numbers
// (entities / orphans / IMPORTS hygiene) to audit.AuditPath — the function the
// daemon used to call — so collapsing the audit load cannot silently move the
// health-history orphan or bug rate.
func TestScanDocument_MatchesAuditReference(t *testing.T) {
	doc := fixtureDoc()
	repoPath, _ := writeFixture(t, doc)

	rep, err := audit.AuditPath(repoPath, false)
	if err != nil {
		t.Fatalf("audit.AuditPath: %v", err)
	}
	if len(rep.Repos) != 1 {
		t.Fatalf("audit repos = %d, want 1", len(rep.Repos))
	}
	rr := rep.Repos[0]

	got := ScanDocument(doc)
	if got.Entities != rr.Entities {
		t.Errorf("Entities = %d, want %d (audit)", got.Entities, rr.Entities)
	}
	if got.Orphans != rr.Orphans {
		t.Errorf("Orphans = %d, want %d (audit)", got.Orphans, rr.Orphans)
	}
	if got.ImportsTotal != rr.ImportsTotal {
		t.Errorf("ImportsTotal = %d, want %d (audit)", got.ImportsTotal, rr.ImportsTotal)
	}
	wantResolved := rr.ImportsToIDFormat[audit.ImportFormatHex] +
		rr.ImportsToIDFormat[audit.ImportFormatExtQualified]
	if got.ImportsResolved != wantResolved {
		t.Errorf("ImportsResolved = %d, want %d (audit)", got.ImportsResolved, wantResolved)
	}
}

// TestScanDocument_MatchesCoverageAndCyclesReference pins coverage and cycle
// counts to the canonical graph.ComputeCoverage / graph.FindImportCycles.
func TestScanDocument_MatchesCoverageAndCyclesReference(t *testing.T) {
	doc := fixtureDoc()
	got := ScanDocument(doc)

	cov := graph.ComputeCoverage(doc)
	if got.TotalProduction != cov.TotalProduction {
		t.Errorf("TotalProduction = %d, want %d", got.TotalProduction, cov.TotalProduction)
	}
	if got.CoveredProduction != cov.CoveredProduction {
		t.Errorf("CoveredProduction = %d, want %d", got.CoveredProduction, cov.CoveredProduction)
	}
	cycles := graph.FindImportCycles(doc.Entities, doc.Relationships, nil)
	if got.Cycles != len(cycles) {
		t.Errorf("Cycles = %d, want %d", got.Cycles, len(cycles))
	}
	if got.Cycles == 0 {
		t.Fatal("fixture must contain at least one import cycle or the assertion is vacuous")
	}
}

// TestScanRepo_OpensTheGraphExactlyOnce is the hazard assertion: the whole
// analytics batch for one repo must touch the on-disk graph a single time. The
// pre-change path opened and fully materialised it four times per repo.
func TestScanRepo_OpensTheGraphExactlyOnce(t *testing.T) {
	doc := fixtureDoc()
	_, stateDir := writeFixture(t, doc)

	opens := 0
	orig := openGraphView
	openGraphView = func(dir string) (graphView, error) {
		opens++
		return orig(dir)
	}
	t.Cleanup(func() { openGraphView = orig })

	if _, err := ScanRepo(stateDir); err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if opens != 1 {
		t.Fatalf("ScanRepo opened the graph %d times, want exactly 1 — the whole "+
			"analytics batch must share a single open view", opens)
	}
}

// TestScanRepo_NeverMaterialisesADocument is the memory hazard, pinned
// structurally: the scan must not go through the full-materialise loader.
func TestScanRepo_NeverMaterialisesADocument(t *testing.T) {
	doc := fixtureDoc()
	_, stateDir := writeFixture(t, doc)

	loads := 0
	orig := loadDocument
	loadDocument = func(dir string) (*graph.Document, error) {
		loads++
		return orig(dir)
	}
	t.Cleanup(func() { loadDocument = orig })

	if _, err := ScanRepo(stateDir); err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if loads != 0 {
		t.Fatalf("ScanRepo materialised a full graph.Document %d times; with a readable "+
			"graph.fb present it must scan the mmap and never materialise", loads)
	}
}

// TestScanRepo_FallsBackToDocumentWhenNoReadableFB keeps the JSON-only /
// version-incompatible repos working: no mmap view is available there, so the
// Document loader is the correct (and only) path.
func TestScanRepo_FallsBackToDocumentWhenNoReadableFB(t *testing.T) {
	doc := fixtureDoc()
	_, stateDir := writeFixture(t, doc)

	openGraphViewOrig := openGraphView
	openGraphView = func(string) (graphView, error) { return nil, errNoView }
	t.Cleanup(func() { openGraphView = openGraphViewOrig })

	got, err := ScanRepo(stateDir)
	if err != nil {
		t.Fatalf("ScanRepo fallback: %v", err)
	}
	if want := ScanDocument(doc); got != want {
		t.Fatalf("fallback scan = %+v, want %+v", got, want)
	}
}

// TestCoverageTotals_NameAffinityIsDirectoryScoped pins the SourceFile field to
// the set graph.CoverageTotalsFromView retains.
//
// THE HAZARD IS SILENT INFLATION, not a crash. Phase 3 (attributeByNameAffinity)
// credits a production subject when a test's normalised name token matches AND
// the two share a directory subtree. The streaming scan retains only four fields
// per entity; drop SourceFile from that set and every retained row reports dir
// "", sharesDirSubtree("", "") is true by its a == b branch, and the subtree gate
// stops discriminating entirely. On a real corpus that credits every same-token
// test/subject pair repo-wide and inflates CoveredProduction — a metric that only
// ever moves up, so nothing downstream would look wrong.
//
// A fixture where every entity lives under one directory cannot see this: the
// gate answers "same subtree" whether or not SourceFile survives. So this fixture
// is deliberately SPLIT — a same-dir pair that must be credited, and a
// cross-subtree pair that must not — and asserts the exact count, which is the
// only form of the assertion that separates the two answers.
func TestCoverageTotals_NameAffinityIsDirectoryScoped(t *testing.T) {
	ents := []graph.Entity{
		// Same directory: affinity SHOULD credit. This is the control — without
		// it, a scanner that credited nothing at all would also pass.
		{ID: "aaaaaaaaaaaaaaa1", Name: "AlphaService", Kind: "Class", SourceFile: "src/alpha.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb1", Name: "TestAlphaService", Kind: "SCOPE.Operation", SourceFile: "src/alpha_test.go", Language: "go"},
		// Disjoint subtrees ("src" vs "tools/qa", neither a prefix of the other):
		// same token, but affinity MUST decline.
		{ID: "aaaaaaaaaaaaaaa2", Name: "BetaService", Kind: "Class", SourceFile: "src/beta.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbb2", Name: "TestBetaService", Kind: "SCOPE.Operation", SourceFile: "tools/qa/beta_test.go", Language: "go"},
	}
	doc := &graph.Document{
		Version:  graph.SchemaVersion,
		Entities: ents,
		Stats:    graph.Stats{Entities: len(ents)},
	}
	_, stateDir := writeFixture(t, doc)

	got, err := ScanRepo(stateDir)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}

	if got.TotalProduction != 2 {
		t.Fatalf("TotalProduction = %d, want 2 — the fixture no longer holds exactly "+
			"one same-dir and one cross-subtree subject, so the counts below prove nothing",
			got.TotalProduction)
	}
	if got.CoveredProduction != 1 {
		t.Errorf("CoveredProduction = %d, want 1: AlphaService is credited by the "+
			"same-directory test; BetaService must NOT be credited by tools/qa/beta_test.go. "+
			"Got 2 => phase-3 name affinity is no longer directory-scoped on the streaming "+
			"path, which means CoverageTotalsFromView stopped retaining SourceFile.",
			got.CoveredProduction)
	}
}

// TestScan_KindRulesMatchTheOriginalInlineRules pins Endpoints, Flows and
// AuthUncovered to the rules they were extracted FROM.
//
// Those three fields are the only ones in RepoScan with no external reference.
// Every other number is proven against a function that pre-dates the change
// (audit.AuditPath, graph.ComputeCoverage, graph.FindImportCycles), but these
// three are computed by countKinds / accumulateRel / isAuthScopedEndpointKind —
// helpers this change introduced, called by BOTH scanView and ScanDocument. The
// differential tests therefore compare the new rules against themselves; a
// mis-transcription would have been invisible, and so would any later drift.
//
// The oracle below is transcribed by hand from the pre-change inline code
// (`git show 388305611:cmd/grafel/rebuild_history.go`) and deliberately calls
// NONE of the package's helpers — routing it through them would restore exactly
// the circularity it exists to break.
func TestScan_KindRulesMatchTheOriginalInlineRules(t *testing.T) {
	doc := fixtureDoc()

	// ── oracle: the original rules, verbatim ─────────────────────────────────
	// From the pre-change entity loop in appendRebuildHistory.
	wantEndpoints, wantFlows := 0, 0
	for _, e := range doc.Entities {
		switch e.Kind {
		case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
			wantEndpoints++
		}
		// isProcessFlow, verbatim — including the len>14 prefix test.
		isFlow := false
		switch e.Kind {
		case "process", "SCOPE.Process":
			isFlow = true
		default:
			isFlow = len(e.Kind) > 14 && e.Kind[:14] == "SCOPE.Process."
		}
		if isFlow {
			wantFlows++
		}
	}
	// From countAuthUncoveredEndpoints, verbatim — case-sensitive on the raw
	// kind, keyed on FromID, and over a NARROWER endpoint set than the tally.
	authed := make(map[string]bool, 16)
	for _, r := range doc.Relationships {
		if r.Kind == "HAS_AUTH" || r.Kind == "REQUIRES_AUTH" || r.Kind == "auth" {
			authed[r.FromID] = true
		}
	}
	wantAuthUncovered := 0
	for _, e := range doc.Entities {
		switch e.Kind {
		case "http_endpoint", "http_endpoint_definition":
			if !authed[e.ID] {
				wantAuthUncovered++
			}
		}
	}

	// ── the fixture must actually exercise each rule ─────────────────────────
	// Without these the three comparisons below could all pass on zeroes, and
	// the equalities would not discriminate between the rules and any weaker
	// rule that agrees with them on an empty set.
	if wantEndpoints == 0 || wantFlows == 0 || wantAuthUncovered == 0 {
		t.Fatalf("vacuous fixture: endpoints=%d flows=%d authUncovered=%d, all must be > 0",
			wantEndpoints, wantFlows, wantAuthUncovered)
	}
	// The endpoint TALLY must be strictly wider than the AUTH-SCOPED set, or the
	// http_endpoint_call carve-out is untested.
	if wantEndpoints <= wantAuthUncovered+len(authed) {
		t.Fatalf("fixture lacks an http_endpoint_call: the endpoint tally (%d) must exceed "+
			"the auth-scoped set (%d uncovered + %d authed)",
			wantEndpoints, wantAuthUncovered, len(authed))
	}
	// A wrong-case auth kind must be present and must have been REJECTED, or the
	// case sensitivity of the auth match is untested.
	sawWrongCase := false
	for _, r := range doc.Relationships {
		if strings.EqualFold(r.Kind, "REQUIRES_AUTH") && r.Kind != "REQUIRES_AUTH" {
			sawWrongCase = true
			if authed[r.FromID] {
				t.Fatalf("fixture entity %s is authed by another edge, so the wrong-case "+
					"edge %q proves nothing about case sensitivity", r.FromID, r.Kind)
			}
		}
	}
	if !sawWrongCase {
		t.Fatal("fixture lacks a wrong-case auth edge; the case-sensitive match is untested")
	}
	// A "SCOPE.Process"-prefixed near-miss must be present and NOT counted as a
	// flow, or the len>14 boundary is untested.
	sawNearMiss := false
	for _, e := range doc.Entities {
		if strings.HasPrefix(e.Kind, "SCOPE.Process") && e.Kind != "SCOPE.Process" &&
			!strings.HasPrefix(e.Kind, "SCOPE.Process.") {
			sawNearMiss = true
		}
	}
	if !sawNearMiss {
		t.Fatal("fixture lacks a SCOPE.Process* near-miss kind; the len>14 prefix " +
			"boundary in isProcessFlowKind is untested")
	}

	// ── both paths must agree with the oracle ────────────────────────────────
	_, stateDir := writeFixture(t, doc)
	streamed, err := ScanRepo(stateDir)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, tc := range []struct {
		path string
		got  RepoScan
	}{
		{"ScanDocument", ScanDocument(doc)},
		{"ScanRepo", streamed},
	} {
		if tc.got.Endpoints != wantEndpoints {
			t.Errorf("%s Endpoints = %d, want %d (original inline rule)",
				tc.path, tc.got.Endpoints, wantEndpoints)
		}
		if tc.got.Flows != wantFlows {
			t.Errorf("%s Flows = %d, want %d (original isProcessFlow)",
				tc.path, tc.got.Flows, wantFlows)
		}
		if tc.got.AuthUncovered != wantAuthUncovered {
			t.Errorf("%s AuthUncovered = %d, want %d (original countAuthUncoveredEndpoints)",
				tc.path, tc.got.AuthUncovered, wantAuthUncovered)
		}
	}
}

// TestTallyRepo_MatchesDocumentReference covers the agents-map stats tally,
// the fourth full load, which is now a kind-only streaming scan.
func TestTallyRepo_MatchesDocumentReference(t *testing.T) {
	doc := fixtureDoc()
	_, stateDir := writeFixture(t, doc)

	got, err := TallyRepo(stateDir)
	if err != nil {
		t.Fatalf("TallyRepo: %v", err)
	}
	if want := TallyDocument(doc); got != want {
		t.Fatalf("TallyRepo = %+v, want %+v", got, want)
	}
	if got.Entities != doc.Stats.Entities || got.Relationships != doc.Stats.Relationships {
		t.Errorf("TallyRepo counts = %d/%d, want %d/%d",
			got.Entities, got.Relationships, doc.Stats.Entities, doc.Stats.Relationships)
	}
}
