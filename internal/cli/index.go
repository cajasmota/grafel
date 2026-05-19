package cli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// newIndexCmd is the thin RPC client for one-shot indexing. Per ADR-0017
// there is no in-process fallback — if the daemon isn't running the
// command returns the canonical error. The flag surface mirrors what
// the old standalone `archigraph index` accepted so muscle memory and
// scripts keep working.
func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "index <repo>",
		Short:              "Index a repository (daemon RPC)",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndexClient(cmd, args)
		},
	}
	return cmd
}

func runIndexClient(cmd *cobra.Command, argv []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	out := fs.String("out", "", "output path for graph.json (default: <repo>/.archigraph/graph.json)")
	repoTag := fs.String("repo-tag", "", "repository tag stored on entities")
	skip := fs.String("skip-pass", "", "comma-separated list of passes to skip")
	pretty := fs.Bool("pretty", false, "emit indented JSON")
	jsonStats := fs.Bool("json-stats", false, "print per-run statistics as JSON")
	repair := fs.Bool("enable-repair-candidates", false, "emit ADR-0015 repair candidates")
	repairApply := fs.Bool("enable-repair-apply", false, "apply allowlisted repairs before classification")
	exportFB := fs.Bool("export-fb", false, "[deprecated] graph.fb is now written by default; this flag is a no-op (ADR-0016 flip-day)")
	exportJSON := fs.Bool("export-json", false, "also write graph.json alongside graph.fb (default: FB-only, ADR-0016 flip-day)")
	printSkipped := fs.Bool("print-skipped", false, "print each directory skipped at walk-time with the matching rule")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("missing <repo> argument")
	}
	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	defer c.Close()

	var skipPasses []string
	if *skip != "" {
		skipPasses = []string{*skip}
	}
	reply, err := c.Index(proto.IndexArgs{
		RepoPath:    fs.Arg(0),
		OutPath:     *out,
		RepoTag:     *repoTag,
		SkipPasses:  skipPasses,
		Pretty:      *pretty,
		JSONStats:   *jsonStats,
		Repair:      *repair,
		RepairApply: *repairApply,
		ExportFB:    *exportFB, // deprecated no-op; kept for back-compat
		ExportJSON:  *exportJSON,
		PrintSkipped: *printSkipped,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "indexed %s -> %s\n", reply.RepoPath, reply.GraphPath)
	if reply.StatsJSON != "" {
		fmt.Fprintln(cmd.OutOrStdout(), reply.StatsJSON)
	}
	return nil
}

// newMCPCmd is retained as a stub that returns a helpful error pointing
// users at the daemon-served MCP endpoint (Phase D). It is intentionally
// not hidden so `archigraph mcp serve` still prints something useful
// rather than "unknown command".
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "(removed) MCP serving moved into the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errors.New(
				"`archigraph mcp serve` was removed in ADR-0017. " +
					"The daemon registers itself as the MCP endpoint during " +
					"`archigraph install` (Phase C+D). " +
					"For now run `archigraph start` and use the socket-backed proxy.",
			)
		},
	}
}

// errDaemonNotRunning is the user-facing message every thin-client
// subcommand returns when the daemon isn't reachable. Defined here so
// every callsite uses the identical wording (see ADR-0017).
var errDaemonNotRunning = errors.New(
	"daemon not running; run 'archigraph start' or reinstall via 'archigraph install'",
)

// newDaemonCmd exposes the long-running daemon mode. It is hidden from
// the primary surface — users normally reach it through `archigraph start`,
// which forks the binary in this mode with stdio detached.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "daemon",
		Short:              "Run the archigraph daemon (used by start/launchd/systemd)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunDaemon == nil {
				return errors.New("daemon entrypoint not wired")
			}
			return activeHooks.RunDaemon(args)
		},
	}
	return cmd
}
