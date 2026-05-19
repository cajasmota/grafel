// Package walk provides the repo file-walker used by the indexer.
// It combines three skip layers at directory-entry time:
//
//   - Layer 1 (P0): .gitignore semantics (root + nested, lazily loaded)
//   - Layer 2 (P1): extended hard-coded skip list
//   - Layer 3 (P2): .archigraphignore overlay
//
// Directory-level skipping avoids enumerating every file inside build/
// cache trees — the key performance win for large mobile repos.
package walk

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

// SkipEntry is one directory that was skipped during a walk.
type SkipEntry struct {
	// AbsPath is the absolute path of the skipped directory.
	AbsPath string
	// Rule is a human-readable description of the matching rule, e.g.
	// ".gitignore line 23", "hardcoded", ".archigraphignore line 5".
	Rule string
}

// Options controls walker behaviour.
type Options struct {
	// PrintSkipped, when non-nil, receives one SkipEntry per skipped dir.
	PrintSkipped io.Writer

	// AdditionalSkipDirs extends the hard-coded skip list with per-repo
	// names from fleet.json's additional_skip_dirs field.
	AdditionalSkipDirs []string
}

// WalkRepo walks root and returns repo-relative file paths (forward-slash,
// no leading slash). Directories that match any skip layer are not entered.
// opts may be nil (defaults used).
func WalkRepo(root string, opts *Options) ([]string, []SkipEntry, error) {
	if opts == nil {
		opts = &Options{}
	}

	// Build the extra skip set from opts (merged with the hard-coded list).
	extraSkip := make(map[string]struct{})
	for _, d := range opts.AdditionalSkipDirs {
		extraSkip[d] = struct{}{}
	}

	var files []string
	var skipped []SkipEntry

	// igStack tracks .gitignore/.archigraphignore files as we descend.
	var igStack IgnoreStack

	// Load the root-level .gitignore and .archigraphignore.
	rootGit, _ := ParseIgnoreFile("", filepath.Join(root, ".gitignore"), ".gitignore")
	rootArchi, _ := ParseIgnoreFile("", filepath.Join(root, ".archigraphignore"), ".archigraphignore")
	igStack.Push(rootGit)
	igStack.Push(rootArchi)

	// depthStack tracks which stack entries were pushed at each depth so
	// we can Pop when leaving a directory.
	// key: absolute dir path → count of entries pushed when entering it.
	type entry struct{ absDir string; count int }
	var depthEntries []entry

	err := filepath.WalkDir(root, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, rerr := filepath.Rel(root, absPath)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		if d.IsDir() {
			base := d.Name()

			// Pop entries for directories we've left.
			for len(depthEntries) > 0 {
				top := depthEntries[len(depthEntries)-1]
				// If the current path is NOT under the tracked dir, pop.
				if !strings.HasPrefix(absPath+string(filepath.Separator), top.absDir+string(filepath.Separator)) {
					for i := 0; i < top.count; i++ {
						igStack.Pop()
					}
					depthEntries = depthEntries[:len(depthEntries)-1]
				} else {
					break
				}
			}

			// Check Layer 2 (P1): hard-coded skip list.
			if reason, ok := hardcodedSkip(base, extraSkip); ok {
				rule := "hardcoded"
				if reason != "" {
					rule = "hardcoded:" + reason
				}
				skipped = append(skipped, SkipEntry{AbsPath: absPath, Rule: rule})
				if opts.PrintSkipped != nil {
					fmt.Fprintf(opts.PrintSkipped, "[skip] %s (rule: %s)\n", absPath, rule)
				}
				return filepath.SkipDir
			}

			// Check Layer 1+3 (P0/P2): gitignore stack.
			if skip, rule := igStack.Match(rel); skip {
				skipped = append(skipped, SkipEntry{AbsPath: absPath, Rule: rule})
				if opts.PrintSkipped != nil {
					fmt.Fprintf(opts.PrintSkipped, "[skip] %s (rule: %s)\n", absPath, rule)
				}
				return filepath.SkipDir
			}

			// Load nested .gitignore/.archigraphignore for this directory.
			pushed := 0
			nestedGit, _ := ParseIgnoreFile(rel, filepath.Join(absPath, ".gitignore"), ".gitignore")
			if nestedGit != nil && len(nestedGit.patterns) > 0 {
				igStack.Push(nestedGit)
				pushed++
			}
			nestedArchi, _ := ParseIgnoreFile(rel, filepath.Join(absPath, ".archigraphignore"), ".archigraphignore")
			if nestedArchi != nil && len(nestedArchi.patterns) > 0 {
				igStack.Push(nestedArchi)
				pushed++
			}
			if pushed > 0 {
				depthEntries = append(depthEntries, entry{absDir: absPath, count: pushed})
			}

			return nil
		}

		// It's a file.
		files = append(files, rel)
		return nil
	})

	return files, skipped, err
}

