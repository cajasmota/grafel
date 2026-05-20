package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/registry"
)

func newCleanupCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "cleanup [--dry-run]",
		Short: "Clean up orphaned registry entries",
		Long: `Scan registry.json and remove entries for groups whose fleet config
files no longer exist at the target path.

Use --dry-run to list orphaned entries without removing them.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCleanup(cmd.OutOrStdout(), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", true,
		"list orphaned entries without removing them (default: true)")
	return cmd
}

func runCleanup(w io.Writer, dryRun bool) error {
	reg, err := registry.Load()
	if err != nil {
		return err
	}

	// Find orphaned entries (config files that don't exist).
	var orphaned []registry.GroupRef
	for _, g := range reg.Groups {
		_, err := os.Stat(g.ConfigPath)
		if err != nil && os.IsNotExist(err) {
			orphaned = append(orphaned, g)
		}
	}

	if len(orphaned) == 0 {
		fmt.Fprintln(w, "✓ No orphaned registry entries found")
		return nil
	}

	fmt.Fprintf(w, "Found %d orphaned entries:\n", len(orphaned))
	for _, g := range orphaned {
		fmt.Fprintf(w, "  - %s (config: %s)\n", g.Name, g.ConfigPath)
	}

	if dryRun {
		fmt.Fprintln(w, "\nRun 'archigraph cleanup' (without --dry-run) to remove these entries")
		return nil
	}

	// Remove orphaned entries from the registry.
	var cleaned []registry.GroupRef
	for _, g := range reg.Groups {
		_, err := os.Stat(g.ConfigPath)
		if err == nil || !os.IsNotExist(err) {
			cleaned = append(cleaned, g)
		}
	}

	reg.Groups = cleaned
	if err := registry.Save(reg); err != nil {
		return err
	}

	fmt.Fprintf(w, "\n✓ Removed %d orphaned entries\n", len(orphaned))
	return nil
}
