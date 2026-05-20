package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newDeleteCmd() *cobra.Command {
	var (
		force      bool
		keepCaches bool
		jsonOut    bool
	)

	cmd := &cobra.Command{
		Use:   "delete <group>",
		Short: "Delete an entire group and all its repos",
		Long: `Delete tears down every repo in a group and removes the group from the
registry entirely.

For each repo: the watcher is stopped, the git hook block is removed, and
the per-repo .archigraph/ cache is deleted (use --keep-caches to skip cache
deletion). The fleet config file and the per-group state directory are
deleted last.

This is a destructive operation. In interactive mode you must TYPE the group
name to confirm. Use --force to skip the confirmation (e.g. in CI), or
--json to produce machine-readable output (also skips the prompt).

The daemon must be running.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeleteImpl(cmd, args[0], force, keepCaches, jsonOut, "")
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"skip the confirmation prompt (dangerous)")
	cmd.Flags().BoolVar(&keepCaches, "keep-caches", false,
		"leave per-repo .archigraph/ directories on disk")
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit machine-readable JSON result")
	return cmd
}

// runDelete is the public wrapper used by runRemoveImpl when the last repo is
// interactively confirmed for deletion.
func runDelete(cmd *cobra.Command, group string, force, keepCaches, jsonOut bool) error {
	return runDeleteImpl(cmd, group, force, keepCaches, jsonOut, "")
}

// runDeleteImpl implements delete with an injectable socketPath. An empty
// socketPath causes it to call client.Dial (the real daemon). Tests pass a
// stub socket so no real daemon is required.
func runDeleteImpl(cmd *cobra.Command, group string, force, keepCaches, jsonOut bool, socketPath string) error {
	out := cmd.OutOrStdout()

	// Validate group exists before dialling.
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

	// Load the fleet so we can report member slugs in the result.
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		// Config file missing is OK — the daemon will handle the empty case.
		cfg = nil
	}

	// High-friction confirmation: user must TYPE the group name unless --force/--json.
	if !force && !jsonOut {
		var typed string
		if err := huh.NewInput().
			Title(fmt.Sprintf("Type %q to confirm deletion of this group and ALL its repos:", group)).
			Description("This action is irreversible. All per-repo caches will be deleted.").
			Value(&typed).
			Run(); err != nil {
			return err
		}
		if strings.TrimSpace(typed) != group {
			fmt.Fprintf(out, "confirmation mismatch — expected %q, got %q\naborted\n", group, strings.TrimSpace(typed))
			return nil
		}
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

	reply, err := c.DeleteGroup(proto.DeleteGroupArgs{
		Group:      group,
		KeepCaches: keepCaches,
	})
	if err != nil {
		return fmt.Errorf("daemon delete-group: %w", err)
	}

	elapsed := time.Since(start)

	if jsonOut {
		type deleteResult struct {
			Success      bool     `json:"success"`
			Deleted      string   `json:"deleted"`
			RemovedRepos []string `json:"removed_repos"`
			FreedBytes   int64    `json:"freed_bytes"`
			DurationMS   int64    `json:"duration_ms"`
		}
		r := deleteResult{
			Success:      true,
			Deleted:      group,
			RemovedRepos: reply.RemovedRepos,
			FreedBytes:   reply.FreedBytes,
			DurationMS:   elapsed.Milliseconds(),
		}
		if r.RemovedRepos == nil {
			r.RemovedRepos = []string{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}

	repos := reply.RemovedRepos
	if len(repos) == 0 && cfg != nil {
		for _, r := range cfg.Repos {
			repos = append(repos, r.Slug)
		}
	}
	fmt.Fprintf(out, "deleted group %q", group)
	if len(repos) > 0 {
		fmt.Fprintf(out, " (%d repos: %s)", len(repos), strings.Join(repos, ", "))
	}
	if reply.FreedBytes > 0 {
		fmt.Fprintf(out, ", freed %s", fmtBytes(reply.FreedBytes))
	}
	fmt.Fprintln(out)
	return nil
}
