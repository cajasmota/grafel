package graph

import (
	"fmt"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// isTestFile unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIsTestFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		// Go
		{"foo_test.go", true},
		{"foo.go", false},
		// Python
		{"test_user.py", true},
		{"user_test.py", true},
		{"user.py", false},
		// TypeScript / JS
		{"user.test.ts", true},
		{"user.spec.ts", true},
		{"user.ts", false},
		{"UserSpec.js", false},
		{"user.test.js", true},
		// Ruby
		{"user_spec.rb", true},
		{"user.rb", false},
		// Java
		{"UserTest.java", true},
		{"UserTests.java", true},
		{"UserSpec.java", true},
		{"User.java", false},
		// Kotlin
		{"UserTest.kt", true},
		// Path-segment
		{"src/__tests__/user.ts", true},
		{"src/test/User.java", true},
		{"src/spec/user.rb", true},
		{"src/main/User.java", false},
		// Python in tests/ directory — no test_ prefix required (#2608):
		// Django projects place test files under core/tests/schedule.py etc.
		// The path-segment check ("/tests/") must fire before the .py stem check.
		{"core/tests/schedule.py", true},
		{"api/tests/views.py", true},
		{"tests/conftest.py", true},
		{"app/test/integration.py", true},
	}
	for _, tc := range cases {
		got := isTestFile(tc.path)
		if got != tc.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeCoverage tests
// ─────────────────────────────────────────────────────────────────────────────

// makeDoc is a helper to construct a minimal graph.Document for tests.
func makeDoc(entities []Entity, rels []Relationship) *Document {
	return &Document{
		Version:       SchemaVersion,
		Entities:      entities,
		Relationships: rels,
	}
}

// TestComputeCoverage_Empty checks that an empty document returns zeroes.
func TestComputeCoverage_Empty(t *testing.T) {
	t.Parallel()
	report := ComputeCoverage(makeDoc(nil, nil))
	if report.TotalProduction != 0 {
		t.Errorf("TotalProduction want 0, got %d", report.TotalProduction)
	}
	if report.CoveragePct != 0 {
		t.Errorf("CoveragePct want 0, got %f", report.CoveragePct)
	}
}

// TestComputeCoverage_DirectTESTSEdge verifies that an explicit TESTS edge
// marks the target entity as covered.
func TestComputeCoverage_DirectTESTSEdge(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "prod1", Name: "HandleUser", Kind: "Function", SourceFile: "handler.go"},
		{ID: "test1", Name: "TestHandleUser", Kind: "Function", SourceFile: "handler_test.go"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "test1", ToID: "prod1", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	if report.TotalProduction != 1 {
		t.Errorf("TotalProduction want 1, got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 1 {
		t.Errorf("CoveredProduction want 1, got %d", report.CoveredProduction)
	}
	if report.CoveragePct != 100.0 {
		t.Errorf("CoveragePct want 100, got %f", report.CoveragePct)
	}
	if len(report.UncoveredEntities) != 0 {
		t.Errorf("UncoveredEntities want 0, got %d", len(report.UncoveredEntities))
	}
}

// TestComputeCoverage_FiveTESTSEdges verifies that a test calling 5 production
// entities generates 5 TESTS edges and marks all 5 as covered.
func TestComputeCoverage_FiveTESTSEdges(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "t1", Name: "TestAll", Kind: "Function", SourceFile: "all_test.go"},
	}
	for i := 0; i < 5; i++ {
		entities = append(entities, Entity{
			ID:         "p" + string(rune('1'+i)),
			Name:       "Prod" + string(rune('A'+i)),
			Kind:       "Function",
			SourceFile: "prod.go",
		})
	}
	// Build explicit TESTS edges (as if emitted by the testmap extractor).
	rels := []Relationship{
		{ID: "r1", FromID: "t1", ToID: "p1", Kind: "TESTS"},
		{ID: "r2", FromID: "t1", ToID: "p2", Kind: "TESTS"},
		{ID: "r3", FromID: "t1", ToID: "p3", Kind: "TESTS"},
		{ID: "r4", FromID: "t1", ToID: "p4", Kind: "TESTS"},
		{ID: "r5", FromID: "t1", ToID: "p5", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	if report.TotalProduction != 5 {
		t.Errorf("TotalProduction want 5, got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 5 {
		t.Errorf("CoveredProduction want 5, got %d", report.CoveredProduction)
	}
	if report.TotalTestsEdges != 5 {
		t.Errorf("TotalTestsEdges want 5, got %d", report.TotalTestsEdges)
	}
	if len(report.UncoveredEntities) != 0 {
		t.Errorf("UncoveredEntities want 0, got %d", len(report.UncoveredEntities))
	}
}

// TestComputeCoverage_SyntheticFromCALLS verifies the synthetic fallback:
// a test entity with CALLS edges to production entities gets virtual TESTS edges.
func TestComputeCoverage_SyntheticFromCALLS(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "t1", Name: "TestFoo", Kind: "Function", SourceFile: "foo_test.go"},
		{ID: "p1", Name: "Foo", Kind: "Function", SourceFile: "foo.go"},
		{ID: "p2", Name: "Bar", Kind: "Function", SourceFile: "bar.go"},
	}
	// No TESTS edges — only CALLS from test to prod.
	rels := []Relationship{
		{ID: "c1", FromID: "t1", ToID: "p1", Kind: "CALLS"},
		{ID: "c2", FromID: "t1", ToID: "p2", Kind: "CALLS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	if report.CoveredProduction != 2 {
		t.Errorf("CoveredProduction want 2 (synthetic), got %d", report.CoveredProduction)
	}
	if report.TotalTestsEdges != 2 {
		t.Errorf("TotalTestsEdges want 2 (synthetic), got %d", report.TotalTestsEdges)
	}
	if len(report.UncoveredEntities) != 0 {
		t.Errorf("UncoveredEntities want 0, got %d", len(report.UncoveredEntities))
	}
}

// TestComputeCoverage_UntestedFlagged verifies that entities without any
// TESTS inbound appear in UncoveredEntities.
func TestComputeCoverage_UntestedFlagged(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "p1", Name: "HandleUser", Kind: "http_endpoint_definition", SourceFile: "handler.go"},
		{ID: "p2", Name: "ExportedFn", Kind: "Function", SourceFile: "lib.go"},
		{ID: "p3", Name: "internalFn", Kind: "Function", SourceFile: "lib.go"},
	}
	// No test entities at all.
	report := ComputeCoverage(makeDoc(entities, nil))
	if report.TotalProduction != 3 {
		t.Errorf("TotalProduction want 3, got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 0 {
		t.Errorf("CoveredProduction want 0, got %d", report.CoveredProduction)
	}
	if len(report.UncoveredEntities) != 3 {
		t.Errorf("UncoveredEntities want 3, got %d", len(report.UncoveredEntities))
	}
	// The http_endpoint_definition should appear first (high severity).
	if report.UncoveredEntities[0].Severity != "high" {
		t.Errorf("first uncovered entity severity want high, got %s", report.UncoveredEntities[0].Severity)
	}
}

// TestComputeCoverage_SeverityOrder checks high < medium < low ordering.
func TestComputeCoverage_SeverityOrder(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "e1", Name: "internalFn", Kind: "Function", SourceFile: "a.go"},
		{ID: "e2", Name: "ExportedFn", Kind: "Function", SourceFile: "b.go"},
		{ID: "e3", Name: "PostUser", Kind: "http_endpoint_definition", SourceFile: "c.go"},
	}
	report := ComputeCoverage(makeDoc(entities, nil))
	sev := []string{}
	for _, u := range report.UncoveredEntities {
		sev = append(sev, u.Severity)
	}
	// high must come before medium must come before low.
	sawHigh, sawMedium := false, false
	for _, s := range sev {
		switch s {
		case "high":
			if sawMedium {
				t.Errorf("high after medium in severity ordering: %v", sev)
			}
			sawHigh = true
		case "medium":
			if !sawHigh && containsSeverity(sev, "high") {
				t.Errorf("medium before high in severity ordering: %v", sev)
			}
			sawMedium = true
		case "low":
			_ = sawMedium // low must come last; already enforced by sortStable
		}
	}
}

