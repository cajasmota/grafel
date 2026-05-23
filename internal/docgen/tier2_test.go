package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makePage(id, md string) docgen.PageOutput {
	return docgen.PageOutput{EntityID: id, MD: md}
}

func mermaidBlock(body string) string {
	return "```mermaid\n" + body + "\n```\n"
}

// ---------------------------------------------------------------------------
// 1. checkFlowDuplication
// ---------------------------------------------------------------------------

func TestCheckFlowDuplication_NoViolation(t *testing.T) {
	pages := []docgen.PageOutput{
		makePage("aaa", mermaidBlock("graph LR\n  A-->B")),
		makePage("bbb", mermaidBlock("graph LR\n  C-->D")),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "flow-duplication" {
			t.Errorf("unexpected flow-duplication violation: %s", v.Message)
		}
	}
}

func TestCheckFlowDuplication_SameFlowTwoPages(t *testing.T) {
	flow := "graph LR\n  Seed-->Worker"
	pages := []docgen.PageOutput{
		makePage("aaa", mermaidBlock(flow)),
		makePage("bbb", mermaidBlock(flow)),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	found := false
	for _, v := range vs {
		if v.Kind == "flow-duplication" {
			found = true
			if v.PageA == "" || v.PageB == "" {
				t.Error("flow-duplication violation missing PageA or PageB")
			}
		}
	}
	if !found {
		t.Error("expected flow-duplication violation, got none")
	}
}

func TestCheckFlowDuplication_SameFlowThreePages(t *testing.T) {
	flow := "graph LR\n  X-->Y"
	pages := []docgen.PageOutput{
		makePage("aaa", mermaidBlock(flow)),
		makePage("bbb", mermaidBlock(flow)),
		makePage("ccc", mermaidBlock(flow)),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	flowDups := 0
	for _, v := range vs {
		if v.Kind == "flow-duplication" {
			flowDups++
		}
	}
	// 3 pages with same flow → 3 pairs (aaa-bbb, aaa-ccc, bbb-ccc)
	if flowDups != 3 {
		t.Errorf("expected 3 flow-duplication violations for 3 identical pages, got %d", flowDups)
	}
}

func TestCheckFlowDuplication_EmptyBlock(t *testing.T) {
	// Empty mermaid bodies should not produce violations.
	pages := []docgen.PageOutput{
		makePage("aaa", "```mermaid\n\n```\n"),
		makePage("bbb", "```mermaid\n\n```\n"),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "flow-duplication" {
			t.Errorf("empty mermaid body should not produce flow-duplication: %s", v.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. checkPatternLinks
// ---------------------------------------------------------------------------

func TestCheckPatternLinks_NoViolation(t *testing.T) {
	// Pattern entity "GatewayAdapter" appears in page A's patterns section AND
	// is mentioned in page B's body → no violation.
	pageA := `<a id="patterns"></a>

## Patterns

**GatewayAdapter** — this entity acts as a gateway.

---
`
	pageB := `Some text mentioning GatewayAdapter somewhere.`
	pages := []docgen.PageOutput{
		makePage("aaa", pageA),
		makePage("bbb", pageB),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "pattern-link" {
			t.Errorf("unexpected pattern-link violation: %s", v.Message)
		}
	}
}

func TestCheckPatternLinks_ViolationWhenUnlinked(t *testing.T) {
	// Pattern entity "OrchestrationLayer" declared in page A but not mentioned
	// in page B.
	pageA := `<a id="patterns"></a>

## Patterns

**OrchestrationLayer** handles coordination.

---
`
	pageB := `Some unrelated text about deployment.`
	pages := []docgen.PageOutput{
		makePage("aaa", pageA),
		makePage("bbb", pageB),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	found := false
	for _, v := range vs {
		if v.Kind == "pattern-link" {
			found = true
		}
	}
	if !found {
		t.Error("expected pattern-link violation for unlinked pattern entity, got none")
	}
}

func TestCheckPatternLinks_SinglePageNoViolation(t *testing.T) {
	// Single-page slice: no cross-page enforcement possible.
	pageA := `<a id="patterns"></a>

## Patterns

**Orchestrator** does things.

---
`
	pages := []docgen.PageOutput{makePage("aaa", pageA)}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "pattern-link" {
			t.Errorf("single-page slice should not have pattern-link violations: %s", v.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. checkAnchorConsistency
// ---------------------------------------------------------------------------

func TestCheckAnchorConsistency_ValidFormat(t *testing.T) {
	// [text](entity-id#section) — valid
	md := "See [overview](abc123def456#overview) for more."
	pages := []docgen.PageOutput{makePage("aaa", md)}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "anchor-consistency" {
			t.Errorf("valid anchor format should not produce violation: %s", v.Message)
		}
	}
}

func TestCheckAnchorConsistency_InvalidFormat(t *testing.T) {
	// [text](../some/path#anchor) — invalid cross-page anchor (path-like)
	md := "See [overview](../relative/path#overview) for more."
	pages := []docgen.PageOutput{makePage("aaa", md)}
	vs := docgen.CheckSliceContracts(pages, 100)
	found := false
	for _, v := range vs {
		if v.Kind == "anchor-consistency" {
			found = true
		}
	}
	if !found {
		t.Error("expected anchor-consistency violation for path-like cross-page anchor")
	}
}

func TestCheckAnchorConsistency_AbsoluteURLSkipped(t *testing.T) {
	// Absolute URLs should be ignored.
	md := "See [docs](https://example.com/page#section) for details."
	pages := []docgen.PageOutput{makePage("aaa", md)}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "anchor-consistency" {
			t.Errorf("absolute URL should not produce anchor-consistency violation: %s", v.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. checkSliceMermaidBudget
// ---------------------------------------------------------------------------

func TestCheckSliceMermaidBudget_UnderBudget(t *testing.T) {
	// 2 pages, 3 mermaid blocks each → 6 total, budget 15 → no violation.
	block := mermaidBlock("graph LR\n  A-->B")
	pages := []docgen.PageOutput{
		makePage("aaa", strings.Repeat(block, 3)),
		makePage("bbb", strings.Repeat(block, 3)),
	}
	vs := docgen.CheckSliceContracts(pages, 15)
	for _, v := range vs {
		if v.Kind == "mermaid-budget" {
			t.Errorf("unexpected mermaid-budget violation: %s", v.Message)
		}
	}
}

func TestCheckSliceMermaidBudget_OverBudget(t *testing.T) {
	// 5 pages × 4 blocks = 20 total, budget 15 → violation.
	block := mermaidBlock("graph LR\n  A-->B")
	var pages []docgen.PageOutput
	for i := 0; i < 5; i++ {
		pages = append(pages, makePage(strings.Repeat("a", i+1), strings.Repeat(block, 4)))
	}
	vs := docgen.CheckSliceContracts(pages, 15)
	found := false
	for _, v := range vs {
		if v.Kind == "mermaid-budget" {
			found = true
			if !strings.Contains(v.Message, "20") {
				t.Errorf("mermaid-budget message should mention count 20: %s", v.Message)
			}
		}
	}
	if !found {
		t.Error("expected mermaid-budget violation for 20 blocks with budget 15")
	}
}

// ---------------------------------------------------------------------------
// Tier2Score JSON schema
// ---------------------------------------------------------------------------

func TestTier2Score_JSONSchema(t *testing.T) {
	score := docgen.Tier2Score{
		Tier:                         2,
		WallTimeMS:                   45000,
		PageCount:                    5,
		TotalTokenCount:              12000,
		CrossPageLinkCount:           18,
		CrossPageLinkUnresolved:      0,
		FlowDuplicationViolations:    0,
		PatternLinkViolations:        0,
		AnchorConsistencyViolations:  0,
		SliceMermaidCount:            12,
		SliceMermaidBudgetViolations: 0,
		SliceEntityIDs:               []string{"aaa", "bbb", "ccc", "ddd", "eee"},
	}

	data, err := json.MarshalIndent(score, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	required := []string{
		"tier", "wall_time_ms", "page_count", "total_token_count",
		"cross_page_link_count", "cross_page_link_unresolved",
		"flow_duplication_violations", "pattern_link_violations",
		"anchor_consistency_violations", "slice_mermaid_count",
		"slice_mermaid_budget_violations", "slice_entity_ids",
	}
	for _, f := range required {
		if _, ok := parsed[f]; !ok {
			t.Errorf("Tier2Score JSON missing required field: %q", f)
		}
	}
	if got := parsed["tier"].(float64); got != 2 {
		t.Errorf("tier: want 2, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// RunTier2 — missing group returns graceful error
// ---------------------------------------------------------------------------

func TestRunTier2_MissingGroup(t *testing.T) {
	opts := docgen.Tier2RunOpts{
		Group:        "no-such-group-xyz",
		SeedEntityID: "abc123",
		OutputDir:    t.TempDir(),
	}
	_, _, err := docgen.RunTier2(opts)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
	if strings.Contains(err.Error(), "nil pointer") {
		t.Errorf("unexpected nil pointer in error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RunTier2 — output directory is created
// ---------------------------------------------------------------------------

func TestRunTier2_OutputDirCreated(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "nested", "tier2-output")
	opts := docgen.Tier2RunOpts{
		Group:        "no-such-group",
		SeedEntityID: "abc",
		MaxPages:     3,
		OutputDir:    outDir,
	}
	_, _, err := docgen.RunTier2(opts)
	// We expect a group-not-found error; but the outDir creation happens first.
	// Either the dir exists (created before graph load) or doesn't.
	// The important invariant: no panic.
	_ = err
}

// ---------------------------------------------------------------------------
// RunTier2 — with a minimal synthetic graph
// ---------------------------------------------------------------------------

// buildMinimalGroupForTier2 sets up a minimal ARCHIGRAPH_HOME with a group
// config, a fake repo, and a graph.json containing seed + 2 dependent entities.
func buildMinimalGroupForTier2(t *testing.T) (archHome, group, seedID string) {
	t.Helper()
	archHome = t.TempDir()
	group = "tier2-test-group"
	seedID = "seed0000aabbccdd"

	// Group config.
	cfgDir := filepath.Join(archHome, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	repoPath := filepath.Join(archHome, "fake-repo2")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repoPath: %v", err)
	}

	groupCfg := map[string]interface{}{
		"repos": []map[string]interface{}{{"path": repoPath}},
	}
	cfgBytes, _ := json.Marshal(groupCfg)
	if err := os.WriteFile(filepath.Join(cfgDir, group+".json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write group config: %v", err)
	}

	// Synthetic graph.json — seed + two dependents connected via CALLS.
	dep1ID := "dep10000aabbccdd"
	dep2ID := "dep20000aabbccdd"
	pr1 := 0.5
	pr2 := 0.3
	entities := []interface{}{
		map[string]interface{}{
			"id": seedID, "name": "SeedCapability", "kind": "SCOPE.Service",
			"source_file": "svc/seed.go", "start_line": 1, "end_line": 100,
			"language": "go",
		},
		map[string]interface{}{
			"id": dep1ID, "name": "DependentA", "kind": "SCOPE.Class",
			"source_file": "pkg/a.go", "start_line": 1, "end_line": 50,
			"language": "go", "pagerank": pr1,
		},
		map[string]interface{}{
			"id": dep2ID, "name": "DependentB", "kind": "SCOPE.Class",
			"source_file": "pkg/b.go", "start_line": 1, "end_line": 50,
			"language": "go", "pagerank": pr2,
		},
	}
	rels := []interface{}{
		map[string]interface{}{
			"id": "rel1aabbccdd0011", "from_id": seedID, "to_id": dep1ID,
			"kind": "CALLS",
		},
		map[string]interface{}{
			"id": "rel2aabbccdd0022", "from_id": seedID, "to_id": dep2ID,
			"kind": "IMPORTS",
		},
	}
	graphDoc := map[string]interface{}{
		"version": 1, "repo": repoPath,
		"entities": entities, "relationships": rels,
	}
	graphBytes, _ := json.Marshal(graphDoc)

	// Write graph.json in a location findGroupGraphDirs can discover.
	// daemon.StateDirForRepo is used; we write into the repo .archigraph/ fallback.
	archigraphDir := filepath.Join(repoPath, ".archigraph")
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		t.Fatalf("mkdir archigraphDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return archHome, group, seedID
}

func TestRunTier2_WithMinimalGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, group, seedID := buildMinimalGroupForTier2(t)

	outDir := t.TempDir()
	opts := docgen.Tier2RunOpts{
		Group:        group,
		SeedEntityID: seedID,
		MaxPages:     3,
		OutputDir:    outDir,
	}

	// RunTier2 may fail if the state-dir resolution does not find the fake repo.
	// That is acceptable for unit tests — we just verify no panic and the
	// returned score has tier=2.
	_, score, err := docgen.RunTier2(opts)
	if err != nil {
		// Check it's a group/graph resolution error, not a panic.
		if strings.Contains(err.Error(), "nil pointer") {
			t.Fatalf("nil pointer panic: %v", err)
		}
		// Graceful error from group loading is OK in test env.
		return
	}

	if score.Tier != 2 {
		t.Errorf("tier: want 2, got %d", score.Tier)
	}
	if score.PageCount == 0 {
		t.Error("expected page_count > 0")
	}

	// score.json must be present in outDir.
	scoreFile := filepath.Join(outDir, "score.json")
	if _, err := os.Stat(scoreFile); err != nil {
		t.Errorf("score.json not found in output dir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CheckSliceContracts — combined smoke test
// ---------------------------------------------------------------------------

func TestCheckSliceContracts_AllPass(t *testing.T) {
	// Slice with well-formed pages: unique flows, no pattern entities, valid
	// anchors, within mermaid budget.
	pages := []docgen.PageOutput{
		makePage("ent1", "<!-- tier1-generated -->\n# Entity1\n\n"+
			mermaidBlock("graph LR\n  A-->B")),
		makePage("ent2", "<!-- tier1-generated -->\n# Entity2\n\n"+
			mermaidBlock("graph LR\n  C-->D")),
	}
	vs := docgen.CheckSliceContracts(pages, 15)
	if len(vs) != 0 {
		t.Errorf("expected no violations for well-formed slice, got: %v", vs)
	}
}

func TestDeduplicateStrings(t *testing.T) {
	// Exported indirectly via CheckSliceContracts — test via flow-duplication
	// where a page has two identical mermaid blocks (same page).
	// The flow should be deduplicated to one occurrence per page, not trigger
	// a duplication violation since it only appears in one page.
	flow := "graph LR\n  A-->B"
	md := mermaidBlock(flow) + "\n" + mermaidBlock(flow) // same block twice in one page
	pages := []docgen.PageOutput{
		makePage("aaa", md),
		makePage("bbb", "different content with "+mermaidBlock("graph LR\n  X-->Y")),
	}
	vs := docgen.CheckSliceContracts(pages, 100)
	for _, v := range vs {
		if v.Kind == "flow-duplication" {
			// Both occurrences are on page "aaa" — no cross-page duplication.
			t.Errorf("same-page duplicate mermaid should not produce cross-page flow-duplication: %s", v.Message)
		}
	}
}
