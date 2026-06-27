// Package diff provides diff-aware (incremental) re-indexing for grafel.
//
// On every full rebuild every source file in a repo is re-processed, even when
// only a handful changed. For a 1 500-file repo with 5 edited files that is
// ~1 495 wasted AST parses. This package tracks per-file SHA-256 content
// hashes in a small manifest persisted to `.grafel/file-index.json` and
// exposes helpers that tell the indexer which files actually changed since the
// last run.
//
// Design goals
//
//   - Zero-overhead on full rebuild: if the manifest is absent or
//     Incremental=false the indexer behaves exactly as before.
//   - Conservative cross-file invalidation: any file that imports a changed
//     file is also marked dirty, so cross-file reference resolution cannot
//     yield stale results.
//   - Git-aware shortcut: when the repo is a git repository, `git diff
//     --name-only HEAD` provides the changed-file list in O(1) without
//     reading every file. Falls back to hash comparison otherwise.
//   - Full-rebuild escape hatch: callers pass Incremental=false (the
//     `grafel rebuild --full` flag) to skip all diffing.
//
// Manifest format (`.grafel/file-index.json`):
//
//	{
//	  "version": 1,
//	  "indexed_at": "2026-05-21T10:00:00Z",
//	  "git_commit": "abc1234",          // empty when not a git repo
//	  "files": {
//	    "src/foo.go": {
//	      "sha256": "e3b0c44298fc1c14...",
//	      "size":   1234,
//	      "mtime":  1716288000000000000
//	    }
//	  }
//	}
package diff

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// Version is the manifest schema version. Increment when the JSON shape changes
// in a backwards-incompatible way; the loader discards manifests with a
// different version (triggering a full rebuild that re-creates the manifest).
const Version = 1

// manifestFile is the name of the per-repo manifest inside the state directory.
const manifestFile = "file-index.json"

// FileEntry holds the hash + metadata for one indexed source file.
type FileEntry struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"` // UnixNano
}

// Manifest is the on-disk representation of the per-repo file index.
type Manifest struct {
	Version   int                  `json:"version"`
	IndexedAt time.Time            `json:"indexed_at"`
	GitCommit string               `json:"git_commit,omitempty"`
	Files     map[string]FileEntry `json:"files"`
}

// LoadManifest reads the manifest from stateDir. Returns an empty manifest
// (ready to accept new entries) when the file is absent, malformed, or has a
// version mismatch.
func LoadManifest(stateDir string) *Manifest {
	path := filepath.Join(stateDir, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return newManifest()
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version != Version {
		return newManifest()
	}
	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}
	return &m
}

// SaveManifest atomically writes m to stateDir. It sets IndexedAt and captures
// the current HEAD commit (best-effort). Returns nil on success.
func SaveManifest(stateDir, repoPath string, m *Manifest) error {
	m.IndexedAt = time.Now().UTC()
	m.GitCommit = headCommit(repoPath)

	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}

	// Atomic write: write to a temp file then rename.
	tmp := filepath.Join(stateDir, manifestFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write manifest tmp: %w", err)
	}
	dst := filepath.Join(stateDir, manifestFile)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}

// newManifest returns an empty, valid manifest.
func newManifest() *Manifest {
	return &Manifest{
		Version: Version,
		Files:   make(map[string]FileEntry),
	}
}

