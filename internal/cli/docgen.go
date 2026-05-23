// Package cli — `archigraph docgen` subcommand (Tier 0 fast-path, issue #1760).
//
// Tier 0 produces ONE markdown section for ONE seed entity with a <30 s
// feedback loop. It is designed for rapid prompt-quality iteration:
//
//	archigraph docgen --tier=0 \
//	  --group=mygroup \
//	  --seed-entity=abc123def456 \
//	  --section=capabilities
//
// Output:  ~/.archigraph/docs/<group>/.tier0-<RFC3339>/<entity-id>-<section>.md
//          ~/.archigraph/docs/<group>/.tier0-<RFC3339>/score.json
//
// Full-group rendering (Tier 1–4) is not yet wired; this file establishes
// the CLI entry point and Tier 0 dispatch. Tier 1–4 flags are stubbed so
// help text is stable.
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

TIER 1–4 — full multi-page rendering:
  Not yet implemented. Use the /generate-docs skill in Claude Code for
  full-group documentation generation.

Available sections (--section):
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
			default:
				return fmt.Errorf("--tier=%d is not yet implemented; only --tier=0 is available in this release", tier)
			}
		},
	}

	cmd.Flags().IntVar(&tier, "tier", 0,
		"docgen tier: 0 = single section snippet (<30 s); 1–4 = full group rendering (not yet implemented)")
	cmd.Flags().StringVar(&group, "group", "",
		"group name (defaults to sole registered group)")
	cmd.Flags().StringVar(&seedEntity, "seed-entity", "",
		"entity ID (or prefix) to render the section for (required for --tier=0)")
	cmd.Flags().StringVar(&section, "section", "",
		fmt.Sprintf("section type to render (required for --tier=0); one of: %s", strings.Join(docgen.KnownSections, ", ")))
	cmd.Flags().StringVar(&outputDir, "output-dir", "",
		"override output directory (default: ~/.archigraph/docs/<group>/.tier0-<timestamp>/)")
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
