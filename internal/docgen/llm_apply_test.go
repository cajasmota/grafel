// Package docgen_test — --llm-mode=apply unit tests (ticket D, issue #1813 chain).
//
// Tests covered:
//  1. Happy path: emit a Tier 1 bundle from a live minimal group, hand-craft a
//     valid result, apply it, assert the final page contains LLM markdown and
//     score fields are correct.
//  2. Hash mismatch: apply with a result whose prompt_hash is wrong → clear error.
//  3. Section coverage: apply with a result missing a section → clear error.
//  4. Extra section in result: apply with a result that has an extra unknown
//     section → clear error.
//  5. Contract violation: apply a result that overshoots the per-section mermaid
//     budget → contract violations recorded in score (no fatal error).
//  6. Missing bundle file: apply with non-existent bundle → clear error.
//  7. Missing result file: apply with non-existent result → clear error.
//  8. Missing --bundle-file flag: ApplyResult without BundleFile → clear error.
//  9. Missing --result-file flag: ApplyResult without ResultFile → clear error.
//
// 10. OUTPUT DISCIPLINE (#2194): apply with outDir matching .vitepress path → refused.
// 11. OUTPUT DISCIPLINE (#2194): apply with outDir matching .docusaurus path → refused.
// 12. OUTPUT DISCIPLINE (#2194): ssgScaffoldingPath unit tests for all patterns.
package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeBundleFile marshals b to a temp file and returns its path.
func writeBundleFile(t *testing.T, dir string, b docgen.LLMPromptBundle) string {
	t.Helper()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	p := filepath.Join(dir, "test-bundle.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return p
}

// writeResultFile marshals r to a temp file and returns its path.
func writeResultFile(t *testing.T, dir string, r docgen.LLMRunResult) string {
	t.Helper()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	p := filepath.Join(dir, "test-result.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write result: %v", err)
	}
	return p
}

