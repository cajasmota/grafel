package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// agentsMDMarkers for the grafel discovery stub.
// Matches the pattern proposed in #658: marker-wrapped idempotent upsert.
const (
	agentsMDStartMarker = "<!-- grafel:start v=1 -->"
	agentsMDEndMarker   = "<!-- grafel:end -->"
)

// agentsMDBlockRegex matches the entire marker-wrapped region.
var agentsMDBlockRegex = regexp.MustCompile(`(?s)` +
	regexp.QuoteMeta(agentsMDStartMarker) +
	`.*?` +
	regexp.QuoteMeta(agentsMDEndMarker))

// newRegisterCmd returns the `grafel register` subcommand.
// It's a hidden command useful for repo setup automation and agent discovery.
func newRegisterCmd() *cobra.Command {
	var writeAgentsMD bool
	var group string
	var repoPath string

	cmd := &cobra.Command{
		Use:    "register [--write-agents-md]",
		Hidden: true,
		Short:  "Register grafel in a repository",
		Long: `register writes an grafel discovery stub into a repository's
AGENTS.md file so agents working in that repo know grafel is available.

The stub is a tiny ~10-line block explaining that grafel is available via MCP
and pointing agents to the MCP handshake for the full agent guide. It is
intentionally minimal — the canonical documentation is delivered via the MCP
instructions field at connection time, not per-repo.

Use --write-agents-md to write the stub to the current directory's AGENTS.md.

The upsert is idempotent — re-running register replaces the block in-place.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			if !writeAgentsMD {
				fmt.Fprintln(out, "register: no action specified")
				fmt.Fprintln(out, "try: grafel register --write-agents-md")
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

			fmt.Fprintf(out, "✓ wrote grafel discovery stub to %s\n", agentsMDPath)
			if group != "" {
				fmt.Fprintf(out, "  group: %s\n", group)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&writeAgentsMD, "write-agents-md", false,
		"write grafel discovery stub to AGENTS.md in the target repo")
	cmd.Flags().StringVar(&group, "group", "",
		"optional grafel group name (default: auto-detect from config)")
	cmd.Flags().StringVar(&repoPath, "repo", "",
		"target repository path (default: current working directory)")

	return cmd
}

// renderAgentsMDStub builds the marker-wrapped discovery block for AGENTS.md.
// This is intentionally minimal — agents learn the full guide via MCP handshake.
func renderAgentsMDStub(groupName string) string {
	return fmt.Sprintf(`%s

## grafel

This repo is part of grafel group **%s**. grafel is available via MCP
(%%mcpServers.grafel%%) and indexes code entities and relationships across
every repo in the group.

The full agent guide — cost model, tool reference, query patterns, routing rules —
is delivered automatically in the MCP %%instructions%% handshake.

%s
`, agentsMDStartMarker, groupName, agentsMDEndMarker)
}

// newAgentsMDPerm is the mode AGENTS.md is CREATED at when it does not exist
// yet.
//
// 0644, and NOT the 0600 the dashboard's MCP host configs use — that reasoning
// does not transfer. AGENTS.md is an ordinary tracked file in the user's git
// repository: it is committed, every collaborator's checkout reads it, and the
// block written into it is public documentation carrying no credential. The same
// call slice 1 made for agenthooks' project-scoped settings.json.
//
// An EXISTING AGENTS.md keeps its own mode; this only decides what a brand-new
// one looks like.
//
// Note this OVERRIDES the user's umask, and is a change from the os.WriteFile
// this replaced: that passed perm through open(2), so under `umask 077` a fresh
// AGENTS.md came out 0600. atomicfile.WriteFile Chmods to exactly the mode
// requested (see its package doc — a deliberate, tree-wide decision), so it now
// comes out 0644 on any umask. Accepted for a committed documentation file, and
// the same trade slice 1 made for agenthooks' settings.json, but it does mean
// this slice WIDENS the create mode on a umask-077 machine — on the very axis
// #6246 is about. It does not touch an existing file's mode, which is the defect
// itself.
const newAgentsMDPerm os.FileMode = 0o644

// upsertAgentsMDFile reads the target AGENTS.md (if it exists), updates or
// appends the marker-wrapped block, and writes it back. Idempotent.
//
// # #6246: one resolved path for BOTH halves
//
// The destination is resolved through any symlink ONCE, up front, and both the
// read and the write then operate on that resolved path.
//
// Doing it in one place is the point. os.ReadFile FOLLOWS a symlink; the
// temp-file + rename this used to end with does NOT — it replaces the LINK
// INODE. So the old code spliced the block into content taken from the link's
// target and then deposited the result somewhere else entirely: the shared copy
// never received the block, the link stopped being a link, and a second run
// found no block at the target and could append another. AGENTS.md is a TRACKED
// FILE, so the user's next commit silently stopped containing the link they
// wrote, with nothing in the diff to say so.
//
// The mode is read from the destination and re-applied, so a 0600 or read-only
// AGENTS.md is no longer widened to the temp file's 0644.
//
// An UNRESOLVABLE chain is an error wrapping atomicfile.ErrSymlinkChain rather
// than a best-effort write at the last hop reached — that hop is itself a
// symlink, so renaming over it flattens one of the user's links and reports
// success.
func upsertAgentsMDFile(path, block string) error {
	target, err := atomicfile.ResolveWriteTarget(path)
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", target, err)
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

	// Atomic write onto the SAME resolved path the read above used.
	//
	// MkdirAll on the RESOLVED directory, matching rulesfiles.atomicWrite: the
	// link's own directory necessarily exists (we just read a link out of it),
	// the target's need not. atomicfile.WriteFile does not create it, so without
	// this a link at a shared AGENTS.md that does not exist yet fails with an
	// opaque error naming a temp file the user never asked for.
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	return atomicfile.WriteFile(target, out, atomicfile.ExistingPerm(target, newAgentsMDPerm))
}
