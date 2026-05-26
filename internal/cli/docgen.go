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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Sub-commands: storage discipline helpers (#2190), output discipline (#2194),
	// and lifecycle management (#2216).
	cmd.AddCommand(newDocgenMigrateInRepoCmd())
	cmd.AddCommand(newDocgenAuditCmd())
	cmd.AddCommand(newDocgenCleanupScaffoldingCmd())
	cmd.AddCommand(newDocgenCleanupCmd())

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

// ---------------------------------------------------------------------------
// Storage-discipline helpers (#2190)
// ---------------------------------------------------------------------------

// docgenHeuristics are file names that indicate a directory is archigraph
// docgen output (as opposed to hand-written docs). The heuristic is
// conservative: all three markers come from the generate-docs skill pipeline.
var docgenHeuristics = []string{
	".plan.md",
	".inventory.json",
	".metadata.json",
}

// isDocgenOutput reports whether dir looks like archigraph docgen output by
// checking for any of the heuristic marker files.
func isDocgenOutput(dir string) bool {
	for _, name := range docgenHeuristics {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// findInRepoDocgenDirs walks every repo in cfg and returns the list of
// docs/ (or doc/) directories that appear to be docgen output.
// It does NOT walk into ~/.archigraph/ to avoid false positives.
func findInRepoDocgenDirs(cfg *registry.GroupConfig) []string {
	var found []string
	for _, r := range cfg.Repos {
		if r.Path == "" {
			continue
		}
		for _, candidate := range []string{"docs", "doc"} {
			dir := filepath.Join(r.Path, candidate)
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				continue
			}
			if isDocgenOutput(dir) {
				found = append(found, dir)
			}
		}
	}
	return found
}

// newDocgenMigrateInRepoCmd returns the `archigraph docgen migrate-in-repo`
// subcommand (#2190, extended in #2216 to cover staging-dir layout).
func newDocgenMigrateInRepoCmd() *cobra.Command {
	var group string
	var yes bool

	cmd := &cobra.Command{
		Use:   "migrate-in-repo [--group <name>] [--yes]",
		Short: "Move in-repo docgen output and orphaned staging runs to the archigraph store",
		Long: `Walks every repo registered in the group and looks for:

  1. docs/ (or doc/) directories that appear to be archigraph docgen output
     (heuristic: presence of .plan.md, .inventory.json, or .metadata.json).

  2. <project>/.archigraph/staging/<run_id>/ directories that were never
     promoted (e.g. aborted runs). These are moved to the canonical store
     under ~/.archigraph/docs/<group>/.staging-recovered/<run_id>/.

For each match the user is asked to confirm before the directory is moved.
Existing canonical docs are backed up to <canonical>.previous-<timestamp>/
before being overwritten.

The operation is idempotent: if the target already exists the command skips
that repo with a warning rather than overwriting existing store content.

Use --yes to skip confirmation prompts (non-interactive / CI).

After migration, the source directory inside the repo working tree is removed.
Run ` + "`archigraph docgen audit`" + ` first to inspect what would be moved without
making any changes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedGroup, err := resolveGroup(group)
			if err != nil {
				return err
			}

			cfg, err := loadGroupConfigFromRegistry(resolvedGroup)
			if err != nil {
				return err
			}

			homeDir, err := registry.HomeDir()
			if err != nil {
				return fmt.Errorf("resolve archigraph home: %w", err)
			}

			w := cmd.OutOrStdout()
			scanner := bufio.NewScanner(cmd.InOrStdin())
			migrated, skipped := 0, 0

			// ── Phase 1: in-repo docs/ directories ───────────────────────
			dirs := findInRepoDocgenDirs(cfg)
			for _, srcDir := range dirs {
				// Determine target path: ~/.archigraph/docs/<group>/<repo-slug>/
				repoSlug := ""
				for _, r := range cfg.Repos {
					if r.Path != "" && strings.HasPrefix(srcDir, r.Path) {
						repoSlug = r.Slug
						break
					}
				}
				if repoSlug == "" {
					repoSlug = filepath.Base(filepath.Dir(srcDir))
				}

				targetDir := filepath.Join(homeDir, "docs", resolvedGroup, repoSlug)

				fmt.Fprintf(w, "\nFound in-repo docgen output:\n  src:  %s\n  dst:  %s\n", srcDir, targetDir)

				if !yes {
					fmt.Fprintf(w, "Move? [y/N]: ")
					if !scanner.Scan() {
						fmt.Fprintln(w, "  skipped (no input)")
						skipped++
						continue
					}
					if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
						fmt.Fprintln(w, "  skipped.")
						skipped++
						continue
					}
				}

				// Idempotency guard: skip if target already exists.
				if _, err := os.Stat(targetDir); err == nil {
					fmt.Fprintf(w, "  [warn] target already exists, skipping to avoid overwrite: %s\n", targetDir)
					skipped++
					continue
				}

				if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
					return fmt.Errorf("create parent for %s: %w", targetDir, err)
				}
				if err := os.Rename(srcDir, targetDir); err != nil {
					return fmt.Errorf("move %s → %s: %w", srcDir, targetDir, err)
				}
				fmt.Fprintf(w, "  [ ok ] moved to %s\n", targetDir)
				migrated++
			}

			// ── Phase 2: orphaned staging runs (#2216) ────────────────────
			// Build a repo list for the docgen.RunMigrateInRepo call.
			var migrateRepos []docgen.MigrateRepo
			for _, r := range cfg.Repos {
				migrateRepos = append(migrateRepos, docgen.MigrateRepo{
					Slug: r.Slug,
					Path: r.Path,
				})
			}

			migrateOpts := docgen.MigrateOptions{
				Group:   resolvedGroup,
				HomeDir: homeDir,
				// StagingOnly: Phase 1 above already processed docs/ dirs with
				// the idempotency guard; RunMigrateInRepo must only sweep
				// orphaned staging runs to avoid double-processing docs/ dirs.
				StagingOnly: true,
				GroupConfigLoader: func(_ string) ([]docgen.MigrateRepo, error) {
					return migrateRepos, nil
				},
				GroupsLoader: func() ([]string, error) { return []string{resolvedGroup}, nil },
				Yes:          yes,
				ConfirmFn: func(msg string) bool {
					if yes {
						return true
					}
					fmt.Fprintf(w, "\n%s\nMove? [y/N]: ", msg)
					if !scanner.Scan() {
						return false
					}
					return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
				},
			}

			migrateResult, migrateErr := docgen.RunMigrateInRepo(migrateOpts)
			if migrateErr != nil {
				fmt.Fprintf(w, "[warn] staging migration error: %v\n", migrateErr)
			} else {
				for _, p := range migrateResult.Migrated {
					fmt.Fprintf(w, "  [ ok ] staged run moved: %s → %s\n", p.Src, p.Dst)
					migrated++
				}
				for _, s := range migrateResult.Skipped {
					fmt.Fprintf(w, "  skipped staged run: %s\n", s)
					skipped++
				}
				for _, e := range migrateResult.Errors {
					fmt.Fprintf(w, "[warn] %s\n", e)
				}
			}

			fmt.Fprintf(w, "\nmigrate-in-repo complete: %d moved, %d skipped.\n", migrated, skipped)
			return nil
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group name (defaults to sole registered group)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompts and move all matches automatically")
	return cmd
}

// newDocgenAuditCmd returns the `archigraph docgen audit` subcommand (#2190).
// It reports in-repo docgen output without moving anything.
func newDocgenAuditCmd() *cobra.Command {
	var group string

	cmd := &cobra.Command{
		Use:   "audit [--group <name>]",
		Short: "Detect in-repo docgen output (read-only; does not move anything)",
		Long: `Walks every repo registered in the group and reports docs/ (or doc/)
directories that appear to contain archigraph docgen output (heuristic: presence
of .plan.md, .inventory.json, or .metadata.json).

Nothing is moved or deleted. Use this before ` + "`archigraph docgen migrate-in-repo`" + `
to inspect what would be migrated.

Exit codes:
  0  No in-repo docgen output detected.
  1  One or more in-repo docgen directories found (actionable: run migrate-in-repo).
  2  Internal error (registry unreadable, etc.).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedGroup, err := resolveGroup(group)
			if err != nil {
				return err
			}

			cfg, err := loadGroupConfigFromRegistry(resolvedGroup)
			if err != nil {
				return err
			}

			dirs := findInRepoDocgenDirs(cfg)
			w := cmd.OutOrStdout()

			if len(dirs) == 0 {
				fmt.Fprintln(w, "[ ok ] No in-repo docgen output detected for group:", resolvedGroup)
				return nil
			}

			fmt.Fprintf(w, "[warn] In-repo docgen output detected (group: %s):\n", resolvedGroup)
			for _, d := range dirs {
				// Report which heuristic markers were found.
				var markers []string
				for _, name := range docgenHeuristics {
					if _, err := os.Stat(filepath.Join(d, name)); err == nil {
						markers = append(markers, name)
					}
				}
				fmt.Fprintf(w, "  %s  (markers: %s)\n", d, strings.Join(markers, ", "))
			}
			fmt.Fprintln(w, "\nRun `archigraph docgen migrate-in-repo` to move these to the archigraph store.")

			// Return a sentinel error so the shell exit code is 1.
			return &docgenAuditError{count: len(dirs)}
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group name (defaults to sole registered group)")
	return cmd
}

// newDocgenCleanupScaffoldingCmd returns the `archigraph docgen cleanup-scaffolding`
// subcommand (#2194 — OUTPUT DISCIPLINE).
//
// It walks:
//   - Every registered repo's docs/ (and doc/) directory
//   - ~/.archigraph/docs/<group>/
//
// …looking for SSG-scaffolding artifacts (VitePress, Docusaurus, Sphinx,
// mkdocs, package.json, config.ts/config.js) that a misbehaving agent may
// have written. Prompts the user to confirm before removing each artifact.
// Use --yes for non-interactive / CI mode.
func newDocgenCleanupScaffoldingCmd() *cobra.Command {
	var group string
	var yes bool

	cmd := &cobra.Command{
		Use:   "cleanup-scaffolding [--group <name>] [--yes]",
		Short: "Remove SSG-scaffolding artifacts written by a misbehaving agent",
		Long: `Walks every repo registered in the group (docs/ and doc/) AND the
archigraph-managed store (~/.archigraph/docs/<group>/) looking for
SSG-scaffolding artifacts that the generate-docs skill must never produce:

  .vitepress/    VitePress config directory
  .docusaurus/   Docusaurus config directory
  sphinx/        Sphinx build directory
  mkdocs.yml     MkDocs config file
  config.ts      VitePress / generic SSG entry-point
  config.js      generic SSG entry-point
  package.json   SSG build manifest (at docs root)

Each match is reported before removal. The operation is idempotent: running
cleanup-scaffolding a second time on a clean tree is a no-op.

Use --yes to skip confirmation prompts (non-interactive / CI).

Closes #2194. Parallel to migrate-in-repo (#2190) for storage discipline.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedGroup, err := resolveGroup(group)
			if err != nil {
				return err
			}

			cfg, err := loadGroupConfigFromRegistry(resolvedGroup)
			if err != nil {
				return err
			}

			homeDir, err := registry.HomeDir()
			if err != nil {
				return fmt.Errorf("resolve archigraph home: %w", err)
			}

			// Collect all directories to scan.
			var scanRoots []string

			// 1. Repo working trees (docs/ and doc/).
			for _, r := range cfg.Repos {
				if r.Path == "" {
					continue
				}
				for _, sub := range []string{"docs", "doc"} {
					dir := filepath.Join(r.Path, sub)
					if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
						scanRoots = append(scanRoots, dir)
					}
				}
			}

			// 2. Archigraph-managed store for this group.
			storeDir := filepath.Join(homeDir, "docs", resolvedGroup)
			if info, statErr := os.Stat(storeDir); statErr == nil && info.IsDir() {
				scanRoots = append(scanRoots, storeDir)
			}

			if len(scanRoots) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No directories to scan.")
				return nil
			}

			// Find scaffolding artifacts in every root.
			artifacts := findSSGScaffoldingArtifacts(scanRoots)
			w := cmd.OutOrStdout()

			if len(artifacts) == 0 {
				fmt.Fprintln(w, "[ ok ] No SSG-scaffolding artifacts detected.")
				return nil
			}

			fmt.Fprintf(w, "[OUTPUT DISCIPLINE] SSG-scaffolding artifacts detected (%d):\n", len(artifacts))
			for _, a := range artifacts {
				fmt.Fprintf(w, "  %s  (%s)\n", a.path, a.reason)
			}

			scanner := bufio.NewScanner(cmd.InOrStdin())
			removed, skipped := 0, 0

			for _, a := range artifacts {
				if !yes {
					fmt.Fprintf(w, "\nRemove %s? [y/N]: ", a.path)
					if !scanner.Scan() {
						fmt.Fprintln(w, "  skipped (no input)")
						skipped++
						continue
					}
					if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
						fmt.Fprintln(w, "  skipped.")
						skipped++
						continue
					}
				}

				if removeErr := os.RemoveAll(a.path); removeErr != nil {
					fmt.Fprintf(w, "  [error] remove %s: %v\n", a.path, removeErr)
					skipped++
					continue
				}
				fmt.Fprintf(w, "  [ ok ] removed %s\n", a.path)
				removed++
			}

			fmt.Fprintf(w, "\ncleanup-scaffolding complete: %d removed, %d skipped.\n", removed, skipped)
			return nil
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group name (defaults to sole registered group)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompts and remove all matches automatically")
	return cmd
}

// ssgArtifact pairs a path with a human-readable reason string.
type ssgArtifact struct {
	path   string
	reason string
}

// findSSGScaffoldingArtifacts walks each root directory (non-recursively for
// the top level, then descends one level into subgroups in the store) and
// returns all entries that match the SSG-scaffolding patterns.
func findSSGScaffoldingArtifacts(roots []string) []ssgArtifact {
	var found []ssgArtifact

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fullPath := filepath.Join(root, e.Name())
			if reason := ssgArtifactReason(e.Name(), e.IsDir()); reason != "" {
				found = append(found, ssgArtifact{path: fullPath, reason: reason})
			}
		}
	}
	return found
}

// ssgArtifactReason returns a human-readable reason string if name matches an
// SSG-scaffolding pattern, or "" if it is clean.
// isDir is true when the entry is a directory.
func ssgArtifactReason(name string, isDir bool) string {
	switch {
	case name == ".vitepress" && isDir:
		return "VitePress config directory"
	case name == ".docusaurus" && isDir:
		return "Docusaurus cache/config directory"
	case name == "sphinx" && isDir:
		return "Sphinx build directory"
	case name == "mkdocs.yml":
		return "MkDocs config file"
	case name == "config.ts":
		return "SSG config entry-point (config.ts)"
	case name == "config.js":
		return "SSG config entry-point (config.js)"
	case name == "package.json":
		return "SSG build manifest (package.json at docs root)"
	}
	return ""
}

// newDocgenCleanupCmd returns the `archigraph docgen cleanup` subcommand (#2216).
//
// Removes stale staging runs and .previous-* backups from the docgen store.
// Safe by design: canonical docs are never touched.
func newDocgenCleanupCmd() *cobra.Command {
	var group string
	var maxAge string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "cleanup [--group <name>] [--max-age 7d] [--dry-run]",
		Short: "Remove stale staging runs and .previous-* backups from the docgen store",
		Long: `Walks the staging directory (<project>/.archigraph/staging/) and the
backup directories (~/.archigraph/docs/<group>.previous-*/) and removes any
entry whose creation time is older than --max-age (default: 7 days).

Canonical docs (~/.archigraph/docs/<group>/) are NEVER touched.
The operation is idempotent: running it on an already-clean tree is a no-op.

Use --dry-run to report what would be removed without making any changes.
Use --group to scope cleanup to a single group (default: all groups).

Examples:
  archigraph docgen cleanup
  archigraph docgen cleanup --group mygroup --max-age 3d --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			age, err := parseMaxAge(maxAge)
			if err != nil {
				return fmt.Errorf("--max-age: %w", err)
			}

			// Collect project roots from the registry so staging dirs can be found.
			var projectRoots []string
			if group != "" {
				cfg, cfgErr := loadGroupConfigFromRegistry(group)
				if cfgErr == nil {
					for _, r := range cfg.Repos {
						if r.Path != "" {
							projectRoots = append(projectRoots, r.Path)
						}
					}
				}
			} else {
				groups, gErr := registry.Groups()
				if gErr == nil {
					for _, g := range groups {
						cfg, cfgErr := registry.LoadGroupConfig(g.ConfigPath)
						if cfgErr != nil {
							continue
						}
						for _, r := range cfg.Repos {
							if r.Path != "" {
								projectRoots = append(projectRoots, r.Path)
							}
						}
					}
				}
			}

			opts := docgen.CleanupOptions{
				Group:        group,
				MaxAge:       age,
				DryRun:       dryRun,
				ProjectRoots: projectRoots,
			}

			result, err := docgen.RunDocgenCleanup(opts)
			if err != nil {
				return fmt.Errorf("docgen cleanup: %w", err)
			}

			w := cmd.OutOrStdout()
			prefix := ""
			if dryRun {
				prefix = "(dry-run) "
			}
			if len(result.RemovedPaths) == 0 {
				fmt.Fprintf(w, "%s[ ok ] Nothing to remove.\n", prefix)
			} else {
				fmt.Fprintf(w, "%sRemoved %d item(s), freed %s:\n",
					prefix, len(result.RemovedPaths), formatBytes(result.TotalBytes))
				for _, p := range result.RemovedPaths {
					fmt.Fprintf(w, "  %s\n", p)
				}
			}
			for _, e := range result.Errors {
				fmt.Fprintf(w, "[warn] %s\n", e)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&group, "group", "",
		"scope cleanup to a single group (default: all groups)")
	cmd.Flags().StringVar(&maxAge, "max-age", "7d",
		"remove entries older than this duration (e.g. 7d, 24h, 168h)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"report what would be removed without making any changes")
	return cmd
}

// parseMaxAge parses a human-readable duration string like "7d", "24h",
// "168h" into a time.Duration.
func parseMaxAge(s string) (time.Duration, error) {
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}
	// Support "Nd" shorthand for N days.
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := parseInt64(s[:len(s)-1])
		if err != nil {
			return 0, fmt.Errorf("invalid days value %q: %w", s, err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (try e.g. 7d, 168h): %w", s, err)
	}
	return d, nil
}

// parseInt64 parses a decimal integer string.
func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit character %q", c)
		}
		n = n*10 + int64(c-'0')
	}
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	return n, nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// loadGroupConfigFromRegistry looks up the group's config path from the
// registry (which respects ARCHIGRAPH_HOME) and loads it. This is the
// correct way to load group config when the caller only has a group name —
// it avoids hardcoding the XDG config path.
func loadGroupConfigFromRegistry(groupName string) (*registry.GroupConfig, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	for _, g := range groups {
		if g.Name == groupName {
			cfg, err := registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				return nil, fmt.Errorf("load group config for %q: %w", groupName, err)
			}
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("group %q not found in registry; run `archigraph wizard` to register it", groupName)
}

// docgenAuditError is a sentinel returned by the audit command when in-repo
// docgen output is found. It causes a non-zero exit code without printing an
// extra error message (cobra prints Error() for non-nil errors).
type docgenAuditError struct{ count int }

func (e *docgenAuditError) Error() string {
	return fmt.Sprintf("%d in-repo docgen director(ies) detected; run `archigraph docgen migrate-in-repo` to fix", e.count)
}

// auditDocgenForGroup is the shared logic used by both `archigraph docgen
// audit` and the `--audit-docs` flag on `archigraph doctor`. It returns the
// list of offending directories (nil == clean). The w parameter is unused
// in this function but kept for interface consistency with callers that also
// write supplementary output — see runDoctorAuditDocs.
func auditDocgenForGroup(w interface{ Write([]byte) (int, error) }, groupName string) ([]string, error) {
	cfg, err := loadGroupConfigFromRegistry(groupName)
	if err != nil {
		return nil, err
	}
	dirs := findInRepoDocgenDirs(cfg)
	return dirs, nil
}

// DocsDirFor is a package-level helper that returns ~/.archigraph/docs/<group>/
// using the canonical HomeDir logic from the registry package. Exported so
// the doctor command and tests can reference the expected path.
func DocsDirFor(group string) (string, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "docs", group), nil
}

// looksLikeGitWorkdir reports true when dir (or any ancestor up to depth 3)
// contains a .git entry — a cheap guard used by the audit to avoid flagging
// the archigraph store itself if it happens to live inside a git repo.
func looksLikeGitWorkdir(dir string) bool {
	p := dir
	for range [3]struct{}{} {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return false
}

// walkSubdirs calls fn for every immediate subdirectory of dir. It does not
// recurse further — the heuristic only needs the top-level docs/ directory.
func walkSubdirs(dir string, fn func(string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			fn(filepath.Join(dir, e.Name()))
		}
	}
}

// Ensure fs and filepath are used (they are used by the migration helpers).
var _ = fs.ErrNotExist
var _ = filepath.Join
