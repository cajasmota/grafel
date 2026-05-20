package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// agentsMDMarkers for the archigraph discovery stub.
// Matches the pattern proposed in #658: marker-wrapped idempotent upsert.
const (
	agentsMDStartMarker = "<!-- archigraph:start v=1 -->"
	agentsMDEndMarker   = "<!-- archigraph:end -->"
)

// agentsMDBlockRegex matches the entire marker-wrapped region.
var agentsMDBlockRegex = regexp.MustCompile(`(?s)` +
	regexp.QuoteMeta(agentsMDStartMarker) +
	`.*?` +
	regexp.QuoteMeta(agentsMDEndMarker))

// newRegisterCmd returns the `archigraph register` subcommand.
// It's a hidden command useful for repo setup automation and agent discovery.
func newRegisterCmd() *cobra.Command {
	var writeAgentsMD bool
	var group string
	var repoPath string

	cmd := &cobra.Command{
		Use:    "register [--write-agents-md]",
		Hidden: true,
		Short:  "Register archigraph in a repository",
		Long: `register writes an archigraph discovery stub into a repository's
AGENTS.md file so agents working in that repo know archigraph is available.

The stub is a tiny ~10-line block explaining that archigraph is available via MCP
and pointing agents to the MCP handshake for the full agent guide. It is
intentionally minimal — the canonical documentation is delivered via the MCP
instructions field at connection time, not per-repo.

Use --write-agents-md to write the stub to the current directory's AGENTS.md.

The upsert is idempotent — re-running register replaces the block in-place.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			if !writeAgentsMD {
				fmt.Fprintln(out, "register: no action specified")
				fmt.Fprintln(out, "try: archigraph register --write-agents-md")
				return nil
			}

			// Determine target repo path (default to cwd).
			targetPath := repoPath
			if targetPath == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
				targetPath = wd
			}

			// Write AGENTS.md stub.
			agentsMDPath := filepath.Join(targetPath, "AGENTS.md")
			groupName := group
			if groupName == "" {
				groupName = "<group-name>"
			}

			stub := renderAgentsMDStub(groupName)
			if err := upsertAgentsMDFile(agentsMDPath, stub); err != nil {
				return fmt.Errorf("upsert AGENTS.md: %w", err)
			}

			fmt.Fprintf(out, "✓ wrote archigraph discovery stub to %s\n", agentsMDPath)
			if group != "" {
				fmt.Fprintf(out, "  group: %s\n", group)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&writeAgentsMD, "write-agents-md", false,
		"write archigraph discovery stub to AGENTS.md in the target repo")
	cmd.Flags().StringVar(&group, "group", "",
		"optional archigraph group name (default: auto-detect from config)")
	cmd.Flags().StringVar(&repoPath, "repo", "",
		"target repository path (default: current working directory)")

	return cmd
}

// renderAgentsMDStub builds the marker-wrapped discovery block for AGENTS.md.
// This is intentionally minimal — agents learn the full guide via MCP handshake.
func renderAgentsMDStub(groupName string) string {
	return fmt.Sprintf(`%s

## archigraph

This repo is part of archigraph group **%s**. archigraph is available via MCP
(%%mcpServers.archigraph%%) and indexes code entities and relationships across
every repo in the group.

The full agent guide — cost model, tool reference, query patterns, routing rules —
is delivered automatically in the MCP %%instructions%% handshake.

%s
`, agentsMDStartMarker, groupName, agentsMDEndMarker)
}

// upsertAgentsMDFile reads the target AGENTS.md (if it exists), updates or
// appends the marker-wrapped block, and writes it back. Idempotent.
func upsertAgentsMDFile(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var out []byte
	if os.IsNotExist(err) || len(existing) == 0 {
		// File doesn't exist or is empty; write the block.
		out = []byte(block)
	} else if agentsMDBlockRegex.Match(existing) {
		// Block exists; replace it in-place (idempotent).
		out = agentsMDBlockRegex.ReplaceAll(existing, []byte(strings.TrimRight(block, "\n")))
	} else {
		// File exists but no block; append with separator.
		buf := strings.Builder{}
		buf.Write(existing)
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
		buf.WriteString(block)
		out = []byte(buf.String())
	}

	// Atomic write via temp file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
