// Package cli — `archigraph docgen` subcommand (Tier 0 + Tier 1, issue #1760).
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
// Full-group rendering (Tier 2–4) is not yet wired.
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
		tier       int
		group      string
		seedEntity string
		section    string
		pageID     string
		outputDir  string
		listSecs   bool
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

TIER 2–4 — full multi-page rendering:
  Not yet implemented. Use the /generate-docs skill in Claude Code for
  full-group documentation generation.

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
				return runDocgenTier0(cmd, group, seedEntity, section, outputDir)
			case 1:
				return runDocgenTier1(cmd, group, seedEntity, pageID, outputDir)
			default:
				return fmt.Errorf("--tier=%d is not yet implemented; available: 0, 1", tier)
			}
		},
	}

	cmd.Flags().IntVar(&tier, "tier", 0,
		"docgen tier: 0 = single section snippet (<30 s); 1 = single complete page (<120 s); 2–4 = full group rendering (not yet implemented)")
	cmd.Flags().StringVar(&group, "group", "",
		"group name (defaults to sole registered group)")
	cmd.Flags().StringVar(&seedEntity, "seed-entity", "",
		"entity ID (or prefix) to render (required for all tiers)")
	cmd.Flags().StringVar(&section, "section", "",
		fmt.Sprintf("section type to render (required for --tier=0); one of: %s", strings.Join(docgen.KnownSections, ", ")))
	cmd.Flags().StringVar(&pageID, "page-id", "",
		"override output filename stem for --tier=1 (default: sanitised entity ID)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "",
		"override output directory (default: ~/.archigraph/docs/<group>/.tier{N}-<timestamp>/)")
	cmd.Flags().BoolVar(&listSecs, "list-sections", false,
		"print all valid section names and exit")

	return cmd
}

// runDocgenTier0 executes the Tier 0 single-section fast path.
func runDocgenTier0(cmd *cobra.Command, group, seedEntity, section, outputDir string) error {
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
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "  output:    %s\n", mdPath)
	fmt.Fprintf(out, "  score:     %s\n", scorePath)

	// Print SCORE.json to stdout when running interactively (pipe detection
	// omitted intentionally — the score is small and always useful).
	fmt.Fprintf(out, "\n--- score.json ---\n")
	scoreBytes, _ := json.MarshalIndent(score, "", "  ")
	fmt.Fprintln(out, string(scoreBytes))

	return nil
}

// runDocgenTier1 executes the Tier 1 single-page path (<120 s).
func runDocgenTier1(cmd *cobra.Command, group, seedEntity, pageID, outputDir string) error {
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
