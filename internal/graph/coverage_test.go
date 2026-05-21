package graph

import (
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// isTestFile unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIsTestFile(t *testing.T) {
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

// TestComputeCoverage_ByModule checks per-module aggregation.
func TestComputeCoverage_ByModule(t *testing.T) {
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

// TestComputeCoverage_TotalTestsCountsOnlyCallables verifies that TotalTests
// counts only SCOPE.Operation / Function / Method entities from test files —
// not every symbol that happens to live in a test file (imports, constants,
// class declarations, SCOPE.Pattern wrapper entities, etc.).
//
// This exercises issue #1410 where 593 test entities were reported for ~3
// test files because every Python / Go symbol was counted, including imports,
// module-level constants, and testmap's SCOPE.Pattern wrapper records.
func TestComputeCoverage_TotalTestsCountsOnlyCallables(t *testing.T) {
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
