// Package cli — `archigraph docgen` subcommand (Tier 0–4, issue #1760).
//
// Tier 0 produces ONE markdown section for ONE seed entity with a <30 s
// feedback loop. It is designed for rapid prompt-quality iteration:
//
//	archigraph docgen --tier=0 \
//	  --group=mygroup \
//	  --seed-entity=abc123def456 \
//	  --section=capabilities
//
// Tier 1 produces ONE complete multi-section page for ONE seed entity with a
// <120 s wall-time budget. It validates the per-page contract (anchors, link
// stability, mermaid budget) and is the acceptance gate before full-group
// rendering (Tier 2–4):
//
//	archigraph docgen --tier=1 \
//	  --group=mygroup \
//	  --seed-entity=abc123def456
//
// Tier 2 produces a COHERENT SLICE of ~5 pages — the seed capability plus its
// highest-priority dependents — and validates CROSS-PAGE contracts. Wall-time
// target: <10 minutes:
//
//	archigraph docgen --tier=2 \
//	  --group=mygroup \
//	  --seed-entity=abc123def456 \
//	  --max-pages=5
//
// Tier 3 produces a FULL DOC SET for ONE repo within a multi-repo group.
// Wall-time target: <20 minutes:
//
//	archigraph docgen --tier=3 \
//	  --group=mygroup \
//	  --repo=core
//
// Tier 4 produces a FULL DOC SET for ALL repos in a multi-repo group and
// enforces CROSS-REPO coherence contracts. Wall-time target: <60 seconds
// (deterministic stubs; repo Tier 3 runs are concurrent):
//
//	archigraph docgen --tier=4 --group=mygroup
//
// Output (Tier 0):
//
//	~/.archigraph/docs/<group>/.tier0-<RFC3339>/<entity-id>-<section>.md
//	~/.archigraph/docs/<group>/.tier0-<RFC3339>/score.json
//
// Output (Tier 1):
//
//	~/.archigraph/docs/<group>/.tier1-<RFC3339>/<entity-id>-page.md
//	~/.archigraph/docs/<group>/.tier1-<RFC3339>/score.json
//
// Output (Tier 2):
//
//	~/.archigraph/docs/<group>/.tier2-<RFC3339>/<entity-id>-page.md  (N pages)
//	~/.archigraph/docs/<group>/.tier2-<RFC3339>/score.json
//
// Output (Tier 3):
//
//	~/.archigraph/docs/<group>/.tier3-<RFC3339>/<repo>/index.md
//	~/.archigraph/docs/<group>/.tier3-<RFC3339>/<repo>/<entity-id>-page.md
//	~/.archigraph/docs/<group>/.tier3-<RFC3339>/<repo>/score.json
//
// Output (Tier 4):
//
//	~/.archigraph/docs/<group>/.tier4-<RFC3339>/index.md
//	~/.archigraph/docs/<group>/.tier4-<RFC3339>/score.json
//	~/.archigraph/docs/<group>/.tier4-<RFC3339>/<repo>/index.md
//	~/.archigraph/docs/<group>/.tier4-<RFC3339>/<repo>/<entity-id>-page.md
//	~/.archigraph/docs/<group>/.tier4-<RFC3339>/<repo>/score.json
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newDocgenCmd returns the `archigraph docgen` cobra command.
func newDocgenCmd() *cobra.Command {
	var (
		tier          int
		group         string
		seedEntity    string
		section       string
		pageID        string
		outputDir     string
		listSecs      bool
		maxPages      int
		mermaidBudget int
		repoSlug      string
		llmMode       string
		bundleFile    string
		resultFile    string
		cacheDir      string
		noCache       bool
	)

	cmd := &cobra.Command{
		Use:   "docgen [flags]",
		Short: "Generate documentation for a group or a single section (Tier 0–4)",
		Long: `Generate documentation for a registered archigraph group.

TIER 0 (--tier=0) — fast single-section snippet path:
  Renders ONE markdown section for ONE seed entity. Completes in <30 seconds.
  Designed for rapid prompt-quality iteration — no LLM call, no cross-page
  linking, no module grouping. Pure local graph context.

  Output:
    ~/.archigraph/docs/<group>/.tier0-<timestamp>/<entity-id>-<section>.md
    ~/.archigraph/docs/<group>/.tier0-<timestamp>/score.json

  Example:
    archigraph docgen --tier=0 --group=mygroup \
      --seed-entity=abc123def456 --section=capabilities

TIER 1 (--tier=1) — single complete page path (<120 s):
  Renders ALL applicable sections for ONE seed entity and assembles them into
  a single markdown page. Validates the per-page contract: anchor IDs, internal
  link stability, mermaid budget, and duplicate-flow detection. Fail-fast on
  contract violations — fix them before advancing to full-group Tier 2+.

  Output:
    ~/.archigraph/docs/<group>/.tier1-<timestamp>/<entity-id>-page.md
    ~/.archigraph/docs/<group>/.tier1-<timestamp>/score.json

  Example:
    archigraph docgen --tier=1 --group=mygroup \
      --seed-entity=abc123def456

TIER 2 (--tier=2) — coherent slice path (<10 min):
  Generates a coherent SLICE of pages — the seed capability entity plus its
  highest-priority dependents (by PageRank / outbound degree). Runs Tier 1
  per-page rendering on each entity then enforces CROSS-PAGE contracts:
    • No flow (mermaid block body) duplicated across 2+ pages.
    • Pattern entities in one page are referenced in related pages.
    • Cross-page anchor links follow the <entity-id>#<section> format.
    • Slice-wide mermaid count within budget (default 15).

  Output:
    ~/.archigraph/docs/<group>/.tier2-<timestamp>/<entity-id>-page.md  (N pages)
    ~/.archigraph/docs/<group>/.tier2-<timestamp>/score.json

  Example:
    archigraph docgen --tier=2 --group=mygroup \
      --seed-entity=abc123def456 --max-pages=5

TIER 3 (--tier=3) — full repo doc set (<20 min):
  Enumerates all page-worthy entities in ONE repo (services, packages, modules,
  viewsets, etc.) and runs Tier 2 per seed. Generates a repo-level index.md
  and enforces three repo-level contracts:
    • Every page-worthy entity has a home page (repo-coverage).
    • No two pages claim the same entity as primary (page-ownership).
    • Repo index.md links to every generated page (repo-index).

  Requires --repo <slug> (the repo slug from the group config).

  Output:
    ~/.archigraph/docs/<group>/.tier3-<timestamp>/<repo>/index.md
    ~/.archigraph/docs/<group>/.tier3-<timestamp>/<repo>/<entity-id>-page.md
    ~/.archigraph/docs/<group>/.tier3-<timestamp>/<repo>/score.json

  Example:
    archigraph docgen --tier=3 --group=mygroup --repo=core

TIER 4 (--tier=4) — full group doc set with cross-repo coherence (<60 s):
  Runs Tier 3 for every repo in the group CONCURRENTLY (pool size 3) and
  enforces three cross-repo contracts:
    • Every cross-repo link target has a page in its target repo (cross-repo-coverage).
    • Group index.md links to every repo's index (group-index).
    • No mermaid flow block appears in pages of 2+ different repos (cross-repo-flow-dedup).

  Output:
    ~/.archigraph/docs/<group>/.tier4-<timestamp>/index.md        (group-level)
    ~/.archigraph/docs/<group>/.tier4-<timestamp>/score.json      (group-level rollup)
    ~/.archigraph/docs/<group>/.tier4-<timestamp>/<repo>/index.md
    ~/.archigraph/docs/<group>/.tier4-<timestamp>/<repo>/score.json
    ~/.archigraph/docs/<group>/.tier4-<timestamp>/<repo>/<entity-id>-page.md

  Example:
    archigraph docgen --tier=4 --group=mygroup

Available sections (--section, used by --tier=0 only):
  ` + strings.Join(docgen.KnownSections, ", "),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listSecs {
				for _, s := range docgen.KnownSections {
					fmt.Fprintln(cmd.OutOrStdout(), s)
				}
				return nil
			}

			switch tier {
			case 0:
				return runDocgenTier0(cmd, group, seedEntity, section, outputDir, llmMode)
			case 1:
				return runDocgenTier1(cmd, group, seedEntity, pageID, outputDir, llmMode, bundleFile, resultFile)
			case 2:
				return runDocgenTier2(cmd, group, seedEntity, outputDir, llmMode, cacheDir, maxPages, mermaidBudget, noCache)
			case 3:
				return runDocgenTier3(cmd, group, repoSlug, outputDir, llmMode, cacheDir, maxPages, mermaidBudget, noCache)
			case 4:
				return runDocgenTier4(cmd, group, outputDir, llmMode, cacheDir, maxPages, mermaidBudget, noCache)
			default:
				return fmt.Errorf("--tier=%d is not yet implemented; available: 0, 1, 2, 3, 4", tier)
			}
		},
	}

	cmd.Flags().IntVar(&tier, "tier", 0,
		"docgen tier: 0 = single section snippet (<30 s); 1 = single complete page (<120 s); 2 = coherent slice cross-page (<10 min); 3 = full repo doc set (<20 min); 4 = full group with cross-repo contracts (<60 s deterministic)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 5,
		"maximum pages to generate for --tier=2 (seed + top-N dependents)")
	cmd.Flags().IntVar(&mermaidBudget, "mermaid-budget", 0,
		"override slice mermaid budget for --tier=2 (default 15)")
	cmd.Flags().StringVar(&group, "group", "",
		"group name (defaults to sole registered group)")
	cmd.Flags().StringVar(&seedEntity, "seed-entity", "",
		"entity ID to render (required for all tiers). Accepts both raw hex (e.g. 7a349f6cd77984c9) "+
			"and the prefixed form returned by archigraph_find (e.g. archigraph::7a349f6cd77984c9 "+
			"or upvate-core::7a349f6cd77984c9). The <group>:: prefix is stripped automatically.")
	cmd.Flags().StringVar(&section, "section", "",
		fmt.Sprintf("section type to render (required for --tier=0); one of: %s", strings.Join(docgen.KnownSections, ", ")))
	cmd.Flags().StringVar(&pageID, "page-id", "",
		"override output filename stem for --tier=1 (default: sanitised entity ID)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "",
		"override output directory (default: ~/.archigraph/docs/<group>/.tier{N}-<timestamp>/)")
	cmd.Flags().StringVar(&repoSlug, "repo", "",
		"repo slug within the group (required for --tier=3); see group config for available slugs")
	cmd.Flags().BoolVar(&listSecs, "list-sections", false,
		"print all valid section names and exit")
	cmd.Flags().StringVar(&llmMode, "llm-mode", "",
		`LLM integration mode: "" (default, stub-only), "emit" (write LLMPromptBundle JSON alongside stub), "apply" (Tier 0/1 only: read result file, validate, assemble final page; Tier 2/3/4 returns error)`)
	cmd.Flags().StringVar(&bundleFile, "bundle-file", "",
		"path to the LLMPromptBundle JSON file (required when --llm-mode=apply at Tier 0/1)")
	cmd.Flags().StringVar(&resultFile, "result-file", "",
		"path to the LLMRunResult JSON file written by the orchestrator (required when --llm-mode=apply at Tier 0/1)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "",
		"override the section-level LLM cache directory (default: ~/.archigraph/docs/<group>/.llm-cache/); applies to all tiers")
	cmd.Flags().BoolVar(&noCache, "no-cache", false,
		"disable section-level LLM cache reads and writes for this run; applies to all tiers")

	return cmd
}

