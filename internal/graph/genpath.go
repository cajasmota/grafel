// Package graph — genpath.go implements the generation-file + pointer layout
// for the on-disk FlatBuffers graph (issue #5891, "Approach A").
//
// # Why this exists
//
// Historically every reindex overwrote a single fixed file, <dir>/graph.fb,
// via a tmp+rename. On Windows renaming OVER a file that another process still
// has memory-mapped fails with ERROR_USER_MAPPED_FILE — precisely the case for
// grafel's serve/MCP zero-copy reader, which keeps graph.fb mmap'd while agents
// query it. That hazard forced the mmap serve path OFF by default on Windows.
//
// The generation layout removes the rename-over-a-mapped-file entirely:
//
//   - Each write emits a BRAND-NEW file graph.<gen>.fb (gen = monotonically
//     increasing integer) and then atomically writes a tiny `current` pointer
//     file naming the active generation. No existing (possibly mapped) file is
//     ever renamed over.
//   - Readers resolve the active file through CurrentGraphPath: read `current`
//     → return <dir>/graph.<gen>.fb; if the pointer is absent (a legacy repo
//     that has not been reindexed since the migration) fall back to the flat
//     <dir>/graph.fb. This makes the migration LAZY: existing flat-file repos
//     keep working untouched and pick up the gen layout only on their next
//     naturally-triggered reindex. There is NO forced reindex and NO
//     fbversion bump.
//
// The `.fb` suffix is preserved on gen files (graph.<gen>.fb) so existing
// strings.HasSuffix(path, ".fb") checks keep working unchanged.
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// currentPointerName is the tiny pointer file naming the active generation.
const currentPointerName = "current"

// flatGraphName is the legacy fixed graph filename used before the gen layout
// and still used as the resolver's fallback for un-migrated repos.
const flatGraphName = "graph.fb"

// genFileRe matches a generation graph file: graph.<digits>.fb.
var genFileRe = regexp.MustCompile(`^graph\.(\d+)\.fb$`)

// GenFileName renders the on-disk filename for a given generation.
func GenFileName(gen uint64) string {
	return fmt.Sprintf("graph.%d.fb", gen)
}

// IsGraphFileName reports whether base (a bare filename, not a path) is a
// FlatBuffers graph file recognised by the gen layout: the legacy flat
// "graph.fb" or a generation file "graph.<gen>.fb". Enumeration/GC callers use
// this so a directory walk that previously matched only "graph.fb" also sees
// the generation files.
func IsGraphFileName(base string) bool {
	if base == flatGraphName {
		return true
	}
	_, ok := parseGen(base)
	return ok
}

// parseGen returns the generation integer encoded in a bare filename
// (graph.<gen>.fb) and whether it matched.
func parseGen(name string) (uint64, bool) {
	m := genFileRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// readPointer returns the gen filename named by <dir>/current, validated
// against the graph.<gen>.fb shape so a corrupt/hostile pointer can never
// escape dir via path traversal. ok is false when the pointer is absent,
// unreadable, or malformed.
func readPointer(dir string) (name string, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, currentPointerName))
	if err != nil {
		return "", false
	}
	name = strings.TrimSpace(string(data))
	if _, valid := parseGen(name); !valid {
		return "", false
	}
	return name, true
}

// CurrentGraphPath resolves the active graph file for a state dir. It is the
// single central resolver every reader routes through:
//
//  1. If <dir>/current names an existing graph.<gen>.fb, return that path.
//  2. Otherwise fall back to the legacy flat <dir>/graph.fb path.
//
// The flat path is returned even when it does not exist on disk — callers stat
// or open it and handle absence, exactly as they did before the gen layout.
// This preserves the pre-existing "graph absent" semantics for a brand-new,
// never-indexed repo (no pointer, no flat file).
func CurrentGraphPath(dir string) string {
	if dir == "" {
		return ""
	}
	if name, ok := readPointer(dir); ok {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		// Pointer names a missing gen file (torn write / manual deletion) —
		// fall through to the flat fallback rather than returning a
		// guaranteed-ENOENT gen path.
	}
	return filepath.Join(dir, flatGraphName)
}

// NextGen returns the next generation integer to write in dir: one greater
// than the maximum generation observed among the `current` pointer and every
// graph.<gen>.fb already present. Returns 1 for a dir that has never held a
// gen file. Scanning the directory (not merely trusting the pointer) keeps the
// sequence monotonic even if the pointer was manually deleted while gen files
// remain, so a stale reader never resolves a freshly-written gen to an older
// file.
func NextGen(dir string) uint64 {
	var maxGen uint64
	if name, ok := readPointer(dir); ok {
		if v, _ := parseGen(name); v > maxGen {
			maxGen = v
		}
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			if v, ok := parseGen(e.Name()); ok && v > maxGen {
				maxGen = v
			}
		}
	}
	return maxGen + 1
}

