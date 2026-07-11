package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// newIndexCmd is the thin RPC client for one-shot indexing. Per ADR-0017
// there is no in-process fallback — if the daemon isn't running the
// command returns the canonical error. The flag surface mirrors what
// the old standalone `grafel index` accepted so muscle memory and
// scripts keep working.
func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index <repo>",
		Short: "Index a repository (daemon RPC)",
		Long: `Index a repository via the daemon RPC.

  --ref <ref>  operate on a specific git ref (branch/tag).
               Use @current for the active HEAD (default).
               @all is refused (index is a destructive write operation).`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndexClient(cmd, args)
		},
	}
	return cmd
}

func runIndexClient(cmd *cobra.Command, argv []string) error {
	// Reorder argv so flags appear before positional arguments.
	// This allows both: grafel index <repo> --export-fb
	//              and: grafel index --export-fb <repo>
	var flags []string
	var positionals []string
	for _, arg := range argv {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			positionals = append(positionals, arg)
		}
	}
	reorderedArgv := append(flags, positionals...)

	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	out := fs.String("out", "", "output path for graph.json (default: <repo>/.grafel/graph.json)")
	repoTag := fs.String("repo-tag", "", "repository tag stored on entities")
	skip := fs.String("skip-pass", "", "comma-separated list of passes to skip")
	pretty := fs.Bool("pretty", false, "emit indented JSON")
	jsonStats := fs.Bool("json-stats", false, "print per-run statistics as JSON")
	repair := fs.Bool("enable-repair-candidates", false, "emit ADR-0015 repair candidates")
	repairApply := fs.Bool("enable-repair-apply", false, "apply allowlisted repairs before classification")
	exportFB := fs.Bool("export-fb", false, "[deprecated] graph.fb is now written by default; this flag is a no-op (ADR-0016 flip-day)")
	exportJSON := fs.Bool("export-json", false, "also write graph.json alongside graph.fb (default: FB-only, ADR-0016 flip-day)")
	printSkipped := fs.Bool("print-skipped", false, "print each directory skipped at walk-time with the matching rule")
	quiet := fs.Bool("quiet", false, "suppress progress output; print only the final summary line")
	jsonProgress := fs.Bool("json-progress", false, "emit one JSON event per line (for scripting; implies --quiet for non-JSON output)")
	refFlag := fs.String("ref", "", "operate on a specific git ref; @all is refused (index is a destructive write). Use @current for active HEAD (default).")
	async := fs.Bool("async", false, "enqueue a debounced/coalesced reindex on the daemon and return immediately without waiting for it to finish (used by git hooks; #3366)")
	noWait := fs.Bool("no-wait", false, "alias for --async")
	if err := fs.Parse(reorderedArgv); err != nil {
		return err
	}
	asyncMode := *async || *noWait

	// Validate --ref: @all is refused for index (destructive).
	_, _, refErr := resolveRef(*refFlag, false /* @all NOT ok */)
	if refErr != nil {
		return refErr
	}
	// Note: daemon-side ref routing for index is tracked in #2220.
	// The validated ref is accepted here so scripts can be written against
	// the final interface before the daemon wiring lands.
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

	repoPath := fs.Arg(0)
	slug := filepath.Base(repoPath)
	w := cmd.OutOrStdout()

	var skipPasses []string
	if *skip != "" {
		skipPasses = []string{*skip}
	}

	indexArgs := proto.IndexArgs{
		RepoPath:     repoPath,
		OutPath:      *out,
		RepoTag:      *repoTag,
		SkipPasses:   skipPasses,
		Pretty:       *pretty,
		JSONStats:    *jsonStats,
		Repair:       *repair,
		RepairApply:  *repairApply,
		ExportFB:     *exportFB, // deprecated no-op; kept for back-compat
		ExportJSON:   *exportJSON,
		PrintSkipped: *printSkipped,
		Async:        asyncMode,
	}

	if asyncMode {
		// Async enqueue path (#3366): the daemon coalesces this onto its
		// debounced scheduler and ACKs immediately — we do NOT wait for the
		// reindex to finish and emit no heartbeat. Used by git hooks so git
		// writes are never blocked. If the daemon is down the RPC errors,
		// which the hook swallows via `|| true`.
		if _, err := c.Index(indexArgs); err != nil {
			return err
		}
		return nil
	}

	if *quiet || *jsonProgress {
		// Quiet or scripting mode: run synchronously, no heartbeat.
		reply, err := c.Index(indexArgs)
		if err != nil {
			return err
		}
		if *jsonProgress {
			// Emit a simple done event.
			emitJSONProgressState(w, "", proto.RepoProgressState{
				Slug:  slug,
				Path:  reply.RepoPath,
				Phase: proto.PhaseCompleted,
				Index: 1,
				Total: 1,
			})
		} else {
			fmt.Fprintf(w, "indexed %s -> %s\n", reply.RepoPath, reply.GraphPath)
			if reply.StatsJSON != "" {
				fmt.Fprintln(w, reply.StatsJSON)
			}
		}
		return nil
	}

	// Default: show heartbeat while index is running.
	fmt.Fprintf(w, "Indexing '%s'...\n", slug)

	type indexResult struct {
		reply proto.IndexReply
		err   error
	}
	resultCh := make(chan indexResult, 1)
	start := time.Now()
	go func() {
		reply, err := c.Index(indexArgs)
		resultCh <- indexResult{reply: reply, err: err}
	}()

	const heartbeatInterval = 5 * time.Second
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	var res indexResult
	for {
		select {
		case res = <-resultCh:
			goto done
		case <-ticker.C:
			fmt.Fprintf(w, "  ... indexing %s (%s elapsed)\n", slug, fmtDuration(time.Since(start)))
		}
	}