// runDocgenTier0 executes the Tier 0 single-section fast path.
func runDocgenTier0(cmd *cobra.Command, group, seedEntity, section, outputDir, llmMode string) error {
	// Resolve group.
	resolvedGroup, err := resolveGroup(group)
	if err != nil {
		return err
	}

	// Validate required flags.
	if seedEntity == "" {
		return errors.New("--seed-entity is required for --tier=0\n\nHint: run `archigraph status` to list entity IDs, or use the MCP archigraph_find tool")
	}
	if section == "" {
		return fmt.Errorf("--section is required for --tier=0\n\nValid sections: %s", strings.Join(docgen.KnownSections, ", "))
	}

	opts := docgen.RunOpts{
		Group:        resolvedGroup,
		SeedEntityID: seedEntity,
		Section:      section,
		OutputDir:    outputDir,
		LLMMode:      llmMode,
	}

	mdPath, scorePath, score, err := docgen.Run(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 0: %w", err)
	}

	// Print human-readable summary.
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier0 complete\n\n")
	fmt.Fprintf(out, "  section:   %s\n", score.Section)
	fmt.Fprintf(out, "  entity:    %s\n", score.SeedEntity)
	fmt.Fprintf(out, "  found:     %v\n", score.SeedEntityFound)
	fmt.Fprintf(out, "  wall:      %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:    ~%d\n", score.TokenCountEstimate)
	fmt.Fprintf(out, "  lines:     %d\n", score.Lines)
	fmt.Fprintf(out, "  words:     %d\n", score.Words)
	fmt.Fprintf(out, "  mermaid:   %d\n", score.MermaidCount)
	fmt.Fprintf(out, "  neighbours:%d\n", score.NeighboursIncluded)
	if score.LLMMode != "" {
		fmt.Fprintf(out, "  llm-mode:  %s\n", score.LLMMode)
	}
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "  output:    %s\n", mdPath)
	fmt.Fprintf(out, "  score:     %s\n", scorePath)
	if llmMode == "emit" {
		// Bundle file sits next to the stub with a predictable name.
		bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
		fmt.Fprintf(out, "  bundle:    %s\n", bundlePath)
	}

	// Print SCORE.json to stdout when running interactively (pipe detection
	// omitted intentionally — the score is small and always useful).
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier1 executes the Tier 1 single-page path (<120 s).
// When llmMode == "apply", bundleFile and resultFile must be non-empty; the
// function delegates to docgen.ApplyResult instead of docgen.RunTier1.
func runDocgenTier1(cmd *cobra.Command, group, seedEntity, pageID, outputDir, llmMode, bundleFile, resultFile string) error {
	// --llm-mode=apply is a special sub-path that does not require --seed-entity
	// (the entity ID is read from the bundle file) and requires --bundle-file +
	// --result-file instead.
	if llmMode == "apply" {
		return runDocgenTier1Apply(cmd, pageID, outputDir, bundleFile, resultFile)
	}

	resolvedGroup, err := resolveGroup(group)
	if err != nil {
		return err
	}

	if seedEntity == "" {
		return errors.New("--seed-entity is required for --tier=1\n\nHint: run `archigraph status` to list entity IDs, or use the MCP archigraph_find tool")
	}

	opts := docgen.Tier1RunOpts{
		Group:        resolvedGroup,
		SeedEntityID: seedEntity,
		PageID:       pageID,
		OutputDir:    outputDir,
		LLMMode:      llmMode,
	}

	mdPath, scorePath, score, err := docgen.RunTier1(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 1: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier1 complete\n\n")
	fmt.Fprintf(out, "  entity:     %s\n", score.SeedEntity)
	fmt.Fprintf(out, "  found:      %v\n", score.SeedEntityFound)
	fmt.Fprintf(out, "  sections:   %d\n", score.SectionCount)
	fmt.Fprintf(out, "  wall:       %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:     ~%d\n", score.TokenCountEstimate)
	fmt.Fprintf(out, "  mermaid:    %d (oversized sections: %d)\n", score.MermaidCount, score.MermaidOversized)
	fmt.Fprintf(out, "  links:      %d (unresolved: %d)\n", score.InternalLinkCount, score.InternalLinkUnresolved)
	fmt.Fprintf(out, "  dup-flows:  %d\n", score.DuplicatedFlowCount)
	fmt.Fprintf(out, "  anchors:    %d\n", score.AnchorCount)
	if score.LLMMode != "" {
		fmt.Fprintf(out, "  llm-mode:   %s\n", score.LLMMode)
	}
	if len(score.ContractViolations) > 0 {
		// In emit mode, contract violations are informational — the LLM will
		// produce the real prose; the stub contracts are indicative only.
		label := "CONTRACT VIOLATIONS"
		if llmMode == "emit" {
			label = "CONTRACT NOTES (emit mode — informational)"
		}
		fmt.Fprintf(out, "\n  %s (%d):\n", label, len(score.ContractViolations))
		for _, v := range score.ContractViolations {
			fmt.Fprintf(out, "    - %s\n", v)
		}
	} else {
		fmt.Fprintf(out, "  contract:   PASS\n")
	}
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "  output:     %s\n", mdPath)
	fmt.Fprintf(out, "  score:      %s\n", scorePath)
	if llmMode == "emit" {
		// Bundle file sits next to the page with a predictable name.
		// mdPath is <outDir>/<pageID>-page.md; bundle is <outDir>/<pageID>-page-bundle.json
		bundlePath := mdPath[:len(mdPath)-len(".md")] + "-bundle.json"
		fmt.Fprintf(out, "  bundle:     %s\n", bundlePath)
	}
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier1Apply executes the Tier 1 --llm-mode=apply path.
// It reads the bundle + result files, delegates to docgen.ApplyResult, and
// prints a human-readable summary with the same layout as the normal Tier 1 run.
func runDocgenTier1Apply(cmd *cobra.Command, pageID, outputDir, bundleFile, resultFile string) error {
	if bundleFile == "" {
		return errors.New("--bundle-file is required when --llm-mode=apply")
	}
	if resultFile == "" {
		return errors.New("--result-file is required when --llm-mode=apply")
	}

	opts := docgen.Tier1RunOpts{
		PageID:     pageID,
		OutputDir:  outputDir,
		LLMMode:    "apply",
		BundleFile: bundleFile,
		ResultFile: resultFile,
	}

	mdPath, scorePath, score, err := docgen.ApplyResult(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 1 apply: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier1 apply complete\n\n")
	fmt.Fprintf(out, "  entity:     %s\n", score.SeedEntity)
	fmt.Fprintf(out, "  found:      %v\n", score.SeedEntityFound)
	fmt.Fprintf(out, "  sections:   %d\n", score.SectionCount)
	fmt.Fprintf(out, "  wall:       %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:     ~%d\n", score.TokenCountEstimate)
	fmt.Fprintf(out, "  mermaid:    %d (oversized sections: %d)\n", score.MermaidCount, score.MermaidOversized)
	fmt.Fprintf(out, "  links:      %d (unresolved: %d)\n", score.InternalLinkCount, score.InternalLinkUnresolved)
	fmt.Fprintf(out, "  dup-flows:  %d\n", score.DuplicatedFlowCount)
	fmt.Fprintf(out, "  anchors:    %d\n", score.AnchorCount)
	fmt.Fprintf(out, "  llm-mode:   apply\n")
	if len(score.ContractViolations) > 0 {
		fmt.Fprintf(out, "\n  CONTRACT VIOLATIONS (%d):\n", len(score.ContractViolations))
		for _, v := range score.ContractViolations {
			fmt.Fprintf(out, "    - %s\n", v)
		}
	} else {
		fmt.Fprintf(out, "  contract:   PASS\n")
	}
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "  output:     %s\n", mdPath)
	fmt.Fprintf(out, "  score:      %s\n", scorePath)
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier2 executes the Tier 2 coherent-slice path (<10 min).
func runDocgenTier2(cmd *cobra.Command, group, seedEntity, outputDir, llmMode, cacheDir string, maxPages, mermaidBudget int, noCache bool) error {
	resolvedGroup, err := resolveGroup(group)
	if err != nil {
		return err
	}

	if seedEntity == "" {
		return errors.New("--seed-entity is required for --tier=2\n\nHint: run `archigraph status` to list entity IDs, or use the MCP archigraph_find tool")
	}

	opts := docgen.Tier2RunOpts{
		Group:         resolvedGroup,
		SeedEntityID:  seedEntity,
		MaxPages:      maxPages,
		MermaidBudget: mermaidBudget,
		OutputDir:     outputDir,
		LLMMode:       llmMode,
		CacheDir:      cacheDir,
		NoCache:       noCache,
	}

	outDir, score, err := docgen.RunTier2(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 2: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier2 complete\n\n")
	fmt.Fprintf(out, "  seed:           %s\n", seedEntity)
	fmt.Fprintf(out, "  pages:          %d\n", score.PageCount)
	fmt.Fprintf(out, "  wall:           %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:         ~%d\n", score.TotalTokenCount)
	fmt.Fprintf(out, "  mermaid:        %d (budget: %d)\n", score.SliceMermaidCount, opts.MermaidBudget)
	fmt.Fprintf(out, "  cross-links:    %d (unresolved: %d)\n", score.CrossPageLinkCount, score.CrossPageLinkUnresolved)
	fmt.Fprintf(out, "  flow-dups:      %d\n", score.FlowDuplicationViolations)
	fmt.Fprintf(out, "  pattern-links:  %d violations\n", score.PatternLinkViolations)
	fmt.Fprintf(out, "  anchor-consist: %d violations\n", score.AnchorConsistencyViolations)
	fmt.Fprintf(out, "  mermaid-budget: %d violations\n", score.SliceMermaidBudgetViolations)
	if score.LLMMode != "" {
		fmt.Fprintf(out, "  llm-mode:       %s\n", score.LLMMode)
	}

	totalViolations := score.FlowDuplicationViolations + score.PatternLinkViolations +
		score.AnchorConsistencyViolations + score.SliceMermaidBudgetViolations
	if totalViolations > 0 {
		fmt.Fprintf(out, "\n  CROSS-PAGE VIOLATIONS (%d):\n", totalViolations)
		for _, v := range score.Violations {
			fmt.Fprintf(out, "    - %s\n", v)
		}
	} else {
		fmt.Fprintf(out, "  contract:       PASS\n")
	}

	fmt.Fprintf(out, "\n  output:         %s\n", outDir)
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier3 executes the Tier 3 full-repo doc set path (<20 min).
func runDocgenTier3(cmd *cobra.Command, group, repoSlug, outputDir, llmMode, cacheDir string, maxPages, mermaidBudget int, noCache bool) error {
	resolvedGroup, err := resolveGroup(group)
	if err != nil {
		return err
	}

	opts := docgen.Tier3RunOpts{
		Group:         resolvedGroup,
		RepoSlug:      repoSlug,
		MaxPages:      maxPages,
		MermaidBudget: mermaidBudget,
		OutputDir:     outputDir,
		LLMMode:       llmMode,
		CacheDir:      cacheDir,
		NoCache:       noCache,
	}

	rootDir, score, err := docgen.RunTier3(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 3: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier3 complete\n\n")
	fmt.Fprintf(out, "  repo:             %s\n", score.Repo)
	fmt.Fprintf(out, "  pages:            %d\n", score.PageCount)
	fmt.Fprintf(out, "  slices:           %d\n", score.SliceCount)
	fmt.Fprintf(out, "  wall:             %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:           ~%d\n", score.TotalTokenCount)
	fmt.Fprintf(out, "  index-links:      %d (unresolved: %d)\n", score.IndexLinkCount, score.IndexLinkUnresolved)
	fmt.Fprintf(out, "  missing-coverage: %d\n", score.MissingCoverageCount)
	fmt.Fprintf(out, "  ownership-conflicts: %d\n", score.OwnershipConflictCount)
	fmt.Fprintf(out, "  skipped:          %d (above %d-seed cap)\n", score.SkippedBelowBudgetCount, docgen.MaxSeedsPerRepo)
	if score.LLMMode != "" {
		fmt.Fprintf(out, "  llm-mode:         %s\n", score.LLMMode)
	}

	totalViolations := score.MissingCoverageCount + score.OwnershipConflictCount + score.IndexLinkUnresolved
	if totalViolations > 0 {
		fmt.Fprintf(out, "\n  REPO-LEVEL VIOLATIONS (%d):\n", len(score.Violations))
		for _, v := range score.Violations {
			fmt.Fprintf(out, "    - %s\n", v)
		}
	} else {
		fmt.Fprintf(out, "  contracts:        PASS\n")
	}

	fmt.Fprintf(out, "\n  output:           %s\n", rootDir)
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier4 executes the Tier 4 full-group doc set path (<60 s).
func runDocgenTier4(cmd *cobra.Command, group, outputDir, llmMode, cacheDir string, maxPages, mermaidBudget int, noCache bool) error {
	resolvedGroup, err := resolveGroup(group)
	if err != nil {
		return err
	}

	opts := docgen.Tier4RunOpts{
		Group:         resolvedGroup,
		MaxPages:      maxPages,
		MermaidBudget: mermaidBudget,
		OutputDir:     outputDir,
		LLMMode:       llmMode,
		CacheDir:      cacheDir,
		NoCache:       noCache,
	}

	rootDir, score, err := docgen.RunTier4(opts)
	if err != nil {
		return fmt.Errorf("docgen tier 4: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tier4 complete\n\n")
	fmt.Fprintf(out, "  group:                 %s\n", score.Group)
	fmt.Fprintf(out, "  repos:                 %d\n", score.RepoCount)
	fmt.Fprintf(out, "  total-pages:           %d\n", score.TotalPageCount)
	fmt.Fprintf(out, "  wall:                  %d ms\n", score.WallTimeMS)
	fmt.Fprintf(out, "  tokens:                ~%d\n", score.TotalTokenCount)
	fmt.Fprintf(out, "  cross-repo-links:      %d (unresolved: %d)\n", score.CrossRepoLinkCount, score.CrossRepoLinkUnresolved)
	fmt.Fprintf(out, "  flow-dedup-violations: %d\n", score.CrossRepoFlowDedupViolations)
	fmt.Fprintf(out, "  group-index-unresolved:%d\n", score.GroupIndexUnresolved)
	if score.LLMMode != "" {
		fmt.Fprintf(out, "  llm-mode:              %s\n", score.LLMMode)
	}

	totalViolations := score.CrossRepoLinkUnresolved + score.CrossRepoFlowDedupViolations + score.GroupIndexUnresolved
	if totalViolations > 0 || len(score.Violations) > 0 {
		fmt.Fprintf(out, "\n  CROSS-REPO VIOLATIONS (%d):\n", len(score.Violations))
		for _, v := range score.Violations {
			fmt.Fprintf(out, "    - %s\n", v)
		}
	} else {
		fmt.Fprintf(out, "  contracts:             PASS\n")
	}

	fmt.Fprintf(out, "\n  per-repo summary:\n")
	for _, rs := range score.PerRepoScores {
		fmt.Fprintf(out, "    %-20s  pages: %d  violations: %d\n",
			rs.Repo, rs.PageCount, len(rs.Violations))
	}

	fmt.Fprintf(out, "\n  output:                %s\n", rootDir)
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// resolveGroup resolves the group name, defaulting to the sole registered
// group when only one exists.
func resolveGroup(group string) (string, error) {
	if group != "" {
		return group, nil
	}
	groups, err := registry.Groups()
	if err != nil {
		return "", fmt.Errorf("read registry: %w", err)
	}
	if len(groups) == 0 {
		return "", errors.New("no groups registered; run `archigraph wizard` first")
	}
	if len(groups) == 1 {
		return groups[0].Name, nil
	}
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	return "", fmt.Errorf("multiple groups registered (%s); pass --group <name>",
		strings.Join(names, ", "))
}

// resolveGroupConfig reads the raw group config JSON. Exported for tests.
func resolveGroupConfig(group string) (map[string]interface{}, error) {
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