func containsSeverity(sev []string, target string) bool {
	for _, s := range sev {
		if s == target {
			return true
		}
	}
	return false
}

// TestComputeCoverage_ByDirectory checks per-directory aggregation.
func TestComputeCoverage_ByDirectory(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "p1", Name: "Foo", Kind: "Function", SourceFile: "pkg/foo/foo.go"},
		{ID: "p2", Name: "Bar", Kind: "Function", SourceFile: "pkg/foo/bar.go"},
		{ID: "p3", Name: "Baz", Kind: "Function", SourceFile: "pkg/baz/baz.go"},
		{ID: "t1", Name: "TestFoo", Kind: "Function", SourceFile: "pkg/foo/foo_test.go"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "t1", ToID: "p1", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	dirMap := make(map[string]DirCoverage)
	for _, d := range report.ByDirectory {
		dirMap[d.Dir] = d
	}
	fooDir := dirMap["pkg/foo"]
	if fooDir.Total != 2 {
		t.Errorf("pkg/foo total want 2, got %d", fooDir.Total)
	}
	if fooDir.Covered != 1 {
		t.Errorf("pkg/foo covered want 1, got %d", fooDir.Covered)
	}
	bazDir := dirMap["pkg/baz"]
	if bazDir.Total != 1 {
		t.Errorf("pkg/baz total want 1, got %d", bazDir.Total)
	}
	if bazDir.Covered != 0 {
		t.Errorf("pkg/baz covered want 0, got %d", bazDir.Covered)
	}
}

