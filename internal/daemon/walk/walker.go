// Package walk provides the repo file-walker used by the indexer.
// It combines five skip layers at directory-entry time:
//
//   - Layer 1 (P0): .gitignore semantics (root + nested, lazily loaded)
//   - Layer 2 (P1): extended hard-coded skip list
//   - Layer 3 (P2): .grafelignore overlay
//   - Layer 4 (P3): .gitattributes linguist-generated=true wildcard
//   - Layer 5 (P4): git sparse-checkout — files not present in the sparse
//     pattern set are silently skipped; directories that have no matching
//     descendants are entered but yield no files (#2181 / M4 of #2175).
//
// Directory-level skipping avoids enumerating every file inside build/
// cache trees — the key performance win for large mobile repos.
package walk

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// SkipEntry is one directory that was skipped during a walk.
type SkipEntry struct {
	// AbsPath is the absolute path of the skipped directory.
	AbsPath string
	// Rule is a human-readable description of the matching rule, e.g.
	// ".gitignore line 23", "hardcoded", ".grafelignore line 5".
	Rule string
}

// Options controls walker behaviour.
type Options struct {
	// PrintSkipped, when non-nil, receives one SkipEntry per skipped dir.
	PrintSkipped io.Writer

	// AdditionalSkipDirs extends the hard-coded skip list with per-repo
	// names from fleet.json's additional_skip_dirs field.
	AdditionalSkipDirs []string

	// Sparse holds the result of probing the repo for git sparse-checkout
	// state (Layer 5 / P4). When Sparse.IsSparse is true, files whose
	// repo-relative path is not covered by the sparse pattern set are
	// silently skipped — no extraction error is raised. Callers obtain
	// this by calling gitmeta.ProbeRepo before invoking WalkRepo.
	//
	// When nil or zero-value (IsSparse=false), no sparse filtering is applied.
	Sparse *gitmeta.SparseInfo
}

