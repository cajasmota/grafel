package daemon

// Tests for canonicalizePath + the case-collision fix (#2086).
//
// These tests verify that:
//   - Two inputs that differ only in casing hash to the SAME value when
//     the on-disk directory exists (case-insensitive FS behaviour).
//   - The logic still works correctly on case-sensitive paths (canonical
//     == input when the exact-cased entry is on disk).
//   - The cache is hit on repeated calls (no additional fs walk).
//   - A completely missing path preserves the input casing and still
//     produces a valid (non-panicking, non-empty) hash.
//   - WarnCaseCollisions detects stale store dirs correctly.
//
// All temp directories are created under os.TempDir() — no real project
// paths are referenced so the tests are safe on any machine.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearCanonicalCache evicts all entries from the package-level sync.Map
// so tests start with a clean slate and don't interfere with each other.
func clearCanonicalCache() {
	canonicalCache.Range(func(k, v any) bool {
		canonicalCache.Delete(k)
		return true
	})
}

// TestCanonicalizePathSameCasing verifies that the canonical path equals
// the exact on-disk path when casing matches.
func TestCanonicalizePathSameCasing(t *testing.T) {
	clearCanonicalCache()
	dir := t.TempDir() // already the on-disk casing
	got := canonicalizePath(dir)
	// canonicalizePath should return the same path (or an equivalent
	// absolute path) since the casing is already correct.
	// Use os.SameFile to handle symlinks / trailing sep differences.
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("canonicalized path %q does not exist: %v", got, err)
	}
	wantInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("original path %q does not exist: %v", dir, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("canonicalizePath(%q) = %q, not the same inode", dir, got)
	}
}

// TestRepoStateHashCaseVariants is the core regression test for #2086.
// It creates an actual directory with CamelCase on disk, then hashes
// the correct-cased path and a lower-cased variant. On a
// case-insensitive filesystem (APFS on macOS) both must produce the
// same hash because they refer to the same directory.
//
// On a truly case-sensitive filesystem (Linux ext4, some tmpfs) this
// test is skipped automatically when the two probed paths are
// physically different inodes.
func TestRepoStateHashCaseVariants(t *testing.T) {
	clearCanonicalCache()

	// Create a temp parent and a CamelCase child.
	parent := t.TempDir()
	camel := filepath.Join(parent, "CamelCaseDir")
	if err := os.Mkdir(camel, 0755); err != nil {
		t.Fatalf("mkdir CamelCaseDir: %v", err)
	}
	lower := filepath.Join(parent, "camelcasedir")

	// Probe whether the FS is case-insensitive.
	_, err := os.Stat(lower)
	if err != nil {
		// Lower path doesn't resolve — case-sensitive FS. Skip the
		// collision assertion but still verify the hash doesn't panic.
		t.Logf("case-sensitive filesystem detected: skipping collision assertion, verifying no-panic only")
		h := repoStateHash(camel)
		if h == "" {
			t.Fatal("repoStateHash returned empty string")
		}
		return
	}

	// Case-insensitive: both paths must canonicalize to the same on-disk
	// name and therefore produce the same hash.
	clearCanonicalCache()
	hCamel := repoStateHash(camel)
	clearCanonicalCache()
	hLower := repoStateHash(lower)

	if hCamel != hLower {
		t.Errorf("case-insensitive FS: hash mismatch — camel=%s lower=%s want equal", hCamel, hLower)
	}
}

// TestRepoStateHashMissingPath verifies that a completely non-existent
// path does not panic and produces a non-empty, valid hash string.
func TestRepoStateHashMissingPath(t *testing.T) {
	clearCanonicalCache()
	h := repoStateHash("/tmp/nonexistent-archigraph-test-2086/sub/dir")
	if h == "" {
		t.Fatal("repoStateHash returned empty string for missing path")
	}
	if len(h) != 16 {
		t.Errorf("expected 16-hex-char hash, got %q (len %d)", h, len(h))
	}
	for _, c := range h {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("hash %q contains non-hex char %q", h, c)
		}
	}
}

// TestCanonicalizePathCacheHit verifies that after the first call the
// cache is populated and a subsequent call returns the same value
// without errors.
func TestCanonicalizePathCacheHit(t *testing.T) {
	clearCanonicalCache()
	dir := t.TempDir()

	first := canonicalizePath(dir)
	second := canonicalizePath(dir)

	if first != second {
		t.Errorf("cache hit returned different value: first=%q second=%q", first, second)
	}
	// Verify the cache has an entry.
	_, loaded := canonicalCache.Load(dir)
	if !loaded {
		t.Error("expected cache entry after call, found none")
	}
}

