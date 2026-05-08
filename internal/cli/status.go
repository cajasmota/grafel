package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install/watchers"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [group]",
		Short: "Show watcher + index status",
		RunE: func(cmd *cobra.Command, args []string) error {
			filterGroup := ""
			if len(args) == 1 {
				filterGroup = args[0]
			}
			return runStatus(cmd.OutOrStdout(), filterGroup)
		},
	}
}

func runStatus(w io.Writer, filter string) error {
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if filter != "" && g.Name != filter {
			continue
		}
		fmt.Fprintf(w, "Group: %s\n", g.Name)
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			fmt.Fprintf(w, "  (config error: %v)\n", err)
			continue
		}
		for _, r := range cfg.Repos {
			line := fmt.Sprintf("  %-20s  %s", r.Slug, r.Path)
			graph := filepath.Join(r.Path, ".archigraph", "graph.json")
			if fi, err := os.Stat(graph); err == nil {
				age := time.Since(fi.ModTime()).Truncate(time.Second)
				line += fmt.Sprintf("  graph.json: %s ago", age)
			} else {
				line += "  graph.json: (none)"
			}
			u := watchers.Unit{Group: g.Name, Repo: r.Path}
			if up, err := watchers.UnitPath(u); err == nil {
				if _, err := os.Stat(up); err == nil {
					line += "  watcher: installed"
				} else {
					line += "  watcher: (none)"
				}
			}
			fmt.Fprintln(w, line)
		}
	}
	return nil
}
