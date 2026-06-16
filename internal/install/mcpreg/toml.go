package mcpreg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Codex (the OpenAI CLI) configures MCP servers in ~/.codex/config.toml, NOT
// JSON, and under the table name `mcp_servers` (underscore) rather than the
// JSON-world `mcpServers`. grafel has no TOML dependency in go.mod, so rather
// than pull one in we treat the file line-by-line as a sequence of
// table-delimited blocks and surgically add/remove only the
// [mcp_servers.grafel] block. Every other table — including foreign MCP
// servers such as [mcp_servers.other] and unrelated top-level keys — is
// preserved byte-for-byte. The same backup/sidecar machinery used for the
// JSON hosts snapshots the original first, so rollback (RestorePath) is
// format-agnostic.

// codexServerTable is the TOML table header grafel owns in Codex config.
// Codex requires the underscore form `mcp_servers`; the hyphen/camelCase
// variants are silently ignored by Codex.
const codexServerTable = "mcp_servers." + ServerName

// isTOML reports whether a config path should be edited as TOML rather than
// JSON. Dispatch is purely on file extension so format-agnostic callers
// (e.g. the uninstall loop, which only has a recorded path) route correctly.
func isTOML(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".toml")
}

// tomlHeader returns the table name from a line like `[a.b.c]` (trimmed),
// or "" if the line is not a (standard, non-array) table header. Array-of-
// tables headers (`[[x]]`) are intentionally not matched: we never write one
// and must leave any foreign ones untouched as ordinary content.
func tomlHeader(line string) string {
	t := strings.TrimSpace(line)
	if len(t) < 2 || t[0] != '[' || t[len(t)-1] != ']' {
		return ""
	}
	if strings.HasPrefix(t, "[[") {
		return ""
	}
	return strings.TrimSpace(t[1 : len(t)-1])
}

// stripGrafelBlock returns the file content with the [mcp_servers.grafel]
// table (its header line plus all following lines up to the next table
// header or EOF) removed, and reports whether such a block was present.
// Leading blank lines that immediately preceded the removed block are also
// trimmed so we don't accumulate blank lines across re-registrations.
func stripGrafelBlock(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var out []string
	removed := false
	skipping := false
	for _, line := range lines {
		hdr := tomlHeader(line)
		if hdr != "" {
			if hdr == codexServerTable {
				skipping = true
				removed = true
				// Drop trailing blank lines we already emitted that were
				// acting as the separator before this block.
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				continue
			}
			skipping = false
		}
		if skipping {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), removed
}

// grafelTOMLBlock renders the canonical grafel server table. Mirrors the JSON
// Entry: command + args ["mcp-bridge"]. (Codex derives stdio transport from
// the presence of command/args, so no explicit type key is emitted.)
func grafelTOMLBlock(binPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", codexServerTable)
	fmt.Fprintf(&b, "command = %s\n", tomlQuote(binPath))
	b.WriteString("args = [" + tomlQuote("mcp-bridge") + "]\n")
	return b.String()
}

// tomlQuote renders a Go string as a TOML basic string literal.
func tomlQuote(s string) string {
	// strconv.Quote produces a valid TOML basic string for our inputs
	// (escapes backslashes, quotes and control chars the same way).
	return strconv.Quote(s)
}

// registerTOML adds or replaces the [mcp_servers.grafel] table in a Codex
// config.toml, preserving every other table and top-level key. Idempotent.
func registerTOML(path, binPath string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stripped, _ := stripGrafelBlock(string(raw))
	// Normalise: trim a trailing run of blank lines, then re-append exactly
	// one separating blank line before our block when there is prior content.
	stripped = strings.TrimRight(stripped, "\n")
	var out strings.Builder
	if strings.TrimSpace(stripped) != "" {
		out.WriteString(stripped)
		out.WriteString("\n\n")
	}
	out.WriteString(grafelTOMLBlock(binPath))
	return writeRaw(path, out.String())
}

// unregisterTOML removes ONLY the [mcp_servers.grafel] table from a Codex
// config.toml, leaving every foreign table intact. Idempotent: a missing
// file or missing block is a no-op. If removing grafel leaves the file empty
// (or whitespace-only) the now-pointless file is deleted rather than left as
// an empty husk.
func unregisterTOML(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	stripped, removed := stripGrafelBlock(string(raw))
	if !removed {
		return nil
	}
	if strings.TrimSpace(stripped) == "" {
		// We were the only content; don't leave an empty file behind.
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return rmErr
		}
		return nil
	}
	return writeRaw(path, strings.TrimRight(stripped, "\n")+"\n")
}

// writeRaw writes content atomically (temp file + rename), matching the JSON
// writer's durability behaviour.
func writeRaw(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