// TestCanonicalizePathEmpty verifies the empty-string fast-return.
func TestCanonicalizePathEmpty(t *testing.T) {
	got := canonicalizePath("")
	if got != "" {
		t.Errorf("canonicalizePath(\"\") = %q, want \"\"", got)
	}
}

// TestWarnCaseCollisions_DetectsStaleDir verifies that WarnCaseCollisions
// reports a stale store dir whose hash was computed from a case-variant
// of the fleet path.
func TestWarnCaseCollisions_DetectsStaleDir(t *testing.T) {
	clearCanonicalCache()
	storeDir := t.TempDir()

	// Simulate: the fleet registers /tmp/.../CamelDir (on-disk casing).
	parent := t.TempDir()
	camelDir := filepath.Join(parent, "CamelDir")
	if err := os.Mkdir(camelDir, 0755); err != nil {
		t.Fatalf("mkdir CamelDir: %v", err)
	}

	// Compute what the canonical slug would be.
	canonicalSlug := repoSlug(canonicalizePath(camelDir))

	// Simulate a stale store entry with the WRONG-casing hash.
	// We do this by computing the hash of the lower-cased variant directly.
	lowerVariant := filepath.Join(parent, "cameldir") // wrong casing
	// We can't call repoSlug on lowerVariant as canonicalizePath will fix
	// it on case-insensitive FS. Construct the stale slug manually by
	// hashing the lower-cased path string directly.
	import_safe_hash := func(p string) string {
		// Inline sha256 + hex to avoid importing crypto in the test body.
		// We already import crypto via the package under test; just call
		// repoStateHash but bypass the cache by using the raw path.
		// We need a stale slug, so we produce one with a different hash.
		// Strategy: create a slug from a path that we know differs.
		return repoSlug(p + "-collision-suffix") // force a different hash
	}
	_ = import_safe_hash
	_ = lowerVariant

	// Simpler: just create a store dir whose name shares the base prefix
	// but has a different hash suffix (anything != canonicalSlug).
	base := strings.SplitN(canonicalSlug, "-", -1)
	basePart := strings.Join(base[:len(base)-1], "-") // everything before last "-<hash>"
	staleSlug := basePart + "-0000000000000000"        // obviously wrong hash
	staleDir := filepath.Join(storeDir, staleSlug)
	if err := os.Mkdir(staleDir, 0755); err != nil {
		t.Fatalf("mkdir staleDir: %v", err)
	}

	// Also create the canonical dir so both exist.
	canonicalDir := filepath.Join(storeDir, canonicalSlug)
	if err := os.Mkdir(canonicalDir, 0755); err != nil {
		t.Fatalf("mkdir canonicalDir: %v", err)
	}

	dups := WarnCaseCollisions(storeDir, []string{camelDir})
	if len(dups) == 0 {
		t.Fatalf("expected WarnCaseCollisions to detect stale dir %q, got none", staleSlug)
	}
	found := false
	for _, pair := range dups {
		if pair[0] == staleDir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected stale dir %q in collision list, got: %v", staleDir, dups)
	}
}

// TestWarnCaseCollisions_NoDuplicates verifies that WarnCaseCollisions
// returns nil when there are no stale store dirs.
func TestWarnCaseCollisions_NoDuplicates(t *testing.T) {
	clearCanonicalCache()
	storeDir := t.TempDir()

	parent := t.TempDir()
	camelDir := filepath.Join(parent, "CamelDir")
	if err := os.Mkdir(camelDir, 0755); err != nil {
		t.Fatalf("mkdir CamelDir: %v", err)
	}

	// Create only the canonical store dir.
	canonicalSlug := repoSlug(camelDir)
	canonicalDir := filepath.Join(storeDir, canonicalSlug)
	if err := os.Mkdir(canonicalDir, 0755); err != nil {
		t.Fatalf("mkdir canonicalDir: %v", err)
	}

	dups := WarnCaseCollisions(storeDir, []string{camelDir})
	if len(dups) != 0 {
		t.Errorf("expected no collisions, got: %v", dups)
	}
}

// TestWarnCaseCollisions_EmptyStore verifies no panic on empty / missing store.
func TestWarnCaseCollisions_EmptyStore(t *testing.T) {
	dups := WarnCaseCollisions("", []string{"/some/repo"})
	if dups != nil {
		t.Errorf("expected nil for empty storeDir, got %v", dups)
	}
	dups = WarnCaseCollisions("/tmp/nonexistent-store-archigraph-2086", []string{"/some/repo"})
	if dups != nil {
		t.Errorf("expected nil for nonexistent storeDir, got %v", dups)
	}
}
