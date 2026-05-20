package cli

import (
	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/version"
)

// primarySurface is the small command list shown by `archigraph --help`.
// Power commands live under `archigraph help advanced`.
var primarySurface = map[string]bool{
	"wizard":    true,
	"onboard":   true,
	"install":   true,
	"update":    true,
	"doctor":    true,
	"status":    true,
	"list":      true,
	"dashboard": true,
	"help":      true,
}

// newRoot constructs the cobra root command with every subcommand
// attached. Splitting construction out of an init() keeps tests
// hermetic — they can build a fresh tree per case.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "archigraph",
		Short:         "multi-repo code knowledge graphs for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}
	root.SetVersionTemplate("{{.Version}}\n")

	// Subcommands. Each is defined in its own file in this package.
	root.AddCommand(
		newWizardCmd(),
		newOnboardCmd(),
		newInstallCmd(),
		newUpdateCmd(),
		newDoctorCmd(),
		newStatusCmd(),
		newListCmd(),
		newRebuildCmd(),
		newResetCmd(),
		newUninstallCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newLogsCmd(),
		newDaemonCmd(),
		newMonorepoCmd(),
		newWatchCmd(),
		newIndexCmd(),
		newMCPCmd(),
		newDashboardCmd(),
		newQualityCmd(),
		newLinksCmd(),
		newExtractCmd(),
		newPatternsCmd(),
		newMCPBridgeCmd(),
		newCleanupCmd(),
		newRegisterCmd(),
		newHelpCmd(),
	)

	// Trim default help to the primary surface.
	root.SetHelpTemplate(primaryHelpTemplate)
	return root
}

const primaryHelpTemplate = `archigraph — multi-repo code knowledge graphs for AI agents

Usage:
  archigraph <command> [flags]

First-time setup:
  install       Register the daemon as a system service and start it
                (replaces wizard + onboard for most users)

Setup (advanced):
  wizard        Interactive setup for a new group
  onboard       Join a teammate's existing group

Operate:
  update        Update archigraph
  doctor        Run health checks across all groups
  status        Show daemon + index status
  list          List registered groups (alias: ls)
  dashboard     Open the code-graph dashboard in your browser

Help:
  help          Show this message
  help advanced Show every subcommand

Flags:
{{.LocalFlags.FlagUsages}}
Run 'archigraph help advanced' to see uninstall, rebuild, monorepo, etc.
`

const advancedHelpText = `archigraph — full command surface

First-time setup:
  install [--foreground]          Register daemon as OS service + start it
  uninstall                       Stop and remove the daemon service

Setup (advanced):
  wizard                          Interactive setup for a new group
  onboard [path]                  Join a teammate's existing group

Operate:
  update [--refresh-rules-lite]   Update archigraph
  doctor [--kill-stale]           Run health checks; --kill-stale terminates orphaned /tmp daemons
  status [group]                  Show daemon health + per-repo state
  list                            List registered groups (alias: ls)

Repair:
  rebuild [group] [slug]          Force AST rebuild (no cache, daemon RPC)
  reset [group] [slug]            Wipe .archigraph/ and rebuild via daemon

Maintenance:
  cleanup [--dry-run]             Remove orphaned registry entries

Daemon (manual):
  start                           Start daemon (MCP + indexer + dashboard + watchers)
  stop                            Stop daemon and all managed services
  restart                         Restart daemon (MCP + indexer + dashboard + watchers)
  logs [-f] [-n N]                Print or follow the daemon log
  watch <repo>                    Long-lived watcher (legacy; daemon owns watching)

Monorepo:
  monorepo add [group] [path]     Pick which packages get indexed
  monorepo remove [group] [path]  Deselect modules
  monorepo list                   List indexed monorepo modules

Indexing:
  index <repo>                    Index a repository (daemon RPC)

MCP:
  mcp                             (removed) daemon serves MCP; see ADR-0017

Dashboard:
  dashboard                       Open dashboard in browser (auto-starts daemon if needed)
  dashboard serve [--port N]      Run standalone dashboard HTTP server (dev/advanced)

Quality:
  quality <fixture-dir>                       Measure extraction recall vs a golden fixture
  quality audit-orphans [--corpus] <path>     Audit orphan rate + edge hygiene; emits md or JSON

Agent-learned patterns (ADR-0018):
  patterns list [--needs-attention]           Show patterns table (rejected/low-conf/stale with --needs-attention)
  patterns show <id>                          Pretty-print a pattern as JSON
  patterns edit <id>                          Edit a pattern in $EDITOR
  patterns delete <id> [--force]              Remove a pattern (dry-run by default)
  patterns export --repo <p> | --file <p>     Write the CLAUDE.md marker block
  patterns import --repo <p> | --file <p>     Diff CLAUDE.md vs the store
  patterns config [key=value]                 Get/set thresholds (per_subagent_threshold, …)
  patterns gc [--dry-run=false]               Prune candidates older than candidate_decay_days
`
