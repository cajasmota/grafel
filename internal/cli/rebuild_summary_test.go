package cli

// Tests for issue #989 — rich rebuild summary table generation and rendering.
// These tests exercise the non-filesystem parts (formatting, topNKinds, fmtInt)
// so they run without any on-disk artefacts.

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// fmtInt
// ---------------------------------------------------------------------------

func TestFmtInt_SmallValues(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{1001, "1,001"},
		{9999, "9,999"},
		{10000, "10,000"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
		{20718, "20,718"},
		{95213, "95,213"},
	}
	for _, tc := range cases {
		got := fmtInt(tc.n)
		if got != tc.want {
			t.Errorf("fmtInt(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFmtInt_Negative(t *testing.T) {
	got := fmtInt(-1234)
	if got != "-1,234" {
		t.Errorf("fmtInt(-1234) = %q, want %q", got, "-1,234")
	}
}

// ---------------------------------------------------------------------------
// topNKinds
// ---------------------------------------------------------------------------

func TestTopNKinds_EmptyMap(t *testing.T) {
	rows, other := topNKinds(map[string]int{}, 5)
	if len(rows) != 0 {
		t.Errorf("expected empty rows, got %v", rows)
	}
	if other != 0 {
		t.Errorf("expected other=0, got %d", other)
	}
}

func TestTopNKinds_FewerThanN(t *testing.T) {
	m := map[string]int{"Function": 10, "Class": 5}
	rows, other := topNKinds(m, 5)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if other != 0 {
		t.Errorf("expected other=0, got %d", other)
	}
	// Sorted by count desc.
	if rows[0].Kind != "Function" || rows[0].Count != 10 {
		t.Errorf("row[0] = %+v, want Function/10", rows[0])
	}
}

func TestTopNKinds_MoreThanN(t *testing.T) {
	m := map[string]int{
		"Function":    4892,
		"Class":       1231,
		"Variable":    8402,
		"HTTPEndpoint": 744,
		"Other1":      3000,
		"Other2":      2000,
		"Other3":      1000,
	}
	rows, other := topNKinds(m, 5)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	// Top 5 by count: Variable(8402), Function(4892), Other1(3000), Other2(2000), Class(1231).
	if rows[0].Kind != "Variable" {
		t.Errorf("row[0].Kind = %q, want Variable", rows[0].Kind)
	}
	if rows[0].Count != 8402 {
		t.Errorf("row[0].Count = %d, want 8402", rows[0].Count)
	}
	// Other = HTTPEndpoint(744) + Other3(1000) = 1744.
	wantOther := 744 + 1000
	if other != wantOther {
		t.Errorf("other = %d, want %d", other, wantOther)
	}
}

func TestTopNKinds_TieBreakByName(t *testing.T) {
	// Equal counts should be sorted alphabetically.
	m := map[string]int{"Beta": 5, "Alpha": 5}
	rows, _ := topNKinds(m, 5)
	if rows[0].Kind != "Alpha" {
		t.Errorf("expected Alpha first on tie, got %q", rows[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// maxKindLen
// ---------------------------------------------------------------------------

func TestMaxKindLen(t *testing.T) {
	rows := []kindRow{{Kind: "Function"}, {Kind: "Class"}, {Kind: "HTTPEndpoint"}}
	got := maxKindLen(rows, false)
	if got != len("HTTPEndpoint") {
		t.Errorf("maxKindLen = %d, want %d", got, len("HTTPEndpoint"))
	}
}

func TestMaxKindLen_WithOther(t *testing.T) {
	// When withOther=true the minimum width is 5 (len("Other")).
	rows := []kindRow{{Kind: "A"}}
	got := maxKindLen(rows, true)
	if got != 5 {
		t.Errorf("maxKindLen = %d, want 5", got)
	}
}

// ---------------------------------------------------------------------------
// PrintRebuildSummary
// ---------------------------------------------------------------------------

func TestPrintRebuildSummary_BasicShape(t *testing.T) {
	s := &RebuildSummary{
		Group:              "mygroup",
		TotalEntities:      20718,
		TotalRelationships: 95213,
		EntityByKind: map[string]int{
			"Function":     4892,
			"Class":        1231,
			"Variable":     8402,
			"HTTPEndpoint": 744,
			"Other":        5449,
		},
		RelByKind: map[string]int{
			"CALLS":      12818,
			"IMPORTS":    8127,
			"REFERENCES": 61902,
			"CONTAINS":   6219,
			"Other":      6147,
		},
		CrossRepoEdges:       234,
		ProcessFlows:         50,
		HTTPEndpoints:        744,
		EnrichmentCandidates: 89,
		RepairCandidates:     12,
		OrphanEntities:       1341,
		OrphanRate:           6.5,
		Elapsed:              17 * time.Second,
	}

	var buf bytes.Buffer
	PrintRebuildSummary(&buf, s)
	out := buf.String()

	// Header line.
	if !strings.Contains(out, "Group 'mygroup' rebuilt (17s)") {
		t.Errorf("missing header line in output:\n%s", out)
	}

	// Entities section.
	if !strings.Contains(out, "Entities (20,718 total):") {
		t.Errorf("missing entities total in output:\n%s", out)
	}
	if !strings.Contains(out, "Variable") || !strings.Contains(out, "8,402") {
		t.Errorf("missing Variable kind in output:\n%s", out)
	}

	// Relationships section.
	if !strings.Contains(out, "Relationships (95,213 total):") {
		t.Errorf("missing relationships total in output:\n%s", out)
	}
	if !strings.Contains(out, "REFERENCES") || !strings.Contains(out, "61,902") {
		t.Errorf("missing REFERENCES kind in output:\n%s", out)
	}

	// Derived stats.
	if !strings.Contains(out, "Cross-repo edges:") || !strings.Contains(out, "234") {
		t.Errorf("missing cross-repo edges in output:\n%s", out)
	}
	if !strings.Contains(out, "Process flows:") || !strings.Contains(out, "50") {
		t.Errorf("missing process flows in output:\n%s", out)
	}
	if !strings.Contains(out, "HTTP endpoints:") || !strings.Contains(out, "744") {
		t.Errorf("missing HTTP endpoints in output:\n%s", out)
	}
	if !strings.Contains(out, "Enrichment candidates:") || !strings.Contains(out, "89") {
		t.Errorf("missing enrichment candidates in output:\n%s", out)
	}
	if !strings.Contains(out, "Repair candidates:") || !strings.Contains(out, "12") {
		t.Errorf("missing repair candidates in output:\n%s", out)
	}
	if !strings.Contains(out, "Orphan entities:") || !strings.Contains(out, "1,341") {
		t.Errorf("missing orphan entities in output:\n%s", out)
	}
	if !strings.Contains(out, "6.5%") {
		t.Errorf("missing orphan rate in output:\n%s", out)
	}
}

func TestPrintRebuildSummary_ZeroEntities_NoOrphanLine(t *testing.T) {
	// When TotalEntities == 0, orphan line should be suppressed.
	s := &RebuildSummary{
		Group:   "empty",
		Elapsed: 1 * time.Second,
		EntityByKind: map[string]int{},
		RelByKind:    map[string]int{},
	}
	var buf bytes.Buffer
	PrintRebuildSummary(&buf, s)
	out := buf.String()
	if strings.Contains(out, "Orphan entities:") {
		t.Errorf("orphan line should be absent when TotalEntities==0, got:\n%s", out)
	}
}

func TestPrintRebuildSummary_OtherRowPresent(t *testing.T) {
	// When more than 5 entity kinds exist, an "Other" row must appear.
	s := &RebuildSummary{
		Group:         "g",
		TotalEntities: 600,
		EntityByKind: map[string]int{
			"A": 100, "B": 100, "C": 100, "D": 100, "E": 100, "F": 100,
		},
		RelByKind: map[string]int{},
		Elapsed:   5 * time.Second,
	}
	var buf bytes.Buffer
	PrintRebuildSummary(&buf, s)
	out := buf.String()
	if !strings.Contains(out, "Other") {
		t.Errorf("expected 'Other' row when >5 kinds, got:\n%s", out)
	}
}

func TestPrintRebuildSummary_ElapsedFormatting(t *testing.T) {
	// A 2-minute rebuild should show "2m00s".
	s := &RebuildSummary{
		Group:        "g",
		EntityByKind: map[string]int{},
		RelByKind:    map[string]int{},
		Elapsed:      2 * time.Minute,
	}
	var buf bytes.Buffer
	PrintRebuildSummary(&buf, s)
	out := buf.String()
	if !strings.Contains(out, "2m00s") {
		t.Errorf("expected '2m00s' in elapsed, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// countLinksFile
// ---------------------------------------------------------------------------

func TestCountLinksFile_NonExistent(t *testing.T) {
	got := countLinksFile("/nonexistent/path/to/links.json")
	if got != 0 {
		t.Errorf("expected 0 for missing file, got %d", got)
	}
}

func TestCountLinksFile_ValidFile(t *testing.T) {
	// Write a temp file with two link entries.
	tmp := t.TempDir()
	path := tmp + "/g-links.json"
	content := `{"version":1,"links":[{"id":"a"},{"id":"b"}]}`
	if err := writeTestFile(path, content); err != nil {
		t.Fatal(err)
	}
	got := countLinksFile(path)
	if got != 2 {
		t.Errorf("expected 2 links, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// loadCandidateCounts
// ---------------------------------------------------------------------------

func TestLoadCandidateCounts_MissingDir(t *testing.T) {
	enrich, repair := loadCandidateCounts("/nonexistent/stateDir")
	if enrich != 0 || repair != 0 {
		t.Errorf("expected (0,0) for missing dir, got (%d,%d)", enrich, repair)
	}
}

func TestLoadCandidateCounts_BareArray(t *testing.T) {
	tmp := t.TempDir()
	// Write a bare-array candidates file.
	content := `[{"kind":"describe_entity"},{"kind":"repair_edge"},{"kind":"describe_entity"}]`
	if err := writeTestFile(tmp+"/enrichment-candidates.json", content); err != nil {
		t.Fatal(err)
	}
	enrich, repair := loadCandidateCounts(tmp)
	if enrich != 2 {
		t.Errorf("expected enrich=2, got %d", enrich)
	}
	if repair != 1 {
		t.Errorf("expected repair=1, got %d", repair)
	}
}

func TestLoadCandidateCounts_ObjectEnvelope(t *testing.T) {
	tmp := t.TempDir()
	content := `{"candidates":[{"kind":"repair_edge"},{"kind":"repair_edge"}]}`
	if err := writeTestFile(tmp+"/enrichment-candidates.json", content); err != nil {
		t.Fatal(err)
	}
	enrich, repair := loadCandidateCounts(tmp)
	if repair != 2 {
		t.Errorf("expected repair=2, got %d", repair)
	}
	if enrich != 0 {
		t.Errorf("expected enrich=0, got %d", enrich)
	}
}

// ---------------------------------------------------------------------------
// normaliseEntityKind
// ---------------------------------------------------------------------------

func TestNormaliseEntityKind(t *testing.T) {
	cases := []struct{ input, want string }{
		{"function", "Function"},
		{"method", "Function"},
		{"class", "Class"},
		{"struct", "Class"},
		{"interface", "Class"},
		{"variable", "Variable"},
		{"constant", "Variable"},
		{"field", "Variable"},
		{"http_endpoint", "HTTPEndpoint"},
		{"custom_kind", "custom_kind"},
	}
	for _, tc := range cases {
		got := normaliseEntityKind(tc.input)
		if got != tc.want {
			t.Errorf("normaliseEntityKind(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// loadGraphStats (#1076)
// ---------------------------------------------------------------------------

func TestLoadGraphStats_Missing(t *testing.T) {
	e, r := loadGraphStats("/nonexistent/stateDir")
	if e != 0 || r != 0 {
		t.Errorf("expected (0,0) for missing file, got (%d,%d)", e, r)
	}
}

func TestLoadGraphStats_Valid(t *testing.T) {
	tmp := t.TempDir()
	content := `{"version":1,"total_entities":7343,"total_relationships":25930,"communities":690}`
	if err := writeTestFile(tmp+"/graph-stats.json", content); err != nil {
		t.Fatal(err)
	}
	e, r := loadGraphStats(tmp)
	if e != 7343 {
		t.Errorf("expected total_entities=7343, got %d", e)
	}
	if r != 25930 {
		t.Errorf("expected total_relationships=25930, got %d", r)
	}
}

func TestLoadGraphStats_Malformed(t *testing.T) {
	tmp := t.TempDir()
	if err := writeTestFile(tmp+"/graph-stats.json", "not json {"); err != nil {
		t.Fatal(err)
	}
	e, r := loadGraphStats(tmp)
	if e != 0 || r != 0 {
		t.Errorf("expected (0,0) for malformed JSON, got (%d,%d)", e, r)
	}
}

// TestComputeRebuildSummary_SidecarFallback verifies that when the graph.fb
// file is absent (simulating a LoadGraphFromDir failure) the sidecar totals
// from graph-stats.json are used, fixing #1076.
func TestComputeRebuildSummary_SidecarFallback(t *testing.T) {
	// Create a fake repo directory with only graph-stats.json (no graph.fb).
	// Use t.TempDir() as both the repo dir and its .archigraph/ subdir since
	// StateDirForRepo adds "/.archigraph" to the path.
	repoDir := t.TempDir()
	stateDir := repoDir + "/.archigraph"
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statsContent := `{"version":1,"total_entities":12345,"total_relationships":67890}`
	if err := writeTestFile(stateDir+"/graph-stats.json", statsContent); err != nil {
		t.Fatal(err)
	}

	sum := ComputeRebuildSummary("testgroup", []string{repoDir}, 5*time.Second)

	// graph.fb absent → LoadGraphFromDir fails → sidecar fallback kicks in.
	if sum.TotalEntities != 12345 {
		t.Errorf("TotalEntities = %d, want 12345 (sidecar fallback)", sum.TotalEntities)
	}
	if sum.TotalRelationships != 67890 {
		t.Errorf("TotalRelationships = %d, want 67890 (sidecar fallback)", sum.TotalRelationships)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeTestFile writes content to path.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
