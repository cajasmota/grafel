package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newRemoveCmd() *cobra.Command {
	var (
		keepCache bool
		force     bool
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "remove <group> <slug>",
		Short: "Remove a single repo from a group",
		Long: `Remove unregisters a repository from an archigraph group.

The watcher is stopped, the git hook block is removed from the repo's
.git/hooks/* files, and the per-repo .archigraph/ cache is deleted (use
--keep-cache to leave it on disk). The repo entry is removed from the
group's fleet config and the change is persisted.

When this is the last repo in the group, the command prints a warning and
asks whether to delete the whole group. In --force or --json mode the
command refuses with an error instead (preventing accidental orphaned groups).

The daemon must be running. Use --json for machine-readable output.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoveImpl(cmd, args[0], args[1], keepCache, force, jsonOut, "")
		},
	}

	cmd.Flags().BoolVar(&keepCache, "keep-cache", false,
		"leave <repo>/.archigraph/ on disk (do not delete the cache)")
	cmd.Flags().BoolVar(&force, "force", false,
		"skip confirmation prompt")
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit machine-readable JSON result")
	return cmd
}

func runRemove(cmd *cobra.Command, group, slug string, keepCache, force, jsonOut bool) error {
	return runRemoveImpl(cmd, group, slug, keepCache, force, jsonOut, "")
}

// runRemoveImpl implements remove with an injectable socketPath. An empty
// socketPath causes it to call client.Dial (the real daemon). Tests pass a
// stub socket so no real daemon is required.
func runRemoveImpl(cmd *cobra.Command, group, slug string, keepCache, force, jsonOut bool, socketPath string) error {
	out := cmd.OutOrStdout()

	// Validate the group + slug exist before dialling the daemon.
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
	found := false
	for _, r := range cfg.Repos {
		if r.Slug == slug {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("repo %q not found in group %s", slug, group)
	}

	// Interactive confirmation (skip in --force or --json mode).
	if !force && !jsonOut {
		var confirmed bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Remove repo %q from group %q?", slug, group)).
			Description("The watcher will be stopped, the git hook block removed,\nand the per-repo cache deleted (unless --keep-cache).").
			Value(&confirmed).
			Run(); err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}

	// Check if this is the last repo.
	isLastRepo := len(cfg.Repos) == 1
	if isLastRepo && (force || jsonOut) {
		return fmt.Errorf(
			"group %q has only one repo (%s); use 'archigraph delete %s' to remove the whole group",
			group, slug, group,
		)
	}

	start := time.Now()

	// Dial daemon and send RPC.
	var c *client.Client
	if socketPath != "" {
		c, err = client.DialPath(socketPath)
	} else {
		c, err = client.Dial()
	}
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	defer c.Close()

	reply, err := c.RemoveRepo(proto.RemoveRepoArgs{
		Group:     group,
		Slug:      slug,
		KeepCache: keepCache,
	})
	if err != nil {
		return fmt.Errorf("daemon remove-repo: %w", err)
	}

	elapsed := time.Since(start)

	if jsonOut {
		type removeResult struct {
			Success bool `json:"success"`
			Removed struct {
				Group string `json:"group"`
				Slug  string `json:"slug"`
			} `json:"removed"`
			FreedBytes int64 `json:"freed_bytes"`
			DurationMS int64 `json:"duration_ms"`
		}
		r := removeResult{
			Success:    true,
			FreedBytes: reply.FreedBytes,
			DurationMS: elapsed.Milliseconds(),
		}
		r.Removed.Group = group
		r.Removed.Slug = slug
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}

	fmt.Fprintf(out, "removed %s/%s", group, slug)
	if reply.FreedBytes > 0 {
		fmt.Fprintf(out, " (freed %s)", fmtBytes(reply.FreedBytes))
	}
	fmt.Fprintln(out)

	// Prompt for group deletion if this was the last repo (interactive only).
	if isLastRepo {
		var deleteGroup bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Group %q is now empty. Delete the whole group?", group)).
			Value(&deleteGroup).
			Run(); err != nil {
			return err
		}
		if deleteGroup {
			return runDeleteImpl(cmd, group, false, false, jsonOut, socketPath)
		}
	}

	return nil
}

// fmtBytes formats a byte count as a human-readable string.
func fmtBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GiB", float64(b)/gb)
	case b >= mb:
		return fmt.Sprintf("%.1f MiB", float64(b)/mb)
	case b >= kb:
		return fmt.Sprintf("%.1f KiB", float64(b)/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