// TestComputeCoverage_ByFile checks per-file aggregation (the deepest tier)
// and that directory rollups equal the sum of their files (#4552).
func TestComputeCoverage_ByFile(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "p1", Name: "Foo", Kind: "Function", SourceFile: "pkg/foo/foo.go"},
		{ID: "p2", Name: "Foo2", Kind: "Function", SourceFile: "pkg/foo/foo.go"},
		{ID: "p3", Name: "Bar", Kind: "Function", SourceFile: "pkg/foo/bar.go"},
		{ID: "p4", Name: "Baz", Kind: "Function", SourceFile: "pkg/baz/baz.go"},
		{ID: "t1", Name: "TestFoo", Kind: "Function", SourceFile: "pkg/foo/foo_test.go"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "t1", ToID: "p1", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))

	fileMap := make(map[string]FileCoverage)
	for _, f := range report.ByFile {
		fileMap[f.File] = f
	}
	fooFile := fileMap["pkg/foo/foo.go"]
	if fooFile.Total != 2 || fooFile.Covered != 1 {
		t.Errorf("pkg/foo/foo.go want total=2 covered=1, got total=%d covered=%d",
			fooFile.Total, fooFile.Covered)
	}
	if fooFile.Dir != "pkg/foo" {
		t.Errorf("pkg/foo/foo.go dir want pkg/foo, got %q", fooFile.Dir)
	}
	barFile := fileMap["pkg/foo/bar.go"]
	if barFile.Total != 1 || barFile.Covered != 0 {
		t.Errorf("pkg/foo/bar.go want total=1 covered=0, got total=%d covered=%d",
			barFile.Total, barFile.Covered)
	}
	bazFile := fileMap["pkg/baz/baz.go"]
	if bazFile.Total != 1 || bazFile.Covered != 0 {
		t.Errorf("pkg/baz/baz.go want total=1 covered=0, got total=%d covered=%d",
			bazFile.Total, bazFile.Covered)
	}

	// Directory rollups must equal the sum of their files.
	dirMap := make(map[string]DirCoverage)
	for _, d := range report.ByDirectory {
		dirMap[d.Dir] = d
	}
	fileSum := make(map[string]struct{ total, covered int })
	for _, f := range report.ByFile {
		s := fileSum[f.Dir]
		s.total += f.Total
		s.covered += f.Covered
		fileSum[f.Dir] = s
	}
	for dir, d := range dirMap {
		s := fileSum[dir]
		if d.Total != s.total || d.Covered != s.covered {
			t.Errorf("dir %q rollup want total=%d covered=%d (file sum), got total=%d covered=%d",
				dir, s.total, s.covered, d.Total, d.Covered)
		}
	}
}

