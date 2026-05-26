package cli

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

// newBenchCaptureCmd returns the cobra command for `archigraph bench-capture`.
//
// The actual implementation lives in cmd/archigraph/bench_capture.go (package
// main) to keep internal/cli free of the heavy stdlib math + IO imports.
// The command is wired via activeHooks.RunBenchCapture set by cli.Execute().
//
// Surface: `archigraph bench-capture rpc [--log <path>] [--start-offset N] [--end-offset N]`
//
// Closes #2298, #2362, #2363.
func newBenchCaptureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench-capture",
		Short: "Capture daemon-log RPC metrics into JSON (bench skill helper)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if activeHooks.RunBenchCapture == nil {
				return errors.New("bench-capture handler not wired")
			}
			return activeHooks.RunBenchCapture(args)
		},
	}

	// Add subcommands for rpc
	cmd.AddCommand(newBenchCaptureRPCCmd())

	return cmd
}

// newBenchCaptureRPCCmd returns the cobra command for `archigraph bench-capture rpc`.
func newBenchCaptureRPCCmd() *cobra.Command {
	var (
		logPath   string
		startOff  int64
		endOff    int64
	)

	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Capture RPC metrics from daemon log",
		Long: `Reads a daemon log slice between two byte offsets, parses every
[mcp-rpc] tool=<X> elapsed=<N>ms line, aggregates counts + handler durations,
and emits JSON to stdout.

Flags:
  --log <path>           path to daemon log file (default: ~/.archigraph/logs/daemon.log)
  --start-offset <N>     byte offset to start reading from (inclusive, default: 0)
  --end-offset <N>       byte offset to stop reading at (exclusive, default: -1 for EOF)`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunBenchCapture == nil {
				return errors.New("bench-capture handler not wired")
			}
			// Construct argv for the handler: ["rpc", "--log", logPath, "--start-offset", ...]
			argv := []string{"rpc", "--log", logPath, "--start-offset", formatInt64(startOff), "--end-offset", formatInt64(endOff)}
			return activeHooks.RunBenchCapture(argv)
		},
	}

	cmd.Flags().StringVar(&logPath, "log", "~/.archigraph/logs/daemon.log", "path to daemon log file")
	cmd.Flags().Int64Var(&startOff, "start-offset", 0, "byte offset to start reading from (inclusive)")
	cmd.Flags().Int64Var(&endOff, "end-offset", -1, "byte offset to stop reading at (exclusive); -1 = EOF")

	return cmd
}

// formatInt64 converts an int64 to a string for flag passing.
func formatInt64(n int64) string {
	return strconv.FormatInt(n, 10)
}
