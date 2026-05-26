package cli

// personas.go wires the `archigraph personas` subcommand family.
//
// Currently exposes one verb:
//
//	archigraph personas render --target <target> [--output <dir>] [--personas-dir <dir>]
//
// Targets: claude-code, windsurf, cursor, codex  (issue #2476).

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/personas/renderer"
)

// newPersonasCmd returns the `archigraph personas` parent command.
func newPersonasCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "personas",
		Short: "Manage archigraph consultant personas",
		Long: `Commands for working with the archigraph consultant personas.

Personas live in skills/archigraph-consult/personas/*.md as canonical
Claude Code subagent files. The render subcommand emits platform-specific
wrappers from those canonical files without modifying the originals.`,
	}
	root.AddCommand(newPersonasRenderCmd())
	return root
}

// newPersonasRenderCmd returns the `archigraph personas render` subcommand.
func newPersonasRenderCmd() *cobra.Command {
	var (
		target      string
		outputDir   string
		personasDir string
		quiet       bool
	)

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Emit platform-specific persona wrappers from canonical files",
		Long: `render reads canonical Claude Code persona files and emits
platform-specific wrappers into the output directory.

Supported targets:

  claude-code  Copy as-is (canonical format, no transformation)
  windsurf     .windsurf/workflows/<name>.md with simplified frontmatter
  cursor       .cursor/commands/<name>.md with simplified frontmatter
  codex        <name>.codex.md stub with TODO marker (format TBD)

The command is idempotent: re-running overwrites previous output.

Examples:

  # Render Windsurf workflows into the current project root
  archigraph personas render --target windsurf --output .

  # Render Cursor commands into a specific directory
  archigraph personas render --target cursor --output ~/my-project

  # Use an explicit personas source directory
  archigraph personas render --target windsurf \
      --personas-dir /path/to/archigraph/skills/archigraph-consult/personas \
      --output .`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPersonasRender(cmd, target, outputDir, personasDir, quiet)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "rendering target: claude-code, windsurf, cursor, codex (required)")
	cmd.Flags().StringVar(&outputDir, "output", ".", "directory to write rendered files into")
	cmd.Flags().StringVar(&personasDir, "personas-dir", "", "path to canonical persona files (auto-discovered if not set)")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress per-file output; only print errors")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func runPersonasRender(cmd *cobra.Command, targetStr, outputDir, personasDir string, quiet bool) error {
	t, err := renderer.ParseTarget(targetStr)
	if err != nil {
		return err
	}

	// Resolve personas source directory.
	if personasDir == "" {
		// Walk upward from cwd.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("personas render: getting working directory: %w", err)
		}
		personasDir, err = renderer.DiscoverPersonasDir(cwd, 6)
		if err != nil {
			return fmt.Errorf("personas render: %w\n\nHint: run from the archigraph repo root, or pass --personas-dir explicitly", err)
		}
	}

	// Resolve output directory.
	if outputDir == "" || outputDir == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("personas render: getting working directory: %w", err)
		}
		outputDir = cwd
	}

	if !quiet {
		fmt.Fprintf(cmd.OutOrStdout(), "archigraph personas render\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  source : %s\n", personasDir)
		fmt.Fprintf(cmd.OutOrStdout(), "  target : %s\n", targetStr)
		fmt.Fprintf(cmd.OutOrStdout(), "  output : %s\n\n", outputDir)
	}

	results, err := renderer.Render(personasDir, outputDir, t)
	if err != nil {
		// Render returns the first error but still processes all files;
		// report it but don't suppress the success list.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
	}

	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Name)
		if !quiet {
			fmt.Fprintf(cmd.OutOrStdout(), "  wrote  %s\n", r.Dest)
		}
	}

	if !quiet {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d persona(s) rendered: %s\n",
			len(results), strings.Join(names, ", "))
	}
	return nil
}
