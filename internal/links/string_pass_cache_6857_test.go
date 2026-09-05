package links

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// string_pass_cache_6857_test.go — #6857.
//
// `string_pass` is the ONE hardened source read in this package that fails
// loudly instead of mapping a read failure to "nothing found" (#6839 treats it
// as the reference behaviour). `scanFile` could return before the hardened
// read ever happened: the string-scan cache is keyed on the ABSOLUTE PATH and
// validated on mtime+size only, so a hit returned extractions for a file the
// pass never opened, and the caller could not tell that apart from a
// successful read.
//
// WHAT THE WINDOW ACTUALLY IS, measured rather than assumed. `scanFile` stats
// first and returns a silent skip for a directory, and a stat error (ENOENT,
// ELOOP on a self-referential symlink) never reaches the cache at all — so
// those shapes cannot demonstrate anything. What DOES reach the cache with
// mtime and size intact is:
//
//   - a regular file that is no longer READABLE (chmod 0000 changes neither
//     mtime nor size, so no replacement of the file is needed at all). The
//     hardened read fails with EACCES, which is not ErrNotRegular and
//     therefore propagates — this is the loud arm itself, and it is the one
//     graded below.
//   - a non-regular, non-directory file at the path (a FIFO, socket or
//     device): stat succeeds, IsDir is false, size 0 and mtime is settable.
//     Graded in string_pass_cache_fifo_6857_test.go, which needs mkfifo.
//
// Both are asserted on the EMITTED ARTEFACT — the extractions `scanFile`
// returns — not on a cache-hit counter, because this package has already been
// burned by a test that graded the helper while the real call site went
// unobserved (#6450 Task 2, constant_propagation_test.go:111).

// cachedValue is the sentinel a hand-written cache entry carries. It never
// appears in any file on disk, so seeing it come out of scanFile proves the
// answer came from the cache and not from a read.
const cachedValue = "/api/v1/from-cache"

// diskValue is what the file on disk actually contains.
const diskValue = "/api/v1/from-disk"

// scanCacheFileForTest re-derives scanFile's cache filename for absPath.
//
// It DUPLICATES the derivation rather than calling a shared helper, and that
// is deliberate: extracting it out of scanFile removed the ".json" literal
// from the enclosing function and silently dropped the site from
// internal/safeio's name-chosen-open sweep ledger
// (name_chosen_open_sweep_guard_6478_test.go), i.e. the refactor blinded an
// audit. Duplication here cannot do that, and it cannot drift silently
// either: if scanFile's derivation changes, every seeded entry lands at a
// filename nothing reads and TestScanFile_LegitimateWarmCacheHitIsStillServed
// fails.
func scanCacheFileForTest(cacheDir, absPath string) string {
	h := sha256.Sum256([]byte(absPath))
	return filepath.Join(cacheDir, hex.EncodeToString(h[:])[:32]+".json")
}

// writeSource writes a Go file whose only classifiable literal is value.
func writeSource(t *testing.T, path, value string) {
	t.Helper()
	body := "package p\n\nvar p = \"" + value + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedCache writes a cache entry for absPath carrying cachedValue, stamped
// with mtime/size taken from whatever is at absPath right now — i.e. an entry
// that the mtime+size check accepts.
func seedCache(t *testing.T, cacheDir, absPath, relPath string) {
	t.Helper()
	fi, err := os.Stat(absPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := scanCacheEntry{
		File:  absPath,
		Mtime: fi.ModTime().UnixNano(),
		Size:  fi.Size(),
		Values: []Extraction{{
			Category: catHTTPPath, Value: cachedValue, File: relPath, Line: 3,
		}},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scanCacheFileForTest(cacheDir, absPath), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanFile_CacheHitDoesNotServeAnUnreadableFile is the RED case. The file
// is never touched — only its mode is — so mtime and size still match the
// cache entry exactly, and the pre-fix cache branch hands back extractions for
// a file that cannot be opened at all. The hardened read's EACCES is not
// ErrNotRegular, so the contract says it propagates and aborts the run; a
// cache hit turned that into a confident, wrong answer.
func TestScanFile_CacheHitDoesNotServeAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "scan")
	abs := filepath.Join(dir, "h.go")
	writeSource(t, abs, diskValue)
	seedCache(t, cacheDir, abs, "h.go")

	if err := os.Chmod(abs, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(abs, 0o644) })
	if _, err := os.ReadFile(abs); err == nil {
		t.Skip("running with rights that ignore file mode (root, or a GOOS that does not enforce it); cannot make a read fail this way")
	}

	got, err := scanFile(abs, "h.go", cacheDir)
	if err == nil {
		t.Fatalf("scanFile returned nil error for an unreadable file, with extractions %+v: a cache hit must not answer for a file the hardened read would refuse (#6857)", got)
	}
	if len(got) != 0 {
		t.Fatalf("scanFile returned %d extractions (%+v) alongside the error; want none", len(got), got)
	}
}

// TestScanFile_LegitimateWarmCacheHitIsStillServed is the OTHER direction, and
// it is the one that catches the fix that "works" by not using the cache. The
// file is an ordinary readable regular file whose contents say diskValue,
// while the cache entry says cachedValue; the pass must still answer from the
// cache. A fix that disables the cache, or that re-reads and re-classifies on
// a hit, returns diskValue here and fails.
func TestScanFile_LegitimateWarmCacheHitIsStillServed(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "scan")
	abs := filepath.Join(dir, "h.go")
	writeSource(t, abs, diskValue)
	seedCache(t, cacheDir, abs, "h.go")

	got, err := scanFile(abs, "h.go", cacheDir)
	if err != nil {
		t.Fatalf("scanFile on a readable regular file with a valid cache entry: %v", err)
	}
	if len(got) != 1 || got[0].Value != cachedValue {
		t.Fatalf("warm cache hit returned %+v; want the cached %q — the cache must still be served for a file the hardened read accepts", got, cachedValue)
	}
}

// TestScanFile_StaleCacheEntryStillFallsThroughToTheRead pins the third arm:
// the fix must not turn a MISS into a hit. mtime+size no longer match, so the
// pass reads and classifies, and the sentinel must not appear.
func TestScanFile_StaleCacheEntryStillFallsThroughToTheRead(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "scan")
	abs := filepath.Join(dir, "h.go")
	writeSource(t, abs, diskValue)
	seedCache(t, cacheDir, abs, "h.go")
	// Different size (and mtime): the entry is now stale.
	writeSource(t, abs, "/api/v1/edited")

	got, err := scanFile(abs, "h.go", cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "/api/v1/edited" {
		t.Fatalf("stale cache entry: scanFile returned %+v; want the freshly-read literal", got)
	}
}