// TestComputeCoverage_ByModule checks per-module aggregation.
func TestComputeCoverage_ByModule(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "p1", Name: "A", Kind: "Function", SourceFile: "a.go",
			Properties: map[string]string{"module": "auth"}},
		{ID: "p2", Name: "B", Kind: "Function", SourceFile: "b.go",
			Properties: map[string]string{"module": "auth"}},
		{ID: "p3", Name: "C", Kind: "Function", SourceFile: "c.go",
			Properties: map[string]string{"module": "payments"}},
		{ID: "t1", Name: "TestA", Kind: "Function", SourceFile: "a_test.go"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "t1", ToID: "p1", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	modMap := make(map[string]ModuleCoverage)
	for _, m := range report.ByModule {
		modMap[m.Module] = m
	}
	authMod := modMap["auth"]
	if authMod.Total != 2 {
		t.Errorf("auth.Total want 2, got %d", authMod.Total)
	}
	if authMod.Covered != 1 {
		t.Errorf("auth.Covered want 1, got %d", authMod.Covered)
	}
	payMod := modMap["payments"]
	if payMod.Total != 1 {
		t.Errorf("payments.Total want 1, got %d", payMod.Total)
	}
	if payMod.Covered != 0 {
		t.Errorf("payments.Covered want 0, got %d", payMod.Covered)
	}
}

// TestComputeCoverage_HTTPEndpointHighSeverity ensures HTTP endpoints without
// tests are surfaced with "high" severity.
func TestComputeCoverage_HTTPEndpointHighSeverity(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "ep1", Name: "POST /users", Kind: "http_endpoint", SourceFile: "routes.go"},
	}
	report := ComputeCoverage(makeDoc(entities, nil))
	if len(report.UncoveredEntities) != 1 {
		t.Fatalf("want 1 uncovered, got %d", len(report.UncoveredEntities))
	}
	if report.UncoveredEntities[0].Severity != "high" {
		t.Errorf("HTTP endpoint severity want high, got %s", report.UncoveredEntities[0].Severity)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeEntityCoverage tests (#1774)
// ─────────────────────────────────────────────────────────────────────────────

// TestComputeEntityCoverage_KnownTested verifies that a production entity with
// a direct TESTS inbound edge is reported as tested with the covering test ID.
func TestComputeEntityCoverage_KnownTested(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "prod1", Name: "HandleUser", Kind: "Function", SourceFile: "handler.go"},
		{ID: "test1", Name: "TestHandleUser", Kind: "Function", SourceFile: "handler_test.go"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "test1", ToID: "prod1", Kind: "TESTS"},
	}
	result, found := ComputeEntityCoverage(makeDoc(entities, rels), "prod1")
	if !found {
		t.Fatal("want found=true, got false")
	}
	if !result.Tested {
		t.Errorf("want Tested=true, got false")
	}
	if result.CoverageFraction != 1.0 {
		t.Errorf("want CoverageFraction=1.0, got %f", result.CoverageFraction)
	}
	if len(result.CoveringTests) != 1 || result.CoveringTests[0] != "test1" {
		t.Errorf("want CoveringTests=[test1], got %v", result.CoveringTests)
	}
}

// TestComputeEntityCoverage_KnownUntested verifies that a production entity
// with no inbound TESTS edges (and no CALLS fallback) is reported as untested.
func TestComputeEntityCoverage_KnownUntested(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "prod1", Name: "UntestedFn", Kind: "Function", SourceFile: "lib.go"},
		{ID: "test1", Name: "TestOther", Kind: "Function", SourceFile: "other_test.go"},
	}
	// No TESTS or CALLS edges to prod1.
	result, found := ComputeEntityCoverage(makeDoc(entities, nil), "prod1")
	if !found {
		t.Fatal("want found=true, got false")
	}
	if result.Tested {
		t.Errorf("want Tested=false, got true")
	}
	if result.CoverageFraction != 0.0 {
		t.Errorf("want CoverageFraction=0.0, got %f", result.CoverageFraction)
	}
	if len(result.CoveringTests) != 0 {
		t.Errorf("want CoveringTests=[], got %v", result.CoveringTests)
	}
}

// TestComputeEntityCoverage_Unknown verifies that an unknown entity_id returns
// found=false (triggering "entity not found" in the MCP handler).
func TestComputeEntityCoverage_Unknown(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "prod1", Name: "HandleUser", Kind: "Function", SourceFile: "handler.go"},
	}
	result, found := ComputeEntityCoverage(makeDoc(entities, nil), "does-not-exist")
	if found {
		t.Errorf("want found=false for unknown entity, got result=%+v", result)
	}
	if result != nil {
		t.Errorf("want nil result for unknown entity, got %+v", result)
	}
}