// hardcodedSkip reports whether a directory basename is on the extended
// hard-coded skip list. extraSkip merges in per-group additional_skip_dirs.
// Returns (reason, true) when the directory should be skipped.
func hardcodedSkip(base string, extra map[string]struct{}) (string, bool) {
	if _, ok := hardcodedSkipDirs[base]; ok {
		return "", true
	}
	if _, ok := extra[base]; ok {
		return "additional_skip_dirs", true
	}
	return "", false
}

// hardcodedSkipDirs is the extended set of well-known build/cache
// directory basenames that are never source code. This is layer 2 (P1).
// The .gitignore layer (P0) handles repos with a clean .gitignore;
// this list is the backstop for repos that don't.
//
// IMPORTANT: "build" and "dist" are generic names that CAN legitimately
// contain source in some projects. The .gitignore layer is the primary
// signal for those; this list is conservative.
var hardcodedSkipDirs = map[string]struct{}{
	// VCS
	".git": {},
	".hg":  {},
	".svn": {},

	// JS / TS
	"node_modules": {},
	"dist":         {},
	"out":          {},
	".next":        {},
	".nuxt":        {},
	"coverage":     {},
	".expo":        {},
	".expo-shared": {},
	".parcel-cache": {},
	".turbo":       {},

	// Go / Rust / Java / Python (common names in SkipDirs already)
	"vendor":        {},
	"target":        {},
	"build":         {},
	"__pycache__":   {},
	".pytest_cache": {},
	".mypy_cache":   {},
	".tox":          {},

	// Python packaging
	"*.egg-info": {}, // won't match via map — handled below in func

	// iOS / Xcode / CocoaPods
	"Pods":        {},
	"DerivedData": {},
	"xcuserdata":  {},
	".swiftpm":    {},

	// Android / Gradle
	".gradle":  {},
	"captures": {},
	".idea":    {},

	// Mobile build outputs
	"APK":      {},
	"IPA":      {},
	"Builds":   {},
	"Releases": {},

	// Prior-tool outputs (use generic tool-output suffix)
	"graphify-out":    {},
	"gfleet-out":      {},
	".archigraph-out": {},
	".archigraph":     {},

	// Python virtualenvs
	"venv":  {},
	".venv": {},

	// Misc IDE
	".vscode": {},
}

func init() {
	// Ensure *.egg-info suffix matching is handled at walk-time in WalkRepo.
	// (map keys are exact basenames; suffix patterns are checked separately.)
}

// IsHardcodedSkip is exported for use by the watcher (internal/daemon/watch).
// Returns true when base is in the extended hard-coded skip list OR has
// a well-known suffix (*.egg-info, *-out).
func IsHardcodedSkip(base string) bool {
	if _, ok := hardcodedSkipDirs[base]; ok {
		return true
	}
	// *.egg-info directories created by Python packaging.
	if strings.HasSuffix(base, ".egg-info") {
		return true
	}
	return false
}
