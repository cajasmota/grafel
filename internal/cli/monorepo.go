package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
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
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "remove [group] [path]",
		Short: "Deselect modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("usage: monorepo remove <group> <path>")
			}
			modules := splitCSV(modulesFlag)
			result, err := runMonorepoMutate(args[0], args[1], func(r *registry.Repo, _ detect.Monorepo) {
				r.Modules = without(r.Modules, modules)
			})
			if err != nil {
				return err
			}

			// Notify daemon so it rebuilds with the updated module list.
			// A missing daemon is non-fatal: the next watcher-driven rebuild
			// will pick up the new module list from the persisted fleet config.
			notifyErr := monorepoNotifyDaemon(result.group, result.slug)

			if jsonOut {
				type monorepoRemoveResult struct {
					Success       bool     `json:"success"`
					Group         string   `json:"group"`
					Slug          string   `json:"slug"`
					Modules       []string `json:"modules"`
					DaemonQueued  bool     `json:"daemon_queued"`
					DaemonWarning string   `json:"daemon_warning,omitempty"`
				}
				r := monorepoRemoveResult{
					Success:      true,
					Group:        result.group,
					Slug:         result.slug,
					Modules:      result.modules,
					DaemonQueued: notifyErr == nil,
				}
				if notifyErr != nil {
					r.DaemonWarning = notifyErr.Error()
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s modules: %s (kind=%s)\n",
				result.group, result.slug, strings.Join(result.modules, ","), result.kind)
			if notifyErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: daemon not notified: %v\n", notifyErr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&modulesFlag, "modules", "", "comma-separated package paths to disable")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON result")
	return cmd
}

// monorepoMutateSummary is like monorepoMutate but returns structured info
// instead of printing directly, so callers can choose their output format.
type monorepoMutateSummary struct {
	group   string
	slug    string
	modules []string
	kind    detect.MonorepoKind
}

func runMonorepoMutate(group, repoPath string, fn func(*registry.Repo, detect.Monorepo)) (monorepoMutateSummary, error) {
	groups, err := registry.Groups()
	if err != nil {
		return monorepoMutateSummary{}, err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return monorepoMutateSummary{}, fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return monorepoMutateSummary{}, err
	}
	var repo *registry.Repo
	for i := range cfg.Repos {
		if cfg.Repos[i].Path == repoPath || cfg.Repos[i].Slug == repoPath {
			repo = &cfg.Repos[i]
			break
		}
	}
	if repo == nil {
		return monorepoMutateSummary{}, fmt.Errorf("repo not in group %s: %s", group, repoPath)
	}
	detected, _ := detect.DetectMonorepo(repo.Path)
	fn(repo, detected)
	if err := registry.SaveGroupConfig(ref.ConfigPath, cfg); err != nil {
		return monorepoMutateSummary{}, err
	}
	return monorepoMutateSummary{
		group:   group,
		slug:    repo.Slug,
		modules: repo.Modules,
		kind:    detected.Kind,
	}, nil
}

// monorepoNotifyDaemon asks the daemon to queue a rebuild for the repo so
// the updated module list takes effect. Returns nil on success; a non-nil
// error means the daemon was not notified (it may not be running).
func monorepoNotifyDaemon(group, slug string) error {
	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return nil // not running is fine; next watcher event will pick it up
		}
		return err
	}
	defer c.Close()
	_, err = c.Rebuild(proto.RebuildArgs{Group: group, Slug: slug})
	return err
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
