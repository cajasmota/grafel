package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/registry"
)

// rebuild and reset both invoke the indexer; reset additionally deletes
// the on-disk .archigraph/ before doing so. remerge is a deprecated
// alias that prints a warning and forwards to rebuild.

func newRebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild [group] [slug]",
		Short: "Force AST rebuild (no cache)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRebuild(cmd, args, false)
		},
	}
}

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [group] [slug]",
		Short: "Wipe .archigraph/ and rebuild",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRebuild(cmd, args, true)
		},
	}
}

func newRemergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "remerge [group]",
		Short:  "DEPRECATED: re-run cross-repo link passes",
		Hidden: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: `archigraph remerge` is deprecated; use `archigraph rebuild [group]`.")
			return runRebuild(cmd, args, false)
		},
	}
}

func runRebuild(cmd *cobra.Command, args []string, wipe bool) error {
	if activeHooks.RunIndex == nil {
		return errors.New("index handler not wired")
	}
	if len(args) == 0 {
		return errors.New("supply [group] (and optional [slug])")
	}
	groupName := args[0]
	slug := ""
	if len(args) > 1 {
		slug = args[1]
	}
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == groupName {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", groupName)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return err
	}
	for _, r := range cfg.Repos {
		if slug != "" && r.Slug != slug {
			continue
		}
		if wipe {
			_ = os.RemoveAll(filepath.Join(r.Path, ".archigraph"))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "indexing %s (%s)\n", r.Slug, r.Path)
		if err := activeHooks.RunIndex([]string{r.Path}); err != nil {
			return err
		}
	}
	return nil
}