// WalkRepo walks root and returns repo-relative file paths (forward-slash,
// no leading slash). Directories that match any skip layer are not entered.
// opts may be nil (defaults used).
func WalkRepo(root string, opts *Options) ([]string, []SkipEntry, error) {
	if opts == nil {
		opts = &Options{}
	}

	// TCC guard (#5296): refuse outright to index a repo ROOT that resolves
	// into one of the user's protected macOS media folders (~/Music, ~/Photos,
	// ...) or is itself a media-library bundle. Walking such a tree pops a
	// macOS privacy prompt ("grafel would like to access your Music/Photos").
	if absRoot, aerr := filepath.Abs(root); aerr == nil {
		if protected, reason := IsProtectedPath(absRoot); protected {
			if opts.PrintSkipped != nil {
				fmt.Fprintf(opts.PrintSkipped, "[WARN] refusing to index %s — %s; this path lives in a protected location and will not be walked (#5296)\n", absRoot, reason)
			}
			return nil, []SkipEntry{{AbsPath: absRoot, Rule: "protected:" + reason}}, fmt.Errorf("walk: refusing to index protected path %s (%s)", absRoot, reason)
		}
	}

	// Build the extra skip set from opts (merged with the hard-coded list).
	extraSkip := make(map[string]struct{})
	for _, d := range opts.AdditionalSkipDirs {
		extraSkip[d] = struct{}{}
	}

	var files []string
	var skipped []SkipEntry

	// dirCount tracks directories entered so we can WARN once if the tree
	// blows past the watch-dir cap — a strong signal the path is not a real
	// code repo (#5296: the live failure walked 875 dirs of a 588MB non-code
	// tree). A value <= 0 disables the cap.
	dirCap := WatchDirCap()
	dirCount := 0
	capWarned := false

	// igStack tracks .gitignore/.grafelignore files as we descend.
	var igStack IgnoreStack

	// Load the root-level .gitignore and .grafelignore.
	rootGit, _ := ParseIgnoreFile("", filepath.Join(root, ".gitignore"), ".gitignore")
	rootArchi, _ := ParseIgnoreFile("", filepath.Join(root, ".grafelignore"), ".grafelignore")
	igStack.Push(rootGit)
	igStack.Push(rootArchi)

	// depthStack tracks which stack entries were pushed at each depth so
	// we can Pop when leaving a directory.
	// key: absolute dir path → count of entries pushed when entering it.
	type entry struct {
		absDir string
		count  int
	}
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

			// TCC guard (#5296): never descend into protected macOS media
			// folders or media-library bundles. This is checked before the
			// gitignore/hardcoded layers because descending even once is what
			// trips the privacy prompt.
			if protected, reason := IsProtectedPath(absPath); protected {
				rule := "protected:" + reason
				skipped = append(skipped, SkipEntry{AbsPath: absPath, Rule: rule})
				if opts.PrintSkipped != nil {
					fmt.Fprintf(opts.PrintSkipped, "[skip] %s (rule: %s)\n", absPath, rule)
				}
				return filepath.SkipDir
			}

			// Watch-dir cap tripwire: once the tree exceeds the cap, WARN once
			// and stop descending into further subtrees. Real code repos stay
			// well under the (generous) cap; a non-code tree that blows past it
			// is almost certainly a media/asset folder, not source.
			if dirCap > 0 {
				dirCount++
				if dirCount > dirCap {
					if !capWarned {
						capWarned = true
						if opts.PrintSkipped != nil {
							fmt.Fprintf(opts.PrintSkipped, "[WARN] %s exceeded the %d-directory cap; this may not be a real code repo — skipping the remaining subtrees (#5296). Override with GRAFEL_WATCH_DIR_CAP.\n", root, dirCap)
						}
					}
					skipped = append(skipped, SkipEntry{AbsPath: absPath, Rule: "dir-cap"})
					return filepath.SkipDir
				}
			}

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

			// Check Layer 4 (P3): .gitattributes linguist-generated wildcard.
			if isLinguistGeneratedDir(absPath) {
				rule := "linguist-generated"
				skipped = append(skipped, SkipEntry{AbsPath: absPath, Rule: rule})
				if opts.PrintSkipped != nil {
					fmt.Fprintf(opts.PrintSkipped, "[skip] %s (rule: %s)\n", absPath, rule)
				}
				return filepath.SkipDir
			}

			// Load nested .gitignore/.grafelignore for this directory.
			pushed := 0
			nestedGit, _ := ParseIgnoreFile(rel, filepath.Join(absPath, ".gitignore"), ".gitignore")
			if nestedGit != nil && len(nestedGit.patterns) > 0 {
				igStack.Push(nestedGit)
				pushed++
			}
			nestedArchi, _ := ParseIgnoreFile(rel, filepath.Join(absPath, ".grafelignore"), ".grafelignore")
			if nestedArchi != nil && len(nestedArchi.patterns) > 0 {
				igStack.Push(nestedArchi)
				pushed++
			}
			if pushed > 0 {
				depthEntries = append(depthEntries, entry{absDir: absPath, count: pushed})
			}

			return nil
		}

		// It's a file. Filter by extension (issue #1629) — binary / image /
		// media / archive / compiled files never carry source-graph content.
		if shouldSkipFileByExt(d.Name()) {
			return nil
		}

		// Layer 5 (P4): sparse-checkout filter (#2181 / M4 of #2175).
		// When the repo uses git sparse-checkout, only index files whose
		// path is included in the sparse pattern set. Missing files are
		// silently skipped — no error is raised, matching the semantics of
		// a regular git sparse checkout (absent files are simply not present).
		if opts.Sparse != nil && opts.Sparse.IsSparse {
			if !gitmeta.IsPathIncluded(*opts.Sparse, rel) {
				return nil
			}
		}

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
	// *.egg-info Python packaging dirs — match by suffix.
	if strings.HasSuffix(base, ".egg-info") {
		return "egg-info", true
	}
	if _, ok := extra[base]; ok {
		return "additional_skip_dirs", true
	}
	return "", false
}

// defaultWalkerSkipDirs returns a copy of the hard-coded directory-basename
// skip set used by the walker (issue #1629). The set is grouped by category
// in the variable declaration below; callers should treat it as read-only.
func defaultWalkerSkipDirs() map[string]struct{} {
	out := make(map[string]struct{}, len(hardcodedSkipDirs))
	for k, v := range hardcodedSkipDirs {
		out[k] = v
	}
	return out
}

