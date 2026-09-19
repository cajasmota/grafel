package audit

// bugrate_7271_test.go — the three states of the unresolved-import metric, and
// the single derivation both the terminal and the dashboard route through.

import "testing"

// TestBugRate_ThreeStates pins what the type exists for: a measured-bad rate, a
// measured-good rate, and "nothing was measured" — which must NOT answer like a
// measured zero, because that collapse is #7271.
func TestBugRate_ThreeStates(t *testing.T) {
	var unmeasured BugRate
	if unmeasured.Known() {
		t.Errorf("zero BugRate claims to be a measurement: %+v", unmeasured)
	}
	if p := unmeasured.PctPtr(); p != nil {
		t.Errorf("unmeasured PctPtr = %v, want nil", *p)
	}

	good := BugRate{TotalImports: 10, ResolvedImports: 10}
	if !good.Known() {
		t.Errorf("a 10-edge tally is a measurement: %+v", good)
	}
	if got := good.Pct(); got != 0 {
		t.Errorf("fully resolved Pct = %v, want 0", got)
	}
	if p := good.PctPtr(); p == nil || *p != 0 {
		t.Errorf("measured-zero PctPtr = %v, want a 0 value (not nil)", p)
	}

	bad := BugRate{TotalImports: 4, ResolvedImports: 3}
	if got := bad.Pct(); got != 25.0 {
		t.Errorf("1-of-4 unresolved Pct = %v, want 25", got)
	}
}

// TestBugRate_AddEdgeCountsOnlyImports guards the accumulator's selectivity in
// both directions: a non-IMPORTS edge with an unresolvable target must not
// inflate the rate, and an IMPORTS edge must be counted whatever the case of
// its kind (the graph carries both spellings; the audit pass uppercases).
func TestBugRate_AddEdgeCountsOnlyImports(t *testing.T) {
	var b BugRate
	b.AddEdge("CALLS", "./not-an-id")
	b.AddEdge("CONTAINS", "./not-an-id")
	if b.Known() {
		t.Fatalf("non-IMPORTS edges were tallied: %+v", b)
	}

	b.AddEdge("imports", "./unresolved")
	if b.TotalImports != 1 {
		t.Errorf("lowercase 'imports' not counted: %+v", b)
	}
	b.AddEdge("IMPORTS", "aaaaaaaaaaaaaaaa")
	b.AddEdge("IMPORTS", "ext:react:useState")
	b.AddEdge("IMPORTS", "ext:antd")
	if b.TotalImports != 4 {
		t.Errorf("TotalImports = %d, want 4: %+v", b.TotalImports, b)
	}
	// Resolved: the hex id and the qualified ext: placeholder. The bare ext:
	// and the path string are not addressable.
	if b.ResolvedImports != 2 {
		t.Errorf("ResolvedImports = %d, want 2: %+v", b.ResolvedImports, b)
	}
	if got := b.Pct(); got != 50.0 {
		t.Errorf("Pct = %v, want 50", got)
	}
}

// TestBugRate_FromReportMatchesAddEdge is the single-derivation pin. Callers
// that walked a graph (doctor, rebuild) accumulate with AddEdge; callers that
// audited a repo (the dashboard) build from the report histogram. Fed the same
// edges, the two entry points must produce the identical tally — otherwise the
// two surfaces can once again disagree about the same group.
func TestBugRate_FromReportMatchesAddEdge(t *testing.T) {
	edges := []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "ext:react:useState", "ext:antd", "./rel", "raw-token"}

	var walked BugRate
	rr := &RepoReport{ImportsToIDFormat: map[ImportFormat]int{}}
	for _, to := range edges {
		walked.AddEdge("IMPORTS", to)
		rr.ImportsTotal++
		rr.ImportsToIDFormat[ClassifyImportToID(to)]++
	}

	fromReport := BugRateFromReport(rr)
	if fromReport != walked {
		t.Errorf("BugRateFromReport = %+v, AddEdge tally = %+v", fromReport, walked)
	}
	if got := walked.Pct(); got != 50.0 {
		t.Errorf("Pct = %v, want 50 (3 of 6 unresolved)", got)
	}
	if BugRateFromReport(nil).Known() {
		t.Errorf("a nil report is not a measurement")
	}
}

// TestBugRate_AddMerges covers group aggregation over repos, including a repo
// that contributed nothing.
func TestBugRate_AddMerges(t *testing.T) {
	b := BugRate{TotalImports: 2, ResolvedImports: 1}
	b.Add(BugRate{TotalImports: 2, ResolvedImports: 2})
	b.Add(BugRate{})
	if b != (BugRate{TotalImports: 4, ResolvedImports: 3}) {
		t.Fatalf("merged tally = %+v, want {4 3}", b)
	}
	if got := b.Pct(); got != 25.0 {
		t.Errorf("merged Pct = %v, want 25", got)
	}
}
