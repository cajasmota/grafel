// Package docgen — Tier 1 single-page end-to-end path (issue #1760).
//
// Tier 1 renders a COMPLETE multi-section page for a SINGLE seed entity with
// a <120 s wall-time budget. It is the contract-validation harness for the
// per-page output: anchor IDs, internal link stability, mermaid budget, and
// duplicate-flow detection.
//
// Tier 1 extends Tier 0 by:
//   - Selecting the appropriate section subset for the entity's kind.
//   - Rendering ALL selected sections concurrently (goroutine pool of 4).
//   - Assembling the sections into a single page with a generated TOC.
//   - Running per-page contract checks:
//     • Anchor-ID determinism (heading → slug).
//     • Mermaid block count per-section (≤ MermaidBudgetPerSection).
//     • Total mermaid blocks across the page (≤ MermaidBudgetPage).
//     • Internal relative link target resolution against the page's own anchors.
//     • Duplicate flow-block detection.
//   - Writing <entity-id>-page.md + score.json in the Tier 1 schema.
//
// LLM calls: Tier 1 does NOT call an external LLM. Like Tier 0 it produces a
// deterministic, fully-resolved context page. The per-page contract checks
// (anchors, links, mermaid) are what distinguish Tier 1 from Tier 0; the LLM
// prose-fill layer is Tier 2 (not yet implemented).
package docgen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cajasmota/archigraph/internal/graph"
)

// MermaidBudgetPerSection is the maximum mermaid blocks allowed in a single
// rendered section before the page check is flagged.
const MermaidBudgetPerSection = 3

// MermaidBudgetPage is the maximum total mermaid blocks allowed across the
// entire assembled page.
const MermaidBudgetPage = 9

// Tier1RunOpts contains the resolved inputs for a Tier 1 run.
type Tier1RunOpts struct {
	// Group is the archigraph group name.
	Group string
	// SeedEntityID is the entity ID (or prefix) to render the page for.
	SeedEntityID string
	// PageID is an optional override for the output filename stem. Defaults to
	// a sanitised form of SeedEntityID.
	PageID string
	// OutputDir overrides the default ~/.archigraph/docs/<group>/.tier1-<ts>/
	// location. Useful in tests.
	OutputDir string
	// ConcurrencyLimit controls the goroutine pool size for parallel section
	// rendering. Defaults to 4 when 0.
	ConcurrencyLimit int
	// LLMMode controls the LLM integration mode. Valid values:
	//   "" — default: write page .md + score.json only (existing behaviour).
	//   "emit" — write page .md + score.json AND an LLMPromptBundle JSON file.
	//   "apply" — read BundleFile + ResultFile, validate, assemble with real prose,
	//             run contracts, write final page + score.json.
	// Any other value is an error.
	LLMMode string
	// BundleFile is the path to the LLMPromptBundle JSON file.
	// Required when LLMMode == "apply".
	BundleFile string
	// ResultFile is the path to the LLMRunResult JSON file written by the
	// external orchestrator.  Required when LLMMode == "apply".
	ResultFile string
	// CacheDir overrides the default section-level LLM cache directory:
	//   ~/.archigraph/docs/<group>/.llm-cache/
	// Ignored when NoCache is true.
	CacheDir string
	// NoCache disables both cache reads and writes (useful for benchmark /
	// quality-check runs that must not use or pollute the section cache).
	NoCache bool
}