// Filter partitions relPaths into (changed, unchanged).
//
// A file is "changed" when:
//   - It has no entry in the manifest (new file), or
//   - Its mtime or size differs from the manifest entry AND its SHA-256
//     content hash differs (two-stage check: fast stat, then hash only on
//     mtime/size mismatch).
//
// relPaths must be repo-relative (forward-slash, no leading slash) as
// returned by walk.WalkRepo. absRepo is the absolute repo root; it is
// joined with each relPath to form the absolute path for stat/hash.
//
// Cross-file invalidation: any relPath whose basename (import target) appears
// as a changed file's basename is also marked changed. This is a conservative
// approximation — a proper import-graph traversal is left for a future pass.
func Filter(absRepo string, relPaths []string, manifest *Manifest) (changed, unchanged []string) {
	// Phase 1: classify each file as dirty or clean.
	dirty := make(map[string]bool, len(relPaths))
	for _, rel := range relPaths {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if isChanged(abs, rel, manifest) {
			dirty[rel] = true
		}
	}

	// Phase 2: cross-file invalidation.
	// Build a set of base names (without extension) of dirty files, then mark
	// any file whose own base name suffix-matches a dirty name as also dirty.
	// This catches "anyone that might import a changed module".
	dirtyBases := make(map[string]bool, len(dirty))
	for rel := range dirty {
		dirtyBases[moduleBase(rel)] = true
	}
	for _, rel := range relPaths {
		if dirty[rel] {
			continue
		}
		if dirtyBases[moduleBase(rel)] {
			dirty[rel] = true
		}
	}

	changed = make([]string, 0, len(dirty))
	unchanged = make([]string, 0, len(relPaths)-len(dirty))
	for _, rel := range relPaths {
		if dirty[rel] {
			changed = append(changed, rel)
		} else {
			unchanged = append(unchanged, rel)
		}
	}
	return changed, unchanged
}

// UpdateManifest records the current on-disk state for every file in
// relPaths into m. Call this after a successful index write so the next
// incremental run has accurate baseline hashes.
func UpdateManifest(absRepo string, relPaths []string, m *Manifest) {
	var mu sync.Mutex
	// Best-effort parallel hash for large repos.
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, rel := range relPaths {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			abs := filepath.Join(absRepo, filepath.FromSlash(r))
			entry, err := hashFile(abs)
			if err != nil {
				return
			}
			mu.Lock()
			m.Files[r] = entry
			mu.Unlock()
		}(rel)
	}
	wg.Wait()

	// Reconcile (#5667): drop entries for files no longer in the walked set so
	// the manifest cannot retain stale records — e.g. a file that became
	// gitignored (build artifacts now excluded by walk.WalkRepo). All callers
	// pass the complete current walk, so membership in relPaths is
	// authoritative. Without this prune an entry, once added, is immortal: a
	// now-ignored file is reported as "deleted" on every pass, perpetually
	// tripping the too-many-changed full-reindex fallback and pinning the daemon.
	want := make(map[string]struct{}, len(relPaths))
	for _, r := range relPaths {
		want[r] = struct{}{}
	}
	for k := range m.Files {
		if _, ok := want[k]; !ok {
			delete(m.Files, k)
		}
	}
}

// GitChangedFiles uses `git diff --name-only HEAD` to return the set of
// repo-relative paths changed since the last HEAD commit. Returns nil when
// the repo is not a git repository or git is not available.
func GitChangedFiles(repoPath string) (map[string]bool, error) {
	// Verify this is a git repo. Bounded (#5286): a stuck git child during heavy
	// churn must not wedge the index worker — a timeout here is treated as "not
	// a git repo" so FilterWithGit falls back to hash comparison.
	if _, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "--is-inside-work-tree"); !ok {
		return nil, nil // not a git repo (or git wedged) — not an error
	}

	// git diff --name-only HEAD: tracked files that differ from HEAD.
	// Bounded: on timeout/error return an error so the caller falls back to the
	// full hash-based scan instead of hanging on a U-state git child (#5286).
	diffOut, ok := gitmeta.RunGitBoundedC(repoPath, "diff", "--name-only", "HEAD")
	if !ok {
		// HEAD may not exist in a brand-new repo, or git timed out; either way
		// signal a full-rebuild fallback rather than blocking.
		return nil, fmt.Errorf("git diff HEAD: bounded git failed (timeout or no HEAD)")
	}

	// git ls-files --others --exclude-standard: untracked new files (best-effort,
	// bounded). A timeout here just means we miss untracked files this pass.
	untrackedOut, _ := gitmeta.RunGitBoundedC(repoPath, "ls-files", "--others", "--exclude-standard")

	changed := make(map[string]bool)
	for _, buf := range [][]byte{diffOut, untrackedOut} {
		sc := bufio.NewScanner(bytes.NewReader(buf))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				// git outputs forward-slash paths already on all platforms.
				changed[line] = true
			}
		}
	}
	return changed, nil
}

