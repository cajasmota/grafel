// Package skilllink handles symlinking archigraph skills into Claude Code's
// discovery directories.
//
// The archigraph skills are distributed with the binary and must be
// symlinked into ~/.claude/skills/, ~/.claude-*/skills/, etc. so that
// Claude Code's skill discovery mechanism can find them.
//
// This supports both shipped binaries (where skills live in a known
// location relative to the binary) and local dev environments (where skills
// live in the archigraph repo).
package skilllink

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SkillNames lists the 8 archigraph skills in canonical order.
var SkillNames = []string{
	"archigraph-aware-review",
	"archigraph-business-docs",
	"archigraph-consult",
	"archigraph-graph-enrich",
	"archigraph-graph-quality",
	"archigraph-help",
	"archigraph-patterns-discover",
	"archigraph-patterns-sync",
	"archigraph-resolve",
	"archigraph-security-audit",
	"archigraph-tech-docs",
	"archigraph-test-page",
	"extend-convention",
	"using-archigraph",
}

// ClaudeSkillsDirForConfig derives the skills directory associated with a
// Claude Code config file.
//
// Claude Code's layout has two flavours:
//
//   - Primary config at HOME/.claude.json — skills live at HOME/.claude/skills/
//     (the parent of the config file is HOME, NOT the instance dir).
//   - Sidecar config at HOME/.claude-X/.claude.json — skills live at
//     HOME/.claude-X/skills/ (the parent of the config IS the instance dir).
//
// The unifying rule is: if the config's parent directory basename already
// starts with ".claude" (sidecar layout), the parent IS the instance dir
// and skills sit alongside the config.  Otherwise, the instance dir is
// derived by stripping ".json" from the config's basename and treating the
// remainder as a subdirectory of the parent.
//
// Examples:
//
//	~/.claude.json                       → ~/.claude/skills
//	~/.claude-personal/.claude.json      → ~/.claude-personal/skills
//	~/.claude-extra/.claude.json         → ~/.claude-extra/skills
//	/abs/path/.claude.json               → /abs/path/.claude/skills
//	~/.claude-personal.json (flat)       → ~/.claude-personal/skills
//
// If configPath does not end in ".json", the empty string is returned.
func ClaudeSkillsDirForConfig(configPath string) string {
	if !strings.HasSuffix(configPath, ".json") {
		return ""
	}
	parent := filepath.Dir(configPath)
	if strings.HasPrefix(filepath.Base(parent), ".claude") {
		// Sidecar layout — parent dir is the Claude instance dir.
		return filepath.Join(parent, "skills")
	}
	// Primary / flat layout — derive the instance dir by stripping ".json".
	stem := strings.TrimSuffix(filepath.Base(configPath), ".json")
	return filepath.Join(parent, stem, "skills")
}

// PruneOrphanSkillSymlinks removes any *symlink* entries in skillsSubdir
// whose basename is not in the current SkillNames set. This cleans up after
// renamed or retired skills from earlier installs.
//
// Defensive: only symlinks are removed; regular directories (manual
// installs) are left untouched.  Errors are reported via out but do not
// abort the caller.
func PruneOrphanSkillSymlinks(out io.Writer, skillsSubdir string) {
	entries, err := os.ReadDir(skillsSubdir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(out, "    ⚠ scan for orphan skills in %s: %v\n", skillsSubdir, err)
		}
		return
	}
	current := make(map[string]bool, len(SkillNames))
	for _, name := range SkillNames {
		current[name] = true
	}
	for _, e := range entries {
		name := e.Name()
		if current[name] {
			continue
		}
		full := filepath.Join(skillsSubdir, name)
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			// Don't touch regular directories — those are manual installs.
			continue
		}
		if err := os.Remove(full); err != nil {
			fmt.Fprintf(out, "    ⚠ remove orphan skill symlink %s: %v\n", full, err)
			continue
		}
		fmt.Fprintf(out, "    ⓘ removed orphan skill symlink: %s\n", name)
	}
}

