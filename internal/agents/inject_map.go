// Package agents provides the Architecture Map injector that appends (or
// updates) an idempotent marker block in a project's AGENTS.md (or CLAUDE.md)
// after every archigraph rebuild. The block tells AI coding agents that this
// repo is indexed, where the dashboard lives, and which MCP endpoints to
// query.
//
// Design notes:
//   - The block is bounded by <!-- archigraph:architecture-map:start --> …
//     <!-- archigraph:architecture-map:end --> so re-runs UPDATE in place.
//   - If the marker pair is missing from an existing file the block is
//     appended at the end with a blank-line separator.
//   - If no agent file exists at all, AGENTS.md is created.
//   - The feature is gated by GroupConfig.AutoInjectAgentsMD (default false).
//   - AI-tool detection: if .claude/ or .cursor/ exist in the repo root the
//     block includes a tailored hint for that toolchain.
package agents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Marker tokens for the architecture-map block. Use a distinct namespace
// from the patterns block (archigraph:patterns) so the two can coexist
// independently in the same file.
const (
	MapStartMarker = "<!-- archigraph:architecture-map:start -->"
	MapEndMarker   = "<!-- archigraph:architecture-map:end -->"
)

// blockRegex matches the entire marker-wrapped region (start + content + end),
// using DOTALL so newlines inside the block are consumed.
var blockRegex = regexp.MustCompile(`(?s)` +
	regexp.QuoteMeta(MapStartMarker) +
	`.*?` +
	regexp.QuoteMeta(MapEndMarker))

// Stats carries the per-repo counters that the Architecture Map block
// displays. All fields are optional — zero values render as 0.
type Stats struct {
	// Group is the archigraph group this repo belongs to (used to construct
	// the dashboard URL fragment).
	Group string

	// DashboardPort is the local dashboard port (default 47274).
	DashboardPort int

	// Entities is the total entity count after the rebuild.
	Entities int

	// Relationships is the total relationship count.
	Relationships int

	// HTTPEndpoints is the count of http_endpoint entities.
	HTTPEndpoints int

	// ProcessFlows is the count of process-flow entities.
	ProcessFlows int

	// Queues is the count of queue entities.
	Queues int

	// Topics is the count of topic/pub-sub entities.
	Topics int

	// IndexedAt is the timestamp written into the block. Defaults to now.
	IndexedAt time.Time

	// BinaryPath is the absolute path to the archigraph binary used for the
	// MCP snippet. Defaults to os.Executable() at render time if empty.
	BinaryPath string
}

// InjectArchitectureMap finds the best agent-facing file in repoPath
// (AGENTS.md > CLAUDE.md > GEMINI.md), creates AGENTS.md if none exists, and
// upserts the Architecture Map marker block with the provided stats.
//
// The function is intentionally best-effort: it logs a non-fatal warning to
// stderr and returns nil when the file cannot be read or written, so a transient
// filesystem issue never fails the rebuild.
func InjectArchitectureMap(repoPath string, stats Stats) error {
	targetPath := resolveTargetFile(repoPath)

	if stats.DashboardPort == 0 {
		stats.DashboardPort = 47274
	}
	if stats.IndexedAt.IsZero() {
		stats.IndexedAt = time.Now().UTC()
	}
	if stats.BinaryPath == "" {
		if exe, err := os.Executable(); err == nil {
			stats.BinaryPath = exe
		} else {
			stats.BinaryPath = "archigraph"
		}
	}

	detected := detectAITools(repoPath)
	block := renderBlock(stats, detected)
	return upsertFile(targetPath, block)
}

