// Package docgen — --llm-mode=apply: read LLMRunResult, validate, assemble,
// score, and write the final page (ticket D, issue #1813 chain).
//
// applyResult is the third step of the LLM iteration loop:
//
//  1. (emit)  RunTier1 --llm-mode=emit  → writes stub page + LLMPromptBundle JSON.
//  2. (fill)  External orchestrator reads bundle, calls LLM per section, writes
//             LLMRunResult JSON (one markdown blob per section).
//  3. (apply) applyResult --llm-mode=apply  → THIS FILE
//             Reads bundle + result, validates hashes + section coverage,
//             assembles the final page using assemblePage with LLM markdown,
//             runs checkPageContract on real prose, writes final page + score.json.
//
// No LLM calls are made here.  No network access.  Pure file I/O + the
// existing Tier 1 assembly and contract machinery.
package docgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ApplyResult reads a bundle and result from disk, validates them, assembles
// the final page with LLM-filled prose, runs per-page contracts, and writes:
//
//   - <outDir>/<pageID>-page.md  (final page, overwriting any stub)
//   - <outDir>/score.json        (final score with llm_mode="apply")
//
// It also writes each section result to the section-level LLM cache so
// subsequent --llm-mode=emit calls for the same prompt hash return cache_hit=true.
// Cache writes are skipped when opts.NoCache is true.
//
// It returns the paths to those two files and the assembled score.
//
// Exported so the CLI layer and tests can call it directly.
func ApplyResult(opts Tier1RunOpts) (mdPath string, scorePath string, score Tier1Score, err error) {
	start := time.Now()

	// Validate required flags.
	if opts.BundleFile == "" {
		err = fmt.Errorf("--bundle-file is required when --llm-mode=apply")
		return
	}
	if opts.ResultFile == "" {
		err = fmt.Errorf("--result-file is required when --llm-mode=apply")
		return
	}

	// Read and unmarshal bundle.
	bundleData, readErr := os.ReadFile(opts.BundleFile)
	if readErr != nil {
		err = fmt.Errorf("read bundle file %q: %w", opts.BundleFile, readErr)
		return
	}
	var bundle LLMPromptBundle
	if unmarshalErr := json.Unmarshal(bundleData, &bundle); unmarshalErr != nil {
		err = fmt.Errorf("unmarshal bundle %q: %w", opts.BundleFile, unmarshalErr)
		return
	}

	// Read and unmarshal result.
	resultData, readErr := os.ReadFile(opts.ResultFile)
	if readErr != nil {
		err = fmt.Errorf("read result file %q: %w", opts.ResultFile, readErr)
		return
	}
	var result LLMRunResult
	if unmarshalErr := json.Unmarshal(resultData, &result); unmarshalErr != nil {
		err = fmt.Errorf("unmarshal result %q: %w", opts.ResultFile, unmarshalErr)
		return
	}

	// Validation 1: prompt_hash must match.
	if hashErr := BundleHashValid(&bundle, &result); hashErr != nil {
		err = fmt.Errorf("--llm-mode=apply stale result: %w", hashErr)
		return
	}

	// Validation 2: section coverage — every bundle section must appear in
	// result.SectionResults, and every result section must appear in the bundle.
	bundleSectionSet := make(map[string]bool, len(bundle.Sections))
	for _, sp := range bundle.Sections {
		bundleSectionSet[sp.Section] = true
	}
	resultSectionMap := make(map[string]string, len(result.SectionResults))
	for _, sr := range result.SectionResults {
		resultSectionMap[sr.Section] = sr.Markdown
	}

	// Check bundle → result coverage.
	var missingInResult []string
	for sec := range bundleSectionSet {
		if _, ok := resultSectionMap[sec]; !ok {
			missingInResult = append(missingInResult, sec)
		}
	}
	if len(missingInResult) > 0 {
		err = fmt.Errorf(
			"--llm-mode=apply section coverage error: bundle sections missing from result: %s",
			strings.Join(missingInResult, ", "),
		)
		return
	}

	// Check result → bundle coverage (extra sections in result are an error).
	var extraInResult []string
	for sec := range resultSectionMap {
		if !bundleSectionSet[sec] {
			extraInResult = append(extraInResult, sec)
		}
	}
	if len(extraInResult) > 0 {
		err = fmt.Errorf(
			"--llm-mode=apply section coverage error: result contains sections not in bundle: %s",
			strings.Join(extraInResult, ", "),
		)
		return
	}

	// Determine the ordered section list from the bundle (KnownSections order
	// is preserved by BuildBundle, but we re-derive it for safety).
	var sections []string
	for _, sec := range KnownSections {
		if bundleSectionSet[sec] {
			sections = append(sections, sec)
		}
	}

	// Build sectionMap from LLM-filled markdown.
	sectionMap := make(map[string]string, len(sections))
	for sec, md := range resultSectionMap {
		sectionMap[sec] = md
	}

	// Page assembly — reuse existing assemblePage machinery with LLM markdown.
	pageEntityName := bundle.SeedEntityID
	if bundle.GraphContext.EntityName != "" {
		pageEntityName = bundle.GraphContext.EntityName
	}
	page, anchors := assemblePage(pageEntityName, sections, sectionMap)

	// Run per-page contracts on the real prose.
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

	// Write section results to the section-level cache so subsequent
	// --llm-mode=emit calls for the same prompt_hash return cache_hit=true.
	// We do this before file I/O so the cache is populated even if a later
	// write fails. Errors are non-fatal: a cache write failure is silently
	// ignored (the apply still succeeds; the next emit will just miss the cache).
	cacheWrites := 0
	if !opts.NoCache {
		cacheDir := opts.CacheDir
		if cacheDir == "" {
			if cd, cdErr := DefaultCacheDir(bundle.Group); cdErr == nil {
				cacheDir = cd
			}
		}
		if cacheDir != "" {
			// Build a per-section prompt_hash lookup from the bundle so we can
			// match each result to its hash without recomputing hashes.
			bundleHashBySection := make(map[string]string, len(bundle.Sections))
			for _, sp := range bundle.Sections {
				if sp.PromptHash != "" {
					bundleHashBySection[sp.Section] = sp.PromptHash
				}
			}
			for _, sr := range result.SectionResults {
				ph, ok := bundleHashBySection[sr.Section]
				if !ok || ph == "" {
					continue // no hash available; skip this section
				}
				entry := CacheEntry{
					PromptHash:   ph,
					Section:      sr.Section,
					Markdown:     sr.Markdown,
					WordCount:    sr.WordCount,
					MermaidCount: sr.MermaidCount,
					LinkRefs:     sr.LinkRefs,
					CachedAt:     time.Now().UTC().Format(time.RFC3339),
				}
				if writeErr := WriteCache(cacheDir, entry); writeErr == nil {
					cacheWrites++
				}
				// silently ignore individual write errors
			}
		}
	}

	score = Tier1Score{
		Tier:                   1,
		WallTimeMS:             time.Since(start).Milliseconds(),
		SeedEntity:             bundle.SeedEntityID,
		SeedEntityFound:        bundle.GraphContext.EntityName != "",
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
		LLMMode:                "apply",
		CacheWrites:            cacheWrites,
	}

	// Resolve output directory.
	outDir := opts.OutputDir
	if outDir == "" {
		outDir, err = defaultTier1OutDir(bundle.Group)
		if err != nil {
			return
		}
	}
	if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create output dir %s: %w", outDir, mkErr)
		return
	}

	// Determine pageID from opts → bundle → sanitised entity ID.
	pageID := opts.PageID
	if pageID == "" {
		pageID = bundle.PageID
	}
	if pageID == "" {
		pageID = sanitizeFilename(bundle.SeedEntityID)
	}

	// Write final page markdown.
	mdFile := filepath.Join(outDir, pageID+"-page.md")
	if wErr := os.WriteFile(mdFile, []byte(page), 0o644); wErr != nil {
		err = fmt.Errorf("write final page: %w", wErr)
		return
	}

	// Write score.json.
	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal apply score: %w", jErr)
		return
	}
	scoreFile := filepath.Join(outDir, "score.json")
	if wErr := os.WriteFile(scoreFile, scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write score.json: %w", wErr)
		return
	}

	mdPath = mdFile
	scorePath = scoreFile
	return
}