// Tier1Score is the machine-readable quality scorecard written by Tier 1.
// It extends Tier 0's scope with page-level contract metrics.
type Tier1Score struct {
	Tier                   int      `json:"tier"`
	WallTimeMS             int64    `json:"wall_time_ms"`
	SeedEntity             string   `json:"seed_entity"`
	SeedEntityFound        bool     `json:"seed_entity_found"`
	SectionCount           int      `json:"section_count"`
	TokenCountEstimate     int      `json:"token_count_estimate"`
	InternalLinkCount      int      `json:"internal_link_count"`
	InternalLinkUnresolved int      `json:"internal_link_unresolved"`
	MermaidCount           int      `json:"mermaid_count"`
	MermaidOversized       int      `json:"mermaid_oversized"`
	ProseWordsPerSection   int      `json:"prose_density_words_per_section"`
	DuplicatedFlowCount    int      `json:"duplicated_flow_count"`
	AnchorCount            int      `json:"anchor_count"`
	ContractViolations     []string `json:"contract_violations,omitempty"`
	// LLMMode is set to "emit" when the run was invoked with --llm-mode=emit.
	// Empty string means the default deterministic-stub-only mode.
	LLMMode     string `json:"llm_mode,omitempty"`
	// CacheHits is the number of sections that were satisfied from the section
	// cache during --llm-mode=emit (i.e. LLM call skippable for those sections).
	CacheHits   int `json:"cache_hits,omitempty"`
	// CacheWrites is the number of section results written to the cache during
	// --llm-mode=apply.
	CacheWrites int `json:"cache_writes,omitempty"`
}

// sectionResult holds the output of one parallel section render.
type sectionResult struct {
	section string
	md      string
}

