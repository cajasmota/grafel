// Package cli implements the archigraph command-line surface.
//
// Commands are registered onto a single cobra root in root.go; each
// command lives in its own file in this package. Top-level help is
// trimmed to a small primary surface (install-and-forget); the full
// surface is reachable via `archigraph help advanced`.
package cli

import (
	"fmt"
	"os"
)

// Hooks lets package main inject implementations of subcommands that
// must live in cmd/archigraph (because they pull in heavy internal
// packages that should not be visible from the CLI surface).
type Hooks struct {
	RunIndex     func(argv []string) error
	RunMCP       func(argv []string) error
	RunDashboard func(argv []string) error
	// RunLinks runs the cross-repo link passes for a group. Wired up
	// from cmd/archigraph so the watcher loop in watch.go can re-trigger
	// link passes whenever a registered repo's graph.json changes.
	// May be nil; callers must check.
	RunLinks func(group string) error
}

// Execute is the entrypoint called from cmd/archigraph/main.go.
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
