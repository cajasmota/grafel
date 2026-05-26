// Package renderer emits platform-specific persona wrappers from the canonical
// Claude Code subagent persona files that live in
// skills/archigraph-consult/personas/*.md.
//
// Supported targets (issue #2476):
//
//	claude-code  — copy as-is (canonical format)
//	windsurf     — .windsurf/workflows/<name>.md with adapted frontmatter
//	cursor       — .cursor/commands/<name>.md with adapted frontmatter
//	codex        — <name>.codex.md stub with TODO marker
//
// All targets are idempotent: re-running overwrites the previous output.
package renderer

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Target is one of the supported rendering targets.
type Target string

const (
	TargetClaudeCode Target = "claude-code"
	TargetWindsurf   Target = "windsurf"
	TargetCursor     Target = "cursor"
	TargetCodex      Target = "codex"
)

// AllTargets lists every supported Target value.
var AllTargets = []Target{
	TargetClaudeCode,
	TargetWindsurf,
	TargetCursor,
	TargetCodex,
}

// ParseTarget returns a Target from a string, or an error if unrecognised.
func ParseTarget(s string) (Target, error) {
	switch Target(s) {
	case TargetClaudeCode, TargetWindsurf, TargetCursor, TargetCodex:
		return Target(s), nil
	}
	return "", fmt.Errorf("unknown target %q; valid targets: claude-code, windsurf, cursor, codex", s)
}

// Result describes a single file that was (or would be) written.
type Result struct {
	Source string // absolute path of the canonical persona file
	Dest   string // absolute path of the emitted file
	Name   string // persona base name (e.g. "architect")
}

// Render reads every *.md file in srcDir and writes target-specific wrappers
// into outDir. It returns the list of files written. Errors from individual
// persona files are accumulated; Render returns the first error encountered
// but still attempts all files.
func Render(srcDir, outDir string, target Target) ([]Result, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, fmt.Errorf("renderer: reading persona source dir %s: %w", srcDir, err)
	}

	var results []Result
	var firstErr error

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		srcPath := filepath.Join(srcDir, e.Name())
		name := strings.TrimSuffix(e.Name(), ".md")

		dest, err := renderOne(srcPath, name, outDir, target)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results = append(results, Result{Source: srcPath, Dest: dest, Name: name})
	}

	return results, firstErr
}

// renderOne renders a single persona file. It returns the absolute path of the
// emitted file.
func renderOne(srcPath, name, outDir string, target Target) (string, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("renderer: reading %s: %w", srcPath, err)
	}

	frontmatter, body := splitFrontmatter(string(raw))

	var destRel string
	var content string

	switch target {
	case TargetClaudeCode:
		destRel = name + ".md"
		content = string(raw)

	case TargetWindsurf:
		destRel = filepath.Join(".windsurf", "workflows", name+".md")
		content = renderWindsurf(name, frontmatter, body)

	case TargetCursor:
		destRel = filepath.Join(".cursor", "commands", name+".md")
		content = renderCursor(name, frontmatter, body)

	case TargetCodex:
		destRel = name + ".codex.md"
		content = renderCodex(name, frontmatter, body)

	default:
		return "", fmt.Errorf("renderer: unknown target %q", target)
	}

	dest := filepath.Join(outDir, destRel)
	if err := mkdirAndWrite(dest, content); err != nil {
		return "", err
	}
	return dest, nil
}

// splitFrontmatter splits a markdown file into its YAML frontmatter block
// (without the --- delimiters) and the rest of the body. If the file does not
// begin with a --- block, frontmatter is empty and body is the full text.
func splitFrontmatter(src string) (frontmatter, body string) {
	if !strings.HasPrefix(src, "---") {
		return "", src
	}
	// Skip the opening ---\n
	rest := src[3:]
	if idx := strings.Index(rest, "\n"); idx >= 0 {
		rest = rest[idx+1:]
	}
	// Find closing ---
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", src
	}
	frontmatter = rest[:end]
	body = rest[end+4:] // skip \n---
	// skip optional trailing newline after closing ---
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return frontmatter, body
}