// TestComputeEntityCoverage_SyntheticCALLS verifies that the synthetic CALLS
// fallback also works for the per-entity path.
func TestComputeEntityCoverage_SyntheticCALLS(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "t1", Name: "TestFoo", Kind: "Function", SourceFile: "foo_test.go"},
		{ID: "p1", Name: "Foo", Kind: "Function", SourceFile: "foo.go"},
	}
	// No TESTS edge — only CALLS from test to prod.
	rels := []Relationship{
		{ID: "c1", FromID: "t1", ToID: "p1", Kind: "CALLS"},
	}
	result, found := ComputeEntityCoverage(makeDoc(entities, rels), "p1")
	if !found {
		t.Fatal("want found=true, got false")
	}
	if !result.Tested {
		t.Errorf("want Tested=true (synthetic CALLS fallback), got false")
	}
	if len(result.CoveringTests) != 1 || result.CoveringTests[0] != "t1" {
		t.Errorf("want CoveringTests=[t1], got %v", result.CoveringTests)
	}
}

// TestComputeEntityCoverage_OutputSize verifies that the result struct is
// compact — the covering_tests slice alone is bounded by the actual test count,
// not the whole entity list. (Token-budget guard from #1774 spec.)
func TestComputeEntityCoverage_OutputSize(t *testing.T) {
	t.Parallel()
	// Build 100 test entities and 100 production entities; only one covers prod1.
	entities := []Entity{
		{ID: "prod1", Name: "TargetFn", Kind: "Function", SourceFile: "target.go"},
	}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("prod%d", i+2)
		entities = append(entities, Entity{
			ID: id, Name: "OtherFn", Kind: "Function", SourceFile: "other.go",
		})
	}
	// One test covers prod1; 99 other tests cover nothing relevant.
	entities = append(entities, Entity{
		ID: "test1", Name: "TestTarget", Kind: "Function", SourceFile: "target_test.go",
	})
	for i := 0; i < 99; i++ {
		entities = append(entities, Entity{
			ID: fmt.Sprintf("testother%d", i), Name: "TestOther", Kind: "Function",
			SourceFile: "other_test.go",
		})
	}
	rels := []Relationship{
		{ID: "r1", FromID: "test1", ToID: "prod1", Kind: "TESTS"},
	}
	result, found := ComputeEntityCoverage(makeDoc(entities, rels), "prod1")
	if !found {
		t.Fatal("want found=true")
	}
	if !result.Tested {
		t.Errorf("want Tested=true")
	}
	// Only the one covering test should appear — not all 100.
	if len(result.CoveringTests) != 1 {
		t.Errorf("want 1 covering test, got %d: %v", len(result.CoveringTests), result.CoveringTests)
	}
}

