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

// SkillNames lists the archigraph skills in canonical order.
var SkillNames = []string{
	"archigraph-aware-review",
	"archigraph-business-docs",
	"archigraph-graph-enrich",
	"archigraph-graph-quality",
	"archigraph-patterns-discover",
	"archigraph-patterns-sync",
	"archigraph-resolve",
	"archigraph-security-audit",
	"archigraph-tech-docs",
	"archigraph-test-page",
	"extend-convention",
	"generate-docs",
	"using-archigraph",
}

// DiscoverSkillsDir finds the source directory containing the archigraph
// skills. It tries these locations in order:
//
// 1. If skillsSourceDir is non-empty, use it as-is (no validation)
// 2. Check $(dirname binPath)/../skills (for shipped binaries)
// 3. Check $ARCHIGRAPH_SKILLS_DIR env var if set
// 4. Check ~/Documents/Projects/archigraph/skills (fallback for local dev)
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

	// Try $(dirname binPath)/../skills (standard for shipped binaries).
	if binPath != "" {
		binDir := filepath.Dir(binPath)
		candidate := filepath.Join(binDir, "..", "skills")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	// Try env var override (useful in CI or special deployments).
	if envPath := os.Getenv("ARCHIGRAPH_SKILLS_DIR"); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && info.IsDir() {
			return envPath
		}
	}

	// Fallback: local dev repository.
	home, err := os.UserHomeDir()
	if err == nil {
		devPath := filepath.Join(home, "Documents", "Projects", "archigraph", "skills")
		if info, err := os.Stat(devPath); err == nil && info.IsDir() {
			return devPath
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
		skillsSubdir := filepath.Join(filepath.Dir(cfgPath), "skills")
		if err := os.MkdirAll(skillsSubdir, 0o755); err != nil {
			fmt.Fprintf(out, "  ⚠ create skills dir %s: %v\n", skillsSubdir, err)
			continue
		}

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
		skillsSubdir := filepath.Join(filepath.Dir(cfgPath), "skills")

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