// pointerFlipRetries / pointerFlipRetryDelay bound the pointer-rename retry
// loop. On Unix the rename-over succeeds on the first try. On Windows, a
// concurrent resolver mid os.ReadFile of `current` can briefly hold it open
// and make the rename fail transiently with a sharing violation — the pointer
// is a tiny file read in microseconds, so a short bounded backoff rides that
// window out. This is NOT the mmap hazard (the pointer is never mapped); it is
// only the same brief open/read/close race the overlay swap already tolerates.
var (
	pointerFlipRetries    = 40
	pointerFlipRetryDelay = 5 * time.Millisecond
)

// WriteCurrentPointer atomically points <dir>/current at genName (a bare
// graph.<gen>.fb filename) via a sibling .tmp + rename. The pointer file is
// tiny and NEVER memory-mapped, so this rename-over is safe on every platform
// (it is not the ERROR_USER_MAPPED_FILE hazard the gen layout removes); a
// bounded retry absorbs the transient Windows reader-open race.
func WriteCurrentPointer(dir, genName string) error {
	if _, ok := parseGen(genName); !ok {
		return fmt.Errorf("graph.WriteCurrentPointer: invalid gen name %q", genName)
	}
	tmp := filepath.Join(dir, currentPointerName+".tmp")
	if err := os.WriteFile(tmp, []byte(genName+"\n"), 0o644); err != nil {
		return fmt.Errorf("graph.WriteCurrentPointer: write tmp: %w", err)
	}
	dst := filepath.Join(dir, currentPointerName)
	var err error
	for i := 0; i < pointerFlipRetries; i++ {
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(pointerFlipRetryDelay)
	}
	os.Remove(tmp)
	return fmt.Errorf("graph.WriteCurrentPointer: rename: %w", err)
}

// GCStaleGens best-effort unlinks generation files older than the
// immediately-previous generation, keeping the current gen and the one before
// it. The previous gen is retained because serve/MCP may still have it mapped
// during a reload overlap (it swaps to the new gen on its next mtime-drift
// poll); deleting it out from under an active mmap would be unsafe on Unix and
// impossible on Windows.
//
// This is STRICTLY best-effort and MUST NOT affect the caller's success: a
// still-mapped gen on Windows fails to delete with a sharing violation, which
// is silently ignored and swept on a later write. A GC error never propagates.
// Returns the list of gen files actually removed (for tests/observability).
func GCStaleGens(dir string, currentGen uint64) []string {
	if currentGen <= 1 {
		return nil // nothing older than the immediately-previous gen exists
	}
	keepFrom := currentGen - 1 // keep currentGen and currentGen-1
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		v, ok := parseGen(e.Name())
		if !ok || v >= keepFrom {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed = append(removed, e.Name())
		}
		// On failure (e.g. Windows ERROR_SHARING_VIOLATION on a still-mapped
		// gen) swallow the error and try again on the next write. Never fail.
	}
	return removed
}

// WriteGenGraph writes buf as a NEW generation file in dir, flips the `current`
// pointer to it, and best-effort GCs stale generations. It is the single
// producer primitive behind every writer (full-index and incremental): it
// never renames over an existing (possibly mapped) file — the gen filename is
// freshly allocated by NextGen, so its tmp+rename lands on a name nothing has
// open. Returns the absolute path of the gen file written, which callers pass
// to directory-keyed sidecar writers (WriteSidecar keys on filepath.Dir).
//
// The `current` pointer is flipped only AFTER the gen file is durably renamed
// into place, so a reader never resolves the pointer to a half-written file.
// A GC failure after the flip does not fail the write (see GCStaleGens).
func WriteGenGraph(dir string, buf []byte) (genPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: mkdir %s: %w", dir, err)
	}
	gen := NextGen(dir)
	name := GenFileName(gen)
	genPath = filepath.Join(dir, name)
	tmp := genPath + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: write tmp: %w", err)
	}
	if err := os.Rename(tmp, genPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("graph.WriteGenGraph: rename gen: %w", err)
	}
	if err := WriteCurrentPointer(dir, name); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: flip pointer: %w", err)
	}
	GCStaleGens(dir, gen) // best-effort; never fails the write
	return genPath, nil
}