// DiscoverSkillsDir finds the source directory containing the archigraph
// skills. It tries these locations in order:
//
//  1. If skillsSourceDir is non-empty, use it as-is (caller-validated override)
//  2. Check $(dirname binPath)/skills — sibling layout produced by
//     `go build ./cmd/archigraph` in the repo root (e.g. repo/archigraph +
//     repo/skills)
//  3. Check $(dirname binPath)/../skills — one-up layout used by shipped
//     binaries installed under a bin/ subdirectory (e.g. prefix/bin/archigraph +
//     prefix/skills)
//  4. Check $ARCHIGRAPH_SKILLS_DIR env var if set
//  5. Walk up ancestor directories from dirname(binPath) up to 5 levels,
//     checking <ancestor>/skills at each level — handles arbitrary repo layouts
//     such as repo/build/archigraph + repo/skills
//
// Returns "" if none of the locations exist, which signals the caller to
// error or skip the step.
func DiscoverSkillsDir(binPath, skillsSourceDir string) string {
	// Explicit override from flag or config.
	if skillsSourceDir != "" {
		if info, err := os.Stat(skillsSourceDir); err == nil && info.IsDir() {
			return skillsSourceDir
		}
		// Don't fall through if the user explicitly set it but it doesn't exist.
		return ""
	}

	if binPath != "" {
		binDir := filepath.Dir(binPath)

		// Try sibling: $(dirname binPath)/skills — produced by `go build` in repo root.
		sibling := filepath.Join(binDir, "skills")
		if info, err := os.Stat(sibling); err == nil && info.IsDir() {
			return sibling
		}

		// Try one-up: $(dirname binPath)/../skills — shipped bin/ layout.
		oneUp := filepath.Join(binDir, "..", "skills")
		if info, err := os.Stat(oneUp); err == nil && info.IsDir() {
			return oneUp
		}
	}

	// Try env var override (useful in CI or special deployments).
	if envPath := os.Getenv("ARCHIGRAPH_SKILLS_DIR"); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && info.IsDir() {
			return envPath
		}
	}

	// Walk up ancestors of binPath (up to 5 levels) looking for a skills/
	// subdirectory.  This handles layouts like repo/build/archigraph + repo/skills
	// without encoding any machine-specific path.
	if binPath != "" {
		dir := filepath.Dir(binPath)
		for i := 0; i < 5; i++ {
			parent := filepath.Dir(dir)
			if parent == dir {
				// Reached filesystem root.
				break
			}
			dir = parent
			candidate := filepath.Join(dir, "skills")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}

	return ""
}

// InstallSkillsInClaudeConfigs symlinks the archigraph skills into every
// detected Claude Code config directory's skills/ subdirectory.
//
// claudeConfigDirs: list of ~/.claude.json paths (typically from mcpreg.DetectClaudeConfigDirs)
// skillsSourceDir: explicit override of skills location (from --skills-source-dir flag)
// binPath: path to the archigraph binary (used to infer skills location)
//
// Returns the list of directories where skills were successfully installed,
// and prints status to out. Errors are soft — we report them but don't abort
// the install if, for example, one config dir is unwritable.
func InstallSkillsInClaudeConfigs(out io.Writer, binPath, skillsSourceDir string, claudeConfigDirs []string) []string {
	skillsDir := DiscoverSkillsDir(binPath, skillsSourceDir)
	if skillsDir == "" {
		fmt.Fprintf(out, "  ⚠ skills directory not found at expected locations; skipping skill link\n")
		fmt.Fprintf(out, "    Set --skills-source-dir <path> to override\n")
		return nil
	}

	installed := []string{}
	for _, cfgPath := range claudeConfigDirs {
		skillsSubdir := ClaudeSkillsDirForConfig(cfgPath)
		if skillsSubdir == "" {
			fmt.Fprintf(out, "  ⚠ cannot derive skills dir for %s (not a .json config); skipping\n", cfgPath)
			continue
		}
		if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
			fmt.Fprintf(out, "  ⚠ create skills dir %s: %v\n", skillsSubdir, err)
			continue
		}

		// Prune orphan symlinks (renamed/retired skills from earlier installs)
		// BEFORE adding the current set, so a stale entry never survives a
		// re-install.
		PruneOrphanSkillSymlinks(out, skillsSubdir)

		allOK := true
		for _, skillName := range SkillNames {
			src := filepath.Join(skillsDir, skillName)
			dst := filepath.Join(skillsSubdir, skillName)

			// Check if destination exists.
			dstInfo, err := os.Lstat(dst)
			if err == nil {
				// Destination exists. Check if it's a symlink.
				if dstInfo.Mode()&os.ModeSymlink != 0 {
					// It's a symlink. Replace it (idempotent update).
					if err := os.Remove(dst); err != nil {
						fmt.Fprintf(out, "    ⚠ remove old symlink %s: %v\n", dst, err)
						allOK = false
						continue
					}
				} else {
					// It's a regular directory (user manual install). Skip with warning.
					fmt.Fprintf(out, "    ⚠ %s exists as directory (manual install?); skipping\n", skillName)
					continue
				}
			} else if !os.IsNotExist(err) {
				// Other error (e.g., permission denied).
				fmt.Fprintf(out, "    ⚠ stat %s: %v\n", dst, err)
				allOK = false
				continue
			}

			// Create the symlink.
			if err := os.Symlink(src, dst); err != nil {
				fmt.Fprintf(out, "    ⚠ symlink %s: %v\n", skillName, err)
				allOK = false
				continue
			}
		}

		if allOK {
			installed = append(installed, skillsSubdir)
		}
	}

	if len(installed) > 0 {
		fmt.Fprintf(out, "  Skills linked in:\n")
		for _, p := range installed {
			fmt.Fprintf(out, "    %s\n", p)
		}
	}
	return installed
}