// makeMatchingBundleAndResult returns a minimal Tier 1 bundle and a fully
// matching result (same prompt_hash, same section set).
//
// sections is the slice of section names to include. The caller is
// responsible for ensuring they are a subset of KnownSections.
func makeMatchingBundleAndResult(sections []string) (docgen.LLMPromptBundle, docgen.LLMRunResult) {
	hash := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"

	bundleSections := make([]docgen.LLMSectionPrompt, len(sections))
	for i, s := range sections {
		bundleSections[i] = docgen.LLMSectionPrompt{
			Section:      s,
			AnchorID:     docgen.SectionSlug(s),
			StubMarkdown: "<!-- stub -->",
			Guidance:     "Write something.",
			MaxWords:     300,
			MaxMermaid:   1,
			NeighbourIDs: []string{},
		}
	}

	bundle := docgen.LLMPromptBundle{
		Version:      docgen.LLMBundleVersion,
		Tier:         1,
		Group:        "apply-test-group",
		SeedEntityID: "applyentity0001",
		PageID:       "applyentity0001",
		Sections:     bundleSections,
		GraphContext: docgen.LLMGraphContext{
			EntityName: "ApplyTestEntity",
			EntityKind: "function",
		},
		PromptHash:  hash,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	sectionResults := make([]docgen.LLMSectionResult, len(sections))
	for i, s := range sections {
		sectionResults[i] = docgen.LLMSectionResult{
			Section:      s,
			Markdown:     "## " + s + "\n\nLLM-generated prose for " + s + ".\n",
			MermaidCount: 0,
			WordCount:    6,
			LinkRefs:     []string{},
		}
	}

	result := docgen.LLMRunResult{
		Version:        docgen.LLMBundleVersion,
		PromptHash:     hash,
		Tier:           1,
		Group:          "apply-test-group",
		SeedEntityID:   "applyentity0001",
		SectionResults: sectionResults,
		FilledAt:       time.Now().UTC().Format(time.RFC3339),
	}

	return bundle, result
}

// ---------------------------------------------------------------------------
// Test 1: Happy path
// ---------------------------------------------------------------------------

// TestApplyResult_HappyPath verifies that ApplyResult:
//   - writes the final page containing the LLM markdown for each section,
//   - writes a score.json with llm_mode="apply",
//   - sets SeedEntityFound=true when GraphContext.EntityName is non-empty,
//   - records zero contract violations for valid input.
func TestApplyResult_HappyPath(t *testing.T) {
	t.Parallel()

	sections := []string{"overview", "flows"}
	bundle, result := makeMatchingBundleAndResult(sections)

	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  outDir,
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	mdPath, scorePath, score, err := docgen.ApplyResult(opts)
	if err != nil {
		t.Fatalf("ApplyResult returned unexpected error: %v", err)
	}

	// 1. Output files must exist.
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("page .md not written: %v", statErr)
	}
	if _, statErr := os.Stat(scorePath); statErr != nil {
		t.Errorf("score.json not written: %v", statErr)
	}

	// 2. Page must contain LLM-generated prose for each section.
	pageData, readErr := os.ReadFile(mdPath)
	if readErr != nil {
		t.Fatalf("read page: %v", readErr)
	}
	pageText := string(pageData)
	for _, s := range sections {
		wantSnippet := "LLM-generated prose for " + s
		if !strings.Contains(pageText, wantSnippet) {
			t.Errorf("page missing LLM prose for section %q; page snippet:\n%.300s", s, pageText)
		}
	}

	// 3. Page must carry the tier1 header marker.
	if !strings.Contains(pageText, "<!-- tier1-generated -->") {
		t.Errorf("page missing <!-- tier1-generated --> header")
	}

	// 4. Score fields must be correct.
	if score.LLMMode != "apply" {
		t.Errorf("score.LLMMode: got %q want %q", score.LLMMode, "apply")
	}
	if score.SeedEntity != bundle.SeedEntityID {
		t.Errorf("score.SeedEntity: got %q want %q", score.SeedEntity, bundle.SeedEntityID)
	}
	if !score.SeedEntityFound {
		t.Error("score.SeedEntityFound: expected true (entity name set in GraphContext)")
	}
	if score.SectionCount != len(sections) {
		t.Errorf("score.SectionCount: got %d want %d", score.SectionCount, len(sections))
	}
	if len(score.ContractViolations) > 0 {
		t.Errorf("unexpected contract violations: %v", score.ContractViolations)
	}

	// 5. score.json on disk must have llm_mode="apply".
	scoreData, readErr := os.ReadFile(scorePath)
	if readErr != nil {
		t.Fatalf("read score.json: %v", readErr)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(scoreData, &parsed); jsonErr != nil {
		t.Fatalf("parse score.json: %v", jsonErr)
	}
	if got, ok := parsed["llm_mode"]; !ok || got != "apply" {
		t.Errorf("score.json llm_mode: got %v want %q", got, "apply")
	}
}

// ---------------------------------------------------------------------------
// Test 2: Hash mismatch
// ---------------------------------------------------------------------------

// TestApplyResult_HashMismatch verifies that applying a result with a
// different prompt_hash returns a clear "stale result" error.
func TestApplyResult_HashMismatch(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)
	// Tamper with the result hash.
	result.PromptHash = "0000000000000000000000000000000000000000000000000000000000000000"

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for hash mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "stale result") && !strings.Contains(err.Error(), "prompt_hash mismatch") {
		t.Errorf("expected stale-result or prompt_hash mismatch in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Section coverage — missing section in result
// ---------------------------------------------------------------------------

// TestApplyResult_MissingSection verifies that a result that omits a section
// present in the bundle returns a clear section-coverage error.
func TestApplyResult_MissingSection(t *testing.T) {
	t.Parallel()

	sections := []string{"overview", "flows"}
	bundle, result := makeMatchingBundleAndResult(sections)
	// Remove "flows" from the result.
	result.SectionResults = []docgen.LLMSectionResult{result.SectionResults[0]}

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for missing section, got nil")
	}
	if !strings.Contains(err.Error(), "section coverage") {
		t.Errorf("expected 'section coverage' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "flows") {
		t.Errorf("expected missing section name 'flows' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Extra section in result
// ---------------------------------------------------------------------------

// TestApplyResult_ExtraSection verifies that a result carrying a section not
// present in the bundle returns a clear section-coverage error.
func TestApplyResult_ExtraSection(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)
	// Add an extra section to the result that the bundle did not request.
	result.SectionResults = append(result.SectionResults, docgen.LLMSectionResult{
		Section:  "glossary",
		Markdown: "## Glossary\n\nSome terms.\n",
	})

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for extra section in result, got nil")
	}
	if !strings.Contains(err.Error(), "section coverage") {
		t.Errorf("expected 'section coverage' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "glossary") {
		t.Errorf("expected extra section name 'glossary' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Contract violation — mermaid budget overshoot
// ---------------------------------------------------------------------------

// TestApplyResult_ContractViolation_MermaidOverBudget verifies that a result
// that overshoots the per-section mermaid budget results in contract violations
// recorded in the score, but NOT a fatal error (violations are reportable).
func TestApplyResult_ContractViolation_MermaidOverBudget(t *testing.T) {
	t.Parallel()

	sections := []string{"flows"}
	bundle, result := makeMatchingBundleAndResult(sections)

	// Replace the "flows" section result with 4 mermaid blocks (budget is 3).
	mermaidBlock := "```mermaid\ngraph LR\n    A-->B\n```\n"
	overloadedMarkdown := strings.Repeat(mermaidBlock, 4)
	result.SectionResults = []docgen.LLMSectionResult{
		{
			Section:      "flows",
			Markdown:     overloadedMarkdown,
			MermaidCount: 4,
			WordCount:    20,
			LinkRefs:     []string{},
		},
	}

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, score, err := docgen.ApplyResult(opts)
	if err != nil {
		t.Fatalf("ApplyResult returned unexpected error: %v", err)
	}

	// Must have at least one contract violation mentioning mermaid.
	if len(score.ContractViolations) == 0 {
		t.Error("expected contract violations for over-budget mermaid, got none")
	}
	found := false
	for _, v := range score.ContractViolations {
		if strings.Contains(v, "mermaid") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mermaid violation in %v", score.ContractViolations)
	}
	// Score must still record llm_mode=apply.
	if score.LLMMode != "apply" {
		t.Errorf("score.LLMMode: got %q want %q", score.LLMMode, "apply")
	}
}

// ---------------------------------------------------------------------------
// Test 6: Missing bundle file
// ---------------------------------------------------------------------------

// TestApplyResult_MissingBundleFile verifies that applying with a
// non-existent bundle path returns a clear file-read error.
func TestApplyResult_MissingBundleFile(t *testing.T) {
	t.Parallel()

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: "/nonexistent/path/bundle.json",
		ResultFile: "/nonexistent/path/result.json",
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for missing bundle file, got nil")
	}
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("expected 'bundle' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Missing result file
// ---------------------------------------------------------------------------

// TestApplyResult_MissingResultFile verifies that applying with a
// non-existent result path returns a clear file-read error.
func TestApplyResult_MissingResultFile(t *testing.T) {
	t.Parallel()

	// Bundle must exist so we get past the bundle-read stage.
	sections := []string{"overview"}
	bundle, _ := makeMatchingBundleAndResult(sections)
	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: filepath.Join(tmpDir, "nonexistent-result.json"),
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for missing result file, got nil")
	}
	if !strings.Contains(err.Error(), "result") {
		t.Errorf("expected 'result' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 8: Missing --bundle-file flag
// ---------------------------------------------------------------------------

// TestApplyResult_MissingBundleFileFlag verifies that calling ApplyResult with
// an empty BundleFile returns a clear error.
func TestApplyResult_MissingBundleFileFlag(t *testing.T) {
	t.Parallel()

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: "", // missing
		ResultFile: "/some/result.json",
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for empty BundleFile, got nil")
	}
	if !strings.Contains(err.Error(), "bundle-file") && !strings.Contains(err.Error(), "BundleFile") {
		t.Errorf("expected bundle-file mention in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 9: Missing --result-file flag
// ---------------------------------------------------------------------------

// TestApplyResult_MissingResultFileFlag verifies that calling ApplyResult with
// an empty ResultFile returns a clear error.
func TestApplyResult_MissingResultFileFlag(t *testing.T) {
	t.Parallel()

	opts := docgen.Tier1RunOpts{
		OutputDir:  t.TempDir(),
		LLMMode:    "apply",
		BundleFile: "/some/bundle.json",
		ResultFile: "", // missing
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected error for empty ResultFile, got nil")
	}
	if !strings.Contains(err.Error(), "result-file") && !strings.Contains(err.Error(), "ResultFile") {
		t.Errorf("expected result-file mention in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 10: score.json carries correct fields
// ---------------------------------------------------------------------------

// TestApplyResult_ScoreJSON_AllFields verifies that the score.json written to
// disk by ApplyResult carries all required fields.
func TestApplyResult_ScoreJSON_AllFields(t *testing.T) {
	t.Parallel()

	sections := []string{"overview", "api"}
	bundle, result := makeMatchingBundleAndResult(sections)

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	opts := docgen.Tier1RunOpts{
		OutputDir:  filepath.Join(tmpDir, "out-fields"),
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, scorePath, _, err := docgen.ApplyResult(opts)
	if err != nil {
		t.Fatalf("ApplyResult error: %v", err)
	}

	scoreData, readErr := os.ReadFile(scorePath)
	if readErr != nil {
		t.Fatalf("read score.json: %v", readErr)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(scoreData, &parsed); jsonErr != nil {
		t.Fatalf("parse score.json: %v", jsonErr)
	}

	required := []string{
		"tier", "wall_time_ms", "seed_entity", "seed_entity_found",
		"section_count", "token_count_estimate",
		"internal_link_count", "internal_link_unresolved",
		"mermaid_count", "mermaid_oversized",
		"prose_density_words_per_section", "duplicated_flow_count",
		"anchor_count", "llm_mode",
	}
	for _, f := range required {
		if _, ok := parsed[f]; !ok {
			t.Errorf("score.json missing required field: %q", f)
		}
	}
	if got := parsed["llm_mode"]; got != "apply" {
		t.Errorf("score.json llm_mode: got %v want %q", got, "apply")
	}
	if got := parsed["tier"].(float64); got != 1 {
		t.Errorf("score.json tier: got %v want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Test 11: pageID resolution — opts.PageID > bundle.PageID > sanitised entity ID
// ---------------------------------------------------------------------------

// TestApplyResult_PageIDResolution verifies the page filename stem priority:
// opts.PageID beats bundle.PageID beats sanitised entity ID.
func TestApplyResult_PageIDResolution(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)
	// bundle.PageID is "applyentity0001" by makeMatchingBundleAndResult.

	t.Run("opts.PageID wins", func(t *testing.T) {
		tmpDir := t.TempDir()
		bundlePath := writeBundleFile(t, tmpDir, bundle)
		resultPath := writeResultFile(t, tmpDir, result)
		outDir := filepath.Join(tmpDir, "out1")

		opts := docgen.Tier1RunOpts{
			PageID:     "custom-page-id",
			OutputDir:  outDir,
			LLMMode:    "apply",
			BundleFile: bundlePath,
			ResultFile: resultPath,
		}
		mdPath, _, _, err := docgen.ApplyResult(opts)
		if err != nil {
			t.Fatalf("ApplyResult error: %v", err)
		}
		if !strings.Contains(filepath.Base(mdPath), "custom-page-id") {
			t.Errorf("expected 'custom-page-id' in filename, got %q", filepath.Base(mdPath))
		}
	})

	t.Run("bundle.PageID used when opts.PageID empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		bundlePath := writeBundleFile(t, tmpDir, bundle)
		resultPath := writeResultFile(t, tmpDir, result)
		outDir := filepath.Join(tmpDir, "out2")

		opts := docgen.Tier1RunOpts{
			PageID:     "", // empty → use bundle.PageID
			OutputDir:  outDir,
			LLMMode:    "apply",
			BundleFile: bundlePath,
			ResultFile: resultPath,
		}
		mdPath, _, _, err := docgen.ApplyResult(opts)
		if err != nil {
			t.Fatalf("ApplyResult error: %v", err)
		}
		if !strings.Contains(filepath.Base(mdPath), bundle.PageID) {
			t.Errorf("expected bundle.PageID %q in filename, got %q", bundle.PageID, filepath.Base(mdPath))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests 10–12: OUTPUT DISCIPLINE (#2194) — apply-step SSG refusal
// ---------------------------------------------------------------------------

// TestApplyResult_RefusesVitepressOutputDir verifies that ApplyResult returns
// an error when the resolved output-dir is inside a .vitepress directory.
func TestApplyResult_RefusesVitepressOutputDir(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	// An LLM agent might set output-dir to a VitePress directory.
	vitepressOutDir := filepath.Join(tmpDir, "docs", ".vitepress", "generated")

	opts := docgen.Tier1RunOpts{
		OutputDir:  vitepressOutDir,
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected OUTPUT DISCIPLINE refusal for .vitepress output-dir, got nil")
	}
	if !strings.Contains(err.Error(), "OUTPUT DISCIPLINE") {
		t.Errorf("expected 'OUTPUT DISCIPLINE' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), ".vitepress") {
		t.Errorf("expected '.vitepress' pattern in error, got: %v", err)
	}
	// The directory must NOT have been created.
	if _, statErr := os.Stat(vitepressOutDir); statErr == nil {
		t.Error("OUTPUT DISCIPLINE: .vitepress output-dir was created despite refusal")
	}
}

// TestApplyResult_RefusesDocusaurusOutputDir verifies refusal for .docusaurus paths.
func TestApplyResult_RefusesDocusaurusOutputDir(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)

	tmpDir := t.TempDir()
	bundlePath := writeBundleFile(t, tmpDir, bundle)
	resultPath := writeResultFile(t, tmpDir, result)

	docusaurusOutDir := filepath.Join(tmpDir, "docs", ".docusaurus", "generated")

	opts := docgen.Tier1RunOpts{
		OutputDir:  docusaurusOutDir,
		LLMMode:    "apply",
		BundleFile: bundlePath,
		ResultFile: resultPath,
	}

	_, _, _, err := docgen.ApplyResult(opts)
	if err == nil {
		t.Fatal("expected OUTPUT DISCIPLINE refusal for .docusaurus output-dir, got nil")
	}
	if !strings.Contains(err.Error(), "OUTPUT DISCIPLINE") {
		t.Errorf("expected 'OUTPUT DISCIPLINE' in error, got: %v", err)
	}
}

// TestApplyResult_OutputDisciplinePatterns exercises ssgScaffoldingPath logic
// indirectly via ApplyResult for directory-style SSG patterns.
func TestApplyResult_OutputDisciplinePatterns(t *testing.T) {
	t.Parallel()

	sections := []string{"overview"}
	bundle, result := makeMatchingBundleAndResult(sections)

	// Patterns that must be refused as output-dir (directory-style artifacts).
	refusedDirs := []struct {
		rel     string
		pattern string
	}{
		{filepath.Join("docs", ".vitepress", "dist"), ".vitepress"},
		{filepath.Join("docs", ".docusaurus", "cache"), ".docusaurus"},
		{filepath.Join("docs", "sphinx", "_build"), "sphinx"},
	}

	for _, tc := range refusedDirs {
		tc := tc // capture
		t.Run("refused="+tc.pattern, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			bundlePath := writeBundleFile(t, tmpDir, bundle)
			resultPath := writeResultFile(t, tmpDir, result)

			opts := docgen.Tier1RunOpts{
				OutputDir:  filepath.Join(tmpDir, tc.rel),
				LLMMode:    "apply",
				BundleFile: bundlePath,
				ResultFile: resultPath,
			}
			_, _, _, err := docgen.ApplyResult(opts)
			if err == nil {
				t.Fatalf("expected OUTPUT DISCIPLINE refusal for %q, got nil", tc.rel)
			}
			if !strings.Contains(err.Error(), "OUTPUT DISCIPLINE") {
				t.Errorf("expected 'OUTPUT DISCIPLINE' in error for %q, got: %v", tc.rel, err)
			}
		})
	}
}