// RunTier1 executes a Tier 1 full-page render and returns the path to the
// output markdown file and its score.
//
// When opts.LLMMode == "emit" the function also writes a sibling
// <entity-id>-page-bundle.json containing the LLMPromptBundle for ALL sections
// on this page. The bundle is emitted ALONGSIDE the stub; no LLM is called.
// Contract checks that depend on real prose are still run against the stub, but
// are not fatal in emit mode (score.ContractViolations is still populated for
// visibility).
func RunTier1(opts Tier1RunOpts) (mdPath string, scorePath string, score Tier1Score, err error) {
	start := time.Now()

	if err = validateLLMMode(opts.LLMMode); err != nil {
		return
	}

	if opts.ConcurrencyLimit <= 0 {
		opts.ConcurrencyLimit = 4
	}

	// Resolve output directory.
	outDir := opts.OutputDir
	if outDir == "" {
		outDir, err = defaultTier1OutDir(opts.Group)
		if err != nil {
			return
		}
	}
	if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create output dir %s: %w", outDir, mkErr)
		return
	}

	// Load entity context — reuse tier0 machinery.
	_, entity, neighbours, _, _, err := loadEntityContext(opts.Group, opts.SeedEntityID)
	if err != nil {
		return
	}

	// Select section subset for this entity.
	kind := ""
	if entity != nil {
		kind = entity.Kind
	}
	sections := sectionsForEntityKind(kind)

	// Render all sections concurrently.
	sectionMap := renderSectionsConcurrent(sections, entity, neighbours, opts.ConcurrencyLimit)

	// Assemble into a page in KnownSections order.
	pageEntityName := opts.SeedEntityID
	if entity != nil {
		pageEntityName = entity.Name
	}
	page, anchors := assemblePage(pageEntityName, sections, sectionMap)

	// Run per-page contract checks.
	// In emit mode contract violations are recorded but not fatal — the bundle
	// is the handoff artifact, not the final rendered page.
	violations := checkPageContract(page, anchors, sections, sectionMap)

	// Compute score metrics.
	mermaidCount := strings.Count(page, "```mermaid")
	mermaidOversized := countMermaidOversized(sectionMap)
	internalLinks := countInternalPageLinks(page)
	unresolvedLinks := countUnresolvedPageLinks(page, anchors)
	duplicatedFlows := CountDuplicatedFlows(sectionMap)
	words := countWords(page)
	wordsPerSection := 0
	if len(sections) > 0 {
		wordsPerSection = words / len(sections)
	}
	tokens := estimateTokens(page)

	score = Tier1Score{
		Tier:                   1,
		WallTimeMS:             time.Since(start).Milliseconds(),
		SeedEntity:             opts.SeedEntityID,
		SeedEntityFound:        entity != nil,
		SectionCount:           len(sections),
		TokenCountEstimate:     tokens,
		InternalLinkCount:      internalLinks,
		InternalLinkUnresolved: unresolvedLinks,
		MermaidCount:           mermaidCount,
		MermaidOversized:       mermaidOversized,
		ProseWordsPerSection:   wordsPerSection,
		DuplicatedFlowCount:    duplicatedFlows,
		AnchorCount:            len(anchors),
		ContractViolations:     violations,
		LLMMode:                opts.LLMMode,
	}

	// Write page markdown file.
	pageID := opts.PageID
	if pageID == "" {
		pageID = sanitizeFilename(opts.SeedEntityID)
	}
	mdFile := filepath.Join(outDir, pageID+"-page.md")
	if wErr := os.WriteFile(mdFile, []byte(page), 0o644); wErr != nil {
		err = fmt.Errorf("write page file: %w", wErr)
		return
	}

	// Write score.json.
	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal tier1 score: %w", jErr)
		return
	}
	scoreFile := filepath.Join(outDir, "score.json")
	if wErr := os.WriteFile(scoreFile, scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write score.json: %w", wErr)
		return
	}

	// --llm-mode=emit: build and persist the LLMPromptBundle alongside the page.
	// The Tier 1 bundle contains ALL sections selected for the entity kind.
	// Cache reads are wired via BuildBundleOpts so hit sections get
	// cache_hit=true in the emitted bundle.
	//
	// Atomicity invariant: in emit mode the page file and its sibling bundle
	// must BOTH be present or BOTH be absent. If any step below fails after
	// the page file has already been written, we remove the page file before
	// returning the error so callers (Tier 2/3/4) never see an orphaned page
	// that has no bundle. This ensures bundle_count == page_count. (#1835)
	if opts.LLMMode == "emit" {
		bundleOpts := BuildBundleOpts{
			RunOpts: RunOpts{
				Group:        opts.Group,
				SeedEntityID: opts.SeedEntityID,
				OutputDir:    opts.OutputDir,
				CacheDir:     opts.CacheDir,
				NoCache:      opts.NoCache,
			},
			PageID:   pageID,
			Tier:     1,
			CacheDir: opts.CacheDir,
			NoCache:  opts.NoCache,
		}
		bundle, bErr := BuildBundle(context.Background(), bundleOpts)
		if bErr != nil {
			// Roll back the page file so the directory stays consistent.
			_ = os.Remove(mdFile)
			err = fmt.Errorf("build tier1 llm bundle: %w", bErr)
			return
		}

		// Count cache hits and record in score.
		cacheHits := 0
		for _, sp := range bundle.Sections {
			if sp.CacheHit {
				cacheHits++
			}
		}
		score.CacheHits = cacheHits

		bundleBytes, mErr := json.MarshalIndent(bundle, "", "  ")
		if mErr != nil {
			// Roll back the page file so the directory stays consistent.
			_ = os.Remove(mdFile)
			err = fmt.Errorf("marshal tier1 llm bundle: %w", mErr)
			return
		}
		bundleFile := filepath.Join(outDir, pageID+"-page-bundle.json")
		if wErr := os.WriteFile(bundleFile, bundleBytes, 0o644); wErr != nil {
			// Roll back the page file so the directory stays consistent.
			_ = os.Remove(mdFile)
			err = fmt.Errorf("write tier1 bundle file: %w", wErr)
			return
		}

		// Re-write score.json now that CacheHits is populated.
		scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
		if jErr != nil {
			err = fmt.Errorf("marshal tier1 score (post-emit): %w", jErr)
			return
		}
		if wErr := os.WriteFile(filepath.Join(outDir, "score.json"), scoreBytes, 0o644); wErr != nil {
			err = fmt.Errorf("write score.json (post-emit): %w", wErr)
			return
		}
	}

	mdPath = mdFile
	scorePath = scoreFile
	return
}

// ---------------------------------------------------------------------------
// Section selection
// ---------------------------------------------------------------------------