// defaultWalkerSkipExtensions returns a copy of the hard-coded file-extension
// skip set used by the walker (issue #1629). Extensions are stored lowercase,
// with the leading dot (".png", ".mp4", ...).
func defaultWalkerSkipExtensions() map[string]struct{} {
	out := make(map[string]struct{}, len(hardcodedSkipExtensions))
	for k, v := range hardcodedSkipExtensions {
		out[k] = v
	}
	return out
}

// shouldSkipFileByExt reports whether a file should be skipped purely based
// on its extension (issue #1629). Binary / image / archive / media files
// never carry source-graph content but appear all over real repos
// (assets/, public/, etc.). Filtering by extension catches them even when
// the containing directory does not match a skip-list name.
func shouldSkipFileByExt(name string) bool {
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return false
	}
	ext := strings.ToLower(name[dot:])
	_, ok := hardcodedSkipExtensions[ext]
	return ok
}

// hardcodedSkipDirs is the extended set of well-known build/cache /
// tool-agent / asset / generated-docs directory basenames that are never
// source code. This is layer 2 (P1). The .gitignore layer (P0) handles
// repos with a clean .gitignore; this list is the backstop for repos that
// don't and for noise that is not usually .gitignored (asset trees,
// tool-agent dirs, hand-authored docs).
//
// Categories are grouped for readability. To extend at runtime use
// fleet.json `additional_skip_dirs` — surfaced via WithAdditionalSkipDirs.
//
// IMPORTANT: "build" / "dist" / "docs" are generic names that CAN
// legitimately contain source in some projects. The .gitignore layer is
// the primary signal; if a project really has source under docs/, set
// `additional_skip_dirs` to override or move the source out of docs/.
var hardcodedSkipDirs = map[string]struct{}{
	// VCS
	".git": {},
	".hg":  {},
	".svn": {},

	// JS / TS build output + caches
	"node_modules":  {},
	"dist":          {},
	"build":         {},
	"out":           {},
	".next":         {},
	".nuxt":         {},
	"coverage":      {},
	".cache":        {},
	".expo":         {},
	".expo-shared":  {},
	".parcel-cache": {},
	".turbo":        {},

	// Go / Rust / Java / Python build output + caches
	"vendor":        {},
	"target":        {},
	"bin":           {},
	"obj":           {},
	"__pycache__":   {},
	".pytest_cache": {},
	".mypy_cache":   {},
	".ruff_cache":   {},
	".tox":          {},

	// Python virtualenvs
	"venv":  {},
	".venv": {},
	"env":   {},

	// iOS / Xcode / CocoaPods
	"Pods":        {},
	"DerivedData": {},
	"xcuserdata":  {},
	".swiftpm":    {},

	// Android / Gradle / Terraform
	".gradle":    {},
	"captures":   {},
	".terraform": {},

	// Mobile build outputs
	"APK":      {},
	"IPA":      {},
	"Builds":   {},
	"Releases": {},

	// Prior-tool outputs
	"graphify-out": {},
	"gfleet-out":   {},
	".grafel-out":  {},
	".grafel":      {},

	// IDE / editor metadata
	".vscode":  {},
	".idea":    {},
	".vs":      {},
	".fleet":   {},
	".project": {},

	// Tool / agent dirs (issue #1629) — checked-in tool-config noise that
	// is not source and should never enter the graph. Cover the popular
	// AI / pair-programming and CI metadata dirs.
	".github":          {},
	".gitlab":          {},
	".circleci":        {},
	".husky":           {},
	".devcontainer":    {},
	".claude":          {},
	".claude-personal": {},
	".cursor":          {},
	".windsurf":        {},
	".aider":           {},
	".aider.tags":      {},
	".gemini":          {},
	".continue":        {},
	".tabnine":         {},
	".copilot":         {},
	".kalani":          {},
	".archicraft":      {},

	// Asset / binary / media dirs (issue #1629) — non-source by convention.
	// Binary file extensions are also filtered (see hardcodedSkipExtensions),
	// but skipping the directory avoids enumerating thousands of entries.
	"assets": {},
	"images": {},
	"img":    {},
	"media":  {},
	"fonts":  {},
	"icons":  {},
	"static": {},

	// Generated / hand-authored docs (issue #1629). With #1658, generated
	// docs live in the daemon store, NOT the repo. Remaining repo docs/
	// dirs are mostly legacy/hand-authored markdown which is not source
	// for the graph. Override via additional_skip_dirs if a project
	// really has code under docs/.
	"docs":    {},
	"doc":     {},
	"docsite": {},
	"_site":   {},
	"site":    {},
	"_book":   {},
	"_posts":  {},
	"_drafts": {},

	// Generated code (MANIFEST §25, D24): protobuf/OpenAPI/gRPC stubs and
	// any directory named "_generated" must be excluded from the graph.
	"_generated": {},
}