// parseFrontmatterField returns the value of a simple key: value line from
// a YAML frontmatter string.
func parseFrontmatterField(fm, key string) string {
	for _, line := range strings.Split(fm, "\n") {
		if strings.HasPrefix(line, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		}
	}
	return ""
}

// renderWindsurf adapts the canonical persona to a Windsurf workflow file.
// Windsurf workflows use a simpler frontmatter shape: description only.
func renderWindsurf(name, frontmatter, body string) string {
	description := parseFrontmatterField(frontmatter, "description")
	if description == "" {
		description = "archigraph " + name + " persona"
	}
	// Windsurf multi-line description comes as a YAML block scalar; unwrap.
	description = strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))

	var b strings.Builder
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "# Generated by archigraph personas render --target windsurf\n")
	fmt.Fprintf(&b, "# Source: skills/archigraph-consult/personas/%s.md\n", name)
	fmt.Fprintf(&b, "# DO NOT EDIT — re-run `archigraph personas render` to regenerate.\n")
	fmt.Fprintf(&b, "description: %s\n", description)
	fmt.Fprintf(&b, "---\n\n")
	b.WriteString(body)
	return b.String()
}

// renderCursor adapts the canonical persona to a Cursor command file.
func renderCursor(name, frontmatter, body string) string {
	description := parseFrontmatterField(frontmatter, "description")
	if description == "" {
		description = "archigraph " + name + " persona"
	}
	description = strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))

	var b strings.Builder
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "# Generated by archigraph personas render --target cursor\n")
	fmt.Fprintf(&b, "# Source: skills/archigraph-consult/personas/%s.md\n", name)
	fmt.Fprintf(&b, "# DO NOT EDIT — re-run `archigraph personas render` to regenerate.\n")
	fmt.Fprintf(&b, "description: %s\n", description)
	fmt.Fprintf(&b, "---\n\n")
	b.WriteString(body)
	return b.String()
}

// renderCodex writes a stub file with a TODO marker. The content model and
// frontmatter for Codex is TBD and will be filled in a follow-up.
func renderCodex(name, frontmatter, body string) string {
	description := parseFrontmatterField(frontmatter, "description")
	if description == "" {
		description = "archigraph " + name + " persona"
	}
	description = strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — Codex stub\n", name)
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# Generated by archigraph personas render --target codex\n")
	fmt.Fprintf(&b, "# Source: skills/archigraph-consult/personas/%s.md\n", name)
	fmt.Fprintf(&b, "# DO NOT EDIT — re-run `archigraph personas render` to regenerate.\n")
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# TODO: Codex command format is not yet finalised.\n")
	fmt.Fprintf(&b, "# This stub preserves the persona body for when the format is defined.\n")
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# Description: %s\n\n", description)
	b.WriteString(body)
	return b.String()
}

// mkdirAndWrite creates parent directories and writes content to path,
// overwriting any existing file.
func mkdirAndWrite(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("renderer: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("renderer: writing %s: %w", path, err)
	}
	return nil
}

// DiscoverPersonasDir walks upward from base looking for the canonical
// skills/archigraph-consult/personas directory. Returns an error if not found
// within maxDepth levels. This is used by the CLI when --personas-dir is not
// explicitly set.
func DiscoverPersonasDir(base string, maxDepth int) (string, error) {
	dir := base
	for i := 0; i < maxDepth; i++ {
		candidate := filepath.Join(dir, "skills", "archigraph-consult", "personas")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("renderer: could not find skills/archigraph-consult/personas under %s (walked %d levels)", base, maxDepth)
}

// CountMD returns the number of *.md files in dir. Useful in tests.
func CountMD(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, _ error) error {
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			n++
		}
		return nil
	})
	return n
}

// WriteLines is a helper for test file creation: writes lines to w.
func WriteLines(w io.Writer, lines ...string) {
	bw := bufio.NewWriter(w)
	for _, l := range lines {
		fmt.Fprintln(bw, l)
	}
	_ = bw.Flush()
}