// SectionsForEntityKind selects the ordered section subset appropriate for the
// given entity-kind string.  It delegates to ResolveSectionProfile so that the
// per-kind registry in sections_by_kind.go is the single source of truth.
//
// When the kind is unrecognised the full KnownSections list is returned so the
// page is maximally informative (default profile = backward-compatible).
//
// Exported so CLI help text and tests can call it without a live entity.
func SectionsForEntityKind(kind string) []string {
	return sectionsForEntityKind(kind)
}

// sectionsForEntityKind is the internal implementation; callers outside the
// package should use SectionsForEntityKind.
func sectionsForEntityKind(kind string) []string {
	return ResolveSectionProfile(kind, "").Sections
}

// ---------------------------------------------------------------------------
// Parallel section rendering
// ---------------------------------------------------------------------------

// renderSectionsConcurrent renders all sections concurrently using a bounded
// goroutine pool of size concurrency.  Returns a map of section → markdown.
func renderSectionsConcurrent(sections []string, entity *graph.Entity, neighbours []graph.Entity, concurrency int) map[string]string {
	work := make(chan string, len(sections))
	for _, s := range sections {
		work <- s
	}
	close(work)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]string, len(sections))
	)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for section := range work {
				md := renderSection(section, entity, neighbours)
				mu.Lock()
				results[section] = md
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return results
}

// ---------------------------------------------------------------------------
// Page assembly
// ---------------------------------------------------------------------------

// assemblePage concatenates rendered sections into a single markdown page.
// It prepends a TOC and annotates each section with an HTML anchor whose ID
// is the section slug.  Returns the assembled text and the set of anchor IDs.
func assemblePage(entityName string, sections []string, sectionMap map[string]string) (string, map[string]bool) {
	var b strings.Builder
	anchors := make(map[string]bool)

	// Page header.
	b.WriteString("<!-- tier1-generated -->\n")
	b.WriteString(fmt.Sprintf("# %s — Documentation Page\n\n", entityName))

	// Table of contents.
	b.WriteString("## Contents\n\n")
	for _, sec := range sections {
		anchor := sectionSlug(sec)
		b.WriteString(fmt.Sprintf("- [%s](#%s)\n", sec, anchor))
	}
	b.WriteString("\n---\n\n")

	// Section bodies in KnownSections order for determinism.
	for _, sec := range KnownSections {
		md, ok := sectionMap[sec]
		if !ok {
			continue
		}
		anchor := sectionSlug(sec)
		anchors[anchor] = true
		b.WriteString(fmt.Sprintf("<a id=\"%s\"></a>\n\n", anchor))
		b.WriteString(md)
		b.WriteString("\n\n---\n\n")
	}

	return b.String(), anchors
}

// SectionSlug converts a section name to a GitHub-flavour anchor ID.
// E.g. "reference-config" → "reference-config".
// Exported for use in tests and external tooling.
func SectionSlug(section string) string {
	return sectionSlug(section)
}

