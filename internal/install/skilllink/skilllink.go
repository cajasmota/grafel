// Package skilllink handles symlinking grafel skills into Claude Code's
// discovery directories.
//
// The grafel skills are distributed with the binary and must be
// symlinked into ~/.claude/skills/, ~/.claude-*/skills/, etc. so that
// Claude Code's skill discovery mechanism can find them.
//
// This supports both shipped binaries (where skills live in a known
// location relative to the binary) and local dev environments (where skills
// live in the grafel repo).
package skilllink

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	grafelassets "github.com/cajasmota/grafel"
)

// SkillNames lists the 15 user-invocable grafel skills in canonical order.
//
// Deliberately EXCLUDED from this list (#5274): `grafel-graph-read` and
// `grafel-graph-write`. Those two are not standalone Claude skills — their
// SKILL.md frontmatter describes them as "Shared grafel read/persistence
// protocol — Compose into any persona", and they carry no `when-to-use`
// (so Claude Code would never auto-trigger them). They are shared protocol
// fragments composed into the consult/docs personas, not user-facing
// commands, so symlinking them would surface two un-triggerable skills in
// the user's skills directory. `grafel-feedback`, by contrast, IS a
// directly user-invocable skill (it has a full `when-to-use` and a
// /grafel-feedback entry point) and is included.
var SkillNames = []string{
	"grafel-aware-review",
	"grafel-business-docs",
	"grafel-consult",
	"grafel-feedback",
	"grafel-graph-enrich",
	"grafel-graph-quality",
	"grafel-help",
	"grafel-patterns-discover",
	"grafel-patterns-sync",
	"grafel-resolve",
	"grafel-security-audit",
	"grafel-tech-docs",
	"grafel-test-page",
	"extend-convention",
	"using-grafel",
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

// DiscoverSkillsDir finds the source directory containing the grafel
// skills. It tries these locations in order:
//
//  1. If skillsSourceDir is non-empty, use it as-is (caller-validated override)
//  2. Check $(dirname binPath)/skills — sibling layout produced by
//     `go build ./cmd/grafel` in the repo root (e.g. repo/grafel +
//     repo/skills)
//  3. Check $(dirname binPath)/../skills — one-up layout used by shipped
//     binaries installed under a bin/ subdirectory (e.g. prefix/bin/grafel +
//     prefix/skills)
//  4. Check $GRAFEL_SKILLS_DIR env var if set
//  5. Walk up ancestor directories from dirname(binPath) up to 5 levels,
//     checking <ancestor>/skills at each level — handles arbitrary repo layouts
//     such as repo/build/grafel + repo/skills
//  6. Fall back to the skills EMBEDDED in the binary (#5503): materialise the
//     bundled skills/ tree into a stable on-disk cache and return that path.
//     This is what makes `grafel install` work for a released-tarball,
//     binary-only install where no skills/ directory exists next to the binary
//     (the macOS symptom in #5503). Steps 1–5 still win when a real on-disk
//     source is present (so a dev checkout always uses the live tree).
//
// Returns "" only if every source — including the embedded fallback — is
// unavailable, which signals the caller to error or skip the step.
//
// DiscoverSkillsDir is a thin wrapper over DiscoverSkillsDirVerbose that
// discards the list of attempted paths.
func DiscoverSkillsDir(binPath, skillsSourceDir string) string {
	dir, _ := DiscoverSkillsDirVerbose(binPath, skillsSourceDir)
	return dir
}

// DiscoverSkillsDirVerbose behaves like DiscoverSkillsDir but also returns the
// ordered list of every candidate path it actually checked. When discovery
// fails (returns ""), the attempted list lets the caller build an accurate,
// non-misleading error that names exactly the paths that were probed — rather
// than conflating the failure with the process's current working directory.
//
// Unlike the previous implementation, an explicit --skills-source-dir that
// does not resolve to a directory no longer short-circuits discovery: we record
// the failed explicit path and fall through to the remaining heuristics, so a
// stale/typo'd flag never masks a perfectly valid sibling/env/ancestor dir.
func DiscoverSkillsDirVerbose(binPath, skillsSourceDir string) (string, []string) {
	var attempted []string
	check := func(label, path string) (string, bool) {
		if path == "" {
			return "", false
		}
		attempted = append(attempted, fmt.Sprintf("%s: %s", label, path))
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, true
		}
		return "", false
	}

	// Explicit override from flag or config. We no longer early-return on a
	// stat failure: record the attempt and continue so a bad flag can't mask a
	// valid auto-discovered location.
	if skillsSourceDir != "" {
		if p, ok := check("--skills-source-dir", skillsSourceDir); ok {
			return p, attempted
		}
	}

	if binPath != "" {
		binDir := filepath.Dir(binPath)

		// Try sibling: $(dirname binPath)/skills — produced by `go build` in repo root.
		if p, ok := check("sibling", filepath.Join(binDir, "skills")); ok {
			return p, attempted
		}

		// Try one-up: $(dirname binPath)/../skills — shipped bin/ layout.
		if p, ok := check("one-up", filepath.Join(binDir, "..", "skills")); ok {
			return p, attempted
		}
	}

	// Try env var override (useful in CI or special deployments).
	if envPath := os.Getenv("GRAFEL_SKILLS_DIR"); envPath != "" {
		if p, ok := check("GRAFEL_SKILLS_DIR", envPath); ok {
			return p, attempted
		}
	}

	// Walk up ancestors of binPath (up to 5 levels) looking for a skills/
	// subdirectory.  This handles layouts like repo/build/grafel + repo/skills
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
			if p, ok := check("ancestor", filepath.Join(dir, "skills")); ok {
				return p, attempted
			}
		}
	}

	// Final fallback (#5503): no on-disk skills/ source was found next to the
	// binary or in any ancestor. This is the normal situation for a released
	// tarball install (the binary ships alone). Materialise the skills that are
	// embedded in the binary into a stable cache dir and use that. This is what
	// fixes the macOS symptom where MCP registered but no skills landed.
	if p, err := MaterializeEmbeddedSkills(); err == nil && p != "" {
		attempted = append(attempted, fmt.Sprintf("embedded: %s", p))
		return p, attempted
	} else if err != nil {
		attempted = append(attempted, fmt.Sprintf("embedded: %v", err))
	}

	return "", attempted
}

