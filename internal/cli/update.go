package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/hooks"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newUpdateCmd() *cobra.Command {
	var (
		refreshLite bool
		pinVersion  string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update archigraph and reapply hooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if pinVersion != "" {
				fmt.Fprintf(out, "pin requested: %s (handled by install.sh self-update)\n", pinVersion)
			}
			if refreshLite {
				fmt.Fprintln(out, "refreshing rules-lite (no-op in current build)")
			}
			bin, _ := os.Executable()
			groups, err := registry.Groups()
			if err != nil {
				return err
			}
			for _, g := range groups {
				cfg, err := registry.LoadGroupConfig(g.ConfigPath)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", g.Name, err)
					continue
				}
				for _, r := range cfg.Repos {
					if cfg.Features.GitHooks {
						if err := hooks.Install(r.Path, bin); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "hooks %s: %v\n", r.Slug, err)
						}
					}
				}
				// Re-run install to refresh watcher units + MCP entries.
				_, _ = install.Apply(install.Options{
					Group:   g.Name,
					Config:  cfg,
					BinPath: bin,
				})
			}
			// MCP entries should always reflect the live binary path.
			regPath, _ := registry.RegistryPath()
			_, _ = mcpreg.Register(mcpreg.ClaudeCode, bin, regPath)
			_, _ = mcpreg.Register(mcpreg.Windsurf, bin, regPath)
			fmt.Fprintln(out, "update complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&refreshLite, "refresh-rules-lite", false, "refresh the lite rule-pack")
	cmd.Flags().StringVar(&pinVersion, "pin-version", "", "pin archigraph to a specific version on self-update")
	return cmd
}