// resolveTargetFile returns the first agent-facing file that already exists in
// repoPath, in preference order: AGENTS.md, CLAUDE.md, GEMINI.md. If none
// exist it returns the path for AGENTS.md (which will be created on write).
func resolveTargetFile(repoPath string) string {
	candidates := []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
	for _, name := range candidates {
		p := filepath.Join(repoPath, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(repoPath, "AGENTS.md")
}

// detectedTools is the set of AI toolchains the injector found in the repo.
type detectedTools struct {
	claude bool // .claude/ directory present
	cursor bool // .cursor/ directory present
}

// detectAITools scans the repo root for well-known AI tool configuration
// directories and returns which ones were found.
func detectAITools(repoPath string) detectedTools {
	var d detectedTools
	if _, err := os.Stat(filepath.Join(repoPath, ".claude")); err == nil {
		d.claude = true
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".cursor")); err == nil {
		d.cursor = true
	}
	return d
}

// renderBlock builds the full marker-wrapped Architecture Map block.
func renderBlock(s Stats, tools detectedTools) string {
	var b strings.Builder

	fmt.Fprintln(&b, MapStartMarker)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Architecture Map")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "This repo is indexed by [archigraph](https://archigraph.dev).")
	fmt.Fprintln(&b)

	dashURL := fmt.Sprintf("http://127.0.0.1:%d", s.DashboardPort)
	if s.Group != "" {
		dashURL = fmt.Sprintf("http://127.0.0.1:%d/graph/%s", s.DashboardPort, s.Group)
	}
	fmt.Fprintf(&b, "- **Dashboard**: %s\n", dashURL)

	if s.Group != "" {
		fmt.Fprintf(&b, "- **Entities**: %d — `/api/entities/%s`\n", s.Entities, s.Group)
		fmt.Fprintf(&b, "- **Relationships**: %d — `/api/relationships/%s`\n", s.Relationships, s.Group)
		if s.HTTPEndpoints > 0 {
			fmt.Fprintf(&b, "- **HTTP Endpoints**: %d — `/paths/%s`\n", s.HTTPEndpoints, s.Group)
		}
		if s.ProcessFlows > 0 {
			fmt.Fprintf(&b, "- **Process Flows**: %d — `/flows/%s`\n", s.ProcessFlows, s.Group)
		}
		if s.Queues > 0 || s.Topics > 0 {
			fmt.Fprintf(&b, "- **Topology**: %d queues, %d topics — `/topology/%s`\n",
				s.Queues, s.Topics, s.Group)
		}
	} else {
		fmt.Fprintf(&b, "- **Entities**: %d\n", s.Entities)
		fmt.Fprintf(&b, "- **Relationships**: %d\n", s.Relationships)
		if s.HTTPEndpoints > 0 {
			fmt.Fprintf(&b, "- **HTTP Endpoints**: %d\n", s.HTTPEndpoints)
		}
		if s.ProcessFlows > 0 {
			fmt.Fprintf(&b, "- **Process Flows**: %d\n", s.ProcessFlows)
		}
		if s.Queues > 0 || s.Topics > 0 {
			fmt.Fprintf(&b, "- **Topology**: %d queues, %d topics\n", s.Queues, s.Topics)
		}
	}

	fmt.Fprintf(&b, "- **Last indexed**: %s\n", s.IndexedAt.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintln(&b)

	// MCP snippet — tailor to detected toolchain.
	fmt.Fprintln(&b, "### MCP server (for AI agents)")
	fmt.Fprintln(&b)

	if tools.claude {
		fmt.Fprintln(&b, "Claude Code (`.claude/settings.json`) — archigraph is already registered via `archigraph install`.")
		fmt.Fprintln(&b, "If you need to add it manually:")
	} else if tools.cursor {
		fmt.Fprintln(&b, "Cursor (`.cursor/mcp.json`) — add the archigraph MCP server:")
	} else {
		fmt.Fprintln(&b, "Add the archigraph MCP server to your AI agent config:")
	}

	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "```json\n{ \"mcpServers\": { \"archigraph\": { \"command\": \"%s\", \"args\": [\"mcp\"] } } }\n```\n",
		s.BinaryPath)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "_Do not edit between the markers — this block is auto-updated by `archigraph rebuild`._")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, MapEndMarker)

	return b.String()
}

// upsertFile reads path, replaces the marker block (or appends if absent),
// and writes back atomically. Creates the file (and parent dirs) if missing.
// User content outside the markers is preserved byte-for-byte.
func upsertFile(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var out []byte
	switch {
	case os.IsNotExist(err) || len(existing) == 0:
		// New or empty file — write the block directly.
		out = []byte(block)
	case blockRegex.Match(existing):
		// Existing block — replace in-place (idempotent).
		out = blockRegex.ReplaceAll(existing, []byte(strings.TrimRight(block, "\n")))
	default:
		// File exists but no block — append with blank-line separator.
		buf := bytes.NewBuffer(existing)
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
		buf.WriteString(block)
		out = buf.Bytes()
	}

	// Ensure parent directory exists (in case this is a brand-new repo).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