// RemoveSkillsFromClaudeConfigs removes the symlinked archigraph skills from
// every detected Claude Code config directory's skills/ subdirectory.
//
// Only removes symlinks; if a skill exists as a regular directory (user manual
// install), it's left alone with a warning.
//
// Returns the list of directories from which skills were successfully removed.
func RemoveSkillsFromClaudeConfigs(out io.Writer, claudeConfigDirs []string) []string {
	removed := []string{}
	for _, cfgPath := range claudeConfigDirs {
		skillsSubdir := ClaudeSkillsDirForConfig(cfgPath)
		if skillsSubdir == "" {
			fmt.Fprintf(out, "  ⚠ cannot derive skills dir for %s (not a .json config); skipping\n", cfgPath)
			continue
		}

		// Check if skills subdir exists.
		info, err := os.Stat(skillsSubdir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			fmt.Fprintf(out, "  ⚠ stat skills dir %s: %v\n", skillsSubdir, err)
			continue
		}
		if !info.IsDir() {
			fmt.Fprintf(out, "  ⚠ %s is not a directory\n", skillsSubdir)
			continue
		}

		allOK := true
		for _, skillName := range SkillNames {
			dst := filepath.Join(skillsSubdir, skillName)

			// Check if destination exists.
			dstInfo, err := os.Lstat(dst)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				fmt.Fprintf(out, "    ⚠ stat %s: %v\n", dst, err)
				allOK = false
				continue
			}

			// Only remove if it's a symlink.
			if dstInfo.Mode()&os.ModeSymlink == 0 {
				fmt.Fprintf(out, "    ⚠ %s is not a symlink (manual install?); leaving alone\n", skillName)
				continue
			}

			// Remove the symlink.
			if err := os.Remove(dst); err != nil {
				fmt.Fprintf(out, "    ⚠ remove symlink %s: %v\n", skillName, err)
				allOK = false
				continue
			}
		}

		if allOK {
			removed = append(removed, skillsSubdir)
		}
	}

	if len(removed) > 0 {
		fmt.Fprintf(out, "  Skills removed from:\n")
		for _, p := range removed {
			fmt.Fprintf(out, "    %s\n", p)
		}
	}
	return removed
}

// ValidateSkillSymlinks checks that all expected skills are correctly
// symlinked in the given directory. Used for testing and verification.
//
// Returns a description of any missing or incorrect symlinks, empty string
// if all are correct.
func ValidateSkillSymlinks(skillsSubdir string) string {
	var errs []string
	for _, skillName := range SkillNames {
		dst := filepath.Join(skillsSubdir, skillName)
		info, err := os.Lstat(dst)
		if err != nil {
			if os.IsNotExist(err) {
				errs = append(errs, fmt.Sprintf("%s: not found", skillName))
			} else {
				errs = append(errs, fmt.Sprintf("%s: stat error: %v", skillName, err))
			}
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			errs = append(errs, fmt.Sprintf("%s: not a symlink", skillName))
		}
	}
	if len(errs) > 0 {
		return strings.Join(errs, "; ")
	}
	return ""
}