func sectionSlug(section string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(section) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Per-page contract checks
// ---------------------------------------------------------------------------

// CheckPageContract validates the assembled page and returns a slice of
// human-readable contract violations. An empty slice means the page passes.
// Exported for use in tests and external tooling.
func CheckPageContract(page string, anchors map[string]bool, sections []string, sectionMap map[string]string) []string {
	return checkPageContract(page, anchors, sections, sectionMap)
}

func checkPageContract(page string, anchors map[string]bool, sections []string, sectionMap map[string]string) []string {
	var violations []string

	// 1. Mermaid budget per-section.
	for sec, md := range sectionMap {
		count := strings.Count(md, "```mermaid")
		if count > MermaidBudgetPerSection {
			violations = append(violations,
				fmt.Sprintf("section %q has %d mermaid blocks (budget: %d)", sec, count, MermaidBudgetPerSection))
		}
	}

	// 2. Total mermaid budget.
	totalMermaid := strings.Count(page, "```mermaid")
	if totalMermaid > MermaidBudgetPage {
		violations = append(violations,
			fmt.Sprintf("page has %d total mermaid blocks (budget: %d)", totalMermaid, MermaidBudgetPage))
	}

	// 3. Internal relative hash-anchor links must resolve.
	unresolved := countUnresolvedPageLinks(page, anchors)
	if unresolved > 0 {
		violations = append(violations,
			fmt.Sprintf("%d internal relative links have unresolved anchor targets", unresolved))
	}

	// 4. Anchor determinism: every selected section must have its anchor.
	for _, sec := range sections {
		slug := sectionSlug(sec)
		if !anchors[slug] {
			violations = append(violations,
				fmt.Sprintf("section %q is missing expected anchor #%s", sec, slug))
		}
	}

	return violations
}

// countMermaidOversized returns the number of sections that exceed the
// MermaidBudgetPerSection limit.
func countMermaidOversized(sectionMap map[string]string) int {
	n := 0
	for _, md := range sectionMap {
		if strings.Count(md, "```mermaid") > MermaidBudgetPerSection {
			n++
		}
	}
	return n
}

// internalPageLinkRE matches relative hash-anchor markdown links: [text](#anchor).
var internalPageLinkRE = regexp.MustCompile(`\[([^\]]+)\]\(#([^)]+)\)`)

// countInternalPageLinks counts hash-anchor links in the assembled page.
func countInternalPageLinks(page string) int {
	return len(internalPageLinkRE.FindAllString(page, -1))
}

// countUnresolvedPageLinks counts hash-anchor links whose target anchor is not
// present in the page's known anchor set.
func countUnresolvedPageLinks(page string, anchors map[string]bool) int {
	n := 0
	for _, m := range internalPageLinkRE.FindAllStringSubmatch(page, -1) {
		target := m[2]
		if !anchors[target] {
			n++
		}
	}
	return n
}

// flowBlockRE matches mermaid fenced blocks for duplicate-flow detection.
var flowBlockRE = regexp.MustCompile("(?s)```mermaid\n(.*?)```")

// CountDuplicatedFlows returns the number of mermaid blocks whose trimmed
// content is identical to another block across any section.
// Note: blocks that appear multiple times within the SAME section are deduplicated
// first (they don't constitute cross-section duplication).
// Exported so tests and external tooling can call it directly.
func CountDuplicatedFlows(sectionMap map[string]string) int {
	// Map from flow body → list of sections containing it (deduplicated per section).
	flowSections := make(map[string]map[string]bool)
	for sec, md := range sectionMap {
		// Deduplicate flows within this section first.
		flowsInSection := make(map[string]bool)
		for _, m := range flowBlockRE.FindAllStringSubmatch(md, -1) {
			body := strings.TrimSpace(m[1])
			if body != "" {
				flowsInSection[body] = true
			}
		}
		// Record which sections contain each unique flow.
		for body := range flowsInSection {
			if flowSections[body] == nil {
				flowSections[body] = make(map[string]bool)
			}
			flowSections[body][sec] = true
		}
	}
	// Count flows that appear in multiple sections.
	duplicates := 0
	for _, sections := range flowSections {
		if len(sections) > 1 {
			duplicates++
		}
	}
	return duplicates
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// defaultTier1OutDir returns ~/.archigraph/docs/<group>/.tier1-<ts>/.
func defaultTier1OutDir(group string) (string, error) {
	home, err := tier1HomeDir()
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.NewReplacer(":", "-").Replace(ts)
	return filepath.Join(home, "docs", group, ".tier1-"+ts), nil
}

// tier1HomeDir returns the archigraph home directory honouring
// ARCHIGRAPH_HOME override exactly as registry.HomeDir does.
// We replicate the tiny logic here to keep tier1 self-contained; registry is
// already imported by tier0.go in the same compilation unit.
func tier1HomeDir() (string, error) {
	if h := os.Getenv("ARCHIGRAPH_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".archigraph"), nil
}
