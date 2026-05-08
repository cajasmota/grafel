package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newInstallCmd() *cobra.Command {
	var groupFlag string
	cmd := &cobra.Command{
		Use:   "install [config] | --group <name>",
		Short: "Apply a group config (hooks, watchers, MCP)",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var (
				cfgPath string
				group   string
			)
			switch {
			case groupFlag != "":
				groups, err := registry.Groups()
				if err != nil {
					return err
				}
				for _, g := range groups {
					if g.Name == groupFlag {
						cfgPath = g.ConfigPath
						group = g.Name
						break
					}
				}
				if cfgPath == "" {
					return fmt.Errorf("group not registered: %s", groupFlag)
				}
			case len(args) == 1:
				cfgPath = args[0]
			default:
				return errors.New("supply a config path or --group <name>")
			}
			cfg, err := registry.LoadGroupConfig(cfgPath)
			if err != nil {
				return err
			}
			if group == "" {
				group = cfg.Name
			}
			bin, _ := os.Executable()
			res, err := install.Apply(install.Options{
				Group:   group,
				Config:  cfg,
				BinPath: bin,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "installed group %q\n", group)
			fmt.Fprintf(out, "  config:    %s\n", res.GroupConfigPath)
			for _, p := range res.HooksInstalled {
				fmt.Fprintf(out, "  hooks:     %s\n", p)
			}
			for _, p := range res.WatcherUnits {
				fmt.Fprintf(out, "  watcher:   %s\n", p)
			}
			for _, p := range res.MCPSettings {
				fmt.Fprintf(out, "  mcp:       %s\n", p)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&groupFlag, "group", "", "install a previously-registered group by name")
	return cmd
}
