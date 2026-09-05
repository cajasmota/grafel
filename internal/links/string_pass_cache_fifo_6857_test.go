//go:build !windows

package links

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanFile_CacheHitDoesNotServeANonRegularFile is the second half of the
// #6857 window. A FIFO's stat succeeds and IsDir is false, so it sails past
// scanFile's own pre-cache gate; its size is 0 and its mtime is whatever the
// entry says, so a cache entry stamped from it validates. Pre-fix the pass
// returned extractions for a named pipe it never opened. Post-fix the
// hardened path refuses it as ErrNotRegular, which this site maps to a silent
// skip — so the artefact is: no extractions, no error.
//
// The FIFO lives inside t.TempDir() and is removed with it. runWithin and
// mkfifo come from safe_source_read_6823_test.go; the deadline is there
// because the failure mode of an unhardened read here is a hang, not a wrong
// answer.
func TestScanFile_CacheHitDoesNotServeANonRegularFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "scan")
	abs := mkfifo(t, dir, "pipe.go")
	seedCache(t, cacheDir, abs, "pipe.go")

	var (
		got []Extraction
		err error
	)
	runWithin(t, fifoDeadline, "scanFile on a FIFO with a valid cache entry", func() {
		got, err = scanFile(abs, "pipe.go", cacheDir)
	})
	if err != nil {
		t.Fatalf("scanFile on a FIFO: %v; a non-regular file is a skip at this site, not an error", err)
	}
	if len(got) != 0 {
		t.Fatalf("scanFile returned %+v for a named pipe: a cache hit must not answer for a path the hardened read would refuse (#6857)", got)
	}
}

// TestScanFile_WarmCacheHitThroughASymlinkIsStillServed grades the probe's
// SYMLINK POLICY, which is otherwise a free parameter: readSourceFile follows
// symlinks and judges the target, because the indexing walk mints a file
// entity for a symlink-to-regular-file and refusing them would delete
// legitimate coverage. A validation that rejected symlinks would be stricter
// than the read it stands in for, and would silently drop those files from the
// string pass — a different bug wearing the same fix.
func TestScanFile_WarmCacheHitThroughASymlinkIsStillServed(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "scan")
	target := filepath.Join(dir, "target.go")
	writeSource(t, target, diskValue)
	abs := filepath.Join(dir, "link.go")
	if err := os.Symlink(target, abs); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	seedCache(t, cacheDir, abs, "link.go")

	got, err := scanFile(abs, "link.go", cacheDir)
	if err != nil {
		t.Fatalf("scanFile through a symlink to a regular file: %v; the read this validation stands in for accepts it", err)
	}
	if len(got) != 1 || got[0].Value != cachedValue {
		t.Fatalf("symlinked warm cache hit returned %+v; want the cached %q", got, cachedValue)
	}
}
