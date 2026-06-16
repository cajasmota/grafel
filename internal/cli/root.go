package cli

import (
	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/version"
)

// primarySurface is the small command list shown by `grafel --help`.
// Power commands live under `grafel help advanced`.
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
		Use:           "grafel",
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
		newModeCmd(),
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
		newStoreCmd(),
		newDocgenCmd(),
		newRegisterCmd(),
		newGroupCmd(),
		newExportCmd(),
		newRemoveCmd(),
		newDeleteCmd(),
		newBranchesCmd(),
		newInstallHooksCmd(),
		newBenchCaptureCmd(),
		newPersonasCmd(),
		newFeedbackCmd(),
		newHelpCmd(),
	)

	// Trim default help to the primary surface.
	root.SetHelpTemplate(primaryHelpTemplate)
	return root
}

const primaryHelpTemplate = `grafel — multi-repo code knowledge graphs for AI agents

Usage:
  grafel <command> [flags]

First-time setup:
  install       Register the daemon as a system service and start it
                (replaces wizard + onboard for most users)

Setup (advanced):
  wizard        Interactive setup for a new group
  onboard       Join a teammate's existing group

Operate:
  update        Update grafel
  doctor        Run health checks across all groups
  status        Show daemon + index status
  list          List registered groups (alias: ls)
  dashboard     Open the code-graph dashboard in your browser

Help:
  help          Show this message
  help advanced Show every subcommand

Flags:
{{.LocalFlags.FlagUsages}}
Run 'grafel help advanced' to see uninstall, rebuild, monorepo, etc.
`

const advancedHelpText = `grafel — full command surface

First-time setup:
  install [--foreground]          Register daemon as OS service + start it
  uninstall                       Stop and remove the daemon service

Setup (advanced):
  wizard                          Interactive setup for a new group
  onboard [path]                  Join a teammate's existing group

Operate:
  update [--refresh-rules-lite]   Update grafel
  doctor [--kill-stale] [--ref <ref>]  Run health checks; --kill-stale terminates orphaned /tmp daemons
  status [group] [--ref <ref>]         Show daemon health + per-repo state
  list [--ref <ref>]                   List registered groups (alias: ls)

Repair:
  rebuild [group] [slug] [--ref <ref>]  Force AST rebuild (no cache, daemon RPC)
  reset [group] [slug]                  Wipe .grafel/ and rebuild via daemon

Daemon modes (S7):
  mode <background|workstation|readonly>
                                  Switch operational mode + restart daemon

Lifecycle:
  group add <group> --repo slug=path [--index] [--json]
                                       Register a group non-interactively (scriptable wizard; for agents/CI)
  remove <group> <slug> [--ref <ref>]  Remove a single repo from a group
  delete <group>                       Delete an entire group and all its repos
  branches [group]                     List per-ref graph tiers + lifecycle management

Branch management (PH6):
  branches [group]                List all refs: tier, idle, size, pin state
  branches --json                 Machine-readable JSON output
  branches --prune-stale [Nd]     Delete EXPIRED graphs (optional TTL override)
  branches --pin <repo> --pin-ref <ref>
                                  Mark a ref as pinned (never expires)
  branches --unpin <repo> --unpin-ref <ref>
                                  Un-pin a previously pinned ref
  branches --keep-last N --keep-last-repo <repo>
                                  Keep only N most-recent feature branches

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
  index <repo> [--ref <ref>]      Index a repository (daemon RPC)

MCP:
  mcp                             (removed) daemon serves MCP; see ADR-0017

Dashboard:
  dashboard                       Open dashboard in browser (auto-starts daemon if needed)
  dashboard serve [--port N]      Run standalone dashboard HTTP server (dev/advanced)

Export:
  export graphml [--group --ref --out file]   Export the group graph to GraphML (XML)
  export cypher  [--group --ref --out file]   Export the group graph to Neo4j Cypher
  export svg     [--group --ref --out --top-N] Export a static SVG of the group graph
  export html    [--group --ref --out --top-N] Export a self-contained HTML graph viewer

Quality:
  quality <fixture-dir>                       Measure extraction recall vs a golden fixture
  quality audit-orphans [--corpus] <path>     Audit orphan rate + edge hygiene; emits md or JSON
  quality bug-rate-corpus [flags] <dir>       Composite health score across a corpus of indexed groups
  quality check [--strict] <group|path>       Evaluate architectural fitness rules (.grafel/fitness.yaml)

Documentation generation:
  docgen --tier=0 --seed-entity=<id> --section=<name>
                                  Render ONE section for ONE entity (<30 s feedback loop)
  docgen --list-sections          List all valid section names

Bench skill helpers:
  bench-capture rpc [--log <path>] [--start-offset N] [--end-offset N]
                                  Parse daemon-log RPC window → JSON (mcp_rpc_count, handler ms p50/p99)

Agent-learned patterns (ADR-0018):
  patterns list [--needs-attention]           Show patterns table (rejected/low-conf/stale with --needs-attention)
  patterns show <id>                          Pretty-print a pattern as JSON
  patterns edit <id>                          Edit a pattern in $EDITOR
  patterns delete <id> [--force]              Remove a pattern (dry-run by default)
  patterns export --repo <p> | --file <p>     Write the CLAUDE.md marker block
  patterns import --repo <p> | --file <p>     Diff CLAUDE.md vs the store
  patterns config [key=value]                 Get/set thresholds (per_subagent_threshold, …)
  patterns gc [--dry-run=false]               Prune candidates older than candidate_decay_days

Personas (cross-platform wrappers):
  personas render --target <target> [--output <dir>] [--personas-dir <dir>]
                                  Render platform-specific wrappers from canonical persona files
                                  Targets: claude-code, windsurf, cursor, codex
`