done:
	if res.err != nil {
		return res.err
	}
	fmt.Fprintf(w, "indexed %s -> %s\n", res.reply.RepoPath, res.reply.GraphPath)
	if res.reply.StatsJSON != "" {
		fmt.Fprintln(w, res.reply.StatsJSON)
	}
	return nil
}

// newMCPCmd is retained as a stub that returns a helpful error pointing
// users at the daemon-served MCP endpoint (Phase D). It is intentionally
// not hidden so `grafel mcp serve` still prints something useful
// rather than "unknown command".
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "(removed) MCP serving moved into the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errors.New(
				"`grafel mcp serve` was removed in ADR-0017. " +
					"The daemon registers itself as the MCP endpoint during " +
					"`grafel install` (Phase C+D). " +
					"For now run `grafel start` and use the socket-backed proxy.",
			)
		},
	}
}

// errDaemonNotRunning is the user-facing message every thin-client
// subcommand returns when the daemon isn't reachable. Defined here so
// every callsite uses the identical wording (see ADR-0017).
var errDaemonNotRunning = errors.New(
	"daemon not running; run 'grafel start' or reinstall via 'grafel install'",
)

// newDaemonCmd exposes the long-running daemon mode. It is hidden from
// the primary surface — users normally reach it through `grafel start`,
// which forks the binary in this mode with stdio detached.
//
// ADR-0024 (serve/engine split, Phase 1): `daemon` is the back-compat name.
// Its RunE calls the SAME path as `grafel serve` (RunServe) so an existing
// OS unit (launchd/systemd/SCM) that still execs `grafel daemon` becomes a
// serve process transparently — no client-visible change.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "daemon",
		Short:              "Run the grafel daemon (used by start/launchd/systemd)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunServe != nil {
				return activeHooks.RunServe(args)
			}
			// Fallback for any host that has not yet wired RunServe.
			if activeHooks.RunDaemon == nil {
				return errors.New("daemon entrypoint not wired")
			}
			return activeHooks.RunDaemon(args)
		},
	}
	return cmd
}

// newServeCmd exposes the serve plane (ADR-0024 Phase 1): the MCP socket,
// the dashboard, and graph_cache mmap reads. Hidden — not part of the
// public surface yet; `grafel daemon` remains the documented/back-compat
// entrypoint until `grafel install`/OS units are retargeted in a later PR.
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "serve",
		Short:              "Run the grafel serve plane (MCP socket + dashboard)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunServe == nil {
				return errors.New("serve entrypoint not wired")
			}
			return activeHooks.RunServe(args)
		},
	}
	return cmd
}

// newEngineCmd exposes the engine plane (ADR-0024 Phase 1): the scheduler,
// watcher, extraction, and fbwriter. Hidden. In this PR it is not yet
// independently runnable — the real process split (serve spawning/
// supervising an engine child) lands in PR2, epic #5729.
func newEngineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "engine",
		Short:              "Run the grafel engine plane (scheduler/watcher/extraction/fbwriter)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunEngine == nil {
				return errors.New("engine entrypoint not wired")
			}
			return activeHooks.RunEngine(args)
		},
	}
	return cmd
}