// hardcodedSkipExtensions is the set of lowercase file extensions (with
// leading dot) that the walker filters at file-level (issue #1629).
// These are binary, image, audio, video, archive and document formats
// that never carry source-graph content. Filtering at the walker means
// extractors and the graph builder never see them.
var hardcodedSkipExtensions = map[string]struct{}{
	// Raster images
	".png":  {},
	".jpg":  {},
	".jpeg": {},
	".gif":  {},
	".bmp":  {},
	".tiff": {},
	".tif":  {},
	".webp": {},
	".ico":  {},
	".heic": {},
	".heif": {},
	".avif": {},

	// Vector / design
	".svg":    {},
	".ai":     {},
	".eps":    {},
	".psd":    {},
	".sketch": {},
	".fig":    {},
	".xd":     {},

	// Video
	".mp4":  {},
	".mov":  {},
	".webm": {},
	".avi":  {},
	".mkv":  {},
	".m4v":  {},

	// Audio
	".wav":  {},
	".mp3":  {},
	".m4a":  {},
	".ogg":  {},
	".flac": {},
	".aac":  {},

	// Documents
	".pdf":  {},
	".doc":  {},
	".docx": {},
	".ppt":  {},
	".pptx": {},
	".xls":  {},
	".xlsx": {},

	// Archives / packed binaries
	".zip": {},
	".tar": {},
	".gz":  {},
	".tgz": {},
	".bz2": {},
	".xz":  {},
	".7z":  {},
	".rar": {},
	".jar": {},
	".war": {},
	".ear": {},
	".aar": {},
	".apk": {},
	".ipa": {},
	".dmg": {},
	".iso": {},
	".pkg": {},
	".deb": {},
	".rpm": {},

	// Compiled / object code
	".class": {},
	".pyc":   {},
	".pyo":   {},
	".o":     {},
	".a":     {},
	".so":    {},
	".dylib": {},
	".dll":   {},
	".exe":   {},
	".wasm":  {},

	// Fonts
	".ttf":   {},
	".otf":   {},
	".woff":  {},
	".woff2": {},
	".eot":   {},
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

// isLinguistGeneratedDir returns true when a directory contains a
// .gitattributes file that marks all of its content as linguist-generated
// (i.e. it has a line matching "* linguist-generated=true" or
// "* -linguist-detectable" with a wildcard covering all files). This is
// Layer 4 (P3) — a lightweight backstop for generated dirs that use the
// GitHub linguist convention instead of _generated naming.
//
// We only trigger on the strict wildcard pattern (`* linguist-generated=true`)
// to avoid false positives from partial attribute files.
//
// #1721: uses openWithDeadline (same deadline as ParseIgnoreFile) to avoid
// blocking the walker when open(2) wedges on a watched path. On timeout
// returns false (treat as not generated — safe conservative default).
func isLinguistGeneratedDir(absPath string) bool {
	p := filepath.Join(absPath, ".gitattributes")
	f, err := openWithDeadline(p, 5*time.Second)
	if err != nil {
		// os.IsNotExist, ErrIgnoreFileTimeout, or other error — all safe to skip.
		if !errors.Is(err, ErrIgnoreFileTimeout) && !os.IsNotExist(err) {
			// Unexpected error — log-friendly: just return false (no panic).
			_ = err
		}
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Matches lines like: "* linguist-generated=true"
		// (with any amount of whitespace between the glob and the attribute).
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "*" {
			for _, attr := range parts[1:] {
				if strings.EqualFold(attr, "linguist-generated=true") {
					return true
				}
			}
		}
	}
	return false
}
