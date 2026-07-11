// Package cli implements the grafel command-line surface.
//
// Commands are registered onto a single cobra root in root.go; each
// command lives in its own file in this package. Top-level help is
// trimmed to a small primary surface (install-and-forget); the full
// surface is reachable via `grafel help advanced`.
package cli

import (
	"fmt"
	"os"
)

// Hooks lets package main inject implementations of subcommands that
// must live in cmd/grafel (because they pull in heavy internal
// packages that should not be visible from the CLI surface).
type Hooks struct {
	// RunDaemon runs the long-running daemon (the `grafel daemon`
	// hidden subcommand). It blocks until the daemon exits. Wired from
	// cmd/grafel because the daemon imports the extractor stack.
	//
	// ADR-0024 (serve/engine split, Phase 1): `daemon` is now a back-compat
	// shim over the same path as RunServe, kept so an existing OS unit that
	// still execs `grafel daemon` transparently becomes a serve process.
	RunDaemon    func(argv []string) error
	RunDashboard func(argv []string) error
	RunQuality   func(argv []string) error

	// RunServe runs the serve plane (MCP socket + dashboard + graph_cache
	// mmap reads), and — while the ADR-0024 capability flag is off (the
	// default) — the engine plane in-process too, identically to RunDaemon.
	// It is the `grafel serve` hidden subcommand's entrypoint and is also
	// what the back-compat `grafel daemon` shim calls. Wired from
	// cmd/grafel for the same reason as RunDaemon.
	RunServe func(argv []string) error

	// RunEngine runs the engine plane (scheduler + watcher + extraction +
	// fbwriter) as the `grafel engine` hidden subcommand's entrypoint. In
	// this PR (ADR-0024 Phase 1: entrypoint/config carve only) it is not
	// yet independently runnable — the real process split lands in PR2.
	// Wired from cmd/grafel for the same reason as RunDaemon.
	RunEngine func(argv []string) error
	// RunLinks runs the cross-repo link passes for a group. Wired up
	// from cmd/grafel so the daemon (Phase B) can re-trigger link
	// passes whenever a registered repo's graph.json changes.
	// May be nil; callers must check.
	RunLinks func(group string) error

	// RunExtract is the Phase F subprocess entrypoint. Wired up from
	// cmd/grafel so the extract subcommand can run the per-file
	// extractor pipeline (Pass 1 + 2.5 + 3) on a bounded batch and
	// stream JSONL records to stdout. Invoked by the daemon-side
	// extract coordinator via fork-exec of the same binary.
	RunExtract func(argv []string) error

	// RunBenchCapture is the entrypoint for `grafel bench-capture`.
	// Wired from cmd/grafel/bench_capture.go; reads a daemon log
	// slice between byte offsets, parses [mcp-rpc] elapsed= lines, and
	// emits JSON to stdout. See #2298.
	RunBenchCapture func(argv []string) error
}

// Execute is the entrypoint called from cmd/grafel/main.go.
// It returns once cobra has dispatched (or printed an error).
func Execute(hooks Hooks) {
	activeHooks = hooks
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// activeHooks is set by Execute; the index/mcp cobra commands look
// here for the cmd/main-provided implementations. Package-level
// state is fine — Execute is called exactly once.
var activeHooks Hooks