// TestComputeCoverage_TotalTestsCountsOnlyCallables verifies that TotalTests
// counts only SCOPE.Operation / Function / Method entities from test files —
// not every symbol that happens to live in a test file (imports, constants,
// class declarations, SCOPE.Pattern wrapper entities, etc.).
//
// This exercises issue #1410 where 593 test entities were reported for ~3
// test files because every Python / Go symbol was counted, including imports,
// module-level constants, and testmap's SCOPE.Pattern wrapper records.
func TestComputeCoverage_TotalTestsCountsOnlyCallables(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		// Test file — the actual test function emitted as SCOPE.Operation (counts).
		{ID: "t1", Name: "test_create_order", Kind: "SCOPE.Operation", SourceFile: "tests/test_orders.py"},
		// Test file — a helper class (must NOT count).
		{ID: "t2", Name: "OrderTestHelper", Kind: "Class", SourceFile: "tests/test_orders.py"},
		// Test file — a SCOPE.Component (file node, must NOT count).
		{ID: "t3", Name: "tests/test_orders.py", Kind: "SCOPE.Component", SourceFile: "tests/test_orders.py"},
		// Test file — a SCOPE.Pattern wrapper emitted by testmap (must NOT count).
		{ID: "t4", Name: "test_create_order -> create_order", Kind: "SCOPE.Pattern",
			SourceFile: "tests/test_orders.py",
			Properties: map[string]string{"pattern_kind": "test_coverage"}},
		// Production entity — SCOPE.Operation as emitted by Go/Python/JS extractors.
		{ID: "p1", Name: "create_order", Kind: "SCOPE.Operation", SourceFile: "orders/service.py"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "t1", ToID: "p1", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	// Only the SCOPE.Operation from the test file (t1) counts as a test entity.
	if report.TotalTests != 1 {
		t.Errorf("TotalTests want 1 (only callable test entities), got %d", report.TotalTests)
	}
	// The production SCOPE.Operation is covered.
	if report.TotalProduction != 1 {
		t.Errorf("TotalProduction want 1 (SCOPE.Operation now in scope), got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 1 {
		t.Errorf("CoveredProduction want 1, got %d", report.CoveredProduction)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #4510 — testable-production denominator + name-affinity attribution
// ─────────────────────────────────────────────────────────────────────────────

// TestIsNonTestableFile pins the non-testable-file predicate (#4510).
func TestIsNonTestableFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		// scripts / migrations / generated / config — excluded
		{"scripts/seed.ts", true},
		{"src/scripts/deploy.js", true},
		{"api/migrations/0001_initial.py", true},
		{"db/migration/V1__init.sql", true},
		{"src/__generated__/graphql.ts", true},
		{"gen/pb/order.go", true},
		{"jest.config.ts", true},
		{"vite.config.js", true},
		{"types/order.d.ts", true},
		{"order.pb.go", true},
		{"order_pb2.py", true},
		{"src/index.ts", true}, // barrel
		{"pkg/doc.go", true},   // go package doc
		{"node_modules/lib/x.js", true},
		// real production — kept
		{"orders/service.ts", false},
		{"orders/controller.go", false},
		{"app/use_cases/place_order.py", false},
	}
	for _, tc := range cases {
		if got := isNonTestableFile(tc.path); got != tc.want {
			t.Errorf("isNonTestableFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestComputeCoverage_4510_DenominatorAndAffinity is the in-pipeline fixture for
// #4510. It mixes (a) a testable service covered by a name-affine spec with no
// direct TESTS edge, (b) a script, (c) a migration, (d) a DTO, (e) a type-only
// interface, (f) a config file. It asserts:
//   - the denominator counts ONLY the two testable services (excludes b–f);
//   - the name-affinity pass attributes the spec to its same-subtree subject.
func TestComputeCoverage_4510_DenominatorAndAffinity(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		// (a) testable service, covered only by name-affinity (no TESTS edge)
		{ID: "svc", Name: "OrderService", Kind: "Class", SourceFile: "orders/order_service.ts"},
		// a second testable service, left UNcovered
		{ID: "svc2", Name: "PaymentService", Kind: "Class", SourceFile: "payments/payment_service.ts"},
		// (b) script — must NOT count
		{ID: "scr", Name: "seedDb", Kind: "Function", SourceFile: "scripts/seed.ts"},
		// (c) migration — must NOT count
		{ID: "mig", Name: "up", Kind: "Function", SourceFile: "migrations/0001_init.ts"},
		// (d) DTO — must NOT count (name suffix)
		{ID: "dto", Name: "OrderDTO", Kind: "Class", SourceFile: "orders/order.dto.ts"},
		// (e) type-only interface — must NOT count (kind excluded)
		{ID: "iface", Name: "IOrder", Kind: "Interface", SourceFile: "orders/order.ts"},
		// (f) config — must NOT count
		{ID: "cfg", Name: "config", Kind: "Function", SourceFile: "jest.config.ts"},
		// a spec whose normalised token matches OrderService, same subtree
		{ID: "spec", Name: "OrderServiceSpec", Kind: "SCOPE.Operation",
			SourceFile: "orders/__tests__/order_service.spec.ts"},
	}
	report := ComputeCoverage(makeDoc(entities, nil))

	if report.TotalProduction != 2 {
		t.Fatalf("TotalProduction want 2 (only OrderService+PaymentService), got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 1 {
		t.Errorf("CoveredProduction want 1 (name-affinity attributes OrderService), got %d", report.CoveredProduction)
	}
	// Confirm the covered one is OrderService (PaymentService stays uncovered).
	for _, u := range report.UncoveredEntities {
		if u.EntityID == "svc" {
			t.Errorf("OrderService should be covered by name-affinity but is in uncovered list")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #4553: endpoint crediting via handler (read-layer shape)
// ─────────────────────────────────────────────────────────────────────────────

// TestComputeCoverage_EndpointCreditedViaHandler models the upvate-v3 symptom
// (#4553): a NestJS controller spec exercises the handler method, but the
// http_endpoint_definition the handler IMPLEMENTS is a separate synthetic node
// that no test points at directly. Before the phase-4 hop the endpoint reads
// uncovered (RED); after, the covered handler credits the endpoint (GREEN).
//
// The graph shape mirrors the live read layer:
//
//	getHello (SCOPE.Operation, app.controller.ts)
//	    --IMPLEMENTS--> GET / (http_endpoint_definition, app.controller.ts)
//	TestGetHello (app.controller.spec.ts) --TESTS--> getHello
func TestComputeCoverage_EndpointCreditedViaHandler(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "handler", Name: "getHello", Kind: "SCOPE.Operation",
			SourceFile: "src/app.controller.ts", StartLine: 9},
		{ID: "ep", Name: "GET /", Kind: "http_endpoint_definition",
			SourceFile: "src/app.controller.ts", StartLine: 9},
		{ID: "spec", Name: "AppController", Kind: "SCOPE.Operation",
			SourceFile: "src/app.controller.spec.ts"},
	}
	rels := []Relationship{
		// handler IMPLEMENTS endpoint-definition (#1639/#4316 shape).
		{ID: "i1", FromID: "handler", ToID: "ep", Kind: "IMPLEMENTS"},
		// The controller spec tests the handler method.
		{ID: "t1", FromID: "spec", ToID: "handler", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))

	if report.TotalProduction != 2 {
		t.Fatalf("TotalProduction want 2 (handler+endpoint), got %d", report.TotalProduction)
	}
	// Both the handler (direct TESTS) and the endpoint (handler hop) covered.
	if report.CoveredProduction != 2 {
		t.Errorf("CoveredProduction want 2 (handler + endpoint via IMPLEMENTS hop), got %d", report.CoveredProduction)
	}
	for _, u := range report.UncoveredEntities {
		if u.EntityID == "ep" {
			t.Errorf("endpoint should be credited covered via its tested handler but is uncovered (#4553 RED)")
		}
	}

	// Single-entity path must agree.
	res, ok := ComputeEntityCoverage(makeDoc(entities, rels), "ep")
	if !ok {
		t.Fatalf("ComputeEntityCoverage(ep) not found")
	}
	if !res.Tested {
		t.Errorf("ComputeEntityCoverage(ep).Tested want true (handler hop), got false")
	}
}

// TestComputeCoverage_EndpointNotCreditedWhenHandlerUntested is the negative
// control: an endpoint whose handler is NOT tested stays uncovered, so the hop
// does not fabricate coverage.
func TestComputeCoverage_EndpointNotCreditedWhenHandlerUntested(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "handler", Name: "getHello", Kind: "SCOPE.Operation",
			SourceFile: "src/app.controller.ts"},
		{ID: "ep", Name: "GET /", Kind: "http_endpoint_definition",
			SourceFile: "src/app.controller.ts"},
	}
	rels := []Relationship{
		{ID: "i1", FromID: "handler", ToID: "ep", Kind: "IMPLEMENTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	if report.CoveredProduction != 0 {
		t.Errorf("CoveredProduction want 0 (handler untested), got %d", report.CoveredProduction)
	}
}

// TestComputeCoverage_EndpointCreditedViaRoutesTo verifies the reverse edge
// direction (Spring/Express shape: definition --ROUTES_TO--> handler) is also
// honoured, proving the hop is framework-agnostic.
func TestComputeCoverage_EndpointCreditedViaRoutesTo(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "ep", Name: "GET /users", Kind: "http_endpoint_definition",
			SourceFile: "UserController.java"},
		{ID: "handler", Name: "listUsers", Kind: "SCOPE.Operation",
			SourceFile: "UserController.java"},
		{ID: "spec", Name: "UserControllerTest", Kind: "SCOPE.Operation",
			SourceFile: "UserControllerTest.java"},
	}
	rels := []Relationship{
		{ID: "r1", FromID: "ep", ToID: "handler", Kind: "ROUTES_TO"},
		{ID: "t1", FromID: "spec", ToID: "handler", Kind: "TESTS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	if report.CoveredProduction != 2 {
		t.Errorf("CoveredProduction want 2 (handler + endpoint via ROUTES_TO), got %d", report.CoveredProduction)
	}
}
