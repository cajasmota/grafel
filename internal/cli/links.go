package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/links"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newLinksCmd is the hidden top-level entry point used by hooks. It
// exposes a single sub-command, `pass <group>`, that runs the three
// cross-repo link passes against the per-repo graph.json files of every
// repo in the group.
func newLinksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "links",
		Short:  "Run cross-repo link passes",
		Hidden: true,
	}
	cmd.AddCommand(newLinksPassCmd())
	return cmd
}

func newLinksPassCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pass <group>",
		Short: "Run P1/P2/P3 link passes for a group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("supply a group name")
			}
			return runLinksForGroup(cmd, args[0])
		},
	}
}

// runLinksForGroup loads the group config, builds a synthetic graphs dir
// where each repo's path resolves to its per-repo .archigraph/graph.json,
// then invokes links.RunAllPasses. The graphs-dir convention used by
// loadAllGraphs is "any directory containing one or more graph.json
// files at any depth"; we pass the group state dir and write symlinks
// pointing at each repo's graph.json. To keep this hermetic we instead
// build a temporary scratch dir.
func runLinksForGroup(cmd *cobra.Command, group string) error {
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

	graphsDir, cleanup, err := stageGraphsDir(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	res, err := links.RunAllPasses(group, graphsDir, "")
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "links: group=%s\n", res.Group)
	for _, r := range res.Results {
		fmt.Fprintf(out, "  %-7s links=%-4d candidates=%-4d skipped=%d\n",
			r.Pass, r.LinksAdded, r.Candidates, r.Skipped)
	}
	fmt.Fprintf(out, "  output: %s\n", res.OutLinks)
	return nil
}

// stageGraphsDir does NOT copy graphs; it returns a directory containing
// one symlink per repo to the repo's actual <repo>/.archigraph/graph.json.
// This keeps the on-disk layout that loadAllGraphs expects (one
// graph.json per nested dir) without duplicating bytes.
func stageGraphsDir(cfg *registry.GroupConfig) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "archigraph-links-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	for _, r := range cfg.Repos {
		src := filepath.Join(r.Path, ".archigraph", "graph.json")
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dstDir := filepath.Join(tmp, r.Slug)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
		dst := filepath.Join(dstDir, "graph.json")
		if err := os.Symlink(src, dst); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return tmp, cleanup, nil
}
