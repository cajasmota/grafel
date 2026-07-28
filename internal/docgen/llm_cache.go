// Package docgen — section-level LLM cache keyed by prompt_hash (ticket E,
// issue #1813 chain).
//
// The cache stores per-section LLM results at:
//
//	~/.grafel/docs/<group>/.llm-cache/<prompt_hash>.json
//
// Design:
//   - Key   = per-section prompt_hash (sha256 of version+section+entity+nodeHash+guidance).
//   - Value = CacheEntry (markdown + word/mermaid metrics + link refs + timestamp).
//   - Read  = returns nil,nil when entry is absent (clean cache-miss semantic).
//   - Write = atomic: write to .tmp sibling, rename into place.
//   - Disabled entirely when NoCache=true in the caller's opts.
//
// Cross-platform: pure Go file I/O, filepath.Join throughout, no syscalls.
package docgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// CacheEntry is the on-disk value stored for one section result.
type CacheEntry struct {
	// PromptHash is the per-section prompt hash used as the filename key.
	PromptHash string `json:"prompt_hash"`
	// Section is the section name (e.g. "overview").
	Section string `json:"section"`
	// Markdown is the LLM-generated prose for the section.
	Markdown string `json:"markdown"`
	// WordCount is the word count of Markdown.
	WordCount int `json:"word_count"`
	// MermaidCount is the number of mermaid blocks in Markdown.
	MermaidCount int `json:"mermaid_count"`
	// LinkRefs holds relative links found in Markdown.
	LinkRefs []string `json:"link_refs"`
	// CachedAt is the RFC3339 timestamp when the entry was written.
	CachedAt string `json:"cached_at"`
}

// cacheFilePath returns the path to the cache file for the given hash.
func cacheFilePath(cacheDir, promptHash string) string {
	return filepath.Join(cacheDir, promptHash+".json")
}

// ReadCache loads a CacheEntry for promptHash from cacheDir.
// Returns (nil, nil) when the entry does not exist — callers treat nil as a
// cache miss without an error. Any other error (permissions, corrupt JSON) is
// returned directly.
func ReadCache(cacheDir, promptHash string) (*CacheEntry, error) {
	path := cacheFilePath(cacheDir, promptHash)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // clean miss
		}
		return nil, fmt.Errorf("read cache entry %q: %w", path, err)
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal cache entry %q: %w", path, err)
	}
	return &entry, nil
}

// WriteCache persists entry to cacheDir/<entry.PromptHash>.json atomically
// (write to .tmp, then rename).  cacheDir is created if it does not exist.
func WriteCache(cacheDir string, entry CacheEntry) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", cacheDir, err)
	}
	if entry.CachedAt == "" {
		entry.CachedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}
	target := cacheFilePath(cacheDir, entry.PromptHash)
	// #6018: unique temp name — docgen passes write this cache from concurrent
	// workers, and two passes computing the same prompt hash aim at one path.
	if err := atomicfile.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("write cache file %q: %w", target, err)
	}
	return nil
}

// CacheStats returns the number of entries and total byte size stored in cacheDir.
// Returns (0, 0, nil) when the directory does not exist.
func CacheStats(cacheDir string) (entries int, totalBytes int64, err error) {
	des, readErr := os.ReadDir(cacheDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("read cache dir %s: %w", cacheDir, readErr)
	}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// Only count <hash>.json files (skip .tmp leftovers and unrelated files).
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		info, statErr := de.Info()
		if statErr != nil {
			continue // skip unreadable entries, don't abort
		}
		entries++
		totalBytes += info.Size()
	}
	return entries, totalBytes, nil
}

// DefaultCacheDir returns the default cache directory for a group:
//
//	~/.grafel/docs/<group>/.llm-cache/
//
// Respects GRAFEL_HOME override (same as tier0/tier1 helpers).
func DefaultCacheDir(group string) (string, error) {
	home, err := tier1HomeDir() // reuse the existing home-dir resolver
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "docs", group, ".llm-cache"), nil
}