// embeddedSkillsCacheDir returns the stable on-disk location where the
// binary-embedded skills are materialised: $HOME/.grafel/skills-cache.
// It honours HOME on every platform so tests and sidecar HOMEs resolve
// consistently with the rest of the install lifecycle (DefaultStatePath,
// MCP registration). XDG is intentionally not consulted here because the
// grafel state root is HOME/.grafel everywhere.
func embeddedSkillsCacheDir() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		home = h
	}
	return filepath.Join(home, ".grafel", "skills-cache"), nil
}

// MaterializeEmbeddedSkills extracts the skills bundled into the binary
// (grafelassets.SkillsFS, embedded from the repo-root skills/ tree) into a
// stable cache directory and returns that directory's path.
//
// The returned directory contains one subdirectory per skill (matching the
// SkillNames the rest of the install lifecycle copies/symlinks from), so it is
// a drop-in replacement for an on-disk skills/ source.
//
// It is idempotent: a file is only rewritten when its content differs from the
// embedded copy, so re-installs are cheap and re-materialisation is a no-op
// when nothing changed (e.g. same binary version).
func MaterializeEmbeddedSkills() (string, error) {
	cacheDir, err := embeddedSkillsCacheDir()
	if err != nil {
		return "", err
	}
	// Walk the embedded "skills" tree and mirror it under cacheDir. The embed
	// FS roots paths at "skills/...", so we strip that prefix and write the
	// remainder under cacheDir.
	const root = "skills"
	walkErr := fs.WalkDir(grafelassets.SkillsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(cacheDir, 0o755)
		}
		dst := filepath.Join(cacheDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := grafelassets.SkillsFS.ReadFile(p)
		if err != nil {
			return err
		}
		// Idempotent write: skip if the on-disk file already matches.
		if existing, rerr := os.ReadFile(dst); rerr == nil && bytesEqual(existing, data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if walkErr != nil {
		return "", fmt.Errorf("materialise embedded skills into %s: %w", cacheDir, walkErr)
	}
	return cacheDir, nil
}

// bytesEqual is a tiny helper avoiding a bytes import for one comparison.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// InstallSkillsInClaudeConfigs symlinks the grafel skills into every
// detected Claude Code config directory's skills/ subdirectory.
//
// claudeConfigDirs: list of ~/.claude.json paths (typically from mcpreg.DetectClaudeConfigDirs)
// skillsSourceDir: explicit override of skills location (from --skills-source-dir flag)
// binPath: path to the grafel binary (used to infer skills location)
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

// RemoveSkillsFromClaudeConfigs removes the symlinked grafel skills from
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
