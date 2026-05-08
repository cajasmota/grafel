package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newMonorepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monorepo",
		Short: "Manage indexed monorepo modules",
	}
	cmd.AddCommand(
		newMonorepoAddCmd(),
		newMonorepoRemoveCmd(),
		newMonorepoListCmd(),
	)
	return cmd
}

func newMonorepoAddCmd() *cobra.Command {
	var modulesFlag string
	cmd := &cobra.Command{
		Use:   "add [group] [path]",
		Short: "Pick which packages get indexed",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("usage: monorepo add <group> <path>")
			}
			modules := splitCSV(modulesFlag)
			return monorepoMutate(cmd, args[0], args[1], func(r *registry.Repo, detected detect.Monorepo) {
				if len(modules) == 0 {
					modules = detected.Packages
				}
				r.Modules = uniqueAdd(r.Modules, modules)
			})
		},
	}
	cmd.Flags().StringVar(&modulesFlag, "modules", "", "comma-separated package paths to enable")
	return cmd
}

func newMonorepoRemoveCmd() *cobra.Command {
	var modulesFlag string
	cmd := &cobra.Command{
		Use:   "remove [group] [path]",
		Short: "Deselect modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("usage: monorepo remove <group> <path>")
			}
			modules := splitCSV(modulesFlag)
			return monorepoMutate(cmd, args[0], args[1], func(r *registry.Repo, _ detect.Monorepo) {
				r.Modules = without(r.Modules, modules)
			})
		},
	}
	cmd.Flags().StringVar(&modulesFlag, "modules", "", "comma-separated package paths to disable")
	return cmd
}

func newMonorepoListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List indexed monorepo modules across all groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			groups, err := registry.Groups()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, g := range groups {
				cfg, err := registry.LoadGroupConfig(g.ConfigPath)
				if err != nil {
					continue
				}
				for _, r := range cfg.Repos {
					if len(r.Modules) == 0 {
						continue
					}
					fmt.Fprintf(out, "%s/%s  %s\n", g.Name, r.Slug, strings.Join(r.Modules, ","))
				}
			}
			return nil
		},
	}
}

func monorepoMutate(cmd *cobra.Command, group, repoPath string, fn func(*registry.Repo, detect.Monorepo)) error {
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return err
	}
	var repo *registry.Repo
	for i := range cfg.Repos {
		if cfg.Repos[i].Path == repoPath || cfg.Repos[i].Slug == repoPath {
			repo = &cfg.Repos[i]
			break
		}
	}
	if repo == nil {
		return fmt.Errorf("repo not in group %s: %s", group, repoPath)
	}
	detected, _ := detect.DetectMonorepo(repo.Path)
	fn(repo, detected)
	if err := registry.SaveGroupConfig(ref.ConfigPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s/%s modules: %s (kind=%s)\n",
		group, repo.Slug, strings.Join(repo.Modules, ","), detected.Kind)
	return nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniqueAdd(base, add []string) []string {
	seen := map[string]struct{}{}
	for _, b := range base {
		seen[b] = struct{}{}
	}
	out := append([]string(nil), base...)
	for _, a := range add {
		if _, ok := seen[a]; !ok {
			out = append(out, a)
			seen[a] = struct{}{}
		}
	}
	return out
}

func without(base, drop []string) []string {
	dropSet := map[string]struct{}{}
	for _, d := range drop {
		dropSet[d] = struct{}{}
	}
	out := base[:0]
	for _, b := range base {
		if _, ok := dropSet[b]; !ok {
			out = append(out, b)
		}
	}
	return out
}