// FilterWithGit is like Filter but uses git status as a fast first pass when
// the repo is a git repository. Only files reported by git as changed are
// handed to the hash-based Filter; the rest are assumed unchanged.
//
// Falls back to hash-based Filter when:
//   - The repo is not a git repository.
//   - git is not available.
//   - The last manifest commit equals HEAD (nothing changed according to git)
//     but there are new files not yet tracked.
func FilterWithGit(absRepo string, relPaths []string, manifest *Manifest) (changed, unchanged []string) {
	gitChanged, err := GitChangedFiles(absRepo)
	if err != nil || gitChanged == nil {
		// git unavailable or repo is not a git repo — fall back to hash comparison.
		return Filter(absRepo, relPaths, manifest)
	}

	// git-aware path: files reported by git go through hash-based check;
	// files NOT reported by git are trusted as unchanged.
	var gitDirty, gitClean []string
	for _, rel := range relPaths {
		if gitChanged[rel] {
			gitDirty = append(gitDirty, rel)
		} else {
			gitClean = append(gitClean, rel)
		}
	}

	// Hash-check only the git-reported dirty files.
	dirtySet := make(map[string]bool)
	for _, rel := range gitDirty {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if isChanged(abs, rel, manifest) {
			dirtySet[rel] = true
		}
	}

	// Cross-file invalidation within the git-dirty set.
	dirtyBases := make(map[string]bool, len(dirtySet))
	for rel := range dirtySet {
		dirtyBases[moduleBase(rel)] = true
	}
	// Re-check git-clean files only when a dirty base matches.
	var secondPassClean []string
	for _, rel := range gitClean {
		if dirtyBases[moduleBase(rel)] {
			dirtySet[rel] = true
		} else {
			secondPassClean = append(secondPassClean, rel)
		}
	}

	changed = make([]string, 0, len(dirtySet))
	unchanged = make([]string, 0, len(secondPassClean))
	for _, rel := range relPaths {
		if dirtySet[rel] {
			changed = append(changed, rel)
		}
	}
	unchanged = secondPassClean
	return changed, unchanged
}

// Stats holds incremental-run statistics surfaced to the caller.
type Stats struct {
	Total     int // total files discovered
	Changed   int // files that will be re-processed
	Unchanged int // files skipped (cache hit)
}

// CacheHitRate returns the cache-hit percentage (0–100).
func (s Stats) CacheHitRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return 100.0 * float64(s.Unchanged) / float64(s.Total)
}

// isChanged returns true when relPath must be re-extracted (new file, or mtime
// and size changed with a differing hash).
func isChanged(absPath, relPath string, manifest *Manifest) bool {
	entry, ok := manifest.Files[relPath]
	if !ok {
		return true // new file
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return true // assume changed on error
	}
	if info.Size() == entry.Size && info.ModTime().UnixNano() == entry.Mtime {
		return false // fast path: unchanged
	}
	// mtime or size differs — verify with hash.
	newEntry, err := hashFile(absPath)
	if err != nil {
		return true
	}
	return newEntry.SHA256 != entry.SHA256
}

// hashFile computes the SHA-256 of the file at path and returns a FileEntry.
func hashFile(path string) (FileEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileEntry{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FileEntry{}, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileEntry{}, err
	}
	return FileEntry{
		SHA256: hex.EncodeToString(h.Sum(nil)),
		Size:   info.Size(),
		Mtime:  info.ModTime().UnixNano(),
	}, nil
}

// moduleBase returns the stem of a file path without extension, used for
// conservative cross-file invalidation (e.g. "src/user.go" → "user").
func moduleBase(relPath string) string {
	base := filepath.Base(relPath)
	if ext := filepath.Ext(base); ext != "" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

// headCommit returns the short HEAD commit hash for the repo at repoPath, or
// empty string if git is unavailable or this is not a git repo.
func headCommit(repoPath string) string {
	// Bounded (#5286): never let a stuck git child hang the index worker.
	out, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "--short", "HEAD")
	if !ok {
		return ""
	}
	return strings.TrimSpace(string(out))
}
